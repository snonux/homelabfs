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

// Tests for the replacement of abandoned, never-validated cells (task x64,
// replaceAbandoned in refusal.go). The exporter fails a first attempt back on
// an authentication ESTALE or credential EACCES of the final chunk (w64),
// which the runtime refuses before it stores or validates that chunk. The
// exporter's single writer then moves on to a different frame.

// partialLong writes every chunk of the multi-chunk mapped CALL c except the
// final one, as when the exporter's final chunk is refused before validation.
func partialLong(t *testing.T, tr *Transport, n Native, c Command, flags uint16) error {
	t.Helper()
	raw := mustJSON(t, c)
	final := (len(raw) - 1) / ChunkBytes * ChunkBytes
	if final == 0 {
		t.Fatal("partialLong needs a multi-chunk cell")
	}
	return mappingWrite(t, tr, n, c, flags, 0, final)
}

// TestAbandonedDonorWorkspaceReplaced: a donor send whose final chunk was
// refused before validation leaves a half-filled reclaim workspace. After
// the exporter's fail-back the next cell, numbered or another donor send,
// arrives at the same native number and is admitted instead of getting
// WouldBlock for good; the donor target stays reclaimable.
func TestAbandonedDonorWorkspaceReplaced(t *testing.T) {
	for _, next := range []string{"numbered", "donor"} {
		t.Run(next, func(t *testing.T) {
			f, tr, root := mappedFixture(t)
			native, guest := admitHello(t, f, tr, 1, 1, nil, nil)
			donor := mappingLongCall(t, f, 2, 2)
			donor.Reclaim, donor.Delegation.GuestReclaim = native, guest
			if err := partialLong(t, tr, f.native, donor, 2); err != nil {
				t.Fatal("partial donor send", err)
			}
			tr.mu.Lock()
			partial := tr.workspace != nil && tr.high == 1
			tr.mu.Unlock()
			if !partial {
				t.Fatal("partial donor send did not fill the workspace")
			}
			// Each admitted donor send takes its target's credit, so one
			// ordinary credit stays per admitted fresh cell.
			high, ordinary := uint64(2), 1
			if next == "numbered" {
				admitHello(t, f, tr, 2, 3, nil, nil)
				admitHello(t, f, tr, 3, 4, native, guest)
				high, ordinary = 3, 2
			} else {
				admitHello(t, f, tr, 2, 3, native, guest)
			}
			creditConsistent(t, tr)
			tr.Close()
			if state, _ := inspectCells(t, root); state.Workspace || state.High != high || state.Ordinary != ordinary || state.Refused != nil {
				t.Fatalf("protected index %+v", state)
			}
		})
	}
}

// TestAbandonedNumberedCellsReleaseCredit: a numbered cell whose final chunk
// was refused before validation keeps its number spent (p64 fail-back), and
// the next cell at high+1 removes it with its credit. Thirteen abandoned
// cells in a row would otherwise fill the session's 12 incomplete ordinary
// cells, so the next allocation got WouldBlock.
func TestAbandonedNumberedCellsReleaseCredit(t *testing.T) {
	f, tr, root := mappedFixture(t)
	admitHello(t, f, tr, 1, 1, nil, nil)
	native := uint64(2)
	for i := 0; i < 13; i++ {
		if err := partialLong(t, tr, f.native, mappingLongCall(t, f, native, native), 0); err != nil {
			t.Fatalf("abandoned cell %d: %v", i, err)
		}
		native++
	}
	admitHello(t, f, tr, native, native, nil, nil)
	tr.mu.Lock()
	clean := len(tr.cells) == 2 && tr.ordinary == 2 && tr.incomplete(f.c.Session) == 0 && tr.refused == nil
	tr.mu.Unlock()
	if !clean {
		t.Fatal("abandoned cells kept their credit or count")
	}
	// The exporter never writes an abandoned number again; it reads as spent.
	if err := partialLong(t, tr, f.native, mappingLongCall(t, f, native-1, native-1), 0); !errors.Is(err, fsapi.Stale) {
		t.Fatal("write to an abandoned number got", err)
	}
	creditConsistent(t, tr)
	tr.Close()
	state, cells := inspectCells(t, root)
	if len(cells) != 2 || state.Ordinary != 2 || state.High != native || state.Refused != nil {
		t.Fatalf("protected index cells=%v ordinary=%d high=%d refused=%+v", state.Cells, state.Ordinary, state.High, state.Refused)
	}
}

// TestPartialCellIdenticalReplayAccepted: an identical replay of a partial
// cell or workspace (the exporter pins a middle-chunk refusal and resends
// from the first chunk) is still accepted into what the runtime kept.
func TestPartialCellIdenticalReplayAccepted(t *testing.T) {
	for _, flags := range []uint16{0, 2} {
		t.Run(map[uint16]string{0: "numbered", 2: "donor"}[flags], func(t *testing.T) {
			f, tr, _ := mappedFixture(t)
			native, guest := admitHello(t, f, tr, 1, 1, nil, nil)
			long := mappingLongCall(t, f, 2, 2)
			if flags == 2 {
				long.Reclaim, long.Delegation.GuestReclaim = native, guest
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := partialLong(t, tr, f.native, long, flags); err != nil {
					t.Fatalf("attempt %d of a partial cell: %v", attempt, err)
				}
			}
			tr.mu.Lock()
			c := tr.cells[2]
			if flags == 2 {
				c = tr.workspace
			}
			kept := c != nil && c.count == len(c.chunks)-1
			tr.mu.Unlock()
			if !kept {
				t.Fatal("identical replay replaced the partial cell")
			}
		})
	}
}

// TestValidatedCellNotReplaced: a complete cell was validated, so a different
// frame never replaces it. A donor send refused WouldBlock keeps the
// workspace and the next frame at its number still waits; a numbered cell
// refused Permission (kept, the exporter pins it) survives a later number and
// a different frame at its own number is Invalid.
func TestValidatedCellNotReplaced(t *testing.T) {
	t.Run("donor", func(t *testing.T) {
		f, tr, _ := mappedFixture(t)
		native, guest := admitHello(t, f, tr, 1, 1, nil, nil)
		tr.mu.Lock()
		saved, full := tr.ordinaryQueue, make(chan job, 1)
		full <- job{}
		tr.ordinaryQueue = full
		tr.mu.Unlock()
		donor := mappedHello(t, f, 2, 2)
		donor.Reclaim, donor.Delegation.GuestReclaim = native, guest
		err := mappingWrite(t, tr, f.native, donor, 2, 0, -1)
		tr.mu.Lock()
		tr.ordinaryQueue = saved
		tr.mu.Unlock()
		if !errors.Is(err, fsapi.WouldBlock) {
			t.Fatal("donor send not refused WouldBlock", err)
		}
		if err = mappingWrite(t, tr, f.native, mappedHello(t, f, 2, 3), 0, 0, -1); !errors.Is(err, fsapi.WouldBlock) {
			t.Fatal("different frame over a validated workspace got", err)
		}
		if err = mappingWrite(t, tr, f.native, donor, 2, 0, -1); err != nil {
			t.Fatal("donor replay after WouldBlock", err)
		}
	})
	t.Run("numbered", func(t *testing.T) {
		f, tr, _ := mappedFixture(t)
		if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 1, 1), 1, 0, -1); !errors.Is(err, fsapi.Permission) {
			t.Fatal("credit-class refusal", err)
		}
		admitHello(t, f, tr, 2, 2, nil, nil)
		if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 1, 3), 0, 0, -1); !errors.Is(err, fsapi.Invalid) {
			t.Fatal("different frame over a validated cell got", err)
		}
		tr.mu.Lock()
		kept := tr.cells[1] != nil && tr.control == 1 && tr.ordinary == 1
		tr.mu.Unlock()
		if !kept {
			t.Fatal("validated refused cell replaced")
		}
	})
}

// TestAbandonedCellRestore: after a replacement the ledger holds no
// incomplete row, so Suspend and restore work and the restored transport
// has the cleaned state: the abandoned number is spent, its credit is free,
// and the next cell is admitted. Before x64 Suspend refused the stuck row.
// It uses the unmapped restore primitives of TestRefusedCellRestart.
func TestAbandonedCellRestore(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	f.s.config.Native = &windowRestoreNative{localNative: localNative{expected: f.native}, hook: func() error { return nil }}
	f.s.config.Claims = windowRestoreClaims{f.claims}
	f.s.config.Validation = &fsservice.ValidationProfile{Disposable: true, Name: "x64 abandoned-cell restore primitive"}
	config := f.s.config
	root := filepath.Join(t.TempDir(), "transport")
	tr, err := NewDurableTransport(f.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 1)
	_, wire := partialFrame(t, f, 2, 0)
	write := func(tr *Transport) error {
		_, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, 2*CellStride, wire)
		return err
	}
	if err = write(tr); err != nil {
		t.Fatal("first chunk of the abandoned cell", err)
	}
	f.next = 2
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 3)
	funding, err := tr.Suspend()
	if err != nil {
		t.Fatal("suspend after a replacement", err)
	}
	_, next, err := RestoreDurableEndpoint(t.Context(), config, root, funding)
	if err != nil {
		t.Fatal("restore after a replacement", err)
	}
	t.Cleanup(next.Close)
	next.mu.Lock()
	restored := next.high == 3 && len(next.cells) == 2 && next.cells[2] == nil && next.ordinary == 2 && next.workspace == nil && next.refused == nil
	next.mu.Unlock()
	if !restored {
		t.Fatal("restored transport differs from the replaced state")
	}
	if err = write(next); !errors.Is(err, fsapi.Stale) {
		t.Fatal("abandoned number after restore got", err)
	}
	sendNative(t, next, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, next, f.native, 4)
	creditConsistent(t, next)
}

// TestAbandonedReplacementProtectionFails: the replacement commits in one
// ledger transaction. If it fails, the transport is fenced, the write is
// refused and the ledger still holds the abandoned cell unchanged.
func TestAbandonedReplacementProtectionFails(t *testing.T) {
	f, tr, root := durableFixture(t)
	frame, wire := partialFrame(t, f, 1, 0)
	if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); err != nil {
		t.Fatal(err)
	}
	tr.ledger.failpoint = func(name string) error {
		if name == "before-commit" {
			return errors.New("replacement protection uncertainty")
		}
		return nil
	}
	f.next = 1
	command := helloCommand(t, f)
	raw := mustJSON(t, command)
	next, _ := EncodeFrame(Frame{Kind: 1, Total: uint32(len(raw)), Session: parseID(f.c.Session), Cell: 2, Hash: sha256.Sum256(raw), Data: raw})
	if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, 2*CellStride, next); err == nil {
		t.Fatal("write after a failed replacement accepted")
	}
	tr.mu.Lock()
	fenced := tr.failed != nil
	tr.mu.Unlock()
	if !fenced {
		t.Fatal("failed replacement did not fence the transport")
	}
	tr.Close()
	state, cells := inspectCells(t, root)
	if state.High != 1 || state.Ordinary != 1 || len(cells) != 1 || cells[0].Count != 1 || !bytes.Equal(cells[0].Body[:ChunkBytes], frame.Data) {
		t.Fatalf("ledger changed by a failed replacement: %+v", state)
	}
}
