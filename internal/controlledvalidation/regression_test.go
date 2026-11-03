package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

type credentialSet struct {
	scope   guestbridge.Context
	allowed map[string]guestbridge.Credentials
}

func (c credentialSet) Verify(_ context.Context, scope guestbridge.Context, cred guestbridge.Credentials) error {
	expected, ok := c.allowed[cred.Process]
	a, _ := json.Marshal(expected)
	b, _ := json.Marshal(cred)
	if !ok || scope != c.scope || !bytes.Equal(a, b) {
		return fsapi.Permission
	}
	return nil
}

type bridgeHarness struct {
	f       *fixture
	s       *guestbridge.Service
	n       guestbridge.Native
	cell    uint64
	process string
	root    wireResult
}

func newBridgeHarness(t *testing.T) *bridgeHarness {
	t.Helper()
	f := newFixture(t)
	scope := f.pc.Authorization.Enrollment.Scope
	n := guestbridge.Native{UID: 1000, GID: 1001, PID: 123, Unique: 1, Groups: []uint32{200}, Process: id(66).String(), Endpoint: f.pc.Authorization.Enrollment.Endpoint}
	root := f.newBinding("ROOT", map[string]any{}, false)
	other := root.Call.Credentials
	other.Process = id(67).String()
	s, err := guestbridge.New(t.Context(), guestbridge.Config{Validation: f.config.Validation, Endpoint: n.Endpoint, Native: &nativeFacts{native: n}, Attestor: credentialSet{scope: scope, allowed: map[string]guestbridge.Credentials{root.Call.Credentials.Process: root.Call.Credentials, other.Process: other}}, Claims: f.provider, Execution: f.provider, Views: noViews{}, Scopes: []guestbridge.Context{scope}, MaxCapabilities: 5})
	if err != nil {
		t.Fatal(err)
	}
	h := &bridgeHarness{f: f, s: s, n: n, process: root.Call.Credentials.Process}
	h.send("HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0})
	h.send("CALL", root.Call)
	h.root = h.query(root)
	return h
}
func (h *bridgeHarness) send(op string, payload any) []byte {
	h.f.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		h.f.t.Fatal(err)
	}
	h.cell++
	response, err := h.s.Execute(h.f.t.Context(), h.n, guestbridge.Command{Cell: h.cell, Context: h.f.pc.Authorization.Enrollment.Scope, Op: op, Payload: raw})
	if err != nil {
		h.f.t.Fatal(err)
	}
	return response
}
func (h *bridgeHarness) query(b guestbridge.Binding) wireResult {
	h.f.t.Helper()
	response := h.send("QUERY", map[string]any{"claim": b.Call.Claim})
	var result struct {
		Payload struct {
			Status string
			Result wireResult
		}
	}
	if err := json.Unmarshal(response, &result); err != nil {
		h.f.t.Fatal(err)
	}
	if result.Payload.Status != "TERMINAL" {
		h.f.t.Fatalf("expected terminal: %s", response)
	}
	return result.Payload.Result
}
func (h *bridgeHarness) call(op string, args any) (guestbridge.Binding, wireResult) {
	h.f.t.Helper()
	b := h.f.newBinding(op, args, false)
	h.send("CALL", b.Call)
	return b, h.query(b)
}
func (h *bridgeHarness) create() (guestbridge.Binding, wireResult) {
	return h.call("CREATE", map[string]any{"parent": h.root.Capabilities[0], "name": "66696c65", "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
}
func (h *bridgeHarness) entry(b guestbridge.Binding) entry {
	h.f.t.Helper()
	var out entry
	if err := h.f.provider.db.View(func(tx *bolt.Tx) error { e, _, err := h.f.provider.load(tx, b.Call.Claim); out = e; return err }); err != nil {
		h.f.t.Fatal(err)
	}
	return out
}

func TestBridgeCreditRefusalDoesNotPublishPredictedLifetimeState(t *testing.T) {
	for _, op := range []string{"PROCESS_EXIT", "DESCRIPTION_RELEASE", "DESCRIPTION_CLOSE", "OPEN"} {
		t.Run(op, func(t *testing.T) {
			h := newBridgeHarness(t)
			f := h.f
			_, created := h.create()
			if created.Code != "OK" || len(created.Capabilities) != 2 {
				t.Fatalf("create %+v", created)
			}
			description := created.Capabilities[1]
			var args any
			switch op {
			case "PROCESS_EXIT":
				args = map[string]any{"process": h.process, "exit_sequence": 1}
			case "DESCRIPTION_RELEASE":
				args = map[string]any{"description": description, "release_sequence": 1}
			case "DESCRIPTION_CLOSE":
				args = map[string]any{"description": description, "process": h.process, "close_sequence": 1}
			case "OPEN":
				args = map[string]any{"object": created.Capabilities[0], "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}}
			}
			originalCredit := f.provider.config.ClaimCreditBytes
			f.provider.config.ClaimCreditBytes = 4096
			refused := f.newBinding(op, args, false)
			h.send("CALL", refused.Call)
			pending := h.entry(refused)
			if pending.Status != "RESERVED" || pending.Plan != nil || pending.Outcome != nil || len(pending.ProposedResult) != 0 {
				t.Fatalf("credit refusal admitted plan: %+v", pending)
			}
			if _, err := f.provider.ResolveGap(t.Context(), refused.Context, refused.Call.Claim); err != nil {
				t.Fatalf("never admitted claim lost no-effect resolution: %v", err)
			}
			f.provider.config.ClaimCreditBytes = originalCredit
			_, read := h.call("READ", map[string]any{"description": description, "offset": 0, "count": 1})
			if read.Code != "OK" {
				t.Fatalf("refused %s changed live credentials/description: %+v", op, read)
			}
			retried, result := h.call(op, args)
			if result.Code != "OK" {
				t.Fatalf("refused %s consumed live sequence/grant: %+v", op, result)
			}
			if op == "OPEN" {
				e := h.entry(retried)
				if e.Outcome == nil || e.Outcome.Result.Handle != 2 {
					t.Fatalf("refused OPEN consumed service handle: %+v", e.Outcome)
				}
			}
		})
	}
}

func TestActualLifetimeFailpointsRequireOriginalEffect(t *testing.T) {
	for _, op := range []fsapi.Operation{fsapi.Open, fsapi.Lookup, fsapi.Release, fsapi.Forget, fsapi.DescriptorClose, fsapi.ProcessExit} {
		for _, point := range []string{"plan-protected", "effect-before-outcome"} {
			t.Run(fmt.Sprintf("op-%d/%s", op, point), func(t *testing.T) {
				f := newFixture(t)
				createdBinding := f.binding("CREATE", map[string]any{})
				created, err := execute(f, createdBinding, createCommand(f, "file"))
				if err != nil {
					t.Fatal(err)
				}
				c := command(f, op)
				c.Object = created.Result.Attr.Object
				c.Handle = created.Result.Handle
				opcode := map[fsapi.Operation]string{fsapi.Open: "OPEN", fsapi.Lookup: "LOOKUP", fsapi.Release: "DESCRIPTION_RELEASE", fsapi.Forget: "FORGET", fsapi.DescriptorClose: "DESCRIPTION_CLOSE", fsapi.ProcessExit: "PROCESS_EXIT"}[op]
				if op == fsapi.Open {
					c.Handle = 0
					c.Flags = fsapi.OpenFlags{Access: fsapi.ReadWrite}
				}
				if op == fsapi.Lookup {
					c.Handle = 0
					c.Parent = f.config.Storage.Root
					c.Name = "file"
				}
				if op == fsapi.Release {
					c.DescriptionOnly = true
				}
				if op == fsapi.Forget {
					c.Handle = 0
					c.Count = 1
				}
				if op == fsapi.ProcessExit {
					c.Handle = 0
				}
				b := f.binding(opcode, map[string]any{})
				crash := errors.New("intentional lifetime failpoint")
				f.provider.config.Failpoint = func(name string) error {
					if name == point {
						return crash
					}
					return nil
				}
				if _, err = execute(f, b, c); !errors.Is(err, crash) {
					t.Fatalf("failpoint %v", err)
				}
				f.provider.config.Failpoint = nil
				out, ready, err := f.provider.RecoverOutcome(t.Context(), b)
				if point == "plan-protected" {
					if ready || !errors.Is(err, fsapi.Unavailable) {
						t.Fatalf("unexecuted lifetime reported terminal: %v %v", ready, err)
					}
				} else {
					if !ready || err != nil {
						t.Fatalf("executed lifetime evidence lost: %v %v", ready, err)
					}
					if op == fsapi.Open {
						r := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
						read := command(f, fsapi.Read)
						read.Object = c.Object
						read.Handle = out.Result.Handle
						read.Size = 1
						if err = f.service.Dispatch(t.Context(), r, read); err != nil {
							t.Fatal(err)
						}
						result, err := r.Completion().Wait(t.Context())
						if err != nil || result.Code != fsapi.OK {
							t.Fatalf("recovered DESCRIPTION has no actual handle: %v %v", result.Code, err)
						}
					}
				}
			})
		}
	}
}

func TestBridgePrivateStagingPreservesRawAncestorNames(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.call("CREATE", map[string]any{"parent": h.root.Capabilities[0], "name": "ff", "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	if created.Code != "OK" {
		t.Fatalf("create raw name: %+v", created)
	}
	_, observed := h.call("GETATTR", map[string]any{"object": created.Capabilities[0]})
	if observed.Code != "OK" {
		t.Fatalf("private staging normalized raw ancestor name: %+v", observed)
	}
}

func TestBridgeOpenRecoveryNeverInventsOrOverwritesHandle(t *testing.T) {
	for _, point := range []string{"plan-protected", "effect-before-outcome"} {
		t.Run(point, func(t *testing.T) {
			h := newBridgeHarness(t)
			createdBinding, created := h.create()
			original := h.entry(createdBinding)
			object := original.Outcome.Result.Attr.Object
			b := h.f.newBinding("OPEN", map[string]any{"object": created.Capabilities[0], "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}}, false)
			stopped := errors.New("open crash")
			h.f.provider.config.Failpoint = func(name string) error {
				if name == point {
					return stopped
				}
				return nil
			}
			h.send("CALL", b.Call)
			h.f.provider.config.Failpoint = nil
			if point == "effect-before-outcome" {
				recovered := h.query(b)
				if recovered.Code != "OK" || len(recovered.Capabilities) != 1 {
					t.Fatalf("recovery %+v", recovered)
				}
				_, read := h.call("READ", map[string]any{"description": recovered.Capabilities[0], "offset": 0, "count": 1})
				if read.Code != "OK" {
					t.Fatalf("recovered capability has no service handle: %+v", read)
				}
				return
			}
			request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
			c := command(h.f, fsapi.Open)
			c.Object = object
			c.Flags = fsapi.OpenFlags{Access: fsapi.ReadWrite}
			if err := h.f.service.Dispatch(t.Context(), request, c); err != nil {
				t.Fatal(err)
			}
			native, err := request.Completion().Wait(t.Context())
			if err != nil || native.Code != fsapi.OK {
				t.Fatalf("native OPEN %v %v", native.Code, err)
			}
			predicted := h.entry(b).Plan.Result.Handle
			if native.Handle != predicted {
				t.Fatal("test did not occupy predicted handle")
			}
			h.cell++
			raw, _ := json.Marshal(map[string]any{"claim": b.Call.Claim})
			_, err = h.s.Execute(t.Context(), h.n, guestbridge.Command{Cell: h.cell, Context: b.Context, Op: "QUERY", Payload: raw})
			if !errors.Is(err, fsapi.Unavailable) {
				t.Fatalf("unexecuted OPEN was presented as success: %v", err)
			}
			read := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
			c = command(h.f, fsapi.Read)
			c.Object = object
			c.Handle = native.Handle
			c.Size = 1
			if err = h.f.service.Dispatch(t.Context(), read, c); err != nil {
				t.Fatal(err)
			}
			result, err := read.Completion().Wait(t.Context())
			if err != nil || result.Code != fsapi.OK {
				t.Fatalf("recovery overwrote native handle: %v %v", result.Code, err)
			}
		})
	}
}

func TestBridgeCompletedExitInstallsStateDespiteCanceledReply(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	b := h.f.newBinding("PROCESS_EXIT", map[string]any{"process": h.process, "exit_sequence": 1}, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h.f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			cancel()
		}
		return nil
	}
	raw, err := json.Marshal(b.Call)
	if err != nil {
		t.Fatal(err)
	}
	h.cell++
	_, err = h.s.Execute(ctx, h.n, guestbridge.Command{Cell: h.cell, Context: b.Context, Op: "CALL", Payload: raw})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("reply %v", err)
	}
	h.f.provider.config.Failpoint = nil
	if e := h.entry(b); e.Status != "TERMINAL" {
		t.Fatalf("completed exit lost durable terminal result: %s", e.Status)
	}
	next := h.f.newBinding("READ", map[string]any{"description": created.Capabilities[1], "offset": 0, "count": 1}, false)
	response := h.send("CALL", next.Call)
	if !bytes.Contains(response, []byte("REJECTED")) {
		t.Fatalf("canceled reply prevented completed exit installation: %s", response)
	}
}

func (h *bridgeHarness) assertRetirementCoverage(principal guestbridge.Claim, gaps map[uint64]string) {
	h.f.t.Helper()
	hashes := make([]string, 0, h.f.next)
	if err := h.f.provider.db.View(func(tx *bolt.Tx) error {
		for n := uint64(1); n <= h.f.next; n++ {
			claim := principal
			claim.Number = n
			e, ok, err := h.f.provider.load(tx, claim)
			if err != nil {
				return err
			}
			if !ok {
				return fsapi.Stale
			}
			if e.Status == "TERMINAL" {
				var result wireResult
				if err = json.Unmarshal(e.Result, &result); err != nil {
					return err
				}
				hashes = append(hashes, result.ID)
			} else {
				proof, ok := gaps[n]
				if !ok || !e.Gap || e.Status != "DECLINED" {
					return fsapi.Unavailable
				}
				hashes = append(hashes, proof)
			}
		}
		return nil
	}); err != nil {
		h.f.t.Fatal(err)
	}
	response := h.send("RETIRE", map[string]any{"realm": principal.Realm, "subject": principal.Subject, "anchor": principal.Anchor, "nonce": principal.Nonce, "through": h.f.next, "result_hashes": hashes})
	var retired struct {
		Payload struct {
			Status string
			Floor  uint64
		}
	}
	if err := json.Unmarshal(response, &retired); err != nil {
		h.f.t.Fatal(err)
	}
	if retired.Payload.Status != "RETIRED" || retired.Payload.Floor != h.f.next {
		h.f.t.Fatalf("incomplete retirement coverage: %s", response)
	}
}
func (h *bridgeHarness) assertUnadmittedGap(b guestbridge.Binding) string {
	h.f.t.Helper()
	e := h.entry(b)
	if e.Status != "ALLOCATED" || e.Plan != nil || e.Binding.Operation.Valid() || e.Outcome != nil {
		h.f.t.Fatalf("intervening call was admitted: %+v", e)
	}
	proof, err := h.f.provider.ResolveGap(h.f.t.Context(), b.Context, b.Call.Claim)
	if err != nil {
		h.f.t.Fatal(err)
	}
	return proof
}

func TestBridgePendingOpenBlocksNewSnapshotUntilExactRecovery(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	open := h.f.newBinding("OPEN", map[string]any{"object": created.Capabilities[0], "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}}, false)
	interrupted := errors.New("effect before outcome")
	h.f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return interrupted
		}
		return nil
	}
	h.send("CALL", open.Call)
	h.f.provider.config.Failpoint = nil
	original := bytes.Clone(h.entry(open).ProposedResult)
	attempted := h.f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
	reply := h.send("CALL", attempted.Call)
	if !bytes.Contains(reply, []byte("REJECTED")) {
		t.Fatalf("new snapshot bypassed pending OPEN: %s", reply)
	}
	gap := h.assertUnadmittedGap(attempted)
	recovered := h.query(open)
	var expected wireResult
	if err := json.Unmarshal(original, &expected); err != nil {
		t.Fatal(err)
	}
	if recovered.ID != expected.ID || len(recovered.Capabilities) != 1 || recovered.Capabilities[0].Token != expected.Capabilities[0].Token {
		t.Fatalf("original DESCRIPTION changed: %+v", recovered)
	}
	_, observed := h.call("GETATTR", map[string]any{"object": created.Capabilities[0]})
	if observed.Code != "OK" {
		t.Fatalf("unrelated call after safe recovery: %+v", observed)
	}
	_, read := h.call("READ", map[string]any{"description": recovered.Capabilities[0], "offset": 0, "count": 1})
	if read.Code != "OK" {
		t.Fatalf("exact recovered DESCRIPTION was overwritten: %+v", read)
	}
	h.assertRetirementCoverage(open.Call.Claim, map[uint64]string{attempted.Call.Claim.Number: gap})
}

func TestBridgePendingExitCannotLoseExitedMarker(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	exit := h.f.newBinding("PROCESS_EXIT", map[string]any{"process": h.process, "exit_sequence": 1}, false)
	interrupted := errors.New("exit effect before outcome")
	h.f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return interrupted
		}
		return nil
	}
	h.send("CALL", exit.Call)
	h.f.provider.config.Failpoint = nil
	attempted := h.f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
	reply := h.send("CALL", attempted.Call)
	if !bytes.Contains(reply, []byte("REJECTED")) {
		t.Fatalf("new snapshot bypassed pending exit: %s", reply)
	}
	gap := h.assertUnadmittedGap(attempted)
	if result := h.query(exit); result.Code != "OK" {
		t.Fatalf("exit recovery %+v", result)
	}
	other := h.f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
	other.Call.Credentials.Process = id(67).String()
	h.send("CALL", other.Call)
	if result := h.query(other); result.Code != "OK" {
		t.Fatalf("unrelated process after recovery: %+v", result)
	}
	old := h.f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
	reply = h.send("CALL", old.Call)
	if !bytes.Contains(reply, []byte("REJECTED")) {
		t.Fatalf("exited marker was overwritten: %s", reply)
	}
	oldGap := h.assertUnadmittedGap(old)
	h.assertRetirementCoverage(exit.Call.Claim, map[uint64]string{attempted.Call.Claim.Number: gap, old.Call.Claim.Number: oldGap})
}

type installFault struct {
	delegate Facts
	failed   atomic.Bool
}

func (f *installFault) Check(ctx context.Context, e Enrollment) error {
	if f.failed.Load() {
		return fsapi.Unavailable
	}
	return f.delegate.Check(ctx, e)
}

func TestBridgeCatchesUpTerminalSnapshotBeforeNewPrediction(t *testing.T) {
	for _, op := range []string{"OPEN", "PROCESS_EXIT"} {
		t.Run(op, func(t *testing.T) {
			h := newBridgeHarness(t)
			_, created := h.create()
			var args any
			if op == "OPEN" {
				args = map[string]any{"object": created.Capabilities[0], "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}}
			} else {
				args = map[string]any{"process": h.process, "exit_sequence": 1}
			}
			operation := h.f.newBinding(op, args, false)
			fault := &installFault{delegate: h.f.provider.config.Facts}
			h.f.provider.config.Facts = fault
			h.f.provider.config.Failpoint = func(name string) error {
				if name == "effect-before-outcome" {
					fault.failed.Store(true)
				}
				return nil
			}
			h.send("CALL", operation.Call)
			h.f.provider.config.Failpoint = nil
			saved := h.entry(operation)
			if saved.Status != "TERMINAL" || len(saved.Result) == 0 {
				t.Fatalf("test did not protect terminal before install fault: %s", saved.Status)
			}
			fault.failed.Store(false)
			unrelated := h.f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
			if op == "PROCESS_EXIT" {
				unrelated.Call.Credentials.Process = id(67).String()
			}
			h.send("CALL", unrelated.Call)
			if result := h.query(unrelated); result.Code != "OK" {
				t.Fatalf("unrelated call %+v", result)
			}
			original := h.query(operation)
			if op == "OPEN" {
				var expected wireResult
				if err := json.Unmarshal(saved.Result, &expected); err != nil {
					t.Fatal(err)
				}
				if original.Capabilities[0].Token != expected.Capabilities[0].Token {
					t.Fatal("original capability replaced")
				}
				_, read := h.call("READ", map[string]any{"description": original.Capabilities[0], "offset": 0, "count": 1})
				if read.Code != "OK" {
					t.Fatalf("latest terminal grant overwritten: %+v", read)
				}
			} else {
				old := h.f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
				reply := h.send("CALL", old.Call)
				if !bytes.Contains(reply, []byte("REJECTED")) {
					t.Fatalf("latest terminal exit marker overwritten: %s", reply)
				}
			}
		})
	}
}

func TestPendingSnapshotAdmissionBoundarySurvivesRestart(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	pending := h.f.newBinding("OPEN", map[string]any{"object": created.Capabilities[0], "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}}, false)
	interrupted := errors.New("pending staged OPEN")
	h.f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return interrupted
		}
		return nil
	}
	h.send("CALL", pending.Call)
	f := h.f
	if err := f.provider.Close(); err != nil {
		t.Fatal(err)
	}
	f.provider = nil
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	f.workspace(id(33))
	f.start()
	f.enroll()
	f.open()
	next := f.newBinding("GETATTR", map[string]any{"object": created.Capabilities[0]}, false)
	if _, err := f.provider.Reserve(t.Context(), next); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("restart lost durable staged-operation boundary: %v", err)
	}
	h.assertUnadmittedGap(next)
	state, err := f.provider.Resolve(t.Context(), pending.Context, pending.Call.Claim)
	if err != nil || state.Status != "PENDING" {
		t.Fatalf("original pending claim inaccessible: %s %v", state.Status, err)
	}
	if _, ready, err := f.provider.RecoverOutcome(t.Context(), state.Binding); ready || !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("restart invented an old description effect: %v %v", ready, err)
	}
	if err = f.provider.Check(t.Context(), pending.Context); err != nil {
		t.Fatalf("pending boundary incorrectly closed authenticated query scope: %v", err)
	}
}
