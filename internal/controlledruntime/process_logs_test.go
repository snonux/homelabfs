//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const diagnosticHelperCoverageNotice = "warning: GOCOVERDIR not set, no coverage data emitted\n"

type partialDiagnosticSink struct {
	diagnosticSink
	result error
	calls  int
}

type failingDiagnosticStatus struct {
	diagnosticStatus
	mode                                       string
	pendingFinal, failed, closed, finalWritten bool
}

// This subprocess is a disposable test helper, never bhyve or a native guest.
func TestExporterDiagnosticsHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "hfs-diagnostics" {
		return
	}
	command := os.NewFile(3, "inherited-supervisor")
	if _, err := os.Stdout.Write(bytes.Repeat([]byte{'o'}, exporterLogBytes+65536)); err != nil {
		os.Exit(91)
	}
	if _, err := os.Stderr.Write([]byte("bootstrap-refusal-stderr")); err != nil {
		os.Exit(92)
	}
	if _, err := command.Write([]byte("R")); err != nil {
		os.Exit(93)
	}
	// Parent closes its ownership channels without killing or joining this
	// process. This continuation emits output after that Close.
	var b [1]byte
	_, _ = command.Read(b[:])
	if _, err := os.Stderr.Write([]byte("-after-management-EOF")); err != nil {
		os.Exit(94)
	}
	_ = command.Close()
	os.Exit(0)
}

func TestActualExporterDiagnosticsDrainOverflowAndClose(t *testing.T) {
	s := ownerFixture(t)
	spec := helperSpec(t)
	spec.Args = []string{"-test.run=^TestExporterDiagnosticsHelper$", "--", "hfs-diagnostics"}
	require(t, s.Start(t.Context(), spec))
	require(t, s.child.control.SetReadDeadline(time.Now().Add(5*time.Second)))
	var b [1]byte
	_, err := io.ReadFull(s.child.control, b[:])
	require(t, err)
	if b[0] != 'R' {
		t.Fatal("child did not drain its actual overflow")
	}
	deadline := time.Now().Add(5 * time.Second)
	state, ok := s.Diagnostics()
	for state.Stdout.Observed != exporterLogBytes+65536 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		state, ok = s.Diagnostics()
	}
	if !ok || state.Complete || !state.Stdout.Overflow || state.Stdout.Stored != exporterLogBytes || state.Stdout.Observed != exporterLogBytes+65536 {
		t.Fatalf("live evidence: %+v", state)
	}
	require(t, s.Close())
	select {
	case <-s.child.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not finish after ordinary management EOF")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		state, _ = s.Diagnostics()
		if state.ProcessExited && state.Stdout.EOF && state.Stderr.EOF {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("drain incomplete: %+v", state)
		}
		time.Sleep(time.Millisecond)
	}
	if state.Complete || state.WaitError || state.Stdout.ReadError || state.Stderr.WriteError || !state.ProcessExited {
		t.Fatalf("overflow incorrectly completed or child failed: %+v", state)
	}
	out, err := os.ReadFile(filepath.Join(s.root, "exporter.stdout"))
	require(t, err)
	stderr, err := os.ReadFile(filepath.Join(s.root, "exporter.stderr"))
	require(t, err)
	if len(out) != exporterLogBytes || !bytes.Equal(out, bytes.Repeat([]byte{'o'}, exporterLogBytes)) || string(stderr) != "bootstrap-refusal-stderr-after-management-EOF"+diagnosticHelperCoverageSuffix() {
		t.Fatalf("separate bounded child logs: stdout=%d stderr=%q", len(out), stderr)
	}
	var saved ExporterDiagnostics
	raw, err := os.ReadFile(filepath.Join(s.root, "exporter-diagnostics.json"))
	require(t, err)
	require(t, json.Unmarshal(raw, &saved))
	if len(raw) != exporterLogStatusBytes || !saved.Stdout.Overflow || saved.Complete || !saved.Stderr.EOF {
		t.Fatalf("saved evidence: %+v bytes=%d", saved, len(raw))
	}
}

func TestExporterDiagnosticsStorageFailureStillConsumes(t *testing.T) {
	s := ownerFixture(t)
	logs, err := newExporterLogs(s.directory)
	require(t, err)
	defer logs.abort()
	stream := logs.streams[0]
	require(t, stream.file.Close()) // Explicit filesystem I/O failure boundary.
	payload := bytes.Repeat([]byte{'x'}, exporterLogBytes+1)
	n, err := stream.Write(payload)
	if err != nil || n != len(payload) {
		t.Fatalf("failed capture broke pipe consumer: %d %v", n, err)
	}
	if !logs.state.Stdout.WriteError || !logs.state.Stdout.Overflow || logs.state.Stdout.Stored != 0 || logs.snapshot().Complete {
		t.Fatalf("hidden capture refusal: %+v", logs.snapshot())
	}
	if err := logs.checkpoint(); err != nil {
		t.Fatal(err)
	}
}

func TestExporterDiagnosticsPreservesExistingAndFailedStart(t *testing.T) {
	s := ownerFixture(t)
	path := filepath.Join(s.root, "exporter.stderr")
	require(t, os.WriteFile(path, []byte("preserve-original"), 0600))
	if _, err := newExporterLogs(s.directory); err == nil {
		t.Fatal("existing diagnostic overwritten")
	}
	raw, err := os.ReadFile(path)
	require(t, err)
	if string(raw) != "preserve-original" {
		t.Fatal("existing evidence changed")
	}
	var state ExporterDiagnostics
	status, err := os.ReadFile(filepath.Join(s.root, "exporter-diagnostics.json"))
	require(t, err)
	require(t, json.Unmarshal(status, &state))
	if state.Started || state.Complete {
		t.Fatalf("partial capture setup certified: %+v", state)
	}
	for _, name := range []string{"exporter.stdout", "exporter.stderr", "exporter-diagnostics.json"} {
		st, err := os.Stat(filepath.Join(s.root, name))
		require(t, err)
		if st.Mode().Perm() != 0600 {
			t.Fatalf("nonprivate %s", name)
		}
	}
}

func TestActualExporterDiagnosticsCompleteQuietExit(t *testing.T) {
	s := runningFixture(t)
	require(t, s.Close())
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, ok := s.Diagnostics()
		if ok && state.Complete {
			suffix := diagnosticHelperCoverageSuffix()
			if state.WaitError || state.Stdout.Observed != 0 || state.Stderr.Observed != uint64(len(suffix)) {
				t.Fatalf("quiet exit diagnostics: %+v", state)
			}
			raw, err := os.ReadFile(filepath.Join(s.root, "exporter.stderr"))
			require(t, err)
			if string(raw) != suffix {
				t.Fatalf("helper metadata capture differs: %q", raw)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("final diagnostics missing: %+v", state)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestFailedExecRetainsIncompleteDiagnostics(t *testing.T) {
	s := ownerFixture(t)
	spec := helperSpec(t)
	spec.Path = filepath.Join(t.TempDir(), "not-executable")
	payload := []byte("not executable machine code")
	require(t, os.WriteFile(spec.Path, payload, 0600))
	spec.SHA256 = sha256.Sum256(payload)
	if err := s.Start(t.Context(), spec); err == nil {
		t.Fatal("invalid exec succeeded")
	}
	var state ExporterDiagnostics
	raw, err := os.ReadFile(filepath.Join(s.root, "exporter-diagnostics.json"))
	require(t, err)
	require(t, json.Unmarshal(raw, &state))
	if state.Started || state.Complete || s.child != nil {
		t.Fatalf("failed exec diagnostics: %+v", state)
	}
}

func TestExporterDiagnosticsProcessExitDoesNotImplyPipeEOF(t *testing.T) {
	s := ownerFixture(t)
	logs, err := newExporterLogs(s.directory)
	require(t, err)
	// Explicit OS boundary fixture: duplicate the two inherited child ends,
	// then report exit while another descriptor owner still keeps them open.
	var inherited [2]*os.File
	for i, stream := range logs.streams {
		fd, err := unix.Dup(int(stream.writer.Fd()))
		require(t, err)
		inherited[i] = os.NewFile(uintptr(fd), "retained-child-pipe")
		t.Cleanup(func() { _ = inherited[i].Close() })
	}
	logs.start()
	logs.exited(nil)
	logs.mu.Lock()
	state := logs.snapshot()
	logs.mu.Unlock()
	if !state.ProcessExited || state.Stdout.EOF || state.Stderr.EOF || state.Complete {
		t.Fatalf("exit inferred pipe closure: %+v", state)
	}
	for _, f := range inherited {
		require(t, f.Close())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		logs.mu.Lock()
		state = logs.snapshot()
		logs.mu.Unlock()
		if state.Complete {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("real pipe EOF not recorded: %+v", state)
		}
		time.Sleep(time.Millisecond)
	}
}

// This disposable child waits until the parent installs an explicit file-write
// OS boundary, then makes real stdout writes through the production capture.
func TestExporterPartialDiagnosticsHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "hfs-partial-diagnostics" {
		return
	}
	control := os.NewFile(3, "inherited-supervisor")
	if _, err := control.Write([]byte{'R'}); err != nil {
		os.Exit(91)
	}
	var command [1]byte
	if _, err := io.ReadFull(control, command[:]); err != nil || command[0] != 'W' {
		os.Exit(92)
	}
	if _, err := os.Stdout.Write([]byte("first-write-prefix")); err != nil {
		os.Exit(93)
	}
	if _, err := os.Stdout.Write(bytes.Repeat([]byte{'x'}, exporterLogBytes+17)); err != nil {
		os.Exit(94)
	}
	if _, err := control.Write([]byte{'R'}); err != nil {
		os.Exit(95)
	}
	_, _ = control.Read(command[:])
	_ = control.Close()
	os.Exit(0)
}

func TestActualExporterDiagnosticsPartialWriteStillDrains(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{{"partial-eio", unix.EIO}, {"short-nil", nil}} {
		t.Run(test.name, func(t *testing.T) {
			s := diagnosticFaultOwner(t)
			spec := helperSpec(t)
			spec.Args = []string{"-test.run=^TestExporterPartialDiagnosticsHelper$", "--", "hfs-partial-diagnostics"}
			require(t, s.Start(t.Context(), spec))
			require(t, s.child.control.SetDeadline(time.Now().Add(5*time.Second)))
			var ready [1]byte
			_, err := io.ReadFull(s.child.control, ready[:])
			require(t, err)
			logs := s.child.logs
			logs.mu.Lock()
			sink := &partialDiagnosticSink{diagnosticSink: logs.streams[0].file, result: test.err}
			logs.streams[0].file = sink
			logs.mu.Unlock()
			_, err = s.child.control.Write([]byte{'W'})
			require(t, err)
			_, err = io.ReadFull(s.child.control, ready[:])
			require(t, err)
			if ready[0] != 'R' {
				t.Fatal("child did not finish real writes after capture refusal")
			}
			require(t, s.child.control.Close())
			state := waitDiagnosticFinal(t, s)
			observed := uint64(len("first-write-prefix") + exporterLogBytes + 17)
			if state.Stdout.Observed != observed || state.Stdout.Stored != 7 || !state.Stdout.WriteError || !state.Stdout.Overflow || state.Complete || state.WaitError {
				t.Fatalf("partial capture hidden or stopped consuming: %+v", state)
			}
			logs.mu.Lock()
			calls := sink.calls
			logs.mu.Unlock()
			if calls != 1 {
				t.Fatalf("sticky refusal retried sink: %d calls", calls)
			}
			raw, err := os.ReadFile(filepath.Join(s.root, "exporter.stdout"))
			require(t, err)
			if string(raw) != "first-w" || len(raw) > exporterLogBytes {
				t.Fatalf("actual partial bytes lost or cap exceeded: %q", raw)
			}
			var saved ExporterDiagnostics
			raw, err = os.ReadFile(filepath.Join(s.root, "exporter-diagnostics.json"))
			require(t, err)
			require(t, json.Unmarshal(raw, &saved))
			if len(raw) != exporterLogStatusBytes || saved.Complete || !saved.Stdout.WriteError || saved.Stdout.Stored != 7 || saved.Stdout.Observed != observed {
				t.Fatalf("saved partial failure incorrect: %+v", saved)
			}
			require(t, s.Close())
		})
	}
}

func TestActualExporterDiagnosticsFinalStatusFailureRetained(t *testing.T) {
	for _, mode := range []string{"sync-eio", "write-at-eio"} {
		t.Run(mode, func(t *testing.T) {
			s := diagnosticFaultOwner(t)
			require(t, s.Start(t.Context(), helperSpec(t)))
			require(t, s.child.control.SetReadDeadline(time.Now().Add(5*time.Second)))
			var ready [1]byte
			_, err := io.ReadFull(s.child.control, ready[:])
			require(t, err)
			logs := s.child.logs
			logs.mu.Lock()
			status := &failingDiagnosticStatus{diagnosticStatus: logs.status, mode: mode}
			logs.status = status
			logs.mu.Unlock()
			// Ordinary management EOF releases this actual disposable child.
			// The injected final status outcome never controls its lifetime.
			require(t, s.child.control.Close())
			state := waitDiagnosticFinal(t, s)
			if state.Complete || !state.StatusError || state.WaitError {
				t.Fatalf("status refusal certified: %+v", state)
			}
			logs.mu.Lock()
			failed, closed, finalWritten := status.failed, status.closed, status.finalWritten
			logs.mu.Unlock()
			if !failed || !closed || mode == "sync-eio" && !finalWritten {
				t.Fatalf("OS failure did not occur at actual final write/Sync: failed=%v closed=%v finalwritten=%v", failed, closed, finalWritten)
			}
			var saved ExporterDiagnostics
			raw, err := os.ReadFile(filepath.Join(s.root, "exporter-diagnostics.json"))
			require(t, err)
			require(t, json.Unmarshal(raw, &saved))
			if len(raw) != exporterLogStatusBytes || saved.Complete || !saved.StatusError {
				t.Fatalf("readable final success not invalidated: %+v", saved)
			}
			for i := 0; i < 2; i++ {
				if err := s.Close(); !errors.Is(err, unix.EIO) {
					t.Fatalf("Close %d lost terminal error after status descriptor closed: %v", i, err)
				}
			}
		})
	}
}

func (s *partialDiagnosticSink) Write(p []byte) (int, error) {
	s.calls++
	if s.calls != 1 {
		return s.diagnosticSink.Write(p)
	}
	n, err := s.diagnosticSink.Write(p[:min(7, len(p))])
	if err != nil {
		return n, err
	}
	return n, s.result
}

func (s *failingDiagnosticStatus) WriteAt(p []byte, offset int64) (int, error) {
	var state ExporterDiagnostics
	if err := json.Unmarshal(p, &state); err != nil {
		return 0, err
	}
	s.pendingFinal = state.Complete
	if state.Complete && !s.failed && s.mode == "write-at-eio" {
		s.failed = true
		n, err := s.diagnosticStatus.WriteAt(p[:17], offset)
		if err != nil {
			return n, err
		}
		return n, unix.EIO
	}
	n, err := s.diagnosticStatus.WriteAt(p, offset)
	if state.Complete && n == len(p) && err == nil {
		s.finalWritten = true
	}
	return n, err
}

func (s *failingDiagnosticStatus) Sync() error {
	if s.pendingFinal && !s.failed && s.mode == "sync-eio" {
		s.failed = true
		return unix.EIO
	}
	return s.diagnosticStatus.Sync()
}

func (s *failingDiagnosticStatus) Close() error {
	s.closed = true
	return s.diagnosticStatus.Close()
}

func waitDiagnosticFinal(t *testing.T, s *Supervisor) ExporterDiagnostics {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, ok := s.Diagnostics()
		if ok && state.ProcessExited && state.Stdout.EOF && state.Stderr.EOF {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual child/drains not finished: %+v", state)
		}
		time.Sleep(time.Millisecond)
	}
}

// diagnosticFaultOwner preserves unexpected failures while accepting the EIO
// deliberately injected by the status test. Each test asserts its own Close
// outcome; cleanup never turns an uncertain owner into a successful result.
func diagnosticFaultOwner(t *testing.T) *Supervisor {
	t.Helper()
	root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "diag-fault-")
	require(t, err)
	s, err := Create(t.Context(), filepath.Join(root, "owner"))
	require(t, err)
	t.Cleanup(func() {
		if err := s.Close(); err != nil && !errors.Is(err, unix.EIO) {
			t.Error(err)
		}
		if s.child != nil {
			select {
			case <-s.child.exited:
			case <-time.After(5 * time.Second):
				t.Error("disposable fault child failed to exit after management EOF")
			}
			if err := s.child.executable.Close(); err != nil {
				t.Error(err)
			}
		}
		if t.Failed() {
			t.Logf("preserved diagnostic fault resources: %s", root)
		} else {
			require(t, os.RemoveAll(root))
		}
	})
	return s
}

// Production strips the test runner environment. Covered helper binaries report
// that missing GOCOVERDIR at exit; assert the exact captured metadata rather
// than changing the production child environment or discarding diagnostic bytes.
func diagnosticHelperCoverageSuffix() string {
	if testing.CoverMode() != "" {
		return diagnosticHelperCoverageNotice
	}
	return ""
}
