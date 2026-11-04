package viewcontrol

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

type physicalContinuationKey struct{}
type physicalContinuation struct {
	controller *Controller
	outer      guestbridge.CommandObservation
	scope      guestbridge.Context
	operation  string
	payload    json.RawMessage
	live       *atomic.Bool
}

// VerifyPhysicalContinuation checks a synchronous, controller-owned replay of a
// protected release through its authenticated query. It cannot mint provenance,
// replace VerifiedRequest, or authorize physical/source facts by itself.
func VerifyPhysicalContinuation(ctx context.Context, service *fsservice.Service, facts Facts, scope guestbridge.Context, operation string, payload json.RawMessage) error {
	owned, ok := ctx.Value(physicalContinuationKey{}).(physicalContinuation)
	outer, observed := guestbridge.VerifiedRequest(ctx)
	if !ok || !observed || owned.live == nil || !owned.live.Load() || owned.controller == nil ||
		!owned.controller.UsesFacts(service, facts) || owned.scope != scope || owned.operation != operation || operation != "VIEW_RELEASE" ||
		!bytes.Equal(owned.payload, payload) || !reflect.DeepEqual(owned.outer, outer) || outer.Scope != scope || outer.Operation != "VIEW_QUERY" {
		return fsapi.Permission
	}
	return nil
}

// Only ExchangeView, while holding its protected ledger lock and after exact
// outer authentication/ticket lookup, can create this borrowed continuation.
func (c *Controller) releaseQueryContext(ctx context.Context, scope guestbridge.Context, raw json.RawMessage, query guestbridge.Ticket, saved ticketState, live *atomic.Bool) context.Context {
	outer, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || outer.Scope != scope || outer.Operation != "VIEW_QUERY" || outer.PayloadHash != digest(raw) ||
		saved.Scope != scope || !equal(saved.Request, query) || saved.Operation != "VIEW_RELEASE" || absent(saved.Payload) {
		return ctx
	}
	return context.WithValue(ctx, physicalContinuationKey{}, physicalContinuation{controller: c, outer: outer, scope: scope, operation: saved.Operation, payload: saved.Payload, live: live})
}
