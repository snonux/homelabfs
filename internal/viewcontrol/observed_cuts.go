package viewcontrol

import (
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Observable digests of recent cuts (task e84, mismatch_scope.go).
//
// An expected-target mismatch can be scoped only when the expected cut is
// observably equal to the current one, but the expected cut is usually
// pruned from Sources by then (its leases were released, retired and
// compacted). The controller keeps the observable digest of the last
// observedCutLimit cuts it retained, in memory only and guarded by mu: it is
// not part of the protected ledger, so no ledger byte, digest or reply
// changes, and a reopened controller starts empty. A missing digest only
// keeps the all-domain recall, so losing the cache is safe.
//
// The key is the cut's whole identity without the per-domain context hash
// (cutKey): journal incarnation, epoch, position and hash, the state hash
// (SHA-256 of the complete State bytes) and the serving generation, under
// this controller's enrollment and profile. A target naming a cut therefore
// finds exactly the digest computed from that cut's actual bytes; a forged
// or unknown target finds nothing.

// observedCutLimit bounds the in-memory digest cache: 64 entries of about
// 200 bytes. A domain's installed view is one of the cuts retained since its
// last grant or install, far fewer than 64 at the zq3 spec (Sources 10).
const observedCutLimit = 64

// observedCut is one remembered digest with the journal incarnation and
// position used to evict the oldest entry first.
type observedCut struct {
	digest, incarnation string
	position            uint64
}

// cutKey identifies a cut independently of the domain whose target names it.
func cutKey(cut guestbridge.LocalViewCut) string {
	cut.ContextHash = ""
	return digest(cut)
}

// observe computes the observable digest of source, remembers it under the
// source's cut and returns it.
func (c *Controller) observe(source fsservice.ControlledViewSource) (string, bool) {
	sum, ok := source.ObservableDigest()
	if !ok {
		return "", false
	}
	if c.observed == nil {
		c.observed = map[string]observedCut{}
	}
	cut := c.target(guestbridge.Context{}, source).Cut
	c.observed[cutKey(*cut)] = observedCut{digest: sum, incarnation: cut.JournalIncarnation, position: cut.JournalPosition}
	for len(c.observed) > observedCutLimit {
		c.evictOldestObserved(cut.JournalIncarnation)
	}
	return sum, true
}

// digestOf returns the observable digest of source, from the cache when its
// cut is remembered, else by computing and remembering it (observe).
func (c *Controller) digestOf(source fsservice.ControlledViewSource) (string, bool) {
	if o, ok := c.observed[cutKey(*c.target(guestbridge.Context{}, source).Cut)]; ok {
		return o.digest, true
	}
	return c.observe(source)
}

// evictOldestObserved drops one remembered cut: a cut of a journal
// incarnation other than current (an older incarnation, which no current
// target can name any longer) first, else the one with the lowest journal
// position.
func (c *Controller) evictOldestObserved(current string) {
	oldest, first := "", true
	var best observedCut
	for key, o := range c.observed {
		if first || olderObserved(o, best, current) {
			oldest, best, first = key, o, false
		}
	}
	delete(c.observed, oldest)
}

// olderObserved reports whether a should be evicted before b: a foreign
// incarnation before the current one, then the lower journal position.
func olderObserved(a, b observedCut, current string) bool {
	if (a.incarnation != current) != (b.incarnation != current) {
		return a.incarnation != current
	}
	return a.position < b.position
}

// observedDigest returns the observable digest of the cut target names for
// scope: from the cache, else from the retained source (and caches it).
// It reports false for a target of another kind or domain, or a cut whose
// digest is neither remembered nor derivable.
func (c *Controller) observedDigest(scope guestbridge.Context, target guestbridge.ViewTarget) (string, bool) {
	cut := target.Cut
	if target.Kind != "CONTROLLED_LOCAL" || cut == nil || cut.ContextHash != digest(scope) {
		return "", false
	}
	if o, ok := c.observed[cutKey(*cut)]; ok {
		return o.digest, true
	}
	source, ok := c.retainedSource(scope, target)
	if !ok {
		return "", false
	}
	return c.observe(source)
}

// retainedSource finds the retained cut whose target for scope is target.
// The journal position is compared first, so the state hash is computed only
// for the matching candidates.
func (c *Controller) retainedSource(scope guestbridge.Context, target guestbridge.ViewTarget) (fsservice.ControlledViewSource, bool) {
	for _, source := range c.state.Sources {
		if source.Through.Position.Sequence != target.Cut.JournalPosition {
			continue
		}
		if equal(c.target(scope, source), target) {
			return source, true
		}
	}
	return fsservice.ControlledViewSource{}, false
}
