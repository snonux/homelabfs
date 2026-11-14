package viewcontrol

import (
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Source-cut accounting (task i64, after zq3-r7 R7-2).
//
// The ledger retains one Sources entry per distinct cut (journal position plus
// state hash) that a capture remembered: Open's baseline, every admission
// (reads included), every acquisition, every recall event batch and Recover.
// Captures of an unchanged cut deduplicate; every committed mutation (CREATE,
// WRITE, MKDIR, RENAME, SETATTR, a last-lookup FORGET, DRAIN_WRITE, ...) makes
// the next capture a new entry.
//
// Cuts leave the ledger two ways. compact() drops every unpinned cut after a
// VIEW_RETIRE, which the guest sends only for terminal view tickets. And,
// outside a recall, each admission (Admit, AdmitNative, DIRTY_RESERVE) drops
// every cut that no lease or recall event targets, except the one it admits
// (source_pruning.go, which gives the safety argument). A lease-free workload
// therefore holds one cut whatever its length; each lease in the ledger pins
// its cut until a VIEW_RETIRE compacts the lease.
//
// sourceCapacity admits a non-read op only while
//
//	len(Sources) + extra + sourceReserve + unfrozen reservations <= Sources
//
// where extra is the op's own capture: 1 for a new cut, 0 when the capture
// deduplicates (admitCut captures before it charges). sourceReserve keeps two
// cuts free beyond it (since k04): one for the op's post-effect cut, which the
// next capture remembers (a recall's events, an acquisition, the next
// admission or Recover), and one spare. Whether the spare is needed is a
// separate design question, not something this file decides.
//
// DIRTY_RESERVE uses the same check with extra 1 for its future drain cut;
// DRAIN_WRITE uses extra 0. A non-read admission never sees an open
// reservation (conflicts() starts a recall instead), so the unfrozen term is
// 0 there.
const (
	// sourceReserve is the constant term of sourceCapacity.
	sourceReserve = 2
	// baselineSources is the cut Open remembers before READY.
	baselineSources = 1
	// admittedSources is the extra a non-read admission charges for a new cut.
	admittedSources = 1
	// MaxSources is validBounds' upper bound on Bounds.Sources.
	MaxSources = 128
)

// MinSources is the enrollment floor on Bounds.Sources. It guarantees one
// reference sequence from READY, before any VIEW_RETIRE: every domain admits
// one non-read op (CREATE/OPEN), takes one write lease with one dirty
// reservation, a recall drains every reservation, and the op that started the
// recall is still admitted after REOPEN. Counted as if no cut were ever
// pruned and every capture were new, the cuts charged when that last op is
// admitted are:
//
//	baselineSources                1  Open's baseline (READY)
//	+ domains                      D  one committed non-read op per domain
//	+ domains                      D  one DRAIN_WRITE per domain; the last
//	                                  cut is remembered by INSTALL_DRAIN
//	+ admittedSources              1  the parked op's own capture
//	+ sourceReserve                2  sourceCapacity's reserve
//	= 2*domains + 4                   (8 for the two-domain zq3 lab)
//
// That is the sequence's peak: the reservations are all frozen and drained by
// then, and a recall with nothing to drain adds no cut.
//
// Lock-only reductions (PROCESS_EXIT, DESCRIPTION_CLOSE; task 784) pass the
// recall barrier, so one can be admitted inside an all-domain recall's DRAIN
// phase, but only from a domain without its own open reservation
// (ownReservationOpen). It captures the current cut between two drain writes,
// is charged admittedSources for it and remembers it while pruning is off.
// That cut is the one the next DRAIN_WRITE or the INSTALL_DRAIN event
// remembers anyway, so the set of retained cuts is unchanged; only the next
// drain's capacity check counts it one capture earlier. Counted unpruned as
// above, with k drains (k <= D in the reference sequence: one reservation
// per domain), the j-th drain's check sees len(Sources) <= 1 + D + (j-1),
// extra 0, the reserve 2 and k-j+1 undrained reservations: D+k+3 <= 2D+3.
// The early capture adds one: at most 2D+4 = MinSources, never above it. The
// exit's own check may refuse it with a plain NoSpace, which QUERY resumes
// after REOPEN.
// TestZQ3SourceFloorCoversMidDrainExit runs that case at MinSources(2).
// Pruning and the deduplicated charge only ever lower these counts (a pruned
// set is a subset of the unpruned one, and extra drops from 1 to 0), so the
// floor holds with them too; TestZQ3SourceFloorCoversReferenceSequence runs
// the sequence at exactly MinSources(2).
//
// The floor is not a workload budget. Lease-free I/O needs only the current
// cut plus the reserve, however many mutations it makes, because admissions
// prune. What exhausts Sources is cuts pinned by leases (active, or inactive
// but not yet compacted by a VIEW_RETIRE) and drains inside one recall: a
// recall drains against the cuts it started with plus one per drain.
func MinSources(domains int) int {
	return baselineSources + 2*domains + admittedSources + sourceReserve
}

// CheckSourceFloor refuses Bounds.Sources outside [MinSources(domains),
// MaxSources]. Below the floor the sequence above is not guaranteed; without
// pruning its last non-read op got NoSpace at run time. zq3-r7 R7-2 had 2
// for 2 domains, so even mount(8)'s first PROCESS_EXIT was refused. Because MinSources(63) >
// MaxSources, at most 62 domains can enroll. It wraps fsapi.Invalid.
func CheckSourceFloor(b Bounds, domains int) error {
	if domains < 1 {
		return fmt.Errorf("%w: enrollment needs at least one domain", fsapi.Invalid)
	}
	floor := MinSources(domains)
	if b.Sources < floor || b.Sources > MaxSources {
		return fmt.Errorf("%w: Bounds.Sources %d outside [%d, %d]: %d domains need 1 baseline + 2 cuts per domain + 1 admitted + 2 reserved source cuts",
			fsapi.Invalid, b.Sources, floor, MaxSources, domains)
	}
	return nil
}
