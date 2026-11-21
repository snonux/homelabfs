package controlledvalidation

import (
	"errors"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// Refusal trace (task a84, after zq3-r8).
//
// An uncertain refusal of a reserved claim (viewcontrol's WouldBlock while a
// recall is active, a full service queue, another operation's PENDING
// snapshot, ...) returns from execute as an error, and the bridge answers it
// with ADMITTED/OK and nothing else (guestbridge executeCall): the claim stays
// RESERVED/UNPREPARED and QUERY retries it. In zq3-r8 two such claims stayed
// stuck until the guest's 30 s QUERY window ran out, and nothing in the
// ledger said why. noteRefusal now records the last refusal class on the
// claim entry, and OBSERVE's claim summary (b84, evidence_claims.go) shows it.
//
// What keeps it a pure diagnostic:
//   - it is written only to an exact never-prepared claim (neverPrepared, not
//     a gap). A claim that reached PREPARING, PENDING or TERMINAL, or that a
//     Cancel or ResolveGap made a gap, is never touched, so a gap proof (the
//     hash of the entry) can never change after it was taken;
//   - no decision reads it: neverPrepared, loadExact, Cancel and ResolveGap
//     ignore it, and checkPlanCredit leaves it out of a plan's footprint;
//   - it is bounded: a class is a fixed stage plus an errno name, and one
//     claim carries one trace;
//   - it is rate-limited: while the class stays the same at most one write a
//     second (refusalTraceSeconds), so a QUERY loop polling every 10 ms does
//     not turn into a ledger fsync every 10 ms. A refusal that is not written
//     rolls its transaction back, which writes nothing;
//   - it is best effort: a failed write is dropped and never changes the
//     error execute returns. It is written under p.execution after checking
//     p.closed, like every other ledger write, so SuspendRestart's digest
//     still covers the whole ledger.

// The stages execute records a refusal for.
const (
	refusalLoad      = "LOAD"      // loadExact refused: another operation's PENDING snapshot holds the scope
	refusalAdmission = "ADMISSION" // the Admission controller refused (uncertain), or protecting a definite refusal failed
	refusalExecution = "EXECUTION" // the serialized worker failed before preparation began
)

// refusalTraceSeconds is the minimum time between two writes of the same
// class. A new class is always written.
const refusalTraceSeconds = 1

// refusalTrace is the last uncertain refusal of one never-prepared claim.
// Class is "<stage>:<errno>", for example "ADMISSION:WouldBlock". First and
// Last are Unix seconds of the first and the latest recorded refusal of that
// class. Count counts the recorded refusals of that class; with the rate
// limit it is a lower bound of the actual attempts. A new class restarts the
// trace.
type refusalTrace struct {
	Class       string
	Count       uint64
	First, Last int64
}

// errRefusalUnchanged rolls back a refusal transaction that has nothing to
// write (rate-limited, or the claim is no longer never prepared).
var errRefusalUnchanged = errors.New("refusal trace unchanged")

// refusalClass names a refusal of stage by the errno it wraps.
func refusalClass(stage string, err error) string {
	return stage + ":" + guestbridge.ErrnoName(err)
}

// noteRefusal records err as the last refusal of b's claim when the claim is
// still the exact never-prepared original. It is best effort and returns
// nothing: execute's answer is the same whether or not the trace was written.
func (p *Provider) noteRefusal(b guestbridge.Binding, stage string, err error) {
	if err == nil {
		return
	}
	class := refusalClass(stage, err)
	now := p.clock().Unix()
	p.execution.Lock()
	defer p.execution.Unlock()
	if p.closed.Load() {
		return
	}
	_ = p.updateOpen(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, b.Call.Claim)
		if err != nil || !ok || !sameExact(e.Binding, b) || e.Gap || !neverPrepared(e) {
			return errRefusalUnchanged
		}
		next, changed := observeRefusal(e.Refusal, class, now)
		if !changed {
			return errRefusalUnchanged
		}
		e.Refusal = next
		return p.putClaim(tx, claimKey(e.Claim), e)
	})
}

// observeRefusal returns the trace after one refusal of class at now, and
// whether it changed: a new class starts a fresh trace, the same class adds
// one to Count once refusalTraceSeconds have passed since Last.
func observeRefusal(old *refusalTrace, class string, now int64) (*refusalTrace, bool) {
	if old == nil || old.Class != class {
		return &refusalTrace{Class: class, Count: 1, First: now, Last: now}, true
	}
	if now-old.Last < refusalTraceSeconds {
		return old, false
	}
	next := *old
	next.Count++
	next.Last = now
	return &next, true
}

// clock is the refusal trace's time source.
func (p *Provider) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}
