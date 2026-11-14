//go:build linux || freebsd

package controlledruntime

import (
	"fmt"
	"testing"
	"time"
)

// Report and checks of the recall workload model (recall_workload_test.go).

// report logs, per workload step, the recalls its release started (by kind)
// with their VIEW_ACKs and slow observations, and every wait it sent with
// its modelled and wall latency.
func (m *recallModel) report() {
	m.t.Helper()
	var ackObservations int32
	for _, p := range m.phases {
		kinds := m.recallKinds(p.recalls)
		ackObservations += p.ackObservations
		m.t.Logf("%-20s recalls scoped %d, all-domain %d, single %d; VIEW_ACKs %d (%d BLOCKED); slow observations %d; worst wait %s (%s)",
			p.name, kinds["scoped"], kinds["all-domain"], kinds["single"], p.acks, p.blocked, p.ackObservations, p.worst.Round(time.Millisecond), p.worstLabel)
		for _, w := range p.waits {
			m.t.Logf("    %-44s %-8s %8s (wall %8s), %2d observations", w.label, w.status, w.latency.Round(time.Millisecond), w.wall.Round(time.Millisecond), w.observations)
		}
	}
	kinds := m.recallKinds(m.order)
	total := m.probes.Load()
	m.t.Logf("total: %d recalls (scoped %d, all-domain %d, single %d), %d accepted ACKs, %d slow observations (%d for VIEW_ACKs, %d refreshes for other requests)", len(m.order), kinds["scoped"], kinds["all-domain"], kinds["single"], m.acks, total, ackObservations, total-ackObservations)
}

// recallKinds counts the recalls ids by kind.
func (m *recallModel) recallKinds(ids []string) map[string]int {
	kinds := map[string]int{}
	for _, id := range ids {
		kinds[m.recalls[id].kind()]++
	}
	return kinds
}

// lateWaits returns the waits of kind that never ended (never sent, or
// still pending) or that the guest gave up on. A wait that ended with a
// terminal reply counts as in time even past 30 s, as the guest accepts a
// terminal reply to a query it sent before the deadline.
func (m *recallModel) lateWaits(kind string) []*guestWait {
	var late []*guestWait
	for _, p := range m.phases {
		for _, w := range p.waits {
			if w.kind == kind && (!w.done || w.expired) {
				late = append(late, w)
			}
		}
	}
	return late
}

// describe formats a wait for a failure or log line.
func describe(w *guestWait) string {
	return fmt.Sprintf("%s %s after %s (%d observations)", w.label, w.status, w.latency.Round(time.Millisecond), w.observations)
}

// checkWaits requires every wait of kind to have ended in time and to pass
// checkObservations.
func (m *recallModel) checkWaits(t *testing.T, kind string) {
	for _, w := range m.lateWaits(kind) {
		t.Errorf("not terminal within the guest's %s window: %s", guestQueryWindow, describe(w))
	}
	m.checkObservations(t, kind, false)
}

// checkObservations is the deterministic check: each wait's slow
// observations at the model latency plus hostOverheadAllowance must fit the
// guest window, so the verdict does not depend on how fast the test host is.
// completed limits it to waits that ended with a terminal reply.
func (m *recallModel) checkObservations(t *testing.T, kind string, completed bool) {
	for _, p := range m.phases {
		for _, w := range p.waits {
			if w.kind != kind || (completed && (!w.done || w.expired)) {
				continue
			}
			if paid := time.Duration(w.observations)*m.latency + hostOverheadAllowance; paid >= guestQueryWindow {
				t.Errorf("%s: %d observations at %s plus %s host allowance = %s, not within the guest's %s window", w.label, w.observations, m.latency, hostOverheadAllowance, paid, guestQueryWindow)
			}
		}
	}
}

// checkSettle requires round 1 of the gate to be quiet, measured from
// mount-1's end, before bootstrap.py's fixed settle (usableSettle) ends,
// less readyLag for the READY record and SSH lag the model does not replay.
func (m *recallModel) checkSettle(t *testing.T) {
	limit := usableSettle - readyLag
	t.Logf("round 1 quiet %s after mount-1's end; settle allows %s (USABLE_SETTLE_SECONDS %s - %s READY/SSH lag), slack %s", m.round1.Round(time.Millisecond), limit, usableSettle, readyLag, (limit - m.round1).Round(time.Millisecond))
	if m.round1 < 0 || m.round1 > limit {
		t.Errorf("round 1 not quiet within the gate's settle: %s > %s; the final probe would overlap its cleanup", m.round1.Round(time.Millisecond), limit)
	}
}

// checkViews requires that no probe's view moved (fence or revision) between
// its target capture and its GETATTR's end.
func (m *recallModel) checkViews(t *testing.T) {
	for _, move := range m.viewMoves {
		t.Errorf("view moved under the probe (guest ESTALE): %s", move)
	}
}

// checkBudgets reports the controller-lifetime budgets that recalls and
// leases spend at the zq3 values and requires the modelled workload to use at
// most a quarter of the recall budgets (task f84 angle). beginRecall
// (viewcontrol recall.go) starts a recall only while AckArchive rows + 8 per
// domain stay within Identities*8; every accepted first ACK adds a row (5 per
// participant per recall) and nothing removes rows. Every recall also spends
// one of 2*Identities ledger UUIDs, which dirty reservations share (their
// provenance, viewcontrol tickets.go reserve). Every view ticket
// (VIEW_ACQUIRE, VIEW_RELEASE) spends one of Identities. None of the three is
// recycled (q64); the ticket budget is logged as the run blocker it is.
func (m *recallModel) checkBudgets(t *testing.T, v zq3Values) {
	domains := len(m.mounts)
	rows := v.Views.Identities * 8
	headroom := rows - 8*domains
	allDomain := headroom/(5*domains) + 1
	scoped := min(headroom/5+1, 2*v.Views.Identities)
	uuids := 2 * v.Views.Identities
	t.Logf("AckArchive: %d of %d rows; recall budget per controller lifetime: %d all-domain or %d scoped recalls (recall UUIDs: %d used of %d, shared with dirty-reservation provenance)", m.acks, rows, allDomain, scoped, len(m.order), uuids)
	if m.acks*4 > headroom || len(m.order)*4 > uuids {
		t.Errorf("modelled bootstrap and k64 gate use more than a quarter of a recall budget: %d AckArchive rows of %d, %d recalls of %d UUIDs", m.acks, headroom, len(m.order), uuids)
	}
	left := v.Views.Identities - m.tickets
	t.Logf("RUN BLOCKER (q64): bootstrap and the k64 gate already spend %d of %d Views.Identities view tickets; every hfs_begin(mutation=false) (GETATTR, LOOKUP, READLINK, STATFS, and hfs_permission's hfs_refresh) spends 2 more and one scoped recall, so only about %d lease-taking reads remain for T1-T11 (o64 models hundreds)", m.tickets, v.Views.Identities, left/2)
}
