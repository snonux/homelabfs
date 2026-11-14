//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
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
	lease, acquired := m.lease(mount, child, mount.target)
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
