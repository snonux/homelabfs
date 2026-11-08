//go:build linux || freebsd

package runtimeprep

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Output names inside a generated bundle.
const (
	ConfigFile     = "runtime-config.json"
	VolumePlanFile = "volume-init.json"
	TemplateFile   = "request-template.json"
	RecordFile     = "generation.json"
)

// idmapCanonical is the only user-namespace map the controlled guest provider
// admits (init_user_ns, identity over all 2^32-1 IDs); the kernel derives the
// mount's idmap_hash as SHA256 of exactly these bytes (hfs_controlled_idmap).
const idmapCanonical = `{"gid":[[0,0,4294967295]],"uid":[[0,0,4294967295]]}`

// Bundle is one complete generated output, keyed by bundle-relative path.
type Bundle struct {
	Files  map[string][]byte
	Record Record
}

// identities are the fresh static scope/mount UUIDs of one run. Fields that
// the runtime fills from actual service/exporter/FUSE state stay empty.
type identities struct {
	sessions, guestMounts, anchors, nonces          []string
	cluster, export, originNode, originIncarnation  string
	userNamespace, realm, volume, authority, policy string
	registry                                        string
}

// sessionKeys holds one original domain's fresh host and guest receipt keys.
type sessionKeys struct{ host, guest ed25519.PrivateKey }

type generator struct {
	spec     Spec
	output   string
	operator ed25519.PublicKey
	layout   exporterLayout
	ids      identities
	keys     []sessionKeys
	args     []string
}

// Generate builds the bundle for spec, to be written below the absolute output
// directory. random supplies all key and UUID entropy (crypto/rand in the CLI).
// The resulting configuration is validated by the actual runtime validator.
func Generate(ctx context.Context, spec Spec, output string, random io.Reader) (Bundle, error) {
	g := generator{spec: spec, output: output}
	var err error
	if !cleanAbsolute(output) {
		return Bundle{}, fmt.Errorf("require a clean absolute output directory")
	}
	if g.operator, err = validateSpec(spec); err != nil {
		return Bundle{}, err
	}
	if g.layout, err = parseExporterTemplate(spec.Exporter.Args); err != nil {
		return Bundle{}, err
	}
	if err = g.generateSecrets(random); err != nil {
		return Bundle{}, err
	}
	if g.args, err = resolveArgs(spec.Exporter.Args, g.layout.devices, g.ids.sessions); err != nil {
		return Bundle{}, err
	}
	config := g.runtimeConfig()
	if err = validateRuntimeConfig(ctx, config, output); err != nil {
		return Bundle{}, fmt.Errorf("actual runtime validation refused the generated config: %w", err)
	}
	return g.bundle(config)
}

// generateSecrets draws every fresh identity and key for this run.
func (g *generator) generateSecrets(random io.Reader) error {
	ids := &g.ids
	shared := []*string{&ids.cluster, &ids.export, &ids.originNode, &ids.originIncarnation,
		&ids.userNamespace, &ids.realm, &ids.volume}
	for _, field := range shared {
		id, err := freshUUID(random)
		if err != nil {
			return err
		}
		*field = id
	}
	for range sessionCount {
		var values [4]string
		for i := range values {
			id, err := freshUUID(random)
			if err != nil {
				return err
			}
			values[i] = id
		}
		ids.sessions = append(ids.sessions, values[0])
		ids.guestMounts = append(ids.guestMounts, values[1])
		ids.anchors = append(ids.anchors, values[2])
		ids.nonces = append(ids.nonces, values[3])
		keys, err := freshKeys(random)
		if err != nil {
			return err
		}
		g.keys = append(g.keys, keys)
	}
	if err := distinctIdentities(ids); err != nil {
		return err
	}
	// The controlled profile has no normal authority, registry or policy
	// document; these hashes are domain-separated run labels compared only for
	// equality by the bridge, never evidence of a normal authority.
	ids.authority = labelHash(g.spec.RunID, ids.volume, "authority")
	ids.policy = labelHash(g.spec.RunID, ids.volume, "policy")
	ids.registry = labelHash(g.spec.RunID, ids.volume, "registry")
	return nil
}

// runtimeConfig assembles the controlled-runtime/1 configuration.
func (g *generator) runtimeConfig() controlledruntime.RuntimeConfig {
	s := g.spec
	sum, _ := decodeHex(s.Exporter.SHA256, 32, "exporter") // validated in validateSpec
	c := controlledruntime.RuntimeConfig{
		Version: "homelabfs-controlled-runtime/1", VolumeConfig: s.Volume.Config,
		OwnerDirectory: s.OwnerDirectory, EnrollmentFile: s.EnrollmentFile,
		Exporter: controlledruntime.Executable{Path: s.Exporter.Path, SHA256: [32]byte(sum),
			Args: g.args, ControlledBootstrap: true},
		Operator: g.operator, HostKeys: map[string]ed25519.PrivateKey{},
		GuestKeys: map[string]ed25519.PublicKey{}, Views: s.Views, Service: s.Service,
		Providers: s.Providers, Capabilities: s.Capabilities,
		CapabilityIdentities: s.CapabilityIdentities, CredentialRevisions: s.CredentialRevisions,
		Budget: s.Budget, BootstrapSeconds: s.BootstrapSeconds,
	}
	for i, session := range g.ids.sessions {
		c.Scopes = append(c.Scopes, g.scope(i))
		c.HostKeys[session] = g.keys[i].host
		c.GuestKeys[session] = g.keys[i].guest.Public().(ed25519.PublicKey)
	}
	return c
}

// scope is one operator-selected static Context. Configuration, Generation,
// OriginJournal, ExporterIncarnation and FuseSession are placeholders the
// runtime replaces with actual state in its proposal; GuestBoot stays empty
// for the signer to fill from the actual measured boot. OriginNode and
// OriginIncarnation label this disposable controlled origin; no controlled
// runtime check compares them with daemoncli's node.json.
func (g *generator) scope(i int) guestbridge.Context {
	ids := g.ids
	return guestbridge.Context{
		AuthorityHash: ids.authority, Cluster: ids.cluster, Export: ids.export,
		GuestMount: ids.guestMounts[i], OriginIncarnation: ids.originIncarnation,
		OriginNode: ids.originNode, PolicyHash: ids.policy, RegistryHash: ids.registry,
		Session: ids.sessions[i], VM: g.layout.vm, Volume: ids.volume,
	}
}

// distinctIdentities refuses a broken entropy source: every fresh UUID of the
// run must differ, since sessions, mounts and ticket families are compared by
// identity and a collision would silently merge two of them.
func distinctIdentities(ids *identities) error {
	all := []string{ids.cluster, ids.export, ids.originNode, ids.originIncarnation,
		ids.userNamespace, ids.realm, ids.volume}
	for _, list := range [][]string{ids.sessions, ids.guestMounts, ids.anchors, ids.nonces} {
		all = append(all, list...)
	}
	seen := map[string]bool{}
	for _, id := range all {
		if seen[id] {
			return fmt.Errorf("entropy source repeated a UUID")
		}
		seen[id] = true
	}
	return nil
}

func freshUUID(random io.Reader) (string, error) {
	var id fsapi.UUID
	if _, err := io.ReadFull(random, id[:]); err != nil {
		return "", fmt.Errorf("draw UUID entropy: %w", err)
	}
	id[6] = id[6]&0x0f | 0x40 // RFC 4122 version 4
	id[8] = id[8]&0x3f | 0x80 // RFC 4122 variant
	return id.String(), nil
}

func freshKeys(random io.Reader) (sessionKeys, error) {
	var keys sessionKeys
	for _, key := range []*ed25519.PrivateKey{&keys.host, &keys.guest} {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := io.ReadFull(random, seed); err != nil {
			return keys, fmt.Errorf("draw key entropy: %w", err)
		}
		*key = ed25519.NewKeyFromSeed(seed)
	}
	if keys.host.Equal(keys.guest) {
		return keys, fmt.Errorf("entropy source produced identical keys")
	}
	return keys, nil
}

func labelHash(runID, volume, label string) string {
	sum := sha256.Sum256([]byte("homelabfs-runtime-prep/1\x00" + label + "\x00" + runID + "\x00" + volume))
	return hex.EncodeToString(sum[:])
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func keyFile(kind string, index int) string {
	return filepath.Join("keys", fmt.Sprintf("%s-%d.key", kind, index))
}

func marshalFile(value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func utcNow() string { return time.Now().UTC().Format(time.RFC3339) }
