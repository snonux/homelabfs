package behavior

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type failedDiagnosticWriter struct{ err error }

func (w failedDiagnosticWriter) Write([]byte) (int, error) { return 0, w.err }

func TestAppendDiagnosticHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "append-diagnostic-helper" {
		t.Skip("private subprocess entry point")
	}
	os.Exit(CLI(context.Background(), []string{"-helper"}, os.Stdout, os.Stderr))
}

func TestAppendDiagnosticsPreserveWorkloadAndOracle(t *testing.T) {
	var input, output, diagnostics bytes.Buffer
	for _, command := range []Command{{Op: "bind", Path: t.TempDir(), Run: "probe", Create: true}, {Op: "append", Data: "A"}, {Op: "append", Data: "B"}, {Op: "verify-append"}} {
		if err := json.NewEncoder(&input).Encode(command); err != nil {
			t.Fatal(err)
		}
	}
	if err := serveHelper(t.Context(), &input, &output, &diagnostics); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	for range 4 {
		var reply Reply
		if err := decoder.Decode(&reply); err != nil || reply.Error != "" {
			t.Fatalf("actual helper reply: %+v, %v", reply, err)
		}
	}
	events := decodeAppendDiagnostics(t, diagnostics.Bytes())
	if len(events) != 76 {
		t.Fatalf("diagnostic count %d, want bounded 2*38", len(events))
	}
	for i, marker := range []string{"A", "B"} {
		group := events[i*38 : (i+1)*38]
		phases := []string{"open.begin", "open.end"}
		for range appendCount {
			phases = append(phases, "write.begin", "write.end")
		}
		phases = append(phases, "fsync.begin", "fsync.end", "close.begin", "close.end")
		for n, event := range group {
			wantRecord := -1
			if n >= 2 && n < 34 {
				wantRecord = (n - 2) / 2
			}
			if event.Marker != marker || event.Phase != phases[n] || event.Record != wantRecord || event.Error != "" || event.Errno != 0 {
				t.Fatalf("unexpected event %d: %+v", n, event)
			}
			if n > 0 && event.ElapsedNS < group[n-1].ElapsedNS {
				t.Fatal("monotonic elapsed time went backwards")
			}
		}
	}
}

func TestAppendDiagnosticsRecordNativeFailure(t *testing.T) {
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "append"), 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	var output bytes.Buffer
	trace := newAppendDiagnostics(&output, "A")
	err = appendRecordsObserved(int(root.Fd()), "A", trace)
	if !errors.Is(err, unix.EISDIR) || trace.failure() != nil {
		t.Fatalf("actual open result: %v, trace: %v", err, trace.failure())
	}
	events := decodeAppendDiagnostics(t, output.Bytes())
	if len(events) != 2 || events[0].Phase != "open.begin" || events[1].Phase != "open.end" || events[1].Errno != int(unix.EISDIR) || events[1].Error == "" {
		t.Fatalf("lost native failed operation: %+v", events)
	}
}

func TestAppendDiagnosticFailureCannotPass(t *testing.T) {
	path := t.TempDir()
	root, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	want := errors.New("diagnostic output unavailable")
	h := helper{work: root, lock: -1, diagnostics: failedDiagnosticWriter{want}}
	if err := h.execute(Command{Op: "append", Data: "A"}, &Reply{}); !errors.Is(err, want) {
		t.Fatalf("diagnostic failure became success: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(path, "append"))
	if err != nil {
		t.Fatal(err)
	}
	var expected strings.Builder
	for i := range appendCount {
		expected.WriteString(appendRecord("A", i) + "\n")
	}
	if string(data) != expected.String() {
		t.Fatal("diagnostic failure changed synthetic syscall workload")
	}
}

func TestAppendDiagnosticsBlockedNativeOperation(t *testing.T) {
	for _, phase := range []string{"open", "write"} {
		t.Run(phase, func(t *testing.T) {
			base := t.TempDir()
			log, err := os.Create(filepath.Join(base, "diagnostics"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = log.Close() }()
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			spawn := func(ctx context.Context, in io.Reader, out io.Writer, _ *syscall.Credential) (Process, error) {
				command := exec.CommandContext(ctx, executable, "-test.run=^TestAppendDiagnosticHelper$", "append-diagnostic-helper")
				command.Stdin, command.Stdout, command.Stderr = in, out, log
				if err := command.Start(); err != nil {
					return nil, err
				}
				return &commandProcess{command: command}, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var clients []*Client
			for i := range 2 {
				client, err := Start(ctx, spawn, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = client.Stop(context.Background()) }()
				clients = append(clients, client)
				reply, err := client.Call(ctx, Command{Op: "bind", Path: base, Run: "probe", Create: i == 0})
				if err != nil || reply.Error != "" {
					t.Fatalf("bind: %+v, %v", reply, err)
				}
			}
			fifo := filepath.Join(base, "probe", "append")
			if err := unix.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			if phase == "write" {
				fillAppendFIFO(t, fifo)
			}
			bounded, stop := context.WithTimeout(ctx, 100*time.Millisecond)
			defer stop()
			start := time.Now()
			err = (&run{c: Config{Clients: clients}}).append(bounded)
			if !errors.Is(err, os.ErrDeadlineExceeded) || !strings.Contains(err.Error(), "append helper 0 receive") {
				t.Fatalf("blocked actual %s result: %v", phase, err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("pipe response ignored bounded timeout")
			}
			for _, client := range clients {
				if err := client.Stop(ctx); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(log.Name())
			if err != nil {
				t.Fatal(err)
			}
			events := decodeAppendDiagnostics(t, data)
			want := 1
			if phase == "write" {
				want = 3
			}
			for _, marker := range []string{"A", "B"} {
				var group []appendDiagnostic
				for _, event := range events {
					if event.Marker == marker {
						group = append(group, event)
					}
				}
				if len(group) != want || group[len(group)-1].Phase != phase+".begin" {
					t.Fatalf("blocked %s not identified for %s: %+v", phase, marker, events)
				}
			}
		})
	}
}

func fillAppendFIFO(t *testing.T, path string) {
	t.Helper()
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	for range 1024 {
		_, err := unix.Write(fd, make([]byte, 4096))
		if errors.Is(err, unix.EAGAIN) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("FIFO capacity exceeds bounded fixture")
}

func decodeAppendDiagnostics(t *testing.T, data []byte) []appendDiagnostic {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var events []appendDiagnostic
	for {
		var event appendDiagnostic
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		if event.Event != "behavior-append" || event.PID == 0 || event.Time.IsZero() || event.ElapsedNS < 0 {
			t.Fatalf("missing causal identity/time: %+v", event)
		}
		events = append(events, event)
	}
}
