package fsapi

import (
	"fmt"
	"slices"
)

// Position identifies one contiguous committed prefix in a particular history.
// A sequence number alone cannot identify replay or recovered state.
type Position struct {
	Volume, Generation, Configuration UUID
	Epoch, Sequence                   uint64
}

// DecisionID identifies the immutable coordinator decision slot for a request.
type DecisionID struct {
	Coordinator, Generation, Configuration UUID
	Epoch, Term                            uint64
	Request                                TransactionID
}

// DecisionRef identifies both the slot and the specific protected decision record.
type DecisionRef struct {
	ID     DecisionID
	Record Position
	Digest Digest
}

// Visibility identifies the shared namespace generation published after all installs.
type Visibility struct {
	Closure              UUID
	Manifest, Generation uint64
}

// CommittedResult is a visible, fully protected transaction receipt. Object is
// the moved/linked source identity; Replaced is absent for no replacement.
// Positions cover every participant, including the immutable request anchor.
type CommittedResult struct {
	Object     ObjectID
	Replaced   *ObjectID
	Decision   DecisionRef
	Positions  []Position
	Visibility Visibility
}

// OutcomeState describes the actual transaction outcome, independently of a
// transport wait or cancellation. Pending and Indeterminate are not rollback.
type OutcomeState uint8

const (
	// Rejected proves no admission and no namespace/object effects.
	Rejected OutcomeState = iota + 1
	// Pending records a claim or admission without an established terminal outcome.
	Pending
	// Committed reports visible effects satisfying the durability contract.
	Committed
	// Aborted reports a protected abort with locks and staging released.
	Aborted
	// Indeterminate reports missing authority/evidence, requiring same-ID recovery.
	Indeterminate
	// RetiredID reports an unusable identity covered by a durable floor/tombstone.
	RetiredID
)

// TransactionStage reports how far a nonterminal request has progressed.
type TransactionStage uint8

const (
	// Claimed means the immutable anchor has protected the original request.
	Claimed TransactionStage = iota + 1
	// Bound means the anchor has protected immutable coordinator/participant binding.
	Bound
	// Preparing means admitted participants are preparing the operation.
	Preparing
	// Deciding means the protected decision is being resolved.
	Deciding
	// Installing means an irreversible commit is installing behind visibility gates.
	Installing
)

// TransactionCode classifies protected rejections and aborts, not native errno.
type TransactionCode uint8

const (
	// NoFailure accompanies outcomes without a rejection/abort code.
	NoFailure TransactionCode = iota
	// InvalidInput indicates invalid endpoint or request syntax.
	InvalidInput
	// UnsupportedVersion indicates an unsupported original-request version.
	UnsupportedVersion
	// UnsupportedOperation indicates an unsupported operation or capability.
	UnsupportedOperation
	// MissingObject indicates a missing source or parent.
	MissingObject
	// DestinationExists indicates a forbidden replacement.
	DestinationExists
	// DirectoryNotEmpty indicates a nonempty replacement directory.
	DirectoryNotEmpty
	// AccessDenied indicates authorization or Unix permission denial.
	AccessDenied
	// ReadOnlyView indicates a forbidden mutation through a read-only view.
	ReadOnlyView
	// RevisionConflict indicates failed original compare-and-set expectations.
	RevisionConflict
	// CycleOrRoot indicates an invalid directory ancestry/root mutation.
	CycleOrRoot
	// PlacementConflict indicates incompatible ownership, membership or partners.
	PlacementConflict
	// AuthorityUnavailable indicates unavailable exclusive serving authority.
	AuthorityUnavailable
	// ResourceLimit indicates exhausted admitted capacity or counters.
	ResourceLimit
	// TransactionIO indicates a preflight or protected abort I/O failure.
	TransactionIO
	// IDConflict indicates that this anchored identity already claims other bytes.
	IDConflict
	// UnknownID is a proven absent request under available authoritative history.
	UnknownID
	// AbortRequested indicates a protected abort won before a commit proposal.
	AbortRequested
)

// Uncertainty classifies an unresolved outcome without claiming rollback.
type Uncertainty uint8

const (
	// NoUncertainty accompanies an outcome whose state is known.
	NoUncertainty Uncertainty = iota
	// MissingAuthority indicates an anchor/coordinator whose authority is uncertain.
	MissingAuthority
	// MissingHistory indicates unavailable recovery or deduplication evidence.
	MissingHistory
	// DecisionUnresolved indicates an unresolved protected decision slot.
	DecisionUnresolved
)

// TransactionOutcome is the typed Execute/GetStatus/Cancel reply. When Claimed
// is true, ID and RequestDigest describe the anchor's protected original claim.
// IDConflict therefore returns the existing claim's digest. An error after
// admission remains Pending/Indeterminate until a protected terminal outcome.
type TransactionOutcome struct {
	ID            TransactionID
	Claimed       bool
	RequestDigest Digest
	State         OutcomeState
	Code          TransactionCode
	Stage         TransactionStage
	Uncertainty   Uncertainty
	Result        *CommittedResult
	AbortDecision *DecisionRef
}

// Valid reports whether a position has all required history identities.
// Sequence zero denotes the empty contiguous prefix of a valid history.
func (p Position) Valid() bool {
	return p.Volume.Valid() && p.Generation.Valid() && p.Configuration.Valid() && p.Epoch != 0
}

// Valid reports whether the slot is scoped by complete authority and request identity.
func (d DecisionID) Valid() bool {
	return d.Coordinator.Valid() && d.Generation.Valid() && d.Configuration.Valid() && d.Epoch != 0 && d.Term != 0 && d.Request.Valid()
}

// Valid reports whether the record belongs to the decision's coordinator history.
func (d DecisionRef) Valid() bool {
	return d.ID.Valid() && d.Record.Valid() && d.Record.Sequence != 0 && d.Record.Volume == d.ID.Coordinator && d.Record.Generation == d.ID.Generation && d.Record.Configuration == d.ID.Configuration && d.Record.Epoch == d.ID.Epoch
}

// Validate checks the structure of a typed outcome. It cannot verify durable
// evidence, serving authority or visibility; those remain service obligations.
func (o TransactionOutcome) Validate() error {
	return o.validate()
}

func (r CommittedResult) validate(id TransactionID) error {
	if !r.Object.Valid() || r.Replaced != nil && !r.Replaced.Valid() || !r.Decision.Valid() || r.Decision.ID.Request != id || !r.Visibility.Closure.Valid() || r.Visibility.Manifest == 0 || r.Visibility.Generation == 0 || len(r.Positions) == 0 {
		return fmt.Errorf("%w: committed transaction receipt", Invalid)
	}
	seen := make(map[UUID]bool, len(r.Positions))
	for _, position := range r.Positions {
		if !position.Valid() || seen[position.Volume] {
			return fmt.Errorf("%w: committed positions", Invalid)
		}
		if position.Volume == r.Decision.ID.Coordinator && (position.Generation != r.Decision.Record.Generation || position.Configuration != r.Decision.Record.Configuration || position.Epoch != r.Decision.Record.Epoch || position.Sequence < r.Decision.Record.Sequence) {
			return fmt.Errorf("%w: committed prefix does not cover decision", Invalid)
		}
		seen[position.Volume] = true
	}
	if !seen[id.Session.Anchor] || !seen[r.Decision.ID.Coordinator] || !seen[r.Object.Home] || r.Replaced != nil && !seen[r.Replaced.Home] {
		return fmt.Errorf("%w: missing anchor/coordinator/object-home position", Invalid)
	}
	return nil
}

func (o TransactionOutcome) validate() error {
	if !o.ID.Valid() || o.State < Rejected || o.State > RetiredID || o.Code > AbortRequested || o.Stage > Installing || o.Uncertainty > DecisionUnresolved || !o.Claimed && o.RequestDigest != (Digest{}) {
		return fmt.Errorf("%w: transaction outcome", Invalid)
	}
	if o.State != Committed && o.Result != nil || o.State != Aborted && o.AbortDecision != nil || o.State != Pending && o.Stage != 0 || o.State != Indeterminate && o.Uncertainty != NoUncertainty || o.State != Rejected && o.State != Aborted && o.Code != NoFailure {
		return fmt.Errorf("%w: incompatible transaction outcome payload", Invalid)
	}
	switch o.State {
	case Rejected:
		if o.Code == NoFailure || o.Code == AbortRequested || o.Code == IDConflict && !o.Claimed || o.Code == UnknownID && o.Claimed {
			return fmt.Errorf("%w: rejection reason/claim mismatch", Invalid)
		}
	case Pending:
		if !o.Claimed || o.Stage == 0 {
			return fmt.Errorf("%w: pending without claim/stage", Invalid)
		}
	case Committed:
		if !o.Claimed || o.Result == nil {
			return fmt.Errorf("%w: commit without claim/receipt", Invalid)
		}
		return o.Result.validate(o.ID)
	case Aborted:
		if !o.Claimed || o.Code == NoFailure || o.AbortDecision == nil || !o.AbortDecision.Valid() || o.AbortDecision.ID.Request != o.ID {
			return fmt.Errorf("%w: abort without protected decision", Invalid)
		}
	case Indeterminate:
		if o.Uncertainty == NoUncertainty {
			return fmt.Errorf("%w: indeterminate without reason", Invalid)
		}
	}
	return nil
}

func (o TransactionOutcome) clone() TransactionOutcome {
	if o.Result != nil {
		r := *o.Result
		r.Positions = slices.Clone(r.Positions)
		if r.Replaced != nil {
			replaced := *r.Replaced
			r.Replaced = &replaced
		}
		o.Result = &r
	}
	if o.AbortDecision != nil {
		decision := *o.AbortDecision
		o.AbortDecision = &decision
	}
	return o
}
