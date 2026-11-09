//go:build linux || freebsd

package runtimeprep

import (
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledruntime"
)

// TestGuestFloorDerivation pins the per-mount units to the kernel derivation
// (hfs_controlled_internal.h) and the two-mount lab minimum.
func TestGuestFloorDerivation(t *testing.T) {
	if guestFloorControl != 95 || guestFloorOrdinary != 75 ||
		guestFloorControlReceived != 15 || guestFloorOrdinaryReceived != 12 {
		t.Fatalf("per-mount floor drifted: control %d ordinary %d received %d/%d",
			guestFloorControl, guestFloorOrdinary, guestFloorControlReceived, guestFloorOrdinaryReceived)
	}
	if reservations, received := guestSlotFloor(sessionCount); reservations != 380 || received != 60 {
		t.Fatalf("two-mount slot floor %d/%d, want 380/60", reservations, received)
	}
	if control, ordinary := guestByteFloor(sessionCount); control != 280<<20 || ordinary != 242<<20 {
		t.Fatalf("two-mount byte floor %d/%d, want 280/242 MiB", control, ordinary)
	}
}

// TestGuestFloorBoundary accepts exactly the floor and refuses one below each
// bound, including zq3-r3's provisioned reservations 32.
func TestGuestFloorBoundary(t *testing.T) {
	floor := exampleSpec(t).Budget.Guest
	floor.Reservations, floor.Received = guestSlotFloor(sessionCount)
	floor.ControlBytes, floor.OrdinaryBytes = guestByteFloor(sessionCount)
	if err := checkGuestFloor(floor, sessionCount); err != nil {
		t.Fatalf("exact floor refused: %v", err)
	}
	below := floor
	below.Reservations = 32
	if err := checkGuestFloor(below, sessionCount); err == nil || !strings.Contains(err.Error(), "slot floor 380/60") {
		t.Fatalf("zq3-r3 reservations 32: %v", err)
	}
	for name, mutate := range map[string]func(*controlledruntime.GuestArtifactBudget){
		"reservations": func(b *controlledruntime.GuestArtifactBudget) { b.Reservations-- },
		"received":     func(b *controlledruntime.GuestArtifactBudget) { b.Received-- },
		"control":      func(b *controlledruntime.GuestArtifactBudget) { b.ControlBytes-- },
		"ordinary":     func(b *controlledruntime.GuestArtifactBudget) { b.OrdinaryBytes-- },
	} {
		below = floor
		mutate(&below)
		if err := checkGuestFloor(below, sessionCount); err == nil {
			t.Fatalf("%s one below the floor accepted", name)
		}
	}
	if err := checkGuestFloor(exampleSpec(t).Budget.Guest, sessionCount); err != nil {
		t.Fatalf("example spec below the floor: %v", err)
	}
}
