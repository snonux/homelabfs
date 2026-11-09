//go:build linux

package runtimeprep

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/daemoncli"
)

const exampleVM = "e10b6225-44d0-11f1-882a-e8ff1ed7f3d7"

func TestGenerateExampleSpecPassesActualRuntimeValidation(t *testing.T) {
	root := shortRoot(t)
	s := exampleSpec(t)
	operatorFixture(t, root, &s)
	// An unreadable operator key proves the generator never opens it. Root
	// can read a mode-0 file, so that proof is skipped for root; the rest of
	// the test still runs.
	if os.Geteuid() != 0 {
		if err := os.Chmod(s.OperatorKey, 0); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Log("running as root: operator key unreadability proof skipped")
	}
	bundle, output := generateBundle(t, root, s)
	var config controlledruntime.RuntimeConfig
	readBundleJSON(t, output, ConfigFile, &config)
	var template RequestTemplate
	readBundleJSON(t, output, TemplateFile, &template)
	checkScopes(t, config, bundle.Record)
	checkExporter(t, s, config, bundle.Record)
	checkKeys(t, output, config, template)
	checkFiles(t, output, bundle)
	var plan VolumeInitPlan
	readBundleJSON(t, output, VolumePlanFile, &plan)
	if plan.Version != VolumeInitVersion || plan.VolumeID != bundle.Record.Volume || plan.Helper != s.InitHelper.Path ||
		plan.HelperSHA256 != s.InitHelper.SHA256 || plan.Config != config.VolumeConfig {
		t.Fatalf("volume-init plan does not match the generation: %+v", plan)
	}
	if bundle.Record.OperatorPrivateKeyRead || bundle.Record.OperatorKey != s.OperatorKey {
		t.Fatal("record must state that only the operator key path was recorded")
	}
	again, err := Generate(t.Context(), s, filepath.Join(root, "second"), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if again.Record.Volume == bundle.Record.Volume || again.Record.Sessions[0].Session == bundle.Record.Sessions[0].Session ||
		again.Record.Sessions[0].HostPublicKey == bundle.Record.Sessions[0].HostPublicKey {
		t.Fatal("a second generation reused identities or keys")
	}
}

func readBundleJSON(t *testing.T, output, name string, into any) {
	t.Helper()
	if _, err := readStrict(filepath.Join(output, name), into); err != nil {
		t.Fatal(err)
	}
}

func checkScopes(t *testing.T, c controlledruntime.RuntimeConfig, r Record) {
	t.Helper()
	if len(c.Scopes) != 2 || len(r.Sessions) != 2 || c.Scopes[0].Session == c.Scopes[1].Session {
		t.Fatal("expected two distinct sessions")
	}
	for i, scope := range c.Scopes {
		if scope.Session != r.Sessions[i].Session || scope.VM != exampleVM || scope.Volume != r.Volume ||
			scope.GuestMount != r.Sessions[i].GuestMount || scope.GuestBoot != "" {
			t.Fatalf("scope %d does not match the record: %+v", i, scope)
		}
		// Runtime-owned placeholders stay empty for the actual proposal.
		if scope.Configuration+scope.Generation+scope.OriginJournal+scope.ExporterIncarnation+scope.FuseSession != "" {
			t.Fatalf("scope %d pre-fills runtime-owned identities", i)
		}
	}
}

func checkExporter(t *testing.T, s Spec, c controlledruntime.RuntimeConfig, r Record) {
	t.Helper()
	e := c.Exporter
	if e.Path != s.Exporter.Path || hex.EncodeToString(e.SHA256[:]) != s.Exporter.SHA256 || !e.ControlledBootstrap {
		t.Fatal("exporter pin changed")
	}
	for i, slot := range []string{"pci.0.7.0", "pci.0.8.0"} {
		if !slices.Contains(e.Args, slot+".hfs-controller="+r.Sessions[i].Session) ||
			!slices.Contains(e.Args, fmt.Sprintf("%s.sharename=hfs%d", slot, i)) {
			t.Fatalf("session %d not bound to its 9p device", i)
		}
	}
	if strings.Contains(strings.Join(e.Args, " "), "{{") {
		t.Fatal("unresolved template text in exporter argv")
	}
}

func checkKeys(t *testing.T, output string, c controlledruntime.RuntimeConfig, tmpl RequestTemplate) {
	t.Helper()
	for i, scope := range c.Scopes {
		host := readSeed(t, filepath.Join(output, keyFile("host", i)))
		guest := readSeed(t, filepath.Join(output, keyFile("guest", i)))
		if !host.Equal(c.HostKeys[scope.Session]) || !bytes.Equal(guest.Public().(ed25519.PublicKey), c.GuestKeys[scope.Session]) {
			t.Fatalf("session %d key files differ from the runtime config", i)
		}
		if tmpl.Keys[i].Host != filepath.Join(output, keyFile("host", i)) ||
			tmpl.GuestPublicKeys[i] != hex.EncodeToString(guest.Public().(ed25519.PublicKey)) ||
			tmpl.Mounts[i].IDMapHash != "063ee717c983abc52c8cdeaeaee32c62ecb93c8fc88e6f887313db9a6c471f98" ||
			tmpl.Mounts[i].DeviceTag != fmt.Sprintf("hfs%d", i) {
			t.Fatalf("template domain %d inconsistent", i)
		}
	}
}

func readSeed(t *testing.T, path string) ed25519.PrivateKey {
	t.Helper()
	raw, err := readPrivate(path, 64)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := decodeHex(string(raw), ed25519.SeedSize, path)
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(seed)
}

// checkFiles verifies hashes, private modes and that the record holds no seed.
func checkFiles(t *testing.T, output string, b Bundle) {
	t.Helper()
	record, err := os.ReadFile(filepath.Join(output, RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range b.Files {
		info, err := os.Lstat(filepath.Join(output, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s not a private 0600 file", name)
		}
		if strings.HasPrefix(name, "keys/") && bytes.Contains(record, raw) {
			t.Fatalf("record leaks private key %s", name)
		}
		if name != RecordFile && b.Record.Files[name] != sha256Hex(raw) {
			t.Fatalf("record hash of %s wrong", name)
		}
	}
	for _, dir := range []string{output, filepath.Join(output, "keys")} {
		if info, err := os.Lstat(dir); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s not a private 0700 directory", dir)
		}
	}
}

// TestGeneratedVolumeOpensWithRuntimeService initialises the volume exactly as
// tools/lab/runtime-volume-init.py drives the pinned helper (here the same
// daemoncli source, in process) and opens it through the runtime's actual
// controlled service constructor with the generated limits and paths. The
// host filesystem is an explicit local boundary: the test UID stands in for root.
func TestGeneratedVolumeOpensWithRuntimeService(t *testing.T) {
	root := shortRoot(t)
	s := exampleSpec(t)
	operatorFixture(t, root, &s)
	localLayout(t, root, &s)
	bundle, output := generateBundle(t, root, s)
	var plan VolumeInitPlan
	readBundleJSON(t, output, VolumePlanFile, &plan)
	initialiseVolume(t, plan)
	l, err := daemoncli.OpenControlled(t.Context(), plan.Config, s.Service)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if got := l.Service().FrontendInfo().Volume.String(); got != bundle.Record.Volume {
		t.Fatalf("runtime proposal would refuse: service volume %s, scopes name %s", got, bundle.Record.Volume)
	}
	if err := l.CheckRuntimePaths(s.OwnerDirectory, s.EnrollmentFile); err != nil {
		t.Fatal(err)
	}
	if err := l.CheckRuntimePaths(filepath.Join(plan.VolumeDir, "owner"), s.EnrollmentFile); err == nil {
		t.Fatal("owner directory inside the volume accepted")
	}
}

func initialiseVolume(t *testing.T, plan VolumeInitPlan) {
	t.Helper()
	var stdout bytes.Buffer
	if err := daemoncli.Run(t.Context(), []string{"node-init", "--control", plan.ControlDir}, &stdout, &stdout); err != nil {
		t.Fatal(err)
	}
	var node struct {
		FormatVersion int    `json:"format_version"`
		NodeID        string `json:"node_id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &node); err != nil {
		t.Fatal(err)
	}
	writePrivateJSON(t, plan.Config, map[string]any{"format_version": 1, "node_id": node.NodeID,
		"volume_id": plan.VolumeID, "control_dir": plan.ControlDir, "volume_dir": plan.VolumeDir,
		"mountpoint": plan.Mountpoint})
	stdout.Reset()
	if err := daemoncli.Run(t.Context(), []string{"volume-init", "--config", plan.Config, "--disposable-validation"}, &stdout, &stdout); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plan.Mountpoint, 0o700); err != nil {
		t.Fatal(err)
	}
}

func ninePDevice(slot int, tag, session string) []string {
	return []string{"-s", fmt.Sprintf("0:%d:0,virtio-9p", slot), "-o", fmt.Sprintf("pci.0.%d.0.sharename=%s", slot, tag),
		"-o", fmt.Sprintf("pci.0.%d.0.hfs-controller=%s", slot, session)}
}

func exporterArgs(tail []string, parts ...[]string) []string {
	args := []string{"-c", "4", "-m", "6G", "-AHP", "-s", "0:0,hostbridge", "-s", "31:0,lpc",
		"-s", "0:4:0,nvme,/zroot/bhyve/rocky/disk0.img"}
	for _, p := range parts {
		args = append(args, p...)
	}
	return append(args, tail...)
}

// specRefusals names one invalid mutation per generator- or runtime-owned
// rule; names starting with "runtime " must be refused by the actual runtime
// validator, all others by the generator before that validator runs.
func specRefusals() map[string]func(*Spec) {
	vm := []string{"-U", exampleVM, "hfs-zq3-r1"}
	d0, d1 := ninePDevice(7, "hfs0", "{{session:0}}"), ninePDevice(8, "hfs1", "{{session:1}}")
	args := func(parts ...[]string) func(*Spec) {
		return func(s *Spec) { s.Exporter.Args = exporterArgs(vm, parts...) }
	}
	return map[string]func(*Spec){
		"version":           func(s *Spec) { s.Version = "homelabfs-runtime-prep-spec/0" },
		"run id":            func(s *Spec) { s.RunID = "ZQ3" },
		"operator hex":      func(s *Spec) { s.OperatorPublicKey = "2F6D" },
		"operator path":     func(s *Spec) { s.OperatorKey = "operator.pem" },
		"overlap":           func(s *Spec) { s.OwnerDirectory = s.Volume.VolumeDirectory + "/owner" },
		"executable inside": func(s *Spec) { s.InitHelper.Path = s.OwnerDirectory + "/homelabfs" },
		"socket length":     func(s *Spec) { s.OwnerDirectory = "/" + strings.Repeat("o", 100) },
		"exporter hash":     func(s *Spec) { s.Exporter.SHA256 = "00" },
		"controller work":   func(s *Spec) { s.Views.WorkBytes = 256 << 20 },
		"legacy 9p form":    args([]string{"-s", "0:7:0,virtio-9p,sharename=hfs0,hfs-controller={{session:0}}"}, d1),
		"stock 9p export":   args(d0, d1, []string{"-s", "0:9:0,virtio-9p,homelabfs_poc=/var/tmp/x"}),
		"one device":        args(d0),
		"three devices":     args(d0, d1, ninePDevice(9, "hfs2", "{{session:2}}")),
		"duplicate session": args(d0, ninePDevice(8, "hfs1", "{{session:0}}")),
		"duplicate tag":     args(d0, ninePDevice(8, "hfs0", "{{session:1}}")),
		"stray placeholder": args(d0, d1, []string{"-l", "com1,/dev/{{session:1}}"}),
		"ro node":           args(d0, d1, []string{"-o", "pci.0.7.0.ro=true"}),
		"path node":         args(d0, d1, []string{"-o", "pci.0.8.0.path=/var/tmp/x"}),
		"node before slot":  args([]string{"-o", "pci.0.7.0.sharename=hfs0"}, d0, d1),
		"device override":   args(d0, d1, []string{"-o", "pci.0.4.0.device=virtio-9p"}),
		"duplicate slot":    args(d0, d1, []string{"-s", "0:7:0,virtio-9p"}),
		"config file":       args([]string{"-k", "/var/tmp/x.conf"}, d0, d1),
		"unknown option":    args([]string{"-Z"}, d0, d1),
		"missing vm uuid":   func(s *Spec) { s.Exporter.Args = exporterArgs([]string{"hfs-zq3-r1"}, d0, d1) },
		"bad vm uuid":       func(s *Spec) { s.Exporter.Args = exporterArgs([]string{"-U", "E10B", "hfs"}, d0, d1) },
		"no vm name":        func(s *Spec) { s.Exporter.Args = exporterArgs([]string{"-U", exampleVM}, d0, d1) },
		"two vm names":      func(s *Spec) { s.Exporter.Args = exporterArgs(append(vm, "extra"), d0, d1) },
		"too many args":     args(d0, d1, slices.Repeat([]string{"-A"}, maxExporterArgs)),
		"too many bytes":    args(d0, d1, []string{"-l", "com1," + strings.Repeat("x", maxExporterBytes)}),
		"runtime canary":    func(s *Spec) { s.Service.LogicalBytes = 8 << 20 },
		"runtime bootstrap": func(s *Spec) { s.BootstrapSeconds = 121 },
		"runtime host":      func(s *Spec) { s.Budget.HostBytes = 2 << 30 },
		"guest slot floor":  func(s *Spec) { s.Budget.Guest.Reservations = 32 }, // zq3-r3
		"guest floor - 1":   func(s *Spec) { s.Budget.Guest.Reservations = 379 },
		"guest received":    func(s *Spec) { s.Budget.Guest.Received = 59 },
		"guest control":     func(s *Spec) { s.Budget.Guest.ControlBytes = 128 << 20 }, // zq3-r3
		"guest ordinary":    func(s *Spec) { s.Budget.Guest.OrdinaryBytes = 128 << 20 },
		"runtime guest":     func(s *Spec) { s.Budget.Guest.Cookies = 0 },
		"runtime views":     func(s *Spec) { s.Views.Sources = 1 },
	}
}

func TestGenerateRefusals(t *testing.T) {
	for name, mutate := range specRefusals() {
		t.Run(name, func(t *testing.T) {
			s := exampleSpec(t)
			s.OperatorPublicKey = hex.EncodeToString(make([]byte, 32))
			mutate(&s)
			_, err := Generate(t.Context(), s, filepath.Join(shortRoot(t), "out"), rand.Reader)
			if err == nil {
				t.Fatal("invalid spec accepted")
			}
			// Generator-owned checks must refuse before the runtime validator;
			// runtime-owned rules must be refused by the actual validator.
			if strings.HasPrefix(name, "runtime ") != strings.Contains(err.Error(), "actual runtime validation") {
				t.Fatalf("refused by the wrong layer: %v", err)
			}
			t.Log(err)
		})
	}
}

func TestGenerateRefusesShortEntropy(t *testing.T) {
	s := exampleSpec(t)
	s.OperatorPublicKey = hex.EncodeToString(make([]byte, 32))
	output := filepath.Join(shortRoot(t), "out")
	for name, c := range map[string]struct {
		entropy []byte
		want    string
	}{
		"short":    {make([]byte, 40), "draw UUID entropy"},
		"constant": {make([]byte, 4096), "entropy source produced identical keys"},
	} {
		_, err := Generate(t.Context(), s, output, bytes.NewReader(c.entropy))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s entropy: want %q, got %v", name, c.want, err)
		}
	}
}
