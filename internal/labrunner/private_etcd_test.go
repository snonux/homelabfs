package labrunner

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

type fakeEtcdStep struct {
	change func(*EtcdStepReceipt)
	calls  []EtcdStepRequest
}
type fakeEtcdRBAC struct {
	calls  []EtcdRBACOperation
	failAt int
}
type fakeScenarioProbe struct {
	features []string
	foreign  bool
	calls    []string
}

type fakeEtcdPackages struct {
	calls   []EtcdOperation
	root    string
	corrupt bool
}

func (f *fakeEtcdPackages) RunEtcdCommand(_ context.Context, op EtcdOperation, _ int64) error {
	f.calls = append(f.calls, op)
	if err := os.MkdirAll(f.root+"/rootfs/usr/local/bin", 0700); err != nil {
		return err
	}
	for _, name := range []string{"etcd", "etcdctl"} {
		data := testEtcdELF(name)
		if f.corrupt {
			data = []byte("wrong")
		}
		if err := os.WriteFile(f.root+"/rootfs/usr/local/bin/"+name, data, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeEtcdStep) InvokeEtcdStep(_ context.Context, req EtcdStepRequest) (EtcdStepReceipt, error) {
	f.calls = append(f.calls, req)
	r := EtcdStepReceipt{PlanHash: req.PlanHash, Step: req.Step, Target: req.Target, Outcome: "applied", Terminal: true, Started: req.Deadline.Add(-time.Second), Finished: req.Deadline}
	if f.change != nil {
		f.change(&r)
	}
	return r, nil
}
func (f *fakeEtcdRBAC) ApplyEtcdRBAC(_ context.Context, op EtcdRBACOperation) error {
	f.calls = append(f.calls, op)
	if len(f.calls) == f.failAt {
		return errors.New("injected provisioning failure")
	}
	return nil
}
func (f *fakeScenarioProbe) ProbeScenario(_ context.Context, a Artifact, guest string) (ScenarioCapabilities, error) {
	f.calls = append(f.calls, guest)
	hash := a.SHA256
	if f.foreign {
		hash = strings.Repeat("0", 64)
	}
	return ScenarioCapabilities{ArtifactSHA256: hash, Guest: guest, Features: f.features}, nil
}

func TestPrivateEtcdPlanIsolationAndApproval(t *testing.T) {
	p, a, now := privateEtcdFixture(t)
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEtcdBundle(p); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Plan)
	}{
		{"production namespace", func(p *Plan) { p.PrivateEtcd.Cluster = "12345678-1234-1234-1234-123456789abc" }},
		{"wrong bootstrap token", func(p *Plan) { p.PrivateEtcd.Token = "production" }},
		{"unknown stop policy", func(p *Plan) { p.PrivateEtcd.StopPolicy = "kill-by-pid" }},
		{"wrong ABI", func(p *Plan) { p.PrivateEtcd.ABI = "FreeBSD:14:amd64" }},
		{"network fallback package", func(p *Plan) { p.PrivateEtcd.Packages[0].Artifact = "https://repo/pkg" }},
		{"missing dependency", func(p *Plan) { p.PrivateEtcd.Packages[0].Dependencies = []string{"absent"} }},
		{"cyclic dependency", func(p *Plan) { p.PrivateEtcd.Packages[0].Dependencies = []string{"coreos-etcd36"} }},
		{"wrong package version", func(p *Plan) { p.PrivateEtcd.Packages[0].Version = "latest" }},
		{"unbounded compressed bytes", func(p *Plan) { p.PrivateEtcd.Packages[0].Bytes = p.PrivateEtcd.DiskBytes }},
		{"shared helper", func(p *Plan) { p.PrivateEtcd.Helper = p.PrivateEtcd.Driver }},
		{"HFS bootstrap storage", func(p *Plan) {
			p.Resources[1].Location = "hfsdata0/data/hfs-run-run1/etcd"
			p.Resources[1].Kind = "dataset"
		}},
		{"foreign guest address", func(p *Plan) { p.Targets[0].Address = "192.0.2.1" }},
		{"unsafe argv", func(p *Plan) { p.Steps[4].Args = []string{"--force"} }},
		{"missing private cleanup", func(p *Plan) {
			for i, s := range p.Steps {
				if s.ID == "etcd-teardown-hfs-test0" {
					p.Steps = append(p.Steps[:i], p.Steps[i+1:]...)
					break
				}
			}
		}},
		{"stage2", func(p *Plan) { p.Stage = "stage2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			var changed Plan
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			tc.change(&changed)
			if err := changed.CheckApproval(a, now, false); err == nil {
				t.Fatal("unsafe/changed plan accepted")
			}
		})
	}
	ops, err := PrivateEtcdOperations(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 22 {
		t.Fatalf("want seven operations per guest and one controller bootstrap: %d", len(ops))
	}
	for _, op := range ops {
		if !nativeGuest(op.Target) && (op.Kind != "rbac" || op.Target != "f3") {
			t.Fatal(op)
		}
		if op.Kind == "install" && (!slices.Contains(op.Args, "--no-scripts") || !slices.Contains(op.Args, "-r") || slices.Contains(op.Args, "-M")) {
			t.Fatal(op)
		}
		if op.Kind == "serve" {
			args := strings.Join(op.Args, " ")
			for _, required := range []string{"32379", "32380", "--client-cert-auth=true", "--peer-client-cert-auth=true", "--data-dir=/var/tmp/hfs-run-run1/etcd/data"} {
				if !strings.Contains(args, required) {
					t.Fatal(args)
				}
			}
			if strings.Contains(args, "hfsdata") || strings.Contains(args, ":2379") {
				t.Fatal(args)
			}
		}
	}
}

func TestPrivateEtcdFakeExecutorAndReceipts(t *testing.T) {
	p, a, now := privateEtcdFixture(t)
	for _, tc := range []struct {
		name      string
		change    func(*EtcdStepReceipt)
		uncertain bool
	}{
		{"valid", nil, false},
		{"exit0-empty", func(r *EtcdStepReceipt) { *r = EtcdStepReceipt{} }, true},
		{"foreign", func(r *EtcdStepReceipt) { r.PlanHash = strings.Repeat("0", 64) }, true},
		{"outstanding", func(r *EtcdStepReceipt) { r.Terminal = false }, true},
		{"late", func(r *EtcdStepReceipt) { r.Finished = r.Finished.Add(time.Nanosecond) }, true},
		{"oversized", func(r *EtcdStepReceipt) { r.Message = strings.Repeat("x", 4097) }, true},
		{"unsupported", func(r *EtcdStepReceipt) { r.Outcome = "unsupported" }, false},
		{"unknown outcome", func(r *EtcdStepReceipt) { r.Outcome = "passed" }, true},
		{"uncertain cannot claim terminal", func(r *EtcdStepReceipt) { r.Outcome = "uncertain"; r.Terminal = true }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeEtcdStep{change: tc.change}
			_, err := RunEtcdStep(t.Context(), p, a, "etcd-stage-hfs-test0", f, now)
			if tc.name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("bad infrastructure receipt accepted")
				}
				var failure *Error
				if !errors.As(err, &failure) || failure.Uncertain != tc.uncertain {
					t.Fatalf("bad classification %v", err)
				}
			}
			if len(f.calls) != 1 || f.calls[0].Target != "hfs-test0" {
				t.Fatal(f.calls)
			}
		})
	}
	bad := a
	bad.PlanHash = strings.Repeat("0", 64)
	f := &fakeEtcdStep{}
	if _, err := RunEtcdStep(t.Context(), p, bad, "etcd-stage-hfs-test0", f, now); err == nil || len(f.calls) != 0 {
		t.Fatal("invalid approval reached executor")
	}
}

func TestPrivateEtcdReviewCLIAndIdentity(t *testing.T) {
	p, a, _ := privateEtcdFixture(t)
	source := filepath.Join(t.TempDir(), "source.json")
	output := filepath.Join(t.TempDir(), "review.json")
	if err := WriteJSON(source, p); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := PrivateEtcdCLI(t.Context(), []string{"private-etcd-plan", "--input", source, "--out", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var review struct {
		PlanHash   string           `json:"plan_hash"`
		Operations []EtcdOperation  `json:"operations"`
		Scenarios  []NativeScenario `json:"scenarios"`
	}
	if err := ReadJSON(output, &review); err != nil {
		t.Fatal(err)
	}
	if review.PlanHash != a.PlanHash || len(review.Operations) != 22 || len(review.Scenarios) != 15 {
		t.Fatal("incomplete canonical review")
	}
	first, token, err := NewPrivateEtcdIdentity("run1")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := NewPrivateEtcdIdentity("run1")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "68667374-") || token != "hfs-private-run1-"+first {
		t.Fatal("non-private or reused test identity")
	}
	if _, _, err := NewPrivateEtcdIdentity("../production"); err == nil {
		t.Fatal("unsafe identity input")
	}
}

func TestPrivateEtcdPKIAndRoleIsolation(t *testing.T) {
	p, _, now := privateEtcdFixture(t)
	pki, err := GenerateEtcdPKI(p, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateEtcdPKI(p, now)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(pki.ClientCA, pki.PeerCA) || bytes.Equal(pki.ClientCA, second.ClientCA) {
		t.Fatal("test trust roots reused")
	}
	for i := range 3 {
		guest := []string{"hfs-test0", "hfs-test1", "hfs-test2"}[i]
		selected := EtcdPKI{ClientCA: pki.ClientCA, PeerCA: pki.PeerCA, Members: map[string]map[string]EtcdIdentity{guest: pki.Members[guest]}}
		if err := validateEtcdMemberTLS(p, guest, selected); err != nil {
			t.Fatal(err)
		}
		selected.Members[guest] = pki.Members["hfs-test1"]
		if guest != "hfs-test1" && validateEtcdMemberTLS(p, guest, selected) == nil {
			t.Fatal("foreign member key accepted")
		}
	}
	roles, err := EtcdRoles(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 5 || len(pki.Clients) != 6 {
		t.Fatal("missing independent client principals")
	}
	for _, role := range roles {
		for _, permission := range role.Permissions {
			if !strings.HasPrefix(permission.Start, "/homelabfs/v1/clusters/"+p.PrivateEtcd.Cluster+"/") {
				t.Fatal(permission)
			}
		}
	}
	dir := filepath.Join(t.TempDir(), "private")
	if err := WriteEtcdPKI(dir, p, pki); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatal(info.Name(), info.Mode())
		}
	}
	var member EtcdPKI
	if err := ReadJSON(filepath.Join(dir, "hfs-test0.json"), &member); err != nil {
		t.Fatal(err)
	}
	if len(member.Clients) != 0 || len(member.Members) != 1 {
		t.Fatal("controller or foreign member credentials leaked")
	}
}

func TestPrivateEtcdRBACFake(t *testing.T) {
	p, _, _ := privateEtcdFixture(t)
	ops, err := EtcdRBACOperations(p)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeEtcdRBAC{}
	if err := ProvisionEtcdRBAC(t.Context(), p, f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ops, f.calls) || ops[0].User != "root" || ops[len(ops)-1].Kind != "enable" {
		t.Fatal(ops)
	}
	for _, op := range ops {
		if op.Kind == "grant" && op.User != "root" && op.Role == "root" {
			t.Fatal("HFS client escalated to root")
		}
		if op.Kind == "permission" && strings.HasSuffix(op.Start, "policy") && strings.HasSuffix(op.Role, "controller") && op.Write {
			t.Fatal("controller writes policy")
		}
	}
	f = &fakeEtcdRBAC{failAt: 4}
	if err := ProvisionEtcdRBAC(t.Context(), p, f); err == nil || len(f.calls) != 4 || slices.ContainsFunc(f.calls, func(op EtcdRBACOperation) bool { return op.Kind == "enable" }) {
		t.Fatal("partial bootstrap retried or enabled auth")
	}
}

func TestNativeRFScenariosStayGated(t *testing.T) {
	p, _, _ := privateEtcdFixture(t)
	if len(NativeScenarios()) != 15 {
		t.Fatal("R/F coverage changed")
	}
	for _, definition := range NativeScenarios() {
		prepared, err := PrepareNativeScenario(t.Context(), p, definition.ID, p.PrivateEtcd.Helper, nil)
		if err != nil || prepared.Outcome != "unsupported" {
			t.Fatal(prepared, err)
		}
		f := &fakeScenarioProbe{features: definition.Requires}
		prepared, err = PrepareNativeScenario(t.Context(), p, definition.ID, p.PrivateEtcd.Helper, f)
		if err != nil || prepared.Outcome != "blocked" && definition.ID != "F05" || len(prepared.Reasons) == 0 || len(f.calls) != 3 {
			t.Fatal(prepared, err, f.calls)
		}
		f.foreign = true
		prepared, err = PrepareNativeScenario(t.Context(), p, definition.ID, p.PrivateEtcd.Helper, f)
		if err != nil || prepared.Outcome != "unsupported" {
			t.Fatal("foreign binary capability accepted", prepared, err)
		}
	}
	for _, definition := range NativeScenarios() {
		if definition.ID == "F03" {
			if len(definition.Faults) != 2 || !slices.Equal(definition.Faults[0].Ports, []uint16{32379, 32380}) || !slices.Equal(definition.Faults[1].Ports, []uint16{32443}) {
				t.Fatal("coordination and replication partitions conflated")
			}
		}
	}
}

func TestPrivateEtcdArchiveRejectsUnsafePayload(t *testing.T) {
	p, _, _ := privateEtcdFixture(t)
	pkg := p.PrivateEtcd.Packages[0]
	for _, tc := range []struct {
		name     string
		header   tar.Header
		manifest string
	}{
		{"valid", tar.Header{Name: "usr/local/bin/etcd", Mode: 0755, Typeflag: tar.TypeReg}, ""},
		{"traversal", tar.Header{Name: "usr/local/../../etc/rc.conf", Mode: 0600, Typeflag: tar.TypeReg}, ""},
		{"symlink", tar.Header{Name: "usr/local/bin/etcd", Mode: 0755, Typeflag: tar.TypeSymlink, Linkname: "/etc/rc.conf"}, ""},
		{"setuid", tar.Header{Name: "usr/local/bin/etcd", Mode: 04755, Typeflag: tar.TypeReg}, ""},
		{"foreign payload", tar.Header{Name: "etc/rc.conf", Mode: 0600, Typeflag: tar.TypeReg}, ""},
		{"wrong ABI", tar.Header{Name: "usr/local/bin/etcd", Mode: 0755, Typeflag: tar.TypeReg}, `{"name":"coreos-etcd36","version":"3.6.16","abi":"FreeBSD:14:amd64"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "test.pkg")
			writeTestEtcdArchive(t, file, pkg, tc.header, tc.manifest)
			err := validateEtcdArchive(file, pkg, p.PrivateEtcd)
			if (err == nil) != (tc.name == "valid") {
				t.Fatal(err)
			}
		})
	}
}

func TestPrivateEtcdStagingFakeOnly(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid", true: "wrong-binary"}[corrupt], func(t *testing.T) {
			p, _, _ := privateEtcdFixture(t)
			root, err := os.MkdirTemp("/var/tmp", "hfs-run-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(root) }()
			old := p.RunID
			p.RunID = strings.TrimPrefix(filepath.Base(root), "hfs-run-")
			p.PrivateEtcd.Token = "hfs-private-" + p.RunID + "-" + p.PrivateEtcd.Cluster
			for i := range p.Resources {
				p.Resources[i].Location = strings.ReplaceAll(p.Resources[i].Location, "hfs-run-"+old, "hfs-run-"+p.RunID)
			}
			for _, name := range []string{"etcd", "etcdctl"} {
				sum := sha256.Sum256(testEtcdELF(name))
				if name == "etcd" {
					p.PrivateEtcd.EtcdSHA256 = hex.EncodeToString(sum[:])
				} else {
					p.PrivateEtcd.EtcdctlSHA256 = hex.EncodeToString(sum[:])
				}
			}
			if err := os.Mkdir(root+"/input", 0700); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{p.PrivateEtcd.PkgStatic, p.PrivateEtcd.Packages[0].Artifact} {
				a := etcdArtifact(p, id)
				b, err := os.ReadFile(a.Path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(root+"/input/"+id, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			f := &fakeEtcdPackages{root: root + "/etcd", corrupt: corrupt}
			err = stagePrivateEtcd(t.Context(), p, "hfs-test0", f)
			if (err != nil) != corrupt || len(f.calls) != 1 {
				t.Fatal("unexpected fake staging result", err, f.calls)
			}
			e, readErr := readEtcdEnrollment(p, "hfs-test0", root+"/etcd")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if corrupt && (e.Outstanding != "stage" || e.Staged) || !corrupt && (!e.Staged || e.Outstanding != "") {
				t.Fatal("stage outcome not preserved", e)
			}
			if f.calls[0].Path != root+"/etcd/pkg-static" || f.calls[0].Args[1] != root+"/etcd/rootfs" {
				t.Fatal("package command escaped private root")
			}
			if _, err := os.Stat(root + "/etcd/packages/coreos-etcd36-3.6.16.pkg"); err != nil {
				t.Fatal("failed resources lost", err)
			}
		})
	}
}

func testEtcdELF(name string) []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 9})
	binary.LittleEndian.PutUint16(b[16:], 2)
	binary.LittleEndian.PutUint16(b[18:], 62)
	binary.LittleEndian.PutUint32(b[20:], 1)
	binary.LittleEndian.PutUint16(b[52:], 64)
	return append(b, []byte(name)...)
}

func privateEtcdFixture(t *testing.T) (Plan, Approval, time.Time) {
	t.Helper()
	p, _, _ := fixture(t)
	now := time.Now().UTC()
	p.Created = now.Add(-time.Minute)
	p.Expires = now.Add(time.Hour)
	add := func(id, name, version string, data []byte) Artifact {
		file := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(file, data, 0600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		a := Artifact{ID: id, Path: file, Version: version, SHA256: hex.EncodeToString(sum[:])}
		p.Artifacts = append(p.Artifacts, a)
		return a
	}
	pkg := EtcdPackage{Name: "coreos-etcd36", Version: "3.6.16", Artifact: "etcd-package"}
	packagePath := filepath.Join(t.TempDir(), "etcd.pkg")
	writeTestEtcdArchive(t, packagePath, pkg, tar.Header{Name: "usr/local/bin/etcd", Mode: 0755, Typeflag: tar.TypeReg}, "")
	packageData, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	add(pkg.Artifact, "etcd.pkg", pkg.Version, packageData)
	pkg.Bytes = int64(len(packageData))
	helper := add("etcd-helper", "hfs-lab-freebsd", "fixture", []byte("native helper fixture"))
	static := add("pkg-static", "pkg-static", "fixture", []byte("offline pkg fixture"))
	_ = helper
	_ = static
	cluster := "68667374-0000-4000-8000-000000000001"
	p.PrivateEtcd = &PrivateEtcd{Cluster: cluster, Token: "hfs-private-" + p.RunID + "-" + cluster, FreeBSD: "15.1-RELEASE", ABI: "FreeBSD:15:amd64", Driver: "driver", Helper: "etcd-helper", PkgStatic: "pkg-static", HelperBytes: int64(len("native helper fixture")), PkgStaticBytes: int64(len("offline pkg fixture")), Packages: []EtcdPackage{pkg}, EtcdSHA256: strings.Repeat("a", 64), EtcdctlSHA256: strings.Repeat("b", 64), DiskBytes: 512 << 20, MemoryBytes: 1536 << 20, CPUSeconds: 60, CPUHardSeconds: 65, MaxLogBytes: 1 << 16, StepSeconds: 2, StopPolicy: "term-then-kill-owned-child-v1"}
	p.PrivateEtcd.Controller = EtcdControllerPlacement{Target: "f3", Hostname: "f3", Interface: "vm-hfslab", SourceAddress: "172.31.253.1", Resource: "etcd-controller"}
	p.Budget.DiskBytes = 3 << 30
	p.Budget.ArtifactBytes = 10 << 20
	p.Budget.RuntimeSeconds = 1200
	p.Budget.CleanupSeconds = 300
	for i := range 3 {
		guest := []string{"hfs-test0", "hfs-test1", "hfs-test2"}[i]
		p.Targets[i].Address = guestAddress(guest)
		p.Resources = append(p.Resources, Resource{Name: "etcd-" + guest, Kind: "directory", Target: guest, Location: etcdRoot(p) + "/etcd", MaxBytes: 512 << 20}, Resource{Name: "input-" + guest, Kind: "directory", Target: guest, Location: etcdRoot(p) + "/input", MaxBytes: 2 << 20})
	}
	p.Resources = append(p.Resources, Resource{Name: "etcd-controller", Target: "f3", Kind: "directory", Location: etcdControllerRoot(p), MaxBytes: 4 << 20})
	original := slices.Clone(p.Steps)
	p.Steps = slices.Clone(original[:4])
	etcd := PrivateEtcdSteps(p)
	for _, s := range etcd {
		if s.Phase == "setup" {
			p.Steps = append(p.Steps, s)
		}
	}
	p.Steps = append(p.Steps, original[4], original[5])
	for _, s := range etcd {
		if s.Phase == "collect" {
			p.Steps = append(p.Steps, s)
		}
	}
	for _, s := range etcd {
		if s.Phase == "cleanup" {
			p.Steps = append(p.Steps, s)
		}
	}
	p.Steps = append(p.Steps, original[6:]...)
	hash, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return p, Approval{PlanHash: hash, Operator: "Paul", ApprovedAt: now, Statement: "I approve this exact plan"}, now
}

func writeTestEtcdArchive(t *testing.T, file string, pkg EtcdPackage, header tar.Header, manifest string) {
	t.Helper()
	var b bytes.Buffer
	z, err := zstd.NewWriter(&b)
	if err != nil {
		t.Fatal(err)
	}
	w := tar.NewWriter(z)
	if manifest == "" {
		raw, err := json.Marshal(map[string]any{"name": pkg.Name, "version": pkg.Version, "abi": "FreeBSD:15:amd64"})
		if err != nil {
			t.Fatal(err)
		}
		manifest = string(raw)
	}
	if err := w.WriteHeader(&tar.Header{Name: "+COMPACT_MANIFEST", Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(manifest))}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteHeader(&header); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
