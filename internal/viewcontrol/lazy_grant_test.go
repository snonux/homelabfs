package viewcontrol

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestPendingAcquireGrantedLazilyAndStable documents the host side of task
// 2f4 (zq3-r17 R17-1). A VIEW_ACQUIRE answered PENDING because its domain is
// recalling is not granted when the recall completes: finishRecall and
// advanceFences only raise the fences, and the ticket stays PENDING until the
// next VIEW_QUERY re-admits it (queryOrCancelTicket -> readmit). That query
// grants at the fence the last REOPENED ACK raised, so a guest whose ACK reply
// is still in flight sees a lease one fence ahead of its own. Every later
// VIEW_QUERY returns the stored GRANTED byte for byte: the guest may wait
// for its fence to catch up and query again without a new ticket, identity
// or lease (hfs_view_grant_settle, patch 0004).
func TestPendingAcquireGrantedLazilyAndStable(t *testing.T) {
	r := newZQ3Rig(t, 0)
	released := r.domainRead(1)
	payload := r.startRelease(1, released)
	ticket, acquired := r.acquire(1, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	if acquired.Status != "PENDING" {
		t.Fatalf("READ acquisition during its domain's recall: %+v", acquired)
	}
	before := r.controller.fence(r.scopes[1].Session)
	r.completeCleanRecall()
	r.controller.mu.Lock()
	after := r.controller.fence(r.scopes[1].Session)
	pending := 0
	for _, saved := range r.controller.state.Tickets {
		if saved.Operation == "VIEW_ACQUIRE" && saved.Request.Number == ticket.Number && saved.Request.Nonce == ticket.Nonce && status(saved.Current) == "PENDING" {
			pending++
		}
	}
	r.controller.mu.Unlock()
	if after != before+1 || pending != 1 {
		t.Fatalf("after the recall: fence %d -> %d, PENDING acquisitions %d; want +1 and a still PENDING ticket", before, after, pending)
	}
	query := map[string]any{"request": ticket}
	first := r.directView(1, "VIEW_QUERY", query)
	var granted viewReply
	must(t, json.Unmarshal(first, &granted))
	if granted.Status != "GRANTED" || granted.Lease == nil || granted.Lease.Fence != after {
		t.Fatalf("first VIEW_QUERY after the recall: %s, want GRANTED at fence %d", first, after)
	}
	leases := r.activeLeases()
	for range 3 {
		if again := r.directView(1, "VIEW_QUERY", query); !bytes.Equal(again, first) {
			t.Fatalf("repeated VIEW_QUERY changed the stored GRANTED:\n%s\n%s", first, again)
		}
	}
	if r.activeLeases() != leases {
		t.Fatalf("repeated VIEW_QUERY changed the active leases: %d -> %d", leases, r.activeLeases())
	}
	r.physical("VIEW_RELEASE", payload)
}

// activeLeases counts the controller's active leases.
func (r *rig) activeLeases() int {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	active := 0
	for _, lease := range r.controller.state.Leases {
		if lease.Active {
			active++
		}
	}
	return active
}
