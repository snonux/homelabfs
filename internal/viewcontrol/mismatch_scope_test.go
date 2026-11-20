package viewcontrol

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"maps"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Scoped expected-target recalls (task e84, mismatch_scope.go). A pin-only
// cut move under observably current peer leases recalls only the requesting
// domain; an observable change, a dirty requester, an EXCLUSIVE_DIRTY request
// or a pruned expected cut keeps the all-domain recall.

// observeReleased takes and releases one guest observation on domain i's
// current cut (recall-free, je4) and returns the lease: its target is the
// view the guest has installed, and the inactive lease keeps that cut
// retained until compaction.
func (r *rig) observeReleased(i int) *guestbridge.ViewLease {
	r.t.Helper()
	lease := r.observeCurrent(i, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if reply := r.release(i, lease); reply.Status != "RELEASED" {
		r.t.Fatalf("observation release %d: %+v", i, reply)
	}
	return lease
}

// dropLifetimes sends the final DESCRIPTION_RELEASE and the FORGET of an
// object domain i created: a pin-only cut move (HandleRelease records) that
// reclaims nothing, since the object stays linked.
func (r *rig) dropLifetimes(i int, path, description guestbridge.Capability) {
	r.t.Helper()
	for _, step := range []struct {
		op   string
		args map[string]any
	}{
		{"DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1}},
		{"FORGET", map[string]any{"object": path, "count": 1}},
	} {
		if _, status := r.pendingCall(i, step.op, step.args); status != "TERMINAL" {
			r.t.Fatalf("%s: %s", step.op, status)
		}
	}
}

// staleAcquire sends domain i's observation acquire expecting target and
// requires PENDING; it returns the ticket for the later VIEW_QUERY.
func (r *rig) staleAcquire(i int, target guestbridge.ViewTarget) guestbridge.Ticket {
	r.t.Helper()
	ticket := r.ticket(i)
	reply := r.view(i, "VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": target})
	if reply.Status != "PENDING" {
		r.t.Fatalf("stale observation acquire %d: %+v", i, reply)
	}
	return ticket
}

// requireRecallScope requires a running recall of the given scope ("" for
// all-domain) with exactly the given participants.
func (r *rig) requireRecallScope(t *testing.T, scope string, participants ...int) {
	t.Helper()
	got, members, exists := r.recallSnapshot()
	if !exists || got != scope || !equal(members, participants) {
		t.Fatalf("recall scope %q participants %v exists %v, want %q %v", got, members, exists, scope, participants)
	}
}

// finishMismatch completes the running clean recall and requires the stale
// ticket's retry to answer BLOCKED for its old expectation.
func (r *rig) finishMismatch(t *testing.T, i int, ticket guestbridge.Ticket) {
	t.Helper()
	r.completeCleanRecall()
	if reply := r.view(i, "VIEW_QUERY", map[string]any{"request": ticket}); reply.Status != "BLOCKED" {
		t.Fatalf("stale ticket after its recall: %+v", reply)
	}
}

// TestPinOnlyMismatchRecallIsScoped: m0's final DESCRIPTION_RELEASE and
// FORGET move the cut under both domains' active observation leases (pins
// only). m1's next observation expects its old view: the recall covers m1
// alone, m0 keeps its lease and fence and its root GETATTR proceeds, and
// m1's retry is BLOCKED before its fresh observation is granted.
func TestPinOnlyMismatchRecallIsScoped(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "moves-cut", false)
	peer := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	installed := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.dropLifetimes(0, path, description)
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	if !r.leaseActive(peer) {
		t.Fatal("peer lease closed by a scoped mismatch recall")
	}
	if _, status := r.pendingCall(0, "GETATTR", map[string]any{"object": r.roots[0]}); status != "TERMINAL" {
		t.Fatalf("peer GETATTR during a scoped mismatch recall: %s", status)
	}
	r.finishMismatch(t, 1, ticket)
	if r.controller.fence(r.scopes[0].Session) != 1 || r.controller.fence(r.scopes[1].Session) != 2 {
		t.Fatalf("fences %d %d", r.controller.fence(r.scopes[0].Session), r.controller.fence(r.scopes[1].Session))
	}
	r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	records := history(t, r.controller).Recalls
	if last := records[len(records)-1]; last.Kind != TriggerExpectedTarget || last.Scope != "DOMAIN" {
		t.Fatalf("history %+v", last)
	}
	// The peer's lease survived the scoped recall on its pre-move target, so
	// its release keeps the scoped release recall of the moved cut (je4's
	// current-cut condition), exactly as without the mismatch.
	r.requireReleaseRecall(t, 0, peer, r.scopes[0].Session)
}

// TestOpenUnlinkedFinalReleaseMismatch: the final DESCRIPTION_RELEASE and
// FORGET of an open-unlinked file journal only HandleRelease records here:
// the object is reclaimed only by fsservice's reaper (lifetimes.go reap),
// which runs only with a ReclamationPolicy, and the controlled service has
// none. The unlinked object stays in the catalog unchanged and unreachable
// by name, so the move is pin-only and the peer's mismatch is scoped. Once
// a reaper journals the Reclaim, the object leaves the catalog and its
// allocation is retired, which changes the observable catalog
// (storage TestObservableCatalogOnRealStore), and a mismatch across it
// keeps the all-domain recall like any observable change: the test
// evaluates mismatchRecallScope against that reclaimed catalog shape too.
func TestOpenUnlinkedFinalReleaseMismatch(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "unlinked", false)
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	if res := r.call(0, "UNLINK", map[string]any{"parent": root, "name": hex.EncodeToString([]byte("unlinked"))}); res.Code != "OK" {
		t.Fatalf("UNLINK %+v", res)
	}
	installed := r.observeReleased(1)
	r.observeReleased(0)
	before, err := r.controller.capture(t.Context())
	must(t, err)
	r.dropLifetimes(0, path, description)
	after, err := r.controller.capture(t.Context())
	must(t, err)
	if sourceKey(before) == sourceKey(after) || !before.ObservablyEqual(after) {
		t.Fatal("final release without a reaper is not a pin-only move")
	}
	reclaimed := after
	reclaimed.State = withoutObject(t, after.State)
	if after.ObservablyEqual(reclaimed) {
		t.Fatal("a reclaimed object is not an observable change")
	}
	r.controller.mu.Lock()
	scope := r.controller.mismatchRecallScope(r.scopes[1], request{Mode: "READ", Target: &installed.Target}, reclaimed)
	r.controller.mu.Unlock()
	if scope != "" {
		t.Fatalf("mismatch across a Reclaim scoped to %q", scope)
	}
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
}

// withoutObject returns State with its catalog's unlinked object (nlink 0)
// moved to retired_allocations, the catalog shape a Reclaim leaves.
func withoutObject(t *testing.T, state []byte) []byte {
	t.Helper()
	n := binary.BigEndian.Uint64(state[:8])
	var catalog map[string]any
	must(t, json.Unmarshal(state[8:8+n], &catalog))
	var kept []any
	retired, _ := catalog["retired_allocations"].([]any)
	for _, o := range catalog["objects"].([]any) {
		object := o.(map[string]any)
		if object["attributes"].(map[string]any)["nlink"].(float64) == 0 {
			retired = append(retired, object["descriptor"])
			continue
		}
		kept = append(kept, object)
	}
	catalog["objects"], catalog["retired_allocations"] = kept, retired
	raw, err := json.Marshal(catalog)
	must(t, err)
	out := binary.BigEndian.AppendUint64(nil, uint64(len(raw)))
	return append(append(out, raw...), state[8+n:]...)
}

// TestObservedCutEviction: eviction drops cuts of another journal
// incarnation before the lowest position of the current one.
func TestObservedCutEviction(t *testing.T) {
	c := &Controller{observed: map[string]observedCut{
		"old-high": {incarnation: "old", position: 90},
		"cur-low":  {incarnation: "cur", position: 1},
		"cur-high": {incarnation: "cur", position: 50},
	}}
	c.evictOldestObserved("cur")
	if _, ok := c.observed["old-high"]; ok {
		t.Fatal("older incarnation kept")
	}
	c.evictOldestObserved("cur")
	if _, ok := c.observed["cur-low"]; ok || len(c.observed) != 1 {
		t.Fatalf("eviction left %v", c.observed)
	}
}

// TestMixedMismatchSequences: a pin-only move followed by a mutation keeps
// the all-domain recall; once that recall installed the new cut, a later
// pin-only move is scoped again.
func TestMixedMismatchSequences(t *testing.T) {
	r := newZQ3Rig(t, 0)
	pathA, descriptionA := r.create(0, "pin-then", false)
	installed := r.observeReleased(1)
	r.observeReleased(0)
	r.dropLifetimes(0, pathA, descriptionA)
	pathB, descriptionB := r.create(0, "mutation", false)
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, "", 0, 1)
	r.finishMismatch(t, 1, ticket)

	installed = r.observeReleased(1)
	r.observeReleased(0)
	r.dropLifetimes(0, pathB, descriptionB)
	ticket = r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
}

// TestPinOnlyMismatchWaitsForRunningRecall: while m0's scoped release recall
// runs, m1's pin-only mismatch stays PENDING without spending its recall;
// after that recall its retry starts the scoped m1 recall and, once that
// ran, answers BLOCKED.
func TestPinOnlyMismatchWaitsForRunningRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "concurrent", false)
	installed := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.dropLifetimes(0, path, description)
	release := r.startRelease(0, r.domainRead(0))
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[0].Session, 0)
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", release)
	if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": release["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("m0 release %+v", reply)
	}
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": ticket}); reply.Status != "PENDING" {
		t.Fatalf("mismatch retry after the running recall: %+v", reply)
	}
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
}

// TestMismatchScopeConditions evaluates mismatchRecallScope on a pin-only
// move with each safety condition broken once on a copy of the ledger and
// of the in-memory digest cache.
func TestMismatchScopeConditions(t *testing.T) {
	r := newZQ3Rig(t, 0)
	before, err := r.controller.capture(t.Context())
	must(t, err)
	path, description := r.create(0, "conditions", false)
	peer := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	installed := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.dropLifetimes(0, path, description)
	session := r.scopes[1].Session
	pruneExpected := func(c *Controller) {
		for key, source := range c.state.Sources {
			if equal(c.target(r.scopes[1], source), installed.Target) {
				delete(c.state.Sources, key)
			}
		}
	}
	olderPeer := func(active bool) func(c *Controller) {
		return func(c *Controller) {
			c.state.Sources[sourceKey(before)] = before
			l := c.state.Leases[peer.ID]
			l.Lease.Target, l.Active = c.target(r.scopes[0], before), active
			c.state.Leases[peer.ID] = l
		}
	}
	for _, tc := range []struct {
		name   string
		mode   string
		change func(c *Controller)
		want   string
	}{
		{"pin-only move", "READ", func(*Controller) {}, session},
		{"EXCLUSIVE_DIRTY request", "EXCLUSIVE_DIRTY", func(*Controller) {}, ""},
		{"requester holds an EXCLUSIVE_DIRTY lease", "READ", func(c *Controller) {
			write := c.state.Leases[installed.ID]
			write.Lease.ID, write.Lease.Mode = "write", "EXCLUSIVE_DIRTY"
			c.state.Leases["write"] = write
		}, ""},
		{"requester holds an open reservation", "READ", func(c *Controller) {
			c.state.Reservations["open"] = reservationState{Reservation: guestbridge.DirtyReservation{ID: "open", Scope: guestbridge.Context{Session: session}}, Status: "RESERVED"}
		}, ""},
		{"expected cut pruned but remembered", "READ", pruneExpected, session},
		{"expected cut pruned and forgotten (reopen)", "READ", func(c *Controller) {
			pruneExpected(c)
			c.observed = nil
		}, ""},
		{"expected cut retained, cache empty", "READ", func(c *Controller) { c.observed = nil }, session},
		{"peer lease on an observably older cut", "READ", olderPeer(true), ""},
		{"inactive peer lease on an older cut", "READ", olderPeer(false), session},
		{"untargeted (discovery) peer lease", "READ", func(c *Controller) {
			l := c.state.Leases[peer.ID]
			l.Targeted = false
			c.state.Leases[peer.ID] = l
		}, ""},
		{"pre-e84 ablation", "READ", func(c *Controller) { c.allDomainMismatch = true }, ""},
	} {
		if got := mismatchScopeAfter(r, installed.Target, tc.mode, tc.change); got != tc.want {
			t.Errorf("%s: scope %q, want %q", tc.name, got, tc.want)
		}
	}
}

// mismatchScopeAfter applies change to copies of the controller's ledger
// maps and digest cache, evaluates mismatchRecallScope for domain 1
// expecting target and restores both.
func mismatchScopeAfter(r *rig, target guestbridge.ViewTarget, mode string, change func(*Controller)) string {
	c := r.controller
	current, err := c.capture(r.t.Context())
	must(r.t, err)
	c.mu.Lock()
	defer c.mu.Unlock()
	saved, observed := c.state, c.observed
	c.state.Leases = maps.Clone(saved.Leases)
	c.state.Reservations = maps.Clone(saved.Reservations)
	c.state.Sources = maps.Clone(saved.Sources)
	c.observed = maps.Clone(observed)
	change(c)
	got := c.mismatchRecallScope(r.scopes[1], request{Mode: mode, Target: &target}, current)
	c.state, c.observed, c.allDomainMismatch = saved, observed, false
	return got
}

// TestObservablyEqualCuts pins the classification itself on real captures:
// a pin-only move is observably equal, a mutation is not, and neither is a
// cut of a different serving generation or one with malformed State.
func TestObservablyEqualCuts(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "classify", false)
	base, err := r.controller.capture(t.Context())
	must(t, err)
	r.dropLifetimes(0, path, description)
	pinned, err := r.controller.capture(t.Context())
	must(t, err)
	if sourceKey(base) == sourceKey(pinned) || !base.ObservablyEqual(pinned) {
		t.Fatal("pin-only move not classified observably equal")
	}
	r.create(0, "classify-2", false)
	mutated, err := r.controller.capture(t.Context())
	must(t, err)
	if pinned.ObservablyEqual(mutated) {
		t.Fatal("mutation classified observably equal")
	}
	other := pinned
	other.Visibility = uid(99)
	if pinned.ObservablyEqual(other) {
		t.Fatal("different serving generation classified observably equal")
	}
	truncated := pinned
	truncated.State = pinned.State[:4]
	if pinned.ObservablyEqual(truncated) {
		t.Fatal("malformed state classified observably equal")
	}
}
