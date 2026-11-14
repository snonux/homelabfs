package guestbridge

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// stallingViews answers every view exchange EMPTY except a VIEW_POLL that
// names stalledSequence: that one takes longer than the worker's 10 ms retry
// tick and then refuses WouldBlock, for good. It is the shape of the 984
// finding, a held control cell whose every retry outlasts the tick.
type stallingViews struct {
	localViews
	stall   time.Duration
	stalled atomic.Int64
	// fresh receives, for every other VIEW_POLL, how many stalled retries had
	// started when the worker dispatched it. That orders the fresh cell
	// against the retries by the worker's own schedule, not by wall time.
	fresh chan int64
}

const stalledSequence = 7

func (v *stallingViews) Exchange(ctx context.Context, scope Context, op string, raw json.RawMessage) (json.RawMessage, error) {
	var payload struct {
		AfterSequence uint64 `json:"after_sequence"`
	}
	if op == "VIEW_POLL" && json.Unmarshal(raw, &payload) == nil && payload.AfterSequence == stalledSequence {
		v.stalled.Add(1)
		time.Sleep(v.stall)
		return nil, fsapi.WouldBlock
	}
	if op == "VIEW_POLL" && v.fresh != nil {
		v.fresh <- v.stalled.Load()
	}
	return localViews{}.Exchange(ctx, scope, op, raw)
}

// A permanently-WouldBlock control cell whose every retry takes longer than
// the retry tick must not starve the control lane (h84). Before h84 the
// worker gave a due retry priority over its queue on every iteration; a retry
// that outlasted the 10 ms tick left the next tick already due, so the same
// cell ran again and again and freshly queued control cells (another mount's
// VIEW_WAIT/VIEW_QUERY in the 984 workload model) were never answered. Each
// fresh cell here must be answered while the stalled cell keeps retrying.
//
// The bound is counted in retries, not wall time (i84): an earlier 1 s
// elapsed-time bound could fail spuriously when a loaded host delayed the
// worker. The count is sampled after sendNative returns, which is after the
// cell is queued (building and writing the command can take a slow host long
// enough for several retries, which must not count). From then on at most one
// retry, the one already running, can precede the queued cell: after a retry
// the h84 schedule takes a waiting queued cell first. A cell dispatched before
// the sample counts as zero ahead. The pre-h84 worker never dispatched the
// fresh cell at all.
func TestRetryingControlCellDoesNotStarveQueuedCells(t *testing.T) {
	f := newBridge(t)
	views := &stallingViews{stall: 25 * time.Millisecond, fresh: make(chan int64, 8)}
	config := f.s.config
	config.Views = views
	service, err := New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	f.s = service
	f.hello(t)
	f.next = 0
	tr, err := NewTransport(f.s, transportBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Close()
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 1)
	domain := grantedDomain(t, f)
	poll := func(cell, after uint64) {
		c := f.command(t, "VIEW_POLL", map[string]any{"domain": domain, "after_sequence": after})
		c.Cell = cell
		sendNative(t, tr, f.native, c, 1) // VIEW_POLL travels the control lane
	}
	poll(2, stalledSequence)
	waitStalls(t, views, 2) // the cell is in the retry ring and retrying
	stalls := views.stalled.Load()
	for cell := uint64(3); cell < 8; cell++ {
		requireDispatchedNext(t, views, cell, func() { poll(cell, 0) })
		_ = fetchNativeAt(t, tr, f.native, cell, f.native.Endpoint.RecallNode)
	}
	// The stalled cell is still retried, not dropped or blocked. Its phase is
	// not sampled for "pending": with the queue empty it retries back to back,
	// so it is "pending" only between recordExecuteFailure and beginJob.
	waitStalls(t, views, stalls+1)
	tr.mu.Lock()
	c := tr.cells[2]
	retried := c != nil && c.response == nil && (c.jobPhase == "pending" || c.jobPhase == "executing")
	tr.mu.Unlock()
	if !retried {
		t.Fatal("stalled cell left the retry ring")
	}
}

// requireDispatchedNext queues a fresh control cell with send and requires
// the worker to dispatch it with at most one stalled retry ahead of it (see
// TestRetryingControlCellDoesNotStarveQueuedCells for why one).
func requireDispatchedNext(t *testing.T, views *stallingViews, cell uint64, send func()) {
	t.Helper()
	send()
	before := views.stalled.Load() // after the enqueue; see the test's comment
	var dispatched int64
	select {
	case dispatched = <-views.fresh:
	case <-time.After(answerBound):
		t.Fatalf("queued control cell %d never dispatched behind a retrying cell (%d retries)", cell, views.stalled.Load()-before)
	}
	if ahead := dispatched - before; ahead > 1 {
		t.Fatalf("queued control cell %d dispatched after %d retries of the stalled cell, want at most 1", cell, ahead)
	}
}

// grantedDomain returns the DOMAIN capability ROOT grants this session, so a
// VIEW_POLL naming it passes dispatch and reaches the views exchange.
func grantedDomain(t *testing.T, f *bridgeFixture) Capability {
	t.Helper()
	_, root := f.call(t, f.cred(), "ROOT", map[string]any{})
	var domain Capability
	raw, _ := json.Marshal(root["capabilities"].([]any)[1])
	if err := json.Unmarshal(raw, &domain); err != nil {
		t.Fatal(err)
	}
	return domain
}

func waitStalls(t *testing.T, v *stallingViews, n int64) {
	t.Helper()
	deadline := time.Now().Add(answerBound)
	for v.stalled.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("stalled cell executed %d times, want %d", v.stalled.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// The ring's schedule, driven by a hand-fed tick: with a tick always due and
// a refused cell retried forever, queued cells and retries alternate, so
// neither the queue nor the ring is starved; the ring stays FIFO; and a
// closed queue ends the worker even with retries pending.
func TestRetryRingAlternatesDueRetriesWithQueuedCells(t *testing.T) {
	tick := make(chan time.Time, 1)
	due := func() {
		select {
		case tick <- time.Time{}:
		default:
		}
	}
	queue := make(chan job, 4)
	ring := newRetryRing(tick)
	ring.push(job{id: 1})
	ring.push(job{id: 2})
	for _, id := range []uint64{10, 11, 12} {
		queue <- job{id: id}
	}
	var order []uint64
	for range 7 {
		due()
		j, ok := ring.next(queue)
		if !ok {
			t.Fatal("open queue reported closed")
		}
		order = append(order, j.id)
		if j.id < 10 {
			ring.push(j) // permanently refused
		}
	}
	want := []uint64{1, 10, 2, 11, 1, 12, 2}
	if !equalWire(order, want) {
		t.Fatalf("schedule %v, want %v", order, want)
	}
	close(queue)
	if _, ok := ring.next(queue); ok {
		t.Fatal("closed queue did not stop the worker")
	}
}
