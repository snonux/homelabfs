//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// oversizedEvidence builds a real evidence record whose encoding exceeds the
// reply bound. Actual owners cannot reach that size (every section is
// bounded), so this is the only way to reach the unreportable path.
func oversizedEvidence(t *testing.T) *lifecycleEvidence {
	t.Helper()
	rows := make([]domainObservationProof, 2000)
	for i := range rows {
		rows[i] = domainObservationProof{Session: testUUID(byte(i)).String(), Phase: "ACTIVE", Revision: uint64(i), Fence: 1}
	}
	e := &lifecycleEvidence{Version: lifecycleEvidenceVersion, Stage: lifecycleRestarted, Admissions: []admissionEvidence{{Domains: rows}}}
	raw, err := json.Marshal(e)
	require(t, err)
	if len(raw) <= lifecycleReplyBytes {
		t.Fatalf("fixture evidence only %d bytes", len(raw))
	}
	return e
}

// worstEvidenceError is the longest error text evidence keeps, made of a byte
// JSON escapes sixfold, so it is the largest protect_error a reply can add.
func worstEvidenceError() string {
	return evidenceError(errors.New(strings.Repeat("<", 2*lifecycleErrorBytes)))
}

// TestLifecycleRestartOutcomeBound: a consumed restart whose post-attempt
// evidence does not fit is reported as EFFECT_UNREPORTABLE with its actual
// outcome and every bounded field, never as REFUSED; a fitting one is kept.
func TestLifecycleRestartOutcomeBound(t *testing.T) {
	huge := oversizedEvidence(t)
	partial := []string{"bridge", "controller", "facts", "producer", "service", "transport"}
	for i := 0; i < 16; i++ {
		partial = append(partial, "provider:"+testUUID(byte(100+i)).String())
	}
	cut := testUUID(3).String()
	for _, c := range []struct {
		name         string
		view         lifecycleRestartView
		unreportable bool
	}{
		{"reportable-restarted", lifecycleRestartView{Status: lifecycleRestarted, Epoch: 2, Cut: cut, Barrier: "b", Physical: "p", Evidence: &lifecycleEvidence{Version: lifecycleEvidenceVersion, Stage: lifecycleRestarted}}, false},
		{"reportable-fenced", lifecycleRestartView{Status: lifecycleFenced, Step: "suspend", Error: "e", Epoch: 2, Cut: cut, Evidence: &lifecycleEvidence{Stage: lifecycleFenced}}, false},
		{"oversized-restarted", lifecycleRestartView{Status: lifecycleRestarted, Epoch: 2, Cut: cut, Barrier: "b", Physical: "p", Evidence: huge}, true},
		{"oversized-fenced-reopen", lifecycleRestartView{Status: lifecycleFenced, Step: "reopen", Error: worstEvidenceError(), Epoch: 2, Cut: cut, Barrier: "b", Partial: partial, Evidence: huge}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, raw := boundRestartOutcome(c.view, lifecycleOutcomeBytes)
			if raw == nil || len(raw) > lifecycleOutcomeBytes {
				t.Fatalf("outcome encoding %d bytes exceeds %d", len(raw), lifecycleOutcomeBytes)
			}
			var decoded lifecycleRestartView
			require(t, json.Unmarshal(raw, &decoded))
			if decoded.Status != got.Status || decoded.Outcome != got.Outcome {
				t.Fatalf("encoding differs from the reported outcome: %+v vs %+v", decoded, got)
			}
			checkBoundedOutcome(t, c.view, got, c.unreportable)
			// The reply may add the worst protect_error and still fits the frame.
			got.ProtectError = worstEvidenceError()
			reply, err := json.Marshal(got)
			require(t, err)
			if len(reply) > lifecycleReplyBytes {
				t.Fatalf("reply with protect_error %d bytes exceeds the frame", len(reply))
			}
		})
	}
	// The frame backstop never turns a possibly effective command into REFUSED.
	for opcode, want := range map[uint8]string{LifecycleObserve: "REFUSED", LifecycleRestart: lifecycleUnreportable, LifecycleArmLostReply: lifecycleUnreportable} {
		if got := oversizedReply(opcode); got.Status != want || got.Publication != nil {
			t.Fatalf("oversized reply for opcode %d: %+v", opcode, got)
		}
	}
}

func checkBoundedOutcome(t *testing.T, want, got lifecycleRestartView, unreportable bool) {
	t.Helper()
	if !unreportable {
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("fitting outcome changed: %+v", got)
		}
		return
	}
	if got.Status != lifecycleUnreportable || got.Outcome != want.Status || got.Evidence != nil || !strings.Contains(got.EvidenceError, "outcome bound") {
		t.Fatalf("oversized outcome not reported truthfully: %+v", got)
	}
	if got.Step != want.Step || got.Error != want.Error || got.Epoch != want.Epoch || got.Cut != want.Cut || got.Barrier != want.Barrier || got.Physical != want.Physical || !slices.Equal(got.Partial, want.Partial) {
		t.Fatalf("oversized outcome lost a bounded field: %+v", got)
	}
}

// TestLifecycleRestartOutcomeProtection writes the bounded outcome into a real
// exclusive supervisor owner directory and covers every refusal.
func TestLifecycleRestartOutcomeProtection(t *testing.T) {
	s, err := Create(t.Context(), filepath.Join(t.TempDir(), "owner"))
	require(t, err)
	file := filepath.Join(s.root, "control-restart.json")
	for name, raw := range map[string][]byte{"unencodable": nil, "oversized": make([]byte, lifecycleOutcomeBytes+1)} {
		if protectRestartOutcome(s, raw) == nil {
			t.Fatalf("%s outcome protected", name)
		}
	}
	if _, err := os.Lstat(file); !os.IsNotExist(err) {
		t.Fatalf("refused outcome created its file: %v", err)
	}
	_, raw := boundRestartOutcome(lifecycleRestartView{Status: lifecycleRestarted, Epoch: 2, Cut: testUUID(3).String(), Evidence: oversizedEvidence(t)}, lifecycleOutcomeBytes)
	require(t, protectRestartOutcome(s, raw))
	checkProtectedOutcome(t, file, raw)
	// Exclusive: a second protection refuses and preserves the first record.
	if err := protectRestartOutcome(s, []byte(`{"status":"FENCED"}`)); !errors.Is(err, unix.EEXIST) {
		t.Fatalf("existing outcome overwritten or misreported: %v", err)
	}
	checkProtectedOutcome(t, file, raw)
	require(t, s.Close())
	if err := protectRestartOutcome(s, raw); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("closed owner directory protected an outcome: %v", err)
	}
}

func checkProtectedOutcome(t *testing.T, file string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(file)
	require(t, err)
	info, err := os.Lstat(file)
	require(t, err)
	if !bytes.Equal(got, want) || !bytes.Contains(got, []byte(`"status":"EFFECT_UNREPORTABLE"`)) || info.Mode() != 0o600 {
		t.Fatalf("protected outcome %v: %s", info.Mode(), got)
	}
}

// TestLifecycleSocketBinding covers the socket's exclusive creation, its exact
// 0600 mode set without following a symlink and identity-checked removal.
func TestLifecycleSocketBinding(t *testing.T) {
	t.Run("private-0600-and-own-removal", func(t *testing.T) {
		owner := privateOwner(t)
		listener, socket, err := bindLifecycleSocket(owner)
		require(t, err)
		info, err := os.Lstat(socket.path)
		require(t, err)
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != lifecycleSocketMode || socket.path != filepath.Join(owner, LifecycleSocketName) {
			t.Fatalf("lifecycle socket %s mode %v", socket.path, info.Mode())
		}
		conn, err := net.Dial("unix", socket.path)
		require(t, err)
		require(t, conn.Close())
		require(t, listener.Close())
		require(t, socket.close())
		if _, err := os.Lstat(socket.path); !os.IsNotExist(err) {
			t.Fatalf("own socket name not removed: %v", err)
		}
	})
	t.Run("existing-names-preserved", checkExistingSocketNames)
	t.Run("replaced-after-bind", checkReplacedSocket)
	t.Run("non-private-owner", func(t *testing.T) {
		shared := t.TempDir()
		require(t, os.Chmod(shared, 0o755))
		private := privateOwner(t)
		link := filepath.Join(t.TempDir(), "owner-link")
		require(t, os.Symlink(private, link))
		for _, owner := range []string{shared, link} {
			if _, _, err := bindLifecycleSocket(owner); err == nil {
				t.Fatalf("socket bound in non-private owner %s", owner)
			}
		}
		for _, dir := range []string{shared, private} {
			if _, err := os.Lstat(filepath.Join(dir, LifecycleSocketName)); !os.IsNotExist(err) {
				t.Fatalf("refused owner received a socket: %v", err)
			}
		}
	})
}

// privateOwner is a fresh 0700 owner root. os.MkdirTemp creates it with mode
// 0700 (t.TempDir honours the umask) and keeps the socket path short enough
// for sockaddr_un, which t.TempDir's long test-named paths are not.
func privateOwner(t *testing.T) string {
	t.Helper()
	owner, err := os.MkdirTemp("", "lc-")
	require(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(owner) })
	return owner
}

// checkExistingSocketNames: any existing name, including a symlink to a file
// outside the owner root, is refused without touching it or its target.
func checkExistingSocketNames(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "dangling-symlink"} {
		owner := privateOwner(t)
		path := filepath.Join(owner, LifecycleSocketName)
		target := filepath.Join(t.TempDir(), "outside")
		require(t, os.WriteFile(target, []byte("outside"), 0o644))
		require(t, os.Chmod(target, 0o644))
		switch kind {
		case "file":
			require(t, os.WriteFile(path, []byte("preexisting"), 0o600))
		case "symlink":
			require(t, os.Symlink(target, path))
		default:
			require(t, os.Symlink(filepath.Join(owner, "absent"), path))
		}
		before, err := os.Lstat(path)
		require(t, err)
		if _, _, err := bindLifecycleSocket(owner); !errors.Is(err, fsapi.Permission) || !strings.Contains(err.Error(), "existing lifecycle socket preserved") {
			t.Fatalf("%s: existing name not refused: %v", kind, err)
		}
		after, err := os.Lstat(path)
		require(t, err)
		if !os.SameFile(before, after) || after.Mode() != before.Mode() {
			t.Fatalf("%s: existing name changed: %v -> %v", kind, before.Mode(), after.Mode())
		}
		checkOutsideTarget(t, target)
	}
}

// checkReplacedSocket swaps the bound name for a symlink to an outside file:
// protection refuses without changing the target, and close preserves the name.
func checkReplacedSocket(t *testing.T) {
	owner := privateOwner(t)
	listener, socket, err := bindLifecycleSocket(owner)
	require(t, err)
	defer func() { _ = listener.Close() }()
	target := filepath.Join(t.TempDir(), "outside")
	require(t, os.WriteFile(target, []byte("outside"), 0o644))
	require(t, os.Chmod(target, 0o644))
	require(t, os.Remove(socket.path))
	require(t, os.Symlink(target, socket.path))
	if err := socket.protect(); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("replaced socket name protected: %v", err)
	}
	checkOutsideTarget(t, target)
	if err := socket.close(); err == nil || !strings.Contains(err.Error(), "preserved") {
		t.Fatalf("replaced socket name removed or unreported: %v", err)
	}
	if info, err := os.Lstat(socket.path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("replacement not preserved: %v", err)
	}
}

func checkOutsideTarget(t *testing.T, target string) {
	t.Helper()
	info, err := os.Stat(target)
	require(t, err)
	raw, err := os.ReadFile(target)
	require(t, err)
	if info.Mode().Perm() != 0o644 || string(raw) != "outside" {
		t.Fatalf("symlink target changed: %v %q", info.Mode(), raw)
	}
}

// acceptFailure is the error shape (*net.UnixListener).AcceptUnix returns for
// an accept syscall failure; tests use it at the declared accept OS boundary.
func acceptFailure(errno unix.Errno) error {
	return &net.OpError{Op: "accept", Net: "unix", Err: os.NewSyscallError("accept4", errno)}
}

func TestLifecycleAcceptErrorClassification(t *testing.T) {
	for _, c := range []struct {
		err       error
		transient bool
	}{
		{acceptFailure(unix.EMFILE), true},
		{acceptFailure(unix.ENFILE), true},
		{acceptFailure(unix.ENOBUFS), true},
		{acceptFailure(unix.ENOMEM), true},
		{acceptFailure(unix.ECONNABORTED), true},
		{acceptFailure(unix.EINVAL), false},
		{acceptFailure(unix.EBADF), false},
		{acceptFailure(unix.ENOTSOCK), false},
		{net.ErrClosed, false},
		{errors.New("opaque"), false},
	} {
		if got := transientAcceptError(c.err); got != c.transient {
			t.Fatalf("%v: transient=%v", c.err, got)
		}
	}
	var d time.Duration
	for i := 0; i < 12; i++ {
		d = nextAcceptBackoff(d)
		if (i == 0 && d != lifecycleAcceptBackoffMin) || d < lifecycleAcceptBackoffMin || d > lifecycleAcceptBackoffMax {
			t.Fatalf("backoff step %d: %v", i, d)
		}
	}
	if d != lifecycleAcceptBackoffMax {
		t.Fatalf("backoff did not settle at its bound: %v", d)
	}
}

// TestLifecycleEndpointAcceptFailuresAndUnsupportedRestart runs a real
// runtime without a native shell switch and with the declared accept OS
// boundary substituted: simulated descriptor exhaustion is retried and
// reported, RESTART refuses before any effect, and a simulated broken
// listener ends the runtime with that diagnostic while every owner remains.
func TestLifecycleEndpointAcceptFailuresAndUnsupportedRestart(t *testing.T) {
	var transient atomic.Int32
	var broken atomic.Bool
	transient.Store(3)
	run := startLifecycleRun(t, buildRestartChild(t), func(_ *lifecycleRun, platform *runtimeOS) {
		platform.replace = nil
		platform.accept = func(l *net.UnixListener) (*net.UnixConn, error) {
			if transient.Add(-1) >= 0 {
				return nil, acceptFailure(unix.EMFILE)
			}
			c, err := l.AcceptUnix()
			if err == nil && broken.Load() {
				_ = c.Close()
				return nil, acceptFailure(unix.EINVAL)
			}
			return c, err
		}
	})
	if e := run.observe(1); e.Endpoint == nil || e.Endpoint.AcceptErrors != 3 || !strings.Contains(e.Endpoint.LastAcceptError, "too many open files") || !e.Complete {
		t.Fatalf("transient accept failures not retried and reported: %+v", e.Endpoint)
	}
	checkUnsupportedRestart(t, run)
	// Transient failures after a served connection are retried as well.
	transient.Store(2)
	run.observe(5)
	if e := run.observe(6); e.Endpoint == nil || e.Endpoint.AcceptErrors != 5 {
		t.Fatalf("later transient accept failures: %+v", e.Endpoint)
	}
	checkBrokenListenerExit(t, run, &broken)
}

// checkUnsupportedRestart: without the native shell switch RESTART refuses
// before any effect. It consumes its sequence but no attempt or binding.
func checkUnsupportedRestart(t *testing.T, run *lifecycleRun) {
	t.Helper()
	for _, sequence := range []uint32{2, 4} {
		_, refused := run.command(LifecycleRestart, sequence, run.ready.Runtime, run.ready.Cut, 1)
		if refused.Status != "REFUSED" || !strings.Contains(refused.Reason, "native shell switch unsupported") {
			t.Fatalf("restart without native switch: %+v", refused)
		}
		if sequence == 4 {
			break
		}
		if _, again := run.command(LifecycleObserve, 2, run.ready.Runtime, "", 0); again.Status != "REFUSED" {
			t.Fatalf("refused restart did not consume its sequence: %+v", again)
		}
		if e := run.observe(3); e.Stage != lifecycleServing || e.Epoch != 1 || e.Cut != run.ready.Cut || e.Restart != nil {
			t.Fatalf("refused restart changed the binding: %+v", e)
		}
	}
	if run.switches.Load() != 0 || run.r.supervisor.restartAttempted {
		t.Fatal("unsupported restart reached C9")
	}
	if _, err := os.Lstat(filepath.Join(run.c.OwnerDirectory, "control-restart.json")); !os.IsNotExist(err) {
		t.Fatalf("refused restart protected an outcome: %v", err)
	}
}

// checkBrokenListenerExit: a permanent accept failure ends the runtime with
// that diagnostic; the VM stand-in survives, the socket name and the control
// funding are returned, and no restart outcome appears.
func checkBrokenListenerExit(t *testing.T, run *lifecycleRun, broken *atomic.Bool) {
	t.Helper()
	broken.Store(true)
	id, err := fsapi.ParseUUID(run.ready.Runtime)
	require(t, err)
	if _, _, err := lifecycleCommand(t.Context(), run.ready.Socket, lifecycleRequest{opcode: LifecycleObserve, sequence: 7, runtime: id}); err == nil {
		t.Fatal("broken listener still replied")
	}
	err = run.finish()
	if err == nil || !strings.Contains(err.Error(), "accept failed permanently") || !strings.Contains(err.Error(), "invalid argument") {
		t.Fatalf("permanent accept failure not reported: %v", err)
	}
	if e := run.r.supervisor.child.live(); e != nil {
		t.Fatalf("endpoint loss killed the VM stand-in: %v", e)
	}
	if _, err := os.Lstat(run.ready.Socket); !os.IsNotExist(err) {
		t.Fatal("lifecycle socket outlived its joined endpoint")
	}
	if run.r.supervisor.runtimeFunding.control != nil {
		t.Fatal("lifecycle funding not returned after its worker ended")
	}
	if _, err := os.Lstat(filepath.Join(run.c.OwnerDirectory, "control-restart.json")); !os.IsNotExist(err) {
		t.Fatalf("endpoint loss produced a restart outcome: %v", err)
	}
}

// TestLifecycleRestartReopenFailure changes the declared guest physical OS
// boundary between the attempt's suspension and reopen refreshes, so the real
// Reopen fails after a successful suspend. A pre-existing control-restart.json
// makes the real outcome protection fail; the reply says so and the existing
// file is preserved.
func TestLifecycleRestartReopenFailure(t *testing.T) {
	run := startLifecycleRun(t, buildRestartChild(t), nil)
	file := filepath.Join(run.c.OwnerDirectory, "control-restart.json")
	require(t, os.WriteFile(file, []byte("preexisting\n"), 0o600))
	original, err := os.ReadFile(run.r.observationFile)
	require(t, err)
	var observed installedObservation
	require(t, json.Unmarshal(original, &observed))
	observed.BootID = fsapi.UUID{9}.String()
	changed, err := canonicalJSON(observed)
	require(t, err)
	served := observationSequence(t, run.r.observationFile, original, changed)
	_, failed := run.command(LifecycleRestart, 1, run.ready.Runtime, run.ready.Cut, 1)
	if failed.Status != lifecycleFenced || failed.Step != "reopen" || failed.Epoch != 2 || failed.Barrier == "" || failed.Physical != "" || !slices.Contains(failed.Partial, "service") || run.switches.Load() != 0 {
		t.Fatalf("reopen failure not fenced with retained partial owners: %+v", failed)
	}
	if e := failed.Evidence; e == nil || e.Stage != lifecycleFenced || e.Complete || e.Restart == nil || e.Restart.Step != "reopen" {
		t.Fatalf("reopen failure evidence: %+v", failed.Evidence)
	}
	if !strings.Contains(failed.ProtectError, "file exists") {
		t.Fatalf("unprotected outcome not reported: %+v", failed)
	}
	checkFencedRuntimeExit(t, run)
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "preexisting\n" {
		t.Fatalf("existing outcome file replaced: %q %v", raw, err)
	}
	if n := served(); n < 2 {
		t.Fatalf("physical boundary read %d times", n)
	}
}

// observationSequence replaces the declared guest observation OS boundary
// (the file the pinned observer prints) with a FIFO: the first read returns
// first and every later read returns later. It returns the reads served.
// Each served reader gets its own FIFO inode: once a reader is attached, the
// name is replaced by a fresh FIFO before the body is written, so a reader
// still draining cannot receive the next body as well.
func observationSequence(t *testing.T, file string, first, later []byte) func() int {
	t.Helper()
	require(t, os.Remove(file))
	require(t, unix.Mkfifo(file, 0o600))
	var served atomic.Int32
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			f, err := os.OpenFile(file, os.O_WRONLY, 0) // blocks until a reader opens
			if err != nil {
				return
			}
			select {
			case <-stop:
				_ = f.Close()
				return
			default:
			}
			if err = errors.Join(os.Remove(file), unix.Mkfifo(file, 0o600)); err != nil {
				_ = f.Close()
				return
			}
			body := later
			if served.Add(1) == 1 {
				body = first
			}
			_, _ = f.Write(body)
			_ = f.Close()
		}
	}()
	t.Cleanup(func() {
		close(stop)
		// A nonblocking reader releases the writer's pending open.
		if r, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NONBLOCK, 0); err == nil {
			<-done
			_ = r.Close()
		}
	})
	return func() int { return int(served.Load()) }
}

// closedPeerPair returns the client end of a connected unix stream pair whose
// endpoint end already queued reply (when non-nil) and closed, so the
// client's request write deterministically fails with EPIPE.
func closedPeerPair(t *testing.T, reply func(net.Conn) error) net.Conn {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	require(t, err)
	conns := make([]net.Conn, 2)
	for i, fd := range fds {
		file := os.NewFile(uintptr(fd), "lifecycle-pair")
		conns[i], err = net.FileConn(file)
		require(t, errors.Join(err, file.Close()))
	}
	t.Cleanup(func() { _ = conns[0].Close() })
	if reply != nil {
		require(t, reply(conns[1]))
	}
	require(t, conns[1].Close())
	require(t, conns[0].SetDeadline(time.Now().Add(5*time.Second)))
	return conns[0]
}

// TestLifecycleExchangeRefusalBeforeRequest pins task 564: the endpoint may
// refuse an unauthorized peer and close before the client's request write, so
// the write fails with EPIPE while the refusal is already queued. The client
// must return that refusal, and nothing but a refusal, after a failed write.
func TestLifecycleExchangeRefusalBeforeRequest(t *testing.T) {
	request := lifecycleRequest{opcode: LifecycleObserve, sequence: 1, runtime: testUUID(1)}
	refused := closedPeerPair(t, func(c net.Conn) error {
		return writeLifecycleReply(c, LifecycleRefused, 0, refusal("peer credentials unauthorized"))
	})
	op, raw, err := lifecycleExchange(refused, request)
	if err != nil || op != LifecycleRefused || !bytes.Contains(raw, []byte("peer credentials unauthorized")) {
		t.Fatalf("queued refusal lost after failed write: op=%x %s %v", op, raw, err)
	}
	// No reply at all: the write error stands.
	if _, _, err = lifecycleExchange(closedPeerPair(t, nil), request); !errors.Is(err, unix.EPIPE) {
		t.Fatalf("closed endpoint without reply: %v", err)
	}
	// A non-refusal reply cannot answer an unreceived request: still the write error.
	answered := closedPeerPair(t, func(c net.Conn) error {
		return writeLifecycleReply(c, LifecycleObserve|0x80, 1, map[string]string{"status": "OBSERVED"})
	})
	if _, _, err = lifecycleExchange(answered, request); !errors.Is(err, unix.EPIPE) {
		t.Fatalf("non-refusal accepted after failed write: %v", err)
	}
}
