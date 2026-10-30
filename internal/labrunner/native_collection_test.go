package labrunner

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNativeCollectionAdmissionGate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failed    string
		uncertain bool
		want      []string
	}{
		{"admission", "preflight", false, []string{"preflight"}},
		{"rocky stop", "rocky-stop", false, []string{"preflight", "rocky-stop", "rocky-restore"}},
		{"lab start", "lab-start", false, []string{"preflight", "rocky-stop", "lab-start", "lab-stop", "rocky-restore"}},
		{"partial setup", "native-setup", false, []string{"preflight", "rocky-stop", "lab-start", "native-setup", "native-collect", "native-cleanup", "lab-stop", "rocky-restore"}},
		{"test failure", "native-test", false, []string{"preflight", "rocky-stop", "lab-start", "native-setup", "native-test", "native-collect", "native-cleanup", "lab-stop", "rocky-restore"}},
		{"uncertain setup", "native-setup", true, []string{"preflight", "rocky-stop", "lab-start", "native-setup"}},
		{"success", "", false, []string{"preflight", "rocky-stop", "lab-start", "native-setup", "native-test", "native-collect", "native-cleanup", "lab-stop", "rocky-restore"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, a, now := nativeCollectionFixture(t)
			f := &fakeExecutor{fail: map[string]error{tc.failed: &Error{Category: "infrastructure", Uncertain: tc.uncertain, Err: errors.New("injected terminal failure")}}}
			// The original regression made this unnecessary guest call uncertain.
			if !contains(tc.want, "native-collect") {
				f.fail["native-collect"] = &Error{Uncertain: true, Err: errors.New("guest is stopped")}
			}
			r := Runner{Executor: f, Now: func() time.Time { return now }}
			rep, err := r.Run(t.Context(), p, a, false, nil)
			if (err != nil) != (tc.failed != "") || rep.Success != (tc.failed == "") || rep.Complete == tc.uncertain {
				t.Fatalf("incorrect terminal report: %+v %v", rep, err)
			}
			if !reflect.DeepEqual(f.calls, tc.want) {
				t.Fatalf("unexpected driver/lifecycle calls: got %v want %v", f.calls, tc.want)
			}
			if tc.failed == "preflight" {
				for _, result := range rep.Results {
					if (result.Phase == "collect" || result.Phase == "cleanup" || result.Phase == "restore") && result.Status != "skipped" {
						t.Fatalf("inapplicable action not skipped: %+v", result)
					}
					if result.Phase == "test" && result.Status == "passed" {
						t.Fatal("admission failure fabricated a test pass")
					}
				}
				var stored Report
				if err := ReadJSON(filepath.Join(p.EvidenceDir, "report.json"), &stored); err != nil || !stored.Complete || stored.Success {
					t.Fatalf("incorrect durable failed report: %+v %v", stored, err)
				}
				data, err := os.ReadFile(filepath.Join(p.EvidenceDir, "report.xml"))
				if err != nil {
					t.Fatal(err)
				}
				var suite junitSuite
				if err := xml.Unmarshal(data, &suite); err != nil || suite.Failures == 0 {
					t.Fatalf("JUnit lost admission failure: %+v %v", suite, err)
				}
			}
			// Resume and cleanup preserve the result without replaying guest work.
			for _, cleanup := range []bool{false, true} {
				f.calls = nil
				again, err := r.Run(t.Context(), p, a, cleanup, nil)
				if len(f.calls) != 0 || again.Complete != rep.Complete || again.Success != rep.Success || (err != nil) != (tc.failed != "") {
					t.Fatalf("resume changed terminal result or replayed work: %+v %v %v", again, f.calls, err)
				}
			}
		})
	}
}

func TestNativeCollectionAfterPersistedAdmissionFailure(t *testing.T) {
	p, a, now := nativeCollectionFixture(t)
	f := &fakeExecutor{fail: map[string]error{"preflight": &Error{Category: "infrastructure", Err: errors.New("missing admission snapshot")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	j, err := openJournal(p, a, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.step(t.Context(), p, j, 0); err == nil {
		t.Fatal("expected admission failure")
	}
	// Recover an older checkpoint written before the abort latch was saved.
	j.state.Aborted = false
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	if err := j.close(); err != nil {
		t.Fatal(err)
	}
	f.calls = nil
	rep, err := r.Run(t.Context(), p, a, false, nil)
	if err == nil || !rep.Complete || rep.Success || len(f.calls) != 0 {
		t.Fatalf("admission recovery dispatched work or erased failure: %+v %v %v", rep, f.calls, err)
	}
}

func TestUnconditionalCollectionAfterAdmissionFailure(t *testing.T) {
	p, a, now := fixture(t)
	f := &fakeExecutor{fail: map[string]error{"preflight": &Error{Category: "infrastructure", Err: errors.New("admission failure")}}}
	r := Runner{Executor: f, Now: func() time.Time { return now }}
	rep, err := r.Run(t.Context(), p, a, false, nil)
	if err == nil || !rep.Complete || rep.Success || !reflect.DeepEqual(f.calls, []string{"preflight", "collect"}) {
		t.Fatalf("unconditional collection changed: %+v %v %v", rep, f.calls, err)
	}
}

// Exercise the shipped Python fragment producer and real Go Plan/Runner with a
// fake executor. No driver, SSH, lifecycle helper or workload is executed.
func nativeCollectionFixture(t *testing.T) (Plan, Approval, time.Time) {
	t.Helper()
	p, a, now := fixture(t)
	root := t.TempDir()
	for _, name := range []string{"homelabfs", "hfs-behavior"} {
		header := make([]byte, 64)
		copy(header, []byte{'\x7f', 'E', 'L', 'F', 2, 1, 1, 9, 0})
		header[16], header[18] = 2, 62
		if err := os.WriteFile(filepath.Join(root, name), header, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source, err := filepath.Abs("../../tools/lab/native-single-node.py")
	if err != nil {
		t.Fatal(err)
	}
	const program = `import importlib.util, json, pathlib, sys
source, root = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2])
module = importlib.util.spec_from_file_location("native", source)
native = importlib.util.module_from_spec(module)
module.loader.exec_module(native)
spec = native.specification("run1", root / "homelabfs", root / "hfs-behavior", inspect=False)
path = root / "native-spec.json"
path.write_text(json.dumps(spec))
print(json.dumps(native.outer_fragment(path)))
`
	command := exec.CommandContext(t.Context(), "python3", "-B", "-c", program, source, root)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("offline native fragment: %v: %s", err, raw)
	}
	var fragment struct {
		Artifacts []Artifact  `json:"artifacts"`
		Resources []Resource  `json:"resources"`
		Tests     []Selection `json:"tests"`
		Steps     []Step      `json:"steps"`
	}
	if err := json.Unmarshal(raw, &fragment); err != nil {
		t.Fatal(err)
	}
	p.Artifacts = append(p.Artifacts, fragment.Artifacts...)
	p.Resources, p.Tests = fragment.Resources, fragment.Tests
	steps := append([]Step{}, p.Steps[:3]...)
	steps = append(steps, fragment.Steps...)
	p.Steps = append(steps, p.Steps[7:]...)
	p.Budget.DiskBytes, p.Budget.IOBytes, p.Budget.ArtifactBytes = 4<<30, 48<<20, 32<<20
	p.Budget.RetentionHours, p.Budget.RuntimeSeconds, p.Budget.CleanupSeconds = 168, 3600, 240
	a.PlanHash, err = p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return p, a, now
}
