package viewcontrol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Own observation leases and own CALLs (task vj4, self_conflict.go): the
// requester's own Targeted DOMAIN READ lease is skipped; every peer lease,
// every own non-observation lease and the pre-vj4 ablation still recall.

// mkdirStatus sends domain i's MKDIR of dir under its root and returns the
// claim's first QUERY status (TERMINAL when no recall held it).
func (r *rig) mkdirStatus(i int, dir string) string {
	r.t.Helper()
	root := r.call(i, "ROOT", map[string]any{}).Capabilities[0]
	_, status := r.pendingCall(i, "MKDIR", map[string]any{"parent": root, "name": name(dir), "mode": 0700, "umask": 0})
	return status
}

// requireCallConflict requires a running CALL_CONFLICT recall of the given
// scope ("" for all-domain) and participants, started by an op of domain
// requester.
func (r *rig) requireCallConflict(t *testing.T, requester int, scope string, participants ...int) {
	t.Helper()
	r.requireRecallScope(t, scope, participants...)
	records := history(t, r.controller).Recalls
	last := records[len(records)-1]
	if last.Kind != TriggerCallConflict || last.Opcode != "MKDIR" || last.Domain != r.scopes[requester].Session {
		t.Fatalf("recall %s/%s by %s, want CALL_CONFLICT/MKDIR by m%d", last.Kind, last.Opcode, last.Domain, requester)
	}
}

// TestOwnObservationDoesNotRecallOwnMutation is follow-up F: m0's MKDIR
// while its own observation lease is still active (release queued) is
// admitted at once, starts no recall, leaves the lease active, moves no
// fence and is counted as a self admission. The lease's release then finds
// the cut moved: it is not an Advance lease, so it recalls m0 alone.
func TestOwnObservationDoesNotRecallOwnMutation(t *testing.T) {
	r := newZQ3Rig(t, 0)
	own := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if status := r.mkdirStatus(0, "vj4-own"); status != "TERMINAL" {
		t.Fatalf("MKDIR under its own observation lease: %s", status)
	}
	r.requireNoRecall(t, "own-lease MKDIR", own)
	if r.recallTotal() != 0 || r.selfAdmissions() != 1 || r.controller.fence(r.scopes[0].Session) != 1 {
		t.Fatalf("recalls %d, self admissions %d, fence %d", r.recallTotal(), r.selfAdmissions(), r.controller.fence(r.scopes[0].Session))
	}
	r.startRelease(0, own)
	r.requireRecallScope(t, r.scopes[0].Session, 0)
}

// TestOwnAdvanceObservationReleasesWithoutRecall: the T1 shape with an
// advancing guest. m0's advance observation (granted on the current cut),
// its MKDIR and then the queued release: no recall at all, because an
// Advance lease's release skips the recall although the MKDIR moved the cut.
func TestOwnAdvanceObservationReleasesWithoutRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, reply := r.advanceAcquire(0, r.currentTarget(0))
	if reply.Status != "GRANTED" || reply.Lease == nil || !r.leaseState(reply.Lease).Advance {
		t.Fatalf("advance observation: %+v", reply)
	}
	if status := r.mkdirStatus(0, "vj4-advance"); status != "TERMINAL" {
		t.Fatalf("MKDIR under its own advance lease: %s", status)
	}
	if released := r.release(0, reply.Lease); released.Status != "RELEASED" || r.recallTotal() != 0 {
		t.Fatalf("release %+v after %d recalls", released, r.recallTotal())
	}
}

// TestOwnLaterObservationSeesOwnMutation: after the self-admitted MKDIR m0's
// next observation still expects its pre-MKDIR view, and the host never
// grants that older cut again: without advance the expected-target recall
// runs (scoped to m0, its own still-active lease does not widen it), with
// advance the grant is on the current cut, which holds the MKDIR.
func TestOwnLaterObservationSeesOwnMutation(t *testing.T) {
	r := newZQ3Rig(t, 0)
	own := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if status := r.mkdirStatus(0, "vj4-later"); status != "TERMINAL" {
		t.Fatalf("MKDIR: %s", status)
	}
	ticket := r.staleAcquire(0, own.Target)
	r.requireRecallScope(t, r.scopes[0].Session, 0)
	r.finishMismatch(t, 0, ticket)

	a := newZQ3Rig(t, 0)
	installed := a.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if status := a.mkdirStatus(0, "vj4-later"); status != "TERMINAL" {
		t.Fatalf("MKDIR: %s", status)
	}
	_, reply := a.advanceAcquire(0, installed.Target)
	lease := a.requireAdvanced(t, 0, reply)
	if equal(lease.Target, installed.Target) {
		t.Fatal("advance granted the pre-MKDIR cut")
	}
}

// TestPeerObservationStillRecalls: cross-domain conflicts are unchanged. A
// peer's observation lease alone, or together with the requester's own,
// makes the MKDIR start the all-domain CALL_CONFLICT recall with exactly the
// scope and participants the pre-vj4 rule (selfConflicts ablation) gives,
// and no self admission is counted.
func TestPeerObservationStillRecalls(t *testing.T) {
	for _, own := range []bool{false, true} {
		pre := peerConflictRecall(t, own, true)
		vj4 := peerConflictRecall(t, own, false)
		if !equal(pre, vj4) || pre.Scope != "" || len(pre.Participants) == 0 {
			t.Errorf("own %v: vj4 recall %+v, pre-vj4 %+v; want the same all-domain recall", own, vj4, pre)
		}
	}
}

// conflictRecall is a CALL_CONFLICT recall's scope and participants
// (exported, so equal's canonical encoding compares them).
type conflictRecall struct {
	Scope        string
	Participants []int
}

// peerConflictRecall runs m0's MKDIR under m1's observation lease (and m0's
// own when own is set) on a fresh rig, with or without the pre-vj4 rule,
// requires it to be held by a CALL_CONFLICT recall of m0's MKDIR without a
// self admission, completes the recall (which revokes the peer lease) and
// returns the recall's scope and participants.
func peerConflictRecall(t *testing.T, own, preVJ4 bool) conflictRecall {
	t.Helper()
	r := newZQ3Rig(t, 0)
	r.controller.selfConflicts = preVJ4
	if own {
		r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	}
	peer := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if status := r.mkdirStatus(0, "vj4-peer"); status == "TERMINAL" {
		t.Fatalf("own %v pre-vj4 %v: MKDIR under a peer lease was not held", own, preVJ4)
	}
	scope, participants, _ := r.recallSnapshot()
	r.requireCallConflict(t, 0, scope, participants...)
	if r.selfAdmissions() != 0 {
		t.Fatalf("own %v pre-vj4 %v: %d self admissions", own, preVJ4, r.selfAdmissions())
	}
	r.completeCleanRecall()
	if r.leaseActive(peer) {
		t.Fatalf("own %v pre-vj4 %v: the peer lease survived its recall", own, preVJ4)
	}
	return conflictRecall{Scope: scope, Participants: participants}
}

// TestOwnNonObservationLeasesStillRecall: only the requester's Targeted
// DOMAIN READ leases are skipped. Its untargeted discovery lease
// (target:null), its PAGE READ lease and, under the pre-vj4 ablation, its
// observation lease still start the CALL_CONFLICT recall.
func TestOwnNonObservationLeasesStillRecall(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *rig)
	}{
		{"untargeted DOMAIN READ", func(r *rig) { r.domainRead(0) }},
		{"PAGE READ", func(r *rig) { r.pageRead(0) }},
		{"pre-vj4 ablation", func(r *rig) {
			r.controller.selfConflicts = true
			r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newZQ3Rig(t, 0)
			tc.setup(r)
			if status := r.mkdirStatus(0, "vj4-kept"); status == "TERMINAL" {
				t.Fatal("MKDIR was not held")
			}
			r.requireCallConflict(t, 0, "", 0) // all-domain; only m0 registered
			if r.selfAdmissions() != 0 {
				t.Fatalf("%d self admissions", r.selfAdmissions())
			}
		})
	}
}

// TestSelfObservationPredicate pins selfObservation field by field: only an
// active, Targeted DOMAIN READ lease whose Scope is the requesting Context
// exactly is skipped; another session, another field of the same session's
// Context (guest mount, generation, FUSE session), an inactive, untargeted,
// PAGE, OBJECT or EXCLUSIVE_DIRTY lease is not.
func TestSelfObservationPredicate(t *testing.T) {
	r := newZQ3Rig(t, 0)
	scope := r.scopes[0]
	base := leaseState{Active: true, Targeted: true, Lease: guestbridge.ViewLease{Scope: scope, Access: guestbridge.ViewAccess{Kind: "DOMAIN"}, Mode: "READ"}}
	otherMount := scope
	otherMount.GuestMount = "another-mount"
	otherGeneration := scope
	otherGeneration.Generation = "another-generation"
	otherFuse := scope
	otherFuse.FuseSession = "another-fuse-session"
	cases := []struct {
		name   string
		mutate func(*leaseState)
		want   bool
	}{
		{"own observation", func(*leaseState) {}, true},
		{"peer session", func(l *leaseState) { l.Lease.Scope = r.scopes[1] }, false},
		{"same session, other mount", func(l *leaseState) { l.Lease.Scope = otherMount }, false},
		{"same session, other generation", func(l *leaseState) { l.Lease.Scope = otherGeneration }, false},
		{"same session, other FUSE session", func(l *leaseState) { l.Lease.Scope = otherFuse }, false},
		{"OBJECT", func(l *leaseState) { l.Lease.Access.Kind = "OBJECT" }, false},
		{"inactive", func(l *leaseState) { l.Active = false }, false},
		{"untargeted", func(l *leaseState) { l.Targeted = false }, false},
		{"PAGE", func(l *leaseState) { l.Lease.Access.Kind = "PAGE" }, false},
		{"EXCLUSIVE_DIRTY", func(l *leaseState) { l.Lease.Mode = "EXCLUSIVE_DIRTY" }, false},
	}
	for _, tc := range cases {
		l := base
		tc.mutate(&l)
		if got := selfObservation(l, scope); got != tc.want {
			t.Errorf("%s: selfObservation %v, want %v", tc.name, got, tc.want)
		}
	}
	if selfObservation(base, guestbridge.Context{}) {
		t.Error("the zero Context (native commands) matched a guest lease")
	}
}

// TestSelfAdmissionsObserveShape: OBSERVE's recall history omits
// "self_admissions" while it is 0, so older consumers see the pre-vj4 shape,
// and reports it once a CALL was admitted past its own observation lease.
func TestSelfAdmissionsObserveShape(t *testing.T) {
	r := newZQ3Rig(t, 0)
	encode := func() string {
		raw, err := json.Marshal(history(t, r.controller))
		must(t, err)
		return string(raw)
	}
	if strings.Contains(encode(), "self_admissions") {
		t.Fatalf("fresh history carries self_admissions: %s", encode())
	}
	r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if status := r.mkdirStatus(0, "vj4-shape"); status != "TERMINAL" {
		t.Fatalf("MKDIR: %s", status)
	}
	if !strings.Contains(encode(), `"self_admissions":1`) {
		t.Fatalf("history after one self admission: %s", encode())
	}
}

// TestPeerMutationRecallsSelfAdmittedLease: m0's MKDIR is self-admitted past
// its own observation lease, which stays active on the pre-MKDIR cut; m1's
// mutation then conflicts with that stale lease exactly as before vj4, so it
// starts the all-domain CALL_CONFLICT recall, whose completion deactivates
// m0's lease.
func TestPeerMutationRecallsSelfAdmittedLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	stale := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if status := r.mkdirStatus(0, "vj4-self"); status != "TERMINAL" || r.selfAdmissions() != 1 {
		t.Fatalf("own MKDIR: %s, %d self admissions", status, r.selfAdmissions())
	}
	if status := r.mkdirStatus(1, "vj4-peer"); status == "TERMINAL" {
		t.Fatal("peer MKDIR was not held by m0's stale lease")
	}
	scope, participants, _ := r.recallSnapshot()
	if scope != "" || len(participants) == 0 || participants[0] != 0 {
		t.Fatalf("recall scope %q participants %v, want all-domain with m0", scope, participants)
	}
	r.requireCallConflict(t, 1, scope, participants...)
	r.completeCleanRecall()
	if r.leaseActive(stale) || r.selfAdmissions() != 1 {
		t.Fatalf("stale lease active %v, %d self admissions", r.leaseActive(stale), r.selfAdmissions())
	}
}
