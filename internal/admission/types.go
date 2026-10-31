// Package admission bounds owned resources before allocation and schedules
// independent closures. It supplies no authority, durable coverage or grant.
package admission

import "errors"

// Class identifies an isolated resource pool. Unused reserve is never loaned to
// another class; this preserves capacity during live-traffic saturation.
type Class uint8

const (
	Live       Class = iota // Live carries ordinary origin work.
	CatchUp                 // CatchUp carries bounded historical chunks.
	Recovery                // Recovery preserves reconstruction capacity.
	Control                 // Control preserves authority and acknowledgement capacity.
	Drain                   // Drain preserves pre-reserved dirty-page packages.
	Memory                  // Memory carries baseline, candidate and pinned view arenas.
	ClassCount              // ClassCount is the number of independently bounded pools.
)

var (
	ErrCapacity = errors.New("admission capacity exhausted")
	ErrInvalid  = errors.New("invalid admission request or limits")
	ErrState    = errors.New("invalid admission lease state")
	ErrAccepted = errors.New("accepted obligation requires protocol authorization")
)

// Resources counts the full lifetime footprint, including queued and retained
// work. Bytes includes OwnerRAM and Dependencies (disjoint RAM subcategories).
// Journal is additional reserved durable-source space, including choice/result
// and retry fragments. Workers counts executing work, not waiting goroutines.
// Every holding costs at least one Operation, even for a memory arena or socket.
// Callers must include transient copies before allocating them.
type Resources struct {
	Bytes, Operations, OwnerRAM, Journal, Dependencies, Workers, Connections uint64
}

// Limits bounds one class globally, by origin peer and by logical volume.
// The sum over classes is the corresponding total hard bound. Zero disables a
// resource; an entirely zero class is disabled. Each scope reserves every class
// independently, so saturated ordinary traffic cannot exhaust drain/control.
type Limits struct {
	Global, PerPeer, PerVolume Resources
}

// Config is immutable after New. Policy is mandatory for any accepted release,
// grant completion or transfer; nil allows only local unaccepted allocations.
type Config struct {
	Pools  [ClassCount]Limits
	Policy Policy
}

// Request names accounting scopes, never authorization roles. Any authenticated
// admitted origin, including one outside replica placement, can be a Peer.
// Scope strings are nonempty and at most 256 bytes. Closure is optional for
// Reserve; Enqueue requires it for mutating work. Blocked queues a conflicting
// final operation without holding the slot/worker needed by prior dirty drains.
type Request struct {
	Peer, Volume, Closure string
	Class                 Class
	Cost                  Resources
	Mutating, Blocked     bool
}

// Action distinguishes publication completion from actual resource release.
type Action uint8

const (
	FinishGrant       Action = iota + 1 // FinishGrant opens the next serial slot.
	ReleaseResources                    // ReleaseResources retires an accepted holding.
	TransferResources                   // TransferResources moves an obligation to Target.
)

// Holding binds policy checks to a specific live lease and immutable obligation.
// Lease is an opaque comparable identity; it is not a wire certificate.
type Holding struct {
	Lease      *Lease
	Request    Request
	Obligation string
}

// Authorization is checked by the external protocol's coverage/publication
// authority. Evidence is bounded opaque protocol evidence (at most 4096 bytes),
// never interpreted here as an ACK, timeout, durable checkpoint or GC permission.
type Authorization struct {
	Action         Action
	Source, Target Holding
	Evidence       string
}

// Policy must validate the exact grant/source/result and lifecycle action.
// It runs without the budget lock. A successful callback is only an assertion
// by this injected authority; this package cannot establish durable coverage.
type Policy interface {
	Authorize(Authorization) error
}

// Usage is a consistent bounded snapshot. Retained accepted resources remain in
// Used after the worker and serial slot are released by Finish.
type Usage struct {
	Used                                Resources
	Pools                               [ClassCount]Resources
	Queued, Running, Retained, Accepted uint64
}
