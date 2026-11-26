//go:build linux || freebsd

package controlledruntime

import (
	"strings"
	"testing"
)

// TestZQ3ChildRetireBesideHeldObservation replays zq3-r22 R22-1 (task km4,
// docs/lab/results/20261126-zq3-r22/README.md) in the recall workload model.
//
// T1's access child 1 (its own credentials, hence its own ticket families)
// read a file on m0 under DOMAIN READ observation leases and exited. m0's
// cleanup worker released those leases (RELEASED at once, task je4), then
// sent hfs_retire's RETIRE and hfs_view_retire's VIEW_RETIRE for child 1's
// families. Meanwhile other processes on m0 held active observation leases
// (child 2's, then the creating process's). The view controller's own check
// passed (child 1's credentials held no active lease, retirementBlocked),
// but Facts.CheckPhysical refused the retirement domain-wide with WouldBlock
// (facts.go checkRetire). The transport retries WouldBlock in its pending
// ring without answering the cell, so the guest's VIEW_RETIRE fetched AGAIN
// until its 30 s deadline (-110) and blocked m0, and the CREATE waiting for
// session->retirement behind it failed ENOSPC after 33 s. The owner
// snapshot taken at the failure OBSERVE holds that VIEW_RETIRE (host cell
// 16030) still "executing", i.e. retried, and three active m0 leases.
//
// The refusal must be an answer: BLOCKED with the floor unchanged, which
// the guest records (view->retire_blocked) and offers again later (re4),
// instead of a cell that is never answered. Once the holder's lease is
// released too, the next cleanup's VIEW_RETIRE retires child 1's families.
func TestZQ3ChildRetireBesideHeldObservation(t *testing.T) {
	if testing.Short() {
		t.Skip("assembles the runtime and replays two mount(8)s")
	}
	t.Parallel()
	m := newRecallModelVirtual(t, readZQ3Values(t), recallModelLatency(t))
	m.mount(0)
	m.mount(1)
	m.settle()
	mount := m.mounts[0]
	m.begin("R22-1: m0 child retires beside a held observation")
	holder := m.process("holder", nil) // child 2's / the creating process's lease
	m.enterView(mount)
	held, heldTicket := m.lease(mount, holder, mount.target)
	if held == nil {
		t.FailNow()
	}
	// Access child 1: one observation lease and its GETATTR, then the
	// release on the cleanup worker, which retires afterwards. Its
	// PROCESS_EXIT is left out: it is asynchronous in the guest and the
	// model does not order it after the queued release.
	child := m.process("access child 1", nil)
	lease, acquired := m.lease(mount, child, mount.target)
	if lease == nil {
		t.FailNow()
	}
	m.await(m.claim(mount, child, "GETATTR", map[string]any{"object": mount.root}))
	m.queueRelease(mount, child, *lease, acquired)
	m.settle()
	m.requireRetires(t, "BLOCKED")
	family := mount.familyIndex[child.name+"/acquire"]
	if family.floor != 0 {
		t.Errorf("child 1's acquisition floor %d after BLOCKED, want 0", family.floor)
	}

	m.begin("holder releases; its cleanup offers child 1's families again")
	m.queueRelease(mount, holder, *held, heldTicket)
	m.settle()
	m.requireRetires(t, "RETIRED")
	if family.floor != 1 {
		t.Errorf("child 1's acquisition floor %d with no lease left, want 1", family.floor)
	}
}

// requireRetires requires every wait of the current step to have ended in
// time and each of its VIEW_RETIREs (at least one) to have answered status.
func (m *recallModel) requireRetires(t *testing.T, status string) {
	t.Helper()
	retires := 0
	for _, w := range m.phase.waits {
		if w.expired || !w.done {
			t.Errorf("%s: %s", m.phase.name, describe(w))
		}
		if strings.HasSuffix(w.label, " VIEW_RETIRE") {
			retires++
			if w.status != status {
				t.Errorf("%s: %s answered %s, want %s", m.phase.name, w.label, w.status, status)
			}
		}
	}
	if retires == 0 {
		t.Fatalf("%s: the cleanup sent no VIEW_RETIRE", m.phase.name)
	}
}
