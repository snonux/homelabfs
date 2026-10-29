package sim

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/transport"
)

// Link faults apply to packets beneath a reliable ordered byte stream. Lost
// packets retransmit; duplicates are suppressed; reordering concerns independent
// sockets. Application message replay must be injected above this abstraction.
type Link struct {
	Partition       bool
	Loss, Duplicate uint16 // per thousand, 0..1000
	Delay           time.Duration
	Jitter          time.Duration
	Retry           time.Duration
	Attempts        uint16
}

// Network implements transport.Network with controlled packet deliveries.
// Dial calls and scenario commands must be serialized. Socket workers may run
// concurrently: packet identities and choices do not depend on their arrival.
type Network struct {
	mu          sync.Mutex
	scheduler   *Scheduler
	seed        uint64
	listeners   map[netip.AddrPort]*listener
	links       map[route]Link
	paused      map[netip.Addr]bool
	connections []*pipe
	next        uint64
	bytes       int
}
type route struct{ from, to netip.Addr }
type listener struct {
	network  *Network
	addr     netip.AddrPort
	incoming chan net.Conn
	done     chan struct{}
	once     sync.Once
}
type pipe struct {
	network *Network
	id      uint64
	ends    [2]*socket
}
type socket struct {
	waitClock                   <-chan struct{}
	writing                     bool
	waiting                     bool
	pipe                        *pipe
	side                        int
	local, remote               netip.AddrPort
	mu                          sync.Mutex
	buffer                      []byte
	closed, reset               bool
	readDeadline, writeDeadline time.Time
	changed                     chan struct{}
	sequence                    uint64
	writeMu                     sync.Mutex
}
type packet struct {
	key      string
	conn     *socket
	data     []byte
	result   chan error
	attempt  uint16
	released bool
}

type timeoutError struct{}

var _ transport.Network = (*Network)(nil)
var _ net.Conn = (*socket)(nil)
var _ net.Listener = (*listener)(nil)

// NewNetwork binds one seed and finite scheduler byte/connection budgets.
func NewNetwork(s *Scheduler, seed uint64) (*Network, error) {
	if s == nil {
		return nil, fmt.Errorf("missing simulation scheduler")
	}
	n := &Network{scheduler: s, seed: seed, listeners: make(map[netip.AddrPort]*listener), links: make(map[route]Link), paused: make(map[netip.Addr]bool)}
	s.mu.Lock()
	if len(s.networks) >= 16 {
		s.failed = fmt.Errorf("simulation network budget exhausted")
		s.signal()
		s.mu.Unlock()
		return nil, s.Err()
	} else {
		s.networks = append(s.networks, n)
	}
	s.mu.Unlock()
	return n, nil
}

// SetLink changes only the selected direction. Already scheduled packets retain
// their sampled delay; partition/loss are evaluated at their delivery boundary.
func (n *Network) SetLink(from, to netip.Addr, l Link) error {
	if !from.IsValid() || !to.IsValid() || l.Loss > 1000 || l.Duplicate > 1000 || l.Delay < 0 || l.Jitter < 0 || l.Delay > time.Hour || l.Jitter > time.Hour || l.Retry <= 0 || l.Retry > time.Hour || l.Attempts < 1 || l.Attempts > 1000 {
		return fmt.Errorf("invalid simulated link")
	}
	return n.scheduler.Do("link", fmt.Sprintf("%s>%s %+v", from, to, l), func() error { n.mu.Lock(); defer n.mu.Unlock(); n.links[route{from, to}] = l; return nil })
}

// Pause gates all socket reads and packet deliveries at this host. Controlled
// node commands also reject paused actors; it is not Go goroutine preemption.
func (n *Network) Pause(host netip.Addr, paused bool) error {
	return n.scheduler.Do("network-pause", fmt.Sprintf("%s %t", host, paused), func() error {
		n.mu.Lock()
		n.paused[host] = paused
		conns := append([]*pipe(nil), n.connections...)
		n.mu.Unlock()
		for _, p := range conns {
			for _, c := range p.ends {
				c.mu.Lock()
				c.signal()
				c.mu.Unlock()
			}
		}
		return nil
	})
}

// Listen registers an exact numeric address without opening OS sockets.
func (n *Network) Listen(ctx context.Context, a netip.AddrPort) (net.Listener, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if !a.IsValid() || a.Port() == 0 || len(n.listeners) >= 256 {
		return nil, fmt.Errorf("invalid/full simulated listener")
	}
	if _, ok := n.listeners[a]; ok {
		return nil, fmt.Errorf("simulated address in use")
	}
	l := &listener{network: n, addr: a, incoming: make(chan net.Conn, n.scheduler.limits.Pending), done: make(chan struct{})}
	n.listeners[a] = l
	return l, nil
}

// Dial creates one stable connection ID in controlled caller order. The caller
// drives its HELLO packets through Step; no handshake/authentication is replaced.
func (n *Network) Dial(ctx context.Context, local, remote netip.AddrPort) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	l := n.listeners[remote]
	if l == nil || len(n.connections) >= n.scheduler.limits.Pending {
		return nil, fmt.Errorf("simulated connection refused/capacity")
	}
	n.next++
	if local.Port() == 0 {
		local = netip.AddrPortFrom(local.Addr(), uint16(20000+n.next%40000))
	}
	p := &pipe{network: n, id: n.next}
	p.ends[0] = &socket{pipe: p, local: local, remote: remote, changed: make(chan struct{})}
	p.ends[1] = &socket{pipe: p, side: 1, local: remote, remote: local, changed: make(chan struct{})}
	select {
	case l.incoming <- p.ends[1]:
		n.connections = append(n.connections, p)
		return p.ends[0], nil
	default:
		return nil, fmt.Errorf("simulated accept capacity")
	}
}

// Reset schedules an observable connection failure. Closing one local actor
// never immediately tells a surviving peer that its RAM has disappeared.
func (n *Network) Reset(id uint64, side int) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range n.connections {
		if p.id == id && side >= 0 && side < 2 {
			return p.ends[side].enqueueReset()
		}
	}
	return fmt.Errorf("unknown simulated connection")
}
func (n *Network) link(c *socket) Link {
	n.mu.Lock()
	defer n.mu.Unlock()
	l, ok := n.links[route{c.local.Addr(), c.remote.Addr()}]
	if !ok {
		l = Link{Retry: time.Millisecond, Attempts: 100}
	}
	return l
}
func (n *Network) isPaused(a netip.Addr) bool { n.mu.Lock(); defer n.mu.Unlock(); return n.paused[a] }
func (n *Network) sample(key string, salt uint64) uint64 {
	h := fnv.New64a()
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n.seed^salt)
	_, _ = h.Write(b[:])
	_, _ = h.Write([]byte(key))
	return h.Sum64()
}
func (l *listener) Accept() (net.Conn, error) {
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case c := <-l.incoming:
		return c, nil
	}
}
func (l *listener) Close() error {
	l.once.Do(func() { l.network.mu.Lock(); delete(l.network.listeners, l.addr); l.network.mu.Unlock(); close(l.done) })
	return nil
}
func (l *listener) Addr() net.Addr     { return net.TCPAddrFromAddrPort(l.addr) }
func (c *socket) LocalAddr() net.Addr  { return net.TCPAddrFromAddrPort(c.local) }
func (c *socket) RemoteAddr() net.Addr { return net.TCPAddrFromAddrPort(c.remote) }
func (c *socket) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.pipe.network.mu.Lock()
	c.pipe.network.bytes -= len(c.buffer)
	c.pipe.network.mu.Unlock()
	c.buffer = nil
	c.signal()
	c.mu.Unlock()
	return c.enqueueReset()
}
func (c *socket) enqueueReset() error {
	key := fmt.Sprintf("conn/%d/%d/reset", c.pipe.id, c.side)
	return c.pipe.network.scheduler.enqueue(key, scheduled{at: c.pipe.network.scheduler.clock.Time(), detail: "remote reset observation", run: func() error {
		peer := c.pipe.ends[1-c.side]
		peer.mu.Lock()
		peer.reset = true
		peer.signal()
		peer.mu.Unlock()
		return nil
	}})
}
func (c *socket) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	for {
		now, clockChanged := c.pipe.network.scheduler.clock.notification()
		paused := c.pipe.network.isPaused(c.local.Addr())
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return 0, net.ErrClosed
		}
		if !paused {
			if len(c.buffer) > 0 {
				n := copy(b, c.buffer)
				c.buffer = c.buffer[n:]
				if len(c.buffer) == 0 {
					c.buffer = nil
				}
				c.pipe.network.mu.Lock()
				c.pipe.network.bytes -= n
				c.pipe.network.mu.Unlock()
				c.mu.Unlock()
				return n, nil
			}
			if c.reset {
				c.mu.Unlock()
				return 0, io.EOF
			}
			if !c.readDeadline.IsZero() && !now.Before(c.readDeadline) {
				c.mu.Unlock()
				return 0, timeoutError{}
			}
		}
		c.notify()
		c.waiting = true
		c.waitClock = clockChanged
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-changed:
		case <-clockChanged:
		}
		c.mu.Lock()
		c.waiting = false
		c.mu.Unlock()
	}
}
func (c *socket) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	if c.closed || c.reset {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.writing = true
	defer func() { c.mu.Lock(); c.writing = false; c.mu.Unlock() }()
	c.sequence++
	seq := c.sequence
	c.mu.Unlock()
	n := c.pipe.network
	n.mu.Lock()
	if len(b) > n.scheduler.limits.Bytes-n.bytes {
		n.mu.Unlock()
		return 0, n.scheduler.fail(fmt.Errorf("simulation byte budget exhausted"))
	}
	n.bytes += len(b)
	n.mu.Unlock()
	p := &packet{key: fmt.Sprintf("conn/%d/%d/write/%d", c.pipe.id, c.side, seq), conn: c, data: append([]byte(nil), b...), result: make(chan error, 1)}
	if err := p.enqueue(false); err != nil {
		p.release()
		return 0, err
	}
	for {
		select {
		case err := <-p.result:
			if err != nil {
				return 0, err
			}
			return len(b), nil
		default:
		}
		now, clockChanged := n.scheduler.clock.notification()
		c.mu.Lock()
		closed := c.closed
		deadline := c.writeDeadline
		changed := c.changed
		c.mu.Unlock()
		if closed {
			return 0, net.ErrClosed
		}
		if !n.isPaused(c.local.Addr()) && !deadline.IsZero() && !now.Before(deadline) {
			return 0, timeoutError{}
		}
		select {
		case err := <-p.result:
			if err != nil {
				return 0, err
			}
			return len(b), nil
		case <-changed:
		case <-clockChanged:
		}
	}
}
func (c *socket) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.writeDeadline = t
	c.signal()
	return nil
}
func (c *socket) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readDeadline = t
	c.signal()
	return nil
}
func (c *socket) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeDeadline = t
	c.signal()
	return nil
}
func (c *socket) signal() { c.waiting = false; c.notify() }
func (c *socket) notify() { close(c.changed); c.changed = make(chan struct{}) }
func (p *packet) enqueue(retry bool) error {
	n := p.conn.pipe.network
	l := n.link(p.conn)
	delay := l.Delay
	if l.Jitter > 0 {
		delay += time.Duration(n.sample(p.key, uint64(p.attempt)) % uint64(l.Jitter+1))
	}
	if retry {
		delay += l.Retry
	}
	return n.scheduler.enqueue(p.key, scheduled{at: n.scheduler.clock.Time().Add(delay), detail: fmt.Sprintf("packet attempt=%d", p.attempt+1), run: p.deliver})
}
func (p *packet) deliver() error {
	c := p.conn
	n := c.pipe.network
	l := n.link(c)
	dropped := l.Partition || n.isPaused(c.local.Addr()) || n.isPaused(c.remote.Addr()) || n.sample(p.key, uint64(p.attempt)+1000)%1000 < uint64(l.Loss)
	if dropped {
		if err := n.scheduler.Do(p.key+"/loss", "packet lost or link unavailable", func() error { return nil }); err != nil {
			return err
		}
		p.attempt++
		if p.attempt < l.Attempts {
			return p.enqueue(true)
		}
		p.release()
		p.result <- timeoutError{}
		return nil
	}
	// Retransmission and duplicate packets never duplicate stream bytes. Fault
	// choices use stable identity, rather than consuming a shared PRNG sequence.
	duplicate := n.sample(p.key, 2000)%1000 < uint64(l.Duplicate)
	peer := c.pipe.ends[1-c.side]
	peer.mu.Lock()
	if !peer.closed {
		peer.buffer = append(peer.buffer, p.data...)
		peer.signal()
	}
	closed := peer.closed
	peer.mu.Unlock()
	if closed {
		p.release()
	} else {
		p.released = true
		p.data = nil
	}
	p.result <- nil
	if duplicate {
		return n.scheduler.Do(p.key+"/duplicate", "packet duplicate suppressed", func() error { return nil })
	}
	return nil
}
func (p *packet) release() {
	if p.released {
		return
	}
	p.released = true
	n := p.conn.pipe.network
	n.mu.Lock()
	n.bytes -= len(p.data)
	n.mu.Unlock()
}

func (timeoutError) Error() string   { return "simulated I/O deadline exceeded" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// AwaitRead waits until an actual core socket reader has parked after installing
// its deadline. No goroutine sleep or empty-queue inference is involved. side 0
// is the dialer, side 1 the listener. The scenario must not deliver concurrently.
func (n *Network) AwaitRead(ctx context.Context, id uint64, side int) error {
	n.mu.Lock()
	var c *socket
	for _, p := range n.connections {
		if p.id == id && side >= 0 && side < 2 {
			c = p.ends[side]
		}
	}
	n.mu.Unlock()
	if c == nil {
		return fmt.Errorf("unknown simulated socket")
	}
	for {
		now, clockChanged := n.scheduler.clock.notification()
		paused := n.isPaused(c.local.Addr())
		c.mu.Lock()
		waiting := c.waiting && !paused && len(c.buffer) == 0 && !c.readDeadline.IsZero() && now.Before(c.readDeadline)
		if waiting {
			select {
			case <-c.waitClock:
				waiting = false
			default:
			}
		}
		closed, changed := c.closed || c.reset, c.changed
		c.mu.Unlock()
		if closed {
			return net.ErrClosed
		}
		if waiting {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		case <-clockChanged:
		}
	}
}

func (n *Network) expiresBy(at time.Time) bool {
	n.mu.Lock()
	connections := append([]*pipe(nil), n.connections...)
	n.mu.Unlock()
	for _, p := range connections {
		for _, c := range p.ends {
			if n.isPaused(c.local.Addr()) {
				continue
			}
			c.mu.Lock()
			due := !c.closed && ((c.waiting && !c.readDeadline.IsZero() && !c.readDeadline.After(at)) || (c.writing && !c.writeDeadline.IsZero() && !c.writeDeadline.After(at)))
			c.mu.Unlock()
			if due {
				return true
			}
		}
	}
	return false
}
