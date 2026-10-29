package labrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// PrivateEtcdClientPort and PrivateEtcdPeerPort are reserved exclusively inside
// the native disposable guests. Production and SSH ports are never selected.
const (
	PrivateEtcdClientPort  = 32379
	PrivateEtcdPeerPort    = 32380
	PrivateReplicationPort = 32443
)

// PrivateEtcd describes an offline, independently bootstrappable test cluster.
// Its guest-local helpers never provision VMs or fetch dependencies.
type PrivateEtcd struct {
	Controller     EtcdControllerPlacement `json:"controller"`
	Cluster        string                  `json:"cluster"`
	Token          string                  `json:"token"`
	FreeBSD        string                  `json:"freebsd"`
	ABI            string                  `json:"abi"`
	Driver         string                  `json:"driver"`
	Helper         string                  `json:"helper"`
	PkgStatic      string                  `json:"pkg_static"`
	PkgStaticBytes int64                   `json:"pkg_static_bytes"`
	HelperBytes    int64                   `json:"helper_bytes"`
	Packages       []EtcdPackage           `json:"packages"`
	EtcdSHA256     string                  `json:"etcd_sha256"`
	EtcdctlSHA256  string                  `json:"etcdctl_sha256"`
	DiskBytes      int64                   `json:"disk_bytes_per_guest"`
	MemoryBytes    int64                   `json:"memory_bytes_per_guest"`
	CPUSeconds     int                     `json:"cpu_soft_seconds_per_process"`
	CPUHardSeconds int                     `json:"cpu_hard_seconds_per_process"`
	MaxLogBytes    int64                   `json:"max_log_bytes_per_guest"`
	StepSeconds    int                     `json:"step_seconds"`
	StopPolicy     string                  `json:"stop_policy"`
}

// EtcdPackage pins every archive in a closed dependency graph. Artifact refers
// to Plan.Artifacts; no path, repository, version constraint or URL is resolved.
type EtcdPackage struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Artifact     string   `json:"artifact"`
	Bytes        int64    `json:"bytes"`
	Dependencies []string `json:"dependencies"`
}

// EtcdOperation is a reviewable recipe. Preconditions are mandatory native
// checks, not evidence established by constructing or printing this value.
type EtcdOperation struct {
	ID          string   `json:"id"`
	Target      string   `json:"target"`
	Kind        string   `json:"kind"`
	Path        string   `json:"path,omitempty"`
	Args        []string `json:"args,omitempty"`
	Environment []string `json:"environment,omitempty"`
	Checks      []string `json:"checks"`
}

// ValidatePrivateEtcd checks isolation, pins and budgets without lab contact.
func ValidatePrivateEtcd(p Plan) error {
	e := p.PrivateEtcd
	if e == nil {
		return nil
	}
	cluster, err := fsapi.ParseUUID(e.Cluster)
	if err != nil || !cluster.Valid() || !strings.HasPrefix(e.Cluster, "68667374-") || p.Stage != "stage1" || !p.PreserveFailures || e.FreeBSD != "15.1-RELEASE" || e.ABI != "FreeBSD:15:amd64" || e.Token != "hfs-private-"+p.RunID+"-"+e.Cluster || e.StopPolicy != "term-then-kill-owned-child-v1" {
		return errors.New("private etcd requires native FreeBSD15.1 amd64, fresh test identity and failure preservation")
	}
	if e.DiskBytes < 256<<20 || e.DiskBytes > 2<<30 || e.MemoryBytes < 128<<20 || e.MemoryBytes > 1536<<20 || e.CPUSeconds < 1 || e.CPUSeconds > 3600 || e.MaxLogBytes <= 0 || e.MaxLogBytes > p.Budget.ArtifactBytes/3 || e.StepSeconds < 1 || e.StepSeconds > 300 || !sha256Text(e.EtcdSHA256) || !sha256Text(e.EtcdctlSHA256) {
		return errors.New("private etcd requires explicit bounded disk, memory, CPU, evidence, deadlines and binary digests")
	}
	if e.MaxLogBytes > p.Budget.ArtifactBytes/int64(len(p.Steps)+2)/4 {
		return errors.New("private etcd log cap must leave room for each bounded step receipt")
	}
	if e.CPUHardSeconds < e.CPUSeconds+5 || e.CPUHardSeconds > e.CPUSeconds+10 {
		return errors.New("private helper needs explicit bounded CPU hard-limit headroom for owned-child cleanup")
	}
	artifacts := map[string]Artifact{}
	for _, a := range p.Artifacts {
		artifacts[a.ID] = a
	}
	if e.Driver == e.PkgStatic || e.Helper == e.Driver || e.Helper == e.PkgStatic || artifacts[e.Helper].ID == "" || artifacts[e.Driver].ID == "" || artifacts[e.PkgStatic].ID == "" || artifacts[e.PkgStatic].Version == "" {
		return errors.New("separate pinned private-etcd driver and offline pkg-static required")
	}
	if err := validateEtcdPackages(e.Packages, artifacts, e.DiskBytes/4); err != nil {
		return err
	}
	if e.PkgStaticBytes <= 0 || e.PkgStaticBytes > 64<<20 || e.HelperBytes <= 0 || e.HelperBytes > 64<<20 {
		return errors.New("offline pkg-static and native helper need bounded exact byte sizes")
	}
	if err := validateEtcdControllerPlacement(p); err != nil {
		return err
	}
	inputBytes := e.PkgStaticBytes + e.HelperBytes + (1 << 20)
	for _, pkg := range e.Packages {
		inputBytes += pkg.Bytes
	}
	for i := range 3 {
		guest := fmt.Sprintf("hfs-test%d", i)
		found := false
		for _, r := range p.Resources {
			found = found || r.Target == guest && r.Kind == "directory" && r.Location == etcdRoot(p)+"/etcd" && r.MaxBytes >= e.DiskBytes
		}
		if !found {
			return fmt.Errorf("private etcd needs its exact bounded OS-disk resource on %s", guest)
		}
		inputFound := false
		for _, r := range p.Resources {
			inputFound = inputFound || r.Target == guest && r.Kind == "directory" && r.Location == etcdRoot(p)+"/input" && r.MaxBytes >= inputBytes
		}
		if !inputFound {
			return fmt.Errorf("private etcd requires bounded staged-input resource on %s", guest)
		}
		for _, t := range p.Targets {
			if t.Name == guest && t.Address != guestAddress(guest) {
				return errors.New("private etcd guest address must match isolated native inventory")
			}
		}
	}
	for _, f := range p.Faults {
		if f.Network != nil && !slices.Contains([]uint16{PrivateEtcdClientPort, PrivateEtcdPeerPort, PrivateReplicationPort}, f.Network.Port) {
			return errors.New("private cluster faults may use only explicitly reserved test ports")
		}
	}
	return validateEtcdSteps(p)
}

// VerifyEtcdBundle checks local package sizes and checksums without extraction.
// It never fetches, upgrades pkg or contacts a guest. Archive payload/ABI checks
// and a complete extracted-byte bound remain mandatory native adapter checks.
func VerifyEtcdBundle(p Plan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.PrivateEtcd == nil {
		return errors.New("private etcd inputs missing")
	}
	if err := p.VerifyArtifacts(); err != nil {
		return err
	}
	for _, pin := range []struct {
		id    string
		bytes int64
	}{{p.PrivateEtcd.Helper, p.PrivateEtcd.HelperBytes}, {p.PrivateEtcd.PkgStatic, p.PrivateEtcd.PkgStaticBytes}} {
		a := etcdArtifact(p, pin.id)
		info, err := os.Lstat(a.Path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != pin.bytes {
			return errors.New("offline helper/pkg-static size or file type differs from pin")
		}
	}
	remaining := p.PrivateEtcd.DiskBytes / 4
	for _, pkg := range p.PrivateEtcd.Packages {
		for _, a := range p.Artifacts {
			if a.ID == pkg.Artifact {
				info, err := os.Lstat(a.Path)
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() || info.Size() != pkg.Bytes {
					return fmt.Errorf("package %s is not an exact-size regular archive", pkg.Name)
				}
				used, err := inspectEtcdArchive(a.Path, pkg, p.PrivateEtcd, remaining)
				if err != nil {
					return err
				}
				remaining -= used
			}
		}
	}
	return nil
}

// PrivateEtcdOperations builds setup, collection and teardown recipes. Launch
// uses an owned child lifetime, never daemon readiness or a reusable raw PID.
func PrivateEtcdOperations(p Plan) ([]EtcdOperation, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	e := p.PrivateEtcd
	if e == nil {
		return nil, errors.New("private etcd inputs missing")
	}
	var out []EtcdOperation
	for i := range 3 {
		guest := fmt.Sprintf("hfs-test%d", i)
		root := etcdRoot(p) + "/etcd"
		checks := []string{"exact-plan-approval-and-guest", "private-real-owned-paths-and-ancestors", "outside-hfs-datasets-and-mounts", "no-outstanding-operation", "disk-cpu-memory-log-and-deadline-enforced"}
		add := func(kind, path string, args []string, more ...string) {
			out = append(out, EtcdOperation{ID: kind + "-" + guest, Target: guest, Kind: kind, Path: path, Args: args, Checks: append(slices.Clone(checks), more...)})
		}
		add("stage", "offline-package-stage-v1", []string{root}, "closed-dependency-graph", "package-abi-and-version", "reject-archive-links-traversal-special-files-and-oversize", "stage-only-exact-verified-package-files", "fresh-empty-private-rootfs", "empty-pkg-config-and-repository-directory")
		args := []string{"-r", root + "/rootfs", "-C", root + "/pkg.conf", "-R", root + "/repos", "-o", "ABI=" + e.ABI, "-o", "ASSUME_ALWAYS_YES=true", "-o", "HANDLE_RC_SCRIPTS=false", "add", "--no-scripts"}
		for _, pkg := range e.Packages {
			args = append(args, root+"/packages/"+pkg.Name+"-"+pkg.Version+".pkg")
		}
		add("install", root+"/pkg-static", args, "pinned-pkg-static-sha256", "no-network-or-repository-access", "private-package-database-only", "verify-extracted-etcd-and-etcdctl-digests", "native-loader-feasibility-is-an-unexecuted-gate")
		out[len(out)-1].Environment = []string{"PATH=/usr/bin:/bin", "PKG_ENABLE_PLUGINS=no", "ASSUME_ALWAYS_YES=yes", "GOMAXPROCS=1"}
		add("tls", "install-test-pki-v1", []string{root + "/tls"}, "fresh-run-ca-only", "per-member-server-and-peer-keys", "role-client-keys-separated", "never-copy-ca-key-to-guests", "private-key-mode0600")
		add("serve", "owned-etcd-child-v1", etcdArgs(p, guest), "retain-direct-child-unreaped-until-signal-gate-closed", "no-hfs-workspace-readiness-binding", "record-launch-birth-and-executable-digest", "preserve-child-on-partial-failure")
		add("collect", "collect-private-etcd-v1", []string{root}, "bounded-enrollment-supervisor-log-and-directory-byte-evidence", "no-private-keys-in-evidence", "controller-mtls-health-auth-evidence-is-separate", "transport-persists-evidence-outside-guests")
		add("stop", "owned-etcd-stop-v1", []string{root}, "exact-enrolled-child-handle-birth-and-digest", "close-signal-gate-before-reap", "term-then-kill-owned-child-after-two-seconds", "no-pid-rediscovery", "preserve-on-unknown-or-timeout")
		add("teardown", "teardown-private-etcd-v1", []string{root}, "exact-durable-run-cluster-device-inode-enrollment", "all-owned-children-reaped", "purge-only-enrolled-private-member-leaf-keys", "preserve-package-data-and-evidence-roots-for-retention", "never-delete-parent-or-unenrolled-paths")
	}
	out = append(out, EtcdOperation{ID: "rbac", Target: "f3", Kind: "rbac", Path: "private-etcd-bootstrap-v1", Args: []string{e.Cluster, etcdControllerRoot(p) + "/pki"}, Checks: []string{"exact-plan-approval", "exact-f3-hostname-staged-helper-and-controller-enrollment", "vm-hfslab-source-172.31.253.1-route", "request-bound-controller-pki-manifest", "mtls-status-and-exact-three-member-topology", "bootstrap-admin-client-controller-local-only", "root-user-before-auth-enable", "exact-coord.Permissions-ranges", "verify-positive-and-negative-role-permissions", "bootstrap-credentials-retained-controller-only-pending-approved-retirement"}})
	return out, nil
}

func validateEtcdPackages(packages []EtcdPackage, artifacts map[string]Artifact, maxBytes int64) error {
	if len(packages) == 0 || len(packages) > 32 {
		return errors.New("bounded complete offline package bundle required")
	}
	names, ids := map[string]bool{}, map[string]bool{}
	var total int64
	for _, pkg := range packages {
		a := artifacts[pkg.Artifact]
		if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_+.-]{0,127}$`).MatchString(pkg.Name) || !regexp.MustCompile(`^[0-9][0-9a-zA-Z._,+-]{0,127}$`).MatchString(pkg.Version) || names[pkg.Name] || ids[pkg.Artifact] || a.ID == "" || !strings.HasSuffix(a.Path, ".pkg") || a.Version != pkg.Version || pkg.Version == "" || pkg.Bytes <= 0 || pkg.Bytes > maxBytes-total || len(pkg.Dependencies) > 32 {
			return errors.New("offline packages require unique exact names, pinned versions, bytes and artifact digests")
		}
		names[pkg.Name], ids[pkg.Artifact] = true, true
		total += pkg.Bytes
	}
	if !names["coreos-etcd36"] {
		return errors.New("offline bundle must contain coreos-etcd36")
	}
	done, active := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if !names[name] || active[name] {
			return errors.New("missing or cyclic offline dependency")
		}
		if done[name] {
			return nil
		}
		active[name] = true
		for _, pkg := range packages {
			if pkg.Name == name {
				for _, dep := range pkg.Dependencies {
					if err := visit(dep); err != nil {
						return err
					}
				}
			}
		}
		active[name], done[name] = false, true
		return nil
	}
	for name := range names {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

func etcdArgs(p Plan, guest string) []string {
	e, root := p.PrivateEtcd, etcdRoot(p)+"/etcd"
	url := func(g string, port int) string { return fmt.Sprintf("https://%s:%d", guestAddress(g), port) }
	var members []string
	for i := range 3 {
		g := fmt.Sprintf("hfs-test%d", i)
		members = append(members, "hfs-private-"+p.RunID+"-"+g+"="+url(g, PrivateEtcdPeerPort))
	}
	return []string{root + "/rootfs/usr/local/bin/etcd", "--name=hfs-private-" + p.RunID + "-" + guest, "--data-dir=" + root + "/data", "--initial-cluster=" + strings.Join(members, ","), "--initial-cluster-token=" + e.Token, "--initial-cluster-state=new", "--listen-client-urls=" + url(guest, PrivateEtcdClientPort), "--advertise-client-urls=" + url(guest, PrivateEtcdClientPort), "--listen-peer-urls=" + url(guest, PrivateEtcdPeerPort), "--initial-advertise-peer-urls=" + url(guest, PrivateEtcdPeerPort), "--client-cert-auth=true", "--trusted-ca-file=" + root + "/tls/client-ca.pem", "--cert-file=" + root + "/tls/server.pem", "--key-file=" + root + "/tls/server-key.pem", "--peer-client-cert-auth=true", "--peer-trusted-ca-file=" + root + "/tls/peer-ca.pem", "--peer-cert-file=" + root + "/tls/peer.pem", "--peer-key-file=" + root + "/tls/peer-key.pem", "--tls-min-version=TLS1.3", "--quota-backend-bytes=67108864", "--max-wals=2", "--max-snapshots=2", "--max-request-bytes=1048576", "--log-outputs=stderr"}
}
func etcdRoot(p Plan) string { return "/var/tmp/hfs-run-" + p.RunID }

// HashEtcdInput computes a local regular file pin for offline plan preparation.
func HashEtcdInput(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || !filepath.IsAbs(path) {
		return "", 0, errors.New("pin requires an absolute regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
