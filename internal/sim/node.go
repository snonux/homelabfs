package sim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/failpoint"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/transport"
	"github.com/snonux/homelabfs/internal/vfs"
)

// Factory constructs actual core services against this lifetime's guarded roots.
// It must install bounded/cancellable hooks and return all its workers in Closer.
// No distributed protocol, readiness oracle or normal serving policy is supplied.
type Factory func(context.Context, *Runtime) (io.Closer, error)

// NodeConfig describes an isolated actor admitted by the real static registry.
// Each actor retains at most 64 roots, 16 lifetimes, 100000 storage calls and
// 8 MiB of cumulative writes; limits fail closed rather than silently evicting.
type NodeConfig struct {
	Seed       uint64
	Registry   *cluster.Registry
	Node       fsapi.UUID
	Address    netip.AddrPort
	Network    *Network
	Scheduler  *Scheduler
	Disk       vfs.Config
	Factory    Factory
	Authorizer transport.Authorizer
}

// Node owns real core instances and simulated persistence, not a normalnode.Node.
// Its synthetic durable identity record does not exercise the OS lifecycle lock.
// Its only executable commands must enter through Call or the controlled caller;
// Pause cannot preempt arbitrary Go code already executing within an operation.
type Node struct {
	callScope       uint64
	hold            *failpoint.BlockHold
	faults          map[failpoint.Name]*Fault
	rootsMu         sync.Mutex
	mu              sync.Mutex
	cfg             NodeConfig
	generation      uint64
	roots           map[string]*vfs.Simulator
	budget          *diskBudget
	gate            *diskGate
	clock           *Clock
	runtime         *Runtime
	service         io.Closer
	paused, running bool
	calls           int
}

// Runtime supplies a single fresh fixture lifetime. Old values lose all storage
// access on Crash. Factory-created volatile state must be owned by its Closer.
type Runtime struct {
	node     *Node
	gate     *diskGate
	Identity cluster.Identity
	Endpoint *transport.Endpoint
	Clock    *Clock
	Hooks    *failpoint.Registry
	control  *failpoint.Control
	Random   *Random
}

// Fault wraps a simulator-owned core activation. Release is serialized with
// pause/lifecycle control and cannot release a checkpoint while its actor pauses.
type Fault struct {
	name       failpoint.Name
	node       *Node
	runtime    *Runtime
	activation *failpoint.Activation
	action     failpoint.Action
	released   bool
}

// NewNode registers a stopped actor; Restart must be called explicitly. It
// opens no OS resources and never calls normalnode.Start with fabricated proof.
func NewNode(c NodeConfig) (*Node, error) {
	if c.Registry == nil || c.Network == nil || c.Scheduler == nil || c.Network.scheduler != c.Scheduler || !c.Node.Valid() {
		return nil, fmt.Errorf("invalid simulated node configuration")
	}
	if err := c.Registry.CheckPeer(c.Registry.Cluster(), c.Node, c.Address.Addr()); err != nil {
		return nil, err
	}
	if c.Disk.MaxFileSize == 0 {
		c.Disk.MaxFileSize = 8 << 20
	}
	if c.Disk.MaxFileSize < 1 || c.Disk.MaxFileSize > 8<<20 {
		return nil, fmt.Errorf("simulation file budget")
	}
	n := &Node{cfg: c, roots: make(map[string]*vfs.Simulator), budget: &diskBudget{}}
	c.Scheduler.mu.Lock()
	defer c.Scheduler.mu.Unlock()
	if len(c.Scheduler.actors) >= 16 {
		return nil, fmt.Errorf("simulation node budget")
	}
	c.Scheduler.actors = append(c.Scheduler.actors, n)
	return n, nil
}

// Restart reconstructs fresh core services with a new persisted incarnation.
// Historical journal headers/authority remain exactly as the factory stored them.
func (n *Node) Restart(ctx context.Context) error {
	return n.cfg.Scheduler.Do("node/restart", n.cfg.Node.String(), func() (retErr error) {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.running || n.calls != 0 || n.generation >= 16 {
			return fmt.Errorf("running node or lifetime budget exhausted")
		}
		n.generation++
		n.gate = &diskGate{budget: n.budget}
		n.clock = NewClock()
		if err := n.clock.AdvanceTo(n.cfg.Scheduler.clock.Time()); err != nil {
			return err
		}
		id := cluster.Identity{Cluster: n.cfg.Registry.Cluster(), Node: n.cfg.Node, Incarnation: UUID(n.cfg.Seed, fmt.Sprintf("%s/incarnation/%d", n.cfg.Node, n.generation))}
		hooks, control := failpoint.NewForTesting()
		r := &Runtime{node: n, gate: n.gate, Identity: id, Clock: n.clock, Hooks: hooks, control: control, Random: NewRandom(n.cfg.Seed, fmt.Sprintf("%s/service/%d", n.cfg.Node, n.generation))}
		n.runtime = r
		defer func() {
			if !n.running {
				n.gate.fence()
				control.DisableAll()
				if r.Endpoint != nil {
					retErr = errors.Join(retErr, r.Endpoint.Close())
				}
				if n.service != nil {
					retErr = errors.Join(retErr, n.service.Close())
				}
				n.service = nil
				n.runtime = nil
				n.clock = nil
			}
		}()
		if err := n.persist(ctx, r); err != nil {
			return err
		}
		var err error
		r.Endpoint, err = transport.New(transport.Config{Registry: n.cfg.Registry, Identity: id, Local: n.cfg.Address, Network: n.cfg.Network, Clock: n.clock.Transport(), Random: NewRandom(n.cfg.Seed, fmt.Sprintf("%s/transport/%d", n.cfg.Node, n.generation)), Limits: transport.Limits{Message: 1 << 20, Queue: 8, Bytes: 2 << 20, Operations: 8}, MaxConnections: 8, Timeout: time.Second, Authorizer: n.cfg.Authorizer})
		if err != nil {
			return err
		}
		if err = r.Endpoint.Listen(ctx); err != nil {
			return errors.Join(err, r.Endpoint.Close())
		}
		if n.cfg.Factory != nil {
			n.service, err = n.cfg.Factory(ctx, r)
			if err != nil {
				n.gate.fence()
				control.DisableAll()
				return errors.Join(err, r.Endpoint.Close())
			}
		}
		n.cfg.Network.mu.Lock()
		n.cfg.Network.paused[n.cfg.Address.Addr()] = false
		n.cfg.Network.mu.Unlock()
		n.running = true
		n.paused = false
		return nil
	})
}

// Crash fences all old storage access, discards unsynced state and drops local
// workers/RAM. Surviving peers retain queued bytes/frames and learn reset only
// when the scenario steps the separate network reset observation event.
func (n *Node) Crash() error {
	return n.cfg.Scheduler.Do("node/crash", n.cfg.Node.String(), func() error {
		n.mu.Lock()
		defer n.mu.Unlock()
		if !n.running {
			return fmt.Errorf("node is stopped")
		}
		n.gate.fence()
		n.rootsMu.Lock()
		names := make([]string, 0, len(n.roots))
		for name := range n.roots {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			n.roots[name].Crash()
		}
		n.rootsMu.Unlock()
		n.releaseHold()
		n.runtime.control.DisableAll()
		err := n.runtime.Endpoint.Close()
		// A dead worker's drain errors (e.g. invalidated source descriptors) cannot
		// revive its state. The storage fence precedes draining and never reopens.
		if n.service != nil {
			_ = n.service.Close()
		}
		n.service = nil
		n.running = false
		n.runtime = nil
		n.clock = nil
		return err
	})
}

// Close drains a live fixture normally. Use Crash for RAM/persistence loss.
func (n *Node) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.running {
		return nil
	}
	n.releaseHold()
	n.runtime.control.DisableAll()
	err := n.runtime.Endpoint.Close()
	if n.service != nil {
		err = errors.Join(err, n.service.Close())
	}
	n.gate.fence()
	n.runtime = nil
	n.service = nil
	n.running = false
	n.clock = nil
	return err
}

// Pause gates future controlled commands and network boundaries, freezing this
// actor's timer notifications. Resume first restores elapsed virtual deadlines.
func (n *Node) Pause() error { return n.pause(true, nil) }

// Resume allows commands and packets after applying elapsed virtual time.
func (n *Node) Resume() error { return n.pause(false, nil) }

// Call executes one bounded scenario command against the live core lifetime.
// Do not retain Runtime as current liveness evidence after this call returns.
func (n *Node) Call(ctx context.Context, label string, fn func(context.Context, *Runtime) error) error {
	n.mu.Lock()
	r, usable := n.runtime, n.running && !n.paused && n.calls == 0
	if usable {
		n.calls++
		n.callScope++
		r.control.Scope(n.callScope)
	}
	n.mu.Unlock()
	if !usable {
		return fmt.Errorf("node unavailable or another controlled call active")
	}
	defer func() { n.mu.Lock(); n.calls--; r.control.Scope(0); n.mu.Unlock() }()
	return n.cfg.Scheduler.Do("node/call", n.cfg.Node.String()+" "+label, func() error { return fn(ctx, r) })
}

// Enable configures a core failpoint without os.Exit or wall-clock sleeps.
// ReturnError and Block are supported; virtual delays use scheduler events.
func (n *Node) Enable(name failpoint.Name, rule failpoint.Rule) (*Fault, error) {
	n.mu.Lock()
	r, usable := n.runtime, n.running && !n.paused
	defer n.mu.Unlock()
	if !usable || rule.Action == failpoint.Crash || rule.Action == failpoint.Sleep {
		return nil, fmt.Errorf("unsupported simulated failpoint action")
	}
	var a *failpoint.Activation
	err := n.cfg.Scheduler.Do("failpoint", fmt.Sprintf("%s %s action=%d nth=%d", n.cfg.Node, name, rule.Action, rule.Nth), func() error { var err error; a, err = r.control.Enable(name, rule); return err })
	if err != nil {
		return nil, err
	}
	if n.faults == nil {
		n.faults = make(map[failpoint.Name]*Fault)
	}
	if old := n.faults[name]; old != nil {
		old.released = true
	}
	f := &Fault{name: name, node: n, runtime: r, activation: a, action: rule.Action}
	n.faults[name] = f
	return f, nil
}

// Disk opens a named persistent root through this lifetime's crash fence. Root
// creation is deterministic in factory order and limited to 64 per actor.
func (r *Runtime) Disk(name string) (storage.Filesystem, error) {
	if !fs.ValidPath(name) || name == "." || len(name) > 256 {
		return nil, fmt.Errorf("invalid simulation root name")
	}
	// Factories run under the node lock; controlled calls never mutate this map
	// concurrently with lifecycle actions. The facade independently gates workers.
	release, err := r.gate.enter(0)
	if err != nil {
		return nil, err
	}
	defer release()
	r.node.rootsMu.Lock()
	defer r.node.rootsMu.Unlock()
	root := r.node.roots[name]
	if root == nil {
		if len(r.node.roots) >= 64 {
			return nil, fmt.Errorf("simulation root budget exhausted")
		}
		c := r.node.cfg.Disk
		c.Seed = r.node.cfg.Network.sample(r.node.cfg.Node.String()+"/"+name, 0)
		var err error
		root, err = vfs.NewSimulator(c)
		if err != nil {
			return nil, err
		}
		r.node.roots[name] = root
	}
	return &disk{backend: root, gate: r.gate}, nil
}
func (n *Node) persist(ctx context.Context, r *Runtime) error {
	root, err := r.Disk("control")
	if err != nil {
		return err
	}
	f, err := root.Open(ctx, r.Identity.Incarnation.String(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	raw := []byte(r.Identity.Cluster.String() + "\n" + r.Identity.Node.String() + "\n" + r.Identity.Incarnation.String() + "\n")
	written, err := f.WriteAt(ctx, raw, 0)
	if err == nil && written != len(raw) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync(ctx)
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return root.SyncDir(ctx, ".")
}
func (n *Node) pause(paused bool, checkpoint *Fault) error {
	n.mu.Lock()
	if !n.running || n.paused == paused {
		n.mu.Unlock()
		return fmt.Errorf("invalid node pause transition")
	}
	if paused && checkpoint != nil && n.calls != 1 {
		n.mu.Unlock()
		return fmt.Errorf("checkpoint requires a current Call")
	}
	if paused && n.calls > 0 {
		if checkpoint == nil || checkpoint.node != n || checkpoint.runtime != n.runtime || checkpoint.action != failpoint.Block || checkpoint.released {
			n.mu.Unlock()
			return fmt.Errorf("in-flight pause requires an unreleased Block checkpoint")
		}
		if n.faults[checkpoint.name] != checkpoint {
			n.mu.Unlock()
			return fmt.Errorf("pause checkpoint replaced")
		}
		hold, err := checkpoint.activation.HoldBlock(n.callScope)
		if err != nil {
			n.mu.Unlock()
			return fmt.Errorf("pause checkpoint: %w", err)
		}
		n.hold = hold
	}
	n.paused = paused
	clock := n.clock
	n.mu.Unlock()
	if err := n.cfg.Network.Pause(n.cfg.Address.Addr(), paused); err != nil {
		return err
	}
	if !paused {
		if err := clock.AdvanceTo(n.cfg.Scheduler.clock.Time()); err != nil {
			return err
		}
		n.mu.Lock()
		n.releaseHold()
		n.mu.Unlock()
		return nil
	}
	return nil
}

// PauseAt suspends an in-flight controlled call at a reached Block failpoint.
// Arbitrary executing Go code cannot be paused; Pause rejects such a request.
func (n *Node) PauseAt(f *Fault) error {
	if f == nil {
		return fmt.Errorf("missing pause checkpoint")
	}
	return n.pause(true, f)
}

// Hits reports arrivals at the actual core hook.
func (f *Fault) Hits() uint64 { return f.activation.Hits() }

// Triggered observes the reached hook before a selected Block waits for release.
func (f *Fault) Triggered() <-chan struct{} { return f.activation.Triggered() }

// Release unblocks this fault after Resume, through the controlled trace.
func (f *Fault) Release() error {
	f.node.mu.Lock()
	defer f.node.mu.Unlock()
	if f.node.paused || f.node.runtime != f.runtime {
		return fmt.Errorf("paused or expired fault lifetime")
	}
	return f.node.cfg.Scheduler.Do("failpoint/release", f.node.cfg.Node.String(), func() error { f.released = true; f.activation.Release(); return nil })
}

// releaseHold requires the node lock. Crash calls it only after fencing storage.
func (n *Node) releaseHold() {
	if n.hold != nil {
		n.hold.Release()
		n.hold = nil
	}
}
