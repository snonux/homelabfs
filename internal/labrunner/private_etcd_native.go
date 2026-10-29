package labrunner

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/snonux/homelabfs/internal/admin"
)

// EtcdEnrollment records positive ownership of one private cluster directory.
// A failed or interrupted operation stays outstanding and is never auto-retried.
type EtcdEnrollment struct {
	RunID       string        `json:"run_id"`
	PlanHash    string        `json:"plan_hash"`
	Target      string        `json:"target"`
	Cluster     string        `json:"cluster"`
	Device      uint64        `json:"device"`
	Inode       uint64        `json:"inode"`
	Outstanding string        `json:"outstanding"`
	Staged      bool          `json:"staged"`
	Stopped     bool          `json:"stopped"`
	Failure     string        `json:"failure,omitempty"`
	Process     *FaultProcess `json:"process,omitempty"`
}

// EtcdCommandExecutor is the bounded guest-local subprocess seam. Implementations
// must never add a shell, repositories, ambient credentials or remote transport.
type EtcdCommandExecutor interface {
	RunEtcdCommand(context.Context, EtcdOperation, int64) error
}

// LocalEtcdCommands executes the exact offline pkg command after admission.
type LocalEtcdCommands struct{}

// RunEtcdCommand owns and waits for one bounded command. Installation is offline
// and script-free; execution failure leaves its enrollment outstanding.
func (LocalEtcdCommands) RunEtcdCommand(ctx context.Context, op EtcdOperation, maxLog int64) error {
	f, err := os.OpenFile(filepath.Join(filepath.Dir(op.Path), "install.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	cmd := exec.CommandContext(ctx, op.Path, op.Args...)
	cmd.Env = op.Environment
	w := &boundedLog{file: f, remaining: maxLog}
	cmd.Stdout, cmd.Stderr = w, w
	cmd.WaitDelay = time.Second
	return errors.Join(cmd.Run(), f.Sync())
}

// StagePrivateEtcd installs an exact offline bundle into its enrolled private
// rootfs. Inputs must already be transferred by the approved pinned transport to
// RUN/input/ARTIFACT_ID; this helper never fetches or copies controller paths.
// It is guest-local only and does not start a process or enable any service.
func StagePrivateEtcd(ctx context.Context, p Plan, a Approval, guest string, executor EtcdCommandExecutor) error {
	if err := admitEtcdNative(p, a, guest, false); err != nil {
		return err
	}
	return stagePrivateEtcd(ctx, p, guest, executor)
}
func stagePrivateEtcd(ctx context.Context, p Plan, guest string, executor EtcdCommandExecutor) error {
	if executor == nil {
		return errors.New("offline package executor required")
	}
	lock, err := lockEtcdRun(p)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	root := etcdRoot(p) + "/etcd"
	if err := privateDirectory(etcdRoot(p)); err != nil {
		return err
	}
	if err := os.Mkdir(root, 0700); err != nil {
		return err
	}
	if err := privateDirectory(root); err != nil {
		return err
	}
	enrollment, err := newEtcdEnrollment(p, guest, root)
	if err != nil {
		return err
	}
	enrollment.Outstanding = "stage"
	if err := WriteJSON(filepath.Join(root, "enrollment.json"), enrollment); err != nil {
		return err
	}
	for _, dir := range []string{"rootfs", "packages", "repos", "data", "tls", "control"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			return err
		}
	}
	if err := writeEtcdFile(filepath.Join(root, "pkg.conf"), []byte("REPOS_DIR: []\nPKG_ENABLE_PLUGINS: false\n"), 0600); err != nil {
		return err
	}
	if err := stageEtcdInputs(p, root); err != nil {
		return err
	}
	ops, err := PrivateEtcdOperations(p)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if op.Target == guest && op.Kind == "install" {
			ctx, cancel := context.WithTimeout(ctx, time.Duration(p.PrivateEtcd.StepSeconds)*time.Second)
			defer cancel()
			if err := executor.RunEtcdCommand(ctx, op, p.PrivateEtcd.MaxLogBytes); err != nil {
				return err
			}
		}
	}
	for name, digest := range map[string]string{"etcd": p.PrivateEtcd.EtcdSHA256, "etcdctl": p.PrivateEtcd.EtcdctlSHA256} {
		if err := verifyPrivateEtcdBinary(filepath.Join(root, "rootfs/usr/local/bin", name), digest); err != nil {
			return err
		}
	}
	if _, err := boundedEtcdTree(root, p.PrivateEtcd.DiskBytes); err != nil {
		return err
	}
	enrollment.Staged, enrollment.Outstanding = true, ""
	return replaceEtcdEnrollment(root, enrollment)
}

// InstallEtcdMemberTLS writes only the selected member's leaf keys and public
// CAs. The CA signing keys and other members'/role keys are never transferred.
func InstallEtcdMemberTLS(p Plan, a Approval, guest string, pki EtcdPKI) error {
	if err := admitEtcdNative(p, a, guest, false); err != nil {
		return err
	}
	lock, err := lockEtcdRun(p)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	root := etcdRoot(p) + "/etcd"
	enrollment, err := readEtcdEnrollment(p, guest, root)
	if err != nil {
		return err
	}
	if !enrollment.Staged || enrollment.Outstanding != "" || enrollment.Process != nil {
		return errors.New("TLS requires a clean staged unlaunched member")
	}
	member := pki.Members[guest]
	if len(pki.ClientCA) == 0 || len(pki.PeerCA) == 0 || len(member) != 2 || len(pki.Members) != 1 || len(pki.Clients) != 0 {
		return errors.New("exact member TLS material required")
	}
	if err := validateEtcdMemberTLS(p, guest, pki); err != nil {
		return err
	}
	for name, content := range map[string][]byte{"client-ca.pem": pki.ClientCA, "peer-ca.pem": pki.PeerCA, "server.pem": member["server"].Certificate, "server-key.pem": member["server"].Key, "peer.pem": member["peer"].Certificate, "peer-key.pem": member["peer"].Key} {
		if len(content) == 0 || len(content) > 16384 {
			return errors.New("missing or oversized TLS material")
		}
		if err := writeEtcdFile(filepath.Join(root, "tls", name), content, 0600); err != nil {
			return err
		}
	}
	return nil
}

// ServePrivateEtcd retains the actual direct child for its entire launch. It
// exposes only the process supervisor socket, never an HFS readiness socket.
// Resource/deadline breaches terminate this exact owned child and retain files.
// Run this foreground helper in the approved persistent driver, never nohup.
func ServePrivateEtcd(ctx context.Context, p Plan, a Approval, guest string) error {
	return servePrivateEtcd(ctx, p, a, guest, nil)
}
func servePrivateEtcd(ctx context.Context, p Plan, a Approval, guest string, ready func(FaultProcess) error) error {
	if err := admitEtcdNative(p, a, guest, false); err != nil {
		return err
	}
	lock, err := lockEtcdRun(p)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	root := etcdRoot(p) + "/etcd"
	enrollment, err := readEtcdEnrollment(p, guest, root)
	if err != nil {
		return err
	}
	if !enrollment.Staged || enrollment.Process != nil || enrollment.Outstanding != "" {
		return errors.New("member is not fresh, staged and idle")
	}
	for _, name := range []string{"client-ca.pem", "peer-ca.pem", "server.pem", "server-key.pem", "peer.pem", "peer-key.pem"} {
		if _, err := privateEtcdFile(filepath.Join(root, "tls", name)); err != nil {
			return err
		}
	}
	enrollment.Outstanding = "serve"
	if err := replaceEtcdEnrollment(root, enrollment); err != nil {
		return err
	}
	args := etcdArgs(p, guest)
	hash, err := p.Hash()
	if err != nil {
		return err
	}
	supervisor, err := StartFaultSupervisor(ctx, SupervisorConfig{Role: "private-etcd", RunID: p.RunID, Target: guest, Node: guest, Volume: "private-etcd", PlanHash: hash, Directory: root + "/control", Executable: args[0], SHA256: p.PrivateEtcd.EtcdSHA256, Handle: "etcd", Args: args[1:], MaxLogBytes: p.PrivateEtcd.MaxLogBytes, TerminateIntent: func(process FaultProcess) error { return recordEtcdStop(root, process) }})
	if supervisor == nil {
		return err
	}
	if err != nil {
		return stopEtcdPreserving(root, enrollment, supervisor, err)
	}
	process := supervisor.Process()
	enrollment.Process = &process
	enrollment.Outstanding = ""
	if err := replaceEtcdEnrollment(root, enrollment); err != nil {
		return stopEtcdPreserving(root, enrollment, supervisor, err)
	}
	if ready != nil {
		if err := ready(process); err != nil {
			return stopEtcdPreserving(root, enrollment, supervisor, err)
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Duration(p.Budget.RuntimeSeconds) * time.Second)
	defer deadline.Stop()
	for {
		supervisor.mu.Lock()
		exited, observeErr := supervisor.exited()
		supervisor.mu.Unlock()
		if observeErr != nil {
			return stopEtcdPreserving(root, enrollment, supervisor, observeErr)
		}
		if exited {
			break
		}
		// A normal stop request uses TERM. Escalation is an explicit part of
		// the approved StopPolicy, always through this same owned child.
		if _, statErr := os.Lstat(root + "/stop-request.json"); statErr == nil {
			var requested FaultProcess
			if readErr := readOwnedJSON(root+"/stop-request.json", &requested); readErr != nil || !reflect.DeepEqual(requested, process) {
				return stopEtcdPreserving(root, enrollment, supervisor, errors.Join(errors.New("foreign stop intent"), readErr))
			}
			err = finishEtcdChild(supervisor)
			enrollment.Stopped = supervisor.child.ProcessState != nil
			if expectedEtcdSignalExit(err) {
				err = nil
			}
			return errors.Join(err, replaceEtcdEnrollment(root, enrollment))
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return stopEtcdPreserving(root, enrollment, supervisor, statErr)
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-deadline.C:
			err = errors.New("private etcd approved runtime exhausted")
		case <-ticker.C:
			_, err = boundedEtcdTree(root, p.PrivateEtcd.DiskBytes)
		}
		if err != nil {
			return stopEtcdPreserving(root, enrollment, supervisor, err)
		}
	}
	closeErr := supervisor.Close()
	enrollment.Stopped = true
	var requested FaultProcess
	intentErr := readOwnedJSON(root+"/stop-request.json", &requested)
	if intentErr == nil && reflect.DeepEqual(requested, process) && expectedEtcdSignalExit(closeErr) {
		closeErr = nil
	}
	if intentErr != nil || !reflect.DeepEqual(requested, process) {
		closeErr = errors.Join(errors.New("private etcd exited without its approved stop intent"), closeErr)
	}
	return errors.Join(closeErr, replaceEtcdEnrollment(root, enrollment))
}

// StopPrivateEtcd requests TERM through the exact owning supervisor. A stopped
// enrollment is idempotent; a lost supervisor is uncertain and never adopted by
// PID. No host lifecycle, wildcard kill, global service or mount action occurs.
func StopPrivateEtcd(ctx context.Context, p Plan, a Approval, guest string) error {
	if err := admitEtcdNative(p, a, guest, true); err != nil {
		return err
	}
	root := etcdRoot(p) + "/etcd"
	e, err := readEtcdEnrollment(p, guest, root)
	if err != nil {
		return err
	}
	if e.Stopped || e.Process == nil {
		if e.Outstanding != "" && !e.Stopped {
			return errors.New("outstanding private etcd operation requires investigation")
		}
		return nil
	}
	if e.Outstanding != "" {
		return errors.New("outstanding private etcd operation requires investigation")
	}
	var observed FaultProcess
	if err := readOwnedJSON(root+"/control/supervisor-etcd.json", &observed); err != nil {
		return err
	}
	if !reflect.DeepEqual(*e.Process, observed) {
		return errors.New("private etcd launch identity mismatch")
	}
	_, err = admin.Call(ctx, root+"/control/supervisor-etcd.sock", admin.Request{Version: 1, Operation: "test-fault", Fault: signalToken("etcd-terminate", observed), Enabled: true, ExpectedNode: guest, ExpectedVolume: "private-etcd", ExpectedGeneration: observed.Birth})
	if err != nil {
		return err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			current, err := readEtcdEnrollment(p, guest, root)
			if err != nil {
				return err
			}
			if current.Stopped {
				return nil
			}
		}
	}
}

// TeardownPrivateEtcd stops only the positively enrolled process and removes
// only its private TLS leaves after stop confirmation. Package/data/evidence
// roots are deliberately retained for bounded inspection, including failures.
func TeardownPrivateEtcd(ctx context.Context, p Plan, a Approval, guest string) error {
	if err := StopPrivateEtcd(ctx, p, a, guest); err != nil {
		return err
	}
	lock, err := lockEtcdRun(p)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	root := etcdRoot(p) + "/etcd"
	e, err := readEtcdEnrollment(p, guest, root)
	if err != nil {
		return err
	}
	if e.Process != nil && !e.Stopped {
		return errors.New("private etcd child is not reaped")
	}
	for _, name := range []string{"server-key.pem", "peer-key.pem"} {
		file := filepath.Join(root, "tls", name)
		if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if _, err := privateEtcdFile(file); err != nil {
			return err
		}
		if err := os.Remove(file); err != nil {
			return err
		}
	}
	return nil
}

func admitEtcdNative(p Plan, a Approval, guest string, recovery bool) error {
	if err := p.CheckApproval(a, time.Now(), recovery); err != nil {
		return err
	}
	if p.PrivateEtcd == nil || !nativeGuest(guest) || runtime.GOOS != "freebsd" || runtime.GOARCH != "amd64" {
		return errors.New("private etcd needs native FreeBSD amd64 guest")
	}
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}
	if hostname != etcdGuestHostname(guest) {
		return errors.New("private etcd target hostname mismatch")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if !stringsWithin(executable, etcdRoot(p)) {
		return errors.New("native helper executable must be staged inside this run")
	}
	if err := verifyExecutable(executable, etcdArtifact(p, p.PrivateEtcd.Helper).SHA256); err != nil {
		return err
	}
	info, err := os.Lstat(executable)
	if err != nil {
		return err
	}
	if info.Size() != p.PrivateEtcd.HelperBytes {
		return errors.New("native helper byte size differs from plan")
	}
	if err := checkEtcdPlatform(etcdRoot(p)); err != nil {
		return err
	}
	return nil
}

func etcdGuestHostname(guest string) string {
	if !nativeGuest(guest) {
		return ""
	}
	return guest + ".test"
}

func stageEtcdInputs(p Plan, root string) error {
	remaining := p.PrivateEtcd.DiskBytes / 4
	for _, pkg := range p.PrivateEtcd.Packages {
		a := etcdArtifact(p, pkg.Artifact)
		src := etcdRoot(p) + "/input/" + pkg.Artifact
		if err := verifyEtcdInput(src, a.SHA256, pkg.Bytes); err != nil {
			return err
		}
		used, err := inspectEtcdArchive(src, pkg, p.PrivateEtcd, remaining)
		if err != nil {
			return err
		}
		remaining -= used
		if err := copyEtcdFile(src, root+"/packages/"+pkg.Name+"-"+pkg.Version+".pkg", 0600); err != nil {
			return err
		}
	}
	a := etcdArtifact(p, p.PrivateEtcd.PkgStatic)
	src := etcdRoot(p) + "/input/" + a.ID
	if err := verifyEtcdInput(src, a.SHA256, p.PrivateEtcd.PkgStaticBytes); err != nil {
		return err
	}
	return copyEtcdFile(src, root+"/pkg-static", 0700)
}

func validateEtcdArchive(file string, pkg EtcdPackage, e *PrivateEtcd) error {
	_, err := inspectEtcdArchive(file, pkg, e, e.DiskBytes/4)
	return err
}
func inspectEtcdArchive(file string, pkg EtcdPackage, e *PrivateEtcd, remaining int64) (used int64, retErr error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	z, err := zstd.NewReader(f, zstd.WithDecoderMaxMemory(uint64(e.MemoryBytes)))
	if err != nil {
		return 0, err
	}
	defer z.Close()
	r := tar.NewReader(z)
	var total int64
	manifest := false
	seen := map[string]bool{}
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		name := strings.TrimPrefix(strings.TrimPrefix(h.Name, "./"), "/")
		if h.Typeflag == tar.TypeDir {
			name = strings.TrimSuffix(name, "/")
		}
		if name == "" || path.Clean(name) != name || strings.HasPrefix(name, "../") || seen[name] || h.Size < 0 || h.Size > remaining-total || h.Mode&06000 != 0 || h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return 0, errors.New("unsafe or oversized offline package archive")
		}
		seen[name] = true
		total += h.Size
		parentDirectory := h.Typeflag == tar.TypeDir && (name == "usr" || name == "usr/local")
		if name == "+COMPACT_MANIFEST" || name == "+MANIFEST" {
			if h.Size > 1<<20 {
				return 0, errors.New("oversized package manifest")
			}
			var m struct {
				Name    string
				Version string
				ABI     string
				Deps    map[string]struct{ Version string }
			}
			if err := json.NewDecoder(io.LimitReader(r, h.Size)).Decode(&m); err != nil {
				return 0, err
			}
			if m.Name != pkg.Name || m.Version != pkg.Version || m.ABI != e.ABI || len(m.Deps) != len(pkg.Dependencies) {
				return 0, errors.New("offline package identity/dependency manifest mismatch")
			}
			for name, dep := range m.Deps {
				if !slicesContain(pkg.Dependencies, name) {
					return 0, errors.New("unlisted offline dependency")
				}
				matched := false
				for _, pin := range e.Packages {
					matched = matched || pin.Name == name && pin.Version == dep.Version
				}
				if !matched {
					return 0, errors.New("dependency version differs from offline pin")
				}
			}
			manifest = manifest || name == "+COMPACT_MANIFEST"
		} else if name != "+MTREE_DIRS" && !strings.HasPrefix(name, "usr/local/") && !parentDirectory {
			return 0, errors.New("package payload outside isolated usr/local prefix")
		}
	}
	if !manifest {
		return 0, errors.New("offline package missing compact manifest")
	}
	return total, nil
}

func newEtcdEnrollment(p Plan, guest, root string) (EtcdEnrollment, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return EtcdEnrollment{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return EtcdEnrollment{}, errors.New("native directory identity unavailable")
	}
	hash, err := p.Hash()
	if err != nil {
		return EtcdEnrollment{}, err
	}
	return EtcdEnrollment{RunID: p.RunID, PlanHash: hash, Target: guest, Cluster: p.PrivateEtcd.Cluster, Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}
func readEtcdEnrollment(p Plan, guest, root string) (EtcdEnrollment, error) {
	if err := privateDirectory(root); err != nil {
		return EtcdEnrollment{}, err
	}
	var e EtcdEnrollment
	if err := readOwnedJSON(root+"/enrollment.json", &e); err != nil {
		return e, err
	}
	want, err := newEtcdEnrollment(p, guest, root)
	if err != nil {
		return e, err
	}
	if e.RunID != want.RunID || e.PlanHash != want.PlanHash || e.Target != want.Target || e.Cluster != want.Cluster || e.Device != want.Device || e.Inode != want.Inode {
		return e, errors.New("foreign or replaced private etcd enrollment")
	}
	return e, nil
}
func replaceEtcdEnrollment(root string, e EtcdEnrollment) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	tmp := root + "/enrollment.next"
	if err := writeEtcdFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, root+"/enrollment.json"); err != nil {
		return err
	}
	f, err := os.Open(root)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}
func verifyEtcdInput(file, digest string, size int64) error {
	info, err := privateEtcdFile(file)
	if err != nil {
		return err
	}
	if size > 0 && info.Size() != size {
		return errors.New("offline input size mismatch")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return errors.New("offline input digest mismatch")
	}
	return nil
}
func privateEtcdFile(file string) (os.FileInfo, error) {
	if err := privateDirectory(filepath.Dir(file)); err != nil {
		return nil, err
	}
	info, err := os.Lstat(file)
	if err != nil {
		return nil, err
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || s.Uid != uint32(os.Geteuid()) || s.Nlink != 1 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private etcd file requires real owned single-link mode0600/0700")
	}
	return info, nil
}
func writeEtcdFile(file string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(b)
	return errors.Join(err, f.Sync())
}
func copyEtcdFile(src, dst string, mode os.FileMode) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	target, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	defer func() { _ = target.Close() }()
	_, err = io.Copy(target, f)
	return errors.Join(err, target.Sync())
}
func boundedEtcdTree(root string, max int64) (int64, error) {
	var total int64
	entries := 0
	deadline := time.Now().Add(time.Second)
	err := filepath.WalkDir(root, func(file string, d os.DirEntry, err error) error {
		entries++
		if entries > 65536 || time.Now().After(deadline) {
			return errors.New("private etcd directory inventory exceeded its bounded scan")
		}
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() && info.Mode()&os.ModeSocket == 0 {
			return errors.New("unexpected link or special file in private etcd root")
		}
		if info.Size() > max-total {
			return errors.New("private etcd disk budget exhausted")
		}
		total += info.Size()
		return nil
	})
	return total, err
}
func finishEtcdChild(s *FaultSupervisor) error {
	if s.child == nil {
		return s.Close()
	}
	// Both signals go through the same mutex/unreaped-child gate as public
	// controls; failed startup never requires opening public signal admission.
	var errs error
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		errs = errors.Join(errs, signalOwnedEtcd(s, signal))
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			s.mu.Lock()
			exited := false
			var err error
			if s.exited != nil {
				exited, err = s.exited()
			}
			s.mu.Unlock()
			if err != nil {
				errs = errors.Join(errs, err)
				break
			}
			if exited {
				return errors.Join(errs, s.Close())
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return errors.Join(errs, s.Close())
}
func signalOwnedEtcd(s *FaultSupervisor, signal syscall.Signal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Role != "private-etcd" || s.closed || s.child == nil || signal != syscall.SIGTERM && signal != syscall.SIGKILL {
		return errors.New("owned private etcd cleanup target closed or mismatched")
	}
	if s.exited != nil {
		exited, err := s.exited()
		if err == nil && exited {
			return nil
		}
	}
	if signal == syscall.SIGTERM && s.config.TerminateIntent != nil {
		if err := s.config.TerminateIntent(s.process); err != nil {
			return err
		}
	}
	// Wait cannot reap until Close permanently closes admission under this
	// mutex, so even a startup-failed/exited child still reserves this PID.
	return s.child.Process.Signal(signal)
}

func recordEtcdStop(root string, process FaultProcess) error {
	file := root + "/stop-request.json"
	if _, err := os.Lstat(file); errors.Is(err, os.ErrNotExist) {
		if err := WriteJSON(root+"/stop-request.next", process); err != nil {
			return err
		}
		if err := os.Rename(root+"/stop-request.next", file); err != nil {
			return err
		}
		f, err := os.Open(root)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if err := f.Sync(); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	var requested FaultProcess
	if err := readOwnedJSON(file, &requested); err != nil {
		return err
	}
	if !reflect.DeepEqual(process, requested) {
		return errors.New("stop intent targets another launch")
	}
	return nil
}
func expectedEtcdSignalExit(err error) bool {
	if err == nil {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, part := range joined.Unwrap() {
			if !expectedEtcdSignalExit(part) {
				return false
			}
		}
		return true
	}
	exit, ok := err.(*exec.ExitError)
	if !ok {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && (status.Signal() == syscall.SIGTERM || status.Signal() == syscall.SIGKILL)
}
func stopEtcdPreserving(root string, e EtcdEnrollment, s *FaultSupervisor, cause error) error {
	stopErr := finishEtcdChild(s)
	if expectedEtcdSignalExit(stopErr) {
		stopErr = nil
	}
	e.Failure = cause.Error()
	if len(e.Failure) > 4096 {
		e.Failure = e.Failure[:4096]
	}
	if s.child != nil {
		process := s.Process()
		e.Process = &process
		e.Stopped = s.child.ProcessState != nil
	}
	if e.Stopped {
		e.Outstanding = ""
	}
	return errors.Join(cause, stopErr, replaceEtcdEnrollment(root, e))
}
func lockEtcdRun(p Plan) (*os.File, error) {
	root := etcdRoot(p)
	if err := privateDirectory(root); err != nil {
		return nil, err
	}
	file := root + "/private-etcd.lock"
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if _, err := privateEtcdFile(file); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errors.New("another private etcd lifecycle owns this run")
	}
	return f, nil
}
func etcdArtifact(p Plan, id string) Artifact {
	for _, a := range p.Artifacts {
		if a.ID == id {
			return a
		}
	}
	return Artifact{}
}
func slicesContain(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func privateEtcdLimits(e *PrivateEtcd) error {
	if runtime.GOOS != "freebsd" {
		return errors.New("resource limits belong only in the dedicated native helper")
	}
	for _, limit := range []struct {
		resource int
		value    uint64
	}{{syscall.RLIMIT_AS, uint64(e.MemoryBytes)}, {syscall.RLIMIT_CPU, uint64(e.CPUSeconds)}, {syscall.RLIMIT_FSIZE, uint64(e.DiskBytes / 4)}, {syscall.RLIMIT_NOFILE, 256}} {
		if limit.value == 0 || limit.value > 1<<63-1 {
			return errors.New("private etcd rlimit exceeds positive signed native range")
		}
		hard := limit.value
		if limit.resource == syscall.RLIMIT_CPU {
			hard = uint64(e.CPUHardSeconds)
		}
		if err := setEtcdRlimit(limit.resource, limit.value, hard); err != nil {
			return err
		}
	}
	return nil
}
