package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Session abandon of stranded claims (task hh4, a84 part 2).
//
// A blocked guest session (hfs_session_block) stops querying its uncertain
// claims and keeps them retained; a claim the host left RESERVED/UNPREPARED
// then stays charged against the provider's credit and pins the session's
// retirement floor for good. ABANDON is the guest's explicit, host-bound
// signal that it gives up on an exact list of its own claims: it promises
// never to send another CALL, QUERY or GAP_RESOLVE for them. The provider
// then declines every listed claim that was provably never prepared as a
// protected gap and returns its credit, in one ledger transaction, and
// answers each number with ABANDONED (and the gap proof the guest retires
// it with), KEPT (it may have had an effect; it stays retained), RETIRED_ID
// or UNKNOWN (never allocated; nothing to release). The design and the
// safety argument are in docs/design/hh4-abandon.md; the wire schema in
// docs/spec/Retained-9p-controlled.md.
//
// ABANDON is a controlled-only operation (frame minor >= 2). It is reserved
// control work (controlCommand), like GAP_RESOLVE, and like GAP_RESOLVE it
// is not a view operation, so it skips the view provider's request check
// (checkExecution): it takes no cut, installs nothing and only gives up
// authority the session already held.

// MaxAbandonClaims bounds one ABANDON's claim list. The guest batches longer
// lists; 64 numbers keep the request well inside one 64 KiB body.
const MaxAbandonClaims = 64

// ControlledAbandon declines a session's own never-prepared claims as gaps
// and returns their credit. Implementations require the private
// VerifiedRequest observation of the exact ABANDON command and live
// controlled enrollment, as ControlledRetirement does.
type ControlledAbandon interface {
	AbandonOriginal(context.Context, Context, json.RawMessage) (json.RawMessage, error)
}

// validateAbandon checks the closed ABANDON payload:
// {realm:uuid, subject:bytes(1024), anchor:uuid, nonce:uuid,
// claims:[{number:n64, original_hash:hash}]} with 1..MaxAbandonClaims
// claims in strictly ascending number order.
func validateAbandon(v any) error {
	o, err := object(v, "realm", "subject", "anchor", "nonce", "claims")
	if err != nil || !uuid(o["realm"]) || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !bytesField(o["subject"], 1024, true) {
		return fsapi.Invalid
	}
	claims, ok := o["claims"].([]any)
	if !ok || len(claims) == 0 || len(claims) > MaxAbandonClaims {
		return fsapi.Invalid
	}
	var last uint64
	for i, raw := range claims {
		claim, err := object(raw, "number", "original_hash")
		if err != nil || !uintRange(claim["number"], 1, ^uint64(0)) || !hash(claim["original_hash"]) {
			return fsapi.Invalid
		}
		n, _ := strconv.ParseUint(string(claim["number"].(json.Number)), 10, 64)
		if i > 0 && n <= last {
			return fsapi.Invalid
		}
		last = n
	}
	return nil
}

// validateAbandonResponse checks a provider reply against its request: the
// closed {claims:[{number, proof_hash, status}]} shape, one outcome per
// requested number in the same order, a proof exactly on ABANDONED.
func validateAbandonResponse(request, response json.RawMessage) error {
	var req struct {
		Claims []struct{ Number uint64 } `json:"claims"`
	}
	if json.Unmarshal(request, &req) != nil {
		return fsapi.Invalid
	}
	v, err := Canonical(response)
	if err != nil {
		return err
	}
	o, err := object(v, "claims")
	if err != nil {
		return fsapi.Invalid
	}
	outcomes, ok := o["claims"].([]any)
	if !ok || len(outcomes) != len(req.Claims) {
		return fsapi.Invalid
	}
	for i, raw := range outcomes {
		outcome, err := object(raw, "number", "proof_hash", "status")
		if err != nil || !uintRange(outcome["number"], req.Claims[i].Number, req.Claims[i].Number) {
			return fsapi.Invalid
		}
		status := text(outcome["status"])
		if !oneOf(status, "ABANDONED", "KEPT", "RETIRED_ID", "UNKNOWN") || (status == "ABANDONED") != hash(outcome["proof_hash"]) || status != "ABANDONED" && outcome["proof_hash"] != nil {
			return fsapi.Invalid
		}
	}
	return nil
}

// abandon runs one ABANDON: the controlled protocol and live profile and
// endpoint are rechecked on both providers (as for RETIRE), the claims
// provider runs the ledger transaction and its reply is checked against
// the request before it becomes the response payload.
func (s *Service) abandon(ctx context.Context, c Command) (any, error) {
	if c.Protocol == nil || !IsControlledProtocol(*c.Protocol) || s.config.Validation == nil {
		return nil, fsapi.Permission
	}
	abandoner, ok := s.config.Claims.(ControlledAbandon)
	if !ok {
		return nil, fsapi.Unsupported
	}
	for _, provider := range []any{s.config.Claims, s.config.Execution} {
		enrolled, ok := provider.(ValidationEnrollment)
		if !ok {
			return nil, fsapi.Permission
		}
		if err := enrolled.CheckValidation(ctx, c.Context, *s.config.Validation, s.config.Endpoint); err != nil {
			return nil, err
		}
	}
	reply, err := abandoner.AbandonOriginal(ctx, c.Context, bytes.Clone(c.Payload))
	if err != nil {
		return nil, err
	}
	if err = validateAbandonResponse(c.Payload, reply); err != nil {
		return nil, err
	}
	return json.RawMessage(reply), nil
}
