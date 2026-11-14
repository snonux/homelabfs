//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// releaseLedger is a ledger of domain "a" at fence 3 holding the older lease
// (fence 2, inactive: its recall completed) and a newer active lease of the
// same domain (fence 3), each in its own lineage.
func releaseLedger(t *testing.T) (factsLedger, guestbridge.ViewLease, guestbridge.ViewLease) {
	t.Helper()
	scope := guestbridge.Context{Session: "a"}
	older := guestbridge.ViewLease{ID: "older", Scope: scope, Access: guestbridge.ViewAccess{Kind: "DOMAIN"}, Mode: "READ", Fence: 2, Revision: 1}
	newer := older
	newer.ID, newer.Fence = "newer", 3
	var l factsLedger
	l.Fences = map[string]uint64{"a": 3}
	l.Leases = map[string]struct {
		Lease  guestbridge.ViewLease
		Active bool
	}{}
	l.Leases[older.ID] = struct {
		Lease  guestbridge.ViewLease
		Active bool
	}{older, false}
	l.Leases[newer.ID] = struct {
		Lease  guestbridge.ViewLease
		Active bool
	}{newer, true}
	return l, older, newer
}

func addReservation(l *factsLedger, leaseID, status string) {
	if l.Reservations == nil {
		l.Reservations = map[string]struct {
			Reservation               guestbridge.DirtyReservation
			Status, Recall, TicketKey string
			Call                      *guestbridge.Call
			Entry                     json.RawMessage
		}{}
	}
	r := l.Reservations["r"]
	r.Reservation = guestbridge.DirtyReservation{ID: "r", Scope: guestbridge.Context{Session: "a"}, LeaseID: leaseID}
	r.Status = status
	l.Reservations["r"] = r
}

func setLease(l *factsLedger, lease guestbridge.ViewLease, active bool) {
	entry := l.Leases[lease.ID]
	entry.Lease, entry.Active = lease, active
	l.Leases[lease.ID] = entry
}

// TestFactsReleaseIsPerLease: an older lease's release is certified while a
// newer lease of the same domain is active (task g84), but every per-lease
// physical obligation the old domain-wide check covered still refuses it.
func TestFactsReleaseIsPerLease(t *testing.T) {
	for _, c := range releaseCases() {
		l, older, newer := releaseLedger(t)
		released := c.change(&l, older, newer)
		raw, err := json.Marshal(map[string]any{"lease": released})
		require(t, err)
		if err := l.checkRelease(guestbridge.Context{Session: "a"}, raw); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
	l, older, _ := releaseLedger(t)
	raw, err := json.Marshal(map[string]any{"lease": older})
	require(t, err)
	if err := l.checkRelease(guestbridge.Context{Session: "b"}, raw); !errors.Is(err, fsapi.Stale) {
		t.Errorf("release from another domain: %v", err)
	}
}

type releaseCase struct {
	name string
	// change edits the releaseLedger and returns the lease to release.
	change func(*factsLedger, guestbridge.ViewLease, guestbridge.ViewLease) guestbridge.ViewLease
	want   error
}

// releaseCases: the first three are certified beside the active newer lease;
// the rest are the per-lease obligations that must still refuse.
func releaseCases() []releaseCase {
	return []releaseCase{
		{"newer lease active", func(_ *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease { return older }, nil},
		{"newer dirty lease holds an open reservation", func(l *factsLedger, older, newer guestbridge.ViewLease) guestbridge.ViewLease {
			delete(l.Leases, newer.ID)
			newer.Mode = "EXCLUSIVE_DIRTY"
			setLease(l, newer, true)
			addReservation(l, newer.ID, "RESERVED")
			return older
		}, nil},
		{"own reservation closed", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			addReservation(l, older.ID, "CLOSED")
			return older
		}, nil},
		{"lease itself active", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			setLease(l, older, true)
			return older
		}, fsapi.WouldBlock},
		{"renewed sibling active", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			sibling := older
			sibling.ID, sibling.Revision = "sibling", 2
			setLease(l, sibling, true)
			return older
		}, fsapi.WouldBlock},
		{"inactive renewed sibling owns a frozen reservation", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			sibling := older
			sibling.ID, sibling.Revision = "sibling", 2
			setLease(l, sibling, false)
			addReservation(l, sibling.ID, "FROZEN")
			return older
		}, fsapi.WouldBlock},
		{"own reservation open", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			addReservation(l, older.ID, "FROZEN")
			return older
		}, fsapi.WouldBlock},
		{"open reservation of unknown lease", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			addReservation(l, "gone", "RESERVED")
			return older
		}, fsapi.WouldBlock},
		{"no recall since the grant", func(l *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			l.Fences["a"] = older.Fence
			return older
		}, fsapi.WouldBlock},
		{"newer lease release before its own recall", func(l *factsLedger, _, newer guestbridge.ViewLease) guestbridge.ViewLease {
			setLease(l, newer, false) // even inactive: fence 3 is not superseded
			return newer
		}, fsapi.WouldBlock},
		{"unknown lease", func(_ *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			older.ID = "unknown"
			return older
		}, fsapi.Stale},
		{"payload differs from ledger", func(_ *factsLedger, older, _ guestbridge.ViewLease) guestbridge.ViewLease {
			older.Fence = 1
			return older
		}, fsapi.Stale},
	}
}

// TestFactsRetireStaysDomainWide: retirement still refuses while any lease or
// open reservation of the domain remains.
func TestFactsRetireStaysDomainWide(t *testing.T) {
	l, _, _ := releaseLedger(t)
	if err := l.checkRetire(guestbridge.Context{Session: "a"}); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("retire beside an active lease: %v", err)
	}
	l.Leases = nil
	addReservation(&l, "older", "RESERVED")
	if err := l.checkRetire(guestbridge.Context{Session: "a"}); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("retire beside an open reservation: %v", err)
	}
}
