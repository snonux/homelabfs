package fsapi

import (
	"context"
	"slices"
)

// Operation identifies one filesystem command; cross-mount rename/link fails.
type Operation uint8

const (
	// Lookup resolves a component and acquires a lookup reference.
	Lookup Operation = iota + 1
	// Getattr reads committed logical attributes.
	Getattr
	// Create creates or opens a regular file and acquires lookup/open references.
	Create
	// Open acquires a handle to an existing exact object.
	Open
	// Read reads bytes through an open handle.
	Read
	// Write changes an owned byte range through an open handle.
	Write
	// Truncate sets an exact regular-file size.
	Truncate
	// RenameOp moves one name within the same mount.
	RenameOp
	// LinkOp adds a regular-file alias within the same mount.
	LinkOp
	// Unlink removes one nondirectory name.
	Unlink
	// Mkdir creates a directory and acquires a lookup reference.
	Mkdir
	// Rmdir removes an empty directory name.
	Rmdir
	// SymlinkOp creates byte-preserving symlink text and a lookup reference.
	SymlinkOp
	// Readlink reads logical symlink text without following it.
	Readlink
	// Readdir reads a revision-bound page, without acquiring lookup references.
	Readdir
	// Setattr changes selected logical metadata fields.
	Setattr
	// Statfs reads admitted logical capacity counters.
	Statfs
	// Sync covers the specified durability scope and all earlier volume commands.
	Sync
	// Release drains one open reference without adding a data sync promise.
	Release
	// Forget drops the specified number of lookup references.
	Forget
	// TryLock acquires or converts an advisory lock without waiting.
	TryLock
	// GetLock reports one conflicting lock without acquiring it.
	GetLock
	// Unlock removes the caller's specified range (or whole-file flock).
	Unlock
	// WaitLock waits asynchronously; CancelLock must cancel an abandoned attempt.
	WaitLock
	// CancelLock cancels the matching WaitID, or observes its prior completion.
	CancelLock
	// ProcessExit retires Owner and releases its fcntl locks and waiters.
	ProcessExit
	// SessionExit retires all process identities in Owner.Session.
	SessionExit
	// DescriptorClose drops process fcntl locks for any closed fd, keeping the
	// shared open description, flock and lookup references alive.
	DescriptorClose
	// Reap requests a policy-authorized orphan reclamation sweep.
	Reap
)

// Attributes selects explicit metadata fields; nil preserves a field. Now uses
// one owner-chosen timestamp for the complete command. Size uses Truncate.
type Attributes struct {
	Mode               *uint16
	UID, GID           *uint32
	Atime, Mtime       *Timestamp
	AtimeNow, MtimeNow bool
}

// TruncateTimes selects native timestamps carried with a size change. Nil
// preserves atime and uses the service clock for mtime/ctime. These values are
// part of the same exact TRUNCATE metadata afterimage, not a second mutation.
// Timestamp selections share truncate's descriptor/current-write authorization.
type TruncateTimes struct {
	Atime, Mtime, Ctime *Timestamp
	AtimeNow, MtimeNow  bool
}

// Command is the ordinary adapter dispatch contract. Mount identifies the
// serving mount, Object/Parent/Destination are stable IDs, Name and NewName are
// single raw-byte components. NoReplace selects rename without replacement. DestinationMount is required for Rename/Link,
// even when equal to Mount. Offset is ignored for an append handle. Required
// captures a prior committed source cut; zero requests the admission cut.
// Generation binds handles and resumed directory cursors to the issuing
// serving generation. It is mandatory for Handle or DirectoryRevision.
// Readdir uses After and DirectoryRevision (zero starts a traversal). Lookup
// owns one reference, released by Forget.Count. No API field borrows memory.
type Command struct {
	Operation                   Operation
	Mount, DestinationMount     UUID
	Generation                  UUID
	Object, Parent, Destination ObjectID
	Name, NewName               string
	Handle                      OpenHandle
	Flags                       OpenFlags
	NoReplace                   bool
	// DescriptionOnly on Release drains only the final open description. The
	// adapter already delivered per-fd DescriptorClose events; repeating their
	// process cleanup could erase locks reacquired on another description.
	DescriptionOnly   bool
	Mode, Umask       uint16
	Data              []byte
	Offset, Size      int64
	Count             uint64
	Limit             int
	After             string
	DirectoryRevision uint64
	Attributes        Attributes
	TruncateTimes     TruncateTimes
	Durability        DurabilityRequest
	Lock              Lock
	Required          Position
}

// DirEntry owns a byte-preserving logical name and exact name revision.
type DirEntry struct {
	Name     string
	Object   ObjectID
	Revision uint64
	Kind     Kind
}

// Capacity describes the service's admitted bounded local object/data budget.
// It is not a claim about raw ZFS free space or replicated physical usage.
type Capacity struct {
	Objects, ObjectsFree uint64
	Bytes, BytesFree     uint64
}

// Filesystem admits requests into a bounded queue. A nil dispatch error means
// the service owns immutable arguments and must eventually complete the request,
// even if ctx or a Completion.Wait is canceled. Dispatch errors mean no admission.
// A result error after admission does not imply mutation rollback.
type Filesystem interface {
	Dispatch(context.Context, *Request, Command) error
}

// Clone returns an independent snapshot of every mutable argument.
func (c Command) Clone() Command {
	c.Data = slices.Clone(c.Data)
	a := c.Attributes
	if a.Mode != nil {
		v := *a.Mode
		a.Mode = &v
	}
	if a.UID != nil {
		v := *a.UID
		a.UID = &v
	}
	if a.GID != nil {
		v := *a.GID
		a.GID = &v
	}
	if a.Atime != nil {
		v := *a.Atime
		a.Atime = &v
	}
	if a.Mtime != nil {
		v := *a.Mtime
		a.Mtime = &v
	}
	c.Attributes = a
	t := c.TruncateTimes
	if t.Atime != nil {
		v := *t.Atime
		t.Atime = &v
	}
	if t.Mtime != nil {
		v := *t.Mtime
		t.Mtime = &v
	}
	if t.Ctime != nil {
		v := *t.Ctime
		t.Ctime = &v
	}
	c.TruncateTimes = t
	return c
}
