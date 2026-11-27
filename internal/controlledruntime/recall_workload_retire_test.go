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
// km4 made the refusal an answer: BLOCKED with the floor unchanged, which
// the guest records (view->retire_blocked) and offers again later (re4),
// instead of a cell that is never answered.
//
// gn4 (zq3-r24 R24-1, docs/lab/results/20261127-zq3-r24/README.md): the
// physical check is now per credentials, as the controller's own
// retirementBlocked already was, so child 1's families retire at once
// beside the holder's lease. In r24 the domain-wide refusal kept 12
// released VIEW_RELEASE tickets of three other credential sets charged
// (about 321 MB of m0's 512 MiB guest control lane) while the 1024-group
// child held its own observation lease, and that child's next release
// ticket charge failed ENOSPC. The holder's own families still wait for
// its release.
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
	m.begin("R22-1/R24-1: m0 child retires beside a held observation")
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
	m.requireRetires(t, "RETIRED")
	family := mount.familyIndex[child.name+"/acquire"]
	if family.floor != 1 {
		t.Errorf("child 1's acquisition floor %d beside the holder's lease, want 1", family.floor)
	}
	holderFamily := mount.familyIndex[holder.name+"/acquire"]
	if holderFamily.floor != 0 {
		t.Errorf("holder's acquisition floor %d while its lease is active, want 0", holderFamily.floor)
	}

	m.begin("holder releases; its cleanup retires its own families")
	m.queueRelease(mount, holder, *held, heldTicket)
	m.settle()
	m.requireRetires(t, "RETIRED")
	if holderFamily.floor != 1 {
		t.Errorf("holder's acquisition floor %d with no lease left, want 1", holderFamily.floor)
	}
}

// TestZQ3HolderRetiresBesideItsNextObservation replays zq3-r25 R25-1 (task
// hp4, docs/lab/results/20261127-zq3-r25/README.md) in the recall workload
// model.
//
// The T1 process ran its observations back to back. The guest's cleanup
// worker releases an observation's lease only after hfs_mmap_end, so the
// next observation of the same process usually acquired first: 8 of its
// last 13 acquisitions shared the previous, still active lease. When the
// worker then released the older holder and sent its VIEW_RETIREs, a lease
// of T1's own credentials was always active, and gn4's per-credential rule
// answered both of T1's families BLOCKED. 13 answered VIEW_RELEASE tickets
// (about 25.5 MiB each in the guest) filled m0's control lane and the
// retention inventory's lstat failed ENOSPC.
//
// hp4 makes the rule per ticket: the answered release retires at once. The
// first acquisition still names the shared lease, which its next holder
// keeps, so its family waits until that holder released too.
func TestZQ3HolderRetiresBesideItsNextObservation(t *testing.T) {
	if testing.Short() {
		t.Skip("assembles the runtime and replays two mount(8)s")
	}
	t.Parallel()
	m := newRecallModelVirtual(t, readZQ3Values(t), recallModelLatency(t))
	m.mount(0)
	m.mount(1)
	m.settle()
	mount := m.mounts[0]
	m.begin("R25-1: T1's next observation acquires before the last one is released")
	t1 := m.process("T1", nil)
	m.enterView(mount)
	first, firstTicket := m.lease(mount, t1, mount.target)
	next, nextTicket := m.lease(mount, t1, mount.target)
	if first == nil || next == nil {
		t.FailNow()
	}
	if first.ID != next.ID {
		t.Fatalf("the next observation got lease %s, want the shared %s", next.ID, first.ID)
	}
	m.queueRelease(mount, t1, *first, firstTicket)
	m.settle()
	acquire, release := mount.familyIndex[t1.name+"/acquire"], mount.familyIndex[t1.name+"/control"]
	if release == nil {
		t.Fatal("T1's release family missing")
	}
	if release.floor != 1 {
		t.Fatalf("T1's release floor %d beside its held observation, want 1 (its answered release)", release.floor)
	}
	if acquire.floor != 0 {
		t.Errorf("T1's acquisition floor %d while its first ticket's shared lease is held, want 0", acquire.floor)
	}

	m.begin("the next observation's release retires the rest")
	m.queueRelease(mount, t1, *next, nextTicket)
	m.settle()
	m.requireRetires(t, "RETIRED")
	if acquire.floor != 2 || release.floor != 2 {
		t.Errorf("T1's floors %d (acquisition) and %d (release) with no lease left, want 2 and 2", acquire.floor, release.floor)
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
