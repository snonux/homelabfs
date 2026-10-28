package coord

import (
	"context"
	"errors"
	"math"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
)

type fixture struct {
	operator, controller, daemon *Client
	compact                      func(int64) error
}
type factory func(*testing.T) fixture

func TestMemoryContract(t *testing.T) {
	runContract(t, func(t *testing.T) fixture {
		memory, err := NewMemory(id(1))
		if err != nil {
			t.Fatal(err)
		}
		clients := make([]*Client, 0, 3)
		for _, role := range []Capability{Operator, Controller, Daemon} {
			c, err := memory.Connect(role)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
			clients = append(clients, c.Client)
		}
		return fixture{clients[0], clients[1], clients[2], memory.Compact}
	})
}

func runContract(t *testing.T, newFixture factory) {
	t.Helper()
	runExtendedContract(t, newFixture)
	runWatchResyncContract(t, newFixture)
	t.Run("AllocationCASAndAdmission", func(t *testing.T) {
		f := newFixture(t)
		ctx := t.Context()
		p := policy(f.operator.cluster)
		if _, err := f.daemon.GetPolicy(ctx); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing policy: %v", err)
		}
		if _, err := f.controller.PutPolicy(ctx, 0, p); !errors.Is(err, ErrDenied) {
			t.Fatalf("controller changed policy: %v", err)
		}
		s, err := f.operator.PutPolicy(ctx, 0, p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.operator.PutPolicy(ctx, 0, p); !errors.Is(err, ErrConflict) {
			t.Fatalf("create overwrote policy: %v", err)
		}
		req := Request{Volume: id(5), Owner: id(3), Incarnation: id(9), PolicyRevision: s.Revision}
		if _, err := f.daemon.Allocate(ctx, req); !errors.Is(err, ErrDenied) {
			t.Fatalf("daemon allocated: %v", err)
		}
		if _, err := f.operator.Allocate(ctx, req); !errors.Is(err, ErrDenied) {
			t.Fatalf("operator allocated: %v", err)
		}
		a, err := f.controller.Allocate(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if a.Grant.History.Epoch != 2 || a.Grant.History.CoordinatorTerm != 2 || a.Grant.History.Configuration == id(7) {
			t.Fatal("allocation failed to advance history")
		}
		if _, err := f.controller.Allocate(ctx, req); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale grant CAS: %v", err)
		}
		got, err := f.daemon.GetGrant(ctx, req.Volume)
		if err != nil || got != a {
			t.Fatalf("grant read: %+v %v", got, err)
		}
		req.ExpectedGrant = a.Revision
		b, err := f.controller.Allocate(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if b.Grant.History.Epoch != 3 || b.Grant.History.Configuration == a.Grant.History.Configuration {
			t.Fatal("history reused")
		}
		req.Owner = id(11)
		if _, err := f.controller.Allocate(ctx, req); !errors.Is(err, ErrDenied) {
			t.Fatalf("unknown candidate: %v", err)
		}
		s.Policy.Registry.Revision++
		s, err = f.operator.PutPolicy(ctx, s.Revision, s.Policy)
		if err != nil {
			t.Fatal(err)
		}
		req.Owner = id(3)
		req.ExpectedGrant = b.Revision
		if _, err := f.controller.Allocate(ctx, req); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale policy: %v", err)
		}
		req.PolicyRevision = s.Revision
		c, err := f.controller.Allocate(ctx, req)
		if err != nil || c.Grant.History.Epoch != 4 {
			t.Fatalf("configuration update regressed epoch: %+v %v", c, err)
		}
	})
	t.Run("ConcurrentAllocation", func(t *testing.T) {
		f := newFixture(t)
		ctx := t.Context()
		s, err := f.operator.PutPolicy(ctx, 0, policy(f.operator.cluster))
		if err != nil {
			t.Fatal(err)
		}
		req := Request{Volume: id(5), Owner: id(2), Incarnation: id(9), PolicyRevision: s.Revision}
		results := make(chan error, 8)
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() { _, err := f.controller.Allocate(ctx, req); results <- err })
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else if !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
		}
		if successes != 1 {
			t.Fatalf("successful allocations: %d", successes)
		}
	})
	t.Run("EmergencyFailsClosed", func(t *testing.T) {
		f := newFixture(t)
		ctx := t.Context()
		p := policy(f.operator.cluster)
		p.Mode = Emergency
		p.EmergencyGeneration = id(12)
		p.ServingNode = id(2)
		p.Recovery = []RecoveryPeer{{Node: id(3), Volumes: []fsapi.UUID{id(5)}}}
		s, err := f.operator.PutPolicy(ctx, 0, p)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Policy.CheckRecovery(id(3), id(5)); err != nil {
			t.Fatal(err)
		}
		if err := s.Policy.CheckRecovery(id(4), id(5)); !errors.Is(err, ErrDenied) {
			t.Fatal("unapproved recovery allowed")
		}
		_, err = f.controller.Allocate(ctx, Request{Volume: id(5), Owner: id(2), Incarnation: id(9), PolicyRevision: s.Revision})
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("emergency granted normal allocation: %v", err)
		}
		p.Registry.Revision++
		p.Recovery = append(p.Recovery, p.Recovery[0])
		if _, err := f.operator.PutPolicy(ctx, s.Revision, p); err == nil {
			t.Fatal("duplicate recovery admission accepted")
		}
	})
	t.Run("ReplayWatchAndCompaction", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		s, err := f.operator.PutPolicy(ctx, 0, policy(f.operator.cluster))
		if err != nil {
			t.Fatal(err)
		}
		first := s.Revision
		for range 2 {
			s.Policy.Registry.Revision++
			s, err = f.operator.PutPolicy(ctx, s.Revision, s.Policy)
			if err != nil {
				t.Fatal(err)
			}
		}
		events, err := f.daemon.WatchPolicy(ctx, first)
		if err != nil {
			t.Fatal(err)
		}
		for want := uint64(2); want <= 3; want++ {
			select {
			case event := <-events:
				if event.Err != nil || event.Snapshot.Policy.Registry.Revision != want {
					t.Fatalf("watch: %+v", event)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if err := f.compact(s.Revision); err != nil {
			t.Fatal(err)
		}
		expired, err := f.daemon.WatchPolicy(ctx, first)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case event := <-expired:
			if !errors.Is(event.Err, ErrCompacted) {
				t.Fatalf("compaction: %+v", event)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	})
	t.Run("InvalidPolicyAndCancellation", func(t *testing.T) {
		f := newFixture(t)
		ctx := t.Context()
		p := policy(f.operator.cluster)
		p.Mode = "unknown"
		if _, err := f.operator.PutPolicy(ctx, 0, p); err == nil {
			t.Fatal("unknown mode accepted")
		}
		p = policy(f.operator.cluster)
		p.Registry.Volumes[0].Members[0].Node = id(11)
		if _, err := f.operator.PutPolicy(ctx, 0, p); err == nil {
			t.Fatal("unassigned member accepted")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := f.daemon.GetPolicy(canceled); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	})
}

func TestMemoryPartitionsAndUnknownCommit(t *testing.T) {
	m, _ := NewMemory(id(1))
	op, _ := m.Connect(Operator)
	control, _ := m.Connect(Controller)
	reader, _ := m.Connect(Daemon)
	t.Cleanup(func() { _ = op.Close(); _ = control.Close(); _ = reader.Close() })
	ctx := t.Context()
	s, err := op.PutPolicy(ctx, 0, policy(id(1)))
	if err != nil {
		t.Fatal(err)
	}
	watch, err := reader.WatchPolicy(ctx, s.WatchRevision)
	if err != nil {
		t.Fatal(err)
	}
	reader.SetPartition(true)
	event := <-watch
	if !errors.Is(event.Err, ErrUnavailable) {
		t.Fatalf("partition watch: %+v", event)
	}
	if _, err := reader.GetPolicy(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("partition read succeeded")
	}
	if _, err := control.GetPolicy(ctx); err != nil {
		t.Fatal("partition affected other connection")
	}
	reader.SetPartition(false)
	req := Request{Volume: id(5), Owner: id(2), Incarnation: id(9), PolicyRevision: s.Revision}
	m.SetQuorum(false)
	if _, err := control.Allocate(ctx, req); !errors.Is(err, ErrUnavailable) {
		t.Fatal("quorumless allocation succeeded")
	}
	m.SetQuorum(true)
	if _, err := reader.GetGrant(ctx, id(5)); !errors.Is(err, ErrNotFound) {
		t.Fatal("precommit rejection mutated state")
	}
	control.LoseNextCommitReply()
	if _, err := control.Allocate(ctx, req); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("lost reply: %v", err)
	}
	a, err := reader.GetGrant(ctx, id(5))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.Allocate(ctx, req); !errors.Is(err, ErrConflict) {
		t.Fatal("unknown commit duplicated allocation")
	}
	req.ExpectedGrant = a.Revision
	b, err := control.Allocate(ctx, req)
	if err != nil || b.Grant.History.Epoch != a.Grant.History.Epoch+1 {
		t.Fatalf("post-unknown allocation: %+v %v", b, err)
	}
}

func TestZeroClientAndPolicy(t *testing.T) {
	var c Client
	if _, err := c.GetPolicy(t.Context()); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if err := (Policy{}).CheckRecovery(id(2), id(5)); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func policy(clusterID fsapi.UUID) Policy {
	return Policy{Format: 1, Mode: Normal, Registry: cluster.Config{Cluster: clusterID, Revision: 1, Nodes: []cluster.Node{
		{ID: id(2), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.2")}},
		{ID: id(3), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.3")}},
		{ID: id(4), Admission: cluster.Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.4")}},
	}, Volumes: []cluster.Volume{{ID: id(5), History: cluster.History{Generation: id(6), Configuration: id(7), Epoch: 1, CoordinatorTerm: 1}, Members: []cluster.Member{{Node: id(2), Role: cluster.Primary}, {Node: id(3), Role: cluster.Sync}, {Node: id(4), Role: cluster.Async}}}}}}
}

func id(last byte) fsapi.UUID { var id fsapi.UUID; id[0] = 1; id[15] = last; return id }

func runExtendedContract(t *testing.T, newFixture factory) {
	t.Helper()
	t.Run("ConfigurationReservations", func(t *testing.T) {
		f := newFixture(t)
		ctx := t.Context()
		s, err := f.operator.PutPolicy(ctx, 0, policy(f.operator.cluster))
		if err != nil {
			t.Fatal(err)
		}
		req := Request{Volume: id(5), Owner: id(2), Incarnation: id(9), PolicyRevision: s.Revision}
		f.controller.newConfiguration = func() (fsapi.UUID, error) { return id(7), nil }
		if _, err := f.controller.Allocate(ctx, req); err == nil {
			t.Fatal("genesis configuration reused")
		}
		f.controller.newConfiguration = func() (fsapi.UUID, error) { return id(13), nil }
		first, err := f.controller.Allocate(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		req.ExpectedGrant = first.Revision
		if _, err := f.controller.Allocate(ctx, req); !errors.Is(err, ErrConflict) {
			t.Fatalf("allocation configuration reused: %v", err)
		}
		f.controller.newConfiguration = nil
		if _, err := f.controller.Allocate(ctx, req); err != nil {
			t.Fatal(err)
		}
		stored, err := f.controller.store.get(ctx, f.controller.prefix+"configurations/"+id(13).String())
		if err != nil {
			t.Fatal(err)
		}
		old, err := f.controller.allocation(stored, id(5))
		if err != nil || old != first {
			t.Fatalf("prior allocation history lost: %+v %v", old, err)
		}
	})
	t.Run("CounterExhaustionAndGeneration", func(t *testing.T) {
		f := newFixture(t)
		ctx := t.Context()
		p := policy(f.operator.cluster)
		p.Registry.Volumes[0].History.Epoch = math.MaxUint64
		s, err := f.operator.PutPolicy(ctx, 0, p)
		if err != nil {
			t.Fatal(err)
		}
		req := Request{Volume: id(5), Owner: id(2), Incarnation: id(9), PolicyRevision: s.Revision}
		if _, err := f.controller.Allocate(ctx, req); err == nil {
			t.Fatal("epoch wrapped")
		}
		p.Registry.Revision++
		p.Registry.Volumes[0].History.Epoch = 1
		s, err = f.operator.PutPolicy(ctx, s.Revision, p)
		if err != nil {
			t.Fatal(err)
		}
		req.PolicyRevision = s.Revision
		a, err := f.controller.Allocate(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		p.Registry.Revision++
		p.Registry.Volumes[0].History.Generation = id(14)
		s, err = f.operator.PutPolicy(ctx, s.Revision, p)
		if err != nil {
			t.Fatal(err)
		}
		req.PolicyRevision = s.Revision
		req.ExpectedGrant = a.Revision
		if _, err := f.controller.Allocate(ctx, req); err == nil {
			t.Fatal("automatic generation transition allowed")
		}
	})
}

func runWatchResyncContract(t *testing.T, newFixture factory) {
	t.Helper()
	t.Run("FreshWatchCursorAfterGrantOnlyCompaction", func(t *testing.T) {
		f := newFixture(t)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		initial, err := f.operator.PutPolicy(ctx, 0, policy(f.operator.cluster))
		if err != nil {
			t.Fatal(err)
		}
		grant, err := f.controller.Allocate(ctx, Request{Volume: id(5), Owner: id(2), Incarnation: id(9), PolicyRevision: initial.Revision})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.compact(grant.Revision); err != nil {
			t.Fatal(err)
		}
		fresh, err := f.daemon.GetPolicy(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if fresh.Revision != initial.Revision || fresh.WatchRevision < grant.Revision {
			t.Fatalf("snapshot confused CAS and watch revisions: %+v", fresh)
		}
		events, err := f.daemon.WatchPolicy(ctx, fresh.WatchRevision)
		if err != nil {
			t.Fatal(err)
		}
		fresh.Policy.Registry.Revision++
		updated, err := f.operator.PutPolicy(ctx, fresh.Revision, fresh.Policy)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case event := <-events:
			if event.Err != nil || event.Snapshot.Revision != updated.Revision {
				t.Fatalf("resumed watch: %+v", event)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	})
}
