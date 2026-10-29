package transactions

import (
	"context"
	"errors"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

var (
	// ErrEvidence requires missing protection, authority, or recovery evidence.
	ErrEvidence = errors.New("transaction evidence unavailable")
	// ErrConflict identifies contradictory immutable history or a reused identity.
	ErrConflict = errors.New("transaction history conflict")
	// ErrTransition identifies an operation out of protocol order.
	ErrTransition = errors.New("invalid transaction transition")
	// ErrRetired permanently rejects a retired request or closed session.
	ErrRetired = errors.New("retired transaction identity")
	// ErrLimit reports bounded admission or retained-history exhaustion.
	ErrLimit = errors.New("transaction capacity limit")
)

// Binding identifies a live authenticated normal replication session. It is
// independent of the immutable creator headers in historical frames.
type Binding struct {
	Session, Connection record.UUID
	Authority           record.Authority
}

// ProtectionRequest contains the exact durable candidate and complete retained
// local history. The transport must retain ordered prefixes, graph dependencies,
// artifacts and async catch-up obligations before reporting success. Frames are
// bounded by Options and owned by this call, never mutable engine buffers.
type ProtectionRequest struct {
	Binding   Binding
	Candidate segment.Entry
	Frames    []segment.Entry
	Required  record.Vector
	// Histories names exact owner and reporter prefix boundaries, including
	// predecessor configurations. Vector alone cannot describe several histories
	// of one volume. No sequence comparison crosses a full stream identity.
	Histories []record.FrameRef
	Artifacts []record.Artifact
	Decision  bool
}

// ProtectionReceipt is current-session retention evidence. Acceptance is the
// exact partner-authored ACCEPT frame for a decision, absent for other records.
// The transport must verify its reporter stream prefix as well as its bytes.
type ProtectionReceipt struct {
	Session, Connection  record.UUID
	Partner, Incarnation record.UUID
	Candidate            record.FrameRef
	Retained             record.Vector
	Histories            []record.FrameRef
	Acceptance           []byte
}

// Protector is the bounded normal transport/retention seam. Check must validate
// installed exclusive authority, current authenticated session and complete
// anchor recovery, both before and after potentially blocking operations. There
// is deliberately no default implementation or local-only fallback.
type Protector interface {
	Check(context.Context, Binding) error
	Protect(context.Context, ProtectionRequest) (ProtectionReceipt, error)
}

// Applier performs exact local redo or abort cleanup behind closed closure gates.
// It must durably deduplicate the immutable operation/applied cut across later
// operations: replaying old set-A after newer set-B must not restore A. Repeating
// a raw syscall alone is insufficient. A crash before INSTALL safely repeats
// Apply. Entries expose the participant, exact source position and decision.
// Deduplicate by the immutable prepared operation/position, not the decision
// ballot: fenced recovery can install that same redo under a later ballot.
// Barrier validates/drains every affected alias and returns durable attestation
// inputs; it must not publish or release gates. Publication belongs to service.
type Applier interface {
	Apply(context.Context, segment.Entry, segment.Entry) error
	Abort(context.Context, AbortRequest) error
	Barrier(context.Context, BarrierRequest) error
}

// AbortRequest identifies private cleanup even when BEGIN fan-out stopped before
// this participant's anchor or prepare. Binding and decision remain authoritative;
// missing anchor/prepare never invents namespace redo or a local outcome.
type AbortRequest struct {
	Participant      record.UUID
	Begin, Decision  segment.Entry
	Anchor, Prepared *segment.Entry
}

// BarrierRequest gives the private apply capability exact attestation and install
// frames. It must validate the actual cache/drain evidence, not trust Drained
// merely because its byte representation is true.
type BarrierRequest struct {
	Complete           record.Complete
	Installs, Barriers []segment.Entry
}

// Group supplies an exclusively owned, already initialized owner stream.
// Directory creation/formatting and accepted tail repair are outside this package.
type Group struct {
	Backend segment.Backend
	Stream  segment.Stream
	Writer  segment.Options
	Binding Binding
}

// Options supplies mandatory authority/transport and private apply capabilities.
// Header chooses fresh operation IDs and diagnostic timestamps; history and
// creator incarnation are overwritten from the group's installed authority.
// MaximumFrames/MaximumBytes bound all retained graphs; zero uses 4096/32 MiB.
type Options struct {
	Groups []Group
	// HistoryGroups preserve immutable predecessor owner streams. EvidenceGroups
	// are reporter streams for durable promises and accepted tails. Both require
	// Recovery and preserve exact historical creator bytes.
	HistoryGroups  []Group
	EvidenceGroups []Group
	Recovery       RecoveryVerifier
	Protector      Protector
	Applier        Applier
	Header         func() record.Header
	MaximumFrames  int
	MaximumBytes   int
}

// Engine serializes one closure's proposals, persistence, protection and replay.
// Close it before opening another instance over the same storage. A failed
// append requires close/reopen; failed protection requires Reconcile. All retained
// records, payloads and external artifacts remain pinned; this engine has no GC.
type Engine struct {
	mu              gate
	opts            Options
	groups          map[record.UUID]Group
	writers         map[record.UUID]*segment.Writer
	recoveryWriters map[key]*segment.Writer
	streams         map[key]Group
	entries         []segment.Entry
	protected       map[key]bool
	accepts         map[key]segment.Entry
	state           *ledger
	blocked         error
	storageErr      error
	closed          bool
	bytes           int
}

// Status reports anchored state without resolving a path or executing a request.
// Result is returned only after current protection of its complete prefix.
type Status struct {
	ID        fsapi.TransactionID
	Digest    record.Hash
	Stage     fsapi.TransactionStage
	Result    *record.TerminalResult
	Binding   *record.Binding
	Uncertain bool
}

type key struct {
	volume, generation, configuration record.UUID
	epoch, sequence                   uint64
	kind                              uint8
	reporter, incarnation             record.UUID
}

type session struct {
	registration, retirement, close *segment.Entry
	floor                           uint64
}

type request struct {
	claim, bind, begin, decision, complete, result *segment.Entry
	anchors, prepares, installs                    map[record.UUID]*segment.Entry
	candidates                                     map[uint64]*segment.Entry
	selection                                      *segment.Entry
}

type ledger struct {
	sessions   map[fsapi.SessionID]*session
	requests   map[fsapi.TransactionID]*request
	affinities map[record.AffinityRef]map[record.UUID]*segment.Entry
	active     map[record.UUID]fsapi.TransactionID
	visibility map[record.UUID]uint64
	frames     map[key]segment.Entry
	promises   map[record.UUID]uint64
}
