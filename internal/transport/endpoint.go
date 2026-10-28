package transport

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// Direction distinguishes incoming claims from outgoing authorization checks.
type Direction uint8

const (
	Incoming Direction = iota + 1
	Outgoing
)

// Authorizer validates every stream message against installed protocol state,
// exact binding/history/authority, roles, readiness and retained evidence. It must
// be concurrency safe, bounded and nonblocking. No implementation means deny.
// Authentication alone never grants stream or workload rights.
type Authorizer interface {
	Check(cluster.Identity, Frame, Direction) error
}

// Config supplies fixed addresses and finite transport resources. Ports may be
// zero only for a local disposable listener. Identity must come from a held
// durable cluster.Lifetime in a normal node; use normalnode.Start to enforce it.
type Config struct {
	Registry       *cluster.Registry
	Identity       cluster.Identity
	Local          netip.AddrPort
	Network        Network
	Limits         Limits
	MaxConnections int
	Timeout        time.Duration
	Authorizer     Authorizer
}

// Endpoint owns sockets, not retained replication evidence or serving rights.
// Its registry and identity are fixed except for explicit registry replacement.
// Close drains all transport goroutines before returning.
type dialAttempt struct {
	peer   fsapi.UUID
	cancel context.CancelFunc
}

type Endpoint struct {
	dials         map[*dialAttempt]bool
	incarnations  map[fsapi.UUID]fsapi.UUID
	mu            sync.Mutex
	cfg           Config
	listener      net.Listener
	conns         map[*Conn]bool
	sessions      map[fsapi.UUID]*session
	revoked       map[fsapi.UUID]bool
	accepted      chan *Conn
	done          chan struct{}
	closed        bool
	workers       sync.WaitGroup
	closeOnce     sync.Once
	pendingDials  int
	networkCtx    context.Context
	cancelNetwork context.CancelFunc
}
type session struct {
	id, peerInc fsapi.UUID
	control     *Conn
}

// New validates all policy before opening any socket. It starts no listener.
func New(cfg Config) (*Endpoint, error) {
	if cfg.Registry == nil || cfg.Network == nil || !cfg.Identity.Incarnation.Valid() || cfg.Identity.Cluster != cfg.Registry.Cluster() || !validAddress(cfg.Local, true) || !cfg.Limits.valid() || cfg.MaxConnections < 1 || cfg.MaxConnections > 128 || uint64(cfg.MaxConnections)*cfg.Limits.Bytes > 128<<20 || cfg.Timeout <= 0 || cfg.Timeout > time.Minute {
		return nil, fmt.Errorf("invalid transport configuration")
	}
	if err := cfg.Registry.CheckPeer(cfg.Identity.Cluster, cfg.Identity.Node, cfg.Local.Addr()); err != nil {
		return nil, err
	}
	networkCtx, cancelNetwork := context.WithCancel(context.Background())
	return &Endpoint{dials: make(map[*dialAttempt]bool), incarnations: make(map[fsapi.UUID]fsapi.UUID), networkCtx: networkCtx, cancelNetwork: cancelNetwork, cfg: cfg, conns: make(map[*Conn]bool), sessions: make(map[fsapi.UUID]*session), revoked: make(map[fsapi.UUID]bool), accepted: make(chan *Conn, cfg.MaxConnections), done: make(chan struct{})}, nil
}

// Listen binds only the configured address. Failed binds have no fallback.
func (e *Endpoint) Listen(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || e.listener != nil {
		return fmt.Errorf("endpoint closed or already listening")
	}
	listenCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	l, err := e.cfg.Network.Listen(listenCtx, e.cfg.Local)
	if err != nil {
		return err
	}
	addr, err := observed(l.Addr())
	if err != nil || addr.Addr() != e.cfg.Local.Addr() || (e.cfg.Local.Port() != 0 && addr.Port() != e.cfg.Local.Port()) {
		_ = l.Close()
		return fmt.Errorf("network changed pinned listener")
	}
	e.listener = l
	e.workers.Add(1)
	go e.acceptLoop(l)
	return nil
}

// Address returns the actual pinned listener, including an assigned test port.
func (e *Endpoint) Address() (netip.AddrPort, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.listener == nil {
		return netip.AddrPort{}, fmt.Errorf("endpoint is not listening")
	}
	return observed(e.listener.Addr())
}

// Accept returns the next fully authenticated connection. Stream traffic is
// still denied until the caller's Authorizer approves its exact binding.
func (e *Endpoint) Accept(ctx context.Context) (*Conn, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-e.done:
			return nil, net.ErrClosed
		case c := <-e.accepted:
			if e.current(c) {
				return c, nil
			}
		}
	}
}

// Dial authenticates a fresh socket to an exact approved node address. A control
// session must already exist for live/bulk connections; no data socket enrolls it.
func (e *Endpoint) Dial(ctx context.Context, peer fsapi.UUID, remote netip.AddrPort, class Class) (*Conn, error) {
	if class < Control || class > Bulk || !validAddress(remote, false) {
		return nil, fmt.Errorf("invalid class or remote address")
	}
	e.mu.Lock()
	if e.closed || e.revoked[peer] {
		e.mu.Unlock()
		return nil, net.ErrClosed
	}
	if err := e.cfg.Registry.CheckPeer(e.cfg.Identity.Cluster, peer, remote.Addr()); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	if len(e.conns)+e.pendingDials >= e.cfg.MaxConnections {
		e.mu.Unlock()
		return nil, fmt.Errorf("connection capacity")
	}
	dialCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	attempt := &dialAttempt{peer, cancel}
	e.dials[attempt] = true
	e.pendingDials++
	e.workers.Add(1)
	local, network, networkCtx := e.cfg.Local, e.cfg.Network, e.networkCtx
	e.mu.Unlock()
	defer func() { e.mu.Lock(); delete(e.dials, attempt); e.pendingDials--; e.mu.Unlock(); e.workers.Done() }()
	defer cancel()
	stopCancel := context.AfterFunc(networkCtx, cancel)
	defer stopCancel()
	raw, err := network.Dial(dialCtx, local, remote)
	if err != nil {
		return nil, err
	}
	actual, err := observed(raw.RemoteAddr())
	if err != nil || actual != remote {
		_ = raw.Close()
		return nil, fmt.Errorf("network changed pinned destination")
	}
	c, err := e.attach(raw, true)
	if err != nil {
		return nil, err
	}
	if err := e.handshake(dialCtx, c, peer, class, true); err != nil {
		_ = c.Close()
		return nil, err
	}
	e.startReader(c)
	return c, nil
}

// Revoke immediately denies new sockets and closes every queued/active class for
// this node. Installing a newer registry explicitly resets local revocations.
func (e *Endpoint) Revoke(peer fsapi.UUID) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.revoked[peer] = true
	e.closePeerLocked(peer)
}

// ExpectIncarnation installs an externally reconciled peer-lifetime expectation
// and invalidates its old sockets. It cannot fence a daemon or certify retention.
// A changed UUID never replaces this expectation merely by arriving on TCP.
func (e *Endpoint) ExpectIncarnation(peer, incarnation fsapi.UUID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	n, ok := e.cfg.Registry.Node(peer)
	if e.closed || !ok || n.Admission != cluster.Admitted || !incarnation.Valid() {
		return fmt.Errorf("invalid peer incarnation expectation")
	}
	e.closePeerLocked(peer)
	e.incarnations[peer] = incarnation
	return nil
}

// InstallRegistry atomically invalidates all sessions, queued traffic and pending
// handshakes. Only a strictly newer snapshot of this cluster is accepted.
func (e *Endpoint) InstallRegistry(r *cluster.Registry) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || r == nil || r.Cluster() != e.cfg.Identity.Cluster || r.Revision() <= e.cfg.Registry.Revision() {
		return fmt.Errorf("invalid registry replacement")
	}
	if err := r.CheckPeer(r.Cluster(), e.cfg.Identity.Node, e.cfg.Local.Addr()); err != nil {
		return err
	}
	e.cancelNetwork()
	e.networkCtx, e.cancelNetwork = context.WithCancel(context.Background())
	for c := range e.conns {
		c.stop()
	}
	e.sessions = make(map[fsapi.UUID]*session)
	e.revoked = make(map[fsapi.UUID]bool)
	e.cfg.Registry = r
	return nil
}

// Close closes the listener and sockets and drains transport workers. It never
// discards retained acceptance obligations, selects history, or implies abort.
func (e *Endpoint) Close() error {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.cancelNetwork()
		close(e.done)
		if e.listener != nil {
			_ = e.listener.Close()
		}
		for c := range e.conns {
			c.stop()
		}
		e.sessions = make(map[fsapi.UUID]*session)
		e.mu.Unlock()
		e.workers.Wait()
	})
	return nil
}

func (e *Endpoint) attach(raw net.Conn, reserved bool) (*Conn, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	local, err := observed(raw.LocalAddr())
	pending := e.pendingDials
	if reserved {
		pending--
	}
	if e.closed || err != nil || local.Addr() != e.cfg.Local.Addr() || len(e.conns)+pending >= e.cfg.MaxConnections {
		_ = raw.Close()
		return nil, fmt.Errorf("closed, unpinned or full endpoint")
	}
	c := newConn(e, raw, e.cfg.Limits, e.cfg.Timeout)
	e.conns[c] = true
	e.workers.Add(1)
	go c.writeLoop()
	return c, nil
}
func (e *Endpoint) acceptLoop(l net.Listener) {
	defer e.workers.Done()
	for {
		raw, err := l.Accept()
		if err != nil {
			return
		}
		c, err := e.attach(raw, false)
		if err != nil {
			continue
		}
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			_ = c.Close()
			return
		}
		e.workers.Add(1)
		e.mu.Unlock()
		go func() {
			defer e.workers.Done()
			ctx, cancel := context.WithTimeout(context.Background(), e.cfg.Timeout)
			defer cancel()
			if err := e.handshake(ctx, c, fsapi.UUID{}, 0, false); err != nil {
				_ = c.Close()
				return
			}
			e.startReader(c)
			select {
			case e.accepted <- c:
			case <-e.done:
				_ = c.Close()
			case <-c.done:
			}
		}()
	}
}
func (e *Endpoint) current(c *Conn) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentLocked(c)
}
func (e *Endpoint) currentLocked(c *Conn) bool {
	s := e.sessions[c.peer]
	return !e.closed && !e.revoked[c.peer] && e.conns[c] && s != nil && s.id == c.session && s.peerInc == c.peerInc && !c.stopped()
}
func (e *Endpoint) detach(c *Conn) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.conns, c)
	if s := e.sessions[c.peer]; s != nil && s.control == c {
		e.closePeerLocked(c.peer)
	}
}
func (e *Endpoint) closePeerLocked(peer fsapi.UUID) {
	delete(e.sessions, peer)
	for attempt := range e.dials {
		if attempt.peer == peer {
			attempt.cancel()
		}
	}
	for c := range e.conns {
		if c.peer == peer {
			c.stop()
		}
	}
}
func (e *Endpoint) authorize(c *Conn, f Frame, d Direction) error {
	e.mu.Lock()
	valid := e.currentLocked(c)
	a := e.cfg.Authorizer
	e.mu.Unlock()
	if !valid {
		return net.ErrClosed
	}
	if f.Kind == Hello || f.Kind == HelloOK {
		return fmt.Errorf("handshake on established connection")
	}
	if f.Kind >= OpenStream && f.Kind <= StatusReply {
		if a == nil {
			return fmt.Errorf("stream authorization unavailable")
		}
		if err := a.Check(cluster.Identity{Cluster: e.cfg.Identity.Cluster, Node: c.peer, Incarnation: c.peerInc}, f, d); err != nil {
			return fmt.Errorf("stream denied: %w", err)
		}
	}
	if !e.current(c) {
		return net.ErrClosed
	}
	return nil
}

func (e *Endpoint) startReader(c *Conn) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.currentLocked(c) {
		c.stop()
		return
	}
	e.workers.Add(1)
	go c.readLoop()
}
