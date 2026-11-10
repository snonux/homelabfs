package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

type localNative struct{ expected Native }
type localAttestor struct{}
type localClaims struct {
	mu     sync.Mutex
	states map[string]ClaimState
	next   byte
}
type localViews struct{}
type localVerifier struct{}
type localDriver struct {
	mu        sync.Mutex
	view      *logicalview.Store
	planner   *fsservice.LogicalPlanner
	authority record.NormalAuthority
	config    Context
	calls     int
	fail      bool
	block     chan struct{}
	entered   chan struct{}
	allowed   map[Context]bool
	serial    uint64
}
type bridgeFixture struct {
	s          *Service
	c          Context
	native     Native
	claims     *localClaims
	driver     *localDriver
	root, file fsapi.ObjectID
	view       *logicalview.Store
	next       uint64
}

func (localNative) Enroll(context.Context, Endpoint, fsapi.UUID) error { return nil }
func (v localNative) Verify(_ context.Context, n Native, _ fsapi.UUID) error {
	if n.UID != v.expected.UID || n.GID != v.expected.GID || n.PID != v.expected.PID || n.Process != v.expected.Process || n.Endpoint != v.expected.Endpoint || !bytes.Equal(uints(n.Groups), uints(v.expected.Groups)) {
		return fsapi.Permission
	}
	return nil
}
func (localAttestor) Verify(_ context.Context, _ Context, c Credentials) error {
	if c.IDMapHash != digest([]byte("idmap")) || c.UserNamespace != uid(80).String() {
		return fsapi.Permission
	}
	return nil
}
func (localVerifier) Check(ctx context.Context, _ logicalview.Check) error { return ctx.Err() }
func (localViews) ValidateDrain(context.Context, Binding) error            { return fsapi.Unavailable }
func (localViews) Exchange(_ context.Context, _ Context, op string, _ json.RawMessage) (json.RawMessage, error) {
	if op == "RESUME" {
		return nil, fsapi.Unavailable
	}
	return marshal(map[string]any{"status": "EMPTY", "event": nil})
}
func (c *localClaims) Resolve(_ context.Context, scope Context, claim Claim) (ClaimState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.states[claimKey(claim)]
	if !ok {
		return ClaimState{Status: "UNKNOWN"}, nil
	}
	if state.Binding.Context != scope {
		return ClaimState{}, fsapi.Permission
	}
	return state, nil
}
func (c *localClaims) Reserve(_ context.Context, b Binding) (ClaimState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := claimKey(b.Call.Claim)
	if old, ok := c.states[key]; ok {
		return old, nil
	}
	c.next++
	b.Operation = uid(c.next)
	if oneOf(b.Call.Intent.Opcode, "CREATE", "MKDIR", "SYMLINK") {
		b.Allocation = fsapi.ObjectID{Home: parseID(b.Context.Volume), Allocation: uid(c.next + 100)}
	}
	state := ClaimState{Status: "RESERVED", Binding: b}
	c.states[key] = state
	return state, nil
}
func (c *localClaims) ProtectResult(_ context.Context, b Binding, r json.RawMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[claimKey(b.Call.Claim)]
	state.Status = "TERMINAL"
	state.Result = bytes.Clone(r)
	c.states[claimKey(b.Call.Claim)] = state
	return nil
}
func (c *localClaims) Cancel(ctx context.Context, scope Context, claim Claim) (ClaimState, error) {
	s, e := c.Resolve(ctx, scope, claim)
	if s.Status == "UNKNOWN" {
		s.Status = "INDETERMINATE"
	}
	return s, e
}
func (c *localClaims) Retire(_ context.Context, _ Context, _ json.RawMessage) (json.RawMessage, error) {
	return marshal(map[string]any{"status": "BLOCKED", "floor": 0, "proof_hash": nil})
}
func (d *localDriver) ServingGeneration(context.Context, Context) (fsapi.UUID, error) {
	return d.view.Generation(), nil
}
func (d *localDriver) Check(ctx context.Context, c Context) error {
	if c != d.config && !d.allowed[c] {
		return fsapi.Permission
	}
	return ctx.Err()
}
func (d *localDriver) Execute(ctx context.Context, b Binding, prepare func(Captured) (Prepared, error)) (Outcome, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.block != nil {
		if d.entered != nil {
			select {
			case d.entered <- struct{}{}:
			default:
			}
		}
		<-d.block
	}
	if d.fail {
		return Outcome{}, fsapi.Unavailable
	}
	o, e := d.view.Begin(ctx)
	if e != nil {
		return Outcome{}, e
	}
	cut, err := NewLogicalCut(d.planner, o)
	if err != nil {
		return Outcome{}, err
	}
	p, e := prepare(cut)
	closeErr := o.Close()
	if closeErr != nil {
		return Outcome{}, closeErr
	}
	if errors.Is(e, fsservice.ErrDelegatedFileTooLarge) {
		return Outcome{DelegatedError: e, Publication: d.publication()}, nil
	}
	if e != nil {
		return Outcome{Result: fsapi.Result{Code: errnoOf(e)}, Publication: d.publication()}, nil
	}
	if p.Plan != nil {
		d.serial++
		hash, e := record.NormalAuthorityHash(d.authority)
		if e != nil {
			return Outcome{}, e
		}
		origin := record.OriginStream{Cluster: d.authority.Cluster, Volume: parseID(b.Context.Volume), Generation: d.authority.Volumes[0].Generation, Configuration: d.authority.Volumes[0].Configuration, Epoch: 1, Origin: parseID(b.Context.OriginNode), Journal: parseID(b.Context.OriginJournal)}
		g := logicalview.Stage{Slot: record.OrderedSlot{Authority: hash, Sequencer: d.authority.Sequencer, Serial: d.serial}, Previous: p.Plan.Cut, Origin: origin, OriginLaunch: record.Launch{Node: origin.Origin, Incarnation: parseID(b.Context.OriginIncarnation)}, Recipient: d.authority.Sequencer, Claim: p.Plan.Claim, Intent: p.Plan.Intent, Operations: []storage.Operation{p.Plan.Operation}}
		if g.Origin.Origin == d.authority.Sequencer.Node {
			g.Recipient = d.authority.OwnerPartner
		}
		digest, e := d.view.Stage(ctx, g)
		if e != nil {
			return Outcome{}, e
		}
		h := sha256.Sum256([]byte("COMPONENT fixture proof, no runtime authority"))
		certificate := record.NormalRef{Stream: origin, Creator: g.Slot.Sequencer, Kind: record.NormalSelectionKind, Sequence: d.serial, SHA256: h}
		source := record.NormalRef{Stream: origin, Creator: g.OriginLaunch, Kind: record.NormalResultKind, Sequence: d.serial, SHA256: h}
		memory := source
		memory.Stream.Origin = g.Recipient.Node
		memory.Creator = g.Recipient
		memory.Kind = record.NormalAcceptanceKind
		ev := logicalview.Evidence{Digest: digest, Selected: record.SelectedRef{Slot: g.Slot, Certificate: certificate}, Sources: []record.SourceCoverage{{Stream: origin, Holder: g.OriginLaunch, Through: d.serial, Last: source}}, Acceptances: []record.NormalRef{memory}, Result: []byte("component-only result"), Proof: []byte("component-only verifier"), Acks: d.view.Domains()}
		if e = d.view.Select(ctx, ev); e != nil {
			return Outcome{}, e
		}
		if e = d.view.Publish(ctx, ev); e != nil {
			return Outcome{}, e
		}
		p.Result = p.Plan.Result
	}
	out := Outcome{Result: p.Result, Publication: d.publication()}
	o, e = d.view.Begin(ctx)
	if e != nil {
		return Outcome{}, e
	}
	defer func() { _ = o.Close() }()
	if b.Call.Intent.Opcode == "CREATE" && out.Result.Attr != nil {
		f := p.Command.Flags
		f.Create = false
		f.Exclusive = false
		f.Truncate = false
		out.Result, e = d.planner.Observe(ctx, o, guestCred(b.Call.Credentials), fsapi.Command{Operation: fsapi.Open, Mount: p.Command.Mount, Object: out.Result.Attr.Object, Flags: f})
		if e != nil {
			return Outcome{}, e
		}
	}
	if oneOf(b.Call.Intent.Opcode, "LOOKUP", "CREATE", "MKDIR", "SYMLINK") {
		entry, e := o.Lookup(ctx, p.Command.Parent, p.Command.Name)
		if e != nil {
			return Outcome{}, e
		}
		out.EntryRevision = entry.Revision
	}
	for _, entry := range out.Result.Entries {
		obj, e := o.Stat(ctx, entry.Object)
		if e != nil {
			return Outcome{}, e
		}
		out.EntryAttributes = append(out.EntryAttributes, obj.Attr)
	}
	if p.Command.Operation == fsapi.Release {
		if e = d.view.ReleaseHandle(ctx, d.view.Generation(), p.Command.Handle); e != nil {
			return Outcome{}, e
		}
	}
	return out, nil
}
func (d *localDriver) publication() json.RawMessage { return json.RawMessage("null") }
func newBridge(t *testing.T) *bridgeFixture {
	t.Helper()
	volume := uid(1)
	root := fsapi.ObjectID{Home: volume, Allocation: uid(2)}
	file := fsapi.ObjectID{Home: volume, Allocation: uid(3)}
	position := fsapi.Position{Volume: volume, Generation: uid(4), Configuration: uid(5), Epoch: 1}
	data := []byte("base")
	baseline := storage.Snapshot{Image: storage.Image{Version: 1, Volume: volume, Root: root, Position: position, Objects: []storage.Object{{Descriptor: storage.Descriptor{Object: root, Kind: fsapi.Directory, Creation: uid(6)}, Attr: fsapi.Attr{Object: root, Kind: fsapi.Directory, UID: 1000, GID: 777, Mode: 0777, Nlink: 2, Revision: 1}, LedgerRevision: 1, Parent: &storage.Parent{Object: root, Revision: 1}}, {Descriptor: storage.Descriptor{Object: file, Kind: fsapi.Regular, Creation: uid(7)}, Attr: fsapi.Attr{Object: file, Kind: fsapi.Regular, UID: 999, GID: 42, Mode: 0004, Size: 4, Nlink: 1, Revision: 1}, LedgerRevision: 1, References: []storage.Reference{{Parent: root, Name: "base", Revision: 1}}}}, Entries: []storage.Entry{{Parent: root, Name: "base", Revision: 1, Target: &file}}, Payloads: []storage.Payload{{Object: file, Size: 4, SHA256: sha256.Sum256(data)}}}, Data: [][]byte{data}}
	a := record.NormalAuthority{Cluster: uid(10), Registry: uid(11), RegistryRevision: 1, RecoveryGeneration: uid(12), Closure: uid(13), ManifestVersion: 1, Admitted: []fsapi.UUID{uid(20), uid(21), uid(22)}, Volumes: []record.NormalVolume{{Volume: volume, Generation: position.Generation, Configuration: position.Configuration, Epoch: 1, Replicas: []fsapi.UUID{uid(20), uid(21), uid(22)}}}, Sequencer: record.Launch{Node: uid(20), Incarnation: uid(23)}, OwnerPartner: record.Launch{Node: uid(21), Incarnation: uid(24)}, FenceLineage: []record.Hash{sha256.Sum256([]byte("fixture exclusion"))}}
	budget, e := admission.New(admission.DefaultConfig())
	if e != nil {
		t.Fatal(e)
	}
	view, e := logicalview.New(t.Context(), logicalview.Config{Authority: a, Generation: uid(30), Baseline: []storage.Snapshot{baseline}, Domains: []logicalview.Domain{{ID: uid(31), Session: uid(32)}, {ID: uid(33), Session: uid(34)}}, Budget: budget, Verifier: localVerifier{}, Limits: logicalview.Limits{MaxSnapshotBytes: 16 << 10, MaxClaimBytes: 16 << 10, Versions: 16, Claims: 32, Handles: 16, Readers: 4, Dependencies: 8, Identities: 256}})
	if e != nil {
		t.Fatal(e)
	}
	unix, e := fsservice.NewUnixPermissions(uid(70))
	if e != nil {
		t.Fatal(e)
	}
	planner, e := fsservice.NewLogicalPlanner(fsservice.LogicalConfig{View: view, Permissions: unix, Limits: fsservice.Limits{IOBytes: 65536, LogicalBytes: 1 << 20}})
	if e != nil {
		t.Fatal(e)
	}
	h, e := record.NormalAuthorityHash(a)
	if e != nil {
		t.Fatal(e)
	}
	c := Context{AuthorityHash: fmt.Sprintf("%x", h), Cluster: a.Cluster.String(), Configuration: uid(5).String(), Export: uid(40).String(), ExporterIncarnation: uid(41).String(), FuseSession: uid(42).String(), Generation: uid(4).String(), GuestBoot: uid(43).String(), GuestMount: uid(44).String(), OriginIncarnation: uid(45).String(), OriginJournal: uid(46).String(), OriginNode: uid(22).String(), PolicyHash: digest([]byte("policy")), RegistryHash: digest([]byte("registry")), Session: uid(47).String(), VM: uid(48).String(), Volume: volume.String()}
	native := Native{UID: 0, GID: 0, PID: 123, Groups: []uint32{0, 5}, Process: uid(49).String(), Endpoint: Endpoint{FuseSession: c.FuseSession, CommandNode: 100, ResultNode: 101, RecallNode: 102}}
	claims := &localClaims{states: map[string]ClaimState{}, next: 80}
	driver := &localDriver{view: view, planner: planner, authority: a, config: c}
	s, e := New(t.Context(), Config{Validation: &fsservice.ValidationProfile{Disposable: true, Name: "retained bridge component only"}, Endpoint: native.Endpoint, Native: localNative{native}, Attestor: localAttestor{}, Claims: claims, Execution: driver, Views: localViews{}, Planner: planner, Scopes: []Context{c}})
	if e != nil {
		t.Fatal(e)
	}
	return &bridgeFixture{s: s, c: c, native: native, claims: claims, driver: driver, root: root, file: file, view: view}
}
func (f *bridgeFixture) cred() Credentials {
	return Credentials{FSUID: 1000, FSGID: 1001, Groups: []uint32{}, Process: uid(79).String(), Revision: 1, IDMapHash: digest([]byte("idmap")), UserNamespace: uid(80).String()}
}
func (f *bridgeFixture) command(t *testing.T, op string, payload any) Command {
	t.Helper()
	f.next++
	raw, e := marshal(payload)
	if e != nil {
		t.Fatal(e)
	}
	c := Command{Cell: f.next, Context: f.c, Op: op, Payload: raw}
	if op == "RELEASE_CELL" {
		var ack Ack
		if e = json.Unmarshal(raw, &ack); e != nil {
			t.Fatal(e)
		}
		c.Reclaim = &ack
	}
	b, e := marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	c, e = DecodeCommand(b)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func (f *bridgeFixture) hello(t *testing.T) {
	t.Helper()
	_, e := f.s.Execute(t.Context(), f.native, f.command(t, "HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0}))
	if e != nil {
		t.Fatal(e)
	}
}
func (f *bridgeFixture) call(t *testing.T, cred Credentials, op string, args any) (Command, map[string]any) {
	t.Helper()
	intent, _ := marshal(Intent{Opcode: op, Args: mustJSON(t, args)})
	call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "616263", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: f.next + 1, OriginalHash: digest(intent)}, Credentials: cred, Intent: Intent{Opcode: op, Args: mustJSON(t, args)}}
	cmd := f.command(t, "CALL", call)
	raw, e := f.s.Execute(t.Context(), f.native, cmd)
	if e != nil {
		t.Fatal(e)
	}
	var envelope map[string]any
	if e = json.Unmarshal(raw, &envelope); e != nil {
		t.Fatal(e)
	}
	if envelope["payload"].(map[string]any)["status"] != "ADMITTED" {
		return cmd, envelope["payload"].(map[string]any)
	}
	state, e := f.claims.Resolve(t.Context(), f.c, call.Claim)
	if e != nil {
		t.Fatal(e)
	}
	if state.Status != "TERMINAL" {
		return cmd, map[string]any{"status": state.Status}
	}
	var result map[string]any
	if e = json.Unmarshal(state.Result, &result); e != nil {
		t.Fatal(e)
	}
	return cmd, result
}
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, e := marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func capabilityAt(t *testing.T, r map[string]any, i int) Capability {
	t.Helper()
	b := mustJSON(t, r["capabilities"].([]any)[i])
	var cap Capability
	if e := json.Unmarshal(b, &cap); e != nil {
		t.Fatal(e)
	}
	return cap
}
func uid(n byte) fsapi.UUID   { var id fsapi.UUID; id[6] = 0x40; id[8] = 0x80; id[15] = n; return id }
func uints(v []uint32) []byte { b, _ := json.Marshal(v); return b }
func claimKey(c Claim) string {
	return c.Realm + ":" + c.Subject + ":" + c.Anchor + ":" + c.Nonce + fmt.Sprint(c.Number)
}
func errnoOf(e error) fsapi.Errno {
	for _, c := range fsapi.Errnos() {
		if errors.Is(e, c) {
			return c
		}
	}
	return fsapi.IO
}

func TestDelegatedDynamicGroupsCommittedRAMAndMetadata(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	cap := capabilityAt(t, root, 0)
	_, look := f.call(t, cred, "LOOKUP", map[string]any{"parent": cap, "name": "62617365"})
	file := capabilityAt(t, look, 0)
	flags := map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}
	_, opened := f.call(t, cred, "OPEN", map[string]any{"object": file, "flags": flags})
	if opened["code"] != "OK" {
		t.Fatal(opened)
	}
	cred.Revision = 2
	cred.Groups = []uint32{42}
	_, denied := f.call(t, cred, "OPEN", map[string]any{"object": file, "flags": flags})
	if denied["code"] != "Permission" {
		t.Fatalf("0004 group member: %v", denied)
	}
	cred.Revision = 3
	cred.Groups = make([]uint32, 1024)
	for i := range cred.Groups {
		cred.Groups[i] = uint32(i + 100)
	}
	_, allowed := f.call(t, cred, "OPEN", map[string]any{"object": file, "flags": flags})
	if allowed["code"] != "OK" {
		t.Fatal(allowed)
	}
	cred = f.cred()
	cred.FSUID = 0
	cred.Revision = 4
	flags["access"] = "READWRITE"
	flags["create"] = true
	flags["exclusive"] = true
	_, created := f.call(t, cred, "CREATE", map[string]any{"parent": cap, "name": "6e6577", "mode": 4095, "umask": 0, "flags": flags})
	if created["code"] != "OK" {
		t.Fatal(created)
	}
	attr := created["attributes"].(map[string]any)
	if attr["uid"] != float64(0) || attr["gid"] != float64(777) || attr["mode"] != float64(4095) {
		t.Fatal(attr)
	}
	// The owner RAM contains it although this component has no backing filesystem.
	o, e := f.view.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = o.Lookup(t.Context(), f.root, "new"); e != nil {
		t.Fatal(e)
	}
	_ = o.Close()
}
func TestDelegatedClaimDedupCancelAndConfinement(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cmd, result := f.call(t, f.cred(), "ROOT", map[string]any{})
	if result["code"] != "OK" {
		t.Fatal(result)
	}
	before := f.driver.calls
	if _, e := f.s.Execute(t.Context(), f.native, cmd); e != nil {
		t.Fatal(e)
	}
	if f.driver.calls != before {
		t.Fatal("duplicate dispatched")
	}
	var call Call
	_ = json.Unmarshal(cmd.Payload, &call)
	call.Credentials.Revision++
	cmd.Payload = mustJSON(t, call)
	b, e := f.s.Execute(t.Context(), f.native, cmd)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(b, []byte("ID_CONFLICT")) {
		t.Fatal(string(b))
	}
	cmd.Context.Volume = uid(90).String()
	if _, e = f.s.Execute(t.Context(), f.native, cmd); !errors.Is(e, fsapi.Permission) {
		t.Fatal(e)
	}
	unknown := call.Claim
	unknown.Number = 900
	cancel := f.command(t, "CANCEL", map[string]any{"claim": unknown})
	b, e = f.s.Execute(t.Context(), f.native, cancel)
	if e != nil || !bytes.Contains(b, []byte("INDETERMINATE")) {
		t.Fatal(string(b), e)
	}
	f.driver.fail = true
	_, pending := f.call(t, f.cred(), "ROOT", map[string]any{})
	if pending["status"] != "RESERVED" {
		t.Fatal(pending)
	}
}
func TestBridgeFrameAndSchemaRejections(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	cap := capabilityAt(t, root, 0)
	flags := map[string]any{"access": "WRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	args := map[string]any{"parent": cap, "name": "78", "mode": 4095, "umask": 512, "flags": flags}
	intent := Intent{Opcode: "CREATE", Args: mustJSON(t, args)}
	call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 99, OriginalHash: digest(mustJSON(t, intent))}, Credentials: cred, Intent: intent}
	cmd := Command{Cell: 99, Context: f.c, Op: "CALL", Payload: mustJSON(t, call)}
	if _, e := DecodeCommand(mustJSON(t, cmd)); e == nil {
		t.Fatal("high umask accepted")
	}
	for _, raw := range []string{`{"x":1,"x":1}`, `{"x":1.0}`, `{"x":-0}`, `{"x":"\u0061"}`, `{ "x":1}`, `{"x":18446744073709551616}`} {
		if _, e := Canonical([]byte(raw)); e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func transportBudget(t *testing.T) *admission.Budget {
	t.Helper()
	var c admission.Config
	for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: 12 << 20} {
		r := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
		c.Pools[class] = admission.Limits{Global: r, PerPeer: r, PerVolume: r}
	}
	b, e := admission.New(c)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func sendNative(t *testing.T, tr *Transport, n Native, c Command, flags uint16) {
	t.Helper()
	raw := mustJSON(t, c)
	hash := sha256.Sum256(raw)
	for offset := 0; offset < len(raw); offset += ChunkBytes {
		frame, e := EncodeFrame(Frame{Kind: 1, Flags: flags, Total: uint32(len(raw)), Offset: uint32(offset), Session: parseID(c.Context.Session), Cell: c.Cell, Hash: hash, Data: raw[offset:min(offset+ChunkBytes, len(raw))]})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tr.Write(t.Context(), n, n.Endpoint.CommandNode, c.Cell*CellStride+uint64(offset/ChunkBytes)*(HeaderBytes+ChunkBytes), frame); e != nil {
			t.Fatal(e)
		}
	}
}
func fetchNative(t *testing.T, tr *Transport, n Native, id uint64) []byte {
	t.Helper()
	return fetchNativeAt(t, tr, n, id, n.Endpoint.ResultNode)
}
func fetchNativeAt(t *testing.T, tr *Transport, n Native, id, node uint64) []byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, e := tr.Read(t.Context(), n, node, id*CellStride, 32768)
		if errors.Is(e, fsapi.WouldBlock) && time.Now().Before(deadline) {
			runtime.Gosched()
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		f, e := DecodeFrame(data)
		if e != nil {
			t.Fatal(e)
		}
		if int(f.Total) != len(f.Data) {
			t.Fatal("test expected one response chunk")
		}
		return f.Data
	}
}
func TestBridgeEndpointFullCreditReplacementAndNativeBinding(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	tr, e := NewTransport(f.s, transportBudget(t))
	if e != nil {
		t.Fatal(e)
	}
	defer tr.Close()
	hello := map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0}
	for i := 0; i < 64; i++ {
		sendNative(t, tr, f.native, f.command(t, "HELLO", hello), 0)
	}
	claim := Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 1, OriginalHash: digest([]byte("x"))}
	for i := 0; i < 16; i++ {
		sendNative(t, tr, f.native, f.command(t, "QUERY", map[string]any{"claim": claim}), 1)
	}
	snapshot := fetchNative(t, tr, f.native, 1)
	ack := &Ack{Cell: 1, ResponseHash: digest(snapshot)}
	replacement := f.command(t, "RELEASE_CELL", ack)
	replacement.Reclaim = ack
	sendNative(t, tr, f.native, replacement, 2)
	_ = fetchNative(t, tr, f.native, 81)
	if _, e := tr.Read(t.Context(), f.native, f.native.Endpoint.ResultNode, CellStride, 1); !errors.Is(e, fsapi.Stale) {
		t.Fatal(e)
	}
	if len(tr.cells) != 80 || tr.ordinary != 64 || tr.control != 16 || tr.high != 81 {
		t.Fatal("credits changed", len(tr.cells), tr.ordinary, tr.control, tr.high)
	}
	forged := f.native
	forged.UID = 1000
	if _, e := tr.Read(t.Context(), forged, f.native.Endpoint.ResultNode, 2*CellStride, 1); !errors.Is(e, fsapi.Permission) {
		t.Fatal(e)
	}
	wrong := *ack
	wrong.ResponseHash = digest([]byte("wrong"))
	bad := f.command(t, "RELEASE_CELL", wrong)
	bad.Reclaim = &wrong
	raw := mustJSON(t, bad)
	frame, _ := EncodeFrame(Frame{Kind: 1, Flags: 2, Total: uint32(len(raw)), Session: parseID(f.c.Session), Cell: bad.Cell, Hash: sha256.Sum256(raw), Data: raw})
	if _, e := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, bad.Cell*CellStride, frame); e == nil {
		t.Fatal("wrong ack accepted")
	}
	if tr.high != 81 || len(tr.cells) != 80 {
		t.Fatal("invalid candidate changed allocator")
	}
}

func TestBridgeNativeFlagsFragmentedAdmissionAndPadding(t *testing.T) {
	for _, tt := range []struct {
		name, opcode string
		flags        uint16
		change       bool
		want         error
	}{{"ordinary", "WRITE", 0, false, nil}, {"drain", "DRAIN_WRITE", 1, false, nil}, {"wrong-control", "WRITE", 1, false, fsapi.Permission}, {"wrong-ordinary", "DRAIN_WRITE", 0, false, fsapi.Permission}, {"changed-chunks", "WRITE", 0, true, fsapi.Invalid}} {
		t.Run(tt.name, func(t *testing.T) {
			f := newBridge(t)
			f.hello(t)
			f.driver.fail = true
			tr, e := NewTransport(f.s, transportBudget(t))
			if e != nil {
				t.Fatal(e)
			}
			defer tr.Close()
			cap := Capability{Token: digest([]byte("unknown-but-well-shaped")), Kind: "DESCRIPTION", Scope: f.c, Home: f.c.Volume, Allocation: uid(3).String(), Description: ptr(uid(100).String()), Rights: 3, Revision: 1, CredentialHash: digest([]byte("cred")), AncestorsHash: digest([]byte("ancestors"))}
			args := map[string]any{"description": cap, "offset": 0, "max_end": int64(1<<63 - 1), "data": fmt.Sprintf("%x", bytes.Repeat([]byte{0xab}, 65536))}
			if tt.opcode == "DRAIN_WRITE" {
				delete(args, "max_end")
				args["recall"] = uid(100).String()
				args["dirty_entry"] = digest([]byte("dirty"))
				args["reservation"] = digest([]byte("reserved"))
			}
			intent := Intent{Opcode: tt.opcode, Args: mustJSON(t, args)}
			call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 1, OriginalHash: digest(mustJSON(t, intent))}, Credentials: f.cred(), Intent: intent}
			command := Command{Cell: 1, Context: f.c, Op: "CALL", Payload: mustJSON(t, call)}
			raw := mustJSON(t, command)
			var got error
			for off := 0; off < len(raw); off += ChunkBytes {
				flags := tt.flags
				if tt.change && off > 0 {
					flags ^= 1
				}
				frame, e := EncodeFrame(Frame{Kind: 1, Flags: flags, Total: uint32(len(raw)), Offset: uint32(off), Session: parseID(f.c.Session), Cell: 1, Hash: sha256.Sum256(raw), Data: raw[off:min(off+ChunkBytes, len(raw))]})
				if e != nil {
					t.Fatal(e)
				}
				_, got = tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride+uint64(off/ChunkBytes)*(HeaderBytes+ChunkBytes), frame)
				if got != nil {
					break
				}
			}
			if !errors.Is(got, tt.want) {
				t.Fatalf("admission=%v want=%v", got, tt.want)
			}
		})
	}
}
func ptr(s string) *string { return &s }
func TestDelegatedDescriptionKeepsGrantAfterChmodUnlinkAndFinalRelease(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	cred.FSUID = 0
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	parent := capabilityAt(t, root, 0)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	_, created := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": "6c697665", "mode": 384, "umask": 0, "flags": flags})
	object, description := capabilityAt(t, created, 0), capabilityAt(t, created, 1)
	_, changed := f.call(t, cred, "SETATTR", map[string]any{"object": object, "attributes": map[string]any{"mode": 0, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}})
	if changed["code"] != "OK" {
		t.Fatal(changed)
	}
	_, removed := f.call(t, cred, "UNLINK", map[string]any{"parent": parent, "name": "6c697665"})
	if removed["code"] != "OK" {
		t.Fatal(removed)
	}
	cred.FSUID = 1234
	cred.Revision = 2
	_, written := f.call(t, cred, "WRITE", map[string]any{"description": description, "offset": 0, "max_end": int64(1<<63 - 1), "data": "616263"})
	if written["code"] != "OK" || written["count"] != float64(3) {
		t.Fatal(written)
	}
	_, read := f.call(t, cred, "READ", map[string]any{"description": description, "offset": 0, "count": 3})
	if read["code"] != "OK" || read["data"] != "616263" {
		t.Fatal(read)
	}
	_, closed := f.call(t, cred, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	if closed["code"] != "OK" {
		t.Fatal(closed)
	}
	_, stale := f.call(t, cred, "READ", map[string]any{"description": description, "offset": 0, "count": 3})
	if stale["code"] != "Permission" {
		t.Fatal(stale)
	}
}

// truncateTimes is a TRUNCATE "times" argument that changes no timestamps.
var truncateTimes = map[string]any{"atime": nil, "mtime": nil, "ctime": nil, "atime_now": false, "mtime_now": false}

// TestDescriptionBackedTruncateSyncSurviveRenameAndUnlink pins r64: an open
// file keeps the object capability it had before rename/unlink, so TRUNCATE
// (ftruncate) and SYNC (fsync) carrying its description must succeed even
// though the object's recorded path no longer resolves. POSIX requires both.
func TestDescriptionBackedTruncateSyncSurviveRenameAndUnlink(t *testing.T) {
	for _, change := range []string{"RENAME", "UNLINK"} {
		t.Run(change, func(t *testing.T) {
			f := newBridge(t)
			f.hello(t)
			cred := f.cred()
			cred.FSUID = 0
			_, root := f.call(t, cred, "ROOT", map[string]any{})
			parent := capabilityAt(t, root, 0)
			flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
			_, created := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": "6f6c64", "mode": 384, "umask": 0, "flags": flags})
			object, description := capabilityAt(t, created, 0), capabilityAt(t, created, 1)
			if _, w := f.call(t, cred, "WRITE", map[string]any{"description": description, "offset": 0, "max_end": int64(1<<63 - 1), "data": "616263646566"}); w["code"] != "OK" {
				t.Fatal(w)
			}
			var changed map[string]any
			if change == "RENAME" {
				_, changed = f.call(t, cred, "RENAME", map[string]any{"source_parent": parent, "source_name": "6f6c64", "destination_parent": parent, "destination_name": "6e6577", "no_replace": false})
			} else {
				_, changed = f.call(t, cred, "UNLINK", map[string]any{"parent": parent, "name": "6f6c64"})
			}
			if changed["code"] != "OK" {
				t.Fatal(changed)
			}
			// Reuse the old name for another file: the path re-check now finds
			// a different target (Stale in every cut, where a vanished name may
			// surface as NotFound), and the description-backed calls below must
			// still reach the original object, not whatever owns the name now.
			if _, r := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": "6f6c64", "mode": 384, "umask": 0, "flags": flags}); r["code"] != "OK" {
				t.Fatal(r)
			}
			// Without a description the outdated path capability stays Stale.
			if _, r := f.call(t, cred, "TRUNCATE", map[string]any{"object": object, "description": nil, "size": 2, "times": truncateTimes}); r["code"] != "Stale" {
				t.Fatal("null-description truncate:", r)
			}
			if _, r := f.call(t, cred, "SYNC", map[string]any{"object": object, "description": nil, "scope": "FILE"}); r["code"] != "Stale" {
				t.Fatal("null-description sync:", r)
			}
			if _, r := f.call(t, cred, "TRUNCATE", map[string]any{"object": object, "description": description, "size": 2, "times": truncateTimes}); r["code"] != "OK" {
				t.Fatal("description truncate:", r)
			}
			if _, r := f.call(t, cred, "SYNC", map[string]any{"object": object, "description": description, "scope": "FILE"}); r["code"] != "OK" {
				t.Fatal("description sync:", r)
			}
			if _, r := f.call(t, cred, "READ", map[string]any{"description": description, "offset": 0, "count": 6}); r["code"] != "OK" || r["data"] != "6162" {
				t.Fatal("read after truncate:", r)
			}
		})
	}
}

// TestDescriptionBackedTruncateRejectsMismatchAndDeadTokens covers the r64
// negatives: an object that is not the description's object is Stale, and a
// released or forged object token is Permission.
func TestDescriptionBackedTruncateRejectsMismatchAndDeadTokens(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	cred.FSUID = 0
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	parent := capabilityAt(t, root, 0)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	_, a := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": "61", "mode": 384, "umask": 0, "flags": flags})
	_, b := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": "62", "mode": 384, "umask": 0, "flags": flags})
	objectA, descriptionA := capabilityAt(t, a, 0), capabilityAt(t, a, 1)
	objectB := capabilityAt(t, b, 0)
	truncate := func(object, description Capability) any {
		_, r := f.call(t, cred, "TRUNCATE", map[string]any{"object": object, "description": description, "size": 0, "times": truncateTimes})
		return r["code"]
	}
	if got := truncate(objectB, descriptionA); got != "Stale" {
		t.Fatal("mismatched object:", got)
	}
	forged := objectA
	forged.Token = digest([]byte("never-issued"))
	if got := truncate(forged, descriptionA); got != "Permission" {
		t.Fatal("forged object:", got)
	}
	if _, r := f.call(t, cred, "FORGET", map[string]any{"object": objectA, "count": 1}); r["code"] != "OK" {
		t.Fatal(r)
	}
	if got := truncate(objectA, descriptionA); got != "Permission" {
		t.Fatal("released object:", got)
	}
}

// descriptionIn returns the DESCRIPTION capability of a reply.
func descriptionIn(t *testing.T, reply map[string]any) Capability {
	t.Helper()
	caps, _ := reply["capabilities"].([]any)
	for i := range caps {
		if c := capabilityAt(t, reply, i); c.Kind == "DESCRIPTION" {
			return c
		}
	}
	t.Fatal("no description in", reply)
	return Capability{}
}

// TestDescriptionBackedCallsRefuseWeakOrDeadDescriptions: once the
// description is the sole authority for TRUNCATE/SYNC (r64), a read-only
// description cannot truncate and a released description authorizes nothing.
func TestDescriptionBackedCallsRefuseWeakOrDeadDescriptions(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	cred.FSUID = 0
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	parent := capabilityAt(t, root, 0)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	_, created := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": "61", "mode": 384, "umask": 0, "flags": flags})
	object, description := capabilityAt(t, created, 0), capabilityAt(t, created, 1)
	readOnly := map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}
	_, opened := f.call(t, cred, "OPEN", map[string]any{"object": object, "flags": readOnly})
	if opened["code"] != "OK" {
		t.Fatal(opened)
	}
	reader := descriptionIn(t, opened)
	if reader.Rights&2 != 0 {
		t.Fatal("read-only open issued write rights:", reader.Rights)
	}
	if _, r := f.call(t, cred, "TRUNCATE", map[string]any{"object": object, "description": reader, "size": 0, "times": truncateTimes}); r["code"] != "Permission" {
		t.Fatal("read-only truncate:", r)
	}
	if _, r := f.call(t, cred, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1}); r["code"] != "OK" {
		t.Fatal(r)
	}
	if _, r := f.call(t, cred, "TRUNCATE", map[string]any{"object": object, "description": description, "size": 0, "times": truncateTimes}); r["code"] != "Permission" {
		t.Fatal("released truncate:", r)
	}
	if _, r := f.call(t, cred, "SYNC", map[string]any{"object": object, "description": description, "scope": "FILE"}); r["code"] != "Permission" {
		t.Fatal("released sync:", r)
	}
}

// TestPathAuthorityExemptsOnlyDescribedTruncateSync pins the defence in
// depth: even if the schema let OPEN, SETATTR or LINK carry a description,
// their object would still be path-revalidated.
func TestPathAuthorityExemptsOnlyDescribedTruncateSync(t *testing.T) {
	for _, op := range []string{"TRUNCATE", "SYNC"} {
		if pathAuthority(op, "object", true) || !pathAuthority(op, "object", false) {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"OPEN", "SETATTR", "LINK", "GETATTR", "READDIR"} {
		if !pathAuthority(op, "object", true) {
			t.Fatal(op, "skipped the path re-check")
		}
	}
	if pathAuthority("FORGET", "object", false) {
		t.Fatal("FORGET")
	}
}

// TestDescriptionBackedCallsRefuseForeignSessionDescription: a genuine
// description issued to another session (scope) cannot back TRUNCATE/SYNC
// here, neither as issued nor re-scoped to this session.
func TestDescriptionBackedCallsRefuseForeignSessionDescription(t *testing.T) {
	f := newBridge(t)
	second := f.c
	second.Session = uid(150).String()
	second.GuestBoot = uid(151).String()
	second.GuestMount = uid(152).String()
	second.VM = uid(153).String()
	second.OriginNode = uid(21).String()
	second.OriginIncarnation = uid(154).String()
	second.OriginJournal = uid(155).String()
	f.driver.allowed = map[Context]bool{second: true}
	config := f.s.config
	config.Scopes = []Context{f.c, second}
	service, e := New(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	f.s = service
	first := f.c
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	f.hello(t)
	_, root := f.call(t, f.cred(), "ROOT", map[string]any{})
	_, own := f.call(t, f.cred(), "CREATE", map[string]any{"parent": capabilityAt(t, root, 0), "name": "61", "mode": 384, "umask": 0, "flags": flags})
	object := capabilityAt(t, own, 0)
	f.c = second
	f.hello(t)
	_, root = f.call(t, f.cred(), "ROOT", map[string]any{})
	_, foreign := f.call(t, f.cred(), "CREATE", map[string]any{"parent": capabilityAt(t, root, 0), "name": "62", "mode": 384, "umask": 0, "flags": flags})
	if foreign["code"] != "OK" {
		t.Fatal(foreign)
	}
	issued := capabilityAt(t, foreign, 1)
	rescoped := issued
	rescoped.Scope = first
	f.c = first
	for _, d := range []Capability{issued, rescoped} {
		if _, r := f.call(t, f.cred(), "TRUNCATE", map[string]any{"object": object, "description": d, "size": 0, "times": truncateTimes}); r["code"] != "Permission" {
			t.Fatal("foreign truncate:", r)
		}
		if _, r := f.call(t, f.cred(), "SYNC", map[string]any{"object": object, "description": d, "scope": "FILE"}); r["code"] != "Permission" {
			t.Fatal("foreign sync:", r)
		}
	}
}

func TestDelegatedTwoIndependentlyWritableGuestDomains(t *testing.T) {
	f := newBridge(t)
	second := f.c
	second.Session = uid(150).String()
	second.GuestBoot = uid(151).String()
	second.GuestMount = uid(152).String()
	second.VM = uid(153).String()
	second.OriginNode = uid(21).String()
	second.OriginIncarnation = uid(154).String()
	second.OriginJournal = uid(155).String()
	f.driver.allowed = map[Context]bool{second: true}
	config := f.s.config
	config.Scopes = []Context{f.c, second}
	service, e := New(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	f.s = service
	first := f.c
	f.hello(t)
	_, root := f.call(t, f.cred(), "ROOT", map[string]any{})
	firstCap := capabilityAt(t, root, 0)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	_, a := f.call(t, f.cred(), "CREATE", map[string]any{"parent": firstCap, "name": "61", "mode": 384, "umask": 0, "flags": flags})
	if a["code"] != "OK" {
		t.Fatal(a)
	}
	f.c = second
	f.hello(t)
	_, root = f.call(t, f.cred(), "ROOT", map[string]any{})
	secondCap := capabilityAt(t, root, 0)
	_, bad := f.call(t, f.cred(), "CREATE", map[string]any{"parent": firstCap, "name": "78", "mode": 384, "umask": 0, "flags": flags})
	if bad["code"] != "Permission" {
		t.Fatal(bad)
	}
	_, b := f.call(t, f.cred(), "CREATE", map[string]any{"parent": secondCap, "name": "62", "mode": 384, "umask": 0, "flags": flags})
	if b["code"] != "OK" {
		t.Fatal(b)
	}
	f.c = first
	_, seen := f.call(t, f.cred(), "LOOKUP", map[string]any{"parent": firstCap, "name": "62"})
	if seen["code"] != "OK" {
		t.Fatal(seen)
	}
}
func TestBridgeReleaseProgressWhileOrdinaryExecutionWaits(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	tr, e := NewTransport(f.s, transportBudget(t))
	if e != nil {
		t.Fatal(e)
	}
	defer tr.Close()
	hello := f.command(t, "HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0})
	sendNative(t, tr, f.native, hello, 0)
	snapshot := fetchNative(t, tr, f.native, 1)
	f.driver.block = make(chan struct{})
	defer close(f.driver.block)
	intent := Intent{Opcode: "ROOT", Args: mustJSON(t, map[string]any{})}
	claim := Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 2, OriginalHash: digest(mustJSON(t, intent))}
	sendNative(t, tr, f.native, f.command(t, "CALL", Call{Claim: claim, Credentials: f.cred(), Intent: intent}), 0)
	ack := &Ack{Cell: 1, ResponseHash: digest(snapshot)}
	release := f.command(t, "RELEASE_CELL", ack)
	sendNative(t, tr, f.native, release, 2)
	_ = fetchNative(t, tr, f.native, 3)
}
func TestDelegatedConstructionProfilesAndMissingProviders(t *testing.T) {
	f := newBridge(t)
	config := f.s.config
	config.Planner = nil
	if _, e := New(t.Context(), config); e != nil {
		t.Fatal("controlled profile depends on normal planner", e)
	}
	config.Validation = nil
	if _, e := New(t.Context(), config); !errors.Is(e, fsapi.Invalid) {
		t.Fatal("normal profile lacks required planner", e)
	}
	config = f.s.config
	var native *localNative
	config.Native = native
	if _, e := New(t.Context(), config); !errors.Is(e, fsapi.Invalid) {
		t.Fatal("typed nil provider admitted", e)
	}
}

func TestBridgePublicationBaselineCannotNameSelectedResult(t *testing.T) {
	f := newBridge(t)
	publication := map[string]any{"authority_hash": f.c.AuthorityHash, "sequencer_node": uid(20).String(), "sequencer_incarnation": uid(23).String(), "serial": 0, "selection_hash": digest([]byte("baseline")), "result_ref": map[string]any{"cluster": f.c.Cluster, "volume": f.c.Volume, "generation": f.c.Generation, "configuration": f.c.Configuration, "epoch": 1, "origin_node": f.c.OriginNode, "journal_incarnation": f.c.OriginJournal, "sequence": 1, "frame_hash": digest([]byte("result")), "creator_node": f.c.OriginNode, "creator_incarnation": f.c.OriginIncarnation, "kind": record.NormalResultKind}, "source_manifest_hash": digest([]byte("sources")), "visibility_generation": uid(30).String()}
	value, e := Canonical(mustJSON(t, publication))
	if e != nil {
		t.Fatal(e)
	}
	if e = validatePublication(value); !errors.Is(e, fsapi.Invalid) {
		t.Fatal(e)
	}
}

type enrolledNative struct {
	mu       sync.Mutex
	expected Native
	owners   map[Endpoint]fsapi.UUID
}
type resumeTrap struct {
	localViews
	calls int
}

func (n *enrolledNative) Enroll(_ context.Context, e Endpoint, id fsapi.UUID) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, used := n.owners[e]; used {
		return fsapi.Busy
	}
	n.owners[e] = id
	return nil
}
func (n *enrolledNative) Verify(ctx context.Context, v Native, id fsapi.UUID) error {
	n.mu.Lock()
	owner := n.owners[v.Endpoint]
	n.mu.Unlock()
	if owner != id {
		return fsapi.Permission
	}
	return (localNative{expected: n.expected}).Verify(ctx, v, id)
}
func (v *resumeTrap) Exchange(context.Context, Context, string, json.RawMessage) (json.RawMessage, error) {
	v.calls++
	return marshal(map[string]any{"status": "RECONCILED", "ledger_hash": digest([]byte("fake"))})
}
func TestBridgeEndpointEnrollmentAndUnrestoredResumeStayClosed(t *testing.T) {
	f := newBridge(t)
	config := f.s.config
	verifier := &enrolledNative{expected: f.native, owners: map[Endpoint]fsapi.UUID{}}
	config.Native = verifier
	trap := &resumeTrap{}
	config.Views = trap
	s, e := New(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = New(t.Context(), config); !errors.Is(e, fsapi.Busy) {
		t.Fatal("issued endpoint recreated", e)
	}
	f.s = s
	f.hello(t)
	command := f.command(t, "RESUME", map[string]any{"previous_session": uid(200).String(), "ledger_hash": digest([]byte("missing ledger bytes")), "claims_hash": digest([]byte("claims")), "domains_hash": digest([]byte("domains"))})
	raw, e := s.Execute(t.Context(), f.native, command)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(raw, []byte(`"status":"BLOCKED"`)) || trap.calls != 0 {
		t.Fatal("opaque callback supplied continuity", string(raw), trap.calls)
	}
	if e = verifier.Verify(t.Context(), f.native, uid(250)); !errors.Is(e, fsapi.Permission) {
		t.Fatal("wrong controller accepted", e)
	}
}

func TestBridgeViewControlsProgressWhileDrainExecutionWaits(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	_, root := f.call(t, f.cred(), "ROOT", map[string]any{})
	domain := capabilityAt(t, root, 1)
	f.next = 0
	tr, e := NewTransport(f.s, transportBudget(t))
	if e != nil {
		t.Fatal(e)
	}
	defer tr.Close()
	f.driver.block = make(chan struct{})
	f.driver.entered = make(chan struct{}, 1)
	defer close(f.driver.block)
	desc := Capability{Token: digest([]byte("unknown description")), Kind: "DESCRIPTION", Scope: f.c, Home: f.c.Volume, Allocation: uid(3).String(), Description: ptr(uid(100).String()), Rights: 2, Revision: 1, CredentialHash: digest([]byte("cred")), AncestorsHash: digest([]byte("path"))}
	intent := Intent{Opcode: "DRAIN_WRITE", Args: mustJSON(t, map[string]any{"description": desc, "offset": 0, "data": "ab", "recall": uid(101).String(), "dirty_entry": digest([]byte("dirty")), "reservation": digest([]byte("reservation"))})}
	claim := Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 999, OriginalHash: digest(mustJSON(t, intent))}
	sendNative(t, tr, f.native, f.command(t, "CALL", Call{Claim: claim, Credentials: f.cred(), Intent: intent}), 1)
	select {
	case <-f.driver.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("drain execution did not start")
	}
	sendNative(t, tr, f.native, f.command(t, "VIEW_POLL", map[string]any{"domain": domain, "after_sequence": 0}), 1)
	raw := fetchNativeAt(t, tr, f.native, 2, f.native.Endpoint.RecallNode)
	if !bytes.Contains(raw, []byte(`"status":"EMPTY"`)) {
		t.Fatal(string(raw))
	}
}

func TestWriteMaxEndWireAndRetry(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	cred.FSUID = 0
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	flags := map[string]any{"access": "READWRITE", "append": true, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	_, created := f.call(t, cred, "CREATE", map[string]any{"parent": capabilityAt(t, root, 0), "name": "626f756e646564", "mode": 384, "umask": 0, "flags": flags})
	desc := capabilityAt(t, created, 1)
	_, first := f.call(t, cred, "WRITE", map[string]any{"description": desc, "offset": 0, "data": "6162", "max_end": 100})
	if first["count"] != float64(2) {
		t.Fatal(first)
	}
	cmd, short := f.call(t, cred, "WRITE", map[string]any{"description": desc, "offset": 0, "data": "63646566", "max_end": 4})
	if short["code"] != "OK" || short["count"] != float64(2) {
		t.Fatal(short)
	}
	calls := f.driver.calls
	if _, err := f.s.Execute(t.Context(), f.native, cmd); err != nil || f.driver.calls != calls {
		t.Fatalf("lost retry reexecuted: %v", err)
	}
	_, blocked := f.call(t, cred, "WRITE", map[string]any{"description": desc, "offset": 0, "data": "6566", "max_end": 4})
	if blocked["code"] != "FileTooLarge" || blocked["count"] != float64(0) {
		t.Fatal(blocked)
	}
	_, read := f.call(t, cred, "READ", map[string]any{"description": desc, "offset": 0, "count": 16})
	if read["data"] != "61626364" {
		t.Fatal(read)
	}
	capValue, err := Canonical(mustJSON(t, desc))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateArgs("WRITE", map[string]any{"description": capValue, "offset": json.Number("0"), "data": "65", "max_end": json.Number("4")}); err != nil {
		t.Fatal(err)
	}
	for _, bound := range []any{json.Number("-1"), json.Number("9223372036854775808"), nil, "4"} {
		if err := validateArgs("WRITE", map[string]any{"description": capValue, "offset": json.Number("0"), "data": "65", "max_end": bound}); err == nil {
			t.Fatalf("accepted bound %v", bound)
		}
	}
}
