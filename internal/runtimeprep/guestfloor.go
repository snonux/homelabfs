//go:build linux || freebsd

package runtimeprep

import (
	"fmt"

	"github.com/snonux/homelabfs/internal/controlledruntime"
)

// Guest slot floor (w54, zq3-r3 R3-1). zq3-r3 provisioned Guest.reservations
// 32: the kernel provider refuses a new credit once its partition (control or
// ordinary) holds reservations/2, while bind_super's first progress lane alone
// needs 17, so every mount failed with ENOSPC before ROOT.
//
// These constants mirror the kernel's own derivation in
// fs/9p/hfs_controlled_internal.h (patch 0005, HFS_FLOOR_*), which the guest
// enforces again at provisioning (hfs_controlled_slot_floor). Per mount, while
// it is bound, bootstrapped and serving its first operation:
//
//	package = completion parent + 16 child slots                         = 17
//	owner   = request/ticket reservation + its package                   = 18
//	control  = 3 progress lanes x 17 (control, retirement, terminal)
//	         + 4 (process identity, view, mmap control actor, cleanup)
//	         + 19 (discovery release ticket owner + control family)
//	         + 21 (first operation: process identity, observation cleanup,
//	              release ticket owner + family)                        = 95
//	ordinary = 18 (ROOT request) + 19 (discovery acquire ticket + family)
//	         + 38 (first operation: cookie, acquire ticket + family,
//	              request)                                              = 75
//	received: control 6 + 2 + 1 + 2 x 3 = 15, ordinary 4 x 3 = 12
//
// Both partition counters are global across mounts, so the floor is
// reservations >= 2*max(sum control, sum ordinary) and received likewise. The
// lab enrolls only features 32767 (guestbridge.TerminalProtocol), the larger
// of the two admitted tuples, so every mount is counted as terminal. Two
// mounts need reservations >= 380 and received >= 60. The floor excludes more
// concurrent operations, open descriptions and mapped pages; specs should
// keep a margin above it.
const (
	guestFloorPackage          = 1 + 16
	guestFloorOwner            = 1 + guestFloorPackage
	guestFloorControl          = 3*guestFloorPackage + 4 + (guestFloorOwner + 1) + (3 + guestFloorOwner)
	guestFloorOrdinary         = guestFloorOwner + (guestFloorOwner + 1) + (2 + 2*guestFloorOwner)
	guestFloorControlReceived  = 6 + 2 + 1 + 2*3
	guestFloorOrdinaryReceived = 4 * 3
)

// Guest byte floor for the same units. Unlike the slot counts, the bytes
// depend on kernel struct sizes, so these are measured, not mirrored: the
// controlled_received harness (actual accounting code, userspace struct
// sizes) charged per features-32767 mount 80,962,137 control bytes for
// bind_super's three lanes, 36,559,105 ordinary bytes per request owner and
// 26,802,997 bytes per lease ticket owner in each partition, and a
// 9,239,510-byte permanent control base for two mounts. Summed over the units
// above that is 137,971,627 control and 126,724,304 ordinary bytes per mount.
// Each is rounded up to whole MiB, and the base to 16 MiB to cover a larger
// provision document; kernel struct sizes differ by kilobytes only. The kernel
// enforces bytes at reserve time, not at provisioning, so a too-small byte
// budget would otherwise surface only as a mount failure (two mounts' lanes
// alone exceed the 128 MiB control budget zq3-r3 used).
const (
	guestFloorControlBytes  = 132 << 20
	guestFloorOrdinaryBytes = 121 << 20
	guestFloorControlBase   = 16 << 20
)

// guestSlotFloor returns the minimum reservations and received counts for
// mounts terminal-tuple guest mounts.
func guestSlotFloor(mounts int) (reservations, received uint64) {
	n := uint64(mounts)
	reservations = 2 * n * max(guestFloorControl, guestFloorOrdinary)
	received = 2 * n * max(guestFloorControlReceived, guestFloorOrdinaryReceived)
	return reservations, received
}

// guestByteFloor returns the minimum control and ordinary byte budgets for
// mounts terminal-tuple guest mounts.
func guestByteFloor(mounts int) (control, ordinary uint64) {
	n := uint64(mounts)
	return guestFloorControlBase + n*guestFloorControlBytes, n * guestFloorOrdinaryBytes
}

// checkGuestFloor refuses a guest budget that cannot mount every domain and
// serve one operation on each, before any host step. The runtime's own
// guestBudgetValid accepts any count above zero; the guest refuses a too-small
// slot budget only once the signed provision reaches it.
func checkGuestFloor(b controlledruntime.GuestArtifactBudget, mounts int) error {
	reservations, received := guestSlotFloor(mounts)
	if b.Reservations < reservations || b.Received < received {
		return fmt.Errorf("Budget.Guest reservations %d/received %d below the guest slot floor %d/%d for %d mounts",
			b.Reservations, b.Received, reservations, received, mounts)
	}
	control, ordinary := guestByteFloor(mounts)
	if b.ControlBytes < control || b.OrdinaryBytes < ordinary {
		return fmt.Errorf("Budget.Guest control %d/ordinary %d bytes below the measured guest byte floor %d/%d for %d mounts",
			b.ControlBytes, b.OrdinaryBytes, control, ordinary, mounts)
	}
	return nil
}
