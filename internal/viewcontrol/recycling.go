package viewcontrol

import (
	"encoding/json"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// View identity recycling (task q64) and recall history pruning (task f84).
//
// Before these tasks every view ticket stayed in the ledger as a tombstone
// and kept its Bounds.Identities charge, and the ACK archive, the recall
// UUID set, the lineage revision high-waters and the retirement receipts
// only grew. A guest read takes a DOMAIN READ lease (an acquisition ticket
// and a release ticket) and its release runs a scoped recall, so at the zq3
// spec (Identities 64) a controller lifetime allowed about 32 lease-taking
// reads and about 100 recalls (984 workload model). Each of these records is
// now dropped once nothing can reference it any more:
//
//   - Tickets: VIEW_RETIRE's protected floor retires a run of terminal
//     tickets; compact deletes their rows and returns their Identities
//     charge. A family's numbers stay monotone because the family (High,
//     Floor) is never deleted, so a recycled number can never be admitted
//     again: an admission op under it gets a rights-free RETIRED_ID bound
//     to its own bytes, and QUERY/CANCEL of it are refused as Stale
//     (retiredReply). The guest's view code never sends one: it retires
//     only tickets it holds no reference to, with a terminal reply, and
//     frees them once RETIRED arrives (hfs_view_retire); only test harnesses
//     replay retired admissions (terminal-kernel80).
//   - Lineage revisions: a lease lineage includes its domain's fence, which
//     only grows. Once no lease of a lineage is in the ledger (so no RENEW
//     can name it) and the domain's fence is past the lineage's, no grant can
//     mint that lineage again, so its revision high-water can never prevent
//     a content-id reuse and is dropped (recycleLineages).
//   - Retirement receipts: only each family's latest receipt is kept (see
//     retire). Receipts a ledger wrote before q64 name no family and are
//     never pruned.
//   - ACK archive: per session only the ACKs of the recall that session
//     acknowledged last are kept (pruneAckArchive).
//   - Recall and reservation provenance UUIDs: a UUID is dropped once no
//     retained recall, reservation, ACK archive entry or ticket names it
//     (pruneUUIDs).
//
// Every rule only drops what no live record names, and every drop is saved
// in the same protected write as the change that allowed it, so a crash
// before that write leaves the larger, still valid ledger. Ticket families
// themselves are kept: they are per full credentials, bounded by the guest's
// HFS_VIEW_FAMILIES_MAX per session, and their counters are what makes
// recycling safe.
//
// A sealed session's ticket rows are left alone: VerifyTerminalSeal compares
// its frozen snapshot with the live ledger. Only a ledger written before q64
// can still hold such tombstones; a newer one recycles them before the seal.
// (Its closed leases and reservations can still be dropped by
// dropUnreferencedLeases, as before q64.)
//
// The ledger shape only gains omitempty fields (family.Scope,
// family.Receipt, LineageFences), so a schema-2 ledger written before q64
// opens unchanged and is cleaned up by the next VIEW_RETIRE and the next
// ACK; no schema bump is needed. Downgrading to a binary without recycling
// is unsupported: its terminal seal refuses families whose tickets were
// recycled.

// compact runs only after VIEW_RETIRE advanced a protected floor: it
// recycles the retired tickets, drops closed reservations and inactive
// leases no unretired ticket still names, then the lineage revisions no
// lease can still extend, every source cut nothing pins any more and every
// UUID nothing names. Admissions prune unpinned cuts too
// (source_pruning.go), with the same pins.
func (c *Controller) compact() {
	c.recycleRetiredTickets()
	c.dropUnreferencedLeases()
	c.recycleLineages()
	c.dropUnpinnedSources("")
	c.pruneUUIDs()
}

// recycleRetiredTickets deletes every retired ticket row outside a sealed
// session and returns its identity. The family keeps its owner (Scope) for
// terminalSnapshot, filled in here for a family written before q64.
func (c *Controller) recycleRetiredTickets() {
	for key, t := range c.state.Tickets {
		if !c.retired(t) {
			continue
		}
		if _, sealed := c.state.Terminal[t.Scope.Session]; sealed {
			continue
		}
		_, fk := ticketKeys(t.Scope, t.Request)
		if f := c.state.Families[fk]; f.Scope == nil {
			owner := t.Scope
			f.Scope = &owner
			c.state.Families[fk] = f
		}
		delete(c.state.Tickets, key)
		c.state.Identities--
	}
}

// retiredReply answers an op on a retired number whose row was recycled.
//
// An admission op (VIEW_ACQUIRE, VIEW_RENEW, VIEW_RELEASE, DIRTY_RESERVE,
// DIRTY_FREEZE) carries its complete original request, so it gets RETIRED_ID
// bound to exactly the bytes it sent: operation and request_hash are those
// of this request. For an exact retransmit that is byte-identical to the
// RETIRED_ID the pre-q64 tombstone answered, because the tombstone kept the
// same operation and hash. RETIRED_ID carries no lease, reservation or entry,
// so it grants nothing and cannot be mistaken for any holder's reply.
//
// Behaviour change for a CHANGED request under the old number: the pre-q64
// tombstone answered it RETIRED_ID with the original operation and hash,
// which guestbridge's validateViewResponse then refused as Stale (operation
// or request_hash mismatch), so it was Stale end to end. Now it gets the
// RETIRED_ID of its own bytes. That is harmless: the reply grants nothing,
// it proves nothing about the earlier admission (CheckResponse vouches only
// for these bytes), the guest accepts a ticket reply only when operation and
// request_hash match its own ticket (hfs_ticket_reply), and its exchange
// loop treats RETIRED_ID as -EAGAIN, never as success.
//
// QUERY and CANCEL carry only the ticket, so the original operation and hash
// are gone with the row: they are refused as Stale.
func (c *Controller) retiredReply(op string, ticket guestbridge.Ticket, raw json.RawMessage) (json.RawMessage, error) {
	if queryOrCancel(op) {
		return nil, fsapi.Stale
	}
	t := ticketState{Request: ticket, Operation: op, PayloadHash: digest(map[string]any{"op": op, "payload": raw})}
	return c.reply(t, "RETIRED_ID", nil, nil, nil)
}

// queryOrCancel reports the ops that address an existing ticket instead of
// carrying an original admission.
func queryOrCancel(op string) bool { return op == "VIEW_QUERY" || op == "VIEW_CANCEL" }

// recycleLineages drops the revision high-water of every recorded lineage
// that no lease in the ledger belongs to and whose domain fence has moved
// past it. A lineage without a LineageFences entry (written before q64) is
// kept, as before.
func (c *Controller) recycleLineages() {
	live := map[string]bool{}
	for _, l := range c.state.Leases {
		live[lineageID(l.Lease)] = true
	}
	for lineage, at := range c.state.LineageFences {
		if live[lineage] || at.Fence >= c.fence(at.Session) {
			continue
		}
		delete(c.state.Revisions, lineage)
		delete(c.state.LineageFences, lineage)
	}
}

// archiveKey is the AckArchive key of one accepted ACK.
func archiveKey(scope guestbridge.Context, ack acknowledgement) string {
	return digest(scope) + "/" + ack.Recall + "/" + ack.Phase + "/" + stringNumber(ack.Sequence)
}

// archiveParts splits an AckArchive key into its scope digest and recall.
func archiveParts(key string) (scope, recall string, ok bool) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// pruneAckArchive keeps only the archived ACKs of recall for the session
// whose scope digest is scope; it runs when that session's ACK of recall is
// archived. The guest's view control worker sends one ACK at a time and
// sends the next only after the previous one was answered ACCEPTED
// (hfs_mmap_ack, patch 0004), and a later recall starts only after every
// ACK of the earlier one was accepted. So an ACK of a later recall proves
// the session will never resend an ACK of an earlier one, whose replay
// would otherwise need its archived hash to be answered ACCEPTED again. A
// pruned ACK is answered STALE: its recall is not the current one, and the
// session's event sequence only grows, so it can never match a newer event.
func (c *Controller) pruneAckArchive(scope, recall string) {
	for key := range c.state.AckArchive {
		if s, r, ok := archiveParts(key); ok && s == scope && r != recall {
			delete(c.state.AckArchive, key)
		}
	}
}

// pruneUUIDs drops every UUID that no retained state names. A recycled UUID
// could only be issued again by a 122-bit random collision, and even then an
// old ACK carrying it fails the current recall's per-session sequence and
// hashes, and an old reservation's provenance belongs to an object whose id
// binds its own lease, claim number and credentials.
func (c *Controller) pruneUUIDs() {
	used := c.referencedUUIDs()
	for id := range c.state.UUIDs {
		if !used[id] {
			delete(c.state.UUIDs, id)
		}
	}
}

// validRecycling checks the recycling records of a reopened ledger: the
// identity charge is not negative, every lineage fence belongs to a kept
// revision high-water, and every family's latest receipt is kept.
func (s ledger) validRecycling() bool {
	if s.Identities < 0 {
		return false
	}
	for lineage := range s.LineageFences {
		if _, ok := s.Revisions[lineage]; !ok {
			return false
		}
	}
	for _, f := range s.Families {
		if f.Receipt != "" && s.Retirements[f.Receipt] == nil {
			return false
		}
	}
	return true
}

// referencedUUIDs collects the recall and provenance UUIDs that the current
// recall, the reservations, the ACK archive and the retained tickets name.
func (c *Controller) referencedUUIDs() map[string]bool {
	used := map[string]bool{}
	if c.state.Recall != nil {
		used[c.state.Recall.ID] = true
	}
	for _, r := range c.state.Reservations {
		used[r.Recall] = true
		used[r.Reservation.Provenance] = true
	}
	for key := range c.state.AckArchive {
		if _, recall, ok := archiveParts(key); ok {
			used[recall] = true
		}
	}
	for _, t := range c.state.Tickets {
		var original request
		if json.Unmarshal(t.Payload, &original) == nil {
			used[original.Recall] = true
			used[original.Reservation.Provenance] = true
		}
		for _, raw := range []json.RawMessage{t.Initial, t.Current, t.Cancelled} {
			var response struct{ Reservation *guestbridge.DirtyReservation }
			if json.Unmarshal(raw, &response) == nil && response.Reservation != nil {
				used[response.Reservation.Provenance] = true
			}
		}
	}
	return used
}
