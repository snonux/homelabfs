package labrunner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Executor runs one declared operation. A nil error certifies terminal success;
// Error.Uncertain certifies that a remote/child action may still be outstanding.
type Executor interface {
	Execute(context.Context, Plan, Step, string) error
}

// Error carries a known failure category or an uncertain side-effect outcome.
type Error struct {
	Category  string
	Uncertain bool
	Err       error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Runner injects the side-effect boundary and a clock for approval checks.
type Runner struct {
	Executor Executor
	Now      func() time.Time
}

// Run verifies inputs before opening the durable run journal. Cleanup uses the
// original approval even after expiry and never replays uncertain mutations.
func (r Runner) Run(ctx context.Context, p Plan, a Approval, cleanup bool, reconciliation *Reconciliation) (Report, error) {
	if r.Now == nil || r.Executor == nil {
		return Report{}, errors.New("runner requires executor and clock")
	}
	if err := p.CheckApproval(a, r.Now(), cleanup); err != nil {
		return Report{}, err
	}
	if err := p.VerifyArtifacts(); err != nil {
		return Report{}, err
	}
	j, err := openJournal(p, a, r.Now(), !cleanup)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = j.close() }()
	if err := ensurePlanSnapshot(p); err != nil {
		return reportFor(p, j.state), err
	}
	if reconciliation != nil {
		if err := j.reconcile(*reconciliation, r.Now()); err != nil {
			return reportFor(p, j.state), err
		}
	}
	if err := makeDirectory(p.EvidenceDir); err != nil {
		return reportFor(p, j.state), err
	}
	if hasUncertain(j.state) {
		rep := reportFor(p, j.state)
		writeErr := WriteReport(p.EvidenceDir, rep)
		return rep, errors.Join(errors.New("unfinished side effect requires operator reconciliation; no actions executed"), writeErr)
	}
	if j.state.Complete {
		rep := reportFor(p, j.state)
		err := WriteReport(p.EvidenceDir, rep)
		if !rep.Success {
			err = errors.Join(err, errors.New("run unsuccessful or incomplete"))
		}
		return rep, err
	}
	if cleanup {
		j.state.Aborted = true
		if err := j.save(); err != nil {
			return reportFor(p, j.state), err
		}
	}
	var runErr error
	if !cleanup && (!j.state.Aborted || forwardFailure(j.state) != nil) {
		runErr = r.forward(ctx, p, j)
	}
	if hasUncertain(j.state) {
		runErr = errors.Join(runErr, errors.New("cleanup blocked by uncertain side effect"))
	} else {
		runErr = errors.Join(runErr, r.teardown(p, j))
	}
	j.state.Complete = completed(j.state)
	if err := j.save(); err != nil {
		runErr = errors.Join(runErr, err)
	}
	rep := reportFor(p, j.state)
	runErr = errors.Join(runErr, WriteReport(p.EvidenceDir, rep))
	if !rep.Success {
		runErr = errors.Join(runErr, errors.New("run unsuccessful or incomplete"))
	}
	return rep, runErr
}

// Status reads local run state only; it does not invoke executors or lab probes.
func Status(p Plan) (Report, error) {
	h, err := p.Hash()
	if err != nil {
		return Report{}, err
	}
	var s State
	if err := ReadJSON(filepath.Join(p.StateDir, "journal.json"), &s); err != nil {
		return Report{}, err
	}
	if s.PlanHash != h {
		return Report{}, errors.New("status plan does not match journal")
	}
	return reportFor(p, s), nil
}

func (r Runner) forward(ctx context.Context, p Plan, j *journal) error {
	remaining := time.Duration(p.Budget.RuntimeSeconds)*time.Second - r.Now().Sub(j.state.Started)
	if remaining <= 0 {
		j.state.Aborted = true
		return errors.Join(errors.New("approved runtime elapsed"), j.save())
	}
	ctx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	// A terminal failure may be the last checkpoint written by an older
	// controller. Never retry that forward operation, even if Aborted was not
	// persisted yet; only outstanding read-only collection may proceed.
	failures := forwardFailure(j.state)
	if failures != nil {
		j.state.Aborted = true
		if err := j.save(); err != nil {
			return errors.Join(failures, err)
		}
	}
	for i, s := range p.Steps {
		if s.Phase == "cleanup" || s.Phase == "restore" {
			break
		}
		if j.state.Results[i].Status == "passed" {
			continue
		}
		if failures != nil && s.Phase != "collect" {
			continue
		}
		if ctx.Err() != nil {
			j.state.Aborted = true
			return errors.Join(failures, ctx.Err(), j.save())
		}
		if err := r.step(ctx, p, j, i); err != nil {
			failures = errors.Join(failures, err)
			if hasUncertain(j.state) {
				return failures
			}
			j.state.Aborted = true
			if saveErr := j.save(); saveErr != nil {
				return errors.Join(failures, saveErr)
			}
		}
	}
	return failures
}

func (r Runner) teardown(p Plan, j *journal) error {
	// Teardown has its own bounded context: cancellation of workload execution
	// must not skip an already approved clean shutdown/restoration.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.Budget.CleanupSeconds+p.Budget.RestorationSeconds)*time.Second)
	defer cancel()
	var failures error
	cleanupOK := true
	for i, s := range p.Steps {
		if s.Phase != "cleanup" && s.Phase != "restore" {
			continue
		}
		if s.Phase == "restore" && !cleanupOK {
			failures = errors.Join(failures, errors.New("restoration blocked: cleanup incomplete"))
			for k := i; k < len(p.Steps); k++ {
				if j.state.Results[k].Status != "passed" && j.state.Results[k].Status != "skipped" {
					j.state.Results[k].Status = "blocked"
					j.state.Results[k].Category = "restoration"
					j.state.Results[k].Message = "cleanup is incomplete"
					j.state.History = append(j.state.History, j.state.Results[k])
				}
			}
			failures = errors.Join(failures, j.save())
			break
		}
		if j.state.Results[i].Status == "passed" || j.state.Results[i].Status == "skipped" {
			continue
		}
		if !applicable(j.state, s) {
			j.state.Results[i].Status = "skipped"
			if err := j.save(); err != nil {
				return errors.Join(failures, err)
			}
			continue
		}
		if err := r.step(ctx, p, j, i); err != nil {
			failures = errors.Join(failures, err)
			if s.Phase == "cleanup" {
				cleanupOK = false
			}
			if hasUncertain(j.state) {
				break
			}
		}
	}
	return failures
}

func (r Runner) step(ctx context.Context, p Plan, j *journal, i int) error {
	s := p.Steps[i]
	attempts := 0
	for _, old := range j.state.History {
		if old.ID == s.ID && old.Status == "started" {
			attempts++
		}
	}
	if attempts >= s.MaxAttempts {
		return fmt.Errorf("approved attempt limit exhausted for %s", s.ID)
	}
	x := j.state.Results[i]
	x.Status = "started"
	x.Started = r.Now()
	x.Finished = time.Time{}
	x.Message = ""
	x.Category = ""
	j.state.Results[i] = x
	j.state.History = append(j.state.History, x)
	if err := j.save(); err != nil {
		return fmt.Errorf("persist start before %s: %w", s.ID, err)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.TimeoutSeconds)*time.Second)
	defer cancel()
	err := r.Executor.Execute(ctx, p, s, filepath.Join(p.EvidenceDir, s.ID+".log"))
	x.Finished = r.Now()
	x.Status = "passed"
	if err != nil {
		x.Status = "failed"
		x.Category = "infrastructure"
		x.Message = err.Error()
		var failure *Error
		if errors.As(err, &failure) {
			if contains([]string{"assertion", "infrastructure", "timeout", "cleanup", "restoration"}, failure.Category) {
				x.Category = failure.Category
			}
			if failure.Uncertain {
				x.Status = "ambiguous"
			}
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			x.Category = "timeout"
			x.Status = "ambiguous"
		}
		if s.Phase == "cleanup" {
			x.Category = "cleanup"
		}
		if s.Phase == "restore" {
			x.Category = "restoration"
		}
	}
	j.state.Results[i] = x
	j.state.History = append(j.state.History, x)
	if x.Status == "failed" {
		// Persist the unsuccessful run with its terminal result, including
		// teardown failures. Recovery must never erase a known failure.
		j.state.Aborted = true
	}
	if saveErr := j.save(); saveErr != nil {
		// The persisted checkpoint can still say started. Stop all further
		// actions rather than relying on an in-memory terminal transition.
		j.state.Results[i].Status = "ambiguous"
		j.state.Results[i].Message = "terminal checkpoint could not be persisted"
		return errors.Join(err, saveErr)
	}
	return err
}

func forwardFailure(s State) error {
	for _, result := range s.Results {
		if result.Phase != "cleanup" && result.Phase != "restore" && result.Status == "failed" {
			return fmt.Errorf("forward step %s previously failed: %s", result.ID, result.Message)
		}
	}
	return nil
}

func hasUncertain(s State) bool {
	for _, r := range s.Results {
		if r.Status == "started" || r.Status == "ambiguous" {
			return true
		}
	}
	return false
}
func applicable(state State, s Step) bool {
	for _, r := range state.Results {
		if contains(s.AppliesAfter, r.ID) && r.Status != "pending" && r.Status != "skipped" {
			return true
		}
	}
	return false
}
func completed(s State) bool {
	for _, r := range s.Results {
		if r.Status == "started" || r.Status == "ambiguous" || (r.Phase == "cleanup" || r.Phase == "restore") && r.Status != "passed" && r.Status != "skipped" {
			return false
		}
	}
	return true
}

func ensurePlanSnapshot(p Plan) error {
	path := filepath.Join(p.StateDir, "plan.json")
	var old Plan
	if err := ReadJSON(path, &old); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return WriteJSON(path, p)
		}
		return err
	}
	h, err := old.Hash()
	if err != nil {
		return err
	}
	expected, err := p.Hash()
	if err != nil {
		return err
	}
	if h != expected {
		return errors.New("canonical plan snapshot changed")
	}
	return nil
}
