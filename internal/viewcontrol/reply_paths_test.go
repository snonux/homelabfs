package viewcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// These tests drive VIEW_ACK/VIEW_POLL through the real durable endpoint
// worker. Every spec-defined outcome must arrive as a reply; anything else must
// reach the reader as an explicit error (IO), never as a cell that answers
// WouldBlock until the guest's 30 s transport deadline.

// transportAttempt is execute over the transport, returning the payload or the
// definitive read error.
func (r *rig) transportAttempt(i int, op string, payload any) (json.RawMessage, error) {
	r.t.Helper()
	r.cells[i]++
	raw, err := canonical(payload)
	must(r.t, err)
	protocol := guestbridge.ViewProtocol()
	command := guestbridge.Command{Cell: r.cells[i], Context: r.scopes[i], Protocol: &protocol, Op: op, Payload: raw}
	if transportControl(command) {
		command.Reclaim = r.transportReclaim[i]
	}
	r.transportWrite(i, command)
	node := r.natives[i].Endpoint.ResultNode
	if op == "VIEW_POLL" {
		node = r.natives[i].Endpoint.RecallNode
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		wire, err := r.transport[i].Read(r.t.Context(), r.natives[i], node, command.Cell*guestbridge.CellStride, 32768)
		if errors.Is(err, fsapi.WouldBlock) {
			if time.Now().After(deadline) {
				r.t.Fatalf("%s cell %d left unanswered (silent)", op, command.Cell)
			}
			time.Sleep(time.Millisecond)
			continue
		}
		if err != nil {
			// The refused cell already consumed the donor it reclaimed, and
			// holds its own credit without a response to reclaim later.
			if command.Reclaim != nil {
				r.transportReclaim[i] = nil
			}
			return nil, err
		}
		frame, err := guestbridge.DecodeFrame(wire)
		must(r.t, err)
		if transportControl(command) {
			r.transportReclaim[i] = &guestbridge.Ack{Cell: command.Cell, ResponseHash: hash(frame.Data)}
		}
		var envelope struct{ Payload json.RawMessage }
		must(r.t, json.Unmarshal(frame.Data, &envelope))
		return envelope.Payload, nil
	}
}

// startReleaseRecall acquires and releases a DOMAIN lease so a recall with a
// RECALL event for domain 0 exists, and returns the STOPPED ACK payload.
func (r *rig) startReleaseRecall() json.RawMessage {
	r.t.Helper()
	_, granted := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	payload := map[string]any{"request": r.controlTicket(0), "lease": granted.Lease}
	if pending := r.view(0, "VIEW_RELEASE", payload); pending.Status != "PENDING" {
		r.t.Fatalf("release %+v", pending)
	}
	return r.stoppedPayload(0, r.emptyManifest())
}

func requireAckStatus(t *testing.T, raw json.RawMessage, err error, want string) {
	t.Helper()
	if err != nil || status(raw) != want {
		t.Fatalf("VIEW_ACK reply %s %v, want %s", raw, err, want)
	}
}

func TestTransportViewAckRepliesSpecStatus(t *testing.T) {
	r := newRig(t)
	r.useDurableTransport()
	stopped := r.startReleaseRecall()
	// The observer has not measured STOPPED yet: BLOCKED, resent unchanged.
	raw, err := r.transportAttempt(0, "VIEW_ACK", stopped)
	requireAckStatus(t, raw, err, "BLOCKED")
	// Recovery follows a disconnect that discarded learned ACKs; guest RESUME
	// stays blocked, so it cannot clear for this session: error reply.
	r.controller.mu.Lock()
	r.controller.state.Recovery = true
	r.controller.mu.Unlock()
	r.physical("VIEW_ACK", json.RawMessage(stopped))
	if _, err = r.transportAttempt(0, "VIEW_ACK", stopped); !errors.Is(err, fsapi.IO) {
		t.Fatalf("recovering VIEW_ACK %v, want error reply", err)
	}
	r.controller.mu.Lock()
	r.controller.state.Recovery = false
	r.controller.mu.Unlock()
	// An ACK for a sequence that was never issued cannot acknowledge current work.
	var wrong map[string]json.RawMessage
	must(t, json.Unmarshal(stopped, &wrong))
	wrong["sequence"] = json.RawMessage("7")
	stale := mustRaw(t, wrong)
	r.physical("VIEW_ACK", stale)
	raw, err = r.transportAttempt(0, "VIEW_ACK", stale)
	requireAckStatus(t, raw, err, "STALE")
	raw, err = r.transportAttempt(0, "VIEW_ACK", stopped)
	requireAckStatus(t, raw, err, "ACCEPTED")
	// A poisoned controller has no spec reply: explicit error, not silence.
	r.controller.poisoned.Store(true)
	if _, err = r.transportAttempt(0, "VIEW_ACK", stopped); !errors.Is(err, fsapi.IO) {
		t.Fatalf("poisoned VIEW_ACK %v, want error reply", err)
	}
}

func TestTransportViewPollControllerUnavailableIsErrorReply(t *testing.T) {
	for name, set := range map[string]func(*Controller){
		"recovery": func(c *Controller) { c.mu.Lock(); c.state.Recovery = true; c.mu.Unlock() },
		"poisoned": func(c *Controller) { c.poisoned.Store(true) },
		"closed":   func(c *Controller) { must(t, c.Close()) },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.useDurableTransport()
			if raw := r.poll(0); string(raw) != emptyPollReply {
				t.Fatalf("baseline poll %s", raw)
			}
			set(r.controller)
			_, err := r.transportAttempt(0, "VIEW_POLL", map[string]any{"domain": r.domains[0], "after_sequence": 0})
			if !errors.Is(err, fsapi.IO) {
				t.Fatalf("%s VIEW_POLL %v, want error reply", name, err)
			}
		})
	}
}

// A poll whose opportunistic progress fails definitively (here the protected
// write of the next phase fails) has no poll reply: explicit error.
// Even a WouldBlock-classed write failure poisons the controller, so it is not
// treated as transient contention.
func TestTransportViewPollProgressFailureIsErrorReply(t *testing.T) {
	for name, crash := range map[string]error{"crash": errors.New("protected write failed"), "would-block": fsapi.WouldBlock} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.useDurableTransport()
			stopped := r.startReleaseRecall()
			r.physical("VIEW_ACK", json.RawMessage(stopped))
			raw, err := r.transportAttempt(0, "VIEW_ACK", stopped)
			requireAckStatus(t, raw, err, "ACCEPTED") // the recall is now in DRAIN
			r.controller.config.Failpoint = func(point string) error {
				if point == "state-protected" {
					return crash
				}
				return nil
			}
			if _, err = r.transportAttempt(0, "VIEW_POLL", map[string]any{"domain": r.domains[0], "after_sequence": 0}); !errors.Is(err, fsapi.IO) {
				t.Fatalf("progress failure VIEW_POLL %v, want error reply", err)
			}
		})
	}
}

// Only classes that clear without changing the ACK are BLOCKED; a real
// Permission refusal, bounds, IO, Unavailable and protocol violations are
// definitive errors so the guest fails promptly instead of resending forever.
func TestAckRefusalClassification(t *testing.T) {
	r := newRig(t)
	raw := json.RawMessage(`{"phase":"STOPPED"}`)
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	for err, want := range map[error]string{fsapi.Stale: "STALE", ErrNotObserved: "BLOCKED", fsapi.WouldBlock: "BLOCKED", fsapi.Busy: "BLOCKED"} {
		reply, e := r.controller.ackRefusal(r.scopes[0], raw, err)
		requireAckStatus(t, reply, e, want)
	}
	for _, err := range []error{fsapi.Permission, fsapi.Unavailable, fsapi.IO, fsapi.NoSpace, fsapi.Invalid, fsapi.Unsupported, errors.New("undecodable manifest")} {
		if reply, e := r.controller.ackRefusal(r.scopes[0], raw, err); e == nil || !errors.Is(e, err) {
			t.Fatalf("%v answered as a reply %s: %v", err, reply, e)
		}
	}
}

// One exact ACK may answer BLOCKED for at most BlockedACKBound; then it gets
// an error reply. Other bytes or a non-BLOCKED reply restart the bound.
func TestTransportBlockedACKIsBounded(t *testing.T) {
	r := newRig(t)
	r.useDurableTransport()
	stopped := r.startReleaseRecall() // no observation: ErrNotObserved
	clock := time.Unix(1_000_000, 0)
	r.controller.mu.Lock()
	r.controller.now = func() time.Time { return clock }
	r.controller.mu.Unlock()
	advance := func(d time.Duration) {
		r.controller.mu.Lock()
		clock = clock.Add(d)
		r.controller.mu.Unlock()
	}
	raw, err := r.transportAttempt(0, "VIEW_ACK", stopped)
	requireAckStatus(t, raw, err, "BLOCKED")
	advance(BlockedACKBound)
	raw, err = r.transportAttempt(0, "VIEW_ACK", stopped)
	requireAckStatus(t, raw, err, "BLOCKED") // exactly at the bound
	// A different ACK (another session's bytes) restarts the clock.
	var other map[string]json.RawMessage
	must(t, json.Unmarshal(stopped, &other))
	other["sequence"] = json.RawMessage("7")
	stale := mustRaw(t, other)
	raw, err = r.transportAttempt(0, "VIEW_ACK", stale)
	requireAckStatus(t, raw, err, "STALE")
	raw, err = r.transportAttempt(0, "VIEW_ACK", stopped)
	requireAckStatus(t, raw, err, "BLOCKED") // fresh bound after the STALE reply
	advance(BlockedACKBound + time.Millisecond)
	if _, err = r.transportAttempt(0, "VIEW_ACK", stopped); !errors.Is(err, fsapi.IO) {
		t.Fatalf("ACK blocked past the bound %v, want error reply", err)
	}
	r.controller.mu.Lock()
	tracked := len(r.controller.blockedACKs)
	r.controller.mu.Unlock()
	if tracked != 0 {
		t.Fatal("bound state retained after its error reply")
	}
}

// A poll whose progress meets a full service queue (NoSpace) or a busy
// provider (Busy) answers from the unchanged state, and progress resumes on
// the next poll. Before, NoSpace became a blocked cell and killed the session.
func TestTransportViewPollTransientProgressStillAnswers(t *testing.T) {
	for _, class := range []error{fsapi.NoSpace, fsapi.Busy} {
		t.Run(class.Error(), func(t *testing.T) {
			r := newRig(t)
			r.useDurableTransport()
			stopped := r.startReleaseRecall()
			r.physical("VIEW_ACK", json.RawMessage(stopped))
			raw, err := r.transportAttempt(0, "VIEW_ACK", stopped)
			requireAckStatus(t, raw, err, "ACCEPTED") // recall now in DRAIN
			r.controller.mu.Lock()
			r.controller.config.Failpoint = func(point string) error {
				if point == "view-capture" {
					return class
				}
				return nil
			}
			r.controller.mu.Unlock()
			poll := map[string]any{"domain": r.domains[0], "after_sequence": 0}
			raw, err = r.transportAttempt(0, "VIEW_POLL", poll)
			if err != nil || status(raw) != "EVENT" {
				t.Fatalf("poll under %v: %s %v", class, raw, err)
			}
			r.controller.mu.Lock()
			phase, poisoned := r.controller.state.Recall.Phase, r.controller.poisoned.Load()
			r.controller.config.Failpoint = nil
			r.controller.mu.Unlock()
			if phase != "DRAIN" || poisoned {
				t.Fatalf("failed progress changed state: phase %s poisoned %v", phase, poisoned)
			}
			raw, err = r.transportAttempt(0, "VIEW_POLL", poll)
			if err != nil || !bytes.Contains(raw, []byte(`"phase":"INSTALL_DRAIN"`)) {
				t.Fatalf("progress did not resume: %s %v", raw, err)
			}
		})
	}
}
