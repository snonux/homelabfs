package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sync/atomic"

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

type protectFunc = func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)
type prepareFunc = func(guestbridge.Captured) (guestbridge.Prepared, error)

// execute loads the exact original claim, admits it, then plans, protects and
// applies it on the serialized worker. A definite Admit refusal (task j64,
// AdmissionRefusal) is protected as a terminal failed result without a cut;
// any other Admit error is returned uncertain and leaves the claim
// RESERVED/UNPREPARED for QUERY's resume to retry.
func (p *Provider) execute(ctx context.Context, b guestbridge.Binding, prepare prepareFunc, protect protectFunc, resume bool) (guestbridge.Outcome, error) {
	if err := p.Check(ctx, b.Context); err != nil {
		return guestbridge.Outcome{}, err
	}
	p.execution.Lock()
	saved, err := p.loadExact(b, resume)
	p.execution.Unlock()
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	if saved.Outcome != nil {
		return *saved.Outcome, nil
	}
	if resume {
		live := new(atomic.Bool)
		live.Store(true)
		defer live.Store(false)
		ctx = p.cleanupQueryContext(ctx, b, saved, live)
	}
	admission, refusal, refused, err := p.admit(ctx, b, protect)
	if refused || err != nil {
		return refusal, err
	}
	if admission != nil {
		defer admission.Close()
	}
	return p.executeAdmitted(ctx, b, admission, prepare, protect, resume)
}

// executeAdmitted reloads the claim under p.execution (closing races with a
// concurrent dispatch or replay) and runs planning and the effect on the
// serialized worker, through the admission permit when one was granted.
func (p *Provider) executeAdmitted(ctx context.Context, b guestbridge.Binding, admission Admitted, prepare prepareFunc, protect protectFunc, resume bool) (guestbridge.Outcome, error) {
	p.execution.Lock()
	defer p.execution.Unlock()
	saved, err := p.loadExact(b, resume)
	if err != nil {
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
	err = run(ctx, p.config.Authorization.Enrollment.Profile, func(owned context.Context, cut *fsservice.ControlledCut) error {
		plan := saved.Plan
		var err error
		if plan == nil {
			if plan, err = p.planAndProtect(owned, cut, b, saved, prepare, protect); err != nil {
				return err
			}
		}
		out, err = p.applyAndRecord(owned, cut, b, saved, *plan)
		return err
	})
	return out, err
}

// loadExact reads the exact original entry; callers hold p.execution. Only
// RESERVED, PENDING and TERMINAL non-gap entries execute; resume additionally
// requires a never-prepared RESERVED entry. An unplanned entry waits
// (WouldBlock) while another operation of the scope holds a protected PENDING
// bridge snapshot.
func (p *Provider) loadExact(b guestbridge.Binding, resume bool) (entry, error) {
	var saved entry
	err := p.db.View(func(tx *bolt.Tx) error {
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
		if resume && !neverPrepared(e) {
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
	return saved, err
}

// planAndProtect marks the claim PREPARING, runs the preparer (a preparer
// error becomes a failed plan with that code) and protects plan, planned
// outcome, proposed result and bridge snapshot as PENDING before any effect.
func (p *Provider) planAndProtect(owned context.Context, cut *fsservice.ControlledCut, b guestbridge.Binding, saved entry, prepare prepareFunc, protect protectFunc) (*fsservice.ControlledPlan, error) {
	// A failed protection write must not make an already invoked
	// preparer look like a never-planned admission refusal on QUERY.
	if saved.PreparationStage != "UNPREPARED" {
		return nil, fsapi.WouldBlock
	}
	if err := p.update(b, func(e *entry) error {
		if e.PreparationStage != "UNPREPARED" || e.Plan != nil || e.Outcome != nil {
			return fsapi.Stale
		}
		e.PreparationStage = "PREPARING"
		return nil
	}); err != nil {
		return nil, err
	}
	plan, err := p.runPreparer(owned, cut, b, prepare)
	if err != nil {
		return nil, err
	}
	if err = p.fail("prepared-before-plan-protection"); err != nil {
		return nil, err
	}
	predicted, proposed, bridge, plan, err := p.projectPlan(owned, cut, b, plan, protect)
	if err != nil {
		return nil, err
	}
	if err = p.protectPlan(b, plan, predicted, proposed, bridge); err != nil {
		return nil, err
	}
	if err = p.fail("plan-protected"); err != nil {
		return nil, err
	}
	return plan, nil
}

// runPreparer invokes the bridge preparer once. Its refusal becomes a failed
// plan carrying the refusal's code (IO when it has none): preparation runs
// after PREPARING was recorded, and the failed plan is protected and answered
// like any other terminal result.
func (p *Provider) runPreparer(owned context.Context, cut *fsservice.ControlledCut, b guestbridge.Binding, prepare prepareFunc) (*fsservice.ControlledPlan, error) {
	cap := &captured{cut: cut, provider: p, owned: owned, binding: b}
	prepared, err := prepare(cap)
	if err != nil {
		var code fsapi.Errno
		if !errors.As(err, &code) {
			code = fsapi.IO
		}
		return &fsservice.ControlledPlan{Operation: b.Operation, Result: fsapi.Result{Code: code}}, nil
	}
	if cap.plan == nil || prepared.Command.Operation != cap.plan.Command.Operation || prepared.TransientForget != cap.plan.TransientForget {
		return nil, fsapi.Stale
	}
	return cap.plan, nil
}

// projectPlan computes the planned outcome and, with protect, the bridge's
// proposed result and snapshot. A NoSpace projection precedes plan protection
// and every native effect, so it declines the actual planned result with a
// NoSpace failed plan, not the caller's requested maximum READDIR count or
// hypothetical ancestry.
func (p *Provider) projectPlan(owned context.Context, cut *fsservice.ControlledCut, b guestbridge.Binding, plan *fsservice.ControlledPlan, protect protectFunc) (guestbridge.Outcome, json.RawMessage, json.RawMessage, *fsservice.ControlledPlan, error) {
	predicted, err := plannedOutcome(owned, cut, *plan)
	if err != nil {
		return predicted, nil, nil, nil, err
	}
	if protect == nil {
		return predicted, nil, nil, plan, nil
	}
	proposed, bridge, err := protect(predicted)
	if errors.Is(err, fsapi.NoSpace) {
		plan = &fsservice.ControlledPlan{Operation: b.Operation, Result: fsapi.Result{Code: fsapi.NoSpace}}
		predicted, err = plannedOutcome(owned, cut, *plan)
		if err == nil {
			proposed, bridge, err = protect(predicted)
		}
	}
	if err != nil {
		return predicted, nil, nil, nil, err
	}
	if !controlledResult(proposed) || !json.Valid(bridge) {
		return predicted, nil, nil, nil, fsapi.Invalid
	}
	return predicted, proposed, bridge, plan, nil
}

// protectPlan records the plan as PENDING with its READDIR closure, snapshot
// version and planned outcome, within the claim's retained/source credit.
func (p *Provider) protectPlan(b guestbridge.Binding, plan *fsservice.ControlledPlan, predicted guestbridge.Outcome, proposed, bridge json.RawMessage) error {
	var closure *guestbridge.ReaddirClosure
	if selected := p.config.Authorization.Enrollment.Protocol; selected != nil && guestbridge.HasCleanupProtocol(*selected) && b.Call.Intent.Opcode == "READDIR" && predicted.Result.Code == fsapi.OK {
		var err error
		if closure, err = guestbridge.PrepareReaddirClosure(b, proposed, bridge, *selected); err != nil {
			return err
		}
	}
	var snapshotVersion uint64
	if len(proposed) > 0 {
		if err := p.updateOpen(func(tx *bolt.Tx) error {
			var e error
			snapshotVersion, e = p.bucket(tx, "bridge").NextSequence()
			return e
		}); err != nil {
			return err
		}
	}
	return p.update(b, func(e *entry) error {
		e.ReaddirClosure = closure
		e.SnapshotVersion = snapshotVersion
		e.Plan = plan
		e.ProposedResult = proposed
		e.BridgeState = bridge
		e.PlannedOutcome = &predicted
		e.Status = "PENDING"
		return checkPlanCredit(*e, *plan, predicted)
	})
}

// checkPlanCredit bounds a protected plan's ledger footprint by the claim's
// preowned retained credit and its mutation encoding by the source credit.
func checkPlanCredit(e entry, plan fsservice.ControlledPlan, predicted guestbridge.Outcome) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	outcome, err := json.Marshal(predicted)
	if err != nil {
		return err
	}
	if uint64(len(raw)+len(e.ProposedResult)+len(e.BridgeState)+len(outcome)+4096) > e.RetainedCredit || uint64(len(plan.MutationEncoding)+4096) > e.SourceCredit {
		return fsapi.NoSpace
	}
	return nil
}

// applyAndRecord applies the protected plan and records the outcome; with a
// proposed result the claim becomes TERMINAL and its snapshot is protected.
func (p *Provider) applyAndRecord(owned context.Context, cut *fsservice.ControlledCut, b guestbridge.Binding, saved entry, plan fsservice.ControlledPlan) (guestbridge.Outcome, error) {
	var out guestbridge.Outcome
	result, err := cut.ApplyControlled(owned, plan, saved.Plan != nil)
	if err != nil {
		return out, err
	}
	if err = p.fail("effect-before-outcome"); err != nil {
		return out, err
	}
	if saved.Plan != nil && saved.PlannedOutcome != nil {
		out = *saved.PlannedOutcome
	} else {
		var current entry
		if err = p.db.View(func(tx *bolt.Tx) error { e, _, err := p.load(tx, b.Call.Claim); current = e; return err }); err != nil {
			return out, err
		}
		if current.PlannedOutcome == nil {
			return out, fsapi.Unavailable
		}
		out = *current.PlannedOutcome
	}
	out.Result = result
	return out, p.updateOpen(func(tx *bolt.Tx) error {
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
		owner, err := c.provider.processLockOwner(b, command.Generation)
		if err != nil {
			return guestbridge.Prepared{}, err
		}
		command.Lock.Owner = owner
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

// processLockOwner is the advisory-lock owner a DESCRIPTOR_CLOSE or
// PROCESS_EXIT releases: the session plus a stable per-process token,
// allocated once (bounded by MaxClaims) and reused for that process.
func (p *Provider) processLockOwner(b guestbridge.Binding, generation fsapi.UUID) (fsapi.LockOwner, error) {
	var token uint64
	err := p.updateOpen(func(tx *bolt.Tx) error {
		bucket := p.bucket(tx, "processes")
		key := []byte(b.Call.Credentials.Process)
		if raw := bucket.Get(key); raw != nil {
			if len(raw) != 8 {
				return fsapi.Stale
			}
			token = binary.BigEndian.Uint64(raw)
			return nil
		}
		if bucket.Stats().KeyN >= p.config.MaxClaims {
			return fsapi.NoSpace
		}
		token = uint64(bucket.Stats().KeyN) + 1
		var raw [8]byte
		binary.BigEndian.PutUint64(raw[:], token)
		return bucket.Put(key, raw[:])
	})
	if err != nil {
		return fsapi.LockOwner{}, err
	}
	session, err := fsapi.ParseUUID(b.Context.Session)
	if err != nil {
		return fsapi.LockOwner{}, err
	}
	return fsapi.LockOwner{Session: session, Generation: generation, Token: token}, nil
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
