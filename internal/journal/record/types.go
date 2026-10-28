// Package record implements the version 1.0 journal byte contract. It performs
// representation validation only; it does not establish commitment or authority.
package record

import "github.com/snonux/homelabfs/internal/fsapi"

// UUID is a nonzero semantic identity.
type UUID = fsapi.UUID

// ObjectID identifies an allocation in its immutable home.
type ObjectID = fsapi.ObjectID

// Time preserves an owner-selected timestamp.
type Time = fsapi.Timestamp

// Principal identifies an authenticated subject.
type Principal = fsapi.Principal

// Hash is SHA-256 of exact stored bytes (zero is not a sentinel).
type Hash [32]byte

// Position names a prefix; sequence zero is allowed only for prefixes.
type Position = fsapi.Position

// RequestID identifies an anchored request.
type RequestID = fsapi.TransactionID

// Vector contains one prefix per volume in ascending UUID order.
type Vector []Position

// FrameRef identifies an exact complete frame in an owner or reporter stream.
type FrameRef struct {
	StreamKind          uint8 `wire:"enum=1:2"`
	Reporter            *UUID
	ReporterIncarnation *UUID
	Position            Position
	SHA256              Hash
}

// Credentials captures execution credentials separately from request identity.
type Credentials struct {
	UID, GID uint32
	Groups   []uint32 `wire:"max=1024,sort"`
}

// Endpoint preserves positional byte-exact path components.
type Endpoint struct {
	Volume, View UUID
	Components   []string `wire:"min=1,max=256"`
}

// OriginalRequest is the public canonical request wire schema.
type OriginalRequest struct {
	Version                                                      uint16 `wire:"enum=1:1"`
	ID                                                           RequestID
	Operation                                                    uint8 `wire:"enum=1:2"`
	Source, Destination                                          Endpoint
	Replacement                                                  uint8 `wire:"enum=1:2"`
	ExpectedSource                                               *ObjectID
	SourceParent, SourceName, DestinationParent, DestinationName *uint64
}

// InternalRequest retains all original requested values and selected afterimages.
type InternalRequest struct {
	Version               uint16 `wire:"enum=1:1"`
	ID                    RequestID
	NamespaceVolume, View UUID
	Intent                Intent
}

// RequestDocument is a closed union; exactly one field is present for its class.
type RequestDocument struct {
	Class    uint8
	Original *OriginalRequest
	Internal *InternalRequest
}

// Authority describes an admitted group; decoding is not authorization.
type Authority struct {
	Volume, Generation, Configuration    UUID
	Epoch                                uint64
	Owner, Partner                       UUID
	Members                              []UUID `wire:"min=3,max=4096,sort"`
	CoordinatorTerm                      uint64 `wire:"min=1"`
	OwnerIncarnation, PartnerIncarnation UUID
}

// AffinityRef hashes one exact affinity payload.
type AffinityRef struct {
	Closure         UUID
	ManifestVersion uint64 `wire:"min=1"`
	ManifestSHA256  Hash
}

// Visibility names one shared publication generation.
type Visibility struct {
	Affinity   AffinityRef
	Generation uint64 `wire:"min=1"`
}

// Authorization records admission permission inputs.
type Authorization struct {
	Object         ObjectID
	ObjectRevision uint64 `wire:"min=1"`
	View           UUID
	PolicyRevision uint64 `wire:"min=1"`
	Credentials    Credentials
}

// Artifact identifies an immutable external artifact, never a pathname.
type Artifact struct {
	Kind   uint16 `wire:"enum=1:7"`
	ID     UUID
	Length uint64 `wire:"max=1073741824"`
	SHA256 Hash
}

// Evidence contains complete independently validated nonrecursive frames.
type Evidence struct {
	Frames [][]byte `wire:"max=4096"`
}

// DecisionID uses journal field order, which differs from fsapi.DecisionID.
type DecisionID struct {
	Coordinator, Generation, Configuration UUID
	Epoch                                  uint64
	Request                                RequestID
	Term                                   uint64 `wire:"min=1"`
}

// DecisionRef identifies the exact selected decision.
type DecisionRef struct {
	ID       DecisionID
	Decision FrameRef
}

// PrepareRef identifies participant-local prepared redo.
type PrepareRef struct {
	Participant UUID
	Prepare     FrameRef
	RedoSHA256  Hash
}

// Binding preserves immutable resolved intent across participants.
type Binding struct {
	Request       RequestID
	RequestDigest Hash
	Affinity      AffinityRef
	Coordinator   UUID
	Intent        Intent
	Participants  []UUID      `wire:"min=1,max=64,sort"`
	Authorities   []Authority `wire:"max=64,sort"`
	Dependencies  Vector
}

// TerminalResult is a final receipt; Decision uses a provisional variant of it.
type TerminalResult struct {
	Status              uint8  `wire:"enum=1:3"`
	Code                uint16 `wire:"max=15"`
	Source, Replacement *ObjectID
	Decision            *DecisionRef
	Committed           Vector
	Visibility          *Visibility
	Conflicts           []UUID `wire:"max=64,sort"`
}

// Metadata is a complete logical object afterimage.
type Metadata struct {
	Revision            uint64 `wire:"min=1"`
	Type                uint8  `wire:"enum=1:3"`
	UID, GID            uint32
	Mode                uint16 `wire:"max=4095"`
	Size                uint64 `wire:"max=9223372036854775807"`
	Nlink               uint64
	Atime, Mtime, Ctime Time
	SymlinkText         []byte `wire:"max=4096"`
}

// EntryState retains tombstone revisions.
type EntryState struct {
	Revision uint64
	Target   *ObjectID
}

// EntryEdit changes one exact name state.
type EntryEdit struct {
	Parent        ObjectID
	Name          string `wire:"name"`
	Before, After EntryState
}

// ObjectEdit advances one exact object revision.
type ObjectEdit struct {
	Object           ObjectID
	ExpectedRevision uint64
	After            Metadata
}

// Reference identifies one namespace reference.
type Reference struct {
	Parent        ObjectID
	Name          string `wire:"name"`
	EntryRevision uint64 `wire:"min=1"`
}

// LedgerEdit replaces exact references and counts.
type LedgerEdit struct {
	Object                              ObjectID
	ExpectedRevision, ResultingRevision uint64
	ExpectedNlink, ResultingNlink       uint64      `wire:"max=4096"`
	Remove, Add                         []Reference `wire:"max=256,sort"`
}

// ParentState names an exact parent relation.
type ParentState struct {
	Revision            uint64 `wire:"min=1"`
	Parent              ObjectID
	ParentEntryRevision uint64
}

// ParentEdit changes a directory parent relation.
type ParentEdit struct {
	Directory ObjectID
	Before    *ParentState
	After     ParentState
}

// Redo contains exact participant-local effects.
type Redo struct {
	Objects []ObjectEdit `wire:"max=256,sort"`
	Entries []EntryEdit  `wire:"max=256,sort"`
	Ledgers []LedgerEdit `wire:"max=256,sort"`
	Parents []ParentEdit `wire:"max=256,sort"`
}

// Intent is a closed argument union followed by immutable global metadata.
type Intent struct {
	Kind             uint16
	Arguments        any
	SelectedMetadata []ObjectEdit `wire:"max=256,sort"`
}

// Mutation combines stable intent with participant-local redo.
type Mutation struct {
	Intent         Intent
	Redo           Redo
	Authorizations []Authorization `wire:"max=4096,sort"`
	Dependencies   Vector
}

// Create arguments include requested mode and umask even if afterimages coincide.
type Create struct {
	Parent        ObjectID
	Name          string `wire:"name"`
	Object        ObjectID
	Exclusive     bool
	RequestedMode uint16 `wire:"max=4095"`
	Umask         uint16 `wire:"max=511"`
}

// Mkdir selects a new directory allocation.
type Mkdir struct {
	Parent        ObjectID
	Name          string `wire:"name"`
	Object        ObjectID
	RequestedMode uint16 `wire:"max=4095"`
	Umask         uint16 `wire:"max=511"`
}

// Symlink preserves exact link text.
type Symlink struct {
	Parent ObjectID
	Name   string `wire:"name"`
	Object ObjectID
	Text   []byte `wire:"min=1,max=4096,text"`
}

// Link adds a reference to a regular object.
type Link struct {
	Source, TargetParent ObjectID
	TargetName           string `wire:"name"`
}

// Unlink removes an exact nondirectory occupant.
type Unlink struct {
	Parent  ObjectID
	Name    string `wire:"name"`
	Removed ObjectID
}

// Rmdir removes an exact empty directory occupant.
type Rmdir struct {
	Parent  ObjectID
	Name    string `wire:"name"`
	Removed ObjectID
}

// Rename records selected identities and explicit no-op status.
type Rename struct {
	SourceParent              ObjectID
	SourceName                string `wire:"name"`
	Source, DestinationParent ObjectID
	DestinationName           string `wire:"name"`
	Policy                    uint8  `wire:"enum=1:2"`
	Replaced                  *ObjectID
	Noop                      bool
}

// Write is a nonempty accepted range; append has already selected its offset.
type Write struct {
	Object ObjectID
	Offset uint64 `wire:"max=9223372036854775807"`
	Data   []byte `wire:"min=1,max=1048576"`
}

// Truncate sets the exact regular-file size.
type Truncate struct {
	Object ObjectID
	Size   uint64 `wire:"max=9223372036854775807"`
}

// TimeSelection distinguishes explicit times from the selected value of now.
type TimeSelection struct {
	Source uint8 `wire:"enum=1:2"`
	Value  Time
}

// Setattr includes exactly the fields selected by Mask, in ascending bit order.
type Setattr struct {
	Object       ObjectID
	Mask         uint16
	Mode         *uint16
	UID, GID     *uint32
	Atime, Mtime *TimeSelection
}

// Alias describes cached observations requiring a verified barrier.
type Alias struct {
	View           UUID
	Object, Parent *ObjectID
	Name           string `wire:"max=255"`
	Invalidate     uint8  `wire:"min=1,max=15"`
}

// Tail records distinct contiguous progress prefixes.
type Tail struct {
	Received, Durable, Accepted, Applied Position
	Incarnation                          UUID
	Continuous                           bool
	Missing                              []FrameRef `wire:"sort"`
}

// Coverage retains complete durable replica obligations.
type Coverage struct {
	Replica, Incarnation UUID
	Durable              Vector
	Baseline             *Artifact
	SuffixStart          Vector
	Dependencies         []FrameRef `wire:"sort"`
}

// Header contains owner-selected stream identity and diagnostic time.
type Header struct {
	Volume, Generation, Configuration UUID
	Epoch, Sequence                   uint64
	Operation, Incarnation            UUID
	Observed                          Time
}

// Record is one typed frame. Payload must be a value of a supported payload type.
type Record struct {
	Header  Header
	Payload any
}
