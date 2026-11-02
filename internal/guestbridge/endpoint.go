package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
	done                        chan struct{}
	workers                     sync.WaitGroup
	closed                      bool
}

// NewTransport reserves independent bounded ordinary/control workers. Admission
// must have reserved the fixed 12 MiB replacement parser workspace at enrollment;
// no backend/session may supply another allocator for this inode tuple.
func NewTransport(s *Service, budget *admission.Budget) (*Transport, error) {
	if s == nil || budget == nil {
		return nil, fsapi.Invalid
	}
	s.mu.Lock()
	if s.transportAttached {
		s.mu.Unlock()
		return nil, fsapi.Busy
	}
	s.transportAttached = true
	s.mu.Unlock()
	t := &Transport{service: s, cells: map[uint64]*cell{}, ordinaryQueue: make(chan job, 64), controlQueue: make(chan job, 16), replacementQueue: make(chan job, 1), drainQueue: make(chan job, 16), done: make(chan struct{})}
	// Reserve complete request/reply/receipt bitmap and bounded decode copies for
	// every active credit before any callback allocation. Workspace is independent.
	for _, pool := range []struct {
		class               admission.Class
		bytes, ops, workers uint64
	}{{admission.Live, 64 * 8 << 20, 64, 1}, {admission.Control, 16 * 8 << 20, 16, 2}, {admission.Memory, 12 << 20, 1, 1}} {
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
	t.workers.Add(4)
	go t.worker(t.ordinaryQueue)
	go t.worker(t.controlQueue)
	go t.worker(t.replacementQueue)
	go t.worker(t.drainQueue)
	return t, nil
}

// Write validates and copies one positional chunk without awaiting any service,
// recall, grant or vnode. An error never conveys filesystem success.
func (t *Transport) Write(ctx context.Context, n Native, node, offset uint64, b []byte) (uint32, error) {
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
	if t.closed {
		return 0, fsapi.Unavailable
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
				t.workspace = c
			} else {
				r := CellReservation{Control: f.Flags&1 != 0, Release: func() {}}
				if r.Control && t.control >= 16 || !r.Control && t.ordinary >= 64 {
					return 0, fsapi.WouldBlock
				}
				if t.incomplete(f.Session.String()) >= 16 {
					r.Release()
					return 0, fsapi.WouldBlock
				}
				c = newCell(f)
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
			if c == t.workspace {
				t.workspace = nil
			}
			return 0, fsapi.Invalid
		}
		return uint32(len(b)), nil
	}
	copy(c.body[f.Offset:], f.Data)
	c.chunks[index] = true
	c.count++
	if c.count != len(c.chunks) {
		return uint32(len(b)), nil
	}
	if sha256.Sum256(c.body) != f.Hash {
		if c == t.workspace {
			t.workspace = nil
		}
		return 0, fsapi.Invalid
	}
	command, e := DecodeCommand(c.body)
	if e != nil || command.Cell != f.Cell || command.Context != *scope {
		if c == t.workspace {
			t.workspace = nil
		}
		return 0, fsapi.Invalid
	}
	if e = t.service.CheckScope(command.Context); e != nil {
		return 0, e
	}
	control := controlCommand(command)
	if (command.Reclaim != nil) != (f.Flags&2 != 0) {
		if c == t.workspace {
			t.workspace = nil
		}
		return 0, fsapi.Permission
	}

	if c == t.workspace {
		if command.Reclaim == nil {
			t.workspace = nil
			return 0, fsapi.Invalid
		}
		target := t.cells[command.Reclaim.Cell]
		if target == nil || target.released || target.frame.Session != f.Session || target.unread != 0 || target.response == nil || digest(target.response) != command.Reclaim.ResponseHash || target.frame.Cell == f.Cell {
			t.workspace = nil
			return 0, fsapi.Permission
		}
		if command.Op != "RELEASE_CELL" && target.reservation.Control != control {
			t.workspace = nil
			return 0, fsapi.Permission
		}
		r := CellReservation{Control: f.Flags&1 != 0, Release: func() {}}
		if r.Control != target.reservation.Control {
			t.workspace = nil
			return 0, fsapi.Permission
		}
		queue := t.ordinaryQueue
		if command.Op == "RELEASE_CELL" {
			queue = t.replacementQueue
		} else if drainCommand(command) {
			queue = t.drainQueue
		} else if r.Control {
			queue = t.controlQueue
		}
		if len(queue) == cap(queue) {
			r.Release()
			t.workspace = nil
			return 0, fsapi.WouldBlock
		}
		releaseCell(target)
		delete(t.cells, command.Reclaim.Cell)
		c.reservation = r
		t.cells[f.Cell] = c
		t.high = f.Cell
		t.workspace = nil
	} else if c.reservation.Control != control || command.Reclaim != nil {
		return 0, fsapi.Permission
	}
	queue := t.ordinaryQueue
	if command.Op == "RELEASE_CELL" {
		queue = t.replacementQueue
	} else if drainCommand(command) {
		queue = t.drainQueue
	} else if c.reservation.Control {
		queue = t.controlQueue
	}
	if len(queue) == cap(queue) {
		return 0, fsapi.WouldBlock
	}
	c.command = &command
	queue <- job{id: f.Cell, command: command, native: cloneNative(n)}
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
	for i := k; i < end; i++ {
		if c.seen[j][i] == 0 {
			c.seen[j][i] = 1
			c.unread--
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
	}
	t.mu.Unlock()
	t.workers.Wait()
}
func (t *Transport) worker(queue <-chan job) {
	defer t.workers.Done()
	for j := range queue {
		raw, e := t.service.Execute(context.Background(), j.native, j.command)
		if e != nil {
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
			wire, e := EncodeFrame(Frame{Flags: t.flags(j.id), Kind: kind, Total: uint32(len(raw)), Offset: uint32(offset), Session: session, Cell: j.id, Hash: hash, Data: raw[offset:min(offset+ChunkBytes, len(raw))]})
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
		c := t.cells[j.id]
		if c != nil && !c.released && c.response == nil {
			c.response = raw
			c.wire = frames
			c.seen = seen
			c.unread = unread
			c.resultKind = kind
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
	return &cell{frame: f, body: make([]byte, int(f.Total)), chunks: make([]bool, (int(f.Total)+ChunkBytes-1)/ChunkBytes)}
}
func sameFrame(a, b Frame) bool {
	return a.Flags == b.Flags && a.Kind == b.Kind && a.Total == b.Total && a.Session == b.Session && a.Cell == b.Cell && a.Hash == b.Hash
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
	if c.Op == "CALL" {
		var call Call
		if json.Unmarshal(c.Payload, &call) != nil {
			return false
		}
		return call.Intent.Opcode == "DRAIN_WRITE"
	}
	return c.Op != "HELLO" && c.Op != "CALL"
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
