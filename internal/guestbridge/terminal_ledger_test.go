package guestbridge

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// These tests isolate real C8 framing, mappings and bbolt transactions. The
// verification component deliberately cannot issue terminal authority; owner
// composition and actual signed success are tested by controlledruntime.
func terminalLedgerFixture(t *testing.T) (*bridgeFixture, *Transport, string, Command) {
	t.Helper()
	f := newBridge(t)
	f.hello(t)
	protocol := TerminalProtocol()
	f.s.config.Protocol = &protocol
	f.s.config.Verification = &mappingProducerPrimitive{}
	f.s.config.ViewProtocol = &recordingViewProvider{scope: f.c}
	root := filepath.Join(t.TempDir(), "terminal-ledger")
	tr, err := NewDurableTransport(f.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	guest := make([]Ack, 3)
	native := make([]Ack, 3)
	for i := range guest {
		id := uint64(i + 1)
		op := "HELLO"
		payload := mustJSON(t, protocol)
		flags := uint16(0)
		if i == 2 {
			op, flags = "RESUME", 1
			payload = mustJSON(t, map[string]any{"previous_session": f.c.Session,
				"ledger_hash": digest([]byte("ledger")), "claims_hash": digest([]byte("claims")), "domains_hash": digest([]byte("domains"))})
		}
		command := Command{Cell: id, Context: f.c, Protocol: &protocol, Op: op, Payload: payload,
			Delegation: &Delegation{Version: DelegationVersion, GuestCell: id, GuestOp: op}}
		if err := mappingWrite(t, tr, f.native, command, flags, 0, -1); err != nil {
			t.Fatal(err)
		}
		response := fetchNative(t, tr, f.native, id)
		hash, err := translatedResponseHash(response, id, op, false)
		if err != nil {
			t.Fatal(err)
		}
		guest[i] = Ack{Cell: id, ResponseHash: hash}
		native[i] = Ack{Cell: id, ResponseHash: digest(response)}
	}
	request := TerminalRequest{RetirementID: uid(222).String(), Credentials: f.cred(), GuestThrough: 3,
		ObserverRevision: 2, ClosureHash: digest([]byte("inert observer commitment")), SettledCells: guest[1:]}
	command := Command{Cell: 4, Context: f.c, Protocol: &protocol, Op: "SESSION_RETIRE", Payload: mustJSON(t, request), Reclaim: &native[0],
		Delegation: &Delegation{Version: DelegationVersion, GuestCell: 4, GuestOp: "SESSION_RETIRE", GuestReclaim: &guest[0]}}
	return f, tr, root, command
}

func TestTerminalLedgerAtomicMultipleDeletionFailures(t *testing.T) {
	for _, point := range []string{"before-commit", "after-commit"} {
		t.Run(point, func(t *testing.T) {
			f, tr, root, command := terminalLedgerFixture(t)
			sentinel := errors.New("terminal commit outcome unavailable")
			fired := false
			tr.mu.Lock()
			tr.ledger.failpoint = func(name string) error {
				if name == point && !fired {
					fired = true
					return sentinel
				}
				return nil
			}
			tr.mu.Unlock()
			if err := mappingWrite(t, tr, f.native, command, 2, 0, -1); !errors.Is(err, sentinel) {
				t.Fatalf("exact terminal protection failure not returned: %v", err)
			}
			tr.mu.Lock()
			failed, didFire := tr.failed, fired
			retained := append([]*cell(nil), tr.uncertainReclaims...)
			tr.mu.Unlock()
			if !didFire || failed == nil || len(retained) != 3 {
				t.Fatal("uncertain multi-delete abandoned old live owners")
			}
			for _, old := range retained {
				if old.released || len(old.body) == 0 || len(old.response) == 0 || old.mapping == nil || digest(old.response) != old.mapping.NativeFullHash {
					t.Fatal("uncertain commit released or rewrote an old owner")
				}
			}
			if err := mappingWrite(t, tr, f.native, command, 2, 0, -1); !errors.Is(err, fsapi.Unavailable) {
				t.Fatal("uncertain endpoint admitted replay without continuity", err)
			}
			if raw, err := tr.Read(t.Context(), f.native, f.native.Endpoint.ResultNode, 4*CellStride, 32768); len(raw) != 0 || !errors.Is(err, fsapi.Unavailable) {
				t.Fatal("uncertain protection exposed terminal bytes", err)
			}
			tr.Close()
			state, cells := inspectCells(t, root)
			if point == "before-commit" {
				if state.High != 3 || len(cells) != 3 || state.Ordinary != 2 || state.Control != 1 || len(state.Terminal) != 0 || state.Workspace {
					t.Fatalf("precommit failure changed protected old cut: %+v", state)
				}
				return
			}
			if state.High != 4 || len(cells) != 1 || state.Ordinary != 1 || state.Control != 0 || state.Terminal[f.c.Session] != 4 || state.Workspace {
				t.Fatalf("postcommit ledger is not the complete terminal cut: %+v", state)
			}
			cell := cells[0]
			if cell.Terminal == nil || cell.Terminal.Phase != "TERMINAL_PENDING" || cell.Response != nil || cell.Terminal.Ownership != nil || len(cell.Terminal.Donors) != 2 || cell.Terminal.OldOrdinary != 2 || cell.Terminal.OldControl != 1 {
				t.Fatal("postcommit record lost ownership or invented terminal success")
			}
		})
	}
}
