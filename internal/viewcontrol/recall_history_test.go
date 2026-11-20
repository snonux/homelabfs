package viewcontrol

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// recallPhases is the order in which a complete recall enters its phases.
var recallPhases = []string{"RECALL", "DRAIN", "INSTALL_DRAIN", "WAIT_DRAINED", "INSTALL_FINAL", "REOPEN"}

// steppingClock makes the controller clock advance 1 ms per reading, so
// every recorded offset and duration is positive and ordered.
func steppingClock(r *rig) {
	clock := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	r.controller.mu.Lock()
	r.controller.now = func() time.Time { clock = clock.Add(time.Millisecond); return clock }
	r.controller.mu.Unlock()
}

func history(t *testing.T, c *Controller) RecallHistory {
	t.Helper()
	h, err := c.History(t.Context())
	must(t, err)
	return h
}

// TestRecallHistoryRecordsFullRecall drives one scoped release recall through
// every phase, with a STOPPED ACK BLOCKED twice on its physical observation,
// and checks the recorded trigger, scope, phase starts and ACK rows.
func TestRecallHistoryRecordsFullRecall(t *testing.T) {
	r := newRig(t)
	steppingClock(r)
	stopped := r.startReleaseRecall()
	scope, participants, _ := r.recallSnapshot()
	for range 2 {
		if reply := r.directExchange(0, "VIEW_ACK", json.RawMessage(stopped)); status(reply) != "BLOCKED" {
			t.Fatalf("unobserved STOPPED %s", reply)
		}
	}
	r.physical("VIEW_ACK", json.RawMessage(stopped))
	for range 2 { // the second is an exact replay, recorded once
		if reply := r.directExchange(0, "VIEW_ACK", json.RawMessage(stopped)); status(reply) != "ACCEPTED" {
			t.Fatalf("observed STOPPED %s", reply)
		}
	}
	finishAcks(r, participants)
	h := history(t, r.controller)
	if h.Capacity != RecallHistoryLimit || h.Total != 1 || h.Omitted != 0 || len(h.Recalls) != 1 {
		t.Fatalf("history %+v", h)
	}
	record := h.Recalls[0]
	wantScope := "ALL"
	if scope != "" {
		wantScope = "DOMAIN"
	}
	if record.Kind != TriggerRelease || record.Opcode != "VIEW_RELEASE" || record.Domain != r.scopes[0].Session || record.Scope != wantScope || len(record.Participants) != len(participants) {
		t.Fatalf("trigger/scope %+v (scope %q, participants %v)", record, scope, participants)
	}
	if record.Outcome != RecallCompleted || !strings.HasSuffix(record.StartedUTC, "Z") {
		t.Fatalf("outcome/start %+v", record)
	}
	if _, err := time.Parse(time.RFC3339Nano, record.StartedUTC); err != nil {
		t.Fatal(err)
	}
	requirePhases(t, record)
	requireAcks(t, record, len(participants))
}

// finishAcks acknowledges every phase of the current recall for its
// participants, except domain 0's STOPPED that the caller already sent,
// polling first so the recall advances to the phase's event.
func finishAcks(r *rig, participants []int) {
	r.t.Helper()
	for _, phase := range []string{"STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		for _, i := range participants {
			if i == 0 && phase == "STOPPED" {
				continue
			}
			r.directExchange(i, "VIEW_POLL", map[string]any{"domain": r.domains[i], "after_sequence": 0})
			if reply := r.directExchange(i, "VIEW_ACK", r.ackPayload(i, phase, r.emptyManifest())); status(reply) != "ACCEPTED" {
				r.t.Fatalf("%s ACK %d: %s", phase, i, reply)
			}
		}
	}
	if _, _, exists := r.recallSnapshot(); exists {
		r.t.Fatal("recall did not close")
	}
}

// requirePhases checks every phase start in order, with positive event
// issue times for the phases that issue events, and the end after them.
func requirePhases(t *testing.T, record RecallRecord) {
	t.Helper()
	if len(record.Phases) != len(recallPhases) {
		t.Fatalf("phases %+v", record.Phases)
	}
	last := int64(-1)
	for i, p := range record.Phases {
		issues := p.Phase == "RECALL" || p.Phase == "INSTALL_DRAIN" || p.Phase == "INSTALL_FINAL" || p.Phase == "REOPEN"
		if p.Phase != recallPhases[i] || p.OffsetMicros < last || issues != (p.IssueMicros > 0) {
			t.Fatalf("phase %d %+v after %d", i, p, last)
		}
		last = p.OffsetMicros
	}
	if record.EndMicros <= last {
		t.Fatalf("end %d before last phase %d", record.EndMicros, last)
	}
}

// requireAcks checks one row per participant and ACK phase, without the
// replayed STOPPED, and the BLOCKED wait of domain 0's STOPPED.
func requireAcks(t *testing.T, record RecallRecord, participants int) {
	t.Helper()
	if len(record.Acks) != 5*participants || record.AcksDropped != 0 {
		t.Fatalf("acks %+v", record.Acks)
	}
	first := record.Acks[0]
	if first.Phase != "STOPPED" || first.Domain != record.Domain || first.Blocked != 2 || first.BlockedMicros <= 0 || first.CheckMicros <= 0 {
		t.Fatalf("blocked STOPPED row %+v", first)
	}
	seen := map[string]bool{}
	for _, a := range record.Acks {
		key := a.Domain + "/" + a.Phase
		if seen[key] || a.OffsetMicros <= 0 || a.CheckMicros <= 0 || a.Sequence == 0 {
			t.Fatalf("ack row %+v", a)
		}
		seen[key] = true
		if a != first && a.Blocked != 0 {
			t.Fatalf("unblocked ACK has BLOCKED history %+v", a)
		}
	}
}

// TestRecallHistorySurvivesPruning runs read cycles whose tickets are retired
// and recycled (ACK archive pruning, compaction) and a dirty all-domain round:
// the ledger forgets every finished recall, the history keeps each one.
func TestRecallHistorySurvivesPruning(t *testing.T) {
	r := newZQ3Rig(t, 0)
	x := newRecycleRun(r)
	for n := range 6 {
		x.readCycle(n % 2)
	}
	x.dirtyRound()
	r.controller.mu.Lock()
	archived, recall := len(r.controller.state.AckArchive), r.controller.state.Recall
	r.controller.mu.Unlock()
	if recall != nil || archived >= 5*x.recalls {
		t.Fatalf("ledger kept %d archived ACKs of %d recalls (recall %v): nothing was pruned", archived, x.recalls, recall != nil)
	}
	h := history(t, r.controller)
	if h.Total != uint64(x.recalls) || len(h.Recalls)+h.Omitted != x.recalls {
		t.Fatalf("history total %d, %d records + %d omitted, want %d recalls", h.Total, len(h.Recalls), h.Omitted, x.recalls)
	}
	all := 0
	for _, record := range h.Recalls {
		if record.Outcome != RecallCompleted || len(record.Phases) != len(recallPhases) || len(record.Acks) != 5*len(record.Participants) {
			t.Fatalf("pruned record %+v", record)
		}
		if record.Scope == "ALL" {
			all++
		}
	}
	if all == 0 {
		t.Fatalf("dirty round recorded no all-domain recall: %+v", h.Recalls)
	}
}

// fakeRecall begins recall n in h with one participant.
func fakeRecall(h *recallHistory, n int, start time.Time) string {
	id := "recall-" + strconv.Itoa(n)
	r := &recallState{ID: id, Phase: "RECALL", Events: map[string]json.RawMessage{"session-a": nil}}
	h.begin(r, recallTrigger{TriggerCallConflict, "MKDIR", "session-a"}, start, time.Millisecond)
	return id
}

// TestRecallHistoryRingBound checks the ring, ACK row and evidence byte
// bounds, replay de-duplication, ABANDONED marking and snapshot copies.
func TestRecallHistoryRingBound(t *testing.T) {
	var h recallHistory
	start := time.Now()
	for n := range RecallHistoryLimit + 6 {
		id := fakeRecall(&h, n, start)
		for a := range RecallAckLimit + 3 {
			h.acked(id, RecallAck{Domain: strings.Repeat("d", 36), Phase: "STOPPED", Sequence: uint64(a + 1)}, start.Add(time.Second))
		}
		h.acked(id, RecallAck{Domain: strings.Repeat("d", 36), Phase: "STOPPED", Sequence: 1}, start.Add(2*time.Second))
	}
	if len(h.records) != RecallHistoryLimit || h.total != RecallHistoryLimit+6 || h.records[0].ID != "recall-6" {
		t.Fatalf("ring %d records, total %d, oldest %s", len(h.records), h.total, h.records[0].ID)
	}
	for _, record := range h.records[:RecallHistoryLimit-1] {
		if record.Outcome != RecallAbandoned {
			t.Fatalf("superseded open recall %s is %s", record.ID, record.Outcome)
		}
	}
	newest := h.records[RecallHistoryLimit-1]
	if newest.Outcome != RecallOpen || len(newest.Acks) != RecallAckLimit || newest.AcksDropped != 3 || newest.Acks[0].OffsetMicros != 1e6 {
		t.Fatalf("newest record %+v", newest)
	}
	snap := h.snapshot()
	raw, err := json.Marshal(snap) // the complete evidence, wrapper included
	must(t, err)
	if snap.Omitted == 0 || len(raw) > RecallEvidenceBytes || len(snap.Recalls)+snap.Omitted != RecallHistoryLimit || snap.Recalls[len(snap.Recalls)-1].ID != newest.ID {
		t.Fatalf("snapshot %d records (%d B), %d omitted", len(snap.Recalls), len(raw), snap.Omitted)
	}
	// Exact: one more (older) record, with its omitted count, no longer fits.
	all := h.copyAll()
	more := all
	more.Omitted, more.Recalls = snap.Omitted-1, all.Recalls[snap.Omitted-1:]
	raw, err = json.Marshal(more)
	must(t, err)
	if len(raw) <= RecallEvidenceBytes {
		t.Fatalf("one more record still fits: %d B", len(raw))
	}
	snap.Recalls[len(snap.Recalls)-1].Acks[0].Sequence = 99
	if newest.Acks[0].Sequence == 99 {
		t.Fatal("snapshot shares ACK rows with the ring")
	}
}

// TestRecallHistoryEvidenceRoundTrip encodes and decodes a history: every
// exported field survives, and an empty one encodes its lists as [].
func TestRecallHistoryEvidenceRoundTrip(t *testing.T) {
	var h recallHistory
	empty, err := json.Marshal(h.snapshot())
	must(t, err)
	if string(empty) != `{"capacity":64,"total":0,"omitted":0,"recalls":[]}` {
		t.Fatalf("empty history %s", empty)
	}
	start := time.Date(2026, 11, 20, 1, 2, 3, 4000, time.UTC)
	id := fakeRecall(&h, 1, start)
	h.acked(id, RecallAck{Domain: "session-a", Phase: "STOPPED", Sequence: 4, Blocked: 3, BlockedMicros: 70, CheckMicros: 5}, start.Add(time.Millisecond))
	h.phase(id, "DRAIN", false, start.Add(2*time.Millisecond))
	h.phase(id, "", true, start.Add(3*time.Millisecond))
	want := h.snapshot()
	raw, err := json.Marshal(want)
	must(t, err)
	var got RecallHistory
	must(t, json.Unmarshal(raw, &got))
	want.Recalls[0].started = time.Time{}
	if !reflect.DeepEqual(got, want) || got.Recalls[0].StartedUTC != "2026-11-20T01:02:03.000004Z" || got.Recalls[0].EndMicros != 3000 {
		t.Fatalf("round trip\n got %+v\nwant %+v", got, want)
	}
}
