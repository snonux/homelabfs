package controlledvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// retireVerifier is the controlled runtime producer's FLOOR step in test
// form: after every RETIRED reply it re-verifies the exact protected receipt
// with the actual verified request context (controlledruntime/producer.go).
type retireVerifier struct {
	p        *Provider
	verified int
}

func (v *retireVerifier) Bind(context.Context, guestbridge.VerificationBinding) error { return nil }
func (v *retireVerifier) Prepare(context.Context, guestbridge.Command) error          { return nil }
func (v *retireVerifier) Produce(ctx context.Context, c guestbridge.Command, base json.RawMessage) (*guestbridge.VerificationReceipt, error) {
	var response struct{ Payload json.RawMessage }
	if c.Op != "RETIRE" || json.Unmarshal(base, &response) != nil {
		return nil, nil
	}
	var reply struct{ Status string }
	if json.Unmarshal(response.Payload, &reply) != nil || reply.Status != "RETIRED" {
		return nil, nil
	}
	if err := v.p.VerifyRetirementReceipt(ctx, c.Context, c.Payload, response.Payload); err != nil {
		return nil, err
	}
	v.verified++
	return nil, nil
}

// withRetireVerifier reopens the provider and rebuilds the harness bridge with
// the verifier installed (a bridge HELLOs once per provider lifetime).
func withRetireVerifier(t *testing.T, h *allocationHarness) *retireVerifier {
	t.Helper()
	mustAllocation(t, h.f.provider.Close())
	h.f.provider = nil
	h.f.open()
	v := &retireVerifier{p: h.f.provider}
	h.config.Claims, h.config.Execution = h.f.provider, h.f.provider
	h.config.Native = &nativeFacts{native: h.native}
	h.config.Verification = v
	var err error
	h.bridge, err = guestbridge.New(t.Context(), h.config)
	mustAllocation(t, err)
	_, err = h.send("HELLO", *h.config.Protocol)
	mustAllocation(t, err)
	return v
}

func resultID(t *testing.T, h *allocationHarness, call guestbridge.Call) string {
	t.Helper()
	e, ok := h.stored(call)
	var result struct{ ID string }
	if !ok || json.Unmarshal(e.Result, &result) != nil {
		t.Fatalf("claim %d has no terminal result", call.Claim.Number)
	}
	return result.ID
}

// TestRetireRecyclesRetiredClaims (o64): a retirement returns the retired
// claims' credits and deletes their entries, except the snapshot owner, and
// every replay path of a recycled number keeps its old answer: QUERY state
// RETIRED_ID, Cancel RETIRED_ID, the exact RETIRE receipt (re-verified by
// the producer step), the READDIR_CLEANUP receipt; ResolveGap refuses.
func TestRetireRecyclesRetiredClaims(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	verifier := withRetireVerifier(t, h)
	seedCleanupDirectory(h, 2)
	root := cleanupRoot(h)
	page := cleanupCall(h, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 256})
	request := cleanupRequest(t, func() entry { e, _ := h.stored(page); return e }())
	first, err := h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	// A later claim takes over the bridge snapshot, so the READDIR is not kept.
	cleanupCall(h, "GETATTR", map[string]any{"object": root})
	assertCleanupCredits(t, h.f.provider, 3)
	rootCall := h.original
	rootCall.Claim.Number = 1
	retire := map[string]any{"realm": page.Claim.Realm, "subject": page.Claim.Subject, "anchor": page.Claim.Anchor, "nonce": page.Claim.Nonce, "through": page.Claim.Number, "result_hashes": []string{resultID(t, h, rootCall), resultID(t, h, page)}, "credentials": page.Credentials}
	retired, err := h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, retired)["status"] != "RETIRED" {
		t.Fatalf("retirement refused: %s", retired)
	}
	assertCleanupCredits(t, h.f.provider, 1)
	if _, ok := h.stored(page); ok {
		t.Fatal("retired READDIR entry was not recycled")
	}
	assertRecycledReplays(t, h, page, request, first, retire, retired)
	if verifier.verified != 2 {
		t.Fatalf("producer verified %d RETIRED receipts, want both", verifier.verified)
	}
}

func assertRecycledReplays(t *testing.T, h *allocationHarness, page guestbridge.Call, request guestbridge.ReaddirCleanup, cleanup json.RawMessage, retire map[string]any, retired json.RawMessage) {
	t.Helper()
	scope := h.config.Scopes[0]
	state, err := h.f.provider.Resolve(t.Context(), scope, page.Claim)
	mustAllocation(t, err)
	if state.Status != "RETIRED_ID" {
		t.Fatalf("recycled claim resolves %q, want RETIRED_ID", state.Status)
	}
	state, err = h.f.provider.Cancel(t.Context(), scope, page.Claim)
	if err != nil || state.Status != "RETIRED_ID" {
		t.Fatalf("cancel of a recycled claim: %q %v", state.Status, err)
	}
	if _, err = h.f.provider.ResolveGap(t.Context(), scope, page.Claim); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("gap resolution of a retired claim: %v", err)
	}
	again, err := h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	if !equalJSON(allocationPayload(t, again), allocationPayload(t, cleanup)) {
		t.Fatal("recycled READDIR cleanup replay changed its receipt")
	}
	replayed, err := h.send("RETIRE", retire)
	mustAllocation(t, err)
	if !equalJSON(allocationPayload(t, replayed), allocationPayload(t, retired)) {
		t.Fatal("retirement replay changed its receipt")
	}
}

// TestRecycledEarmarkReturnsItsOwnCredits: an unbound earmark returns the
// credits it reserved, a bound one returns them once through its entry.
func TestRecycledEarmarkReturnsItsOwnCredits(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	p := h.f.provider
	key := claimKey(h.original.Claim)
	mustAllocation(t, p.db.Update(func(tx *bolt.Tx) error {
		if err := p.reserveCredit(tx, "retained-credit", p.config.ClaimCreditBytes, p.config.MaxRetainedBytes); err != nil {
			return err
		}
		if err := p.reserveCredit(tx, "source-credit", p.config.SourceCreditBytes, p.config.MaxSourceBytes); err != nil {
			return err
		}
		if err := put(p.bucket(tx, earmarkCreditBucket), key, earmarkCredit{p.config.ClaimCreditBytes, p.config.SourceCreditBytes}); err != nil {
			return err
		}
		return put(p.bucket(tx, "earmarks"), key, Earmark{Closed: true})
	}))
	assertCleanupCredits(t, p, 1)
	mustAllocation(t, p.db.Update(func(tx *bolt.Tx) error { return p.recycleNumber(tx, key) }))
	assertCleanupCredits(t, p, 0)
	mustAllocation(t, p.db.View(func(tx *bolt.Tx) error {
		if p.bucket(tx, "earmarks").Get(key) != nil || p.bucket(tx, earmarkCreditBucket).Get(key) != nil {
			t.Fatal("recycled earmark left state behind")
		}
		return nil
	}))
	// A second recycle of the same number returns nothing.
	mustAllocation(t, p.db.Update(func(tx *bolt.Tx) error { return p.recycleNumber(tx, key) }))
	assertCleanupCredits(t, p, 0)
}
