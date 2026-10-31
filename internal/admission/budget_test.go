package admission

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
)

type policyFunc func(Authorization) error

func (f policyFunc) Authorize(a Authorization) error { return f(a) }

func testConfig() Config {
	var cfg Config
	for c := Class(0); c < ClassCount; c++ {
		r := Resources{Bytes: 1 << 20, Operations: 64, OwnerRAM: 1 << 20, Journal: 1 << 20, Dependencies: 1 << 20, Workers: 2, Connections: 4}
		p := r
		p.Operations, p.Connections = 32, 2
		cfg.Pools[c] = Limits{r, p, p}
	}
	return cfg
}

func newBudget(t testing.TB, cfg Config) *Budget {
	t.Helper()
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func request(peer, volume string) Request {
	return Request{Peer: peer, Volume: volume, Closure: volume, Class: Live, Cost: Resources{Bytes: 128, Operations: 1, OwnerRAM: 64, Journal: 64, Dependencies: 32, Workers: 1}, Mutating: true}
}

func enqueue(t testing.TB, b *Budget, req Request) *Lease {
	t.Helper()
	l, err := b.Enqueue(req)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestConfigAndRequestRefusals(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"empty":              func(c *Config) { *c = Config{} },
		"overflow":           func(c *Config) { c.Pools[Live].Global.Bytes = math.MaxUint64 },
		"hard_operations":    func(c *Config) { c.Pools[Live].Global.Operations = 65536 },
		"scope_exceeds_pool": func(c *Config) { c.Pools[Live].PerPeer.Workers = 3 },
		"uncounted_records":  func(c *Config) { c.Pools[Live].Global.Operations = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			change(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	_ = newBudget(t, DefaultConfig())
	for name, change := range map[string]func(*Request){
		"unknown_class":       func(r *Request) { r.Class = ClassCount },
		"missing_peer":        func(r *Request) { r.Peer = "" },
		"huge_key":            func(r *Request) { r.Volume = strings.Repeat("x", 257) },
		"missing_closure":     func(r *Request) { r.Closure = "" },
		"uncounted_operation": func(r *Request) { r.Cost.Operations = 0 },
		"overlapping_bytes":   func(r *Request) { r.Cost.Dependencies = 65 },
		"overflow_bytes":      func(r *Request) { r.Cost.OwnerRAM = math.MaxUint64 },
		"missing_worker":      func(r *Request) { r.Cost.Workers = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			b := newBudget(t, testConfig())
			r := request("p", "v")
			change(&r)
			if _, err := b.Enqueue(r); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
			if b.Usage().Used != (Resources{}) {
				t.Fatal("invalid request charged")
			}
		})
	}
	b := newBudget(t, testConfig())
	r := request("p", "v")
	r.Cost.Workers = 3
	if _, err := b.Enqueue(r); !errors.Is(err, ErrCapacity) {
		t.Fatal("impossible worker request queued", err)
	}
}

func TestEveryResourceIsCappedAtEveryScope(t *testing.T) {
	for dimension := range 7 {
		for _, scope := range []string{"global", "peer", "volume"} {
			t.Run(scope+string(rune('0'+dimension)), func(t *testing.T) {
				cfg := testConfig()
				limit := fields(cfg.Pools[Live].Global)
				cost := [7]uint64{128, 1, 64, 64, 32, 1, 1}
				limit[dimension] = cost[dimension]
				switch scope {
				case "global":
					cfg.Pools[Live].Global = resource(limit)
					cfg.Pools[Live].PerPeer = resource(limit)
					cfg.Pools[Live].PerVolume = resource(limit)
				case "peer":
					cfg.Pools[Live].PerPeer = resource(limit)
				case "volume":
					cfg.Pools[Live].PerVolume = resource(limit)
				}
				// Resource sublimits cannot exceed containing Bytes.
				for _, r := range []*Resources{&cfg.Pools[Live].Global, &cfg.Pools[Live].PerPeer, &cfg.Pools[Live].PerVolume} {
					r.OwnerRAM = min(r.OwnerRAM, r.Bytes)
					r.Dependencies = min(r.Dependencies, r.Bytes)
				}
				b := newBudget(t, cfg)
				r := request("p", "v")
				r.Mutating = false
				r.Cost = resource(cost)
				first, err := b.Reserve(r)
				must(t, err)
				if scope == "global" {
					r.Peer, r.Volume = "other", "other"
				}
				if scope == "peer" {
					r.Volume = "other"
				}
				if scope == "volume" {
					r.Peer = "other"
				}
				if _, err = b.Reserve(r); !errors.Is(err, ErrCapacity) {
					t.Fatal("overload accepted", err)
				}
				must(t, first.Release())
				last, err := b.Reserve(r)
				must(t, err)
				must(t, last.Release())
				if b.Usage().Used != (Resources{}) {
					t.Fatal("release leaked credits")
				}
			})
		}
	}
}

func TestScopeChurnLeavesNoWaitersOrHistoricalKeys(t *testing.T) {
	b := newBudget(t, testConfig())
	for range 200 {
		leases := make([]*Lease, 0, 32)
		for i := range 32 {
			r := request(string(rune(1000+i)), string(rune(2000+i)))
			leases = append(leases, enqueue(t, b, r))
		}
		for _, l := range leases {
			must(t, l.Release())
		}
	}
	if len(b.leases) != 0 || len(b.active) != 0 {
		t.Fatal("lease or closure leak")
	}
	for c := Class(0); c < ClassCount; c++ {
		if len(b.queues[c])+len(b.peers[c])+len(b.volumes[c]) != 0 {
			t.Fatal("historical scopes retained")
		}
	}
}

func TestConcurrentReservationBound(t *testing.T) {
	cfg := testConfig()
	cfg.Pools[Live].Global.Operations = 8
	cfg.Pools[Live].PerPeer.Operations = 8
	cfg.Pools[Live].PerVolume.Operations = 8
	b := newBudget(t, cfg)
	var wg sync.WaitGroup
	leases := make(chan *Lease, 64)
	for range 64 {
		wg.Go(func() {
			r := request("p", "v")
			r.Mutating = false
			r.Cost.Workers = 0
			l, err := b.Reserve(r)
			if err == nil {
				leases <- l
			} else if !errors.Is(err, ErrCapacity) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(leases)
	if b.Usage().Used.Operations != 8 {
		t.Fatal(b.Usage())
	}
	for l := range leases {
		must(t, l.Release())
	}
}
