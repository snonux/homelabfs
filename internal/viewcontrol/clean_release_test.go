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

// Complete the actual host empty-inventory protocol. Facts for each exact ACK
// are explicit primitive fixtures, never running-kernel quiescence evidence.
func (r *rig) completeCleanRecall() {
	r.t.Helper()
	r.controller.mu.Lock()
	var domains []int
	for _, reservation := range r.controller.state.Reservations {
		if reservation.Status != "CLOSED" {
			r.controller.mu.Unlock()
			r.t.Fatal("clean helper cannot omit a dirty obligation")
		}
	}
	for i, scope := range r.scopes {
		if _, ok := r.controller.state.Domains[scope.Session]; ok {
			domains = append(domains, i)
		}
	}
	r.controller.mu.Unlock()
	if len(domains) == 0 {
		r.t.Fatal("missing admitted recall domains")
	}
	for _, i := range domains {
		r.ack(i, "STOPPED", r.emptyManifest())
	}
	r.poll(domains[0])
	for _, i := range domains {
		r.ack(i, "INSTALLED_DRAIN", nil)
	}
	for _, i := range domains {
		r.ack(i, "DRAINED", r.emptyManifest())
	}
	for _, i := range domains {
		r.ack(i, "INSTALLED_FINAL", nil)
	}
	for _, i := range domains {
		r.ack(i, "REOPENED", nil)
	}
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if r.controller.state.Recall != nil {
		r.t.Fatal("clean recall did not close")
	}
}

func TestTransportCleanReleaseRequiresRecallBeforeWriteUpgrade(t *testing.T) {
	r := newRig(t)
	r.useTransport()
	_, description := r.create(0, "read-then-write", false)
	_, read := r.acquire(0, page(description, 0), "READ")
	ticket := r.controlTicket(0)
	payload := map[string]any{"request": ticket, "lease": read.Lease}
	// No VIEW_RELEASE physical fact exists: it must still start funded recall,
	// rather than ask a verifier to certify a LIVE readable PTE as closed.
	pending := r.view(0, "VIEW_RELEASE", payload)
	if pending.Status != "PENDING" {
		t.Fatalf("clean release %+v", pending)
	}
	if polled := r.poll(0); !bytes.Contains(polled, []byte(`"phase":"RECALL"`)) {
		t.Fatal("clean release did not deliver RECALL")
	}
	r.controller.mu.Lock()
	active := r.controller.state.Leases[read.Lease.ID].Active
	r.controller.mu.Unlock()
	if !active {
		t.Fatal("rights closed before actual STOPPED proof")
	}
	before := r.view(0, "VIEW_QUERY", map[string]any{"request": ticket})
	if before.Status != "PENDING" {
		t.Fatal("release escaped physical recall")
	}
	r.completeCleanRecall()
	// Even complete installed/reopened source state cannot replace the mandatory
	// exact release physical proof. Query remains an error, never RELEASED.
	if _, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_QUERY", mustRaw(t, map[string]any{"request": ticket})); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("missing final physical proof %v", err)
	}
	r.physical("VIEW_RELEASE", payload)
	released := r.view(0, "VIEW_QUERY", map[string]any{"request": ticket})
	if released.Status != "RELEASED" {
		t.Fatalf("final release %+v", released)
	}
	_, write := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	if write.Status != "GRANTED" || write.Lease.Mode != "EXCLUSIVE_DIRTY" || write.Lease.Fence <= read.Lease.Fence {
		t.Fatalf("read-first shared-write upgrade did not progress %+v", write)
	}
	_, reservedPayload := r.reserve(0, write.Lease, description)
	if reserved := r.view(0, "DIRTY_RESERVE", reservedPayload); reserved.Status != "GRANTED" || reserved.Reservation == nil {
		t.Fatal("upgraded write did not obtain immutable preowned dirty credit")
	}
	replay := r.view(0, "VIEW_RELEASE", payload)
	if replay.Status != "PENDING" {
		t.Fatal("historical initial release bytes mutated")
	}
}

func mustRaw(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := canonical(value)
	must(t, err)
	return raw
}

func TestTransportClean128PressureReclaimsBefore129thGrant(t *testing.T) {
	r := newRig(t, func(c *Config) {
		b := &c.Authorization.Enrollment.Bounds
		b.Tickets = 128
		b.ControlTickets = 132
		b.Identities = 512
		b.LedgerBytes = 96 << 20
		b.WorkBytes = 512 << 20
	})
	// The explicit enrollment oracle serializes two active retry lanes under the
	// race detector. This is a bounded component progress check, not a native
	// latency promise; other transport tests retain their three-second deadline.
	r.transportResponseWait = 20 * time.Second
	r.useTransport()
	_, description := r.create(0, "clean-window", false)
	reads := make([]viewReply, 128)
	ids := make([]string, 128)
	var prior *guestbridge.Ack
	protocol := guestbridge.ViewProtocol()
	for i := range reads {
		payload := map[string]any{"request": r.ticket(0), "domain": r.domains[0], "access": page(description, uint64(i)*4096), "mode": "READ", "target": nil}
		r.cells[0]++
		command := guestbridge.Command{Cell: r.cells[0], Context: r.scopes[0], Protocol: &protocol, Op: "VIEW_ACQUIRE", Payload: mustRaw(t, payload), Reclaim: prior}
		r.transportWrite(0, command)
		response := r.transportRead(0, command)
		var envelope struct{ Payload viewReply }
		must(t, json.Unmarshal(response, &envelope))
		reads[i] = envelope.Payload
		if reads[i].Status != "GRANTED" {
			t.Fatalf("clean page%d %+v", i, reads[i])
		}
		ids[i] = reads[i].ID
		// Reclaim actual transient framed snapshots on the next command,
		// retaining all protected view tickets and lease release credits.
		prior = &guestbridge.Ack{Cell: command.Cell, ResponseHash: hash(response)}
	}
	original := map[string]any{"request": r.ticket(0), "domain": r.domains[0], "access": page(description, 128*4096), "mode": "READ", "target": nil}
	r.cells[0]++
	accepted := guestbridge.Command{Cell: r.cells[0], Context: r.scopes[0], Protocol: &protocol, Op: "VIEW_ACQUIRE", Payload: mustRaw(t, original), Reclaim: prior}
	r.transportWrite(0, accepted)
	if _, err := r.transport[0].Read(t.Context(), r.natives[0], r.natives[0].Endpoint.ResultNode, accepted.Cell*guestbridge.CellStride, 32768); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("129th was not actually acquisition-blocked %v", err)
	}
	releaseTicket := r.controlTicket(0)
	payload := map[string]any{"request": releaseTicket, "lease": reads[0].Lease}
	pending := r.view(0, "VIEW_RELEASE", payload)
	if pending.Status != "PENDING" || !bytes.Contains(r.poll(0), []byte(`"phase":"RECALL"`)) {
		t.Fatal("preowned clean release failed under acquisition pressure")
	}
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	closedIDs := make([]string, 128)
	closed := r.view(0, "VIEW_QUERY", map[string]any{"request": releaseTicket})
	closedIDs[0] = closed.ID
	if closed.Status != "RELEASED" {
		t.Fatal("first clean release not closed")
	}
	for i := 1; i < 128; i++ {
		closed = r.release(0, reads[i].Lease)
		if closed.Status != "RELEASED" {
			t.Fatal("remaining clean sources not closed")
		}
		closedIDs[i] = closed.ID
	}
	r.retireControl(0, 128, closedIDs)
	if floor := r.retire(0, 128, ids); !bytes.Contains(floor, []byte(`"floor":128`)) {
		t.Fatalf("clean acquisition arena not reclaimed %s", floor)
	}
	response := r.transportRead(0, accepted)
	var envelope struct{ Payload viewReply }
	must(t, json.Unmarshal(response, &envelope))
	if envelope.Payload.Status != "GRANTED" || envelope.Payload.Lease.Fence <= reads[0].Lease.Fence {
		t.Fatalf("exact original129th did not progress %+v", envelope.Payload)
	}
}
