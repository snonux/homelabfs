//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"math"
	"os"
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
	case <-time.After(answerBound):
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
	s := newExitLockScene(t)
	pending := s.holdPendingRequests(t)
	// The exit must be the signal's: observe it before stdin closes (sd4).
	signalFixtureExit(t, s.departing, syscall.SIGTERM)
	err := stopFixtureChild(s.departing)
	if !signalExit(err) {
		t.Fatal("expected observed signal exit", err)
	}
	if again := s.departing.process.Wait(); again != err || pending.exits.Load() != 1 {
		t.Fatal("exit notification not exactly once", again, pending.exits.Load())
	}
	if code := awaitExitStatus(t, pending.waitDone); code != fuse.EINTR {
		t.Fatal("exit did not cancel pending lock", code)
	}
	pending.release()
	if code := awaitExitStatus(t, pending.lateDone); code != status(fsapi.Stale) {
		t.Fatal("preauthenticated request acquired after exit", code)
	}
	parent := s.assertRangesRetired(t)
	s.assertDescriptionOutlivesExit(t, parent)
	if err := s.a.Err(); err != nil {
		t.Fatal(err)
	}
}

// exitLockScene is a departing and a surviving helper, each with its own
// open description and POSIX range on one file; the departing helper also
// holds a whole-file flock through its description.
type exitLockScene struct {
	a                   *Adapter
	departing, survivor *fixtureChild
	created             fuse.CreateOut
	dead, live          fuse.LkIn
}

func newExitLockScene(t *testing.T) *exitLockScene {
	t.Helper()
	p, a := exitFixture(t)
	s := &exitLockScene{a: a, departing: startFixtureChild(t, p, nil), survivor: startFixtureChild(t, p, nil)}
	s.departing.call(t, fixtureCommand{Op: "ready"})
	s.survivor.call(t, fixtureCommand{Op: "ready"})
	if code := a.Create(nil, &fuse.CreateIn{InHeader: participantHeader(1, os.Getpid()), Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0666}, "exit-locks", &s.created); code != fuse.OK {
		t.Fatal(code)
	}
	s.dead = s.openRange(t, s.departing.process.PID(), 0, 9)
	s.live = s.openRange(t, s.survivor.process.PID(), 20, 29)
	forged := s.dead
	forged.Uid++
	forged.Lk.Typ = syscall.F_UNLCK
	if code := a.SetLk(nil, &forged); code != fuse.EACCES {
		t.Fatal("wrong credentials could clear another owner's range", code)
	}
	flock := s.dead
	flock.LkFlags = fuse.FUSE_LK_FLOCK
	flock.Lk.End = math.MaxInt64
	if code := a.SetLk(nil, &flock); code != fuse.OK {
		t.Fatal(code)
	}
	return s
}

// openRange opens a description for pid and write-locks [start, end] on it.
func (s *exitLockScene) openRange(t *testing.T, pid int, start, end uint64) fuse.LkIn {
	t.Helper()
	h := participantHeader(s.created.NodeId, pid)
	var out fuse.OpenOut
	if code := s.a.Open(nil, &fuse.OpenIn{InHeader: h, Flags: syscall.O_RDWR}, &out); code != fuse.OK {
		t.Fatal(code)
	}
	lock := fuse.LkIn{InHeader: h, Fh: out.Fh, Owner: uint64(h.Pid), Lk: fuse.FileLock{Start: start, End: end, Typ: syscall.F_WRLCK}}
	if code := s.a.SetLk(nil, &lock); code != fuse.OK {
		t.Fatal(code)
	}
	return lock
}

// exitPending is the departing helper's two in-flight requests: a SetLkw
// queued behind the survivor's range and a SetLk held after authentication.
type exitPending struct {
	waitDone, lateDone chan fuse.Status
	release            func()
	exits              *atomic.Int32
}

// holdPendingRequests admits the departing helper's waiter and parks a
// preauthenticated SetLk in front of the service until release.
func (s *exitLockScene) holdPendingRequests(t *testing.T) exitPending {
	t.Helper()
	service := s.a.service
	waitAdmitted, lateAdmitted, releaseLate := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	p := exitPending{waitDone: make(chan fuse.Status, 1), lateDone: make(chan fuse.Status, 1), exits: new(atomic.Int32)}
	p.release = func() { releaseOnce.Do(func() { close(releaseLate) }) }
	t.Cleanup(p.release)
	s.a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		if c.Operation == fsapi.TryLock && c.Lock.Start == 40 {
			close(lateAdmitted)
			<-releaseLate
		}
		if c.Operation == fsapi.ProcessExit {
			p.exits.Add(1)
		}
		err := service.Dispatch(ctx, r, c)
		if err == nil && c.Operation == fsapi.WaitLock {
			close(waitAdmitted)
		}
		return err
	})
	wait := s.dead
	wait.Lk = s.live.Lk
	go func() { p.waitDone <- s.a.SetLkw(nil, &wait) }()
	awaitSignal(t, waitAdmitted, "wait not admitted")
	late := s.dead
	late.Lk.Start, late.Lk.End = 40, 49
	go func() { p.lateDone <- s.a.SetLk(nil, &late) }()
	awaitSignal(t, lateAdmitted, "late request not authenticated")
	return p
}

// awaitSignal waits answerBound for ch to close and fails with what.
func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(answerBound):
		t.Fatalf("%s within %v", what, answerBound)
	}
}

// assertRangesRetired checks the exit removed the dead process's range and
// pending waiter but left the survivor's range. It returns the parent's lock
// over the dead range, acquired on the parent's own description.
func (s *exitLockScene) assertRangesRetired(t *testing.T) fuse.LkIn {
	t.Helper()
	if code := s.a.SetLk(nil, &s.dead); code != fuse.EACCES {
		t.Fatal("exited identity admitted", code)
	}
	parent := s.dead
	parent.InHeader = participantHeader(s.created.NodeId, os.Getpid())
	parent.Owner, parent.Fh = uint64(os.Getpid()), s.created.Fh
	if code := s.a.SetLk(nil, &parent); code != fuse.OK {
		t.Fatal("dead process range survived positive Wait", code)
	}
	query := parent
	query.Lk = s.live.Lk
	var out fuse.LkOut
	if code := s.a.GetLk(nil, &query, &out); code != fuse.OK || out.Lk.Pid != s.live.Pid {
		t.Fatal("surviving process lock erased", out, code)
	}
	liveUnlock := s.live
	liveUnlock.Lk.Typ = syscall.F_UNLCK
	if code := s.a.SetLk(nil, &liveUnlock); code != fuse.OK {
		t.Fatal(code)
	}
	if code := s.a.SetLk(nil, &query); code != fuse.OK {
		t.Fatal("dead pending waiter granted after survivor unlock", code)
	}
	return parent
}

// assertDescriptionOutlivesExit checks the dead helper's description, flock
// and issued grant survive exit until the kernel's final RELEASE, and that
// the late RELEASE does not erase another process's lock.
func (s *exitLockScene) assertDescriptionOutlivesExit(t *testing.T, parent fuse.LkIn) {
	t.Helper()
	parentFlock := parent
	parentFlock.LkFlags, parentFlock.Lk.End = fuse.FUSE_LK_FLOCK, math.MaxInt64
	if code := s.a.SetLk(nil, &parentFlock); code != fuse.EAGAIN {
		t.Fatal("exit closed retained description/flock", code)
	}
	d, ok := s.a.description(s.dead.Fh, s.created.NodeId, false)
	if !ok {
		t.Fatal("exit discarded description")
	}
	if _, code := s.a.callAuthorized(context.Background(), d.grant, fsapi.Command{Operation: fsapi.Write, Object: d.object, Handle: d.handle, Data: []byte("x")}); code != fuse.OK {
		t.Fatal("exit revoked issued write grant", code)
	}
	s.a.Release(nil, &fuse.ReleaseIn{InHeader: fuse.InHeader{NodeId: s.created.NodeId}, Fh: s.dead.Fh})
	if code := s.a.SetLk(nil, &parentFlock); code != fuse.OK {
		t.Fatal("final description drain retained flock", code)
	}
	contender := s.live
	contender.Lk = parent.Lk
	if code := s.a.SetLk(nil, &contender); code != fuse.EAGAIN {
		t.Fatal("late release erased another process lock", code)
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
	case <-time.After(answerBound):
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
	case <-time.After(answerBound):
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
