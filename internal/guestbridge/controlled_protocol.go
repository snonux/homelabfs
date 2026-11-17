package guestbridge

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// ControlledVersion is the controlled-only 9p version token. Both forwarding
// legs must select it explicitly; a tuple or HELLO never changes enrollment.
const ControlledVersion = "9P2000.L.hfs1.2"

// ControlledAllocation binds ordinary allocation/gap control to the exact
// original scope, full credentials and intent, in the real private ledger.
// Implementations require the private VerifiedRequest observation as well as
// live controlled enrollment. CALL is deliberately absent from this interface.
type ControlledAllocation interface {
	AllocateOriginal(context.Context, Context, Call) error
	ResolveOriginalGap(context.Context, Context, Call) (string, error)
}

// ControlledRetirement authenticates the exact original retirement credentials
// against protected ordinary/dirty bindings before advancing any permanent floor.
type ControlledRetirement interface {
	RetireOriginal(context.Context, Context, json.RawMessage) (json.RawMessage, error)
}

// ControlledProtocol returns the closed controlled allocation/view tuple.
func ControlledProtocol() Protocol { return Protocol{3, 1, 2, 4095, 65536, 3, 0} }

func supportedProtocol(c Config, p Protocol) bool {
	if p == ViewProtocol() {
		return true
	}
	if !IsControlledProtocol(p) || c.Validation == nil || !c.Validation.Disposable {
		return false
	}
	_, allocation := c.Claims.(ControlledAllocation)
	_, retirement := c.Claims.(ControlledRetirement)
	_, enrolled := c.Claims.(ValidationEnrollment)
	_, execution := c.Execution.(ValidationEnrollment)
	_, cleanup := c.Claims.(ReaddirCleanupStore)
	return allocation && retirement && enrolled && execution && (!HasCleanupProtocol(p) || cleanup)
}

func isAllocation(op string) bool { return op == "ALLOCATE" || op == "GAP_RESOLVE" }

func (s *Service) allocation(ctx context.Context, c Command) (any, error) {
	if c.Protocol == nil || !IsControlledProtocol(*c.Protocol) || s.config.Validation == nil {
		return nil, fsapi.Permission
	}
	allocator, ok := s.config.Claims.(ControlledAllocation)
	if !ok {
		return nil, fsapi.Permission
	}
	// Recheck actual live profile/endpoint, not merely the constructor's receipt.
	for _, provider := range []any{s.config.Claims, s.config.Execution} {
		enrolled, ok := provider.(ValidationEnrollment)
		if !ok {
			return nil, fsapi.Permission
		}
		if err := enrolled.CheckValidation(ctx, c.Context, *s.config.Validation, s.config.Endpoint); err != nil {
			return nil, err
		}
	}
	var call Call
	if err := json.Unmarshal(c.Payload, &call); err != nil {
		return nil, fsapi.Invalid
	}
	if err := s.credentials(ctx, c.Context, call.Credentials, s.allocationToleratesExited(c, call)); err != nil {
		return nil, err
	}
	if c.Op == "ALLOCATE" {
		if err := allocator.AllocateOriginal(ctx, c.Context, call); err != nil {
			return nil, err
		}
		return map[string]any{"claim": call.Claim, "status": "ALLOCATED"}, nil
	}
	proof, err := allocator.ResolveOriginalGap(ctx, c.Context, call)
	if err != nil {
		return nil, err
	}
	if !hash(proof) {
		return nil, fsapi.Invalid
	}
	return map[string]any{"claim": call.Claim, "proof_hash": proof, "status": "GAP"}, nil
}

// allocationToleratesExited reports whether an issuer whose PROCESS_EXIT already
// completed may still pass the credential check of this ALLOCATE/GAP_RESOLVE.
// It never admits a fresh credential revision (admitCredentials refuses an
// unknown revision of an exited process unconditionally); it only lets the
// exact bytes already bound for that revision through. Scope, enrollment,
// attestation, the provider's exact original binding and claim budgets are
// checked as for a live issuer.
//
//   - GAP_RESOLVE only closes an exact never-prepared original as no-effect.
//   - ALLOCATE is tolerated exactly for the CALL path's originalCleanup shapes
//     (DESCRIPTION_RELEASE of a live DESCRIPTION grant, FORGET count 1 of a live
//     PATH grant, both bound to these exact credentials). Task 194: the guest's
//     final release can follow its issuer's completed PROCESS_EXIT (an fd
//     inherited by a longer-lived child, or the exit race where PROCESS_EXIT's
//     CALL ran before the release allocated). CALL and the private preparer
//     already accepted that release, but ALLOCATE refused it, so the claim was
//     never allocated and the guest blocked its session. The predicate is
//     evaluated at ALLOCATE time only; CALL and the preparer recheck it, so a
//     grant released in between still refuses the CALL (STALE) and the claim
//     is then closed by GAP_RESOLVE.
//
// Exited cleanup gets no budget exemption and no per-grant cap: it consumes
// ordinary claim numbers and credits like any allocation, and a live process
// could already spend claims on repeated releases of one grant (only the first
// CALL can succeed; the rest refuse STALE and are gap-resolved).
//
// Everything else stays refused for an exited issuer: such a process may only
// give up authority it already held, never cause a new effect. DRAIN_WRITE, the
// other opcode CALL tolerates, never arrives here: its claim comes from the
// dirty earmark (DIRTY_RESERVE + BindFrozen) and the codec refuses it at
// ALLOCATE. DESCRIPTION_CLOSE is refused on every path: its only extra effect,
// releasing the process's fcntl locks, was already performed by the completed
// PROCESS_EXIT for the same per-process lock owner.
func (s *Service) allocationToleratesExited(c Command, call Call) bool {
	if c.Op == "GAP_RESOLVE" {
		return true
	}
	return s.originalCleanup(Binding{Context: c.Context, Call: call})
}

func validateControlledRetirement(v any) error {
	o, err := object(v, "realm", "subject", "anchor", "nonce", "through", "result_hashes", "credentials")
	if err != nil || validateCred(o["credentials"]) != nil {
		return fsapi.Invalid
	}
	legacy := map[string]any{}
	for k, value := range o {
		if k != "credentials" {
			legacy[k] = value
		}
	}
	return validatePayload("RETIRE", legacy, nil)
}

func (s *Service) retireControlled(ctx context.Context, c Command) (json.RawMessage, error) {
	allocator, ok := s.config.Claims.(ControlledRetirement)
	if !ok || s.config.Validation == nil {
		return nil, fsapi.Permission
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
	var request struct{ Credentials Credentials }
	if json.Unmarshal(c.Payload, &request) != nil {
		return nil, fsapi.Invalid
	}
	if err := s.credentials(ctx, c.Context, request.Credentials, true); err != nil {
		return nil, err
	}
	return allocator.RetireOriginal(ctx, c.Context, c.Payload)
}
