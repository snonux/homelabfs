package controlledvalidation

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestLedgerEvidenceListsOpenClaims drives real ROOT and PROCESS_EXIT CALLs
// to TERMINAL, leaves one number ALLOCATED and one GETATTR RESERVED (the
// zq3-r8 shape of a stranded probe claim), and checks the bounded claim
// summary OBSERVE carries (b84).
func TestLedgerEvidenceListsOpenClaims(t *testing.T) {
	h := newAllocationHarness(t)
	for _, op := range []string{"ALLOCATE", "CALL"} {
		_, err := h.send(op, h.original)
		mustAllocation(t, err)
	}
	unsent := h.original
	unsent.Claim.Number = 2
	_, err := h.send("ALLOCATE", unsent)
	mustAllocation(t, err)
	exit := exitCall(t, h, 3)
	for _, op := range []string{"ALLOCATE", "CALL"} {
		_, err = h.send(op, exit)
		mustAllocation(t, err)
	}
	evidence, err := h.f.provider.LedgerEvidence(t.Context())
	mustAllocation(t, err)
	c := evidence.Claims
	if c.Principals != 1 || c.Allocated != 3 || c.Retired != 0 || c.Entries != 3 || c.Open != 1 {
		t.Fatalf("counts %+v", c)
	}
	if len(c.Pending) != 1 || c.Pending[0].Number != 2 || c.Pending[0].Status != "ALLOCATED" {
		t.Fatalf("pending %+v", c.Pending)
	}
	want := []ClaimSummary{{3, "PROCESS_EXIT", "TERMINAL", c.Recent[0].Stage}, c.Pending[0], {1, "ROOT", "TERMINAL", c.Recent[2].Stage}}
	if len(c.Recent) != 3 || c.Recent[0] != want[0] || c.Recent[1] != want[1] || c.Recent[2] != want[2] {
		t.Fatalf("recent %+v", c.Recent)
	}
}

// exitCall is the harness's original call turned into the PROCESS_EXIT of
// its process with public number n.
func exitCall(t *testing.T, h *allocationHarness, n uint64) guestbridge.Call {
	t.Helper()
	exit := h.original
	exit.Claim.Number = n
	exit.Intent = guestbridge.Intent{Opcode: "PROCESS_EXIT", Args: mustAllocationJSON(t, map[string]any{"process": exit.Credentials.Process, "exit_sequence": uint64(1)})}
	raw, err := canonicalJSON(exit.Intent)
	mustAllocation(t, err)
	exit.Claim.OriginalHash = hash(raw)
	return exit
}

// TestLedgerEvidenceReservedClaimAndBounds checks a RESERVED/UNPREPARED
// claim is open with its opcode, and that Pending and Recent stay bounded
// while Open and Entries keep the full counts.
func TestLedgerEvidenceReservedClaimAndBounds(t *testing.T) {
	f := newFixture(t)
	total := claimEvidencePending + 4
	for i := 0; i < total; i++ {
		f.binding("GETATTR", map[string]any{})
	}
	evidence, err := f.provider.LedgerEvidence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := evidence.Claims
	if c.Open != total || c.Entries != total || c.Allocated != uint64(total) {
		t.Fatalf("counts %+v", c)
	}
	if len(c.Pending) != claimEvidencePending || len(c.Recent) != claimEvidenceRecent {
		t.Fatalf("bounds pending %d recent %d", len(c.Pending), len(c.Recent))
	}
	first, newest := c.Pending[0], c.Recent[0]
	if first != (ClaimSummary{1, "GETATTR", "RESERVED", "UNPREPARED"}) || newest.Number != uint64(total) {
		t.Fatalf("first %+v newest %+v", first, newest)
	}
	// The JSON shape is what tools/lab/retained9p/bootstrap.py reads.
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Claims struct {
			Open    int
			Pending []struct {
				Number                uint64
				Opcode, Status, Stage string
			}
		}
	}
	if err = json.Unmarshal(raw, &shape); err != nil || shape.Claims.Open != total || shape.Claims.Pending[0].Stage != "UNPREPARED" {
		t.Fatalf("json %s %v", raw, err)
	}
}

// TestLedgerEvidenceEmptyLedgerEncodesEmptyLists: a ledger without claims
// reports zero counts and empty (not null) lists, which bootstrap.py
// requires to tell "nothing open" from a runtime without the summary.
func TestLedgerEvidenceEmptyLedgerEncodesEmptyLists(t *testing.T) {
	f := newFixture(t)
	evidence, err := f.provider.LedgerEvidence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(evidence.Claims)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Principals":0,"Allocated":0,"Retired":0,"Entries":0,"Open":0,"Pending":[],"Recent":[]}`
	if string(raw) != want {
		t.Fatalf("got %s", raw)
	}
}
