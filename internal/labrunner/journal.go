package labrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Result records a durable transition and preserves failure classification.
type Result struct {
	ID           string    `json:"id"`
	Phase        string    `json:"phase"`
	Status       string    `json:"status"`
	Category     string    `json:"category,omitempty"`
	Message      string    `json:"message,omitempty"`
	Started      time.Time `json:"started,omitempty"`
	Finished     time.Time `json:"finished,omitempty"`
	ReconciledBy string    `json:"reconciled_by,omitempty"`
}

// State is a durable checkpoint, atomically replaced after every transition.
type State struct {
	History  []Result  `json:"history"`
	PlanHash string    `json:"plan_hash"`
	Approval Approval  `json:"approval"`
	Started  time.Time `json:"started"`
	Results  []Result  `json:"results"`
	Aborted  bool      `json:"aborted"`
	Complete bool      `json:"complete"`
}

// Reconciliation records an operator-observed terminal outcome. Absence of a
// process is not success: the operator must verify the expected side effect.
type Reconciliation struct {
	PlanHash            string    `json:"plan_hash"`
	StepID              string    `json:"step_id"`
	Outcome             string    `json:"outcome"`
	Operator            string    `json:"operator"`
	ObservedAt          time.Time `json:"observed_at"`
	Evidence            string    `json:"evidence"`
	NoOutstandingAction bool      `json:"no_outstanding_action"`
}

type journal struct {
	dir   string
	lock  *os.File
	state State
}

func openJournal(p Plan, a Approval, now time.Time, create bool) (*journal, error) {
	h, err := p.Hash()
	if err != nil {
		return nil, err
	}
	if create {
		if err := makeDirectory(p.StateDir); err != nil {
			return nil, err
		}
	}
	lock, err := os.OpenFile(filepath.Join(p.StateDir, "lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("run already locked by another controller")
	}
	j := &journal{dir: p.StateDir, lock: lock}
	if err := ReadJSON(filepath.Join(p.StateDir, "journal.json"), &j.state); err != nil {
		if !errors.Is(err, os.ErrNotExist) || !create {
			_ = j.close()
			return nil, err
		}
		j.state = State{PlanHash: h, Approval: a, Started: now}
		for _, s := range p.Steps {
			j.state.Results = append(j.state.Results, Result{ID: s.ID, Phase: s.Phase, Status: "pending"})
		}
		if err := j.save(); err != nil {
			_ = j.close()
			return nil, err
		}
	}
	if j.state.PlanHash != h || !sameApproval(j.state.Approval, a) || len(j.state.Results) != len(p.Steps) {
		_ = j.close()
		return nil, errors.New("journal does not match plan and original approval")
	}
	for i, r := range j.state.Results {
		if r.ID != p.Steps[i].ID || r.Phase != p.Steps[i].Phase || !contains([]string{"pending", "started", "passed", "failed", "ambiguous", "skipped", "blocked"}, r.Status) {
			_ = j.close()
			return nil, errors.New("invalid journal step sequence")
		}
	}
	return j, nil
}

// WriteJSON durably creates an explicit file without replacing existing input.
func WriteJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func (j *journal) save() error  { return replaceJSON(filepath.Join(j.dir, "journal.json"), j.state) }
func (j *journal) close() error { return j.lock.Close() }

func replaceJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(append(b, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

func makeDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	// Sync newly created directory entries all the way to an existing root;
	// fsync of a checkpoint alone does not persist its parent's creation.
	for dir := path; ; dir = filepath.Dir(dir) {
		if err := syncDirectory(dir); err != nil {
			return err
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

func sameApproval(a, b Approval) bool {
	return a.PlanHash == b.PlanHash && a.Operator == b.Operator && a.Statement == b.Statement && a.ApprovedAt.Equal(b.ApprovedAt)
}

func (j *journal) reconcile(r Reconciliation, now time.Time) error {
	if r.PlanHash != j.state.PlanHash || r.Operator == "" || r.Evidence == "" || !r.NoOutstandingAction || r.ObservedAt.Before(j.state.Started) || r.ObservedAt.After(now) || (r.Outcome != "passed" && r.Outcome != "failed") {
		return errors.New("reconciliation needs matching hash, operator, terminal outcome, dated evidence and no outstanding action")
	}
	for i, x := range j.state.Results {
		if x.ID != r.StepID {
			continue
		}
		if x.Status != "started" && x.Status != "ambiguous" {
			return errors.New("step is not awaiting reconciliation")
		}
		x.Status = r.Outcome
		x.Finished = r.ObservedAt
		x.ReconciledBy = r.Operator
		x.Message = r.Evidence
		if r.Outcome == "failed" {
			x.Category = "infrastructure"
			j.state.Aborted = true
		}
		j.state.Results[i] = x
		j.state.History = append(j.state.History, x)
		return j.save()
	}
	return fmt.Errorf("unknown step %s", r.StepID)
}
