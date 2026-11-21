//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"time"
)

// Scheduler of the recall workload model (recall_workload_test.go): the
// guest's QUERY retry loops, its per-mount cleanup worker and its view event
// worker, interleaved on the host's one control worker.

// modelDeadline bounds the whole model; a stuck recall fails here instead of
// at the package timeout.
const modelDeadline = 5 * time.Minute

// guestRetry is the guest's sleep between two QUERY / VIEW_QUERY sends
// (usleep_range(10000, 12000) in hfs_request_run and hfs_view_exchange).
const guestRetry = 10 * time.Millisecond

// waitStart is when a guest wait began: wall time, the slow observation
// count and the scheduler's idle time, so its modelled time (elapsed) can
// separate observation latency, guest idle time and other host work.
type waitStart struct {
	at    time.Time
	probe int32
	idle  time.Duration
}

// startWait marks the start of a guest wait, just before its first send.
func (m *recallModel) startWait() waitStart {
	return waitStart{at: time.Now(), probe: m.probes.Load(), idle: m.idle}
}

// elapsed is w's modelled time since its first send (since).
func (m *recallModel) elapsed(w *guestWait) time.Duration {
	return m.since(w.start)
}

// since is the modelled time since start: the slow observations paid
// meanwhile at the model latency, the guest's idle retry sleeps, and the
// remaining host work capped at hostOverheadAllowance. The cap keeps a slow
// or -race test host from deciding the outcome; up to the cap the measured
// host work counts as the runtime's own.
func (m *recallModel) since(start waitStart) time.Duration {
	modelled, host := m.split(start)
	return modelled + max(0, min(host, hostOverheadAllowance))
}

// split separates the time since start into its deterministic part (the
// slow observations at the model latency plus the guest's idle retry
// sleeps) and the measured host work: the wall time less the observations'
// real sleeps (m.sleep each: the model latency, or none for a virtual
// observer, newRecallModelVirtual) and the idle sleeps. host is uncapped
// and may be slightly negative (timer slack).
func (m *recallModel) split(start waitStart) (modelled, host time.Duration) {
	probes := time.Duration(m.probes.Load() - start.probe)
	idle := m.idle - start.idle
	return probes*m.latency + idle, time.Since(start.at) - probes*m.sleep - idle
}

// finish ends w now, recording its modelled and wall latency and the slow
// observations paid meanwhile.
func (m *recallModel) finish(w *guestWait) {
	w.done, w.latency, w.wall = true, m.elapsed(w), time.Since(w.start.at)
	w.observations = m.probes.Load() - w.start.probe
}

// register attributes a new wait to the current step.
func (m *recallModel) register(w *guestWait) *guestWait {
	m.phase.waits = append(m.phase.waits, w)
	return w
}

// launch applies a wait's first reply and keeps it pending unless it ended.
func (m *recallModel) launch(w *guestWait, first json.RawMessage) {
	w.sent = true
	m.answer(w, first)
	if !w.done {
		m.pending = append(m.pending, w)
	}
}

// answer applies one reply to w. A terminal status ends the wait, however
// late. A non-terminal reply received at or after the window's end expires
// it, as the guest checks its deadline only after a PENDING / INDETERMINATE
// reply (hfs_request_run, hfs_view_exchange) and otherwise queries again. A
// nil payload is a query the host never answered (see noReply): the guest
// keeps fetching that cell, so w is not queried again and only expires.
func (m *recallModel) answer(w *guestWait, payload json.RawMessage) {
	if payload == nil {
		w.status, w.stuck = "NO_REPLY", true
		m.expireIfDue(w)
		return
	}
	var reply struct{ Status string }
	require(m.t, json.Unmarshal(payload, &reply))
	w.payload, w.status = payload, reply.Status
	if !w.terminal(reply.Status) {
		m.expireIfDue(w)
		return
	}
	m.finish(w)
	if w.latency > w.phase.worst {
		w.phase.worst, w.phase.worstLabel = w.latency, w.label
	}
	if w.onDone != nil {
		w.onDone()
	}
}

// expireIfDue ends w as expired once its modelled time reached the window.
// For a query with no reply this stands in for hfs_transport's own 30 s
// fetch deadline, which fails the exchange the same way.
func (m *recallModel) expireIfDue(w *guestWait) {
	if m.elapsed(w) < guestQueryWindow {
		return
	}
	m.finish(w)
	w.expired = true
	m.t.Logf("EXPIRED after %s (wall %s): %s %s (last reply %s)", w.latency.Round(time.Millisecond), w.wall.Round(time.Millisecond), w.label, w.status, w.payload)
}

// step is one scheduler round: one QUERY per pending wait, the cleanup
// workers' next exchanges, then the event worker's ACKs. An expired wait is
// dropped: the guest has blocked its session (and an expired cleanup
// exchange fails its mount's cleanup worker, runCleanup). An idle round
// sleeps the guest's retry interval, which counts as guest time.
func (m *recallModel) step() {
	for _, w := range m.pending {
		switch {
		case w.done:
		case m.mounts[w.mount].silent:
			// A silent guest (task 0a4) sends nothing more: its waits end
			// unanswered and are dropped.
			w.done, w.status = true, "SILENT"
		case w.stuck:
			m.expireIfDue(w)
		default:
			m.answer(w, w.query())
		}
	}
	m.dispatchCleanup()
	progressed := m.eventWorker()
	still := m.pending[:0]
	for _, w := range m.pending {
		if !w.done {
			still = append(still, w)
		}
	}
	m.pending = still
	m.dispatchCleanup()
	if !progressed {
		time.Sleep(guestRetry)
		m.idle += guestRetry
		m.clock.advance(guestRetry)
	}
}

// modelClock is the admission's reuse and freshness clock in the model: its
// base wall time plus the modelled time paid since, i.e. the observations at
// the model latency and the guest's idle retry sleeps. Host work does not move
// it, so whether a request reuses an observation (admissionReuse) is decided
// by the modelled schedule, not by how loaded the test host is (i84).
type modelClock struct {
	base     time.Time
	advanced atomic.Int64 // nanoseconds
}

func (c *modelClock) now() time.Time {
	return c.base.Add(time.Duration(c.advanced.Load()))
}

func (c *modelClock) advance(d time.Duration) { c.advanced.Add(int64(d)) }

// installModelClock puts a's reuse rule on a new model clock and advances it
// by latency after each observation of the installed (latencyProbe) probe.
// The clock starts at the wall time a's current observation was committed:
// that observation keeps the age it had at commit, and wall time since then
// is not counted.
func installModelClock(a *Admission, latency time.Duration) *modelClock {
	a.mu.Lock()
	clock := &modelClock{base: a.refreshed}
	a.mu.Unlock()
	probe := a.probe
	a.probe = func(ctx context.Context) (installedObservation, error) {
		observed, err := probe(ctx)
		clock.advance(latency)
		return observed, err
	}
	a.now = clock.now
	return clock
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

// settle runs the scheduler until no wait is pending, no cleanup worker has
// work left and no recall exists.
func (m *recallModel) settle() {
	deadline := time.Now().Add(modelDeadline)
	for len(m.pending) > 0 || m.cleanupPending() || m.recallActive() {
		if time.Now().After(deadline) {
			m.t.Fatalf("model deadline: %d waits pending, recall active %v", len(m.pending), m.recallActive())
		}
		m.step()
	}
}

// recallActive reads the controller ledger's recall flag (read-only
// LedgerEvidence). The guest's event worker learns the same from its
// outstanding VIEW_WAIT; asking the ledger keeps idle polls, and the
// observation refreshes they would cause, out of the model. That omission
// is optimistic but small: a poll leg refreshes only when nothing else
// refreshed for five seconds (admissionReuse), i.e. while no recall runs
// (every ACK refreshes) and no request arrives, so it costs at most one
// observation per five idle seconds, and only a request arriving in those
// two seconds waits behind it.
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
// does when both finish a phase before the host answers. Each ACK's guest
// observation runs inline on the scheduler's one thread, serial with every
// other control command, as on the host's control worker before task d84;
// the model keeps it serial on purpose, the conservative bound after d84.
//
// A mount whose INSTALLED_DRAIN is ACCEPTED sends its DRAINED at once, before
// the peer gets its turn, as the guest's hfs_mmap_drained worker does (patch
// 0004). In an all-domain recall the first mount's DRAINED therefore arrives
// while the recall still waits in INSTALL_DRAIN for the peer and is BLOCKED;
// it is resent on a later round (zq3-r11, task y94). Sending every
// INSTALLED_DRAIN before any DRAINED never produced that early DRAINED.
// The gate schedules only start scoped recalls (one participant, whose
// DRAINED is never early); the T1 replay's CALL_CONFLICT recalls
// (recall_workload_t1_test.go) are all-domain with both mounts live, so
// there m0's early DRAINED is BLOCKED and resent once m1's INSTALLED_DRAIN
// was accepted (task 2a4). The blocked-domain case (task 0a4,
// recall_workload_blocked_test.go) drives an all-domain recall whose silent
// peer never sends INSTALLED_DRAIN, so there the early DRAINED stays BLOCKED
// until the peer is fenced; the early DRAINED itself is covered in
// internal/viewcontrol (early_drained_test.go). A mount whose silentAfter
// phase was ACCEPTED sends nothing more (nextACK, step, runCleanup).
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
		m.observeACK(d)
	}
	for _, d := range due {
		if m.sendACK(d) && d.ack["phase"] == "INSTALLED_DRAIN" {
			m.sendDrained(d)
		}
	}
	return len(due) > 0
}

// observeACK publishes d's guest kernel record for the fixed observer.
func (m *recallModel) observeACK(d dueACK) {
	revision := d.mount.revision
	if d.ack["phase"] == "REOPENED" {
		revision++ // the guest advances its revision before reopening
	}
	observeMountACK(m.t, m.r, d.mount.index, d.event, d.ack, revision, d.mount.fence)
}

// sendDrained sends the DRAINED that follows d's accepted INSTALLED_DRAIN
// immediately, from the same retained INSTALL_DRAIN event and without a new
// VIEW_WAIT, as hfs_mmap_drained does.
func (m *recallModel) sendDrained(d dueACK) {
	drained := dueACK{mount: d.mount, recall: d.recall, key: d.key, event: d.event, ack: guestACK(d.event, d.mount.domain, d.mount.peer.scope.Session, "DRAINED")}
	m.observeACK(drained)
	m.sendACK(drained)
}

// nextACK polls mount's current recall event and returns the next ACK it
// owes, if any.
func (m *recallModel) nextACK(mount *modelMount) (dueACK, bool) {
	if !mount.registered || mount.silent {
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

// sendACK sends one VIEW_ACK and reports whether it was ACCEPTED. ACCEPTED
// advances the mount (REOPENED installs the event's target and the next
// fence and revision); BLOCKED is resent on a later round, as the guest
// resends after EAGAIN.
func (m *recallModel) sendACK(d dueACK) bool {
	owner := m.recalls[d.recall].phase
	before := m.probes.Load()
	payload := d.mount.chain.exec(m.t, "VIEW_ACK", d.ack)
	owner.ackObservations += m.probes.Load() - before
	owner.acks++
	switch {
	case bytes.Equal(payload, []byte(`{"status":"BLOCKED"}`)):
		owner.blocked++
		return false
	case !bytes.Equal(payload, []byte(`{"status":"ACCEPTED"}`)):
		m.t.Fatalf("m%d %s VIEW_ACK: %s", d.mount.index, d.ack["phase"], payload)
	}
	m.acks++
	d.mount.acked[d.key]++
	if d.ack["phase"] == d.mount.silentAfter {
		d.mount.silent = true // the guest stops here (task 0a4)
	}
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
	return true
}

// noteRecall records mount as a participant of recall id. A recall seen for
// the first time is attributed to the step of the oldest sent, pending
// VIEW_RELEASE on that mount whose lease is still active (the release that
// started it), or else to the current step.
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
