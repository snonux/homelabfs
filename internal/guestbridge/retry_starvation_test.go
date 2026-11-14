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
	return localViews{}.Exchange(ctx, scope, op, raw)
}

// A permanently-WouldBlock control cell whose every retry takes longer than
// the retry tick must not starve the control lane (h84). Before h84 the
// worker gave a due retry priority over its queue on every iteration; a retry
// that outlasted the 10 ms tick left the next tick already due, so the same
// cell ran again and again and freshly queued control cells (another mount's
// VIEW_WAIT/VIEW_QUERY in the 984 workload model) were never answered. Each
// fresh cell here must be answered while the stalled cell keeps retrying.
func TestRetryingControlCellDoesNotStarveQueuedCells(t *testing.T) {
	f := newBridge(t)
	views := &stallingViews{stall: 25 * time.Millisecond}
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
	for cell := uint64(3); cell < 8; cell++ {
		start := time.Now()
		poll(cell, 0)
		_ = fetchNativeAt(t, tr, f.native, cell, f.native.Endpoint.RecallNode)
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("queued control cell %d answered after %v behind a retrying cell", cell, elapsed)
		}
	}
	waitCellPhase(t, tr, 2, "pending") // the stalled cell is still retried, not dropped
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
	deadline := time.Now().Add(3 * time.Second)
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
