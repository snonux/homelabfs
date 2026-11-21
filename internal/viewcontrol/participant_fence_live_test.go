package viewcontrol

import (
	"encoding/json"
	"testing"
	"time"
)

// Liveness of recall participants after 0a4 review round 1: a participant
// that talks is never fenced, and a controller restart never makes a live
// guest look stalled.

// simulateRestart discards what a controller restart (or Disconnect and
// Recover) discards: the learned ACKs and every in-memory clock and bound.
func (r *rig) simulateRestart() {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	r.controller.state.Recall.Acks = map[string]json.RawMessage{}
	r.controller.resetLiveness()
	r.controller.blockedACKs = nil
}

// TestRestartDoesNotFenceResendingGuest is review round 1's reproduction.
// After a restart mid-INSTALL_DRAIN, m0 (alive) resends its early DRAINED
// and polls every second; patch 0004 does not resend its accepted
// INSTALLED_DRAIN. Before the fix m0 owed that ACK by the learned-only
// check, fenced the silent m1, got the DRAINED's bound error and then
// fenced itself through its own poll: the recall ended without
// participants. Now m0's INSTALLED_DRAIN counts as delivered (archived) and
// its requests keep it alive: only m1 is fenced. When m0 resends its
// INSTALLED_DRAIN (relearned from the archive) the recall completes.
func TestRestartDoesNotFenceResendingGuest(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	installed := r.ack(0, "INSTALLED_DRAIN", nil)
	drained := r.ackPayload(0, "DRAINED", r.emptyManifest())
	if got := r.ackStatus(0, drained); got != "BLOCKED" {
		t.Fatalf("early DRAINED %s", got)
	}
	r.simulateRestart()
	for second := range 200 {
		_, _ = r.execute(0, "VIEW_ACK", drained) // BLOCKED or the bound's error
		r.poll(0)
		r.requireNotFenced(0)
		if second == int(StalledParticipantBound/time.Second)-2 {
			r.requireNotFenced(1)
		}
		advance(time.Second)
	}
	r.requireFenced(1)
	if got := r.ackStatus(0, installed); got != "ACCEPTED" {
		t.Fatalf("INSTALLED_DRAIN resend %s", got)
	}
	r.requirePhase("WAIT_DRAINED")
	if got := r.ackStatus(0, drained); got != "ACCEPTED" {
		t.Fatalf("DRAINED resend %s", got)
	}
	r.finishAfterFence(open)
}

// TestRestartMidWaitDrainedPollingGuestsNotFenced: a restart in
// WAIT_DRAINED, where m0 has delivered everything and m1 still owes its
// DRAINED. Both keep polling for 200 s: nobody is fenced (m0 owes nothing
// by the archive, m1 talks). Then both resend and the recall completes
// with both domains.
func TestRestartMidWaitDrainedPollingGuestsNotFenced(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	installed0 := r.ack(0, "INSTALLED_DRAIN", nil)
	drained0 := r.ackPayload(0, "DRAINED", r.emptyManifest())
	if got := r.ackStatus(0, drained0); got != "BLOCKED" {
		t.Fatalf("early DRAINED %s", got)
	}
	installed1 := r.ack(1, "INSTALLED_DRAIN", nil)
	r.requirePhase("WAIT_DRAINED")
	if got := r.ackStatus(0, drained0); got != "ACCEPTED" {
		t.Fatalf("DRAINED in WAIT_DRAINED %s", got)
	}
	r.simulateRestart()
	for range 200 {
		r.poll(0)
		r.poll(1)
		advance(time.Second)
	}
	r.requireNotFenced(0)
	r.requireNotFenced(1)
	for i, raw := range []json.RawMessage{installed0, drained0, installed1} {
		if got := r.ackStatus(i/2, raw); got != "ACCEPTED" {
			t.Fatalf("resend %d after restart: %s", i, got)
		}
	}
	r.ack(1, "DRAINED", r.emptyManifest())
	r.poll(0)
	r.requirePhase("INSTALL_FINAL")
	for i := range 2 {
		r.ack(i, "INSTALLED_FINAL", nil)
	}
	for i := range 2 {
		r.ack(i, "REOPENED", nil)
	}
	r.requireFences(2, 2)
	if status := r.queryStatus(0, open); status != "TERMINAL" {
		t.Fatalf("parked OPEN: %s", status)
	}
}

// TestSlowInstallerPollingNotFenced: m1 polls every second but does not
// acknowledge INSTALL_DRAIN for 200 s (a slow install): it is not fenced.
// Once it goes completely silent it is fenced after exactly the bound.
func TestSlowInstallerPollingNotFenced(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	r.ack(0, "INSTALLED_DRAIN", nil)
	for range 200 {
		r.poll(1)
		r.poll(0)
		advance(time.Second)
	}
	r.requireNotFenced(1)
	advance(StalledParticipantBound - 2*time.Second) // m1 silent 59 s
	r.poll(0)
	r.requireNotFenced(1)
	advance(time.Second)
	r.poll(0)
	r.requireFenced(1)
	r.ack(0, "DRAINED", r.emptyManifest())
	r.finishAfterFence(open)
}

// TestAllParticipantsSilentFenced: both domains stop after STOPPED. m0 was
// heard 2 s before the check, so the first fence takes only m1 and keeps
// the recall. When m0 too is silent for the bound, the guard lets the fence
// empty the recall: it ends, no fence advances, and the ledger moves to
// LedgerSchemaFenced, which a reopen accepts. A fence of the caller itself,
// or one that would empty the recall while a participant still talks, is
// refused by fenceGuard.
func TestAllParticipantsSilentFenced(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	r.startEarlyDrainRecall()
	advance(StalledParticipantBound - 2*time.Second)
	r.poll(0)
	advance(2 * time.Second)
	r.fenceDirect()
	r.requireFenced(1)
	r.requireNotFenced(0)
	r.requirePhase("INSTALL_DRAIN")
	r.controller.mu.Lock()
	recall, m0 := r.controller.state.Recall, r.scopes[0].Session
	if r.controller.fenceGuard(recall, []string{m0}, m0) || r.controller.fenceGuard(recall, []string{m0}, "") {
		t.Fatal("guard lets a fence empty the recall of a participant heard 2 s ago")
	}
	r.controller.mu.Unlock()
	advance(StalledParticipantBound)
	r.fenceDirect()
	r.requireFenced(0)
	r.requireFences(1, 1)
	r.controller.mu.Lock()
	schema := r.controller.state.Schema
	r.controller.mu.Unlock()
	if schema != LedgerSchemaFenced {
		t.Fatalf("schema %d after a fence, want %d", schema, LedgerSchemaFenced)
	}
	r.reopen()
	r.requireFenced(0)
	r.requireFenced(1)
}

// fenceDirect runs the fence check as a native caller (no session).
func (r *rig) fenceDirect() {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	_, err := r.controller.fenceStalled(r.t.Context(), "")
	must(r.t, err)
}

// TestValidLedgerSchema: schema 2 without a fence, schema 3 with one; an
// old binary accepts only 2, so it refuses every fenced ledger.
func TestValidLedgerSchema(t *testing.T) {
	for _, c := range []struct {
		schema, fenced int
		want           bool
	}{{LedgerSchema, 0, true}, {LedgerSchemaFenced, 1, true}, {LedgerSchema, 1, false}, {LedgerSchemaFenced, 0, false}, {1, 0, false}} {
		if got := ValidLedgerSchema(c.schema, c.fenced); got != c.want {
			t.Errorf("ValidLedgerSchema(%d, %d) = %v", c.schema, c.fenced, got)
		}
	}
}
