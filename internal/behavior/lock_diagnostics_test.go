package behavior

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type lockHandoffProcess struct {
	Process
	fd int
}

func (p *lockHandoffProcess) Wait() error {
	err := p.Process.Wait()
	// The child has really exited. A different process deliberately takes the
	// lock before Wait returns, so the oracle must retain its unexpected EAGAIN.
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
	return errors.Join(err, unix.FcntlFlock(uintptr(p.fd), unix.F_SETLK, &lock))
}

func TestLockDiagnosticHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "lock-diagnostic-helper" {
		t.Skip("private subprocess entry point")
	}
	if err := serveLockDiagnosticHelper(os.Args[len(os.Args)-1]); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func serveLockDiagnosticHelper(mode string) error {
	h := helper{lock: -1}
	defer func() { _ = h.close() }()
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var command Command
		if err := decoder.Decode(&command); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if _, err := fmt.Fprintln(os.Stderr, command.Op); err != nil {
			return err
		}
		if mode == "drop-reply" && command.Op == "fcntl-try" {
			return nil
		}
		var reply Reply
		var err error
		switch {
		case mode == "retain-unlock" && command.Op == "fcntl-unlock":
			// Inject a faulty successful unlock response while the real native
			// lock remains held. This tests diagnostics, not an HFS cause.
		case mode == "query-error" && command.Op == "fcntl-query":
			err = unix.EIO
		case mode == "empty-snapshot" && command.Op == "fcntl-try":
			// EAGAIN can also mean exhausted adapter request capacity. A later
			// native query then finds no holder and must not turn failure to pass.
			err = unix.EAGAIN
		default:
			err = h.execute(command, &reply)
		}
		if err != nil {
			reply.Error = err.Error()
			var errno unix.Errno
			if errors.As(err, &errno) {
				reply.Errno = int(errno)
			}
		}
		if err := encoder.Encode(reply); err != nil {
			return err
		}
	}
}

func TestLockDiagnosticsIdentifyActualStageAndHolder(t *testing.T) {
	for _, stage := range []string{"initial-acquisition", "acquire-after-unlock", "acquire-after-exit"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			r, logs := lockDiagnosticRun(t, ctx, stage)
			parent, err := os.OpenFile(filepath.Join(r.c.Path, r.directory, "locks"), os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parent.Close() }()
			index, tries, holder := 0, 1, os.Getpid()
			switch stage {
			case "initial-acquisition":
				lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
				if err := unix.FcntlFlock(parent.Fd(), unix.F_SETLK, &lock); err != nil {
					t.Fatal(err)
				}
			case "acquire-after-unlock":
				index, tries = 1, 3
				holder = r.c.Clients[0].process.(*commandProcess).command.Process.Pid
			case "acquire-after-exit":
				tries = 3
				r.c.Clients[1].process = &lockHandoffProcess{Process: r.c.Clients[1].process, fd: int(parent.Fd())}
			}
			err = r.locks(ctx)
			if stage != "initial-acquisition" && (len(r.report.Results) != 1 || r.report.Results[0].Status != "pass") {
				t.Fatalf("did not reach failed reacquisition after a successful interruption: %+v", r.report.Results)
			}
			for _, want := range []string{fmt.Sprintf("%s helper %d fcntl-try", stage, index), unix.EAGAIN.Error(), "later F_GETLK snapshot:", fmt.Sprintf("holder_pid=%d", holder), fmt.Sprintf("type=%d", unix.F_WRLCK)} {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q from actual lock failure: %v", want, err)
				}
			}
			assertLockCommands(t, logs[index], tries, 1)
			// The query did not waive failure, release the holder or acquire a
			// replacement lock. A fresh request still observes actual contention.
			reply, err := r.c.Clients[index].Call(ctx, Command{Op: "fcntl-try"})
			if err != nil || reply.Errno != int(unix.EAGAIN) {
				t.Fatalf("diagnostic changed lock state: %+v, %v", reply, err)
			}
		})
	}
}

func TestLockDiagnosticsPreserveUnknownAndQueryFailure(t *testing.T) {
	for _, mode := range []string{"normal", "drop-reply", "query-error", "empty-snapshot"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			r, logs := lockDiagnosticRun(t, ctx, mode)
			for i := range 2 {
				if err := r.call(ctx, i, "open-lock"); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "query-error" {
				if err := r.call(ctx, 0, "fcntl-try"); err != nil {
					t.Fatal(err)
				}
			}
			err := r.lockStep(ctx, "controlled-acquisition", 1, "fcntl-try")
			queries := 0
			switch mode {
			case "normal":
				if err != nil {
					t.Fatal(err)
				}
			case "drop-reply":
				if !errors.Is(err, io.EOF) || !strings.Contains(err.Error(), "controlled-acquisition helper 1 fcntl-try") {
					t.Fatalf("unknown reply outcome lost: %v", err)
				}
			case "query-error":
				queries = 1
				if err == nil || !strings.Contains(err.Error(), unix.EAGAIN.Error()) || !strings.Contains(err.Error(), "F_GETLK diagnostic failed: "+unix.EIO.Error()) {
					t.Fatalf("query failure replaced original contention: %v", err)
				}
			case "empty-snapshot":
				queries = 1
				if err == nil || !strings.Contains(err.Error(), unix.EAGAIN.Error()) || !strings.Contains(err.Error(), fmt.Sprintf("type=%d", unix.F_UNLCK)) {
					t.Fatalf("empty later snapshot waived original contention: %v", err)
				}
			}
			assertLockCommands(t, logs[1], 1, queries)
		})
	}
}

func lockDiagnosticRun(t *testing.T, ctx context.Context, mode string) (*run, []string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := &run{c: Config{Path: t.TempDir(), Profile: "filesystem", Timeout: time.Second}, directory: "probe"}
	var logs []string
	for i := range 3 {
		log, err := os.Create(filepath.Join(t.TempDir(), "commands"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = log.Close() })
		logs = append(logs, log.Name())
		helperMode := "normal"
		if i == 0 && mode == "acquire-after-unlock" {
			helperMode = "retain-unlock"
		}
		if i == 1 && (mode == "drop-reply" || mode == "query-error" || mode == "empty-snapshot") {
			helperMode = mode
		}
		spawn := func(ctx context.Context, in io.Reader, out io.Writer, _ *syscall.Credential) (Process, error) {
			command := exec.CommandContext(ctx, executable, "-test.run=^TestLockDiagnosticHelper$", "lock-diagnostic-helper", helperMode)
			command.Stdin, command.Stdout, command.Stderr = in, out, log
			if err := command.Start(); err != nil {
				return nil, err
			}
			return &commandProcess{command: command}, nil
		}
		client, err := Start(ctx, spawn, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Stop(context.Background()) })
		r.c.Clients = append(r.c.Clients, client)
	}
	if err := r.bind(ctx); err != nil {
		t.Fatal(err)
	}
	return r, logs
}

func assertLockCommands(t *testing.T, path string, tries, queries int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "fcntl-try\n") != tries || strings.Count(string(data), "fcntl-query\n") != queries {
		t.Fatalf("unexpected native acquisition retry or query: %s", data)
	}
}
