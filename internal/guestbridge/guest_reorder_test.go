package guestbridge

import (
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// guestWait is a poll-index-0 VIEW_WAIT original for guest cell guest at
// native cell native, optionally reclaiming an EMPTY donor wait.
func guestWait(t *testing.T, x *viewDispatchFixture, native, guest uint64, reclaim, guestReclaim *Ack) Command {
	p := MappedProtocol()
	return Command{Cell: native, Context: x.bridge.c, Protocol: &p, Op: "VIEW_POLL", Payload: mustJSON(t, map[string]any{"domain": x.domain, "after_sequence": 0}), Reclaim: reclaim, Delegation: &Delegation{Version: DelegationVersion, GuestCell: guest, GuestOp: "VIEW_WAIT", GuestReclaim: guestReclaim}}
}

// donorAcks fetches one completed native response and returns its native and
// translated guest reclaim acknowledgements.
func donorAcks(t *testing.T, raw []byte, native, guest uint64, op string) (*Ack, *Ack) {
	t.Helper()
	translated, err := translatedResponseHash(raw, guest, op, false)
	if err != nil {
		t.Fatal(err)
	}
	return &Ack{Cell: native, ResponseHash: digest(raw)}, &Ack{Cell: guest, ResponseHash: translated}
}

// TestMappedOutOfOrderGuestWaitAcceptedReplayRefused is the R6-2 regression.
// The exporter forwarded guest cell 48 (VIEW_WAIT, reclaiming an EMPTY wait)
// as native 108 after guest cell 49 (QUERY, with its own donor) as native
// 107. Guest 48 was never admitted, so it must be accepted even though it is
// below the session's guest high-water; an exact replay of an admitted guest
// cell must still be refused.
func TestMappedOutOfOrderGuestWaitAcceptedReplayRefused(t *testing.T) {
	x, tr, root := mappedWaitFixture(t)
	n := x.bridge.native
	if err := mappingWrite(t, tr, n, guestWait(t, x, 1, 1, nil, nil), 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	waitNative, waitGuest := donorAcks(t, fetchNativeAt(t, tr, n, 1, n.Endpoint.RecallNode), 1, 1, "VIEW_WAIT")
	if err := mappingWrite(t, tr, n, mappedHello(t, x.bridge, 2, 2), 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	helloNative, helloGuest := donorAcks(t, fetchNative(t, tr, n, 2), 2, 2, "HELLO")
	later := mappedHello(t, x.bridge, 3, 4)
	later.Reclaim, later.Delegation.GuestReclaim = helloNative, helloGuest
	if err := mappingWrite(t, tr, n, later, 2, 0, -1); err != nil {
		t.Fatal("guest 4 with donor", err)
	}
	_ = fetchNative(t, tr, n, 3)
	if err := mappingWrite(t, tr, n, guestWait(t, x, 4, 3, waitNative, waitGuest), 3, 0, -1); err != nil {
		t.Fatal("unissued earlier guest VIEW_WAIT refused after a later guest cell", err)
	}
	if raw := fetchNativeAt(t, tr, n, 4, n.Endpoint.RecallNode); len(raw) == 0 {
		t.Fatal("out-of-order VIEW_WAIT not answered")
	}
	if err := mappingWrite(t, tr, n, guestWait(t, x, 5, 3, nil, nil), 1, 0, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("replayed out-of-order guest cell admitted", err)
	}
	tr.Close()
	state, _ := inspectCells(t, root)
	if state.GuestHigh[x.bridge.c.Session] != 4 || len(state.GuestHoles) != 0 {
		t.Fatalf("guest freshness index high=%d holes=%v", state.GuestHigh[x.bridge.c.Session], state.GuestHoles)
	}
}

// TestMappedGuestReorderWindowBounded keeps the hole set bounded: an
// unissued guest cell is admitted only while it is inside the reorder window
// below the high-water; older ones, and every admitted one, stay refused.
func TestMappedGuestReorderWindowBounded(t *testing.T) {
	f, tr, root := mappedFixture(t)
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 1, 1), 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	high := uint64(1 + guestReorderWindow + 3)
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 2, high), 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	holes := append([]uint64(nil), tr.guestHoles[f.c.Session]...)
	tr.mu.Unlock()
	if len(holes) != guestReorderWindow-1 || holes[0] != high-guestReorderWindow+1 || holes[len(holes)-1] != high-1 {
		t.Fatalf("reorder holes %v below high %d", holes, high)
	}
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 3, holes[0]-1), 0, 0, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("guest cell below the reorder window admitted", err)
	}
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 4, holes[0]), 0, 0, -1); err != nil {
		t.Fatal("guest cell inside the reorder window refused", err)
	}
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 5, holes[0]), 0, 0, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("filled hole admitted twice", err)
	}
	tr.Close()
	state, _ := inspectCells(t, root)
	if got := state.GuestHoles[f.c.Session]; len(got) != guestReorderWindow-2 || got[0] != holes[1] {
		t.Fatalf("protected holes %v", got)
	}
}

// TestGuestHolesValidation refuses protected hole lists that could admit a
// spent or out-of-window guest cell after restore.
func TestGuestHolesValidation(t *testing.T) {
	const session = "s"
	base := func(holes ...uint64) ProtectedTransportState {
		return ProtectedTransportState{GuestHigh: map[string]uint64{session: 40}, GuestHoles: map[string][]uint64{session: holes}}
	}
	if err := validateGuestHoles(base(30, 39), map[GuestIssuance]bool{}); err != nil {
		t.Fatal("valid holes refused", err)
	}
	retired := base(39)
	retired.Terminal = map[string]uint64{session: 9}
	cases := map[string]struct {
		state  ProtectedTransportState
		issued map[GuestIssuance]bool
	}{
		"empty":         {base(), nil},
		"at-high":       {base(40), nil},
		"below-window":  {base(40 - guestReorderWindow), nil},
		"unsorted":      {base(39, 30), nil},
		"issued-marker": {base(35), map[GuestIssuance]bool{{session, 35}: true}},
		"unknown":       {ProtectedTransportState{GuestHoles: map[string][]uint64{session: {1}}}, nil},
		"retired-scope": {retired, nil},
	}
	for name, c := range cases {
		if validateGuestHoles(c.state, c.issued) == nil {
			t.Errorf("%s holes admitted", name)
		}
	}
}
