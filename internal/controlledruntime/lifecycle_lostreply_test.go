//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"golang.org/x/sys/unix"
)

// lostCall is one CALL whose actual response exposure the armed fixture
// dropped: the guest cell, the claim and the bookkeeping exchange would record.
type lostCall struct {
	session int
	cell    uint64
	call    guestbridge.Call
	reclaim *guestbridge.Ack
	control bool
}

// arm sends one HSC1 ARM through the real endpoint. Every binding value is a
// test parameter so negative cases can name stale or foreign targets.
func (run *lifecycleRun) arm(sequence uint32, runtime, cut string, epoch uint64, session string, operation uint8) lifecycleReply {
	run.t.Helper()
	request := lifecycleRequest{opcode: LifecycleArmLostReply, sequence: sequence, epoch: epoch, operation: operation}
	for _, id := range []struct {
		dst *fsapi.UUID
		src string
	}{{&request.runtime, runtime}, {&request.cut, cut}, {&request.session, session}} {
		parsed, err := fsapi.ParseUUID(id.src)
		require(run.t, err)
		*id.dst = parsed
	}
	op, raw, err := lifecycleCommand(run.t.Context(), run.ready.Socket, request)
	require(run.t, err)
	var reply lifecycleReply
	require(run.t, json.Unmarshal(raw, &reply))
	if reply.Status != "REFUSED" && op != LifecycleArmLostReply|0x80 {
		run.t.Fatalf("ARM reply opcode %x", op)
	}
	return reply
}

// observe returns fresh actual evidence through the real endpoint.
func (run *lifecycleRun) observe(sequence uint32) *lifecycleEvidence {
	run.t.Helper()
	_, reply := run.command(LifecycleObserve, sequence, run.ready.Runtime, "", 0)
	if reply.Status != "OBSERVED" || reply.Evidence == nil {
		run.t.Fatalf("observation refused: %+v", reply)
	}
	return reply.Evidence
}

func (d *restartChild) readErrors() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.readEIO
}

// fetchLost issues guest FETCHes through the actual C exporter while the
// response is still pending, and returns the outcome of the one FETCH during
// which the exporter's actual result pread was answered with EIO. That FETCH
// must deliver no bytes. (The C fixture's guest emulation reports every
// non-data FETCH reply, including the exporter's error reply, as EAGAIN.)
// Like fetch, it polls with a 1 ms pause within a 10 s deadline.
func (d *restartChild) fetchLost(session int, cell uint64) uint32 {
	d.t.Helper()
	request := binary.BigEndian.AppendUint64([]byte{byte(session)}, cell)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		before := d.readErrors()
		require(d.t, d.writePacket('F', request))
		status, response, err := d.readResult()
		require(d.t, err)
		dropped := d.readErrors() > before
		switch {
		case dropped && (status == 0 || len(response) != 0):
			d.t.Fatalf("dropped exposure still delivered %d bytes", len(response))
		case dropped:
			return status
		case status == 0:
			d.t.Fatal("armed response exposure was delivered without a dropped read")
		case status != uint32(unix.EAGAIN):
			d.t.Fatalf("FETCH %d failed before exposure: %d", cell, status)
		}
	}
	d.t.Fatalf("FETCH %d stayed pending", cell)
	return 0
}

// loseCall performs ALLOCATE normally, then SENDs the claimed CALL and makes
// exactly one guest FETCH, whose actual response exposure must be dropped.
func (d *restartChild) loseCall(session int, opcode string, args any) lostCall {
	d.t.Helper()
	call := d.newCall(session, opcode, args)
	d.exchange(session, "ALLOCATE", call, nil)
	cell, reclaim, control := d.send(session, "CALL", call, nil)
	d.keep[fmt.Sprintf("%d/%d", session, cell)] = true // retained for post-C9 replay
	if errors := d.readErrors(); errors != 0 {
		d.t.Fatalf("native read errors before the armed exposure: %d", errors)
	}
	d.fetchLost(session, cell)
	return lostCall{session: session, cell: cell, call: call, reclaim: reclaim, control: control}
}

func queryResult(t *testing.T, wire []byte) json.RawMessage {
	t.Helper()
	var query struct {
		Payload struct{ Result json.RawMessage }
	}
	require(t, json.Unmarshal(wire, &query))
	return query.Payload.Result
}

// TestLifecycleLostReplyAtActualResponseExposure arms the fixture through the
// real endpoint, loses the actual MKDIR CALL reply after its committed effect,
// reconciles the uncertain result by claim and then crosses same-owner C9.
func TestLifecycleLostReplyAtActualResponseExposure(t *testing.T) {
	binary := buildRestartChild(t)
	t.Run("reconcile-one-effect-through-c9", func(t *testing.T) {
		run := startLifecycleRun(t, binary, nil)
		root := lostReplyRoot(t, run)
		armed := run.arm(2, run.ready.Runtime, run.ready.Cut, 1, run.r.peers[0].scope.Session, lostReplyOperationCall)
		if armed.Status != "ARMED" || armed.LostReply == nil || armed.LostReply.Exposure.State != guestbridge.LostReplyArmed || armed.LostReply.Cut != run.ready.Cut || armed.LostReply.FundedBytes != lostReplyMemoryBytes || armed.LostReply.Baseline.StateHash == "" {
			t.Fatalf("ARM: %+v", armed)
		}
		lost := run.d.loseCall(0, "MKDIR", map[string]any{"parent": root[0], "name": "6c6f73742d7265706c79", "mode": 0770, "umask": 0})
		fired := checkFiredEvidence(t, run, armed.LostReply, lost)
		result, created := reconcileByClaim(t, run, lost, fired)
		replayLostCall(t, run, lost, fired)
		prepareRestartCut(t, run, root, created)
		checkLostReplyRestart(t, run, lost, result, root)
		run.cancel()
		require(t, run.finish())
	})
	t.Run("restart-before-replay-stays-fenced", func(t *testing.T) { checkRestartBeforeReplay(t, binary) })
}

// checkRestartBeforeReplay loses a claimed CALL reply, reconciles only the
// claim and shows C9 refusing while the exporter never re-read the response.
func checkRestartBeforeReplay(t *testing.T, binary string) {
	run := startLifecycleRun(t, binary, nil)
	lostReplyRoot(t, run)
	if armed := run.arm(2, run.ready.Runtime, run.ready.Cut, 1, run.r.peers[0].scope.Session, lostReplyOperationCall); armed.Status != "ARMED" {
		t.Fatalf("ARM: %+v", armed)
	}
	// A non-mutating claimed CALL keeps the cut free of lookups and
	// historical sources, isolating the never-exposed response row.
	lost := run.d.loseCall(0, "ROOT", map[string]any{})
	if e := run.observe(3); e.LostReply == nil || e.LostReply.Exposure.State != guestbridge.LostReplyFired || *e.LostReply.Exposure.Claim != lost.call.Claim {
		t.Fatalf("fired evidence: %+v", e.LostReply)
	}
	_, wire := run.d.exchange(0, "QUERY", map[string]any{"claim": lost.call.Claim}, nil)
	if !bytes.Contains(queryResult(t, wire), []byte(`"code":"OK"`)) {
		t.Fatalf("claim reconciliation: %s", wire)
	}
	// The claim is reconciled, but the C exporter never received the CALL
	// response: its paired row is pending, so C9 must refuse truthfully.
	_, failed := run.command(LifecycleRestart, 4, run.ready.Runtime, run.ready.Cut, 1)
	if failed.Status != lifecycleFenced || failed.Step != "suspend" || !bytes.Contains([]byte(failed.Error), []byte("paired exporter/C8 cut")) {
		t.Fatalf("restart over a never-exposed response: %+v", failed)
	}
	if e := failed.Evidence; e == nil || e.LostReply == nil || e.LostReply.Exposure.State != guestbridge.LostReplyFired {
		t.Fatalf("fenced evidence lost the fired record: %+v", failed.Evidence)
	}
	checkFencedExit(t, run)
}

// lostReplyRoot resolves the actual ROOT/DOMAIN capabilities of session 0.
func lostReplyRoot(t *testing.T, run *lifecycleRun) []guestbridge.Capability {
	t.Helper()
	_, raw := run.d.root(0)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(raw, &root))
	if e := run.observe(1); e.LostReply != nil || len(root.Capabilities) != 2 {
		t.Fatalf("fixture present before ARM or ROOT incomplete: %+v", e.LostReply)
	}
	return root.Capabilities
}

// checkFiredEvidence compares the fired record with the actual claim and the
// actual service source: the committed effect is visible, coverage is not.
func checkFiredEvidence(t *testing.T, run *lifecycleRun, armed *lostReplyView, lost lostCall) *lifecycleEvidence {
	t.Helper()
	e := run.observe(3)
	x := e.LostReply
	if x == nil || x.Fixture != armed.Fixture || !e.Complete || e.Publication != nil {
		t.Fatalf("fired observation: %+v", e)
	}
	f := x.Exposure
	if f.State != guestbridge.LostReplyFired || f.Claim == nil || *f.Claim != lost.call.Claim || f.Session != run.r.peers[0].scope.Session || f.Cell <= f.ArmedHigh || f.JobPhase != "responded" || f.Outcome != "UNCERTAIN" || f.UnreadBytes != f.WireBytes || f.DroppedBytes == 0 || f.ResponseHash == "" {
		t.Fatalf("fired exposure: %+v", f)
	}
	if e.Service.StateHash == x.Baseline.StateHash || e.Service.Through == nil || x.Baseline.Through == nil || *e.Service.Through == *x.Baseline.Through {
		t.Fatalf("original effect not committed before the dropped exposure: %+v vs %+v", e.Service, x.Baseline)
	}
	return e
}

// reconcileByClaim resolves the uncertain result through the actual claim:
// the original OK result is returned, never re-executed or reclassified.
func reconcileByClaim(t *testing.T, run *lifecycleRun, lost lostCall, fired *lifecycleEvidence) (json.RawMessage, guestbridge.Capability) {
	t.Helper()
	_, wire := run.d.exchange(lost.session, "QUERY", map[string]any{"claim": lost.call.Claim}, nil)
	result := queryResult(t, wire)
	var outcome struct {
		Code         string
		Publication  json.RawMessage
		Capabilities []guestbridge.Capability
	}
	require(t, json.Unmarshal(result, &outcome))
	if outcome.Code != "OK" || string(outcome.Publication) != "null" || len(outcome.Capabilities) != 1 {
		t.Fatalf("claim reconciliation reclassified the original effect: %s", result)
	}
	checkSameEffect(t, run.observe(4), fired, "claim reconciliation")
	return result, outcome.Capabilities[0]
}

// replayLostCall reads the retained immutable CALL response after the
// one-shot: the protected bytes are delivered without another effect.
func replayLostCall(t *testing.T, run *lifecycleRun, lost lostCall, fired *lifecycleEvidence) {
	t.Helper()
	d := run.d
	response := d.fetch(lost.session, lost.cell)
	if d.readErrors() != 1 {
		t.Fatal("the one-shot fixture dropped more than one exposure")
	}
	d.settle(lost.session, lost.cell, "CALL", lost.reclaim, lost.control, response)
	after := run.observe(5)
	checkSameEffect(t, after, fired, "response replay")
	if after.LostReply.Exposure.State != guestbridge.LostReplyFired {
		t.Fatalf("replay rewrote the fired record: %+v", after.LostReply)
	}
}

func checkSameEffect(t *testing.T, got, want *lifecycleEvidence, step string) {
	t.Helper()
	if got.Service.StateHash != want.Service.StateHash || got.Service.Through == nil || *got.Service.Through != *want.Service.Through {
		t.Fatalf("%s changed the journal/source: %+v vs %+v", step, got.Service, want.Service)
	}
}

// prepareRestartCut performs the guest steps any C9 cut after a mutation
// needs: drop the MKDIR lookup and refresh/retire the now historical view
// source through the runtime's own ticket families and an actual recall.
func prepareRestartCut(t *testing.T, run *lifecycleRun, root []guestbridge.Capability, created guestbridge.Capability) {
	t.Helper()
	d := run.d
	d.ordinary(0, "FORGET", map[string]any{"object": created, "count": 1})
	families := run.signed.Authorization.Enrollment.TicketFamilies[0]
	ticket := guestbridge.Ticket{Anchor: families.Acquisition.Anchor, Nonce: families.Acquisition.Nonce, Number: 1, Credentials: run.r.credentials}
	_, wire := d.exchange(0, "VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": root[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}, nil)
	var acquired struct {
		ID    string
		Lease guestbridge.ViewLease
	}
	require(t, json.Unmarshal(restartPayload(t, wire), &acquired))
	d.releaseAndRetireFamily(families, acquired.Lease, acquired.ID)
}

// checkLostReplyRestart runs the real same-owner C9 restart over the
// reconciled cut and checks the original response/result/effect survive it.
func checkLostReplyRestart(t *testing.T, run *lifecycleRun, lost lostCall, result json.RawMessage, root []guestbridge.Capability) {
	t.Helper()
	d := run.d
	before := run.observe(6)
	_, restarted := run.command(LifecycleRestart, 7, run.ready.Runtime, run.ready.Cut, 1)
	if restarted.Status != lifecycleRestarted || restarted.Evidence == nil {
		t.Fatalf("C9 over reconciled lost reply: %+v", restarted)
	}
	checkSameEffect(t, restarted.Evidence, before, "C9 restart")
	if x := restarted.Evidence.LostReply; x == nil || x.Exposure.State != guestbridge.LostReplyFired || x.Exposure.Claim == nil || *x.Exposure.Claim != lost.call.Claim {
		t.Fatalf("C9 lost the fired record: %+v", x)
	}
	if replay := d.fetch(lost.session, lost.cell); !bytes.Equal(replay, d.replies[lost.session][lost.cell]) {
		t.Fatal("CALL response bytes changed across C9")
	}
	_, wire := d.exchange(lost.session, "QUERY", map[string]any{"claim": lost.call.Claim}, nil)
	if !bytes.Equal(queryResult(t, wire), result) {
		t.Fatal("protected original result replanned across C9")
	}
	final := run.observe(8)
	checkSameEffect(t, final, before, "post-C9 replay")
	if !final.Complete || final.Stage != lifecycleRestarted {
		t.Fatalf("post-C9 evidence: %+v", final)
	}
	// Exactly one directory exists: the effect was neither lost nor duplicated.
	_, listing := d.ordinary(0, "READDIR", map[string]any{"object": root[0], "after": "", "revision": 0, "limit": 8})
	var entries struct{ Entries []json.RawMessage }
	require(t, json.Unmarshal(listing, &entries))
	if len(entries.Entries) != 1 {
		t.Fatalf("lost-reply effect count %d: %s", len(entries.Entries), listing)
	}
	// Single use per runtime: no ARM after the restart attempt.
	if again := run.arm(9, run.ready.Runtime, restarted.Cut, 2, run.r.peers[0].scope.Session, lostReplyOperationCall); again.Status != "REFUSED" {
		t.Fatalf("ARM after restart: %+v", again)
	}
}

// TestLifecycleLostReplyArmingRefusals covers unauthorized, foreign, stale,
// wrong-operation, unknown-session, unfunded and repeated arming, then shows
// an unfired fixture never fires on another session and expires at C9.
func TestLifecycleLostReplyArmingRefusals(t *testing.T) {
	run := startLifecycleRun(t, buildRestartChild(t), nil)
	session := run.r.peers[0].scope.Session
	checkArmRefusals(t, run, session)
	checkUnfundedArm(t, run, session)
	// The funded ARM goes through the actual CLI client, as an operator would.
	var out, errout bytes.Buffer
	require(t, Run(t.Context(), []string{"--control", run.ready.Socket, "--operation", "arm-lost-reply", "--runtime", run.ready.Runtime, "--sequence", "20", "--cut", run.ready.Cut, "--epoch", "1", "--session", session, "--lost-operation", "CALL"}, &out, &errout))
	if !bytes.Contains(out.Bytes(), []byte(`"status":"ARMED"`)) {
		t.Fatalf("client ARM: %s %s", out.Bytes(), errout.Bytes())
	}
	if again := run.arm(21, run.ready.Runtime, run.ready.Cut, 1, session, lostReplyOperationCall); again.Status != "REFUSED" {
		t.Fatalf("repeated ARM accepted: %+v", again)
	}
	// Another session's CALL exposure is never the armed one.
	if _, other := run.d.root(1); len(other) == 0 {
		t.Fatal("other session traffic failed while armed")
	}
	if e := run.observe(22); e.LostReply == nil || e.LostReply.Exposure.State != guestbridge.LostReplyArmed || e.LostReply.Exposure.Cell != 0 {
		t.Fatalf("fixture fired on a foreign session: %+v", e.LostReply)
	}
	_, restarted := run.command(LifecycleRestart, 23, run.ready.Runtime, run.ready.Cut, 1)
	if restarted.Status != lifecycleRestarted || restarted.Evidence.LostReply == nil || restarted.Evidence.LostReply.Exposure.State != guestbridge.LostReplyExpired {
		t.Fatalf("unfired fixture crossed C9: %+v", restarted)
	}
	// After expiry the armed session's CALL exposure is delivered normally.
	if _, after := run.d.root(0); len(after) == 0 {
		t.Fatal("expired fixture affected restored traffic")
	}
	run.cancel()
	require(t, run.finish())
}

// checkArmRefusals: none of these may arm, consume the one-shot or reach C9.
func checkArmRefusals(t *testing.T, run *lifecycleRun, session string) {
	t.Helper()
	other := fsapi.UUID{7}.String()
	run.foreign.Store(true)
	refused := run.arm(1, run.ready.Runtime, run.ready.Cut, 1, session, lostReplyOperationCall)
	run.foreign.Store(false)
	if refused.Status != "REFUSED" || !bytes.Contains([]byte(refused.Reason), []byte("peer credentials")) {
		t.Fatalf("foreign peer ARM: %+v", refused)
	}
	sequence := uint32(1)
	for name, c := range map[string]struct {
		runtime, cut, session string
		epoch                 uint64
		operation             uint8
	}{
		"foreign-runtime": {other, run.ready.Cut, session, 1, lostReplyOperationCall},
		"stale-cut":       {run.ready.Runtime, other, session, 1, lostReplyOperationCall},
		"stale-epoch":     {run.ready.Runtime, run.ready.Cut, session, 2, lostReplyOperationCall},
		"operation-0":     {run.ready.Runtime, run.ready.Cut, session, 1, 0},
		"operation-2":     {run.ready.Runtime, run.ready.Cut, session, 1, 2},
		"operation-255":   {run.ready.Runtime, run.ready.Cut, session, 1, 255},
		"unknown-session": {run.ready.Runtime, run.ready.Cut, other, 1, lostReplyOperationCall},
	} {
		sequence++
		if reply := run.arm(sequence, c.runtime, c.cut, c.epoch, c.session, c.operation); reply.Status != "REFUSED" {
			t.Fatalf("%s ARM accepted: %+v", name, reply)
		}
	}
	id, err := fsapi.ParseUUID(run.ready.Runtime)
	require(t, err)
	if op, reply, err := run.rawExchange(lifecycleFrame("HSC1", 1, LifecycleArmLostReply, 0, 10, 56, append(id[:], make([]byte, 40)...))); err != nil || op != LifecycleRefused || reply.Status != "REFUSED" {
		t.Fatalf("short ARM frame accepted: %x %+v %v", op, reply, err)
	}
	if e := run.observe(11); e.LostReply != nil || run.switches.Load() != 0 {
		t.Fatalf("refused ARM left a fixture or reached C9: %+v", e.LostReply)
	}
}

// checkUnfundedArm exhausts the actual runtime Memory pool's operation slot
// that fundRuntime left for the fixture: ARM must refuse before arming and
// keep nothing, so a later funded ARM still works.
func checkUnfundedArm(t *testing.T, run *lifecycleRun, session string) {
	t.Helper()
	budget := run.r.supervisor.runtimeFunding.transport
	held, err := budget.Reserve(admission.Request{Class: admission.Memory, Peer: "test-exhaustion", Volume: "controlled-runtime", Cost: admission.Resources{Bytes: 1, Operations: 1}})
	require(t, err)
	refused := run.arm(12, run.ready.Runtime, run.ready.Cut, 1, session, lostReplyOperationCall)
	require(t, held.Release())
	if refused.Status != "REFUSED" || !bytes.Contains([]byte(refused.Reason), []byte("unfunded")) {
		t.Fatalf("unfunded ARM: %+v", refused)
	}
	if e := run.observe(13); e.LostReply != nil {
		t.Fatalf("unfunded ARM armed: %+v", e.LostReply)
	}
}
