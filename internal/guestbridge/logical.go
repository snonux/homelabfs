package guestbridge

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

var _ Captured = (*LogicalCut)(nil)

// NewLogicalCut retains the caller's already captured observation; the caller
// closes it after prepare returns. Shared planning verifies its actual view.
func NewLogicalCut(planner *fsservice.LogicalPlanner, observation *logicalview.Observation) (*LogicalCut, error) {
	if planner == nil || observation == nil {
		return nil, fsapi.Invalid
	}
	return &LogicalCut{planner: planner, observation: observation}, nil
}

// Image reads committed logical metadata, independently of backing apply.
func (c *LogicalCut) Image(ctx context.Context, volume fsapi.UUID) (storage.Image, error) {
	return c.observation.Image(ctx, volume)
}

// Stat resolves one stable home object at the captured cut.
func (c *LogicalCut) Stat(ctx context.Context, id fsapi.ObjectID) (storage.Object, error) {
	return c.observation.Stat(ctx, id)
}

// Lookup resolves one raw name without following host paths or symlinks.
func (c *LogicalCut) Lookup(ctx context.Context, id fsapi.ObjectID, name string) (storage.Entry, error) {
	return c.observation.Lookup(ctx, id, name)
}

// Prepare invokes the real shared Unix planner/observation code at this same
// RAM cut; publication and protected lifetime commands remain Execution work.
func (c *LogicalCut) Prepare(ctx context.Context, b Binding, command fsapi.Command) (Prepared, error) {
	p := Prepared{Command: command}
	cred := guestCred(b.Call.Credentials)
	switch command.Operation {
	case fsapi.Create, fsapi.Mkdir, fsapi.SymlinkOp, fsapi.Unlink, fsapi.Rmdir, fsapi.RenameOp, fsapi.LinkOp, fsapi.Write, fsapi.Truncate, fsapi.Setattr:
		var plan fsservice.Planned
		var err error
		if b.Call.Intent.Opcode == "WRITE" {
			var a arguments
			if err = json.Unmarshal(b.Call.Intent.Args, &a); err != nil {
				return Prepared{}, fsapi.Invalid
			}
			plan, err = c.planner.PlanDelegatedWrite(ctx, c.observation, cred, command, b.Operation, b.Allocation, int64(a.number("max_end")))
		} else {
			plan, err = c.planner.Plan(ctx, c.observation, cred, command, b.Operation, b.Allocation)
		}
		if err != nil {
			return Prepared{}, err
		}
		p.Plan = &plan
		p.Result = plan.Result
	case fsapi.Sync, fsapi.Release, fsapi.Forget, fsapi.DescriptorClose, fsapi.ProcessExit:
	default:
		result, err := c.planner.Observe(ctx, c.observation, cred, command)
		if err != nil {
			return Prepared{}, err
		}
		p.Result = result
	}
	return p, nil
}
