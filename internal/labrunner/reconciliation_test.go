package labrunner

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReconciliationRejectsObservationBeforeStepStart(t *testing.T) {
	for _, step := range []string{"install", "lab-stop"} {
		for _, status := range []string{"started", "ambiguous"} {
			for _, outcome := range []string{"passed", "failed"} {
				for _, cleanup := range []bool{false, true} {
					mode := map[bool]string{false: "execute", true: "cleanup"}[cleanup]
					t.Run(strings.Join([]string{step, status, outcome, mode}, "/"), func(t *testing.T) {
						p, a, r, f, uncertain := uncertainReconciliationFixture(t, step, status)
						path := filepath.Join(p.StateDir, "journal.json")
						before, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						var state State
						if err := ReadJSON(path, &state); err != nil {
							t.Fatal(err)
						}
						rec := Reconciliation{PlanHash: a.PlanHash, StepID: step, Outcome: outcome, Operator: "Paul", ObservedAt: uncertain.Started.Add(-time.Nanosecond), Evidence: "observation before this attempt was dispatched", NoOutstandingAction: true}
						if !rec.ObservedAt.After(state.Started) {
							t.Fatal("fixture must isolate step chronology from the run-start guard")
						}
						rep, err := r.Run(t.Context(), p, a, cleanup, &rec)
						if err == nil || !strings.Contains(err.Error(), "observation predates step start") || rep.Complete || len(f.calls) != 0 {
							t.Fatalf("stale observation released uncertain step: calls=%v report=%+v err=%v", f.calls, rep, err)
						}
						after, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(before, after) {
							t.Fatal("rejected reconciliation changed durable journal")
						}
					})
				}
			}
		}
	}
}

func TestReconciliationAcceptsObservationSinceStepStart(t *testing.T) {
	for _, tc := range []struct {
		name    string
		step    string
		outcome string
		cleanup bool
		want    []string
	}{
		{"forward-passed", "install", "passed", false, []string{"exercise", "collect", "data-clean", "lab-stop", "rocky-restore"}},
		{"forward-failed", "install", "failed", false, []string{"collect", "data-clean", "lab-stop", "rocky-restore"}},
		{"cleanup-forward-passed", "install", "passed", true, []string{"data-clean", "lab-stop", "rocky-restore"}},
		{"cleanup-forward-failed", "install", "failed", true, []string{"data-clean", "lab-stop", "rocky-restore"}},
		{"teardown-passed", "lab-stop", "passed", false, []string{"rocky-restore"}},
		{"cleanup-teardown-passed", "lab-stop", "passed", true, []string{"rocky-restore"}},
	} {
		for _, observation := range []string{"at-start", "during-step", "after-finish"} {
			t.Run(tc.name+"/"+observation, func(t *testing.T) {
				p, a, r, f, uncertain := uncertainReconciliationFixture(t, tc.step, "ambiguous")
				observedAt := uncertain.Started
				switch observation {
				case "during-step":
					// An independent observer can establish terminality before the
					// controller notices a transport failure and records Finished.
					observedAt = uncertain.Started.Add(uncertain.Finished.Sub(uncertain.Started) / 2)
				case "after-finish":
					observedAt = uncertain.Finished.Add(time.Nanosecond)
				}
				rec := Reconciliation{PlanHash: a.PlanHash, StepID: tc.step, Outcome: tc.outcome, Operator: "Paul", ObservedAt: observedAt, Evidence: "independently verified terminal outcome and expected effects", NoOutstandingAction: true}
				rep, err := r.Run(t.Context(), p, a, tc.cleanup, &rec)
				wantSuccess := tc.outcome == "passed" && !tc.cleanup
				if !rep.Complete || rep.Success != wantSuccess || (err == nil) != wantSuccess {
					t.Fatalf("fresh reconciliation did not resume: report=%+v err=%v", rep, err)
				}
				if !reflect.DeepEqual(f.calls, tc.want) {
					t.Fatalf("reconciliation replayed or skipped work: got %v want %v", f.calls, tc.want)
				}
				for _, result := range rep.Results {
					if result.ID == tc.step && (result.Status != tc.outcome || result.ReconciledBy != rec.Operator || !result.Finished.Equal(rec.ObservedAt)) {
						t.Fatalf("reconciled result lost observation: %+v", result)
					}
				}
			})
		}
	}
}

func uncertainReconciliationFixture(t *testing.T, step, status string) (Plan, Approval, Runner, *fakeExecutor, Result) {
	t.Helper()
	p, a, now := fixture(t)
	f := &fakeExecutor{fail: map[string]error{step: &Error{Category: "infrastructure", Uncertain: true, Err: errors.New("lost terminal result")}}}
	r := Runner{Executor: f, Now: func() time.Time {
		now = now.Add(100 * time.Millisecond)
		return now
	}}
	rep, err := r.Run(t.Context(), p, a, false, nil)
	if err == nil || rep.Complete {
		t.Fatalf("fixture did not stop at uncertain step: %+v %v", rep, err)
	}
	path := filepath.Join(p.StateDir, "journal.json")
	var state State
	if err := ReadJSON(path, &state); err != nil {
		t.Fatal(err)
	}
	for i, result := range state.Results {
		if result.ID != step {
			continue
		}
		if result.Status != "ambiguous" || !result.Started.After(state.Started) || !result.Finished.After(result.Started) {
			t.Fatalf("fixture lacks advancing attempt chronology: %+v", result)
		}
		if status == "started" {
			// Model controller death after its durable start checkpoint.
			result.Status = "started"
			result.Finished = time.Time{}
			result.Category, result.Message = "", ""
			state.Results[i] = result
			state.History = state.History[:len(state.History)-1]
			if err := replaceJSON(path, state); err != nil {
				t.Fatal(err)
			}
		}
		f.calls, f.fail = nil, nil
		return p, a, r, f, result
	}
	t.Fatalf("fixture lacks uncertain step %s", step)
	return Plan{}, Approval{}, Runner{}, nil, Result{}
}
