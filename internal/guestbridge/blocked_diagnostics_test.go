package guestbridge

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// A non-retryable Execute failure leaves its cell "blocked" without a reply.
// The protected row must name the error class so transport.db analysis can
// tell why a guest request was never answered (zq3-r4 R4-1).
func TestDurableTransportBlockedCellRecordsErrorClass(t *testing.T) {
	f, tr, ledgerRoot := durableFixture(t)
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 1)
	// A VIEW_POLL naming a DOMAIN capability this session was never granted,
	// the shape of the zq3-r4 defect: the transport accepts the complete command
	// bytes, then dispatch refuses it with Permission and never answers.
	_, root0 := f.call(t, f.cred(), "ROOT", map[string]any{})
	var domain Capability
	raw, _ := json.Marshal(root0["capabilities"].([]any)[1])
	if err := json.Unmarshal(raw, &domain); err != nil {
		t.Fatal(err)
	}
	domain.Token = digest([]byte("never granted"))
	query := f.command(t, "VIEW_POLL", map[string]any{"domain": domain, "after_sequence": 0})
	query.Cell = 2                        // ROOT ran directly on the bridge; native cells stay contiguous.
	sendNative(t, tr, f.native, query, 1) // VIEW_POLL travels the control lane
	waitCellPhase(t, tr, query.Cell, "blocked")
	tr.Close()
	_, cells := inspectCells(t, ledgerRoot)
	cells = cells[len(cells)-1:]
	if cells[0].JobPhase != "blocked" || cells[0].BlockedError != "PERMISSION" || cells[0].Response != nil {
		t.Fatalf("blocked diagnostics %+v", cells)
	}
	legacy := cells[0]
	legacy.BlockedError = ""
	if err := validateProtectedCell(legacy, false); err != nil {
		t.Fatalf("legacy blocked row without class refused: %v", err)
	}
	wrongPhase := cells[0]
	wrongPhase.JobPhase = "pending"
	if err := validateProtectedCell(wrongPhase, false); err == nil {
		t.Fatal("error class admitted outside blocked phase")
	}
}

func TestBlockedErrorClassIsBoundedAndContentFree(t *testing.T) {
	if got := blockedErrorClass(fmt.Errorf("wrapped: %w", fsapi.Permission)); got != "PERMISSION" {
		t.Fatal(got)
	}
	var syntax any
	err := json.Unmarshal([]byte("{secret"), &syntax)
	if got := blockedErrorClass(err); got != "UNCLASSIFIED:*json.SyntaxError" {
		t.Fatal(got)
	}
	type veryLongErrorTypeNameThatWouldOtherwiseExceedTheProtectedDiagnosticBoundOfTheRow struct{ error }
	long := veryLongErrorTypeNameThatWouldOtherwiseExceedTheProtectedDiagnosticBoundOfTheRow{fmt.Errorf("x")}
	if got := blockedErrorClass(long); len(got) != blockedErrorLimit {
		t.Fatal(got)
	}
}

func waitCellPhase(t *testing.T, tr *Transport, id uint64, phase string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		tr.mu.Lock()
		c := tr.cells[id]
		current := ""
		if c != nil {
			current = c.jobPhase
		}
		tr.mu.Unlock()
		if current == phase {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cell %d phase %q, want %q", id, current, phase)
		}
		time.Sleep(time.Millisecond)
	}
}
