package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// CellReservation retains one pre-reserved endpoint transport credit.
type CellReservation struct {
	Control bool
	Release func()
}
type cell struct {
	frame          Frame
	body           []byte
	chunks         []bool
	count          int
	reservation    CellReservation
	command        *Command
	response       []byte
	wire           [][]byte
	seen           [][]byte
	unread         int
	resultKind     uint16
	released       bool
	native         Native // Historical accepted callback, never restored authentication.
	jobPhase       string
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
	arenas                      []*admission.Lease
	high                        uint64
	cells                       map[uint64]*cell
	ordinary, control           int
	workspace                   *cell
	ordinaryQueue, controlQueue chan job
	replacementQueue            chan job
	drainQueue                  chan job
	acquisitionQueue            chan job
	done                        chan struct{}
	workers                     sync.WaitGroup
	closed                      bool
	ledger                      *transportLedger
	failed                      error
	uncertainReclaims           []*cell // Original owners retained after uncertain atomic protection.
	terminal                    map[string]uint64
	guestHigh                   map[string]uint64
	guestRecent                 []GuestIssuance
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

func newTransport(s *Service, budget *admission.Budget, root string, hook func(string) error) (*Transport, error) {
	if s == nil || budget == nil {
		return nil, fsapi.Invalid
	}
	needsMapped := false
	for _, scope := range s.config.Scopes {
		if p := s.config.sessionProtocol(scope.Session); p != nil && IsMappedProtocol(*p) {
			needsMapped = true
			if err := mappedCellFunding(); err != nil {
				return nil, err
			}
			if root == "" {
				return nil, fsapi.Permission
			}
			if _, ok := s.config.Verification.(MappedVerificationProducer); !ok || missing(s.config.Verification) {
				return nil, fsapi.Permission
			}
		}
	}
	s.mu.Lock()
	if s.transportAttached {
		s.mu.Unlock()
		return nil, fsapi.Busy
	}
	s.transportAttached = true
	s.mu.Unlock()
	t := &Transport{terminal: map[string]uint64{}, guestHigh: map[string]uint64{}, service: s, cells: map[uint64]*cell{}, ordinaryQueue: make(chan job, 64), controlQueue: make(chan job, 16), replacementQueue: make(chan job, 1), drainQueue: make(chan job, 16), acquisitionQueue: make(chan job, 64), done: make(chan struct{})}
	// Reserve complete request/reply/receipt bitmap and bounded decode copies for
	// every active credit before any callback allocation. Workspace is independent.
	pools := []struct {
		class               admission.Class
		bytes, ops, workers uint64
	}{{admission.Live, 64 * 8 << 20, 64, 2}, {admission.Control, 16 * 8 << 20, 16, 2}, {admission.Memory, 12 << 20, 1, 1}}
	if root != "" {
		// Ledger mappings, freelist and per-row encoding/transaction copies need
		// their own finite owner. This is not another parser workspace.
		pools = append(pools, struct {
			class               admission.Class
			bytes, ops, workers uint64
		}{admission.Memory, TransportLedgerMemoryBytes, 1, 0})
	}
	for _, pool := range pools {
		lease, e := budget.Reserve(admission.Request{Class: pool.class, Peer: s.config.Endpoint.FuseSession, Volume: s.config.Endpoint.FuseSession, Cost: admission.Resources{Bytes: pool.bytes, Operations: pool.ops, Workers: pool.workers}})
		if e != nil {
			for _, l := range t.arenas {
				_ = l.Release()
			}
			s.mu.Lock()
			s.transportAttached = false
			s.mu.Unlock()
			return nil, e
		}
		t.arenas = append(t.arenas, lease)
	}
	if root != "" {
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
			for _, l := range t.arenas {
				_ = l.Release()
			}
			// The service remains attached after uncertain protection. A second
			// constructor cannot silently abandon an issued private root.
			return nil, err
		}
	}
	if p, ok := s.config.Verification.(MappedVerificationProducer); needsMapped && ok {
		if err := p.BindTransport(context.Background(), t); err != nil {
			t.failed = err
			if t.ledger != nil {
				_ = t.ledger.close()
			}
			// No worker started and no Transport will be returned. Retain the
			// service/ledger claim, but return its unopened arena reservations.
			for _, lease := range t.arenas {
				_ = lease.Release()
			}
			return nil, err
		}
	}
	s.mu.Lock()
	s.transport = t
	s.mu.Unlock()
	t.workers.Add(5)
	go t.worker(t.ordinaryQueue)
	go t.worker(t.controlQueue)
	go t.worker(t.replacementQueue)
	go t.worker(t.drainQueue)
	go t.worker(t.acquisitionQueue)
	return t, nil
}

// Write validates and copies one positional chunk without awaiting any service,
// recall, grant or vnode. An error never conveys filesystem success.
func (t *Transport) Write(ctx context.Context, n Native, node, offset uint64, b []byte) (written uint32, err error) {
	if e := t.service.Authenticate(ctx, n); e != nil {
		return 0, e
	}
	if node != t.service.config.Endpoint.CommandNode {
		return 0, fsapi.Permission
	}
	f, e := DecodeFrame(b)
	if e != nil {
		return 0, e
	}
	expectedMinor := uint16(0)
	if protocol := t.service.config.sessionProtocol(f.Session.String()); protocol != nil {
		expectedMinor = uint16(protocol.BridgeMinor)
	}
	if f.Minor != expectedMinor {
		return 0, fsapi.Unsupported
	}
	cellID, index, within, e := position(offset)
	if e != nil || cellID != f.Cell || within != 0 || uint64(f.Offset/ChunkBytes) != index || f.Kind != 1 {
		return 0, fsapi.Invalid
	}
	scope := t.scope(f.Session.String())
	if scope == nil {
		return 0, fsapi.Permission
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil {
		return 0, fsapi.Unavailable
	}
	var dispatch *job
	var dispatchQueue chan job
	var reclaimed []*cell
	// Workers cannot execute, and ownership cannot be released, before the exact
	// accepted state has committed. Error paths also protect workspace changes.
	defer func() {
		ids := []uint64{f.Cell}
		for _, old := range reclaimed {
			ids = append(ids, old.frame.Cell)
		}
		if e := t.protectLocked(ids...); e != nil {
			t.uncertainReclaims = append(t.uncertainReclaims, reclaimed...)
			written, err = 0, e
			return
		}
		if len(reclaimed) != 0 {
			for _, old := range reclaimed {
				releaseCell(old)
			}
			if e := t.protectedPoint("reclaim"); e != nil {
				written, err = 0, e
				return
			}
		}
		if dispatch != nil {
			dispatchQueue <- *dispatch
		}
		if e := t.protectedPoint("write"); e != nil {
			written, err = 0, e
		}
	}()
	if final := t.terminal[scope.Session]; final != 0 && final != f.Cell {
		return 0, fsapi.Permission
	}
	c := t.cells[f.Cell]
	if c == nil && f.Cell <= t.high {
		return 0, fsapi.Stale
	}
	if c != nil {
		if c.released {
			return 0, fsapi.Stale
		}
		if !sameFrame(c.frame, f) {
			return 0, fsapi.Invalid
		}
		if c.command != nil {
			if !bytes.Equal(c.body[f.Offset:f.Offset+uint32(len(f.Data))], f.Data) {
				return 0, fsapi.Invalid
			}
			return uint32(len(b)), nil
		}
	}
	if c == nil {
		if f.Cell != t.high+1 {
			return 0, fsapi.Invalid
		}
		if f.Minor == 4 && t.workspace == nil {
			t.pruneGuestRecent()
			if len(t.guestRecent)+t.mappingIncomplete() >= mappingRecentLimit {
				return 0, fsapi.WouldBlock
			}
		}
		if t.workspace != nil {
			if !sameFrame(t.workspace.frame, f) {
				return 0, fsapi.WouldBlock
			}
			c = t.workspace
		} else {
			// A first chunk containing reclaim can use the independent workspace. Full
			// schema validation still precedes atomic release and allocator advancement.
			if f.Flags&2 != 0 {
				c = newCell(f)
				c.guestWatermark = t.guestWatermark(f.Session.String())
				t.workspace = c
			} else {
				r := CellReservation{Control: f.Flags&1 != 0, Release: func() {}}
				if r.Control && t.control >= 16 || !r.Control && t.ordinary >= 64 {
					return 0, fsapi.WouldBlock
				}
				if t.incomplete(f.Session.String()) >= 16 ||
					f.Minor >= 1 && !r.Control && t.incompleteOrdinary(f.Session.String()) >= 12 {
					r.Release()
					return 0, fsapi.WouldBlock
				}
				c = newCell(f)
				c.guestWatermark = t.guestWatermark(f.Session.String())
				c.reservation = r
				t.cells[f.Cell] = c
				t.high = f.Cell
				if r.Control {
					t.control++
				} else {
					t.ordinary++
				}
			}
		}
	}
	index = intIndex(f.Offset)
	if c.chunks[index] {
		if !bytes.Equal(c.body[f.Offset:f.Offset+uint32(len(f.Data))], f.Data) {
			return 0, fsapi.Invalid
		}
		if c.count != len(c.chunks) {
			return uint32(len(b)), nil
		}
	} else {
		copy(c.body[f.Offset:], f.Data)
		c.chunks[index] = true
		c.observations[index] = cloneNative(n)
		c.count++
	}
	if c.count != len(c.chunks) {
		return uint32(len(b)), nil
	}
	if sha256.Sum256(c.body) != f.Hash {
		return 0, fsapi.Invalid
	}
	command, e := DecodeCommandVersion(c.body, f.Minor)
	if e != nil || command.Cell != f.Cell || command.Context != *scope {
		return 0, fsapi.Invalid
	}
	if e = t.service.CheckScope(command.Context); e != nil {
		return 0, e
	}
	control := controlCommand(command)
	if (command.Reclaim != nil) != (f.Flags&2 != 0) {
		return 0, fsapi.Permission
	}

	mappingCandidate, e := t.prepareMapping(c, command)
	if e != nil {
		return 0, e
	}
	terminal, e := t.prepareTerminal(c, command, mappingCandidate)
	if e != nil {
		return 0, e
	}
	if c == t.workspace {
		if command.Reclaim == nil {
			return 0, fsapi.Invalid
		}
		target := t.cells[command.Reclaim.Cell]
		if target == nil || target.released || target.frame.Session != f.Session || target.unread != 0 || target.response == nil || digest(target.response) != command.Reclaim.ResponseHash || target.frame.Cell == f.Cell {
			return 0, fsapi.Permission
		}
		if !oneOf(command.Op, "RELEASE_CELL", "SESSION_RETIRE") && target.reservation.Control != control {
			return 0, fsapi.Permission
		}
		r := CellReservation{Control: f.Flags&1 != 0, Release: func() {}}
		if r.Control != target.reservation.Control {
			return 0, fsapi.Permission
		}
		queue := t.ordinaryQueue
		if oneOf(command.Op, "RELEASE_CELL", "SESSION_RETIRE") {
			queue = t.replacementQueue
		} else if oneOf(command.Op, "VIEW_ACQUIRE", "DIRTY_RESERVE") {
			queue = t.acquisitionQueue
		} else if drainCommand(command) {
			queue = t.drainQueue
		} else if r.Control {
			queue = t.controlQueue
		}
		if len(queue) == cap(queue) {
			r.Release()
			return 0, fsapi.WouldBlock
		}
		reclaimed = append(reclaimed, target)
		delete(t.cells, command.Reclaim.Cell)
		c.reservation = r
		t.cells[f.Cell] = c
		t.high = f.Cell
		t.workspace = nil
	} else if c.reservation.Control != control || command.Reclaim != nil {
		return 0, fsapi.Permission
	}
	queue := t.ordinaryQueue
	if oneOf(command.Op, "RELEASE_CELL", "SESSION_RETIRE") {
		queue = t.replacementQueue
	} else if oneOf(command.Op, "VIEW_ACQUIRE", "DIRTY_RESERVE") {
		queue = t.acquisitionQueue
	} else if drainCommand(command) {
		queue = t.drainQueue
	} else if c.reservation.Control {
		queue = t.controlQueue
	}
	if len(queue) == cap(queue) {
		return 0, fsapi.WouldBlock
	}
	if terminal != nil {
		// Only the exact prevalidated terminal manifest may remove multiple old
		// cells. Stage post-index counters under the owner lock; no credit is
		// released until the complete deletion/candidate/fence transaction commits.
		for _, donor := range terminal.Donors {
			old := t.cells[donor.NativeCell]
			reclaimed = append(reclaimed, old)
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
	t.acceptMapping(c, command, mappingCandidate)
	c.command = &command
	c.native = cloneNative(n)
	c.jobPhase = "accepted"
	dispatch = &job{id: f.Cell, command: command, native: cloneNative(n)}
	dispatchQueue = queue
	return uint32(len(b)), nil
}

// Read returns immutable positional chunk bytes, EAGAIN, ESTALE or exact EOF.
// It never waits for an operation. Native read splitting preserves byte identity.
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
	changed := false
	for i := k; i < end; i++ {
		if c.seen[j][i] == 0 {
			c.seen[j][i] = 1
			c.unread--
			changed = true
		}
	}
	if changed {
		if err := t.protectLocked(id); err != nil {
			return nil, err
		}
		if err := t.protectedPoint("read"); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// Close stops component workers after all dispatched calls finish. It does not
// retire claims, destroy the endpoint allocator or claim cache-domain exclusion.
func (t *Transport) Close() {
	t.mu.Lock()
	if !t.closed {
		t.closed = true
		close(t.ordinaryQueue)
		close(t.controlQueue)
		close(t.replacementQueue)
		close(t.drainQueue)
		close(t.acquisitionQueue)
	}
	t.mu.Unlock()
	t.workers.Wait()
	t.mu.Lock()
	if t.ledger != nil {
		_ = t.ledger.close()
		t.ledger = nil
	}
	t.mu.Unlock()
}
func (t *Transport) worker(queue <-chan job) {
	defer t.workers.Done()
	// The pending ring retains only already funded accepted jobs. Each cell has
	// one dispatch, so aggregate pending storage is bounded by the 80 cell credits
	// and independent replacement workspace; no retry allocates a claim or credit.
	pending := make([]job, 0, 81)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var j job
		retry := false
		// A due retry gets priority over a continuously replenished queue.
		// The pending ring itself is FIFO, so one refusal cannot starve peers.
		if len(pending) > 0 {
			select {
			case <-ticker.C:
				retry = true
			default:
			}
		}
		if !retry {
			select {
			case next, ok := <-queue:
				if !ok {
					return
				}
				j = next
			case <-ticker.C:
				if len(pending) == 0 {
					continue
				}
				retry = true
			}
		}
		if retry {
			j = pending[0]
			copy(pending, pending[1:])
			pending[len(pending)-1] = job{}
			pending = pending[:len(pending)-1]
		}
		invocation := &workerInvocation{live: true}
		t.mu.Lock()
		c := t.cells[j.id]
		active := !t.closed && t.failed == nil && c != nil && !c.released && c.response == nil
		if active {
			c.jobPhase = "executing"
			c.invocation = invocation
			active = t.protectLocked(j.id) == nil
		}
		t.mu.Unlock()
		if !active {
			invocation.live = false
			continue
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
			retryable := errors.Is(e, fsapi.WouldBlock) || errors.Is(e, fsapi.NoSpace)
			t.mu.Lock()
			if c = t.cells[j.id]; c != nil && t.failed == nil {
				c.jobPhase = "blocked"
				if retryable {
					c.jobPhase = "pending"
				}
				_ = t.protectLocked(j.id)
			}
			t.mu.Unlock()
			if retryable {
				pending = append(pending, j)
			}
			continue
		}
		if len(raw) == 0 || len(raw) > BodyBytes {
			continue
		}
		kind := uint16(2)
		if j.command.Op == "VIEW_POLL" {
			kind = 3
		}
		frames := [][]byte{}
		seen := [][]byte{}
		hash := sha256.Sum256(raw)
		session := parseID(j.command.Context.Session)
		unread := 0
		valid := true
		for offset := 0; offset < len(raw); offset += ChunkBytes {
			wire, e := EncodeFrame(Frame{Minor: commandMinor(j.command), Flags: t.flags(j.id), Kind: kind, Total: uint32(len(raw)), Offset: uint32(offset), Session: session, Cell: j.id, Hash: hash, Data: raw[offset:min(offset+ChunkBytes, len(raw))]})
			if e != nil {
				valid = false
				break
			}
			frames = append(frames, wire)
			seen = append(seen, make([]byte, len(wire)))
			unread += len(wire)
		}
		if !valid {
			continue
		}
		t.mu.Lock()
		c = t.cells[j.id]
		if !t.closed && t.failed == nil && c != nil && !c.released && c.response == nil {
			if err := t.finishMappingFull(c, raw); err != nil {
				t.failed = err
				t.mu.Unlock()
				continue
			}
			c.response = raw
			c.wire = frames
			c.seen = seen
			c.unread = unread
			c.resultKind = kind
			c.jobPhase = "responded"
			if c.terminal != nil {
				c.terminal.Phase = "TERMINAL_RESPONDED"
			}
			if t.protectLocked(j.id) == nil {
				_ = t.protectedPoint("response")
			}
		}
		t.mu.Unlock()
	}
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
