package transport

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

type gatedNetwork struct {
	mu   sync.Mutex
	conn *gatedConn
}
type gatedConn struct {
	net.Conn
	mu            sync.Mutex
	gate, entered chan struct{}
	dead          chan struct{}
	once          sync.Once
}

func (n *gatedNetwork) Listen(ctx context.Context, a netip.AddrPort) (net.Listener, error) {
	return (TCP{}).Listen(ctx, a)
}
func (n *gatedNetwork) Dial(ctx context.Context, a, b netip.AddrPort) (net.Conn, error) {
	raw, err := (TCP{}).Dial(ctx, a, b)
	if err != nil {
		return nil, err
	}
	c := &gatedConn{Conn: raw, dead: make(chan struct{})}
	n.mu.Lock()
	n.conn = c
	n.mu.Unlock()
	return c, nil
}
func (c *gatedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	gate, entered := c.gate, c.entered
	c.mu.Unlock()
	if gate != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-gate:
		case <-c.dead:
			return 0, net.ErrClosed
		}
	}
	return c.Conn.Write(b)
}
func (c *gatedConn) Close() error { c.once.Do(func() { close(c.dead) }); return c.Conn.Close() }
func (c *gatedConn) hold() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
	c.entered = make(chan struct{}, 1)
}

func TestQueuedWriterCapacityAndRevocation(t *testing.T) {
	network := &gatedNetwork{}
	cfg := config(t, 2)
	cfg.Network = network
	cfg.Limits.Queue = 2
	cfg.Limits.Operations = 2
	a, b := endpoint(t, cfg), endpoint(t, config(t, 3))
	c, _ := connect(t, a, b, Control)
	network.mu.Lock()
	socket := network.conn
	network.mu.Unlock()
	socket.hold()
	results := make(chan error, 2)
	go func() { results <- c.Send(context.Background(), Ping, fsapi.UUID{}, uid(40), []byte("held writer")) }()
	select {
	case <-socket.entered:
	case <-time.After(time.Second):
		t.Fatal("writer not entered")
	}
	go func() { results <- c.Send(context.Background(), Ping, fsapi.UUID{}, uid(41), []byte("queued writer")) }()
	deadline := time.Now().Add(time.Second)
	for {
		c.budgetMu.Lock()
		n := c.queuedOps
		c.budgetMu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second write not queued")
		}
		time.Sleep(time.Millisecond)
	}
	if err := c.Send(context.Background(), Ping, fsapi.UUID{}, uid(42), nil); err == nil {
		t.Fatal("unbounded queue")
	}
	a.Revoke(uid(3))
	_ = c.Close()
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			t.Fatal("queued write survived revocation")
		}
	}
	c.budgetMu.Lock()
	defer c.budgetMu.Unlock()
	if c.queuedBytes != 0 || c.queuedOps != 0 {
		t.Fatal("revoked queue retained owned buffers")
	}
}

type blockedNetwork struct {
	entered chan struct{}
	mu      sync.Mutex
	calls   int
}

func (n *blockedNetwork) Listen(ctx context.Context, a netip.AddrPort) (net.Listener, error) {
	return (TCP{}).Listen(ctx, a)
}
func (n *blockedNetwork) Dial(ctx context.Context, _, _ netip.AddrPort) (net.Conn, error) {
	n.mu.Lock()
	n.calls++
	n.mu.Unlock()
	close(n.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestPendingDialCapacityAndShutdown(t *testing.T) {
	network := &blockedNetwork{entered: make(chan struct{})}
	cfg := config(t, 2)
	cfg.Network = network
	cfg.MaxConnections = 1
	a := endpoint(t, cfg)
	result := make(chan error, 1)
	go func() {
		_, err := a.Dial(context.Background(), uid(3), netip.MustParseAddrPort("127.0.0.3:1"), Control)
		result <- err
	}()
	<-network.entered
	if _, err := a.Dial(context.Background(), uid(3), netip.MustParseAddrPort("127.0.0.3:1"), Control); err == nil {
		t.Fatal("unbounded pending dial")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("shutdown did not cancel dial")
	}
	network.mu.Lock()
	defer network.mu.Unlock()
	if network.calls != 1 {
		t.Fatal("capacity checked after dialing")
	}
}
func TestReconnectPolicyAndCancellation(t *testing.T) {
	cfg := config(t, 2)
	a := endpoint(t, cfg)
	b := endpoint(t, config(t, 3))
	addr, _ := b.Address()
	c, err := a.Reconnect(context.Background(), uid(3), addr, Control, Backoff{time.Millisecond, 2 * time.Millisecond, 3})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Reconnect(ctx, uid(3), addr, Control, Backoff{time.Millisecond, 2 * time.Millisecond, 3}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.Reconnect(context.Background(), uid(3), addr, Control, Backoff{}); err == nil {
		t.Fatal("unbounded backoff accepted")
	}
}
func TestIncarnationChangeRequiresExplicitExpectation(t *testing.T) {
	a, b, c, _ := pair(t)
	_ = c.Close()
	_ = b.Close()
	cfg := config(t, 3)
	cfg.Identity.Incarnation = uid(33)
	replacement := endpoint(t, cfg)
	addr, _ := replacement.Address()
	if c, err := a.Dial(context.Background(), uid(3), addr, Control); err == nil {
		_ = c.Close()
		t.Fatal("new UUID automatically superseded old lifetime")
	}
	if err := a.ExpectIncarnation(uid(3), uid(33)); err != nil {
		t.Fatal(err)
	}
	c, _ = connect(t, a, replacement, Control)
	_ = c.Close()
	_ = replacement.Close()
	old := endpoint(t, config(t, 3))
	addr, _ = old.Address()
	if c, err := a.Dial(context.Background(), uid(3), addr, Control); err == nil {
		_ = c.Close()
		t.Fatal("stale incarnation returned")
	}
}
func TestHistoricalCreatorBytesPreserved(t *testing.T) {
	a, b, _, _ := pair(t)
	c, d := connect(t, a, b, Bulk)
	raw, err := os.ReadFile("../journal/record/testdata/open.hex")
	if err != nil {
		t.Fatal(err)
	}
	historical, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	r, err := record.Decode(historical)
	if err != nil {
		t.Fatal(err)
	}
	if r.Header.Incarnation == a.cfg.Identity.Incarnation {
		t.Fatal("fixture must differ from forwarding incarnation")
	}
	if err = c.Send(context.Background(), Data, uid(40), uid(41), historical); err != nil {
		t.Fatal(err)
	}
	f, err := d.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.SourceIncarnation != a.cfg.Identity.Incarnation || !bytes.Equal(f.Body, historical) {
		t.Fatal("rewrote historical creator")
	}
}

type blockingAuthorizer struct {
	once             sync.Once
	entered, release chan struct{}
}

func (a *blockingAuthorizer) Check(_ cluster.Identity, _ Frame, _ Direction) error {
	a.once.Do(func() { close(a.entered) })
	<-a.release
	return nil
}
func TestRevocationDuringAuthorizationFailsClosed(t *testing.T) {
	auth := &blockingAuthorizer{entered: make(chan struct{}), release: make(chan struct{})}
	cfg := config(t, 2)
	cfg.Authorizer = auth
	a, b := endpoint(t, cfg), endpoint(t, config(t, 3))
	c, _ := connect(t, a, b, Control)
	result := make(chan error, 1)
	go func() { result <- c.Send(context.Background(), OpenStream, uid(40), uid(41), nil) }()
	<-auth.entered
	a.Revoke(uid(3))
	close(auth.release)
	if err := <-result; err == nil {
		t.Fatal("authorization outlived revocation")
	}
}

type wrongBind struct{}

func (wrongBind) Listen(ctx context.Context, _ netip.AddrPort) (net.Listener, error) {
	return (TCP{}).Listen(ctx, netip.MustParseAddrPort("127.0.0.1:0"))
}
func (wrongBind) Dial(context.Context, netip.AddrPort, netip.AddrPort) (net.Conn, error) {
	return nil, errors.New("unexpected dial")
}
func TestPinnedListenerNeverFallsBack(t *testing.T) {
	cfg := config(t, 2)
	cfg.Network = wrongBind{}
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	if err = e.Listen(context.Background()); err == nil {
		t.Fatal("accepted changed listener interface")
	}
	for _, address := range []string{"0.0.0.0:0", "[::]:0", "224.0.0.1:0", "[::ffff:127.0.0.2]:0", "192.0.2.1:0"} {
		cfg := config(t, 2)
		cfg.Local = netip.MustParseAddrPort(address)
		if _, err := New(cfg); err == nil {
			t.Fatalf("unpinned configuration %s", address)
		}
	}
}
func TestNegotiatesLowerFiniteLimits(t *testing.T) {
	cfg := config(t, 3)
	cfg.Limits = Limits{Message: 2048, Queue: 4, Bytes: 8192, Operations: 3}
	a, b := endpoint(t, config(t, 2)), endpoint(t, cfg)
	c, d := connect(t, a, b, Control)
	if c.Info().Limits != cfg.Limits || d.Info().Limits != cfg.Limits {
		t.Fatal("limits not negotiated down")
	}
	local, err := observed(c.raw.LocalAddr())
	if err != nil || local.Addr() != netip.MustParseAddr("127.0.0.2") {
		t.Fatal("outgoing source was not pinned")
	}
	if err := c.Send(context.Background(), Ping, fsapi.UUID{}, uid(40), make([]byte, 2048)); err == nil {
		t.Fatal("negotiated maximum ignored")
	}
}
func TestRevokeCancelsPendingDial(t *testing.T) {
	network := &blockedNetwork{entered: make(chan struct{})}
	cfg := config(t, 2)
	cfg.Network = network
	a := endpoint(t, cfg)
	result := make(chan error, 1)
	go func() {
		_, err := a.Dial(context.Background(), uid(3), netip.MustParseAddrPort("127.0.0.3:1"), Control)
		result <- err
	}()
	<-network.entered
	a.Revoke(uid(3))
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("revoked dial succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("pending dial outlived revocation")
	}
}

func TestStalledControlHandoffCannotHideDisconnect(t *testing.T) {
	acfg, bcfg := config(t, 2), config(t, 3)
	acfg.Timeout = 100 * time.Millisecond
	bcfg.Timeout = 100 * time.Millisecond
	a, b := endpoint(t, acfg), endpoint(t, bcfg)
	control, remote := connect(t, a, b, Control)
	data, _ := connect(t, a, b, Live)
	if err := remote.Send(context.Background(), Ping, fsapi.UUID{}, uid(40), []byte("deliberately not received")); err != nil {
		t.Fatal(err)
	}
	// a never calls Receive. Its reader must invalidate control/data even while
	// delivery of this valid control frame waits for the application.
	_ = remote.raw.Close()
	deadline := time.After(time.Second)
	for a.currentSession(uid(3)) {
		select {
		case <-deadline:
			t.Fatal("stalled dispatch concealed control loss")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := control.Send(context.Background(), Ping, fsapi.UUID{}, uid(41), nil); err == nil {
		t.Fatal("control survived stalled disconnect")
	}
	if err := data.Send(context.Background(), Ping, fsapi.UUID{}, uid(42), nil); err == nil {
		t.Fatal("data outlived control session")
	}
}
