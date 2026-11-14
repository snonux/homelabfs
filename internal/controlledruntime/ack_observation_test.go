//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// slowObservation stands in for one SSH round trip of the guest observer. The
// zq3-r8 lab run (R8-1) paid about this much per VIEW_ACK.
const slowObservation = 2 * time.Second

// slowProbe wraps admission's real (fixture SSH) phase probe. While slow is
// set every probe is counted, announced on entered (if not nil) and delayed by
// slowObservation, honoring ctx.
func slowProbe(a *Admission, slow *atomic.Bool, probes *atomic.Int32, entered chan<- struct{}) {
	a.probe = func(ctx context.Context) (installedObservation, error) {
		if slow.Load() {
			probes.Add(1)
			if entered != nil {
				entered <- struct{}{}
			}
			select {
			case <-time.After(slowObservation):
			case <-ctx.Done():
				return installedObservation{}, ctx.Err()
			}
		}
		return a.observer.observePhase(ctx)
	}
}

// TestRecallAcksReuseObservationWithSlowObserver runs a full five-phase recall
// over two domains against the assembled runtime with a 2 s guest observer.
// Every ACK is sent twice (the guest resends after EAGAIN/EBUSY or a lost
// reply). Before the change each of those 20 VIEW_ACKs ran its own probe,
// about 40 s of SSH on the control worker. Now an ACK reuses a recent
// observation that already records its exact bytes, so each phase needs at
// most one probe: the first ACK's, which also measured the peer domain.
func TestRecallAcksReuseObservationWithSlowObserver(t *testing.T) {
	r, c := assemblyFixture(t)
	var slow atomic.Bool
	var probes atomic.Int32
	slowProbe(r.admission, &slow, &probes, nil)
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	for _, peer := range r.peers {
		attachAssembly(peer, a, &counter)
	}
	families := c.Authorization.Enrollment.TicketFamilies
	_, domain := bootstrapMount(t, r.peers[0], families[0])
	second := r.peers[1]
	_, _, err = second.execute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(second.call("ROOT", map[string]any{}), &root))
	lease := acquireDomainLease(t, second, families[1], 1, root.Capabilities[1])
	control := guestbridge.Ticket{Anchor: families[1].Control.Anchor, Nonce: families[1].Control.Nonce, Number: 1, Credentials: second.credentials}
	payload, _, _, _, err := second.mappedExecute("VIEW_RELEASE", map[string]any{"request": control, "lease": lease})
	require(t, err)
	if !bytes.Contains(payload, []byte(`"status":"PENDING"`)) {
		t.Fatalf("mount 1 VIEW_RELEASE: %s", payload)
	}

	slow.Store(true)
	started := time.Now()
	acks := doubledRecall(t, r, []guestbridge.Capability{domain, root.Capabilities[1]}, &probes)
	total := probes.Load()
	t.Logf("%d VIEW_ACKs, %d slow observations, recall took %s", acks, total, time.Since(started))
	if acks != 20 || total > 5 {
		t.Fatalf("%d VIEW_ACKs needed %d observations, want at most one per phase (5)", acks, total)
	}
}

// doubledRecall acknowledges every phase of the current recall from both
// mounts, sending each ACK twice. Both mounts' kernel records are published
// before the first ACK, as the guest does when both mounts finish a phase
// before the host answers. It fails when one phase's ACKs needed more than one
// probe and returns the number of VIEW_ACKs sent. Mount 0 runs at fence and
// revision 2 (after its bootstrap recall), mount 1 at 1.
func doubledRecall(t *testing.T, r *sourceRig, domains []guestbridge.Capability, probes *atomic.Int32) int {
	t.Helper()
	fences, revisions := []uint64{2, 1}, []uint64{2, 1}
	chains := []*r5Chain{{peer: r.peers[0]}, {peer: r.peers[1]}}
	sent := 0
	for _, phase := range []string{"STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		acks := make([]map[string]any, len(chains))
		for i, chain := range chains {
			acks[i] = recallACK(t, chain, domains[i], phase)
			revision := revisions[i]
			if phase == "REOPENED" {
				revision++
			}
			event := acks[i]["event"].(map[string]json.RawMessage)
			delete(acks[i], "event")
			observeMountACK(t, r, i, event, acks[i], revision, fences[i])
		}
		before := probes.Load()
		for i, chain := range chains {
			chain.ack(t, phase, acks[i])
			chain.ack(t, phase+" resend", acks[i])
			sent += 2
		}
		if used := probes.Load() - before; used > 1 {
			t.Fatalf("%s: four VIEW_ACKs ran %d observations, want at most 1", phase, used)
		}
	}
	return sent
}

// recallACK polls one mount's current recall event and builds the exact ACK
// the guest kernel sends for phase. The polled event rides along under
// "event" so the caller can record the kernel observation.
func recallACK(t *testing.T, chain *r5Chain, domain guestbridge.Capability, phase string) map[string]any {
	t.Helper()
	event := chain.exec(t, "VIEW_WAIT", map[string]any{"domain": domain, "after_sequence": 0})
	var poll struct{ Event map[string]json.RawMessage }
	require(t, json.Unmarshal(event, &poll))
	if len(poll.Event) == 0 {
		t.Fatalf("missing %s event %s", phase, event)
	}
	ack := map[string]any{"domain": domain, "sequence": poll.Event["sequence"], "recall": poll.Event["recall"], "phase": phase, "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": chain.peer.scope.Session, "manifest": nil, "target": poll.Event["target"], "event": poll.Event}
	if phase == "STOPPED" {
		ack["target"] = nil
	}
	if phase == "STOPPED" || phase == "DRAINED" {
		ack["manifest"] = map[string]any{"entries": []any{}, "hash": hashBytes([]byte("[]"))}
	}
	return ack
}

// TestAdmissionProbeHoldsNoAdmissionLock keeps a 2 s guest probe in flight and
// shows that admission checks, the ACK cache lookup and a real
// ALLOCATE/CALL/QUERY through Producer.Prepare all complete while that probe is
// still running, i.e. no lock they need is held across it. It also checks
// refresh coalescing: a request satisfied by the in-flight probe waits for it,
// while a strict Refresh issued after that probe started runs its own. Run it
// with -race.
func TestAdmissionProbeHoldsNoAdmissionLock(t *testing.T) {
	r := newSourceRigProtocol(t, guestbridge.ControlledProtocol())
	a := r.admission
	var slow atomic.Bool
	var probes atomic.Int32
	entered := make(chan struct{}, 4)
	slowProbe(a, &slow, &probes, entered)
	require(t, a.Refresh(t.Context())) // fresh cache, so the CALL below needs no probe
	slow.Store(true)

	inFlight := time.Now()
	errs := make(chan error, 3)
	firstDone := make(chan struct{})
	go func() { defer close(firstDone); errs <- a.Refresh(context.Background()) }()
	<-entered
	ack := []byte(`{"never":"recorded"}`)
	quick(t, "Admission.check", func() { require(t, a.check(r.scope)) })
	quick(t, "Facts.checkScope", func() { require(t, r.facts.checkScope(t.Context(), r.scope)) })
	quick(t, "Facts.admission", func() {
		if r.facts.admission(r.scope) != a {
			t.Fatal("lost admission")
		}
	})
	quick(t, "measuredACK", func() {
		if a.measuredACK(r.scope, ack, admissionReuse) {
			t.Fatal("unrecorded ACK reported as measured")
		}
	})
	quick(t, "ALLOCATE/CALL/QUERY", func() { r.call("ROOT", map[string]any{}) })
	select {
	case <-firstDone:
		t.Fatal("probe finished before the checks; they did not overlap it")
	default:
	}

	// Coalescing: covered waits for the in-flight probe; strict needs a probe
	// that started after it was called.
	go func() { errs <- a.refreshSince(context.Background(), inFlight) }()
	go func() { errs <- a.Refresh(context.Background()) }()
	for range 3 {
		require(t, <-errs)
	}
	if n := probes.Load(); n != 2 {
		t.Fatalf("%d slow probes, want 2 (the in-flight one and the strict refresh)", n)
	}
}

// quick runs fn on the test goroutine and fails when it took half a slow probe
// or longer, i.e. when it waited for the in-flight probe.
func quick(t *testing.T, name string, fn func()) {
	t.Helper()
	started := time.Now()
	fn()
	if elapsed := time.Since(started); elapsed >= slowObservation/2 {
		t.Fatalf("%s blocked behind the in-flight probe (%s)", name, elapsed)
	}
}

// countingProbe installs a probe on a that counts calls and returns err, or
// the admission's real (fixture SSH) phase observation when err is nil.
func countingProbe(a *Admission, probes *atomic.Int32, err error) {
	a.probe = func(ctx context.Context) (installedObservation, error) {
		probes.Add(1)
		if err != nil {
			return installedObservation{}, err
		}
		return a.observer.observePhase(ctx)
	}
}

// ackFixture is one recorded STOPPED ACK: scope's admission observation holds
// the exact ACK and event hashes at fence 1, and the ledger issued that event.
type ackFixture struct {
	scope       guestbridge.Context
	ack, issued json.RawMessage
	admission   *Admission
	ledger      factsLedger
}

func newACKFixture(t *testing.T) ackFixture {
	t.Helper()
	scope := guestbridge.Context{Session: testUUID(3).String(), AuthorityHash: hashBytes([]byte("authority"))}
	domain := guestbridge.Capability{Kind: "DOMAIN", Scope: scope}
	issued, err := canonicalJSON(map[string]any{"sequence": 1, "domain": domain, "phase": "RECALL", "target": nil})
	require(t, err)
	ack, err := canonicalJSON(physicalACK{Domain: domain, Sequence: 1, Recall: "recall", Phase: "STOPPED", Authority: scope.AuthorityHash, Closure: "closure", Inventory: "inventory", Session: scope.Session, Manifest: json.RawMessage("null")})
	require(t, err)
	now := time.Now()
	a := &Admission{refreshed: now, observedAt: now}
	a.installed.Admission.Domains = []guestbridge.Context{scope}
	a.installed.Admission.Observations = []kernelDomainObservation{{Scope: scope, Revision: 1, Fence: 1, Phase: "STOPPED", EventHash: hashBytes(issued), AcknowledgementHash: hashBytes(ack)}}
	f := ackFixture{scope: scope, ack: ack, issued: issued, admission: a}
	f.ledger.Recall = &struct {
		ID, Phase, Inventory, Closure string
		Events, Acks, Manifests       map[string]json.RawMessage
	}{ID: "recall", Phase: "RECALL", Inventory: "inventory", Closure: "closure", Events: map[string]json.RawMessage{scope.Session: issued}}
	return f
}

// TestMeasuredACKRefusesWhatItCannotProve pins every measuredACK miss.
func TestMeasuredACKRefusesWhatItCannotProve(t *testing.T) {
	other := guestbridge.Context{Session: testUUID(9).String()}
	tests := map[string]struct {
		change func(*ackFixture)
		hit    bool
	}{
		"recorded exact bytes": {func(*ackFixture) {}, true},
		"probe older than reuse bound": {func(f *ackFixture) {
			f.admission.observedAt = time.Now().Add(-admissionReuse)
		}, false},
		"commit fresh but probe started too early": {func(f *ackFixture) {
			f.admission.observedAt = time.Now().Add(-admissionReuse - time.Second)
		}, false},
		"fenced":      {func(f *ackFixture) { f.admission.fenced = true }, false},
		"other scope": {func(f *ackFixture) { f.scope = other }, false},
		"LIVE row": {func(f *ackFixture) {
			f.admission.installed.Admission.Observations[0].Phase = "LIVE"
		}, false},
		"different bytes": {func(f *ackFixture) { f.ack = append(json.RawMessage(nil), f.ack[:len(f.ack)-1]...) }, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			f := newACKFixture(t)
			tc.change(&f)
			if got := f.admission.measuredACK(f.scope, f.ack, admissionReuse); got != tc.hit {
				t.Fatalf("measuredACK = %v, want %v", got, tc.hit)
			}
		})
	}
}

// TestCachedACKHitStillNeedsLedgerMatch shows a measuredACK hit grants nothing
// by itself: when the ledger's fence or issued event no longer matches the
// recorded observation, checkACK still answers ErrNotObserved.
func TestCachedACKHitStillNeedsLedgerMatch(t *testing.T) {
	base := newACKFixture(t)
	facts := &Facts{admissions: []*Admission{base.admission}}
	require(t, facts.checkACK(t.Context(), base.scope, base.ack, base.ledger))
	fenced := base.ledger
	fenced.Fences = map[string]uint64{base.scope.Session: 2}
	moved, err := canonicalJSON(map[string]any{"sequence": 1, "domain": guestbridge.Capability{Kind: "DOMAIN", Scope: base.scope}, "phase": "RECALL", "target": nil, "reissued": true})
	require(t, err)
	reissued := base.ledger
	recall := *base.ledger.Recall
	recall.Events = map[string]json.RawMessage{base.scope.Session: moved}
	reissued.Recall = &recall
	for name, l := range map[string]factsLedger{"ledger fence advanced": fenced, "issued event changed": reissued} {
		if !base.admission.measuredACK(base.scope, base.ack, admissionReuse) {
			t.Fatal("fixture lost its cache hit")
		}
		if err := facts.checkACK(t.Context(), base.scope, base.ack, l); !errors.Is(err, viewcontrol.ErrNotObserved) {
			t.Fatalf("%s: checkACK = %v, want ErrNotObserved", name, err)
		}
	}
}

// TestRefreshForRules pins Prepare's per-admission rule on the real rig
// admission: SESSION_RETIRE probes despite a fresh cache, and a VIEW_ACK for a
// scope another admission owns follows the ordinary five-second rule.
func TestRefreshForRules(t *testing.T) {
	r := newSourceRigProtocol(t, guestbridge.ControlledProtocol())
	a := r.admission
	var probes atomic.Int32
	countingProbe(a, &probes, nil)
	p := &Producer{facts: r.facts}
	require(t, a.Refresh(t.Context()))
	probes.Store(0)
	require(t, p.refreshFor(t.Context(), a, guestbridge.Command{Op: "SESSION_RETIRE", Context: r.scope}))
	if n := probes.Load(); n != 1 {
		t.Fatalf("SESSION_RETIRE ran %d probes on a fresh cache, want 1", n)
	}
	foreign := guestbridge.Command{Op: "VIEW_ACK", Context: guestbridge.Context{Session: testUUID(9).String()}, Payload: json.RawMessage(`{}`)}
	require(t, p.refreshFor(t.Context(), a, foreign))
	if n := probes.Load(); n != 1 {
		t.Fatalf("foreign VIEW_ACK probed a fresh non-owning admission (%d probes)", n)
	}
	a.mu.Lock()
	a.refreshed = time.Now().Add(-admissionReuse)
	a.observedAt = a.refreshed
	a.mu.Unlock()
	require(t, p.refreshFor(t.Context(), a, foreign))
	if n := probes.Load(); n != 2 {
		t.Fatalf("foreign VIEW_ACK on a stale non-owning admission: %d probes in total, want 2", n)
	}
}

// TestRefreshProbeErrorFencesWaitersWithoutProbing: a failed probe fences the
// admission, and a refresh queued behind it returns Unavailable without
// running another probe.
func TestRefreshProbeErrorFencesWaitersWithoutProbing(t *testing.T) {
	a := &Admission{}
	var probes atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	a.probe = func(context.Context) (installedObservation, error) {
		probes.Add(1)
		close(entered)
		<-release
		return installedObservation{}, fsapi.IO
	}
	first := make(chan error, 1)
	go func() { first <- a.Refresh(context.Background()) }()
	<-entered
	waiter := make(chan error, 1)
	go func() { waiter <- a.Refresh(context.Background()) }()
	close(release)
	if err := <-first; !errors.Is(err, fsapi.IO) {
		t.Fatalf("failed probe returned %v", err)
	}
	if err := <-waiter; !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("waiter after a fencing probe returned %v, want Unavailable", err)
	}
	a.mu.Lock()
	fenced := a.fenced
	a.mu.Unlock()
	if n := probes.Load(); n != 1 || !fenced {
		t.Fatalf("%d probes, fenced %v; want 1 probe and a fenced admission", n, fenced)
	}
}

// TestRefreshExpiredContextDoesNotProbeOrFence: a refresh whose context
// expired while it waited for refreshMu returns the context error before
// probing, so it never fences a healthy guest.
func TestRefreshExpiredContextDoesNotProbeOrFence(t *testing.T) {
	a := &Admission{}
	var probes atomic.Int32
	countingProbe(a, &probes, nil)
	ctx, cancel := context.WithCancel(t.Context())
	a.refreshMu.Lock() // an in-flight refresh holds the lock ...
	done := make(chan error, 1)
	go func() { done <- a.Refresh(ctx) }()
	cancel() // ... while the queued caller's deadline expires
	a.refreshMu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expired refresh returned %v, want context.Canceled", err)
	}
	a.mu.Lock()
	fenced := a.fenced
	a.mu.Unlock()
	if n := probes.Load(); n != 0 || fenced {
		t.Fatalf("%d probes, fenced %v; want neither", n, fenced)
	}
}
