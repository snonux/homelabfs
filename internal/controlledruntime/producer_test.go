//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

type sourceRig struct {
	producer              *Producer
	temporary             func() string
	mappingTransport      *guestbridge.Transport
	mappingDirectory      string
	mappingNativeCell     *uint64
	mappingLastNative     *guestbridge.Ack // last native response, for a WAIT continuation reclaim
	t                     *testing.T
	supervisor            *Supervisor
	service               *fsservice.Service
	source                storage.Filesystem
	controller            *viewcontrol.Controller
	provider              *controlledvalidation.Provider
	bridge                *guestbridge.Service
	facts                 *Facts
	admission             *Admission
	installed             installedObservation
	observationFile       string
	scope                 guestbridge.Context
	native                guestbridge.Native
	credentials           guestbridge.Credentials
	credentialCaptures    []guestbridge.Credentials
	key                   ed25519.PrivateKey
	cells, claims         uint64
	peers                 []*sourceRig
	providerFactory       func(int) *controlledvalidation.Provider
	artifactOperator      ed25519.PrivateKey
	artifactGuest         ed25519.PrivateKey
	artifactAuthorization viewcontrol.Authorization
}
type guestCredentialsSetPrimitive struct {
	scopes      []guestbridge.Context
	credentials guestbridge.Credentials
	additional  []guestbridge.Credentials
}

type mountedPrimitive struct{ file *os.File }

func (m mountedPrimitive) check() error { _, err := m.file.Stat(); return err }

func newSourceRigProtocol(t *testing.T, protocol guestbridge.Protocol) *sourceRig {
	t.Helper()
	return newSourceRigDomains(t, protocol, 1)
}

func newSourceRigDomains(t *testing.T, protocol guestbridge.Protocol, count int) *sourceRig {
	t.Helper()
	return newSourceRigDomainsWithIDMap(t, protocol, count, hashBytes([]byte("map")))
}

// newSourceRigDomainsWithIDMap preserves the real construction while allowing
// the C6 guest's actual initial-namespace map identity at the OS boundary.
func newSourceRigDomainsWithIDMap(t *testing.T, protocol guestbridge.Protocol, count int, idmap string) *sourceRig {
	t.Helper()
	return newSourceRigDomainsBootstrap(t, protocol, count, idmap, true)
}

// newSourceRigDomainsUnstarted lets the actual C consumer perform the first
// HELLO. A fixture HELLO would already own guest cell1 in the real mapped ledger.
// Existing constructors continue to perform their original bootstrap.
func newSourceRigDomainsUnstarted(t *testing.T, protocol guestbridge.Protocol, count int, idmap string) *sourceRig {
	t.Helper()
	return newSourceRigDomainsBootstrap(t, protocol, count, idmap, false)
}

func newSourceRigDomainsBootstrap(t *testing.T, protocol guestbridge.Protocol, count int, idmap string, hello bool, additional ...guestbridge.Credentials) *sourceRig {
	t.Helper()
	return newSourceRigDomainsBootstrapFailpoint(t, protocol, count, idmap, hello, nil, additional...)
}

func newSourceRigDomainsBootstrapFailpoint(t *testing.T, protocol guestbridge.Protocol, count int, idmap string, hello bool, failpoint func(int, string) error, additional ...guestbridge.Credentials) *sourceRig {
	return newSourceRigDomainsOwnedSupervisor(t, protocol, count, idmap, hello, failpoint, nil, additional...)
}

func newSourceRigDomainsOwnedSupervisor(t *testing.T, protocol guestbridge.Protocol, count int, idmap string, hello bool, failpoint func(int, string) error, supervisor *Supervisor, additional ...guestbridge.Credentials) *sourceRig {
	t.Helper()
	temporary := t.TempDir
	if !hello {
		// Register before every real owner so normal LIFO cleanup closes children,
		// workers and ledgers before deleting successful disposable state.
		root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "hfs-c6-")
		require(t, err)
		require(t, os.Chmod(root, 0700))
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("retained failed C6 fixture resources: %s", root)
				return
			}
			require(t, os.RemoveAll(root))
		})
		temporary = func() string { dir, err := os.MkdirTemp(root, "resource-"); require(t, err); return dir }
	}
	if supervisor == nil {
		supervisor = runningFixture(t)
	}
	r := &sourceRig{t: t, temporary: temporary, supervisor: supervisor}
	endpoint := guestbridge.Endpoint{FuseSession: testUUID(43).String(), CommandNode: 2, ResultNode: 3, RecallNode: 4}
	// Only actual mount/INIT/native headers and guest credentials are primitive
	// fixtures. All child process/groups, bridge, service, journals, claims,
	// source/controller ledgers, observer process and producer are real.
	f, err := os.CreateTemp(r.temporary(), "native-mount-primitive")
	require(t, err)
	t.Cleanup(func() { _ = f.Close() })
	r.supervisor.mount = &nativeMount{server: &fuse.Server{}, witness: mountedPrimitive{f}, endpointObservation: func() (guestbridge.Endpoint, error) { return endpoint, nil }}
	r.supervisor.record.Endpoint = endpoint
	r.supervisor.record.Stage = "mounted"
	filesystem := func() storage.Filesystem {
		dir := r.temporary()
		require(t, os.Chmod(dir, 0700))
		root, err := vfs.NewOS(t.Context(), dir)
		require(t, err)
		t.Cleanup(func() { _ = root.Close() })
		return root
	}
	profile := fsservice.ValidationProfile{Disposable: true, Name: "controlled-runtime-real-source"}
	permissions, err := fsservice.NewUnixPermissions(testUUID(20))
	require(t, err)
	object := fsapi.ObjectID{Home: testUUID(1), Allocation: testUUID(4)}
	c := fsservice.Config{Stream: segment.Stream{Volume: testUUID(1), Generation: testUUID(2), Configuration: testUUID(3), Epoch: 1, Kind: 1}, Storage: storage.Config{Volume: testUUID(1), Root: object, OwnerUID: uint32(os.Getuid())}, Validation: &profile, RequireControlledViews: true, Permissions: permissions, Source: filesystem(), Reservations: filesystem(), Workspace: fsservice.Workspace{ID: testUUID(30), Data: filesystem(), Journal: filesystem(), Progress: filesystem()}}
	r.source = c.Source
	c.Authority = record.Authority{Volume: testUUID(1), Generation: testUUID(2), Configuration: testUUID(3), Epoch: 1, Owner: testUUID(6), Partner: testUUID(8), Members: []fsapi.UUID{testUUID(6), testUUID(8), testUUID(9)}, CoordinatorTerm: 1, OwnerIncarnation: testUUID(7), PartnerIncarnation: testUUID(10)}
	dir := "."
	for _, part := range strings.Split(c.Stream.Directory(), "/") {
		next := path.Join(dir, part)
		require(t, c.Source.Mkdir(t.Context(), next, 0700))
		require(t, c.Source.SyncDir(t.Context(), dir))
		dir = next
	}
	header := record.Header{Volume: testUUID(1), Generation: testUUID(2), Configuration: testUUID(3), Epoch: 1, Operation: testUUID(90), Incarnation: testUUID(7)}
	writer, err := segment.NewWriter(t.Context(), c.Source, c.Stream, segment.Options{MaximumBytes: 16 << 20, Structural: func(uint64) record.Header { return header }, NewSegment: func() fsapi.UUID { return testUUID(80) }})
	require(t, err)
	init := record.VolumeInit{Root: object, Initial: record.Metadata{Revision: 1, Type: 2, UID: 100, GID: 200, Mode: 02775, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: object}, RootLedger: record.LedgerEdit{Object: object, ResultingRevision: 1}, Authority: c.Authority, Authorization: record.Artifact{Kind: 6, ID: testUUID(70), Length: 1}}
	receipt, err := writer.Append(t.Context(), record.Record{Header: header, Payload: init})
	require(t, err)
	require(t, receipt.Wait(t.Context()))
	require(t, writer.Close())
	c.Selection = apply.Selection{Through: receipt.Reference, Effects: []record.FrameRef{receipt.Reference}, Proof: []byte("real local controlled genesis")}
	r.service, err = fsservice.New(t.Context(), c)
	require(t, err)
	t.Cleanup(func() { _ = r.service.Close() })
	journal, err := r.service.ControlledJournalInfo(t.Context(), profile)
	require(t, err)
	r.scope = guestbridge.Context{AuthorityHash: hashBytes([]byte("authority")), Cluster: testUUID(40).String(), Configuration: testUUID(3).String(), Export: testUUID(41).String(), ExporterIncarnation: r.supervisor.record.ProcessID.String(), FuseSession: endpoint.FuseSession, Generation: testUUID(2).String(), GuestBoot: testUUID(44).String(), GuestMount: testUUID(45).String(), OriginIncarnation: testUUID(46).String(), OriginJournal: journal.Incarnation.String(), OriginNode: testUUID(48).String(), PolicyHash: hashBytes([]byte("policy")), RegistryHash: hashBytes([]byte("registry")), Session: testUUID(50).String(), VM: testUUID(52).String(), Volume: testUUID(1).String()}
	r.credentials = guestbridge.Credentials{FSUID: 1000, FSGID: 1001, Groups: []uint32{200}, Process: testUUID(63).String(), Revision: 1, IDMapHash: idmap, UserNamespace: testUUID(64).String()}
	if len(additional) > 1 {
		t.Fatal("only two predeclared credential captures supported")
	}
	r.credentialCaptures = append([]guestbridge.Credentials{r.credentials}, additional...)
	for i := range r.credentialCaptures {
		r.credentialCaptures[i].Groups = append([]uint32{}, r.credentialCaptures[i].Groups...)
	}

	identity := r.supervisor.child.identity
	r.native = guestbridge.Native{UID: identity.UID, GID: identity.GID, PID: identity.PID, Groups: append([]uint32(nil), identity.Groups...), Process: identity.PublicID.String(), Unique: 1, Endpoint: endpoint}
	r.installed = installedObservation{Version: "homelabfs-installed-observation/1", ManifestHash: hashBytes([]byte("manifest")), BootID: r.scope.GuestBoot, KernelBuildID: strings.Repeat("ab", 20), Modules: map[string]string{}, ABI: physicalContract, Admission: kernelAdmission{Version: "homelabfs-kernel-admission/1", BootID: r.scope.GuestBoot, ProviderNonce: testUUID(66).String(), PhysicalContract: physicalContract, Protocols: map[string]guestbridge.Protocol{r.scope.Session: protocol}, Domains: []guestbridge.Context{r.scope}, Observations: []kernelDomainObservation{{Scope: r.scope, Revision: 1, Fence: 1, Phase: "LIVE"}}}}
	r.peers = []*sourceRig{r}
	for i := 1; i < count; i++ {
		scope := r.scope
		scope.Session = testUUID(byte(150 + i)).String()
		scope.GuestMount = testUUID(byte(160 + i)).String()
		peer := &sourceRig{t: t, supervisor: r.supervisor, service: r.service, scope: scope, credentials: r.credentials, native: r.native}
		r.peers = append(r.peers, peer)
		r.installed.Admission.Domains = append(r.installed.Admission.Domains, scope)
		r.installed.Admission.Protocols[scope.Session] = protocol
		r.installed.Admission.Observations = append(r.installed.Admission.Observations, kernelDomainObservation{Scope: scope, Revision: 1, Fence: 1, Phase: "LIVE"})
	}
	var observer GuestObserver
	var observationFile string
	if hello {
		observer, observationFile = liveObserverFixture(t, r.installed)
	} else {
		observer, observationFile = liveObserverFixtureAt(t, r.installed, r.temporary())
	}
	r.observationFile = observationFile
	r.admission, err = ObserveAdmission(t.Context(), r.supervisor, observer, r.installed.Admission.Domains)
	require(t, err)
	key := func() ed25519.PrivateKey { _, k, err := ed25519.GenerateKey(rand.Reader); require(t, err); return k }
	operator, guest := key(), key()
	r.artifactOperator = append(ed25519.PrivateKey(nil), operator...)
	r.artifactGuest = append(ed25519.PrivateKey(nil), guest...)
	r.key = key()
	enrolled := controlledvalidation.Enrollment{Protocol: &protocol, Profile: profile, Scope: r.scope, Endpoint: endpoint, ServingGeneration: c.Workspace.ID, Ledger: testUUID(65), Journal: journal, HostKey: r.key.Public().(ed25519.PublicKey), GuestKey: guest.Public().(ed25519.PublicKey)}
	e := viewcontrol.Enrollment{Version: viewcontrol.EnrollmentVersion, ID: testUUID(99), Domains: []controlledvalidation.Enrollment{enrolled}, TicketFamilies: []viewcontrol.TicketFamilies{{Acquisition: viewcontrol.TicketFamily{Anchor: testUUID(110).String(), Nonce: testUUID(111).String()}, Control: viewcontrol.TicketFamily{Anchor: testUUID(110).String(), Nonce: testUUID(112).String()}}}, PageSize: 4096, Bounds: viewcontrol.Bounds{Tickets: 16, ControlTickets: 8, Reservations: 4, Identities: 64, Sources: 8, SourceBytes: 64 << 10, LedgerBytes: 12 << 20, WorkBytes: 192 << 20}}
	for i, peer := range r.peers[1:] {
		peer.key = key()
		domain := enrolled
		domain.Scope = peer.scope
		domain.Ledger = testUUID(byte(170 + i))
		domain.HostKey = peer.key.Public().(ed25519.PublicKey)
		e.Domains = append(e.Domains, domain)
		e.TicketFamilies = append(e.TicketFamilies, viewcontrol.TicketFamilies{Acquisition: viewcontrol.TicketFamily{Anchor: testUUID(byte(180 + i)).String(), Nonce: testUUID(byte(190 + i)).String()}, Control: viewcontrol.TicketFamily{Anchor: testUUID(byte(180 + i)).String(), Nonce: testUUID(byte(200 + i)).String()}})
	}
	require(t, r.service.Controlled(t.Context(), profile, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		source, err := cut.ViewSource(ctx, e.Bounds.SourceBytes)
		if err == nil {
			e.Baseline = source.Through
			e.BaselineStateHash = hashBytes(source.State)
		}
		return err
	}))
	r.facts, err = NewFacts(t.Context(), r.service, e, []*Admission{r.admission})
	require(t, err)
	private := func() string { dir := r.temporary(); require(t, os.Chmod(dir, 0700)); return dir }
	providers := map[string]*controlledvalidation.Provider{}
	keys := map[string]ed25519.PrivateKey{}
	routes := map[guestbridge.Context]*controlledvalidation.Provider{}
	r.providerFactory = func(i int) *controlledvalidation.Provider {
		peer := r.peers[i]
		domain := e.Domains[i]
		raw, err := json.Marshal(domain)
		require(t, err)
		var hook func(string) error
		if failpoint != nil {
			hook = func(stage string) error { return failpoint(i, stage) }
		}
		provider, err := controlledvalidation.New(t.Context(), controlledvalidation.Config{Directory: private(), Initialize: true, Service: r.service, Operator: operator.Public().(ed25519.PublicKey), Authorization: controlledvalidation.Authorization{Enrollment: domain, OperatorSignature: ed25519.Sign(operator, raw), HostSignature: ed25519.Sign(peer.key, raw), GuestSignature: ed25519.Sign(guest, raw)}, Facts: r.facts, Failpoint: hook})
		require(t, err)
		t.Cleanup(func() { _ = provider.Close() })
		return provider
	}
	for i, peer := range r.peers {
		peer.provider = r.providerFactory(i)
		providers[peer.scope.Session] = peer.provider
		keys[peer.scope.Session] = peer.key
		routes[peer.scope] = peer.provider
	}
	var raw []byte
	raw, err = json.Marshal(e)
	require(t, err)
	signatures := []viewcontrol.Signatures{}
	for _, peer := range r.peers {
		signatures = append(signatures, viewcontrol.Signatures{Host: ed25519.Sign(peer.key, raw), Guest: ed25519.Sign(guest, raw)})
	}
	authorization := viewcontrol.Authorization{Enrollment: e, Operator: ed25519.Sign(operator, raw), Domains: signatures}
	authorizationBytes, err := json.Marshal(authorization)
	require(t, err)
	require(t, json.Unmarshal(authorizationBytes, &r.artifactAuthorization))
	var controllerFailpoint func(string) error
	if failpoint != nil {
		controllerFailpoint = func(stage string) error { return failpoint(-1, stage) }
	}
	r.controller, err = viewcontrol.New(t.Context(), viewcontrol.Config{Failpoint: controllerFailpoint, Directory: private(), Initialize: true, Service: r.service, Providers: providers, Operator: operator.Public().(ed25519.PublicKey), Authorization: authorization, Facts: r.facts})
	require(t, err)
	t.Cleanup(func() { _ = r.controller.Close() })
	producer, err := NewProducer(t.Context(), r.facts, r.controller, providers, keys)
	require(t, err)
	r.producer = producer
	var claims guestbridge.Claims = r.provider
	var execution guestbridge.Execution = r.provider
	if count > 1 {
		router, err := NewProviderRouter(t.Context(), r.service, routes)
		require(t, err)
		claims, execution = router, router
	}
	r.bridge, err = guestbridge.New(t.Context(), guestbridge.Config{Validation: &profile, Protocol: &protocol, Verification: producer, ViewProtocol: r.controller, Endpoint: endpoint, Native: r.supervisor, Attestor: newGuestCredentialsSetPrimitive(r.installed.Admission.Domains, r.credentialCaptures), Claims: claims, Execution: execution, Views: r.controller, Scopes: r.installed.Admission.Domains})
	require(t, err)
	r.supervisor.record.Stage = "claimed"
	if guestbridge.IsMappedProtocol(protocol) {
		var config admission.Config
		for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: guestbridge.TransportLedgerMemoryBytes + (12 << 20)} {
			resources := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
			config.Pools[class] = admission.Limits{Global: resources, PerPeer: resources, PerVolume: resources}
		}
		budget, err := admission.New(config)
		require(t, err)
		directory := filepath.Join(r.temporary(), "mapped-transport")
		var transport *guestbridge.Transport
		if failpoint == nil {
			transport, err = guestbridge.NewDurableTransport(r.bridge, budget, directory)
		} else {
			transport, err = guestbridge.NewDurableTransportWithFailureHook(r.bridge, budget, directory,
				func(stage string) error { return failpoint(-2, stage) })
		}
		require(t, err)
		t.Cleanup(transport.Close)
		counter := uint64(0)
		for _, peer := range r.peers {
			peer.mappingTransport = transport
			peer.mappingDirectory = directory
			peer.mappingNativeCell = &counter
		}
	}
	for _, peer := range r.peers {
		peer.bridge, peer.controller, peer.facts, peer.admission, peer.installed = r.bridge, r.controller, r.facts, r.admission, r.installed
		peer.observationFile = r.observationFile
		if hello {
			_, _, err = peer.execute("HELLO", protocol)
			require(t, err)
		}
	}
	return r
}

func liveObserverFixture(t *testing.T, o installedObservation) (GuestObserver, string) {
	t.Helper()
	return liveObserverFixtureAt(t, o, t.TempDir())
}
func liveObserverFixtureAt(t *testing.T, o installedObservation, dir string) (GuestObserver, string) {
	t.Helper()
	file := filepath.Join(dir, "kernel-observation.json")
	raw, err := canonicalJSON(o)
	require(t, err)
	require(t, os.WriteFile(file, raw, 0600))
	quoted, _ := json.Marshal(file)
	// The fake SSH primitive needs a host-resolved absolute interpreter (see
	// fixturePython); SSHHash below pins these exact script bytes.
	script := "#!" + fixturePython(t) + "\nimport sys,pathlib\nsys.stdout.write(pathlib.Path(" + string(quoted) + ").read_text())\n"
	ssh := filepath.Join(dir, "pinned-ssh-primitive")
	require(t, os.WriteFile(ssh, []byte(script), 0500))
	hosts := filepath.Join(dir, "known_hosts")
	key := filepath.Join(dir, "identity")
	require(t, os.WriteFile(hosts, []byte("fixture host"), 0600))
	require(t, os.WriteFile(key, []byte("fixture key"), 0600))
	return GuestObserver{SSH: ssh, SSHHash: sha256.Sum256([]byte(script)), KnownHosts: hosts, KnownHostsHash: sha256.Sum256([]byte("fixture host")), Identity: key, Host: "guest.test", ManifestHash: sha256.Sum256([]byte("manifest"))}, file
}
func require(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (r *sourceRig) execute(op string, value any) (json.RawMessage, *guestbridge.VerificationReceipt, error) {
	if r.mappingTransport != nil {
		payload, receipt, _, _, err := r.mappedExecute(op, value)
		if receipt == nil {
			return payload, nil, err
		}
		return payload, &receipt.VerificationReceipt, err
	}
	r.cells++
	raw, err := canonicalJSON(value)
	if err != nil {
		return nil, nil, err
	}
	protocol := r.installed.Admission.Protocols[r.scope.Session]
	command := guestbridge.Command{Cell: r.cells, Context: r.scope, Protocol: &protocol, Op: op, Payload: raw}
	response, err := r.bridge.Execute(r.t.Context(), r.native, command)
	if err != nil {
		return nil, nil, err
	}
	var envelope struct {
		Payload      json.RawMessage
		Verification *guestbridge.VerificationReceipt
	}
	err = json.Unmarshal(response, &envelope)
	if envelope.Verification != nil {
		unsigned, e := unsignedReceipt(*envelope.Verification)
		require(r.t, e)
		signature, e := hex.DecodeString(envelope.Verification.Signature)
		require(r.t, e)
		if !ed25519.Verify(r.key.Public().(ed25519.PublicKey), unsigned, signature) {
			r.t.Fatal("producer did not sign canonical receipt")
		}
		var fields map[string]json.RawMessage
		require(r.t, json.Unmarshal(response, &fields))
		delete(fields, "verification")
		base, e := canonicalJSON(fields)
		require(r.t, e)
		original, e := canonicalJSON(command)
		require(r.t, e)
		if envelope.Verification.ResponseHash != hashBytes(base) || envelope.Verification.CommandHash != hashBytes(original) {
			r.t.Fatal("receipt lost original response/request binding")
		}
	}
	return envelope.Payload, envelope.Verification, err
}
func (r *sourceRig) call(op string, args any) json.RawMessage {
	r.claims++
	raw, err := canonicalJSON(args)
	require(r.t, err)
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	original, err := canonicalJSON(intent)
	require(r.t, err)
	call := guestbridge.Call{Claim: guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: r.claims, OriginalHash: hashBytes(original)}, Credentials: r.credentials, Intent: intent}
	_, _, err = r.execute("ALLOCATE", call)
	require(r.t, err)
	_, _, err = r.execute("CALL", call)
	require(r.t, err)
	payload, _, err := r.execute("QUERY", map[string]any{"claim": call.Claim})
	require(r.t, err)
	var reply struct{ Result json.RawMessage }
	require(r.t, json.Unmarshal(payload, &reply))
	return reply.Result
}

func TestRealControlledProducerSignsSourceEventAndRefusesForgedACK(t *testing.T) {
	for _, selected := range []struct {
		name     string
		protocol guestbridge.Protocol
	}{{"controlled1.2", guestbridge.ControlledProtocol()}, {"cleanup1.3", guestbridge.CleanupProtocol()}} {
		t.Run(selected.name, func(t *testing.T) {
			testRealControlledProducerSignsSourceEventAndRefusesForgedACK(t, selected.protocol)
		})
	}
}

func testRealControlledProducerSignsSourceEventAndRefusesForgedACK(t *testing.T, protocol guestbridge.Protocol) {
	r := newSourceRigProtocol(t, protocol)
	root := r.call("ROOT", map[string]any{})
	var result struct {
		Code         string
		Capabilities []guestbridge.Capability
		Publication  json.RawMessage
	}
	require(t, json.Unmarshal(root, &result))
	if result.Code != "OK" || len(result.Capabilities) != 2 || !bytes.Equal(result.Publication, []byte("null")) {
		t.Fatalf("ROOT result %s", root)
	}
	domain := result.Capabilities[1]
	ticket := guestbridge.Ticket{Anchor: testUUID(110).String(), Nonce: testUUID(111).String(), Number: 1, Credentials: r.credentials}
	payload, receipt, err := r.execute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": domain, "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
	require(t, err)
	if receipt == nil || receipt.Kind != "SOURCE" {
		t.Fatalf("actual source not verified: %s", payload)
	}
	var acquisition struct{ Lease guestbridge.ViewLease }
	require(t, json.Unmarshal(payload, &acquisition))
	control := guestbridge.Ticket{Anchor: ticket.Anchor, Nonce: testUUID(112).String(), Number: 1, Credentials: r.credentials}
	_, _, err = r.execute("VIEW_RELEASE", map[string]any{"request": control, "lease": acquisition.Lease})
	require(t, err)
	payload, receipt, err = r.execute("VIEW_POLL", map[string]any{"domain": domain, "after_sequence": 0})
	require(t, err)
	if receipt == nil || receipt.Kind != "EVENT" {
		t.Fatalf("actual event not verified: %s", payload)
	}
	var poll struct{ Event map[string]json.RawMessage }
	require(t, json.Unmarshal(payload, &poll))
	entries, err := canonicalJSON([]any{})
	require(t, err)
	ack := map[string]any{"domain": domain, "sequence": poll.Event["sequence"], "recall": poll.Event["recall"], "phase": "STOPPED", "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": r.scope.Session, "manifest": map[string]any{"entries": []any{}, "hash": hashBytes(entries)}, "target": nil}
	if _, _, err = r.execute("VIEW_ACK", ack); err == nil {
		t.Fatal("wire ACK without actual observed kernel completion accepted")
	}
	ackRaw, err := canonicalJSON(ack)
	require(t, err)
	eventRaw, err := canonicalJSON(poll.Event)
	require(t, err)
	r.installed.Admission.Observations[0] = kernelDomainObservation{Scope: r.scope, Revision: 1, Fence: 1, Phase: "STOPPED", EventHash: hashBytes(eventRaw), AcknowledgementHash: hashBytes(ackRaw)}
	updated, err := canonicalJSON(r.installed)
	require(t, err)
	require(t, os.WriteFile(r.observationFile, updated, 0600))
	_, _, err = r.execute("VIEW_ACK", ack)
	require(t, err)
	// Protected exact old ACK retries remain valid after the host advances phase.
	_, _, err = r.execute("VIEW_ACK", ack)
	require(t, err)

	for _, phase := range []string{"INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		current, verification, e := r.execute("VIEW_POLL", map[string]any{"domain": domain, "after_sequence": 0})
		require(t, e)
		if verification == nil || verification.Kind != "EVENT" {
			t.Fatalf("phase event missing: %s", current)
		}
		require(t, json.Unmarshal(current, &poll))
		next := map[string]any{"domain": domain, "sequence": poll.Event["sequence"], "recall": poll.Event["recall"], "phase": phase, "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": r.scope.Session, "manifest": nil, "target": poll.Event["target"]}
		if phase == "DRAINED" {
			next["manifest"] = map[string]any{"entries": []any{}, "hash": hashBytes(entries)}
		}
		revision := uint64(1)
		if phase == "REOPENED" {
			revision = 2
		}
		r.observeACK(poll.Event, next, revision)
		_, _, e = r.execute("VIEW_ACK", next)
		require(t, e)
	}
	// A protected old STOPPED retry remains a replay after all physical phases.
	_, _, err = r.execute("VIEW_ACK", ack)
	require(t, err)
	missing := r.installed
	missing.Admission.Domains = nil
	updated, err = canonicalJSON(missing)
	require(t, err)
	require(t, os.WriteFile(r.observationFile, updated, 0600))
	if _, _, err = r.execute("VIEW_ACK", ack); err == nil {
		t.Fatal("missing retained old domain admitted")
	}
}

func TestRealControlledProducerFloorUsesOriginalProtectedCoverage(t *testing.T) {
	for _, selected := range []struct {
		name     string
		protocol guestbridge.Protocol
	}{{"controlled1.2", guestbridge.ControlledProtocol()}, {"cleanup1.3", guestbridge.CleanupProtocol()}} {
		t.Run(selected.name, func(t *testing.T) { testRealControlledProducerFloorUsesOriginalProtectedCoverage(t, selected.protocol) })
	}
}

func testRealControlledProducerFloorUsesOriginalProtectedCoverage(t *testing.T, protocol guestbridge.Protocol) {
	r := newSourceRigProtocol(t, protocol)
	root := r.call("ROOT", map[string]any{})
	var result struct{ ID string }
	require(t, json.Unmarshal(root, &result))
	request := map[string]any{"realm": testUUID(100).String(), "subject": "616263", "anchor": testUUID(101).String(), "nonce": testUUID(103).String(), "through": 1, "result_hashes": []string{result.ID}, "credentials": r.credentials}
	payload, receipt, err := r.execute("RETIRE", request)
	require(t, err)
	if receipt == nil || receipt.Kind != "FLOOR" {
		t.Fatalf("protected complete floor not signed: %s", payload)
	}
	var evidence struct {
		Credentials guestbridge.Credentials
		Through     uint64
	}
	require(t, json.Unmarshal(receipt.Evidence, &evidence))
	if evidence.Through != 1 || !sameJSON(evidence.Credentials, r.credentials) {
		t.Fatal("floor lost original credentials")
	}
	_, retry, err := r.execute("RETIRE", request)
	require(t, err)
	if retry == nil || retry.Kind != "FLOOR" {
		t.Fatal("lost floor receipt could not be reverified")
	}
	changed := r.credentials
	changed.Groups = []uint32{201}
	request["credentials"] = changed
	if _, _, err = r.execute("RETIRE", request); err == nil {
		t.Fatal("changed original owner minted a floor")
	}
}

func (r *sourceRig) observeACK(event map[string]json.RawMessage, ack map[string]any, revision uint64) {
	r.t.Helper()
	raw, err := canonicalJSON(ack)
	require(r.t, err)
	issued, err := canonicalJSON(event)
	require(r.t, err)
	r.installed.Admission.Observations[0] = kernelDomainObservation{Scope: r.scope, Revision: revision, Fence: 1, Phase: ack["phase"].(string), EventHash: hashBytes(issued), AcknowledgementHash: hashBytes(raw)}
	updated, err := canonicalJSON(r.installed)
	require(r.t, err)
	require(r.t, os.WriteFile(r.observationFile, updated, 0600))
}

func TestRealControlledProducerRejectsFalseFrozenOmission(t *testing.T) {
	for _, selected := range []struct {
		name     string
		protocol guestbridge.Protocol
	}{{"controlled1.2", guestbridge.ControlledProtocol()}, {"cleanup1.3", guestbridge.CleanupProtocol()}} {
		t.Run(selected.name, func(t *testing.T) { testRealControlledProducerRejectsFalseFrozenOmission(t, selected.protocol) })
	}
}

func testRealControlledProducerRejectsFalseFrozenOmission(t *testing.T, protocol guestbridge.Protocol) {
	r := newSourceRigProtocol(t, protocol)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(r.call("ROOT", map[string]any{}), &root))
	domain := root.Capabilities[1]
	created := r.call("CREATE", map[string]any{"parent": root.Capabilities[0], "name": "66696c65", "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	var file struct {
		Code         string
		Capabilities []guestbridge.Capability
	}
	require(t, json.Unmarshal(created, &file))
	if file.Code != "OK" {
		t.Fatalf("create %s", created)
	}
	description := file.Capabilities[1]
	offset, length := uint64(0), uint64(4096)
	ticket := guestbridge.Ticket{Anchor: testUUID(110).String(), Nonce: testUUID(111).String(), Number: 1, Credentials: r.credentials}
	payload, _, err := r.execute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": domain, "access": guestbridge.ViewAccess{Kind: "PAGE", Description: &description, Offset: &offset, Length: &length}, "mode": "EXCLUSIVE_DIRTY", "target": nil})
	require(t, err)
	var acquired struct {
		Status string
		Lease  guestbridge.ViewLease
	}
	require(t, json.Unmarshal(payload, &acquired))
	if acquired.Status != "GRANTED" {
		t.Fatalf("acquire %s", payload)
	}
	number := guestbridge.ClaimNumber{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: r.claims + 1}
	ticket.Number = 2
	payload, _, err = r.execute("DIRTY_RESERVE", map[string]any{"request": ticket, "lease": acquired.Lease, "description": description, "offset": 0, "length": 4096, "claim_number": number})
	require(t, err)
	var reserved struct {
		Status      string
		Reservation guestbridge.DirtyReservation
	}
	require(t, json.Unmarshal(payload, &reserved))
	if reserved.Status != "GRANTED" {
		t.Fatalf("reserve %s", payload)
	}
	control := ticket
	control.Nonce = testUUID(112).String()
	control.Number = 1
	_, _, err = r.execute("VIEW_RELEASE", map[string]any{"request": control, "lease": acquired.Lease})
	require(t, err)
	payload, _, err = r.execute("VIEW_POLL", map[string]any{"domain": domain, "after_sequence": 0})
	require(t, err)
	var poll struct{ Event map[string]json.RawMessage }
	require(t, json.Unmarshal(payload, &poll))
	var recall string
	require(t, json.Unmarshal(poll.Event["recall"], &recall))
	entry := map[string]any{"description": description, "offset": 0, "length": 3, "payload_hash": hashBytes([]byte("XYZ")), "reservation": reserved.Reservation.ID}
	entryRaw, err := canonicalJSON(entry)
	require(t, err)
	entry["id"] = hashBytes(entryRaw)
	args, err := canonicalJSON(map[string]any{"description": description, "offset": 0, "data": "58595a", "recall": recall, "dirty_entry": entry["id"], "reservation": reserved.Reservation.ID})
	require(t, err)
	intent := guestbridge.Intent{Opcode: "DRAIN_WRITE", Args: args}
	intentRaw, err := canonicalJSON(intent)
	require(t, err)
	claim := guestbridge.Claim{Realm: number.Realm, Subject: number.Subject, Anchor: number.Anchor, Nonce: number.Nonce, Number: number.Number, OriginalHash: hashBytes(intentRaw)}
	control.Number = 2
	payload, _, err = r.execute("DIRTY_FREEZE", map[string]any{"request": control, "reservation": reserved.Reservation, "recall": recall, "data": "58595a", "claim": claim})
	require(t, err)
	var frozen struct {
		Status string
		Entry  json.RawMessage
	}
	require(t, json.Unmarshal(payload, &frozen))
	if frozen.Status != "FROZEN" {
		t.Fatalf("freeze %s", payload)
	}
	empty, err := canonicalJSON([]any{})
	require(t, err)
	ack := map[string]any{"domain": domain, "sequence": poll.Event["sequence"], "recall": recall, "phase": "STOPPED", "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": r.scope.Session, "manifest": map[string]any{"entries": []any{}, "hash": hashBytes(empty)}, "target": nil}
	r.observeACK(poll.Event, ack, 1)
	if _, _, err = r.execute("VIEW_ACK", ack); err == nil {
		t.Fatal("authenticated kernel primitive falsely omitted an actual frozen bound reservation")
	}
	entries := []json.RawMessage{frozen.Entry}
	entriesRaw, err := canonicalJSON(entries)
	require(t, err)
	ack["manifest"] = map[string]any{"entries": entries, "hash": hashBytes(entriesRaw)}
	r.observeACK(poll.Event, ack, 1)
	_, _, err = r.execute("VIEW_ACK", ack)
	require(t, err)
}

func newGuestCredentialsSetPrimitive(scopes []guestbridge.Context, captures []guestbridge.Credentials) guestCredentialsSetPrimitive {
	copyCredentials := func(c guestbridge.Credentials) guestbridge.Credentials {
		c.Groups = append([]uint32{}, c.Groups...)
		return c
	}
	g := guestCredentialsSetPrimitive{scopes: append([]guestbridge.Context(nil), scopes...), credentials: copyCredentials(captures[0])}
	for _, c := range captures[1:] {
		g.additional = append(g.additional, copyCredentials(c))
	}
	return g
}

func (g guestCredentialsSetPrimitive) Verify(_ context.Context, scope guestbridge.Context, c guestbridge.Credentials) error {
	for _, enrolled := range g.scopes {
		if scope == enrolled {
			if sameJSON(c, g.credentials) {
				return nil
			}
			for _, captured := range g.additional {
				if sameJSON(c, captured) {
					return nil
				}
			}
		}
	}
	return fsapi.Permission
}
