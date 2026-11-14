package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sync/atomic"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// QUERY continuation (task 614, generalised to every opcode by task o84).
//
// A claim whose first Admit failed uncertainly (WouldBlock behind a recall,
// a lost TryLock, ...) stays RESERVED/UNPREPARED and is answered ADMITTED.
// The guest then only QUERYs it; guestbridge resumes it under that outer
// QUERY (recoverValidation -> ResumeValidation -> Admit -> CheckRequest ->
// Facts.Authorize). Facts.Authorize binds every check to the actual
// authenticated request, whose operation and payload are the QUERY's, not the
// original CALL's, so the inner CALL check needs this token.
//
// What the token proves, together with the checks Facts keeps on the outer
// observation (protocol, scope, actual native process and controller):
//
//   - the guest session that owns the claim is asking about exactly that
//     claim now: a verified outer QUERY of the claim's own scope in the
//     enrolled terminal protocol, whose payload is exactly {claim};
//   - the binding being authorized is the provider's own exact original
//     RESERVED/UNPREPARED entry, whose canonical bytes (intent, credentials,
//     claim) already passed Facts.Authorize as an actual guest CALL before
//     Reserve, and which has no protected plan, outcome or effect;
//   - the token lives only for this one execute invocation of this provider,
//     service and Facts, and authorizes nothing but that exact CALL.
//
// Why every opcode may resume (614 had restricted it to DESCRIPTION_RELEASE
// and FORGET only because its dirty-session closure needed no more; no
// per-opcode hazard was recorded, and zq3-r8/r9 parked a PROCESS_EXIT and a
// GETATTR behind exactly that restriction):
//
//   - a repeated identical CALL already resumes the same entry for every
//     opcode (guestbridge call -> recoverValidation), because its outer
//     observation is that CALL; the QUERY path executes nothing a repeated
//     CALL could not;
//   - someone else's claim cannot be named: claims resolve per scope, the
//     outer scope must equal the binding's, reconciled bindings never resume
//     (recoverValidation) and the QUERY payload hash binds the full claim;
//   - a claim of an exited process, or whose credentials changed, is not
//     replayed blindly: the bridge's preparer re-runs the credential check
//     (attestor, exact credential bytes, exit) and the service's Unix policy
//     against the current state, so such a resume protects a TERMINAL failure
//     (e.g. STALE) instead of an effect. Only the cleanup opcodes and
//     DRAIN_WRITE tolerate an exited issuer, exactly as on first dispatch;
//   - Admit itself runs again in full (terminal seal, readiness, recall
//     barrier, drain validation, source floor), so a resumed op meets the
//     same admission as a first one; a definite refusal (j64) still becomes
//     TERMINAL and anything uncertain stays RESERVED for the next QUERY.
//
// Older protocols keep their unchanged behaviour: only the terminal protocol
// (the one the controlled runtime enrolls) mints the token.
type queryContinuationKey struct{}
type queryContinuation struct {
	provider *Provider
	outer    guestbridge.CommandObservation
	binding  guestbridge.Binding
	live     *atomic.Bool
}

// VerifyQueryContinuation accepts only a synchronous retry borrowed from the
// actual provider's exact never-planned entry. The outer QUERY observation
// remains unchanged; this grants no source or physical authority.
func VerifyQueryContinuation(ctx context.Context, service *fsservice.Service, facts Facts, scope guestbridge.Context, operation string, raw json.RawMessage) error {
	owned, ok := ctx.Value(queryContinuationKey{}).(queryContinuation)
	outer, observed := guestbridge.VerifiedRequest(ctx)
	if !ok || !observed || owned.live == nil || !owned.live.Load() || owned.provider == nil ||
		!owned.provider.UsesService(service) || !owned.provider.UsesFacts(facts) ||
		operation != "CALL" || scope != owned.binding.Context || !bytes.Equal(raw, owned.binding.Canonical) ||
		!reflect.DeepEqual(outer, owned.outer) || outer.Scope != scope || outer.Operation != "QUERY" {
		return fsapi.Permission
	}
	return nil
}

// queryContinuationContext attaches the token for one resume. It is called
// only after execute's locked exact-entry/UNPREPARED checks. No public
// constructor can attach this token, and it expires with that one invocation.
// When any check fails it returns ctx unchanged, so the inner CALL check
// refuses as before.
func (p *Provider) queryContinuationContext(ctx context.Context, b guestbridge.Binding, saved entry, live *atomic.Bool) context.Context {
	outer, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || !p.terminalQuery(outer, b) || !exactUnprepared(saved, b) {
		return ctx
	}
	query, err := canonicalJSON(struct {
		Claim guestbridge.Claim `json:"claim"`
	}{b.Call.Claim})
	if err != nil || outer.PayloadHash != hash(query) {
		return ctx
	}
	return context.WithValue(ctx, queryContinuationKey{}, queryContinuation{p, outer, b, live})
}

// terminalQuery: the outer request is a QUERY of the binding's own scope in
// the enrolled terminal protocol.
func (p *Provider) terminalQuery(outer guestbridge.CommandObservation, b guestbridge.Binding) bool {
	selected := p.config.Authorization.Enrollment.Protocol
	return selected != nil && *selected == guestbridge.TerminalProtocol() &&
		outer.Protocol != nil && *outer.Protocol == *selected && outer.Scope == b.Context && outer.Operation == "QUERY"
}

// exactUnprepared: saved is the exact original binding b, still RESERVED and
// never prepared.
func exactUnprepared(saved entry, b guestbridge.Binding) bool {
	return saved.Original != nil && sameBinding(*saved.Original, b) && saved.Status == "RESERVED" &&
		saved.PreparationStage == "UNPREPARED" && sameExact(saved.Binding, b)
}
