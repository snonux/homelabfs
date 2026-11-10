package viewcontrol

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Source pruning outside VIEW_RETIRE (task i64, zq3-r7 R7-2).
//
// compact() used to be the only place that dropped source cuts, and it runs
// only after VIEW_RETIRE, which the guest sends only for terminal view
// tickets. Ordinary I/O never retires, so each mutation kept its cut and a
// lease-free workload hit NoSpace after Sources-3 mutations.
//
// A retained cut is needed only while something can still name its target:
//   - a lease in the ledger, active or not: VIEW replies (acquire, renew,
//     replayed queries) carry it, guestbridge reverifies it with CheckTarget,
//     ReceiptEvidence requires it (retainedTarget), and reservations name
//     their lease. Leases leave the ledger only through compact(), so every
//     ticket reply's lease is still here;
//   - the current recall's events (non-RECALL phases carry a target), which
//     POLL replies, ACKs and the runtime Facts compare.
//
// Nothing else holds a cut by key or target: an admission permit keeps a
// copy of the cut it captured and the controller lock until Close, so no
// pruning runs while one is outstanding; a VIEW_ACQUIRE expected target is
// compared only with the freshly captured current cut; provider claims,
// frozen DRAIN_WRITE results and claim preparation never name a cut; and
// AckArchive keeps only ACK hashes. Restart is helped, not hurt: a C9
// restore requires every retained cut to equal the recaptured current one.
//
// So admissions drop every cut that no lease or recall event targets, except
// the cut being admitted, using compact()'s pin rule. They do it only outside
// a recall, and persist the pruned set in the same save() (one bbolt Update,
// synced to disk) as the admission's own state change, as retire() persists
// compact() with the new floor; a refusal saves the pruned set too. A crash
// before that write leaves the old, larger set, which is still valid; a crash
// after it leaves no reference to a dropped cut.

// sourceKey is the Sources map key of a cut: its journal reference plus the
// state hash, so captures of an unchanged cut deduplicate.
func sourceKey(source fsservice.ControlledViewSource) string {
	return digest(source.Through) + hash(source.State)
}

// pinnedTargets is compact()'s pin rule: the target of every lease in the
// ledger and of every current recall event.
func (c *Controller) pinnedTargets() map[string]bool {
	targets := map[string]bool{}
	for _, l := range c.state.Leases {
		targets[digest(l.Lease.Target)] = true
	}
	if c.state.Recall != nil {
		for _, raw := range c.state.Recall.Events {
			var e struct{ Target *guestbridge.ViewTarget }
			if json.Unmarshal(raw, &e) == nil && e.Target != nil {
				targets[digest(*e.Target)] = true
			}
		}
	}
	return targets
}

// dropUnpinnedSources deletes every retained cut whose target no domain's
// lease or recall event names, except keep, and returns how many it dropped.
func (c *Controller) dropUnpinnedSources(keep string) int {
	targets := c.pinnedTargets()
	dropped := 0
	for key, source := range c.state.Sources {
		if key == keep {
			continue
		}
		pinned := false
		for _, enrollment := range c.config.Authorization.Enrollment.Domains {
			if targets[digest(c.target(enrollment.Scope, source))] {
				pinned = true
				break
			}
		}
		if !pinned {
			delete(c.state.Sources, key)
			dropped++
		}
	}
	return dropped
}

// pruneSources drops unpinned cuts other than keep, outside a recall only,
// and returns how many it dropped. The caller persists the result with its
// own save(), in the same write as the state change it makes, as retire()
// persists compact() together with the new floor. During a recall nothing is
// pruned: drains and install events are charged against the cuts the recall
// started with, and its events pin their targets.
func (c *Controller) pruneSources(keep string) int {
	if c.state.Recall != nil {
		return 0
	}
	return c.dropUnpinnedSources(keep)
}

// admitCut captures the cut an admission runs against, prunes around it,
// charges source capacity for non-read ops and remembers the cut durably.
// Capturing first makes the charge exact: a cut already retained (reads,
// OPEN, SYNC, CLOSE/RELEASE, PROCESS_EXIT after no mutation) costs nothing.
// Capture only flushes the journal writer and reads the applied store, so
// doing it before the capacity check changes no state.
func (c *Controller) admitCut(ctx context.Context, read, drain bool) (fsservice.ControlledViewSource, error) {
	source, err := c.capture(ctx)
	if err != nil {
		return source, err
	}
	key := sourceKey(source)
	pruned := c.pruneSources(key)
	if !read && !c.sourceCapacity(ctx, c.cutCharge(key, drain)) {
		return source, c.refuseAfterPrune(pruned, fsapi.NoSpace)
	}
	if err = c.remember(source); err != nil {
		return source, c.refuseAfterPrune(pruned, err)
	}
	return source, c.save()
}

// refuseAfterPrune persists a pruned ledger before returning a refusal, so
// the live and protected ledgers stay equal even when nothing is admitted.
func (c *Controller) refuseAfterPrune(pruned int, refusal error) error {
	if pruned > 0 {
		if err := c.save(); err != nil {
			return err
		}
	}
	return refusal
}

// cutCharge is sourceCapacity's extra for an admission of the cut key: 0 when
// that cut is already retained or for DRAIN_WRITE (whose own capture was
// never charged), otherwise admittedSources.
func (c *Controller) cutCharge(key string, drain bool) int {
	if _, retained := c.state.Sources[key]; retained || drain {
		return 0
	}
	return admittedSources
}
