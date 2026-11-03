package controlledvalidation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

func cleanupCall(h *allocationHarness, op string, args any) guestbridge.Call {
	h.f.t.Helper()
	call := h.original
	h.f.next++
	call.Claim.Number = h.f.next
	raw, err := canonicalJSON(args)
	mustAllocation(h.f.t, err)
	call.Intent = guestbridge.Intent{Opcode: op, Args: raw}
	raw, err = canonicalJSON(call.Intent)
	mustAllocation(h.f.t, err)
	call.Claim.OriginalHash = hash(raw)
	_, err = h.send("ALLOCATE", call)
	mustAllocation(h.f.t, err)
	_, err = h.send("CALL", call)
	mustAllocation(h.f.t, err)
	return call
}
func cleanupRoot(h *allocationHarness) guestbridge.Capability {
	h.f.t.Helper()
	call := cleanupCall(h, "ROOT", map[string]any{})
	e, _ := h.stored(call)
	var result wireResult
	mustAllocation(h.f.t, json.Unmarshal(e.Result, &result))
	requireCode(h.f.t, result, "OK")
	return result.Capabilities[0]
}
func seedCleanupDirectory(h *allocationHarness, count int) {
	h.f.t.Helper()
	// Disposable fixture setup uses real serialized planning/journal effects;
	// provider claim/source counters remain untouched before consumer operations.
	for i := 0; i < count; i++ {
		operation, err := newUUID()
		mustAllocation(h.f.t, err)
		allocation, err := newUUID()
		mustAllocation(h.f.t, err)
		c := createCommand(h.f, fmt.Sprintf("entry-%03d", i))
		c.Operation = fsapi.Mkdir
		c.Flags = fsapi.OpenFlags{}
		c.Mode = 0755
		err = h.f.service.Controlled(h.f.t.Context(), *h.f.config.Validation, func(ctx context.Context, cut *fsservice.ControlledCut) error {
			plan, err := cut.PlanControlled(ctx, fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}, c, operation, fsapi.ObjectID{Home: id(1), Allocation: allocation}, nil)
			if err != nil {
				return err
			}
			result, err := cut.ApplyControlled(ctx, plan, false)
			if err == nil && result.Code != fsapi.OK {
				return result.Code
			}
			return err
		})
		mustAllocation(h.f.t, err)
	}
}
func cleanupRequest(t *testing.T, e entry) guestbridge.ReaddirCleanup {
	t.Helper()
	if e.ReaddirClosure == nil {
		t.Fatalf("missing parent-funded closure: status=%s stage=%s", e.Status, e.PreparationStage)
	}
	var request guestbridge.ReaddirCleanup
	mustAllocation(t, json.Unmarshal(e.ReaddirClosure.Request, &request))
	return request
}
func assertCleanupCredits(t *testing.T, p *Provider, want uint64) {
	t.Helper()
	mustAllocation(t, p.db.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte(ledgerVersion))
		retained := binary.BigEndian.Uint64(root.Get([]byte("retained-credit")))
		source := binary.BigEndian.Uint64(root.Get([]byte("source-credit")))
		if retained != want*(4<<20) || source != want*(2<<20) {
			t.Fatalf("unfunded/inflated credits retained=%d source=%d claims=%d", retained, source, want)
		}
		return nil
	}))
}
func TestReaddirCleanupFullPageWithinRealDefaultCredits(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	seedCleanupDirectory(h, 256)
	root := cleanupRoot(h)
	call := cleanupCall(h, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 256})
	e, _ := h.stored(call)
	request := cleanupRequest(t, e)
	if len(request.Capabilities) != 256 {
		t.Fatalf("full page entries=%d", len(request.Capabilities))
	}
	original := bytes.Clone(e.Result)
	assertCleanupCredits(t, h.f.provider, 2)
	var err error

	// Exhaust all independent ordinary claim/source credits after the page is
	// committed. Parent-funded cleanup must still make progress without a claim.
	for h.f.next < 64 {
		h.f.next++
		next := h.original
		next.Claim.Number = h.f.next
		_, err = h.send("ALLOCATE", next)
		mustAllocation(t, err)
	}
	assertCleanupCredits(t, h.f.provider, 64)
	next := h.original
	next.Claim.Number = 65
	if _, err = h.send("ALLOCATE", next); err == nil {
		t.Fatal("genuine independent source/claim exhaustion admitted")
	}
	first, err := h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	second, err := h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	if !equalJSON(allocationPayload(t, first), allocationPayload(t, second)) {
		t.Fatal("lost cleanup reply changed receipt")
	}
	e, _ = h.stored(call)
	if !e.ReaddirClosure.Closed || !bytes.Equal(e.Result, original) {
		t.Fatal("closure lost original result")
	}

	// Exact lost-reply retry remains available even with no spare credits.
	_, err = h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	assertCleanupCredits(t, h.f.provider, 64)

}
func TestReaddirCleanupRejectsChangedOwnershipAndPreservesCurrentSnapshot(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	seedCleanupDirectory(h, 3)
	root := cleanupRoot(h)
	page := cleanupCall(h, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 256})
	e, _ := h.stored(page)
	request := cleanupRequest(t, e)
	for _, name := range []string{"token", "partial", "order", "result", "claim", "issuer"} {
		t.Run(name, func(t *testing.T) {
			altered := request
			altered.Capabilities = append([]guestbridge.Capability(nil), request.Capabilities...)
			switch name {
			case "token":
				altered.Capabilities[0].Token = hash([]byte("wrong token"))
			case "partial":
				altered.Capabilities = altered.Capabilities[:1]
			case "order":
				altered.Capabilities[0], altered.Capabilities[1] = altered.Capabilities[1], altered.Capabilities[0]
			case "result":
				altered.ResultID = hash([]byte("wrong result"))
			case "claim":
				altered.Claim.Number++
			case "issuer":
				altered.Credentials.Revision++
			}
			if _, err := h.send("READDIR_CLEANUP", altered); err == nil {
				t.Fatal("accepted changed original")
			}
		})
	}
	// Interleaved LOOKUP has an independent real lookup pin and a newer snapshot.
	lookup := cleanupCall(h, "LOOKUP", map[string]any{"parent": root, "name": hex.EncodeToString([]byte("entry-000"))})
	latest, _ := h.stored(lookup)
	var before map[string]json.RawMessage
	mustAllocation(t, json.Unmarshal(latest.BridgeState, &before))
	_, err := h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	after, _ := h.stored(lookup)
	var state struct {
		Grants map[string]struct {
			Released bool
			NoLookup bool
		}
	}
	mustAllocation(t, json.Unmarshal(after.BridgeState, &state))
	for _, cap := range request.Capabilities {
		if !state.Grants[cap.Token].Released {
			t.Fatal("page grant remained live")
		}
	}
	var result wireResult
	mustAllocation(t, json.Unmarshal(latest.Result, &result))
	if state.Grants[result.Capabilities[0].Token].Released {
		t.Fatal("cleanup consumed independent lookup")
	}
	var actual map[string]json.RawMessage
	mustAllocation(t, json.Unmarshal(after.BridgeState, &actual))
	for key, value := range before {
		if key != "Grants" && !bytes.Equal(value, actual[key]) {
			t.Fatalf("lost current snapshot field %s", key)
		}
	}
	if after.SnapshotVersion <= latest.SnapshotVersion {
		t.Fatal("cleanup failed to advance monotone snapshot sequence")
	}
}

// runCleanupConsumer sends actual production C request bytes into the real
// authenticated host Service/provider, then gives its exact response to C.
func runCleanupConsumer(t *testing.T, h *allocationHarness, call guestbridge.Call, e entry) {
	t.Helper()
	consumer := os.Getenv("HFS_READDIR_CLEANUP_CONSUMER")
	if consumer == "" {
		t.Fatal("production consumer binary required: HFS_READDIR_CLEANUP_CONSUMER")
	}
	cmd := exec.CommandContext(t.Context(), consumer)
	cmd.Env = append(os.Environ(), "HFS_CONSUMER_LOST_REPLY=1")
	in, err := cmd.StdinPipe()
	mustAllocation(t, err)
	out, err := cmd.StdoutPipe()
	mustAllocation(t, err)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	mustAllocation(t, cmd.Start())
	fixture, err := canonicalJSON(map[string]any{"claim": call.Claim, "credentials": call.Credentials, "result": e.Result})
	mustAllocation(t, err)
	_, err = in.Write(append(fixture, '\n'))
	mustAllocation(t, err)
	reader := bufio.NewReaderSize(out, guestbridge.BodyBytes+1)
	for i := 0; i < 2; i++ {
		raw, err := reader.ReadBytes('\n')
		mustAllocation(t, err)
		response, err := h.send("READDIR_CLEANUP", json.RawMessage(bytes.TrimSpace(raw)))
		mustAllocation(t, err)
		_, err = in.Write(append(response, '\n'))
		mustAllocation(t, err)
	}

	mustAllocation(t, in.Close())
	if err = cmd.Wait(); err != nil {
		t.Fatalf("production C consumer: %v: %s", err, diagnostics.String())
	}
}
func TestReaddirCleanupProductionConsumerRealHost(t *testing.T) {
	if os.Getenv("HFS_READDIR_CLEANUP_CONSUMER") == "" {
		t.Skip("explicit production C consumer build required; ordinary Go tests do not claim consumer acceptance")
	}
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	seedCleanupDirectory(h, 256)
	root := cleanupRoot(h)
	call := cleanupCall(h, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 256})
	e, _ := h.stored(call)
	request := cleanupRequest(t, e)
	if len(request.Capabilities) != 256 {
		t.Fatal("missing full page")
	}
	runCleanupConsumer(t, h, call, e)
	e, _ = h.stored(call)
	if !e.ReaddirClosure.Closed {
		t.Fatal("production C consumer failed to close full page")
	}
	assertCleanupCredits(t, h.f.provider, 2)
}

func TestReaddirCleanupAtomicFailureReopenAndRetirement(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	seedCleanupDirectory(h, 2)
	root := cleanupRoot(h)
	call := cleanupCall(h, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 256})
	e, _ := h.stored(call)
	request := cleanupRequest(t, e)
	original := bytes.Clone(e.Result)
	var rootResult, pageResult struct{ ID string }
	rootEntry, _ := h.stored(h.original)
	mustAllocation(t, json.Unmarshal(rootEntry.Result, &rootResult))
	mustAllocation(t, json.Unmarshal(e.Result, &pageResult))
	retire := map[string]any{"realm": call.Claim.Realm, "subject": call.Claim.Subject, "anchor": call.Claim.Anchor, "nonce": call.Claim.Nonce, "through": call.Claim.Number, "result_hashes": []string{rootResult.ID, pageResult.ID}, "credentials": call.Credentials}
	raw, err := h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "BLOCKED" {
		t.Fatal("retired an unclosed page")
	}
	stop := errors.New("cleanup transaction interrupted")
	h.f.provider.config.Failpoint = func(name string) error {
		if name == "readdir-cleanup-before-commit" {
			return stop
		}
		return nil
	}
	if _, err = h.send("READDIR_CLEANUP", request); !errors.Is(err, stop) {
		t.Fatalf("missing atomic failure: %v", err)
	}
	h.f.provider.config.Failpoint = nil
	e, _ = h.stored(call)
	if e.ReaddirClosure.Closed {
		t.Fatal("failed closure committed")
	}
	// Reopen exact protected ledger, not a reconstructed callback result.
	mustAllocation(t, h.f.provider.Close())
	h.f.provider = nil
	h.f.open()
	h.config.Claims = h.f.provider
	h.config.Native = &nativeFacts{native: h.native}
	h.config.Execution = h.f.provider
	h.bridge, err = guestbridge.New(t.Context(), h.config)
	mustAllocation(t, err)
	_, err = h.send("HELLO", *h.config.Protocol)
	mustAllocation(t, err)
	_, err = h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	e, _ = h.stored(call)
	if !e.ReaddirClosure.Closed || !bytes.Equal(e.Result, original) {
		t.Fatal("reopen lost original closure/result")
	}
	raw, err = h.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "RETIRED" {
		t.Fatal("closed complete coverage did not retire")
	}
	// Old receipt remains immutable after floor advancement; charges stay owned.
	_, err = h.send("READDIR_CLEANUP", request)
	mustAllocation(t, err)
	assertCleanupCredits(t, h.f.provider, 2)
}
