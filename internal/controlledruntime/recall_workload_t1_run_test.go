//go:build linux || freebsd

package controlledruntime

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/guestviewmodel"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Admission, exit, report and the test of the T1 replay
// (recall_workload_t1_test.go).

// admission replays guest.py's open_domains for each root in order:
// realpath's lstat of the mount point, the root OPEN under its own
// observation lease, and the fstat of the opened root. The case starts
// seconds after the previous lab step (the controller dispatches it over
// SSH), so the guest is idle for at least admissionReuse first and the
// admission's first request pays a fresh observation, as in the lab.
func (r *t1Replay) admission() {
	r.stage = "admission"
	r.m.clock.advance(admissionReuse)
	r.m.idle += admissionReuse
	for i := range r.m.mounts {
		root := r.m.mounts[i].root
		r.syscall(fmt.Sprintf("m%d realpath lstat root", i), func() {
			r.observe(i, "GETATTR", map[string]any{"object": root})
		})
		r.syscall(fmt.Sprintf("m%d open-root READ|DIRECTORY", i), func() {
			r.roots[i] = r.observe(i, "OPEN", map[string]any{"object": root, "flags": t1Flags("READ", false)}).Capabilities[0]
		})
		r.syscall(fmt.Sprintf("m%d stat-root fstat", i), func() {
			r.observe(i, "GETATTR", map[string]any{"object": root})
		})
	}
}

// exit replays R10-1's process end: m1's root is closed normally, m0's root
// fd leaked, so the exit work's PROCESS_EXITs (both sessions) and m0's last
// fput DESCRIPTION_RELEASE go out back to back, the release after its
// issuer's exit was sent (allowed since task 194). Then the cleanup workers
// finish their releases and retirements.
func (r *t1Replay) exit() {
	r.stage = "exit"
	r.close(1, r.roots[1], "root")
	r.syscall("exit: PROCESS_EXIT m0+m1, leaked m0 root DESCRIPTION_RELEASE", func() {
		exit := map[string]any{"process": r.p.cred.Process, "exit_sequence": 1}
		r.closes[0]++
		var waits []*guestWait
		var held []bool
		for _, c := range []struct {
			mount int
			op    string
			args  map[string]any
		}{{0, "PROCESS_EXIT", exit}, {0, "DESCRIPTION_RELEASE", map[string]any{"description": r.roots[0], "release_sequence": r.closes[0]}}, {1, "PROCESS_EXIT", exit}} {
			before := r.sys.held
			waits = append(waits, r.send(c.mount, c.op, c.args))
			held = append(held, r.sys.held > before)
		}
		for k, w := range waits {
			if res := r.finish(w, held[k]); res.Code != "OK" {
				r.m.t.Errorf("%s: %+v", w.label, res)
			}
		}
	})
	r.syscall("settle: cleanup workers, retirements", r.m.settle)
}

// t1Stats sums a run's syscalls.
type t1Stats struct {
	stage     map[string]time.Duration // modelled time per stage
	total     time.Duration            // modelled time of the whole run
	host      time.Duration            // measured host work besides
	measured  time.Duration            // total plus host: the run as this host paid it
	obs       int32                    // the syscalls' slow observations (no bootstrap)
	held      int
	heldWorst time.Duration
	worst     *t1Syscall // the syscall with the longest exchange
	advances  uint64
	// acks and blocked count the run's VIEW_ACKs and the BLOCKED answers
	// among them: an early DRAINED sent in guest order (eventWorker).
	acks, blocked int
}

// stats sums the run's syscalls and VIEW_ACKs and reads the controller's
// advance counter.
func (r *t1Replay) stats() t1Stats {
	s := t1Stats{stage: map[string]time.Duration{}}
	for _, c := range r.syscalls {
		s.stage[c.stage] += c.latency
		s.total += c.latency
		s.host += c.host
		s.obs += c.observations
		s.held += c.held
		s.heldWorst = max(s.heldWorst, c.heldWorst)
		if s.worst == nil || c.worst > s.worst.worst {
			s.worst = c
		}
	}
	s.measured = s.total + s.host
	for _, p := range r.syscallPhases() {
		s.acks += p.acks
		s.blocked += p.blocked
	}
	h, err := r.m.a.Controller.History(r.m.t.Context())
	require(r.m.t, err)
	s.advances = h.Advances
	return s
}

// report logs every syscall (modelled time, measured host work, slow
// observations, recalls by trigger, held claims, longest exchange) and the
// run's totals against the guest's 30 s exchange window, the admission alarm
// and T1's budget, both as modelled (deterministic) and as measured on this
// host (modelled plus this host's work). The lab's per-request cost is not
// modelled, so neither figure is the lab's time.
func (r *t1Replay) report(t *testing.T) t1Stats {
	for _, c := range r.syscalls {
		held := ""
		if c.held > 0 {
			held = fmt.Sprintf(", %d held (WouldBlock) up to %s", c.held, c.heldWorst.Round(time.Millisecond))
		}
		t.Logf("  %-9s %-52s %7s (+%6s host) %2d obs, recalls %-18s longest %s%s", c.stage, c.name, c.latency.Round(time.Millisecond), c.host.Round(time.Millisecond), c.observations, kindsText(c.kinds), c.worst.Round(time.Millisecond), held)
	}
	s := r.stats()
	t.Logf("modelled: admission %s (alarm %s; the old %s alarm %s), body %s, exit %s; total %s of T1's %s budget, %s",
		s.stage["admission"].Round(time.Millisecond), t1AdmissionAlarm, t1OldAdmissionAlarm, verdict(s.stage["admission"] <= t1OldAdmissionAlarm),
		s.stage["body"].Round(time.Millisecond), s.stage["exit"].Round(time.Millisecond), s.total.Round(time.Millisecond), t1RequestBudget, verdict(s.total <= t1RequestBudget))
	t.Logf("measured: modelled %s + this host's work %s = %s of T1's %s budget, %s",
		s.total.Round(time.Millisecond), s.host.Round(time.Millisecond), s.measured.Round(time.Millisecond), t1RequestBudget, verdict(s.measured <= t1RequestBudget))
	t.Logf("%d syscalls, %d slow observations at %s; recalls %s; %d VIEW_ACKs (%d BLOCKED early DRAINED); %d advance grants; %d held claims (longest %s); longest exchange %s in %q (window %s)",
		len(r.syscalls), s.obs, r.m.latency, kindsText(r.kinds), s.acks, s.blocked, s.advances, s.held, s.heldWorst.Round(time.Millisecond), s.worst.worst.Round(time.Millisecond), s.worst.name, guestQueryWindow)
	return s
}

// syscallPhases returns the model phases of the replay's syscalls (the
// mounts' bootstrap phases before it are left out).
func (r *t1Replay) syscallPhases() []*modelPhase {
	return r.m.phases[len(r.m.phases)-len(r.syscalls):]
}

// verdict names whether a bound held.
func verdict(held bool) string {
	if held {
		return "held"
	}
	return "exceeded"
}

// kindsText formats recall counts by trigger kind in a stable order.
func kindsText(kinds map[string]int) string {
	if len(kinds) == 0 {
		return "none"
	}
	var parts []string
	for kind, n := range kinds {
		parts = append(parts, fmt.Sprintf("%s %d", kind, n))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// check requires every claim, view exchange and retirement to have ended
// within the guest's window (no WouldBlock past it), every claim TERMINAL
// (finish already fails the run otherwise) and every held claim to have
// resumed within the window.
func (r *t1Replay) check(t *testing.T, s t1Stats) {
	for _, kind := range []string{"claim", "view", "retire"} {
		r.m.checkWaits(t, kind)
	}
	for _, p := range r.m.phases {
		for _, w := range p.waits {
			if w.kind == "claim" && (w.status != "TERMINAL" || w.latency >= guestQueryWindow) {
				t.Errorf("claim not TERMINAL within the %s window: %s", guestQueryWindow, describe(w))
			}
		}
	}
	if s.heldWorst >= guestQueryWindow {
		t.Errorf("a held claim resumed only after %s", s.heldWorst)
	}
}

// t1Schedules are the two cleanup schedules each replay runs under.
var t1Schedules = []struct {
	name string
	late bool
}{{"prompt cleanup", false}, {"late cleanup", true}}

// TestZQ3T1CredentialsReplay replays T1 (admission, the credentials()
// body, R10-1's exit) on the assembled runtime with and without the xf4
// view advance, under both cleanup schedules, and logs each syscall's
// modelled time and recalls. Every wait must end within the guest's window
// and every claim TERMINAL. With advance no expected-target recall may run,
// at most one release recall (a lineage budget, xf4 design "Lineages"), the
// admission must fit guest.py's admission alarm and the whole case T1's
// 240 s budget, both modelled and measured (modelled plus this host's
// work, about 80-120 s here, so the measured check has about 30-70 s of
// slack on this laptop and may fail on a much slower or -race host);
// without advance the expected-target recalls must show (the model still
// exercises what advance removes), and the budget is only logged.
func TestZQ3T1CredentialsReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("replays T1 four times on the assembled runtime")
	}
	t.Parallel()
	for _, advance := range []bool{false, true} {
		for _, schedule := range t1Schedules {
			t.Run(fmt.Sprintf("advance %v, %s", advance, schedule.name), func(t *testing.T) {
				r := newT1Replay(t, advance, schedule.late)
				s := runT1(t, r)
				checkT1Recalls(t, r, s)
			})
		}
	}
}

// checkT1Recalls reports every way the run falls short of t1RecallProblems.
func checkT1Recalls(t *testing.T, r *t1Replay, s t1Stats) {
	for _, problem := range t1RecallProblems(r, s) {
		t.Error(problem)
	}
}

// t1RecallProblems holds the run's recall counts and budgets to what xf4
// promises with advance and requires the baseline to still pay the
// expected-target recalls without it; none means the run holds. Both runs
// start all-domain CALL_CONFLICT recalls with both mounts live, so m0's
// DRAINED, sent in guest order right after its own INSTALLED_DRAIN was
// ACCEPTED, must have met the recall still waiting for m1 at least once
// (BLOCKED, then resent). With advance the guest must have asked (sent) as
// well as been granted: a guest whose views are never cacheless sends
// nothing, and the run must fail as zq3-r20 did (uj4).
func t1RecallProblems(r *t1Replay, s t1Stats) []string {
	var problems []string
	expected, released := r.kinds[viewcontrol.TriggerExpectedTarget], r.kinds[viewcontrol.TriggerRelease]
	if r.kinds[viewcontrol.TriggerCallConflict] == 0 || s.blocked == 0 {
		problems = append(problems, fmt.Sprintf("CALL_CONFLICT recalls %d, BLOCKED early DRAINEDs %d: the guest-order DRAINED was not exercised", r.kinds[viewcontrol.TriggerCallConflict], s.blocked))
	}
	if !r.advance {
		if expected == 0 {
			problems = append(problems, "without advance no EXPECTED_TARGET recall ran: the replay no longer exercises what advance removes")
		}
		return problems // its budget verdicts are only logged (report)
	}
	if expected != 0 || released > 1 || s.advances == 0 || r.sent[0]+r.sent[1] == 0 {
		problems = append(problems, fmt.Sprintf("with advance: EXPECTED_TARGET %d, RELEASE %d recalls, %d advance grants, %v sent; want 0, at most 1, some and some", expected, released, s.advances, r.sent))
	}
	if s.stage["admission"] > t1AdmissionAlarm || s.total > t1RequestBudget || s.measured > t1RequestBudget {
		problems = append(problems, fmt.Sprintf("with advance: admission %s (alarm %s), total %s modelled, %s measured (budget %s)", s.stage["admission"], t1AdmissionAlarm, s.total, s.measured, t1RequestBudget))
	}
	return problems
}

// TestZQ3T1ReplayDetectsNonCachelessGuest replays T1 (prompt cleanup) with
// advance support but zq3-r20's cacheless test, which counts every
// registered inode (guestviewmodel.CachelessR20): with the mount roots, the
// root fds and the leaf fd registered the views are never cacheless, so the
// guest must send no advance, none may be granted, the expected-target
// recalls must come back, and t1RecallProblems must report the run. A
// second run keeps tj4's rule but mmaps an inode in m0's view first: m0
// must never ask for advance, while m1 still does.
func TestZQ3T1ReplayDetectsNonCachelessGuest(t *testing.T) {
	if testing.Short() {
		t.Skip("replays T1 twice on the assembled runtime")
	}
	t.Parallel()
	t.Run("r20 rule", func(t *testing.T) {
		r := newT1Replay(t, true, false)
		r.rule = guestviewmodel.CachelessR20
		s := runT1(t, r)
		if r.sent != [2]int{} || s.advances != 0 || r.kinds[viewcontrol.TriggerExpectedTarget] == 0 {
			t.Errorf("r20 rule: %v advances sent, %d granted, recalls %s; want none, none and EXPECTED_TARGET", r.sent, s.advances, kindsText(r.kinds))
		}
		problems := t1RecallProblems(r, s)
		if len(problems) == 0 {
			t.Fatal("t1RecallProblems passed a guest that never sends advance")
		}
		t.Logf("detected: %v", problems)
	})
	t.Run("m0 mmapped", func(t *testing.T) {
		r := newT1Replay(t, true, false)
		r.views[0].Register("m0-mapped-file")
		r.views[0].Mmap("m0-mapped-file")
		s := runT1(t, r)
		if r.sent[0] != 0 || r.sent[1] == 0 {
			t.Errorf("mmapped m0: advances sent %v (%d granted), want none on m0 and some on m1", r.sent, s.advances)
		}
	})
}

// runT1 runs a prepared replay through admission, body and exit, reports it
// and checks its waits (not its recall counts).
func runT1(t *testing.T, r *t1Replay) t1Stats {
	r.admission()
	r.stage = "body"
	r.body()
	r.exit()
	s := r.report(t)
	r.check(t, s)
	return s
}
