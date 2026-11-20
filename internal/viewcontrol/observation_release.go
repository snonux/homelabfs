package viewcontrol

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Recall-free release of a clean observation lease (task je4, after
// zq3-r14..r17).
//
// Every guest VFS read (hfs_begin(mutation=false) -> hfs_mmap_begin) takes a
// DOMAIN READ observation lease with expected_target = its installed view
// and releases it when the request ends (hfs_mmap_end). Since 784 the last
// holder's release started a recall of the releasing domain: four events,
// each acknowledged after a fresh guest observation (0.5-1.2 s in the lab),
// so 4-7 s per release, and guest.py's credentials admission (realpath,
// lstat, open, fstat of each mount root) took 10-12 s per root and raced
// those recalls (R17-1).
//
// Why a release recalled at all. A release asserts that the lease owns no
// PTE, mapping or dirty byte any more, and the controlled runtime's
// Facts.CheckPhysical certifies that only through the domain fence: a recall
// of the domain completed after the grant, so the fixed observer measured
// the guest's STOPPED (every published object revoked, every PTE gone) and
// REOPENED. That protects three things: (1) the guest's cached view does not
// outlive its lease (a peer's later conflicting write no longer recalls this
// domain, so nothing may still be served under the lease); (2) dirty bytes
// of the lineage drain under their original authority first; (3) a lease
// pins its cut, so peers' cuts and source capacity stay consistent while it
// lives. Releasing a lease never moves the cut and only unpins one, so (3)
// never needed the recall; (2) is vacuous for a READ lease without an open
// reservation; only (1) is in question.
//
// Why (1) holds without a recall for an observation lease. A DOMAIN lease
// authorizes no PTE: guest mappings fault under PAGE leases, and a DOMAIN
// observation installs attributes only under its own stamped short MM token
// while the observation is in flight. hfs_mmap_end removes the observation
// and marks its lease revoked under view->lock before it queues the release,
// cached dentries revalidate through RPC, and attributes never become
// timeless pathname permission (patch 0004). hfs_mmap_pressure never selects
// an observation lease (p84), and hfs_lease_release_locked blocks the
// session if a RELEASED lease is still published and not revoked. So when
// the last holder's release arrives no guest object uses the lease, and the
// recall would stop and reopen a view with nothing under the lease to stop.
// The host already relies on exactly this for every non-last holder of a
// DOMAIN lease (q84, lease_holders.go). The one difference for the last
// holder is that the host lease goes inactive, so a peer's later write no
// longer recalls the domain: its stale installed view is then caught by the
// next observation's expected-target mismatch, which starts the all-domain
// recall (e84), exactly as after the scoped recall this replaces.
//
// releaseSkipsRecall therefore answers RELEASED without a recall only when
// all of these hold (anything else keeps the recall path unchanged):
//
//   - the lease is active, is a DOMAIN READ lease, and this is its last
//     holder (Sharers == 0; a non-last holder already returns at once);
//   - Targeted: every holder acquired with expected_target equal to the
//     granted target, i.e. each is an observation of a view installed at
//     this cut. A target:null acquisition is the mount's
//     hfs_mmap_bootstrap discovery, whose loop needs the recall: when
//     hfs_view_bind_initial refuses (-ESTALE, view at another cut), only
//     the recall installs the current cut before it acquires again. A
//     renewal is never Targeted (the guest never renews);
//   - no other lease of the lineage is active (a renewed sibling keeps the
//     old revision's mappings under it);
//   - the domain holds no EXCLUSIVE_DIRTY lease and no open reservation
//     (releaseRecallScope would scope the recall), so nothing of the domain
//     waits to freeze or drain and no lineage reservation can exist;
//   - no recall runs (releaseLease answers PENDING before this);
//   - the current cut equals the lease's target. Then the guest's installed
//     view is current and the recall would reinstall the same cut. If the
//     cut moved (a peer FORGET, final DESCRIPTION_RELEASE or pinning
//     LOOKUP), the scoped recall is kept: it is the cheapest way to bring
//     this domain's view current, where skipping it would make the next
//     observation start an all-domain expected-target recall;
//   - the domain has lineage headroom (lineageHeadroom). A lease lineage
//     includes the credentials (with the process UUID) and the target, so
//     nearly every guest process opens its own lineage, and its revision
//     high-water (Revisions, LineageFences) may only be recycled once the
//     domain's fence moved past it: dropping it earlier would let a later
//     grant of that lineage reuse a lease ID. Before je4 every release moved
//     the fence; without recalls the entries would pile up, and a ledger with
//     more Revisions than Bounds.Identities is refused on reopen. So once
//     the domain holds lineageBudget lineages at its current fence, the
//     release keeps its scoped recall, which moves the fence and lets the
//     next VIEW_RETIRE's compaction recycle them: one recall per
//     lineageBudget processes instead of one per observation.
//
// The release then deactivates the lease and still asks Facts.CheckPhysical,
// which certifies a DOMAIN READ Targeted lease without a fence advance
// (controlledruntime facts.go checkRelease); a refusal restores the lease and
// returns the error, as the recall path does. The domain's fence and view do
// not move, so a following observation gets a fresh lease of the same
// lineage at the next revision (or shares a still active one) with no
// recall. PAGE and OBJECT leases keep the recall: the guest publishes cache
// objects under them until STOP.
//
// Guest follow-up (not needed for safety today): the host trusts the guest's
// revoke-before-release for DOMAIN leases instead of measuring it. A guest
// that reported the revocation in its release (or a measured observer field
// for nr_observations) would let Facts check it rather than rely on the
// kernel's own hfs_lease_release_locked check.

// releaseSkipsRecall reports whether the last holder's release of old needs
// no recall (see the top of this file). The ledger conditions come first;
// only then is the current cut captured to compare it with the lease target.
func (c *Controller) releaseSkipsRecall(ctx context.Context, old leaseState) (bool, error) {
	if !c.observationReleasable(old) {
		return false, nil
	}
	source, err := c.capture(ctx)
	if err != nil {
		return false, err
	}
	return equal(c.target(old.Lease.Scope, source), old.Lease.Target), nil
}

// observationReleasable checks the ledger conditions of a recall-free
// release of old: an active, unshared, Targeted DOMAIN READ lease without an
// active sibling, in a domain with no EXCLUSIVE_DIRTY lease and no open
// reservation.
func (c *Controller) observationReleasable(old leaseState) bool {
	lease := old.Lease
	if !old.Active || old.Sharers != 0 || !old.Targeted || lease.Access.Kind != "DOMAIN" || lease.Mode != "READ" {
		return false
	}
	for _, l := range c.state.Leases {
		if l.Active && l.Lease.ID != lease.ID && sameLineage(l.Lease, lease) {
			return false
		}
	}
	return c.releaseRecallScope(lease) != "" && c.lineageHeadroom(lease.Scope.Session)
}

// lineageBudget is how many lease lineages one domain may hold at its
// current fence before an observation release recalls again: half of
// Bounds.Identities (the reopen bound on Revisions) shared by the domains, so
// the other half stays for leases of the recalling paths and lineages that
// wait for the next compaction.
func (c *Controller) lineageBudget() int {
	enrollment := c.config.Authorization.Enrollment
	return max(enrollment.Bounds.Identities/(2*len(enrollment.Domains)), 1)
}

// lineageHeadroom reports whether session holds fewer than lineageBudget
// lineages at its current fence. Lineages at an older fence are not counted:
// the next compaction recycles them without another recall (recycleLineages).
func (c *Controller) lineageHeadroom(session string) bool {
	fence, n := c.fence(session), 0
	for _, at := range c.state.LineageFences {
		if at.Session == session && at.Fence >= fence {
			n++
		}
	}
	return n < c.lineageBudget()
}

// releaseWithoutRecall deactivates old and answers RELEASED once
// Facts.CheckPhysical certified the release against the ledger in which the
// lease is already inactive. On a refusal the lease is restored, so the
// saved ledger is unchanged and the guest's retry finds the same state.
func (c *Controller) releaseWithoutRecall(ctx context.Context, scope guestbridge.Context, op string, r request, t ticketState, old leaseState) (json.RawMessage, error) {
	closed := old
	closed.Active = false
	closed.ReleaseCredit = false
	c.state.Leases[r.Lease.ID] = closed
	ledger, err := c.rawState()
	if err == nil {
		err = c.config.Facts.CheckPhysical(c.factsContext(ctx), scope, op, t.Payload, ledger)
	}
	if err != nil {
		c.state.Leases[r.Lease.ID] = old
		return nil, err
	}
	return c.reply(t, "RELEASED", nil, nil, nil)
}
