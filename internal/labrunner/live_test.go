package labrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeSmokeAdapter struct {
	calls   []SmokeRequest
	receipt SmokeReceipt
	err     error
}

type liveCleanupExecutor struct {
	forward fakeExecutor
	adapter SmokeAdapter
}

func (f *liveCleanupExecutor) Execute(ctx context.Context, p Plan, s Step, _ string) error {
	if s.Phase == "cleanup" {
		_, err := RunSmokeStep(ctx, p, s.ID, f.adapter)
		return err
	}
	return f.forward.Execute(ctx, p, s, "")
}

func (f *fakeSmokeAdapter) Invoke(_ context.Context, r SmokeRequest) (SmokeReceipt, error) {
	f.calls = append(f.calls, r)
	return f.receipt, f.err
}

func TestLivePlanRejectsUnsafeShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Plan)
	}{
		{"stage1 lifecycle", func(p *Plan) { p.Steps[0].Action = "start-lab" }},
		{"disruption", func(p *Plan) { p.Disruptions = []string{"rocky-downtime"} }},
		{"production mount", func(p *Plan) { p.Live.Volumes[0].GuestMount = "/data/production" }},
		{"raw host path", func(p *Plan) { p.Live.Volumes[0].HostMount = "/zdata/backing" }},
		{"wrong export", func(p *Plan) { p.Live.Volumes[0].Export = "production" }},
		{"resource mismatch", func(p *Plan) { p.Resources[0].Location = "/mnt/production" }},
		{"foreign volume", func(p *Plan) { p.Live.Volumes[0].ID = "hfs-smoke-other-test" }},
		{"missing replica", func(p *Plan) { p.Live.Volumes[0].Replicas = []string{"f0"} }},
		{"repeated replica", func(p *Plan) { p.Live.Volumes[0].Replicas = []string{"f0", "f0"} }},
		{"controller in mount", func(p *Plan) { p.StateDir = p.Live.Volumes[0].GuestMount + "/oracle" }},
		{"missing evidence", func(p *Plan) { p.Live.Stage1Evidence = "missing" }},
		{"unsafe adapter argv", func(p *Plan) { p.Steps[1].Args = []string{"--delete", "/"} }},
		{"skip baseline", func(p *Plan) { p.Tests = p.Tests[:8] }},
		{"skip cleanup", func(p *Plan) { p.Steps = p.Steps[:len(p.Steps)-1] }},
		{"missing cleanup pair", func(p *Plan) { p.Steps[len(p.Steps)-1].AppliesAfter = nil }},
		{"replay workload", func(p *Plan) { p.Steps[4].MaxAttempts = 2 }},
		{"cleanup exceeds deadline", func(p *Plan) { p.Budget.CleanupSeconds = 1 }},
		{"guest allocation", func(p *Plan) { p.Budget.GuestMemoryBytes = 1 << 30 }},
		{"kubernetes not selected", func(p *Plan) { p.Profile = "live-kubernetes" }},
		{"do not preserve failures", func(p *Plan) { p.PreserveFailures = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := liveFixture(t)
			tc.change(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("unsafe live plan accepted")
			}
		})
	}
}

func TestLivePlanApprovalAndReview(t *testing.T) {
	p, a, now := liveFixture(t)
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	input := filepath.Join(t.TempDir(), "proposed.json")
	if err := WriteJSON(input, p); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "reviewed.json")
	if err := CLI(context.Background(), []string{"plan", "--input", input, "--out", output}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != h {
		t.Fatalf("reviewed plan hash differs: %q", out.String())
	}
	p.Steps = nil
	source := filepath.Join(t.TempDir(), "smoke-source.json")
	if err := WriteJSON(source, p); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(t.TempDir(), "generated.json")
	out.Reset()
	if err := CLI(context.Background(), []string{"smoke-plan", "--input", source, "--out", generated, "--step-seconds", "5"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != h {
		t.Fatal("generated schedule changed reviewed plan")
	}
	p.Steps = SmokeSteps(p, 5)
	if err := p.CheckApproval(a, now, false); err != nil {
		t.Fatal(err)
	}
	p.Live.Volumes[0].Epoch++
	if err := p.CheckApproval(a, now, false); err == nil {
		t.Fatal("changed ownership reused approval")
	}
	stage1, oldApproval, _ := fixture(t)
	if p.Stage == stage1.Stage {
		t.Fatal("stage identity lost")
	}
	if err := p.CheckApproval(oldApproval, now, false); err == nil {
		t.Fatal("Stage 1 approval authorized Stage 2")
	}
}

func TestLiveRunnerUsesJournalAndPreservesKnownFailure(t *testing.T) {
	p, a, now := liveFixture(t)
	f := &fakeExecutor{fail: map[string]error{"L03-0": &Error{Category: "assertion", Err: errors.New("rename failure")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	report, err := r.Run(context.Background(), p, a, false, nil)
	if err == nil || report.Success || !report.Complete || !contains(f.calls, "live-collect") || !contains(f.calls, "L10-0") || contains(f.calls, "L04-0") {
		t.Fatalf("unsafe failure path: %+v %v %v", report, f.calls, err)
	}
	f.calls = nil
	if _, err := r.Run(context.Background(), p, a, false, nil); err == nil || len(f.calls) != 0 {
		t.Fatalf("known failed live operation replayed: %v %v", f.calls, err)
	}
	if len(report.RemainingResources) != 1 {
		t.Fatal("failed smoke resources lost from report")
	}
}

func TestLiveDriverValidatesMountAndReplicationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		step   string
		change func(*SmokeReceipt)
	}{
		{"raw directory", "L01-0", func(r *SmokeReceipt) { r.HostFilesystem = "zfs" }},
		{"wrong export", "L01-0", func(r *SmokeReceipt) { r.Export = "raw" }},
		{"wrong owner epoch", "L01-0", func(r *SmokeReceipt) { r.Epoch++ }},
		{"missing mount check", "L01-0", func(r *SmokeReceipt) { r.Checks = r.Checks[1:] }},
		{"missing terminal", "L01-0", func(r *SmokeReceipt) { r.Terminal = false }},
		{"wrong step", "L01-0", func(r *SmokeReceipt) { r.Step = "other" }},
		{"missing replica", "L07-0", func(r *SmokeReceipt) { r.Replicas = r.Replicas[:1] }},
		{"replica lag", "L07-0", func(r *SmokeReceipt) { r.Replicas[1].Applied = 0 }},
		{"checkpoint missing", "L07-0", func(r *SmokeReceipt) { r.Checkpoint = 0 }},
		{"different replica digest", "L08-0", func(r *SmokeReceipt) { r.Replicas[1].Digest = strings.Repeat("f", 64) }},
		{"different checkpoint", "L08-0", func(r *SmokeReceipt) { r.Checkpoint++ }},
		{"empty workload", "L02-0", func(r *SmokeReceipt) { r.IOBytes = 0 }},
		{"workload over budget", "L02-0", func(r *SmokeReceipt) { r.IOBytes = 1 << 60 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := liveFixture(t)
			if tc.step == "L08-0" {
				if err := WriteJSON(smokeOraclePath(p, p.Live.Volumes[0].ID), liveReceipt(t, p, "L07-0")); err != nil {
					t.Fatal(err)
				}
			}
			receipt := liveReceipt(t, p, tc.step)
			tc.change(&receipt)
			f := &fakeSmokeAdapter{receipt: receipt}
			_, err := RunSmokeStep(context.Background(), p, tc.step, f)
			if err == nil {
				t.Fatal("invalid adapter evidence accepted")
			}
		})
	}
}

func TestLiveDriverFunctionalAdapterBoundary(t *testing.T) {
	p, _, _ := liveFixture(t)
	for _, step := range []string{"live-preflight", "create-0", "L01-0", "L02-0", "L07-0", "L08-0", "live-collect"} {
		f := &fakeSmokeAdapter{receipt: liveReceipt(t, p, step)}
		r, err := RunSmokeStep(context.Background(), p, step, f)
		if err != nil || !r.Success || len(f.calls) != 1 || f.calls[0].PlanHash != r.PlanHash || f.calls[0].Plan.Live.Volumes[0].Guest != "r0" {
			t.Fatalf("adapter did not receive exact bounded plan: %+v %v", f.calls, err)
		}
	}
	f := &fakeSmokeAdapter{err: &Error{Category: "infrastructure", Uncertain: true, Err: errors.New("lost guest transport")}}
	_, err := RunSmokeStep(context.Background(), p, "L02-0", f)
	var failure *Error
	if !errors.As(err, &failure) || !failure.Uncertain {
		t.Fatalf("transport loss became terminal success/failure: %v", err)
	}
}

func TestLiveProcessAdapterSeparatesReceiptAndDiagnostics(t *testing.T) {
	p, _, _ := liveFixture(t)
	receipt := liveReceipt(t, p, "L01-0")
	receipt.PlanHash = "REQUEST_PLAN_HASH"
	// stdout cannot replace the diagnostics actually captured by the driver.
	receipt.AdapterDiagnostics = "untrusted stdout diagnostics"
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	setSmokeAdapterFixture(t, &p, string(raw), "ordinary adapter diagnostic")
	actual, err := RunSmokeStep(context.Background(), p, "L01-0", ProcessSmokeAdapter{})
	if err != nil || !actual.Success || actual.AdapterDiagnostics != "ordinary adapter diagnostic\n" {
		t.Fatalf("valid receipt plus stderr did not pass: %+v %v", actual, err)
	}
	encoded, err := json.Marshal(actual)
	if err != nil || !strings.Contains(string(encoded), "ordinary adapter diagnostic") {
		t.Fatalf("captured diagnostics lost from logged receipt: %s %v", encoded, err)
	}
}

func TestLiveProcessAdapterRejectsMalformedReceiptAndPreservesDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, stdout string }{
		{"malformed", `{invalid JSON}`},
		{"truncated", `{"protocol":"hfs-live-v1","terminal":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := liveFixture(t)
			setSmokeAdapterFixture(t, &p, tc.stdout, "bounded failure diagnostics")
			r, err := RunSmokeStep(context.Background(), p, "L01-0", ProcessSmokeAdapter{})
			var failure *Error
			if !errors.As(err, &failure) || !failure.Uncertain || r.AdapterDiagnostics != "bounded failure diagnostics\n" {
				t.Fatalf("invalid receipt lost uncertainty or diagnostics: %+v %v", r, err)
			}
		})
	}
}

func TestLiveProcessAdapterEnforcesAggregateOutputBudget(t *testing.T) {
	p, _, _ := liveFixture(t)
	receipt := liveReceipt(t, p, "L01-0")
	receipt.PlanHash = "REQUEST_PLAN_HASH"
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := strings.Repeat("diagnostic-", 256)
	// Each stream fits individually, but their total exceeds the shared cap.
	limit := int64(len(raw) + len(diagnostics) - 1)
	p.Budget.ArtifactBytes = limit * int64(len(p.Steps)+2)
	setSmokeAdapterFixture(t, &p, string(raw), diagnostics)
	r, err := RunSmokeStep(context.Background(), p, "L01-0", ProcessSmokeAdapter{})
	var failure *Error
	if !errors.As(err, &failure) || !failure.Uncertain || !strings.Contains(err.Error(), "exceeds evidence budget") || int64(len(r.AdapterDiagnostics)) > limit {
		t.Fatalf("aggregate overflow was accepted or diagnostics unbounded: %d %v", len(r.AdapterDiagnostics), err)
	}
}

func TestLiveCleanupUnpublishesAndPreservesFailedData(t *testing.T) {
	p, a, now := liveFixture(t)
	adapter := &fakeSmokeAdapter{receipt: preservedReceipt(t, p)}
	f := &liveCleanupExecutor{forward: fakeExecutor{fail: map[string]error{"L02-0": &Error{Category: "assertion", Err: errors.New("failed bytes")}}}, adapter: adapter}
	report, err := (Runner{Executor: f, Now: func() time.Time { return now }}).Run(context.Background(), p, a, false, nil)
	if err == nil || report.Success || !report.Complete || len(adapter.calls) != 1 || !adapter.calls[0].PreserveFailedData || adapter.receipt.VolumeDeleted || !adapter.receipt.ClientsStopped || len(adapter.receipt.RetainedVolumes) != 1 {
		t.Fatalf("failed cleanup did not unpublish/preserve: %+v %+v %v", report, adapter.calls, err)
	}
	if report.Results[len(report.Results)-1].Status != "passed" || len(report.RemainingResources) != 1 {
		t.Fatal("successful preservation teardown lost its failed run/inventory")
	}
}

func TestLiveCleanupRejectsIncompletePreservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*SmokeReceipt)
	}{
		{"still published", func(r *SmokeReceipt) { r.RetainedVolumes[0].Published = true }},
		{"clients left running", func(r *SmokeReceipt) { r.ClientsStopped = false }},
		{"volume deleted", func(r *SmokeReceipt) { r.VolumeDeleted = true }},
		{"missing inventory", func(r *SmokeReceipt) { r.RetainedVolumes = nil }},
		{"quota exceeded", func(r *SmokeReceipt) { r.RetainedVolumes[0].UsedBytes = 1 << 60 }},
		{"foreign inventory", func(r *SmokeReceipt) { r.RetainedVolumes[0].ID = "other-run" }},
		{"retention changed", func(r *SmokeReceipt) {
			r.RetainedVolumes[0].RetainUntil = r.RetainedVolumes[0].RetainUntil.Add(time.Hour)
		}},
		{"unpublish unsupported", func(r *SmokeReceipt) { r.Success = false; r.Message = "normal unpublish unsupported" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, a, now := liveFixture(t)
			receipt := preservedReceipt(t, p)
			tc.change(&receipt)
			adapter := &fakeSmokeAdapter{receipt: receipt}
			f := &liveCleanupExecutor{forward: fakeExecutor{fail: map[string]error{"L02-0": &Error{Category: "assertion", Err: errors.New("failed bytes")}}}, adapter: adapter}
			report, err := (Runner{Executor: f, Now: func() time.Time { return now }}).Run(context.Background(), p, a, false, nil)
			last := report.Results[len(report.Results)-1]
			if err == nil || report.Success || report.Complete || len(adapter.calls) != 1 || !adapter.calls[0].PreserveFailedData || last.Status != "failed" || last.Category != "cleanup" {
				t.Fatalf("incomplete preservation accepted: %+v %v", report, err)
			}
		})
	}
}

func TestLiveCleanupDeletesOnlySuccessfulRunResources(t *testing.T) {
	p, a, now := liveFixture(t)
	adapter := &fakeSmokeAdapter{receipt: liveReceipt(t, p, "L10-0")}
	f := &liveCleanupExecutor{adapter: adapter}
	report, err := (Runner{Executor: f, Now: func() time.Time { return now }}).Run(context.Background(), p, a, false, nil)
	if err != nil || !report.Success || len(adapter.calls) != 1 || adapter.calls[0].PreserveFailedData || !adapter.receipt.VolumeDeleted || !adapter.receipt.NoResidue || len(adapter.receipt.RetainedVolumes) != 0 {
		t.Fatalf("successful cleanup did not use strict delete contract: %+v %v", report, err)
	}
}

func TestLiveCleanupHistoricalFailureStillPreservesData(t *testing.T) {
	p, a, now := liveFixture(t)
	if _, err := (Runner{Executor: &fakeExecutor{}, Now: func() time.Time { return now }}).Run(context.Background(), p, a, false, nil); err != nil {
		t.Fatal(err)
	}
	var state State
	path := filepath.Join(p.StateDir, "journal.json")
	if err := ReadJSON(path, &state); err != nil {
		t.Fatal(err)
	}
	// An older/recovered snapshot can have passed current cleanup while its
	// historical failure still makes acceptance unsuccessful.
	state.Aborted = false
	state.History = append(state.History, Result{ID: "L10-0", Phase: "cleanup", Status: "failed", Category: "cleanup"})
	if err := replaceJSON(path, state); err != nil {
		t.Fatal(err)
	}
	f := &fakeSmokeAdapter{receipt: preservedReceipt(t, p)}
	if _, err := RunSmokeStep(context.Background(), p, "L10-0", f); err != nil || len(f.calls) != 1 || !f.calls[0].PreserveFailedData {
		t.Fatalf("historical failure lost preservation: %v %v", f.calls, err)
	}
}

func TestLiveCleanupCreationFailureReportsConfirmedAbsence(t *testing.T) {
	p, a, now := liveFixture(t)
	receipt := preservedReceipt(t, p)
	receipt.RetainedVolumes[0].Exists = false
	receipt.RetainedVolumes[0].UsedBytes = 0
	receipt.Epoch = 0
	receipt.Filesystem = ""
	receipt.HostFilesystem = ""
	adapter := &fakeSmokeAdapter{receipt: receipt}
	f := &liveCleanupExecutor{forward: fakeExecutor{fail: map[string]error{"create-0": &Error{Category: "infrastructure", Err: errors.New("creation did not allocate a volume")}}}, adapter: adapter}
	report, err := (Runner{Executor: f, Now: func() time.Time { return now }}).Run(context.Background(), p, a, false, nil)
	if err == nil || report.Success || !report.Complete || len(adapter.calls) != 1 || !adapter.calls[0].PreserveFailedData {
		t.Fatalf("confirmed absent-volume teardown did not finish failed run: %+v %v", report, err)
	}
}

func TestLiveStage1CandidateGate(t *testing.T) {
	p, _, _ := liveFixture(t)
	if err := verifyStage1(p); err != nil {
		t.Fatal(err)
	}
	for i, a := range p.Artifacts {
		if a.ID == p.Live.Candidate {
			p.Artifacts[i].Version = "different-release"
		}
	}
	if err := verifyStage1(p); err == nil {
		t.Fatal("untested candidate passed entry gate")
	}
}

func liveFixture(t *testing.T) (Plan, Approval, time.Time) {
	t.Helper()
	earlier, approval, now := fixture(t)
	result, err := (Runner{Executor: &fakeExecutor{}, Now: func() time.Time { return now }}).Run(context.Background(), earlier, approval, false, nil)
	if err != nil || !result.Success {
		t.Fatal(err)
	}
	root := t.TempDir()
	p := Plan{Schema: 1, RunID: "live1", Stage: "stage2", Created: earlier.Created, Expires: earlier.Expires, Profile: "live-smoke", PreserveFailures: true, StateDir: filepath.Join(root, "state"), EvidenceDir: filepath.Join(root, "evidence"), Budget: Budget{HostHeadroomBytes: 1 << 30, DiskBytes: 32 << 20, IOBytes: 16 << 20, ArtifactBytes: 2 << 20, RetentionHours: 24, RuntimeSeconds: 120, CleanupSeconds: 30}}
	p.Live = &LivePlan{Driver: "driver", Adapter: "adapter", Probe: "probe", Candidate: "candidate", KnownHosts: "keys", Stage1Plan: "stage1-plan", Stage1Evidence: "stage1-evidence", DeploymentEvidence: "deployment", UID: 2001, GID: 2001, DeniedUID: 2002, MaxOperations: 1000, Volumes: []SmokeVolume{{ID: "hfs-smoke-live1-f0", Guest: "r0", Owner: "f0", Epoch: 4, Replicas: []string{"f0", "f1", "f2"}, GuestMount: "/mnt/hfs-smoke/hfs-smoke-live1-f0", HostMount: "/var/run/homelabfs/smoke/hfs-smoke-live1-f0", Export: "hfs-smoke-live1-f0", MaxBytes: 16 << 20}}}
	p.Targets = []Target{{Name: "r0", Host: "f0", Address: "fake-address", InitialState: "Running", Kernel: "fake-kernel", Packages: "fake-packages"}}
	v := p.Live.Volumes[0]
	p.Resources = []Resource{{Name: v.ID, Kind: "smoke-volume", Target: v.Guest, Location: v.GuestMount, MaxBytes: v.MaxBytes}}
	for i := 1; i <= 9; i++ {
		p.Tests = append(p.Tests, Selection{Name: fmtTest(i), Seed: int64(i)})
	}
	for _, id := range []string{"driver", "adapter", "probe", "candidate", "keys", "stage1-plan", "stage1-evidence", "deployment"} {
		path := filepath.Join(root, id)
		data := []byte("fixture-only")
		switch id {
		case "stage1-plan":
			data, err = json.Marshal(earlier)
		case "stage1-evidence":
			data, err = json.Marshal(result)
		case "candidate":
			data, err = os.ReadFile(earlier.Artifacts[0].Path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		p.Artifacts = append(p.Artifacts, Artifact{ID: id, Path: path, Version: earlier.Artifacts[0].Version, SHA256: hex.EncodeToString(sum[:])})
	}
	p.Steps = SmokeSteps(p, 5)
	if err := makeDirectory(p.StateDir); err != nil {
		t.Fatal(err)
	}
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return p, Approval{PlanHash: h, Operator: "Paul", ApprovedAt: now, Statement: "I approve this exact plan"}, now
}

func fmtTest(i int) string { return "L0" + string(rune('0'+i)) }

func liveReceipt(t *testing.T, p Plan, id string) SmokeReceipt {
	t.Helper()
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	var s Step
	for _, step := range p.Steps {
		if step.ID == id {
			s = step
		}
	}
	parts := strings.Split(s.Contract, ":")
	v := p.Live.Volumes[0]
	r := SmokeReceipt{Protocol: "hfs-live-v1", PlanHash: h, Step: id, Volume: parts[2], Terminal: true, Success: true, Guest: v.Guest, Owner: v.Owner, Epoch: v.Epoch, Filesystem: "9p", HostFilesystem: "homelabfs-fuse", Mount: v.GuestMount, HostMount: v.HostMount, Export: v.Export}
	switch parts[1] {
	case "preflight":
		r.Checks = []string{"stage1-candidate-pass", "deployment-authorized", "versions", "pinned-ssh", "headroom", "health-before"}
	case "create":
		r.Checks = []string{"dedicated-volume", "quota", "local-primary", "run-ownership"}
	case "L01":
		r.Checks = []string{"live-mount-chain", "9p-capabilities", "owner-ready", "replicas-ready", "versions", "numeric-identities"}
	case "L02":
		r.Checks = []string{"write-fsync-reopen-checksum"}
		r.IOBytes = 8192
		r.Operations = 8
	case "L07":
		r.Checks = []string{"sync-contract", "async-catchup"}
	case "L08":
		r.Checks = []string{"checkpoint-inspection", "content-metadata-match"}
	case "collect":
		r.Checks = []string{"versions", "checksums", "operation-history", "replication-positions", "health"}
	case "L10":
		r.Checks = []string{"clients-stopped", "normal-unpublish", "volume-deleted", "no-residue", "health-unchanged"}
		r.ClientsStopped = true
		r.VolumeDeleted = true
		r.NoResidue = true
	}
	if parts[1] == "L07" || parts[1] == "L08" {
		r.Checkpoint = 9
		r.OracleDigest = strings.Repeat("a", 64)
		for _, node := range v.Replicas {
			r.Replicas = append(r.Replicas, ReplicaPoint{Node: node, Received: 9, Durable: 9, Applied: 9, Digest: strings.Repeat("a", 64)})
		}
	}
	if reflect.DeepEqual(r.Checks, []string(nil)) {
		t.Fatalf("unsupported fixture step %s", id)
	}
	r.Evidence = map[string]json.RawMessage{}
	for _, check := range r.Checks {
		r.Evidence[check] = json.RawMessage(`{"fixture":"fake-only"}`)
	}
	return r
}

func preservedReceipt(t *testing.T, p Plan) SmokeReceipt {
	t.Helper()
	r := liveReceipt(t, p, "L10-0")
	v := p.Live.Volumes[0]
	r.PreservedFailedData = true
	r.VolumeDeleted = false
	r.NoResidue = false
	r.Checks = []string{"clients-stopped", "normal-unpublish", "failed-data-preserved", "retained-inventory", "health-unchanged"}
	r.Evidence = map[string]json.RawMessage{}
	for _, check := range r.Checks {
		r.Evidence[check] = json.RawMessage(`{"fixture":"fake-only"}`)
	}
	r.RetainedVolumes = []RetainedSmokeVolume{{ID: v.ID, Guest: v.Guest, Owner: v.Owner, Exists: true, Published: false, MaxBytes: v.MaxBytes, UsedBytes: 4096, RetainUntil: p.Created.Add(time.Duration(p.Budget.RetentionHours) * time.Hour)}}
	return r
}

func setSmokeAdapterFixture(t *testing.T, p *Plan, stdout, stderr string) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("local executable fixtures require Bash")
	}
	shellLiteral := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	// The executable reads only its bounded JSON request and writes fixture
	// streams. It never contacts a guest, invokes SSH or accesses a mount.
	script := "#!/usr/bin/env bash\nset -euo pipefail\n" +
		"IFS= read -r request || test -n \"$request\"\n" +
		"if [[ \"$request\" =~ \\\"plan_hash\\\":\\\"([0-9a-f]{64})\\\" ]]; then\n" +
		"    plan_hash=\"${BASH_REMATCH[1]}\"\nelse\n    exit 20\nfi\n" +
		"receipt=" + shellLiteral(stdout) + "\n" +
		"receipt=\"${receipt//REQUEST_PLAN_HASH/$plan_hash}\"\n" +
		"printf '%s\\n' \"$receipt\"\n" +
		"printf '%s\\n' " + shellLiteral(stderr) + " >&2\n"
	for i, a := range p.Artifacts {
		if a.ID == p.Live.Adapter {
			if err := os.WriteFile(a.Path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(a.Path, 0700); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(script))
			p.Artifacts[i].SHA256 = hex.EncodeToString(sum[:])
			return
		}
	}
	t.Fatal("missing fixture adapter")
}
