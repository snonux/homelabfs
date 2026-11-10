package guestbridge

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

// Tests for the cleanup of permanently refused cells (n64, refusal.go). They
// use the refusals the lib9p exporter fails back after e64 and p64.

// writeBody writes every chunk of body into native cell under the given frame
// minor, flags and hash, in order, as the exporter does, and returns the first
// refusal.
func writeBody(t *testing.T, tr *Transport, n Native, session fsapi.UUID, cell uint64, minor, flags uint16, body []byte, hash [32]byte) error {
	t.Helper()
	for offset := 0; offset < len(body); offset += ChunkBytes {
		wire, err := EncodeFrame(Frame{Minor: minor, Kind: 1, Flags: flags, Total: uint32(len(body)), Offset: uint32(offset), Session: session, Cell: cell, Hash: hash, Data: body[offset:min(offset+ChunkBytes, len(body))]})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tr.Write(t.Context(), n, n.Endpoint.CommandNode, cell*CellStride+uint64(offset/ChunkBytes)*(HeaderBytes+ChunkBytes), wire); err != nil {
			return err
		}
	}
	return nil
}

// badHashWrite writes the mapped command c under a frame hash that does not
// match its body, so the completing chunk is refused Invalid.
func badHashWrite(t *testing.T, tr *Transport, n Native, c Command, flags uint16) error {
	t.Helper()
	raw := mustJSON(t, c)
	return writeBody(t, tr, n, parseID(c.Context.Session), c.Cell, 4, flags, raw, sha256.Sum256(append([]byte("not "), raw...)))
}

// admitHello admits a mapped HELLO and returns its native and guest acks.
func admitHello(t *testing.T, f *bridgeFixture, tr *Transport, native, guest uint64, reclaim, guestReclaim *Ack) (*Ack, *Ack) {
	t.Helper()
	command := mappedHello(t, f, native, guest)
	command.Reclaim, command.Delegation.GuestReclaim = reclaim, guestReclaim
	flags := uint16(0)
	if reclaim != nil {
		flags = 2
	}
	if err := mappingWrite(t, tr, f.native, command, flags, 0, -1); err != nil {
		t.Fatalf("native %d guest %d: %v", native, guest, err)
	}
	return donorAcks(t, fetchNative(t, tr, f.native, native), native, guest, "HELLO")
}

// creditConsistent checks that the credit counters equal the live cells.
func creditConsistent(t *testing.T, tr *Transport) {
	t.Helper()
	tr.mu.Lock()
	defer tr.mu.Unlock()
	ordinary, control := 0, 0
	for _, c := range tr.cells {
		if c.reservation.Control {
			control++
		} else {
			ordinary++
		}
	}
	if ordinary != tr.ordinary || control != tr.control {
		t.Fatalf("credit %d/%d for %d/%d live cells", tr.ordinary, tr.control, ordinary, control)
	}
}

// TestRefusedDonorSendFreesWorkspace: a donor send refused for good (e64:
// EACCES, ESTALE, EINVAL) leaves the reclaim workspace, so the next new cell
// takes the same native number instead of getting WouldBlock for good.
func TestRefusedDonorSendFreesWorkspace(t *testing.T) {
	for _, refusal := range []fsapi.Errno{fsapi.Permission, fsapi.Stale, fsapi.Invalid} {
		t.Run(refusal.Error(), func(t *testing.T) {
			f, tr, root := mappedFixture(t)
			native, guest := admitHello(t, f, tr, 1, 1, nil, nil)
			donor := mappedHello(t, f, 2, 2)
			donor.Reclaim, donor.Delegation.GuestReclaim = native, guest
			var err error
			switch refusal {
			case fsapi.Permission:
				wrong := *guest
				wrong.ResponseHash = digest([]byte("wrong"))
				donor.Delegation.GuestReclaim = &wrong
				err = mappingWrite(t, tr, f.native, donor, 2, 0, -1)
			case fsapi.Stale: // guest cell 1 was admitted already
				donor.Delegation.GuestCell = 1
				err = mappingWrite(t, tr, f.native, donor, 2, 0, -1)
			case fsapi.Invalid:
				err = badHashWrite(t, tr, f.native, donor, 2)
			}
			if !errors.Is(err, refusal) {
				t.Fatalf("donor send refusal %v, want %v", err, refusal)
			}
			tr.mu.Lock()
			free := tr.workspace == nil && tr.high == 1 && tr.cells[1] != nil
			tr.mu.Unlock()
			if !free {
				t.Fatal("refused donor frame kept the reclaim workspace")
			}
			// The next new cell takes native 2; the donor stays reclaimable.
			admitHello(t, f, tr, 2, 3, nil, nil)
			admitHello(t, f, tr, 3, 4, native, guest)
			creditConsistent(t, tr)
			tr.Close()
			if state, _ := inspectCells(t, root); state.Workspace || state.High != 3 || state.Refused != nil {
				t.Fatalf("protected index %+v", state)
			}
		})
	}
}

// TestRefusedDonorSendTransientKeepsWorkspace: WouldBlock is transient, so a
// donor frame refused WouldBlock keeps the workspace for its replay.
func TestRefusedDonorSendTransientKeepsWorkspace(t *testing.T) {
	f, tr, _ := mappedFixture(t)
	native, guest := admitHello(t, f, tr, 1, 1, nil, nil)
	// A full dispatch queue refuses the complete donor send WouldBlock. The
	// workers keep draining the original queue, which is put back afterwards.
	tr.mu.Lock()
	saved, full := tr.ordinaryQueue, make(chan job, 1)
	full <- job{}
	tr.ordinaryQueue = full
	tr.mu.Unlock()
	donor := mappedHello(t, f, 2, 2)
	donor.Reclaim, donor.Delegation.GuestReclaim = native, guest
	err := mappingWrite(t, tr, f.native, donor, 2, 0, -1)
	tr.mu.Lock()
	kept := tr.workspace != nil
	tr.ordinaryQueue = saved
	tr.mu.Unlock()
	if !errors.Is(err, fsapi.WouldBlock) {
		t.Fatal("donor send not refused WouldBlock", err)
	}
	if !kept {
		t.Fatal("transient refusal cleared the workspace")
	}
	if err := mappingWrite(t, tr, f.native, donor, 2, 0, -1); err != nil {
		t.Fatal("donor replay after WouldBlock", err)
	}
}

// TestRefusedNumberedCellsReleaseCredit: k numbered cells refused for good
// (p64: Stale from the guest freshness check, Invalid after the first chunk)
// return their credit and leave the incomplete-cell limits, so later cells
// keep flowing. Each refusal is replayed identically and gets the same errno,
// while an identical resend of an admitted cell still succeeds.
func TestRefusedNumberedCellsReleaseCredit(t *testing.T) {
	f, tr, root := mappedFixture(t)
	admitHello(t, f, tr, 1, 1, nil, nil)
	native := uint64(2)
	const k = 70 // above the 12 incomplete ordinary cells and the 64 credits
	for i := 0; i < k; i++ {
		duplicate := mappedHello(t, f, native, 1)
		if err := mappingWrite(t, tr, f.native, duplicate, 0, 0, -1); !errors.Is(err, fsapi.Stale) {
			t.Fatalf("duplicate %d refusal %v", i, err)
		}
		if err := mappingWrite(t, tr, f.native, duplicate, 0, 0, -1); !errors.Is(err, fsapi.Stale) {
			t.Fatalf("replay of refused duplicate %d got %v", i, err)
		}
		native++
	}
	long := mappingLongCall(t, f, native, 2)
	if err := badHashWrite(t, tr, f.native, long, 0); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("multi-chunk content refusal", err)
	}
	if err := badHashWrite(t, tr, f.native, long, 0); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("replay of the refused multi-chunk cell got", err)
	}
	if err := mappingWrite(t, tr, f.native, long, 0, 0, -1); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("another frame on the refused number got", err)
	}
	native++
	creditConsistent(t, tr)
	fresh := mappedHello(t, f, native, 3)
	if err := mappingWrite(t, tr, f.native, fresh, 0, 0, -1); err != nil {
		t.Fatal("fresh cell after refused cells", err)
	}
	_ = fetchNative(t, tr, f.native, native)
	if err := mappingWrite(t, tr, f.native, fresh, 0, 0, -1); err != nil {
		t.Fatal("identical resend of an admitted cell", err)
	}
	creditConsistent(t, tr)
	tr.Close()
	state, cells := inspectCells(t, root)
	if len(cells) != 2 || state.Ordinary != 2 || state.High != native || state.Refused == nil || state.Refused.Header.Cell != native-1 || state.Refused.Error != fsapi.Invalid {
		t.Fatalf("protected index cells=%v ordinary=%d high=%d refused=%+v", state.Cells, state.Ordinary, state.High, state.Refused)
	}
}

// TestRefusedNumberedCellKeptWhenExporterPins: the runtime keeps a refused
// cell whose refusal the exporter does not fail back (p64): a single-chunk
// Invalid, which it cannot tell from a refusal before allocation, and
// Permission. The exporter pins and replays those, and the replay
// revalidates the kept cell.
func TestRefusedNumberedCellKeptWhenExporterPins(t *testing.T) {
	f, tr, _ := mappedFixture(t)
	single := mappedHello(t, f, 1, 1)
	if err := badHashWrite(t, tr, f.native, single, 0); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("single-chunk content refusal", err)
	}
	tr.mu.Lock()
	kept := tr.cells[1] != nil && tr.ordinary == 1 && tr.refused == nil
	tr.mu.Unlock()
	if !kept {
		t.Fatal("single-chunk Invalid cell removed")
	}
	if err := badHashWrite(t, tr, f.native, single, 0); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("replay of a kept refused cell got", err)
	}
	control := mappedHello(t, f, 2, 2)
	if err := mappingWrite(t, tr, f.native, control, 1, 0, -1); !errors.Is(err, fsapi.Permission) {
		t.Fatal("credit-class refusal", err)
	}
	tr.mu.Lock()
	kept = tr.cells[2] != nil && tr.control == 1
	tr.mu.Unlock()
	if !kept {
		t.Fatal("Permission-refused cell removed")
	}
}

// TestRefusedCellsDoNotCloseMarkerAdmission: a refused minor-4 cell with a
// low guest watermark no longer keeps every later issuance marker of its
// session; fresh minor-4 admission stays open after many admitted cells.
func TestRefusedCellsDoNotCloseMarkerAdmission(t *testing.T) {
	f, tr, _ := mappedFixture(t)
	nativeAck, guestAck := admitHello(t, f, tr, 1, 1, nil, nil)
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 2, 1), 0, 0, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("duplicate guest cell refusal", err)
	}
	native := uint64(3)
	for guest := uint64(2); guest <= mappingRecentLimit+10; guest++ {
		nativeAck, guestAck = admitHello(t, f, tr, native, guest, nativeAck, guestAck)
		native++
	}
	tr.mu.Lock()
	markers := len(tr.guestRecent)
	tr.mu.Unlock()
	if markers > 2 {
		t.Fatalf("%d issuance markers retained", markers)
	}
}

// TestRefusedMappedCellSuspends: a refused minor-4 duplicate leaves no
// incomplete row, so Suspend, which takes only responded cells, succeeds and
// its protected cut carries the refusal.
func TestRefusedMappedCellSuspends(t *testing.T) {
	f, tr, _ := mappedFixture(t)
	admitHello(t, f, tr, 1, 1, nil, nil)
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 2, 1), 0, 0, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("duplicate guest cell refusal", err)
	}
	funding, err := tr.Suspend()
	if err != nil {
		t.Fatal("suspend after a refused cell", err)
	}
	state := funding.State()
	if state.High != 2 || len(state.Cells) != 1 || state.Ordinary != 1 || state.Refused == nil || state.Refused.Error != fsapi.Stale {
		t.Fatalf("suspended cut %+v", state)
	}
}

// TestRefusedCellRestart: after a permanent refusal (a multi-chunk cell whose
// content is refused after its first chunk) the ledger holds no incomplete
// cell, so the transport is suspended and restored from it. The restored
// transport has the cleaned state, answers a resend of the refused cell with
// the same errno, and admits the next cell. It uses the unmapped bridge with
// the trusted restore primitives of restore_window_test.go, since a mapped
// restore needs the controlled providers (internal/controlledruntime).
func TestRefusedCellRestart(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	f.s.config.Native = &windowRestoreNative{localNative: localNative{expected: f.native}, hook: func() error { return nil }}
	f.s.config.Claims = windowRestoreClaims{f.claims}
	f.s.config.Validation = &fsservice.ValidationProfile{Disposable: true, Name: "n64 refused-cell restore primitive"}
	config := f.s.config
	root := filepath.Join(t.TempDir(), "transport")
	tr, err := NewDurableTransport(f.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	hello := map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0}
	sendNative(t, tr, f.native, f.command(t, "HELLO", hello), 0)
	_ = fetchNative(t, tr, f.native, 1)
	body := bytes.Repeat([]byte("q"), 2*ChunkBytes)
	refuse := func(tr *Transport) error {
		return writeBody(t, tr, f.native, parseID(f.c.Session), 2, 0, 0, body, sha256.Sum256([]byte("other")))
	}
	if err = refuse(tr); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("multi-chunk content refusal", err)
	}
	funding, err := tr.Suspend()
	if err != nil {
		t.Fatal("suspend after a refused cell", err)
	}
	_, next, err := RestoreDurableEndpoint(t.Context(), config, root, funding)
	if err != nil {
		t.Fatal("restore after a refused cell", err)
	}
	t.Cleanup(next.Close)
	next.mu.Lock()
	restored := next.high == 2 && len(next.cells) == 1 && next.ordinary == 1 && next.workspace == nil && next.refused != nil && next.refused.Header.Cell == 2 && next.refused.Error == fsapi.Invalid
	next.mu.Unlock()
	if !restored {
		t.Fatal("restored transport differs from the cleaned state")
	}
	if err = refuse(next); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("resend of the refused cell after restore got", err)
	}
	f.next = 2
	sendNative(t, next, f.native, f.command(t, "HELLO", hello), 0)
	_ = fetchNative(t, next, f.native, 3)
	creditConsistent(t, next)
}

// TestRefusalIndexValidation refuses a protected refusal record that names a
// live or unspent cell, a donor frame or an errno that is never removed.
func TestRefusalIndexValidation(t *testing.T) {
	header := Frame{Kind: 1, Total: 10, Cell: 3}
	base := func(mutate func(*ProtectedTransportState)) ProtectedTransportState {
		s := ProtectedTransportState{High: 4, Cells: []uint64{4}, Refused: &ProtectedRefusal{Header: header, Error: fsapi.Stale}}
		if mutate != nil {
			mutate(&s)
		}
		return s
	}
	if err := validateRefusal(base(nil)); err != nil {
		t.Fatal("valid refusal refused", err)
	}
	cases := map[string]func(*ProtectedTransportState){
		"live":    func(s *ProtectedTransportState) { s.Cells = []uint64{3, 4} },
		"unspent": func(s *ProtectedTransportState) { s.High = 2 },
		"donor":   func(s *ProtectedTransportState) { s.Refused.Header.Flags = 2 },
		"errno":   func(s *ProtectedTransportState) { s.Refused.Error = fsapi.Permission },
		"data":    func(s *ProtectedTransportState) { s.Refused.Header.Data = []byte("x") },
	}
	for name, mutate := range cases {
		if validateRefusal(base(mutate)) == nil {
			t.Errorf("%s refusal admitted", name)
		}
	}
}
