//go:build linux

package runtimeprep

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/retained9plab"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// bootstrapFixtureDir is the committed export of TestExportBootstrapFixture,
// relative to this package directory (go test runs in it).
const bootstrapFixtureDir = "../../tools/lab/retained9p/testdata/bootstrap"

// TestExportBootstrapFixture writes the real signer-shaped files that the
// zq3 G1 bootstrap orchestrator verifies (tools/lab/retained9p/bootstrap.py,
// testdata/bootstrap/). It is skipped unless HFS_BOOTSTRAP_FIXTURE_DIR names a
// fresh directory, so ordinary test runs write nothing outside t.TempDir.
//
// The flow is TestRequestsDriveActualSigner's: enroll-request, the actual
// signer with a throwaway operator key, bind-request and bind. Only public
// outputs are exported (proposal, template, requests, enrollment, provision,
// awaiting record, bind); the throwaway key files stay in the temporary root
// and are deleted with it. Request files still name those temporary key
// paths, which the Python tests never open.
//
// The budget comes from exampleSpec (testdata/zq3-spec.json), which
// validateSpec holds above the guest floors (guestfloor.go), so a fresh export
// always carries a provisionable budget. Regenerate the committed fixture
// (x54 did, after w54 raised the floors past its old 32/128 MiB budget) with
//
//	HFS_BOOTSTRAP_FIXTURE_DIR=$(mktemp -u) go test ./internal/runtimeprep -run TestExportBootstrapFixture
//
// and copy the nine files over; TestBootstrapFixtureAboveGuestFloor and
// TestBootstrapFixtureAboveSourceFloor guard it. i64 regenerated it again when
// the example spec's Views.Sources rose from 2 to 12 (viewcontrol.MinSources).
func TestExportBootstrapFixture(t *testing.T) {
	target := os.Getenv("HFS_BOOTSTRAP_FIXTURE_DIR")
	if target == "" {
		t.Skip("set HFS_BOOTSTRAP_FIXTURE_DIR to a fresh directory to export the G1 fixture")
	}
	f := newRequestFixture(t)
	var stdout bytes.Buffer
	run(t, []string{"enroll-request", "--template", filepath.Join(f.output, TemplateFile), "--proposal", f.inputs.ProposalPath,
		"--boot-id", f.inputs.BootID, "--provider-nonce", f.inputs.ProviderNonce, "--device-names", "virtio2,virtio3",
		"--observer", f.path("observer.json"), "--output", f.path("enroll-request.json")}, &stdout)
	if err := retained9plab.Run([]string{"enroll", "--request", f.path("enroll-request.json"), "--output", f.path("enrolled")}); err != nil {
		t.Fatal(err)
	}
	provision, err := os.ReadFile(filepath.Join(f.path("enrolled"), "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash, _, err := inspectProvision(provision)
	if err != nil {
		t.Fatal(err)
	}
	writePrivateJSON(t, f.path("awaiting.json"), map[string]any{"stage": "AWAITING_GUEST_BIND",
		"controller": testUUID(t), "provision_hash": hash, "publication": nil})
	run(t, []string{"bind-request", "--template", filepath.Join(f.output, TemplateFile), "--provision",
		filepath.Join(f.path("enrolled"), "provision.json"), "--awaiting", f.path("awaiting.json"),
		"--output", f.path("bind-request.json")}, &stdout)
	if err := retained9plab.Run([]string{"bind", "--request", f.path("bind-request.json"), "--output", f.path("bound")}); err != nil {
		t.Fatal(err)
	}
	exportFixture(t, target, map[string]string{
		"proposal.json":         f.inputs.ProposalPath,
		"request-template.json": filepath.Join(f.output, TemplateFile),
		"observer.json":         f.path("observer.json"),
		"enroll-request.json":   f.path("enroll-request.json"),
		"enrollment.json":       filepath.Join(f.path("enrolled"), "enrollment.json"),
		"provision.json":        filepath.Join(f.path("enrolled"), "provision.json"),
		"awaiting.json":         f.path("awaiting.json"),
		"bind-request.json":     f.path("bind-request.json"),
		"bind.json":             filepath.Join(f.path("bound"), "bind.json"),
	})
}

// exportFixture copies each named public file into a fresh target directory.
func exportFixture(t *testing.T, target string, files map[string]string) {
	t.Helper()
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, source := range files {
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBootstrapFixtureAboveGuestFloor fails when the committed G1 bootstrap
// fixture carries a guest budget the generator would now refuse. The Python
// tests replay that fixture byte for byte, so a stale below-floor budget
// (x54: reservations 32, 128 MiB control/ordinary, refused since w54) would
// otherwise keep passing there while no real bundle could produce it. The
// check calls checkGuestFloor itself, so it follows any floor change.
func TestBootstrapFixtureAboveGuestFloor(t *testing.T) {
	var template, request struct {
		Mounts []controlledruntime.GuestArtifactMount
		Budget controlledruntime.GuestArtifactBudget
	}
	var provision struct {
		Payload struct {
			Budget controlledruntime.GuestArtifactBudget `json:"budget"`
		} `json:"payload"`
	}
	readFixtureJSON(t, "request-template.json", &template)
	readFixtureJSON(t, "enroll-request.json", &request)
	readFixtureJSON(t, "provision.json", &provision)
	if len(template.Mounts) != sessionCount || len(request.Mounts) != sessionCount {
		t.Fatalf("fixture mounts %d/%d, generator emits %d", len(template.Mounts), len(request.Mounts), sessionCount)
	}
	// The signer copies the template budget into the request and the signed
	// provision unchanged; a mismatch means the files came from different runs.
	if request.Budget != template.Budget || provision.Payload.Budget != template.Budget {
		t.Fatalf("fixture budgets differ: template %+v, request %+v, provision %+v",
			template.Budget, request.Budget, provision.Payload.Budget)
	}
	if err := checkGuestFloor(template.Budget, len(template.Mounts)); err != nil {
		t.Fatalf("committed bootstrap fixture is below the guest floor; regenerate it (see TestExportBootstrapFixture): %v", err)
	}
}

// TestBootstrapFixtureAboveSourceFloor is the i64 counterpart for the view
// bounds: the committed proposal and signed enrollment must carry one shared
// Views budget that clears viewcontrol.CheckSourceFloor, so the fixture can
// never again replay the zq3-r7 Sources 2 that refused every non-read op.
func TestBootstrapFixtureAboveSourceFloor(t *testing.T) {
	var proposal viewcontrol.Enrollment
	var enrollment struct {
		Authorization struct{ Enrollment viewcontrol.Enrollment }
	}
	readFixtureJSON(t, "proposal.json", &proposal)
	readFixtureJSON(t, "enrollment.json", &enrollment)
	if enrollment.Authorization.Enrollment.Bounds != proposal.Bounds {
		t.Fatalf("fixture bounds differ: proposal %+v, enrollment %+v", proposal.Bounds, enrollment.Authorization.Enrollment.Bounds)
	}
	if proposal.Bounds != exampleSpec(t).Views {
		t.Fatalf("fixture bounds %+v are not the example spec's %+v; regenerate it (see TestExportBootstrapFixture)", proposal.Bounds, exampleSpec(t).Views)
	}
	if err := viewcontrol.CheckSourceFloor(proposal.Bounds, len(proposal.Domains)); err != nil {
		t.Fatalf("committed bootstrap fixture is below the source floor; regenerate it (see TestExportBootstrapFixture): %v", err)
	}
}

// readFixtureJSON decodes one committed bootstrap fixture file.
func readFixtureJSON(t *testing.T, name string, value any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(bootstrapFixtureDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}
