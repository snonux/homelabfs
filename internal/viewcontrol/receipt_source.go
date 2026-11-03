package viewcontrol

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// UsesFacts identifies the actual locked controller/service/Facts assembly for
// the runtime producer. Labels or copied enrollment JSON cannot satisfy it.
func (c *Controller) UsesFacts(service *fsservice.Service, facts Facts) bool {
	return c.config.Service == service && reflect.ValueOf(c.config.Facts) == reflect.ValueOf(facts)
}

// ReceiptEvidence supplies only evidence backed by this live controller's
// actual retained sources/events. Every source is reverified before minting.
// It runs after bridge dispatch releases controller/service locks; no source
// callback enters FUSE or recursively dispatches another service operation.
func (c *Controller) ReceiptEvidence(ctx context.Context, command guestbridge.Command, payload json.RawMessage) (string, json.RawMessage, error) {
	if err := c.CheckRequest(ctx, command.Context, command.Op, command.Payload); err != nil {
		return "", nil, err
	}
	if !c.mu.TryLock() {
		return "", nil, fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return "", nil, fsapi.Unavailable
	}
	ledger, err := c.rawState()
	if err != nil {
		return "", nil, err
	}
	if err = VerifyRetainedSources(c.factsContext(ctx), c.config.Service, ledger); err != nil {
		return "", nil, err
	}
	var response struct {
		Status      string
		Event       json.RawMessage
		Lease       *guestbridge.ViewLease
		Reservation *guestbridge.DirtyReservation
		Entry       *struct{ Reservation string }
	}
	if json.Unmarshal(payload, &response) != nil {
		return "", nil, fsapi.Invalid
	}
	if command.Op == "VIEW_POLL" && response.Status == "EVENT" {
		if c.state.Recall == nil || !equal(c.state.Recall.Events[command.Context.Session], response.Event) {
			return "", nil, fsapi.Stale
		}
		var event struct {
			Domain        guestbridge.Capability
			Sequence      uint64
			Recall, Phase string
			ManifestHash  *string `json:"manifest_hash"`
			Target        *guestbridge.ViewTarget
		}
		if json.Unmarshal(response.Event, &event) != nil {
			return "", nil, fsapi.Invalid
		}
		if event.Target != nil && !c.retainedTarget(command.Context, *event.Target) {
			return "", nil, fsapi.Stale
		}
		evidence, err := canonical(map[string]any{"domain_token": event.Domain.Token, "sequence": event.Sequence, "recall": event.Recall, "phase": event.Phase, "manifest_hash": event.ManifestHash, "target": event.Target, "retained_owner": c.config.Authorization.Enrollment.ID.String()})
		return "EVENT", evidence, err
	}
	lease := response.Lease
	if response.Reservation != nil {
		saved, ok := c.state.Reservations[response.Reservation.ID]
		if !ok || !equal(saved.Reservation, *response.Reservation) {
			return "", nil, fsapi.Stale
		}
		stored := c.state.Leases[saved.Reservation.LeaseID].Lease
		lease = &stored
	}
	if response.Entry != nil {
		saved, ok := c.state.Reservations[response.Entry.Reservation]
		if !ok {
			return "", nil, fsapi.Stale
		}
		stored := c.state.Leases[saved.Reservation.LeaseID].Lease
		lease = &stored
	}
	if lease == nil {
		return "", nil, nil
	}
	stored, ok := c.state.Leases[lease.ID]
	if !ok || !equal(stored.Lease, *lease) || !c.retainedTarget(command.Context, lease.Target) {
		return "", nil, fsapi.Stale
	}
	evidence, err := canonical(map[string]any{"target": lease.Target, "retained_owner": c.config.Authorization.Enrollment.ID.String()})
	return "SOURCE", evidence, err
}

func (c *Controller) retainedTarget(scope guestbridge.Context, target guestbridge.ViewTarget) bool {
	if target.Kind != "CONTROLLED_LOCAL" {
		return false
	}
	for _, source := range c.state.Sources {
		if equal(target, c.target(scope, source)) {
			return true
		}
	}
	return false
}
