package viewcontrol

import (
	"fmt"
	"testing"
)

// T1 replays of task vj4 (self_conflict.go, xf4 follow-up F): the creating
// CALLs meet the mount's own still-queued observation release
// (advanceReplay.creating). Before vj4 each one started an all-domain
// CALL_CONFLICT recall; since vj4 the requester's own observation lease does
// not conflict with its own CALL, while a peer's lease still does.

// preVJ4Rig is a zq3 rig whose controller runs the pre-vj4 conflict rule
// (the selfConflicts ablation): a CALL conflicts with its own context's
// observation leases too.
func preVJ4Rig(t *testing.T) *rig {
	r := newZQ3Rig(t, 0)
	r.controller.selfConflicts = true
	return r
}

// selfAdmissions is the controller's vj4 counter as OBSERVE reports it.
func (r *rig) selfAdmissions() uint64 {
	r.t.Helper()
	return history(r.t, r.controller).SelfAdmissions
}

// TestZQ3SelfConflictRemovesCallConflictRecalls replays T1 with advance
// twice: under the pre-vj4 rule and with vj4. Before: 7 CALL_CONFLICT
// recalls (one per creating syscall) and no EXPECTED_TARGET recall. With
// vj4: no CALL_CONFLICT and no EXPECTED_TARGET recall, one self admission
// per former CALL_CONFLICT recall, and at most two RELEASE recalls. Those
// are lineage-budget recalls (observation_release.go lineageHeadroom): the
// CALL_CONFLICT recalls used to move both fences, which let compaction
// recycle lineages; without them a domain reaches lineageBudget lineages at
// one fence about twice in T1 (xf4 design, "Lineages" and optional step D).
// Each is scoped to its own domain, where the recalls it replaces were
// all-domain. A third run without advance only logs its counts: no
// CALL_CONFLICT recall either.
func TestZQ3SelfConflictRemovesCallConflictRecalls(t *testing.T) {
	t.Parallel()
	before := (&advanceReplay{r: preVJ4Rig(t), advance: true}).run()
	vj4 := &advanceReplay{r: newZQ3Rig(t, 0), advance: true}
	after := vj4.run()
	plain := &advanceReplay{r: newZQ3Rig(t, 0)}
	withoutAdvance := plain.run()
	t.Logf("T1 with advance: pre-vj4 %v; vj4 %v (%d self admissions, %d advances); vj4 without advance %v (%d self admissions)",
		before, after, vj4.r.selfAdmissions(), vj4.r.advances(), withoutAdvance, plain.r.selfAdmissions())
	for _, problem := range selfConflictVerdict(before, after, vj4.r.selfAdmissions()) {
		t.Error(problem)
	}
	if withoutAdvance[TriggerCallConflict] != 0 {
		t.Errorf("vj4 without advance: %d CALL_CONFLICT recalls, want 0", withoutAdvance[TriggerCallConflict])
	}
}

// selfConflictVerdict lists how a vj4 run falls short against the pre-vj4
// run with the same guest; none means it holds.
func selfConflictVerdict(before, after map[string]int, self uint64) []string {
	var problems []string
	if before[TriggerCallConflict] == 0 {
		problems = append(problems, "pre-vj4 run has no CALL_CONFLICT recall: the replay no longer exercises the self conflict")
	}
	if after[TriggerCallConflict] != 0 || after[TriggerExpectedTarget] != 0 || after[TriggerRelease] > 2 {
		problems = append(problems, fmt.Sprintf("vj4: CALL_CONFLICT %d, EXPECTED_TARGET %d, RELEASE %d; want 0, 0 and at most 2", after[TriggerCallConflict], after[TriggerExpectedTarget], after[TriggerRelease]))
	}
	if self < uint64(before[TriggerCallConflict]) {
		problems = append(problems, fmt.Sprintf("%d self admissions below the %d CALL_CONFLICT recalls they replace", self, before[TriggerCallConflict]))
	}
	return problems
}

// TestZQ3SelfConflictKeepsPeerConflicts replays T1 with advance and a peer
// observation (m1's lease in flight across each of m0's creating CALLs)
// under both rules: the cross-domain conflict must start the same number of
// CALL_CONFLICT recalls, every one all-domain with both mounts as
// participants, and vj4 must count no self admission for them (m0's own
// lease is skipped, but m1's still conflicts, so the CALL is not admitted
// past it).
func TestZQ3SelfConflictKeepsPeerConflicts(t *testing.T) {
	t.Parallel()
	pre := &advanceReplay{r: preVJ4Rig(t), advance: true, peer: true}
	before := pre.run()
	vj4 := &advanceReplay{r: newZQ3Rig(t, 0), advance: true, peer: true}
	after := vj4.run()
	t.Logf("T1 with a peer observation: pre-vj4 %v; vj4 %v (%d self admissions)", before, after, vj4.r.selfAdmissions())
	if before[TriggerCallConflict] == 0 || after[TriggerCallConflict] != before[TriggerCallConflict] {
		t.Errorf("CALL_CONFLICT recalls: pre-vj4 %d, vj4 %d; want equal and some", before[TriggerCallConflict], after[TriggerCallConflict])
	}
	if n := vj4.r.selfAdmissions(); n != 0 {
		t.Errorf("vj4 counted %d self admissions although a peer lease conflicted each time", n)
	}
	for _, record := range history(t, vj4.r.controller).Recalls {
		if record.Kind == TriggerCallConflict && (record.Scope != "ALL" || len(record.Participants) != 2) {
			t.Errorf("cross-domain CALL_CONFLICT recall %s: scope %s participants %v, want ALL with both", record.Opcode, record.Scope, record.Participants)
		}
	}
}
