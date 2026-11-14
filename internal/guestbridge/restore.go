package guestbridge

import (
	"context"
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// EndpointRestoreVerifier is an additional trusted native boundary. A runtime
// implementation must consume a private single-use restart grant over this
// exact cut, full paired ledgers and fresh physical continuity for EVERY scope.
// Enroll remains once-only; decoded records never select this authority.
type EndpointRestoreVerifier interface {
	RestoreEndpoint(context.Context, ProtectedTransportState, Config) error
}

// RestorationFunding transfers existing arena ownership across component reopen.
// Only the actual quiesced old transport can create it. It is not a native proof.
type RestorationFunding struct {
	mu          sync.Mutex
	arenas      []*admission.Lease
	index       ProtectedTransportState
	root        string
	file        os.FileInfo
	directory   os.FileInfo
	predecessor *Transport
	native      NativeVerifier
	attestor    Attestor
	used        bool
}

type endpointRestore struct {
	index    ProtectedTransportState
	sessions map[string]ProtectedBridgeSession
	cells    []ProtectedTransportCell
	ledger   *transportLedger
	funding  *RestorationFunding
}

// Suspend closes admission, joins all old workers and relinquishes its writer.
// The returned finite funding keeps all retained credits charged. Failure leaves
// old resources fenced. Native continuity and guest/exporter quiescence must be
// established separately before a caller starts replacing semantic owners.
func (t *Transport) Suspend() (*RestorationFunding, error) {
	t.stopWorkers()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ledger == nil || len(t.arenas) == 0 || t.failed != nil {
		return nil, fsapi.Unavailable
	}
	if err := checkRestorationCapacity(t.ledger); err != nil {
		return nil, err
	}
	index, err := inspectEndpointLedger(t.ledger, func(c ProtectedTransportCell) error {
		if c.Command == nil || c.JobPhase != "responded" {
			return fsapi.Unavailable
		}
		return nil
	}, func(_ Context, row ProtectedBridgeSession) error {
		var ss session
		if err := decodeTransportJSON(row.State, bridgeSessionLimit, &ss); err != nil {
			return err
		}
		return restorableSession(&ss)
	})
	if err != nil {
		return nil, err
	}
	if index.Controller != t.service.controller || index.Endpoint != t.Endpoint() || !index.BridgeSessions {
		return nil, fsapi.Unavailable
	}
	file, err := t.ledger.file.Stat()
	if err != nil {
		return nil, err
	}
	named, err := t.ledger.open("transport.db", unix.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	namedInfo, err := named.Stat()
	closeErr := named.Close()
	if err != nil || closeErr != nil || !os.SameFile(file, namedInfo) {
		return nil, fsapi.Stale
	}
	directory, err := t.ledger.directory.Stat()
	if err != nil {
		return nil, err
	}
	f := &RestorationFunding{arenas: t.arenas, index: index, root: t.ledger.directory.Name(), file: file, directory: directory, predecessor: t, native: t.service.config.Native, attestor: t.service.config.Attestor}
	if err = t.ledger.close(); err != nil {
		return nil, err
	}
	t.ledger = nil
	t.arenas = nil
	// The protected rows now own bytes; their original arenas transfer rather
	// than retaining a second full decoded graph through the predecessor.
	t.cells = nil
	t.workspace = nil
	t.uncertainReclaims = nil
	t.guestHigh = nil
	t.guestHoles = nil
	t.refused = nil
	t.guestRecent = nil
	t.terminal = nil
	t.service.mu.Lock()
	t.service.sessions = nil
	t.service.mu.Unlock()
	return f, nil
}

// Root is the actual retained ledger name; callers cannot select another cut.
func (f *RestorationFunding) Root() string {
	if f == nil {
		return ""
	}
	return f.root
}

// State is inert exact original index evidence for paired-ledger reconciliation.
func (f *RestorationFunding) State() ProtectedTransportState {
	if f == nil {
		return ProtectedTransportState{}
	}
	raw, _ := json.Marshal(f.index)
	var state ProtectedTransportState
	_ = json.Unmarshal(raw, &state)
	state.Digest = f.index.Digest
	return state
}

// RestorationConfig retains the original bridge's immutable construction policy.
// It is not an authorization to replace any native endpoint.
func (t *Transport) RestorationConfig() (Config, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil || t.ledger == nil {
		return Config{}, fsapi.Unavailable
	}
	c := t.service.config
	c.Scopes = append([]Context(nil), c.Scopes...)
	if c.Protocol != nil {
		protocol := *c.Protocol
		c.Protocol = &protocol
	}
	if c.SessionProtocols != nil {
		protocols := make(map[string]Protocol, len(c.SessionProtocols))
		for id, p := range c.SessionProtocols {
			protocols[id] = p
		}
		c.SessionProtocols = protocols
	}
	return c, nil
}

// Restores identifies only a privately funded successor of this quiesced object.
func (t *Transport) Restores(old *Transport) bool {
	return t != nil && old != nil && t.predecessor == old && t.Endpoint() == old.Endpoint() && t.service.controller == old.service.controller
}

// RestoreDurableEndpoint reconstructs a supported fully protected cut. It never
// executes saved commands. Unsupported partial/executing/workspace cases remain
// blocked; the trusted native verifier must independently authorize this exact
// complete ledger before the original controller is reconstructed.
func RestoreDurableEndpoint(ctx context.Context, c Config, root string, funding *RestorationFunding) (*Service, *Transport, error) {
	if funding == nil {
		return nil, nil, fsapi.Permission
	}
	funding.mu.Lock()
	defer funding.mu.Unlock()
	if funding.used {
		return nil, nil, fsapi.Permission
	}
	if !sameRestoreNative(c.Native, funding.native) || !reflect.DeepEqual(c.Attestor, funding.attestor) {
		return nil, nil, fsapi.Permission
	}
	full, err := filepath.Abs(root)
	if err != nil || full != funding.root {
		return nil, nil, fsapi.Stale
	}
	info, err := os.Stat(full + "/transport.db")
	dirInfo, dirErr := os.Stat(full)
	if err != nil || dirErr != nil || !os.SameFile(info, funding.file) || !os.SameFile(dirInfo, funding.directory) {
		return nil, nil, fsapi.Stale
	}
	funding.used = true // An uncertain attempt never recycles its recovery grant.
	r := &endpointRestore{funding: funding, sessions: map[string]ProtectedBridgeSession{}}
	index, err := InspectRestorationEndpoint(root, func(cell ProtectedTransportCell) error {
		if cell.JobPhase != "responded" || cell.Command == nil {
			return fsapi.Unavailable
		}
		r.cells = append(r.cells, cell)
		return nil
	}, func(scope Context, row ProtectedBridgeSession) error { r.sessions[scope.Session] = row; return nil })
	if err != nil {
		return nil, nil, err
	}
	r.index = index
	if index.Digest != funding.index.Digest || index.Controller != funding.index.Controller || index.Endpoint != c.Endpoint || index.Workspace || !index.BridgeSessions || !reflect.DeepEqual(index.Scopes, c.Scopes) {
		return nil, nil, fsapi.Stale
	}
	verifier, ok := c.Native.(EndpointRestoreVerifier)
	if !ok {
		return nil, nil, fsapi.Permission
	}
	if err = verifier.RestoreEndpoint(ctx, index, c); err != nil {
		return nil, nil, err
	}
	s, err := newService(ctx, c, r)
	if err != nil {
		return nil, nil, err
	}
	r.ledger, err = openWritableTransportLedger(root)
	if err != nil {
		return nil, nil, err
	}
	// Callbacks above may run arbitrary trusted owner code. Bind the ACTUAL
	// final writer descriptors, not path stats taken before those callbacks.
	file, fileErr := r.ledger.file.Stat()
	directory, dirErr := r.ledger.directory.Stat()
	if fileErr != nil || dirErr != nil || !os.SameFile(file, funding.file) || !os.SameFile(directory, funding.directory) {
		_ = r.ledger.close()
		return nil, nil, fsapi.Stale
	}
	if err = checkRestorationCapacity(r.ledger); err != nil {
		_ = r.ledger.close()
		return nil, nil, err
	}
	// The writer lock excludes changes between final inspection and installation.
	current, err := inspectEndpointLedger(r.ledger, nil, nil)
	if err != nil || current.Digest != index.Digest {
		_ = r.ledger.close()
		if err == nil {
			err = fsapi.Stale
		}
		return nil, nil, err
	}
	t, err := restoreTransport(s, r)
	if err != nil {
		_ = r.ledger.close()
		return nil, nil, err
	}
	funding.arenas = nil
	return s, t, nil
}

func (s *Service) restoreSessions(ctx context.Context, r *endpointRestore) error {
	store, ok := s.config.Claims.(interface {
		CheckRestoredBridge(context.Context, Context, fsapi.UUID, json.RawMessage) error
	})
	if !ok || s.config.Validation == nil {
		return fsapi.Permission
	}
	for i, scope := range s.config.Scopes {
		if !reflect.DeepEqual(s.config.sessionProtocol(scope.Session), r.index.Protocols[i]) {
			return fsapi.Stale
		}
		row, ok := r.sessions[scope.Session]
		if !ok {
			return fsapi.Stale
		}
		var ss session
		if err := decodeTransportJSON(row.State, bridgeSessionLimit, &ss); err != nil {
			return err
		}
		if ss.Reserved != 0 || ss.Generation != s.sessions[scope.Session].Generation || len(ss.Grants) > s.config.MaxCapabilityIdentities || liveGrants(ss.Grants) > s.config.MaxCapabilities || len(ss.Credentials) > s.config.MaxCredentialRevisions {
			return fsapi.Unavailable
		}
		if err := restorableSession(&ss); err != nil {
			return err
		}
		if err := store.CheckRestoredBridge(ctx, scope, s.controller, row.State); err != nil {
			return err
		}
		s.sessions[scope.Session] = &ss
	}
	return nil
}

func restoreTransport(s *Service, r *endpointRestore) (*Transport, error) {
	t := &Transport{predecessor: r.funding.predecessor, service: s, arenas: r.funding.arenas, ledger: r.ledger, high: r.index.High, ordinary: r.index.Ordinary, control: r.index.Control, runtimeHellos: r.index.RuntimeHellos, terminal: r.index.Terminal, guestHigh: r.index.GuestHigh, guestHoles: r.index.GuestHoles, refused: r.index.Refused, guestRecent: r.index.GuestRecent, cells: map[uint64]*cell{}}
	t.makeQueues()
	for _, v := range r.cells {
		c := &cell{frame: v.Header, body: v.Body, chunks: v.Chunks, observations: v.Observations, count: v.Count, reservation: CellReservation{Control: v.Control, Release: func() {}}, command: v.Command, native: v.Native, jobPhase: v.JobPhase, response: v.Response, wire: v.Wire, seen: v.Seen, unread: v.Unread, resultKind: v.ResultKind, guestWatermark: v.GuestWatermark, mapping: v.Mapping, terminal: v.Terminal}
		t.cells[c.frame.Cell] = c
	}
	s.transportAttached = true
	s.transport = t
	if p, ok := s.config.Verification.(MappedVerificationProducer); ok {
		if err := p.BindTransport(context.Background(), t); err != nil {
			return nil, err
		}
	}
	t.startWorkers()
	return t, nil
}

func sameRestoreNative(a, b NativeVerifier) bool {
	if a == nil || b == nil || reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}
	left, right := reflect.ValueOf(a), reflect.ValueOf(b)
	return left.Kind() == reflect.Pointer && left.Pointer() == right.Pointer()
}

func restorableSession(ss *session) error {
	if ss.Reserved != 0 {
		return fsapi.Unavailable
	}
	for _, g := range ss.Grants {
		if g == nil || !g.Released && (g.Handle != 0 || !g.NoLookup || !oneOf(g.Cap.Kind, "ROOT", "DOMAIN")) {
			return fsapi.Unavailable
		}
	}
	return nil
}

// The NEW barrier path retains decoded complete cuts, unlike streaming inert
// inspection. Four conservative decoded copies (64 MiB), two bounded inode
// inventories (90 MiB), moved native IDs (8 MiB), complete session copies
// (24 MiB) and parser/transaction workspace (28 MiB) fit the existing 256 MiB
// ledger headroom above its 768 MiB operating mapping ceiling. No normal C8
// capacity is reduced; larger cuts are unsupported for barriered restoration.
const restorationCutLimit = 16 << 20

func checkRestorationCapacity(l *transportLedger) error {
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > transportFileCeiling {
		return fsapi.NoSpace
	}
	return l.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(transportBucket)
		if b == nil {
			return fsapi.Stale
		}
		total := 0
		return b.ForEach(func(k, v []byte) error {
			total += len(k) + len(v)
			if total > restorationCutLimit {
				return fsapi.NoSpace
			}
			return nil
		})
	})
}
