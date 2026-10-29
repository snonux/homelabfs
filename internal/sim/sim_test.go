package sim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/transport"
)

type rig struct {
	s        *Scheduler
	network  *Network
	registry *cluster.Registry
	nodes    []*Node
	ctx      context.Context
}
type dialResult struct {
	conn *transport.Conn
	err  error
}
type allowFixture struct{}

func (allowFixture) Check(_ cluster.Identity, _ transport.Frame, _ transport.Direction) error {
	return nil
}
func newRig(t *testing.T, seed uint64, count int, factory Factory) *rig {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	s, err := NewScheduler(Limits{Steps: 10000, Pending: 256, Bytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	network, err := NewNetwork(s, seed)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{s: s, network: network, ctx: ctx}
	cfg := cluster.Config{Cluster: UUID(seed, "cluster"), Revision: 1}
	for i := 0; i < count; i++ {
		cfg.Nodes = append(cfg.Nodes, cluster.Node{ID: UUID(seed, fmt.Sprintf("node/%d", i)), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.AddrFrom4([4]byte{192, 0, 2, byte(i + 1)})}})
	}
	r.registry, err = cluster.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range cfg.Nodes {
		n, e := NewNode(NodeConfig{Seed: seed, Registry: r.registry, Node: member.ID, Address: netip.AddrPortFrom(member.Addresses[0], 10000), Network: r.network, Scheduler: s, Factory: factory, Authorizer: allowFixture{}})
		if e != nil {
			t.Fatal(e)
		}
		if e = n.Restart(ctx); e != nil {
			t.Fatal(e)
		}
		r.nodes = append(r.nodes, n)
		t.Cleanup(func() {
			if e := n.Close(); e != nil {
				t.Error(e)
			}
		})
	}
	return r
}
func (r *rig) connect(t *testing.T, a, b int) (*transport.Conn, *transport.Conn) {
	t.Helper()
	result := make(chan dialResult, 1)
	go func() {
		c, e := r.nodes[a].runtime.Endpoint.Dial(r.ctx, r.nodes[b].cfg.Node, r.nodes[b].cfg.Address, transport.Control)
		result <- dialResult{c, e}
	}()
	// Full real transport handshake; exact boundaries remove goroutine arrival
	// ordering from delivery choice. Connection IDs follow controlled Dial order.
	return r.finishConnect(t, b, result)
}
func (r *rig) finishConnect(t *testing.T, b int, result <-chan dialResult) (*transport.Conn, *transport.Conn) {
	t.Helper()
	// All current tests use a fresh first connection per rig.
	for _, suffix := range []string{"0/write/1", "1/write/1", "1/write/2", "0/write/2", "1/write/3"} {
		if err := r.s.Step(r.ctx, "conn/1/"+suffix); err != nil {
			t.Fatal(err)
		}
	}
	var d dialResult
	select {
	case d = <-result:
	case <-r.ctx.Done():
		t.Fatal(r.ctx.Err())
	}
	if d.err != nil {
		t.Fatal(d.err)
	}
	accepted, err := r.nodes[b].runtime.Endpoint.Accept(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return d.conn, accepted
}
func deliver(t *testing.T, r *rig, key string) {
	t.Helper()
	if err := r.s.Await(r.ctx, key); err != nil {
		t.Fatal(err)
	}
	for r.s.Pending(key) {
		if err := r.s.Step(r.ctx, key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSeededTracesIgnoreGoroutineScheduling(t *testing.T) {
	run := func(seed uint64, yield bool) []Event {
		r := newRig(t, seed, 3, nil)
		a, b := r.connect(t, 0, 1)
		from, to := r.nodes[0].cfg.Address.Addr(), r.nodes[1].cfg.Address.Addr()
		if err := r.network.SetLink(from, to, Link{Loss: 450, Duplicate: 1000, Delay: time.Millisecond, Jitter: 5 * time.Millisecond, Retry: time.Millisecond, Attempts: 32}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			body := []byte(fmt.Sprintf("message %d", i))
			sent := make(chan error, 1)
			go func() {
				if yield {
					for j := 0; j < 7; j++ {
						runtime.Gosched()
					}
				}
				sent <- a.Send(r.ctx, transport.Ping, fsapi.UUID{}, UUID(seed, fmt.Sprint(i)), body)
			}()
			deliver(t, r, fmt.Sprintf("conn/1/0/write/%d", i+3))
			if err := <-sent; err != nil {
				t.Fatal(err)
			}
			f, err := b.Receive(r.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, f.Body) {
				t.Fatal("stream changed or duplicated bytes")
			}
		}
		return r.s.Trace()
	}
	a := run(77, false)
	for i := 0; i < 5; i++ {
		if got := run(77, true); !reflect.DeepEqual(a, got) {
			t.Fatalf("seed replay changed\nfirst=%+v\nnext=%+v", a, got)
		}
	}
	if reflect.DeepEqual(a, run(78, true)) {
		t.Fatal("different seeds did not select different faults")
	}
}

func TestVirtualDeadlineStopsRealHandshake(t *testing.T) {
	r := newRig(t, 9, 2, nil)
	done := make(chan error, 1)
	go func() {
		_, err := r.nodes[0].runtime.Endpoint.Dial(r.ctx, r.nodes[1].cfg.Node, r.nodes[1].cfg.Address, transport.Control)
		done <- err
	}()
	if err := r.s.Await(r.ctx, "conn/1/0/write/1"); err != nil {
		t.Fatal(err)
	}
	// No packet delivery and no wall-clock sleep: timeout includes the handshake
	// context and the currently blocked real transport framing writer.
	if err := r.s.Advance(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked handshake succeeded")
		}
	case <-r.ctx.Done():
		t.Fatal("virtual deadline did not cancel handshake")
	}
}

func TestOneWayPartitionAndReset(t *testing.T) {
	r := newRig(t, 11, 2, nil)
	a, b := r.connect(t, 0, 1)
	from, to := r.nodes[0].cfg.Address.Addr(), r.nodes[1].cfg.Address.Addr()
	if err := r.network.SetLink(from, to, Link{Partition: true, Retry: time.Millisecond, Attempts: 10}); err != nil {
		t.Fatal(err)
	}
	sent := make(chan error, 1)
	go func() { sent <- a.Send(r.ctx, transport.Ping, fsapi.UUID{}, UUID(11, "a"), []byte("held")) }()
	if err := r.s.Step(r.ctx, "conn/1/0/write/3"); err != nil {
		t.Fatal(err)
	}
	reverse := make(chan error, 1)
	go func() { reverse <- b.Send(r.ctx, transport.Pong, fsapi.UUID{}, UUID(11, "b"), []byte("reverse")) }()
	deliver(t, r, "conn/1/1/write/4")
	if err := <-reverse; err != nil {
		t.Fatal(err)
	}
	frame, err := a.Receive(r.ctx)
	if err != nil || string(frame.Body) != "reverse" {
		t.Fatalf("reverse link: %v %+v", err, frame)
	}
	if err = r.network.SetLink(from, to, Link{Retry: time.Millisecond, Attempts: 10}); err != nil {
		t.Fatal(err)
	}
	deliver(t, r, "conn/1/0/write/3")
	if err = <-sent; err != nil {
		t.Fatal(err)
	}
	if _, err = b.Receive(r.ctx); err != nil {
		t.Fatal(err)
	}
	if err = r.network.Reset(1, 0); err != nil {
		t.Fatal(err)
	}
	if err = r.s.Step(r.ctx, "conn/1/0/reset"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Receive(r.ctx); err == nil {
		t.Fatal("reset connection remained usable")
	}
}

func TestClockAndFailClosedBudgets(t *testing.T) {
	c := NewClock()
	ctx, cancel := c.Transport().WithTimeout(context.Background(), time.Second)
	defer cancel()
	timer := c.Transport().NewTimer(2 * time.Second)
	if err := c.AdvanceTo(c.Time().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatal(ctx.Err())
	}
	if !timer.Stop() {
		t.Fatal("timer expired early")
	}
	s, err := NewScheduler(Limits{Steps: 1, Pending: 1, Bytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Do("one", "", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.Advance(time.Second); err == nil {
		t.Fatal("step budget did not fail closed")
	}
	if err = s.Do("after", "", func() error { t.Fatal("executed after failure"); return nil }); err == nil {
		t.Fatal("failed scheduler resumed")
	}
}

func TestReorderingAcrossReliableStreams(t *testing.T) {
	s, err := NewScheduler(Limits{Steps: 20, Pending: 16, Bytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	n, err := NewNetwork(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	a := netip.MustParseAddrPort("192.0.2.1:10000")
	b := netip.MustParseAddrPort("192.0.2.2:10000")
	listener, err := n.Listen(t.Context(), b)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	var senders, receivers []io.ReadWriteCloser
	for i := 0; i < 2; i++ {
		sender, e := n.Dial(t.Context(), a, b)
		if e != nil {
			t.Fatal(e)
		}
		receiver, e := listener.Accept()
		if e != nil {
			t.Fatal(e)
		}
		senders = append(senders, sender)
		receivers = append(receivers, receiver)
	}
	t.Cleanup(func() {
		for i := range senders {
			_ = senders[i].Close()
			_ = receivers[i].Close()
		}
	})
	done := make(chan error, 2)
	for i := range senders {
		go func() { _, e := senders[i].Write([]byte{byte(i + 1)}); done <- e }()
	}
	if err = s.Step(t.Context(), "conn/2/0/write/1"); err != nil {
		t.Fatal(err)
	}
	b2 := make([]byte, 1)
	if _, err = receivers[1].Read(b2); err != nil || b2[0] != 2 {
		t.Fatalf("reorder: %v %v", b2, err)
	}
	if err = s.Step(t.Context(), "conn/1/0/write/1"); err != nil {
		t.Fatal(err)
	}
	if _, err = receivers[0].Read(b2); err != nil || b2[0] != 1 {
		t.Fatalf("stream: %v %v", b2, err)
	}
	for i := 0; i < 2; i++ {
		if err = <-done; err != nil {
			t.Fatal(err)
		}
	}
}
