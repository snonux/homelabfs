package viewcontrol

import (
	"bytes"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Shared lease holders (task q84, lease_holders.go). Two concurrent
// observations of one mount with the same lineage each acquire with their own
// ticket and receive the same host lease; each later releases its own guest
// lease object on its own control ticket.

// sharedPair grants m0 the same DOMAIN READ lease twice, as two concurrent
// same-credential GETATTR observations of one mount acquire it.
func (r *rig) sharedPair() *guestbridge.ViewLease {
	r.t.Helper()
	first, second := r.domainRead(0), r.domainRead(0)
	if first.ID != second.ID {
		r.t.Fatalf("same-lineage acquisitions got distinct leases %s %s", first.ID, second.ID)
	}
	return first
}

// reservedReleases returns the release credits a lease still reserves: the
// unbound main credit plus one per further holder.
func (r *rig) reservedReleases(lease *guestbridge.ViewLease) int {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	l := r.controller.state.Leases[lease.ID]
	reserved := l.Sharers
	if l.ReleaseCredit && l.ReleaseTicket == "" {
		reserved++
	}
	return reserved
}

// TestSharedLeaseFirstReleaseKeepsView is the offline repro of q84: the first
// observation's end releases its holder while the second is in flight. That
// release must neither recall nor move the view: it answers RELEASED at once,
// the lease stays active, the fence and cut stay, and the in-flight
// observation's next GETATTR is admitted (on main the release started a
// scoped recall of m0, which held that GETATTR, as in zq3-r9 R9-1).
func TestSharedLeaseFirstReleaseKeepsView(t *testing.T) {
	r := newZQ3Rig(t, 0)
	shared := r.sharedPair()
	if got := r.reservedReleases(shared); got != 2 {
		t.Fatalf("two holders reserve %d release credits", got)
	}
	before, err := r.controller.capture(t.Context())
	must(t, err)
	payload := map[string]any{"request": r.controlTicket(0), "lease": shared}
	if reply := r.view(0, "VIEW_RELEASE", payload); reply.Status != "RELEASED" {
		t.Fatalf("first holder release: %+v", reply)
	}
	if _, _, exists := r.recallSnapshot(); exists {
		t.Fatal("first holder release started a recall")
	}
	if !r.leaseActive(shared) || r.controller.fence(r.scopes[0].Session) != 1 {
		t.Fatal("first holder release revoked the shared lease or moved the fence")
	}
	if _, status := r.pendingCall(0, "GETATTR", map[string]any{"object": r.roots[0]}); status != "TERMINAL" {
		t.Fatalf("in-flight observation's GETATTR after the first release: %s", status)
	}
	after, err := r.controller.capture(t.Context())
	must(t, err)
	if sourceKey(before) != sourceKey(after) {
		t.Fatal("first holder release moved the cut")
	}
	if got := r.reservedReleases(shared); got != 1 {
		t.Fatalf("one remaining holder reserves %d release credits", got)
	}
	// The replay of the holder release is its protected RELEASED.
	if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("holder release query: %+v", reply)
	}
	r.lastHolderReleases(shared)
}

// lastHolderReleases releases the remaining holder of lease on m0: it still
// recalls m0 (scoped), and RELEASED needs that completed recall and the
// physical fact, as for an unshared lease.
func (r *rig) lastHolderReleases(lease *guestbridge.ViewLease) {
	r.t.Helper()
	payload := r.startRelease(0, lease)
	if scope, _, exists := r.recallSnapshot(); !exists || scope != r.scopes[0].Session {
		r.t.Fatalf("last holder release recall scope %q exists %v", scope, exists)
	}
	if got := r.reservedReleases(lease); got != 0 {
		r.t.Fatalf("released lease still reserves %d credits", got)
	}
	r.completeCleanRecall()
	// Without the physical fact the release cannot be certified.
	if raw, err := r.execute(0, "VIEW_QUERY", map[string]any{"request": payload["request"]}); err == nil {
		r.t.Fatalf("last holder released without the physical fact: %s", raw)
	}
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		r.t.Fatalf("last holder release: %+v", reply)
	}
	if r.leaseActive(lease) || r.controller.fence(r.scopes[0].Session) != 2 {
		r.t.Fatal("last holder release left the lease active or the fence unchanged")
	}
}

// TestSharedLeaseHolderOfPendingRelease: a release that is PENDING behind
// another domain's scoped recall has bound the main credit when a second
// observation shares the lease. Readmitted after that recall, it is no longer
// the last holder: it answers RELEASED without recalling m0 and hands the
// main credit back to the remaining holder, whose release still recalls.
func TestSharedLeaseHolderOfPendingRelease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	first := r.domainRead(0)
	peer := r.startRelease(1, r.domainRead(1)) // m1's scoped recall runs
	payload := map[string]any{"request": r.controlTicket(0), "lease": first}
	if reply := r.view(0, "VIEW_RELEASE", payload); reply.Status != "PENDING" {
		t.Fatalf("release behind a peer recall: %+v", reply)
	}
	if second := r.domainRead(0); second.ID != first.ID {
		t.Fatal("same-lineage acquisition beside a pending release got a new lease")
	}
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", peer)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": peer["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("peer release: %+v", reply)
	}
	if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("pending holder release after the peer recall: %+v", reply)
	}
	if _, _, exists := r.recallSnapshot(); exists || !r.leaseActive(first) {
		t.Fatal("non-last holder release recalled m0")
	}
	if got := r.reservedReleases(first); got != 1 {
		t.Fatalf("remaining holder reserves %d release credits", got)
	}
	r.lastHolderReleases(first)
}

// TestSharedLeaseSurvivesReopen: the holder count is durable ledger state.
// After a controller reopen the first holder's release still answers RELEASED
// without a recall, and the last one still recalls.
func TestSharedLeaseSurvivesReopen(t *testing.T) {
	r := newZQ3Rig(t, 0)
	shared := r.sharedPair()
	r.reopen()
	if got := r.reservedReleases(shared); got != 2 {
		t.Fatalf("reopened holders reserve %d release credits", got)
	}
	if reply := status(r.directView(0, "VIEW_RELEASE", map[string]any{"request": r.controlTicket(0), "lease": shared})); reply != "RELEASED" {
		t.Fatalf("first holder release after reopen: %s", reply)
	}
	if reply := status(r.directView(0, "VIEW_RELEASE", map[string]any{"request": r.controlTicket(0), "lease": shared})); reply != "PENDING" {
		t.Fatalf("last holder release after reopen: %s", reply)
	}
	if scope, _, exists := r.recallSnapshot(); !exists || scope != r.scopes[0].Session {
		t.Fatalf("last holder release recall scope %q exists %v", scope, exists)
	}
}

// TestUnsharedLeaseStillRecalls: a lease with one holder is unchanged by
// q84: its release recalls at once and its ledger entry carries no holder
// count (byte-identical to the pre-q84 layout).
func TestUnsharedLeaseStillRecalls(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.domainRead(0)
	r.controller.mu.Lock()
	raw, err := canonical(r.controller.state.Leases[lease.ID])
	r.controller.mu.Unlock()
	must(t, err)
	if bytes.Contains(raw, []byte("Sharers")) {
		t.Fatalf("unshared lease ledger entry: %s", raw)
	}
	r.lastHolderReleases(lease)
}

// TestSharedPageLeaseReleaseStillRecalls: a cache page lease shared by two
// faults of one page keeps the ordinary path for every holder. The guest
// keeps a published cache object unrevoked until STOP and blocks the session
// on a RELEASED object it still publishes, so the first release must not
// answer RELEASED early: it is PENDING and recalls, the second release waits
// behind that recall, and both are RELEASED only after the certified closure,
// with every credit returned at the end.
func TestSharedPageLeaseReleaseStillRecalls(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(0, "shared-page", false)
	_, a := r.acquire(0, page(description, 0), "READ")
	_, b := r.acquire(0, page(description, 0), "READ")
	if a.Status != "GRANTED" || b.Status != "GRANTED" || a.Lease.ID != b.Lease.ID {
		t.Fatalf("same-page acquisitions not shared: %+v %+v", a, b)
	}
	shared := a.Lease
	first := r.startRelease(0, shared)
	if _, _, exists := r.recallSnapshot(); !exists {
		t.Fatal("first PAGE holder release started no recall")
	}
	second := r.startRelease(0, shared)
	r.completeCleanRecall()
	for i, payload := range []map[string]any{first, second} {
		r.physical("VIEW_RELEASE", payload)
		if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
			t.Fatalf("PAGE holder %d release: %+v", i, reply)
		}
	}
	r.controller.mu.Lock()
	l := r.controller.state.Leases[shared.ID]
	r.controller.mu.Unlock()
	if l.Active || l.ReleaseCredit || l.Sharers != 0 || r.reservedReleases(shared) != 0 {
		t.Fatalf("released PAGE lease state %+v", l)
	}
}

// capacityRig is the zq3 rig with only four control credits.
func capacityRig(t *testing.T) *rig {
	return newRig(t, func(c *Config) {
		b := zq3SpecBounds(t)
		b.ControlTickets = 4
		c.Authorization.Enrollment.Bounds = b
	})
}

// fillHolders shares one DOMAIN READ lease of m0 until the next acquisition
// is BLOCKED, and returns the lease, the last granted holder's ticket and the
// number of holders.
func (r *rig) fillHolders() (*guestbridge.ViewLease, guestbridge.Ticket, int) {
	r.t.Helper()
	shared := r.domainRead(0)
	holders := 1
	var last guestbridge.Ticket
	for {
		ticket, next := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
		if next.Status == "BLOCKED" {
			return shared, last, holders
		}
		if next.Status != "GRANTED" || next.Lease.ID != shared.ID {
			r.t.Fatalf("shared acquisition %+v", next)
		}
		last = ticket
		holders++
	}
}

// TestSharedGrantBlockedAtCapacity: each holder reserves a release credit, so
// with four control credits the fifth same-lineage acquisition is BLOCKED.
func TestSharedGrantBlockedAtCapacity(t *testing.T) {
	r := capacityRig(t)
	shared, _, holders := r.fillHolders()
	if holders != 4 || r.reservedReleases(shared) != 4 {
		t.Fatalf("holders %d reserve %d credits", holders, r.reservedReleases(shared))
	}
}

// TestSharedHolderCancel: cancelling one granted holder of a shared lease
// drops only its reference and credit; the lease stays active, the other
// non-last releases answer RELEASED and the last holder still recalls and is
// certified.
func TestSharedHolderCancel(t *testing.T) {
	r := capacityRig(t)
	shared, cancelled, holders := r.fillHolders()
	original := map[string]any{"request": cancelled, "domain": r.domains[0], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}
	r.physical("VIEW_CANCEL", original)
	if reply := r.view(0, "VIEW_CANCEL", map[string]any{"request": cancelled}); reply.Status != "CANCELLED" {
		t.Fatalf("holder cancel: %+v", reply)
	}
	if r.reservedReleases(shared) != holders-1 || !r.leaseActive(shared) {
		t.Fatalf("after cancel: %d credits, active %v", r.reservedReleases(shared), r.leaseActive(shared))
	}
	for i := 2; i < holders; i++ {
		payload := map[string]any{"request": r.controlTicket(0), "lease": shared}
		if reply := r.view(0, "VIEW_RELEASE", payload); reply.Status != "RELEASED" {
			t.Fatalf("holder release %d: %+v", i, reply)
		}
	}
	r.lastHolderReleases(shared)
}

// TestSharedLeaseRevokedByRecallBeforeRelease: a conflicting acquisition's
// all-domain recall deactivates a shared DOMAIN lease before any holder
// released. The first release then only returns its reference; the last
// holder's release needs no further recall but still the per-lease physical
// certification (g84).
func TestSharedLeaseRevokedByRecallBeforeRelease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "dirty", false)
	shared := r.sharedPair()
	if _, dirty := r.acquire(1, page(description, 0), "EXCLUSIVE_DIRTY"); dirty.Status != "PENDING" {
		t.Fatalf("conflicting acquisition: %+v", dirty)
	}
	r.completeCleanRecall()
	if r.leaseActive(shared) {
		t.Fatal("recall left the shared lease active")
	}
	if reply := r.view(0, "VIEW_RELEASE", map[string]any{"request": r.controlTicket(0), "lease": shared}); reply.Status != "RELEASED" {
		t.Fatalf("first holder release after the recall: %+v", reply)
	}
	payload := map[string]any{"request": r.controlTicket(0), "lease": shared}
	if raw, err := r.execute(0, "VIEW_RELEASE", payload); err == nil {
		t.Fatalf("last holder released without the physical fact: %s", raw)
	}
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("last holder release after the recall: %+v", reply)
	}
	if _, _, exists := r.recallSnapshot(); exists || r.reservedReleases(shared) != 0 {
		t.Fatal("last holder release recalled again or kept a credit")
	}
}

// TestSharedLeaseAcquisitionWaitsForLastHolder (hp4, zq3-r25 R25-1): two
// acquisitions share one active DOMAIN READ lease. After the first holder's
// release its answered release retires at once, but its acquisition still
// names the shared lease, which the second holder keeps active, so a
// VIEW_RETIRE through it stays BLOCKED until the last holder released too.
func TestSharedLeaseAcquisitionWaitsForLastHolder(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, first := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	_, second := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	if first.Status != "GRANTED" || second.Status != "GRANTED" || first.Lease.ID != second.Lease.ID {
		t.Fatalf("shared acquisitions %+v / %+v", first, second)
	}
	payload := map[string]any{"request": r.controlTicket(0), "lease": first.Lease}
	released := r.view(0, "VIEW_RELEASE", payload)
	if released.Status != "RELEASED" || !r.leaseActive(first.Lease) {
		t.Fatalf("first holder release %+v (lease active %v)", released, r.leaseActive(first.Lease))
	}
	if raw := r.retireControl(0, 1, []string{released.ID}); !bytes.Contains(raw, []byte(`"status":"RETIRED"`)) {
		t.Fatalf("first holder's answered release beside the shared lease: %s", raw)
	}
	if raw := r.retire(0, 1, []string{first.ID}); !bytes.Contains(raw, []byte(`"status":"BLOCKED"`)) {
		t.Fatalf("retired the first acquisition while the shared lease is active: %s", raw)
	}
	r.lastHolderReleases(first.Lease)
	if raw := r.retire(0, 2, []string{first.ID, second.ID}); !bytes.Contains(raw, []byte(`"status":"RETIRED"`)) {
		t.Fatalf("acquisitions after the last holder released: %s", raw)
	}
}
