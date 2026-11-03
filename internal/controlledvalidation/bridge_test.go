package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

type nativeFacts struct {
	mu         sync.Mutex
	native     guestbridge.Native
	controller fsapi.UUID
}
type credentialFacts struct {
	expected guestbridge.Credentials
	scope    guestbridge.Context
}
type noViews struct{}
type wireResult struct {
	ID, Code     string
	Count        uint64
	Data         string
	Capabilities []guestbridge.Capability
	Publication  json.RawMessage
}

func (n *nativeFacts) Enroll(_ context.Context, e guestbridge.Endpoint, id fsapi.UUID) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.controller.Valid() || e != n.native.Endpoint {
		return fsapi.Stale
	}
	n.controller = id
	return nil
}
func (n *nativeFacts) Verify(_ context.Context, actual guestbridge.Native, id fsapi.UUID) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(n.native)
	if id != n.controller || !bytes.Equal(a, b) {
		return fsapi.Permission
	}
	return nil
}
func (c credentialFacts) Verify(_ context.Context, s guestbridge.Context, actual guestbridge.Credentials) error {
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(c.expected)
	if s != c.scope || !bytes.Equal(a, b) {
		return fsapi.Permission
	}
	return nil
}
func (noViews) ValidateDrain(context.Context, guestbridge.Binding) error { return fsapi.Unavailable }
func (noViews) Exchange(context.Context, guestbridge.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, fsapi.Unavailable
}

func TestRealBridgeProtectsCapabilitiesBeforeEffectAndReply(t *testing.T) {
	f := newFixture(t)
	scope := f.pc.Authorization.Enrollment.Scope
	native := guestbridge.Native{UID: 1000, GID: 1001, PID: 123, Unique: 1, Groups: []uint32{200}, Process: id(66).String(), Endpoint: f.pc.Authorization.Enrollment.Endpoint}
	verifier := &nativeFacts{native: native}
	template := f.newBinding("ROOT", map[string]any{}, false)
	config := guestbridge.Config{Validation: f.config.Validation, Endpoint: native.Endpoint, Native: verifier, Attestor: credentialFacts{expected: template.Call.Credentials, scope: scope}, Claims: f.provider, Execution: f.provider, Views: noViews{}, Scopes: []guestbridge.Context{scope}}
	wrong := config
	profile := *f.config.Validation
	profile.Name = "unsigned-profile"
	wrong.Validation = &profile
	if _, err := guestbridge.New(t.Context(), wrong); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("unsigned bridge profile %v", err)
	}
	wrong = config
	wrong.Endpoint.RecallNode = 5
	if _, err := guestbridge.New(t.Context(), wrong); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("unsigned endpoint %v", err)
	}
	bridge, err := guestbridge.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	cell := uint64(0)
	request := func(op string, payload any) ([]byte, error) {
		cell++
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return bridge.Execute(t.Context(), native, guestbridge.Command{Cell: cell, Context: scope, Op: op, Payload: raw})
	}
	if _, err = request("HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0}); err != nil {
		t.Fatal(err)
	}
	call := func(b guestbridge.Binding) wireResult {
		t.Helper()
		if _, err = request("CALL", b.Call); err != nil {
			t.Fatal(err)
		}
		state, e := f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
		if e != nil || state.Status != "TERMINAL" {
			t.Fatalf("CALL did not complete real service execution: %s %v", state.Status, e)
		}
		response, err := request("QUERY", map[string]any{"claim": b.Call.Claim})
		if err != nil {
			t.Fatal(err)
		}
		var q struct {
			Payload struct {
				Status string
				Result wireResult
			}
		}
		if err = json.Unmarshal(response, &q); err != nil {
			t.Fatal(err)
		}
		if q.Payload.Status != "TERMINAL" {
			t.Fatalf("nonterminal %s", response)
		}
		if string(q.Payload.Result.Publication) != "null" {
			t.Fatalf("invented publication %s", response)
		}
		return q.Payload.Result
	}
	root := call(template)
	if root.Code != "OK" || len(root.Capabilities) != 2 {
		t.Fatalf("root %+v", root)
	}
	flags := map[string]any{"access": "READWRITE", "append": true, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	b := f.newBinding("CREATE", map[string]any{"parent": root.Capabilities[0], "name": hex.EncodeToString([]byte("file")), "mode": 0660, "umask": 0, "flags": flags}, false)
	crash := errors.New("effect before terminal")
	f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return crash
		}
		return nil
	}
	if _, err = request("CALL", b.Call); err != nil {
		t.Fatal(err)
	}
	var originalResult, originalSnapshot []byte
	if err = f.provider.db.View(func(tx *bolt.Tx) error {
		e, _, err := f.provider.load(tx, b.Call.Claim)
		originalResult = bytes.Clone(e.ProposedResult)
		originalSnapshot = bytes.Clone(e.BridgeState)
		if e.Status != "PENDING" || len(originalResult) == 0 || len(originalSnapshot) == 0 {
			t.Fatal("capabilities not protected before effect")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.provider.config.Failpoint = nil
	response, err := request("QUERY", map[string]any{"claim": b.Call.Claim})
	if err != nil {
		t.Fatal(err)
	}
	var query struct {
		Payload struct {
			Status string
			Result json.RawMessage
		}
	}
	if err = json.Unmarshal(response, &query); err != nil {
		t.Fatal(err)
	}
	if query.Payload.Status != "TERMINAL" || !bytes.Equal(query.Payload.Result, originalResult) {
		t.Fatalf("replaced original capabilities: %s", response)
	}
	var created wireResult
	if err = json.Unmarshal(originalResult, &created); err != nil {
		t.Fatal(err)
	}
	if created.Code != "OK" || len(created.Capabilities) != 2 {
		t.Fatalf("create %s", originalResult)
	}
	// The live grant bytes are exactly those retained before the effect.
	persisted, err := f.provider.Restore(t.Context(), scope)
	if err != nil || !bytes.Equal(persisted, originalSnapshot) {
		t.Fatalf("snapshot changed: %v", err)
	}
	description := created.Capabilities[1]
	write := call(f.newBinding("WRITE", map[string]any{"description": description, "offset": 0, "data": "61626364", "max_end": 3}, false))
	if write.Code != "OK" || write.Count != 3 {
		t.Fatalf("write %+v", write)
	}
	read := call(f.newBinding("READ", map[string]any{"description": description, "offset": 0, "count": 64}, false))
	if read.Code != "OK" || read.Data != "616263" {
		t.Fatalf("read %+v", read)
	}
	tooLarge := call(f.newBinding("WRITE", map[string]any{"description": description, "offset": 0, "data": "65", "max_end": 3}, false))
	if tooLarge.Code != "FileTooLarge" {
		t.Fatalf("tooLarge %+v", tooLarge)
	}
	sync := call(f.newBinding("SYNC", map[string]any{"object": created.Capabilities[0], "description": description, "scope": "FILE"}, false))
	if sync.Code != "OK" {
		t.Fatalf("sync %+v", sync)
	}
	close := call(f.newBinding("DESCRIPTION_CLOSE", map[string]any{"description": description, "process": template.Call.Credentials.Process, "close_sequence": 1}, false))
	if close.Code != "OK" {
		t.Fatalf("close %+v", close)
	}
	released := call(f.newBinding("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1}, false))
	if released.Code != "OK" {
		t.Fatalf("release %+v", released)
	}
	dead := call(f.newBinding("READ", map[string]any{"description": description, "offset": 0, "count": 1}, false))
	if dead.Code != "Permission" {
		t.Fatalf("released description %+v", dead)
	}
	// A real restart after a second CREATE's effect recovers the already chosen
	// capability bytes, although the old generation's descriptions stay excluded.
	crashBinding := f.newBinding("CREATE", map[string]any{"parent": root.Capabilities[0], "name": "72657374617274", "mode": 0660, "umask": 0, "flags": flags}, false)
	f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return crash
		}
		return nil
	}
	if _, err = request("CALL", crashBinding.Call); err != nil {
		t.Fatal(err)
	}
	var exact []byte
	if err = f.provider.db.View(func(tx *bolt.Tx) error {
		e, _, err := f.provider.load(tx, crashBinding.Call.Claim)
		exact = bytes.Clone(e.ProposedResult)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.provider.config.Failpoint = nil
	if err = f.provider.Close(); err != nil {
		t.Fatal(err)
	}
	f.provider = nil
	if err = f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	f.workspace(id(32))
	f.start()
	f.enroll()
	f.open()
	retained, err := f.provider.Resolve(t.Context(), scope, crashBinding.Call.Claim)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready, err := f.provider.RecoverOutcome(t.Context(), retained.Binding); err != nil || !ready {
		t.Fatalf("actual capability restart ready=%v %v", ready, err)
	}
	terminal, err := f.provider.Resolve(t.Context(), scope, crashBinding.Call.Claim)
	if err != nil || terminal.Status != "TERMINAL" || !bytes.Equal(terminal.Result, exact) {
		t.Fatalf("actual capability bytes lost: %s %v", terminal.Result, err)
	}
	config.Claims = f.provider
	config.Execution = f.provider
	// Reconstructing a Go object cannot claim a fresh native endpoint lifetime.
	if _, err = guestbridge.New(t.Context(), config); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("endpoint silently reused: %v", err)
	}
}
