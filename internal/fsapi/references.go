package fsapi

import (
	"fmt"
	"math"
	"sync"
)

// References counts one object's local lookup and open lifetimes in one
// adapter session/generation. Its zero value is empty and ready for use; it
// must not be copied. It does not own namespace links, mappings, replay pins,
// snapshots or foreign-home references. Only the service can authorize reclaim.
type References struct {
	mu            sync.Mutex
	lookups, open uint64
}

// ReferenceCounts is an instantaneous local reference snapshot.
type ReferenceCounts struct {
	Lookups uint64
	Open    uint64
}

// Lookup adds kernel lookup references. A forget must later release each one.
func (r *References) Lookup(count uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if count == 0 || count > math.MaxUint64-r.lookups {
		return fmt.Errorf("%w: lookup reference count", Invalid)
	}
	r.lookups += count
	return nil
}

// Forget drops only lookup references; open handles remain bound to the object.
func (r *References) Forget(count uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if count == 0 || count > r.lookups {
		return fmt.Errorf("%w: forget reference count", Invalid)
	}
	r.lookups -= count
	return nil
}

// Open acquires a local open reference independently of kernel lookup counts.
func (r *References) Open() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.open == math.MaxUint64 {
		return fmt.Errorf("%w: open reference exhaustion", Invalid)
	}
	r.open++
	return nil
}

// Release drops one open reference without changing lookup counts or adding a
// durability barrier. Handle validity and exactly-one release are service duties.
func (r *References) Release() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.open == 0 {
		return fmt.Errorf("%w: no open reference", Invalid)
	}
	r.open--
	return nil
}

// Counts returns the independent local lifetime counts.
func (r *References) Counts() ReferenceCounts {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ReferenceCounts{Lookups: r.lookups, Open: r.open}
}
