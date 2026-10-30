package behavior

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
)

type failingStopProcess struct {
	Process
	failure error
}

func (p failingStopProcess) Wait() error { return errors.Join(p.Process.Wait(), p.failure) }

func TestStopSignalHelper(t *testing.T) {
	if os.Getenv("HOMELABFS_STOP_SIGNAL_HELPER") != "1" {
		t.Skip("private subprocess entry point")
	}
	// Remain alive independently of command-pipe EOF, so Stop must observe the
	// expected SIGTERM alongside any independent process-owner failure.
	wake := make(chan os.Signal, 1)
	signal.Notify(wake, syscall.SIGUSR1)
	if err := json.NewEncoder(os.Stdout).Encode(Reply{}); err != nil {
		os.Exit(2)
	}
	<-wake
	os.Exit(3)
}

func TestClientStopPreservesRetirementFailureWithExpectedSignal(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("process retirement failed")
	for _, extra := range []error{nil, failure} {
		spawn := func(ctx context.Context, in io.Reader, out io.Writer, _ *syscall.Credential) (Process, error) {
			command := exec.CommandContext(ctx, executable, "-test.run=^TestStopSignalHelper$")
			command.Env = append(os.Environ(), "HOMELABFS_STOP_SIGNAL_HELPER=1")
			command.Stdin, command.Stdout = in, out
			if err := command.Start(); err != nil {
				return nil, err
			}
			return failingStopProcess{Process: &commandProcess{command: command}, failure: extra}, nil
		}
		client, err := Start(t.Context(), spawn, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Stop(context.Background()) })
		if _, err := client.Receive(t.Context()); err != nil {
			t.Fatal(err)
		}
		got := client.Stop(t.Context())
		if extra == nil && got != nil || extra != nil && !errors.Is(got, failure) {
			t.Fatalf("Stop lost independent failure: extra=%v got=%v", extra, got)
		}
	}
}

func TestStopWaitErrorPreservesLifecycleFailureAlongsideSignal(t *testing.T) {
	// Use the real OS exit status instead of fabricating platform wait bits.
	exit := exec.Command("sh", "-c", "kill -TERM $$").Run()
	if exit == nil {
		t.Fatal("signal child succeeded")
	}
	lifecycle := errors.New("process retirement failed")
	for _, tc := range []struct {
		name string
		wait error
		want error
	}{
		{"signal-only", exit, nil},
		{"signal-with-notification", errors.Join(exit, lifecycle), lifecycle},
		{"nested-notification", errors.Join(errors.Join(exit, lifecycle), exit), lifecycle},
		{"notification-only", lifecycle, lifecycle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := stopWaitError(tc.wait)
			if tc.want == nil && got != nil || tc.want != nil && !errors.Is(got, tc.want) {
				t.Fatal(got)
			}
		})
	}
}
