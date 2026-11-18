package viewcontrol

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestEarlyDrainedIsBlockedNotStale is zq3-r11's R11-1 (task y94). The guest
// (patch 0004 hfs_mmap_process/hfs_mmap_drained) sends DRAINED as soon as its
// own INSTALLED_DRAIN is ACCEPTED. In an all-domain recall that DRAINED
// arrives while the recall still waits in INSTALL_DRAIN for the peer's
// INSTALLED_DRAIN. The host used to answer it STALE; the guest maps STALE to
// -ESTALE, stops its view worker for good and blocks its session, and the
// recall then waited for that domain's DRAINED forever, holding a parked
// OPEN behind it. The spec requires BLOCKED for an ACK that may become
// acceptable unchanged: the identical bytes must be ACCEPTED once the recall
// reaches WAIT_DRAINED, the recall must complete and the OPEN must reach
// TERMINAL.
func TestEarlyDrainedIsBlockedNotStale(t *testing.T) {
	r := newZQ3Rig(t, 0)
	open := r.startEarlyDrainRecall()
	// m1 has not installed: its DRAINED cannot become acceptable unchanged
	// before its own INSTALLED_DRAIN is accepted, so it stays STALE.
	if got := r.ackStatus(1, r.ackPayload(1, "DRAINED", r.emptyManifest())); got != "STALE" {
		t.Fatalf("DRAINED without its own INSTALLED_DRAIN answered %s, want STALE", got)
	}
	r.ack(0, "INSTALLED_DRAIN", nil)
	early := r.requireEarlyDrainedBlocked(open)
	r.ack(1, "INSTALLED_DRAIN", nil)
	r.ack(1, "DRAINED", r.emptyManifest())
	r.finishEarlyDrainRecall(early, open)
}

// TestEarlyDrainedBlockedAfterLearnedACKsDiscarded: a Disconnect or a
// controller restart (New) discards the recall's learned ACKs, but m0's
// INSTALLED_DRAIN stays in the protected AckArchive and the guest, which saw
// it ACCEPTED, goes on to its DRAINED. That DRAINED is still BLOCKED, not
// STALE; m0's INSTALLED_DRAIN resend relearns the ACK (replayACK) and the
// recall completes. The rig's bridges cannot follow a reopened controller,
// so the discard is applied to the ledger directly, exactly as both do it.
func TestEarlyDrainedBlockedAfterLearnedACKsDiscarded(t *testing.T) {
	r := newZQ3Rig(t, 0)
	open := r.startEarlyDrainRecall()
	installed := r.ack(0, "INSTALLED_DRAIN", nil)
	r.controller.mu.Lock()
	r.controller.state.Recall.Acks = map[string]json.RawMessage{}
	r.controller.mu.Unlock()
	early := r.requireEarlyDrainedBlocked(open)
	if got := r.ackStatus(0, installed); got != "ACCEPTED" {
		t.Fatalf("INSTALLED_DRAIN resend after restart answered %s, want ACCEPTED", got)
	}
	r.ack(1, "INSTALLED_DRAIN", nil)
	r.ack(1, "DRAINED", r.emptyManifest())
	r.finishEarlyDrainRecall(early, open)
}

// startEarlyDrainRecall starts an all-domain recall of both domains, parks
// r11's OPEN behind it (guest.py's read-only open of m0's root) and drives
// both STOPPED ACKs so the recall waits in INSTALL_DRAIN.
//
// The recall is started by a clean READ release of a domain that also holds
// an EXCLUSIVE_DIRTY lease (as TestExclusiveDomainReadReleaseStaysGlobal),
// not by r11's own trigger (a read-only OPEN conflicting with its caller's
// observation lease, task z94), so these tests do not depend on that
// admission rule.
func (r *rig) startEarlyDrainRecall() guestbridge.Call {
	t := r.t
	t.Helper()
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	_, description := r.create(1, "exclusive", false)
	r.pageRead(0)
	_, read := r.acquire(1, page(description, 4096), "READ")
	_, write := r.acquire(1, page(description, 0), "EXCLUSIVE_DIRTY")
	if read.Status != "GRANTED" || write.Status != "GRANTED" {
		t.Fatalf("leases %+v %+v", read, write)
	}
	r.startRelease(1, read.Lease)
	if scope, participants, exists := r.recallSnapshot(); !exists || scope != "" || len(participants) != 2 {
		t.Fatalf("recall scope %q participants %v exists %v", scope, participants, exists)
	}
	open, status := r.pendingCall(0, "OPEN", map[string]any{"object": root, "flags": map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}})
	if status != "INDETERMINATE" {
		t.Fatalf("OPEN during an all-domain recall: %s", status)
	}
	r.ack(0, "STOPPED", r.emptyManifest())
	r.ack(1, "STOPPED", r.emptyManifest())
	r.poll(0)
	r.requirePhase("INSTALL_DRAIN")
	return open
}

// requireEarlyDrainedBlocked sends m0's DRAINED while the recall still waits
// in INSTALL_DRAIN for m1, as the guest does right after its own
// INSTALLED_DRAIN, and requires BLOCKED for it and its resends with nothing
// advanced. It returns the exact DRAINED bytes for the final resend.
func (r *rig) requireEarlyDrainedBlocked(open guestbridge.Call) json.RawMessage {
	r.t.Helper()
	early := r.ackPayload(0, "DRAINED", r.emptyManifest())
	for range 3 { // the guest resends a BLOCKED ACK every 10 ms
		if got := r.ackStatus(0, early); got != "BLOCKED" {
			r.t.Fatalf("early DRAINED answered %s, want BLOCKED", got)
		}
	}
	r.requirePhase("INSTALL_DRAIN")
	if r.queryStatus(0, open) != "INDETERMINATE" {
		r.t.Fatal("OPEN admitted before the recall reopened")
	}
	return early
}

// finishEarlyDrainRecall resends m0's early DRAINED once m1's INSTALLED_DRAIN
// moved the recall to WAIT_DRAINED, requires ACCEPTED, completes the recall
// and requires the parked OPEN to reach TERMINAL.
func (r *rig) finishEarlyDrainRecall(early json.RawMessage, open guestbridge.Call) {
	r.t.Helper()
	r.requirePhase("WAIT_DRAINED")
	if got := r.ackStatus(0, early); got != "ACCEPTED" {
		r.t.Fatalf("resent early DRAINED answered %s, want ACCEPTED", got)
	}
	r.poll(0)
	r.requirePhase("INSTALL_FINAL")
	for i := range 2 {
		r.ack(i, "INSTALLED_FINAL", nil)
	}
	for i := range 2 {
		r.ack(i, "REOPENED", nil)
	}
	if _, _, exists := r.recallSnapshot(); exists {
		r.t.Fatal("recall retained after both REOPENED")
	}
	if status := r.queryStatus(0, open); status != "TERMINAL" {
		r.t.Fatalf("parked OPEN after the recall: %s", status)
	}
}

// requirePhase fails unless the current recall is in phase.
func (r *rig) requirePhase(phase string) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if r.controller.state.Recall == nil || r.controller.state.Recall.Phase != phase {
		r.t.Fatalf("recall %+v, want phase %s", r.controller.state.Recall, phase)
	}
}
