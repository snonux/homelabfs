package fsapi

import (
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

// UUID is a stable 128-bit identity, independent of native inode numbers.
// Allocation randomness, uniqueness and nonreuse are the allocating service's job.
type UUID [16]byte

// ObjectID identifies one never-reused allocation in its immutable home volume.
type ObjectID struct {
	Home       UUID
	Allocation UUID
}

// Kind is the immutable logical object type.
type Kind uint8

const (
	// Regular is a regular file with a private home-owned payload.
	Regular Kind = iota + 1
	// Directory owns logical entries, even when moved into another volume's view.
	Directory
	// Symlink stores byte-preserving text in logical metadata.
	Symlink
)

// Timestamp is a replicated wall-clock value. Replicas use the owner's exact
// value, never their local apply time. Nanoseconds must be below one billion.
type Timestamp struct {
	Seconds     int64
	Nanoseconds uint32
}

// Attr is committed logical metadata, shared by every alias of Object.
// Mode contains only Unix permission and special bits (07777), not file type.
// Nlink may be zero while open, mapped or recovery-pinned references remain.
type Attr struct {
	Object              ObjectID
	Kind                Kind
	UID, GID            uint32
	Mode                uint16
	Size                int64
	Nlink, Revision     uint64
	Atime, Mtime, Ctime Timestamp
}

// Cred is a snapshot of execution credentials, not a transaction's stable
// authenticated principal. PID is advisory caller metadata, not authorization.
type Cred struct {
	UID, GID uint32
	Groups   []uint32
	PID      uint32
}

// AccessMode expresses access independently of OS flag bit patterns.
type AccessMode uint8

const (
	// ReadOnly opens for reading.
	ReadOnly AccessMode = iota + 1
	// WriteOnly opens for writing.
	WriteOnly
	// ReadWrite opens for both reading and writing.
	ReadWrite
)

// SyncMode is the required per-write barrier on an open handle.
type SyncMode uint8

const (
	// OrdinaryWrite uses the full normal mutation acknowledgement boundary.
	OrdinaryWrite SyncMode = iota
	// DataWrite additionally orders prior data and retrieval-critical metadata.
	DataWrite
	// IntegrityWrite additionally orders prior data and full file metadata.
	IntegrityWrite
)

// OpenFlags contains semantic open options, never native syscall flag values.
type OpenFlags struct {
	Access                              AccessMode
	Create, Exclusive, Truncate, Append bool
	Sync                                SyncMode
}

// DurabilityRequest selects the dependencies covered by a barrier. All three
// use the initial one-durable-local-copy plus partner-memory boundary. Neither
// close nor release adds a barrier; directory durability is explicit.
type DurabilityRequest uint8

const (
	// DataDurability covers prior data and retrieval-critical metadata.
	DataDurability DurabilityRequest = iota + 1
	// FileDurability covers prior data and full file metadata.
	FileDurability
	// DirectoryDurability covers prior namespace changes and their dependencies.
	DirectoryDurability
)

// OpenHandle is an opaque service-issued handle, distinct from lookup references.
// Zero is invalid; the service scopes it to a mount's current serving generation.
type OpenHandle uint64

// LockOwner scopes an opaque adapter owner token by session and serving generation.
// PID alone is never a lock identity, and native per-mount locking is insufficient
// for aliases of one ObjectID in different namespace volumes.
type LockOwner struct {
	Session, Generation UUID
	Token               uint64
}

// Clock supplies deterministic owner timestamps. It does not establish serving
// authority or permit a wall clock to be used as a fencing mechanism.
type Clock interface {
	Now() Timestamp
}

// WallClock obtains timestamps from the local wall clock.
type WallClock struct{}

var _ Clock = WallClock{}

// ParseUUID accepts only nonzero, lowercase canonical hyphenated UUID text.
func ParseUUID(s string) (UUID, error) {
	var id UUID
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || strings.ToLower(s) != s {
		return id, fmt.Errorf("%w: UUID spelling", Invalid)
	}
	raw := strings.ReplaceAll(s, "-", "")
	if len(raw) != 32 {
		return id, fmt.Errorf("%w: UUID spelling", Invalid)
	}
	if _, err := hex.Decode(id[:], []byte(raw)); err != nil {
		return UUID{}, fmt.Errorf("%w: UUID spelling", Invalid)
	}
	if !id.Valid() {
		return UUID{}, fmt.Errorf("%w: zero UUID", Invalid)
	}
	return id, nil
}

// Valid reports whether id is nonzero. Specific UUID allocation policies belong
// to the service, not to parsing existing volume/history identities.
func (id UUID) Valid() bool { return id != UUID{} }

// String returns lowercase canonical hyphenated spelling.
func (id UUID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// Valid reports whether both identity components are nonzero.
func (id ObjectID) Valid() bool { return id.Home.Valid() && id.Allocation.Valid() }

// Valid reports whether the timestamp can be replicated without normalization.
func (t Timestamp) Valid() bool { return t.Nanoseconds < 1_000_000_000 }

// Validate rejects metadata outside the shared backing-store contract.
func (a Attr) Validate() error {
	if !a.Object.Valid() || a.Kind < Regular || a.Kind > Symlink || a.Mode & ^uint16(0o7777) != 0 || a.Size < 0 || a.Revision == 0 || !a.Atime.Valid() || !a.Mtime.Valid() || !a.Ctime.Valid() {
		return fmt.Errorf("%w: object attributes", Invalid)
	}
	return nil
}

// Clone returns credentials owning an independent supplementary-group slice.
func (c Cred) Clone() Cred {
	c.Groups = slices.Clone(c.Groups)
	return c
}

// Validate rejects unsupported or inconsistent semantic open options.
func (f OpenFlags) Validate() error {
	if f.Access < ReadOnly || f.Access > ReadWrite || f.Sync > IntegrityWrite || f.Exclusive && !f.Create || f.Access == ReadOnly && (f.Truncate || f.Append) {
		return fmt.Errorf("%w: open options", Invalid)
	}
	return nil
}

// Valid reports whether the barrier kind is supported by the shared contract.
func (d DurabilityRequest) Valid() bool { return d >= DataDurability && d <= DirectoryDurability }

// Valid reports whether the owner has a session and serving-generation scope.
func (o LockOwner) Valid() bool { return o.Session.Valid() && o.Generation.Valid() }

// Now samples the wall clock once for owner-chosen replicated timestamps.
func (WallClock) Now() Timestamp {
	now := time.Now()
	return Timestamp{Seconds: now.Unix(), Nanoseconds: uint32(now.Nanosecond())}
}

// ValidateRange accepts offsets and lengths whose exclusive end fits int64.
// A zero-length range at the largest supported offset is valid.
func ValidateRange(offset, length int64) error {
	if offset < 0 || length < 0 || length > math.MaxInt64-offset {
		return fmt.Errorf("%w: file range", Invalid)
	}
	return nil
}
