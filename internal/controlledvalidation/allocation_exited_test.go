package controlledvalidation

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Task 194 (found while fixing lab run zq3-r10's R10-1): the guest's final
// release of a description can arrive after its issuer's PROCESS_EXIT already
// completed (an fd inherited by a child that outlives its parent, or the exit
// race where PROCESS_EXIT's CALL runs before the release allocates). CALL has
// always tolerated an exited issuer for exactly the originalCleanup opcodes;
// ALLOCATE refused them, so the guest blocked its session. These tests run the
// actual bridge and provider through ALLOCATE + CALL.

// exitedCall derives the next ordinary claim of the harness principal for one
// intent, keeping the original (exited) credentials unless the caller edits them.
func (h *allocationHarness) exitedCall(number uint64, opcode string, args any) guestbridge.Call {
	h.f.t.Helper()
	call := h.original
	call.Claim.Number = number
	call.Intent = guestbridge.Intent{Opcode: opcode, Args: mustAllocationJSON(h.f.t, args)}
	raw, err := canonicalJSON(call.Intent)
	mustAllocation(h.f.t, err)
	call.Claim.OriginalHash = hash(raw)
	return call
}

// allocateAndCall runs ALLOCATE then CALL and returns the protected result.
func (h *allocationHarness) allocateAndCall(call guestbridge.Call) wireResult {
	h.f.t.Helper()
	if _, err := h.send("ALLOCATE", call); err != nil {
		h.f.t.Fatalf("ALLOCATE %s: %v", call.Intent.Opcode, err)
	}
	response, err := h.send("CALL", call)
	mustAllocation(h.f.t, err)
	if status := allocationPayload(h.f.t, response)["status"]; status != "ADMITTED" {
		h.f.t.Fatalf("CALL %s: %s", call.Intent.Opcode, response)
	}
	stored, _ := h.stored(call)
	var result wireResult
	if stored.Status != "TERMINAL" || json.Unmarshal(stored.Result, &result) != nil {
		h.f.t.Fatalf("CALL %s did not complete: %+v", call.Intent.Opcode, stored)
	}
	return result
}

// exitedFixture creates one file (PATH + DESCRIPTION grants) with claims 1-2
// and completes the issuer's PROCESS_EXIT as claim 3.
func exitedFixture(t *testing.T) (*allocationHarness, guestbridge.Capability, guestbridge.Capability) {
	t.Helper()
	h := newAllocationHarness(t)
	root := h.allocateAndCall(h.original)
	if root.Code != "OK" || len(root.Capabilities) == 0 || root.Capabilities[0].Kind != "ROOT" {
		t.Fatalf("ROOT: %+v", root)
	}
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	created := h.allocateAndCall(h.exitedCall(2, "CREATE", map[string]any{"parent": root.Capabilities[0], "name": "66", "mode": 0600, "umask": 0, "flags": flags}))
	if created.Code != "OK" || len(created.Capabilities) != 2 || created.Capabilities[1].Kind != "DESCRIPTION" {
		t.Fatalf("CREATE: %+v", created)
	}
	exit := h.allocateAndCall(h.exitedCall(3, "PROCESS_EXIT", map[string]any{"process": h.original.Credentials.Process, "exit_sequence": uint64(1)}))
	if exit.Code != "OK" {
		t.Fatalf("PROCESS_EXIT: %+v", exit)
	}
	return h, created.Capabilities[0], created.Capabilities[1]
}

func TestControlledExitedIssuerAllocatesOriginalCleanup(t *testing.T) {
	h, object, description := exitedFixture(t)
	if r := h.allocateAndCall(h.exitedCall(4, "FORGET", map[string]any{"object": object, "count": 1})); r.Code != "OK" {
		t.Fatalf("exited FORGET: %+v", r)
	}
	if r := h.allocateAndCall(h.exitedCall(5, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})); r.Code != "OK" {
		t.Fatalf("exited DESCRIPTION_RELEASE: %+v", r)
	}
	// The grants are now released: the same cleanup cannot allocate twice.
	if _, err := h.send("ALLOCATE", h.exitedCall(6, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 2})); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("released description allocated again: %v", err)
	}
	if _, err := h.send("ALLOCATE", h.exitedCall(6, "FORGET", map[string]any{"object": object, "count": 1})); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("forgotten object allocated again: %v", err)
	}
}

func TestControlledExitedIssuerCannotAllocateEffects(t *testing.T) {
	h, object, description := exitedFixture(t)
	flags := map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}
	refused := map[string]guestbridge.Call{
		"WRITE":             h.exitedCall(4, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61", "max_end": 1}),
		"CREATE":            h.exitedCall(4, "CREATE", map[string]any{"parent": object, "name": "67", "mode": 0600, "umask": 0, "flags": flags}),
		"OPEN":              h.exitedCall(4, "OPEN", map[string]any{"object": object, "flags": flags}),
		"READ":              h.exitedCall(4, "READ", map[string]any{"description": description, "offset": 0, "count": 1}),
		"DESCRIPTION_CLOSE": h.exitedCall(4, "DESCRIPTION_CLOSE", map[string]any{"description": description, "process": h.original.Credentials.Process, "close_sequence": 1}),
		// FORGET of more than the one lookup is not the originalCleanup shape.
		"FORGET count 2": h.exitedCall(4, "FORGET", map[string]any{"object": object, "count": 2}),
	}
	// A new attested revision of the exited process is never admitted either.
	revised := h.exitedCall(4, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	revised.Credentials.Revision++
	refused["DESCRIPTION_RELEASE new revision"] = revised
	forged := description
	forged.Token = hash([]byte("never-issued"))
	refused["DESCRIPTION_RELEASE forged grant"] = h.exitedCall(4, "DESCRIPTION_RELEASE", map[string]any{"description": forged, "release_sequence": 1})
	for name, call := range refused {
		if _, err := h.send("ALLOCATE", call); !errors.Is(err, fsapi.Stale) {
			t.Fatalf("exited issuer allocated %s: %v", name, err)
		}
		if _, exists := h.stored(call); exists {
			t.Fatalf("refused %s left a ledger entry", name)
		}
	}
	// Nothing above consumed claim 4: the exact cleanup still allocates it.
	if r := h.allocateAndCall(h.exitedCall(4, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})); r.Code != "OK" {
		t.Fatalf("exited DESCRIPTION_RELEASE after refusals: %+v", r)
	}
}

// A live issuer keeps the ordinary path: its DESCRIPTION_RELEASE and later
// mutations allocate regardless of the exited-issuer rule.
func TestControlledLiveIssuerAllocationUnchanged(t *testing.T) {
	h := newAllocationHarness(t)
	root := h.allocateAndCall(h.original)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	created := h.allocateAndCall(h.exitedCall(2, "CREATE", map[string]any{"parent": root.Capabilities[0], "name": "66", "mode": 0600, "umask": 0, "flags": flags}))
	description := created.Capabilities[1]
	if r := h.allocateAndCall(h.exitedCall(3, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61", "max_end": 1})); r.Code != "OK" {
		t.Fatalf("live WRITE: %+v", r)
	}
	if r := h.allocateAndCall(h.exitedCall(4, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})); r.Code != "OK" {
		t.Fatalf("live DESCRIPTION_RELEASE: %+v", r)
	}
}
