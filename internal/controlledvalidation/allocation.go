package controlledvalidation

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

var _ guestbridge.ControlledAllocation = (*Provider)(nil)

// AllocateOriginal protects the exact original request atomically with issuance.
// Only the bridge's private authenticated controlled observation can invoke it.
func (p *Provider) AllocateOriginal(ctx context.Context, scope guestbridge.Context, call guestbridge.Call) error {
	b, err := p.allocationOriginal(ctx, scope, call, "ALLOCATE")
	if err != nil {
		return err
	}
	return p.allocate(call.Claim, &b)
}

// ResolveOriginalGap closes only an exact never-prepared ordinary allocation.
// Dirty earmarks use the controller's existing typed CANCELLED-ticket closure.
func (p *Provider) ResolveOriginalGap(ctx context.Context, scope guestbridge.Context, call guestbridge.Call) (string, error) {
	b, err := p.allocationOriginal(ctx, scope, call, "GAP_RESOLVE")
	if err != nil {
		return "", err
	}
	err = p.db.View(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, call.Claim)
		if err != nil {
			return err
		}
		if !ok || e.Original == nil || !sameExact(*e.Original, b) {
			return fsapi.Permission
		}
		// Even a failed preparation has uncertain effects; never infer absence.
		if e.PreparationStage != "" && e.PreparationStage != "UNPREPARED" {
			return fsapi.Unavailable
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return p.ResolveGap(ctx, scope, call.Claim)
}

// protectedGapQuery exposes the definite protected no-effect state only to its
// actual authenticated mapped QUERY. It supplies no retirement/destruction right.
// Common direct/legacy/CANCEL lookups keep their conservative state projection.
func (p *Provider) protectedGapQuery(ctx context.Context, scope guestbridge.Context, e entry) bool {
	selected := p.config.Authorization.Enrollment.Protocol
	if selected == nil || *selected != guestbridge.MappedProtocol() || !e.Gap || e.Status != "DECLINED" ||
		e.Original == nil || e.Original.Context != scope || e.Original.Call.Claim != e.Claim ||
		e.Plan != nil || e.Outcome != nil || len(e.Result) != 0 || len(e.ProposedResult) != 0 ||
		(e.PreparationStage != "" && e.PreparationStage != "UNPREPARED") {
		return false
	}
	observed, ok := guestbridge.VerifiedRequest(ctx)
	raw, err := canonicalJSON(map[string]any{"claim": e.Claim})
	return err == nil && ok && observed.Scope == scope && observed.Operation == "QUERY" &&
		observed.Protocol != nil && *observed.Protocol == *selected && observed.PayloadHash == hash(raw) &&
		observed.Native.Endpoint == p.config.Authorization.Enrollment.Endpoint
}

// CheckBridgeProtocol refuses using an explicitly enrolled successor with any
// other bridge tuple, including the legacy default.
func (p *Provider) CheckBridgeProtocol(ctx context.Context, scope guestbridge.Context, selected *guestbridge.Protocol) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	enrolled := p.config.Authorization.Enrollment.Protocol
	if enrolled != nil {
		if selected == nil || *selected != *enrolled {
			return fsapi.Permission
		}
		return nil
	}
	if selected != nil && guestbridge.IsControlledProtocol(*selected) {
		return fsapi.Permission
	}
	return nil
}

func (p *Provider) allocationOriginal(ctx context.Context, scope guestbridge.Context, call guestbridge.Call, op string) (guestbridge.Binding, error) {
	if err := p.Check(ctx, scope); err != nil {
		return guestbridge.Binding{}, err
	}
	observed, ok := guestbridge.VerifiedRequest(ctx)
	raw, err := canonicalCall(call)
	if err != nil {
		return guestbridge.Binding{}, err
	}
	enrolled := p.config.Authorization.Enrollment.Protocol
	if enrolled == nil || !guestbridge.IsControlledProtocol(*enrolled) {
		return guestbridge.Binding{}, fsapi.Permission
	}
	protocol := *enrolled
	if !ok || observed.Scope != scope || observed.Operation != op || observed.Protocol == nil || *observed.Protocol != protocol || observed.PayloadHash != hash(raw) || observed.Native.Endpoint != p.config.Authorization.Enrollment.Endpoint {
		return guestbridge.Binding{}, fsapi.Permission
	}
	// Decode the same closed union independently before touching the ledger.
	envelope, err := canonicalJSON(guestbridge.Command{Protocol: &protocol, Cell: observed.Cell, Context: scope, Op: op, Payload: json.RawMessage(raw)})
	if err != nil {
		return guestbridge.Binding{}, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, uint16(protocol.BridgeMinor)); err != nil {
		return guestbridge.Binding{}, err
	}
	return guestbridge.Binding{Context: scope, Call: call, Canonical: raw}, nil
}
