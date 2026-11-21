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
// of holders beyond the first, and the fast path depends on the access kind:
//
//   - DOMAIN lease (observation and bootstrap discovery leases). A release
//     while Sharers > 0 only returns that holder's reference: it answers
//     RELEASED at once, starts no recall and leaves the lease's activity
//     untouched. It must not answer PENDING and wait for the others: the
//     guest serializes a mount's releases on one cleanup worker, so a waiting
//     release would sit in front of the release it waits for (the g84
//     deadlock). This is safe because a DOMAIN lease authorizes no PTE and
//     hfs_mmap_end revokes the observation's guest object before it queues
//     the release. A holder RELEASED certifies nothing physical: the host
//     lease stays active and every recall still covers it.
//   - PAGE or OBJECT lease (cache page leases). Two faults of one page under
//     one description and credentials share the lease; the guest publishes
//     each cache object through hfs_view_page_enter and revokes it only at
//     STOP or invalidation, and pressure may release such a published object.
//     The guest's hfs_lease_release_locked blocks the whole session when a
//     RELEASED object is still published and unrevoked, so an early RELEASED
//     here is a liveness hazard, not something that check absorbs. These
//     releases keep the ordinary path (PENDING, recall, per-lease
//     certification of task g84); the recall revokes every holder's object
//     and frees capacity. Only where that path answers RELEASED does a
//     non-last holder give back its reference and credit instead of clearing
//     the lease's release credit, so the remaining holders' releases are
//     still funded.
//
// The last holder's release of any lease takes the ordinary path, except
// that a guest observation lease (every holder Targeted, DOMAIN READ, current
// cut) is RELEASED there without a recall (task je4, observation_release.go).
// The guest's hfs_mmap_bootstrap discovery release relies on its release
// starting a recall; it acquires with target:null, so its lease is never
// Targeted, and a discovery holder that shares an observation lease clears
// Targeted for good (shareLease), so the last holder's release recalls.
//
// Each holder owns one reserved release credit, as the protocol requires of a
// preowned release: ReleaseCredit for one holder plus Sharers more, counted by
// controlCapacity and identityCapacity. A shared grant therefore needs the
// same capacity as a new lease and is BLOCKED without it. Whichever release
// arrives while the main credit is unbound binds it (ticketCapacity); a later
// release while it is bound is admitted on a sharer credit. A holder release
// gives back one sharer credit and, if it had bound the main credit, unbinds
// it again for the remaining holders, so once every admitted release has
// been answered the reserved credits equal the holders that have not
// released. (Two PAGE releases PENDING at once reserve one credit too many
// until they are answered, which only errs towards BLOCKED.)
//
// This is not an over-count (reviewed in task qe4, zq3-r16 R16-1): a lease
// shared by N observations costs N control charges because each holder sends
// its own VIEW_RELEASE on its own control ticket. A RELEASED holder ticket
// keeps its charge only until a VIEW_RETIRE floor passes it
// (retirementBlocked); the zq3 spec sizes ControlTickets for that.
//
// A renewal (renewLease) leaves the old revision's record as it was apart
// from Active = false: its ReleaseCredit, ReleaseTicket and Sharers stay
// there and keep funding every holder of the old revision, including the
// renewing one, whose old-revision release the guest still owes. The new
// revision starts with its own fresh ReleaseCredit and no sharers. The old
// revision's last holder's release recalls the lineage while the renewed
// sibling is active, as before (the guest never renews).

// shareLease grants the active lease existing (stored under id) to one more
// acquisition ticket, reserving that holder's release credit. targeted says
// whether this holder named the lease's target as its expected target; one
// untargeted (discovery) holder clears Targeted for good, so the last
// holder's release keeps its recall (observation_release.go). advance says
// whether this holder requested advance (task xf4); Advance is ANDed the same
// way, so one holder without it makes the last release keep je4's
// current-cut condition for good.
func (c *Controller) shareLease(scope guestbridge.Context, id string, existing leaseState, t ticketState, targeted, advance bool) (json.RawMessage, error) {
	// Defensive: grantLease, the only caller, has already passed grantBound,
	// which checks these same two credits, so this never fires today. It
	// stays so that a future caller cannot share past the bounds, and records
	// its capacity BLOCKED like grantLease (task 2h4).
	if !c.controlCapacity(1) {
		return c.blocked(scope, t, BlockedCapacity, BoundControl)
	}
	if !c.identityCapacity(1) {
		return c.blocked(scope, t, BlockedCapacity, BoundIdentity)
	}
	existing.Sharers++
	existing.Targeted = existing.Targeted && targeted
	existing.Advance = existing.Advance && advance
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
// holders still hold: it gives back that holder's credit and answers RELEASED,
// leaving the lease's activity and release credit to the remaining holders.
// releaseLease calls it at once for a DOMAIN lease and only after the
// certified closure for a PAGE or OBJECT lease.
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
