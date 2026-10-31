package admission

import (
	"fmt"
	"math"
	"strings"
	"sync"
)

// Budget is a process-local, nonblocking owner of bounded reservations. It has
// no internal goroutines, channels, disk accesses or waiters. Its zero value is
// disabled; use New. Locks cover counters and queue metadata only.
type Budget struct {
	mu             sync.Mutex
	cfg            Config
	limits         Limits
	used           [ClassCount]Resources
	peers, volumes [ClassCount]map[string]Resources
	leases         map[*Lease]struct{}
	active         map[string]*Lease
	queues         [ClassCount][]*volumeQueue
	nextClass      Class
}

// New validates hard aggregate bounds before allocating any scope maps.
func New(cfg Config) (*Budget, error) {
	var total Limits
	for _, p := range cfg.Pools {
		if !within(p.PerPeer, p.Global) || !within(p.PerVolume, p.Global) {
			return nil, fmt.Errorf("scope exceeds global pool: %w", ErrInvalid)
		}
		for _, r := range []Resources{p.Global, p.PerPeer, p.PerVolume} {
			if !validResources(r) || (r != (Resources{}) && r.Operations == 0) {
				return nil, ErrInvalid
			}
		}
		var ok bool
		if total.Global, ok = add(total.Global, p.Global); !ok {
			return nil, ErrInvalid
		}
		if total.PerPeer, ok = add(total.PerPeer, p.PerPeer); !ok {
			return nil, ErrInvalid
		}
		if total.PerVolume, ok = add(total.PerVolume, p.PerVolume); !ok {
			return nil, ErrInvalid
		}
	}
	// These component ceilings bound queue/map cardinality and all arithmetic.
	hard := Resources{4 << 30, 65536, 4 << 30, 4 << 30, 4 << 30, 256, 256}
	if total.Global.Operations == 0 || !within(total.Global, hard) {
		return nil, fmt.Errorf("aggregate hard ceiling: %w", ErrInvalid)
	}
	b := &Budget{cfg: cfg, limits: total, leases: make(map[*Lease]struct{}), active: make(map[string]*Lease)}
	for c := Class(0); c < ClassCount; c++ {
		b.peers[c] = make(map[string]Resources)
		b.volumes[c] = make(map[string]Resources)
	}
	return b, nil
}

// DefaultConfig returns conservative component limits, not a calibrated host or
// canary capacity profile. Deployers must include runtime/kernel overhead too.
func DefaultConfig() Config {
	var cfg Config
	for c := Class(0); c < ClassCount; c++ {
		bytes, ops, workers, conns := uint64(8<<20), uint64(256), uint64(2), uint64(4)
		switch c {
		case Live:
			workers = 4
		case Recovery:
			bytes = 32 << 20
		case Control:
			bytes = 1 << 20
		case Drain:
			bytes = 16 << 20
		case Memory:
			bytes, ops, workers, conns = 128<<20, 64, 0, 0
		}
		r := Resources{Bytes: bytes, Operations: ops, OwnerRAM: bytes, Journal: bytes, Dependencies: bytes, Workers: workers, Connections: conns}
		// OwnerRAM/Dependencies are sublimits; their maxima need not be used
		// simultaneously. Actual requests must fit the containing Bytes.
		volume := Resources{Bytes: r.Bytes / 2, Operations: r.Operations / 2, OwnerRAM: r.OwnerRAM / 2, Journal: r.Journal / 2, Dependencies: r.Dependencies / 2, Workers: r.Workers / 2, Connections: r.Connections / 2}
		cfg.Pools[c] = Limits{Global: r, PerPeer: r, PerVolume: volume}
	}
	return cfg
}

// Limits returns the checked sum of all class capacities.
func (b *Budget) Limits() Limits { return b.limits }

// Reserve reserves all resources immediately, before the caller allocates or
// opens a connection. It never queues or waits. Local arenas use Memory;
// reconstruction allocations use a separate Recovery holding.
func (b *Budget) Reserve(req Request) (*Lease, error) {
	if req.Mutating || req.Blocked {
		return nil, ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reserve(req, false)
}

// Usage returns accounting, not RSS or proof of physical storage availability.
func (b *Budget) Usage() Usage {
	b.mu.Lock()
	defer b.mu.Unlock()
	u := Usage{Pools: b.used}
	for _, r := range b.used {
		u.Used, _ = add(u.Used, r)
	}
	for l := range b.leases {
		switch l.state {
		case queued:
			u.Queued++
		case running:
			u.Running++
		case retained:
			u.Retained++
		}
		if l.obligation != "" {
			u.Accepted++
		}
	}
	return u
}

func (b *Budget) reserve(req Request, queue bool) (*Lease, error) {
	if !validRequest(req) {
		return nil, ErrInvalid
	}
	p := b.cfg.Pools[req.Class]
	if !within(req.Cost, p.Global) || !within(req.Cost, p.PerPeer) || !within(req.Cost, p.PerVolume) {
		return nil, ErrCapacity
	}
	charge := req.Cost
	if queue {
		charge.Workers = 0
	}
	if !b.fits(req, charge) {
		return nil, ErrCapacity
	}
	// Small scope substrings must not retain arbitrarily large caller buffers.
	req.Peer = strings.Clone(req.Peer)
	req.Volume = strings.Clone(req.Volume)
	req.Closure = strings.Clone(req.Closure)
	l := &Lease{budget: b, req: req, charge: charge, state: running, ready: !req.Blocked}
	if queue {
		l.state = queued
	}
	b.charge(req, charge, true)
	b.leases[l] = struct{}{}
	return l, nil
}

func (b *Budget) fits(req Request, r Resources) bool {
	p := b.cfg.Pools[req.Class]
	return fits(b.used[req.Class], r, p.Global) && fits(b.peers[req.Class][req.Peer], r, p.PerPeer) && fits(b.volumes[req.Class][req.Volume], r, p.PerVolume)
}

func (b *Budget) charge(req Request, r Resources, increase bool) {
	c := req.Class
	combine := subtract
	if increase {
		combine = sum
	}
	b.used[c] = combine(b.used[c], r)
	peer := combine(b.peers[c][req.Peer], r)
	volume := combine(b.volumes[c][req.Volume], r)
	if peer.Operations == 0 {
		delete(b.peers[c], req.Peer)
	} else {
		b.peers[c][req.Peer] = peer
	}
	if volume.Operations == 0 {
		delete(b.volumes[c], req.Volume)
	} else {
		b.volumes[c][req.Volume] = volume
	}
}

func validRequest(r Request) bool {
	return r.Class < ClassCount && validKey(r.Peer) && validKey(r.Volume) && len(r.Closure) <= 256 && (!r.Mutating || validKey(r.Closure)) && r.Cost.Operations > 0 && validResources(r.Cost) && r.Cost.OwnerRAM <= r.Cost.Bytes && r.Cost.Dependencies <= r.Cost.Bytes-r.Cost.OwnerRAM
}

func validKey(s string) bool { return len(s) > 0 && len(s) <= 256 }

func validResources(r Resources) bool {
	// Capacity sublimits may overlap; request subcategories may not.
	return r.OwnerRAM <= r.Bytes && r.Dependencies <= r.Bytes
}

func fits(a, cost, limit Resources) bool {
	r, ok := add(a, cost)
	return ok && within(r, limit)
}

func within(a, b Resources) bool {
	return a.Bytes <= b.Bytes && a.Operations <= b.Operations && a.OwnerRAM <= b.OwnerRAM && a.Journal <= b.Journal && a.Dependencies <= b.Dependencies && a.Workers <= b.Workers && a.Connections <= b.Connections
}

func add(a, b Resources) (Resources, bool) {
	x, y := fields(a), fields(b)
	for i := range x {
		if math.MaxUint64-x[i] < y[i] {
			return Resources{}, false
		}
		x[i] += y[i]
	}
	return resource(x), true
}

func sum(a, b Resources) Resources { r, _ := add(a, b); return r }
func subtract(a, b Resources) Resources {
	x, y := fields(a), fields(b)
	for i := range x {
		x[i] -= y[i]
	}
	return resource(x)
}
func fields(r Resources) [7]uint64 {
	return [7]uint64{r.Bytes, r.Operations, r.OwnerRAM, r.Journal, r.Dependencies, r.Workers, r.Connections}
}
func resource(x [7]uint64) Resources { return Resources{x[0], x[1], x[2], x[3], x[4], x[5], x[6]} }
