//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"time"
)

// Scheduler of the recall workload model (recall_workload_test.go): the
// guest's QUERY retry loops and its view event worker, interleaved on the
// host's one control worker.

// modelDeadline bounds the whole model; a stuck recall fails here instead of
// at the package timeout.
const modelDeadline = 5 * time.Minute

// waitStart is when a guest wait began: wall time (its 30 s window) and the
// slow observation count, so a wait splits into observation latency and
// other host work.
type waitStart struct {
	at    time.Time
	probe int32
}

// startWait marks the start of a guest wait, just before its first send.
func (m *recallModel) startWait() waitStart {
	return waitStart{at: time.Now(), probe: m.probes.Load()}
}

// finish ends w now: its latency and the observations paid meanwhile.
func (m *recallModel) finish(w *guestWait) {
	w.done, w.latency = true, time.Since(w.start.at)
	w.observations = m.probes.Load() - w.start.probe
}

// track records a wait's first reply and keeps it pending unless terminal.
func (m *recallModel) track(w *guestWait, first json.RawMessage) {
	m.phase.waits = append(m.phase.waits, w)
	m.answer(w, first)
	if !w.done {
		m.pending = append(m.pending, w)
	}
}

// answer applies one reply to w: a terminal status ends the wait.
func (m *recallModel) answer(w *guestWait, payload json.RawMessage) {
	var reply struct{ Status string }
	require(m.t, json.Unmarshal(payload, &reply))
	w.payload, w.status = payload, reply.Status
	if w.terminal(reply.Status) {
		m.finish(w)
		if w.latency > w.phase.worst {
			w.phase.worst, w.phase.worstLabel = w.latency, w.label
		}
	}
}

// step is one scheduler round: one QUERY per pending wait, then the event
// worker's ACKs. A wait past the guest window expires and is dropped: the
// guest has blocked its session, so nothing sends it again (and a release
// dropped this way never starts its recall). An idle round sleeps the
// guest's 10 ms retry interval.
func (m *recallModel) step() {
	for _, w := range m.pending {
		if !w.done {
			m.answer(w, w.query())
		}
	}
	progressed := m.eventWorker()
	still := m.pending[:0]
	for _, w := range m.pending {
		if !w.done && time.Since(w.start.at) > guestQueryWindow {
			m.finish(w)
			w.expired = true
			m.t.Logf("EXPIRED after %s: %s (last reply %s)", w.latency.Round(time.Millisecond), w.label, w.payload)
		}
		if !w.done {
			still = append(still, w)
		}
	}
	m.pending = still
	if !progressed {
		time.Sleep(10 * time.Millisecond)
	}
}

// await runs the scheduler until w is done.
func (m *recallModel) await(w *guestWait) {
	deadline := time.Now().Add(modelDeadline)
	for !w.done {
		if time.Now().After(deadline) {
			m.t.Fatalf("model deadline waiting for %s", w.label)
		}
		m.step()
	}
}

// settle runs the scheduler until no wait is pending and no recall exists.
func (m *recallModel) settle() {
	deadline := time.Now().Add(modelDeadline)
	for len(m.pending) > 0 || m.recallActive() {
		if time.Now().After(deadline) {
			m.t.Fatalf("model deadline: %d waits pending, recall active %v", len(m.pending), m.recallActive())
		}
		m.step()
	}
}

// recallActive reads the controller ledger's recall flag (read-only
// LedgerEvidence). The guest's event worker learns the same from its
// outstanding VIEW_WAIT; asking the ledger keeps idle polls, and the
// observation refreshes they would cause, out of the model.
func (m *recallModel) recallActive() bool {
	evidence, err := m.a.Controller.LedgerEvidence(m.t.Context())
	require(m.t, err)
	return evidence.Recall
}

// dueACK is one ACK a mount owes for its current recall event.
type dueACK struct {
	mount  *modelMount
	recall string
	key    string
	event  map[string]json.RawMessage
	ack    map[string]any
}

// eventWorker sends every ACK the mounts owe for the current recall. Both
// mounts' kernel records are published before the first ACK, as the guest
// does when both finish a phase before the host answers.
func (m *recallModel) eventWorker() bool {
	if !m.recallActive() {
		return false
	}
	var due []dueACK
	for _, mount := range m.mounts {
		if d, ok := m.nextACK(mount); ok {
			due = append(due, d)
		}
	}
	for _, d := range due {
		revision := d.mount.revision
		if d.ack["phase"] == "REOPENED" {
			revision++ // the guest advances its revision before reopening
		}
		observeMountACK(m.t, m.r, d.mount.index, d.event, d.ack, revision, d.mount.fence)
	}
	for _, d := range due {
		m.sendACK(d)
	}
	return len(due) > 0
}

// nextACK polls mount's current recall event and returns the next ACK it
// owes, if any.
func (m *recallModel) nextACK(mount *modelMount) (dueACK, bool) {
	if !mount.registered {
		return dueACK{}, false
	}
	payload := mount.chain.exec(m.t, "VIEW_WAIT", map[string]any{"domain": mount.domain, "after_sequence": 0})
	var poll struct{ Event map[string]json.RawMessage }
	require(m.t, json.Unmarshal(payload, &poll))
	if len(poll.Event) == 0 {
		return dueACK{}, false
	}
	var id, phase string
	require(m.t, json.Unmarshal(poll.Event["recall"], &id))
	require(m.t, json.Unmarshal(poll.Event["phase"], &phase))
	m.noteRecall(id, mount)
	key := id + "/" + string(poll.Event["sequence"])
	phases := ackPhases[phase]
	n := mount.acked[key]
	if n >= len(phases) {
		return dueACK{}, false
	}
	return dueACK{mount: mount, recall: id, key: key, event: poll.Event, ack: guestACK(poll.Event, mount.domain, mount.peer.scope.Session, phases[n])}, true
}

// sendACK sends one VIEW_ACK. ACCEPTED advances the mount (REOPENED installs
// the event's target and the next fence and revision); BLOCKED is resent on a
// later round, as the guest resends after EAGAIN.
func (m *recallModel) sendACK(d dueACK) {
	owner := m.recalls[d.recall].phase
	before := m.probes.Load()
	payload := d.mount.chain.exec(m.t, "VIEW_ACK", d.ack)
	owner.ackObservations += m.probes.Load() - before
	owner.acks++
	switch {
	case bytes.Equal(payload, []byte(`{"status":"BLOCKED"}`)):
		owner.blocked++
		return
	case !bytes.Equal(payload, []byte(`{"status":"ACCEPTED"}`)):
		m.t.Fatalf("m%d %s VIEW_ACK: %s", d.mount.index, d.ack["phase"], payload)
	}
	m.acks++
	d.mount.acked[d.key]++
	if d.ack["phase"] == "STOPPED" {
		// The recall's STOPPED quiesces every lease of the mount
		// (quiesceParticipants), so no pending release there can start
		// another recall.
		for _, w := range m.pending {
			if w.release && w.mount == d.mount.index {
				w.quiesced = true
			}
		}
	}
	if d.ack["phase"] == "REOPENED" {
		d.mount.target = d.event["target"]
		d.mount.fence++
		d.mount.revision++
	}
}

// noteRecall records mount as a participant of recall id. A recall seen for
// the first time is attributed to the step of the oldest pending VIEW_RELEASE
// on that mount whose lease is still active (the release that started it),
// or else to the current step.
func (m *recallModel) noteRecall(id string, mount *modelMount) {
	rc := m.recalls[id]
	if rc == nil {
		rc = &modelRecall{id: id, phase: m.phase, registered: m.registered(), participants: map[int]bool{}}
		for _, w := range m.pending {
			if w.release && !w.done && !w.quiesced && w.mount == mount.index {
				rc.phase = w.phase
				break
			}
		}
		m.recalls[id] = rc
		m.order = append(m.order, id)
		rc.phase.recalls = append(rc.phase.recalls, id)
	}
	rc.participants[mount.index] = true
}

// registered counts the mounts that have a view domain (took a lease).
func (m *recallModel) registered() int {
	n := 0
	for _, mount := range m.mounts {
		if mount.registered {
			n++
		}
	}
	return n
}
