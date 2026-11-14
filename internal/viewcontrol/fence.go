package viewcontrol

// LedgerSchema is the layout version of the protected controller ledger
// (viewcontrol-v1.db) inside the unchanged EnrollmentVersion bucket. Schema 2
// replaced the single controller-wide Fence with one fence per domain
// (Fences). A ledger without this exact schema, including every schema-1
// ledger, is refused on reopen with fsapi.Permission: a controller-wide fence
// cannot be split into per-domain fences without each guest mount's REOPENED
// history, so such a ledger is never guessed forward.
const LedgerSchema = 2

// DomainFence returns the physical cache-lifetime fence of one domain
// (session) in a schema-2 ledger's Fences map.
//
// The guest kernel keeps a fence per mount: it starts at 1 and advances only
// when that mount's own REOPENED acknowledgement is accepted. The controller
// mirrors exactly that: a domain that never completed a recall has no Fences
// entry and is at fence 1, and every completed recall advances only the
// domains that received its events. A controller-wide fence instead stamped a
// later domain's first lease with another domain's recall count, which the
// guest refused (zq3-r6 R6-1, mount-1 EKEYREJECTED).
func DomainFence(fences map[string]uint64, session string) uint64 {
	if fence := fences[session]; fence != 0 {
		return fence
	}
	return 1
}

// fence is the current fence of one domain in the live ledger.
func (c *Controller) fence(session string) uint64 {
	return DomainFence(c.state.Fences, session)
}

// fenceExhausted reports whether a new recall would have to advance a domain
// fence that is already at its maximum. The recall's participants are exactly
// the live (unsealed) inventoried domains that events() issues to: all of
// them, or only scope for a domain-scoped recall (scope != "").
func (c *Controller) fenceExhausted(scope string) bool {
	for session := range c.state.Domains {
		if _, sealed := c.state.Terminal[session]; sealed || (scope != "" && session != scope) {
			continue
		}
		if c.fence(session) == ^uint64(0) {
			return true
		}
	}
	return false
}

// advanceFences completes a recall: every domain that received its events
// (and therefore acknowledged REOPENED, see allAcked) moves to its next fence.
// Domains outside the recall keep theirs, as their guest mounts do.
func (c *Controller) advanceFences(r *recallState) {
	for session := range r.Events {
		c.state.Fences[session] = c.fence(session) + 1
	}
}

// validFences checks the per-domain fences of a reopened ledger. Only a
// domain that completed a recall has an entry, so every entry belongs to a
// registered domain and is at least 2 (an absent entry is fence 1).
func (s ledger) validFences() bool {
	if s.Schema != LedgerSchema || s.Fences == nil || len(s.Fences) > len(s.Domains) {
		return false
	}
	for session, fence := range s.Fences {
		if _, ok := s.Domains[session]; !ok || fence < 2 {
			return false
		}
	}
	return true
}
