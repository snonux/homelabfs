package controlledvalidation

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

type directoryResult struct {
	wireResult
	Entries []struct{ Capability guestbridge.Capability }
}

func (h *bridgeHarness) scan() (guestbridge.Binding, directoryResult) {
	h.f.t.Helper()
	b, result := h.call("READDIR", map[string]any{"object": h.root.Capabilities[0], "after": "", "revision": 0, "limit": 1})
	var page directoryResult
	if err := json.Unmarshal(h.entry(b).Result, &page); err != nil {
		h.f.t.Fatal(err)
	}
	if result.Code != "OK" || len(page.Entries) != 1 {
		h.f.t.Fatalf("scan: %+v", page)
	}
	return b, page
}
func (h *bridgeHarness) forget(cap guestbridge.Capability) (guestbridge.Binding, wireResult) {
	return h.call("FORGET", map[string]any{"object": cap, "count": 1})
}
func requireCode(t *testing.T, r wireResult, code string) {
	t.Helper()
	if r.Code != code {
		t.Fatalf("got %+v, want %s", r, code)
	}
}
func TestBridgeReclaimsExactCapabilityLifetimes(t *testing.T) {
	h := newBridgeHarness(t) // deliberately five live capabilities
	h.f.provider.config.ClaimCreditBytes = 1 << 20
	h.f.provider.config.SourceCreditBytes = 1 << 19
	_, created := h.create()
	requireCode(t, created, "OK")
	path, description := created.Capabilities[0], created.Capabilities[1]
	_, closed := h.call("DESCRIPTION_CLOSE", map[string]any{"description": description, "process": h.process, "close_sequence": 1})
	requireCode(t, closed, "OK")
	_, released := h.call("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	requireCode(t, released, "OK")
	_, forgotten := h.forget(path)
	requireCode(t, forgotten, "OK")
	tokens := map[string]bool{}
	for i := 0; i < 12; i++ {
		scan, page := h.scan()
		cap := page.Entries[0].Capability
		if tokens[cap.Token] {
			t.Fatal("reused token")
		}
		tokens[cap.Token] = true
		if target := os.Getenv("HOMELABFS_READDIR_FIXTURE"); target != "" && i == 0 {
			raw, err := json.Marshal(map[string]any{"context": scan.Context, "credentials": scan.Call.Credentials, "result": json.RawMessage(h.entry(scan).Result)})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(target, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if target := os.Getenv("HOMELABFS_READDIR_FIXTURE"); target != "" && i == 1 {
			raw, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			var fixture map[string]json.RawMessage
			if err = json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			fixture["next_result"] = h.entry(scan).Result
			raw, err = json.Marshal(fixture)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(target, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}
		original := bytes.Clone(h.entry(scan).Result)
		forget, r := h.forget(cap)
		requireCode(t, r, "OK")
		h.send("CALL", forget.Call)
		requireCode(t, h.query(forget), "OK")
		_, duplicate := h.forget(cap)
		requireCode(t, duplicate, "Permission")
		h.send("CALL", scan.Call)
		_ = h.query(scan)
		if !bytes.Equal(original, h.entry(scan).Result) {
			t.Fatal("replay changed original directory result")
		}
		_, stale := h.call("GETATTR", map[string]any{"object": cap})
		requireCode(t, stale, "Permission")
		// A fresh lookup owns its own pin; no scan forget may consume it.
		_, lookup := h.call("LOOKUP", map[string]any{"parent": h.root.Capabilities[0], "name": "66696c65"})
		requireCode(t, lookup, "OK")
		_, opened := h.call("OPEN", map[string]any{"object": lookup.Capabilities[0], "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}})
		requireCode(t, opened, "OK")
		d := opened.Capabilities[0]
		_, close := h.call("DESCRIPTION_CLOSE", map[string]any{"description": d, "process": h.process, "close_sequence": 1})
		requireCode(t, close, "OK")
		release, r := h.call("DESCRIPTION_RELEASE", map[string]any{"description": d, "release_sequence": 1})
		requireCode(t, r, "OK")
		h.send("CALL", release.Call)
		requireCode(t, h.query(release), "OK")
		_, r = h.call("DESCRIPTION_RELEASE", map[string]any{"description": d, "release_sequence": 2})
		requireCode(t, r, "Permission")
		_, r = h.forget(lookup.Capabilities[0])
		requireCode(t, r, "OK")
	}
}
func TestBridgeHeldCapabilitiesStillExhaustLiveBudget(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	_, first := h.scan()
	cap := first.Entries[0].Capability
	_, full := h.call("READDIR", map[string]any{"object": h.root.Capabilities[0], "after": "", "revision": 0, "limit": 1})
	requireCode(t, full, "NoSpace")
	_, bad := h.call("FORGET", map[string]any{"object": cap, "count": 2})
	requireCode(t, bad, "Invalid")
	_, stillFull := h.call("OPEN", map[string]any{"object": created.Capabilities[0], "flags": map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}})
	requireCode(t, stillFull, "NoSpace")
	_, r := h.forget(cap)
	requireCode(t, r, "OK")
	_, _ = h.scan()
}
func TestBridgeTransientForgetInterruptedProtection(t *testing.T) {
	for _, point := range []string{"plan-protected", "effect-before-outcome"} {
		t.Run(point, func(t *testing.T) {
			h := newBridgeHarness(t)
			_, created := h.create()
			requireCode(t, created, "OK")
			_, page := h.scan()
			cap := page.Entries[0].Capability
			b := h.f.newBinding("FORGET", map[string]any{"object": cap, "count": 1}, false)
			stop := errors.New("transient forget interrupted")
			h.f.provider.config.Failpoint = func(name string) error {
				if name == point {
					return stop
				}
				return nil
			}
			h.send("CALL", b.Call)
			h.f.provider.config.Failpoint = nil
			saved := h.entry(b)
			if saved.Plan == nil || !saved.Plan.TransientForget {
				t.Fatal("lost typed transient plan")
			}
			// Reopen while still PENDING, then recover only original protected work.
			if err := h.f.provider.Close(); err != nil {
				t.Fatal(err)
			}
			h.f.provider = nil
			h.f.pc.Initialize = false
			h.f.open()
			_, ready, err := h.f.provider.RecoverOutcome(t.Context(), saved.Binding)
			if point == "plan-protected" {
				if ready || !errors.Is(err, fsapi.Unavailable) {
					t.Fatalf("invented effect: %v %v", ready, err)
				}
				raw, restoreErr := h.f.provider.Restore(t.Context(), b.Context)
				if restoreErr != nil {
					t.Fatal(restoreErr)
				}
				var previous struct {
					Grants map[string]struct{ Released bool }
				}
				if restoreErr = json.Unmarshal(raw, &previous); restoreErr != nil {
					t.Fatal(restoreErr)
				}
				if previous.Grants[cap.Token].Released {
					t.Fatal("unexecuted plan published predicted forget")
				}
				return
			}
			if !ready || err != nil {
				t.Fatalf("lost original effect: %v %v", ready, err)
			}
			state, err := h.f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
			if err != nil || state.Status != "TERMINAL" || !bytes.Equal(state.Result, saved.ProposedResult) {
				t.Fatalf("durable original forget: %s %v", state.Status, err)
			}
			raw, err := h.f.provider.Restore(t.Context(), b.Context)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				Grants map[string]struct{ Released bool }
			}
			if err = json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatal(err)
			}
			if !snapshot.Grants[cap.Token].Released {
				t.Fatal("recovered terminal forgot its tombstone")
			}

		})
	}
}

func TestBridgeCapabilityIdentityBudgetNeverReusesTombstones(t *testing.T) {
	h := newBridgeHarnessLimits(t, 8)
	_, created := h.create()
	requireCode(t, created, "OK")
	for i := 0; i < 4; i++ {
		_, page := h.scan()
		_, r := h.forget(page.Entries[0].Capability)
		requireCode(t, r, "OK")
	}
	_, exhausted := h.call("READDIR", map[string]any{"object": h.root.Capabilities[0], "after": "", "revision": 0, "limit": 1})
	requireCode(t, exhausted, "NoSpace")
	// Lifetime commands need no new identities even when issuance is exhausted.
	_, r := h.forget(created.Capabilities[0])
	requireCode(t, r, "OK")
	_, r = h.call("DESCRIPTION_RELEASE", map[string]any{"description": created.Capabilities[1], "release_sequence": 1})
	requireCode(t, r, "OK")
	_, exhausted = h.call("LOOKUP", map[string]any{"parent": h.root.Capabilities[0], "name": "66696c65"})
	requireCode(t, exhausted, "NoSpace")
}

func TestBridgeForgetAfterUnlinkPreservesOpenDescription(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	_, page := h.scan()
	_, unlinked := h.call("UNLINK", map[string]any{"parent": h.root.Capabilities[0], "name": "66696c65"})
	requireCode(t, unlinked, "OK")
	altered := page.Entries[0].Capability
	altered.Rights = 1
	_, forged := h.forget(altered)
	requireCode(t, forged, "Permission")
	_, transient := h.forget(page.Entries[0].Capability)
	requireCode(t, transient, "OK")
	_, path := h.forget(created.Capabilities[0])
	requireCode(t, path, "OK")
	_, read := h.call("READ", map[string]any{"description": created.Capabilities[1], "offset": 0, "count": 1})
	requireCode(t, read, "OK")
	_, released := h.call("DESCRIPTION_RELEASE", map[string]any{"description": created.Capabilities[1], "release_sequence": 1})
	requireCode(t, released, "OK")
	_, stale := h.call("READ", map[string]any{"description": created.Capabilities[1], "offset": 0, "count": 1})
	requireCode(t, stale, "Permission")
}

// TestBridgeDescriptionBackedTruncateSyncAfterRenameUnlink pins r64 against
// the durable provider: ftruncate after rename and fsync of an open unlinked
// file carry the open description, so the outdated object capability is only
// checked as a live grant. A null description keeps the path re-check.
func TestBridgeDescriptionBackedTruncateSyncAfterRenameUnlink(t *testing.T) {
	times := map[string]any{"atime": nil, "mtime": nil, "ctime": nil, "atime_now": false, "mtime_now": false}
	for _, change := range []string{"RENAME", "UNLINK"} {
		t.Run(change, func(t *testing.T) {
			h := newBridgeHarness(t)
			_, created := h.create()
			requireCode(t, created, "OK")
			object, description := created.Capabilities[0], created.Capabilities[1]
			_, written := h.call("WRITE", map[string]any{"description": description, "offset": 0, "max_end": int64(1<<63 - 1), "data": "616263646566"})
			requireCode(t, written, "OK")
			root := h.root.Capabilities[0]
			var changed wireResult
			if change == "RENAME" {
				_, changed = h.call("RENAME", map[string]any{"source_parent": root, "source_name": "66696c65", "destination_parent": root, "destination_name": "6e6577", "no_replace": false})
			} else {
				_, changed = h.call("UNLINK", map[string]any{"parent": root, "name": "66696c65"})
			}
			requireCode(t, changed, "OK")
			_, r := h.call("TRUNCATE", map[string]any{"object": object, "description": nil, "size": 2, "times": times})
			requireCode(t, r, "Stale")
			_, r = h.call("SYNC", map[string]any{"object": object, "description": nil, "scope": "FILE"})
			requireCode(t, r, "Stale")
			_, r = h.call("TRUNCATE", map[string]any{"object": object, "description": description, "size": 2, "times": times})
			requireCode(t, r, "OK")
			_, r = h.call("SYNC", map[string]any{"object": object, "description": description, "scope": "FILE"})
			requireCode(t, r, "OK")
			_, r = h.call("READ", map[string]any{"description": description, "offset": 0, "count": 6})
			requireCode(t, r, "OK")
			if r.Data != "6162" {
				t.Fatalf("read after truncate: %q", r.Data)
			}
		})
	}
}

// TestBridgeDescriptionBackedTruncateRejectsMismatchAndDeadTokens: an object
// other than the description's is Stale; a forged or released object token is
// Permission even though the description itself is live.
func TestBridgeDescriptionBackedTruncateRejectsMismatchAndDeadTokens(t *testing.T) {
	times := map[string]any{"atime": nil, "mtime": nil, "ctime": nil, "atime_now": false, "mtime_now": false}
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	object, description := created.Capabilities[0], created.Capabilities[1]
	truncate := func(obj guestbridge.Capability) wireResult {
		_, r := h.call("TRUNCATE", map[string]any{"object": obj, "description": description, "size": 0, "times": times})
		return r
	}
	// The live root grant names another object than the description.
	requireCode(t, truncate(h.root.Capabilities[0]), "Stale")
	forged := object
	forged.Token = hash([]byte("never-issued"))
	requireCode(t, truncate(forged), "Permission")
	_, r := h.forget(object)
	requireCode(t, r, "OK")
	requireCode(t, truncate(object), "Permission")
}

func TestBridgeExitedIssuerCanOnlyCloseExactOriginalGrants(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	_, page := h.scan()
	cap := page.Entries[0].Capability
	description := created.Capabilities[1]
	_, exited := h.call("PROCESS_EXIT", map[string]any{"process": h.process, "exit_sequence": 1})
	requireCode(t, exited, "OK")
	reject := func(op string, args any, rev bool) {
		t.Helper()
		b := h.f.newBinding(op, args, false)
		if rev {
			b.Call.Credentials.Revision++
		}
		reply := h.send("CALL", b.Call)
		if !bytes.Contains(reply, []byte(`"status":"REJECTED"`)) {
			t.Fatalf("exited issuer admitted %s: %s", op, reply)
		}
	}
	reject("READ", map[string]any{"description": description, "offset": 0, "count": 1}, false)
	reject("DESCRIPTION_CLOSE", map[string]any{"description": description, "process": h.process, "close_sequence": 1}, false)
	reject("FORGET", map[string]any{"object": cap, "count": 1}, true)
	forged := cap
	forged.Rights = 1
	reject("FORGET", map[string]any{"object": forged, "count": 1}, false)
	wrong := cap
	wrong.Token = hash([]byte("never-issued"))
	reject("FORGET", map[string]any{"object": wrong, "count": 1}, false)
	reject("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1}, true)
	forget, r := h.forget(cap)
	requireCode(t, r, "OK")
	h.send("CALL", forget.Call)
	requireCode(t, h.query(forget), "OK")
	reject("FORGET", map[string]any{"object": cap, "count": 1}, false)
	_, r = h.forget(created.Capabilities[0])
	requireCode(t, r, "OK")
	release, r := h.call("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	requireCode(t, r, "OK")
	h.send("CALL", release.Call)
	requireCode(t, h.query(release), "OK")
	reject("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 2}, false)
	reject("READ", map[string]any{"description": description, "offset": 0, "count": 1}, false)
}

func TestPrivateLifetimeSchemaRefusesUnclassifiedLedger(t *testing.T) {
	f := newFixture(t)
	if err := f.provider.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(ledgerVersion))
		var scope map[string]json.RawMessage
		if err := json.Unmarshal(bucket.Get([]byte("scope")), &scope); err != nil {
			return err
		}
		delete(scope, "CapabilityLifetimes")
		raw, err := json.Marshal(scope)
		if err != nil {
			return err
		}
		return bucket.Put([]byte("scope"), raw)
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.provider.Close(); err != nil {
		t.Fatal(err)
	}
	f.provider = nil
	f.pc.Initialize = false
	old, err := New(t.Context(), f.pc)
	if old != nil {
		_ = old.Close()
	}
	if !errors.Is(err, fsapi.Permission) {
		t.Fatalf("unclassified ledger opened: %v", err)
	}
}
