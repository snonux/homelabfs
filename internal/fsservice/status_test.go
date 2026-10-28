package fsservice

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type waitingProtocol struct {
	contractProtocol
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *waitingProtocol) Protect(ctx context.Context, proposal Proposal) ([]byte, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return p.contractProtocol.Protect(ctx, proposal)
}

func TestStatusResponsiveWhileWorkerWaitsAndSnapshotsProgress(t *testing.T) {
	f := setup(t, "sim")
	protocol := &waitingProtocol{entered: make(chan struct{}), release: make(chan struct{})}
	f.c.Validation = nil
	f.c.Protocol = protocol
	f.c.Permissions = allowPolicy()
	f.start()
	initial := f.service.Status()
	if initial.State != Serving || !initial.SourcePosition.Valid() || !initial.AppliedPosition.Valid() {
		t.Fatal(initial)
	}
	request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
	if err := f.service.Dispatch(t.Context(), request, fsapi.Command{Mount: f.c.Storage.Volume, Operation: fsapi.Mkdir, Parent: f.c.Storage.Root, Name: "status-progress", Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-protocol.entered:
	case <-time.After(time.Second):
		t.Fatal("protocol not called")
	}
	defer func() {
		select {
		case <-protocol.release:
		default:
			close(protocol.release)
		}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		status := f.service.Status()
		if status.WaitReason == "partner-protection" {
			if status.State != Serving || status.SourcePosition.Sequence <= initial.SourcePosition.Sequence || status.AppliedPosition != initial.AppliedPosition || status.BackingSlots != initial.BackingSlots {
				t.Fatal("fabricated progress while waiting", status, initial)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("status unresponsive", status)
		}
		time.Sleep(time.Millisecond)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			_ = f.service.Status()
		}
	}()
	close(protocol.release)
	result, err := request.Completion().Wait(t.Context())
	if err != nil || result.Code != fsapi.OK {
		t.Fatal(err, result)
	}
	<-done
	final := f.service.Status()
	if final.AppliedPosition.Sequence <= initial.AppliedPosition.Sequence || final.BackingSlots <= initial.BackingSlots || final.WaitReason != "" {
		t.Fatal("progress not published", final)
	}
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	if status := f.service.Status(); status.State != Blocked || status.WaitReason != "service-closed" {
		t.Fatal(status)
	}
	f.service = nil
}

func TestExternalAdmissionStopSurvivesWorkerPublication(t *testing.T) {
	f := setup(t, "sim")
	protocol := &waitingProtocol{entered: make(chan struct{}), release: make(chan struct{})}
	f.c.Validation = nil
	f.c.Protocol = protocol
	f.c.Permissions = allowPolicy()
	f.start()
	first := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
	if err := f.service.Dispatch(t.Context(), first, fsapi.Command{Mount: f.c.Storage.Volume, Operation: fsapi.Mkdir, Parent: f.c.Storage.Root, Name: "stopped", Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-protocol.entered:
	case <-time.After(time.Second):
		t.Fatal("worker not waiting")
	}
	queued := []*fsapi.Request{}
	for _, operation := range []fsapi.Operation{fsapi.Statfs, fsapi.Mkdir, fsapi.WaitLock} {
		request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
		if err := f.service.Dispatch(t.Context(), request, fsapi.Command{Mount: f.c.Storage.Volume, Operation: operation}); err != nil {
			t.Fatal(err)
		}
		queued = append(queued, request)
	}
	f.service.BlockAdmission("native-mount-uncertain")
	// A later worker serving publication cannot override an external permanent gate.
	f.service.state.Store(uint32(Serving))
	f.service.status.Store(&Status{State: Serving})
	if f.service.State() != Blocked || f.service.Status().WaitReason != "native-mount-uncertain" {
		t.Fatal(f.service.Status())
	}
	close(protocol.release)
	result, err := first.Completion().Wait(t.Context())
	if err != nil || result.Code == fsapi.OK {
		t.Fatal(result, err)
	}
	for _, request := range queued {
		result, err := request.Completion().Wait(t.Context())
		if err != nil || result.Code != fsapi.Unavailable {
			t.Fatal("queued workload escaped stop", result, err)
		}
	}
	if f.service.Status().State != Blocked || f.service.Status().WaitReason != "native-mount-uncertain" {
		t.Fatal(f.service.Status())
	}
}
