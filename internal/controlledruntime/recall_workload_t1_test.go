//go:build linux || freebsd

package controlledruntime

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// T1 credentials replay in the recall workload model (tasks 094 and 2a4).
//
// zq3-r10 (R10-1) and zq3-r11 (R11-1) failed in T1's admission, and zq3-r19
// (R19-1) spent T1's time in expected-target recalls. internal/viewcontrol
// replays T1 at the observation level (zq3_advance_replay_test.go, with and
// without the xf4 view advance) but pays no observation latency and runs no
// bridge, transport or Facts. This replay drives the same syscalls through
// the assembled runtime (recall_workload_test.go) with the model's slow
// guest observer, so every recall pays its ACK observations and every
// request its admission refresh, and it measures each guest syscall:
// modelled time, slow observations, the recalls it started by trigger kind
// and the claims a recall held (WouldBlock, then o84's QUERY resume).
//
// Guest shapes (guest.py credentials(), patches 0003/0004):
//   - every read RPC (GETATTR, LOOKUP, READ, and since z94 the read-only root
//     OPEN) runs under its own DOMAIN READ observation lease expecting the
//     mount's installed view (hfs_mmap_begin, after hfs_view_enter waits for
//     LIVE); with advance the acquisition carries "advance": true and a
//     grant on a newer cut is adopted (target := granted cut, revision++,
//     hfs_view_advance). Its VIEW_RELEASE goes to the mount's cleanup worker
//     (hfs_mmap_end), which also retires claims and view tickets;
//   - mutations (MKDIR, CREATE, SETATTR, WRITE, closes, FORGET) are direct
//     CALLs. A creating syscall meets the mount's previous observation lease
//     still active (its release queued behind the cleanup worker, follow-up
//     F of xf4), which starts the all-domain CALL_CONFLICT recall that holds
//     the CALL until REOPEN;
//   - dentries never revalidate, so every walk FORGETs the unpinned cached
//     dentry and LOOKUPs again; a LOOKUP of a name without grants journals a
//     handle pin and moves the cut under its own observation lease;
//   - the credential children are folded into guest.py's process (their
//     permission outcomes do not change the view traffic) and chowns are
//     replayed as chmods (the rig's mount credential is no root), exactly as
//     the viewcontrol replay does.
//
// Admission (guest.py admit/open_domains) for each root in order: realpath's
// lstat of the mount point (a GETATTR observation), open(O_RDONLY|
// O_DIRECTORY|O_NOFOLLOW) (OPEN READ under its own observation lease) and
// fstat (a GETATTR observation). Exit replays R10-1's order: m1's root is
// closed normally, m0's root fd leaked, so the process's PROCESS_EXITs and
// m0's final DESCRIPTION_RELEASE go out back to back, the release after
// its issuer's exit (task 194).
//
// Two cleanup schedules bound the race between guest.py and the cleanup
// worker: prompt, where a queued release is sent at once (the worker is
// idle), and late, where it is sent only when the next CALL goes out, i.e.
// after the next syscall's VIEW_ACQUIRE was granted. Late is 094's worst
// case: a release that recalls lands between an observation's grant and
// its CALL, which the recall then holds (WouldBlock, QUERY resume).
//
// Timing. Observations are virtual (newRecallModelVirtual): each costs the
// model latency (2 s by default, HFS_RECALL_MODEL_LATENCY) in modelled time
// only. A syscall's modelled time is its observations plus the guest's idle
// retry sleeps and is deterministic. The test host's own work is measured
// beside it; the run's total is also judged as modelled plus that host
// work (the advance budget check), although the lab's per-request cost is
// not modelled. The model clock (admissionReuse) does not age with
// host work either, so between recalls requests keep reusing one
// observation; the lab refreshes once per five seconds of real time, so its
// observation counts are higher (a lower bound, as in the gate model).

// t1AdmissionAlarm is guest.py's ADMISSION_SECONDS: reading the request and
// the whole admission must end within it (be4; 10 s before, R10-1/R11-1).
const t1AdmissionAlarm = 150 * time.Second

// t1OldAdmissionAlarm is the 10 s admission alarm zq3-r10 and r11 ran with.
const t1OldAdmissionAlarm = 10 * time.Second

// t1RequestBudget is guest.py's REQUEST_SECONDS_MAX, T1's whole deadline
// counted from process start.
const t1RequestBudget = 240 * time.Second

// t1Replay is one run of the T1 replay on a fresh model.
type t1Replay struct {
	m        *recallModel
	p        *modelProcess // guest.py, with its children folded in
	advance  bool
	late     bool
	roots    [2]guestbridge.Capability // the root descriptions admission opened
	dcache   [2]map[string]*t1Dentry
	files    map[string]*t1Dentry // open description token -> pinned dentry
	closes   [2]uint64            // per-mount close and release sequences
	syscalls []*t1Syscall
	stage    string         // admission, body or exit
	sys      *t1Syscall     // the syscall running now
	seen     uint64         // recall history total already counted
	kinds    map[string]int // every counted recall, by trigger kind
}

// t1Dentry is a cached guest dentry: its grant, the open files pinning it
// and whether a walk unhashed it (its grant is FORGOTTEN at the last close).
type t1Dentry struct {
	cap  guestbridge.Capability
	pins int
	dead bool
}

// t1Syscall is one guest syscall's measurement. latency is its modelled
// time (slow observations at the model latency plus idle retry sleeps,
// deterministic) and host the test host's measured work besides (not
// deterministic, logged only). held counts its claims whose first QUERY was
// not TERMINAL (a recall held the CALL: WouldBlock) and heldWorst the
// longest such claim; worst is its longest exchange (recallModel.since).
type t1Syscall struct {
	name, stage   string
	start         waitStart
	latency, host time.Duration
	observations  int32
	kinds         map[string]int
	held          int
	heldWorst     time.Duration
	worst         time.Duration
	worstLabel    string
}

// t1Result is a TERMINAL claim's result.
type t1Result struct {
	Code         string
	Capabilities []guestbridge.Capability
}

// newT1Replay mounts both domains on a virtual-latency model (each
// observation costs the model latency in modelled time only, the
// controller's bounds run on the model clock) and settles, so the replay
// starts where guest.py's T1 starts.
func newT1Replay(t *testing.T, advance, late bool) *t1Replay {
	m := newRecallModelVirtual(t, readZQ3Values(t), recallModelLatency(t))
	m.mount(0)
	m.mount(1)
	m.settle()
	r := &t1Replay{m: m, advance: advance, late: late, files: map[string]*t1Dentry{}, kinds: map[string]int{}}
	r.dcache = [2]map[string]*t1Dentry{{}, {}}
	r.p = m.process("guest.py", nil)
	h, err := m.a.Controller.History(t.Context())
	require(t, err)
	r.seen = h.Total
	return r
}

// syscall runs body as one guest syscall of the current stage and records
// its measurement; recalls are counted when it ends.
func (r *t1Replay) syscall(name string, body func()) {
	m := r.m
	m.begin(name)
	s := &t1Syscall{name: name, stage: r.stage, start: m.startWait(), kinds: map[string]int{}}
	r.sys = s
	body()
	s.latency, s.host = m.split(s.start)
	s.observations = m.probes.Load() - s.start.probe
	r.follow()
	for _, w := range m.phase.waits {
		if w.done && w.latency > s.worst {
			s.worst, s.worstLabel = w.latency, w.label
		}
	}
	r.syscalls = append(r.syscalls, s)
}

// follow counts the recalls started since the last call, by trigger kind,
// into the run and the current syscall. The history ring keeps
// RecallHistoryLimit records; a syscall starts a handful at most.
func (r *t1Replay) follow() {
	h, err := r.m.a.Controller.History(r.m.t.Context())
	require(r.m.t, err)
	fresh := int(h.Total - r.seen)
	if fresh > len(h.Recalls) {
		r.m.t.Fatalf("%d recalls since the last syscall, history ring holds %d", fresh, len(h.Recalls))
	}
	for _, record := range h.Recalls[len(h.Recalls)-fresh:] {
		r.kinds[record.Kind]++
		r.sys.kinds[record.Kind]++
	}
	r.seen = h.Total
}

// acquire takes mount i's observation lease on its installed view. A
// BLOCKED expectation (after its expected-target recall reinstalled the
// view) is retried on the new installed target, at most twice. An advance
// grant on a newer cut is adopted.
func (r *t1Replay) acquire(i int) (*guestbridge.ViewLease, *ticketRec) {
	m, mount := r.m, r.m.mounts[i]
	for range 3 {
		m.enterView(mount)
		w, rec := m.acquireWait(mount, r.p, mount.target, r.advance)
		switch w.status {
		case "GRANTED":
			lease := grantedLease(m.t, w)
			r.adopt(mount, w)
			return lease, rec
		case "BLOCKED":
			continue
		}
		m.t.Fatalf("%s: %s", w.label, w.payload)
	}
	m.t.Fatalf("m%d observation: no grant after two retries", i)
	return nil, nil
}

// adopt installs a granted lease's target when it differs from the mount's
// installed one (an advance grant): hfs_view_advance switches the target
// and bumps the view revision; the fence does not move.
func (r *t1Replay) adopt(mount *modelMount, w *guestWait) {
	var granted struct {
		Lease struct{ Target json.RawMessage }
	}
	require(r.m.t, json.Unmarshal(w.payload, &granted))
	var installed, target guestbridge.ViewTarget
	require(r.m.t, json.Unmarshal(mount.target, &installed))
	require(r.m.t, json.Unmarshal(granted.Lease.Target, &target))
	if !sameJSON(installed, target) {
		mount.target = granted.Lease.Target
		mount.revision++
	}
}

// release hands the lease's VIEW_RELEASE to mount i's cleanup worker, which
// sends it at once (prompt schedule) or when the next CALL goes out (late).
func (r *t1Replay) release(i int, lease *guestbridge.ViewLease, acquired *ticketRec) {
	m, mount := r.m, r.m.mounts[i]
	w := m.register(m.releaseWait(mount, r.p, *lease, acquired))
	mount.jobs = append(mount.jobs, func() *guestWait { m.sendRelease(w); return w })
	if !r.late {
		m.dispatchCleanup()
	}
}

// send starts one claim of guest.py on mount i (the late schedule's cleanup
// worker sends its queued release first) and counts it as held when its
// first QUERY was not TERMINAL.
func (r *t1Replay) send(i int, op string, args map[string]any) *guestWait {
	if r.late {
		r.m.dispatchCleanup()
	}
	w := r.m.claim(r.m.mounts[i], r.p, op, args)
	if !w.done {
		r.sys.held++
	}
	return w
}

// finish waits for claim w and returns its result; a claim that did not end
// TERMINAL fails the run (the guest would block its session).
func (r *t1Replay) finish(w *guestWait, held bool) t1Result {
	r.m.await(w)
	if w.expired || w.status != "TERMINAL" {
		r.m.t.Fatalf("%s: %s after %s", w.label, w.status, w.latency)
	}
	if held && w.latency > r.sys.heldWorst {
		r.sys.heldWorst = w.latency
	}
	var res t1Result
	require(r.m.t, json.Unmarshal(claimResult(r.m.t, w), &res))
	return res
}

// do runs one CALL to its end.
func (r *t1Replay) do(i int, op string, args map[string]any) t1Result {
	before := r.sys.held
	w := r.send(i, op, args)
	return r.finish(w, r.sys.held > before)
}

// observe runs one read RPC under its own observation lease.
func (r *t1Replay) observe(i int, op string, args map[string]any) t1Result {
	lease, rec := r.acquire(i)
	res := r.do(i, op, args)
	r.release(i, lease, rec)
	return res
}

// t1Name is a name's wire encoding.
func t1Name(s string) string { return hex.EncodeToString([]byte(s)) }

// t1Key keys a cached dentry by its parent's allocation and name.
func t1Key(dir guestbridge.Capability, n string) string { return dir.Allocation + "/" + n }

// t1Flags are OPEN / CREATE flags; create also makes the open exclusive.
func t1Flags(access string, create bool) map[string]any {
	return map[string]any{"access": access, "append": false, "create": create, "exclusive": create, "sync": "ORDINARY", "truncate": false}
}

// t1Label names a syscall after its mount when it is not guest.py's m0.
func t1Label(i int, format string, args ...any) string {
	if i == 0 {
		return fmt.Sprintf(format, args...)
	}
	return fmt.Sprintf("m%d ", i) + fmt.Sprintf(format, args...)
}
