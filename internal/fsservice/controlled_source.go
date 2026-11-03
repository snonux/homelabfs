package fsservice

import (
	"context"
	"math"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

// ControlledEffect is one exact planned backing effect, never a source receipt.
type ControlledEffect struct {
	ID      fsapi.UUID
	Payload any
}

// SourceBytesAfter preflights actual catalog primitives and outstanding future
// sparse growth on the sealed worker without copying/allocating payloads.
func (c *ControlledCut) SourceBytesAfter(ctx context.Context, effects []ControlledEffect, future []storage.FutureWrite, extraPins int, maxBytes int64) (int64, error) {
	if !c.active || c.service.State() != Serving {
		return 0, fsapi.Unavailable
	}
	image, err := c.service.applied.Store.Export(ctx)
	if err != nil {
		return 0, err
	}
	operations, err := c.sourceOperations(ctx, image, effects)
	if err != nil {
		return 0, err
	}
	return storage.ProjectedSourceBytes(ctx, image, operations, future, extraPins, c.service.config.Storage.Limits, maxBytes)
}

func (c *ControlledCut) sourceOperations(ctx context.Context, image storage.Image, effects []ControlledEffect) ([]storage.Operation, error) {
	operations := make([]storage.Operation, 0, len(effects))
	position := image.Position
	for _, effect := range effects {
		if position.Sequence == math.MaxUint64 {
			return nil, fsapi.NoSpace
		}
		position.Sequence++
		op := storage.Operation{ID: effect.ID, Position: position}
		switch payload := effect.Payload.(type) {
		case record.Mutation:
			var err error
			op, err = apply.MutationOperation(ctx, c.service.applied.Store, effect.ID, position, payload)
			if err != nil {
				return nil, err
			}
		case record.HandlePin:
			op.Kind = storage.HandlePin
			op.Object = payload.Object
			op.Participants = []fsapi.UUID{payload.Object.Home}
			op.Pins = []storage.PinChange{{After: storage.Pin{ID: payload.Pin, Object: payload.Object, Generation: payload.ServingGeneration, Incarnation: payload.Incarnation, Active: true}}}
		case record.HandleRelease:
			found := false
			for _, pin := range image.Pins {
				if pin.ID == payload.Pin {
					before := pin
					after := pin
					after.Active = false
					op.Kind = storage.HandleRelease
					op.Object = pin.Object
					op.Participants = []fsapi.UUID{pin.Object.Home}
					op.Pins = []storage.PinChange{{Before: &before, After: after}}
					found = true
					break
				}
			}
			if !found {
				return nil, fsapi.Stale
			}
		default:
			return nil, fsapi.Unsupported
		}
		operations = append(operations, op)
	}
	return operations, nil
}

func (c *ControlledCut) checkSourcePlan(ctx context.Context, plan ControlledPlan) error {
	if !c.service.config.RequireControlledViews {
		return nil
	}
	if c.permit == nil {
		return fsapi.Permission
	}
	effects := []ControlledEffect{}
	if plan.Mutation != nil {
		effects = append(effects, ControlledEffect{plan.Operation, *plan.Mutation})
	}
	if plan.Pin != nil {
		effects = append(effects, ControlledEffect{plan.PinOperation, *plan.Pin})
	}
	return c.permit.CheckSource(ctx, c, effects)
}
