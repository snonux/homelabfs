package fsapi

import "math"

// LockDomain selects independent BSD advisory lock namespaces.
type LockDomain uint8

const (
	// POSIXLock is a process/session-owned byte-range fcntl lock.
	POSIXLock LockDomain = iota + 1
	// FlockLock is a whole-file lock owned by an open description (Handle).
	FlockLock
)

// LockKind selects a shared or exclusive advisory lock.
type LockKind uint8

const (
	// SharedLock permits other shared holders.
	SharedLock LockKind = iota + 1
	// ExclusiveLock excludes other holders in the same lock domain.
	ExclusiveLock
)

// Lock specifies a normalized SEEK_SET byte range. Length zero extends through
// all future bytes. Adapters normalize SEEK_CUR/END and negative lengths first.
// Owner is an authenticated adapter process identity, never just a PID. WaitID
// uniquely identifies a blocking attempt for this serving generation; CancelLock
// resolves cancellation in worker order. A canceled Completion.Wait alone does
// not cancel acquisition. Flock requires Start and Length zero.
type Lock struct {
	Domain        LockDomain
	Kind          LockKind
	Start, Length int64
	Owner         LockOwner
	WaitID        UUID
}

// LockConflict describes one conflicting holder; nil means no conflict.
// Handle identifies the open description for flock, Owner the process for fcntl.
type LockConflict struct {
	Lock   Lock
	Handle OpenHandle
}

// Validate checks the normalized bounded lock shape and scoped owner identity.
func (l Lock) Validate() error {
	if !l.Owner.Valid() || l.Domain != POSIXLock && l.Domain != FlockLock || l.Kind != SharedLock && l.Kind != ExclusiveLock || l.Start < 0 || l.Length < 0 || l.Length > math.MaxInt64-l.Start || l.Domain == FlockLock && (l.Start != 0 || l.Length != 0) {
		return Invalid
	}
	return nil
}
