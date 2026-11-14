package viewcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TestZQ3LeaseCyclesRecycleViewIdentities is the q64/f84 workload: at the
// committed zq3 spec's exact Views bounds (Identities 64, so 64 ticket
// identities, 128 UUIDs and a 512-row ACK archive) it drives thousands of
// DOMAIN READ acquire/release cycles, each with a scoped recall, plus dirty
// rounds whose all-domain recall freezes and drains a reservation per
// domain, then reopens the controller and keeps cycling against the
// reopened ledger. Before q64 the 33rd cycle's acquisition was refused
// (every ticket kept its identity), and before f84 the ACK archive refused
// about the 100th recall. Every formerly lifetime record must now peak at
// no more than half its bound.
func TestZQ3LeaseCyclesRecycleViewIdentities(t *testing.T) {
	cycles, dirtyEvery, afterReopen := 1000, 50, 100
	if testing.Short() {
		cycles, afterReopen = 110, 10
	}
	// HFS_Q64_CYCLES runs a longer workload by hand (each cycle re-reads
	// the journal several times through the real stack, about 0.1 s).
	if n, err := strconv.Atoi(os.Getenv("HFS_Q64_CYCLES")); err == nil && n > 0 {
		cycles = n
	}
	bounds := zq3SpecBounds(t)
	r := newZQ3Rig(t, 0)
	x := newRecycleRun(r)
	for n := range cycles {
		if n%dirtyEvery == dirtyEvery-1 {
			x.dirtyRound()
			continue
		}
		x.readCycle(n % 2)
	}
	before := r.controller.state.Identities
	r.reopen()
	x.direct = true
	if r.controller.state.Identities != before {
		t.Fatalf("reopen changed identities %d -> %d", before, r.controller.state.Identities)
	}
	for n := range afterReopen {
		x.readCycle(n % 2)
	}
	admitted := x.admitted()
	p := x.peaks
	t.Logf("cycles %d, recalls %d, view tickets admitted %d; peaks: identities %d, ticket rows %d, UUIDs %d, ACK archive %d, revisions %d, lineage fences %d, receipts %d, leases %d, sources %d, ledger %d B",
		x.cycles, x.recalls, admitted, p.identities, p.tickets, p.uuids, p.archive, p.revisions, p.lineages, p.retirements, p.leases, p.sources, p.ledgerBytes)
	// The run must exceed the old lifetime walls: Identities tickets, and
	// the recalls whose (at least five) ACKs fill the 8*Identities archive.
	if admitted < uint64(3*bounds.Identities) || x.recalls <= 8*bounds.Identities/5 {
		t.Fatalf("run too small to prove recycling: %d tickets, %d recalls", admitted, x.recalls)
	}
	x.requireMargins(t, bounds)
}

// TestRecycledTicketReplaysAreRefused retransmits every view op of a cycle
// whose tickets were retired and recycled. Each must be refused, never
// admitted as a new ticket nor answered for the newer holder of the
// domain's next lease, which must stay untouched. The replays repeat after
// a reopen of the recycled ledger.
func TestRecycledTicketReplaysAreRefused(t *testing.T) {
	r := newZQ3Rig(t, 0)
	x := newRecycleRun(r)
	old := x.readCycle(0)
	newer := r.domainRead(0)
	x.requireReplaysRefused(old)
	if !r.leaseActive(newer) {
		t.Fatal("replayed old release touched the newer lease")
	}
	r.reopen()
	x.direct = true
	x.requireReplaysRefused(old)
	if !r.leaseActive(newer) {
		t.Fatal("replay after reopen touched the newer lease")
	}
}

// requireReplaysRefused sends a recycled cycle's ops again: the exact
// admissions, QUERY and CANCEL of both tickets and a changed admission
// under the old number are Stale, and CheckResponse vouches for none of
// the old replies.
func (x *recycleRun) requireReplaysRefused(old readRecord) {
	r := x.r
	r.t.Helper()
	// A different op under the recycled acquisition number.
	changed := map[string]any{"request": old.acquire["request"], "lease": old.release["lease"]}
	for _, c := range []struct {
		op      string
		payload any
	}{
		{"VIEW_ACQUIRE", old.acquire},
		{"VIEW_RELEASE", old.release},
		{"VIEW_QUERY", map[string]any{"request": old.acquire["request"]}},
		{"VIEW_QUERY", map[string]any{"request": old.release["request"]}},
		{"VIEW_CANCEL", map[string]any{"request": old.acquire["request"]}},
		{"VIEW_CANCEL", map[string]any{"request": old.release["request"]}},
		{"VIEW_RENEW", changed},
	} {
		raw, err := canonical(c.payload)
		must(r.t, err)
		if reply, err := r.controller.ExchangeView(r.t.Context(), r.scopes[0], c.op, raw); !errors.Is(err, fsapi.Stale) {
			r.t.Fatalf("%s of a recycled ticket: %s %v, want Stale", c.op, reply, err)
		}
	}
	raw, err := canonical(old.acquire)
	must(r.t, err)
	if err = r.controller.CheckResponse(r.t.Context(), r.scopes[0], "VIEW_ACQUIRE", raw, old.granted); !errors.Is(err, fsapi.Permission) {
		r.t.Fatalf("CheckResponse vouched for a recycled ticket: %v", err)
	}
}

// TestRecycledACKReplays checks the ACK archive rule: once a session ACKed a
// later recall, an ACK of its earlier recall is answered STALE (it can never
// match the session's grown event sequence), while the exact latest ACK is
// still ACCEPTED, also after a reopen.
func TestRecycledACKReplays(t *testing.T) {
	r := newZQ3Rig(t, 0)
	x := newRecycleRun(r)
	first := x.readCycle(0).reopened
	latest := x.readCycle(0).reopened
	x.requireACK(0, first, "STALE")
	x.requireACK(0, latest, "ACCEPTED")
	r.controller.mu.Lock()
	archived := len(r.controller.state.AckArchive)
	r.controller.mu.Unlock()
	if archived != 5 {
		t.Fatalf("ACK archive keeps %d entries, want the 5 of the session's last recall", archived)
	}
	r.reopen()
	x.requireACK(0, first, "STALE")
	x.requireACK(0, latest, "ACCEPTED")
}

// requireACK resends an exact ACK straight to the controller (the bridge
// turns a STALE reply into a refused cell) and requires reply status want.
func (x *recycleRun) requireACK(i int, ack json.RawMessage, want string) {
	x.r.t.Helper()
	if reply := x.r.directExchange(i, "VIEW_ACK", ack); status(reply) != want {
		x.r.t.Fatalf("ACK replay: %s, want %s", reply, want)
	}
}

// TestRecycledFamiliesStillSeal checks terminalSnapshot after recycling: a
// session whose families are fully retired has no ticket rows left, yet its
// families keep their recorded owner, so the snapshot includes them instead
// of refusing them as unowned (Permission).
func TestRecycledFamiliesStillSeal(t *testing.T) {
	r := newZQ3Rig(t, 0)
	x := newRecycleRun(r)
	x.readCycle(0)
	x.readCycle(1)
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	for i, scope := range r.scopes {
		raw, err := r.controller.terminalSnapshot(scope)
		must(t, err)
		var snapshot terminalSnapshot
		must(t, json.Unmarshal(raw, &snapshot))
		if len(snapshot.Tickets) != 0 || len(snapshot.Families) != 2 {
			t.Fatalf("m%d snapshot: %d tickets, %d families", i, len(snapshot.Tickets), len(snapshot.Families))
		}
		for _, f := range snapshot.Families {
			if f.Scope == nil || *f.Scope != scope || f.High != f.Floor {
				t.Fatalf("m%d snapshot family %+v", i, f)
			}
		}
	}
}

// TestLegacyLedgerOpensAndRecycles reopens a schema-2 ledger in the layout
// before q64: retired tickets kept as tombstones that still charge
// Identities, families without owner or receipt, and lineage revisions
// without lineage fences. It must open, still answer a tombstone with
// RETIRED_ID, and the next VIEW_RETIRE must recycle the tombstones.
func TestLegacyLedgerOpensAndRecycles(t *testing.T) {
	r := newZQ3Rig(t, 0)
	x := newRecycleRun(r)
	rec := x.readUnretired(0)
	r.controller.mu.Lock()
	rows, revisions := map[string]ticketState{}, map[string]uint64{}
	for key, ticket := range r.controller.state.Tickets {
		rows[key] = ticket
	}
	for key, revision := range r.controller.state.Revisions {
		revisions[key] = revision
	}
	r.controller.mu.Unlock()
	x.retireAll(0)
	r.controller.mu.Lock()
	r.controller.makeLegacy(rows, revisions)
	legacy, err := r.controller.rawState()
	must(t, err)
	must(t, r.controller.save())
	r.controller.mu.Unlock()
	if bytes.Contains(legacy, []byte("LineageFences")) || bytes.Contains(legacy, []byte("Receipt")) {
		t.Fatal("legacy ledger still carries q64 fields")
	}
	r.reopen()
	x.direct = true
	if r.controller.state.Identities != 2 || len(r.controller.state.Tickets) != 2 {
		t.Fatalf("legacy reopen: %d identities, %d rows", r.controller.state.Identities, len(r.controller.state.Tickets))
	}
	if reply := x.view(0, "VIEW_QUERY", map[string]any{"request": rec.acquire["request"]}); status(reply) != "RETIRED_ID" {
		t.Fatalf("legacy tombstone: %s", reply)
	}
	legacyRevisions := len(r.controller.state.Revisions)
	if legacyRevisions == 0 {
		t.Fatal("legacy ledger has no lineage revision to keep")
	}
	x.readCycle(0)
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if len(r.controller.state.Tickets) != 0 || r.controller.state.Identities != 0 {
		t.Fatalf("legacy tombstones not recycled: %d rows, %d identities", len(r.controller.state.Tickets), r.controller.state.Identities)
	}
	for _, f := range r.controller.state.Families {
		if f.Scope == nil || f.Receipt == "" {
			t.Fatalf("legacy family not adopted: %+v", f)
		}
	}
	// A legacy lineage has no recorded fence and keeps its revision.
	if len(r.controller.state.Revisions) != legacyRevisions {
		t.Fatalf("revisions %d, want the %d legacy ones kept", len(r.controller.state.Revisions), legacyRevisions)
	}
}

// makeLegacy rewrites the live ledger into the pre-q64 layout: the given
// retired rows come back as tombstones (bodies stripped as the old compact
// did) that charge Identities, the lineage revisions come back without
// lineage fences, and the q64 fields are cleared. c.mu is held.
func (c *Controller) makeLegacy(rows map[string]ticketState, revisions map[string]uint64) {
	for key, t := range rows {
		t.Payload, t.Initial, t.Current, t.Cancelled, t.DeclinedGap, t.GapClosureReply = nil, nil, nil, nil, nil, nil
		c.state.Tickets[key] = t
		c.state.Identities++
	}
	for key, f := range c.state.Families {
		f.Scope, f.Receipt = nil, ""
		c.state.Families[key] = f
	}
	c.state.LineageFences = map[string]lineageFence{}
	for key, revision := range revisions {
		c.state.Revisions[key] = revision
	}
}
