// Package logicalview retains bounded immutable owner RAM projections of
// externally verified committed closure cuts. It performs no network protocol,
// source sync, fencing or platform cache invalidation. No production verifier is
// supplied, and constructing a view never authorizes normal daemon publication.
package logicalview

import (
	"context"
	"sync"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

// Phase names an external trust boundary; codec validity is never its proof.
type Phase uint8

const (
	Baseline Phase = iota + 1
	ReadObservation
	HandleAdmission
	HandleRetirement
	Candidate
	Selection
	Publication
	BackingInstalled
	BackingDurable
	Retirement
	Recovery
)

// Domain identifies an admitted frontend cache/observation lifetime. Every
// reader and writer, including native/root and retained guest aliases, belongs
// in this inventory. Replacing a session requires external positive exclusion.
type Domain struct{ ID, Session fsapi.UUID }

// HandleCheck binds a home lifetime obligation to an exact description. The
// verifier must protect its home pin before admission and preserve/reconcile
// source obligations before retirement; a RAM map alone is insufficient.
type HandleCheck struct {
	ID         fsapi.OpenHandle
	Object     fsapi.ObjectID
	Flags      fsapi.OpenFlags
	Generation fsapi.UUID
}

// Check is owned input to the external authority/source/barrier verifier.
// Candidate checks include the complete replay bytes and prior source graph.
// Selection requires origin durable commit and distinct recipient retention;
// Publication requires stopped/drained/installed acknowledgements for every
// exact Domain. Retirement requires equivalent durable coverage before release.
type Check struct {
	Handle    *HandleCheck
	Recovery  *RecoveryState
	Phase     Phase
	Authority record.NormalAuthority
	Stage     Stage
	Evidence  Evidence
	Domains   []Domain
	Baseline  []storage.Snapshot
}

// Verifier authenticates current authority and actual durability/retention/cache
// facts, maintaining the closure gate across Check and the synchronous caller.
// Hooks must not reenter Store. There is deliberately no default implementation.
type Verifier interface {
	Check(context.Context, Check) error
}

// Limits bound a preallocated admission arena. MaxSnapshotBytes counts complete
// closure catalog encoding plus all logical payload bytes (including holes).
// Four extra snapshot slots cover candidate/temporary copies; recovery has its
// own isolated reservation. Zero fields select conservative finite defaults.
type Limits struct {
	MaxSnapshotBytes, MaxClaimBytes                              int64
	Versions, Claims, Handles, Readers, Dependencies, Identities int
	Storage                                                      storage.Limits
}

// Config transfers a verified complete baseline into owned RAM. All authority
// volumes must occur exactly once. Generation identifies this serving lifetime,
// not an origin offset. Source pins and unresolved claims survive recovery.
type Config struct {
	Authority  record.NormalAuthority
	Generation fsapi.UUID
	Baseline   []storage.Snapshot
	Sources    []record.SourceCoverage
	Domains    []Domain
	Budget     *admission.Budget
	Verifier   Verifier
	Limits     Limits
}

// Stage binds immutable internal claim bytes to one exact ordered candidate.
// Operations are complete per-home primitive afterimages, including aliases,
// home ledgers and pin changes; they are not origin-journal sequence numbers.
// Dependencies contain actual pinned replay bytes, never fetch promises.
// This generic form also supports home fragments without extending normal3's
// ordinary grant codec or enabling the unsupported public transaction protocol.
type Stage struct {
	Slot                    record.OrderedSlot
	Previous                *record.SelectedRef
	Origin                  record.OriginStream
	OriginLaunch, Recipient record.Launch
	Claim                   fsapi.UUID
	Intent                  []byte
	Operations              []storage.Operation
	Dependencies            []Dependency
}

// Dependency owns exact original creator bytes pinned to durable coverage.
type Dependency struct {
	Ref   record.NormalRef
	Bytes []byte
}

// Evidence binds an externally checked transition to its exact candidate and
// selected certificate. Result bytes are immutable; absence remains uncertain.
// Sources are actual durable coverage, distinct from acceptance references.
// Acks must exactly match the full current domain inventory for publication.
type Evidence struct {
	NoEffect      bool
	Claims        []fsapi.UUID
	Digest        record.Hash
	Selected      record.SelectedRef
	Sources       []record.SourceCoverage
	Acceptances   []record.NormalRef
	Result, Proof []byte
	Acks          []Domain
}

// Frontiers separates per-origin receipt/durability from closure selection,
// visible RAM and asynchronously installed/durable backing cuts.
type Frontiers struct {
	Sources                               []SourceFrontier
	Selected, Visible, Installed, Durable *record.SelectedRef
}

// SourceFrontier records one complete stream's actual source positions.
type SourceFrontier struct {
	Stream            record.OriginStream
	Received, Durable uint64
}

// Claim retains immutable request/selection/result/source evidence. Selected
// false is unresolved, never a no-effect choice. Backpressure preserves it.
type Claim struct {
	Stage    Stage
	Digest   record.Hash
	Evidence *Evidence
}

type version struct {
	cut           *record.SelectedRef
	images        map[fsapi.UUID]storage.Snapshot
	pins          int
	recoveredPins int
}
type pending struct {
	claim Claim
	next  *version
}
type openHandle struct {
	Object fsapi.ObjectID
	Flags  fsapi.OpenFlags
}

// Store serializes a dependency closure. Candidates, versions, claims and
// sources stay owned until explicit verified retirement. It must not be copied.
type Store struct {
	mu                sync.Mutex
	config            Config
	hash              record.Hash
	arena, recovery   []*admission.Lease
	versions          []*version
	selected, visible *version
	pending           *pending
	claims            map[fsapi.UUID]Claim
	used              map[fsapi.ObjectID]bool
	identities        map[fsapi.UUID]uint8
	allocations       map[fsapi.ObjectID]fsapi.UUID
	handles           map[fsapi.OpenHandle]openHandle
	nextHandle        fsapi.OpenHandle
	readers           int
	pinCount          int
	frontiers         Frontiers
	closed            bool
}

// Observation pins one atomic visible closure cut. All planning, metadata,
// authorization and reads use these same images. Close ends the old-generation
// observation; publication cannot proceed while any observation is active.
type Observation struct {
	mu      sync.Mutex
	store   *Store
	version *version
	closed  bool
}

// VersionPin retains an old immutable version for an internal obligation. It
// grants no frontend observation or source-release authority.
type VersionPin struct {
	store   *Store
	version *version
	once    sync.Once
}
