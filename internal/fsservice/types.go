package fsservice

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

// State is the per-volume serving gate.
type State uint32

const (
	// Recovering keeps all workload access closed during reconstruction.
	Recovering State = iota + 1
	// Serving admits commands behind the current authority/apply gates.
	Serving
	// Blocked preserves uncertain state and rejects new commands until recovery.
	Blocked
)

// Proposal owns the exact newly authored bytes and source references. Protect
// must retain their entire ordered prefix and dependencies (including async
// delivery obligations), not acknowledge just these frames or their hashes.
type Proposal struct {
	Previous, Through record.FrameRef
	Frames            []segment.Entry
	Selection         apply.Selection
}

// Protocol supplies the real normal-mode trust boundary. Check verifies current
// exclusive authority, partner/session readiness and visibility for the cut at
// both admission and completion. Protect must establish the commit/visibility
// conditions and partner-retained complete prefix, returning an owned proof for
// Authorize. Local Flush runs concurrently; success still requires both results.
// Hooks must impose bounded internal deadlines; request wait cancellation does
// not abort an admitted command. Protect may read the exclusively stable source
// through caller-configured read access while Flush runs, and must close every
// reader before returning. Frames excludes writer-owned OPEN/SEAL; obtain and
// retain the full prefix, including those structural frames, from that source.
// Authorize verifies startup and online complete selections and retains external
// gates through application. No production implementation is supplied here.
type Protocol interface {
	apply.Authorizer
	Check(context.Context, fsapi.Position) error
	Protect(context.Context, Proposal) ([]byte, error)
}

// PermissionPolicy authorizes freshly applied logical metadata and credentials.
// qq3 will implement the Unix permission policy. The service requires an explicit
// policy in normal mode; a privileged daemon never implies workload permission.
// attr contains all objects involved, including parents/replacement. Hooks must
// not reenter the service or mutate command buffers concurrently.
type PermissionPolicy interface {
	Check(context.Context, fsapi.Cred, fsapi.Command, []fsapi.Attr) (PermissionDecision, error)
}

// PermissionDecision binds successful permission checks to a view and policy
// revision for immutable journal authorization inputs.
type PermissionDecision struct {
	View     fsapi.UUID
	Revision uint64
}

// ValidationProfile explicitly permits local-only disposable harness results.
// It supplies no fake ACCEPT, normal durability, emergency authority or daemon
// publication. Both fields are mandatory and normal Protocol must be absent.
type ValidationProfile struct {
	Disposable bool
	Name       string
}

// Workspace supplies fresh, private reconstruction roots, retained on any error.
// ID must be new for each recovery, including failed attempts. Progress can be
// retained across restart; data/journal must always be new empty roots.
type Workspace struct {
	ID                      fsapi.UUID
	Data, Journal, Progress storage.Filesystem
}

// Limits bounds queued immutable commands, I/O, handles, lookup references,
// admitted logical bytes and retained reservation discovery. Zero uses defaults.
type Limits struct {
	Queue, IOBytes, Handles, Lookups, Reservations int
	LogicalBytes                                   int64
}

// Config transfers exclusive source writer ownership for one volume to Service.
// The caller owns and closes all filesystem roots after Close. Roots must be
// private, disjoint (including ancestors), administratively exclusive and on
// admitted storage. The service checks exact root identities/ownership. Source
// ancestors must already be durably prepared; volume initialization is external.
// No daemon, mount, checkpoint acceptance or retention is configured here.
type Config struct {
	Source, Reservations storage.Filesystem
	Stream               segment.Stream
	Authority            record.Authority
	Storage              storage.Config
	Workspace            Workspace
	Selection            apply.Selection
	Protocol             Protocol
	Permissions          PermissionPolicy
	Validation           *ValidationProfile
	Clock                fsapi.Clock
	Random               io.Reader
	Limits               Limits
	Segment              segment.Options
	Apply                apply.Config
}

type command struct {
	request *fsapi.Request
	value   fsapi.Command
}
type lifetime struct {
	pin     fsapi.UUID
	lookups uint64
	handles uint64
}
type handle struct {
	object fsapi.ObjectID
	flags  fsapi.OpenFlags
}

// Service coordinates one volume. All catalog, payload and source work runs on
// its bounded worker. Multiple instances may serve disjoint admitted volumes;
// foreign-home coordination and explicit transactions remain a separate service.
// It must not be copied. Close drains accepted commands without canceling them.
type Service struct {
	config      Config
	state       atomic.Uint32
	mu          sync.Mutex
	closed      bool
	accepted    map[*fsapi.Request]bool
	queue       chan command
	done        chan struct{}
	writer      *segment.Writer
	applied     apply.Result
	selection   apply.Selection
	ids         map[fsapi.UUID]bool
	allocations map[fsapi.ObjectID]bool
	lifetimes   map[fsapi.ObjectID]*lifetime
	handles     map[fsapi.OpenHandle]handle
	nextHandle  fsapi.OpenHandle
	lookupCount uint64
}

var _ fsapi.Filesystem = (*Service)(nil)
