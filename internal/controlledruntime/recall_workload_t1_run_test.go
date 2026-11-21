//go:build linux || freebsd

package controlledruntime

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

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
		s.held += c.held
		s.heldWorst = max(s.heldWorst, c.heldWorst)
		if s.worst == nil || c.worst > s.worst.worst {
			s.worst = c
		}
	}
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
// and T1's budget. The verdicts use the modelled time; this host's work is
// logged beside it (the lab's per-request cost is not modelled).
func (r *t1Replay) report(t *testing.T) t1Stats {
	for _, c := range r.syscalls {
		held := ""
		if c.held > 0 {
			held = fmt.Sprintf(", %d held (WouldBlock) up to %s", c.held, c.heldWorst.Round(time.Millisecond))
		}
		t.Logf("  %-9s %-52s %7s (+%6s host) %2d obs, recalls %-18s longest %s%s", c.stage, c.name, c.latency.Round(time.Millisecond), c.host.Round(time.Millisecond), c.observations, kindsText(c.kinds), c.worst.Round(time.Millisecond), held)
	}
	s := r.stats()
	t.Logf("modelled: admission %s (alarm %s; the old %s alarm %s), body %s, exit %s; total %s of T1's %s budget (this host's work: +%s)",
		s.stage["admission"].Round(time.Millisecond), t1AdmissionAlarm, t1OldAdmissionAlarm, verdict(s.stage["admission"] <= t1OldAdmissionAlarm),
		s.stage["body"].Round(time.Millisecond), s.stage["exit"].Round(time.Millisecond), s.total.Round(time.Millisecond), t1RequestBudget, s.host.Round(time.Millisecond))
	t.Logf("%d syscalls, %d slow observations at %s; recalls %s; %d VIEW_ACKs (%d BLOCKED early DRAINED); %d advance grants; %d held claims (longest %s); longest exchange %s in %q (window %s)",
		len(r.syscalls), r.m.probes.Load(), r.m.latency, kindsText(r.kinds), s.acks, s.blocked, s.advances, s.held, s.heldWorst.Round(time.Millisecond), s.worst.worst.Round(time.Millisecond), s.worst.name, guestQueryWindow)
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
// 240 s budget; without advance the expected-target recalls must show
// (the model still exercises what advance removes), and the budget is only
// logged.
func TestZQ3T1CredentialsReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("replays T1 four times on the assembled runtime")
	}
	t.Parallel()
	for _, advance := range []bool{false, true} {
		for _, schedule := range t1Schedules {
			t.Run(fmt.Sprintf("advance %v, %s", advance, schedule.name), func(t *testing.T) {
				r := newT1Replay(t, advance, schedule.late)
				r.admission()
				r.stage = "body"
				r.body()
				r.exit()
				s := r.report(t)
				r.check(t, s)
				checkT1Recalls(t, r, s)
			})
		}
	}
}

// checkT1Recalls holds the run's recall counts and budgets to what xf4
// promises with advance and requires the baseline to still pay the
// expected-target recalls without it. Both runs start all-domain
// CALL_CONFLICT recalls with both mounts live, so m0's DRAINED, sent in
// guest order right after its own INSTALLED_DRAIN was ACCEPTED, must have
// met the recall still waiting for m1 at least once (BLOCKED, then resent).
func checkT1Recalls(t *testing.T, r *t1Replay, s t1Stats) {
	expected, released := r.kinds[viewcontrol.TriggerExpectedTarget], r.kinds[viewcontrol.TriggerRelease]
	if r.kinds[viewcontrol.TriggerCallConflict] == 0 || s.blocked == 0 {
		t.Errorf("CALL_CONFLICT recalls %d, BLOCKED early DRAINEDs %d: the guest-order DRAINED was not exercised", r.kinds[viewcontrol.TriggerCallConflict], s.blocked)
	}
	if !r.advance {
		if expected == 0 {
			t.Errorf("without advance no EXPECTED_TARGET recall ran: the replay no longer exercises what advance removes")
		}
		t.Logf("without advance: total %s, T1 budget %s %s", s.total.Round(time.Millisecond), t1RequestBudget, verdict(s.total <= t1RequestBudget))
		return
	}
	if expected != 0 || released > 1 || s.advances == 0 {
		t.Errorf("with advance: EXPECTED_TARGET %d, RELEASE %d recalls, %d advance grants; want 0, at most 1 and some", expected, released, s.advances)
	}
	if s.stage["admission"] > t1AdmissionAlarm || s.total > t1RequestBudget {
		t.Errorf("with advance: admission %s (alarm %s), total %s (budget %s)", s.stage["admission"], t1AdmissionAlarm, s.total, t1RequestBudget)
	}
}
