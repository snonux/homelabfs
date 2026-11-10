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
// Captures of an unchanged cut deduplicate, but every committed mutation
// (CREATE, WRITE, OPEN with a pin, DRAIN_WRITE, ...) makes the next capture a
// new entry. Only VIEW_RETIRE compacts entries that no lease or recall event
// still pins (retirement.go compact), and the guest sends VIEW_RETIRE only
// for terminal view tickets. Until then the count only grows.
//
// sourceCapacity admits a non-read op only while
//
//	len(Sources) + extra + sourceReserve + unfrozen reservations <= Sources
//
// where extra is the op's own capture (1, charged even when it deduplicates)
// and sourceReserve keeps two cuts free beyond it (since k04): one for the
// op's post-effect cut, which the next capture remembers (a recall's events,
// an acquisition, the next admission or Recover), and one spare. The floor
// below charges both; whether the spare is needed is a separate design
// question, not something this file decides.
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
	// admittedSources is the extra a non-read admission charges for itself.
	admittedSources = 1
	// MaxSources is validBounds' upper bound on Bounds.Sources.
	MaxSources = 128
)

// MinSources is the smallest Bounds.Sources that carries an enrollment of
// domains from READY through one all-domain recall without any VIEW_RETIRE:
// every domain admits one non-read op (CREATE/OPEN), takes one write lease
// with one dirty reservation, a recall drains every reservation, and the op
// that started the recall is still admitted after REOPEN. Counting the cuts
// retained when that last op is admitted:
//
//	baselineSources                1  Open's baseline (READY)
//	+ domains                      D  one committed non-read op per domain
//	+ domains                      D  one DRAIN_WRITE per domain; the last
//	                                  cut is remembered by INSTALL_DRAIN
//	+ admittedSources              1  the parked op's own capture
//	+ sourceReserve                2  sourceCapacity's reserve
//	= 2*domains + 4                   (8 for the two-domain zq3 lab)
//
// The reservations are all frozen and drained by then, and a PROCESS_EXIT or
// recall with nothing to drain adds no cut, so this is the peak of the
// sequence. TestZQ3SourceFloorIsExact runs it at exactly MinSources(2) and
// shows the count is reached. Every Sources unit above the floor is one more
// committed mutation the enrollment can admit before a VIEW_RETIRE.
func MinSources(domains int) int {
	return baselineSources + 2*domains + admittedSources + sourceReserve
}

// CheckSourceFloor refuses Bounds.Sources outside [MinSources(domains),
// MaxSources]. Below the floor, the sequence above cannot finish: its last
// non-read op gets NoSpace at run time. zq3-r7 R7-2 had 2 for 2 domains, so
// even mount(8)'s first PROCESS_EXIT was refused. Because MinSources(63) >
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
