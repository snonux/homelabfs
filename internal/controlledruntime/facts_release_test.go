//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
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
	l.Leases = map[string]factsLease{}
	l.Leases[older.ID] = factsLease{Lease: older}
	l.Leases[newer.ID] = factsLease{Lease: newer, Active: true}
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

// addTicket stores ticket number of the family (scope, anchor "f", nonce,
// credentials c) with operation op and current reply status, naming lease
// (when not empty) as a GRANTED acquisition does.
func addTicket(t *testing.T, l *factsLedger, scope guestbridge.Context, nonce string, c guestbridge.Credentials, number uint64, op, status, lease string) {
	t.Helper()
	if l.Tickets == nil {
		l.Tickets = map[string]struct {
			Scope                     guestbridge.Context
			Request                   guestbridge.Ticket
			Operation                 string
			Payload, Current, Initial json.RawMessage
		}{}
	}
	reply := map[string]any{"status": status, "lease": nil}
	if lease != "" {
		reply["lease"] = map[string]any{"id": lease}
	}
	current, err := json.Marshal(reply)
	require(t, err)
	entry := l.Tickets[scope.Session+nonce+string(rune('0'+number))]
	entry.Scope, entry.Operation, entry.Current = scope, op, current
	entry.Request = guestbridge.Ticket{Anchor: "f", Nonce: nonce, Number: number, Credentials: c}
	l.Tickets[scope.Session+nonce+string(rune('0'+number))] = entry
}

// TestFactsRetireIsPerTicket: retirement refuses with
// viewcontrol.ErrRetirementHeld (km4: the controller answers it BLOCKED
// instead of a never-answered WouldBlock) while the retired range holds a
// GRANTED acquisition of a still active lease, or any open reservation of
// the domain remains.
//
// hp4 (zq3-r25 R25-1): until hp4 any active lease of the family's own
// credentials refused it (gn4; before gn4, any active lease of the domain).
// In r25 the T1 process's guest cleanup released its observation leases
// asynchronously, often only after its next observation had acquired, so a
// lease of T1's credentials was active whenever its VIEW_RETIREs arrived,
// and 13 answered RELEASED
// tickets of its release family (about 25.5 MiB each in the guest) filled
// m0's 512 MiB control lane. An answered release, and an acquisition whose
// lease every holder released, name nothing a holder may still use.
func TestFactsRetireIsPerTicket(t *testing.T) {
	scope := guestbridge.Context{Session: "a"}
	child := guestbridge.Credentials{FSUID: 41001, FSGID: 42001, Groups: []uint32{42001}}
	retire := func(nonce string, through uint64) json.RawMessage {
		raw, err := json.Marshal(map[string]any{"anchor": "f", "nonce": nonce, "credentials": child, "through": through})
		require(t, err)
		return raw
	}
	l, older, newer := releaseLedger(t) // older released, newer active
	newer.CredentialHash = viewcontrol.CredentialDigest(child)
	setLease(&l, newer, true)
	// The child's acquisitions 1 (older, released) and 2 (newer, held), and
	// its answered release of older.
	addTicket(t, &l, scope, "acquire", child, 1, "VIEW_ACQUIRE", "GRANTED", older.ID)
	addTicket(t, &l, scope, "acquire", child, 2, "VIEW_ACQUIRE", "GRANTED", newer.ID)
	addTicket(t, &l, scope, "release", child, 1, "VIEW_RELEASE", "RELEASED", "")
	check := func(name string, raw json.RawMessage, want bool) {
		t.Helper()
		err := l.checkRetire(scope, raw)
		if want && !errors.Is(err, viewcontrol.ErrRetirementHeld) || !want && err != nil {
			t.Errorf("%s: %v, want held %v", name, err, want)
		}
	}
	check("answered release beside the family's own active lease (r25)", retire("release", 1), false)
	check("released acquisition beside the family's own active lease", retire("acquire", 1), false)
	check("range with the acquisition of the active lease", retire("acquire", 2), true)
	if err := l.checkRetire(guestbridge.Context{Session: "b"}, retire("acquire", 2)); err != nil {
		t.Errorf("retire in another domain beside this domain's active lease: %v", err)
	}
	addReservation(&l, "older", "RESERVED")
	check("answered release beside an open reservation", retire("release", 1), true)
	l.Reservations = nil
	l.Leases = nil
	check("acquisitions with no owner left", retire("acquire", 2), false)
	if err := l.checkRetire(scope, json.RawMessage(`{`)); err == nil {
		t.Fatal("retire with an undecodable request passed")
	}
}

// observationLedger is releaseLedger with no recall since the older lease's
// grant (domain fence 2 = its fence) and the older lease a Targeted DOMAIN
// READ observation lease, as viewcontrol's recall-free release (task je4)
// presents it: already inactive.
func observationLedger(t *testing.T) (factsLedger, guestbridge.ViewLease) {
	t.Helper()
	l, older, newer := releaseLedger(t)
	delete(l.Leases, newer.ID)
	l.Fences["a"] = older.Fence
	l.Leases[older.ID] = factsLease{Lease: older, Targeted: true}
	return l, older
}

// TestFactsObservationReleaseWithoutRecall: only a Targeted DOMAIN READ lease
// is certified without a recall since its grant, and still only while its
// lineage is inactive and holds no open reservation (task je4).
func TestFactsObservationReleaseWithoutRecall(t *testing.T) {
	for _, c := range observationCases() {
		l, older := observationLedger(t)
		released := c.change(&l, older)
		raw, err := json.Marshal(map[string]any{"lease": released})
		require(t, err)
		if err := l.checkRelease(guestbridge.Context{Session: "a"}, raw); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

type observationCase struct {
	name string
	// change edits the observationLedger and returns the lease to release.
	change func(*factsLedger, guestbridge.ViewLease) guestbridge.ViewLease
	want   error
}

// observationCases: the first is certified; every other lease kind and every
// per-lease obligation still refuses without a recall since the grant.
func observationCases() []observationCase {
	return []observationCase{
		{"observation lease", func(_ *factsLedger, l guestbridge.ViewLease) guestbridge.ViewLease { return l }, nil},
		{"untargeted discovery lease", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			l.Leases[lease.ID] = factsLease{Lease: lease}
			return lease
		}, fsapi.WouldBlock},
		{"PAGE lease", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			return retarget(l, lease, func(v *guestbridge.ViewLease) { v.Access.Kind = "PAGE" })
		}, fsapi.WouldBlock},
		{"OBJECT lease", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			return retarget(l, lease, func(v *guestbridge.ViewLease) { v.Access.Kind = "OBJECT" })
		}, fsapi.WouldBlock},
		{"EXCLUSIVE_DIRTY lease", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			return retarget(l, lease, func(v *guestbridge.ViewLease) { v.Mode = "EXCLUSIVE_DIRTY" })
		}, fsapi.WouldBlock},
		{"lease itself active", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			l.Leases[lease.ID] = factsLease{Lease: lease, Active: true, Targeted: true}
			return lease
		}, fsapi.WouldBlock},
		{"renewed sibling active", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			sibling := lease
			sibling.ID, sibling.Revision = "sibling", 2
			setLease(l, sibling, true)
			return lease
		}, fsapi.WouldBlock},
		{"own reservation open", func(l *factsLedger, lease guestbridge.ViewLease) guestbridge.ViewLease {
			addReservation(l, lease.ID, "RESERVED")
			return lease
		}, fsapi.WouldBlock},
	}
}

// retarget replaces the Targeted ledger lease with a changed copy and returns
// the copy to release.
func retarget(l *factsLedger, lease guestbridge.ViewLease, change func(*guestbridge.ViewLease)) guestbridge.ViewLease {
	delete(l.Leases, lease.ID)
	change(&lease)
	l.Leases[lease.ID] = factsLease{Lease: lease, Targeted: true}
	return lease
}

// TestFactsAdvanceReleaseWithoutRecall: an Advance lease (task xf4,
// viewcontrol advance.go) is a Targeted DOMAIN READ lease whose release the
// controller answers without a recall even though the cut moved past its
// target. The Facts decode the controller's actual ledger row, Advance field
// included, and certify it like any observation lease: checkRelease never
// compares the lease target with the current cut, because a DOMAIN lease
// authorizes no PTE whatever cut it names. Advance alone grants nothing: a
// row that is not Targeted still needs a recall since its grant.
func TestFactsAdvanceReleaseWithoutRecall(t *testing.T) {
	_, older := observationLedger(t)
	older.Target = guestbridge.ViewTarget{Kind: "CONTROLLED_LOCAL", Cut: &guestbridge.LocalViewCut{JournalEpoch: 1, JournalPosition: 7}}
	for _, c := range []struct {
		name string
		row  map[string]any
		want error
	}{
		{"Targeted Advance lease", map[string]any{"Lease": older, "Targeted": true, "Advance": true}, nil},
		{"Advance without Targeted", map[string]any{"Lease": older, "Advance": true}, fsapi.WouldBlock},
	} {
		raw, err := json.Marshal(map[string]any{"Fences": map[string]uint64{"a": older.Fence}, "Leases": map[string]any{older.ID: c.row}})
		require(t, err)
		var l factsLedger
		require(t, json.Unmarshal(raw, &l))
		released, err := json.Marshal(map[string]any{"lease": older})
		require(t, err)
		if err := l.checkRelease(guestbridge.Context{Session: "a"}, released); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}
