package controlledvalidation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admission"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// allocationViews stops at the platform boundary. These tests use the actual
// bridge, provider and temporary journal, but do not assert kernel/source proof.
type allocationViews struct {
	scope guestbridge.Context
	calls *int
}

type allocationCredentialFacts struct {
	original  credentialFacts
	alternate guestbridge.Credentials
}

type allocationHarness struct {
	f        *fixture
	bridge   *guestbridge.Service
	config   guestbridge.Config
	native   guestbridge.Native
	original guestbridge.Call
	cell     uint64
}

func (a allocationCredentialFacts) Verify(ctx context.Context, scope guestbridge.Context, credentials guestbridge.Credentials) error {
	revision := a.original.expected
	revision.Revision++
	if equalJSON(credentials, revision) {
		return (credentialFacts{expected: revision, scope: a.original.scope}).Verify(ctx, scope, credentials)
	}
	if equalJSON(credentials, a.alternate) {
		return (credentialFacts{expected: a.alternate, scope: a.original.scope}).Verify(ctx, scope, credentials)
	}
	return a.original.Verify(ctx, scope, credentials)
}
func alternateAllocationCredentials(original guestbridge.Credentials) guestbridge.Credentials {
	original.Process = id(68).String()
	original.Groups = []uint32{201}
	return original
}

func (v allocationViews) CheckEnrollment(_ context.Context, scope guestbridge.Context, p guestbridge.Protocol) error {
	if scope != v.scope || p != guestbridge.ControlledProtocol() {
		return fsapi.Permission
	}
	return nil
}
func (allocationViews) CheckTarget(context.Context, guestbridge.Context, guestbridge.ViewTarget) error {
	return fsapi.Unsupported
}
func (v allocationViews) CheckRequest(ctx context.Context, scope guestbridge.Context, op string, raw json.RawMessage) error {
	observed, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || scope != v.scope || observed.Scope != scope || observed.Operation != op || observed.PayloadHash != hash(raw) || observed.Protocol == nil || *observed.Protocol != guestbridge.ControlledProtocol() {
		return fsapi.Permission
	}
	return nil
}
func (allocationViews) CheckResponse(context.Context, guestbridge.Context, string, json.RawMessage, json.RawMessage) error {
	return fsapi.Unsupported
}
func (v allocationViews) ExchangeView(context.Context, guestbridge.Context, string, json.RawMessage) (json.RawMessage, error) {
	*v.calls++
	return nil, fsapi.Unsupported
}

func newAllocationHarness(t *testing.T) *allocationHarness {
	t.Helper()
	f := newFixture(t)
	mustAllocation(t, f.provider.Close())
	f.provider = nil
	protocol := guestbridge.ControlledProtocol()
	f.pc.Authorization.Enrollment.Protocol = &protocol
	f.pc.Authorization.Enrollment.Ledger = id(67)
	f.pc.Directory = tempDirectory(t)
	f.pc.Initialize = true
	f.enroll()
	f.open()
	scope := f.pc.Authorization.Enrollment.Scope
	call := guestbridge.Call{Claim: guestbridge.Claim{Realm: id(60).String(), Subject: "616263", Anchor: id(61).String(), Nonce: id(62).String(), Number: 1}, Credentials: guestbridge.Credentials{FSUID: 1000, FSGID: 1001, Groups: []uint32{200}, Process: id(63).String(), Revision: 1, IDMapHash: hash([]byte("map")), UserNamespace: id(64).String()}, Intent: guestbridge.Intent{Opcode: "ROOT", Args: json.RawMessage(`{}`)}}
	raw, err := canonicalJSON(call.Intent)
	mustAllocation(t, err)
	call.Claim.OriginalHash = hash(raw)
	native := guestbridge.Native{UID: 1000, GID: 1001, PID: 123, Unique: 1, Groups: []uint32{200}, Process: id(66).String(), Endpoint: f.pc.Authorization.Enrollment.Endpoint}
	config := guestbridge.Config{Protocol: &protocol, Validation: f.config.Validation, Endpoint: native.Endpoint, Native: &nativeFacts{native: native}, Attestor: allocationCredentialFacts{original: credentialFacts{expected: call.Credentials, scope: scope}, alternate: alternateAllocationCredentials(call.Credentials)}, Claims: f.provider, Execution: f.provider, Views: noViews{}, ViewProtocol: allocationViews{scope: scope, calls: new(int)}, Scopes: []guestbridge.Context{scope}}
	bridge, err := guestbridge.New(t.Context(), config)
	mustAllocation(t, err)
	h := &allocationHarness{f: f, bridge: bridge, config: config, native: native, original: call}
	_, err = h.send("HELLO", protocol)
	mustAllocation(t, err)
	return h
}
func mustAllocation(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (h *allocationHarness) command(op string, payload any) guestbridge.Command {
	h.f.t.Helper()
	h.cell++
	raw, err := canonicalJSON(payload)
	mustAllocation(h.f.t, err)
	return guestbridge.Command{Protocol: h.config.Protocol, Cell: h.cell, Context: h.config.Scopes[0], Op: op, Payload: raw}
}
func (h *allocationHarness) send(op string, payload any) ([]byte, error) {
	return h.bridge.Execute(h.f.t.Context(), h.native, h.command(op, payload))
}
func (h *allocationHarness) stored(call guestbridge.Call) (entry, bool) {
	h.f.t.Helper()
	var e entry
	var exists bool
	err := h.f.provider.db.View(func(tx *bolt.Tx) error { var err error; e, exists, err = h.f.provider.load(tx, call.Claim); return err })
	mustAllocation(h.f.t, err)
	return e, exists
}
func allocationPayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var envelope struct{ Payload map[string]any }
	mustAllocation(t, json.Unmarshal(raw, &envelope))
	return envelope.Payload
}

func TestControlledAllocationExactLostReplyAndCall(t *testing.T) {
	h := newAllocationHarness(t)
	// Ordinary CALL cannot issue a number, even in the negotiated successor.
	raw, err := h.send("CALL", h.original)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "REJECTED" {
		t.Fatal(string(raw))
	}
	if _, exists := h.stored(h.original); exists {
		t.Fatal("CALL allocated")
	}
	first, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	e, exists := h.stored(h.original)
	if !exists || e.Status != "ALLOCATED" || e.Original == nil || e.Binding.Operation.Valid() {
		t.Fatal("allocation did not retain exact unexecuted original")
	}
	retry, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	// Cell changes; the immutable protected acknowledgment payload does not.
	a, _ := canonicalJSON(allocationPayload(t, first))
	b, _ := canonicalJSON(allocationPayload(t, retry))
	if !bytes.Equal(a, b) {
		t.Fatal("lost allocation reply changed acknowledgment")
	}
	changed := h.original
	changed.Credentials.Groups = []uint32{201}
	if _, err = h.send("ALLOCATE", changed); err == nil {
		t.Fatal("changed credentials accepted")
	}
	raw, err = h.send("CALL", h.original)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "ADMITTED" {
		t.Fatal(string(raw))
	}
	e, _ = h.stored(h.original)
	if e.Status != "TERMINAL" || !controlledResult(e.Result) || !e.Binding.Operation.Valid() {
		t.Fatal("actual service result not protected")
	}
	if _, err = h.send("GAP_RESOLVE", h.original); err == nil {
		t.Fatal("terminal claim became gap")
	}
}

func TestControlledAllocationAuthenticationAndIdentity(t *testing.T) {
	h := newAllocationHarness(t)
	for _, name := range []string{"native", "scope", "credentials", "intent", "number", "protocol", "legacy"} {
		t.Run(name, func(t *testing.T) {
			call := h.original
			n := h.native
			c := h.command("ALLOCATE", call)
			switch name {
			case "native":
				n.PID++
			case "scope":
				c.Context.GuestBoot = id(99).String()
			case "credentials":
				call.Credentials.Groups = []uint32{201}
				c = h.command("ALLOCATE", call)
			case "intent":
				call.Claim.OriginalHash = hash([]byte("different"))
				c = h.command("ALLOCATE", call)
			case "number":
				call.Claim.Number = 2
				c = h.command("ALLOCATE", call)
			case "protocol":
				p := guestbridge.ViewProtocol()
				c.Protocol = &p
			case "legacy":
				c.Protocol = nil
			}
			if _, err := h.bridge.Execute(t.Context(), n, c); err == nil {
				t.Fatal("invalid allocation accepted")
			}
			if _, exists := h.stored(h.original); exists {
				t.Fatal("rejection allocated")
			}
		})
	}
	if err := h.f.provider.AllocateOriginal(t.Context(), h.config.Scopes[0], h.original); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("external context %v", err)
	}
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	changed := h.original
	changed.Claim.OriginalHash = hash([]byte("changed"))
	if err = h.f.provider.Allocate(t.Context(), h.config.Scopes[0], changed.Claim); err == nil {
		t.Fatal("identity replaced")
	}
	// Repeated HELLO cannot select another enrollment or rewrite its tuple.
	_, err = h.send("HELLO", guestbridge.ControlledProtocol())
	mustAllocation(t, err)
	if _, err = h.send("HELLO", guestbridge.ViewProtocol()); err == nil {
		t.Fatal("HELLO downgraded")
	}
	for _, p := range []*guestbridge.Protocol{nil, ptrProtocol(guestbridge.ViewProtocol())} {
		config := h.config
		config.Protocol = p
		config.Native = &nativeFacts{native: h.native}
		if _, err = guestbridge.New(t.Context(), config); err == nil {
			t.Fatal("signed1.2 profile downgraded")
		}
	}
	config := h.config
	config.Validation = nil
	config.Native = &nativeFacts{native: h.native}
	if _, err = guestbridge.New(t.Context(), config); err == nil {
		t.Fatal("normal mode accepted controlled successor")
	}
}
func ptrProtocol(p guestbridge.Protocol) *guestbridge.Protocol { return &p }

func TestControlledGapCoverageAndRestart(t *testing.T) {
	h := newAllocationHarness(t)
	if _, err := h.send("GAP_RESOLVE", h.original); err == nil {
		t.Fatal("unissued gap")
	}
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	raw, err := h.send("GAP_RESOLVE", h.original)
	mustAllocation(t, err)
	proof := allocationPayload(t, raw)["proof_hash"].(string)
	again, err := h.send("GAP_RESOLVE", h.original)
	mustAllocation(t, err)
	if allocationPayload(t, again)["proof_hash"] != proof {
		t.Fatal("gap reply loss changed proof")
	}
	c := h.original.Claim
	retire := map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "through": uint64(2), "result_hashes": []string{proof, proof}, "credentials": h.original.Credentials}
	raw, err = h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "BLOCKED" {
		t.Fatal("unissued floor skipped")
	}
	retire["through"] = uint64(1)
	retire["result_hashes"] = []string{proof}
	retire["credentials"] = alternateAllocationCredentials(h.original.Credentials)
	if _, err = h.send("RETIRE", retire); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("attested foreign original retired family: %v", err)
	}
	retire["credentials"] = h.original.Credentials
	retire["result_hashes"] = []string{hash([]byte("wrong"))}
	raw, err = h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "BLOCKED" {
		t.Fatal("wrong coverage retired")
	}
	retire["result_hashes"] = []string{proof}
	raw, err = h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "RETIRED" {
		t.Fatal(string(raw))
	}
	retry, err := h.send("RETIRE", retire)
	mustAllocation(t, err)
	originalPayload, _ := canonicalJSON(allocationPayload(t, raw))
	retryPayload, _ := canonicalJSON(allocationPayload(t, retry))
	if !bytes.Equal(originalPayload, retryPayload) {
		t.Fatal("lost retirement reply changed protected floor receipt")
	}
	mustAllocation(t, h.f.provider.Close())
	h.f.provider = nil
	h.f.open()
	replay, err := h.f.provider.Retire(t.Context(), h.config.Scopes[0], mustAllocationJSON(t, retire))
	mustAllocation(t, err)
	a, _ := canonicalJSON(allocationPayload(t, raw))
	b, err := canonicalJSON(json.RawMessage(replay))
	mustAllocation(t, err)
	if !bytes.Equal(a, b) {
		t.Fatal("restart retirement reply changed")
	}
	state, err := h.f.provider.Resolve(t.Context(), h.config.Scopes[0], c)
	mustAllocation(t, err)
	if state.Status != "RETIRED_ID" {
		t.Fatal(state.Status)
	}
}
func mustAllocationJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := canonicalJSON(v)
	mustAllocation(t, err)
	return raw
}

func TestControlledGapRefusesPreparationAndEffects(t *testing.T) {
	for _, failpoint := range []string{"prepared-before-plan-protection", "plan-protected", "effect-before-outcome"} {
		t.Run(failpoint, func(t *testing.T) {
			h := newAllocationHarness(t)
			_, err := h.send("ALLOCATE", h.original)
			mustAllocation(t, err)
			_, err = h.send("CALL", h.original)
			mustAllocation(t, err)
			root, _ := h.stored(h.original)
			var result wireResult
			mustAllocation(t, json.Unmarshal(root.Result, &result))
			var parent guestbridge.Capability
			for _, cap := range result.Capabilities {
				if cap.Kind == "ROOT" {
					parent = cap
				}
			}
			if parent.Token == "" {
				t.Fatal("missing actual root capability")
			}
			call := h.original
			call.Claim.Number = 2
			call.Intent = guestbridge.Intent{Opcode: "CREATE", Args: mustAllocationJSON(t, map[string]any{"parent": parent, "name": "63726561746564", "mode": uint64(0660), "umask": uint64(0), "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "truncate": false, "sync": "ORDINARY"}})}
			raw, err := canonicalJSON(call.Intent)
			mustAllocation(t, err)
			call.Claim.OriginalHash = hash(raw)
			_, err = h.send("ALLOCATE", call)
			mustAllocation(t, err)
			h.f.provider.config.Failpoint = func(name string) error {
				if name == failpoint {
					return errors.New("intentional allocation fixture interruption")
				}
				return nil
			}
			_, err = h.send("CALL", call)
			mustAllocation(t, err)
			saved, _ := h.stored(call)
			if saved.Status == "TERMINAL" {
				t.Fatal("failpoint did not interrupt")
			}
			if failpoint == "effect-before-outcome" {
				lookup := command(h.f, fsapi.Lookup)
				lookup.Parent = h.f.config.Storage.Root
				lookup.Name = "created"
				request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
				mustAllocation(t, h.f.service.Dispatch(t.Context(), request, lookup))
				got, err := request.Completion().Wait(t.Context())
				mustAllocation(t, err)
				if got.Code != 0 {
					t.Fatalf("filesystem effect absent: %v", got.Code)
				}
			}
			if _, err = h.send("GAP_RESOLVE", call); err == nil {
				t.Fatal("uncertain/prepared claim became gap")
			}
			if failpoint == "prepared-before-plan-protection" {
				if _, err = h.send("CANCEL", map[string]any{"claim": call.Claim}); !errors.Is(err, fsapi.Permission) {
					t.Fatalf("CANCEL closed uncertain original: %v", err)
				}
				retained, _ := h.stored(call)
				if !equalJSON(retained, saved) || retained.Gap || retained.Status == "DECLINED" {
					t.Fatal("CANCEL rewrote protected preparation uncertainty")
				}
			}
			family := call.Claim
			retirement := map[string]any{"realm": family.Realm, "subject": family.Subject, "anchor": family.Anchor, "nonce": family.Nonce, "through": uint64(2), "result_hashes": []string{result.ID, hash([]byte("unproven-pending"))}, "credentials": call.Credentials}
			response, err := h.send("RETIRE", retirement)
			mustAllocation(t, err)
			if allocationPayload(t, response)["status"] != "BLOCKED" {
				t.Fatal("floor crossed pending/preparation hole")
			}
			// Reopen the real private ledger; uncertainty does not become absence.
			mustAllocation(t, h.f.provider.Close())
			h.f.provider = nil
			h.f.open()
			if failpoint == "prepared-before-plan-protection" {
				if _, err = h.f.provider.Cancel(t.Context(), h.config.Scopes[0], call.Claim); !errors.Is(err, fsapi.Permission) {
					t.Fatalf("restart CANCEL closed uncertain original: %v", err)
				}
				retained, _ := h.stored(call)
				if !equalJSON(retained, saved) || retained.Gap || retained.Status == "DECLINED" {
					t.Fatal("restart changed original uncertainty")
				}
			}
			if _, err = h.f.provider.ResolveGap(t.Context(), h.config.Scopes[0], call.Claim); err == nil {
				t.Fatal("restart gapped prepared/effected claim")
			}
			blocked, err := h.f.provider.Retire(t.Context(), h.config.Scopes[0], mustAllocationJSON(t, retirement))
			mustAllocation(t, err)
			var floor struct {
				Status string
				Floor  uint64
			}
			mustAllocation(t, json.Unmarshal(blocked, &floor))
			if floor.Status != "BLOCKED" || floor.Floor != 0 {
				t.Fatal("restart crossed uncertain floor")
			}

		})
	}
}

func TestControlledEnrollmentProtocolPinSurvivesReopen(t *testing.T) {
	f := newFixture(t)
	// Nil extension encoding must remain byte-identical to the prior enrollment.
	e := f.pc.Authorization.Enrollment
	raw, err := json.Marshal(e)
	mustAllocation(t, err)
	legacy, err := json.Marshal(struct{ Profile, Scope, Endpoint, ServingGeneration, Ledger, Journal, HostKey, GuestKey any }{e.Profile, e.Scope, e.Endpoint, e.ServingGeneration, e.Ledger, e.Journal, e.HostKey, e.GuestKey})
	mustAllocation(t, err)
	if !bytes.Equal(raw, legacy) {
		t.Fatal("nil protocol changed original signed enrollment bytes/order")
	}
	mustAllocation(t, f.provider.db.View(func(tx *bolt.Tx) error {
		expected, err := json.Marshal(struct {
			CapabilityLifetimes              uint32
			Scope, Profile, Ledger, Operator any
		}{2, e.Scope, e.Profile, e.Ledger, f.pc.Operator})
		if err != nil {
			return err
		}
		if !bytes.Equal(expected, tx.Bucket([]byte(ledgerVersion)).Get([]byte("scope"))) {
			t.Fatal("nil protocol changed original durable scope bytes/order")
		}
		return nil
	}))
	var fields map[string]any
	mustAllocation(t, json.Unmarshal(raw, &fields))
	if _, exists := fields["Protocol"]; exists {
		t.Fatal("nil protocol changed signed JSON")
	}
	mustAllocation(t, f.provider.Close())
	f.provider = nil
	p := guestbridge.ControlledProtocol()
	f.pc.Authorization.Enrollment.Protocol = &p
	f.enroll()
	if opened, err := New(t.Context(), f.pc); err == nil {
		_ = opened.Close()
		t.Fatal("signed successor reopened legacy private scope")
	}
	f.pc.Authorization.Enrollment.Protocol = nil
	f.enroll()
	f.open()
	mustAllocation(t, f.provider.Close())
	f.provider = nil
	h := newAllocationHarness(t)
	mustAllocation(t, h.f.provider.Close())
	h.f.provider = nil
	h.f.pc.Authorization.Enrollment.Protocol = nil
	h.f.enroll()
	if opened, err := New(t.Context(), h.f.pc); err == nil {
		_ = opened.Close()
		t.Fatal("signed legacy profile reopened successor private scope")
	}
}

func TestControlledAllocationOriginalCannotChangeAtReserve(t *testing.T) {
	h := newAllocationHarness(t)
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	changed := h.original
	changed.Credentials = alternateAllocationCredentials(h.original.Credentials)
	if _, err := h.send("ALLOCATE", changed); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("attested changed original allocation: %v", err)
	}
	raw, err := canonicalCall(changed)
	mustAllocation(t, err)
	if _, err = h.f.provider.Reserve(t.Context(), guestbridge.Binding{Context: h.config.Scopes[0], Call: changed, Canonical: raw}); err == nil {
		t.Fatal("Reserve replaced allocated full credentials")
	}
	changed = h.original
	changed.Intent = guestbridge.Intent{Opcode: "PROCESS_EXIT", Args: mustAllocationJSON(t, map[string]any{"process": changed.Credentials.Process, "exit_sequence": uint64(1)})}
	raw, err = canonicalJSON(changed.Intent)
	mustAllocation(t, err)
	changed.Claim.OriginalHash = hash(raw)
	if _, err = h.send("ALLOCATE", changed); err == nil {
		t.Fatal("exact number rebound to another valid intent")
	}
}

func TestControlledViewsRefuseMissingReceiptProducer(t *testing.T) {
	h := newAllocationHarness(t)
	// A structurally valid successor view command must refuse before asking the
	// primitive controller boundary to dispatch. No source/event producer exists.
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/views.json")
	mustAllocation(t, err)
	var corpus struct {
		Commands []struct{ Body guestbridge.Command }
	}
	mustAllocation(t, json.Unmarshal(raw, &corpus))
	found := false
	for _, v := range corpus.Commands {
		if v.Body.Op == "VIEW_RETIRE" {
			c := v.Body
			c.Protocol = h.config.Protocol
			c.Context = h.config.Scopes[0]
			// Retire payload contains credentials/family but no scoped capability.
			if _, err = h.bridge.Execute(t.Context(), h.native, c); !errors.Is(err, fsapi.Unsupported) {
				t.Fatalf("missing producer returned %v", err)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing literal view retirement")
	}
	if *h.config.ViewProtocol.(allocationViews).calls != 0 {
		t.Fatal("missing producer reached controller mutation")
	}
}

func TestControlledRealTransportAllocationRetry(t *testing.T) {
	h := newAllocationHarness(t)
	var config admission.Config
	for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: 12 << 20} {
		r := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
		config.Pools[class] = admission.Limits{Global: r, PerPeer: r, PerVolume: r}
	}
	budget, err := admission.New(config)
	mustAllocation(t, err)
	transport, err := guestbridge.NewTransport(h.bridge, budget)
	mustAllocation(t, err)
	defer transport.Close()
	h.cell = 0
	exchange := func(c guestbridge.Command, flags uint16) []byte {
		t.Helper()
		raw, err := canonicalJSON(c)
		mustAllocation(t, err)
		session, err := fsapi.ParseUUID(c.Context.Session)
		mustAllocation(t, err)
		frame := guestbridge.Frame{Minor: 2, Kind: 1, Flags: flags, Total: uint32(len(raw)), Session: session, Cell: c.Cell, Hash: sha256.Sum256(raw), Data: raw}
		wire, err := guestbridge.EncodeFrame(frame)
		mustAllocation(t, err)
		frame.Minor = 1
		wrong, err := guestbridge.EncodeFrame(frame)
		mustAllocation(t, err)
		if _, err = transport.Write(t.Context(), h.native, h.native.Endpoint.CommandNode, c.Cell*guestbridge.CellStride, wrong); !errors.Is(err, fsapi.Unsupported) {
			t.Fatalf("mixed frame admitted: %v", err)
		}
		_, err = transport.Write(t.Context(), h.native, h.native.Endpoint.CommandNode, c.Cell*guestbridge.CellStride, wire)
		mustAllocation(t, err)
		deadline := time.Now().Add(5 * time.Second)
		for {
			reply, err := transport.Read(t.Context(), h.native, h.native.Endpoint.ResultNode, c.Cell*guestbridge.CellStride, guestbridge.ChunkBytes)
			if errors.Is(err, fsapi.WouldBlock) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
				continue
			}
			mustAllocation(t, err)
			decoded, err := guestbridge.DecodeFrame(reply)
			mustAllocation(t, err)
			if decoded.Minor != 2 || int(decoded.Total) != len(decoded.Data) {
				t.Fatal("wrong response version/body")
			}
			var envelope map[string]any
			mustAllocation(t, json.Unmarshal(decoded.Data, &envelope))
			verification, exists := envelope["verification"]
			if !exists || verification != nil {
				t.Fatal("foundation minted verification authority")
			}
			return decoded.Data
		}
	}
	_ = exchange(h.command("HELLO", guestbridge.ControlledProtocol()), 0)
	first := exchange(h.command("ALLOCATE", h.original), 0)
	second := exchange(h.command("ALLOCATE", h.original), 0)
	a, _ := canonicalJSON(allocationPayload(t, first))
	b, _ := canonicalJSON(allocationPayload(t, second))
	if !bytes.Equal(a, b) {
		t.Fatal("framed allocation retry changed identity")
	}
	_ = exchange(h.command("GAP_RESOLVE", h.original), 1)
}

func TestControlledAllocationCreditFailureDoesNotIssueNumber(t *testing.T) {
	h := newAllocationHarness(t)
	budget := h.f.provider.config.MaxRetainedBytes
	h.f.provider.config.MaxRetainedBytes = h.f.provider.config.ClaimCreditBytes - 1
	if _, err := h.send("ALLOCATE", h.original); !errors.Is(err, fsapi.NoSpace) {
		t.Fatalf("credit refusal: %v", err)
	}
	if _, exists := h.stored(h.original); exists {
		t.Fatal("failed protection issued number")
	}
	h.f.provider.config.MaxRetainedBytes = budget
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	saved, _ := h.stored(h.original)
	if saved.RetainedCredit != h.f.provider.config.ClaimCreditBytes || saved.SourceCredit != h.f.provider.config.SourceCreditBytes {
		t.Fatal("allocation lost reserved credits")
	}
	raw, err := canonicalCall(h.original)
	mustAllocation(t, err)
	state, err := h.f.provider.Reserve(t.Context(), guestbridge.Binding{Context: h.config.Scopes[0], Call: h.original, Canonical: raw})
	mustAllocation(t, err)
	// Exact service-issued operation/allocation IDs remain valid on local retry;
	// comparing the original request must not treat those IDs as caller changes.
	_, err = h.f.provider.Reserve(t.Context(), state.Binding)
	mustAllocation(t, err)
}

func TestControlledAllocationRechecksLiveEnrollment(t *testing.T) {
	h := newAllocationHarness(t)
	original := h.f.provider.config.Facts
	h.f.provider.config.Facts = facts{}
	if _, err := h.send("ALLOCATE", h.original); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("stale live enrollment: %v", err)
	}
	if _, exists := h.stored(h.original); exists {
		t.Fatal("stale enrollment allocated")
	}
	h.f.provider.config.Facts = original
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
}

func TestControlledAllocationCannotAdoptDirtyEarmark(t *testing.T) {
	h := newAllocationHarness(t)
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	_, err = h.send("CALL", h.original)
	mustAllocation(t, err)
	root, _ := h.stored(h.original)
	var rooted wireResult
	mustAllocation(t, json.Unmarshal(root.Result, &rooted))
	call := h.original
	call.Claim.Number = 2
	call.Intent = guestbridge.Intent{Opcode: "CREATE", Args: mustAllocationJSON(t, map[string]any{"parent": rooted.Capabilities[0], "name": "6469727479", "mode": uint64(0660), "umask": uint64(0), "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "truncate": false, "sync": "ORDINARY"}})}
	raw, err := canonicalJSON(call.Intent)
	mustAllocation(t, err)
	call.Claim.OriginalHash = hash(raw)
	_, err = h.send("ALLOCATE", call)
	mustAllocation(t, err)
	_, err = h.send("CALL", call)
	mustAllocation(t, err)
	created, _ := h.stored(call)
	var result wireResult
	mustAllocation(t, json.Unmarshal(created.Result, &result))
	var description guestbridge.Capability
	for _, cap := range result.Capabilities {
		if cap.Kind == "DESCRIPTION" {
			description = cap
		}
	}
	if description.Token == "" {
		t.Fatal("missing actual description")
	}
	n := guestbridge.ClaimNumber{Realm: call.Claim.Realm, Subject: call.Claim.Subject, Anchor: call.Claim.Anchor, Nonce: call.Claim.Nonce, Number: 3}
	earmark := Earmark{Scope: h.config.Scopes[0], Number: n, Credentials: call.Credentials, Description: description, Reservation: hash([]byte("owned-dirty-reservation")), Length: 4096}
	mustAllocation(t, h.f.provider.EarmarkNumber(t.Context(), earmark))
	ordinary := h.original
	ordinary.Claim.Number = 3
	if _, err = h.send("ALLOCATE", ordinary); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("ordinary allocation adopted dirty number: %v", err)
	}
	if _, err = h.send("GAP_RESOLVE", ordinary); err == nil {
		t.Fatal("ordinary gap closed dirty earmark")
	}
	mustAllocation(t, h.f.provider.db.View(func(tx *bolt.Tx) error {
		var retained Earmark
		raw := h.f.provider.bucket(tx, "earmarks").Get(claimKey(ordinary.Claim))
		if err := json.Unmarshal(raw, &retained); err != nil {
			return err
		}
		if retained.Bound != nil || retained.Closed || retained.GapProof != "" || retained.Number != n {
			t.Fatal("ordinary control changed typed unbound earmark")
		}
		return nil
	}))
}

func TestControlledExitedIssuerCanCloseOnlyExactOriginalNumbers(t *testing.T) {
	h := newAllocationHarness(t)
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	_, err = h.send("CALL", h.original)
	mustAllocation(t, err)
	root, _ := h.stored(h.original)
	var rooted wireResult
	mustAllocation(t, json.Unmarshal(root.Result, &rooted))
	unsent := h.original
	unsent.Claim.Number = 2
	_, err = h.send("ALLOCATE", unsent)
	mustAllocation(t, err)
	exit := h.original
	exit.Claim.Number = 3
	exit.Intent = guestbridge.Intent{Opcode: "PROCESS_EXIT", Args: mustAllocationJSON(t, map[string]any{"process": exit.Credentials.Process, "exit_sequence": uint64(1)})}
	raw, err := canonicalJSON(exit.Intent)
	mustAllocation(t, err)
	exit.Claim.OriginalHash = hash(raw)
	_, err = h.send("ALLOCATE", exit)
	mustAllocation(t, err)
	_, err = h.send("CALL", exit)
	mustAllocation(t, err)
	exited, _ := h.stored(exit)
	var exitedResult wireResult
	mustAllocation(t, json.Unmarshal(exited.Result, &exitedResult))
	if exited.Status != "TERMINAL" || exitedResult.Code != "OK" {
		t.Fatal("actual PROCESS_EXIT did not complete")
	}
	response, err := h.send("CALL", unsent)
	mustAllocation(t, err)
	if allocationPayload(t, response)["status"] != "REJECTED" {
		t.Fatal("exited issuer executed original unsent CALL")
	}
	fresh := h.original
	fresh.Claim.Number = 4
	if _, err = h.send("ALLOCATE", fresh); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("exited issuer allocated fresh claim: %v", err)
	}
	changed := unsent
	changed.Credentials.Revision++
	if _, err = h.send("GAP_RESOLVE", changed); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("new attested revision closed exited original: %v", err)
	}
	response, err = h.send("GAP_RESOLVE", unsent)
	mustAllocation(t, err)
	proof := allocationPayload(t, response)["proof_hash"].(string)
	c := unsent.Claim
	retire := map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "through": uint64(3), "result_hashes": []string{rooted.ID, proof, exitedResult.ID}, "credentials": changed.Credentials}
	if _, err = h.send("RETIRE", retire); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("new revision retired exited originals: %v", err)
	}
	retire["credentials"] = h.original.Credentials
	response, err = h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, response)["status"] != "RETIRED" {
		t.Fatal("exited original numbers stranded")
	}
	if _, err = h.send("ALLOCATE", fresh); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("management revived exited issuer: %v", err)
	}
}
