package viewcontrol

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"maps"
	"testing"

	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Scoped expected-target recalls (tasks e84 and wf4, mismatch_scope.go). A
// cut move, pin-only or observable, recalls only the requesting domain while
// every active peer lease is Targeted on an observably current cut (or no
// peer holds one); a peer lease on an observably older or unknown cut, an
// untargeted peer lease, an open reservation in any domain, a dirty
// requester or an EXCLUSIVE_DIRTY request keeps the all-domain recall.

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
// (storage TestObservableCatalogOnRealStore). A Reclaim is an authority
// reduction, so unlike a mutation it can be admitted under an active peer
// lease, leaving that lease on an observably older cut: the test evaluates
// mismatchRecallScope against the reclaimed catalog shape with idle peers
// (scoped since wf4: nothing of theirs is stale under a lease) and with an
// active peer observation on the pre-Reclaim cut (all-domain).
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
	if got := reclaimScope(r, installed.Target, reclaimed); got != r.scopes[1].Session {
		t.Fatalf("mismatch across a Reclaim with idle peers scoped to %q", got)
	}
	peer := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if got := reclaimScope(r, installed.Target, reclaimed); got != "" {
		t.Fatalf("mismatch across a Reclaim under a pre-Reclaim peer lease scoped to %q", got)
	}
	if reply := r.release(0, peer); reply.Status != "RELEASED" {
		t.Fatalf("peer observation release: %+v", reply)
	}
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
}

// reclaimScope evaluates mismatchRecallScope for domain 1 expecting target
// as if current were the captured cut.
func reclaimScope(r *rig, target guestbridge.ViewTarget, current fsservice.ControlledViewSource) string {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	return r.controller.mismatchRecallScope(r.scopes[1], request{Mode: "READ", Target: &target}, current)
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

// TestMixedMismatchSequences: with idle peers a pin-only move followed by a
// mutation is scoped like a pin-only move alone (wf4), and so is a later
// pin-only move. TestMixedMismatchSequencesPeerLease keeps the all-domain
// then scoped-again transition under a live peer lease.
func TestMixedMismatchSequences(t *testing.T) {
	r := newZQ3Rig(t, 0)
	pathA, descriptionA := r.create(0, "pin-then", false)
	installed := r.observeReleased(1)
	r.observeReleased(0)
	r.dropLifetimes(0, pathA, descriptionA)
	pathB, descriptionB := r.create(0, "mutation", false)
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)

	installed = r.observeReleased(1)
	r.observeReleased(0)
	r.dropLifetimes(0, pathB, descriptionB)
	ticket = r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
}

// TestMixedMismatchSequencesPeerLease is the pre-wf4 transition with a live
// peer lease: m0 holds a Targeted observation on the cut before a pin-only
// move and a mutation, so m1's mismatch recalls all domains (and quiesces
// m0's lease); once that recall installed the new cut and the peers are idle
// again, a later pin-only move is scoped. A mutation is admitted only with
// no active lease, so the live ledger reactivates m0's released observation
// on its pre-mutation cut after the CREATE, as a Reclaim (an authority
// reduction admitted under leases) could leave it.
func TestMixedMismatchSequencesPeerLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	pathA, descriptionA := r.create(0, "pin-then", false)
	older, err := r.controller.capture(t.Context())
	must(t, err)
	peer := r.observeReleased(0)
	installed := r.observeReleased(1)
	r.dropLifetimes(0, pathA, descriptionA)
	pathB, descriptionB := r.create(0, "mutation", false)
	r.reactivateOn(peer, older)
	ticket := r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, "", 0, 1)
	r.finishMismatch(t, 1, ticket)
	if r.leaseActive(peer) {
		t.Fatal("peer lease survived the all-domain mismatch recall")
	}

	installed = r.observeReleased(1)
	r.observeReleased(0)
	r.dropLifetimes(0, pathB, descriptionB)
	ticket = r.staleAcquire(1, installed.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
}

// reactivateOn makes the released lease active again in the live ledger,
// with its cut source retained, and saves the ledger.
func (r *rig) reactivateOn(lease *guestbridge.ViewLease, source fsservice.ControlledViewSource) {
	r.t.Helper()
	c := r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	if !equal(c.target(lease.Scope, source), lease.Target) {
		r.t.Fatal("lease target is not the given cut")
	}
	c.state.Sources[sourceKey(source)] = source
	l := c.state.Leases[lease.ID]
	l.Active = true
	c.state.Leases[lease.ID] = l
	must(r.t, c.save())
}

// TestIdlePeerAcquireDuringScopedMismatchRecall: while m0's scoped mismatch
// recall after its MKDIR runs, the idle m1 observes with its stale view. m1
// is outside the recall, so its READ acquisition is not held
// (recallHoldsLease), but its expected target mismatches and no second
// recall can start: it is PENDING, never granted on the stale view, and the
// running recall stays scoped to m0. After it, m1's retry starts m1's own
// scoped recall and then answers BLOCKED for the stale expectation.
func TestIdlePeerAcquireDuringScopedMismatchRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	peerView := r.observeReleased(1)
	installed := r.observeReleased(0)
	r.mkdir(0, "race")
	ticket := r.staleAcquire(0, installed.Target)
	r.requireRecallScope(t, r.scopes[0].Session, 0)
	peerTicket := r.staleAcquire(1, peerView.Target)
	r.requireRecallScope(t, r.scopes[0].Session, 0)
	r.finishMismatch(t, 0, ticket)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": peerTicket}); reply.Status != "PENDING" {
		t.Fatalf("stale peer retry after the scoped recall: %+v", reply)
	}
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, peerTicket)
}

// mkdir sends domain i's MKDIR of name under its root: an observable
// mutation. It conflicts with every active lease, so the callers hold none.
func (r *rig) mkdir(i int, name string) {
	r.t.Helper()
	root := r.call(i, "ROOT", map[string]any{}).Capabilities[0]
	if res := r.call(i, "MKDIR", map[string]any{"parent": root, "name": hex.EncodeToString([]byte(name)), "mode": 0700, "umask": 0}); res.Code != "OK" {
		r.t.Fatalf("MKDIR %+v", res)
	}
}

// TestMutationMismatchWithIdlePeerIsScoped is zq3-r18 T1 (wf4): m1 holds no
// lease, m0 creates a directory and m0's next GETATTR observation expects
// its pre-MKDIR view. The recall covers m0 alone and m1's fence stays. m1's
// stale view is never served: its own next observation mismatches too and
// recalls only m1, after which it is granted on the current cut.
func TestMutationMismatchWithIdlePeerIsScoped(t *testing.T) {
	r := newZQ3Rig(t, 0)
	peerView := r.observeReleased(1)
	installed := r.observeReleased(0)
	r.mkdir(0, "zq3-credentials")
	ticket := r.staleAcquire(0, installed.Target)
	r.requireRecallScope(t, r.scopes[0].Session, 0)
	r.finishMismatch(t, 0, ticket)
	r.observeReleased(0)
	records := history(t, r.controller).Recalls
	if last := records[len(records)-1]; last.Kind != TriggerExpectedTarget || last.Scope != "DOMAIN" {
		t.Fatalf("history %+v", last)
	}
	if r.controller.fence(r.scopes[0].Session) != 2 || r.controller.fence(r.scopes[1].Session) != 1 {
		t.Fatalf("fences %d %d", r.controller.fence(r.scopes[0].Session), r.controller.fence(r.scopes[1].Session))
	}
	ticket = r.staleAcquire(1, peerView.Target)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
	r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
}

// TestMutationMismatchPeerLeaseOnOlderCut: after m1's MKDIR, a Targeted m0
// lease on the pre-MKDIR cut keeps m1's mismatch recall all-domain, because
// reinstalling the current cut changes what m0 can observe. A mutation is
// admitted only with no active lease (it conflicts with every one), so the
// ledger copy reactivates m0's released observation on the older cut, as an
// authority reduction's Reclaim could leave it
// (TestOpenUnlinkedFinalReleaseMismatch); the same lease inactive is an
// idle peer and the recall stays scoped.
func TestMutationMismatchPeerLeaseOnOlderCut(t *testing.T) {
	r := newZQ3Rig(t, 0)
	older, err := r.controller.capture(t.Context())
	must(t, err)
	peer := r.observeReleased(0)
	if !equal(r.controller.target(r.scopes[0], older), peer.Target) {
		t.Fatal("peer observation is not on the captured pre-MKDIR cut")
	}
	installed := r.observeReleased(1)
	r.mkdir(1, "older-peer")
	peerOnOlder := func(active bool) func(c *Controller) {
		return func(c *Controller) {
			c.state.Sources[sourceKey(older)] = older
			l := c.state.Leases[peer.ID]
			l.Active = active
			c.state.Leases[peer.ID] = l
		}
	}
	if got := mismatchScopeAfter(r, installed.Target, "READ", peerOnOlder(true)); got != "" {
		t.Fatalf("peer lease on the pre-MKDIR cut: scope %q, want all-domain", got)
	}
	if got := mismatchScopeAfter(r, installed.Target, "READ", peerOnOlder(false)); got != r.scopes[1].Session {
		t.Fatalf("idle peer after MKDIR: scope %q, want %q", got, r.scopes[1].Session)
	}
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

// scopeCase is one mismatchRecallScope evaluation: change breaks (or keeps)
// one condition on the ledger copy, and want is the expected scope.
type scopeCase struct {
	name   string
	mode   string
	change func(c *Controller)
	want   string
}

// conditionsFixture is the TestMismatchScopeConditions setup: a pin-only
// move after both domains observed the same cut, plus the cut before it.
type conditionsFixture struct {
	r               *rig
	before          fsservice.ControlledViewSource
	peer, installed *guestbridge.ViewLease
	session         string
}

// TestMismatchScopeConditions evaluates mismatchRecallScope on a pin-only
// move with each safety condition broken once on a copy of the ledger and
// of the in-memory digest cache.
func TestMismatchScopeConditions(t *testing.T) {
	r := newZQ3Rig(t, 0)
	before, err := r.controller.capture(t.Context())
	must(t, err)
	path, description := r.create(0, "conditions", false)
	f := conditionsFixture{r: r, before: before, session: r.scopes[1].Session}
	f.peer = r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	f.installed = r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.dropLifetimes(0, path, description)
	for _, tc := range append(f.requesterCases(), f.peerCases()...) {
		if got := mismatchScopeAfter(r, f.installed.Target, tc.mode, tc.change); got != tc.want {
			t.Errorf("%s: scope %q, want %q", tc.name, got, tc.want)
		}
	}
}

// pruneExpected drops the retained cut the requester's expected target names.
func (f conditionsFixture) pruneExpected(c *Controller) {
	for key, source := range c.state.Sources {
		if equal(c.target(f.r.scopes[1], source), f.installed.Target) {
			delete(c.state.Sources, key)
		}
	}
}

// requesterCases break the requester's own conditions or its expected cut.
// Since wf4 the expected cut is never classified, so pruning and forgetting
// it matters only through the peer lease that shares it.
func (f conditionsFixture) requesterCases() []scopeCase {
	return []scopeCase{
		{"pin-only move", "READ", func(*Controller) {}, f.session},
		{"EXCLUSIVE_DIRTY request", "EXCLUSIVE_DIRTY", func(*Controller) {}, ""},
		{"requester holds an EXCLUSIVE_DIRTY lease", "READ", func(c *Controller) {
			write := c.state.Leases[f.installed.ID]
			write.Lease.ID, write.Lease.Mode = "write", "EXCLUSIVE_DIRTY"
			c.state.Leases["write"] = write
		}, ""},
		{"requester holds an open reservation", "READ", func(c *Controller) {
			c.state.Reservations["open"] = reservationState{Reservation: guestbridge.DirtyReservation{ID: "open", Scope: guestbridge.Context{Session: f.session}}, Status: "RESERVED"}
		}, ""},
		{"expected cut pruned but remembered", "READ", f.pruneExpected, f.session},
		// The peer observed the same cut as the requester, so pruning the
		// expected cut leaves the active peer lease's digest unknown too.
		{"expected and peer cut pruned and forgotten (reopen)", "READ", func(c *Controller) {
			f.pruneExpected(c)
			c.observed = nil
		}, ""},
		{"expected cut forgotten, peer idle", "READ", func(c *Controller) {
			f.pruneExpected(c)
			c.observed = nil
			f.setPeer(c, func(l *leaseState) { l.Active = false })
		}, f.session},
		{"expected cut retained, cache empty", "READ", func(c *Controller) { c.observed = nil }, f.session},
		{"pre-e84 ablation", "READ", func(c *Controller) { c.allDomainMismatch = true }, ""},
	}
}

// peerCases break the peer conditions: its lease's cut, kind and activity,
// and its reservations.
func (f conditionsFixture) peerCases() []scopeCase {
	olderPeer := func(active bool) func(c *Controller) {
		return func(c *Controller) {
			c.state.Sources[sourceKey(f.before)] = f.before
			f.setPeer(c, func(l *leaseState) { l.Lease.Target, l.Active = c.target(f.r.scopes[0], f.before), active })
		}
	}
	return []scopeCase{
		{"peer holds an open reservation", "READ", func(c *Controller) {
			c.state.Reservations["peer"] = reservationState{Reservation: guestbridge.DirtyReservation{ID: "peer", Scope: f.r.scopes[0]}, Status: "DRAINING"}
		}, ""},
		{"peer lease on an observably older cut", "READ", olderPeer(true), ""},
		{"inactive peer lease on an older cut", "READ", olderPeer(false), f.session},
		{"untargeted (discovery) peer lease", "READ", func(c *Controller) {
			f.setPeer(c, func(l *leaseState) { l.Targeted = false })
		}, ""},
	}
}

// setPeer applies edit to the peer lease in the ledger copy.
func (f conditionsFixture) setPeer(c *Controller, edit func(*leaseState)) {
	l := c.state.Leases[f.peer.ID]
	edit(&l)
	c.state.Leases[f.peer.ID] = l
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
