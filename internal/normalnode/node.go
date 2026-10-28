// Package normalnode owns the durable normal-node startup ordering. It is
// separate from the disposable local validation CLI and grants no serving rights.
package normalnode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/transport"
)

// Config loads a validated registry before durable lifecycle admission. Transport
// Identity and Registry must be unset: Start supplies them from the held lifetime.
type Config struct {
	ControlDir string
	Node       fsapi.UUID
	Loader     cluster.Loader
	Transport  transport.Config
}

// Node holds exclusive incarnation admission until sockets and attached services
// have drained. Every fresh lifetime starts RECOVERING; this API exposes no gate
// that can turn transport reachability into workload serving authority.
type Node struct {
	mu       sync.Mutex
	lifetime *cluster.Lifetime
	endpoint *transport.Endpoint
	services []io.Closer
	closed   bool
	closeErr error
}

// An uncertain closer cannot safely release its local exclusion even if its
// caller drops the Node. Pin these exceptional lifetimes until process exit so
// an os.File finalizer cannot accidentally reopen incarnation admission.
var undrainedLifetimes = struct {
	sync.Mutex
	entries []*cluster.Lifetime
}{}

// Start loads admission, persists a fresh exclusive incarnation, then opens the
// configured listener. Any failure closes sockets before releasing the lifetime.
func Start(ctx context.Context, cfg Config) (*Node, error) {
	if cfg.Transport.Registry != nil || cfg.Transport.Identity != (cluster.Identity{}) {
		return nil, fmt.Errorf("normal startup supplies transport identity/registry")
	}
	registry, err := cluster.Load(ctx, cfg.Loader)
	if err != nil {
		return nil, err
	}
	if err := registry.CheckPeer(registry.Cluster(), cfg.Node, cfg.Transport.Local.Addr()); err != nil {
		return nil, err
	}
	lifetime, err := cluster.Start(ctx, cfg.ControlDir, registry.Cluster(), cfg.Node)
	if err != nil {
		return nil, err
	}
	n := &Node{lifetime: lifetime}
	cfg.Transport.Registry, cfg.Transport.Identity = registry, lifetime.Identity()
	n.endpoint, err = transport.New(cfg.Transport)
	if err != nil {
		return nil, errors.Join(err, n.Close())
	}
	if err = n.endpoint.Listen(ctx); err != nil {
		return nil, errors.Join(err, n.Close())
	}
	return n, nil
}

// Transport exposes the authenticated transport while this Node remains open.
func (n *Node) Transport() *transport.Endpoint { return n.endpoint }

// Attach retains a service until node shutdown. Only services constructed from
// this node's lifetime may be attached. Shutdown closes transport first, then
// attached services in reverse order, and releases local exclusivity last.
func (n *Node) Attach(service io.Closer) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || service == nil {
		return fmt.Errorf("closed node or absent service")
	}
	n.services = append(n.services, service)
	return nil
}

// Close drains sockets and services before releasing the durable lifetime.
// Any service close failure retains exclusivity until process exit/inspection;
// retry does not release admission after an uncertain drain.
func (n *Node) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return n.closeErr
	}
	n.closed = true
	if n.endpoint != nil {
		n.closeErr = errors.Join(n.closeErr, n.endpoint.Close())
	}
	for i := len(n.services) - 1; i >= 0; i-- {
		n.closeErr = errors.Join(n.closeErr, n.services[i].Close())
	}
	if n.closeErr != nil {
		undrainedLifetimes.Lock()
		undrainedLifetimes.entries = append(undrainedLifetimes.entries, n.lifetime)
		undrainedLifetimes.Unlock()
		return n.closeErr
	}
	n.closeErr = n.lifetime.Close()
	return n.closeErr
}
