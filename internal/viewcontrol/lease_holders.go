package viewcontrol

import (
	"encoding/json"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Shared lease holders (task q84, follow-up of p84 and zq3-r9 R9-1).
//
// grantLease hands the active lease of a lineage (domain, access, credential
// hash, mode, target, fence) to every acquisition of that lineage. The guest
// (patch 0004) does not share anything itself: each observation calls
// hfs_lease_acquire with its own acquisition ticket, builds its own guest
// hfs_lease object with its own preowned release ticket, and its
// hfs_mmap_end queues the release of exactly that object. Two concurrent
// same-lineage observations of one mount therefore hold two guest objects
// naming one host lease ID, and send two VIEW_RELEASEs on distinct control
// tickets.
//
// Before q84 the first of those releases started the release recall (since
// 784 a scoped recall of the domain for a clean READ lease). That moved the
// view under the observation still in flight: its stamped hfs_attr_install
// saw a moved fence/revision (ESTALE) and its next admission was refused
// behind the recall, as zq3-r9 R9-1 showed for pressure releases.
//
// The host therefore counts holders. The lease state's Sharers is the number
// of holders beyond the first. A release while Sharers > 0 only returns that
// holder's reference: it answers RELEASED at once, starts no recall and
// leaves the lease's activity untouched. Only the release of the last holder
// takes the ordinary path (recall, then the per-lease physical certification
// of task g84). A holder release must not answer PENDING and wait for the
// others: the guest serializes a mount's releases on one cleanup worker, so a
// waiting release would sit in front of the release it waits for (the g84
// deadlock). A holder RELEASED certifies nothing physical: the host lease
// stays active, every recall still covers it and its mappings stay
// authorized by it. The guest independently refuses a RELEASED object it
// still publishes (hfs_lease_release_locked).
//
// Each holder owns one reserved release credit, as the protocol requires of a
// preowned release: ReleaseCredit for one holder plus Sharers more, counted by
// controlCapacity and identityCapacity. A shared grant therefore needs the
// same capacity as a new lease and is BLOCKED without it. Whichever release
// arrives while the main credit is unbound binds it (ticketCapacity); a later
// release while it is bound is admitted on a sharer credit. A holder release
// gives back one sharer credit and, if it had bound the main credit, unbinds
// it again for the remaining holders, so the reserved credits always equal
// the holders that have not released.
//
// A renewal moves only its own holder to the new revision; the old revision
// keeps its remaining holders, and its last holder's release recalls the
// lineage as before (the guest never renews).

// shareLease grants the active lease existing (stored under id) to one more
// acquisition ticket, reserving that holder's release credit.
func (c *Controller) shareLease(id string, existing leaseState, t ticketState) (json.RawMessage, error) {
	if !c.controlCapacity(1) || !c.identityCapacity(1) {
		return c.reply(t, "BLOCKED", nil, nil, nil)
	}
	existing.Sharers++
	c.state.Leases[id] = existing
	lease := existing.Lease
	return c.reply(t, "GRANTED", &lease, nil, nil)
}

// sharerReleaseAdmissible reports whether a VIEW_RELEASE of lease state l may
// be admitted on a reserved sharer credit: the main credit is already bound,
// another holder remains, and the release names exactly this lease under its
// scope and credentials (the checks changeLease repeats), so a foreign or
// malformed release cannot spend a holder's credit.
func sharerReleaseAdmissible(scope guestbridge.Context, l leaseState, r request) bool {
	return l.Sharers > 0 && equal(l.Lease, r.Lease) && l.Lease.Scope == scope && l.Lease.CredentialHash == digest(r.Request.Credentials)
}

// releaseHolder answers the release of one holder of a lease that other
// holders still hold: it gives back that holder's credit and answers RELEASED
// without a recall, leaving the lease as it is for the remaining holders.
func (c *Controller) releaseHolder(scope guestbridge.Context, r request, t ticketState, old leaseState) (json.RawMessage, error) {
	old.Sharers--
	if key, _ := ticketKeys(scope, t.Request); old.ReleaseTicket == key {
		old.ReleaseTicket = "" // the main credit funds a remaining holder again
	}
	c.state.Leases[r.Lease.ID] = old
	return c.reply(t, "RELEASED", nil, nil, nil)
}

// cancelHolder drops the holder reference of a cancelled granted acquisition
// of a shared lease. It reports false when the lease has no other holder, so
// the caller deactivates the lineage as before.
func (c *Controller) cancelHolder(lease guestbridge.ViewLease) bool {
	l, ok := c.state.Leases[lease.ID]
	if !ok || l.Sharers == 0 || !equal(l.Lease, lease) {
		return false
	}
	l.Sharers--
	c.state.Leases[lease.ID] = l
	return true
}
