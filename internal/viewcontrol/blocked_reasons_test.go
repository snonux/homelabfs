package viewcontrol

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// BLOCKED reasons (task 2h4, blocked_reasons.go): OBSERVE's recall history
// says why a ticket was BLOCKED, an expected-target mismatch after its recall
// or a capacity bound, without decoding the ledger.

// requireOneBlocked requires exactly one recorded BLOCKED decision in c's
// history: ticket of domain i, opcode VIEW_ACQUIRE, with reason and bound.
func (r *rig) requireOneBlocked(t *testing.T, i int, ticket guestbridge.Ticket, reason, bound string) {
	t.Helper()
	b := history(t, r.controller).Blocked
	if b == nil || len(b.Recent) != 1 || b.Omitted != 0 {
		t.Fatalf("blocked history %+v, want one decision", b)
	}
	key, _ := ticketKeys(r.scopes[i], ticket)
	row := b.Recent[0]
	if row.Ticket != key || row.Domain != r.scopes[i].Session || row.Opcode != "VIEW_ACQUIRE" || row.Reason != reason || row.Bound != bound {
		t.Fatalf("blocked row %+v, want ticket %s domain %s reason %s bound %q", row, key, r.scopes[i].Session, reason, bound)
	}
	if _, err := time.Parse(time.RFC3339Nano, row.AtUTC); err != nil || !strings.HasSuffix(row.AtUTC, "Z") {
		t.Fatalf("blocked row time %q: %v", row.AtUTC, err)
	}
	mismatch, capacity := uint64(0), uint64(0)
	if reason == BlockedExpectedMismatch {
		mismatch = 1
	} else {
		capacity = 1
	}
	if b.ExpectedMismatch != mismatch || b.Capacity != capacity {
		t.Fatalf("blocked counters %+v", b)
	}
}

// TestBlockedReasonExpectedMismatch: m1's stale observation acquire starts
// its scoped recall (PENDING) and its retry after the recall is BLOCKED; the
// history records it as an expected-target mismatch. A replay of the saved
// BLOCKED reply is no new decision and is not recorded again.
func TestBlockedReasonExpectedMismatch(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "moves-cut", false)
	installed := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.dropLifetimes(0, path, description)
	ticket := r.staleAcquire(1, installed.Target)
	if b := history(t, r.controller).Blocked; b != nil {
		t.Fatalf("PENDING recorded as BLOCKED: %+v", b)
	}
	r.finishMismatch(t, 1, ticket)
	r.requireOneBlocked(t, 1, ticket, BlockedExpectedMismatch, "")
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": ticket}); reply.Status != "BLOCKED" {
		t.Fatalf("replayed stale ticket: %+v", reply)
	}
	r.requireOneBlocked(t, 1, ticket, BlockedExpectedMismatch, "")
}

// TestBlockedReasonCapacity: with Revisions at Bounds.Identities a new
// lineage is BLOCKED, recorded as a capacity BLOCKED of bound LINEAGES; the
// shared grant of an existing lineage before it records nothing.
func TestBlockedReasonCapacity(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "page", false)
	held := r.domainRead(0)
	r.fillRevisions(r.revisionsRoom())
	if shared := r.domainRead(0); shared.ID != held.ID {
		t.Fatalf("existing lineage not shared at the bound: %s %s", shared.ID, held.ID)
	}
	ticket, reply := r.acquire(1, page(description, 0), "READ")
	if reply.Status != "BLOCKED" {
		t.Fatalf("new lineage at the Revisions bound: %+v", reply)
	}
	r.requireOneBlocked(t, 1, ticket, BlockedCapacity, BoundLineages)
}

// TestGrantBoundNamesEachBound: grantBound names the bound that does not fit
// (and grantFits agrees), in grantFits' check order.
func TestGrantBoundNamesEachBound(t *testing.T) {
	r := newZQ3Rig(t, 0)
	c := r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	lease := guestbridge.ViewLease{Scope: r.scopes[0], Access: guestbridge.ViewAccess{Kind: "DOMAIN"}, Mode: "READ"}
	if got := c.grantBound(lease); got != "" || !c.grantFits(lease) {
		t.Fatalf("empty ledger bound %q", got)
	}
	c.state.Revisions[lineageID(lease)] = ^uint64(0)
	if got := c.grantBound(lease); got != BoundRevisions || c.grantFits(lease) {
		t.Fatalf("exhausted revision bound %q", got)
	}
	delete(c.state.Revisions, lineageID(lease))
	identities := c.config.Authorization.Enrollment.Bounds.Identities
	for k := range identities {
		c.state.Leases[fmt.Sprintf("filler/%d", k)] = leaseState{}
	}
	if got := c.grantBound(lease); got != BoundLeases {
		t.Fatalf("full lease table bound %q", got)
	}
	c.state.Identities = identities
	if got := c.grantBound(lease); got != BoundControl && got != BoundIdentity {
		t.Fatalf("spent credits bound %q", got)
	}
}

// TestBlockedHistoryBoundAndEvidence: the ring keeps the newest
// BlockedHistoryLimit decisions and counts the rest, a full ring next to a
// full recall ring still fits RecallEvidenceBytes, and OBSERVE's shape is
// the pre-2h4 one until the first BLOCKED, then carries "blocked".
func TestBlockedHistoryBoundAndEvidence(t *testing.T) {
	var h recallHistory
	start := time.Now()
	for n := range RecallHistoryLimit {
		id := fakeRecall(&h, n, start)
		for a := range RecallAckLimit {
			h.acked(id, RecallAck{Domain: strings.Repeat("d", 36), Phase: "STOPPED", Sequence: uint64(a + 1)}, start)
		}
	}
	for n := range BlockedHistoryLimit + 4 {
		reason, bound := BlockedCapacity, BoundSources
		if n%2 == 0 {
			reason, bound = BlockedExpectedMismatch, ""
		}
		h.blocked.add(BlockedReply{Ticket: strings.Repeat("f", 64) + fmt.Sprintf("/%d", n), Domain: strings.Repeat("s", 36), Opcode: "VIEW_ACQUIRE", Reason: reason, Bound: bound, AtUTC: start.UTC().Format(time.RFC3339Nano)})
	}
	snap := h.snapshot()
	b := snap.Blocked
	if b == nil || len(b.Recent) != BlockedHistoryLimit || b.Omitted != 4 || b.ExpectedMismatch != 10 || b.Capacity != 10 || !strings.HasSuffix(b.Recent[0].Ticket, "/4") {
		t.Fatalf("blocked ring %+v", b)
	}
	raw, err := json.Marshal(snap)
	must(t, err)
	if len(raw) > RecallEvidenceBytes || snap.Omitted == 0 || len(snap.Recalls) == 0 {
		t.Fatalf("evidence %d B, %d records, %d omitted", len(raw), len(snap.Recalls), snap.Omitted)
	}
	var shape map[string]json.RawMessage
	must(t, json.Unmarshal(raw, &shape))
	var blocked map[string]json.RawMessage
	must(t, json.Unmarshal(shape["blocked"], &blocked))
	for _, key := range []string{"expected_mismatch", "capacity", "omitted", "recent"} {
		if _, ok := blocked[key]; !ok {
			t.Fatalf("blocked evidence lacks %q: %s", key, shape["blocked"])
		}
	}
	snap.Blocked.Recent[0].Ticket = "changed"
	if h.blocked.recent[0].Ticket == "changed" {
		t.Fatal("snapshot shares BLOCKED rows with the ring")
	}
}

// TestFitBlockedTrimsOversizedRows: should the rows ever outgrow the bound,
// fitBlocked drops the oldest ones, counting them in Omitted, until the
// wrapper fits.
func TestFitBlockedTrimsOversizedRows(t *testing.T) {
	var h recallHistory
	for n := range BlockedHistoryLimit {
		h.blocked.add(BlockedReply{Ticket: fmt.Sprintf("%d", n), Domain: strings.Repeat("s", RecallEvidenceBytes/8), Reason: BlockedCapacity})
	}
	snap := h.snapshot()
	raw, err := json.Marshal(snap)
	must(t, err)
	if len(raw) > RecallEvidenceBytes || snap.Blocked.Omitted == 0 || snap.Blocked.Omitted+uint64(len(snap.Blocked.Recent)) != BlockedHistoryLimit {
		t.Fatalf("oversized rows: %d B, %d kept, %d omitted", len(raw), len(snap.Blocked.Recent), snap.Blocked.Omitted)
	}
	if last := snap.Blocked.Recent[len(snap.Blocked.Recent)-1]; last.Ticket != fmt.Sprintf("%d", BlockedHistoryLimit-1) {
		t.Fatalf("newest row lost: %+v", last)
	}
	var round RecallHistory
	must(t, json.Unmarshal(raw, &round))
	if !reflect.DeepEqual(round.Blocked, snap.Blocked) {
		t.Fatalf("round trip %+v", round.Blocked)
	}
}
