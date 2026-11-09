package viewcontrol

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// emptyPollReply is the spec's canonical EMPTY VIEW_POLL payload.
const emptyPollReply = `{"event":null,"status":"EMPTY"}`

// The guest kernel starts polling as soon as its view control exists, before
// its first VIEW_ACQUIRE registers the domain (zq3-r4 R4-1). Such a poll must
// be answered EMPTY without changing the ledger, while capability checks stay.
func TestViewPollBeforeAcquireIsEmpty(t *testing.T) {
	r := newRig(t)
	before := r.ledgerBytes()
	if raw := r.poll(0); string(raw) != emptyPollReply {
		t.Fatalf("poll before acquire %s", raw)
	}
	if after := r.ledgerBytes(); !bytes.Equal(before, after) {
		t.Fatal("EMPTY poll for an unregistered domain changed the ledger")
	}
	r.controller.mu.Lock()
	_, registered := r.controller.state.Domains[r.scopes[0].Session]
	r.controller.mu.Unlock()
	if registered {
		t.Fatal("poll registered the domain into the recall inventory")
	}
	// The controller itself still refuses another scope's DOMAIN capability and
	// a non-DOMAIN capability (the closed schema refuses that one first), even
	// when the bridge's grant check is bypassed.
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	for name, refusal := range map[string]struct {
		capability guestbridge.Capability
		err        error
	}{"other-scope": {r.domains[1], fsapi.Permission}, "not-domain": {root, fsapi.Invalid}} {
		payload := mustRaw(t, map[string]any{"domain": refusal.capability, "after_sequence": 0})
		if _, err := r.controller.Exchange(t.Context(), r.scopes[0], "VIEW_POLL", payload); !errors.Is(err, refusal.err) {
			t.Fatalf("%s unregistered poll: %v", name, err)
		}
	}
	// Once registered, a mismatched capability is still refused.
	if _, granted := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ"); granted.Status != "GRANTED" {
		t.Fatalf("acquire %+v", granted)
	}
	payload := mustRaw(t, map[string]any{"domain": r.domains[1], "after_sequence": 0})
	if _, err := r.controller.Exchange(t.Context(), r.scopes[0], "VIEW_POLL", payload); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("registered mismatched poll: %v", err)
	}
	if raw := r.poll(0); string(raw) != emptyPollReply {
		t.Fatalf("registered poll without recall %s", raw)
	}
}

// The exact zq3-r4 mount sequence through the real durable endpoint worker:
// poll before acquire, acquire GRANTED, release PENDING, the next poll
// delivers RECALL, and the acknowledgements complete the release. Peer 1
// never acquires, as in the lab where it had not sent HELLO yet. Every cell
// must end "responded"; a silent "blocked" cell is what stranded the mount.
// The acknowledgements replay zq3-r5 (R5-1, task 964): the release loop keeps
// querying PENDING after INSTALLED_DRAIN, and the guest resends the identical
// INSTALLED_DRAIN in a new cell before DRAINED, as its event worker does when
// it re-runs the event after EAGAIN/EBUSY (hfs_mmap_events).
func TestTransportPollBeforeAcquireDeliversReleaseRecall(t *testing.T) {
	r := newRig(t)
	roots := r.useDurableTransport()
	if raw := r.poll(0); string(raw) != emptyPollReply {
		t.Fatalf("poll before acquire %s", raw)
	}
	_, granted := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	if granted.Status != "GRANTED" {
		t.Fatalf("acquire %+v", granted)
	}
	ticket := r.controlTicket(0)
	payload := map[string]any{"request": ticket, "lease": granted.Lease}
	if pending := r.view(0, "VIEW_RELEASE", payload); pending.Status != "PENDING" {
		t.Fatalf("release %+v", pending)
	}
	if event := r.poll(0); !bytes.Contains(event, []byte(`"status":"EVENT"`)) || !bytes.Contains(event, []byte(`"phase":"RECALL"`)) {
		t.Fatalf("release recall not delivered %s", event)
	}
	r.replayR5Recall(ticket)
	r.physical("VIEW_RELEASE", payload)
	if released := r.view(0, "VIEW_QUERY", map[string]any{"request": ticket}); released.Status != "RELEASED" {
		t.Fatalf("release not completed %+v", released)
	}
	// Control cells form a reclaim chain: each next control command reclaims the
	// previous response by hash, which the endpoint refuses for a cell without
	// one. The chain therefore already proves the first poll was answered; the
	// inspection checks every cell still retained.
	r.transport[0].Close()
	cells := 0
	_, err := guestbridge.InspectTransport(roots[0], func(c guestbridge.ProtectedTransportCell) error {
		cells++
		if c.JobPhase != "responded" || c.BlockedError != "" {
			t.Errorf("cell %d %s phase %q error %q", c.Header.Cell, c.Command.Op, c.JobPhase, c.BlockedError)
		}
		return nil
	})
	must(t, err)
	if cells == 0 {
		t.Fatal("no protected cells inspected")
	}
}

// useDurableTransport is useTransport with a private durable ledger per
// endpoint, so a test can inspect each protected cell's job phase afterwards.
func (r *rig) useDurableTransport() []string {
	r.transport = make([]*guestbridge.Transport, len(r.bridges))
	r.transportReclaim = make([]*guestbridge.Ack, len(r.bridges))
	roots := make([]string, len(r.bridges))
	for i := range r.bridges {
		roots[i] = filepath.Join(r.t.TempDir(), "transport")
		var err error
		r.transport[i], err = guestbridge.NewDurableTransport(r.bridges[i], r.transportBudget(guestbridge.TransportLedgerMemoryBytes+(12<<20)), roots[i])
		must(r.t, err)
		r.t.Cleanup(r.transport[i].Close)
		r.cells[i] = 0
	}
	return roots
}

func (r *rig) ledgerBytes() []byte {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	raw, err := r.controller.rawState()
	must(r.t, err)
	return raw
}

// replayR5Recall completes a clean single-domain recall in the zq3-r5 order:
// STOPPED, poll INSTALL_DRAIN, INSTALLED_DRAIN, about 20 VIEW_QUERY polls that
// stay PENDING while the ledger waits for DRAINED, an identical INSTALLED_DRAIN
// resend that is ACCEPTED again, then DRAINED, INSTALLED_FINAL and REOPENED.
func (r *rig) replayR5Recall(ticket guestbridge.Ticket) {
	r.t.Helper()
	r.ack(0, "STOPPED", r.emptyManifest())
	if event := r.poll(0); !bytes.Contains(event, []byte(`"phase":"INSTALL_DRAIN"`)) {
		r.t.Fatalf("INSTALL_DRAIN not delivered %s", event)
	}
	first := r.ack(0, "INSTALLED_DRAIN", nil)
	for range 20 {
		if query := r.view(0, "VIEW_QUERY", map[string]any{"request": ticket}); query.Status != "PENDING" {
			r.t.Fatalf("release query while waiting for DRAINED %+v", query)
		}
	}
	if resend := r.ack(0, "INSTALLED_DRAIN", nil); !bytes.Equal(first, resend) {
		r.t.Fatalf("INSTALLED_DRAIN resend differs:\n%s\n%s", first, resend)
	}
	r.ack(0, "DRAINED", r.emptyManifest())
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if r.controller.state.Recall != nil {
		r.t.Fatal("r5 recall did not close")
	}
}
