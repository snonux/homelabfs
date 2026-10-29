package sim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/failpoint"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/transport"
)

type localService struct {
	config     fsservice.Config
	service    *fsservice.Service
	historical []byte
	starts     int
}

func (f *localService) factory(ctx context.Context, r *Runtime) (io.Closer, error) {
	f.starts++
	var err error
	if f.config.Source, err = r.Disk("source"); err != nil {
		return nil, err
	}
	if f.config.Reservations, err = r.Disk("reservations"); err != nil {
		return nil, err
	}
	source := f.config.Source
	if f.starts == 1 {
		if err = f.initialize(ctx, r, source); err != nil {
			return nil, err
		}
	}
	f.config.Clock = r.Clock
	f.config.Random = r.Random
	f.config.Storage.Random = NewRandom(r.node.cfg.Seed, fmt.Sprintf("storage/%d", f.starts))
	f.config.Workspace.ID = UUID(r.node.cfg.Seed, fmt.Sprintf("workspace/%d", f.starts))
	roots := []*storage.Filesystem{&f.config.Workspace.Data, &f.config.Workspace.Journal, &f.config.Workspace.Progress}
	for i, root := range roots {
		*root, err = r.Disk(fmt.Sprintf("workspace/%d/%d", f.starts, i))
		if err != nil {
			return nil, err
		}
	}
	// The existing journal boundary feeds the real failpoint Registry; no mock
	// filesystem command or replacement journal implementation is involved.
	f.config.Segment.Failpoint = func(boundary string) error {
		if boundary == "before-append" {
			return r.Hooks.Hit(ctx, failpoint.JournalAppend)
		}
		return nil
	}
	f.service, err = fsservice.New(ctx, f.config)
	if err != nil {
		return nil, err
	}
	return f.service, nil
}
func (f *localService) initialize(ctx context.Context, r *Runtime, source storage.Filesystem) error {
	seed := r.node.cfg.Seed
	volume, generation, configuration := UUID(seed, "volume"), UUID(seed, "generation"), UUID(seed, "configuration")
	root := fsapi.ObjectID{Home: volume, Allocation: UUID(seed, "root")}
	members := []fsapi.UUID{r.Identity.Node, UUID(seed, "fixture-partner"), UUID(seed, "fixture-async")}
	slices.SortFunc(members, func(a, b fsapi.UUID) int { return bytes.Compare(a[:], b[:]) })
	f.config.Stream = segment.Stream{Volume: volume, Generation: generation, Configuration: configuration, Epoch: 1, Kind: 1}
	f.config.Storage = storage.Config{Volume: volume, Root: root}
	// This explicit disposable authority labels the original local source; it
	// never claims the current transport incarnation or distributed readiness.
	f.config.Authority = record.Authority{Volume: volume, Generation: generation, Configuration: configuration, Epoch: 1, Owner: r.Identity.Node, Partner: UUID(seed, "fixture-partner"), Members: members, CoordinatorTerm: 1, OwnerIncarnation: r.Identity.Incarnation, PartnerIncarnation: UUID(seed, "fixture-partner-incarnation")}
	f.config.Validation = &fsservice.ValidationProfile{Disposable: true, Name: "deterministic simulator local service only"}
	f.config.Permissions, _ = fsservice.NewUnixPermissions(UUID(seed, "permission-view"))
	f.config.Segment.MaximumBytes = 1 << 20
	dir := "."
	for _, part := range strings.Split(f.config.Stream.Directory(), "/") {
		next := path.Join(dir, part)
		if err := source.Mkdir(ctx, next, 0o700); err != nil {
			return err
		}
		if err := source.SyncDir(ctx, dir); err != nil {
			return err
		}
		dir = next
	}
	h := record.Header{Volume: volume, Generation: generation, Configuration: configuration, Epoch: 1, Operation: UUID(seed, "genesis-structural"), Incarnation: r.Identity.Incarnation, Observed: r.Clock.Now()}
	w, err := segment.NewWriter(ctx, source, f.config.Stream, segment.Options{MaximumBytes: 1 << 20, Structural: func(uint64) record.Header { return h }, NewSegment: func() fsapi.UUID { return UUID(seed, "genesis-segment") }})
	if err != nil {
		return err
	}
	h.Operation = UUID(seed, "genesis-operation")
	init := record.VolumeInit{Root: root, Initial: record.Metadata{Revision: 1, Type: 2, UID: 1000, GID: 1000, Mode: 0o775, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: root}, RootLedger: record.LedgerEdit{Object: root, ResultingRevision: 1}, Authority: f.config.Authority, Authorization: record.Artifact{Kind: 6, ID: UUID(seed, "disposable-genesis-evidence"), Length: 1}}
	receipt, err := w.Append(ctx, record.Record{Header: h, Payload: init})
	if err != nil {
		return errors.Join(err, w.Close())
	}
	err = receipt.Wait(ctx)
	err = errors.Join(err, w.Close())
	if err != nil {
		return err
	}
	f.config.Selection = apply.Selection{Through: receipt.Reference, Effects: []record.FrameRef{receipt.Reference}, Proof: []byte("explicit local validation fixture")}
	reader, err := segment.OpenReader(ctx, source, f.config.Stream, segment.RecoveryOptions{}, record.Position{Volume: volume, Generation: generation, Configuration: configuration, Epoch: 1, Sequence: 1})
	if err != nil {
		return err
	}
	for {
		entry, e := reader.Next(ctx)
		if e != nil {
			return errors.Join(e, reader.Close())
		}
		if _, ok := entry.Record.Payload.(record.VolumeInit); ok {
			f.historical = entry.Bytes
			break
		}
	}
	return reader.Close()
}
func (f *localService) command(ctx context.Context, c fsapi.Command) (fsapi.Result, error) {
	c.Mount = f.config.Storage.Volume
	if c.Handle != 0 {
		c.Generation = f.config.Workspace.ID
	}
	request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1000})
	if err := f.service.Dispatch(ctx, request, c); err != nil {
		return fsapi.Result{}, err
	}
	result, err := request.Completion().Wait(ctx)
	if err != nil {
		return result, err
	}
	if result.Code != fsapi.OK {
		return result, result.Code
	}
	return result, nil
}

func TestRealSingleNodeServiceCrashReplayAndFailpoint(t *testing.T) {
	f := &localService{}
	r := newRig(t, 31, 1, f.factory)
	node := r.nodes[0]
	var handle fsapi.OpenHandle
	root := f.config.Storage.Root
	if err := node.Call(r.ctx, "create and write", func(ctx context.Context, _ *Runtime) error {
		result, err := f.command(ctx, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "file", Mode: 0o640, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
		if err != nil {
			return err
		}
		handle = result.Handle
		_, err = f.command(ctx, fsapi.Command{Operation: fsapi.Write, Handle: handle, Data: []byte("durable core journal")})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	oldSource := f.config.Source
	oldIncarnation := node.runtime.Identity.Incarnation
	if err := node.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := node.Call(r.ctx, "paused command", func(context.Context, *Runtime) error { t.Fatal("paused actor executed"); return nil }); err == nil {
		t.Fatal("paused call admitted")
	}
	if err := r.s.Advance(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := node.Resume(); err != nil {
		t.Fatal(err)
	}
	if err := node.Crash(); err != nil {
		t.Fatal(err)
	}
	if _, err := oldSource.Open(r.ctx, "new-after-crash", os.O_CREATE|os.O_WRONLY, 0o600); !errors.Is(err, fs.ErrClosed) {
		t.Fatal("old lifetime root survived crash", err)
	}
	if err := node.Restart(r.ctx); err != nil {
		t.Fatal(err)
	}
	if node.runtime.Identity.Incarnation == oldIncarnation {
		t.Fatal("incarnation reused")
	}
	if f.config.Authority.OwnerIncarnation != oldIncarnation {
		t.Fatal("historical source authority rewritten")
	}
	if err := node.Call(r.ctx, "replay and read", func(ctx context.Context, _ *Runtime) error {
		lookup, err := f.command(ctx, fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "file"})
		if err != nil {
			return err
		}
		opened, err := f.command(ctx, fsapi.Command{Operation: fsapi.Open, Object: lookup.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
		if err != nil {
			return err
		}
		data, err := f.command(ctx, fsapi.Command{Operation: fsapi.Read, Handle: opened.Handle, Size: 128})
		if err != nil {
			return err
		}
		if string(data.Data) != "durable core journal" {
			return fmt.Errorf("lost acknowledged local data: %q", data.Data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Stop before real journal append: the fault must reach the actual service,
	// return a failure and leave it blocked rather than invent a success.
	activation, err := node.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.ReturnError, Nth: 1, Err: errors.New("injected append fault")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.command(r.ctx, fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "fault", Mode: 0o700})
	if err == nil {
		t.Fatal("real journal failpoint did not fail command")
	}
	if activation.Hits() != 1 {
		t.Fatal("core hook not reached")
	}
	// Crash drains poisoned workers without trying to gracefully repair a source.
	if err = node.Crash(); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalACKSurvivesUnobservedPeerRAMLoss(t *testing.T) {
	for _, crashBeforeDelivery := range []bool{true, false} {
		t.Run(fmt.Sprint(crashBeforeDelivery), func(t *testing.T) {
			fixtures := make(map[fsapi.UUID]*localService)
			factory := func(ctx context.Context, runtime *Runtime) (io.Closer, error) {
				f := fixtures[runtime.Identity.Node]
				if f == nil {
					f = &localService{}
					fixtures[runtime.Identity.Node] = f
				}
				return f.factory(ctx, runtime)
			}
			r := newRig(t, 44, 2, factory)
			a, b := r.connect(t, 0, 1)
			peer := r.nodes[1]
			historical := append([]byte(nil), fixtures[peer.cfg.Node].historical...)
			oldID := peer.runtime.Identity.Incarnation
			sent := make(chan error, 1)
			go func() {
				sent <- b.Send(r.ctx, transport.Receipt, UUID(44, "binding"), UUID(44, "receipt-correlation"), historical)
			}()
			key := "conn/1/1/write/4"
			if err := r.s.Await(r.ctx, key); err != nil {
				t.Fatal(err)
			}
			if crashBeforeDelivery {
				if err := peer.Crash(); err != nil {
					t.Fatal(err)
				}
			}
			deliver(t, r, key)
			if !crashBeforeDelivery {
				if err := <-sent; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := <-sent; err == nil {
					t.Fatal("crashed pending sender unexpectedly completed")
				}
			}
			frame, err := a.Receive(r.ctx)
			if err != nil {
				t.Fatal("local crash synchronously purged peer's old ACK", err)
			}
			if frame.SourceIncarnation != oldID || !bytes.Equal(frame.Body, historical) {
				t.Fatal("historical envelope rewritten")
			}
			decoded, err := record.Decode(frame.Body)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Header.Incarnation != oldID {
				t.Fatal("journal creator replaced by current transport identity")
			}
			if !crashBeforeDelivery {
				if err = peer.Crash(); err != nil {
					t.Fatal(err)
				}
			}
			// This is an oracle boundary, not a production client success decision. A
			// receipt is historical evidence despite peer RAM loss before client reply.
			if err = r.s.Do("oracle/client-reply-boundary", "peer RAM lost; historical receipt retained; no current-memory guarantee", func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if err = peer.Restart(r.ctx); err != nil {
				t.Fatal(err)
			}
			if peer.runtime.Identity.Incarnation == oldID {
				t.Fatal("restart retained incarnation")
			}
			if err = r.s.Step(r.ctx, "conn/1/1/reset"); err != nil {
				t.Fatal(err)
			}
			if _, err = a.Receive(r.ctx); err == nil {
				t.Fatal("remote reset not observed")
			}
		})
	}
}

func TestPendingCoreOperationPausesOnlyAtExplicitCheckpoint(t *testing.T) {
	f := &localService{}
	r := newRig(t, 91, 1, f.factory)
	node := r.nodes[0]
	checkpoint, err := node.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.Block, Nth: 1})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- node.Call(r.ctx, "blocked mkdir", func(ctx context.Context, _ *Runtime) error {
			_, e := f.command(ctx, fsapi.Command{Operation: fsapi.Mkdir, Parent: f.config.Storage.Root, Name: "paused", Mode: 0o700})
			return e
		})
	}()
	select {
	case <-checkpoint.Triggered():
	case <-r.ctx.Done():
		t.Fatal(r.ctx.Err())
	}
	if err = node.Pause(); err == nil {
		t.Fatal("unqualified in-flight pause was accepted")
	}
	if err = node.PauseAt(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err = checkpoint.Release(); err == nil {
		t.Fatal("paused checkpoint released")
	}
	if _, err = node.Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.ReturnError, Err: errors.New("replacement")}); err == nil {
		t.Fatal("paused checkpoint replaced and released")
	}
	if err = r.s.Advance(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		t.Fatalf("operation completed during paused core checkpoint: %v", err)
	default:
	}
	if err = node.Resume(); err != nil {
		t.Fatal(err)
	}
	if err = checkpoint.Release(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-r.ctx.Done():
		t.Fatal(r.ctx.Err())
	}
	if _, err = f.command(r.ctx, fsapi.Command{Operation: fsapi.Lookup, Parent: f.config.Storage.Root, Name: "paused"}); err != nil {
		t.Fatal(err)
	}
}

func TestSparseExtentConsumesStorageBudget(t *testing.T) {
	r := newRig(t, 118, 1, nil)
	node := r.nodes[0]
	err := node.Call(r.ctx, "bounded sparse writes", func(ctx context.Context, runtime *Runtime) error {
		disk, err := runtime.Disk("sparse")
		if err != nil {
			return err
		}
		for i := 0; i < 2; i++ {
			file, err := disk.Open(ctx, fmt.Sprint(i), os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, writeErr := file.WriteAt(ctx, []byte{1}, (4<<20)-1)
			closeErr := file.Close()
			if i == 0 {
				if err = errors.Join(writeErr, closeErr); err != nil {
					return err
				}
			} else if writeErr == nil {
				return fmt.Errorf("sparse gap bypassed extent budget")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
