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
	config.OwnerDirectory = filepath.Join(t.TempDir(), "must-stay-absent")
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
		peer.bridge = a.Bridge
		peer.producer = a.Producer
		peer.controller = a.Controller
		peer.facts = a.Facts
		peer.provider = a.Providers[peer.scope.Session]
		peer.mappingTransport = a.Transport
		peer.mappingNativeCell = &counter
		peer.cells = 0
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
	if resources.Workers != exporterDiagnosticWorkers || resources.Connections != exporterDiagnosticPipes {
		t.Fatalf("diagnostic owners absent from retained runtime lease: %+v", resources)
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
