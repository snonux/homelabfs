package guestbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sys/unix"
)

// TransportLedgerMemoryBytes reserves ledger mappings, metadata and bounded
// serialization/transaction copies in addition to the ordinary transport arenas.
const TransportLedgerMemoryBytes = 1 << 30

const (
	transportCellLimit  = 8 << 20
	transportIndexLimit = 128 << 10
	// This is an operating ceiling checked before each transaction, not a claim
	// that bbolt's file can never exceed it during allocation/commit. There are
	// at most two bounded cell puts per transaction, no retained read transactions,
	// and AllocSize is reduced to 1 MiB. Uncertain failures fence the owner.
	transportFileCeiling = 768 << 20
)

var transportBucket = []byte("hfs-transport-1")
var transportIndexKey = []byte("index")
var transportWorkspaceKey = []byte("workspace")

// ProtectedTransportState is inert evidence, not an enrollment or restore token.
// The complement of Cells through High exactly records all released IDs (floor
// and holes), without a tombstone per historical cell. A workspace is unissued.
// Scopes and Protocols are parallel arrays retaining the exact selected tuples.
type ProtectedTransportState struct {
	RuntimeHellos     map[string]runtimeHelloWitness `json:",omitempty"`
	BridgeSessions    bool
	Version           uint32
	Endpoint          Endpoint
	Controller        fsapi.UUID
	Scopes            []Context
	Protocols         []*Protocol
	High              uint64
	Terminal          map[string]uint64 `json:",omitempty"`
	GuestHigh         map[string]uint64
	GuestRecent       []GuestIssuance
	Cells             []uint64
	Ordinary, Control int
	Workspace         bool
	Digest            string `json:"-"`
}

// ProtectedTransportCell retains exact transport bytes and historical callback
// observations. These values cannot construct a VerifiedRequest or grant native
// authority; a future coordinator must reconcile all other authoritative ledgers.
type ProtectedTransportCell struct {
	Header       Frame
	Body         []byte
	Chunks       []bool
	Observations []Native
	Count        int
	Control      bool
	Command      *Command
	Native       Native
	JobPhase     string
	// BlockedError is the error class (fsapi errno name or UNCLASSIFIED:<type>)
	// that left JobPhase "blocked". It is omitted otherwise, so rows written
	// before it existed and rows of non-blocked cells keep their exact bytes.
	BlockedError   string `json:",omitempty"`
	Response       []byte
	Wire, Seen     [][]byte
	Unread         int
	ResultKind     uint16
	GuestWatermark uint64
	Terminal       *ProtectedTerminal `json:",omitempty"`
	Mapping        *ProtectedMapping
}

type transportLedger struct {
	directory, lock *os.File
	parent          *os.File
	file            *os.File
	db              *bolt.DB
	// Private failure injection, never a successful authority callback.
	failpoint func(string) error
}

func createTransportLedger(root string) (*transportLedger, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	parentFD, err := unix.Open(filepath.Dir(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	parent := os.NewFile(uintptr(parentFD), filepath.Dir(root))
	if err = unix.Mkdirat(parentFD, filepath.Base(root), 0700); err != nil {
		_ = parent.Close()
		return nil, err
	}
	fd, err := unix.Openat(parentFD, filepath.Base(root), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	ledger, err := openTransportLedgerDirectory(root, fd, true)
	if err != nil {
		_ = parent.Close()
		return nil, err
	}
	// Keep the exact naming parent until the initial typed ledger has committed.
	// Constructor success must cover the namespace as well as file contents.
	ledger.parent = parent
	return ledger, nil
}

func openTransportLedger(root string, create bool) (_ *transportLedger, err error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return openTransportLedgerDirectory(root, fd, create)
}

func openTransportLedgerDirectory(root string, fd int, create bool, writable ...bool) (_ *transportLedger, err error) {
	l := &transportLedger{directory: os.NewFile(uintptr(fd), root)}
	write := create || len(writable) > 0 && writable[0]
	defer func() {
		if err != nil {
			_ = l.close()
		}
	}()
	if err = transportOwned(l.directory, true); err != nil {
		return nil, err
	}
	entries, err := l.directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	if create && len(entries) != 0 {
		return nil, fsapi.Stale
	}
	if !create {
		if len(entries) != 2 {
			return nil, fsapi.Stale
		}
		for _, e := range entries {
			if e.Name() != "transport.db" && e.Name() != "owner.lock" {
				return nil, fsapi.Stale
			}
		}
	}
	flags := unix.O_RDWR
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
	}
	l.lock, err = l.open("owner.lock", flags, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(l.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, err
	}
	// bbolt also owns the actual database lock. Pin the directory fd and reject
	// symlinks, hardlinks, foreign owners and nonprivate files before bbolt sees it.
	l.db, err = bolt.Open(filepath.Join(root, "transport.db"), 0600, &bolt.Options{
		Timeout: time.Millisecond, ReadOnly: !write,
		OpenFile: func(_ string, _ int, _ os.FileMode) (*os.File, error) {
			mode := unix.O_RDONLY
			if write {
				mode = unix.O_RDWR
			}
			if create {
				mode = unix.O_RDWR | unix.O_CREAT | unix.O_EXCL
			}
			f, err := l.open("transport.db", mode, 0600)
			if err == nil && !create {
				info, e := f.Stat()
				if e != nil {
					err = e
				} else if info.Size() > transportFileCeiling+(64<<20) {
					err = fsapi.NoSpace
				}
				if err != nil {
					_ = f.Close()
					return nil, err
				}
			}
			l.file = f
			return f, err
		},
	})
	if err != nil {
		return nil, err
	}
	l.db.AllocSize = 1 << 20
	if create {
		err = l.directory.Sync()
	}
	if err != nil {
		return nil, err
	}
	return l, nil
}

func (l *transportLedger) open(name string, flags int, mode uint32) (*os.File, error) {
	fd, err := unix.Openat(int(l.directory.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), filepath.Join(l.directory.Name(), name))
	if err = transportOwned(f, false); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
func transportOwned(f *os.File, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	kind := uint32(unix.S_IFREG)
	if directory {
		kind = unix.S_IFDIR
	}
	if st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 || uint32(st.Mode)&unix.S_IFMT != kind || !directory && st.Nlink != 1 {
		return fsapi.Permission
	}
	return nil
}

// finishNamespace runs after initial contents/root-directory protection and before
// any workers or successful constructor return. It uses the actual retained
// naming-parent fd: neither a reopened pathname nor a callback asserts durability.
func (l *transportLedger) finishNamespace() error {
	if l.parent == nil {
		return fsapi.Invalid
	}
	if err := l.parent.Sync(); err != nil {
		return err
	}
	if err := l.parent.Close(); err != nil {
		return err
	}
	l.parent = nil
	return nil
}

func (l *transportLedger) close() error {
	var err error
	if l.db != nil {
		err = l.db.Close()
		l.db = nil
	}
	if l.lock != nil {
		if e := l.lock.Close(); err == nil {
			err = e
		}
		l.lock = nil
	}
	if l.parent != nil {
		if e := l.parent.Close(); err == nil {
			err = e
		}
		l.parent = nil
	}
	if l.directory != nil {
		if e := l.directory.Close(); err == nil {
			err = e
		}
		l.directory = nil
	}
	return err
}
func transportCellKey(id uint64) []byte {
	key := make([]byte, 9)
	key[0] = 'c'
	binary.BigEndian.PutUint64(key[1:], id)
	return key
}
func protectedCell(c *cell) ProtectedTransportCell {
	h := c.frame
	// The complete body plus the chunk bitmap owns these bytes; keeping the first
	// frame's borrowed Data slice would duplicate a callback buffer and alias it.
	h.Data = nil
	return ProtectedTransportCell{Header: h, Body: c.body, Chunks: c.chunks, Observations: c.observations, Count: c.count, Control: c.reservation.Control, Command: c.command, Native: c.native, JobPhase: c.jobPhase, BlockedError: c.blockedError, Response: c.response, Wire: c.wire, Seen: c.seen, Unread: c.unread, ResultKind: c.resultKind, GuestWatermark: c.guestWatermark, Mapping: c.mapping, Terminal: c.terminal}
}
func (t *Transport) protectedIndex() ProtectedTransportState {
	s := ProtectedTransportState{RuntimeHellos: t.runtimeHellos, BridgeSessions: true, Version: 1, Endpoint: t.Endpoint(), Controller: t.service.controller, High: t.high, Terminal: t.terminal, GuestHigh: t.guestHigh, GuestRecent: t.guestRecent, Ordinary: t.ordinary, Control: t.control, Workspace: t.workspace != nil, Cells: make([]uint64, 0, len(t.cells))}
	for id := range t.cells {
		s.Cells = append(s.Cells, id)
	}
	slices.Sort(s.Cells)
	for _, scope := range t.service.config.Scopes {
		s.Scopes = append(s.Scopes, scope)
		s.Protocols = append(s.Protocols, t.service.config.sessionProtocol(scope.Session))
	}
	return s
}

// protectLocked uses bounded per-cell deltas. The index, changed cell, workspace
// and deleted reclaim target commit together. There is no append-only history,
// full-state JSON copy or reader held across writer transactions.
func (t *Transport) protectLocked(ids ...uint64) error {
	if t.ledger == nil {
		return nil
	}
	if t.failed != nil {
		return fsapi.Unavailable
	}
	if err := t.ledger.protect(t, ids); err != nil {
		t.failed = err
		return fmt.Errorf("transport protection fenced: %w", err)
	}
	return nil
}
func (t *Transport) protectedPoint(name string) error {
	if t.ledger != nil && t.ledger.failpoint != nil {
		if err := t.ledger.failpoint("protected-" + name); err != nil {
			t.failed = err
			return err
		}
	}
	return nil
}
func (l *transportLedger) protect(t *Transport, ids []uint64) error {
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() >= transportFileCeiling {
		return fsapi.NoSpace
	}
	if len(ids) > 2 && !t.terminalDeletionDelta(ids) {
		return fsapi.Invalid
	}
	if len(t.guestRecent)+t.mappingIncomplete() > mappingRecentLimit {
		return fsapi.NoSpace
	}
	index := t.protectedIndex()
	if err = validateTransportIndex(index); err != nil {
		return err
	}
	raw, err := json.Marshal(index)
	if err != nil || len(raw) > transportIndexLimit {
		return fsapi.NoSpace
	}
	// At most one new/changed live cell plus one workspace. A reclaimed ID is a
	// delete, so serialized cell puts per transaction never exceed 16 MiB.
	rows, err := t.service.protectedSessions()
	if err != nil {
		return err
	}
	for _, id := range ids {
		key := string(transportCellKey(id))
		rows[key] = nil
		if c := t.cells[id]; c != nil {
			value := protectedCell(c)
			if err = validateTransportCellBinding(index, value, false); err != nil {
				return err
			}
			rows[key], err = boundedTransportJSON(value)
			if err != nil {
				return err
			}
		}
	}
	rows[string(transportWorkspaceKey)] = nil
	if t.workspace != nil {
		value := protectedCell(t.workspace)
		if err = validateTransportCellBinding(index, value, true); err != nil {
			return err
		}
		rows[string(transportWorkspaceKey)], err = boundedTransportJSON(value)
		if err != nil {
			return err
		}
	}
	if l.failpoint != nil {
		if err = l.failpoint("before-commit"); err != nil {
			return err
		}
	}
	err = l.db.Update(func(tx *bolt.Tx) error {
		b, e := tx.CreateBucketIfNotExists(transportBucket)
		if e != nil {
			return e
		}
		if e = b.Put(transportIndexKey, raw); e != nil {
			return e
		}
		for key, value := range rows {
			if value == nil {
				e = b.Delete([]byte(key))
			} else {
				e = b.Put([]byte(key), value)
			}
			if e != nil {
				return e
			}
		}
		return nil
	})
	if err == nil && l.failpoint != nil {
		err = l.failpoint("after-commit")
	}
	return err
}
func boundedTransportJSON(c ProtectedTransportCell) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(c)
	raw := bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	if err != nil {
		return nil, err
	}
	if len(raw) > transportCellLimit {
		return nil, fsapi.NoSpace
	}
	return raw, nil
}
func validateTransportIndex(s ProtectedTransportState) error {
	if s.Version != 1 || !validEndpoint(s.Endpoint) || !s.Controller.Valid() || s.High > MaxCell || len(s.Cells) > 80 || len(s.Scopes) == 0 || len(s.Scopes) > 64 || len(s.Protocols) != len(s.Scopes) || s.Ordinary < 0 || s.Ordinary > 64 || s.Control < 0 || s.Control > 16 || s.Ordinary+s.Control != len(s.Cells) {
		return fsapi.Stale
	}
	if err := validateRuntimeHellos(s); err != nil {
		return err
	}
	if len(s.Terminal) > 64 {
		return fsapi.Stale
	}
	for session, id := range s.Terminal {
		known := false
		for i, scope := range s.Scopes {
			if scope.Session == session && s.Protocols[i] != nil && *s.Protocols[i] == TerminalProtocol() {
				known = true
			}
		}
		if !known || id == 0 || !slices.Contains(s.Cells, id) {
			return fsapi.Stale
		}
	}
	if len(s.GuestHigh) > 64 || len(s.GuestRecent) > mappingRecentLimit {
		return fsapi.Stale
	}
	for session, high := range s.GuestHigh {
		known := false
		for i, scope := range s.Scopes {
			if scope.Session == session && s.Protocols[i] != nil && IsMappedProtocol(*s.Protocols[i]) {
				known = true
			}
		}
		if !known || high > MaxCell {
			return fsapi.Stale
		}
	}
	seenGuest := map[GuestIssuance]bool{}
	for _, issued := range s.GuestRecent {
		if issued.Cell < 1 || issued.Cell > s.GuestHigh[issued.Session] || seenGuest[issued] {
			return fsapi.Stale
		}
		seenGuest[issued] = true
	}
	var previous uint64
	for _, id := range s.Cells {
		if id <= previous || id > s.High {
			return fsapi.Stale
		}
		previous = id
	}
	for i, scope := range s.Scopes {
		if scope.FuseSession != s.Endpoint.FuseSession {
			return fsapi.Stale
		}
		if p := s.Protocols[i]; p != nil && *p != ViewProtocol() && *p != ControlledProtocol() && *p != CleanupProtocol() && !IsMappedProtocol(*p) {
			return fsapi.Stale
		}
	}
	return nil
}

// blockedErrorLimit bounds the protected BlockedError diagnostic.
const blockedErrorLimit = 96

func validateProtectedCell(c ProtectedTransportCell, workspace bool) error {
	if c.Header.Cell < 1 || c.Header.Cell > MaxCell || c.Header.Total == 0 || c.Header.Total > BodyBytes || len(c.Header.Data) != 0 || len(c.Body) != int(c.Header.Total) || len(c.Chunks) != (len(c.Body)+ChunkBytes-1)/ChunkBytes || len(c.Observations) != len(c.Chunks) || len(c.Chunks) > 32 || c.Control != (c.Header.Flags&1 != 0) && !workspace {
		return fsapi.Stale
	}
	count := 0
	for i, present := range c.Chunks {
		if present {
			count++
			n := c.Observations[i]
			if len(n.Groups) > 1024 || !validUUID(n.Process) {
				return fsapi.Stale
			}
		}
	}
	if c.Count != count {
		return fsapi.Stale
	}
	if c.Command == nil {
		if c.JobPhase != "" || c.Response != nil {
			return fsapi.Stale
		}
	} else {
		decoded, err := DecodeCommandVersion(c.Body, c.Header.Minor)
		if err != nil || sha256.Sum256(c.Body) != c.Header.Hash || !reflect.DeepEqual(decoded, *c.Command) {
			return fsapi.Stale
		}
		if workspace || count != len(c.Chunks) || c.Command.Cell != c.Header.Cell || len(c.Native.Groups) > 1024 || !validUUID(c.Native.Process) || !oneOf(c.JobPhase, "accepted", "executing", "pending", "blocked", "responded") {
			return fsapi.Stale
		}
	}
	// Only a blocked cell may name its error class; legacy blocked rows have none.
	if c.BlockedError != "" && (c.JobPhase != "blocked" || len(c.BlockedError) > blockedErrorLimit) {
		return fsapi.Stale
	}
	if err := validateProtectedMapping(c); err != nil {
		return err
	}
	if c.Response == nil {
		if c.Wire != nil || c.Seen != nil || c.Unread != 0 || c.ResultKind != 0 || c.JobPhase == "responded" {
			return fsapi.Stale
		}
		return nil
	}
	if c.JobPhase != "responded" || len(c.Response) == 0 || len(c.Response) > BodyBytes || len(c.Wire) != (len(c.Response)+ChunkBytes-1)/ChunkBytes || len(c.Seen) != len(c.Wire) || c.ResultKind != 2 && c.ResultKind != 3 {
		return fsapi.Stale
	}
	unread := 0
	responseHash := sha256.Sum256(c.Response)
	for i, wire := range c.Wire {
		f, err := DecodeFrame(wire)
		if err != nil || f.Cell != c.Header.Cell || f.Session != c.Header.Session || f.Minor != c.Header.Minor || f.Flags != c.Header.Flags || f.Kind != c.ResultKind || int(f.Total) != len(c.Response) || f.Hash != responseHash || int(f.Offset) != i*ChunkBytes || !bytes.Equal(f.Data, c.Response[i*ChunkBytes:min((i+1)*ChunkBytes, len(c.Response))]) || len(c.Seen[i]) != len(wire) {
			return fsapi.Stale
		}
		for _, seen := range c.Seen[i] {
			if seen == 0 {
				unread++
			} else if seen != 1 {
				return fsapi.Stale
			}
		}
	}
	if unread != c.Unread {
		return fsapi.Stale
	}
	return nil
}

func decodeTransportJSON(raw []byte, limit int, value any) error {
	if len(raw) == 0 || len(raw) > limit {
		return fsapi.Stale
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return fsapi.Stale
	}
	return nil
}

// InspectTransport visits protected cells in ID order, followed by the workspace,
// while exclusively owning the private root. It never constructs live objects or
// queues work. Callbacks must not reenter this ledger. One bounded row is decoded
// at a time; retaining callback values is the caller's separately funded cost.
// No result, even a matching digest, authorizes RESUME or cross-ledger restoration.
func InspectTransport(root string, visit func(ProtectedTransportCell) error) (ProtectedTransportState, error) {
	return InspectEndpoint(root, visit, nil)
}

// InspectEndpoint additionally visits complete bridge sessions when present.
// Like InspectTransport, all returned records remain inert and partial visits
// followed by an error must never be used for restoration.
func InspectEndpoint(root string, visit func(ProtectedTransportCell) error, sessionVisit func(Context, ProtectedBridgeSession) error) (state ProtectedTransportState, err error) {
	return inspectEndpoint(root, visit, sessionVisit, false)
}

// InspectRestorationEndpoint refuses cuts beyond the private barrier workspace
// before decoding or retaining any row. All returned records are still inert.
func InspectRestorationEndpoint(root string, visit func(ProtectedTransportCell) error, sessionVisit func(Context, ProtectedBridgeSession) error) (ProtectedTransportState, error) {
	return inspectEndpoint(root, visit, sessionVisit, true)
}

func inspectEndpoint(root string, visit func(ProtectedTransportCell) error, sessionVisit func(Context, ProtectedBridgeSession) error, restoration bool) (state ProtectedTransportState, err error) {
	l, err := openTransportLedger(root, false)
	if err != nil {
		return state, err
	}
	defer func() {
		if e := l.close(); err == nil {
			err = e
		}
	}()
	if restoration {
		if err = checkRestorationCapacity(l); err != nil {
			return state, err
		}
	}
	return inspectEndpointLedger(l, visit, sessionVisit)
}

func inspectEndpointLedger(l *transportLedger, visit func(ProtectedTransportCell) error, sessionVisit func(Context, ProtectedBridgeSession) error) (state ProtectedTransportState, err error) {
	info, err := l.file.Stat()
	if err != nil {
		return state, err
	}
	if info.Size() > transportFileCeiling+(64<<20) {
		return state, fsapi.NoSpace
	}
	err = l.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(transportBucket)
		if b == nil {
			return fsapi.Stale
		}
		roots := 0
		if e := tx.ForEach(func(_ []byte, _ *bolt.Bucket) error { roots++; return nil }); e != nil || roots != 1 {
			return fsapi.Stale
		}
		raw := b.Get(transportIndexKey)
		if e := decodeTransportJSON(raw, transportIndexLimit, &state); e != nil {
			return e
		}
		if e := validateTransportIndex(state); e != nil {
			return e
		}
		hash := sha256.New()
		_, _ = hash.Write(raw)
		expected := len(state.Cells) + 1
		if state.BridgeSessions {
			expected += len(state.Scopes)
		}
		if state.Workspace {
			expected++
		}
		count := 0
		if e := b.ForEach(func(_, v []byte) error {
			if v == nil {
				return fsapi.Stale
			}
			count++
			return nil
		}); e != nil {
			return e
		}
		if count != expected {
			return fsapi.Stale
		}
		if state.BridgeSessions {
			total := 0
			for _, scope := range state.Scopes {
				key := bridgeSessionKey(scope.Session)
				raw := b.Get(key)
				total += len(raw)
				if total > bridgeSessionLimit {
					return fsapi.NoSpace
				}
				var row ProtectedBridgeSession
				if e := decodeTransportJSON(raw, bridgeSessionLimit, &row); e != nil {
					return e
				}
				if e := validateBridgeSession(state, scope, row); e != nil {
					return e
				}
				_, _ = hash.Write(key)
				_, _ = hash.Write(raw)
				if sessionVisit != nil {
					if e := sessionVisit(scope, row); e != nil {
						return e
					}
				}
			}
		}
		ordinary, control, mappingPending := 0, 0, 0
		inspect := func(key []byte, workspace bool, id uint64) error {
			raw := b.Get(key)
			var c ProtectedTransportCell
			if e := decodeTransportJSON(raw, transportCellLimit, &c); e != nil {
				return e
			}
			if c.Header.Cell != id {
				return fsapi.Stale
			}
			if e := validateTransportCellBinding(state, c, workspace); e != nil {
				return e
			}
			if c.Header.Minor == 4 && c.Command == nil {
				mappingPending++
			}
			if !workspace {
				if c.Control {
					control++
				} else {
					ordinary++
				}
			}
			_, _ = hash.Write(key)
			_, _ = hash.Write(raw)
			if visit != nil {
				return visit(c)
			}
			return nil
		}
		for _, id := range state.Cells {
			if e := inspect(transportCellKey(id), false, id); e != nil {
				return e
			}
		}
		if state.Workspace {
			if e := inspect(transportWorkspaceKey, true, state.High+1); e != nil {
				return e
			}
		}
		if ordinary != state.Ordinary || control != state.Control || mappingPending+len(state.GuestRecent) > mappingRecentLimit {
			return fsapi.Stale
		}
		state.Digest = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
	return state, err
}

func validateTransportCellBinding(s ProtectedTransportState, c ProtectedTransportCell, workspace bool) error {
	if witness, ok := s.RuntimeHellos[c.Header.Session.String()]; ok && witness.Cell == c.Header.Cell {
		if workspace || c.Command == nil || c.Command.Op != "HELLO" || c.JobPhase != "responded" || digest(c.Body) != witness.CommandHash || digest(c.Response) != witness.ResponseHash {
			return fsapi.Stale
		}
	}
	if err := validateTerminalCell(s, c, workspace); err != nil {
		return err
	}
	if err := validateProtectedCell(c, workspace); err != nil {
		return err
	}
	var scope *Context
	var protocol *Protocol
	for i := range s.Scopes {
		if s.Scopes[i].Session == c.Header.Session.String() {
			scope = &s.Scopes[i]
			protocol = s.Protocols[i]
			break
		}
	}
	if scope == nil {
		return fsapi.Stale
	}
	minor := uint16(0)
	if protocol != nil {
		minor = uint16(protocol.BridgeMinor)
	}
	if c.GuestWatermark > s.GuestHigh[c.Header.Session.String()] {
		return fsapi.Stale
	}
	if c.Header.Minor != minor || c.Header.Kind != 1 || c.Header.Flags > 3 || c.Header.Offset%ChunkBytes != 0 || c.Header.Offset >= c.Header.Total {
		return fsapi.Stale
	}
	for i, present := range c.Chunks {
		if present && c.Observations[i].Endpoint != s.Endpoint {
			return fsapi.Stale
		}
	}
	if err := validateMappingChain(s, c); err != nil {
		return err
	}
	if c.Command != nil && (c.Command.Context != *scope || c.Native.Endpoint != s.Endpoint || !reflect.DeepEqual(c.Command.Protocol, protocol)) {
		return fsapi.Stale
	}
	return nil
}

func openWritableTransportLedger(root string) (*transportLedger, error) {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return openTransportLedgerDirectory(root, fd, false, true)
}
