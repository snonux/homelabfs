package controlledvalidation

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// Session abandon (task hh4, a84 part 2; docs/design/hh4-abandon.md).
//
// A guest that blocked its session sends one ABANDON naming the exact claims
// it gives up on. Every listed claim that is provably never prepared is
// declined as a protected gap and its retained and source credit returned
// to the budgets, all in one bbolt transaction under p.execution:
//
//   - abandonable (abandonable): an AllocateOriginal number whose CALL has
//     not reserved it (ALLOCATED, original credit preowned), or the exact
//     never-prepared RESERVED/UNPREPARED claim (neverPrepared). No plan was
//     protected for either, so no preparer ran and no effect can exist.
//     The entry becomes Gap/DECLINED with Abandoned set and both credits
//     zero, so a later Retire's recycle returns nothing (no double release);
//   - an existing proven gap (GAP_RESOLVE, Cancel or an earlier ABANDON) is
//     answered ABANDONED with its current proof and is never rewritten: the
//     guest may already hold that proof, and Retire checks the entry hash.
//     Its credit returns at recycle, as before;
//   - everything else is KEPT: PREPARING (the preparer may have run),
//     PENDING, TERMINAL (including a definite admission refusal), a claim
//     bound to an open dirty earmark (a DRAIN_WRITE byte obligation), and a
//     number that has only a dirty earmark and no claim entry (allocated by
//     EarmarkNumber, holding earmark credit; its closure is the controller's
//     CANCELLED-ticket path, never ABANDON);
//   - a number at or below the floor is RETIRED_ID and a number with neither
//     an entry nor an earmark UNKNOWN; neither has anything to release.
//
// For a number with a claim entry, the identity checks come first: an entry
// not bound to this exact session scope, or named with a different original
// hash, refuses the whole command (Permission or Stale) and the transaction
// writes nothing, so a forged or stale list cannot partly apply. A retired
// number (its entry may be recycled) and an earmark-only number have no
// entry to check and are answered RETIRED_ID and KEPT.
//
// Linearizability. Every writer that moves a never-prepared claim on holds
// p.execution and rechecks the claim inside its own transaction:
// executeAdmitted's PREPARING write (the only way into the preparer, for a
// CALL and for a QUERY resume alike), refuseUnprepared and noteRefusal; and
// loadExact refuses a gap. The abandon holds p.execution too, so it is
// ordered against each of them: either the claim was already PREPARING
// (KEPT) or the later writer finds the gap and refuses (Stale). Reserve and
// Allocate take no mutex but are bbolt writers, which are serialized: a
// CALL that reserves after the abandon finds the gap (Permission). A
// replayed ALLOCATE of an abandoned number answers its protected
// acknowledgement again and writes nothing (as after GAP_RESOLVE); its CALL
// then finds the gap.

// abandonedCounters is the root key of the lifetime ABANDON counters OBSERVE
// reports (ClaimEvidence.Abandoned, AbandonedCredit). Diagnostics only.
const abandonedCounters = "abandoned"

// abandonFailpoint fails the ABANDON transaction after every claim was
// rewritten and before commit: component tests use it as a crash point.
const abandonFailpoint = "abandon-before-commit"

type abandonRequest struct {
	Realm   string         `json:"realm"`
	Subject string         `json:"subject"`
	Anchor  string         `json:"anchor"`
	Nonce   string         `json:"nonce"`
	Claims  []abandonClaim `json:"claims"`
}

type abandonClaim struct {
	Number       uint64 `json:"number"`
	OriginalHash string `json:"original_hash"`
}

// abandonOutcome is one number's answer; ProofHash only on ABANDONED.
type abandonOutcome struct {
	Number    uint64  `json:"number"`
	ProofHash *string `json:"proof_hash"`
	Status    string  `json:"status"`
}

// abandonTotals counts what one transaction released, for the counters.
type abandonTotals struct {
	Claims uint64
	Credit uint64
}

var _ guestbridge.ControlledAbandon = (*Provider)(nil)

// AbandonOriginal authenticates the exact observed ABANDON of this scope
// (the RetireOriginal checks: enrolled controlled protocol, payload hash,
// endpoint, independently decoded closed union) and runs the abandon.
func (p *Provider) AbandonOriginal(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	enrolled := p.config.Authorization.Enrollment
	if enrolled.Protocol == nil || !guestbridge.IsControlledProtocol(*enrolled.Protocol) {
		return nil, fsapi.Permission
	}
	protocol := *enrolled.Protocol
	observed, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || observed.Scope != scope || observed.Operation != "ABANDON" || observed.Protocol == nil || *observed.Protocol != protocol || observed.PayloadHash != hash(raw) || observed.Native.Endpoint != enrolled.Endpoint {
		return nil, fsapi.Permission
	}
	envelope, err := canonicalJSON(guestbridge.Command{Protocol: &protocol, Cell: observed.Cell, Context: scope, Op: "ABANDON", Payload: raw})
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, uint16(protocol.BridgeMinor)); err != nil {
		return nil, err
	}
	var request abandonRequest
	if err = json.Unmarshal(raw, &request); err != nil {
		return nil, fsapi.Invalid
	}
	outcomes, err := p.abandon(scope, request)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"claims": outcomes})
}

// abandon is the single ledger transaction. A replay of the same request
// rewrites nothing (every abandoned claim is now a proven gap) and answers
// the same outcomes, so it releases no credit twice.
func (p *Provider) abandon(scope guestbridge.Context, request abandonRequest) ([]abandonOutcome, error) {
	p.execution.Lock()
	defer p.execution.Unlock()
	var outcomes []abandonOutcome
	err := p.updateOpen(func(tx *bolt.Tx) error {
		outcomes = make([]abandonOutcome, 0, len(request.Claims))
		var totals abandonTotals
		for _, named := range request.Claims {
			claim := guestbridge.Claim{Realm: request.Realm, Subject: request.Subject, Anchor: request.Anchor, Nonce: request.Nonce, Number: named.Number, OriginalHash: named.OriginalHash}
			outcome, err := p.abandonOne(tx, scope, claim, &totals)
			if err != nil {
				return err
			}
			outcomes = append(outcomes, outcome)
		}
		if err := p.fail(abandonFailpoint); err != nil {
			return err
		}
		return p.countAbandoned(tx, totals)
	})
	return outcomes, err
}

// abandonOne classifies one named claim and, when abandonable, declines it
// and releases its credit into totals (see the file comment).
func (p *Provider) abandonOne(tx *bolt.Tx, scope guestbridge.Context, claim guestbridge.Claim, totals *abandonTotals) (abandonOutcome, error) {
	outcome := abandonOutcome{Number: claim.Number, Status: "KEPT"}
	if retired, err := p.retired(tx, claim); err != nil || retired {
		outcome.Status = "RETIRED_ID"
		return outcome, err
	}
	e, ok, err := p.load(tx, claim)
	if err != nil {
		return outcome, err
	}
	if !ok {
		if p.bucket(tx, "earmarks").Get(claimKey(claim)) == nil {
			outcome.Status = "UNKNOWN"
		}
		return outcome, nil
	}
	if e.Claim != claim {
		return outcome, fsapi.Stale
	}
	if owner := claimOwner(e); owner.Context != scope {
		return outcome, fsapi.Permission
	}
	if open, err := p.openEarmark(tx, claim); err != nil || open {
		return outcome, err
	}
	switch {
	case provenGap(e):
	case abandonable(e):
		if err = p.declineAbandoned(tx, &e, totals); err != nil {
			return outcome, err
		}
	default:
		return outcome, nil
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return outcome, err
	}
	proof := hash(raw)
	outcome.Status, outcome.ProofHash = "ABANDONED", &proof
	return outcome, nil
}

// declineAbandoned turns an abandonable entry into a gap with zero credit
// and returns its retained and source credit to the budgets.
func (p *Provider) declineAbandoned(tx *bolt.Tx, e *entry, totals *abandonTotals) error {
	retained, source := e.RetainedCredit, e.SourceCredit
	if err := p.releaseCredit(tx, "retained-credit", retained); err != nil {
		return err
	}
	if err := p.releaseCredit(tx, "source-credit", source); err != nil {
		return err
	}
	e.Gap, e.Status, e.Abandoned = true, "DECLINED", true
	e.RetainedCredit, e.SourceCredit = 0, 0
	totals.Claims++
	totals.Credit = saturatingAdd(totals.Credit, saturatingAdd(retained, source))
	return p.putClaim(tx, claimKey(e.Claim), *e)
}

// openEarmark reports whether claim is bound to a dirty earmark that is not
// closed: its DRAIN_WRITE is an immutable byte obligation (dh4), never
// abandoned. Cancel and ResolveGap refuse the same state with WouldBlock.
func (p *Provider) openEarmark(tx *bolt.Tx, claim guestbridge.Claim) (bool, error) {
	raw := p.bucket(tx, "earmarks").Get(claimKey(claim))
	if raw == nil {
		return false, nil
	}
	var earmark Earmark
	if err := json.Unmarshal(raw, &earmark); err != nil {
		return false, err
	}
	return earmark.Bound != nil && !earmark.Closed, nil
}

// claimOwner is the binding that names the claim's session: the protected
// original of an AllocateOriginal number, else the reserved binding. A
// legacy local allocation has neither, so its zero context never matches.
func claimOwner(e entry) guestbridge.Binding {
	if e.Original != nil {
		return *e.Original
	}
	return e.Binding
}

// provenGap is a declined gap with nothing planned, applied or answered
// whose preparation never began: the never-prepared state abandonable
// requires, so ABANDONED always means provably never prepared. Cancel
// refuses a PREPARING original, but a legacy Cancel can still decline a
// non-original PREPARING claim; such a gap is KEPT here, whoever owns it.
func provenGap(e entry) bool {
	unprepared := e.PreparationStage == "" || e.PreparationStage == "UNPREPARED"
	return e.Gap && e.Status == "DECLINED" && e.Plan == nil && e.Outcome == nil && len(e.Result) == 0 && unprepared
}

// abandonable is a claim no preparer can have touched: an AllocateOriginal
// number not yet reserved by its CALL, or the never-prepared RESERVED entry.
func abandonable(e entry) bool {
	if e.Gap || e.Original == nil {
		return false
	}
	allocated := e.Status == "ALLOCATED" && (e.PreparationStage == "" || e.PreparationStage == "UNPREPARED") &&
		e.Plan == nil && e.Outcome == nil && len(e.Result) == 0
	return allocated || neverPrepared(e)
}

// countAbandoned adds one transaction's totals to the lifetime counters.
func (p *Provider) countAbandoned(tx *bolt.Tx, totals abandonTotals) error {
	if totals.Claims == 0 {
		return nil
	}
	counters, err := p.abandonedTotals(tx)
	if err != nil {
		return err
	}
	counters.Claims = saturatingAdd(counters.Claims, totals.Claims)
	counters.Credit = saturatingAdd(counters.Credit, totals.Credit)
	return put(tx.Bucket([]byte(ledgerVersion)), []byte(abandonedCounters), counters)
}

// abandonedTotals reads the lifetime counters (zero before any abandon).
func (p *Provider) abandonedTotals(tx *bolt.Tx) (abandonTotals, error) {
	var counters abandonTotals
	raw := tx.Bucket([]byte(ledgerVersion)).Get([]byte(abandonedCounters))
	if raw == nil {
		return counters, nil
	}
	if err := json.Unmarshal(raw, &counters); err != nil {
		return counters, fsapi.Stale
	}
	return counters, nil
}

func saturatingAdd(a, b uint64) uint64 {
	if a > ^uint64(0)-b {
		return ^uint64(0)
	}
	return a + b
}
