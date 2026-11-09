//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

func assemblyFixture(t *testing.T) (*sourceRig, AssemblyConfig) {
	t.Helper()
	r := newSourceRigDomains(t, guestbridge.TerminalProtocol(), 2)
	// Reuse only the real backing service, observed guest and supervised process.
	// The original fixture's native INIT/enrollment is the declared OS primitive;
	// discard its semantic owners before composing the runtime's actual owners.
	r.mappingTransport.Close()
	require(t, r.controller.Close())
	for _, peer := range r.peers {
		require(t, peer.provider.Close())
	}
	r.supervisor.record.Controller = fsapi.UUID{}
	r.supervisor.record.Stage = "mounted"
	var pools admission.Config
	for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: guestbridge.TransportLedgerMemoryBytes + (12 << 20)} {
		resources := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
		pools.Pools[class] = admission.Limits{Global: resources, PerPeer: resources, PerVolume: resources}
	}
	budget, err := admission.New(pools)
	require(t, err)
	c := AssemblyConfig{Directory: t.TempDir(), Service: r.service, Supervisor: r.supervisor, Admissions: []*Admission{r.admission}, Authorization: r.artifactAuthorization, Operator: r.artifactOperator.Public().(ed25519.PublicKey), HostKeys: map[string]ed25519.PrivateKey{}, Attestor: newGuestCredentialsSetPrimitive(r.installed.Admission.Domains, r.credentialCaptures), ProviderLimits: ProviderLimits{Claims: 64, RetainedBytes: 16 << 20, SourceBytes: 8 << 20, ClaimCreditBytes: 1 << 20, SourceCreditBytes: 1 << 20}, Capabilities: 64, CapabilityIdentities: 128, CredentialRevisions: 32, Budget: budget}
	for i, d := range c.Authorization.Enrollment.Domains {
		raw, err := json.Marshal(d)
		require(t, err)
		key := r.peers[i].key
		c.HostKeys[d.Scope.Session] = key
		c.Providers = append(c.Providers, controlledvalidation.Authorization{Enrollment: d, OperatorSignature: ed25519.Sign(r.artifactOperator, raw), HostSignature: ed25519.Sign(key, raw), GuestSignature: ed25519.Sign(r.artifactGuest, raw)})
	}
	budgetPolicy := GuestArtifactBudget{OrdinaryBytes: 64 << 20, ControlBytes: 32 << 20, Reservations: 32, Cookies: 32, Received: 32}
	group := GuestProvisionGroup{Authorization: c.Authorization}
	for i, peer := range r.peers {
		group.Signers = append(group.Signers, GuestArtifactSigners{Host: peer.key, Guest: r.artifactGuest})
		group.LocalMounts = append(group.LocalMounts, GuestArtifactMount{DomainIndex: i, Session: peer.scope.Session, DeviceTag: "hfs" + string(rune('0'+i)), DeviceName: "virtio" + string(rune('0'+i)), UserNamespace: peer.credentials.UserNamespace, IDMapHash: peer.credentials.IDMapHash, Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), ViewControlNonce: c.Authorization.Enrollment.TicketFamilies[i].Control.Nonce})
	}
	provision, err := SignGuestProvision(r.scope.GuestBoot, r.installed.Admission.ProviderNonce, []GuestProvisionGroup{group}, budgetPolicy, r.artifactOperator)
	require(t, err)
	c.Attestor, err = newKernelAttestor(r.admission, c.Authorization, provision, c.Operator, budgetPolicy)
	require(t, err)
	return r, c
}

func TestRuntimeProposalStagedExternalConsent(t *testing.T) {
	r, c := assemblyFixture(t)
	config := RuntimeConfig{Scopes: []guestbridge.Context{r.scope, r.peers[1].scope}, Views: c.Authorization.Enrollment.Bounds, HostKeys: c.HostKeys, GuestKeys: map[string]ed25519.PublicKey{}}
	for _, d := range c.Authorization.Enrollment.Domains {
		config.GuestKeys[d.Scope.Session] = d.GuestKey
	}
	config.Scopes[0].GuestBoot = ""
	config.Scopes[1].GuestBoot = ""
	proposal, err := runtimeProposal(t.Context(), config, r.service, r.supervisor, r.native.Endpoint)
	require(t, err)
	if proposal.Domains[0].Scope.ExporterIncarnation != r.supervisor.record.ProcessID.String() || proposal.Domains[0].ServingGeneration != r.service.FrontendInfo().Generation || proposal.BaselineStateHash == "" {
		t.Fatal("proposal omitted actual retained service/native binding")
	}
	require(t, protectProposal(r.supervisor, proposal))
	var original viewcontrol.Enrollment
	require(t, readRuntimeFile(filepath.Join(r.supervisor.root, "proposal.json"), &original))
	if !sameJSON(original, proposal) {
		t.Fatal("inert exact proposal changed on disk")
	}
	signed := proposal
	signed.Domains = append([]controlledvalidation.Enrollment(nil), proposal.Domains...)
	for i := range signed.Domains {
		signed.Domains[i].Scope.GuestBoot = r.scope.GuestBoot
	}
	bound, err := bindRuntimeProposal(proposal, signed)
	require(t, err)
	group := GuestProvisionGroup{Authorization: viewcontrol.Authorization{Enrollment: bound}}
	for _, peer := range r.peers {
		group.Signers = append(group.Signers, GuestArtifactSigners{peer.key, r.artifactGuest})
	}
	resignArtifact(t, &group, r.artifactOperator) // explicit external operator boundary
	if _, err := guestAuthorization(group.Authorization, c.Operator); err != nil {
		t.Fatal(err)
	}
	changed := signed
	changed.Domains = append([]controlledvalidation.Enrollment(nil), signed.Domains...)
	changed.Domains[1].Scope.PolicyHash = hashBytes([]byte("different-policy"))
	if _, err = bindRuntimeProposal(proposal, changed); err == nil {
		t.Fatal("external consent changed proposed policy")
	}
	changed = signed
	changed.Domains = append([]controlledvalidation.Enrollment(nil), signed.Domains...)
	changed.Domains[1].Scope.GuestBoot = testUUID(221).String()
	if _, err = bindRuntimeProposal(proposal, changed); err == nil {
		t.Fatal("two different guest boots admitted")
	}
	// Fully configured production Linux startup must refuse before creating any
	// owner, workspace, child or mount. The separate assembly test owns OS seams.
	config.Version = "homelabfs-controlled-runtime/1"
	config.VolumeConfig = filepath.Join(t.TempDir(), "volume-config")
	// The owner root also holds the private lifecycle socket, so it must fit
	// sockaddr_un; t.TempDir names embed the long test name.
	short, err := os.MkdirTemp(os.Getenv("TMPDIR"), "rt-")
	require(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	config.OwnerDirectory = filepath.Join(short, "must-stay-absent")
	config.EnrollmentFile = filepath.Join(t.TempDir(), "handoff")
	config.Operator = c.Operator
	config.Providers = c.ProviderLimits
	config.Capabilities = 64
	config.CapabilityIdentities = 128
	config.CredentialRevisions = 32
	config.BootstrapSeconds = 1
	config.Service = fsservice.Limits{Queue: 8, IOBytes: 65536, Handles: 64, Lookups: 128, Reservations: 128, Locks: 64, LogicalBytes: 4 << 20}
	config.Budget = RuntimeBudget{HostBytes: 4 << 30, ServiceBytes: 64 << 20, ExporterBytes: 274 << 20, GuestCopyBytes: 16 << 20, GuestBytes: 2 << 30, GuestBaseBytes: 256 << 20, Guest: GuestArtifactBudget{OrdinaryBytes: 64 << 20, ControlBytes: 32 << 20, Reservations: 32, Cookies: 32, Received: 32}}
	require(t, validateRuntimeConfig(t.Context(), config))
	long := config
	long.OwnerDirectory = filepath.Join(short, strings.Repeat("o", 104))
	if validateRuntimeConfig(t.Context(), long) == nil {
		t.Fatal("owner directory beyond sockaddr_un admitted")
	}
	alias := filepath.Join(t.TempDir(), "runtime-parent-alias")
	require(t, os.Symlink(filepath.Dir(config.OwnerDirectory), alias))
	for _, field := range []string{"owner", "handoff"} {
		changed := config
		if field == "owner" {
			changed.OwnerDirectory = filepath.Join(alias, "runtime")
		} else {
			changed.EnrollmentFile = filepath.Join(alias, "consent")
		}
		if err := validateRuntimeConfig(t.Context(), changed); err == nil {
			t.Fatalf("config admitted %s parent alias", field)
		}
	}
	file := filepath.Join(t.TempDir(), "runtime.json")
	raw, err := json.Marshal(config)
	require(t, err)
	require(t, os.WriteFile(file, raw, 0600))
	if runtime.GOOS == "linux" {
		var out, logs bytes.Buffer
		if err = Run(t.Context(), []string{"--config", file, "--disposable-validation"}, &out, &logs); !errors.Is(err, fsapi.Unsupported) {
			t.Fatalf("native adapter refusal: %v", err)
		}
		if out.Len() != 0 {
			t.Fatal("native refusal emitted readiness")
		}
		if _, err = os.Stat(config.OwnerDirectory); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("native refusal touched owner")
		}
	}
}

func TestRuntimeAssemblyTwoDomainsRealFraming(t *testing.T) {
	r, c := assemblyFixture(t)
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	scopes := []guestbridge.Context{r.scope, r.peers[1].scope}
	controller := r.supervisor.record.Controller
	check := func() error {
		return a.Transport.CheckControlledSessions(t.Context(), controller, scopes, guestbridge.TerminalProtocol())
	}
	if !errors.Is(check(), fsapi.WouldBlock) {
		t.Fatal("provisioned but unbound sessions became ready")
	}
	for index, peer := range r.peers {
		attachAssembly(peer, a, &counter)
		payload, _, err := peer.execute("HELLO", guestbridge.TerminalProtocol())
		require(t, err)
		if !bytes.Contains(payload, []byte(`"status":"ACCEPTED"`)) {
			t.Fatalf("controlled publication: %s", payload)
		}
		if index == 0 && !errors.Is(check(), fsapi.WouldBlock) {
			t.Fatal("one missing HELLO became ready")
		}
		result := peer.call("ROOT", map[string]any{})
		if !bytes.Contains(result, []byte(`"publication":null`)) {
			t.Fatalf("real allocated ROOT publication: %s", result)
		}
	}
	require(t, check())
	_, _, err = r.execute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	require(t, check()) // exact replay/new HELLO does not replace monotone witness
	if a.Transport.CheckControlledSessions(t.Context(), testUUID(202), scopes, guestbridge.TerminalProtocol()) == nil {
		t.Fatal("changed controller became ready")
	}
	if a.Transport.CheckControlledSessions(t.Context(), controller, scopes, guestbridge.MappedProtocol()) == nil {
		t.Fatal("wrong tuple became ready")
	}
	wrongScopes := append([]guestbridge.Context(nil), scopes...)
	wrongScopes[1].GuestMount = scopes[0].GuestMount
	if a.Transport.CheckControlledSessions(t.Context(), controller, wrongScopes, guestbridge.TerminalProtocol()) == nil {
		t.Fatal("partial/wrong scope became ready")
	}
	a.Transport.Close()
	state, err := guestbridge.InspectEndpoint(filepath.Join(c.Directory, "transport"), nil, nil)
	require(t, err)
	if state.High == 0 || !state.BridgeSessions {
		t.Fatal("actual durable endpoint rows absent")
	}
	if len(state.RuntimeHellos) != 2 {
		t.Fatal("completed HELLO witnesses not protected in actual C8 index")
	}
	// A complete original scope is mandatory even when the session UUID matches.
	wrong := r.peers[1].scope
	wrong.GuestMount = r.scope.GuestMount
	router, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{r.scope: a.Providers[r.scope.Session], r.peers[1].scope: a.Providers[r.peers[1].scope.Session]})
	require(t, err)
	if _, err = router.Resolve(t.Context(), wrong, guestbridge.Claim{}); err == nil {
		t.Fatal("partial-scope fallback")
	}
}

// attachAssembly points a fixture peer at the assembled runtime's actual
// owners; all peers share the endpoint's one native cell allocator.
func attachAssembly(peer *sourceRig, a *Assembly, counter *uint64) {
	peer.bridge = a.Bridge
	peer.producer = a.Producer
	peer.controller = a.Controller
	peer.facts = a.Facts
	peer.provider = a.Providers[peer.scope.Session]
	peer.mappingTransport = a.Transport
	peer.mappingNativeCell = counter
	peer.cells = 0
}

// The zq3-r4 guest mount sequence against the assembled runtime while the
// second peer has not sent HELLO yet, so the runtime is not ready. The guest
// kernel posts VIEW_WAIT (native VIEW_POLL) before its first VIEW_ACQUIRE;
// that poll must be answered EMPTY, not left as a silent blocked cell. The
// same logical wait then continues and delivers the RECALL started by the
// guest's own VIEW_RELEASE. The acknowledgements then follow the zq3-r5 order
// (task 964), including the identical INSTALLED_DRAIN resend, until the
// release is RELEASED; the runtime stays not ready throughout.
func TestRuntimeAssemblyMountSequenceBeforeReady(t *testing.T) {
	r, c := assemblyFixture(t)
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	controller := r.supervisor.record.Controller
	notReady := func(step string) {
		t.Helper()
		err := a.Transport.CheckControlledSessions(t.Context(), controller, []guestbridge.Context{r.scope, r.peers[1].scope}, guestbridge.TerminalProtocol())
		if !errors.Is(err, fsapi.WouldBlock) {
			t.Fatalf("%s: runtime readiness %v with peer 1 missing HELLO", step, err)
		}
	}
	for _, peer := range r.peers {
		attachAssembly(peer, a, &counter)
	}
	peer := r.peers[0]
	_, _, err = peer.execute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(peer.call("ROOT", map[string]any{}), &root))
	domain := root.Capabilities[1]
	notReady("ROOT")

	wait := map[string]any{"domain": domain, "after_sequence": 0}
	payload, receipt, _, _, err := peer.mappedExecute("VIEW_WAIT", wait)
	require(t, err)
	if string(payload) != `{"event":null,"status":"EMPTY"}` || receipt != nil {
		t.Fatalf("VIEW_WAIT before ACQUIRE: %s", payload)
	}
	guestWait, emptyLeg := peer.cells, peer.mappingLastNative
	notReady("VIEW_WAIT")

	families := c.Authorization.Enrollment.TicketFamilies[0]
	ticket := guestbridge.Ticket{Anchor: families.Acquisition.Anchor, Nonce: families.Acquisition.Nonce, Number: 1, Credentials: peer.credentials}
	payload, _, _, _, err = peer.mappedExecute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": domain, "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
	require(t, err)
	var acquisition struct {
		Status string
		Lease  guestbridge.ViewLease
	}
	require(t, json.Unmarshal(payload, &acquisition))
	if acquisition.Status != "GRANTED" {
		t.Fatalf("VIEW_ACQUIRE: %s", payload)
	}
	control := guestbridge.Ticket{Anchor: families.Control.Anchor, Nonce: families.Control.Nonce, Number: 1, Credentials: peer.credentials}
	payload, _, _, _, err = peer.mappedExecute("VIEW_RELEASE", map[string]any{"request": control, "lease": acquisition.Lease})
	require(t, err)
	if !bytes.Contains(payload, []byte(`"status":"PENDING"`)) {
		t.Fatalf("VIEW_RELEASE: %s", payload)
	}
	notReady("VIEW_RELEASE")

	// The wait that arrived before the acquire continues with its next leg.
	payload, receipt, _, _, err = peer.mappedWaitContinuation(guestWait, 1, wait, emptyLeg)
	require(t, err)
	if !bytes.Contains(payload, []byte(`"status":"EVENT"`)) || !bytes.Contains(payload, []byte(`"phase":"RECALL"`)) {
		t.Fatalf("continued VIEW_WAIT: %s", payload)
	}
	if receipt == nil || receipt.Kind != "EVENT" || receipt.PollCount != 2 {
		t.Fatalf("continued VIEW_WAIT receipt %+v", receipt)
	}
	notReady("EVENT")
	r5RecallAcks(t, peer, domain, control, payload, notReady)
}

// r5Chain sends control commands the way the guest kernel reuses its 16
// reserved control slots: each command reclaims the previous control response
// (guest and native cell), so a long VIEW_QUERY loop never exhausts the lane.
type r5Chain struct {
	peer          *sourceRig
	guest, native *guestbridge.Ack
}

func (c *r5Chain) exec(t *testing.T, op string, value any) json.RawMessage {
	t.Helper()
	payload, _, _, translated, err := c.peer.mappedExecuteReclaim(op, value, c.guest, c.native, nil)
	require(t, err)
	c.guest = &guestbridge.Ack{Cell: c.peer.cells, ResponseHash: hashBytes(translated)}
	c.native = c.peer.mappingLastNative
	return payload
}

// r5RecallAcks acknowledges the delivered RECALL in the zq3-r5 order:
// STOPPED, INSTALLED_DRAIN, about 20 release VIEW_QUERY polls that stay
// PENDING, an identical INSTALLED_DRAIN resend in a new cell (the guest's
// bounded resend after a local transport failure), then DRAINED,
// INSTALLED_FINAL and REOPENED, and finally the RELEASED release query.
func r5RecallAcks(t *testing.T, peer *sourceRig, domain guestbridge.Capability, control guestbridge.Ticket, recall json.RawMessage, notReady func(string)) {
	t.Helper()
	chain := &r5Chain{peer: peer}
	chain.phaseAck(t, domain, "STOPPED", recall)
	notReady("STOPPED")
	installed := chain.phaseAck(t, domain, "INSTALLED_DRAIN", nil)
	notReady("INSTALLED_DRAIN")
	for range 20 {
		if payload := chain.exec(t, "VIEW_QUERY", map[string]any{"request": control}); !bytes.Contains(payload, []byte(`"status":"PENDING"`)) {
			t.Fatalf("release query while waiting for DRAINED: %s", payload)
		}
	}
	notReady("VIEW_QUERY PENDING")
	chain.ack(t, "INSTALLED_DRAIN resend", installed)
	notReady("INSTALLED_DRAIN resend")
	for _, phase := range []string{"DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		chain.phaseAck(t, domain, phase, nil)
		notReady(phase)
	}
	if payload := chain.exec(t, "VIEW_QUERY", map[string]any{"request": control}); !bytes.Contains(payload, []byte(`"status":"RELEASED"`)) {
		t.Fatalf("release after REOPENED: %s", payload)
	}
	notReady("RELEASED")
}

// phaseAck acknowledges one recall phase: it polls the phase's event unless
// the caller already holds it, records the declared kernel observation for the
// exact ACK and sends it. It returns the ACK so that it can be resent as is.
func (c *r5Chain) phaseAck(t *testing.T, domain guestbridge.Capability, phase string, event json.RawMessage) map[string]any {
	t.Helper()
	if event == nil {
		event = c.exec(t, "VIEW_WAIT", map[string]any{"domain": domain, "after_sequence": 0})
	}
	var poll struct{ Event map[string]json.RawMessage }
	require(t, json.Unmarshal(event, &poll))
	if len(poll.Event) == 0 {
		t.Fatalf("missing %s event %s", phase, event)
	}
	ack := map[string]any{"domain": domain, "sequence": poll.Event["sequence"], "recall": poll.Event["recall"], "phase": phase, "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": c.peer.scope.Session, "manifest": nil, "target": poll.Event["target"]}
	if phase == "STOPPED" {
		ack["target"] = nil
	}
	if phase == "STOPPED" || phase == "DRAINED" {
		ack["manifest"] = map[string]any{"entries": []any{}, "hash": hashBytes([]byte("[]"))}
	}
	revision := uint64(1)
	if phase == "REOPENED" {
		revision = 2
	}
	c.peer.observeACK(poll.Event, ack, revision)
	c.ack(t, phase, ack)
	return ack
}

// ack sends one VIEW_ACK and requires ACCEPTED.
func (c *r5Chain) ack(t *testing.T, phase string, ack map[string]any) {
	t.Helper()
	if payload := c.exec(t, "VIEW_ACK", ack); !bytes.Equal(payload, []byte(`{"status":"ACCEPTED"}`)) {
		t.Fatalf("%s VIEW_ACK: %s", phase, payload)
	}
}

func TestRuntimeAssemblyMissingAdaptersRefuse(t *testing.T) {
	_, base := assemblyFixture(t)
	tests := map[string]func(*AssemblyConfig){
		"service": func(c *AssemblyConfig) { c.Service = nil }, "supervisor": func(c *AssemblyConfig) { c.Supervisor = nil }, "guest-admission": func(c *AssemblyConfig) { c.Admissions = nil }, "attestor": func(c *AssemblyConfig) { c.Attestor = nil }, "budget": func(c *AssemblyConfig) { c.Budget = nil }, "operator": func(c *AssemblyConfig) { c.Operator = nil }, "provider": func(c *AssemblyConfig) { c.Providers = nil }, "host-key": func(c *AssemblyConfig) { c.HostKeys = nil }, "explicit-provider-budget": func(c *AssemblyConfig) { c.ProviderLimits = ProviderLimits{} }, "capability-identities": func(c *AssemblyConfig) { c.CapabilityIdentities = 0 },
		"missing-original-domain": func(c *AssemblyConfig) {
			c.Authorization.Enrollment.Domains = append([]controlledvalidation.Enrollment(nil), c.Authorization.Enrollment.Domains[:1]...)
			c.Providers = c.Providers[:1]
			c.HostKeys = map[string]ed25519.PrivateKey{c.Providers[0].Enrollment.Scope.Session: c.HostKeys[c.Providers[0].Enrollment.Scope.Session]}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			c := base
			c.Directory = t.TempDir()
			change(&c)
			a, err := Assemble(t.Context(), c)
			if a != nil {
				require(t, a.Close())
			}
			if err == nil {
				t.Fatal("missing adapter admitted")
			}
		})
	}
}

func TestRuntimeFundingCompleteOwners(t *testing.T) {
	r, c := assemblyFixture(t)
	b := RuntimeBudget{HostBytes: 4 << 30, ServiceBytes: 64 << 20, ExporterBytes: 274 << 20, GuestCopyBytes: 16 << 20, GuestBytes: 2 << 30, GuestBaseBytes: 256 << 20, Guest: GuestArtifactBudget{OrdinaryBytes: 64 << 20, ControlBytes: 32 << 20, Reservations: 32, Cookies: 32, Received: 32}}
	funding, err := fundRuntime(b, c.ProviderLimits, c.Authorization.Enrollment.Bounds, 2)
	require(t, err)
	defer func() { require(t, funding.owner.Release()) }()
	resources := funding.transport.Usage().Used
	if resources.Workers != exporterDiagnosticWorkers+lifecycleWorkers || resources.Connections != exporterDiagnosticPipes+lifecycleConnections {
		t.Fatalf("diagnostic/lifecycle owners absent from runtime leases: %+v", resources)
	}
	control, err := lifecycleMemoryBytes(c.Authorization.Enrollment.Bounds)
	require(t, err)
	require(t, funding.releaseControl())
	require(t, funding.releaseControl()) // idempotent after the endpoint joined
	if after := funding.transport.Usage().Used; after.Bytes != resources.Bytes-control || after.Workers != exporterDiagnosticWorkers || after.Connections != exporterDiagnosticPipes {
		t.Fatalf("lifecycle control funding not separately releasable: %+v", after)
	}
	c.Budget = funding.transport
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	used := funding.transport.Usage().Used.Bytes
	if used < guestbridge.TransportLedgerMemoryBytes+(652<<20) {
		t.Fatal("C8 durable reservation absent")
	}
	for _, change := range []func(*RuntimeBudget){func(b *RuntimeBudget) { b.HostBytes = 1 << 30 }, func(b *RuntimeBudget) { b.GuestBytes = 4 << 30 }, func(b *RuntimeBudget) { b.GuestCopyBytes = 0 }, func(b *RuntimeBudget) { b.ServiceBytes = math.MaxUint64 }} {
		copy := b
		change(&copy)
		if f, err := fundRuntime(copy, c.ProviderLimits, c.Authorization.Enrollment.Bounds, 2); err == nil {
			_ = f.owner.Release()
			t.Fatal("incomplete/overflowed budget admitted")
		}
	}
	_ = r
}

// TestRuntimeFundingHostBoundary checks the host envelope at its exact sum and
// one byte short now that the 64 KiB lost-reply lease is pre-counted, and that
// at the exact sum the assembled runtime still finds the fixture's bytes and
// its operation slot, leaving the Memory pool exactly full.
func TestRuntimeFundingHostBoundary(t *testing.T) {
	_, c := assemblyFixture(t)
	views := c.Authorization.Enrollment.Bounds
	b := RuntimeBudget{ServiceBytes: 64 << 20, ExporterBytes: 274 << 20, GuestCopyBytes: 16 << 20, GuestBytes: 2 << 30, GuestBaseBytes: 256 << 20, Guest: GuestArtifactBudget{OrdinaryBytes: 64 << 20, ControlBytes: 32 << 20, Reservations: 32, Cookies: 32, Received: 32}}
	owners, err := runtimeOwnerBytes(b, c.ProviderLimits, views, 2)
	require(t, err)
	control, err := lifecycleMemoryBytes(views)
	require(t, err)
	exact, err := checkedBudgetSum(owners, control, lostReplyMemoryBytes, 652<<20, guestbridge.TransportLedgerMemoryBytes)
	require(t, err)
	b.HostBytes = exact - 1
	if f, err := fundRuntime(b, c.ProviderLimits, views, 2); !errors.Is(err, fsapi.NoSpace) {
		if f != nil {
			_ = f.owner.Release()
			_ = f.releaseControl()
		}
		t.Fatalf("host budget one byte short admitted: %v", err)
	}
	b.HostBytes = exact
	funding, err := fundRuntime(b, c.ProviderLimits, views, 2)
	require(t, err)
	defer func() { require(t, errors.Join(funding.owner.Release(), funding.releaseControl())) }()
	c.Budget = funding.transport
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	lease, err := funding.reserveLostReply()
	require(t, err)
	defer func() { require(t, lease.Release()) }()
	memory := funding.transport.Usage().Pools[admission.Memory]
	if memory.Bytes != exact-(640<<20) || memory.Operations != 5 {
		t.Fatalf("exact host envelope left Memory unaccounted: %+v", memory)
	}
}

func TestRuntimeCLIRefusesWithoutNativeAdapters(t *testing.T) {
	var out, errout bytes.Buffer
	if Run(t.Context(), nil, &out, &errout) == nil || out.Len() != 0 {
		t.Fatal("implicit runtime selected")
	}
	var c RuntimeConfig
	if decodeRuntimeJSON([]byte(`{"Version":"x","version":"y"}`), &c) == nil {
		t.Fatal("case alias accepted")
	}
	if decodeRuntimeJSON([]byte(`{"Version":"x","Version":"y"}`), &c) == nil {
		t.Fatal("duplicate accepted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := waitRuntimeEnrollment(ctx, filepath.Join(t.TempDir(), "never-installed")); err == nil {
		t.Fatal("missing operator handoff admitted")
	}
	file := filepath.Join(t.TempDir(), "authorization")
	require(t, os.WriteFile(file, []byte(`{}`), 0644))
	if readRuntimeFile(file, &RuntimeEnrollment{}) == nil {
		t.Fatal("public handoff accepted")
	}
}
