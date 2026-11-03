package viewcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func (r *rig) native(command fsapi.Command) fsapi.Result {
	r.t.Helper()
	request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
	must(r.t, r.service.Dispatch(r.t.Context(), request, command))
	result, err := request.Completion().Wait(r.t.Context())
	must(r.t, err)
	return result
}
func (r *rig) drainSingle(reservation guestbridge.DirtyReservation, data string) {
	r.t.Helper()
	payload := freezeRequest(r.controlTicket(0), reservation, r.controller.state.Recall.ID, data)
	reply := r.view(0, "DIRTY_FREEZE", payload)
	manifest, _ := canonical(map[string]any{"entries": []json.RawMessage{reply.Entry}, "hash": digest([]json.RawMessage{reply.Entry})})
	r.ack(0, "STOPPED", manifest)
	raw, _ := canonical(payload)
	var request request
	must(r.t, json.Unmarshal(raw, &request))
	_, call, err := frozen(request)
	must(r.t, err)
	drained := r.sendCall(0, call)
	if drained.Code != "OK" {
		r.t.Fatalf("preowned drain refused %+v", drained)
	}
	source, err := r.controller.capture(r.t.Context())
	must(r.t, err)
	if int64(len(source.State)) > r.cc.Authorization.Enrollment.Bounds.SourceBytes {
		r.t.Fatal("accepted drain exceeded source bytes")
	}
	r.poll(0)
	if r.controller.state.Recall.Phase != "INSTALL_DRAIN" {
		r.t.Fatal("accepted drain stranded intermediate install")
	}
	r.ack(0, "INSTALLED_DRAIN", nil)
	r.ack(0, "DRAINED", manifest)
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
}

func TestDirtyGrowthCreditRejectsReviewerSparseOffsetBeforePTE(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "r1-dirty", false)
	before, err := r.controller.capture(t.Context())
	must(t, err)
	_, lease := r.acquire(0, page(description, 65536), "EXCLUSIVE_DIRTY")
	ticket, payload := r.reserve(0, lease.Lease, description)
	reserved := r.view(0, "DIRTY_RESERVE", payload)
	if reserved.Status != "BLOCKED" || reserved.Reservation != nil || len(r.controller.state.Reservations) != 0 {
		t.Fatalf("unrepresentable future page received PTE credit %+v", reserved)
	}
	after, err := r.controller.capture(t.Context())
	must(t, err)
	if !sameSource(before, after) {
		t.Fatal("dirty byte-credit refusal changed source")
	}
	r.physical("VIEW_CANCEL", payload)
	closed := r.view(0, "VIEW_CANCEL", map[string]any{"request": ticket})
	if closed.Status != "CANCELLED" {
		t.Fatal("declined future number was not closed")
	}
}

func TestAggregateOutstandingDirtyGrowthAndReceiptCredit(t *testing.T) {
	r := newRig(t)
	_, a := r.create(0, "aggregate-a", false)
	_, b := r.create(0, "aggregate-b", false)
	_, first := r.acquire(0, page(a, 28672), "EXCLUSIVE_DIRTY")
	_, firstPayload := r.reserve(0, first.Lease, a)
	reserved := r.view(0, "DIRTY_RESERVE", firstPayload)
	if reserved.Status != "GRANTED" {
		t.Fatal("single bounded future extent refused")
	}
	_, second := r.acquire(0, page(b, 28672), "EXCLUSIVE_DIRTY")
	_, secondPayload := r.reserve(0, second.Lease, b)
	blocked := r.view(0, "DIRTY_RESERVE", secondPayload)
	if blocked.Status != "BLOCKED" || len(r.controller.state.Reservations) != 1 {
		t.Fatal("individually fitting pages overcommitted aggregate source bytes")
	}
	r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	r.drainSingle(*reserved.Reservation, "61")
}

func TestOrdinaryAndNativeSparseGrowthRejectBeforeAnySourceEffect(t *testing.T) {
	r := newRig(t)
	object, description := r.create(0, "r1-ordinary", false)
	before, err := r.controller.capture(t.Context())
	must(t, err)
	result := r.call(0, "WRITE", map[string]any{"description": description, "offset": 65536, "data": "61", "max_end": 65537})
	if result.Code != "NoSpace" || result.Count != 0 {
		t.Fatalf("ordinary sparse write effected before source credit %+v", result)
	}
	after, err := r.controller.capture(t.Context())
	must(t, err)
	if !sameSource(before, after) {
		t.Fatal("rejected ordinary write changed journal/catalog/payload")
	}
	home, err := fsapi.ParseUUID(object.Home)
	must(t, err)
	allocation, err := fsapi.ParseUUID(object.Allocation)
	must(t, err)
	opened := r.native(fsapi.Command{Operation: fsapi.Open, Mount: home, Object: fsapi.ObjectID{Home: home, Allocation: allocation}, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
	if opened.Code != fsapi.OK {
		t.Fatalf("native open %+v", opened)
	}
	nativeWrite := r.native(fsapi.Command{Operation: fsapi.Write, Mount: home, Object: fsapi.ObjectID{Home: home, Allocation: allocation}, Generation: opened.Generation, Handle: opened.Handle, Offset: 65536, Data: []byte("x")})
	if nativeWrite.Code != fsapi.NoSpace || nativeWrite.Count != 0 {
		t.Fatalf("native write escaped source credit %+v", nativeWrite)
	}
	after, err = r.controller.capture(t.Context())
	must(t, err)
	if !sameSource(before, after) {
		t.Fatal("rejected native write changed source")
	}
	native := r.native(fsapi.Command{Operation: fsapi.Truncate, Mount: home, Object: fsapi.ObjectID{Home: home, Allocation: allocation}, Size: 65537})
	if native.Code != fsapi.NoSpace {
		t.Fatalf("native sparse growth escaped preflight %+v", native)
	}
	after, err = r.controller.capture(t.Context())
	must(t, err)
	if !sameSource(before, after) {
		t.Fatal("rejected native truncate changed source")
	}
	_, grant := r.acquire(0, page(description, 0), "READ")
	if grant.Status != "GRANTED" {
		t.Fatal("rejected write stranded subsequent source grant")
	}
}

func TestNativeCatalogPinAndReceiptBoundIsCheckedBeforeCreation(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Authorization.Enrollment.Bounds.SourceBytes = 4096 })
	root := r.service.FrontendInfo().Root
	accepted := 0
	for i := range 16 {
		before, err := r.controller.capture(t.Context())
		must(t, err)
		result := r.native(fsapi.Command{Operation: fsapi.Create, Mount: root.Home, Parent: root, Name: string([]byte{'m', byte('a' + i)}), Mode: 0660, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
		after, err := r.controller.capture(t.Context())
		must(t, err)
		if result.Code == fsapi.NoSpace {
			if accepted == 0 {
				t.Fatal("fixture admitted no metadata effects")
			}
			if !sameSource(before, after) {
				t.Fatal("catalog/pin/receipt refusal occurred after creation")
			}
			return
		}
		if result.Code != fsapi.OK {
			t.Fatalf("native create %+v", result)
		}
		accepted++
	}
	t.Fatal("catalog/receipt profile bound was not reached")
}

func TestDeclinedSourceNumberClosureCrashKeepsOriginalBindingAndProof(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "declined-crash", false)
	_, lease := r.acquire(0, page(description, 65536), "EXCLUSIVE_DIRTY")
	ticket, payload := r.isolatedReserve(lease.Lease, description)
	declined := r.view(0, "DIRTY_RESERVE", payload)
	if declined.Status != "BLOCKED" {
		t.Fatal("source-bound reserve unexpectedly granted")
	}
	r.physical("VIEW_CANCEL", payload)
	crash := errors.New("declined gap closed before ticket")
	r.controller.config.Failpoint = func(point string) error {
		if point == "declined-gap-closed" {
			return crash
		}
		return nil
	}
	raw, _ := canonical(map[string]any{"request": ticket})
	if _, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_CANCEL", raw); !errors.Is(err, crash) {
		t.Fatalf("missing closure crash %v", err)
	}
	r.reopen()
	response, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_QUERY", raw)
	must(t, err)
	var closed viewReply
	must(t, json.Unmarshal(response, &closed))
	if closed.Status != "CANCELLED" {
		t.Fatalf("declined gap did not reconcile %s", response)
	}
	if proof := r.retireGap(closed.ID); !bytes.Contains(proof, []byte(`"floor":1`)) {
		t.Fatalf("declined ordinary proof lost %s", proof)
	}
	original, _ := canonical(payload)
	replay, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_RESERVE", original)
	must(t, err)
	if status(replay) != "BLOCKED" {
		t.Fatal("closure rewrote original declined reply")
	}
}
