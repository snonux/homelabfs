package labrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateEtcdPKIPrincipalBoundaries(t *testing.T) {
	for _, length := range []int{63, 64} {
		t.Run(string(rune('A'+length-63)), func(t *testing.T) {
			p, _, now := privateEtcdFixture(t)
			renameEtcdRun(&p, strings.Repeat("r", length))
			pki, err := GenerateEtcdPKI(p, now)
			if err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(t.TempDir(), "pki")
			if err := WriteEtcdPKI(directory, p, pki); err != nil {
				t.Fatal("valid boundary principal rejected", err)
			}
			roles, err := EtcdRoles(p)
			if err != nil {
				t.Fatal(err)
			}
			for _, role := range roles {
				if _, err := os.Stat(filepath.Join(directory, role.User+"-key.pem")); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	for _, kind := range []string{"missing-root", "missing-role", "foreign-extra", "foreign-replacement", "empty-key"} {
		t.Run(kind, func(t *testing.T) {
			p, _, now := privateEtcdFixture(t)
			pki, err := GenerateEtcdPKI(p, now)
			if err != nil {
				t.Fatal(err)
			}
			roles, err := EtcdRoles(p)
			if err != nil {
				t.Fatal(err)
			}
			user := roles[0].User
			switch kind {
			case "missing-root":
				delete(pki.Clients, "root")
			case "missing-role":
				delete(pki.Clients, user)
			case "foreign-extra":
				pki.Clients["foreign"] = pki.Clients[user]
			case "foreign-replacement":
				pki.Clients["../foreign"] = pki.Clients[user]
				delete(pki.Clients, user)
			case "empty-key":
				identity := pki.Clients[user]
				identity.Key = nil
				pki.Clients[user] = identity
			}
			directory := filepath.Join(t.TempDir(), "must-not-exist")
			if err := WriteEtcdPKI(directory, p, pki); err == nil {
				t.Fatal("invalid principal set accepted")
			}
			if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("principal validation had filesystem effects", err)
			}
		})
	}
}

func TestPrivateEtcdControllerRouteAndStagedAdmission(t *testing.T) {
	for _, kind := range []string{"valid", "workstation", "guest", "missing-interface", "down-interface", "wrong-mask", "wrong-source", "foreign-helper", "tampered-pki", "foreign-pki-file", "source-artifact-missing"} {
		t.Run(kind, func(t *testing.T) {
			req, observed, directory := etcdControllerFixture(t)
			switch kind {
			case "workstation":
				observed.OS = "linux"
				observed.Hostname = "workstation"
			case "guest":
				observed.Hostname = "hfs-test0.test"
			case "missing-interface":
				observed.Interfaces = nil
			case "down-interface":
				observed.Interfaces[0].Up = false
			case "wrong-mask":
				observed.Interfaces[0].Addresses = []string{"172.31.253.1/32"}
			case "wrong-source":
				observed.Interfaces[0].Addresses = []string{"192.0.2.1/24"}
			case "foreign-helper":
				if err := os.WriteFile(observed.Executable, []byte("foreign helper"), 0700); err != nil {
					t.Fatal(err)
				}
			case "tampered-pki":
				if err := os.WriteFile(filepath.Join(directory, "root-key.pem"), []byte("foreign key"), 0600); err != nil {
					t.Fatal(err)
				}
			case "foreign-pki-file":
				if err := os.WriteFile(filepath.Join(directory, "hfs-test0.json"), []byte("guest keys forbidden here"), 0600); err != nil {
					t.Fatal(err)
				}
			case "source-artifact-missing":
				if err := os.Remove(req.Plan.Artifacts[0].Path); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			enrollment, err := bootstrapEtcdOnController(t.Context(), req, directory, observed, func(_ context.Context, _ Plan, _ string) error { calls++; return nil })
			valid := kind == "valid" || kind == "source-artifact-missing"
			if (err == nil) != valid || calls != map[bool]int{true: 1, false: 0}[valid] {
				t.Fatal("placement/input guard reached bootstrap incorrectly", kind, calls, err)
			}
			if valid {
				if err := validateEtcdControllerReceipt(req, &enrollment); err != nil {
					t.Fatal(err)
				}
				_, err := bootstrapEtcdOnController(t.Context(), req, directory, observed, func(context.Context, Plan, string) error { t.Fatal("duplicate bootstrap reached network"); return nil })
				if err == nil {
					t.Fatal("duplicate controller enrollment adopted")
				}
			}
		})
	}
}

func TestPrivateEtcdControllerRequestAndReceiptBinding(t *testing.T) {
	req, observed, directory := etcdControllerFixture(t)
	if req.Target != "f3" || req.Operation != "rbac" || len(req.ControllerPKI) != 14 {
		t.Fatal("controller request not complete", req.Target, len(req.ControllerPKI))
	}
	if _, err := PrepareEtcdStep(req.Plan, req.Approval, "etcd-rbac", time.Now()); err == nil {
		t.Fatal("unbound generic bootstrap request accepted")
	}
	for _, kind := range []string{"target", "missing-pin", "foreign-pin", "pin-digest"} {
		t.Run(kind, func(t *testing.T) {
			var changed EtcdStepRequest
			raw, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "target":
				changed.Target = "hfs-test0"
			case "missing-pin":
				changed.ControllerPKI = changed.ControllerPKI[1:]
			case "foreign-pin":
				changed.ControllerPKI[0].Name = "hfs-test0.json"
			case "pin-digest":
				changed.ControllerPKI[0].SHA256 = "unbounded"
			}
			if err := validateEtcdStepRequest(changed, time.Now()); err == nil {
				t.Fatal("changed request binding accepted")
			}
		})
	}
	enrollment, err := bootstrapEtcdOnController(t.Context(), req, directory, observed, func(context.Context, Plan, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, foreign := range []bool{false, true} {
		f := &fakeEtcdStep{change: func(receipt *EtcdStepReceipt) {
			receipt.Controller = &enrollment
			receipt.Started = time.Now().UTC()
			receipt.Finished = receipt.Started
			if foreign {
				receipt.Target = "hfs-test0"
			}
		}}
		_, err := RunEtcdBootstrapStep(t.Context(), req.Plan, req.Approval, directory, f, time.Now())
		if (err == nil) == foreign || len(f.calls) != 1 || f.calls[0].Target != "f3" {
			t.Fatal("f3 bootstrap receipt dispatch/binding failed", foreign, err)
		}
	}
	for _, kind := range []string{"target", "resource", "pki", "helper", "not-configured"} {
		t.Run(kind, func(t *testing.T) {
			changed := enrollment
			switch kind {
			case "target":
				changed.Placement.Target = "hfs-test0"
			case "resource":
				changed.Resource.MaxBytes++
			case "pki":
				changed.PKISHA256 = strings.Repeat("0", 64)
			case "helper":
				changed.HelperSHA256 = strings.Repeat("0", 64)
			case "not-configured":
				changed.Configured = false
			}
			if err := validateEtcdControllerReceipt(req, &changed); err == nil {
				t.Fatal("foreign controller receipt accepted")
			}
		})
	}
	for _, address := range []string{"172.31.253.11:2379", "192.0.2.1:32379", "localhost:32379"} {
		if conn, err := privateEtcdControllerDialer(req.Plan)(t.Context(), address); err == nil || conn != nil {
			if conn != nil {
				_ = conn.Close()
			}
			t.Fatal("foreign route was dialed")
		}
	}
}

func TestPrivateEtcdControllerSourceArtifactsBeforeTransfer(t *testing.T) {
	req, _, directory := etcdControllerFixture(t)
	file := req.Plan.Artifacts[0].Path
	if err := os.WriteFile(file, []byte("changed workstation source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareEtcdBootstrapStep(req.Plan, req.Approval, directory, time.Now()); err == nil {
		t.Fatal("changed workstation artifacts reached transfer preparation")
	}
}

func TestPrivateEtcdControllerResourceScope(t *testing.T) {
	p, _, _ := privateEtcdFixture(t)
	for _, kind := range []string{"missing", "foreign-location", "foreign-name", "foreign-host", "global-dataset", "extra-host-resource"} {
		t.Run(kind, func(t *testing.T) {
			var changed Plan
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			i := len(changed.Resources) - 1
			switch kind {
			case "missing":
				changed.Resources = changed.Resources[:i]
			case "foreign-location":
				changed.Resources[i].Location = etcdRoot(p) + "/other"
			case "foreign-name":
				changed.Resources[i].Name = "other"
			case "foreign-host":
				changed.Resources[i].Target = "f2"
			case "global-dataset":
				changed.Resources[i].Kind = "dataset"
				changed.Resources[i].Location = "zroot/bhyve"
			case "extra-host-resource":
				r := changed.Resources[i]
				r.Name = "unrelated-host"
				r.Location = etcdRoot(p) + "/other"
				changed.Resources = append(changed.Resources, r)
			}
			if err := changed.Validate(); err == nil {
				t.Fatal("generic f3 resource authority admitted")
			}
		})
	}
}

func TestPrivateEtcdDocumentedGuestHostname(t *testing.T) {
	for _, guest := range []string{"hfs-test0", "hfs-test1", "hfs-test2"} {
		if etcdGuestHostname(guest) != guest+".test" {
			t.Fatal("documented native hostname not required")
		}
	}
	for _, foreign := range []string{"hfs-test0.test", "hfs-test0.other", "f3", "rocky"} {
		if etcdGuestHostname(foreign) != "" {
			t.Fatal("foreign/bare alias adopted as a native target")
		}
	}
}

func etcdControllerFixture(t *testing.T) (EtcdStepRequest, etcdControllerRuntime, string) {
	t.Helper()
	p, _, now := privateEtcdFixture(t)
	root, err := os.MkdirTemp("/var/tmp", "hfs-run-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	renameEtcdRun(&p, strings.TrimPrefix(filepath.Base(root), "hfs-run-"))
	p.PrivateEtcd.StepSeconds = 30
	for i := range p.Steps {
		if strings.HasPrefix(p.Steps[i].Contract, "private-etcd-v1:") {
			p.Steps[i].TimeoutSeconds = 30
		}
	}
	p.Budget.CleanupSeconds = 300
	hash, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	approval := Approval{PlanHash: hash, Operator: "Paul", ApprovedAt: now, Statement: "I approve this exact plan"}
	pki, err := GenerateEtcdPKI(p, now)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "pki")
	if err := WriteEtcdPKI(source, p, pki); err != nil {
		t.Fatal(err)
	}
	req, err := PrepareEtcdBootstrapStep(p, approval, source, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	controller := etcdControllerRoot(p)
	if err := os.Mkdir(controller, 0700); err != nil {
		t.Fatal(err)
	}
	directory := controller + "/pki"
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, pin := range req.ControllerPKI {
		data, err := os.ReadFile(filepath.Join(source, pin.Name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, pin.Name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	helper, err := os.ReadFile(etcdArtifact(p, p.PrivateEtcd.Helper).Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controller+"/hfs-lab", helper, 0700); err != nil {
		t.Fatal(err)
	}
	observed := etcdControllerRuntime{OS: "freebsd", Arch: "amd64", Hostname: p.PrivateEtcd.Controller.Hostname, Executable: controller + "/hfs-lab", Interfaces: []etcdControllerInterface{{Name: "vm-hfslab", Up: true, Addresses: []string{"172.31.253.1/24"}}}}
	return req, observed, directory
}

func renameEtcdRun(p *Plan, run string) {
	old := p.RunID
	p.RunID = run
	p.PrivateEtcd.Token = "hfs-private-" + run + "-" + p.PrivateEtcd.Cluster
	for i := range p.Resources {
		p.Resources[i].Location = strings.ReplaceAll(p.Resources[i].Location, "hfs-run-"+old, "hfs-run-"+run)
	}
}
