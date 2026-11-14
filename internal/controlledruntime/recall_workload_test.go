//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Recall workload model (task 984).
//
// zq3-r8 failed on view recalls that no offline gate drove (R8-1,
// docs/lab/results/20261114-zq3-r8/README.md "Root cause"): o64's workload
// model (internal/viewcontrol/zq3_workload_model_test.go) counts CALLs but
// takes no lease, starts no recall and pays no guest-observation latency.
// This model replays what zq3-r8 did up to the failure against the assembled
// runtime (Assemble: real bridge, transport, producer, Facts and view
// controller) at the zq3 spec's values, with a slow guest observer:
//
//   - each mount(8): HELLO, ROOT, an early VIEW_WAIT, the bootstrap DOMAIN
//     READ lease (expected_target NULL) and its VIEW_RELEASE, which waits for
//     RELEASED (patch 0004 hfs_mmap_bootstrap loop), then the process exits
//     (PROCESS_EXIT, sent asynchronously by the guest's exit work);
//   - the k64 usable gate (tools/lab/retained9p/bootstrap.py step_usable):
//     a first lstat of each root, a fixed settle until mount-1's end plus
//     USABLE_SETTLE_SECONDS (the guest QUERY window plus 10 s), then a final
//     lstat of each root. Each lstat child's hfs_getattr calls
//     hfs_begin(mutation=false), so hfs_mmap_begin takes a DOMAIN READ lease
//     with expected_target = the view's installed target (the last REOPEN
//     target), then the GETATTR claim runs, hfs_mmap_end queues the cleanup
//     work on the mount's one cleanup worker (VIEW_RELEASE, then RETIRE and
//     VIEW_RETIRE, recall_workload_cleanup_test.go), and the child exits
//     (PROCESS_EXIT). Cleanup and exit are asynchronous.
//
// Concurrency. The host serializes every control-lane command (QUERY,
// VIEW_QUERY, VIEW_WAIT, VIEW_RELEASE, VIEW_ACK) on one control worker, and
// before task d84 a VIEW_ACK's guest observation runs on that worker. The
// model is therefore one sequential scheduler (step): every outstanding
// guest wait sends one QUERY / VIEW_QUERY, each mount's cleanup worker sends
// its next release (one at a time, in order), then the guest's event worker
// sends the ACK each mount owes for its current recall event, publishing
// both mounts' kernel records first as the guest does. ACKs and their
// observations stay serial on that one thread on purpose: that is today's
// host, and after d84 it remains the conservative bound (d84 takes the
// observation off the control worker, not the five observations of a recall
// off each other), so the model cannot show what d84 gains. Observation
// latency is real time (latencyProbe), because the admission's five-second
// reuse rule (admissionReuse) runs on the wall clock.
//
// The guest's retry loops and its probe processes race. Two schedules bound
// that race:
//   - interleaved: one scheduler round runs before every request a process
//     sends, so a pending release's VIEW_QUERY gets in before the next
//     process's VIEW_ACQUIRE (the release loop's 10 ms retry wins);
//   - foreground first: a process's requests run back to back and pending
//     waits only advance while a process waits. The lstat thread parked in
//     hfs_view_enter wakes on REOPEN and acquires before the release's next
//     VIEW_QUERY.
//
// Facts.CheckPhysical refuses a VIEW_RELEASE while ANY lease of its domain
// is active. When a newer lease of the same mount is granted after the older
// release's own recall, the older release waits for that lease's release
// recall; but that release sits behind it on the mount's cleanup worker and
// is never sent, so the older one times out and fails the mount's cleanup
// (task g84). Worse, the held release's VIEW_QUERY gets WouldBlock, which
// the transport retries in the control worker's pending ring; before h84 the
// control lane then answered nothing else in the model (noReply), so the
// other mount's pending release died too. The gate's settle keeps its
// two rounds apart, so the gate itself never queues two releases of one
// mount; the back-to-back case is TestZQ3BackToBackSameMountReleasesG84.
//
// The guest gives every claim (hfs_request_run) and every view ticket
// exchange (hfs_view_exchange) 30 s from its first send; it gives up only
// when a non-terminal reply arrives at or after that point. The model
// measures each wait in modelled time (recallModel.elapsed): observations at
// the model latency, guest idle retry time, and other host work up to
// hostOverheadAllowance. Every wait must end, and, deterministically, its
// observations at the model latency plus that allowance must fit the window.

// guestQueryWindow is the guest kernel's fixed QUERY / VIEW_QUERY deadline.
const guestQueryWindow = 30 * time.Second

// hostOverheadAllowance is the host work besides observations that a wait
// may pay before the excess is blamed on the test host. The critical wait
// (the foreground-first m1 release, 10 observations) hit this cap in every
// run, so in practice the verdict is "at most 11 observations at 2 s per
// wait" (11*2 s + 6 s < 30 s, 12 would not fit). The lab passes therefore
// only if one SSH observation costs less than about 2.4 s (10 observations
// plus the allowance in 30 s); that cost is unknown (zq3-r8 README). The
// observation counts themselves drift slightly with host speed, because the
// five-second admissionReuse rule decides whether a request refreshes.
const hostOverheadAllowance = 6 * time.Second

// usableSettle is bootstrap.py's USABLE_SETTLE_SECONDS (GUEST_QUERY_SECONDS
// + 10): the gate's final probe starts that long after mount-1's end.
const usableSettle = guestQueryWindow + 10*time.Second

// readyLag is the part of usableSettle the lab spends before the gate's
// first probe (READY record, SSH round trips), which the model does not
// replay; round 1 must be quiet within usableSettle - readyLag.
const readyLag = 5 * time.Second

// recallModelLatency is the per-observation latency the model pays: 2 s by
// default (about what zq3-r8 paid per SSH observation), or a Go duration in
// HFS_RECALL_MODEL_LATENCY for exploring other observers.
func recallModelLatency(t *testing.T) time.Duration {
	t.Helper()
	value := os.Getenv("HFS_RECALL_MODEL_LATENCY")
	if value == "" {
		return slowObservation
	}
	latency, err := time.ParseDuration(value)
	require(t, err)
	return latency
}

// ackPhases maps each recall event phase to the ACKs the guest sends for it.
// INSTALL_DRAIN is acknowledged twice: INSTALLED_DRAIN, then DRAINED once the
// guest's dirty pages are drained (none here).
var ackPhases = map[string][]string{"RECALL": {"STOPPED"}, "INSTALL_DRAIN": {"INSTALLED_DRAIN", "DRAINED"}, "INSTALL_FINAL": {"INSTALLED_FINAL"}, "REOPEN": {"REOPENED"}}

// viewTerminal lists the view ticket statuses hfs_view_exchange stops on.
var viewTerminal = map[string]bool{"GRANTED": true, "FROZEN": true, "RELEASED": true, "CANCELLED": true, "BLOCKED": true}

// recallModel is the scheduler and its accounting.
type recallModel struct {
	t         *testing.T
	r         *sourceRig
	a         *Assembly
	families  []viewcontrol.TicketFamilies
	mounts    []*modelMount
	pending   []*guestWait
	recalls   map[string]*modelRecall
	order     []string // recall IDs in first-seen order
	probes    atomic.Int32
	latency   time.Duration
	idle      time.Duration // scheduler retry sleeps (guest time)
	phase     *modelPhase
	phases    []*modelPhase
	processes byte
	// interleave runs one scheduler round before every request a process
	// sends (the interleaved schedule); false is foreground first.
	interleave bool
	tickets    int      // view tickets sent (each spends one Views.Identities)
	acks       int      // accepted first ACKs (each adds one AckArchive row)
	viewMoves  []string // probes whose view moved under their GETATTR
	// round1 is the modelled time from mount-1's end until round 1 of the
	// gate was quiet (no wait, cleanup or recall left); -1 when not measured.
	round1 time.Duration
}

// modelMount is one guest mount: its peer rig, the control-lane chain that
// reclaims its previous control response, the view state the guest keeps
// (installed target, per-mount fence and kernel record revision), its
// cleanup worker (queued jobs, the exchange in flight, failure) and what
// RETIRE / VIEW_RETIRE retire (request list, ticket families, floors).
type modelMount struct {
	index           int
	peer            *sourceRig
	chain           *r5Chain
	root, domain    guestbridge.Capability
	target          json.RawMessage
	fence, revision uint64
	registered      bool
	acked           map[string]int // recall/sequence -> ACKs sent for that event
	jobs            []cleanupJob
	inflight        *guestWait
	cleanupFailed   bool
	claims          []*claimRec
	claimFloor      uint64
	families        []*familyRec
	familyIndex     map[string]*familyRec
}

// modelProcess is one guest process: its own credentials, hence its own
// ticket families per mount (ticketKeys hashes the credentials).
type modelProcess struct {
	name    string
	cred    guestbridge.Credentials
	tickets map[string]uint64
}

// guestWait is one claim, view ticket exchange or retirement (kind "claim",
// "view", "retire"), timed from its first send as the guest's 30 s deadline
// is. release marks a VIEW_RELEASE of mount, sent by send (its window starts
// then), so that the recall it starts is attributed to its step; quiesced
// marks one whose lease a later recall of mount already stopped, so it can
// no longer start one. onDone runs when a terminal reply ends it.
type guestWait struct {
	label         string
	phase         *modelPhase
	mount         int
	kind          string
	release       bool
	quiesced      bool
	sent          bool
	stuck         bool // its last query got no reply (noReply)
	start         waitStart
	observations  int32
	send, query   func() json.RawMessage
	terminal      func(status string) bool
	payload       json.RawMessage
	status        string
	latency, wall time.Duration
	done, expired bool
	onDone        func()
}

// modelPhase accounts one workload step: one mount(8) or one k64 lstat
// child. Its waits are the requests it sent; its recalls are the ones its own
// VIEW_RELEASE started (whenever they ran), with their ACKs and the slow
// observations those ACKs paid.
type modelPhase struct {
	name            string
	acks, blocked   int
	ackObservations int32
	recalls         []string
	waits           []*guestWait
	worst           time.Duration
	worstLabel      string
}

// modelRecall records which mounts received a recall's events and how many
// mounts had a view domain when it was first seen.
type modelRecall struct {
	id           string
	phase        *modelPhase
	registered   int
	participants map[int]bool
}

// kind classifies a recall: "single" when only one domain existed (the first
// bootstrap), "all-domain" when every registered domain took part, else
// "scoped" (task 784).
func (rc *modelRecall) kind() string {
	switch {
	case rc.registered <= 1:
		return "single"
	case len(rc.participants) >= rc.registered:
		return "all-domain"
	}
	return "scoped"
}

// TestZQ3RecallWorkloadMeetsGuestQueryWindow replays zq3-r8's bootstrap and
// k64 usable gate with a 2 s guest observer under both schedules (see the
// top of this file) and requires every claim, view exchange and retirement
// to end within the guest's window, round 1 to be quiet before the gate's
// fixed settle ends, no probe's view to move under its GETATTR, and the
// recall budgets to hold (task f84 angle). It logs recalls (scoped,
// all-domain), VIEW_ACKs and slow observations per probe. The schedules use
// separate fixtures and run in parallel: their time is almost all modelled
// observation latency and the gate's settle.
func TestZQ3RecallWorkloadMeetsGuestQueryWindow(t *testing.T) {
	if testing.Short() {
		t.Skip("pays about a minute of modelled guest-observation latency")
	}
	t.Parallel()
	values := readZQ3Values(t)
	latency := recallModelLatency(t)
	for _, schedule := range []struct {
		name       string
		interleave bool
	}{{"interleaved", true}, {"foreground first", false}} {
		t.Run(schedule.name, func(t *testing.T) {
			t.Parallel()
			m := runRecallWorkload(t, values, latency, schedule.interleave, true)
			t.Run("claims", func(t *testing.T) { m.checkWaits(t, "claim") })
			t.Run("view exchanges", func(t *testing.T) { m.checkWaits(t, "view") })
			t.Run("retirements", func(t *testing.T) { m.checkWaits(t, "retire") })
			t.Run("settle", m.checkSettle)
			t.Run("views", m.checkViews)
			t.Run("budgets", func(t *testing.T) { m.checkBudgets(t, values) })
		})
	}
}

// TestZQ3BackToBackSameMountReleasesG84 runs the gate's two rounds back to
// back under the foreground-first schedule, so each mount's second lstat
// takes its lease right after the first one's release recall reopened the
// view. Every wait must end within the window. Before g84 the first release
// was held by Facts.CheckPhysical for the newer lease, whose own release
// waited behind it on the mount's cleanup worker: the first release got no
// reply until its window ended and the mount's cleanup failed. Before h84 the
// held cell's retries also starved the control lane, so the other mount's
// pending release died with it (noReply). g84 certifies a VIEW_RELEASE per
// lease and h84 stops a retrying cell from starving its lane, so a late view
// exchange now fails the test; the failure names whether it still has the
// g84 signature (a VIEW_RELEASE that got no reply, or one never sent behind
// a failed cleanup worker) to point at which fix regressed.
func TestZQ3BackToBackSameMountReleasesG84(t *testing.T) {
	if testing.Short() {
		t.Skip("pays about a minute of modelled guest-observation latency")
	}
	t.Parallel()
	m := runRecallWorkload(t, readZQ3Values(t), recallModelLatency(t), false, false)
	m.checkWaits(t, "claim")
	m.checkWaits(t, "retire")
	m.checkViews(t)
	m.checkObservations(t, "view", true)
	late := m.lateWaits("view")
	if len(late) == 0 {
		return
	}
	for _, w := range late {
		unsent := w.status == "UNSENT" && m.mounts[w.mount].cleanupFailed
		signature := "not the g84 signature"
		if w.release && (w.stuck || unsent) {
			signature = "g84 signature (held or unsent VIEW_RELEASE): g84 regressed"
		}
		t.Errorf("late view exchange, %s: %s", signature, describe(w))
	}
}

// runRecallWorkload runs the bootstrap and the k64 gate once under one
// schedule. With settle, the gate's final probe follows bootstrap.py
// step_usable's fixed settle: it starts usableSettle after mount-1's end, not
// when round 1 happens to be quiet. The model runs round 1 to quiet and
// records how long that took from mount-1's end (round1); checkSettle
// requires it to end inside the settle, so the final probe never overlaps
// round 1's cleanup. Without settle the rounds run back to back (g84 case).
func runRecallWorkload(t *testing.T, values zq3Values, latency time.Duration, interleave, settle bool) *recallModel {
	m := newRecallModel(t, values, latency)
	m.interleave = interleave
	m.mount(0)
	m.mount(1)
	mounted := m.startWait()
	for round := 1; round <= 2; round++ {
		if round == 2 && settle {
			m.settle()
			m.round1 = m.since(mounted)
		}
		for i := range m.mounts {
			m.begin(fmt.Sprintf("k64 round %d m%d lstat", round, i))
			m.usableProbe(i)
		}
	}
	m.settle()
	m.report()
	return m
}

// newRecallModel assembles the runtime at the zq3 values, attaches both
// peers and installs the slow observer for everything that follows.
func newRecallModel(t *testing.T, values zq3Values, latency time.Duration) *recallModel {
	r, c := assemblyFixture(t)
	values.apply(t, &c)
	m := &recallModel{t: t, r: r, latency: latency, families: c.Authorization.Enrollment.TicketFamilies, recalls: map[string]*modelRecall{}, round1: -1}
	var slow atomic.Bool
	slow.Store(true)
	latencyProbe(r.admission, latency, &slow, &m.probes, nil)
	a, err := Assemble(t.Context(), c)
	if err != nil {
		if a != nil {
			_ = a.Close() // partial owners are returned on error
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { require(t, a.Close()) })
	m.a = a
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	for i, peer := range r.peers {
		attachAssembly(peer, a, &counter)
		m.mounts = append(m.mounts, &modelMount{index: i, peer: peer, chain: &r5Chain{peer: peer}, fence: 1, revision: 1, acked: map[string]int{}, familyIndex: map[string]*familyRec{}})
	}
	t.Logf("guest observation latency %s, guest QUERY window %s, host overhead allowance %s", latency, guestQueryWindow, hostOverheadAllowance)
	return m
}

// zq3Values are the committed zq3 spec values the model runs at.
type zq3Values struct {
	Views struct {
		Tickets, ControlTickets, Reservations, Identities, Sources int
	}
	Providers                                               ProviderLimits
	Capabilities, CapabilityIdentities, CredentialRevisions int
}

// readZQ3Values reads internal/runtimeprep/testdata/zq3-spec.json, the spec
// the zq3-r8 bundle was generated from.
func readZQ3Values(t *testing.T) zq3Values {
	t.Helper()
	raw, err := os.ReadFile("../runtimeprep/testdata/zq3-spec.json")
	require(t, err)
	var v zq3Values
	require(t, json.Unmarshal(raw, &v))
	return v
}

// apply sets the spec's provider and bridge values on the assembly and
// requires the fixture's signed view bounds to equal the spec's ticket,
// reservation and identity bounds, which every recall and lease spends. The
// fixture keeps its smaller Sources (8 for 10), which only makes it stricter.
func (v zq3Values) apply(t *testing.T, c *AssemblyConfig) {
	t.Helper()
	b := c.Authorization.Enrollment.Bounds
	if b.Tickets != v.Views.Tickets || b.ControlTickets != v.Views.ControlTickets || b.Reservations != v.Views.Reservations || b.Identities != v.Views.Identities || b.Sources > v.Views.Sources {
		t.Fatalf("fixture view bounds %+v differ from zq3 %+v", b, v.Views)
	}
	c.ProviderLimits = v.Providers
	c.Capabilities, c.CapabilityIdentities, c.CredentialRevisions = v.Capabilities, v.CapabilityIdentities, v.CredentialRevisions
}

// begin starts accounting a new workload step.
func (m *recallModel) begin(name string) {
	m.phase = &modelPhase{name: name}
	m.phases = append(m.phases, m.phase)
}

// process starts a guest process with cred's identity under its own process
// UUID (nil cred: a fresh child of mount 0's credentials).
func (m *recallModel) process(name string, cred *guestbridge.Credentials) *modelProcess {
	if cred == nil {
		m.processes++
		child := m.mounts[0].peer.credentials
		child.Groups = append([]uint32(nil), child.Groups...)
		child.Process = testUUID(160 + m.processes).String()
		cred = &child
	}
	return &modelProcess{name: name, cred: *cred, tickets: map[string]uint64{}}
}

// ticket returns the process's next ticket of family on mount.
func (p *modelProcess) ticket(mount int, kind string, family viewcontrol.TicketFamily) guestbridge.Ticket {
	key := fmt.Sprintf("%d/%s", mount, kind)
	p.tickets[key]++
	return guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: p.tickets[key], Credentials: p.cred}
}
