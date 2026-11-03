package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// These providers record real Service dispatch boundaries. They issue no durable
// lease/source proof: the tests verify delegation and refusal, not a controller.
type recordingViewProvider struct {
	scope                                               Context
	target                                              ViewTarget
	enrollment, requests, exchanges, responses, targets int
	requestErr, responseErr                             error
	reply                                               json.RawMessage
	request, responseRequest, responseBytes             json.RawMessage
}
type recordingViewAttestor struct {
	scope       Context
	credentials Credentials
	calls       int
	err         error
}
type recordingControlViews struct {
	calls int
	reply json.RawMessage
}
type viewDispatchFixture struct {
	bridge                      *bridgeFixture
	provider                    *recordingViewProvider
	attestor                    *recordingViewAttestor
	views                       *recordingControlViews
	domain, object, description Capability
	target                      ViewTarget
}

func (p *recordingViewProvider) CheckEnrollment(_ context.Context, c Context, protocol Protocol) error {
	p.enrollment++
	if c != p.scope || protocol != ViewProtocol() {
		return fsapi.Permission
	}
	return nil
}
func (p *recordingViewProvider) CheckRequest(_ context.Context, c Context, _ string, raw json.RawMessage) error {
	p.requests++
	p.request = bytes.Clone(raw)
	if c != p.scope {
		return fsapi.Permission
	}
	return p.requestErr
}
func (p *recordingViewProvider) CheckTarget(_ context.Context, c Context, target ViewTarget) error {
	p.targets++
	if c != p.scope || !equalWire(target, p.target) {
		return fsapi.Stale
	}
	return nil
}
func (p *recordingViewProvider) CheckResponse(_ context.Context, c Context, _ string, request, response json.RawMessage) error {
	p.responses++
	p.responseRequest = bytes.Clone(request)
	p.responseBytes = bytes.Clone(response)
	if c != p.scope {
		return fsapi.Permission
	}
	return p.responseErr
}
func (p *recordingViewProvider) ExchangeView(_ context.Context, c Context, _ string, request json.RawMessage) (json.RawMessage, error) {
	p.exchanges++
	if c != p.scope || !bytes.Equal(request, p.request) {
		return nil, fsapi.Permission
	}
	return bytes.Clone(p.reply), nil
}
func (a *recordingViewAttestor) Verify(_ context.Context, c Context, cred Credentials) error {
	a.calls++
	if c != a.scope || !equalWire(cred, a.credentials) {
		return fsapi.Permission
	}
	return a.err
}
func (v *recordingControlViews) ValidateDrain(context.Context, Binding) error {
	return fsapi.Unavailable
}
func (v *recordingControlViews) Exchange(_ context.Context, _ Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	v.calls++
	return bytes.Clone(v.reply), nil
}

func newViewDispatchFixture(t *testing.T) *viewDispatchFixture {
	t.Helper()
	f := newBridge(t)
	target := ViewTarget{Kind: "CONTROLLED_LOCAL", Cut: &LocalViewCut{EnrollmentHash: digest([]byte("enrollment fixture")), ProfileHash: digest([]byte("profile fixture")), ContextHash: wireHash(f.c), JournalIncarnation: f.c.OriginJournal, JournalEpoch: 1, JournalPosition: 7, JournalHash: digest([]byte("journal fixture")), StateHash: digest([]byte("state fixture")), VisibilityGeneration: uid(30).String()}}
	p := &recordingViewProvider{scope: f.c, target: target}
	a := &recordingViewAttestor{scope: f.c, credentials: f.cred()}
	v := &recordingControlViews{}
	config := f.s.config
	protocol := ViewProtocol()
	config.Protocol = &protocol
	config.ViewProtocol = p
	config.Attestor = a
	config.Views = v
	service, e := New(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	f.s = service
	x := &viewDispatchFixture{bridge: f, provider: p, attestor: a, views: v, target: target}
	hello := x.command(t, "HELLO", protocol)
	if _, e = service.Execute(t.Context(), f.native, hello); e != nil {
		t.Fatal(e)
	}
	root := x.call(t, "ROOT", map[string]any{})
	rootCap := capabilityAt(t, root, 0)
	x.domain = capabilityAt(t, root, 1)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	created := x.call(t, "CREATE", map[string]any{"parent": rootCap, "name": "766965772d6469737061746368", "mode": 0660, "umask": 0, "flags": flags})
	x.object = capabilityAt(t, created, 0)
	x.description = capabilityAt(t, created, 1)
	if p.enrollment != 1 {
		t.Fatal("enrollment provider skipped")
	}
	x.reset()
	return x
}
func (x *viewDispatchFixture) command(t *testing.T, op string, payload any) Command {
	t.Helper()
	x.bridge.next++
	protocol := ViewProtocol()
	return Command{Protocol: &protocol, Cell: x.bridge.next, Context: x.bridge.c, Op: op, Payload: mustJSON(t, payload)}
}
func (x *viewDispatchFixture) call(t *testing.T, op string, args any) map[string]any {
	t.Helper()
	intent := Intent{Opcode: op, Args: mustJSON(t, args)}
	call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "616263", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: x.bridge.next + 1, OriginalHash: wireHash(intent)}, Credentials: x.bridge.cred(), Intent: intent}
	c := x.command(t, "CALL", call)
	if _, e := x.bridge.s.Execute(t.Context(), x.bridge.native, c); e != nil {
		t.Fatal(e)
	}
	state, e := x.bridge.claims.Resolve(t.Context(), x.bridge.c, call.Claim)
	if e != nil || state.Status != "TERMINAL" {
		t.Fatalf("real dispatch fixture call: %v %s", e, state.Status)
	}
	value, e := Canonical(state.Result)
	if e != nil {
		t.Fatal(e)
	}
	result := value.(map[string]any)
	if result["code"] != "OK" {
		t.Fatal(result)
	}
	return result
}
func (x *viewDispatchFixture) reset() {
	x.provider.requests = 0
	x.provider.exchanges = 0
	x.provider.responses = 0
	x.provider.targets = 0
	x.attestor.calls = 0
	x.views.calls = 0
}
func (x *viewDispatchFixture) acquire(t *testing.T, access ViewAccess, mode string, target *ViewTarget) Command {
	t.Helper()
	ticket := Ticket{Anchor: uid(201).String(), Nonce: uid(202).String(), Number: 1, Credentials: x.bridge.cred()}
	return x.command(t, "VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": x.domain, "access": access, "mode": mode, "target": target})
}
func (x *viewDispatchFixture) lease(t *testing.T, c Command) map[string]any {
	t.Helper()
	value, e := Canonical(c.Payload)
	if e != nil {
		t.Fatal(e)
	}
	p := value.(map[string]any)
	lease := map[string]any{"scope": c.Context, "domain": p["domain"], "access": p["access"], "credential_hash": wireHash(x.bridge.cred()), "mode": p["mode"], "target": x.target, "fence": 1, "revision": 1}
	lease["id"] = wireHash(lease)
	return lease
}
func (x *viewDispatchFixture) reply(t *testing.T, c Command, status string, lease, reservation any) {
	t.Helper()
	value, e := Canonical(c.Payload)
	if e != nil {
		t.Fatal(e)
	}
	p := value.(map[string]any)
	reply := map[string]any{"status": status, "request": p["request"], "operation": c.Op, "request_hash": wireHash(map[string]any{"op": c.Op, "payload": p}), "lease": lease, "reservation": reservation, "entry": nil}
	reply["id"] = wireHash(reply)
	x.provider.reply = mustJSON(t, reply)
}
func (x *viewDispatchFixture) execute(t *testing.T, c Command) []byte {
	t.Helper()
	raw, e := x.bridge.s.Execute(t.Context(), x.bridge.native, c)
	if e != nil {
		t.Fatal(e)
	}
	v, e := Canonical(raw)
	if e != nil {
		t.Fatal(e)
	}
	o := v.(map[string]any)
	if !equalWire(o["protocol"], ViewProtocol()) || !equalWire(o["context"], c.Context) || o["op"] != c.Op || number(o["cell"]) != c.Cell || !equalWire(o["payload"], json.RawMessage(x.provider.reply)) {
		t.Fatal("response lost exact version/scope/bytes")
	}
	return raw
}

func TestViewDispatchSuccessfulBoundaries(t *testing.T) {
	for _, kind := range []string{"DOMAIN", "OBJECT", "PAGE"} {
		t.Run(kind, func(t *testing.T) {
			x := newViewDispatchFixture(t)
			access := ViewAccess{Kind: kind}
			mode := "READ"
			expectedTargets := 1
			var target *ViewTarget
			if kind == "OBJECT" {
				access.Object = &x.object
				target = &x.target
				expectedTargets = 1
			}
			if kind == "PAGE" {
				offset, length := uint64(0), uint64(4096)
				access.Description = &x.description
				access.Offset = &offset
				access.Length = &length
				mode = "EXCLUSIVE_DIRTY"
				target = &x.target
				expectedTargets = 1
			}
			c := x.acquire(t, access, mode, target)
			lease := x.lease(t, c)
			x.reply(t, c, "GRANTED", lease, nil)
			x.execute(t, c)
			if x.provider.requests != 1 || x.attestor.calls != 1 || x.provider.exchanges != 1 || x.provider.responses != 1 || x.provider.targets != expectedTargets {
				t.Fatalf("skipped real acquisition boundary: %+v attestation=%d", x.provider, x.attestor.calls)
			}
			if !bytes.Equal(x.provider.request, c.Payload) || !bytes.Equal(x.provider.responseRequest, c.Payload) || !bytes.Equal(x.provider.responseBytes, x.provider.reply) {
				t.Fatal("provider did not receive exact immutable request/reply")
			}
			if kind != "PAGE" {
				return
			}
			x.reset()
			ticket := Ticket{Anchor: uid(201).String(), Nonce: uid(202).String(), Number: 2, Credentials: x.bridge.cred()}
			cn := ClaimNumber{Realm: uid(60).String(), Subject: "616263", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 99}
			reserve := x.command(t, "DIRTY_RESERVE", map[string]any{"request": ticket, "lease": lease, "description": x.description, "offset": 0, "length": 4096, "claim_number": cn})
			r := map[string]any{"scope": x.bridge.c, "lease_id": lease["id"], "description": x.description, "credentials": x.bridge.cred(), "offset": 0, "length": 4096, "claim_number": cn, "provenance": uid(203).String(), "drain_credit": digest([]byte("preowned fixture credit"))}
			r["id"] = wireHash(r)
			x.reply(t, reserve, "GRANTED", nil, r)
			x.execute(t, reserve)
			if x.provider.exchanges != 1 || x.provider.responses != 1 || x.attestor.calls != 1 {
				t.Fatal("reservation did not dispatch through attestation and response protection")
			}
		})
	}
}

func TestViewDispatchIndependentRefusalGates(t *testing.T) {
	cases := []struct {
		name                                  string
		mutate                                func(*testing.T, *viewDispatchFixture, *Command)
		exchanges, responses, targets, attest int
	}{
		{"original_request_authorization", func(t *testing.T, x *viewDispatchFixture, _ *Command) { x.provider.requestErr = fsapi.Permission }, 0, 0, 0, 0},
		{"original_credentials_attestation", func(t *testing.T, x *viewDispatchFixture, _ *Command) { x.attestor.err = fsapi.Permission }, 0, 0, 0, 1},
		{"forged_domain", func(t *testing.T, x *viewDispatchFixture, _ *Command) {
			delete(x.bridge.s.sessions[x.bridge.c.Session].Grants, x.domain.Token)
		}, 0, 0, 0, 1},
		{"released_domain", func(t *testing.T, x *viewDispatchFixture, _ *Command) {
			x.bridge.s.sessions[x.bridge.c.Session].Grants[x.domain.Token].Released = true
		}, 0, 0, 0, 1},
		{"forged_object", func(t *testing.T, x *viewDispatchFixture, _ *Command) {
			x.bridge.s.sessions[x.bridge.c.Session].Grants[x.object.Token].Cap.Revision++
		}, 0, 0, 0, 1},
		{"released_object", func(t *testing.T, x *viewDispatchFixture, _ *Command) {
			x.bridge.s.sessions[x.bridge.c.Session].Grants[x.object.Token].Released = true
		}, 0, 0, 0, 1},
		{"protected_response_refusal", func(t *testing.T, x *viewDispatchFixture, _ *Command) { x.provider.responseErr = fsapi.Permission }, 1, 1, 0, 1},
		{"returned_target_source", func(t *testing.T, x *viewDispatchFixture, _ *Command) {
			v, e := Canonical(x.provider.reply)
			if e != nil {
				t.Fatal(e)
			}
			o := v.(map[string]any)
			lease := o["lease"].(map[string]any)
			lease["target"].(map[string]any)["cut"].(map[string]any)["journal_hash"] = digest([]byte("unverified source"))
			delete(lease, "id")
			lease["id"] = wireHash(lease)
			delete(o, "id")
			o["id"] = wireHash(o)
			raw, e := marshal(o)
			if e != nil {
				t.Fatal(e)
			}
			x.provider.reply = raw
		}, 1, 1, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newViewDispatchFixture(t)
			c := x.acquire(t, ViewAccess{Kind: "OBJECT", Object: &x.object}, "READ", nil)
			x.reply(t, c, "GRANTED", x.lease(t, c), nil)
			// Prove this exact request/provider setup reaches success before isolating
			// one refusal. That prevents unrelated schema or setup errors masking gates.
			x.execute(t, c)
			x.reset()
			tc.mutate(t, x, &c)
			raw, e := x.bridge.s.Execute(t.Context(), x.bridge.native, c)
			if e == nil || len(raw) != 0 {
				t.Fatal("refused boundary leaked successful response")
			}
			if !errors.Is(e, fsapi.Permission) && !errors.Is(e, fsapi.Stale) {
				t.Fatalf("unrelated failure masked gate: %v", e)
			}
			if x.provider.requests != 1 || x.provider.exchanges != tc.exchanges || x.provider.responses != tc.responses || x.provider.targets != tc.targets || x.attestor.calls != tc.attest {
				t.Fatalf("wrong boundary reached: requests=%d exchange=%d response=%d target=%d attest=%d", x.provider.requests, x.provider.exchanges, x.provider.responses, x.provider.targets, x.attestor.calls)
			}
		})
	}
}

func (x *viewDispatchFixture) dirtyAcquire(t *testing.T) Command {
	t.Helper()
	offset, length := uint64(0), uint64(4096)
	return x.acquire(t, ViewAccess{Kind: "PAGE", Description: &x.description, Offset: &offset, Length: &length}, "EXCLUSIVE_DIRTY", nil)
}
func (x *viewDispatchFixture) pendingReserve(t *testing.T, lease map[string]any) Command {
	t.Helper()
	ticket := Ticket{Anchor: uid(201).String(), Nonce: uid(202).String(), Number: 2, Credentials: x.bridge.cred()}
	cn := ClaimNumber{Realm: uid(60).String(), Subject: "616263", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 99}
	c := x.command(t, "DIRTY_RESERVE", map[string]any{"request": ticket, "lease": lease, "description": x.description, "offset": 0, "length": 4096, "claim_number": cn})
	x.reply(t, c, "PENDING", nil, nil)
	return c
}
func TestViewDispatchDescriptionIssuanceGates(t *testing.T) {
	for _, stage := range []string{"acquire", "reserve"} {
		for _, mutation := range []string{"forged", "released"} {
			t.Run(stage+"/"+mutation, func(t *testing.T) {
				x := newViewDispatchFixture(t)
				c := x.dirtyAcquire(t)
				lease := x.lease(t, c)
				x.reply(t, c, "GRANTED", lease, nil)
				x.execute(t, c)
				if stage == "reserve" {
					c = x.pendingReserve(t, lease)
					x.execute(t, c)
				}
				x.reset()
				grant := x.bridge.s.sessions[x.bridge.c.Session].Grants[x.description.Token]
				if mutation == "forged" {
					grant.Cap.Revision++
				} else {
					grant.Released = true
				}
				raw, e := x.bridge.s.Execute(t.Context(), x.bridge.native, c)
				if !errors.Is(e, fsapi.Permission) || len(raw) != 0 || x.attestor.calls != 1 || x.provider.requests != 1 || x.provider.exchanges != 0 || x.provider.responses != 0 {
					t.Fatalf("description issuance bypass/masked error: %v", e)
				}
			})
		}
	}
}
func TestViewDispatchExpectedTargetReachesIssuer(t *testing.T) {
	x := newViewDispatchFixture(t)
	c := x.acquire(t, ViewAccess{Kind: "DOMAIN"}, "READ", &x.target)
	x.reply(t, c, "PENDING", nil, nil)
	x.execute(t, c)
	x.reset()
	value, e := Canonical(c.Payload)
	if e != nil {
		t.Fatal(e)
	}
	payload := value.(map[string]any)
	payload["target"].(map[string]any)["cut"].(map[string]any)["state_hash"] = digest([]byte("unverified expected state"))
	c.Payload = mustJSON(t, payload)
	// Expected targets carry no authority. A stale or pruned expectation must
	// reach the issuer so it can return PENDING/BLOCKED and drive fresh install.
	// Source proof remains mandatory for every actual returned Lease.target.
	x.reply(t, c, "PENDING", nil, nil)
	raw, e := x.bridge.s.Execute(t.Context(), x.bridge.native, c)
	if e != nil || len(raw) == 0 || x.provider.targets != 0 || x.provider.exchanges != 1 || x.provider.responses != 1 || x.attestor.calls != 1 {
		t.Fatalf("stale expectation failed to reach issuer: %v", e)
	}
}
func (x *viewDispatchFixture) control(t *testing.T, op string) Command {
	t.Helper()
	recall := uid(204).String()
	manifestHash := digest([]byte("frozen manifest fixture"))
	if op == "VIEW_ACK" {
		x.views.reply = mustJSON(t, map[string]any{"status": "ACCEPTED"})
		return x.command(t, op, map[string]any{"domain": x.domain, "sequence": 1, "recall": recall, "phase": "INSTALLED_DRAIN", "authority_hash": x.bridge.c.AuthorityHash, "closure_hash": digest([]byte("closure")), "inventory_hash": digest([]byte("inventory")), "session": x.bridge.c.Session, "manifest": nil, "target": x.target})
	}
	event := map[string]any{"sequence": 1, "recall": recall, "phase": "INSTALL_DRAIN", "domain": x.domain, "authority_hash": x.bridge.c.AuthorityHash, "closure_hash": digest([]byte("closure")), "inventory_hash": digest([]byte("inventory")), "old_visibility": uid(205).String(), "target": x.target, "manifest_hash": manifestHash}
	x.views.reply = mustJSON(t, map[string]any{"status": "EVENT", "event": event})
	return x.command(t, op, map[string]any{"domain": x.domain, "after_sequence": 0})
}
func TestViewDispatchControlTargetSourceGates(t *testing.T) {
	for _, op := range []string{"VIEW_ACK", "VIEW_POLL"} {
		t.Run(op, func(t *testing.T) {
			x := newViewDispatchFixture(t)
			c := x.control(t, op)
			raw, e := x.bridge.s.Execute(t.Context(), x.bridge.native, c)
			if e != nil {
				t.Fatal(e)
			}
			value, e := Canonical(raw)
			if e != nil {
				t.Fatal(e)
			}
			envelope := value.(map[string]any)
			if !equalWire(envelope["payload"], json.RawMessage(x.views.reply)) || !equalWire(envelope["protocol"], ViewProtocol()) || x.views.calls != 1 || x.provider.targets != 1 || x.provider.requests != 1 {
				t.Fatal("control path skipped request/target/exchange or changed reply")
			}
			x.reset()
			if op == "VIEW_ACK" {
				value, e = Canonical(c.Payload)
				if e != nil {
					t.Fatal(e)
				}
				p := value.(map[string]any)
				p["target"].(map[string]any)["cut"].(map[string]any)["journal_hash"] = digest([]byte("unverified input source"))
				c.Payload = mustJSON(t, p)
			} else {
				value, e = Canonical(x.views.reply)
				if e != nil {
					t.Fatal(e)
				}
				p := value.(map[string]any)
				p["event"].(map[string]any)["target"].(map[string]any)["cut"].(map[string]any)["journal_hash"] = digest([]byte("unverified output source"))
				x.views.reply = mustJSON(t, p)
			}
			raw, e = x.bridge.s.Execute(t.Context(), x.bridge.native, c)
			expectedExchanges := 0
			if op == "VIEW_POLL" {
				expectedExchanges = 1
			}
			if !errors.Is(e, fsapi.Stale) || len(raw) != 0 || x.provider.requests != 1 || x.provider.targets != 1 || x.views.calls != expectedExchanges {
				t.Fatalf("control source boundary bypass/masked error: %v, targets=%d exchange=%d", e, x.provider.targets, x.views.calls)
			}
		})
	}
}
