//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type detachedDiagnosticResult struct {
	Native, Signal, Stdout, Stderr int
}

// TestExporterDetachedParent is a disposable runtime owner process. Its abrupt
// exit models Run/main return or daemon death, without relying on Close keeping
// any Go goroutine alive. It never starts bhyve, mounts or guest workloads.
func TestExporterDetachedParent(t *testing.T) {
	if len(os.Args) < 6 || os.Args[len(os.Args)-5] != "hfs-detached-parent" {
		return
	}
	args := os.Args[len(os.Args)-4:]
	owner, err := Create(t.Context(), filepath.Join(args[0], "owner"))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "detached owner Create: %v\n", err)
		os.Exit(91)
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "detached owner read child executable: %v\n", err)
		os.Exit(92)
	}
	spec := Executable{Path: args[1], SHA256: sha256.Sum256(data), Args: []string{args[2], args[3]}}
	if err = owner.Start(t.Context(), spec); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "detached owner Start: %v\n", err)
		os.Exit(93)
	}
	if err = owner.child.control.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "detached owner readiness deadline: %v\n", err)
		os.Exit(94)
	}
	var ready [1]byte
	if _, err = io.ReadFull(owner.child.control, ready[:]); err != nil || ready[0] != 'R' {
		_, _ = fmt.Fprintf(os.Stderr, "detached owner readiness: byte=%q error=%v\n", ready, err)
		os.Exit(95)
	}
	// Deliberately no Close: every reader/drain disappears with this process.
	os.Exit(0)
}

// TestDiagnosticComponentParent exercises actual diagnostic construction and
// attachment under direct private-pinned-path exec solely inside this fixture.
// It does not call or qualify Supervisor.Start, FD exec, observer or readiness.
func TestDiagnosticComponentParent(t *testing.T) {
	if len(os.Args) < 6 || os.Args[len(os.Args)-5] != "hfs-diagnostic-component-parent" {
		return
	}
	args := os.Args[len(os.Args)-4:]
	owner, err := Create(t.Context(), filepath.Join(args[0], "owner"))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component owner Create: %v\n", err)
		os.Exit(81)
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component read child executable: %v\n", err)
		os.Exit(82)
	}
	pinned, err := owner.pinExecutable(Executable{Path: args[1], SHA256: sha256.Sum256(data)})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component private executable pin: %v\n", err)
		os.Exit(83)
	}
	defer func() { _ = pinned.Close() }()
	control, inherited, err := controlPair()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component fixture control pair: %v\n", err)
		os.Exit(84)
	}
	logs, err := newExporterLogs(owner.directory)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component diagnostic constructor: %v\n", err)
		os.Exit(85)
	}
	// This TEST-only pathname is the actual source-pinned private file. It
	// supplies no production fallback for the separate FD-exec prerequisite.
	command := exec.Command(filepath.Join(owner.root, "exporter"), args[2], args[3])
	command.ExtraFiles = []*os.File{inherited}
	logs.attach(command)
	if err = command.Start(); err != nil {
		logs.abort()
		_, _ = fmt.Fprintf(os.Stderr, "component direct fixture exec: %v\n", err)
		os.Exit(86)
	}
	_ = inherited.Close()
	logs.start()
	waited := make(chan error, 1)
	go func() {
		waitErr := command.Wait()
		waited <- waitErr
		logs.exited(waitErr)
	}()
	if err = control.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component readiness deadline: %v\n", err)
		os.Exit(87)
	}
	var ready [1]byte
	if _, err = io.ReadFull(control, ready[:]); err != nil || ready[0] != 'R' {
		_, _ = fmt.Fprintf(os.Stderr, "component readiness: byte=%q error=%v\n", ready, err)
		reportComponentStartupFailure(logs, waited, owner.root)
		os.Exit(88)
	}
	// Actual process exit destroys all real drain goroutines and readers.
	os.Exit(0)
}

// reportComponentStartupFailure never calls Wait a second time. It bounds only
// failure diagnostics for this disposable fixture; successful parent death is
// still abrupt and does not depend on a drain or a child-exit acknowledgement.
func reportComponentStartupFailure(logs *exporterLogs, waited <-chan error, root string) {
	deadline := time.Now().Add(2 * time.Second)
	select {
	case err := <-waited:
		_, _ = fmt.Fprintf(os.Stderr, "component actual child Wait: %v\n", err)
	case <-time.After(time.Until(deadline)):
		_, _ = fmt.Fprintln(os.Stderr, "component child Wait still pending at diagnostic bound")
	}
	var state ExporterDiagnostics
	for {
		logs.mu.Lock()
		state = logs.snapshot()
		logs.mu.Unlock()
		if state.ProcessExited && (state.Stdout.EOF || state.Stdout.ReadError) && (state.Stderr.EOF || state.Stderr.ReadError) || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _ = fmt.Fprintf(os.Stderr, "component bounded diagnostic status: %+v\n", state)
	file, err := os.Open(filepath.Join(root, "exporter.stderr"))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component stderr open: %v\n", err)
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "component stderr stat: %v\n", err)
		return
	}
	buffer := make([]byte, 4096)
	n, err := file.ReadAt(buffer, max(int64(0), info.Size()-int64(len(buffer))))
	_, _ = fmt.Fprintf(os.Stderr, "component stderr tail (<=4096 bytes): %q read=%v\n", buffer[:n], err)
}

func buildDiagnosticChild(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("HFS_DIAGNOSTIC_CHILD"); path != "" {
		if !filepath.IsAbs(path) {
			t.Fatal("fixture child path must be absolute")
		}
		return path
	}
	compiler, err := exec.LookPath("cc")
	require(t, err)
	binary := filepath.Join(t.TempDir(), "diagnostic-child")
	flags := []string{"-std=c11", "-Wall", "-Wextra", "-Werror", "-O1", "-g"}
	if runtime.GOOS == "linux" {
		flags = append(flags, "-D_DEFAULT_SOURCE", "-D_POSIX_C_SOURCE=200809L")
	}
	flags = append(flags, "testdata/diagnostic_child.c", "-o", binary)
	command := exec.CommandContext(t.Context(), compiler, flags...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("disposable C child compile: %v\n%s", err, output)
	}
	return binary
}

func TestActualExporterDiagnosticsParentExit(t *testing.T) {
	checkDiagnosticParentExit(t, "TestExporterDetachedParent", "hfs-detached-parent")
}

// TestDiagnosticComponentParentExit is the diagnostic transport component proof.
// The separate full-Start test remains mandatory and cannot be passed by this.
func TestDiagnosticComponentParentExit(t *testing.T) {
	checkDiagnosticParentExit(t, "TestDiagnosticComponentParent", "hfs-diagnostic-component-parent")
}

func checkDiagnosticParentExit(t *testing.T, helper, marker string) {
	t.Helper()
	binary := buildDiagnosticChild(t)
	modes := []string{"default-negative", "native"}
	if runtime.GOOS == "linux" {
		modes[1] = "linux-ignore-boundary"
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "exit-")
			require(t, err)
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("preserved parent-exit resources: %s", root)
				} else {
					require(t, os.RemoveAll(root))
				}
			})
			path := filepath.Join(root, "oracle")
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			require(t, err)
			defer func() { _ = listener.Close() }()
			parent := helperSpec(t)
			command := exec.CommandContext(t.Context(), parent.Path, "-test.run=^"+helper+"$", "--", marker, root, binary, path, mode)
			var output bytes.Buffer
			command.Stdout, command.Stderr = &output, &output
			require(t, command.Start())
			finished := make(chan error, 1)
			go func() { finished <- command.Wait() }()
			require(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
			connection, err := listener.AcceptUnix()
			if err != nil {
				select {
				case parentErr := <-finished:
					t.Fatalf("oracle accept: %v; actual owner exit: %v\n%s", err, parentErr, output.Bytes())
				default:
					t.Fatalf("oracle accept: %v; actual owner has not completed", err)
				}
			}
			defer func() { _ = connection.Close() }()
			require(t, connection.SetDeadline(time.Now().Add(5*time.Second)))
			var ready [1]byte
			_, err = io.ReadFull(connection, ready[:])
			require(t, err)
			if ready[0] != 'R' {
				t.Fatal("disposable child not ready")
			}
			select {
			case err = <-finished:
				if err != nil {
					t.Fatalf("actual owner exit: %v\n%s", err, output.Bytes())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("disposable owner failed to exit")
			}
			// Actual owner is already reaped. Only the independent test oracle
			// releases the C child's writes on its inherited fd1/fd2.
			_, err = connection.Write([]byte{'W'})
			require(t, err)
			var result detachedDiagnosticResult
			require(t, json.NewDecoder(io.LimitReader(connection, 257)).Decode(&result))
			if mode == "default-negative" {
				if result.Signal != int(unix.SIGPIPE) {
					t.Fatalf("negative writer did not die from SIGPIPE: %+v", result)
				}
			} else {
				if result.Signal != 0 || result.Stdout != int(unix.EPIPE) || result.Stderr != int(unix.EPIPE) {
					t.Fatalf("child failed scoped EPIPE survival: %+v", result)
				}
				if runtime.GOOS == "freebsd" && result.Native != 1 {
					t.Fatalf("native socket option missing after exec: %+v", result)
				}
				if runtime.GOOS == "linux" && result.Native != 0 {
					t.Fatal("Linux boundary claimed native safety")
				}
			}
			var saved ExporterDiagnostics
			raw, err := os.ReadFile(filepath.Join(root, "owner", "exporter-diagnostics.json"))
			require(t, err)
			require(t, json.Unmarshal(raw, &saved))
			if len(raw) != exporterLogStatusBytes || !saved.Started || saved.Complete || saved.ProcessExited || saved.Stdout.EOF || saved.Stderr.EOF {
				t.Fatalf("daemon death fabricated final capture: %+v bytes=%d", saved, len(raw))
			}
			var extra [1]byte
			if n, err := connection.Read(extra[:]); n != 0 || err != io.EOF {
				t.Fatalf("child oracle did not close: n=%d err=%v", n, err)
			}
		})
	}
}
