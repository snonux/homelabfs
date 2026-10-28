package vfs

import (
	"context"
	"io/fs"
)

// MaxReadDir bounds each enumeration page, including requests from recovery code.
const MaxReadDir = 1024

// Identity is an ephemeral local storage identity, never a replicated ObjectID.
// It is useful for detecting aliased roots and validating open descriptors. It
// may be reused after deletion. OS identities are meaningful only on the same
// local host/filesystem; simulator identities only within one instance.
type Identity struct {
	Device uint64
	File   uint64
}

// Info describes physical storage, not logical Unix metadata in the catalog.
// UID, GID, Mode and Identity provide evidence for private-root admission; the
// caller must enforce its owner, permission, mount and overlap policy before use.
type Info struct {
	Mode fs.FileMode
	Size int64
	// Links is the native hard-link count for regular files. Private storage
	// rejects counts other than one; it is never the logical catalog Nlink.
	Links    uint64
	UID      uint32
	GID      uint32
	Identity Identity
}

// Entry is one immediate child with no-follow physical metadata. Names are raw
// basenames, with no ordering guarantee. An entry is discovery evidence only.
type Entry struct {
	Name string
	Info Info
}

// Statter reports the physical object bound to an open descriptor. OS and
// simulator File handles implement it in addition to the offset File interface.
type Statter interface {
	Stat(context.Context) (Info, error)
}

// Dir enumerates a bound directory, retaining identity through rename/removal.
// ReadDir requires 1 <= n <= MaxReadDir and allocates at most n entries. It
// returns io.EOF when exhausted (possibly with a final partial page). Errors may
// accompany a partial page: callers must fail closed rather than treat it as a
// complete scan. Context is checked between entries; syscalls cannot be canceled.
// Concurrent namespace changes may affect results; recovery/GC must serialize
// its complete scan against mutations. Close adds no durability guarantee.
type Dir interface {
	Statter
	ReadDir(context.Context, int) ([]Entry, error)
	Close() error
}

// Discovery supplies the narrow recovery and GC operations beside FS. Stat
// accepts "." for root evidence; Stat/OpenDir reject symlinks and special files.
// Rmdir removes only empty directories, excludes the root, and needs SyncDir
// on the modified parent to persist removal. All paths are root-relative.
type Discovery interface {
	Stat(context.Context, string) (Info, error)
	OpenDir(context.Context, string) (Dir, error)
	Rmdir(context.Context, string) error
}
