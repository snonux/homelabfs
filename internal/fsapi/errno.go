package fsapi

import "fmt"

// Errno is a semantic result code. Its values are not native errno numbers.
// Adapters must map every value explicitly; unknown values must fail closed.
type Errno uint8

const (
	// OK indicates successful completion.
	OK Errno = iota
	// Invalid indicates invalid input or unsupported ranges.
	Invalid
	// NotFound indicates a missing object or name.
	NotFound
	// Exists indicates a destination that must be absent.
	Exists
	// Permission indicates a Unix access denial.
	Permission
	// ReadOnlyFS indicates a read-only namespace view.
	ReadOnlyFS
	// NotDirectory indicates traversal through a nondirectory.
	NotDirectory
	// IsDirectory indicates an unsupported operation on a directory.
	IsDirectory
	// NotEmpty indicates a directory that must be empty.
	NotEmpty
	// CrossDevice rejects ordinary POSIX operations across mount boundaries.
	CrossDevice
	// Unsupported indicates an unsupported operation or capability.
	Unsupported
	// NoSpace indicates exhausted storage or admission capacity.
	NoSpace
	// Quota indicates a capacity quota violation.
	Quota
	// IO indicates an I/O failure; after admission it does not prove rollback.
	IO
	// Unavailable indicates unavailable serving authority or dependencies.
	Unavailable
	// Stale indicates an invalid handle, generation or expected revision.
	Stale
	// Interrupted indicates an interrupted request wait, not mutation rollback.
	Interrupted
	// Busy indicates a resource whose current references prevent the operation.
	Busy
	// WouldBlock indicates a nonblocking lock conflict.
	WouldBlock
	// TooManyLinks indicates link-count exhaustion.
	TooManyLinks
	// NameTooLong indicates a name outside admitted limits.
	NameTooLong
	errnoEnd
)

// Errnos returns the complete set, including OK, for adapter mapping checks.
func Errnos() []Errno {
	codes := make([]Errno, int(errnoEnd))
	for i := range codes {
		codes[i] = Errno(i)
	}
	return codes
}

// ValidateErrnoCoverage verifies that a mapping covers every semantic code
// exactly once, without importing or prescribing either platform's numbers.
func ValidateErrnoCoverage(codes []Errno) error {
	seen := make(map[Errno]bool, len(codes))
	for _, code := range codes {
		if !code.Valid() || seen[code] {
			return fmt.Errorf("%w: invalid or repeated errno %d", Invalid, code)
		}
		seen[code] = true
	}
	if len(seen) != int(errnoEnd) {
		return fmt.Errorf("%w: incomplete errno mapping", Invalid)
	}
	return nil
}

// Valid reports whether code belongs to this version of the API.
func (code Errno) Valid() bool { return code < errnoEnd }

// Error implements error for semantic failures. OK should not be returned as
// a non-nil error; result structs use it as their explicit success code.
func (code Errno) Error() string {
	names := [...]string{"OK", "INVALID", "NOT_FOUND", "EXISTS", "PERMISSION", "READ_ONLY", "NOT_DIRECTORY", "IS_DIRECTORY", "NOT_EMPTY", "CROSS_DEVICE", "UNSUPPORTED", "NO_SPACE", "QUOTA", "IO", "UNAVAILABLE", "STALE", "INTERRUPTED", "BUSY", "WOULD_BLOCK", "TOO_MANY_LINKS", "NAME_TOO_LONG"}
	if !code.Valid() {
		return fmt.Sprintf("UNKNOWN_ERRNO(%d)", code)
	}
	return names[code]
}
