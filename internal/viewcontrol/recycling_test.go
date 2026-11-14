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
// identities, 128 UUIDs and a 512-row ACK archive) it drives hundreds of
// DOMAIN READ acquire/release cycles, each with a scoped recall, plus dirty
// rounds whose all-domain recall freezes and drains a reservation per
// domain, then reopens the controller and keeps cycling against the
// reopened ledger. Before q64 the 33rd cycle's acquisition was refused
// (every ticket kept its identity), and before f84 the ACK archive refused
// about the 100th recall. Every formerly lifetime record must now peak at
// no more than half its bound.
//
// Measured with HFS_Q64_CYCLES=1000 (2026-11-14): 1100 cycles and recalls,
// 2320 view tickets, peaks identities 8, ticket rows 8, UUIDs 5, ACK
// archive 10, revisions 2, receipts 4, ledger 184 KB; the default 350
// cycles reach the same peaks with a 169 KB ledger.
func TestZQ3LeaseCyclesRecycleViewIdentities(t *testing.T) {
	cycles, dirtyEvery, afterReopen := 300, 50, 50
	if testing.Short() {
		cycles, afterReopen = 110, 10
	}
	// HFS_Q64_CYCLES runs a longer workload by hand: each cycle re-reads
	// the journal several times through the real stack (0.1 s and more, so
	// 1000 cycles took about 12 minutes).
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
// whose tickets were retired and recycled. None may be admitted as a new
// ticket or answered for the newer holder of the domain's next lease, which
// must stay untouched: admissions get a rights-free RETIRED_ID of their own
// bytes, QUERY/CANCEL are Stale. The replays repeat after a reopen.
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
// admissions and a different op under the old number answer RETIRED_ID
// bound to their own bytes (which CheckResponse vouches for, but not for the
// old GRANTED reply), and QUERY and CANCEL of both tickets are Stale.
func (x *recycleRun) requireReplaysRefused(old readRecord) {
	r := x.r
	r.t.Helper()
	// A different op under the recycled acquisition number.
	changed := map[string]any{"request": old.acquire["request"], "lease": old.release["lease"]}
	for _, c := range []struct {
		op      string
		payload any
	}{{"VIEW_ACQUIRE", old.acquire}, {"VIEW_RELEASE", old.release}, {"VIEW_RENEW", changed}} {
		x.requireRetiredID(c.op, c.payload)
	}
	for _, request := range []any{old.acquire["request"], old.release["request"]} {
		for _, op := range []string{"VIEW_QUERY", "VIEW_CANCEL"} {
			raw, err := canonical(map[string]any{"request": request})
			must(r.t, err)
			if reply, err := r.controller.ExchangeView(r.t.Context(), r.scopes[0], op, raw); !errors.Is(err, fsapi.Stale) {
				r.t.Fatalf("%s of a recycled ticket: %s %v, want Stale", op, reply, err)
			}
		}
	}
	raw, err := canonical(old.acquire)
	must(r.t, err)
	if err = r.controller.CheckResponse(r.t.Context(), r.scopes[0], "VIEW_ACQUIRE", raw, old.granted); !errors.Is(err, fsapi.Stale) {
		r.t.Fatalf("CheckResponse vouched for a recycled ticket's GRANTED reply: %v", err)
	}
}

// requireRetiredID requires a retired admission replay to answer RETIRED_ID
// with no rights, bound to this exact op and request, and CheckResponse to
// vouch for exactly that reply.
func (x *recycleRun) requireRetiredID(op string, payload any) {
	r := x.r
	r.t.Helper()
	raw, err := canonical(payload)
	must(r.t, err)
	reply, err := r.controller.ExchangeView(r.t.Context(), r.scopes[0], op, raw)
	must(r.t, err)
	var got viewReply
	must(r.t, json.Unmarshal(reply, &got))
	var hashed struct {
		Hash string `json:"request_hash"`
	}
	must(r.t, json.Unmarshal(reply, &hashed))
	if got.Status != "RETIRED_ID" || got.Operation != op || got.Lease != nil || got.Reservation != nil || string(got.Entry) != "null" || hashed.Hash != digest(map[string]any{"op": op, "payload": json.RawMessage(raw)}) {
		r.t.Fatalf("%s of a recycled ticket: %s", op, reply)
	}
	must(r.t, r.controller.CheckResponse(r.t.Context(), r.scopes[0], op, raw, reply))
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
	rec := x.writeLegacyLedger()
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

// writeLegacyLedger runs one read cycle on m0, retires it, and saves the
// ledger rewritten into the pre-q64 layout (makeLegacy), returning the cycle.
func (x *recycleRun) writeLegacyLedger() readRecord {
	r := x.r
	r.t.Helper()
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
	defer r.controller.mu.Unlock()
	r.controller.makeLegacy(rows, revisions)
	legacy, err := r.controller.rawState()
	must(r.t, err)
	must(r.t, r.controller.save())
	if bytes.Contains(legacy, []byte("LineageFences")) || bytes.Contains(legacy, []byte("Receipt")) {
		r.t.Fatal("legacy ledger still carries q64 fields")
	}
	return rec
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
