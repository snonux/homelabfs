package guestbridge

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// lostReplyCall builds one actual CALL command for the real bridge service.
func lostReplyCall(t *testing.T, f *bridgeFixture, op string, args any) Command {
	t.Helper()
	intent := Intent{Opcode: op, Args: mustJSON(t, args)}
	raw, err := marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "616263", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: f.next + 1, OriginalHash: digest(raw)}, Credentials: f.cred(), Intent: intent}
	return f.command(t, "CALL", call)
}

// readResponded waits only while the actual worker has not yet responded and
// then returns the first non-EAGAIN Read outcome for the cell.
func readResponded(t *testing.T, tr *Transport, n Native, id uint64) ([]byte, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := tr.Read(t.Context(), n, n.Endpoint.ResultNode, id*CellStride, 32768)
		if !errors.Is(err, fsapi.WouldBlock) || time.Now().After(deadline) {
			return data, err
		}
		runtime.Gosched()
	}
}

func newLostReplyTransport(t *testing.T) (*bridgeFixture, *Transport) {
	t.Helper()
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	tr, err := NewTransport(f.s, transportBudget(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	return f, tr
}

func TestLostReplyDropsExactlyOneProtectedExposure(t *testing.T) {
	f, tr := newLostReplyTransport(t)
	for name, arm := range map[string]func() error{
		"wrong-operation": func() error { return tr.ArmLostReply(f.c.Session, "QUERY") },
		"unknown-session": func() error { return tr.ArmLostReply(uid(99).String(), LostReplyOperation) },
	} {
		if err := arm(); err == nil {
			t.Fatalf("%s armed", name)
		}
	}
	if _, ok := tr.LostReply(); ok {
		t.Fatal("refused arming left a fixture")
	}
	// A response accepted before arming is never the armed exposure.
	sendNative(t, tr, f.native, lostReplyCall(t, f, "ROOT", map[string]any{}), 0)
	older := fetchNative(t, tr, f.native, 1)
	if err := tr.ArmLostReply(f.c.Session, LostReplyOperation); err != nil {
		t.Fatal(err)
	}
	if err := tr.ArmLostReply(f.c.Session, LostReplyOperation); !errors.Is(err, fsapi.Busy) {
		t.Fatalf("repeated arming: %v", err)
	}
	if again := fetchNative(t, tr, f.native, 1); !bytes.Equal(again, older) {
		t.Fatal("older retained response changed")
	}
	// A different operation after arming is exposed normally.
	sendNative(t, tr, f.native, f.command(t, "QUERY", map[string]any{"claim": Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 1, OriginalHash: digest([]byte("x"))}}), 1)
	_ = fetchNative(t, tr, f.native, 2)
	checkDroppedExposure(t, f, tr)
}

// checkDroppedExposure sends the armed CALL, observes the single dropped
// exposure and then the unchanged protected bytes without re-execution.
func checkDroppedExposure(t *testing.T, f *bridgeFixture, tr *Transport) {
	t.Helper()
	sendNative(t, tr, f.native, lostReplyCall(t, f, "ROOT", map[string]any{}), 0)
	data, err := readResponded(t, tr, f.native, 3)
	if !errors.Is(err, fsapi.IO) || data != nil {
		t.Fatalf("armed exposure delivered: %v %d", err, len(data))
	}
	calls := f.driver.calls
	e, ok := tr.LostReply()
	tr.mu.Lock()
	c := tr.cells[3]
	unread, total, response := c.unread, wireBytes(c.wire), bytes.Clone(c.response)
	tr.mu.Unlock()
	if !ok || e.State != LostReplyFired || e.Cell != 3 || e.ArmedHigh != 1 || e.Claim == nil || e.Claim.Number != 3 || e.ResponseHash != digest(response) || e.JobPhase != "responded" || e.Outcome != "UNCERTAIN" || e.DroppedBytes == 0 {
		t.Fatalf("fired evidence: %+v", e)
	}
	if unread != total || e.UnreadBytes != total {
		t.Fatal("dropped exposure claimed read coverage")
	}
	// One-shot: the retry reads the identical protected response, no re-execution.
	if got := fetchNative(t, tr, f.native, 3); digest(got) != e.ResponseHash || f.driver.calls != calls {
		t.Fatal("retry changed response or re-executed")
	}
	tr.ExpireLostReply()
	if after, _ := tr.LostReply(); after.State != LostReplyFired {
		t.Fatal("expiry rewrote a fired record")
	}
}

func TestLostReplyExpiredAndNonDisposableNeverFire(t *testing.T) {
	f, tr := newLostReplyTransport(t)
	if err := tr.ArmLostReply(f.c.Session, LostReplyOperation); err != nil {
		t.Fatal(err)
	}
	tr.ExpireLostReply()
	sendNative(t, tr, f.native, lostReplyCall(t, f, "ROOT", map[string]any{}), 0)
	if _, err := readResponded(t, tr, f.native, 1); err != nil {
		t.Fatalf("expired fixture fired: %v", err)
	}
	if e, _ := tr.LostReply(); e.State != LostReplyExpired || e.Cell != 0 {
		t.Fatalf("expired evidence: %+v", e)
	}
	g, other := newLostReplyTransport(t)
	disposable := g.s.config.Validation
	g.s.config.Validation = nil // a non-disposable service owner
	if err := other.ArmLostReply(g.c.Session, LostReplyOperation); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("non-disposable service armed: %v", err)
	}
	g.s.config.Validation = disposable
	other.Close()
	if err := other.ArmLostReply(g.c.Session, LostReplyOperation); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("closed transport armed: %v", err)
	}
}
