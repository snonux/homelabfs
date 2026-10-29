//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/behavior"
	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

func TestLinuxBehaviorEINTRBoundary(t *testing.T) {
	requireLocalFuse(t)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	base, err := os.MkdirTemp("", "hfs-behavior-eintr-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("run-owned admission probe: %s", base)
	clean := false
	t.Cleanup(func() {
		if clean && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved probe: %s", base)
		}
	})
	service := serviceWithIO(t, base, nativeRequestBytes)
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	a, err := New(Config{Service: service, Validation: service.FrontendInfo().Validation, Credentials: p})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.BindMount(a.Identity()); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var helperProcess *Participant
	spawn := func(ctx context.Context, in io.Reader, out io.Writer, cred *syscall.Credential) (behavior.Process, error) {
		var err error
		helperProcess, err = p.Start(ctx, ParticipantSpec{Executable: executable, Args: []string{"-test.run=^TestPortableBehaviorHelper$", "behavior-helper"}, Env: append(os.Environ(), "GODEBUG=asyncpreemptoff=1"), Stdin: in, Stdout: out, Stderr: os.Stderr})
		return helperProcess, err
	}
	child, err := behavior.Start(ctx, spawn, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mount *Mount
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopCancel()
		if err := child.Stop(stopCtx); err != nil {
			t.Error(err)
			return
		}
		if mount != nil {
			if err := mount.Unmount(); err != nil {
				t.Error(err)
			}
		}
	})
	gate := make(chan uint32, 2)
	observations := make(chan string, 2)
	var writes atomic.Int32
	actual := a.service
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		if c.Operation != fsapi.Write {
			return actual.Dispatch(ctx, r, c)
		}
		n := writes.Add(1)
		if n == 1 {
			gate <- ctx.(*fuse.Context).Pid
			<-ctx.Done()
			observations <- "pre-admission rejected: " + ctx.Err().Error()
			return ctx.Err()
		}
		err := actual.Dispatch(ctx, r, c)
		if err != nil {
			return err
		}
		if n == 2 {
			gate <- ctx.(*fuse.Context).Pid
			<-ctx.Done()
			result, waitErr := r.Completion().Wait(context.Background())
			observations <- fmt.Sprintf("admitted completion code=%v count=%d wait=%v", result.Code, result.Count, waitErr)
		}
		return nil
	})
	mountpoint := filepath.Join(base, "mount")
	if err := os.Mkdir(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	mount, err = a.Mount(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	id := a.Identity()
	identity := "homelabfs-validation-v1\n" + id.Volume.String() + "\n" + id.Generation.String() + "\n" + id.Session.String() + "\n"
	if reply, err := child.Call(ctx, behavior.Command{Op: "bind", Path: mountpoint, Run: "probe", Identity: identity, Create: true}); err != nil || reply.Error != "" {
		t.Fatal(reply, err)
	}
	for i := range 2 {
		if err := child.Send(ctx, behavior.Command{Op: "append-once", Data: "writer-A:0000\n"}); err != nil {
			t.Fatal(err)
		}
		var tid uint32
		select {
		case tid = <-gate:
		case <-ctx.Done():
			t.Fatal("write gate", ctx.Err())
		}
		// Linux sends the runtime's preemption signal to this exact blocked
		// syscall thread. The helper disables automatic preemption so the two
		// observed cancellation boundaries have no unrelated signal noise.
		if err := unix.Tgkill(helperProcess.PID(), int(tid), unix.SIGURG); err != nil {
			t.Fatal(err)
		}
		reply, err := child.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("raw append %d: %+v; %s", i, reply, <-observations)
		if i == 0 && reply.Errno != int(unix.EINTR) {
			t.Fatalf("pre-admission return: %+v", reply)
		}
		if i == 1 && reply.Error != "" {
			t.Fatalf("admitted append returned error: %+v", reply)
		}
		root, err := OpenMount(mountpoint, a.Identity())
		if err != nil {
			t.Fatal(err)
		}
		fd, err := unix.Openat(int(root.Fd()), "probe/append-probe", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
		if err != nil {
			_ = root.Close()
			t.Fatal(err)
		}
		data := make([]byte, 64)
		n, readErr := unix.Pread(fd, data, 0)
		if err := errors.Join(readErr, unix.Close(fd), root.Close()); err != nil {
			t.Fatal(err)
		}
		want := ""
		if i == 1 {
			want = "writer-A:0000\n"
		}
		if string(data[:n]) != want {
			t.Fatalf("raw append applied incorrect count/content: %q", data[:n])
		}
	}
	if err := child.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := mount.Unmount(); err != nil {
		t.Fatal(err)
	}
	mount = nil
	clean = true
}
