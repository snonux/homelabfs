package normalnode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/transport"
)

type loader struct{ cfg cluster.Config }

func (l loader) Load(context.Context) (cluster.Config, error) { return l.cfg, nil }

type observeNetwork struct {
	t        *testing.T
	dir      string
	listened bool
	fail     bool
}

func (n *observeNetwork) Listen(ctx context.Context, a netip.AddrPort) (net.Listener, error) {
	n.listened = true
	if _, err := os.Stat(filepath.Join(n.dir, "cluster.json")); err != nil {
		n.t.Fatal("listener before cluster binding")
	}
	entries, err := os.ReadDir(filepath.Join(n.dir, "incarnations"))
	if err != nil || len(entries) != 1 {
		n.t.Fatalf("listener before durable incarnation: %v", err)
	}
	if next, err := cluster.Start(ctx, n.dir, id(1), id(2)); err == nil {
		_ = next.Close()
		n.t.Fatal("listener without exclusive lifetime")
	}
	if n.fail {
		return nil, errors.New("injected bind failure")
	}
	return (transport.TCP{}).Listen(ctx, a)
}
func (n *observeNetwork) Dial(ctx context.Context, a, b netip.AddrPort) (net.Conn, error) {
	return (transport.TCP{}).Dial(ctx, a, b)
}

type closer func() error

func (c closer) Close() error { return c() }
func id(n byte) fsapi.UUID    { var u fsapi.UUID; u[15] = n; return u }
func setup(t *testing.T) (Config, *observeNetwork) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node.json"), []byte(fmt.Sprintf(`{"format_version":1,"node_id":%q}`, id(2).String())), 0o600); err != nil {
		t.Fatal(err)
	}
	network := &observeNetwork{t: t, dir: dir}
	return Config{ControlDir: dir, Node: id(2), Loader: loader{cluster.Config{Cluster: id(1), Revision: 1, Nodes: []cluster.Node{{ID: id(2), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("127.0.0.2")}}}}}, Transport: transport.Config{Local: netip.MustParseAddrPort("127.0.0.2:0"), Network: network, Limits: transport.Limits{Message: 4096, Queue: 4, Bytes: 16384, Operations: 4}, MaxConnections: 4, Timeout: time.Second}}, network
}
func TestStartupOrderingAndDrain(t *testing.T) {
	cfg, network := setup(t)
	n, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if !network.listened {
		t.Fatal("missing listener")
	}
	drained := false
	if err := n.Attach(closer(func() error {
		if _, err := n.Transport().Address(); err != nil {
			t.Fatal(err)
		}
		if next, err := cluster.Start(context.Background(), cfg.ControlDir, id(1), id(2)); err == nil {
			_ = next.Close()
			t.Fatal("lifetime released before service drain")
		}
		drained = true
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if !drained {
		t.Fatal("service not drained")
	}
	next, err := cluster.Start(context.Background(), cfg.ControlDir, id(1), id(2))
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
}
func TestFailedBindReleasesOnlyAfterSocketShutdown(t *testing.T) {
	cfg, network := setup(t)
	network.fail = true
	if n, err := Start(context.Background(), cfg); err == nil {
		_ = n.Close()
		t.Fatal("bind succeeded")
	}
	next, err := cluster.Start(context.Background(), cfg.ControlDir, id(1), id(2))
	if err != nil {
		t.Fatal(err)
	}
	_ = next.Close()
}
func TestUncertainDrainRetainsExclusiveLifetime(t *testing.T) {
	cfg, _ := setup(t)
	n, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	expected := errors.New("service drain unknown")
	if err := n.Attach(closer(func() error { return expected })); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := n.Close(); !errors.Is(err, expected) {
			t.Fatal(err)
		}
		if next, err := cluster.Start(context.Background(), cfg.ControlDir, id(1), id(2)); err == nil {
			_ = next.Close()
			t.Fatal("uncertain drain released local admission")
		}
	}
	// This fixture has no real running service. Release only for bounded test cleanup.
	_ = n.lifetime.Close()
}
