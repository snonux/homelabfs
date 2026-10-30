//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

func exitFixture(t *testing.T) (*Participants, *Adapter) {
	t.Helper()
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	a, _ := serviceFixture(t, "", p)
	if err := p.BindMount(a.Identity()); err != nil {
		t.Fatal(err)
	}
	return p, a
}

func participantHeader(node uint64, pid int) fuse.InHeader {
	return fuse.InHeader{NodeId: node, Caller: fuse.Caller{Owner: fuse.Owner{Uid: uint32(os.Geteuid()), Gid: uint32(os.Getegid())}, Pid: uint32(pid)}}
}

func awaitExitStatus(t *testing.T, done <-chan fuse.Status) fuse.Status {
	t.Helper()
	select {
	case code := <-done:
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("bounded lock completion timed out")
		return fuse.EIO
	}
}

func TestParticipantExitDropsHeldPOSIXRangeBeforeReacquisition(t *testing.T) {
	p, a := exitFixture(t)
	child := startFixtureChild(t, p, nil)
	child.call(t, fixtureCommand{Op: "ready"})
	var file fuse.CreateOut
	h := participantHeader(1, child.process.PID())
	if code := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0666}, "held", &file); code != fuse.OK {
		t.Fatal(code)
	}
	h.NodeId = file.NodeId
	lock := fuse.LkIn{InHeader: h, Fh: file.Fh, Owner: uint64(h.Pid), Lk: fuse.FileLock{End: math.MaxInt64, Typ: syscall.F_WRLCK}}
	if code := a.SetLk(nil, &lock); code != fuse.OK {
		t.Fatal(code)
	}
	child.call(t, fixtureCommand{Op: "exit"})
	if err := child.process.Wait(); err != nil {
		t.Fatal(err)
	}
	lock.InHeader = participantHeader(file.NodeId, os.Getpid())
	lock.Owner = uint64(os.Getpid())
	if code := a.SetLk(nil, &lock); code != fuse.OK {
		t.Fatalf("acquire-after-exit: dead child's POSIX range remains: %v", code)
	}
}

// Real child exit is observed by Participants.Wait. Kernel close callbacks are
// deliberately held back: this checks the ownership boundary independently of
// native FLUSH/RELEASE scheduling, including requests authenticated before exit.
func TestParticipantExitRetiresLocksBeforeKernelDescriptionDrain(t *testing.T) {
	p, a := exitFixture(t)
	departing := startFixtureChild(t, p, nil)
	survivor := startFixtureChild(t, p, nil)
	departing.call(t, fixtureCommand{Op: "ready"})
	survivor.call(t, fixtureCommand{Op: "ready"})
	var created fuse.CreateOut
	if code := a.Create(nil, &fuse.CreateIn{InHeader: participantHeader(1, os.Getpid()), Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0666}, "exit-locks", &created); code != fuse.OK {
		t.Fatal(code)
	}
	open := func(pid int) (fuse.InHeader, uint64) {
		h := participantHeader(created.NodeId, pid)
		var out fuse.OpenOut
		if code := a.Open(nil, &fuse.OpenIn{InHeader: h, Flags: syscall.O_RDWR}, &out); code != fuse.OK {
			t.Fatal(code)
		}
		return h, out.Fh
	}
	deadHeader, deadHandle := open(departing.process.PID())
	liveHeader, liveHandle := open(survivor.process.PID())
	dead := fuse.LkIn{InHeader: deadHeader, Fh: deadHandle, Owner: uint64(deadHeader.Pid), Lk: fuse.FileLock{Start: 0, End: 9, Typ: syscall.F_WRLCK}}
	live := fuse.LkIn{InHeader: liveHeader, Fh: liveHandle, Owner: uint64(liveHeader.Pid), Lk: fuse.FileLock{Start: 20, End: 29, Typ: syscall.F_WRLCK}}
	for _, lock := range []*fuse.LkIn{&dead, &live} {
		if code := a.SetLk(nil, lock); code != fuse.OK {
			t.Fatal(code)
		}
	}
	forged := dead
	forged.Uid++
	forged.Lk.Typ = syscall.F_UNLCK
	if code := a.SetLk(nil, &forged); code != fuse.EACCES {
		t.Fatal("wrong credentials could clear another owner's range", code)
	}
	flock := dead
	flock.LkFlags = fuse.FUSE_LK_FLOCK
	flock.Lk.End = math.MaxInt64
	if code := a.SetLk(nil, &flock); code != fuse.OK {
		t.Fatal(code)
	}
	service := a.service
	waitAdmitted, lateAdmitted, releaseLate := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseLate) }) }
	t.Cleanup(release)
	var exits atomic.Int32
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		if c.Operation == fsapi.TryLock && c.Lock.Start == 40 {
			close(lateAdmitted)
			<-releaseLate
		}
		if c.Operation == fsapi.ProcessExit {
			exits.Add(1)
		}
		err := service.Dispatch(ctx, r, c)
		if err == nil && c.Operation == fsapi.WaitLock {
			close(waitAdmitted)
		}
		return err
	})
	wait := dead
	wait.Lk = live.Lk
	waitDone := make(chan fuse.Status, 1)
	go func() { waitDone <- a.SetLkw(nil, &wait) }()
	select {
	case <-waitAdmitted:
	case <-time.After(3 * time.Second):
		t.Fatal("wait not admitted")
	}
	late := dead
	late.Lk.Start, late.Lk.End = 40, 49
	lateDone := make(chan fuse.Status, 1)
	go func() { lateDone <- a.SetLk(nil, &late) }()
	select {
	case <-lateAdmitted:
	case <-time.After(3 * time.Second):
		t.Fatal("late request not authenticated")
	}
	if err := departing.process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = departing.input.Close()
	err := departing.process.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() {
		t.Fatal("expected observed signal exit", err)
	}
	if again := departing.process.Wait(); again != err || exits.Load() != 1 {
		t.Fatal("exit notification not exactly once", again, exits.Load())
	}
	if code := awaitExitStatus(t, waitDone); code != fuse.EINTR {
		t.Fatal("exit did not cancel pending lock", code)
	}
	release()
	if code := awaitExitStatus(t, lateDone); code != status(fsapi.Stale) {
		t.Fatal("preauthenticated request acquired after exit", code)
	}
	if code := a.SetLk(nil, &dead); code != fuse.EACCES {
		t.Fatal("exited identity admitted", code)
	}
	parent := dead
	parent.InHeader = participantHeader(created.NodeId, os.Getpid())
	parent.Owner, parent.Fh = uint64(os.Getpid()), created.Fh
	if code := a.SetLk(nil, &parent); code != fuse.OK {
		t.Fatal("dead process range survived positive Wait", code)
	}
	query := parent
	query.Lk = live.Lk
	var out fuse.LkOut
	if code := a.GetLk(nil, &query, &out); code != fuse.OK || out.Lk.Pid != liveHeader.Pid {
		t.Fatal("surviving process lock erased", out, code)
	}
	liveUnlock := live
	liveUnlock.Lk.Typ = syscall.F_UNLCK
	if code := a.SetLk(nil, &liveUnlock); code != fuse.OK {
		t.Fatal(code)
	}
	if code := a.SetLk(nil, &query); code != fuse.OK {
		t.Fatal("dead pending waiter granted after survivor unlock", code)
	}
	parentFlock := parent
	parentFlock.LkFlags, parentFlock.Lk.End = fuse.FUSE_LK_FLOCK, math.MaxInt64
	if code := a.SetLk(nil, &parentFlock); code != fuse.EAGAIN {
		t.Fatal("exit closed retained description/flock", code)
	}
	d, ok := a.description(deadHandle, created.NodeId, false)
	if !ok {
		t.Fatal("exit discarded description")
	}
	if _, code := a.callAuthorized(context.Background(), d.grant, fsapi.Command{Operation: fsapi.Write, Object: d.object, Handle: d.handle, Data: []byte("x")}); code != fuse.OK {
		t.Fatal("exit revoked issued write grant", code)
	}
	a.Release(nil, &fuse.ReleaseIn{InHeader: fuse.InHeader{NodeId: created.NodeId}, Fh: deadHandle})
	if code := a.SetLk(nil, &parentFlock); code != fuse.OK {
		t.Fatal("final description drain retained flock", code)
	}
	contender := live
	contender.Lk = parent.Lk
	if code := a.SetLk(nil, &contender); code != fuse.EAGAIN {
		t.Fatal("late release erased another process lock", code)
	}
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestParticipantExitNotificationFailureStillRevokesAndReaps(t *testing.T) {
	p, a := exitFixture(t)
	child := startFixtureChild(t, p, nil)
	child.call(t, fixtureCommand{Op: "ready"})
	service := a.service
	var calls int
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		if c.Operation == fsapi.ProcessExit {
			calls++
			return fsapi.IO
		}
		return service.Dispatch(ctx, r, c)
	})
	child.call(t, fixtureCommand{Op: "exit"})
	err := child.process.Wait()
	if !errors.Is(err, fsapi.IO) || !errors.Is(a.Err(), fsapi.IO) || child.process.command.ProcessState == nil {
		t.Fatal("notification failure lost or child not reaped", err, a.Err())
	}
	if again := child.process.Wait(); again != err || calls != 1 {
		t.Fatal("failed notification repeated", again, calls)
	}
	h := participantHeader(1, child.process.PID())
	if _, err := p.Snapshot(context.Background(), Caller{UID: h.Uid, GID: h.Gid, Thread: h.Pid}); !errors.Is(err, fsapi.Permission) {
		t.Fatal("failed notification restored dead admission", err)
	}
	if code := a.Open(nil, &fuse.OpenIn{InHeader: participantHeader(1, os.Getpid())}, &fuse.OpenOut{}); code != status(fsapi.Unavailable) {
		t.Fatal("cleanup failure did not close adapter gate", code)
	}
}

func TestParticipantExitBlockedNotificationAlreadyRevokedAndReaped(t *testing.T) {
	p, a := exitFixture(t)
	child := startFixtureChild(t, p, nil)
	child.call(t, fixtureCommand{Op: "ready"})
	service := a.service
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		if c.Operation == fsapi.ProcessExit {
			close(entered)
			<-release
		}
		return service.Dispatch(ctx, r, c)
	})
	child.call(t, fixtureCommand{Op: "exit"})
	done := make(chan error, 1)
	go func() { done <- child.process.Wait() }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("notification not reached")
	}
	if child.process.command.ProcessState == nil {
		t.Fatal("blocked notification prevented sole reap")
	}
	h := participantHeader(1, child.process.PID())
	if _, err := p.Snapshot(context.Background(), Caller{UID: h.Uid, GID: h.Gid, Thread: h.Pid}); !errors.Is(err, fsapi.Permission) {
		t.Fatal("blocked notification retained admission", err)
	}
	select {
	case err := <-done:
		t.Fatal("Wait returned before retirement completed", err)
	default:
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not complete after retirement")
	}
}

func TestParticipantExitBindingRejectsForeignConnection(t *testing.T) {
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	id := MountIdentity{Volume: testID(1), Generation: testID(2), Session: testID(3)}
	if err := p.BindMount(id); err != nil {
		t.Fatal(err)
	}
	s := serviceWithIO(t, "", nativeRequestBytes)
	if _, err := New(Config{Service: s, Validation: fsservice.ValidationProfile{Disposable: true, Name: s.FrontendInfo().Validation.Name}, Credentials: p}); !errors.Is(err, fsapi.Stale) {
		t.Fatal("foreign process provider accepted", err)
	}
}

func TestParticipantExitBindingCannotBeReplacedBeforeMountBinding(t *testing.T) {
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	s := serviceWithIO(t, "", nativeRequestBytes)
	c := Config{Service: s, Validation: s.FrontendInfo().Validation, Credentials: p}
	a, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(c); !errors.Is(err, fsapi.Stale) {
		t.Fatal("exit callback replaced by another adapter", err)
	}
	wrong := a.Identity()
	wrong.Session = testID(99)
	if err := p.BindMount(wrong); !errors.Is(err, fsapi.Stale) {
		t.Fatal("exit callback accepted foreign credential connection", err)
	}
	if err := p.BindMount(a.Identity()); err != nil {
		t.Fatal("failed replacement corrupted exact binding", err)
	}
}
