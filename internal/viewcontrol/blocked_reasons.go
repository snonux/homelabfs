package viewcontrol

import (
	"encoding/json"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// BLOCKED reasons (task 2h4, zq3-r19 R19-1). A guest that gets a ticket
// BLOCKED cannot tell from the reply why: zq3-r19's access child saw EAGAIN
// and the run could not say whether its VIEW_ACQUIRE was BLOCKED for a stale
// expected target after its recall (expectedMismatch) or for capacity
// (grantLease/grantFits or the remember Sources bound) without decoding
// viewcontrol-v1.db. The controller therefore records each such BLOCKED
// decision with its reason in the diagnostic recall history (task ce4,
// recall_history.go): per-reason counters since this controller opened and a
// bounded ring of the newest decisions. Like the rest of that history it is
// controller memory only: it changes no ledger byte, digest, reply or
// decision, it counts a BLOCKED whose admission later failed to save too, and
// a reopened controller starts empty. A ticket's saved BLOCKED reply is
// replayed without re-deciding, so each recorded row is one decision.
//
// Memory bound: at most BlockedHistoryLimit rows of fixed fields, each about
// 250 bytes encoded (a 64-hex ticket family digest with its number, a session
// UUID, opcode, reason, bound and an RFC 3339 UTC time), so about 4 KiB,
// always well inside RecallEvidenceBytes; fitEvidence still measures it as
// part of the wrapper and trims the ring before any recall record could
// exceed the bound.
const (
	// BlockedHistoryLimit is the number of BLOCKED decisions the ring keeps.
	BlockedHistoryLimit = 16

	// BlockedExpectedMismatch: a VIEW_ACQUIRE whose expected target still
	// differs from the current cut after the recall its mismatch started
	// (expectedMismatch, tickets.go).
	BlockedExpectedMismatch = "EXPECTED_MISMATCH"
	// BlockedCapacity: a grant did not fit a bound; Bound names which one.
	BlockedCapacity = "CAPACITY"
)

// Capacity bounds a BLOCKED of reason BlockedCapacity names (grantBound and
// acquire's remember).
const (
	BoundSources   = "SOURCES"   // Bounds.Sources: the current cut cannot be retained
	BoundControl   = "CONTROL"   // control credits (controlCapacity)
	BoundIdentity  = "IDENTITY"  // identity credits (identityCapacity)
	BoundLeases    = "LEASES"    // the lease table is at Bounds.Identities
	BoundRevisions = "REVISIONS" // the lineage's revision counter is exhausted
	BoundLineages  = "LINEAGES"  // Revisions holds Bounds.Identities lineages (lineageCapacity)
)

// BlockedReply is one recorded BLOCKED decision: the ticket's ledger key
// (family digest "/" number, the key of the ledger's Tickets map), the
// requesting domain session, the ticket's opcode, the reason, for a capacity
// BLOCKED the bound that did not fit, and the controller-clock time in UTC.
type BlockedReply struct {
	Ticket string `json:"ticket"`
	Domain string `json:"domain"`
	Opcode string `json:"opcode"`
	Reason string `json:"reason"`
	Bound  string `json:"bound,omitempty"`
	AtUTC  string `json:"at_utc"`
}

// BlockedHistory is the OBSERVE view of the BLOCKED decisions: the counters
// per reason since this controller opened, Omitted (decisions no longer in
// Recent: pushed out of the ring or trimmed by fitEvidence) and the newest
// decisions, oldest first. RecallHistory carries it omitempty, so evidence
// without any BLOCKED decision keeps its pre-2h4 shape.
type BlockedHistory struct {
	ExpectedMismatch uint64         `json:"expected_mismatch"`
	Capacity         uint64         `json:"capacity"`
	Omitted          uint64         `json:"omitted"`
	Recent           []BlockedReply `json:"recent"`
}

// blockedHistory is the in-memory ring, guarded by Controller.mu.
type blockedHistory struct {
	recent           []BlockedReply
	expectedMismatch uint64
	capacity         uint64
}

// add records row, dropping the oldest row once the ring is full.
func (b *blockedHistory) add(row BlockedReply) {
	// Count each known reason explicitly, so a reason added later cannot be
	// counted under the wrong name by default (2h4 review).
	switch row.Reason {
	case BlockedExpectedMismatch:
		b.expectedMismatch++
	case BlockedCapacity:
		b.capacity++
	}
	if len(b.recent) == BlockedHistoryLimit {
		copy(b.recent, b.recent[1:])
		b.recent = b.recent[:BlockedHistoryLimit-1]
	}
	b.recent = append(b.recent, row)
}

// view copies the ring for OBSERVE, or nil when nothing was ever BLOCKED.
func (b *blockedHistory) view() *BlockedHistory {
	total := b.expectedMismatch + b.capacity
	if total == 0 {
		return nil
	}
	recent := append([]BlockedReply{}, b.recent...)
	return &BlockedHistory{ExpectedMismatch: b.expectedMismatch, Capacity: b.capacity, Omitted: total - uint64(len(recent)), Recent: recent}
}

// blocked records why ticket t of scope is BLOCKED (reason, and for a
// capacity BLOCKED the bound) and answers the BLOCKED reply. c.mu is held.
func (c *Controller) blocked(scope guestbridge.Context, t ticketState, reason, bound string) (json.RawMessage, error) {
	key, _ := ticketKeys(scope, t.Request)
	c.history.blocked.add(BlockedReply{Ticket: key, Domain: scope.Session, Opcode: t.Operation, Reason: reason, Bound: bound, AtUTC: c.clock().UTC().Format(time.RFC3339Nano)})
	return c.reply(t, "BLOCKED", nil, nil, nil)
}

// fitBlocked trims the oldest Recent rows of h.Blocked until the wrapper of h
// (measured with Omitted at its largest possible value, n recall records)
// fits RecallEvidenceBytes, and returns that wrapper's encoded size. With
// BlockedHistoryLimit rows it always fits at once; the loop only keeps the
// bound true should the rows ever grow.
func fitBlocked(h *RecallHistory, n int) (int, error) {
	for {
		wrapper := RecallHistory{Capacity: h.Capacity, Total: h.Total, Advances: h.Advances, Blocked: h.Blocked, Omitted: n, Recalls: []RecallRecord{}}
		base, err := json.Marshal(wrapper)
		if err != nil || len(base) <= RecallEvidenceBytes || h.Blocked == nil || len(h.Blocked.Recent) == 0 {
			return len(base), err
		}
		trimmed := *h.Blocked
		trimmed.Recent = trimmed.Recent[1:]
		trimmed.Omitted++
		h.Blocked = &trimmed
	}
}
