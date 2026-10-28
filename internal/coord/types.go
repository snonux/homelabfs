package coord

import (
	"context"
	"errors"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// Capability selects one externally authenticated etcd principal's privileges.
type Capability uint8

const (
	// Daemon permits reads and policy watches only.
	Daemon Capability = iota + 1
	// Controller additionally permits normal takeover allocation.
	Controller
	// Operator permits admission policy changes, but not takeover allocation.
	Operator
)

// Mode selects persistent normal or emergency admission policy.
type Mode string

const (
	// Normal permits admitted members to request takeover allocations.
	Normal Mode = "normal"
	// Emergency blocks all normal allocations and restricts recovery admission.
	Emergency Mode = "emergency"
)

var (
	// ErrConflict means an exact expected revision changed; reread before retrying.
	ErrConflict = errors.New("coordination revision conflict")
	// ErrDenied means policy or principal denies this action.
	ErrDenied = errors.New("coordination action denied")
	// ErrUnavailable means coordination quorum or connectivity is unavailable.
	ErrUnavailable = errors.New("coordination unavailable")
	// ErrCompacted means a watch cursor cannot be replayed; obtain a fresh snapshot.
	ErrCompacted = errors.New("coordination watch compacted")
	// ErrNotFound means no record has been installed.
	ErrNotFound = errors.New("coordination record not found")
)

// RecoveryPeer is a manually installed, recovery-only emergency permission.
type RecoveryPeer struct {
	Node    fsapi.UUID
	Volumes []fsapi.UUID
}

// Policy stores admission, fixed placement and emergency restrictions together.
// This is a candidate control policy; storing it never activates serving rights.
type Policy struct {
	Format              uint64
	Registry            cluster.Config
	Mode                Mode
	EmergencyGeneration fsapi.UUID
	ServingNode         fsapi.UUID
	Recovery            []RecoveryPeer
}

// Snapshot is an immutable-by-copy policy, exact CAS revision and gap-free
// watch cursor. Use Revision for PutPolicy and Request, WatchRevision for watches.
type Snapshot struct {
	Policy        Policy
	Revision      int64
	WatchRevision int64
}

// Grant authorizes beginning takeover only. It is not a lease, fence, promise,
// activation certificate, replica readiness or authorization to serve any I/O.
type Grant struct {
	Format                              uint64
	Cluster, Volume, Owner, Incarnation fsapi.UUID
	History                             cluster.History
	PolicyRevision                      int64
}

// Allocation is a grant and its exact per-volume compare-and-swap revision.
type Allocation struct {
	Grant    Grant
	Revision int64
}

// Request starts a new normal takeover attempt against two exact snapshots.
// ExpectedGrant is zero only when no allocation has ever been recorded.
type Request struct {
	Volume, Owner, Incarnation    fsapi.UUID
	PolicyRevision, ExpectedGrant int64
}

// Event delivers a complete replacement candidate or one terminal watch error.
// Watches are notifications, never evidence of continuing serving authority.
type Event struct {
	Snapshot Snapshot
	Err      error
}

// Reader supplies infrequent control reads and revisioned policy notifications.
type Reader interface {
	GetPolicy(context.Context) (Snapshot, error)
	GetGrant(context.Context, fsapi.UUID) (Allocation, error)
	WatchPolicy(context.Context, int64) (<-chan Event, error)
}

// Allocator serializes normal takeover attempts; durable storage fencing remains external.
type Allocator interface {
	Allocate(context.Context, Request) (Allocation, error)
}

// PolicyWriter changes admission policy using exact store revisions.
type PolicyWriter interface {
	PutPolicy(context.Context, int64, Policy) (Snapshot, error)
}

type value struct {
	bytes    []byte
	revision int64
	observed int64
}
type condition struct {
	key      string
	revision int64
}
type update struct {
	key   string
	bytes []byte
}
type change struct {
	value value
	err   error
}
type backend interface {
	get(context.Context, string) (value, error)
	commit(context.Context, []condition, []update) (int64, error)
	watch(context.Context, string, int64) (<-chan change, error)
}

// Client applies the same validation and CAS contract to etcd and simulation.
// Its zero value denies all use. It has no filesystem operation methods.
type Client struct {
	store            backend
	cluster          fsapi.UUID
	capability       Capability
	prefix           string
	close            func() error
	newConfiguration func() (fsapi.UUID, error)
}

var (
	_ Reader       = (*Client)(nil)
	_ Allocator    = (*Client)(nil)
	_ PolicyWriter = (*Client)(nil)
)
