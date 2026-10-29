package sim

import (
	"context"
	"errors"
	"github.com/snonux/homelabfs/internal/failpoint"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/transport"
	"runtime"
	"testing"
	"time"
)

func TestPauseRejectsReplacedCheckpoint(t *testing.T) {
	r := newRig(t, 991, 1, nil)
	n := r.nodes[0]
	f, err := n.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	passed := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- n.Call(r.ctx, "checkpoint then unrelated work", func(ctx context.Context, rt *Runtime) error {
			if err := rt.Hooks.Hit(ctx, failpoint.JournalAppend); err != nil {
				return err
			}
			close(passed)
			<-finish
			return nil
		})
	}()
	<-f.Triggered()
	_, err = n.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.ReturnError, Err: errors.New("replacement")})
	if err != nil {
		t.Fatal(err)
	}
	<-passed
	pauseErr := n.PauseAt(f)
	if pauseErr == nil {
		if err = n.Resume(); err != nil {
			t.Fatal(err)
		}
	}
	close(finish)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if pauseErr == nil {
		t.Fatal("PauseAt accepted a replaced activation after the call had left its checkpoint")
	}
}
func TestPauseRejectsCanceledCheckpointFromPreviousCall(t *testing.T) {
	r := newRig(t, 992, 1, nil)
	n := r.nodes[0]
	f, err := n.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	inner, cancel := context.WithCancel(r.ctx)
	done := make(chan error, 1)
	go func() {
		done <- n.Call(r.ctx, "canceled old checkpoint", func(_ context.Context, rt *Runtime) error {
			_ = rt.Hooks.Hit(inner, failpoint.JournalAppend)
			return nil
		})
	}()
	<-f.Triggered()
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	finish := make(chan struct{})
	go func() {
		done <- n.Call(r.ctx, "unrelated new call", func(context.Context, *Runtime) error { close(entered); <-finish; return nil })
	}()
	<-entered
	pauseErr := n.PauseAt(f)
	if pauseErr == nil {
		if err = n.Resume(); err != nil {
			t.Fatal(err)
		}
	}
	close(finish)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if pauseErr == nil {
		t.Fatal("PauseAt accepted a canceled checkpoint belonging to a previous call")
	}
}

func TestAwaitReadRejectsSignaledOldRead(t *testing.T) {
	r := newRig(t, 993, 2, nil)
	a, b := r.connect(t, 0, 1)
	if err := r.network.AwaitRead(r.ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	sent := make(chan error, 1)
	go func() { sent <- a.Send(r.ctx, transport.Ping, fsapi.UUID{}, UUID(993, "ping"), []byte("message")) }()
	if err := r.s.Step(r.ctx, "conn/1/0/write/3"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(r.ctx, 50*time.Millisecond)
	defer cancel()
	err := r.network.AwaitRead(ctx, 1, 1)
	c := r.network.connections[0].ends[1]
	c.mu.Lock()
	buffered := len(c.buffer)
	c.mu.Unlock()
	runtime.GOMAXPROCS(previous)
	if _, e := b.Receive(r.ctx); e != nil {
		t.Fatal(e)
	}
	if e := <-sent; e != nil {
		t.Fatal(e)
	}
	if err == nil && buffered > 0 {
		t.Fatalf("AwaitRead returned for the old signaled wait with %d bytes already readable", buffered)
	}
	if err = r.network.AwaitRead(r.ctx, 1, 1); err != nil {
		t.Fatal("fresh read boundary missing", err)
	}
	c.mu.Lock()
	valid := c.waiting && len(c.buffer) == 0
	c.mu.Unlock()
	if !valid {
		t.Fatal("fresh wait has readable bytes")
	}
}

func TestCancellationCannotEscapeHeldPauseCheckpoint(t *testing.T) {
	r := newRig(t, 994, 1, nil)
	node := r.nodes[0]
	checkpoint, err := node.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	inner, cancel := context.WithCancel(r.ctx)
	defer cancel()
	returned := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- node.Call(r.ctx, "cancel held checkpoint", func(_ context.Context, runtime *Runtime) error {
			returned <- runtime.Hooks.Hit(inner, failpoint.JournalAppend)
			return nil
		})
	}()
	select {
	case <-checkpoint.Triggered():
	case <-r.ctx.Done():
		t.Fatal(r.ctx.Err())
	}
	if err = node.PauseAt(checkpoint); err != nil {
		t.Fatal(err)
	}
	cancel()
	node.mu.Lock()
	hold := node.hold
	node.mu.Unlock()
	select {
	case <-hold.Interrupted():
	case <-r.ctx.Done():
		t.Fatal("held cancellation was not observed")
	}
	if err = r.s.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-returned:
		t.Fatalf("cancellation escaped paused checkpoint: %v", e)
	default:
	}
	if err = node.Resume(); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-returned:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-r.ctx.Done():
		t.Fatal(r.ctx.Err())
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = node.PauseAt(checkpoint); err == nil {
		t.Fatal("completed call authorized another pause")
	}
}

func TestCrashReleasesHeldCheckpointAfterStorageFence(t *testing.T) {
	r := newRig(t, 995, 1, nil)
	node := r.nodes[0]
	checkpoint, err := node.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(r.ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- node.Call(r.ctx, "crash held checkpoint", func(_ context.Context, runtime *Runtime) error {
			_ = runtime.Hooks.Hit(ctx, failpoint.JournalAppend)
			_, e := runtime.Disk("forbidden-after-crash")
			if e == nil {
				return errors.New("held worker escaped storage fence")
			}
			return nil
		})
	}()
	select {
	case <-checkpoint.Triggered():
	case <-r.ctx.Done():
		t.Fatal(r.ctx.Err())
	}
	if err = node.PauseAt(checkpoint); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err = node.Crash(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-r.ctx.Done():
		t.Fatal("crash did not release held checkpoint")
	}
}
