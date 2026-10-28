package storage

import (
	"context"
	"io"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

// Filesystem combines persistence and no-follow discovery on the same root.
type Filesystem interface {
	vfs.FS
	vfs.Discovery
}

// Limits bounds the admitted in-memory catalog and immutable file encoding/decoding.
// Zero fields select conservative defaults, not unlimited resources.
type Limits struct {
	Objects, Entries, Operations, Pins int
	FileBytes                          int64
}

// Closure gates every named home until the external coordinator completes all
// installations and the shared visibility barrier. Enter must authorize this
// exact protected operation, not merely lock a subset of participants. The
// returned function releases this call's gate reference; the coordinator retains
// its closure-wide gate on failure. No closure means foreign effects are refused.
type Closure interface {
	Enter(context.Context, Operation, []fsapi.UUID) (func(), error)
}

// Config specifies expected identity, bounded resources and external coordination.
// OwnerUID is the required administrative owner of both private roots and files.
type Config struct {
	Volume   fsapi.UUID
	Root     fsapi.ObjectID
	OwnerUID uint32
	Limits   Limits
	Random   io.Reader
	Closure  Closure
}

// Descriptor permanently binds an allocation to its type and creation operation.
type Descriptor struct {
	Object   fsapi.ObjectID
	Kind     fsapi.Kind
	Creation fsapi.UUID
}

// Reference is one exact logical alias, including the name's incarnation.
type Reference struct {
	Parent   fsapi.ObjectID
	Name     string
	Revision uint64
}

// Parent is a directory's single logical parent relation (root points to itself).
type Parent struct {
	Revision      uint64
	Object        fsapi.ObjectID
	EntryRevision uint64
}

// Object contains authoritative metadata, home references and directory parent.
// References are sorted by parent then raw name. LedgerRevision is independent
// of metadata revision. A symlink is catalog text, never a native host symlink.
type Object struct {
	Descriptor     Descriptor
	Attr           fsapi.Attr
	Text           []byte
	LedgerRevision uint64
	References     []Reference
	Parent         *Parent
}

// Entry includes tombstones: Target is nil after removal but Revision is retained.
type Entry struct {
	Parent   fsapi.ObjectID
	Name     string
	Revision uint64
	Target   *fsapi.ObjectID
}

// ObjectChange replaces an exact object revision. Before zero means creation.
type ObjectChange struct {
	Before uint64
	After  Object
}

// EntryChange compares and replaces a complete name state; zero Before denotes
// an untouched name, not a removed name with a retained revision.
type EntryChange struct {
	Before Entry
	After  Entry
}

// Pin is a conservative protected home lifetime obligation. Released pins remain
// tombstones so an old pin cannot be resurrected with different identity.
type Pin struct {
	ID                      fsapi.UUID
	Object                  fsapi.ObjectID
	Generation, Incarnation fsapi.UUID
	Active                  bool
}

// PinChange compares a pin's complete previous state. Before nil creates it.
type PinChange struct {
	Before *Pin
	After  Pin
}

// Kind identifies the supported primitive, independently of native syscalls.
type Kind uint8

const (
	Create Kind = iota + 1
	Write
	Truncate
	Link
	Unlink
	Rename
	Mkdir
	Rmdir
	Symlink
	Setattr
	HandlePin
	HandleRelease
	Reclaim
)

// Operation is semantic local redo selected by a trusted apply layer. Changes
// are complete revisioned afterimages, not arithmetic deltas. Sequence is the
// next contiguous backing-apply slot in Position's immutable history; journal
// adapters map their complete ordered committed suffix to these slots. Payload
// effect is allowed only for Write/Truncate and names an exact home allocation.
// Reclaim additionally requires the caller's protected retention authorization;
// this backend never decides that a checkpoint, peer or snapshot can be dropped.
type Operation struct {
	ID           fsapi.UUID
	Position     fsapi.Position
	Kind         Kind
	Objects      []ObjectChange
	Entries      []EntryChange
	Pins         []PinChange
	Object       fsapi.ObjectID
	Offset       int64
	Data         []byte
	Size         int64
	Participants []fsapi.UUID
	// ExpectedRevision and ExpectedLedgerRevision guard protected reclamation.
	ExpectedRevision, ExpectedLedgerRevision uint64
}

// Receipt binds one applied slot to exact semantic bytes.
type Receipt struct {
	Sequence uint64
	ID       fsapi.UUID
	SHA256   [32]byte
}

// Payload identifies an exact clean baseline payload image.
type Payload struct {
	Object fsapi.ObjectID
	Size   int64
	SHA256 [32]byte
}

// Image is a bounded, deterministic clean local baseline. It retains allocations,
// entry tombstones, reference ledgers, pins and duplicate-operation receipts.
// It is not a distributed checkpoint or a substitute for journal acceptance.
type Image struct {
	Version  uint16
	Volume   fsapi.UUID
	Root     fsapi.ObjectID
	Position fsapi.Position
	Objects  []Object
	Entries  []Entry
	Pins     []Pin
	Retired  []Descriptor
	Receipts []Receipt
	Payloads []Payload
}

// Store serializes catalog publication and payload I/O. After an I/O error its
// read gate closes until clean recovery; no partial bytes can become visible.
// It must not be copied. Filesystems remain owned by the caller.
type Store struct {
	mu            sync.Mutex
	data, journal Filesystem
	config        Config
	objects       map[fsapi.ObjectID]Object
	entries       map[entryKey]Entry
	pins          map[fsapi.UUID]Pin
	retired       map[fsapi.ObjectID]Descriptor
	receipts      map[uint64]Receipt
	position      fsapi.Position
	ready         bool
	checkpoint    fsapi.UUID
}
type entryKey struct {
	parent fsapi.ObjectID
	name   string
}
