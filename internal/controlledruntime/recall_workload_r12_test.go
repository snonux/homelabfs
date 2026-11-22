//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// zq3-r12 R12-1 replay in the recall workload model (task qc4).
//
// zq3-r12 (docs/lab/results/20261118-zq3-r12/README.md, "Root cause") was the
// first run with a recall in flight while mount-1 was still mounting: m1's
// bootstrap VIEW_RELEASE started a scoped recall, the mount thread polled
// VIEW_QUERY on that release (guest cells 12, 13; native 163, 168, 170) while
// m1's event worker had a VIEW_WAIT long-poll outstanding (guest cell 8,
// native legs 155 ... 169), and the STOPPED ACK (guest cell 9, native 156)
// was in flight. The host transport then fenced itself, both sessions'
// outstanding VIEW_WAITs failed and mount-1 hung for 30 s. nc4 found the
// trigger (a retried mapped cell re-ran VIEW_POLL and its base changed) and
// made every fence record its reason; oc4 and pc4 turned the outage into
// explicit errors (exporter ERROR 5 UNAVAILABLE, guest -EHOSTDOWN, retry
// loops stopped).
//
// This replay drives that schedule against the assembled runtime with the
// model's virtual observer and holds the STOPPED ACK's guest observation
// (Producer.Prepare's Refresh) open, as the lab's SSH probe was:
//
//   - TestZQ3R12StoppedACKObservationHoldsPollsEmpty explains the README's
//     "after the accepted STOPPED ACK, m1's recall stayed in INSTALL_DRAIN
//     with EMPTY polls for about 600 ms". It did not. Native numbers are
//     assigned at write time, not at answer time: cell 156 was written
//     before legs 157-167, but the VIEW_ACK lane (d84, guestbridge
//     endpoint.go commandQueue) runs the ACK's Prepare first, and a phase's
//     first ACK misses measuredACK (producer.go refreshFor) and pays one
//     fresh guest observation (about 0.6 s on the lab, zq3-r10's timing).
//     Meanwhile the control lane kept answering the VIEW_WAIT legs: the
//     controller had no STOPPED yet, so Exchange -> poll -> pollProgress ->
//     progress -> advancePhase("RECALL") waited for allAcked("STOPPED")
//     and poll answered EMPTY (viewcontrol recall.go). ACCEPTED was
//     protected only after the observation; acknowledge's progress then
//     moved RECALL -> DRAIN, and the next poll's pollProgress issued
//     INSTALL_DRAIN (sequence 2). The INSTALL_DRAIN in the snapshot was
//     issued after those polls, not before them: the hold ended while leg
//     169 ran, whose EMPTY base was protected just before the ACK's
//     Exchange took the controller lock; its receipt then got WouldBlock,
//     and the pre-nc4 retry's poll issued and saw sequence 2 (nc4's
//     trigger). No production bug in the EMPTY window itself: polls are
//     re-read and the VIEW_ACK lane is separate on purpose.
//   - TestZQ3R12TransportSaveFailureDuringMount injects a transport.db save
//     failure at R12-1's two candidate sites (README: the save of the
//     VIEW_QUERY write that reclaims the previous one, commitWrite, or the
//     publication of the outstanding VIEW_WAIT leg, publishResponse) and
//     requires the reason in the failure log (runtime stderr), the
//     lifecycle evidence (OBSERVE transport.failure) and transport.db, and
//     explicit Unavailable answers (EHOSTDOWN, pc4 ERROR 5) on both
//     sessions instead of a hang.

// r12EmptyLegs is the number of EMPTY VIEW_WAIT legs zq3-r12 answered between
// the STOPPED ACK's write (native 156) and R12-1 (legs 157 ... 167).
const r12EmptyLegs = 7

// TestZQ3R12StoppedACKObservationHoldsPollsEmpty replays R12-1's schedule up
// to the STOPPED ACK and shows why m1's VIEW_WAIT legs stayed EMPTY: while
// the ACK's observation runs, the controller has no STOPPED, every leg is
// EMPTY and every release VIEW_QUERY PENDING, and the recall history shows
// RECALL only. ACCEPTED moves the recall to DRAIN, and the very next leg
// issues and delivers INSTALL_DRAIN (sequence 2).
func TestZQ3R12StoppedACKObservationHoldsPollsEmpty(t *testing.T) {
	r := newR12Run(t)
	r.holdStoppedACK()
	r.pollDuringHold(r12EmptyLegs)
	r.requirePhases("while the STOPPED ACK's observation is held", 0, "RECALL")
	r.held.gate.open()
	payload, _, _, _, err := r.ack.finish()
	require(t, err)
	if !bytes.Equal(payload, []byte(`{"status":"ACCEPTED"}`)) {
		t.Fatalf("STOPPED VIEW_ACK after its observation: %s", payload)
	}
	r.requirePhases("after ACCEPTED", 1, "RECALL", "DRAIN")
	event := r.wait.poll(t)
	if !bytes.Contains(event, []byte(`"phase":"INSTALL_DRAIN"`)) || !bytes.Contains(event, []byte(`"sequence":2`)) {
		t.Fatalf("first VIEW_WAIT leg after ACCEPTED: %s", event)
	}
	r.requirePhases("after the next leg", 1, "RECALL", "DRAIN", "INSTALL_DRAIN")
	t.Logf("%d EMPTY legs and %d PENDING release queries while the STOPPED ACK's observation was held (zq3-r12: 7 legs in about 600 ms, one fresh guest observation); the next leg after ACCEPTED delivered INSTALL_DRAIN", r.empty, r.pending)
}

// TestZQ3R12TransportSaveFailureDuringMount injects a transport.db save
// failure (the ledger's before-commit failpoint) into R12-1's schedule at
// each candidate site while the STOPPED ACK's observation is held, m1's
// VIEW_WAIT leg is outstanding and the mount thread polls VIEW_QUERY. The
// fence must record its reason once (failure log, OBSERVE, transport.db) and
// every later exchange of either mount must fail at once with Unavailable.
func TestZQ3R12TransportSaveFailureDuringMount(t *testing.T) {
	for _, site := range []string{"commitWrite", "publishResponse"} {
		t.Run(site, func(t *testing.T) {
			r := newR12Run(t)
			r.holdStoppedACK()
			r.pollDuringHold(2)
			fault := newSaveFault(site)
			leg, cells := r.injectSaveFailure(fault)
			r.requireGuestErrors(leg)
			r.requireFailureRecord(fault, cells)
		})
	}
}

// heldObservation holds the next guest observation after hold() until its
// gate opens: the lab's slow SSH probe of a VIEW_ACK's Prepare, made
// deterministic. It never holds more than one probe.
type heldObservation struct {
	armed   chan struct{}
	entered chan struct{}
	gate    *probeGate
}

func newHeldObservation(t *testing.T) *heldObservation {
	return &heldObservation{armed: make(chan struct{}, 1), entered: make(chan struct{}, 1), gate: newProbeGate(t)}
}

// wrap installs the hold around a's probe (assembleRecallModel's wrapProbe).
func (h *heldObservation) wrap(a *Admission) {
	probe := a.probe
	a.probe = func(ctx context.Context) (installedObservation, error) {
		select {
		case <-h.armed:
			h.entered <- struct{}{}
			select {
			case <-h.gate.ch:
			case <-ctx.Done():
				return installedObservation{}, ctx.Err()
			}
		default:
		}
		return probe(ctx)
	}
}

// hold arms the hold for the next probe.
func (h *heldObservation) hold() { h.armed <- struct{}{} }

// awaitEntered waits until the armed probe is held.
func (h *heldObservation) awaitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-h.entered:
	case <-time.After(answerBound):
		t.Fatal("the STOPPED ACK's observation never started")
	}
}

// eventWait is m1's event worker's outstanding guest VIEW_WAIT: one guest
// cell (with its guest reclaim, identical on every leg) whose native
// VIEW_POLL legs each reclaim the previous EMPTY leg, as the 9p exporter
// polls within one wait (the zq3-r12 legs 155 ... 169 of guest cell 8).
type eventWait struct {
	peer      *sourceRig
	guestCell uint64
	guestAck  *guestbridge.Ack
	legs      uint64
	last      *guestbridge.Ack // the previous leg's native response
	value     map[string]any
}

// write sends the wait's next leg and leaves it outstanding.
func (w *eventWait) write() (*writtenLeg, error) {
	leg, err := w.peer.writeLeg(w.guestCell, w.legs, "VIEW_WAIT", w.value, w.guestAck, w.last, nil)
	if err == nil {
		w.legs++
	}
	return leg, err
}

// poll sends the next leg, fetches its reply and returns the payload.
func (w *eventWait) poll(t *testing.T) json.RawMessage {
	t.Helper()
	leg, err := w.write()
	require(t, err)
	payload, _, _, _, err := leg.finish()
	require(t, err)
	w.last = w.peer.mappingLastNative
	return payload
}

// r12Run is one replay of zq3-r12's schedule: the model, m1, the held
// observation, m1's bootstrap release ticket (the mount thread's VIEW_QUERY
// polls), the event worker's outstanding VIEW_WAIT, the STOPPED ACK whose
// observation is held, and the EMPTY legs and PENDING queries answered
// during the hold.
type r12Run struct {
	t              *testing.T
	m              *recallModel
	m1             *modelMount
	held           *heldObservation
	control        guestbridge.Ticket
	events         *r5Chain
	event          map[string]json.RawMessage
	wait           *eventWait
	ack            *writtenLeg
	empty, pending int
}

// newR12Run assembles the model with a virtual observer and the held
// observation, mounts m0 completely and runs m1's mount(8) up to its
// bootstrap VIEW_RELEASE, which starts a recall scoped to m1 (PENDING), and
// the event worker's VIEW_WAIT that delivers its RECALL event. The mount
// thread (m1's control chain) owns the release and its VIEW_QUERYs; the
// event worker has its own chain, as in zq3-r12 (VIEW_QUERY 168 reclaimed
// 163, VIEW_WAIT leg 169 reclaimed leg 167).
//
// Cleanups run last-registered first, so the gate's own cleanup
// (newProbeGate) would run after the model's a.Close(), whose transport
// Close joins a lane worker parked in the held probe. A failing run would
// then hang in Close until the probe's 10 s bound and fence the admission.
// Opening the gate is idempotent, so it is registered again here, after
// the model, to run before a.Close().
func newR12Run(t *testing.T) *r12Run {
	if testing.Short() {
		t.Skip("assembles the runtime and replays two mounts")
	}
	held := newHeldObservation(t)
	m := assembleRecallModel(t, readZQ3Values(t), recallModelLatency(t), 0, true, held.wrap)
	t.Cleanup(held.gate.open)
	m.mount(0)
	m.settle()
	r := &r12Run{t: t, m: m, m1: m.mounts[1], held: held, events: &r5Chain{peer: m.mounts[1].peer}}
	r.bootstrapM1()
	payload := r.events.exec(t, "VIEW_WAIT", map[string]any{"domain": r.m1.domain, "after_sequence": 0})
	var poll struct{ Event map[string]json.RawMessage }
	require(t, json.Unmarshal(payload, &poll))
	if !bytes.Equal(poll.Event["phase"], []byte(`"RECALL"`)) || !bytes.Equal(poll.Event["sequence"], []byte("1")) {
		t.Fatalf("m1 VIEW_WAIT after its bootstrap release: %s", payload)
	}
	r.event = poll.Event
	r.requirePhases("after m1's bootstrap release", 0, "RECALL")
	return r
}

// bootstrapM1 is recallModel.mount's m1 sequence up to the bootstrap
// VIEW_RELEASE: HELLO, ROOT, the early VIEW_WAIT (event worker, EMPTY), the
// bootstrap lease and the release, sent but not awaited (the scheduler
// would acknowledge the recall itself).
func (r *r12Run) bootstrapM1() {
	m, mount := r.m, r.m1
	m.begin("m1 mount(8) with a recall during bootstrap")
	p := m.process("mount(8)", &mount.peer.credentials)
	_, _, err := mount.peer.execute("HELLO", guestbridge.TerminalProtocol())
	require(r.t, err)
	root := m.claim(mount, p, "ROOT", map[string]any{})
	m.await(root)
	var result struct{ Capabilities []guestbridge.Capability }
	require(r.t, json.Unmarshal(claimResult(r.t, root), &result))
	mount.root, mount.domain = result.Capabilities[0], result.Capabilities[1]
	if early := r.events.exec(r.t, "VIEW_WAIT", map[string]any{"domain": mount.domain, "after_sequence": 0}); !bytes.Contains(early, []byte(`"status":"EMPTY"`)) {
		r.t.Fatalf("m1 early VIEW_WAIT: %s", early)
	}
	lease, _ := m.lease(mount, p, nil)
	if lease == nil {
		r.t.Fatal("m1 bootstrap lease refused")
	}
	r.control, _ = m.ticket(mount, p, "control", m.families[1].Control)
	reply := m.noReply(mount.chain, "VIEW_RELEASE", map[string]any{"request": r.control, "lease": *lease})
	if !bytes.Contains(reply, []byte(`"status":"PENDING"`)) {
		r.t.Fatalf("m1 bootstrap VIEW_RELEASE: %s", reply)
	}
}

// holdStoppedACK posts the event worker's next VIEW_WAIT (after_sequence 1,
// leg 0 EMPTY, zq3-r12's native 155) and then writes the STOPPED ACK
// (native 156, no reclaim) whose Prepare observation is held. A Refresh
// first makes the installed observation fresh, so the legs' own Prepare
// reuses it (refreshIfOlder) instead of queueing behind the held probe; the
// published STOPPED kernel record is what makes the ACK's first send miss
// measuredACK and probe, as on the lab.
func (r *r12Run) holdStoppedACK() {
	peer := r.m1.peer
	peer.cells++
	r.wait = &eventWait{peer: peer, guestCell: peer.cells, guestAck: r.events.guest, last: r.events.native, value: map[string]any{"domain": r.m1.domain, "after_sequence": 1}}
	r.requireEmpty(r.wait.poll(r.t), "VIEW_WAIT leg 0 before the STOPPED ACK")
	require(r.t, r.m.r.admission.Refresh(r.t.Context()))
	ack := guestACK(r.event, r.m1.domain, peer.scope.Session, "STOPPED")
	observeMountACK(r.t, r.m.r, 1, r.event, ack, r.m1.revision, r.m1.fence)
	r.held.hold()
	peer.cells++
	var err error
	r.ack, err = peer.writeLeg(peer.cells, 0, "VIEW_ACK", ack, nil, nil, nil)
	require(r.t, err)
	r.held.awaitEntered(r.t)
}

// pollDuringHold runs legs more VIEW_WAIT legs, each followed by the mount
// thread's VIEW_QUERY of its release, while the ACK's observation is held:
// every leg must be EMPTY and every query PENDING.
func (r *r12Run) pollDuringHold(legs int) {
	for i := range legs {
		r.requireEmpty(r.wait.poll(r.t), fmt.Sprintf("VIEW_WAIT leg %d during the held observation", r.wait.legs))
		r.empty++
		reply := r.m.noReply(r.m1.chain, "VIEW_QUERY", map[string]any{"request": r.control})
		if !bytes.Contains(reply, []byte(`"status":"PENDING"`)) {
			r.t.Fatalf("release VIEW_QUERY %d during the held observation: %s", i, reply)
		}
		r.pending++
	}
}

func (r *r12Run) requireEmpty(payload json.RawMessage, what string) {
	r.t.Helper()
	if !bytes.Equal(payload, []byte(`{"event":null,"status":"EMPTY"}`)) {
		r.t.Fatalf("%s: %s", what, payload)
	}
}

// requirePhases requires m1's recall (the newest history record, scoped to
// m1, opened by its RELEASE) to have entered exactly phases with acks
// accepted ACKs.
func (r *r12Run) requirePhases(when string, acks int, phases ...string) {
	r.t.Helper()
	history, err := r.m.a.Controller.History(r.t.Context())
	require(r.t, err)
	last := history.Recalls[len(history.Recalls)-1]
	got := []string{}
	for _, p := range last.Phases {
		got = append(got, p.Phase)
	}
	if fmt.Sprint(got) != fmt.Sprint(phases) || len(last.Acks) != acks || last.Scope != "DOMAIN" || last.Domain != r.m1.peer.scope.Session {
		r.t.Fatalf("%s: recall %s %s phases %v, %d ACKs; want phases %v, %d ACKs, scoped to m1", when, last.Kind, last.Scope, got, len(last.Acks), phases, acks)
	}
}

// requireUnavailable requires err to be the fenced transport's explicit
// refusal: fsapi.Unavailable, which FUSE answers with EHOSTDOWN, the exporter
// with ERROR 5 UNAVAILABLE (pc4) and the guest with -EHOSTDOWN, stopping its
// retry loops (oc4). WouldBlock instead would make the guest retry until its
// 30 s deadline, the R12-1 hang.
func requireUnavailable(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, fsapi.Unavailable) {
		t.Errorf("%s after the fence: %v, want Unavailable (EHOSTDOWN)", what, err)
	}
}
