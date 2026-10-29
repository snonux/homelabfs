package transport

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type writeRequest struct {
	data   []byte
	result chan error
}

// Conn is one authenticated class within a control-owned session. Send has one
// framing writer; Receive serializes readers. A successful Send means socket
// delivery only, never receipt, durability, acceptance or serving readiness.
type Conn struct {
	endpoint         *Endpoint
	raw              net.Conn
	limits           Limits
	timeout          time.Duration
	queue            chan writeRequest
	done, writerDone chan struct{}
	incoming         chan Frame
	once             sync.Once

	budgetMu                           sync.Mutex
	queuedBytes                        uint64
	queuedOps                          uint32
	peer, peerInc, session, connection fsapi.UUID
	class                              Class
}

// Info is an immutable authenticated connection description.
type Info struct {
	Peer, Incarnation, Session, Connection fsapi.UUID
	Class                                  Class
	Limits                                 Limits
}

func newConn(e *Endpoint, raw net.Conn, limits Limits, timeout time.Duration) *Conn {
	return &Conn{endpoint: e, raw: raw, limits: limits, timeout: timeout, queue: make(chan writeRequest, limits.Queue), done: make(chan struct{}), writerDone: make(chan struct{}), incoming: make(chan Frame)}
}

// Info returns negotiated identities; call only after Dial or Accept succeeds.
func (c *Conn) Info() Info {
	return Info{c.peer, c.peerInc, c.session, c.connection, c.class, c.limits}
}

// Send queues an owned copy with bounded bytes/operations. Capacity failure is
// immediate backpressure. Cancellation after queuing closes this socket because
// delivery may be partial; the operation's protocol outcome remains uncertain.
func (c *Conn) Send(ctx context.Context, kind Kind, binding, correlation fsapi.UUID, body []byte) error {
	id := c.endpoint.cfg.Identity
	f := Frame{Kind: kind, Class: c.class, Session: c.session, Connection: c.connection, Source: id.Node, SourceIncarnation: id.Incarnation, Destination: c.peer, DestinationIncarnation: c.peerInc, Correlation: correlation, Binding: binding, Body: body}
	if !allowedClass(kind, c.class) {
		return fmt.Errorf("message disallowed on connection class")
	}
	if err := c.endpoint.authorize(c, f, Outgoing); err != nil {
		return err
	}
	return c.sendFrame(ctx, f)
}

// Receive reads one complete envelope, checks current identity/session and asks
// the protocol Authorizer before dispatch. The returned body is owned by caller.
func (c *Conn) Receive(ctx context.Context) (Frame, error) {
	select {
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	case <-c.done:
		return Frame{}, net.ErrClosed
	case f := <-c.incoming:
		if err := c.endpoint.authorize(c, f, Incoming); err != nil {
			return Frame{}, err
		}
		return f, nil
	}
}

// Close invalidates this socket and drains its writer. Closing a control socket
// also invalidates all classes of its session. Replication retention is separate.
func (c *Conn) Close() error {
	c.stop()
	<-c.writerDone
	return nil
}

func (c *Conn) stopped() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
func (c *Conn) stop() { c.once.Do(func() { close(c.done); _ = c.raw.Close() }) }
func (c *Conn) reserve(n uint64) bool {
	c.budgetMu.Lock()
	defer c.budgetMu.Unlock()
	if c.queuedOps >= c.limits.Operations || c.queuedOps >= c.limits.Queue || n > c.limits.Bytes-c.queuedBytes {
		return false
	}
	c.queuedOps++
	c.queuedBytes += n
	return true
}
func (c *Conn) release(n uint64) {
	c.budgetMu.Lock()
	c.queuedOps--
	c.queuedBytes -= n
	c.budgetMu.Unlock()
}
func (c *Conn) sendFrame(ctx context.Context, f Frame) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateFrame(f, c.limits.Message); err != nil {
		return err
	}
	size := uint64(HeaderSize) + uint64(len(f.Body))
	if !c.reserve(size) {
		return fmt.Errorf("transport queue capacity")
	}
	b, err := Encode(f, c.limits.Message)
	if err != nil {
		c.release(size)
		return err
	}
	req := writeRequest{b, make(chan error, 1)}
	c.budgetMu.Lock()
	if c.stopped() {
		c.queuedOps--
		c.queuedBytes -= size
		c.budgetMu.Unlock()
		return net.ErrClosed
	}
	select {
	case c.queue <- req:
		c.budgetMu.Unlock()
	default:
		c.queuedOps--
		c.queuedBytes -= size
		c.budgetMu.Unlock()
		return fmt.Errorf("transport queue capacity")
	}

	select {
	case <-ctx.Done():
		c.stop()
		return ctx.Err()
	case <-c.done:
		return net.ErrClosed
	case err := <-req.result:
		return err
	}
}
func (c *Conn) writeLoop() {
	defer c.endpoint.workers.Done()
	defer close(c.writerDone)
	defer c.endpoint.detach(c)
	defer c.drainQueue()
	defer c.stop()
	for {
		select {
		case <-c.done:
			return
		case req := <-c.queue:
			if c.stopped() {
				c.release(uint64(len(req.data)))
				req.result <- net.ErrClosed
				return
			}
			err := c.raw.SetWriteDeadline(c.endpoint.cfg.Clock.Now().Add(c.timeout))
			if err == nil {
				err = writeAll(c.raw, req.data)
			}
			c.release(uint64(len(req.data)))
			req.result <- err
			if err != nil {
				return
			}
		}
	}
}
func (c *Conn) readFrame(ctx context.Context, limit uint32) (Frame, error) {
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	deadline := c.endpoint.cfg.Clock.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := c.raw.SetReadDeadline(deadline); err != nil {
		return Frame{}, err
	}
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = c.raw.SetReadDeadline(c.endpoint.cfg.Clock.Now()); close(finished) })
	defer func() {
		if !stop() {
			<-finished
		}
	}()
	f, err := ReadFrame(c.raw, limit)
	if ctx.Err() != nil {
		return Frame{}, ctx.Err()
	}
	return f, err
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func allowedClass(k Kind, c Class) bool {
	switch k {
	case Data:
		return c == Live || c == Bulk
	case ArtifactChunk:
		return c == Bulk
	case Error, Close, Ping, Pong:
		return true
	default:
		return c == Control
	}
}

func (c *Conn) readLoop() {
	defer c.endpoint.workers.Done()
	defer c.stop()
	for {
		f, err := c.readFrame(context.Background(), c.limits.Message)
		if err != nil {
			return
		}
		id := c.endpoint.cfg.Identity
		if f.Session != c.session || f.Connection != c.connection || f.Source != c.peer || f.SourceIncarnation != c.peerInc || f.Destination != id.Node || f.DestinationIncarnation != id.Incarnation || f.Class != c.class || !allowedClass(f.Kind, c.class) {
			return
		}
		if err := c.endpoint.authorize(c, f, Incoming); err != nil {
			return
		}
		deliveryDeadline := c.endpoint.cfg.Clock.NewTimer(c.timeout)
		select {
		case c.incoming <- f:
			deliveryDeadline.Stop()
		case <-c.done:
			deliveryDeadline.Stop()
			return
		case <-deliveryDeadline.C():
			return
		}
	}
}

func (c *Conn) drainQueue() {
	c.budgetMu.Lock()
	defer c.budgetMu.Unlock()
	for {
		select {
		case req := <-c.queue:
			c.queuedOps--
			c.queuedBytes -= uint64(len(req.data))
			req.result <- net.ErrClosed
		default:
			return
		}
	}
}
