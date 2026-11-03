//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func TestProviderRouterRealTwoMounts(t *testing.T) {
	for _, protocol := range []guestbridge.Protocol{guestbridge.ControlledProtocol(), guestbridge.CleanupProtocol()} {
		t.Run(protocolName(protocol), func(t *testing.T) {
			r := newSourceRigDomains(t, protocol, 2)
			if r.peers[0].provider == r.peers[1].provider || r.peers[0].scope.GuestMount == r.peers[1].scope.GuestMount {
				t.Fatal("fixture did not create distinct providers/mounts")
			}
			for i, peer := range r.peers {
				if peer.scope.VM != r.scope.VM || peer.scope.GuestBoot != r.scope.GuestBoot || peer.native.Endpoint != r.native.Endpoint || peer.supervisor != r.supervisor || peer.service != r.service || peer.bridge != r.bridge || peer.controller != r.controller || peer.facts != r.facts {
					t.Fatal("fixture changed single-VM/endpoint actual owners")
				}
				root := peer.call("ROOT", map[string]any{})
				var result struct {
					ID           string
					Code         string
					Publication  json.RawMessage
					Capabilities []guestbridge.Capability
				}
				require(t, json.Unmarshal(root, &result))
				if result.Code != "OK" || !bytes.Equal(result.Publication, []byte("null")) || len(result.Capabilities) != 2 {
					t.Fatalf("ROOT: %s", root)
				}
				family := r.facts.enrollment.TicketFamilies[i].Acquisition
				ticket := guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: 1, Credentials: peer.credentials}
				_, receipt, err := peer.execute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": result.Capabilities[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
				require(t, err)
				if receipt == nil || receipt.Kind != "SOURCE" || receipt.Scope != peer.scope {
					t.Fatal("missing exact-scope real source receipt")
				}
				intent := guestbridge.Intent{Opcode: "ROOT", Args: json.RawMessage(`{}`)}
				intentBytes, err := canonicalJSON(intent)
				require(t, err)
				gap := guestbridge.Call{Claim: guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: 2, OriginalHash: hashBytes(intentBytes)}, Credentials: peer.credentials, Intent: intent}
				_, _, err = peer.execute("ALLOCATE", gap)
				require(t, err)
				gapReply, _, err := peer.execute("GAP_RESOLVE", gap)
				require(t, err)
				var gapResult struct {
					Status    string
					ProofHash string `json:"proof_hash"`
				}
				require(t, json.Unmarshal(gapReply, &gapResult))
				if gapResult.Status != "GAP" || len(gapResult.ProofHash) != 64 {
					t.Fatalf("unprotected gap reply: %s", gapReply)
				}
				request := map[string]any{"realm": testUUID(100).String(), "subject": "616263", "anchor": testUUID(101).String(), "nonce": testUUID(103).String(), "through": 1, "result_hashes": []string{result.ID}, "credentials": peer.credentials}
				_, receipt, err = peer.execute("RETIRE", request)
				require(t, err)
				if receipt == nil || receipt.Kind != "FLOOR" || receipt.Scope != peer.scope {
					t.Fatal("missing exact-scope floor")
				}
			}
		})
	}
}

func protocolName(p guestbridge.Protocol) string {
	if p == guestbridge.CleanupProtocol() {
		return "1.3"
	}
	return "1.2"
}

func TestProviderRouterConstructionAndBinding(t *testing.T) {
	r := newSourceRigDomains(t, guestbridge.ControlledProtocol(), 2)
	a, b := r.peers[0], r.peers[1]
	routes := map[guestbridge.Context]*controlledvalidation.Provider{a.scope: a.provider, b.scope: b.provider}
	router, err := NewProviderRouter(t.Context(), r.service, routes)
	require(t, err)
	delete(routes, a.scope)
	actual, err := router.route(a.scope)
	require(t, err)
	if actual != a.provider {
		t.Fatal("caller mutation changed route")
	}
	for name, invalid := range map[string]map[guestbridge.Context]*controlledvalidation.Provider{
		"empty": {}, "nil": {a.scope: nil}, "swapped": {a.scope: b.provider, b.scope: a.provider},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewProviderRouter(t.Context(), r.service, invalid); err == nil {
				t.Fatal("invalid router accepted")
			}
		})
	}
	foreign := a.scope
	foreign.GuestMount = testUUID(240).String()
	if _, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{foreign: a.provider}); err == nil {
		t.Fatal("foreign same-session key accepted")
	}
	keys := map[string]ed25519.PrivateKey{a.scope.Session: a.key, b.scope.Session: b.key}
	providers := map[string]*controlledvalidation.Provider{a.scope.Session: a.provider, b.scope.Session: b.provider}
	binding := guestbridge.VerificationBinding{Controller: testUUID(241), Endpoint: r.native.Endpoint, Native: r.supervisor, Claims: router, Execution: router, Views: r.controller, ViewProtocol: r.controller, Scopes: []guestbridge.Context{a.scope, b.scope}}
	fresh := func() *Producer {
		p, err := NewProducer(t.Context(), r.facts, r.controller, providers, keys)
		require(t, err)
		return p
	}
	missing, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{a.scope: a.provider})
	require(t, err)
	foreignProvider := r.providerFactory(0)
	require(t, foreignProvider.BindAdmission(r.controller))
	foreignRouter, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{a.scope: foreignProvider, b.scope: b.provider})
	require(t, err)
	other, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{a.scope: a.provider, b.scope: b.provider})
	require(t, err)
	for name, alter := range map[string]func(*guestbridge.VerificationBinding){
		"missing":                       func(b *guestbridge.VerificationBinding) { b.Claims, b.Execution = missing, missing },
		"foreign-provider-same-session": func(b *guestbridge.VerificationBinding) { b.Claims, b.Execution = foreignRouter, foreignRouter },
		"wrong-endpoint":                func(b *guestbridge.VerificationBinding) { b.Endpoint.ResultNode++ },
		"unequal":                       func(b *guestbridge.VerificationBinding) { b.Execution = other },
		"direct-multiple":               func(b *guestbridge.VerificationBinding) { b.Claims, b.Execution = a.provider, a.provider },
		"foreign-context":               func(b *guestbridge.VerificationBinding) { b.Scopes = []guestbridge.Context{foreign} },
	} {
		t.Run(name, func(t *testing.T) {
			p := fresh()
			invalid := binding
			alter(&invalid)
			if p.Bind(t.Context(), invalid) == nil {
				t.Fatal("invalid actual provider binding accepted")
			}
			if len(p.bindings) != 0 {
				t.Fatal("failed bind installed partial authority")
			}
		})
	}
	require(t, fresh().Bind(t.Context(), binding))
	direct := binding
	direct.Scopes = []guestbridge.Context{a.scope}
	direct.Claims, direct.Execution = a.provider, a.provider
	require(t, fresh().Bind(t.Context(), direct))
	unknown := a.scope
	unknown.Session = testUUID(242).String()
	for _, scope := range []guestbridge.Context{foreign, unknown} {
		binding := guestbridge.Binding{Context: scope}
		for name, invoke := range map[string]func() error{
			"resolve":    func() error { _, err := router.Resolve(t.Context(), scope, guestbridge.Claim{}); return err },
			"reserve":    func() error { _, err := router.Reserve(t.Context(), binding); return err },
			"protect":    func() error { err := router.ProtectResult(t.Context(), binding, nil); return err },
			"cancel":     func() error { _, err := router.Cancel(t.Context(), scope, guestbridge.Claim{}); return err },
			"retire":     func() error { _, err := router.Retire(t.Context(), scope, nil); return err },
			"check":      func() error { err := router.Check(t.Context(), scope); return err },
			"generation": func() error { _, err := router.ServingGeneration(t.Context(), scope); return err },
			"execute":    func() error { _, err := router.Execute(t.Context(), binding, nil); return err },
			"enrollment": func() error {
				err := router.CheckValidation(t.Context(), scope, r.facts.enrollment.Domains[0].Profile, r.native.Endpoint)
				return err
			},
			"restore":            func() error { _, err := router.Restore(t.Context(), scope); return err },
			"finalize":           func() error { err := router.Finalize(t.Context(), binding, nil, nil); return err },
			"recover":            func() error { _, _, err := router.RecoverOutcome(t.Context(), binding); return err },
			"validation-execute": func() error { _, err := router.ExecuteValidation(t.Context(), binding, nil, nil); return err },
			"continuation":       func() error { _, err := router.ResumeValidation(t.Context(), binding, nil, nil); return err },
			"allocate":           func() error { err := router.AllocateOriginal(t.Context(), scope, guestbridge.Call{}); return err },
			"gap":                func() error { _, err := router.ResolveOriginalGap(t.Context(), scope, guestbridge.Call{}); return err },
			"retire-original":    func() error { _, err := router.RetireOriginal(t.Context(), scope, nil); return err },
			"cleanup":            func() error { _, err := router.CleanupReaddir(t.Context(), scope, nil); return err },
			"protocol":           func() error { err := router.CheckBridgeProtocol(t.Context(), scope, nil); return err },
		} {
			t.Run(name, func(t *testing.T) {
				if err := invoke(); !errors.Is(err, fsapi.Permission) {
					t.Fatalf("foreign scope escaped route guard: %v", err)
				}
			})
		}
	}
	// A provider from another real service cannot join or replace these routes.
	otherRig := newSourceRigProtocol(t, guestbridge.ControlledProtocol())
	if _, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{otherRig.scope: otherRig.provider}); err == nil {
		t.Fatal("single foreign-service provider accepted")
	}
	if _, err := NewProviderRouter(t.Context(), r.service, map[guestbridge.Context]*controlledvalidation.Provider{a.scope: a.provider, otherRig.scope: otherRig.provider}); err == nil {
		t.Fatal("cross-service router accepted")
	}
}
