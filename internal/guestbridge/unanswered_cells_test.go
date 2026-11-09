package guestbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Final makes an otherwise retryable class definitive for its cell while the
// diagnostic still names the wrapped errno.
func TestFinalErrorIsNotRetried(t *testing.T) {
	err := Final(fmt.Errorf("progress: %w", fsapi.NoSpace))
	if !errors.Is(err, fsapi.NoSpace) || blockedErrorClass(err) != "NO_SPACE" {
		t.Fatal(err)
	}
	if retryableExecuteError(err) || !retryableExecuteError(fsapi.NoSpace) || !retryableExecuteError(fsapi.WouldBlock) || retryableExecuteError(fsapi.Permission) {
		t.Fatal("retry classification changed")
	}
	if Final(nil) != nil {
		t.Fatal("Final(nil) is not nil")
	}
}

// pollFixture returns a durable transport whose session completed HELLO through
// the transport and holds a granted DOMAIN capability (from a direct ROOT).
func pollFixture(t *testing.T) (*bridgeFixture, *Transport, string, Capability) {
	t.Helper()
	f, tr, ledgerRoot := durableFixture(t)
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 1)
	_, root := f.call(t, f.cred(), "ROOT", map[string]any{})
	var domain Capability
	raw, _ := json.Marshal(root["capabilities"].([]any)[1])
	if err := json.Unmarshal(raw, &domain); err != nil {
		t.Fatal(err)
	}
	return f, tr, ledgerRoot, domain
}

// sendPoll writes one VIEW_POLL on the next native cell (control lane).
func sendPoll(t *testing.T, f *bridgeFixture, tr *Transport, domain Capability) Command {
	t.Helper()
	poll := f.command(t, "VIEW_POLL", map[string]any{"domain": domain, "after_sequence": 0})
	tr.mu.Lock()
	poll.Cell = tr.high + 1 // direct bridge calls consumed fixture cell numbers
	tr.mu.Unlock()
	sendNative(t, tr, f.native, poll, 1)
	return poll
}

// requireErrorReply asserts that a cell the endpoint will never answer gives the
// native reader a definitive error (EIO through FUSE, Rbridge ERROR to the
// guest) instead of WouldBlock, which the guest would retry until its deadline.
func requireErrorReply(t *testing.T, f *bridgeFixture, tr *Transport, cell uint64, class string) {
	t.Helper()
	waitCellPhase(t, tr, cell, "blocked")
	tr.mu.Lock()
	recorded := tr.cells[cell].blockedError
	tr.mu.Unlock()
	if recorded != class {
		t.Fatalf("cell %d error class %q, want %q", cell, recorded, class)
	}
	for _, node := range []uint64{f.native.Endpoint.RecallNode, f.native.Endpoint.ResultNode} {
		if _, err := tr.Read(t.Context(), f.native, node, cell*CellStride, 32768); !errors.Is(err, fsapi.IO) {
			t.Fatalf("unanswerable cell %d read %v, want IO error reply", cell, err)
		}
	}
}

func TestDurableTransportBlockedCellReadIsErrorReply(t *testing.T) {
	f, tr, _, domain := pollFixture(t)
	domain.Token = digest([]byte("never granted"))
	poll := sendPoll(t, f, tr, domain)
	requireErrorReply(t, f, tr, poll.Cell, "PERMISSION")
}

// A DOMAIN grant released (for example by SESSION_RETIRE) makes the bridge
// refuse VIEW_POLL. The spec defines no poll reply for that, so the guest must
// see an explicit transport error promptly.
func TestDurableTransportReleasedDomainPollGetsErrorReply(t *testing.T) {
	f, tr, _, domain := pollFixture(t)
	first := sendPoll(t, f, tr, domain)
	if raw := fetchNativeAt(t, tr, f.native, first.Cell, f.native.Endpoint.RecallNode); !bytes.Contains(raw, []byte(`"status":"EMPTY"`)) {
		t.Fatalf("granted domain poll %s", raw)
	}
	f.s.mu.Lock()
	f.s.sessions[f.c.Session].Grants[domain.Token].Released = true
	f.s.mu.Unlock()
	poll := sendPoll(t, f, tr, domain)
	requireErrorReply(t, f, tr, poll.Cell, "PERMISSION")
}

// An empty or oversized Execute response cannot be framed. The cell must not
// stay "executing" without a reply: it becomes blocked with a diagnostic class
// and answers reads with an error.
func TestDurableTransportUnframeableResponseGetsErrorReply(t *testing.T) {
	for name, replace := range map[string][]byte{"RESPONSE_EMPTY": nil, "RESPONSE_TOO_LARGE": make([]byte, BodyBytes+1)} {
		t.Run(name, func(t *testing.T) {
			f, tr, ledgerRoot, domain := pollFixture(t)
			tr.respondFailpoint = func(c Command, raw []byte) []byte {
				if c.Op == "VIEW_POLL" {
					return replace
				}
				return raw
			}
			poll := sendPoll(t, f, tr, domain)
			requireErrorReply(t, f, tr, poll.Cell, name)
			tr.Close()
			_, cells := inspectCells(t, ledgerRoot)
			last := cells[len(cells)-1]
			if last.Header.Cell != poll.Cell || last.JobPhase != "blocked" || last.BlockedError != name {
				t.Fatalf("protected row %d %q %q", last.Header.Cell, last.JobPhase, last.BlockedError)
			}
		})
	}
}
