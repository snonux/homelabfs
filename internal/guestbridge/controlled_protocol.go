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
	if err := s.credentials(ctx, c.Context, call.Credentials, c.Op == "GAP_RESOLVE"); err != nil {
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
