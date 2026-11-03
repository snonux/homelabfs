package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/storage"
	bolt "go.etcd.io/bbolt"
)

// Execute invokes real serialized filesystem planning and protects exact plan
// and outcome in the private ledger. Recovery reuses original UUID evidence.
func (p *Provider) Execute(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error)) (guestbridge.Outcome, error) {
	return p.execute(ctx, b, prepare, nil, false)
}

func (p *Provider) ExecuteValidation(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error), protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)) (guestbridge.Outcome, error) {
	return p.execute(ctx, b, prepare, protect, false)
}

// ResumeValidation only prepares an exact original RESERVED entry which has
// never protected a plan or outcome. The second locked load closes races with
// ordinary dispatch; existing plans stay on RecoverOutcome's recovery path.
func (p *Provider) ResumeValidation(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error), protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)) (guestbridge.Outcome, error) {
	return p.execute(ctx, b, prepare, protect, true)
}

func (p *Provider) execute(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error), protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error), resume bool) (guestbridge.Outcome, error) {
	if err := p.Check(ctx, b.Context); err != nil {
		return guestbridge.Outcome{}, err
	}
	var saved entry
	loadSaved := func() error {
		return p.db.View(func(tx *bolt.Tx) error {
			e, ok, err := p.load(tx, b.Call.Claim)
			if err != nil {
				return err
			}
			if !ok || !sameExact(e.Binding, b) {
				return fsapi.Stale
			}
			if e.Gap || (e.Status != "RESERVED" && e.Status != "PENDING" && e.Status != "TERMINAL") {
				return fsapi.Stale
			}
			if resume && (e.Status != "RESERVED" || e.PreparationStage != "UNPREPARED" || e.Plan != nil || e.Outcome != nil || e.PlannedOutcome != nil || len(e.ProposedResult) != 0 || len(e.Result) != 0 || len(e.BridgeState) != 0 || e.SnapshotVersion != 0) {
				return fsapi.WouldBlock
			}
			if e.Plan == nil {
				if err = p.checkPendingStage(tx, b.Context, b.Operation); err != nil {
					return err
				}
			}
			saved = e
			return nil
		})
	}
	p.execution.Lock()
	errLoad := loadSaved()
	p.execution.Unlock()
	if errLoad != nil {
		return guestbridge.Outcome{}, errLoad
	}
	if saved.Outcome != nil {
		return *saved.Outcome, nil
	}
	var admission Admitted
	if controller := p.currentAdmission(); controller != nil {
		var err error
		admission, err = controller.Admit(ctx, b)
		if err != nil {
			return guestbridge.Outcome{}, err
		}
		if nilValue(admission) {
			return guestbridge.Outcome{}, fsapi.Permission
		}
		defer admission.Close()
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	if err := loadSaved(); err != nil {
		return guestbridge.Outcome{}, err
	}
	if saved.Outcome != nil {
		return *saved.Outcome, nil
	}
	var out guestbridge.Outcome
	run := p.config.Service.Controlled
	if admission != nil {
		run = func(ctx context.Context, profile fsservice.ValidationProfile, callback func(context.Context, *fsservice.ControlledCut) error) error {
			return p.config.Service.ControlledAdmitted(ctx, profile, admission, callback)
		}
	}
	err := run(ctx, p.config.Authorization.Enrollment.Profile, func(owned context.Context, cut *fsservice.ControlledCut) error {
		plan := saved.Plan
		if plan == nil {
			// A failed protection write must not make an already invoked
			// preparer look like a never-planned admission refusal on QUERY.
			if saved.PreparationStage != "UNPREPARED" {
				return fsapi.WouldBlock
			}
			if err := p.update(b, func(e *entry) error {
				if e.PreparationStage != "UNPREPARED" || e.Plan != nil || e.Outcome != nil {
					return fsapi.Stale
				}
				e.PreparationStage = "PREPARING"
				return nil
			}); err != nil {
				return err
			}
			cap := &captured{cut: cut, provider: p, owned: owned, binding: b}
			prepared, err := prepare(cap)
			if err != nil {
				var code fsapi.Errno
				if !errors.As(err, &code) {
					code = fsapi.IO
				}
				cp := fsservice.ControlledPlan{Operation: b.Operation, Result: fsapi.Result{Code: code}}
				plan = &cp
			} else {
				if cap.plan == nil || prepared.Command.Operation != cap.plan.Command.Operation || prepared.TransientForget != cap.plan.TransientForget {
					return fsapi.Stale
				}
				plan = cap.plan
			}
			if err := p.fail("prepared-before-plan-protection"); err != nil {
				return err
			}
			predicted, err := plannedOutcome(owned, cut, *plan)
			if err != nil {
				return err
			}
			var proposed, bridge json.RawMessage
			if protect != nil {
				proposed, bridge, err = protect(predicted)
				if err != nil {
					return err
				}
				if !controlledResult(proposed) || !json.Valid(bridge) {
					return fsapi.Invalid
				}
			}
			var snapshotVersion uint64
			if len(proposed) > 0 {
				if err = p.db.Update(func(tx *bolt.Tx) error {
					var e error
					snapshotVersion, e = p.bucket(tx, "bridge").NextSequence()
					return e
				}); err != nil {
					return err
				}
			}
			if err = p.update(b, func(e *entry) error {
				e.SnapshotVersion = snapshotVersion
				e.Plan = plan
				e.ProposedResult = proposed
				e.BridgeState = bridge
				e.PlannedOutcome = &predicted
				e.Status = "PENDING"
				raw, err := json.Marshal(e)
				if err != nil {
					return err
				}
				outcome, err := json.Marshal(predicted)
				if err != nil {
					return err
				}
				if uint64(len(raw)+len(proposed)+len(bridge)+len(outcome)+4096) > e.RetainedCredit || uint64(len(plan.MutationEncoding)+4096) > e.SourceCredit {
					return fsapi.NoSpace
				}
				return nil
			}); err != nil {
				return err
			}
			if err = p.fail("plan-protected"); err != nil {
				return err
			}
		}
		result, err := cut.ApplyControlled(owned, *plan, saved.Plan != nil)
		if err != nil {
			return err
		}
		if err = p.fail("effect-before-outcome"); err != nil {
			return err
		}
		if saved.Plan != nil && saved.PlannedOutcome != nil {
			out = *saved.PlannedOutcome
		} else {
			var current entry
			if err = p.db.View(func(tx *bolt.Tx) error { e, _, err := p.load(tx, b.Call.Claim); current = e; return err }); err != nil {
				return err
			}
			if current.PlannedOutcome == nil {
				return fsapi.Unavailable
			}
			out = *current.PlannedOutcome
		}
		out.Result = result
		return p.db.Update(func(tx *bolt.Tx) error {
			e, ok, err := p.load(tx, b.Call.Claim)
			if err != nil {
				return err
			}
			if !ok || !sameExact(e.Binding, b) {
				return fsapi.Stale
			}
			e.Outcome = &out
			if len(e.ProposedResult) > 0 {
				e.Result = bytes.Clone(e.ProposedResult)
				e.Status = "TERMINAL"
				if err = p.protectSnapshot(tx, e); err != nil {
					return err
				}
			}
			return put(p.bucket(tx, "claims"), claimKey(e.Claim), e)
		})
	})
	return out, err
}

func (c *captured) Image(ctx context.Context, id fsapi.UUID) (storage.Image, error) {
	return c.cut.Image(c.owned, id)
}

func (c *captured) Stat(ctx context.Context, id fsapi.ObjectID) (storage.Object, error) {
	return c.cut.Stat(c.owned, id)
}

func (c *captured) Lookup(ctx context.Context, id fsapi.ObjectID, name string) (storage.Entry, error) {
	return c.cut.Lookup(c.owned, id, name)
}

func (c *captured) Prepare(ctx context.Context, b guestbridge.Binding, command fsapi.Command) (guestbridge.Prepared, error) {
	return c.prepare(ctx, b, command, false)
}

func (c *captured) PrepareTransientForget(ctx context.Context, b guestbridge.Binding, command fsapi.Command) (guestbridge.Prepared, error) {
	if b.Call.Intent.Opcode != "FORGET" {
		return guestbridge.Prepared{}, fsapi.Invalid
	}
	return c.prepare(ctx, b, command, true)
}

func (c *captured) prepare(_ context.Context, b guestbridge.Binding, command fsapi.Command, transientForget bool) (guestbridge.Prepared, error) {
	if !sameExact(c.binding, b) {
		return guestbridge.Prepared{}, fsapi.Permission
	}
	if c.plan != nil {
		return guestbridge.Prepared{}, fsapi.Stale
	}
	ctx := c.owned
	var maxEnd *int64
	if b.Call.Intent.Opcode == "WRITE" {
		var a struct {
			MaxEnd int64 `json:"max_end"`
		}
		if err := json.Unmarshal(b.Call.Intent.Args, &a); err != nil {
			return guestbridge.Prepared{}, err
		}
		maxEnd = &a.MaxEnd
	}
	cred := b.Call.Credentials
	if command.Operation == fsapi.DescriptorClose || command.Operation == fsapi.ProcessExit {
		var token uint64
		err := c.provider.db.Update(func(tx *bolt.Tx) error {
			bucket := c.provider.bucket(tx, "processes")
			key := []byte(cred.Process)
			if raw := bucket.Get(key); raw != nil {
				if len(raw) != 8 {
					return fsapi.Stale
				}
				token = binary.BigEndian.Uint64(raw)
				return nil
			}
			if bucket.Stats().KeyN >= c.provider.config.MaxClaims {
				return fsapi.NoSpace
			}
			token = uint64(bucket.Stats().KeyN) + 1
			var raw [8]byte
			binary.BigEndian.PutUint64(raw[:], token)
			return bucket.Put(key, raw[:])
		})
		if err != nil {
			return guestbridge.Prepared{}, err
		}
		session, err := fsapi.ParseUUID(b.Context.Session)
		if err != nil {
			return guestbridge.Prepared{}, err
		}
		command.Lock.Owner = fsapi.LockOwner{Session: session, Generation: command.Generation, Token: token}
	}
	planner := c.cut.PlanControlled
	if transientForget {
		planner = c.cut.PlanTransientForget
	}
	if b.Call.Intent.Opcode == "DRAIN_WRITE" {
		planner = c.cut.PlanControlledDrain
	}
	plan, err := planner(ctx, fsapi.Cred{UID: cred.FSUID, GID: cred.FSGID, Groups: cred.Groups}, command, b.Operation, b.Allocation, maxEnd)
	if err != nil {
		return guestbridge.Prepared{}, err
	}
	c.plan = &plan
	return guestbridge.Prepared{Command: plan.Command, Result: plan.Result, TransientForget: transientForget}, nil
}

// RecoverOutcome resolves the original local operation and exact protected
// result. Missing original journal evidence remains uncertain; no callback runs.
func (p *Provider) RecoverOutcome(ctx context.Context, b guestbridge.Binding) (guestbridge.Outcome, bool, error) {
	if err := p.Check(ctx, b.Context); err != nil {
		return guestbridge.Outcome{}, false, err
	}
	var saved entry
	if err := p.db.View(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, b.Call.Claim)
		if err != nil {
			return err
		}
		if !ok || !sameExact(e.Binding, b) {
			return fsapi.Stale
		}
		saved = e
		return nil
	}); err != nil {
		return guestbridge.Outcome{}, false, err
	}
	if saved.Outcome != nil {
		return *saved.Outcome, true, nil
	}
	if saved.Plan == nil {
		return guestbridge.Outcome{}, false, nil
	}
	out, err := p.Execute(ctx, b, func(guestbridge.Captured) (guestbridge.Prepared, error) {
		return guestbridge.Prepared{}, fsapi.Unavailable
	})
	if err != nil {
		return guestbridge.Outcome{}, false, err
	}
	return out, true, nil
}

func plannedOutcome(ctx context.Context, cut *fsservice.ControlledCut, p fsservice.ControlledPlan) (guestbridge.Outcome, error) {
	out := guestbridge.Outcome{Result: p.Result, Publication: json.RawMessage("null")}
	if p.TooLarge {
		out.DelegatedError = fsservice.ErrDelegatedFileTooLarge
		return out, nil
	}
	if p.Command.Parent.Valid() && p.Command.Name != "" {
		entry, err := cut.Lookup(ctx, p.Command.Parent, p.Command.Name)
		if err != nil {
			return out, err
		}
		out.EntryRevision = entry.Revision
	}
	if p.Mutation != nil {
		for _, entry := range p.Mutation.Redo.Entries {
			if entry.Parent == p.Command.Parent && entry.Name == p.Command.Name {
				out.EntryRevision = entry.After.Revision
			}
		}
	}
	for _, entry := range p.Result.Entries {
		object, err := cut.Stat(ctx, entry.Object)
		if err != nil {
			return out, err
		}
		out.EntryAttributes = append(out.EntryAttributes, object.Attr)
	}
	return out, nil
}
