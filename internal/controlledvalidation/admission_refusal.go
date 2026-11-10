package controlledvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// AdmissionRefusal is a DEFINITE no-effect refusal an Admission returns from
// Admit (task j64, after zq3-r7 R7-2). An Admission may return it only when
// both proof obligations hold, and must say why in Reason:
//
//  1. the refusal precedes every effect, dispatch and durable intent that
//     could later take effect for this binding (Admit runs before the claim
//     reaches PREPARING, so the provider has planned nothing); and
//  2. it is deterministic: no later state of the same enrollment could admit
//     the same binding, so re-running admission on replay would refuse again.
//
// The provider then protects a terminal failed result with Code, exactly the
// shape of a preparer refusal, so CALL stays ADMITTED/OK and QUERY returns
// TERMINAL instead of leaving the claim RESERVED/UNPREPARED. Every other Admit
// error (contention, WouldBlock, recovery, I/O, a re-checked ingress that
// changed) stays uncertain: the claim remains RESERVED and QUERY retries it.
type AdmissionRefusal struct {
	Code   fsapi.Errno
	Reason string
}

func (r *AdmissionRefusal) Error() string {
	return fmt.Sprintf("definite admission refusal %v: %s", r.Code, r.Reason)
}

// Unwrap keeps errors.Is(err, fsapi.NoSpace) and similar checks working.
func (r *AdmissionRefusal) Unwrap() error { return r.Code }

// definiteRefusal reports the protected code of a definite Admit refusal.
// fsapi.OK is never a refusal and is ignored.
func definiteRefusal(err error) (fsapi.Errno, bool) {
	var refusal *AdmissionRefusal
	if !errors.As(err, &refusal) || refusal.Code == fsapi.OK {
		return 0, false
	}
	return refusal.Code, true
}

// refuseUnprepared turns a definite Admit refusal into the same protected
// terminal record a preparer refusal produces (execute's plan == nil path):
// a failed ControlledPlan, its planned outcome, the bridge's proposed result
// and snapshot, then Outcome/Result/TERMINAL. It writes all of that in ONE
// ledger transaction, so a crash leaves either the untouched RESERVED/
// UNPREPARED claim (whose replay re-runs the deterministic admission and
// refuses again) or the complete terminal record; never a half-protected plan.
// No cut is taken and ApplyControlled never runs: a failed plan without a
// command has no filesystem effect to apply.
func (p *Provider) refuseUnprepared(b guestbridge.Binding, code fsapi.Errno, protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)) (guestbridge.Outcome, error) {
	p.execution.Lock()
	defer p.execution.Unlock()
	saved, err := p.loadExact(b, false)
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	if saved.Outcome != nil {
		return *saved.Outcome, nil
	}
	if !neverPrepared(saved) {
		// Something else already began preparing this claim; its own
		// protected path, not this refusal, owns the answer.
		return guestbridge.Outcome{}, fsapi.WouldBlock
	}
	plan := fsservice.ControlledPlan{Operation: b.Operation, Result: fsapi.Result{Code: code}}
	// plannedOutcome of a command-less failed plan reads nothing from a cut.
	out := guestbridge.Outcome{Result: plan.Result, Publication: json.RawMessage("null")}
	proposed, bridge, err := protect(out)
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	if !controlledResult(proposed) || !json.Valid(bridge) {
		return guestbridge.Outcome{}, fsapi.Invalid
	}
	err = p.updateOpen(func(tx *bolt.Tx) error {
		return p.protectRefusal(tx, b, plan, out, proposed, bridge)
	})
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	return out, nil
}

// protectRefusal is refuseUnprepared's single transaction. It re-checks that
// the claim is still exactly the never-prepared original, charges the same
// retained/source credit bound as a protected plan, and records the stage
// ADMISSION_REFUSED (any stage other than UNPREPARED already keeps GAP_RESOLVE
// and DECLINE from rewriting the entry into a gap).
func (p *Provider) protectRefusal(tx *bolt.Tx, b guestbridge.Binding, plan fsservice.ControlledPlan, out guestbridge.Outcome, proposed, bridge json.RawMessage) error {
	e, ok, err := p.load(tx, b.Call.Claim)
	if err != nil {
		return err
	}
	if !ok || !sameExact(e.Binding, b) || e.Gap || !neverPrepared(e) {
		return fsapi.Stale
	}
	version, err := p.bucket(tx, "bridge").NextSequence()
	if err != nil {
		return err
	}
	e.PreparationStage = "ADMISSION_REFUSED"
	e.SnapshotVersion = version
	e.Plan = &plan
	e.PlannedOutcome = &out
	e.ProposedResult = proposed
	e.BridgeState = bridge
	// The same footprint bound protectPlan applies to a protected plan. A
	// failure aborts the whole transaction: the claim stays never-prepared.
	if err = checkPlanCredit(e, plan, out); err != nil {
		return err
	}
	e.Outcome = &out
	e.Result = append(json.RawMessage(nil), proposed...)
	e.Status = "TERMINAL"
	if err = p.protectSnapshot(tx, e); err != nil {
		return err
	}
	return put(p.bucket(tx, "claims"), claimKey(e.Claim), e)
}

// neverPrepared is the exact RESERVED/UNPREPARED original with no protected
// plan, outcome, result or bridge snapshot: the only state a definite
// admission refusal may close.
func neverPrepared(e entry) bool {
	return e.Status == "RESERVED" && e.PreparationStage == "UNPREPARED" && e.Plan == nil && e.Outcome == nil &&
		e.PlannedOutcome == nil && len(e.ProposedResult) == 0 && len(e.Result) == 0 && len(e.BridgeState) == 0 && e.SnapshotVersion == 0
}

// admit runs the Admission (when bound). A definite refusal is answered by
// refuseUnprepared when protect can project the bridge result; refused is
// then true and out/err are that answer. Without protect (the non-validation
// Execute seam) the bridge would have to finalize the result itself, which it
// only does for prepared outcomes, so the refusal stays an uncertain error.
func (p *Provider) admit(ctx context.Context, b guestbridge.Binding, protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)) (admission Admitted, out guestbridge.Outcome, refused bool, err error) {
	controller := p.currentAdmission()
	if controller == nil {
		return nil, out, false, nil
	}
	admission, err = controller.Admit(ctx, b)
	if err != nil {
		if code, ok := definiteRefusal(err); ok && protect != nil {
			out, err = p.refuseUnprepared(b, code, protect)
			return nil, out, true, err
		}
		return nil, out, false, err
	}
	if nilValue(admission) {
		return nil, out, false, fsapi.Permission
	}
	return admission, out, false, nil
}
