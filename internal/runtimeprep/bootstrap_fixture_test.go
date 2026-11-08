//go:build linux

package runtimeprep

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/retained9plab"
)

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
