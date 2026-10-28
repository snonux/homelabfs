package labrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeExecutor struct {
	calls []string
	fail  map[string]error
}

var _ Executor = (*fakeExecutor)(nil)

func (f *fakeExecutor) Execute(_ context.Context, _ Plan, s Step, _ string) error {
	f.calls = append(f.calls, s.ID)
	return f.fail[s.ID]
}

func TestPlanBindingAndRejection(t *testing.T) {
	p, a, now := fixture(t)
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(p, "", "    ")
	if err != nil {
		t.Fatal(err)
	}
	var decoded Plan
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	other, err := decoded.Hash()
	if err != nil || h != other {
		t.Fatalf("formatting changed semantic hash: %s %v", other, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Plan, *Approval)
		at     time.Time
	}{
		{"wrong hash", func(_ *Plan, a *Approval) { a.PlanHash = strings.Repeat("0", 64) }, now},
		{"changed budget", func(p *Plan, _ *Approval) { p.Budget.IOBytes++ }, now},
		{"changed argv", func(p *Plan, _ *Approval) { p.Steps[4].Args = []string{"different"} }, now},
		{"changed seed", func(p *Plan, _ *Approval) { p.Tests[0].Seed++ }, now},
		{"missing operator", func(_ *Plan, a *Approval) { a.Operator = "" }, now},
		{"missing statement", func(_ *Plan, a *Approval) { a.Statement = "yes" }, now},
		{"future approval", func(_ *Plan, a *Approval) { a.ApprovedAt = now.Add(time.Minute) }, now},
		{"expired", func(_ *Plan, _ *Approval) {}, now.Add(2 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var copy Plan
			raw, _ := json.Marshal(p)
			if err := json.Unmarshal(raw, &copy); err != nil {
				t.Fatal(err)
			}
			approval := a
			tc.change(&copy, &approval)
			if err := copy.CheckApproval(approval, tc.at, false); err == nil {
				t.Fatal("accepted changed or stale authorization")
			}
		})
	}
	if err := p.CheckApproval(a, now.Add(48*time.Hour), true); err != nil {
		t.Fatalf("approved recovery must remain possible after expiry: %v", err)
	}
}

func TestPlanRejectsUnsafeShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Plan)
	}{
		{"provision", func(p *Plan) { p.PreparedGuests = false }},
		{"unsafe run ID", func(p *Plan) { p.RunID = "../escape" }},
		{"unsafe step ID", func(p *Plan) { p.Steps[0].ID = "../escape" }},
		{"production resource", func(p *Plan) { p.Resources[0].Location = "/production/hfs-run-run1" }},
		{"unbounded attempts", func(p *Plan) { p.Steps[0].MaxAttempts = 100 }},
		{"missing cleanup pair", func(p *Plan) { p.Steps[6].AppliesAfter = []string{"preflight"} }},
		{"downtime unapproved", func(p *Plan) { p.Disruptions = nil }},
		{"wrong initial state", func(p *Plan) { p.Targets[0].InitialState = "Running" }},
		{"wrong lifecycle args", func(p *Plan) { p.Steps[1].Args = []string{"--force"} }},
		{"memory overcommit", func(p *Plan) { p.Budget.GuestMemoryBytes = 12 << 30 }},
		{"unbounded deadline", func(p *Plan) { p.Steps[0].TimeoutSeconds = 86401 }},
		{"resource budget overflow", func(p *Plan) { p.Resources[0].MaxBytes = p.Budget.DiskBytes + 1 }},
		{"selected test without driver", func(p *Plan) { p.Steps[4].Phase = "setup"; p.Steps[4].Test = "" }},
		{"unselected test driver", func(p *Plan) { p.Steps[4].Test = "unselected" }},
		{"non-test phase claims coverage", func(p *Plan) { p.Steps[3].Test = "synthetic" }},
		{"duplicate selection", func(p *Plan) { p.Tests = append(p.Tests, p.Tests[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := fixture(t)
			tc.change(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("accepted unsafe plan")
			}
		})
	}
}

func TestRunAndIdempotentResume(t *testing.T) {
	p, a, now := fixture(t)
	f := &fakeExecutor{}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	rep, err := r.Run(context.Background(), p, a, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Success || !rep.Complete {
		t.Fatalf("bad report: %+v", rep)
	}
	want := []string{"preflight", "rocky-stop", "lab-start", "install", "exercise", "collect", "data-clean", "lab-stop", "rocky-restore"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("calls %v", f.calls)
	}
	f.calls = nil
	rep, err = r.Run(context.Background(), p, a, false, nil)
	if err != nil || !rep.Success || len(f.calls) != 0 {
		t.Fatalf("completed resume replayed actions: %v %v", f.calls, err)
	}
	if len(rep.History) != 2*len(p.Steps) {
		t.Fatalf("history lost transitions: %d", len(rep.History))
	}
	var suite junitSuite
	b, err := os.ReadFile(filepath.Join(p.EvidenceDir, "report.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal(b, &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Tests != len(p.Steps) || suite.Failures != 0 {
		t.Fatalf("bad JUnit: %+v", suite)
	}
}

func TestAssertionFailureCollectsAndRestores(t *testing.T) {
	p, a, now := fixture(t)
	f := &fakeExecutor{fail: map[string]error{"exercise": &Error{Category: "assertion", Err: errors.New("bad checksum")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	rep, err := r.Run(context.Background(), p, a, false, nil)
	if err == nil || rep.Success || !rep.Complete {
		t.Fatalf("failure hidden: %+v %v", rep, err)
	}
	if rep.Results[4].Category != "assertion" || !contains(f.calls, "collect") || !contains(f.calls, "rocky-restore") || len(rep.RemainingResources) == 0 {
		t.Fatalf("lost failure/evidence/restoration: %+v calls %v", rep, f.calls)
	}
	f.calls = nil
	if _, err := r.Run(context.Background(), p, a, false, nil); err == nil || len(f.calls) != 0 {
		t.Fatalf("failed completed run became successful: %v %v", f.calls, err)
	}
}

func TestUncertainSideEffectRequiresReconciliation(t *testing.T) {
	p, a, now := fixture(t)
	f := &fakeExecutor{fail: map[string]error{"lab-start": &Error{Category: "infrastructure", Uncertain: true, Err: errors.New("SSH lost after remote start")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	rep, err := r.Run(context.Background(), p, a, false, nil)
	if err == nil || rep.Complete || contains(f.calls, "lab-stop") {
		t.Fatalf("unsafe compensation for uncertain mutation: %v %+v", f.calls, rep)
	}
	f.calls = nil
	if _, err := r.Run(context.Background(), p, a, true, nil); err == nil || len(f.calls) != 0 {
		t.Fatalf("recovery replayed unresolved mutation: %v %v", f.calls, err)
	}
	h, _ := p.Hash()
	rec := Reconciliation{PlanHash: h, StepID: "lab-start", Outcome: "failed", Operator: "Paul", ObservedAt: now, Evidence: "verified remote command terminal; lab partially started", NoOutstandingAction: true}
	rep, err = r.Run(context.Background(), p, a, true, &rec)
	if err == nil || !rep.Complete || rep.Success {
		t.Fatalf("cleanup must retain failed run: %+v %v", rep, err)
	}
	want := []string{"lab-stop", "rocky-restore"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("unsafe recovery calls: %v", f.calls)
	}
}

func TestCrashAfterPersistedStartDoesNotReplay(t *testing.T) {
	p, a, now := fixture(t)
	j, err := openJournal(p, a, now, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		j.state.Results[i].Status = "passed"
	}
	j.state.Results[3].Status = "started"
	j.state.Results[3].Started = now
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	if err := j.close(); err != nil {
		t.Fatal(err)
	}
	f := &fakeExecutor{}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	rep, err := r.Run(context.Background(), p, a, false, nil)
	if err == nil || len(f.calls) != 0 || rep.Complete {
		t.Fatalf("crash state replayed: %v %+v", f.calls, rep)
	}
}

func TestPersistedForwardFailureNeverReplays(t *testing.T) {
	p, a, now := fixture(t)
	j, err := openJournal(p, a, now, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		j.state.Results[i].Status = "passed"
	}
	f := &fakeExecutor{fail: map[string]error{"exercise": &Error{Category: "assertion", Err: errors.New("checksum mismatch")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	if err := r.step(t.Context(), p, j, 4); err == nil {
		t.Fatal("expected assertion failure")
	}
	var stored State
	if err := ReadJSON(filepath.Join(p.StateDir, "journal.json"), &stored); err != nil {
		t.Fatal(err)
	}
	if !stored.Aborted || stored.Results[4].Status != "failed" {
		t.Fatalf("failure latch not atomic with terminal result: %+v", stored)
	}
	// Also accept an old checkpoint interrupted between the terminal-result
	// write and the abort write. It must collect/clean up without retrying.
	j.state.Aborted = false
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	if err := j.close(); err != nil {
		t.Fatal(err)
	}
	f.calls, f.fail = nil, nil
	rep, err := r.Run(t.Context(), p, a, false, nil)
	if err == nil || !rep.Complete || rep.Success || rep.Results[4].Status != "failed" {
		t.Fatalf("persisted failure erased: %+v %v", rep, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"collect", "data-clean", "lab-stop", "rocky-restore"}) {
		t.Fatalf("failed forward operation replayed: %v", f.calls)
	}
}

func TestTeardownFailureRemainsUnsuccessfulAfterRecovery(t *testing.T) {
	for _, step := range []string{"lab-stop", "rocky-restore"} {
		for _, cleanup := range []bool{false, true} {
			t.Run(step+map[bool]string{false: "/execute", true: "/cleanup"}[cleanup], func(t *testing.T) {
				p, a, now := fixture(t)
				f := &fakeExecutor{fail: map[string]error{step: &Error{Category: "infrastructure", Err: errors.New("known terminal failure")}}}
				r := Runner{Executor: f, Now: func() time.Time { return now }}
				if rep, err := r.Run(t.Context(), p, a, false, nil); err == nil || rep.Success || rep.Complete {
					t.Fatalf("initial failure hidden: %+v %v", rep, err)
				}
				f.calls, f.fail = nil, nil
				rep, err := r.Run(t.Context(), p, a, cleanup, nil)
				if err == nil || !rep.Complete || rep.Success {
					t.Fatalf("recovery manufactured success: %+v %v", rep, err)
				}
				want := []string{"rocky-restore"}
				if step == "lab-stop" {
					want = []string{"lab-stop", "rocky-restore"}
				}
				if !reflect.DeepEqual(f.calls, want) {
					t.Fatalf("unexpected recovery actions: %v", f.calls)
				}
				var suite junitSuite
				b, err := os.ReadFile(filepath.Join(p.EvidenceDir, "report.xml"))
				if err != nil {
					t.Fatal(err)
				}
				if err := xml.Unmarshal(b, &suite); err != nil {
					t.Fatal(err)
				}
				if suite.Failures == 0 {
					t.Fatalf("JUnit lost historical failure: %+v", suite)
				}
			})
		}
	}
}

func TestReconciledSuccessResumesForward(t *testing.T) {
	p, a, now := fixture(t)
	f := &fakeExecutor{fail: map[string]error{"install": &Error{Category: "infrastructure", Uncertain: true, Err: errors.New("controller lost terminal result")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	if _, err := r.Run(context.Background(), p, a, false, nil); err == nil {
		t.Fatal("uncertain installation reported success")
	}
	f.calls = nil
	f.fail = nil
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	rec := Reconciliation{PlanHash: h, StepID: "install", Outcome: "passed", Operator: "Paul", ObservedAt: now, Evidence: "verified terminal driver and complete expected installation", NoOutstandingAction: true}
	rep, err := r.Run(context.Background(), p, a, false, &rec)
	if err != nil || !rep.Success {
		t.Fatalf("reconciled success did not resume: %+v %v", rep, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"exercise", "collect", "data-clean", "lab-stop", "rocky-restore"}) {
		t.Fatalf("replayed or skipped work: %v", f.calls)
	}
}

func TestCleanupFailureBlocksRestorationAndCanResume(t *testing.T) {
	p, a, now := fixture(t)
	f := &fakeExecutor{fail: map[string]error{"lab-stop": &Error{Category: "infrastructure", Err: errors.New("known stop failure")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	rep, err := r.Run(context.Background(), p, a, false, nil)
	if err == nil || rep.Complete || contains(f.calls, "rocky-restore") || rep.Results[7].Category != "cleanup" {
		t.Fatalf("restored Rocky with lab shutdown incomplete: %v %+v", f.calls, rep)
	}
	f.calls = nil
	f.fail = nil
	rep, err = r.Run(context.Background(), p, a, true, nil)
	if err == nil || !rep.Complete || rep.Success {
		t.Fatalf("cleanup-only result should retain abortion: %+v %v", rep, err)
	}
	if !reflect.DeepEqual(f.calls, []string{"lab-stop", "rocky-restore"}) {
		t.Fatalf("resume replayed completed work: %v", f.calls)
	}
}

func TestRejectBeforeExecutor(t *testing.T) {
	for _, kind := range []string{"approval", "artifact", "snapshot"} {
		t.Run(kind, func(t *testing.T) {
			p, a, now := fixture(t)
			switch kind {
			case "approval":
				a.Statement = ""
			case "artifact":
				if err := os.WriteFile(p.Artifacts[0].Path, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "snapshot":
				if err := os.MkdirAll(p.StateDir, 0700); err != nil {
					t.Fatal(err)
				}
				bad := p
				bad.Profile = "changed"
				if err := WriteJSON(filepath.Join(p.StateDir, "plan.json"), bad); err != nil {
					t.Fatal(err)
				}
			}
			f := &fakeExecutor{}
			if _, err := (Runner{Executor: f, Now: func() time.Time { return now }}).Run(context.Background(), p, a, false, nil); err == nil || len(f.calls) != 0 {
				t.Fatalf("unsafe action before rejection: %v %v", f.calls, err)
			}
		})
	}
}

func TestBoundedLogReportsPersistedPrefix(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	w := boundedLog{file: f, remaining: 3}
	n, err := w.Write([]byte("abcdef"))
	if n != 3 || err == nil {
		t.Fatalf("Write reported %d %v", n, err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil || string(b) != "abc" {
		t.Fatalf("stored %q %v", b, err)
	}
}

func fixture(t *testing.T) (Plan, Approval, time.Time) {
	t.Helper()
	root := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var artifacts []Artifact
	for _, name := range []string{"driver", "vms.sh"} {
		path := filepath.Join(root, name)
		b := []byte("immutable " + name)
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(b)
		id := name
		if name == "vms.sh" {
			id = "vms-helper"
		}
		artifacts = append(artifacts, Artifact{ID: id, Path: path, Version: "fixture-v1", SHA256: hex.EncodeToString(sum[:])})
	}
	p := Plan{Schema: 1, RunID: "run1", Stage: "stage1", Created: now.Add(-time.Minute), Expires: now.Add(time.Hour), Profile: "fake-native", PreparedGuests: true, Artifacts: artifacts, StateDir: filepath.Join(root, "state"), EvidenceDir: filepath.Join(root, "evidence"), PreserveFailures: true, Disruptions: []string{"rocky-downtime"}, Tests: []Selection{{Name: "synthetic", Seed: 42}}, Resources: []Resource{{Name: "payload", Kind: "directory", Target: "hfs-test0", Location: "/var/tmp/hfs-run-run1", MaxBytes: 1 << 20}}, Budget: Budget{GuestMemoryBytes: 6 << 30, HostHeadroomBytes: 4 << 30, CPUs: 6, DiskBytes: 1 << 30, IOBytes: 1 << 20, ArtifactBytes: 1 << 20, RetentionHours: 24, RuntimeSeconds: 60, CleanupSeconds: 30, RestorationSeconds: 10}}
	for _, name := range []string{"hfs-test0", "hfs-test1", "hfs-test2", "rocky", "freebsd"} {
		state := "Stopped"
		if name == "rocky" {
			state = "Running"
		}
		p.Targets = append(p.Targets, Target{Name: name, Host: "f3", Address: "fixture", InitialState: state, Kernel: "fixture-kernel", Packages: "fixture-package-manifest"})
	}
	p.Steps = []Step{
		{ID: "preflight", Phase: "setup", Action: "driver", Artifact: "driver", Contract: "prepared-lab-preflight-v1", Effects: "read-only"},
		{ID: "rocky-stop", Phase: "setup", Action: "stop-rocky", Artifact: "vms-helper", Contract: "vm-lifecycle-v1", Effects: "lifecycle"},
		{ID: "lab-start", Phase: "setup", Action: "start-lab", Artifact: "vms-helper", Contract: "vm-lifecycle-v1", Effects: "lifecycle"},
		{ID: "install", Phase: "setup", Action: "driver", Artifact: "driver", Args: []string{"install"}, Contract: "run-owned-driver-v1", Effects: "run-owned"},
		{ID: "exercise", Phase: "test", Test: "synthetic", Action: "driver", Artifact: "driver", Args: []string{"synthetic"}, Contract: "run-owned-driver-v1", Effects: "run-owned"},
		{ID: "collect", Phase: "collect", Action: "driver", Artifact: "driver", Args: []string{"collect"}, Contract: "evidence-v1", Effects: "read-only"},
		{ID: "data-clean", Phase: "cleanup", Action: "driver", Artifact: "driver", Args: []string{"cleanup"}, Contract: "preserve-failed-data-v1", Effects: "run-owned", Idempotent: true, AppliesAfter: []string{"install", "exercise"}},
		{ID: "lab-stop", Phase: "cleanup", Action: "stop-lab", Artifact: "vms-helper", Contract: "vm-lifecycle-v1", Effects: "lifecycle", Idempotent: true, AppliesAfter: []string{"lab-start"}},
		{ID: "rocky-restore", Phase: "restore", Action: "start-rocky", Artifact: "vms-helper", Contract: "vm-lifecycle-v1", Effects: "lifecycle", Idempotent: true, AppliesAfter: []string{"rocky-stop"}},
	}
	for i := range p.Steps {
		p.Steps[i].TimeoutSeconds = 5
		p.Steps[i].MaxAttempts = 2
	}
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return p, Approval{PlanHash: h, Operator: "Paul", ApprovedAt: now, Statement: "I approve this exact plan"}, now
}
