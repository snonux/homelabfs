package cluster

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type fixedLoader struct {
	config Config
	err    error
}

func (l fixedLoader) Load(context.Context) (Config, error) { return l.config, l.err }

func TestRegistryImmutablePlacement(t *testing.T) {
	c := testConfig()
	r, err := Load(context.Background(), fixedLoader{config: c})
	if err != nil {
		t.Fatal(err)
	}
	if r.Cluster() != c.Cluster || r.Revision() != c.Revision {
		t.Fatal("registry identity")
	}
	volume := c.Volumes[0]
	for _, m := range volume.Members {
		if err := r.CheckAssignment(c.Cluster, m.Node, volume.ID, volume.History, m.Role); err != nil {
			t.Fatal(err)
		}
	}
	// Neither constructor inputs nor detached getter results can change policy.
	c.Nodes[0].Addresses[0] = netip.MustParseAddr("192.0.2.99")
	c.Volumes[0].Members[0].Role = Async
	n, _ := r.Node(testID(2))
	n.Addresses[0] = netip.MustParseAddr("192.0.2.98")
	v, _ := r.Volume(volume.ID)
	v.Members[0].Role = Async
	if err := r.CheckPeer(c.Cluster, testID(2), netip.MustParseAddr("192.0.2.1")); err != nil {
		t.Fatal(err)
	}
	if err := r.CheckAssignment(c.Cluster, testID(2), volume.ID, volume.History, Primary); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				n, _ := r.Node(testID(2))
				n.Addresses[0] = netip.Addr{}
				if err := r.CheckPeer(c.Cluster, testID(2), netip.MustParseAddr("192.0.2.1")); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

func TestRegistryRejectsInvalidSnapshots(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Config)
	}{
		{"cluster", func(c *Config) { c.Cluster = fsapi.UUID{} }},
		{"revision", func(c *Config) { c.Revision = 0 }},
		{"no nodes", func(c *Config) { c.Nodes = nil }},
		{"duplicate node", func(c *Config) { c.Nodes = append(c.Nodes, c.Nodes[0]) }},
		{"zero node", func(c *Config) { c.Nodes[0].ID = fsapi.UUID{} }},
		{"cluster node reuse", func(c *Config) { c.Nodes[0].ID = c.Cluster }},
		{"admission", func(c *Config) { c.Nodes[0].Admission = "" }},
		{"pending member", func(c *Config) { c.Nodes[0].Admission = Pending }},
		{"removed member", func(c *Config) { c.Nodes[0].Admission = Removed }},
		{"missing addresses", func(c *Config) { c.Nodes[0].Addresses = nil }},
		{"shared address", func(c *Config) { c.Nodes[1].Addresses = c.Nodes[0].Addresses }},
		{"repeated address", func(c *Config) { c.Nodes[0].Addresses = append(c.Nodes[0].Addresses, c.Nodes[0].Addresses[0]) }},
		{"unspecified", func(c *Config) { c.Nodes[0].Addresses[0] = netip.MustParseAddr("0.0.0.0") }},
		{"multicast", func(c *Config) { c.Nodes[0].Addresses[0] = netip.MustParseAddr("ff02::1") }},
		{"mapped IPv4", func(c *Config) { c.Nodes[0].Addresses[0] = netip.MustParseAddr("::ffff:192.0.2.1") }},
		{"zone", func(c *Config) { c.Nodes[0].Addresses[0] = netip.MustParseAddr("fe80::1%eth0") }},
		{"duplicate volume", func(c *Config) { c.Volumes = append(c.Volumes, c.Volumes[0]) }},
		{"zero volume", func(c *Config) { c.Volumes[0].ID = fsapi.UUID{} }},
		{"node volume reuse", func(c *Config) { c.Volumes[0].ID = c.Nodes[0].ID }},
		{"cluster volume reuse", func(c *Config) { c.Volumes[0].ID = c.Cluster }},
		{"zero generation", func(c *Config) { c.Volumes[0].History.Generation = fsapi.UUID{} }},
		{"zero configuration", func(c *Config) { c.Volumes[0].History.Configuration = fsapi.UUID{} }},
		{"zero epoch", func(c *Config) { c.Volumes[0].History.Epoch = 0 }},
		{"zero term", func(c *Config) { c.Volumes[0].History.CoordinatorTerm = 0 }},
		{"short group", func(c *Config) { c.Volumes[0].Members = c.Volumes[0].Members[:2] }},
		{"duplicate member", func(c *Config) { c.Volumes[0].Members[1].Node = c.Nodes[0].ID }},
		{"unknown member", func(c *Config) { c.Volumes[0].Members[0].Node = testID(88) }},
		{"duplicate role", func(c *Config) { c.Volumes[0].Members[1].Role = Primary }},
		{"unknown role", func(c *Config) { c.Volumes[0].Members[0].Role = "recovery" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			tc.change(&c)
			if _, err := New(c); err == nil {
				t.Fatal("accepted invalid registry")
			}
		})
	}
}

func TestRegistryDefaultDeny(t *testing.T) {
	c := testConfig()
	c.Nodes = append(c.Nodes, Node{ID: testID(9), Admission: Pending, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.9")}}, Node{ID: testID(10), Admission: Removed, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")}})
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	v := c.Volumes[0]
	for _, node := range []fsapi.UUID{testID(9), testID(10), testID(11), {}} {
		if err := r.CheckPeer(c.Cluster, node, netip.MustParseAddr("192.0.2.9")); err == nil {
			t.Fatal("admitted unknown/nonadmitted peer")
		}
	}
	if err := r.CheckPeer(testID(90), testID(2), netip.MustParseAddr("192.0.2.1")); err == nil {
		t.Fatal("foreign cluster")
	}
	if err := r.CheckPeer(c.Cluster, testID(2), netip.MustParseAddr("192.0.2.2")); err == nil {
		t.Fatal("address impersonation")
	}
	if err := r.CheckAssignment(c.Cluster, testID(2), testID(90), v.History, Primary); err == nil {
		t.Fatal("foreign volume")
	}
	if err := r.CheckAssignment(c.Cluster, testID(3), v.ID, v.History, Primary); err == nil {
		t.Fatal("replica granted primary placement")
	}
	for _, history := range []History{{Generation: testID(80), Configuration: v.History.Configuration, Epoch: 1, CoordinatorTerm: 1}, {Generation: v.History.Generation, Configuration: testID(80), Epoch: 1, CoordinatorTerm: 1}, {Generation: v.History.Generation, Configuration: v.History.Configuration, Epoch: 2, CoordinatorTerm: 1}, {Generation: v.History.Generation, Configuration: v.History.Configuration, Epoch: 1, CoordinatorTerm: 2}} {
		if err := r.CheckAssignment(c.Cluster, testID(2), v.ID, history, Primary); err == nil {
			t.Fatal("accepted different history")
		}
	}
	var empty Registry
	if err := empty.CheckPeer(c.Cluster, testID(2), netip.MustParseAddr("192.0.2.1")); err == nil {
		t.Fatal("zero registry admitted peer")
	}
	if _, ok := r.Node(testID(90)); ok {
		t.Fatal("unknown getter")
	}
	if _, ok := r.Volume(testID(90)); ok {
		t.Fatal("unknown getter")
	}
}

func TestAdmittedNodeHasNoImplicitVolumeAssignment(t *testing.T) {
	c := testConfig()
	c.Nodes = append(c.Nodes, Node{ID: testID(11), Admission: Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.11")}})
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CheckPeer(c.Cluster, testID(11), netip.MustParseAddr("192.0.2.11")); err != nil {
		t.Fatal(err)
	}
	v := c.Volumes[0]
	if err := r.CheckAssignment(c.Cluster, testID(11), v.ID, v.History, Async); err == nil {
		t.Fatal("new admitted node inherited a volume role")
	}
	// A cluster can bootstrap with no volumes while denying every volume request.
	c.Volumes = nil
	r, err = New(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CheckAssignment(c.Cluster, testID(2), v.ID, v.History, Primary); err == nil {
		t.Fatal("empty placement registry admitted volume")
	}
}

func TestLoaderErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Load(ctx, fixedLoader{config: testConfig()}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), nil); err == nil {
		t.Fatal("nil loader")
	}
	sentinel := errors.New("load failed")
	if _, err := Load(context.Background(), fixedLoader{err: sentinel}); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
}

func testID(n byte) fsapi.UUID { return fsapi.UUID{15: n} }

func testConfig() Config {
	return Config{Cluster: testID(1), Revision: 1, Nodes: []Node{
		{ID: testID(2), Admission: Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.1")}},
		{ID: testID(3), Admission: Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.2")}},
		{ID: testID(4), Admission: Admitted, Addresses: []netip.Addr{netip.MustParseAddr("2001:db8::3")}},
	}, Volumes: []Volume{{ID: testID(5), History: History{Generation: testID(6), Configuration: testID(7), Epoch: 1, CoordinatorTerm: 1}, Members: []Member{{Node: testID(2), Role: Primary}, {Node: testID(3), Role: Sync}, {Node: testID(4), Role: Async}}}}}
}
