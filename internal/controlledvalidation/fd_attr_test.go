package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/storage"
)

// Task s64 (docs/design/s64-fd-attr.md) against the durable controlled
// provider: claims, ControlledCut planning and journaled SETATTR.

// fdAttrChmod is a SETATTR "attributes" argument that sets mode 0604.
var fdAttrChmod = map[string]any{"mode": 0o604, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}

// fdAttrOther is a non-owner credential (uid/gid 2000) in its own process.
func fdAttrOther(owner guestbridge.Credentials) guestbridge.Credentials {
	other := owner
	other.FSUID, other.FSGID, other.Groups, other.Process = 2000, 2000, []uint32{}, id(68).String()
	return other
}

// newFdAttrHarness is newBridgeHarness with room for more live grants and
// the non-owner credential of fdAttrOther admitted by the attestor.
func newFdAttrHarness(t *testing.T) *bridgeHarness {
	t.Helper()
	f := newFixture(t)
	scope := f.pc.Authorization.Enrollment.Scope
	n := guestbridge.Native{UID: 1000, GID: 1001, PID: 123, Unique: 1, Groups: []uint32{200}, Process: id(66).String(), Endpoint: f.pc.Authorization.Enrollment.Endpoint}
	root := f.newBinding("ROOT", map[string]any{}, false)
	owner := root.Call.Credentials
	other := fdAttrOther(owner)
	allowed := map[string]guestbridge.Credentials{owner.Process: owner, other.Process: other}
	s, err := guestbridge.New(t.Context(), guestbridge.Config{Validation: f.config.Validation, Endpoint: n.Endpoint, Native: &nativeFacts{native: n}, Attestor: credentialSet{scope: scope, allowed: allowed}, Claims: f.provider, Execution: f.provider, Views: noViews{}, Scopes: []guestbridge.Context{scope}, MaxCapabilities: 16})
	if err != nil {
		t.Fatal(err)
	}
	h := &bridgeHarness{f: f, s: s, n: n, process: owner.Process}
	h.send("HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0})
	h.send("CALL", root.Call)
	h.root = h.query(root)
	return h
}

// callAs is h.call with the given credentials instead of the owner's.
func (h *bridgeHarness) callAs(cred guestbridge.Credentials, op string, args any) wireResult {
	h.f.t.Helper()
	b := h.f.newBinding(op, args, false)
	b.Call.Credentials = cred
	h.send("CALL", b.Call)
	return h.query(b)
}

// fdAttributes queries a terminal claim and returns its result attributes.
func (h *bridgeHarness) fdAttributes(op string, args any) (string, map[string]any) {
	h.f.t.Helper()
	b := h.f.newBinding(op, args, false)
	h.send("CALL", b.Call)
	var result struct {
		Payload struct {
			Result struct {
				Code       string
				Attributes map[string]any
			}
		}
	}
	if err := json.Unmarshal(h.send("QUERY", map[string]any{"claim": b.Call.Claim}), &result); err != nil {
		h.f.t.Fatal(err)
	}
	return result.Payload.Result.Code, result.Payload.Result.Attributes
}

// fdAttrDetach renames ("RENAME") or unlinks the harness file "66696c65".
func fdAttrDetach(h *bridgeHarness, change string) {
	h.f.t.Helper()
	root := h.root.Capabilities[0]
	var changed wireResult
	if change == "RENAME" {
		_, changed = h.call("RENAME", map[string]any{"source_parent": root, "source_name": "66696c65", "destination_parent": root, "destination_name": "6e6577", "no_replace": false})
	} else {
		_, changed = h.call("UNLINK", map[string]any{"parent": root, "name": "66696c65"})
	}
	requireCode(h.f.t, changed, "OK")
}

// TestBridgeDescribedAttrCallsAfterRenameUnlink: after rename or unlink the
// path GETATTR/SETATTR stay Stale, while fstat and fchmod carrying the open
// description reach the original object, whose nlink is 0 once unlinked.
func TestBridgeDescribedAttrCallsAfterRenameUnlink(t *testing.T) {
	for _, change := range []string{"RENAME", "UNLINK"} {
		t.Run(change, func(t *testing.T) {
			h := newBridgeHarness(t)
			_, created := h.create()
			requireCode(t, created, "OK")
			object, description := created.Capabilities[0], created.Capabilities[1]
			fdAttrDetach(h, change)
			_, r := h.call("GETATTR", map[string]any{"object": object})
			requireCode(t, r, "Stale")
			_, r = h.call("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod})
			requireCode(t, r, "Stale")
			_, r = h.call("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod, "description": description})
			requireCode(t, r, "OK")
			code, a := h.fdAttributes("GETATTR", map[string]any{"object": object, "description": description})
			nlink := float64(1)
			if change == "UNLINK" {
				nlink = 0
			}
			if code != "OK" || a["mode"] != float64(0o604) || a["nlink"] != nlink || a["object_allocation"] != object.Allocation {
				t.Fatalf("fstat: %s %v", code, a)
			}
			_, released := h.call("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
			requireCode(t, released, "OK")
			_, r = h.call("GETATTR", map[string]any{"object": object, "description": description})
			requireCode(t, r, "Permission")
		})
	}
}

// TestBridgeDescribedSetattrOwnerRulesMismatchAndForeignScope: on the
// durable path a non-owner (with the owner's description or its own
// read-only one) may fstat an unlinked open file but not chmod or chown it;
// another file's description is Stale; a description re-scoped to another
// session or with an altered token is Permission.
func TestBridgeDescribedSetattrOwnerRulesMismatchAndForeignScope(t *testing.T) {
	h := newFdAttrHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	object, description := created.Capabilities[0], created.Capabilities[1]
	other := fdAttrOther(h.f.newBinding("ROOT", map[string]any{}, false).Call.Credentials)
	readOnly := map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}
	_, r := h.call("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod})
	requireCode(t, r, "OK") // mode 0604: the non-owner may open it read-only
	opened := h.callAs(other, "OPEN", map[string]any{"object": object, "flags": readOnly})
	requireCode(t, opened, "OK")
	reader := opened.Capabilities[0]
	fdAttrDetach(h, "UNLINK")
	chown := map[string]any{"mode": nil, "uid": 2000, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
	for _, d := range []guestbridge.Capability{description, reader} {
		requireCode(t, h.callAs(other, "GETATTR", map[string]any{"object": object, "description": d}), "OK")
		for _, change := range []map[string]any{fdAttrChmod, chown} {
			requireCode(t, h.callAs(other, "SETATTR", map[string]any{"object": object, "attributes": change, "description": d}), "Permission")
		}
	}
	_, second := h.call("CREATE", map[string]any{"parent": h.root.Capabilities[0], "name": "62", "mode": 0o600, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	requireCode(t, second, "OK")
	_, r = h.call("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod, "description": second.Capabilities[1]})
	requireCode(t, r, "Stale")
	rescoped := description
	rescoped.Scope.Session = id(99).String()
	altered := description
	altered.Token = hash([]byte("never issued"))
	for _, d := range []guestbridge.Capability{rescoped, altered} {
		_, r = h.call("GETATTR", map[string]any{"object": object, "description": d})
		requireCode(t, r, "Permission")
	}
}

// TestBridgeDescribedCallReplayAfterRelease: resending a described CALL
// with its original claim after DESCRIPTION_RELEASE returns the recorded
// terminal result (exact bytes), never a fresh Permission.
func TestBridgeDescribedCallReplayAfterRelease(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	object, description := created.Capabilities[0], created.Capabilities[1]
	fdAttrDetach(h, "UNLINK")
	b := h.f.newBinding("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod, "description": description}, false)
	h.send("CALL", b.Call)
	requireCode(t, h.query(b), "OK")
	before, err := h.f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil || before.Status != "TERMINAL" {
		t.Fatalf("original: %s %v", before.Status, err)
	}
	_, released := h.call("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	requireCode(t, released, "OK")
	h.send("CALL", b.Call)
	after, err := h.f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil || after.Status != "TERMINAL" || !bytes.Equal(after.Result, before.Result) {
		t.Fatalf("replay changed the recorded result: %s %v", after.Result, err)
	}
	requireCode(t, h.query(b), "OK")
}

// TestBridgeDescribedSetattrUnlinkedRestartReplay: a described SETATTR on
// an nlink-0 object whose effect is journaled before its outcome is
// recorded (crash) is recovered after a provider and service restart with
// its exact protected result, and the replayed journal keeps the new mode
// on the still-unlinked object.
func TestBridgeDescribedSetattrUnlinkedRestartReplay(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	object, description := created.Capabilities[0], created.Capabilities[1]
	fdAttrDetach(h, "UNLINK")
	b := h.f.newBinding("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod, "description": description}, false)
	h.f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return errors.New("crash after the journaled SETATTR")
		}
		return nil
	}
	h.send("CALL", b.Call)
	saved := h.entry(b)
	if saved.Status != "PENDING" || len(saved.ProposedResult) == 0 {
		t.Fatalf("setattr not protected before its effect: %s", saved.Status)
	}
	fdAttrRestart(h.f)
	state, err := h.f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready, err := h.f.provider.RecoverOutcome(t.Context(), state.Binding); err != nil || !ready {
		t.Fatalf("recover: ready=%v %v", ready, err)
	}
	terminal, err := h.f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil || terminal.Status != "TERMINAL" || !bytes.Equal(terminal.Result, saved.ProposedResult) {
		t.Fatalf("recovered result: %s %s %v", terminal.Status, terminal.Result, err)
	}
	stat := fdAttrStat(h.f, fsapi.ObjectID{Home: parseUUID(t, object.Home), Allocation: parseUUID(t, object.Allocation)})
	if stat.Attr.Mode != 0o604 || stat.Attr.Nlink != 0 {
		t.Fatalf("replayed object: mode %o nlink %d", stat.Attr.Mode, stat.Attr.Nlink)
	}
}

// fdAttrRestart closes the provider and the service and starts both again
// on a new serving generation, replaying the journal.
func fdAttrRestart(f *fixture) {
	f.t.Helper()
	f.provider.config.Failpoint = nil
	if err := f.provider.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.provider = nil
	if err := f.service.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.service = nil
	f.workspace(id(34))
	f.start()
	f.enroll()
	f.open()
}

// fdAttrStat reads an object's committed state through a controlled cut.
func fdAttrStat(f *fixture, object fsapi.ObjectID) (out storage.Object) {
	f.t.Helper()
	err := f.service.Controlled(f.t.Context(), *f.config.Validation, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		var err error
		out, err = cut.Stat(ctx, object)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func parseUUID(t *testing.T, s string) fsapi.UUID {
	t.Helper()
	u, err := fsapi.ParseUUID(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// TestBridgeDescribedAttrStaleWhenServiceHandleIsGone: a DESCRIPTION grant
// that is still live in the bridge ledger but whose service handle has been
// released (here directly through the controlled cut) authorizes nothing:
// fsservice cannot resolve the handle, so the described calls are Stale.
func TestBridgeDescribedAttrStaleWhenServiceHandleIsGone(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	object, description := created.Capabilities[0], created.Capabilities[1]
	handle := fdAttrHandle(h, description)
	release := command(h.f, fsapi.Release)
	release.Object = fsapi.ObjectID{Home: parseUUID(t, object.Home), Allocation: parseUUID(t, object.Allocation)}
	release.Handle = handle
	release.DescriptionOnly = true
	operation, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	err = h.f.service.Controlled(t.Context(), *h.f.config.Validation, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		plan, err := cut.PlanControlled(ctx, fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}, release, operation, fsapi.ObjectID{}, nil)
		if err != nil {
			return err
		}
		result, err := cut.ApplyControlled(ctx, plan, false)
		if err == nil && result.Code != fsapi.OK {
			return result.Code
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, r := h.call("GETATTR", map[string]any{"object": object, "description": description})
	requireCode(t, r, "Stale")
	_, r = h.call("SETATTR", map[string]any{"object": object, "attributes": fdAttrChmod, "description": description})
	requireCode(t, r, "Stale")
}

// fdAttrHandle reads a description grant's service handle from the
// provider's retained bridge snapshot.
func fdAttrHandle(h *bridgeHarness, description guestbridge.Capability) fsapi.OpenHandle {
	h.f.t.Helper()
	raw, err := h.f.provider.Restore(h.f.t.Context(), h.f.pc.Authorization.Enrollment.Scope)
	if err != nil {
		h.f.t.Fatal(err)
	}
	var snapshot struct {
		Grants map[string]struct {
			Handle   fsapi.OpenHandle
			Released bool
		}
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		h.f.t.Fatal(err)
	}
	g, ok := snapshot.Grants[description.Token]
	if !ok || g.Released || g.Handle == 0 {
		h.f.t.Fatalf("no live handle for the description: %+v", g)
	}
	return g.Handle
}
