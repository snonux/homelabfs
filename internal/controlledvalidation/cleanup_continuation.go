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

type cleanupContinuationKey struct{}
type cleanupContinuation struct {
	provider *Provider
	outer    guestbridge.CommandObservation
	binding  guestbridge.Binding
	live     *atomic.Bool
}

// VerifyCleanupQueryContinuation accepts only a synchronous retry borrowed from
// the actual provider's exact never-planned cleanup entry. The outer QUERY
// observation remains unchanged; this grants no source or physical authority.
func VerifyCleanupQueryContinuation(ctx context.Context, service *fsservice.Service, facts Facts, scope guestbridge.Context, operation string, raw json.RawMessage) error {
	owned, ok := ctx.Value(cleanupContinuationKey{}).(cleanupContinuation)
	outer, observed := guestbridge.VerifiedRequest(ctx)
	if !ok || !observed || owned.live == nil || !owned.live.Load() || owned.provider == nil ||
		!owned.provider.UsesService(service) || !owned.provider.UsesFacts(facts) ||
		operation != "CALL" || scope != owned.binding.Context || !bytes.Equal(raw, owned.binding.Canonical) ||
		!reflect.DeepEqual(outer, owned.outer) || outer.Scope != scope || outer.Operation != "QUERY" {
		return fsapi.Permission
	}
	return nil
}

// Called only after execute's locked exact-entry/UNPREPARED checks. No public
// constructor can attach this token, and it expires with that one invocation.
func (p *Provider) cleanupQueryContext(ctx context.Context, b guestbridge.Binding, saved entry, live *atomic.Bool) context.Context {
	outer, ok := guestbridge.VerifiedRequest(ctx)
	selected := p.config.Authorization.Enrollment.Protocol
	if !ok || selected == nil || *selected != guestbridge.TerminalProtocol() ||
		outer.Protocol == nil || *outer.Protocol != *selected || outer.Scope != b.Context || outer.Operation != "QUERY" ||
		saved.Original == nil || !sameBinding(*saved.Original, b) || saved.Status != "RESERVED" || saved.PreparationStage != "UNPREPARED" || !sameExact(saved.Binding, b) ||
		(b.Call.Intent.Opcode != "DESCRIPTION_RELEASE" && b.Call.Intent.Opcode != "FORGET") {
		return ctx
	}
	query, err := canonicalJSON(struct {
		Claim guestbridge.Claim `json:"claim"`
	}{b.Call.Claim})
	if err != nil || outer.PayloadHash != hash(query) {
		return ctx
	}
	return context.WithValue(ctx, cleanupContinuationKey{}, cleanupContinuation{p, outer, b, live})
}
