//go:build linux || freebsd

package controlledruntime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
)

// lifecycleRun is one actual shared driver run past READY. All semantic,
// protocol, file, process and C owners are real (runtimeDriverFixture); only
// the native mount/INIT/fd syscalls, the FUSE shell dispatch switch, kernel peer
// credentials (when a test says so) and observed guest physical state are
// explicitly substituted operating-system boundaries.
type lifecycleRun struct {
	t        *testing.T
	r        *sourceRig
	d        *restartChild
	c        RuntimeConfig
	scanner  *bufio.Scanner
	reader   *io.PipeReader
	done     chan error
	cancel   context.CancelFunc
	ready    lifecycleBinding
	signed   RuntimeEnrollment // the actual signed handoff (runtime ticket families)
	foreign  atomic.Bool       // simulate a different kernel peer UID
	credFail atomic.Bool       // simulate unavailable kernel peer credentials
	switches atomic.Int32
	switchFn func(*fuseadapter.Shell, *guestbridge.Transport, *guestbridge.Transport) error
}

type lifecycleReply struct {
	Status       string             `json:"status"`
	Reason       string             `json:"reason"`
	Sequence     uint32             `json:"sequence"`
	Outcome      string             `json:"outcome"`
	Step         string             `json:"step"`
	Error        string             `json:"error"`
	Epoch        uint64             `json:"epoch"`
	Cut          string             `json:"cut"`
	Barrier      string             `json:"barrier"`
	Physical     string             `json:"physical"`
	Partial      []string           `json:"partial_owners"`
	Evidence     *lifecycleEvidence `json:"evidence"`
	ProtectError string             `json:"protect_error"`
	LostReply    *lostReplyView     `json:"lost_reply"`
}

func startLifecycleRun(t *testing.T, binary string, mutate func(*lifecycleRun, *runtimeOS)) *lifecycleRun {
	t.Helper()
	r, d, c, platform := runtimeDriverFixture(t, binary, 2)
	run := &lifecycleRun{t: t, r: r, d: d, c: c, done: make(chan error, 1)}
	platform.peer = func(conn *net.UnixConn) (admin.Peer, error) {
		peer, err := admin.PeerCredentials(conn)
		if run.foreign.Load() {
			peer.UID++ // declared OS boundary: a different local account
		}
		if run.credFail.Load() {
			return admin.Peer{}, unix.EPERM
		}
		return peer, err
	}
	// Declared OS boundary: the go-fuse dispatch of the one native connection.
	// The real Activate checks/grants and actual C exporter resume still run.
	platform.replace = func(shell *fuseadapter.Shell, old, next *guestbridge.Transport) error {
		run.switches.Add(1)
		if run.switchFn != nil {
			return run.switchFn(shell, old, next)
		}
		if shell == nil || shell != r.supervisor.mount.shell || !next.Restores(old) {
			return fsapi.Permission
		}
		d.mu.Lock()
		d.transport = next
		d.mu.Unlock()
		return nil
	}
	if mutate != nil {
		mutate(run, &platform)
	}
	ctx, cancel := context.WithCancel(t.Context())
	run.cancel = cancel
	reader, writer := io.Pipe()
	run.reader = reader
	go func() { err := runRuntimeWithOS(ctx, c, writer, platform); _ = writer.Close(); run.done <- err }()
	run.scanner = bufio.NewScanner(reader)
	t.Cleanup(func() { cancel(); _ = reader.Close() })
	run.driveToReady()
	return run
}

// driveToReady performs the external operator/guest steps exactly as the
// shared-sequence driver test does, with no fault injected.
func (run *lifecycleRun) driveToReady() {
	t, r, d := run.t, run.r, run.d
	var submitted RuntimeEnrollment
	for run.scanner.Scan() {
		var event map[string]any
		require(t, json.Unmarshal(run.scanner.Bytes(), &event))
		switch event["stage"] {
		case "AWAITING_OPERATOR_ENROLLMENT":
			var proposal viewcontrol.Enrollment
			require(t, readRuntimeFile(event["proposal"].(string), &proposal))
			submitted = runtimeSignedHandoff(t, r, run.c, proposal)
			raw, err := json.Marshal(submitted)
			require(t, err)
			require(t, os.WriteFile(run.c.EnrollmentFile+".next", raw, 0600))
			require(t, os.Rename(run.c.EnrollmentFile+".next", run.c.EnrollmentFile))
		case "AWAITING_GUEST_BIND":
			signers := []GuestArtifactSigners{}
			for _, peer := range r.peers {
				signers = append(signers, GuestArtifactSigners{peer.key, r.artifactGuest})
			}
			_, err := SignGuestBind(submitted.Provision, submitted.Authorization.Enrollment.ID.String(), event["controller"].(string), r.artifactOperator, signers)
			require(t, err)
			for i := 0; i < 2; i++ {
				d.exchange(i, "HELLO", guestbridge.TerminalProtocol(), nil)
			}
		case "READY":
			var ready struct {
				Publication any              `json:"publication"`
				Control     lifecycleBinding `json:"control"`
			}
			require(t, json.Unmarshal(run.scanner.Bytes(), &ready))
			if ready.Publication != nil || ready.Control.Epoch != 1 || ready.Control.Socket != filepath.Join(run.c.OwnerDirectory, LifecycleSocketName) {
				t.Fatalf("READY lacks exact private lifecycle binding: %s", run.scanner.Bytes())
			}
			run.ready = ready.Control
			run.signed = submitted
			d.autoReclaim = true
			return
		}
	}
	t.Fatalf("runtime ended before READY: %v", <-run.done)
}

// finish drains the remaining output and returns the runtime's result.
func (run *lifecycleRun) finish() error {
	for run.scanner.Scan() {
		run.t.Logf("post-READY output: %s", run.scanner.Bytes())
	}
	select {
	case err := <-run.done:
		return err
	case <-time.After(30 * time.Second):
		run.t.Fatal("runtime did not return")
		return nil
	}
}

func (run *lifecycleRun) command(opcode uint8, sequence uint32, runtime, cut string, epoch uint64) (uint8, lifecycleReply) {
	run.t.Helper()
	rid, err := fsapi.ParseUUID(runtime)
	require(run.t, err)
	var cid fsapi.UUID
	if cut != "" {
		cid, err = fsapi.ParseUUID(cut)
		require(run.t, err)
	}
	op, raw, err := lifecycleCommand(run.t.Context(), run.ready.Socket, lifecycleRequest{opcode: opcode, sequence: sequence, runtime: rid, cut: cid, epoch: epoch})
	require(run.t, err)
	var reply lifecycleReply
	require(run.t, json.Unmarshal(raw, &reply))
	return op, reply
}

// rawExchange writes arbitrary bytes and returns the decoded reply opcode and
// status, or an error if the server closed without a reply.
func (run *lifecycleRun) rawExchange(frame []byte) (uint8, lifecycleReply, error) {
	conn, err := net.Dial("unix", run.ready.Socket)
	if err != nil {
		return 0, lifecycleReply{}, err
	}
	defer func() { _ = conn.Close() }()
	require(run.t, conn.SetDeadline(time.Now().Add(15*time.Second)))
	if _, err = conn.Write(frame); err != nil {
		return 0, lifecycleReply{}, err
	}
	op, raw, err := readLifecycleReply(conn)
	if err != nil {
		return 0, lifecycleReply{}, err
	}
	var reply lifecycleReply
	err = json.Unmarshal(raw, &reply)
	return op, reply, err
}

func lifecycleFrame(magic string, version, opcode uint8, reserved uint16, sequence, length uint32, payload []byte) []byte {
	b := make([]byte, 16, 16+len(payload))
	copy(b, magic)
	b[4], b[5] = version, opcode
	binary.BigEndian.PutUint16(b[6:], reserved)
	binary.BigEndian.PutUint32(b[8:], sequence)
	binary.BigEndian.PutUint32(b[12:], length)
	return append(b, payload...)
}

func TestLifecycleEndpointRealC9RestartAndEvidence(t *testing.T) {
	run := startLifecycleRun(t, buildRestartChild(t), nil)
	_, before := run.d.root(0)
	if !bytes.Contains(before, []byte(`"publication":null`)) {
		t.Fatal("actual C/service publication", string(before))
	}
	op, observed := run.command(LifecycleObserve, 1, run.ready.Runtime, "", 0)
	e := observed.Evidence
	if op != LifecycleObserve|0x80 || observed.Status != "OBSERVED" || e == nil || !e.Complete || e.Stage != lifecycleServing || e.Epoch != 1 || e.Cut != run.ready.Cut || e.Publication != nil {
		t.Fatalf("initial actual evidence: %+v", observed)
	}
	checkActualEvidence(t, run, e)
	// One OBSERVE through the actual CLI client path as an external harness would.
	var out, errout bytes.Buffer
	require(t, Run(t.Context(), []string{"--control", run.ready.Socket, "--operation", "observe", "--runtime", run.ready.Runtime, "--sequence", "2"}, &out, &errout))
	if !bytes.Contains(out.Bytes(), []byte(`"status":"OBSERVED"`)) {
		t.Fatalf("client observation: %s %s", out.Bytes(), errout.Bytes())
	}
	restarted := checkRealRestart(t, run, e)
	checkRestoredTraffic(t, run, e, restarted)
	run.cancel()
	require(t, run.finish())
	if err := run.r.supervisor.child.live(); err != nil {
		t.Fatalf("runtime exit killed the VM stand-in: %v", err)
	}
	if _, err := os.Lstat(run.ready.Socket); !os.IsNotExist(err) {
		t.Fatal("lifecycle socket outlived its joined endpoint")
	}
	if run.r.supervisor.runtimeFunding.control != nil {
		t.Fatal("lifecycle funding not returned after its worker joined")
	}
}

// checkRealRestart runs the one real C9 restart and checks its reply, its
// post-attempt evidence and the protected outcome file.
func checkRealRestart(t *testing.T, run *lifecycleRun, e *lifecycleEvidence) lifecycleReply {
	t.Helper()
	op, restarted := run.command(LifecycleRestart, 3, run.ready.Runtime, run.ready.Cut, 1)
	if op != LifecycleRestart|0x80 || restarted.Status != lifecycleRestarted || restarted.Epoch != 2 || restarted.Cut == run.ready.Cut || restarted.Barrier == "" || run.switches.Load() != 1 {
		t.Fatalf("actual C9 restart: %+v", restarted)
	}
	if ev := restarted.Evidence; ev == nil || !ev.Complete || ev.Stage != lifecycleRestarted || ev.Transport.Controller != e.Transport.Controller || ev.Service.StateHash != e.Service.StateHash {
		t.Fatalf("post-restart evidence changed controller/source or incomplete: %+v", restarted.Evidence)
	}
	protected, err := os.ReadFile(filepath.Join(run.c.OwnerDirectory, "control-restart.json"))
	require(t, err)
	if !bytes.Contains(protected, []byte(`"status":"RESTARTED"`)) {
		t.Fatalf("restart outcome not protected before reply: %s", protected)
	}
	return restarted
}

// checkRestoredTraffic sends actual guest traffic through the restored
// transport over the same C owner, then checks fresh evidence and single use.
func checkRestoredTraffic(t *testing.T, run *lifecycleRun, e *lifecycleEvidence, restarted lifecycleReply) {
	t.Helper()
	_, after := run.d.root(1)
	if len(after) == 0 || !bytes.Contains(after, []byte(`"publication":null`)) {
		t.Fatal("restored endpoint did not serve the second original scope", string(after))
	}
	_, final := run.command(LifecycleObserve, 4, run.ready.Runtime, "", 0)
	if f := final.Evidence; f == nil || !f.Complete || f.Epoch != 2 || f.Cut != restarted.Cut || f.Transport.High <= e.Transport.High || f.Transport.Controller != run.r.supervisor.record.Controller.String() || f.Restart == nil || f.Restart.Status != lifecycleRestarted {
		t.Fatalf("post-traffic evidence: %+v", final.Evidence)
	}
	// Single use: neither the consumed nor the new binding can restart again.
	for i, binding := range []struct {
		cut   string
		epoch uint64
	}{{run.ready.Cut, 1}, {restarted.Cut, 2}} {
		if _, again := run.command(LifecycleRestart, uint32(5+i), run.ready.Runtime, binding.cut, binding.epoch); again.Status != "REFUSED" {
			t.Fatalf("repeated restart accepted: %+v", again)
		}
	}
}

// checkActualEvidence compares observed records with the actual owners.
func checkActualEvidence(t *testing.T, run *lifecycleRun, e *lifecycleEvidence) {
	t.Helper()
	s := run.r.supervisor
	if e.Supervisor.Controller != s.record.Controller.String() || e.Supervisor.Stage != "claimed" || !e.Supervisor.Live || e.Supervisor.Diagnostics == nil {
		t.Fatalf("supervisor evidence: %+v", e.Supervisor)
	}
	if len(e.Admissions) != 1 || e.Admissions[0].Fenced || len(e.Admissions[0].Domains) != 2 || e.Admissions[0].Physical == "" {
		t.Fatalf("admission evidence: %+v", e.Admissions)
	}
	if e.Service.State != "SERVING" || e.Service.Through == nil || e.Service.StateHash == "" || e.Service.Journal == "" {
		t.Fatalf("service source evidence: %+v", e.Service)
	}
	if len(e.Providers) != 2 || e.Providers[0].Ledger == nil || e.Providers[1].Ledger == nil || e.Providers[0].Ledger.Digest == e.Providers[1].Ledger.Digest {
		t.Fatalf("provider ledger evidence: %+v", e.Providers)
	}
	if e.Controller.Ledger == nil || !e.Controller.Ledger.Matches || e.Controller.Ledger.Digest == "" {
		t.Fatalf("controller ledger evidence: %+v", e.Controller)
	}
	if e.Transport.Controller != s.record.Controller.String() || e.Transport.Digest == "" || e.Transport.Hellos != 2 || e.Transport.Session != s.record.Endpoint.FuseSession {
		t.Fatalf("C8 evidence: %+v", e.Transport)
	}
}

func TestLifecycleEndpointRefusesUnauthorizedAndMalformed(t *testing.T) {
	run := startLifecycleRun(t, buildRestartChild(t), nil)
	checkClosedOwnerEvidence(t, run)
	checkPeerRefusals(t, run)
	checkFrameRefusals(t, run)
	checkStalledFrame(t, run)
	checkBindingRefusals(t, run)
	run.cancel()
	require(t, run.finish())
}

// checkClosedOwnerEvidence uses the fixture's actual, already closed pre-runtime
// owners: they report Unavailable, never stale digests.
func checkClosedOwnerEvidence(t *testing.T, run *lifecycleRun) {
	t.Helper()
	if _, err := run.r.peers[0].provider.LedgerEvidence(t.Context()); err == nil {
		t.Fatal("closed provider produced ledger evidence")
	}
	if _, err := run.r.controller.LedgerEvidence(t.Context()); err == nil {
		t.Fatal("closed controller produced ledger evidence")
	}
	if _, err := run.r.mappingTransport.ProtectedEvidence(); err == nil {
		t.Fatal("closed transport produced C8 evidence")
	}
}

// checkPeerRefusals: kernel peer credentials are the declared OS boundary. A
// foreign UID and an unavailable credential refuse before any request byte.
func checkPeerRefusals(t *testing.T, run *lifecycleRun) {
	t.Helper()
	for _, flag := range []*atomic.Bool{&run.foreign, &run.credFail} {
		flag.Store(true)
		conn, err := net.Dial("unix", run.ready.Socket)
		require(t, err)
		require(t, conn.SetDeadline(time.Now().Add(10*time.Second)))
		op, raw, err := readLifecycleReply(conn)
		_ = conn.Close()
		flag.Store(false)
		if err != nil || op != LifecycleRefused || !bytes.Contains(raw, []byte("peer credentials unauthorized")) {
			t.Fatalf("unauthorized peer: op=%x %s %v", op, raw, err)
		}
	}
}

// checkFrameRefusals sends closed-schema violations, including an oversized
// declared length that must refuse before any allocation or payload read.
func checkFrameRefusals(t *testing.T, run *lifecycleRun) {
	t.Helper()
	id, err := fsapi.ParseUUID(run.ready.Runtime)
	require(t, err)
	for name, frame := range map[string][]byte{
		"magic":     lifecycleFrame("HSM1", 1, LifecycleObserve, 0, 1, 16, id[:]),
		"version":   lifecycleFrame("HSC1", 2, LifecycleObserve, 0, 1, 16, id[:]),
		"reserved":  lifecycleFrame("HSC1", 1, LifecycleObserve, 1, 1, 16, id[:]),
		"sequence0": lifecycleFrame("HSC1", 1, LifecycleObserve, 0, 0, 16, id[:]),
		"opcode":    lifecycleFrame("HSC1", 1, 9, 0, 1, 16, id[:]),
		"oversized": lifecycleFrame("HSC1", 1, LifecycleObserve, 0, 1, 1<<31, nil),
		"short":     lifecycleFrame("HSC1", 1, LifecycleRestart, 0, 1, 16, id[:]),
		"long":      lifecycleFrame("HSC1", 1, LifecycleObserve, 0, 1, 40, make([]byte, 40)),
	} {
		op, reply, err := run.rawExchange(frame)
		if err != nil || op != LifecycleRefused || reply.Status != "REFUSED" {
			t.Fatalf("%s frame accepted: op=%x %+v %v", name, op, reply, err)
		}
	}
}

// checkStalledFrame: a stalled partial frame is dropped at the bounded deadline
// and the single worker continues with later clients.
func checkStalledFrame(t *testing.T, run *lifecycleRun) {
	t.Helper()
	stalled, err := net.Dial("unix", run.ready.Socket)
	require(t, err)
	_, err = stalled.Write([]byte("HSC"))
	require(t, err)
	start := time.Now()
	require(t, stalled.SetDeadline(time.Now().Add(15*time.Second)))
	op, _, err := readLifecycleReply(stalled)
	_ = stalled.Close()
	if (err == nil && op != LifecycleRefused) || time.Since(start) > lifecycleIOTimeout+5*time.Second {
		t.Fatalf("stalled frame not bounded: op=%x %v after %v", op, err, time.Since(start))
	}
}

// checkBindingRefusals covers wrong runtime, repeated/lower sequences and
// stale cut/epoch, none of which may reach C9 or change the binding.
func checkBindingRefusals(t *testing.T, run *lifecycleRun) {
	t.Helper()
	runtime, other := run.ready.Runtime, fsapi.UUID{1}
	if _, wrong := run.command(LifecycleObserve, 7, other.String(), "", 0); wrong.Status != "REFUSED" {
		t.Fatalf("foreign runtime accepted: %+v", wrong)
	}
	if _, ok := run.command(LifecycleObserve, 7, runtime, "", 0); ok.Status != "OBSERVED" {
		t.Fatalf("refused binding consumed its sequence: %+v", ok)
	}
	for _, sequence := range []uint32{7, 6} {
		if _, again := run.command(LifecycleObserve, sequence, runtime, "", 0); again.Status != "REFUSED" {
			t.Fatalf("non-monotone sequence %d accepted: %+v", sequence, again)
		}
	}
	// Stale cut or epoch consumes the sequence but never starts a restart.
	for i, stale := range []struct {
		sequence uint32
		cut      string
		epoch    uint64
	}{{8, other.String(), 1}, {9, run.ready.Cut, 2}, {9, run.ready.Cut, 1}} {
		if _, reply := run.command(LifecycleRestart, stale.sequence, runtime, stale.cut, stale.epoch); reply.Status != "REFUSED" {
			t.Fatalf("stale/replayed restart %d accepted: %+v", i, reply)
		}
	}
	if run.switches.Load() != 0 || run.r.supervisor.restartAttempted {
		t.Fatal("refused command reached C9")
	}
	if _, still := run.command(LifecycleObserve, 10, runtime, "", 0); still.Evidence == nil || still.Evidence.Epoch != 1 || still.Evidence.Stage != lifecycleServing {
		t.Fatalf("refusals changed lifecycle binding: %+v", still)
	}
	var out, errout bytes.Buffer
	if Run(t.Context(), []string{"--control", run.ready.Socket, "--operation", "observe", "--runtime", runtime, "--sequence", "10"}, &out, &errout) == nil {
		t.Fatal("client hid a refused reply")
	}
	if Run(t.Context(), []string{"--control", run.ready.Socket, "--operation", "observe", "--runtime", runtime, "--sequence", strconv.Itoa(11), "--config", "x"}, &out, &errout) == nil {
		t.Fatal("client accepted mixed runtime flags")
	}
}

func TestLifecycleRestartFailuresStayFenced(t *testing.T) {
	binary := buildRestartChild(t)
	t.Run("inconsistent-physical-state", func(t *testing.T) {
		run := startLifecycleRun(t, binary, nil)
		// Declared physical OS boundary: the guest now reports a different boot.
		var observed installedObservation
		raw, err := os.ReadFile(run.r.observationFile)
		require(t, err)
		require(t, json.Unmarshal(raw, &observed))
		observed.BootID = fsapi.UUID{9}.String()
		raw, err = canonicalJSON(observed)
		require(t, err)
		require(t, os.WriteFile(run.r.observationFile, raw, 0600))
		_, partial := run.command(LifecycleObserve, 1, run.ready.Runtime, "", 0)
		if e := partial.Evidence; e == nil || e.Complete || !e.Admissions[0].Fenced || e.Admissions[0].RefreshError == "" {
			t.Fatalf("inconsistent physical state not reported truthfully: %+v", partial.Evidence)
		}
		_, failed := run.command(LifecycleRestart, 2, run.ready.Runtime, run.ready.Cut, 1)
		if failed.Status != lifecycleFenced || failed.Step != "suspend" || failed.Epoch != 2 || failed.Evidence == nil || failed.Evidence.Complete || run.switches.Load() != 0 {
			t.Fatalf("restart over fenced admission: %+v", failed)
		}
		// Truthful partial status: Suspend already relinquished the C8 writer
		// before its physical refresh failed; providers remain open and readable.
		if e := failed.Evidence; e.Transport.Error == "" || e.Providers[0].Ledger == nil || e.Supervisor.Live {
			t.Fatalf("partial suspension evidence not truthful: %+v", e)
		}
		checkFencedExit(t, run)
	})
	t.Run("native-switch-refused", func(t *testing.T) {
		run := startLifecycleRun(t, binary, func(run *lifecycleRun, _ *runtimeOS) {
			run.switchFn = func(*fuseadapter.Shell, *guestbridge.Transport, *guestbridge.Transport) error {
				return fsapi.Permission
			}
		})
		_, failed := run.command(LifecycleRestart, 1, run.ready.Runtime, run.ready.Cut, 1)
		if failed.Status != lifecycleFenced || failed.Step != "activate" || len(failed.Partial) == 0 || failed.Evidence == nil || failed.Evidence.Stage != lifecycleFenced {
			t.Fatalf("activation failure not fenced with retained owners: %+v", failed)
		}
		checkFencedExit(t, run)
	})
}

// checkFencedExit verifies the runtime ends on its fenced native owner while
// the VM stand-in, protected outcome and all ledgers are preserved.
func checkFencedExit(t *testing.T, run *lifecycleRun) {
	t.Helper()
	checkFencedRuntimeExit(t, run)
	protected, err := os.ReadFile(filepath.Join(run.c.OwnerDirectory, "control-restart.json"))
	require(t, err)
	if !bytes.Contains(protected, []byte(`"status":"FENCED"`)) {
		t.Fatalf("fenced outcome not protected: %s", protected)
	}
}

// checkFencedRuntimeExit verifies the fenced runtime exit, the surviving VM
// stand-in and the owned evidence files, without reading the outcome file.
func checkFencedRuntimeExit(t *testing.T, run *lifecycleRun) {
	t.Helper()
	err := run.finish()
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("C9 restart fenced")) {
		t.Fatalf("fenced restart did not end the runtime truthfully: %v", err)
	}
	if e := run.r.supervisor.child.live(); e != nil {
		t.Fatalf("fenced restart killed the VM stand-in: %v", e)
	}
	for _, name := range []string{"transport", "controller", "provider-0", "provider-1", "issuance.json"} {
		if _, err := os.Lstat(filepath.Join(run.c.OwnerDirectory, name)); err != nil {
			t.Fatalf("fenced restart lost owned evidence %s: %v", name, err)
		}
	}
}
