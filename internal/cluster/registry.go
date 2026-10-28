package cluster

import (
	"context"
	"fmt"
	"net/netip"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Admission is an explicitly configured node admission state.
type Admission string

const (
	// Admitted permits transport and assigned-volume placement checks.
	Admitted Admission = "admitted"
	// Pending records an identity without admitting transport or volume access.
	Pending Admission = "pending"
	// Removed retains the identity/address reservation without admitting access.
	Removed Admission = "removed"
)

// Role is a node's fixed assignment within a volume group.
type Role string

const (
	// Primary is the sole configured owner-stream originator.
	Primary Role = "primary"
	// Sync is the configured synchronous partner.
	Sync Role = "sync"
	// Async is the configured asynchronous replica.
	Async Role = "async"
)

// Config describes one static registry revision. New copies all input slices.
type Config struct {
	Cluster  fsapi.UUID
	Revision uint64
	Nodes    []Node
	Volumes  []Volume
}

// Node binds an identity to exact approved host addresses. Deployment must
// separately authenticate the host-to-address WireGuard mapping and routes.
type Node struct {
	ID        fsapi.UUID
	Admission Admission
	Addresses []netip.Addr
}

// History identifies one exact fixed-owner volume history and coordinator term.
type History struct {
	Generation, Configuration fsapi.UUID
	Epoch, CoordinatorTerm    uint64
}

// Member assigns one node a role; initial groups contain exactly three members.
type Member struct {
	Node fsapi.UUID
	Role Role
}

// Volume is a configured history and its distinct primary, sync and async nodes.
type Volume struct {
	ID      fsapi.UUID
	History History
	Members []Member
}

// Loader supplies a complete registry snapshot. Future coordination adapters
// can implement this seam without adding coordination calls to the I/O path.
type Loader interface {
	Load(context.Context) (Config, error)
}

// Registry is immutable and safe for concurrent use. Its zero value denies all
// membership and placement checks. It never grants workload serving authority.
type Registry struct {
	cluster  fsapi.UUID
	revision uint64
	nodes    map[fsapi.UUID]Node
	volumes  map[fsapi.UUID]Volume
}

// New validates a complete snapshot without enrolling implicit nodes or roles.
func New(c Config) (*Registry, error) {
	if !c.Cluster.Valid() || c.Revision == 0 || len(c.Nodes) == 0 || len(c.Nodes) > 4096 || len(c.Volumes) > 4096 {
		return nil, fmt.Errorf("invalid cluster identity, revision or registry bounds")
	}
	r := &Registry{cluster: c.Cluster, revision: c.Revision, nodes: make(map[fsapi.UUID]Node), volumes: make(map[fsapi.UUID]Volume)}
	addresses := make(map[netip.Addr]bool)
	for _, n := range c.Nodes {
		if err := r.addNode(n, addresses); err != nil {
			return nil, err
		}
	}
	for _, v := range c.Volumes {
		if err := r.addVolume(v); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Load reads and validates a snapshot exactly once; no hot reload is implied.
func Load(ctx context.Context, loader Loader) (*Registry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if loader == nil {
		return nil, fmt.Errorf("missing cluster loader")
	}
	c, err := loader.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load cluster registry: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return New(c)
}

// Cluster returns the configured cluster UUID.
func (r *Registry) Cluster() fsapi.UUID { return r.cluster }

// Revision returns the installed static registry revision.
func (r *Registry) Revision() uint64 { return r.revision }

// Node returns a detached identity record, including nonadmitted records.
func (r *Registry) Node(id fsapi.UUID) (Node, bool) {
	n, ok := r.nodes[id]
	n.Addresses = slices.Clone(n.Addresses)
	return n, ok
}

// Volume returns a detached configured placement, never a serving grant.
func (r *Registry) Volume(id fsapi.UUID) (Volume, bool) {
	v, ok := r.volumes[id]
	v.Members = slices.Clone(v.Members)
	return v, ok
}

// CheckPeer checks claimed cluster/node against an already authenticated,
// observed transport host address. It must run on every new connection. This
// does not authenticate a socket, validate its incarnation or create a session.
func (r *Registry) CheckPeer(cluster, node fsapi.UUID, observed netip.Addr) error {
	if err := r.checkNode(cluster, node); err != nil {
		return err
	}
	if !slices.Contains(r.nodes[node].Addresses, observed) {
		return fmt.Errorf("node %s has no approved transport address %s", node, observed)
	}
	return nil
}

// CheckAssignment checks exact cluster, node, volume, history and role against
// static placement. No supplied epoch or term overrides installed configuration.
// Callers must separately verify session/incarnation, promises and readiness.
func (r *Registry) CheckAssignment(cluster, node, volume fsapi.UUID, history History, role Role) error {
	if err := r.checkNode(cluster, node); err != nil {
		return err
	}
	v, ok := r.volumes[volume]
	if !ok {
		return fmt.Errorf("unknown volume %s", volume)
	}
	if history != v.History {
		return fmt.Errorf("volume %s history does not match installed configuration", volume)
	}
	if !slices.Contains(v.Members, Member{Node: node, Role: role}) {
		return fmt.Errorf("node %s is not assigned role %q on volume %s", node, role, volume)
	}
	return nil
}

func (r *Registry) checkNode(cluster, node fsapi.UUID) error {
	if !r.cluster.Valid() || cluster != r.cluster {
		return fmt.Errorf("foreign or unconfigured cluster %s", cluster)
	}
	n, ok := r.nodes[node]
	if !ok || n.Admission != Admitted {
		return fmt.Errorf("node %s is not admitted", node)
	}
	return nil
}

func (r *Registry) addNode(n Node, addresses map[netip.Addr]bool) error {
	if !n.ID.Valid() || n.ID == r.cluster || len(n.Addresses) == 0 || len(n.Addresses) > 16 {
		return fmt.Errorf("invalid node identity or address bounds")
	}
	if _, ok := r.nodes[n.ID]; ok {
		return fmt.Errorf("duplicate node %s", n.ID)
	}
	if n.Admission != Admitted && n.Admission != Pending && n.Admission != Removed {
		return fmt.Errorf("invalid node admission %q", n.Admission)
	}
	for _, a := range n.Addresses {
		if !a.IsValid() || a.IsUnspecified() || a.IsMulticast() || a.Is4In6() || a.Zone() != "" || addresses[a] {
			return fmt.Errorf("invalid or duplicate node address %s", a)
		}
		addresses[a] = true
	}
	n.Addresses = slices.Clone(n.Addresses)
	r.nodes[n.ID] = n
	return nil
}

func (r *Registry) addVolume(v Volume) error {
	h := v.History
	if !v.ID.Valid() || !h.Generation.Valid() || !h.Configuration.Valid() || h.Epoch == 0 || h.CoordinatorTerm == 0 || len(v.Members) != 3 {
		return fmt.Errorf("invalid volume history or member count")
	}
	if v.ID == r.cluster {
		return fmt.Errorf("volume reuses cluster identity")
	}
	if _, ok := r.nodes[v.ID]; ok {
		return fmt.Errorf("volume reuses node identity")
	}
	if _, ok := r.volumes[v.ID]; ok {
		return fmt.Errorf("duplicate volume %s", v.ID)
	}
	nodes, roles := make(map[fsapi.UUID]bool), make(map[Role]bool)
	for _, m := range v.Members {
		n, ok := r.nodes[m.Node]
		if !ok || n.Admission != Admitted || nodes[m.Node] || roles[m.Role] || (m.Role != Primary && m.Role != Sync && m.Role != Async) {
			return fmt.Errorf("invalid, duplicate or unadmitted member on volume %s", v.ID)
		}
		nodes[m.Node], roles[m.Role] = true, true
	}
	v.Members = slices.Clone(v.Members)
	r.volumes[v.ID] = v
	return nil
}
