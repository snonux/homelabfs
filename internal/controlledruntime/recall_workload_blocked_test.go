//go:build linux || freebsd

package controlledruntime

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// TestZQ3BlockedDomainFencedPeerReopens is the recall workload model's
// blocked-domain case (task 0a4, zq3-r11 R11-2; asked for by 2a4). After
// both mounts' bootstrap, each holds a targeted observation lease and m1
// CREATEs under them, which starts an all-domain recall. m1's guest
// acknowledges STOPPED and then sends nothing more (its view worker stopped,
// or the guest hung): it never acknowledges INSTALL_DRAIN. Before 0a4 the
// recall waited for m1 forever and m0 never reopened.
//
// m1 stopped, holds no active lease and nothing undrained, so the controller
// fences it StalledParticipantBound after INSTALL_DRAIN began and m0
// reopens alone. Meanwhile m0's DRAINED, sent right after its own
// INSTALLED_DRAIN as the guest does, stays BLOCKED waiting for the fence
// instead of failing its view worker after BlockedACKBound. Afterwards m1's
// VIEW_WAIT gets an error reply, only m0's fence advanced, and an m0 lstat
// probe completes within the guest window. Then the operator retires m1
// (task fh4): m1 stays refused and a second m0 lstat probe, whose release
// and retirements pass the real Facts with the retired ledger, completes.
//
// The run is assembled with the real runtime Facts, whose recall
// participant check must leave the fenced session out, and it runs the
// controller on the model clock: while m1 is silent each round adds two
// seconds of modelled guest time, so the 60 s bound costs no 60 s of wall
// time; the observations still pay the model latency.
func TestZQ3BlockedDomainFencedPeerReopens(t *testing.T) {
	if testing.Short() {
		t.Skip("pays a recall of modelled guest-observation latency")
	}
	t.Parallel()
	m := newRecallModelWith(t, readZQ3Values(t), recallModelLatency(t), true)
	m.mount(0)
	m.mount(1)
	m.settle()
	m0, m1 := m.mounts[0], m.mounts[1]
	fence0, fence1 := m0.fence, m1.fence
	m.begin("blocked domain: m1 CREATE under both observation leases")
	m.holdObservation(m0)
	creator := m.holdObservation(m1)
	m1.silentAfter = "STOPPED"
	create := m.claim(m1, creator, "CREATE", map[string]any{"parent": m1.root, "name": hex.EncodeToString([]byte("blocked")), "mode": 0o660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	if create.done || !m.recallActive() {
		t.Fatalf("m1 CREATE under both leases started no recall: %s", create.status)
	}
	elapsed := m.awaitBlockedRecall(m1)
	t.Logf("recall ended %s (modelled) after it started; m1 silent after STOPPED; bound %s", elapsed.Round(time.Millisecond), viewcontrol.StalledParticipantBound)
	if elapsed < viewcontrol.StalledParticipantBound || elapsed > viewcontrol.StalledParticipantBound+guestQueryWindow {
		t.Errorf("recall ended after %s, want within [%s, %s]", elapsed, viewcontrol.StalledParticipantBound, viewcontrol.StalledParticipantBound+guestQueryWindow)
	}
	if m0.fence != fence0+1 || m1.fence != fence1 {
		t.Errorf("fences m0 %d->%d, m1 %d->%d; want only m0 advanced", fence0, m0.fence, fence1, m1.fence)
	}
	m.requireBlockedFence(m1)
	m.requireFencedWait(m1)
	m.begin("m0 lstat after the fence")
	m.usableProbe(0)
	m.settle()
	for _, w := range m.phase.waits {
		if w.expired || !w.done {
			t.Errorf("after the fence: %s", describe(w))
		}
	}
	m.retireFenced(m1)
	m.begin("m0 lstat after the retirement")
	m.usableProbe(0)
	m.settle()
	for _, w := range m.phase.waits {
		if w.expired || !w.done {
			t.Errorf("after the retirement: %s", describe(w))
		}
	}
}

// retireFenced is the operator retiring mount's fenced session (task fh4,
// viewcontrol fenced_retirement.go) with the enrollment's operator key.
// The retired session stays refused at the controller's ingress gate; the
// survivor's later VIEW_RELEASE, RETIRE and VIEW_RETIRE pass the real
// runtime Facts with the resulting ledger, which must accept the tombstone.
// (A second VIEW_WAIT of the fenced mount is not sent: the model's chain
// does not survive the first refused one.)
func (m *recallModel) retireFenced(mount *modelMount) {
	session := mount.peer.scope.Session
	defer func() {
		if err := m.a.Controller.CheckRequest(m.t.Context(), mount.peer.scope, "VIEW_POLL", json.RawMessage(`{}`)); !errors.Is(err, viewcontrol.ErrSessionFenced) {
			m.t.Errorf("retired m%d request: %v, want ErrSessionFenced", mount.index, err)
		}
	}()
	statement, err := m.a.Controller.FencedRetirementStatement(session)
	require(m.t, err)
	signature, err := viewcontrol.SignFencedRetirement(m.r.artifactOperator, statement)
	require(m.t, err)
	require(m.t, m.a.Controller.RetireFenced(m.t.Context(), statement, signature))
	evidence, err := m.a.Controller.LedgerEvidence(m.t.Context())
	require(m.t, err)
	if evidence.Fenced != 1 || evidence.RetiredFenced != 1 {
		m.t.Errorf("ledger evidence after the retirement %+v", evidence)
	}
}

// holdObservation acquires a targeted DOMAIN READ observation lease on
// mount's installed view for a new process and keeps it (never released).
func (m *recallModel) holdObservation(mount *modelMount) *modelProcess {
	p := m.process(fmt.Sprintf("m%d holder", mount.index), nil)
	m.enterView(mount)
	if lease, _ := m.lease(mount, p, mount.target); lease == nil {
		m.t.Fatalf("m%d observation lease refused", mount.index)
	}
	return p
}

// awaitBlockedRecall runs the scheduler until the recall ends and returns
// its modelled duration. Once silent stopped, each round adds two seconds
// of modelled time: the guests keep polling, nothing else happens.
func (m *recallModel) awaitBlockedRecall(silent *modelMount) time.Duration {
	start := m.startWait()
	deadline := time.Now().Add(modelDeadline)
	for m.recallActive() {
		if time.Now().After(deadline) {
			m.t.Fatalf("model deadline: recall still waiting (m%d silent %v)", silent.index, silent.silent)
		}
		m.step()
		if silent.silent {
			m.clock.advance(2 * time.Second)
			m.idle += 2 * time.Second
		}
	}
	return m.since(start)
}

// requireBlockedFence requires the controller to report one fenced session
// and the newest recall history record to be a completed all-domain recall
// that fenced mount's session in INSTALL_DRAIN.
func (m *recallModel) requireBlockedFence(mount *modelMount) {
	evidence, err := m.a.Controller.LedgerEvidence(m.t.Context())
	require(m.t, err)
	if evidence.Fenced != 1 || evidence.Recall {
		m.t.Errorf("ledger evidence %+v, want one fenced session and no recall", evidence)
	}
	history, err := m.a.Controller.History(m.t.Context())
	require(m.t, err)
	last := history.Recalls[len(history.Recalls)-1]
	m.t.Logf("recall %s %s %s: %d ACKs, fenced %+v", last.Kind, last.Scope, last.Outcome, len(last.Acks), last.Fenced)
	if last.Scope != "ALL" || last.Outcome != viewcontrol.RecallCompleted || len(last.Fenced) != 1 || last.Fenced[0].Domain != mount.peer.scope.Session || last.Fenced[0].Phase != "INSTALL_DRAIN" {
		m.t.Errorf("history record %+v", last)
	}
}

// requireFencedWait requires mount's next VIEW_WAIT to get an error reply
// (the controller's ErrSessionFenced blocks that one cell) while the shared
// transport stays up for the other mount.
func (m *recallModel) requireFencedWait(mount *modelMount) {
	_, _, _, _, err := mount.peer.mappedExecuteReclaim("VIEW_WAIT", map[string]any{"domain": mount.domain, "after_sequence": 0}, mount.chain.guest, mount.chain.native, nil)
	if err == nil {
		m.t.Errorf("fenced m%d VIEW_WAIT answered", mount.index)
	} else {
		m.t.Logf("fenced m%d VIEW_WAIT: %v", mount.index, err)
	}
	if failure, failed := m.a.Transport.Failure(); failed {
		m.t.Errorf("the session fence failed the shared transport: %+v", failure)
	}
}
