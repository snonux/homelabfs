package guestbridge

import (
	"context"
	"time"
)

// retryTick is how often a lane worker may take one cell from its retry ring.
const retryTick = 10 * time.Millisecond

// retryRing is one lane worker's bounded FIFO of accepted cells whose dispatch
// answered a transient WouldBlock/NoSpace, and the schedule that interleaves
// them with the lane's queue. It retains only already funded accepted jobs:
// each cell has one dispatch, so aggregate pending storage is bounded by the
// 80 cell credits and independent replacement workspace, and no retry
// allocates a claim or credit.
//
// Scheduling (h84). A due retry gets priority over a continuously replenished
// queue, so a busy lane still retries its refused cells. But right after a
// retry ran, a queued cell (if any) runs before the next retry. The two
// therefore alternate under contention. Before h84 a due retry always won: a
// retry that took longer than the tick left the next tick already due, so a
// permanently refused control cell (a held VIEW_RELEASE/VIEW_QUERY) ran back
// to back and the lane never answered the other mounts' fresh VIEW_WAIT or
// VIEW_QUERY cells (984 workload model). Every lane runs this same worker, so
// the rule holds for each lane that has a retry ring.
//
// The ring itself stays FIFO, so one refused cell cannot starve its ring
// peers either. Neither the queue nor the ring promised FIFO across the two:
// a retried cell already ran after later-queued cells before h84.
type retryRing struct {
	pending   []job
	tick      <-chan time.Time
	lastRetry bool // the previous job came from the ring
}

func newRetryRing(tick <-chan time.Time) *retryRing {
	return &retryRing{pending: make([]job, 0, 81), tick: tick}
}

// push puts a refused job at the tail of the ring.
func (r *retryRing) push(j job) { r.pending = append(r.pending, j) }

// next returns the lane's next job, blocking until a queued job arrives or a
// tick finds a pending retry. ok is false once the queue is closed: Close
// stops local retry work without draining the ring, as before h84.
func (r *retryRing) next(queue <-chan job) (j job, ok bool) {
	if r.lastRetry {
		// A retry just ran: a waiting queued cell goes first (h84).
		select {
		case j, ok = <-queue:
			r.lastRetry = false
			return j, ok
		default:
		}
	}
	if len(r.pending) > 0 {
		select {
		case <-r.tick:
			return r.pop(), true
		default:
		}
	}
	for {
		select {
		case j, ok = <-queue:
			r.lastRetry = false
			return j, ok
		case <-r.tick:
			if len(r.pending) > 0 {
				return r.pop(), true
			}
		}
	}
}

// pop removes and returns the ring's head and records that a retry runs.
func (r *retryRing) pop() job {
	j := r.pending[0]
	copy(r.pending, r.pending[1:])
	r.pending[len(r.pending)-1] = job{}
	r.pending = r.pending[:len(r.pending)-1]
	r.lastRetry = true
	return j
}

// worker serves one lane: it runs each queued or retried cell's single
// dispatch and puts a transiently refused one back into the lane's ring.
func (t *Transport) worker(queue <-chan job) {
	defer t.workers.Done()
	ticker := time.NewTicker(retryTick)
	defer ticker.Stop()
	ring := newRetryRing(ticker.C)
	for {
		j, ok := ring.next(queue)
		if !ok {
			return
		}
		if t.runJob(j) {
			ring.push(j)
		}
	}
}

// runJob dispatches one accepted cell and protects its response. It reports
// whether the cell goes back into the retry ring (a transient refusal).
func (t *Transport) runJob(j job) (retry bool) {
	invocation, active := t.beginJob(j.id)
	if !active {
		return false
	}
	executionContext := context.WithValue(context.Background(), mappingWorkerKey{}, mappingWorker{transport: t, cell: j.id, invocation: invocation})
	raw, e := t.service.Execute(executionContext, j.native, j.command)
	t.mu.Lock()
	invocation.live = false
	if c := t.cells[j.id]; c != nil && c.invocation == invocation {
		c.invocation = nil
	}
	t.mu.Unlock()
	if e != nil {
		return t.recordExecuteFailure(j.id, e)
	}
	if t.respondFailpoint != nil {
		raw = t.respondFailpoint(j.command, raw)
	}
	// A response that cannot be framed must not leave the cell "executing"
	// forever: it becomes blocked with a class and reads answer an error.
	if len(raw) == 0 || len(raw) > BodyBytes {
		t.recordResponseFailure(j.id, responseBoundsClass(raw))
		return false
	}
	kind := uint16(2)
	if j.command.Op == "VIEW_POLL" {
		kind = 3
	}
	frames, seen, unread, valid := t.responseFrames(j, kind, raw)
	if !valid {
		t.recordResponseFailure(j.id, "FRAME_ENCODE")
		return false
	}
	t.publishResponse(j.id, raw, frames, seen, unread, kind)
	return false
}

// beginJob marks a still-live cell "executing" under a fresh invocation and
// protects that phase before dispatch. A closed or failed transport, or a
// released or already answered cell, is skipped.
func (t *Transport) beginJob(id uint64) (*workerInvocation, bool) {
	invocation := &workerInvocation{live: true}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.cells[id]
	active := !t.closed && t.failed == nil && c != nil && !c.released && c.response == nil
	if active {
		c.jobPhase = "executing"
		c.invocation = invocation
		active = t.protectLocked(id) == nil
	}
	if !active {
		invocation.live = false
	}
	return invocation, active
}

// publishResponse makes the first framed response of a still-live cell its
// immutable reply and protects it. A mapping or runtime HELLO protection
// failure fails the whole transport instead.
func (t *Transport) publishResponse(id uint64, raw []byte, frames, seen [][]byte, unread int, kind uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.cells[id]
	if t.closed || t.failed != nil || c == nil || c.released || c.response != nil {
		return
	}
	if err := t.finishMappingFull(c, raw); err != nil {
		t.failed = err
		return
	}
	c.response = raw
	c.wire = frames
	c.seen = seen
	c.unread = unread
	c.resultKind = kind
	c.jobPhase = "responded"
	if err := t.protectRuntimeHello(c); err != nil {
		t.failed = err
		return
	}
	if c.terminal != nil {
		c.terminal.Phase = "TERMINAL_RESPONDED"
	}
	if t.protectLocked(id) == nil {
		_ = t.protectedPoint("response")
	}
}
