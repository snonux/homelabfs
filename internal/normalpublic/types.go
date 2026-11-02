// Package normalpublic implements a separately versioned ordinary public claim
// representation and local durable ledger. It grants no serving authority.
package normalpublic

import (
	"context"
	"errors"

	"github.com/snonux/homelabfs/internal/journal/record"
)

const (
	// Major and Minor identify this public binding format, independently of normal3.
	Major uint16 = 1
	Minor uint16 = 0
	// MaxFrame bounds every decoded source and journal entry before allocation.
	MaxFrame = 8 << 20
)

var (
	// ErrConflict means an allocated identity already has different immutable bytes.
	ErrConflict = errors.New("public claim identity conflict")
	// ErrBlocked means evidence or a complete prior number is unavailable.
	ErrBlocked = errors.New("public claim blocked")
	// ErrRetired permanently rejects attempts at or below a protected floor.
	ErrRetired = errors.New("public claim retired")
)

// Anchor is the authenticated, permanently serialized request-number namespace.
type Anchor struct {
	Realm         record.UUID
	Subject       []byte
	Anchor, Nonce record.UUID
}

// Allocation permanently binds a serialized allocated number to its original intent digest.
type Allocation struct {
	Number   uint64
	Original record.Hash
}

// Claim adds one nonreused allocated number and immutable intent digest.
type Claim struct {
	Anchor   Anchor
	Number   uint64
	Original record.Hash
}

// Scope retains complete original enrollment, rather than the retry's exporter.
type Scope struct {
	Export, ExporterIncarnation, FuseSession record.UUID
	GuestBoot, GuestMount, Session, VM       record.UUID
	Policy, Registry, Authority              record.Hash
}

// Credentials binds the complete VFS credential and provenance snapshot.
// Provenance contains the exact attestation/mapping bytes, verified externally.
type Credentials struct {
	FSUID, FSGID           uint32
	Groups                 []uint32
	Process, UserNamespace record.UUID
	Revision               uint64
	IDMap                  record.Hash
	Provenance             []byte
}

// Binding is immutable once reserved. Intent is the exact canonical ordinary
// operation, including capability/provenance references, never a rewritten redo.
// The admission verifier must validate its closed operation schema and semantics.
type Binding struct {
	Claim        Claim
	Scope        Scope
	Credentials  Credentials
	Intent       []byte
	Operation    record.UUID
	Origin       record.OriginStream
	OriginLaunch record.Launch
	Authority    record.NormalAuthority
	Dependencies []Ref
	// Sources name existing normal3 dependencies only; public originals remain in this format.
	Sources []record.NormalRef
}

// Kind is a closed public-source union; these are never normal3 record tags.
type Kind uint8

const (
	BindingKind Kind = iota + 1
	ChoiceKind
	SelectionKind
	ResultKind
)

// Ref identifies exact original public source bytes, including their creator.
type Ref struct {
	Stream   record.OriginStream
	Sequence uint64
	Creator  record.Launch
	Kind     Kind
	Hash     record.Hash
}

// Choice preserves an irrevocable original-origin COMMIT (1) or DECLINE (2).
type Choice struct {
	Binding Ref
	Outcome uint8
}

// Selection fixes an ordered choice under the exact installed closure authority.
// Predecessor is null only for serial 1, whose Baseline is mandatory.
type Selection struct {
	Choice      Ref
	Slot        record.OrderedSlot
	Predecessor *Ref
	Baseline    record.Hash
}

// Result preserves exact terminal semantic bytes at an exact selected choice.
type Result struct {
	Selection Ref
	Bytes     []byte
}

// Frame has exactly one payload, selected by Kind. Sequences are origin-local;
// codec/graph validation never infers durable prefix coverage from them.
type Frame struct {
	Stream    record.OriginStream
	Sequence  uint64
	Creator   record.Launch
	Operation record.UUID
	Kind      Kind
	Binding   *Binding
	Choice    *Choice
	Selection *Selection
	Result    *Result
}

// Graph owns complete exact source bytes, including every transitive dependency.
type Graph struct {
	Root           Ref
	Frames         [][]byte
	NormalRecovery *record.NormalRecovery
	NormalFrames   [][]byte
}

// Protection includes exact bytes plus external authenticated durability,
// retention and visibility evidence. Evidence is never interpreted as a boolean.
type Protection struct {
	Graph    Graph
	Evidence []byte
}

// Status keeps missing, unsubmitted and uncertain effects distinct.
type Status string

const (
	Unknown         Status = "UNKNOWN"
	Unsent          Status = "UNSENT"
	Pending         Status = "PENDING"
	Indeterminate   Status = "INDETERMINATE"
	CancelRequested Status = "CANCEL_REQUESTED"
	Terminal        Status = "TERMINAL"
	Gap             Status = "PROTECTED_GAP"
	Retired         Status = "RETIRED_ID"
)

// Entry is an independently owned query result. Only Terminal contains Result.
type Entry struct {
	Original    record.Hash
	Number      uint64
	Status      Status
	Binding     *Binding
	Result      []byte
	Protection  *Protection
	GapEvidence []byte
}

// Retirement binds every number in an exact contiguous floor advancement.
// Entries contain terminal protected results or protected no-effect gaps.
type Retirement struct {
	Anchor            Anchor
	Previous, Through uint64
	Entries           []Entry
	Evidence          []byte
}

// Authentication checks live lookup authority before revealing a known binding.
// Forwarded/reconciled retries must authenticate original-anchor access; fresh
// capability/path validation belongs only in Admission after a failed lookup.
type Authentication interface {
	Lookup(context.Context, Scope, Anchor) error
	Allocation(context.Context, Scope, Anchor, uint64, []Allocation, []byte) error
}

// Admission validates original canonical intent, complete credentials/provenance,
// installed scope/authority, unique operation and original O before reservation.
type Admission interface {
	VerifyAdmission(context.Context, Binding) error
}

// Retention must establish actual original-O durable contiguous fragment/result,
// exact distinct M retained bytes, authority, selected outcome, source pins and
// complete visibility barrier. Codec success, maxima and a local sync cannot.
// VerifyGap proves authenticated serialized anchor no-effect for an UNSENT number;
// a missing journal record is insufficient. VerifyRetirement proves complete
// protected result/gap coverage and permanent floor retention. VerifyRecovery
// validates retained ownership/continuity and exact journal bytes on every open.
// There is deliberately no implementation or permissive default in this package.
type Retention interface {
	VerifyProtection(context.Context, Binding, Protection) error
	VerifyGap(context.Context, Scope, Anchor, uint64, []byte) error
	VerifyRetirement(context.Context, Scope, Retirement) error
	VerifyRecovery(context.Context, record.UUID, []byte) error
}

// Limits are explicit finite bounds; zero is invalid rather than a default.
type Limits struct {
	Anchors, Entries, GraphFrames int
	JournalBytes                  int64
}

// Config supplies mandatory independent proof boundaries and resource limits.
type Config struct {
	// LedgerID is the permanently enrolled ledger identity, including on an empty/new path.
	LedgerID       record.UUID
	Authentication Authentication
	Admission      Admission
	Retention      Retention
	Limits         Limits
}
