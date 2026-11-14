//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// TestRuntimeAssemblyOlderReleaseBesideNewerLease replays the serialized-
// release deadlock of task g84 against the assembled runtime. The guest runs
// every lease release of one mount on a single cleanup worker
// (alloc_workqueue "9p-hfs-cleanup", max_active 1), so a newer lease's release
// is queued behind the older one and is never sent while the older one waits.
//
// Process A's VIEW_RELEASE starts the scoped recall of its clean READ lease;
// the recall completes (STOPPED .. REOPENED) and, before A's worker retries,
// process B on the same mount acquires a newer lease at the advanced fence.
// A's release must now answer RELEASED although B is active and B's release
// was never sent: the recall's observed STOPPED already closed every PTE of
// A, and B holds its own authority. Facts.CheckPhysical used to refuse while
// ANY lease of the domain was active, so A waited for B, B waited behind A on
// the guest worker, and A hit the 30 s timeout.
//
// B's own authority is unaffected: its later release still needs its own
// physical recall (PENDING first, RELEASED only after REOPENED).
func TestRuntimeAssemblyOlderReleaseBesideNewerLease(t *testing.T) {
	r, c := assemblyFixture(t)
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	for _, peer := range r.peers {
		attachAssembly(peer, a, &counter)
	}
	families := c.Authorization.Enrollment.TicketFamilies[0]
	peer := r.peers[0]
	_, domain := bootstrapMount(t, peer, families) // domain fence 1 -> 2
	older := acquireDomainLease(t, peer, families, 2, domain)
	olderControl := sendRelease(t, peer, families, 2, older)
	fencedRecall(t, r, []int{0}, []guestbridge.Capability{domain}, []uint64{2}, []uint64{2})

	// "Foreground first": B acquires after REOPEN, before A's retry.
	newer := acquireDomainLease(t, peer, families, 3, domain)
	if newer.Fence != 3 || newer.ID == older.ID {
		t.Fatalf("newer lease %+v did not open a new lineage after the recall", newer)
	}
	// The bug answered WOULD_BLOCK here at once; the guest's retries then ran
	// into its 30 s exchange timeout. One query must now answer RELEASED.
	queryRelease(t, peer, olderControl, "RELEASED")

	// B's lease kept its authority: releasing it starts its own recall.
	newerControl := sendRelease(t, peer, families, 3, newer)
	fencedRecall(t, r, []int{0}, []guestbridge.Capability{domain}, []uint64{3}, []uint64{3})
	queryRelease(t, peer, newerControl, "RELEASED")
}

// sendRelease sends one preowned VIEW_RELEASE of an active lease with control
// ticket number and requires PENDING (it starts the lease's physical recall).
func sendRelease(t *testing.T, peer *sourceRig, families viewcontrol.TicketFamilies, number uint64, lease guestbridge.ViewLease) guestbridge.Ticket {
	t.Helper()
	control := guestbridge.Ticket{Anchor: families.Control.Anchor, Nonce: families.Control.Nonce, Number: number, Credentials: peer.credentials}
	payload, _, _, _, err := peer.mappedExecute("VIEW_RELEASE", map[string]any{"request": control, "lease": lease})
	require(t, err)
	if !bytes.Contains(payload, []byte(`"status":"PENDING"`)) {
		t.Fatalf("VIEW_RELEASE of an active lease: %s", payload)
	}
	return control
}

// queryRelease polls the release ticket once, the way the guest's parked
// exchange resumes it, and requires status.
func queryRelease(t *testing.T, peer *sourceRig, control guestbridge.Ticket, status string) {
	t.Helper()
	payload := (&r5Chain{peer: peer}).exec(t, "VIEW_QUERY", map[string]any{"request": control})
	if !bytes.Contains(payload, []byte(`"status":"`+status+`"`)) {
		t.Fatalf("release query, want %s: %s", status, payload)
	}
}
