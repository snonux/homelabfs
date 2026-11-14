package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// CellReservation retains one pre-reserved endpoint transport credit.
type CellReservation struct {
	Control bool
	Release func()
}
type cell struct {
	frame       Frame
	body        []byte
	chunks      []bool
	count       int
	reservation CellReservation
	command     *Command
	response    []byte
	wire        [][]byte
	seen        [][]byte
	unread      int
	resultKind  uint16
	released    bool
	native      Native // Historical accepted callback, never restored authentication.
	jobPhase    string
	// blockedError names the semantic error class that left jobPhase
	// "blocked". The cell is never answered, so this is the only durable
	// record of why a guest request stays without a reply.
	blockedError   string
	observations   []Native
	mapping        *ProtectedMapping
	guestWatermark uint64
	terminal       *ProtectedTerminal
	invocation     *workerInvocation
}
type job struct {
	native  Native
	id      uint64
	command Command
}

// Transport owns one endpoint-wide native allocator and 80 aggregate credits.
// It must survive exporter/guest session changes; there is no reset operation.
type Transport struct {
	mu                          sync.Mutex
	service                     *Service
	predecessor                 *Transport
	arenas                      []*admission.Lease
	high                        uint64
	cells                       map[uint64]*cell
	ordinary, control           int
	workspace                   *cell
	ordinaryQueue, controlQueue chan job
	replacementQueue            chan job
	drainQueue                  chan job
	acquisitionQueue            chan job
	// viewAckQueue is the VIEW_ACK lane (d84). A VIEW_ACK's preparation may
	// run a guest observation (an SSH probe of up to ten seconds, see the
	// controlled runtime's Producer.Prepare); on the shared control worker
	// every QUERY/CANCEL/RETIRE/VIEW_POLL queued behind it waited for that
	// probe (zq3-r8 R8-1). It still spends control credits.
	viewAckQueue      chan job
	done              chan struct{}
	workers           sync.WaitGroup
	closed            bool
	ledger            *transportLedger
	failed            error
	uncertainReclaims []*cell // Original owners retained after uncertain atomic protection.
	runtimeHellos     map[string]runtimeHelloWitness
	terminal          map[string]uint64
	guestHigh         map[string]uint64
	guestRecent       []GuestIssuance
	// guestHoles holds, per session, the unissued guest cells below guestHigh
	// that may still arrive out of guest order (see guestReorderWindow).
	guestHoles map[string][]uint64
	// refused is the latest numbered cell removed after a permanent refusal,
	// so that a resend gets the same errno (n64, see refusal.go).
	refused *ProtectedRefusal
	// lostReply is the disposable one-shot response-exposure fixture record
	// (see lost_reply.go); nil unless explicitly armed. It is not protocol
	// state and is never written to or restored from the C8 ledger.
	lostReply *LostReplyEvidence
	// respondFailpoint is private failure injection for tests: it may replace
	// Execute's response bytes before bounds/framing. Production leaves it nil.
	respondFailpoint func(Command, []byte) []byte
}

// NewTransport reserves independent bounded ordinary/control workers. Admission
// must have reserved the fixed 12 MiB replacement parser workspace at enrollment;
// no backend/session may supply another allocator for this inode tuple.
func NewTransport(s *Service, budget *admission.Budget) (*Transport, error) {
	return newTransport(s, budget, "", nil)
}

// NewDurableTransport exclusively creates a private transport ledger. An existing
// root is never a fresh endpoint. The budget must fund TransportLedgerMemoryBytes
// in addition to the original transport arenas. This protects ownership only: opening
// a ledger cannot restore a controller, native authority, workers or RESUME.
func NewDurableTransport(s *Service, budget *admission.Budget, root string) (*Transport, error) {
	if root == "" {
		return nil, fsapi.Invalid
	}
	return newTransport(s, budget, root, nil)
}

// NewDurableTransportWithFailureHook installs constructor-bound failure
// instrumentation on the actual ledger. Any injected error fences ownership;
// the hook supplies no state, provenance, restore or response authority.
func NewDurableTransportWithFailureHook(s *Service, budget *admission.Budget, root string, hook func(string) error) (*Transport, error) {
	if root == "" {
		return nil, fsapi.Invalid
	}
	return newTransport(s, budget, root, hook)
}

// newTransport attaches one transport to s: it checks mapped-protocol
// prerequisites, claims the service, reserves the funded arenas, creates the
// optional durable ledger, binds a mapped producer and only then starts the
// workers.
func newTransport(s *Service, budget *admission.Budget, root string, hook func(string) error) (*Transport, error) {
	if s == nil || budget == nil {
		return nil, fsapi.Invalid
	}
	needsMapped, err := mappedPrerequisites(s, root)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.transportAttached {
		s.mu.Unlock()
		return nil, fsapi.Busy
	}
	s.transportAttached = true
	s.mu.Unlock()
	t := &Transport{runtimeHellos: map[string]runtimeHelloWitness{}, terminal: map[string]uint64{}, guestHigh: map[string]uint64{}, guestHoles: map[string][]uint64{}, service: s, cells: map[uint64]*cell{}}
	t.makeQueues()
	if err = t.reserveArenas(budget, root != ""); err != nil {
		s.mu.Lock()
		s.transportAttached = false
		s.mu.Unlock()
		return nil, err
	}
	if root != "" {
		if err = t.openLedger(root, hook); err != nil {
			return nil, err
		}
	}
	if needsMapped {
		if err = t.bindMapped(); err != nil {
			return nil, err
		}
	}
	s.mu.Lock()
	s.transport = t
	s.mu.Unlock()
	t.startWorkers()
	return t, nil
}

// mappedPrerequisites reports whether any enrolled session uses the mapped
// protocol and, if so, refuses unless mapped cells are funded, a durable
// ledger root is given and a mapped verification producer is configured.
func mappedPrerequisites(s *Service, root string) (bool, error) {
	needsMapped := false
	for _, scope := range s.config.Scopes {
		if p := s.config.sessionProtocol(scope.Session); p != nil && IsMappedProtocol(*p) {
			needsMapped = true
			if err := mappedCellFunding(); err != nil {
				return false, err
			}
			if root == "" {
				return false, fsapi.Permission
			}
			if _, ok := s.config.Verification.(MappedVerificationProducer); !ok || missing(s.config.Verification) {
				return false, fsapi.Permission
			}
		}
	}
	return needsMapped, nil
}

// reserveArenas reserves complete request/reply/receipt bitmaps and bounded
// decode copies for every active credit, plus the funded workers, before any
// callback allocation. The replacement workspace is independent. On failure
// every lease already taken is released again.
func (t *Transport) reserveArenas(budget *admission.Budget, durable bool) error {
	type pool struct {
		class               admission.Class
		bytes, ops, workers uint64
	}
	pools := []pool{{admission.Live, 64 * 8 << 20, 64, 2}, {admission.Control, 16 * 8 << 20, 16, controlWorkers}, {admission.Memory, 12 << 20, 1, 1}}
	if durable {
		// Ledger mappings, freelist and per-row encoding/transaction copies need
		// their own finite owner. This is not another parser workspace.
		pools = append(pools, pool{admission.Memory, TransportLedgerMemoryBytes, 1, 0})
	}
	session := t.service.config.Endpoint.FuseSession
	for _, p := range pools {
		lease, e := budget.Reserve(admission.Request{Class: p.class, Peer: session, Volume: session, Cost: admission.Resources{Bytes: p.bytes, Operations: p.ops, Workers: p.workers}})
		if e != nil {
			t.releaseArenas()
			return e
		}
		t.arenas = append(t.arenas, lease)
	}
	return nil
}

// releaseArenas returns every reserved arena lease.
func (t *Transport) releaseArenas() {
	for _, l := range t.arenas {
		_ = l.Release()
	}
	t.arenas = nil
}

// openLedger exclusively creates the private transport ledger and protects
// the empty initial state. On failure the arenas are released but the service
// remains attached: after uncertain protection a second constructor cannot
// silently abandon an issued private root.
func (t *Transport) openLedger(root string, hook func(string) error) error {
	ledger, err := createTransportLedger(root)
	if err == nil {
		ledger.failpoint = hook
		t.ledger = ledger
		err = t.protectLocked()
		if err == nil {
			err = ledger.finishNamespace()
		}
	}
	if err != nil {
		if ledger != nil {
			_ = ledger.close()
		}
		t.releaseArenas()
	}
	return err
}

// bindMapped binds the mapped verification producer to this transport. On
// failure no worker started and no Transport will be returned: the
// service/ledger claim is retained, but the unopened arena reservations are
// returned.
func (t *Transport) bindMapped() error {
	p, ok := t.service.config.Verification.(MappedVerificationProducer)
	if !ok {
		return nil
	}
	if err := p.BindTransport(context.Background(), t); err != nil {
		t.failed = err
		if t.ledger != nil {
			_ = t.ledger.close()
		}
		t.releaseArenas()
		return err
	}
	return nil
}

// controlWorkers is the number of funded workers that serve control-credit
// cells: the control worker, the drain worker and the VIEW_ACK worker (d84).
// The Live pool funds the ordinary and acquisition workers and the Memory
// pool the replacement worker.
const controlWorkers = 3

// makeQueues creates the bounded per-lane job queues. A queue never holds
// more jobs than its lane has credits, so commitWrite's send never blocks.
func (t *Transport) makeQueues() {
	t.ordinaryQueue = make(chan job, 64)
	t.controlQueue = make(chan job, 16)
	t.replacementQueue = make(chan job, 1)
	t.drainQueue = make(chan job, 16)
	t.acquisitionQueue = make(chan job, 64)
	t.viewAckQueue = make(chan job, 16)
	t.done = make(chan struct{})
}

// startWorkers starts one worker per lane. Lanes are independent: workers
// execute accepted cells out of order across lanes (Retained-9p-mapping.md),
// and each cell still has exactly one dispatch and one immutable response.
func (t *Transport) startWorkers() {
	queues := []chan job{t.ordinaryQueue, t.controlQueue, t.replacementQueue, t.drainQueue, t.acquisitionQueue, t.viewAckQueue}
	t.workers.Add(len(queues))
	for _, queue := range queues {
		go t.worker(queue)
	}
}

// writeEffects collects what one Transport.Write changed under the owner lock
// and must protect before anything leaves it: the accepted job and its queue,
// the reclaimed or terminal donor cells, and a permanently refused cell that
// was removed (n64, discardRefused). Their memory is released only after the
// commit.
type writeEffects struct {
	dispatch  *job
	queue     chan job
	reclaimed []*cell
	dropped   *cell
}

// Write validates and copies one positional chunk without awaiting any service,
// recall, grant or vnode. An error never conveys filesystem success.
func (t *Transport) Write(ctx context.Context, n Native, node, offset uint64, b []byte) (written uint32, err error) {
	f, scope, e := t.writeFrame(ctx, n, node, offset, b)
	if e != nil {
		return 0, e
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil {
		return 0, fsapi.Unavailable
	}
	w := &writeEffects{}
	// Workers cannot execute, and ownership cannot be released, before the exact
	// accepted state has committed. Error paths also protect workspace changes
	// and the removal of a permanently refused cell.
	defer func() { written, err = t.commitWrite(f.Cell, w, written, err) }()
	c, admitted, e := t.writeCell(f, scope)
	if e != nil {
		return 0, e
	}
	if admitted {
		return uint32(len(b)), nil
	}
	complete, e := storeChunk(c, f, n)
	if e != nil {
		return 0, e
	}
	if !complete {
		return uint32(len(b)), nil
	}
	if command, e := t.acceptCell(c, f, scope, n, w); e != nil {
		t.discardRefused(c, f, command, e, w)
		return 0, e
	}
	return uint32(len(b)), nil
}

// writeFrame runs the lock-free checks of one native chunk write:
// authentication, the command node, the frame codec, the session's minor, the
// positional offset and the session's registered scope.
func (t *Transport) writeFrame(ctx context.Context, n Native, node, offset uint64, b []byte) (Frame, *Context, error) {
	if e := t.service.Authenticate(ctx, n); e != nil {
		return Frame{}, nil, e
	}
	if node != t.service.config.Endpoint.CommandNode {
		return Frame{}, nil, fsapi.Permission
	}
	f, e := DecodeFrame(b)
	if e != nil {
		return Frame{}, nil, e
	}
	expectedMinor := uint16(0)
	if protocol := t.service.config.sessionProtocol(f.Session.String()); protocol != nil {
		expectedMinor = uint16(protocol.BridgeMinor)
	}
	if f.Minor != expectedMinor {
		return Frame{}, nil, fsapi.Unsupported
	}
	cellID, index, within, e := position(offset)
	if e != nil || cellID != f.Cell || within != 0 || uint64(f.Offset/ChunkBytes) != index || f.Kind != 1 {
		return Frame{}, nil, fsapi.Invalid
	}
	scope := t.scope(f.Session.String())
	if scope == nil {
		return Frame{}, nil, fsapi.Permission
	}
	return f, scope, nil
}

// commitWrite protects every change of one Write before its job is queued or
// any released cell's memory is freed. A protection failure fences the
// transport and retains the old owners (uncertainReclaims).
func (t *Transport) commitWrite(id uint64, w *writeEffects, written uint32, err error) (uint32, error) {
	ids := []uint64{id}
	for _, old := range w.reclaimed {
		ids = append(ids, old.frame.Cell)
	}
	if e := t.protectLocked(ids...); e != nil {
		t.uncertainReclaims = append(t.uncertainReclaims, w.reclaimed...)
		if w.dropped != nil {
			t.uncertainReclaims = append(t.uncertainReclaims, w.dropped)
		}
		return 0, e
	}
	if w.dropped != nil {
		releaseCell(w.dropped)
	}
	if len(w.reclaimed) != 0 {
		for _, old := range w.reclaimed {
			releaseCell(old)
		}
		if e := t.protectedPoint("reclaim"); e != nil {
			return 0, e
		}
	}
	if w.dispatch != nil {
		w.queue <- *w.dispatch
	}
	if e := t.protectedPoint("write"); e != nil {
		return 0, e
	}
	return written, err
}

// writeCell finds or allocates the cell that frame f writes into. admitted is
// true for an identical resend of an already admitted cell, which succeeds
// without any change. A number at or below the high-water without a cell was
// released, permanently refused or abandoned (refusedResend). A different
// frame at an existing cell's own number stays Invalid; only a frame at the
// next number replaces an abandoned cell (nextCell).
func (t *Transport) writeCell(f Frame, scope *Context) (c *cell, admitted bool, err error) {
	if final := t.terminal[scope.Session]; final != 0 && final != f.Cell {
		return nil, false, fsapi.Permission
	}
	c = t.cells[f.Cell]
	if c == nil && f.Cell <= t.high {
		return nil, false, t.refusedResend(f)
	}
	if c != nil {
		if c.released {
			return nil, false, fsapi.Stale
		}
		if !sameFrame(c.frame, f) {
			return nil, false, fsapi.Invalid
		}
		if c.command != nil {
			if !bytes.Equal(c.body[f.Offset:f.Offset+uint32(len(f.Data))], f.Data) {
				return nil, false, fsapi.Invalid
			}
			return c, true, nil
		}
		return c, false, nil
	}
	if f.Cell != t.high+1 {
		return nil, false, fsapi.Invalid
	}
	return t.nextCell(f)
}

// nextCell finds or creates the cell of a frame at the next native number
// (high+1). It first replaces a cell or workspace the exporter abandoned
// (x64, replaceAbandoned), then continues an identical workspace (a
// different frame over a complete, validated workspace still waits) or
// starts a new workspace or numbered cell.
func (t *Transport) nextCell(f Frame) (c *cell, admitted bool, err error) {
	if err = t.replaceAbandoned(f); err != nil {
		return nil, false, err
	}
	if f.Minor == 4 && t.workspace == nil {
		t.pruneGuestRecent()
		if len(t.guestRecent)+t.mappingIncomplete() >= mappingRecentLimit {
			return nil, false, fsapi.WouldBlock
		}
	}
	if t.workspace != nil {
		if !sameFrame(t.workspace.frame, f) {
			return nil, false, fsapi.WouldBlock
		}
		return t.workspace, false, nil
	}
	// A first chunk containing reclaim can use the independent workspace. Full
	// schema validation still precedes atomic release and allocator advancement.
	if f.Flags&2 != 0 {
		c = newCell(f)
		c.guestWatermark = t.guestWatermark(f.Session.String())
		t.workspace = c
		return c, false, nil
	}
	c, err = t.allocateCell(f)
	return c, false, err
}

// allocateCell gives a non-donor cell its native number and one ordinary or
// control credit at its first chunk, before any content check. A permanent
// content refusal later removes it again (discardRefused).
func (t *Transport) allocateCell(f Frame) (*cell, error) {
	r := CellReservation{Control: f.Flags&1 != 0, Release: func() {}}
	if r.Control && t.control >= 16 || !r.Control && t.ordinary >= 64 {
		return nil, fsapi.WouldBlock
	}
	if t.incomplete(f.Session.String()) >= 16 ||
		f.Minor >= 1 && !r.Control && t.incompleteOrdinary(f.Session.String()) >= 12 {
		return nil, fsapi.WouldBlock
	}
	c := newCell(f)
	c.guestWatermark = t.guestWatermark(f.Session.String())
	c.reservation = r
	t.cells[f.Cell] = c
	t.high = f.Cell
	if r.Control {
		t.control++
	} else {
		t.ordinary++
	}
	return c, nil
}

// storeChunk copies one chunk into c, or checks an identical resend of a
// stored chunk, and reports whether every chunk of c is in. A resend of a
// stored chunk of a complete cell revalidates the whole cell.
func storeChunk(c *cell, f Frame, n Native) (bool, error) {
	index := intIndex(f.Offset)
	if c.chunks[index] {
		if !bytes.Equal(c.body[f.Offset:f.Offset+uint32(len(f.Data))], f.Data) {
			return false, fsapi.Invalid
		}
	} else {
		copy(c.body[f.Offset:], f.Data)
		c.chunks[index] = true
		c.observations[index] = cloneNative(n)
		c.count++
	}
	return c.count == len(c.chunks), nil
}

// decodeCell checks a complete cell's hash, decodes its command and binds it
// to the frame and the session's registered scope.
func (t *Transport) decodeCell(c *cell, f Frame, scope *Context) (Command, error) {
	if sha256.Sum256(c.body) != f.Hash {
		return Command{}, fsapi.Invalid
	}
	command, e := DecodeCommandVersion(c.body, f.Minor)
	if e != nil || command.Cell != f.Cell || command.Context != *scope {
		return Command{}, fsapi.Invalid
	}
	if e = t.service.CheckScope(command.Context); e != nil {
		return Command{}, e
	}
	return command, nil
}

// acceptCell validates a complete cell and, if every check passes, admits it:
// a donor send takes its reclaim target's credit and the next native number,
// a terminal manifest stages its donors, and the job is staged for dispatch.
// It returns the decoded command (nil when decoding failed) so that a refusal
// can be classified (discardRefused). Every refusal leaves the transport
// unchanged.
func (t *Transport) acceptCell(c *cell, f Frame, scope *Context, n Native, w *writeEffects) (*Command, error) {
	command, e := t.decodeCell(c, f, scope)
	if e != nil {
		return nil, e
	}
	control := controlCommand(command)
	if (command.Reclaim != nil) != (f.Flags&2 != 0) {
		return &command, fsapi.Permission
	}
	mappingCandidate, e := t.prepareMapping(c, command)
	if e != nil {
		return &command, e
	}
	terminal, e := t.prepareTerminal(c, command, mappingCandidate)
	if e != nil {
		return &command, e
	}
	if c == t.workspace {
		if e = t.checkReclaim(f, command, control); e != nil {
			return &command, e
		}
	} else if c.reservation.Control != control || command.Reclaim != nil {
		return &command, fsapi.Permission
	}
	// The frame's control flag is the cell's credit class: allocateCell sets it
	// from the flag, and checkReclaim requires the donor's target to match it.
	queue := t.commandQueue(command, f.Flags&1 != 0)
	if len(queue) == cap(queue) {
		return &command, fsapi.WouldBlock
	}
	if c == t.workspace {
		t.takeReclaim(c, f, command, w)
	}
	if terminal != nil {
		t.stageTerminal(c, f, command, terminal, w)
	}
	t.acceptMapping(c, command, mappingCandidate)
	c.command = &command
	c.native = cloneNative(n)
	c.jobPhase = "accepted"
	w.dispatch = &job{id: f.Cell, command: command, native: cloneNative(n)}
	w.queue = queue
	return &command, nil
}

// checkReclaim refuses a donor send whose reclaim target is not a fully read,
// responded cell of the same session with the same credit class.
func (t *Transport) checkReclaim(f Frame, command Command, control bool) error {
	if command.Reclaim == nil {
		return fsapi.Invalid
	}
	target := t.cells[command.Reclaim.Cell]
	if target == nil || target.released || target.frame.Session != f.Session || target.unread != 0 || target.response == nil || digest(target.response) != command.Reclaim.ResponseHash || target.frame.Cell == f.Cell {
		return fsapi.Permission
	}
	if !oneOf(command.Op, "RELEASE_CELL", "SESSION_RETIRE") && target.reservation.Control != control {
		return fsapi.Permission
	}
	if (f.Flags&1 != 0) != target.reservation.Control {
		return fsapi.Permission
	}
	return nil
}

// takeReclaim admits the workspace cell: it takes the target's credit and the
// next native number, and the target is released after the commit.
func (t *Transport) takeReclaim(c *cell, f Frame, command Command, w *writeEffects) {
	target := t.cells[command.Reclaim.Cell]
	w.reclaimed = append(w.reclaimed, target)
	delete(t.cells, command.Reclaim.Cell)
	c.reservation = CellReservation{Control: f.Flags&1 != 0, Release: func() {}}
	t.cells[f.Cell] = c
	t.high = f.Cell
	t.workspace = nil
}

// stageTerminal removes the donors of an exact prevalidated terminal manifest,
// the only admission that may remove multiple old cells. Post-index counters
// are staged under the owner lock; no credit is released until the complete
// deletion/candidate/fence transaction commits.
func (t *Transport) stageTerminal(c *cell, f Frame, command Command, terminal *ProtectedTerminal, w *writeEffects) {
	for _, donor := range terminal.Donors {
		old := t.cells[donor.NativeCell]
		w.reclaimed = append(w.reclaimed, old)
		delete(t.cells, donor.NativeCell)
		if old.reservation.Control {
			t.control--
		} else {
			t.ordinary--
		}
	}
	c.terminal = terminal
	t.terminal[command.Context.Session] = f.Cell
}

// commandQueue selects the worker queue of an accepted command. The lane
// only picks the worker; the credit class stays the frame's control flag.
//
// VIEW_ACK has its own lane so that its guest observation cannot hold up the
// other control cells (d84). Moving it off the control worker is safe because
// nothing relied on a VIEW_ACK executing in order with another control cell:
//   - Any two control commands can already wait on the host together. The
//     guest's shared tag 65533 is held per SEND/FETCH exchange, not per
//     command: it is freed after SEND, and the reply is polled with FETCH.
//   - QUERY, VIEW_QUERY, VIEW_POLL and VIEW_WAIT are re-read polls. A QUERY
//     rereads claim state, VIEW_WAIT is keyed by after_sequence, and the guest
//     retries a PENDING answer about every 10 ms, so a poll answered before a
//     concurrent ACK is simply asked again.
//   - Controller state changes are serialized by viewcontrol's own lock (a
//     contended TryLock answers WouldBlock and the cell is retried), not by
//     this worker.
//   - FIFO within the control lane was never promised: the WouldBlock retry
//     ring reorders cells (Retained-9p-view-leases.md), and the lanes already
//     run accepted cells out of order (Retained-9p-mapping.md).
//   - The guest sends a cell that depends on an ACK only after that ACK's reply
//     (its drain is queued after hfs_mmap_ack returns).
//
// The ACK itself still runs preparation (the measured observation) before its
// exchange. The single ACK worker dispatches ACKs in arrival order; a
// WouldBlock retry may run after a later ACK, which is harmless because each
// view's event worker has at most one ACK in flight.
func (t *Transport) commandQueue(command Command, control bool) chan job {
	switch {
	case oneOf(command.Op, "RELEASE_CELL", "SESSION_RETIRE"):
		return t.replacementQueue
	case oneOf(command.Op, "VIEW_ACQUIRE", "DIRTY_RESERVE"):
		return t.acquisitionQueue
	case drainCommand(command):
		return t.drainQueue
	case command.Op == "VIEW_ACK":
		return t.viewAckQueue
	case control:
		return t.controlQueue
	}
	return t.ordinaryQueue
}

// Read returns immutable positional chunk bytes, EAGAIN, ESTALE or exact EOF.
// It never waits for an operation. Native read splitting preserves byte identity.
// This is the actual protected response exposure: an armed disposable
// lost-reply fixture may withhold exactly one computed exposure here (EIO, no
// bytes, no coverage) after the original effect and response were protected.
func (t *Transport) Read(ctx context.Context, n Native, node, offset uint64, count uint32) ([]byte, error) {
	if e := t.service.Authenticate(ctx, n); e != nil {
		return nil, e
	}
	kind := uint16(2)
	if node == t.service.config.Endpoint.RecallNode {
		kind = 3
	} else if node != t.service.config.Endpoint.ResultNode {
		return nil, fsapi.Permission
	}
	if offset >= VirtualSize {
		return []byte{}, nil
	}
	id, j, k, e := position(offset)
	if e != nil {
		return nil, e
	}
	if count > 32768 {
		return nil, fsapi.Invalid
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil {
		return nil, fsapi.Unavailable
	}
	c := t.cells[id]
	if c == nil {
		if id <= t.high {
			return nil, fsapi.Stale
		}
		return nil, fsapi.WouldBlock
	}
	if c.released {
		return nil, fsapi.Stale
	}
	// A blocked cell is never answered or redispatched. Report that as a
	// definitive error (EIO through FUSE, Rbridge ERROR to the guest) rather
	// than WouldBlock, which every reader would retry until its deadline.
	if c.response == nil && c.jobPhase == "blocked" {
		return nil, fsapi.IO
	}
	if c.response == nil || kind != c.resultKind {
		return nil, fsapi.WouldBlock
	}
	last := len(c.wire) - 1
	if j > uint64(last) {
		return []byte{}, nil
	}
	chunk := c.wire[j]
	if k >= uint64(len(chunk)) {
		if j == uint64(last) {
			return []byte{}, nil
		}
		return nil, fsapi.Invalid
	}
	end := min(uint64(len(chunk)), k+uint64(count))
	data := bytes.Clone(chunk[k:end])
	if t.dropExposureLocked(c, id, kind, j, k, data) {
		return nil, fsapi.IO
	}
	if err := t.coverLocked(c, id, j, k, end); err != nil {
		return nil, err
	}
	return data, nil
}

// coverLocked marks newly exposed wire bytes as read and protects that change
// before the bytes are returned. Already covered bytes change nothing.
func (t *Transport) coverLocked(c *cell, id, chunk, from, end uint64) error {
	changed := false
	for i := from; i < end; i++ {
		if c.seen[chunk][i] == 0 {
			c.seen[chunk][i] = 1
			c.unread--
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := t.protectLocked(id); err != nil {
		return err
	}
	return t.protectedPoint("read")
}

// Close stops component workers after all dispatched calls finish. It does not
// retire claims, destroy the endpoint allocator or claim cache-domain exclusion.
func (t *Transport) Close() {
	t.stopWorkers()
	t.mu.Lock()
	if t.ledger != nil {
		_ = t.ledger.close()
		t.ledger = nil
	}
	t.mu.Unlock()
}

func (t *Transport) stopWorkers() {
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		close(t.ordinaryQueue)
		close(t.controlQueue)
		close(t.replacementQueue)
		close(t.drainQueue)
		close(t.acquisitionQueue)
		close(t.viewAckQueue)
	}
	t.mu.Unlock()
	t.workers.Wait()
}

// recordExecuteFailure protects the cell phase after Service.Execute failed and
// reports whether the job is retried. WouldBlock/NoSpace are transient capacity
// refusals and stay "pending" in the retry ring unless wrapped by Final. Every
// other error is final for this dispatch: the cell becomes "blocked", its row
// records the error class for transport.db analysis, and Read answers it with
// an explicit error instead of WouldBlock (zq3-r4: a silent blocked VIEW_POLL
// left the guest retrying FETCH until its 30 s deadline).
func (t *Transport) recordExecuteFailure(id uint64, e error) bool {
	retryable := retryableExecuteError(e)
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.cells[id]; c != nil && t.failed == nil {
		c.jobPhase = "blocked"
		c.blockedError = blockedErrorClass(e)
		if retryable {
			c.jobPhase = "pending"
			c.blockedError = ""
		}
		_ = t.protectLocked(id)
	}
	return retryable
}

// recordResponseFailure makes a cell whose successful Execute produced bytes
// that cannot be framed (empty, over BodyBytes, or unencodable) terminally
// blocked under the given class. The effect may already be protected by the
// service ledger; only this transport reply is unavailable.
func (t *Transport) recordResponseFailure(id uint64, class string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.cells[id]; c != nil && t.failed == nil && c.response == nil {
		c.jobPhase = "blocked"
		c.blockedError = class
		_ = t.protectLocked(id)
	}
}

func responseBoundsClass(raw []byte) string {
	if len(raw) == 0 {
		return "RESPONSE_EMPTY"
	}
	return "RESPONSE_TOO_LARGE"
}

// FinalError marks an Execute error as definitive for its cell even when the
// wrapped class (WouldBlock/NoSpace) would otherwise be retried. Callers use
// it when retrying the identical command cannot succeed, so the guest gets an
// error reply promptly instead of FETCH AGAIN until its deadline.
type FinalError struct{ Err error }

func (e FinalError) Error() string { return "final: " + e.Err.Error() }
func (e FinalError) Unwrap() error { return e.Err }

// Final wraps err as a FinalError; nil stays nil.
func Final(err error) error {
	if err == nil {
		return nil
	}
	return FinalError{Err: err}
}

func retryableExecuteError(e error) bool {
	var final FinalError
	if errors.As(e, &final) {
		return false
	}
	return errors.Is(e, fsapi.WouldBlock) || errors.Is(e, fsapi.NoSpace)
}

// blockedErrorClass returns a bounded, content-free error class: the semantic
// fsapi errno name when one is wrapped, otherwise the Go error type. Error text
// is never stored because it may echo request bytes and is unbounded.
func blockedErrorClass(e error) string {
	var code fsapi.Errno
	if errors.As(e, &code) {
		return code.Error()
	}
	class := fmt.Sprintf("UNCLASSIFIED:%T", e)
	return class[:min(len(class), blockedErrorLimit)]
}

// responseFrames splits one immutable response into wire frames plus their
// unread byte bitmaps. valid is false when any frame cannot be encoded.
func (t *Transport) responseFrames(j job, kind uint16, raw []byte) (frames, seen [][]byte, unread int, valid bool) {
	hash := sha256.Sum256(raw)
	session := parseID(j.command.Context.Session)
	for offset := 0; offset < len(raw); offset += ChunkBytes {
		wire, e := EncodeFrame(Frame{Minor: commandMinor(j.command), Flags: t.flags(j.id), Kind: kind, Total: uint32(len(raw)), Offset: uint32(offset), Session: session, Cell: j.id, Hash: hash, Data: raw[offset:min(offset+ChunkBytes, len(raw))]})
		if e != nil {
			return nil, nil, 0, false
		}
		frames = append(frames, wire)
		seen = append(seen, make([]byte, len(wire)))
		unread += len(wire)
	}
	return frames, seen, unread, true
}

func (t *Transport) scope(session string) *Context {
	t.service.mu.Lock()
	defer t.service.mu.Unlock()
	s := t.service.sessions[session]
	if s == nil {
		return nil
	}
	c := s.Context
	return &c
}
func (t *Transport) incomplete(session string) int {
	n := 0
	for _, c := range t.cells {
		if c.frame.Session.String() == session && c.command == nil && !c.released {
			n++
		}
	}
	return n
}
func newCell(f Frame) *cell {
	f.Data = nil // Body owns the accepted bytes; never retain a borrowed callback slice.
	return &cell{frame: f, body: make([]byte, int(f.Total)), chunks: make([]bool, (int(f.Total)+ChunkBytes-1)/ChunkBytes), observations: make([]Native, (int(f.Total)+ChunkBytes-1)/ChunkBytes)}
}
func sameFrame(a, b Frame) bool {
	return a.Minor == b.Minor && a.Flags == b.Flags && a.Kind == b.Kind && a.Total == b.Total && a.Session == b.Session && a.Cell == b.Cell && a.Hash == b.Hash
}
func intIndex(offset uint32) uint64 { return uint64(offset / ChunkBytes) }
func position(offset uint64) (uint64, uint64, uint64, error) {
	if offset >= VirtualSize {
		return 0, 0, 0, fsapi.Invalid
	}
	id := offset / CellStride
	in := offset % CellStride
	if id < 1 || id > MaxCell {
		return 0, 0, 0, fsapi.Invalid
	}
	return id, in / (HeaderBytes + ChunkBytes), in % (HeaderBytes + ChunkBytes), nil
}
func controlCommand(c Command) bool {
	if oneOf(c.Op, "VIEW_ACQUIRE", "DIRTY_RESERVE") {
		return false
	}
	if c.Op == "CALL" {
		var call Call
		if json.Unmarshal(c.Payload, &call) != nil {
			return false
		}
		return call.Intent.Opcode == "DRAIN_WRITE"
	}
	return c.Op != "HELLO" && c.Op != "CALL" && c.Op != "ALLOCATE"
}
func releaseCell(c *cell) {
	c.released = true
	c.body = nil
	c.response = nil
	c.wire = nil
	c.seen = nil
	c.chunks = nil
	c.command = nil
	c.reservation.Release()
}

// Endpoint returns the enrolled immutable native inode tuple.
func (t *Transport) Endpoint() Endpoint { return t.service.config.Endpoint }

// Authenticate verifies a native callback without touching cell or claim state.
func (t *Transport) Authenticate(ctx context.Context, n Native) error {
	return t.service.Authenticate(ctx, n)
}

func (t *Transport) flags(id uint64) uint16 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.cells[id]; c != nil {
		return c.frame.Flags
	}
	return 0
}

func cloneNative(n Native) Native { n.Groups = append([]uint32(nil), n.Groups...); return n }

func drainCommand(c Command) bool {
	if c.Op != "CALL" {
		return false
	}
	var call Call
	if json.Unmarshal(c.Payload, &call) != nil {
		return false
	}
	return call.Intent.Opcode == "DRAIN_WRITE"
}

func (t *Transport) incompleteOrdinary(session string) int {
	n := 0
	for _, c := range t.cells {
		if c.frame.Session.String() == session && c.command == nil && !c.released && !c.reservation.Control {
			n++
		}
	}
	return n
}
