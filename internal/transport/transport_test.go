package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
)

type allow struct{}

func (allow) Check(cluster.Identity, Frame, Direction) error { return nil }

func uid(n byte) fsapi.UUID { var id fsapi.UUID; id[15] = n; return id }
func registry(t *testing.T, revision uint64) *cluster.Registry {
	t.Helper()
	r, err := cluster.New(cluster.Config{Cluster: uid(1), Revision: revision, Nodes: []cluster.Node{{ID: uid(2), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.2")}}, {ID: uid(3), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.3")}}}})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func config(t *testing.T, n byte) Config {
	return Config{Registry: registry(t, 1), Identity: cluster.Identity{Cluster: uid(1), Node: uid(n), Incarnation: uid(n + 10)}, Local: netip.MustParseAddrPort(fmt.Sprintf("127.0.0.%d:0", n)), Network: TCP{}, Limits: Limits{4096, 32, 65536, 32}, MaxConnections: 8, Timeout: time.Second, Authorizer: allow{}}
}
func endpoint(t *testing.T, cfg Config) *Endpoint {
	t.Helper()
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err = e.Listen(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}
func pair(t *testing.T) (*Endpoint, *Endpoint, *Conn, *Conn) {
	t.Helper()
	a, b := endpoint(t, config(t, 2)), endpoint(t, config(t, 3))
	c, d := connect(t, a, b, Control)
	return a, b, c, d
}
func connect(t *testing.T, a, b *Endpoint, class Class) (*Conn, *Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	addr, err := b.Address()
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Dial(ctx, b.cfg.Identity.Node, addr, class)
	if err != nil {
		t.Fatal(err)
	}
	d, err := b.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return c, d
}
func TestHandshakeClassesAndConcurrentWriters(t *testing.T) {
	a, b, c, d := pair(t)
	if c.Info().Session != d.Info().Session || c.Info().Incarnation != b.cfg.Identity.Incarnation {
		t.Fatal("identity/session")
	}
	for _, class := range []Class{Live, Bulk} {
		x, y := connect(t, a, b, class)
		if x.Info().Session != c.Info().Session || y.Info().Session != d.Info().Session {
			t.Fatal("data not control bound")
		}
	}
	const count = 20
	errc := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errc <- c.Send(context.Background(), Ping, fsapi.UUID{}, uid(byte(i+50)), bytes.Repeat([]byte{byte(i)}, 100))
		}(i)
	}
	seen := make(map[byte]bool)
	for i := 0; i < count; i++ {
		f, err := d.Receive(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Body) != 100 || !bytes.Equal(f.Body, bytes.Repeat(f.Body[:1], 100)) || seen[f.Body[0]] {
			t.Fatal("interleaved or repeated message")
		}
		seen[f.Body[0]] = true
	}
	wg.Wait()
	for i := 0; i < count; i++ {
		if err := <-errc; err != nil {
			t.Fatal(err)
		}
	}
}
func TestRevocationClosesAllClassesAndRegistryReplacement(t *testing.T) {
	a, b, c, d := pair(t)
	x, y := connect(t, a, b, Live)
	a.Revoke(uid(3))
	for _, conn := range []*Conn{c, x} {
		if err := conn.Send(context.Background(), Ping, fsapi.UUID{}, uid(50), nil); err == nil {
			t.Fatal("send after revoke")
		}
	}
	for _, conn := range []*Conn{d, y} {
		if _, err := conn.Receive(context.Background()); err == nil {
			t.Fatal("receive after revoke")
		}
	}
	addr, _ := b.Address()
	if _, err := a.Dial(context.Background(), uid(3), addr, Control); err == nil {
		t.Fatal("revoked reconnect")
	}
	if err := a.InstallRegistry(registry(t, 1)); err == nil {
		t.Fatal("nonmonotonic registry")
	}
	if err := a.InstallRegistry(registry(t, 2)); err != nil {
		t.Fatal(err)
	}
	if err := b.InstallRegistry(registry(t, 2)); err != nil {
		t.Fatal(err)
	}
	connect(t, a, b, Control)
}
func TestControlClosureDropsDataAndReconnectsFresh(t *testing.T) {
	a, b, c, _ := pair(t)
	x, _ := connect(t, a, b, Live)
	old := c.Info().Session
	_ = c.Close()
	if err := x.Send(context.Background(), Ping, fsapi.UUID{}, uid(50), nil); err == nil {
		t.Fatal("orphan data socket")
	}
	// Ensure the remote has observed closure before opening its replacement.
	deadline := time.Now().Add(time.Second)
	for b.currentSession(uid(2)) {
		if time.Now().After(deadline) {
			t.Fatal("remote control not closed")
		}
		time.Sleep(time.Millisecond)
	}
	n, _ := connect(t, a, b, Control)
	if n.Info().Session == old {
		t.Fatal("reused disconnected session")
	}
}
func (e *Endpoint) currentSession(peer fsapi.UUID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessions[peer] != nil
}
func TestDefaultDenyAndWrongClass(t *testing.T) {
	acfg := config(t, 2)
	acfg.Authorizer = nil
	a, b := endpoint(t, acfg), endpoint(t, config(t, 3))
	c, _ := connect(t, a, b, Control)
	if err := c.Send(context.Background(), OpenStream, uid(40), uid(41), nil); err == nil {
		t.Fatal("default allow")
	}
	if err := c.Send(context.Background(), Data, uid(40), uid(41), nil); err == nil {
		t.Fatal("data on control")
	}
}
func TestHandshakeMismatches(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"foreign cluster", func(c *Config) {
			r, err := cluster.New(cluster.Config{Cluster: uid(9), Revision: 1, Nodes: []cluster.Node{{ID: uid(2), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.2")}}, {ID: uid(3), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.3")}}}})
			if err != nil {
				t.Fatal(err)
			}
			c.Registry = r
			c.Identity.Cluster = uid(9)
		}},
		{"registry revision", func(c *Config) { c.Registry = registry(t, 2) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := endpoint(t, config(t, 2))
			bcfg := config(t, 3)
			tc.edit(&bcfg)
			b := endpoint(t, bcfg)
			addr, _ := b.Address()
			if c, err := a.Dial(context.Background(), uid(3), addr, Control); err == nil {
				_ = c.Close()
				t.Fatal("accepted mismatch")
			}
		})
	}
}
func TestNoDataWithoutControlAndDuplicateControl(t *testing.T) {
	a := endpoint(t, config(t, 2))
	b := endpoint(t, config(t, 3))
	addr, _ := b.Address()
	if _, err := a.Dial(context.Background(), uid(3), addr, Live); err == nil {
		t.Fatal("unbound live connection")
	}
	connect(t, a, b, Control)
	if _, err := a.Dial(context.Background(), uid(3), addr, Control); err == nil {
		t.Fatal("duplicate control")
	}
}
func TestObservedHostAndClaimMismatch(t *testing.T) {
	b := endpoint(t, config(t, 3))
	addr, _ := b.Address()
	d := net.Dialer{LocalAddr: net.TCPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.4:0"))}
	raw, err := d.DialContext(context.Background(), "tcp4", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	f := Frame{Kind: Hello, Class: Control, Connection: uid(40), Source: uid(2), SourceIncarnation: uid(12), Destination: uid(3), Correlation: uid(41), Body: encodeGreeting(greeting{1, config(t, 2).Limits, uid(1)})}
	data, err := Encode(f, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeAll(raw, data); err != nil {
		t.Fatal(err)
	}
	_ = raw.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = ReadFrame(raw, 4096); err == nil {
		t.Fatal("claimed foreign host")
	}
}
func TestStaleEnvelopeRejected(t *testing.T) {
	for _, field := range []string{"session", "connection", "source incarnation", "destination incarnation", "class", "binding"} {
		t.Run(field, func(t *testing.T) {
			_, _, c, d := pair(t)
			f := Frame{Kind: Ping, Class: Control, Session: c.session, Connection: c.connection, Source: c.endpoint.cfg.Identity.Node, SourceIncarnation: c.endpoint.cfg.Identity.Incarnation, Destination: c.peer, DestinationIncarnation: c.peerInc, Correlation: uid(40)}
			switch field {
			case "session":
				f.Session = uid(90)
			case "connection":
				f.Connection = uid(90)
			case "source incarnation":
				f.SourceIncarnation = uid(90)
			case "destination incarnation":
				f.DestinationIncarnation = uid(90)
			case "class":
				f.Class = Live
			case "binding":
				f.Kind = HelloOK
			}
			if err := c.sendFrame(context.Background(), f); err != nil {
				t.Fatal(err)
			}
			if _, err := d.Receive(context.Background()); err == nil {
				t.Fatal("stale envelope accepted")
			}
		})
	}
}
func TestCodecRoundtripAndPreallocationBound(t *testing.T) {
	f := Frame{Kind: Ping, Class: Control, Session: uid(1), Connection: uid(2), Source: uid(3), SourceIncarnation: uid(4), Destination: uid(5), DestinationIncarnation: uid(6), Correlation: uid(7), Body: []byte("immutable payload")}
	b, err := Encode(f, 4096)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadFrame(bytes.NewReader(b), 4096)
	if err != nil || !sameGreeting(decoded, f) {
		t.Fatalf("roundtrip %v", err)
	}
	for _, n := range []uint32{0, HeaderSize - 1, 4097, ^uint32(0)} {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], n)
		r := &prefixReader{data: prefix[:]}
		if _, err := ReadFrame(r, 4096); err == nil || r.extra {
			t.Fatalf("unbounded read %d", n)
		}
	}
	for _, offset := range []int{4, 8, 10, 12, 14, 15} {
		bad := bytes.Clone(b)
		bad[offset] = 255
		if _, err := Decode(bad, 4096); err == nil {
			t.Fatalf("invalid header byte %d", offset)
		}
	}
	if _, err := Decode(append(b, 0), 4096); err == nil {
		t.Fatal("trailing bytes")
	}
}

type prefixReader struct {
	data  []byte
	extra bool
}

func (r *prefixReader) Read(b []byte) (int, error) {
	if len(r.data) == 0 {
		r.extra = true
		return 0, io.EOF
	}
	n := copy(b, r.data)
	r.data = r.data[n:]
	return n, nil
}
func FuzzFrameDecoder(f *testing.F) {
	seed, err := Encode(Frame{Kind: Ping, Class: Control, Session: uid(1), Connection: uid(2), Source: uid(3), SourceIncarnation: uid(4), Destination: uid(5), DestinationIncarnation: uid(6), Correlation: uid(7), Body: []byte("seed")}, 4096)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte{0, 0, 0, 0})
	f.Add([]byte{255, 255, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data, MaxMessage)
		_, _ = ReadFrame(bytes.NewReader(data), 4096)
	})
}
