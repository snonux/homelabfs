package viewcontrol

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// These tests use production framing/transport/service, real local journals and
// ledgers. Native and physical facts remain explicit primitive-boundary shims.
func (r *rig) useTransport() {
	r.transport = make([]*guestbridge.Transport, len(r.bridges))
	r.transportReclaim = make([]*guestbridge.Ack, len(r.bridges))
	for i := range r.bridges {
		var config admission.Config
		for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: 12 << 20} {
			resources := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
			config.Pools[class] = admission.Limits{Global: resources, PerPeer: resources, PerVolume: resources}
		}
		budget, err := admission.New(config)
		must(r.t, err)
		r.transport[i], err = guestbridge.NewTransport(r.bridges[i], budget)
		must(r.t, err)
		r.t.Cleanup(r.transport[i].Close)
		r.cells[i] = 0
	}
}
func (r *rig) transportWrite(i int, c guestbridge.Command) {
	r.t.Helper()
	raw, err := canonical(c)
	must(r.t, err)
	flags := uint16(1)
	if c.Op == "CALL" {
		var call guestbridge.Call
		must(r.t, json.Unmarshal(c.Payload, &call))
		if call.Intent.Opcode != "DRAIN_WRITE" {
			flags = 0
		}
	}
	if c.Op == "VIEW_ACQUIRE" || c.Op == "DIRTY_RESERVE" || c.Op == "HELLO" {
		flags = 0
	}
	if c.Reclaim != nil {
		flags |= 2
	}
	session, err := fsapi.ParseUUID(c.Context.Session)
	must(r.t, err)
	for offset := 0; offset < len(raw); offset += guestbridge.ChunkBytes {
		wire, err := guestbridge.EncodeFrame(guestbridge.Frame{Minor: 1, Kind: 1, Flags: flags, Total: uint32(len(raw)), Offset: uint32(offset), Session: session, Cell: c.Cell, Hash: sha256.Sum256(raw), Data: raw[offset:min(offset+guestbridge.ChunkBytes, len(raw))]})
		must(r.t, err)
		deadline := time.Now().Add(3 * time.Second)
		for {
			_, err = r.transport[i].Write(r.t.Context(), r.natives[i], r.natives[i].Endpoint.CommandNode, c.Cell*guestbridge.CellStride+uint64(offset/guestbridge.ChunkBytes)*(guestbridge.HeaderBytes+guestbridge.ChunkBytes), wire)
			if !errors.Is(err, fsapi.WouldBlock) || time.Now().After(deadline) {
				must(r.t, err)
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
}
func (r *rig) transportRead(i int, c guestbridge.Command) []byte {
	r.t.Helper()
	node := r.natives[i].Endpoint.ResultNode
	if c.Op == "VIEW_POLL" {
		node = r.natives[i].Endpoint.RecallNode
	}
	responseWait := r.transportResponseWait
	if responseWait == 0 {
		responseWait = 3 * time.Second
	}
	deadline := time.NewTimer(responseWait)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		raw, err := r.transport[i].Read(r.t.Context(), r.natives[i], node, c.Cell*guestbridge.CellStride, 32768)
		if err == nil {
			frame, e := guestbridge.DecodeFrame(raw)
			must(r.t, e)
			if len(frame.Data) != int(frame.Total) {
				r.t.Fatal("expected bounded single frame")
			}
			return frame.Data
		}
		if !errors.Is(err, fsapi.WouldBlock) {
			r.t.Fatal(err)
		}
		select {
		case <-deadline.C:
			r.t.Fatalf("accepted %s cell %d stranded", c.Op, c.Cell)
		case <-tick.C:
		}
	}
}
func (r *rig) originalCall(i int, op string, args any) guestbridge.Call {
	r.next[i]++
	raw, err := canonical(args)
	must(r.t, err)
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	claim := guestbridge.Claim{Realm: uid(100).String(), Subject: "616263", Anchor: uid(byte(101 + i)).String(), Nonce: uid(byte(103 + i)).String(), Number: r.next[i], OriginalHash: digest(intent)}
	must(r.t, r.providers[i].Allocate(r.t.Context(), r.scopes[i], claim))
	return guestbridge.Call{Claim: claim, Credentials: r.credentials[i], Intent: intent}
}
func TestTransportOriginalCallResumesAfterRecall(t *testing.T) {
	for _, op := range []string{"READ", "WRITE"} {
		for _, resume := range []string{"QUERY", "CALL"} {
			t.Run(op+resume, func(t *testing.T) {
				r := newRig(t)
				_, description := r.create(0, "parked", false)
				r.useTransport()
				_, lease := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
				if lease.Status != "GRANTED" {
					t.Fatal(lease)
				}
				args := map[string]any{"description": description, "offset": 0, "count": 1}
				if op == "WRITE" {
					args = map[string]any{"description": description, "offset": 0, "data": "61", "max_end": 4096}
				}
				original := r.originalCall(0, op, args)
				first, err := r.execute(0, "CALL", original)
				must(t, err)
				if !bytes.Contains(first, []byte(`"status":"ADMITTED"`)) {
					t.Fatal(string(first))
				}
				pending, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
				must(t, err)
				if bytes.Contains(pending, []byte(`"status":"TERMINAL"`)) {
					t.Fatal("planned through dirty lease")
				}
				changed := original
				changed.Credentials.Revision++
				changedRaw, err := canonical(changed)
				must(t, err)
				protocol := guestbridge.ViewProtocol()
				rejected, err := r.bridges[0].Execute(t.Context(), r.natives[0], guestbridge.Command{Cell: 999, Context: r.scopes[0], Protocol: &protocol, Op: "CALL", Payload: changedRaw})
				if err == nil && !bytes.Contains(rejected, []byte(`"code":"ID_CONFLICT"`)) {
					t.Fatalf("changed credentials accepted %s", rejected)
				}

				r.ack(0, "STOPPED", r.emptyManifest())
				r.poll(0)
				r.ack(0, "INSTALLED_DRAIN", nil)
				r.ack(0, "DRAINED", r.emptyManifest())
				r.ack(0, "INSTALLED_FINAL", nil)
				r.ack(0, "REOPENED", nil)
				if resume == "CALL" {
					_, err = r.execute(0, "CALL", original)
					must(t, err)
				}
				terminal, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
				must(t, err)
				if !bytes.Contains(terminal, []byte(`"status":"TERMINAL"`)) || !bytes.Contains(terminal, []byte(`"code":"OK"`)) {
					t.Fatal(string(terminal))
				}
				again, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
				must(t, err)
				if !bytes.Equal(terminal, again) {
					t.Fatal("terminal original changed")
				}

				if op == "WRITE" {
					state, err := r.providers[0].Resolve(t.Context(), r.scopes[0], original.Claim)
					must(t, err)
					reader, err := segment.OpenReader(t.Context(), r.config.Source, r.config.Stream, segment.RecoveryOptions{}, fsapi.Position{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Sequence: 1})
					must(t, err)
					count := 0
					for {
						entry, err := reader.Next(t.Context())
						if errors.Is(err, io.EOF) {
							break
						}
						must(t, err)
						if entry.Record.Header.Operation == state.Binding.Operation {
							if _, ok := entry.Record.Payload.(record.Mutation); ok {
								count++
							}
						}
					}
					must(t, reader.Close())
					if count != 1 {
						t.Fatalf("original mutation journal records=%d", count)
					}
				}
			})
		}
	}
}

func TestTransportAcquisitionRetryRetainsOriginalWhileControlDrains(t *testing.T) {
	r := newRig(t, func(c *Config) {
		b := &c.Authorization.Enrollment.Bounds
		b.Tickets = 3
		b.ControlTickets = 8
		b.Reservations = 2
		b.Identities = 64
		b.Sources = 16
		b.LedgerBytes = 12 << 20
		b.WorkBytes = 192 << 20
	})
	r.useTransport()
	_, description := r.create(0, "credits", false)
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	_, reservePayload := r.reserve(0, dirty.Lease, description)
	reserved := r.view(0, "DIRTY_RESERVE", reservePayload)
	_, other := r.acquire(0, page(description, 4096), "EXCLUSIVE_DIRTY")
	originalTicket := r.ticket(0)
	number := guestbridge.ClaimNumber{Realm: uid(201).String(), Subject: "616263", Anchor: uid(202).String(), Nonce: uid(203).String(), Number: 1}
	original := map[string]any{"request": originalTicket, "lease": other.Lease, "description": description, "offset": 4096, "length": 4096, "claim_number": number}
	r.cells[0]++
	protocol := guestbridge.ViewProtocol()
	rawOriginal, err := canonical(original)
	must(t, err)
	accepted := guestbridge.Command{Cell: r.cells[0], Context: r.scopes[0], Protocol: &protocol, Op: "DIRTY_RESERVE", Payload: rawOriginal}
	r.transportWrite(0, accepted)
	// Neither duplicated native receipt nor a timed retry consumes another
	// request identity, counter or claim.
	r.transportWrite(0, accepted)
	// A second cell carrying the same original waits behind the first in the same worker's FIFO ring.
	// Neither a refusal nor the acquisition-family counter hole may monopolize it.
	secondPayload := original
	secondRaw, err := canonical(secondPayload)
	must(t, err)
	r.cells[0]++
	secondAccepted := guestbridge.Command{Cell: r.cells[0], Context: r.scopes[0], Protocol: &protocol, Op: "DIRTY_RESERVE", Payload: secondRaw}
	r.transportWrite(0, secondAccepted)
	if _, err = r.transport[0].Read(t.Context(), r.natives[0], r.natives[0].Endpoint.ResultNode, accepted.Cell*guestbridge.CellStride, 32768); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("pressure response %v", err)
	}

	// Entirely guest initiated: a preowned RELEASE ticket starts recall even
	// though pressure READ cannot pass the acquisition-family counter hole.
	releaseTicket := r.controlTicket(0)
	releasePayload := map[string]any{"request": releaseTicket, "lease": dirty.Lease}
	r.physical("VIEW_RELEASE", releasePayload)
	pendingRelease := r.view(0, "VIEW_RELEASE", releasePayload)
	if pendingRelease.Status != "PENDING" {
		t.Fatal("funded guest release did not trigger pressure recall")
	}
	polled := r.poll(0)
	if !bytes.Contains(polled, []byte(`"phase":"RECALL"`)) {
		t.Fatal("guest poll did not receive recall")
	}
	var event struct{ Event struct{ Recall string } }
	must(t, json.Unmarshal(polled, &event))
	frozenPayload := freezeRequest(r.controlTicket(0), *reserved.Reservation, event.Event.Recall, "616263")
	frozenReply := r.view(0, "DIRTY_FREEZE", frozenPayload)
	manifest, _ := canonical(map[string]any{"hash": digest([]json.RawMessage{frozenReply.Entry}), "entries": []json.RawMessage{frozenReply.Entry}})
	r.ack(0, "STOPPED", manifest)
	raw, _ := canonical(frozenPayload)
	var parsed request
	must(t, json.Unmarshal(raw, &parsed))
	_, call, err := frozen(parsed)
	must(t, err)
	r.sendCall(0, call)
	r.poll(0)
	r.ack(0, "INSTALLED_DRAIN", nil)
	r.ack(0, "DRAINED", manifest)
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
	firstClosed := r.view(0, "VIEW_QUERY", map[string]any{"request": releaseTicket})
	secondClosed := r.release(0, other.Lease)
	if firstClosed.Status != "RELEASED" || secondClosed.Status != "RELEASED" {
		t.Fatal("funded releases did not close after actual source closure")
	}
	r.retireControl(0, 3, []string{firstClosed.ID, frozenReply.ID, secondClosed.ID})
	r.retire(0, 3, []string{dirty.ID, reserved.ID, other.ID})
	// Retry EXACT original bytes/number. Its stale lease has no authority; source-
	// backed cancellation must close the untouched future ordinary number too.
	response := r.transportRead(0, accepted)
	var envelope struct{ Payload viewReply }
	must(t, json.Unmarshal(response, &envelope))
	resolved := envelope.Payload
	secondResponse := r.transportRead(0, secondAccepted)
	var secondEnvelope struct{ Payload viewReply }
	must(t, json.Unmarshal(secondResponse, &secondEnvelope))
	if secondEnvelope.Payload.Status != "BLOCKED" {
		t.Fatal("later FIFO cell did not progress")
	}

	r.transportWrite(0, accepted)
	if !bytes.Equal(response, r.transportRead(0, accepted)) {
		t.Fatal("duplicate accepted response mutated")
	}

	if resolved.Status != "BLOCKED" {
		t.Fatal("old original request gained new authority")
	}
	if retired := r.retire(0, 4, []string{resolved.ID}); !bytes.Contains(retired, []byte(`"status":"BLOCKED"`)) {
		t.Fatal("view floor orphaned declined ordinary number")
	}
	r.physical("VIEW_CANCEL", original)
	closed := r.view(0, "VIEW_CANCEL", map[string]any{"request": originalTicket})
	if closed.Status != "CANCELLED" {
		t.Fatal("original gap did not close")
	}
	if gap := r.retireGap(closed.ID); !bytes.Contains(gap, []byte(`"floor":1`)) {
		t.Fatalf("ordinary unbound gap not retired %s", gap)
	}
	if floor := r.retire(0, 4, []string{closed.ID}); !bytes.Contains(floor, []byte(`"floor":4`)) {
		t.Fatal("original view gap not retired")
	}
}

func transportControl(c guestbridge.Command) bool {
	if c.Op == "VIEW_ACQUIRE" || c.Op == "DIRTY_RESERVE" || c.Op == "HELLO" {
		return false
	}
	if c.Op == "CALL" {
		var call guestbridge.Call
		_ = json.Unmarshal(c.Payload, &call)
		return call.Intent.Opcode == "DRAIN_WRITE"
	}
	return true
}

func TestTransportCloseDoesNotWaitForUnresolvedPressure(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Authorization.Enrollment.Bounds.Tickets = 1 })
	_, description := r.create(0, "shutdown-pressure", false)
	r.useTransport()
	_, first := r.acquire(0, page(description, 0), "READ")
	if first.Status != "GRANTED" {
		t.Fatal(first)
	}
	payload := map[string]any{"request": r.ticket(0), "domain": r.domains[0], "access": page(description, 4096), "mode": "READ", "target": nil}
	raw, err := canonical(payload)
	must(t, err)
	r.cells[0]++
	protocol := guestbridge.ViewProtocol()
	command := guestbridge.Command{Cell: r.cells[0], Context: r.scopes[0], Protocol: &protocol, Op: "VIEW_ACQUIRE", Payload: raw}
	r.transportWrite(0, command)
	// Give this accepted job a real execution/retry opportunity before closing.
	timer := time.NewTimer(25 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	if _, err = r.transport[0].Read(t.Context(), r.natives[0], r.natives[0].Endpoint.ResultNode, command.Cell*guestbridge.CellStride, 32768); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { r.transport[0].Close(); close(done) }()
	limit := time.NewTimer(time.Second)
	defer limit.Stop()
	select {
	case <-done:
	case <-limit.C:
		t.Fatal("Close waited for unresolved acquisition")
	}
	r.transport[0].Close()
}

func TestTransportInitialCallQueryAndDuplicateRunConcurrently(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "concurrent", false)
	r.useTransport()
	original := r.originalCall(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61", "max_end": 4096})
	protocol := guestbridge.ViewProtocol()
	payload, err := canonical(original)
	must(t, err)
	call := guestbridge.Command{Cell: 1, Context: r.scopes[0], Protocol: &protocol, Op: "CALL", Payload: payload}
	raw, err := canonical(map[string]any{"claim": original.Claim})
	must(t, err)
	query := guestbridge.Command{Cell: 2, Context: r.scopes[0], Protocol: &protocol, Op: "QUERY", Payload: raw}
	duplicate := call
	duplicate.Cell = 3
	r.transportWrite(0, call)
	r.transportWrite(0, query)
	r.transportWrite(0, duplicate)
	initial := r.transportRead(0, call)
	r.transportRead(0, query)
	r.transportRead(0, duplicate)
	r.cells[0] = 3
	terminal, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
	must(t, err)
	if !bytes.Contains(terminal, []byte(`"status":"TERMINAL"`)) || !bytes.Contains(terminal, []byte(`"code":"OK"`)) {
		t.Fatal(string(terminal))
	}
	r.transportWrite(0, call)
	if !bytes.Equal(initial, r.transportRead(0, call)) {
		t.Fatal("initial response mutated")
	}
	read := r.call(0, "READ", map[string]any{"description": description, "offset": 0, "count": 8})
	if read.Data != "61" {
		t.Fatalf("original write changed %s", read.Data)
	}
}
