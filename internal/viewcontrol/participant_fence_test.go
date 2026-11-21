package viewcontrol

import (
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Stalled recall participants (task 0a4, participant_fence.go).

// TestStalledParticipantFencedAfterBound is zq3-r11 R11-2: m1 acknowledges
// STOPPED and then never acknowledges again. Before 0a4 the recall waited
// for m1's INSTALLED_DRAIN forever and held m0's parked OPEN behind it. m1
// has nothing undrained and no active lease, so StalledParticipantBound
// after INSTALL_DRAIN began m0's next poll fences m1, the recall completes
// with m0 alone, only m0's fence advances, the OPEN reaches TERMINAL and
// every later request of m1 is refused with an Unavailable error.
func TestStalledParticipantFencedAfterBound(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	r.ack(0, "INSTALLED_DRAIN", nil)
	advance(StalledParticipantBound - time.Millisecond)
	r.poll(0)
	r.requirePhase("INSTALL_DRAIN")
	r.requireNotFenced(1)
	advance(2 * time.Millisecond)
	r.poll(0)
	r.requireFenced(1)
	r.requirePhase("WAIT_DRAINED")
	r.ack(0, "DRAINED", r.emptyManifest())
	r.finishAfterFence(open)
	r.requireFenceRecorded(1, "INSTALL_DRAIN")
	r.requireRefusedAfterFence(1)
	r.requireNoEventAfterFence(1)
}

// TestBlockedACKKeepsParticipantAlive: a participant resending an ACK that
// stays BLOCKED (its observation has not arrived) is alive and is not
// fenced while that ACK is within BlockedACKBound, even past
// StalledParticipantBound. Once its wait is older than the bound (the
// guest stopped resending, or got the bound's error), it is fenced.
func TestBlockedACKKeepsParticipantAlive(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	r.ack(0, "INSTALLED_DRAIN", nil)
	advance(StalledParticipantBound - time.Second)
	installed := r.unobservedACK(1, "INSTALLED_DRAIN", nil)
	if got := r.ackStatus(1, installed); got != "BLOCKED" {
		t.Fatalf("unobserved INSTALLED_DRAIN answered %s, want BLOCKED", got)
	}
	advance(2 * time.Second) // past the stall bound, m1's wait 1 s old
	r.poll(0)
	r.requireNotFenced(1)
	advance(BlockedACKBound) // m1's wait is past its own bound
	r.poll(0)
	r.requireFenced(1)
	r.ack(0, "DRAINED", r.emptyManifest())
	r.finishAfterFence(open)
}

// TestUndrainedParticipantNeverFenced: m0 froze a dirty reservation and
// stopped; the recall waits in DRAIN for its DRAIN_WRITE. When the learned
// ACKs are discarded (as a Disconnect or a controller reopen does), both
// participants owe their STOPPED again and neither resends it. m1 has
// nothing undrained and is fenced after the bound; m0 holds an undrained
// entry and is never fenced, however long it stays silent. When m0 resends
// its STOPPED and drains, the recall completes with m0 alone and the drained
// data is in the file.
func TestUndrainedParticipantNeverFenced(t *testing.T) {
	r := newRig(t)
	advance := r.testClock()
	frozen := r.freezeDirtyRecall()
	advance(10 * StalledParticipantBound)
	r.poll(0)
	r.poll(1)
	r.requireNotFenced(0)
	r.requireNotFenced(1) // nobody owes an ACK in DRAIN: only the drain is due
	r.controller.mu.Lock()
	r.controller.state.Recall.Acks = map[string]json.RawMessage{}
	r.controller.phaseClock = phaseClock{} // as Disconnect/Recover/reopen do
	r.controller.mu.Unlock()
	r.poll(0) // starts the stall clock again
	advance(StalledParticipantBound)
	r.poll(0)
	r.requireFenced(1)
	r.requireNotFenced(0)
	advance(10 * StalledParticipantBound)
	r.poll(0)
	r.requireNotFenced(0)
	r.requirePhase("DRAIN")
	r.ack(0, "STOPPED", frozen.manifest) // the archived ACK is relearned
	drained := r.sendCall(0, frozen.drain)
	if drained.Code != "OK" || drained.Count != 3 {
		t.Fatalf("drain %+v", drained)
	}
	r.poll(0)
	r.requirePhase("INSTALL_DRAIN")
	r.ack(0, "INSTALLED_DRAIN", nil)
	r.ack(0, "DRAINED", frozen.manifest)
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
	r.requireFences(2, 1)
	read := r.call(0, "READ", map[string]any{"description": frozen.description, "offset": 0, "count": 64})
	if read.Code != "OK" || read.Data != "58595a64" {
		t.Fatalf("drained data %+v", read)
	}
}

// TestParticipantFenceReplayAfterCrash crashes the fence at both points.
// Before the fence is protected nothing changes: the reopened ledger still
// holds m1 in the recall, and the stall clock starts again, so m1 gets a
// fresh bound. After the fence is protected the reopened controller reloads
// it: m1 is out of the recall and refused, and m0 completes the recall.
// Each reopen discards the learned ACKs, so m0 resends its INSTALLED_DRAIN
// at once (relearned from the AckArchive), as a guest must after RESUME;
// a participant that does not resend owes the phase's ACK again and would
// be fenced after the bound too.
func TestParticipantFenceReplayAfterCrash(t *testing.T) {
	var armed atomic.Value
	armed.Store("")
	crash := errors.New("crash")
	r := newRig(t, func(c *Config) {
		c.Authorization.Enrollment.Bounds = zq3SpecBounds(t)
		c.Failpoint = func(name string) error {
			if name == armed.Load().(string) {
				return crash
			}
			return nil
		}
	})
	advance := r.testClock()
	r.startEarlyDrainRecall()
	installed := r.ack(0, "INSTALLED_DRAIN", nil)
	advance(StalledParticipantBound + time.Millisecond)
	r.crashFence(&armed, "participant-fence")
	r.requireNotFenced(1)
	r.reopen()
	r.requireNotFenced(1)
	advance = r.testClock()
	r.directACK(0, installed)
	r.directPoll(0) // a fresh stall clock: no fence yet
	r.requireNotFenced(1)
	advance(StalledParticipantBound + time.Millisecond)
	r.crashFence(&armed, "participant-fenced-protected")
	r.reopen()
	r.requireFenced(1)
	r.requirePhase("INSTALL_DRAIN")
	if _, err := r.tryExchange(1, "VIEW_POLL", map[string]any{"domain": r.domains[1], "after_sequence": 0}); !errors.Is(err, ErrSessionFenced) {
		t.Fatalf("fenced session's poll after reopen: %v, want ErrSessionFenced", err)
	}
	r.directACK(0, installed)
	r.requirePhase("WAIT_DRAINED")
	r.directACK(0, r.ackPayload(0, "DRAINED", r.emptyManifest()))
	r.directPoll(0)
	r.requirePhase("INSTALL_FINAL")
	r.directACK(0, r.ackPayload(0, "INSTALLED_FINAL", nil))
	r.directACK(0, r.ackPayload(0, "REOPENED", nil))
	r.requireFences(2, 1)
}

// frozenRecall is freezeDirtyRecall's result: m0's frozen manifest, the
// exact DRAIN_WRITE that drains it and the file's description.
type frozenRecall struct {
	manifest    json.RawMessage
	drain       guestbridge.Call
	description guestbridge.Capability
}

// freezeDirtyRecall is TestRealLocalTwoDomainFreezeDrainAppendAndRecall's
// start: m0 dirties a page of a file holding "abcd", m1's conflicting READ
// starts an all-domain recall, m0 freezes "XYZ" for offset 0 and both
// domains STOP, so the recall waits in DRAIN for m0's DRAIN_WRITE.
func (r *rig) freezeDirtyRecall() frozenRecall {
	t := r.t
	t.Helper()
	_, description := r.create(0, "fence-mmap", true)
	if w := r.call(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61626364", "max_end": 64}); w.Code != "OK" {
		t.Fatalf("write %+v", w)
	}
	other := r.open(1, "fence-mmap")
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	r.next[0]++
	number := guestbridge.ClaimNumber{Realm: uid(100).String(), Subject: "616263", Anchor: uid(101).String(), Nonce: uid(103).String(), Number: r.next[0]}
	reserved := r.view(0, "DIRTY_RESERVE", map[string]any{"request": r.ticket(0), "lease": dirty.Lease, "description": description, "offset": 0, "length": 4096, "claim_number": number})
	if reserved.Status != "GRANTED" {
		t.Fatalf("reserve %+v", reserved)
	}
	if _, pending := r.acquire(1, page(other, 0), "READ"); pending.Status != "PENDING" {
		t.Fatalf("conflict %+v", pending)
	}
	recall := r.controller.state.Recall.ID
	entry := map[string]any{"description": description, "offset": 0, "length": 3, "payload_hash": hash([]byte("XYZ")), "reservation": reserved.Reservation.ID}
	entry["id"] = digest(entry)
	args, _ := canonical(map[string]any{"description": description, "offset": 0, "data": "58595a", "recall": recall, "dirty_entry": entry["id"], "reservation": reserved.Reservation.ID})
	intent := guestbridge.Intent{Opcode: "DRAIN_WRITE", Args: args}
	claim := guestbridge.Claim{Realm: number.Realm, Subject: number.Subject, Anchor: number.Anchor, Nonce: number.Nonce, Number: number.Number, OriginalHash: digest(intent)}
	frozen := r.view(0, "DIRTY_FREEZE", map[string]any{"request": r.ticket(0), "reservation": *reserved.Reservation, "recall": recall, "data": "58595a", "claim": claim})
	if frozen.Status != "FROZEN" {
		t.Fatalf("freeze %+v", frozen)
	}
	manifest, _ := canonical(map[string]any{"entries": []json.RawMessage{frozen.Entry}, "hash": digest([]json.RawMessage{frozen.Entry})})
	r.ack(0, "STOPPED", manifest)
	r.ack(1, "STOPPED", r.emptyManifest())
	r.requirePhase("DRAIN")
	return frozenRecall{manifest: manifest, drain: guestbridge.Call{Claim: claim, Credentials: r.credentials[0], Intent: intent}, description: description}
}

// crashFence arms failpoint name, requires m0's poll to fail on it (the
// crash) and disarms it.
func (r *rig) crashFence(armed *atomic.Value, name string) {
	r.t.Helper()
	armed.Store(name)
	if _, err := r.tryExchange(0, "VIEW_POLL", map[string]any{"domain": r.domains[0], "after_sequence": 0}); err == nil {
		r.t.Fatalf("poll survived failpoint %s", name)
	}
	armed.Store("")
}

// tryExchange sends a recall op straight to the (possibly reopened)
// controller and returns its error: the rig's bridges keep pointing at the
// closed one after reopen.
func (r *rig) tryExchange(i int, op string, payload any) (json.RawMessage, error) {
	r.t.Helper()
	raw, err := canonical(payload)
	must(r.t, err)
	return r.controller.Exchange(r.t.Context(), r.scopes[i], op, raw)
}

// directPoll polls domain i directly and requires a reply.
func (r *rig) directPoll(i int) {
	r.t.Helper()
	_, err := r.tryExchange(i, "VIEW_POLL", map[string]any{"domain": r.domains[i], "after_sequence": 0})
	must(r.t, err)
}

// directACK sends domain i's exact ACK directly and requires ACCEPTED.
func (r *rig) directACK(i int, raw json.RawMessage) {
	r.t.Helper()
	reply, err := r.tryExchange(i, "VIEW_ACK", raw)
	must(r.t, err)
	if status(reply) != "ACCEPTED" {
		r.t.Fatalf("direct ACK %s: %s", raw, reply)
	}
}

// requireFenced requires domain i fenced in the ledger and out of the recall.
func (r *rig) requireFenced(i int) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	session := r.scopes[i].Session
	f, ok := r.controller.state.Fenced[session]
	if !ok {
		r.t.Fatalf("m%d not fenced (recall %+v)", i, r.controller.state.Recall)
	}
	if f.Fence != r.controller.fence(session) {
		r.t.Fatalf("m%d fence moved after its fence: %+v, now %d", i, f, r.controller.fence(session))
	}
	if rc := r.controller.state.Recall; rc != nil && len(rc.Events[session]) != 0 {
		r.t.Fatalf("fenced m%d still in the recall", i)
	}
	if _, ok := r.controller.fencedSessions.Load(session); !ok {
		r.t.Fatalf("fenced m%d missing from the ingress gate", i)
	}
}

// requireNotFenced requires domain i unfenced.
func (r *rig) requireNotFenced(i int) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if _, ok := r.controller.state.Fenced[r.scopes[i].Session]; ok {
		r.t.Fatalf("m%d fenced", i)
	}
}

// requireFences requires no recall and the two domains' fences.
func (r *rig) requireFences(m0, m1 uint64) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if r.controller.state.Recall != nil {
		r.t.Fatalf("recall retained: %+v", r.controller.state.Recall)
	}
	if got := []uint64{r.controller.fence(r.scopes[0].Session), r.controller.fence(r.scopes[1].Session)}; got[0] != m0 || got[1] != m1 {
		r.t.Fatalf("fences %v, want [%d %d]", got, m0, m1)
	}
}

// finishAfterFence completes the recall with m0 alone from WAIT_DRAINED
// (m0's DRAINED already sent) and requires the parked OPEN TERMINAL.
func (r *rig) finishAfterFence(open guestbridge.Call) {
	r.t.Helper()
	r.poll(0)
	r.requirePhase("INSTALL_FINAL")
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
	r.requireFences(2, 1)
	if status := r.queryStatus(0, open); status != "TERMINAL" {
		r.t.Fatalf("parked OPEN after the fenced recall: %s", status)
	}
}

// requireFenceRecorded requires the recall history's newest record to be
// COMPLETED with exactly one fence row for domain i in phase, and the
// ledger evidence to count one fenced session.
func (r *rig) requireFenceRecorded(i int, phase string) {
	r.t.Helper()
	h, err := r.controller.History(r.t.Context())
	must(r.t, err)
	last := h.Recalls[len(h.Recalls)-1]
	if last.Outcome != RecallCompleted || len(last.Fenced) != 1 || last.Fenced[0].Domain != r.scopes[i].Session || last.Fenced[0].Phase != phase {
		r.t.Fatalf("history record %+v", last)
	}
	evidence, err := r.controller.LedgerEvidence(r.t.Context())
	must(r.t, err)
	if evidence.Fenced != 1 || evidence.Recall {
		r.t.Fatalf("evidence %+v", evidence)
	}
}

// requireRefusedAfterFence requires every kind of request of domain i to
// get an Unavailable error: its poll, a VIEW_ACQUIRE and a CALL (its ACKs
// are refused by the same CheckRequest gate, and under the lock by
// acknowledge).
func (r *rig) requireRefusedAfterFence(i int) {
	r.t.Helper()
	call := r.originalCall(i, "PROCESS_EXIT", map[string]any{"process": r.credentials[i].Process, "exit_sequence": 1})
	requests := []struct {
		op      string
		payload any
	}{
		{"VIEW_POLL", map[string]any{"domain": r.domains[i], "after_sequence": 0}},
		{"VIEW_ACQUIRE", map[string]any{"request": r.ticket(i), "domain": r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}},
		{"CALL", call},
	}
	for _, q := range requests {
		if reply, err := r.execute(i, q.op, q.payload); !errors.Is(err, fsapi.Unavailable) {
			r.t.Fatalf("fenced m%d %s: %s %v, want an Unavailable error", i, q.op, reply, err)
		}
	}
}

// requireNoEventAfterFence starts a new all-domain recall and requires it to
// issue its event to the other domain only.
func (r *rig) requireNoEventAfterFence(i int) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	must(r.t, r.controller.startRecall(r.t.Context(), recallTrigger{Kind: TriggerCallConflict}))
	events := r.controller.state.Recall.Events
	if _, ok := events[r.scopes[i].Session]; ok || len(events) != 1 {
		r.t.Fatalf("recall after the fence issued events to %d sessions, fenced m%d included: %v", len(events), i, ok)
	}
}
