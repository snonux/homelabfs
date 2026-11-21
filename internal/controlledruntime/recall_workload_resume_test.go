//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestZQ3R9PressureReleaseParksGetattr extends the recall workload model
// (task 984, recall_workload_test.go) with zq3-r9's final usable probe on m0
// (docs/lab/results/20261114-zq3-r9/README.md "Root cause", R9-1). The 984
// gate releases a probe's lease only after its GETATTR is TERMINAL, so every
// modelled CALL was admitted on its first try and QUERY resume never ran.
//
// Here the guest's transport pressure path (patch 0003 hfs_transport ->
// hfs_mmap_progress -> 0004 hfs_mmap_pressure, after a normal-lane exchange
// waits more than 50 ms) releases the lstat child's own observation lease
// while its GETATTR is in flight. The model sends that VIEW_RELEASE just
// before the CALL, so the release's scoped READ recall is running when the
// CALL is admitted: Admit returns WouldBlock (recallHoldsCall), the bridge
// answers ADMITTED and the claim stays RESERVED/UNPREPARED. From then on only
// the guest's QUERYs can resume it. Before task o84 every resume was refused
// with PERMISSION (the QUERY continuation covered only DESCRIPTION_RELEASE
// and FORGET) and the claim stayed INDETERMINATE until the 30 s window ended.
// It must now reach TERMINAL inside the window.
//
// Since task je4 the release of a guest observation lease (acquired with
// expected_target = the installed view) needs no recall, and since p84 the
// guest's pressure path never releases an observation lease at all: it
// releases a published cache-page lease, whose release still recalls. The
// model's lease here is therefore acquired with target:null, the shape whose
// release keeps the scoped recall, so the CALL is still held behind a
// recall and QUERY resume is still exercised.
//
// The recall moves the view under the GETATTR (fence and revision 2 -> 3),
// so the actual guest would still answer ESTALE from hfs_attr_install even
// with a TERMINAL OK claim. That is the guest pressure path's bug (it must
// not release a lease whose observation still has a request in flight, task
// p84), not the host's; this test only logs the move.
func TestZQ3R9PressureReleaseParksGetattr(t *testing.T) {
	if testing.Short() {
		t.Skip("pays a recall of modelled guest-observation latency")
	}
	t.Parallel()
	m := newRecallModel(t, readZQ3Values(t), recallModelLatency(t))
	m.mount(0)
	m.mount(1)
	m.settle()
	m.begin("r9 pressure m0 lstat")
	mount := m.mounts[0]
	child := m.process("lstat child pressure", nil)
	m.enterView(mount)
	fence, revision := mount.fence, mount.revision
	lease, acquired := m.lease(mount, child, nil) // keeps the recall (je4)
	if lease == nil {
		t.Fatal("observation lease refused")
	}
	release := m.register(m.releaseWait(mount, child, *lease, acquired))
	m.sendRelease(release)
	if !m.recallActive() {
		t.Fatalf("pressure VIEW_RELEASE started no recall (%s): the CALL would not be held", release.status)
	}
	getattr := m.claim(mount, child, "GETATTR", map[string]any{"object": mount.root})
	if getattr.done {
		t.Fatalf("GETATTR was admitted at once (%s): the model no longer exercises QUERY resume", getattr.status)
	}
	m.await(release)
	m.await(getattr)
	t.Logf("release %s after %s; GETATTR %s after %s (%d observations)", release.status, release.latency, getattr.status, getattr.latency, getattr.observations)
	t.Logf("view under GETATTR: fence %d->%d, revision %d->%d (guest ESTALE, task p84)", fence, mount.fence, revision, mount.revision)
	if getattr.expired || getattr.status != "TERMINAL" {
		t.Fatalf("GETATTR claim parked: %s after %s (last reply %s)", getattr.status, getattr.latency, getattr.payload)
	}
	var result struct{ Code string }
	if err := json.Unmarshal(claimResult(t, getattr), &result); err != nil || result.Code != "OK" {
		t.Errorf("resumed GETATTR result %s: %v", claimResult(t, getattr), err)
	}
}

// TestZQ3R10CredentialsAdmission replays T1's admission and R10-1's exit
// (task 094, zq3-r10 R10-1 and zq3-r11 R11-1; recall_workload_t1_test.go)
// on the assembled runtime, with and without the xf4 view advance, under
// the late cleanup schedule: each observation's VIEW_RELEASE goes out only
// once the next syscall's VIEW_ACQUIRE was granted, just before its CALL.
//
// In zq3-r10 the lstat GETATTR's observation release recalled the domain
// (sometimes both), the root OPEN was CALLed while that recall ran, parked
// (WouldBlock) and was resumed by QUERY to TERMINAL, but the admission took
// 16.9 s against guest.py's 10 s alarm; then PROCESS_EXIT and the leaked
// root's DESCRIPTION_RELEASE went out back to back. In zq3-r11 the root
// OPEN started an all-domain recall itself. Since je4 an observation
// release on an unmoved cut needs no recall, since z94 a read-only OPEN
// conflicts like a read, and since xf4 an Advance lease's release skips the
// recall although the OPEN's handle pin moved the cut. The test logs each
// admission syscall's modelled time and recalls against the 150 s
// admission alarm (and the old 10 s one) and requires every claim TERMINAL
// within the guest's window, the admission within the alarm, the exit's
// back-to-back PROCESS_EXIT and DESCRIPTION_RELEASE OK (finish, exit), and
// with advance an admission without any recall.
func TestZQ3R10CredentialsAdmission(t *testing.T) {
	if testing.Short() {
		t.Skip("replays T1's admission twice on the assembled runtime")
	}
	t.Parallel()
	for _, advance := range []bool{false, true} {
		t.Run(fmt.Sprintf("advance %v", advance), func(t *testing.T) {
			r := newT1Replay(t, advance, true)
			r.admission()
			r.exit()
			s := r.report(t)
			r.check(t, s)
			if s.stage["admission"] > t1AdmissionAlarm {
				t.Errorf("admission took %s, alarm %s", s.stage["admission"], t1AdmissionAlarm)
			}
			if recalls := r.stageRecalls("admission"); advance && len(recalls) != 0 {
				t.Errorf("with advance the admission started recalls: %s", kindsText(recalls))
			}
		})
	}
}

// stageRecalls sums the recalls the syscalls of stage started, by kind.
func (r *t1Replay) stageRecalls(stage string) map[string]int {
	recalls := map[string]int{}
	for _, c := range r.syscalls {
		if c.stage != stage {
			continue
		}
		for kind, n := range c.kinds {
			recalls[kind] += n
		}
	}
	return recalls
}
