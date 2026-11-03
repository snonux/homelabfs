package controlledvalidation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
	bolt "go.etcd.io/bbolt"
)

type facts struct{ enrollment Enrollment }

func (f facts) Check(ctx context.Context, e Enrollment) error {
	a, _ := json.Marshal(f.enrollment)
	b, _ := json.Marshal(e)
	if !bytes.Equal(a, b) {
		return fsapi.Permission
	}
	return ctx.Err()
}

type fixture struct {
	t                     *testing.T
	config                fsservice.Config
	service               *fsservice.Service
	provider              *Provider
	pc                    Config
	next                  uint64
	roots                 []storage.Filesystem
	operator, host, guest ed25519.PrivateKey
}

func id(n byte) fsapi.UUID {
	var id fsapi.UUID
	id[0] = n
	id[6] = 0x40
	id[8] = 0x80
	id[15] = n
	return id
}
func (f *fixture) root() storage.Filesystem {
	f.t.Helper()
	dir := tempDirectory(f.t)
	if err := os.Chmod(dir, 0700); err != nil {
		f.t.Fatal(err)
	}
	root, err := vfs.NewOS(f.t.Context(), dir)
	if err != nil {
		f.t.Fatal(err)
	}
	f.roots = append(f.roots, root)
	f.t.Cleanup(func() { _ = root.Close() })
	return root
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t}
	root := fsapi.ObjectID{Home: id(1), Allocation: id(4)}
	permissions, err := fsservice.NewUnixPermissions(id(20))
	if err != nil {
		t.Fatal(err)
	}
	f.config = fsservice.Config{Stream: segment.Stream{Volume: id(1), Generation: id(2), Configuration: id(3), Epoch: 1, Kind: 1}, Storage: storage.Config{Volume: id(1), Root: root, OwnerUID: uint32(os.Getuid())}, Validation: &fsservice.ValidationProfile{Disposable: true, Name: "controlled-test"}, Permissions: permissions}
	f.config.Authority = record.Authority{Volume: id(1), Generation: id(2), Configuration: id(3), Epoch: 1, Owner: id(6), Partner: id(8), Members: []fsapi.UUID{id(6), id(8), id(9)}, CoordinatorTerm: 1, OwnerIncarnation: id(7), PartnerIncarnation: id(10)}
	f.config.Source = f.root()
	f.config.Reservations = f.root()
	f.workspace(id(30))
	dir := "."
	for _, part := range split(f.config.Stream.Directory()) {
		next := path.Join(dir, part)
		if err = f.config.Source.Mkdir(t.Context(), next, 0700); err != nil {
			t.Fatal(err)
		}
		if err = f.config.Source.SyncDir(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
	header := record.Header{Volume: id(1), Generation: id(2), Configuration: id(3), Epoch: 1, Operation: id(90), Incarnation: id(7)}
	writer, err := segment.NewWriter(t.Context(), f.config.Source, f.config.Stream, segment.Options{MaximumBytes: 16 << 20, Structural: func(uint64) record.Header { return header }, NewSegment: func() fsapi.UUID { return id(80) }})
	if err != nil {
		t.Fatal(err)
	}
	init := record.VolumeInit{Root: root, Initial: record.Metadata{Revision: 1, Type: 2, UID: 100, GID: 200, Mode: 02775, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: root}, RootLedger: record.LedgerEdit{Object: root, ResultingRevision: 1}, Authority: f.config.Authority, Authorization: record.Artifact{Kind: 6, ID: id(70), Length: 1}}
	receipt, err := writer.Append(t.Context(), record.Record{Header: header, Payload: init})
	if err != nil {
		t.Fatal(err)
	}
	if err = receipt.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	f.start()
	f.pc.Directory = tempDirectory(t)
	f.pc.Initialize = true
	if err = os.Chmod(f.pc.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	scope := guestbridge.Context{AuthorityHash: hash([]byte("authority")), Cluster: id(40).String(), Configuration: id(3).String(), Export: id(41).String(), ExporterIncarnation: id(42).String(), FuseSession: id(43).String(), Generation: id(2).String(), GuestBoot: id(44).String(), GuestMount: id(45).String(), OriginIncarnation: id(46).String(), OriginJournal: id(47).String(), OriginNode: id(48).String(), PolicyHash: hash([]byte("policy")), RegistryHash: hash([]byte("registry")), Session: id(49).String(), VM: id(50).String(), Volume: id(1).String()}
	f.pc.Authorization.Enrollment = Enrollment{Ledger: id(65), Profile: *f.config.Validation, Scope: scope, Endpoint: guestbridge.Endpoint{FuseSession: scope.FuseSession, CommandNode: 2, ResultNode: 3, RecallNode: 4}}
	f.enroll()
	f.open()
	t.Cleanup(func() {
		if f.provider != nil {
			_ = f.provider.Close()
		}
		if f.service != nil {
			_ = f.service.Close()
		}
	})
	return f
}
func split(s string) []string {
	var out []string
	start := 0
	for n := range s {
		if s[n] == '/' {
			out = append(out, s[start:n])
			start = n + 1
		}
	}
	return append(out, s[start:])
}
func (f *fixture) workspace(g fsapi.UUID) {
	f.config.Workspace = fsservice.Workspace{ID: g, Data: f.root(), Journal: f.root(), Progress: f.root()}
}
func (f *fixture) start() {
	f.t.Helper()
	s, err := fsservice.New(f.t.Context(), f.config)
	if err != nil {
		f.t.Fatal(err)
	}
	f.service = s
}
func (f *fixture) enroll() {
	f.t.Helper()
	if f.operator == nil {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			f.t.Fatal(err)
		}
		f.operator = key
		_, key, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			f.t.Fatal(err)
		}
		f.host = key
		_, key, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			f.t.Fatal(err)
		}
		f.guest = key
	}
	operator, op := f.operator.Public().(ed25519.PublicKey), f.operator
	host, h := f.host.Public().(ed25519.PublicKey), f.host
	guest, g := f.guest.Public().(ed25519.PublicKey), f.guest
	var err error
	e := &f.pc.Authorization.Enrollment
	e.HostKey = host
	e.GuestKey = guest
	e.ServingGeneration = f.service.FrontendInfo().Generation
	e.Journal, err = f.service.ControlledJournalInfo(f.t.Context(), e.Profile)
	if err != nil {
		f.t.Fatal(err)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		f.t.Fatal(err)
	}
	f.pc.Operator = operator
	f.pc.Authorization.OperatorSignature = ed25519.Sign(op, raw)
	f.pc.Authorization.HostSignature = ed25519.Sign(h, raw)
	f.pc.Authorization.GuestSignature = ed25519.Sign(g, raw)
	f.pc.Facts = facts{*e}
	f.pc.Service = f.service
}
func (f *fixture) open() {
	f.t.Helper()
	p, err := New(f.t.Context(), f.pc)
	if err != nil {
		f.t.Fatal(err)
	}
	f.provider = p
	f.pc.Initialize = false
}
func (f *fixture) binding(op string, args any) guestbridge.Binding {
	return f.newBinding(op, args, true)
}
func (f *fixture) newBinding(op string, args any, reserve bool) guestbridge.Binding {
	f.t.Helper()
	f.next++
	raw, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	intentRaw, err := json.Marshal(intent)
	if err != nil {
		f.t.Fatal(err)
	}
	var unordered any
	decoder := json.NewDecoder(bytes.NewReader(intentRaw))
	decoder.UseNumber()
	if err = decoder.Decode(&unordered); err != nil {
		f.t.Fatal(err)
	}
	intentRaw, err = json.Marshal(unordered)
	if err != nil {
		f.t.Fatal(err)
	}
	canonical, err := guestbridge.Canonical(intentRaw)
	if err != nil {
		f.t.Fatal(err)
	}
	intentRaw, err = json.Marshal(canonical)
	if err != nil {
		f.t.Fatal(err)
	}
	claim := guestbridge.Claim{Realm: id(60).String(), Subject: "616263", Anchor: id(61).String(), Nonce: id(62).String(), Number: f.next, OriginalHash: hash(intentRaw)}
	call := guestbridge.Call{Claim: claim, Credentials: guestbridge.Credentials{FSUID: 1000, FSGID: 1001, Groups: []uint32{200}, Process: id(63).String(), Revision: 1, IDMapHash: hash([]byte("map")), UserNamespace: id(64).String()}, Intent: intent}
	raw, err = canonicalCall(call)
	if err != nil {
		f.t.Fatal(err)
	}
	b := guestbridge.Binding{Context: f.pc.Authorization.Enrollment.Scope, Call: call, Canonical: raw}
	if err = f.provider.Allocate(f.t.Context(), b.Context, claim); err != nil {
		f.t.Fatal(err)
	}
	if !reserve {
		return b
	}
	state, err := f.provider.Reserve(f.t.Context(), b)
	if err != nil {
		f.t.Fatal(err)
	}
	return state.Binding
}
func command(f *fixture, op fsapi.Operation) fsapi.Command {
	return fsapi.Command{Mount: id(1), Generation: f.service.FrontendInfo().Generation, Operation: op}
}
func createCommand(f *fixture, name string) fsapi.Command {
	c := command(f, fsapi.Create)
	c.Parent = f.config.Storage.Root
	c.Name = name
	c.Mode = 0660
	c.Flags = fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}
	return c
}
func execute(f *fixture, b guestbridge.Binding, c fsapi.Command) (guestbridge.Outcome, error) {
	return f.provider.Execute(f.t.Context(), b, func(cut guestbridge.Captured) (guestbridge.Prepared, error) { return cut.Prepare(f.t.Context(), b, c) })
}
func protect(out guestbridge.Outcome) (json.RawMessage, json.RawMessage, error) {
	raw, err := json.Marshal(map[string]any{"id": hash([]byte("original-result")), "handle": out.Result.Handle, "object": out.Result.Attr, "publication": nil})
	return raw, json.RawMessage(`{"protected":"original-capability-snapshot"}`), err
}

func TestRealJournalCrashRecoveryOriginalPlan(t *testing.T) {
	f := newFixture(t)
	b := f.binding("CREATE", map[string]any{})
	c := createCommand(f, "original")
	crash := errors.New("crash after filesystem effect before terminal")
	f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return crash
		}
		return nil
	}
	calls := 0
	_, err := f.provider.ExecuteValidation(t.Context(), b, func(cut guestbridge.Captured) (guestbridge.Prepared, error) {
		calls++
		return cut.Prepare(t.Context(), b, c)
	}, protect)
	if !errors.Is(err, crash) {
		t.Fatalf("got %v", err)
	}
	var original []byte
	if err = f.provider.db.View(func(tx *bolt.Tx) error {
		entry, _, e := f.provider.load(tx, b.Call.Claim)
		original = bytes.Clone(entry.ProposedResult)
		if entry.Plan == nil || entry.Plan.Operation != b.Operation || entry.Plan.Mutation == nil || len(original) == 0 {
			t.Fatal("missing protected original evidence")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = f.provider.Close(); err != nil {
		t.Fatal(err)
	}
	f.provider = nil
	if err = f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	f.workspace(id(31))
	f.start()
	f.enroll()
	f.pc.Failpoint = nil
	f.open()
	out, ready, err := f.provider.RecoverOutcome(t.Context(), b)
	if err != nil || !ready || out.Result.Attr == nil {
		t.Fatalf("recovery ready=%v err=%v", ready, err)
	}
	if calls != 1 {
		t.Fatalf("fresh planning calls %d", calls)
	}
	state, err := f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil || state.Status != "TERMINAL" || !bytes.Equal(state.Result, original) {
		t.Fatalf("lost original terminal bytes: %s %v", state.Status, err)
	}
	state2, err := f.provider.Reserve(t.Context(), b)
	if err != nil || state2.Binding.Operation != b.Operation || !bytes.Equal(state2.Result, original) {
		t.Fatalf("retry changed original: %v", err)
	}
	count := 0
	reader, err := segment.OpenReader(t.Context(), f.config.Source, f.config.Stream, segment.RecoveryOptions{}, fsapi.Position{Volume: id(1), Generation: id(2), Configuration: id(3), Epoch: 1, Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	for {
		e, err := reader.Next(t.Context())
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if e.Record.Header.Operation == b.Operation {
			if _, ok := e.Record.Payload.(record.Mutation); ok {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("original mutation executed %d times", count)
	}
}

func TestAppendBoundUsesRealSerializedSize(t *testing.T) {
	f := newFixture(t)
	b := f.binding("CREATE", map[string]any{})
	c := createCommand(f, "append")
	c.Flags.Append = true
	created, err := execute(f, b, c)
	if err != nil {
		t.Fatal(err)
	}
	write := func(data string, maxEnd int64) guestbridge.Outcome {
		b := f.binding("WRITE", map[string]any{"max_end": maxEnd})
		c := command(f, fsapi.Write)
		c.Object = created.Result.Attr.Object
		c.Handle = created.Result.Handle
		c.Data = []byte(data)
		out, err := execute(f, b, c)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := write("abcd", 6); out.Result.Count != 4 {
		t.Fatalf("first %d", out.Result.Count)
	}
	if out := write("WXYZ", 6); out.Result.Count != 2 {
		t.Fatalf("short %d", out.Result.Count)
	}
	if out := write("x", 6); !errors.Is(out.DelegatedError, fsservice.ErrDelegatedFileTooLarge) || out.Result.Count != 0 {
		t.Fatalf("too large: %+v", out)
	}
	b = f.binding("READ", map[string]any{})
	c = command(f, fsapi.Read)
	c.Object = created.Result.Attr.Object
	c.Handle = created.Result.Handle
	c.Size = 64
	out, err := execute(f, b, c)
	if err != nil || string(out.Result.Data) != "abcdWX" {
		t.Fatalf("data %q err %v", out.Result.Data, err)
	}
}

func TestProtectedGapsAndExactRetirement(t *testing.T) {
	f := newFixture(t)
	b := f.binding("ROOT", map[string]any{})
	scope := b.Context
	raw, _ := json.Marshal(map[string]any{"realm": b.Call.Claim.Realm, "subject": b.Call.Claim.Subject, "anchor": b.Call.Claim.Anchor, "nonce": b.Call.Claim.Nonce, "through": 1, "result_hashes": []string{hash(nil)}})
	response, err := f.provider.Retire(t.Context(), scope, raw)
	if err != nil || !bytes.Contains(response, []byte("BLOCKED")) {
		t.Fatalf("unresolved floor %s %v", response, err)
	}
	proof, err := f.provider.ResolveGap(t.Context(), scope, b.Call.Claim)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(map[string]any{"realm": b.Call.Claim.Realm, "subject": b.Call.Claim.Subject, "anchor": b.Call.Claim.Anchor, "nonce": b.Call.Claim.Nonce, "through": 1, "result_hashes": []string{proof}})
	response, err = f.provider.Retire(t.Context(), scope, raw)
	if err != nil || !bytes.Contains(response, []byte("RETIRED")) {
		t.Fatalf("floor %s %v", response, err)
	}
	if err = f.provider.Close(); err != nil {
		t.Fatal(err)
	}
	f.provider = nil
	f.open()
	state, err := f.provider.Resolve(t.Context(), scope, b.Call.Claim)
	if err != nil || state.Status != "RETIRED_ID" {
		t.Fatalf("floor lost %s %v", state.Status, err)
	}
	if _, err = f.provider.Reserve(t.Context(), b); err == nil {
		t.Fatal("retired gap admitted")
	}
}

func TestNegativeBindingsEnrollmentAndLock(t *testing.T) {
	f := newFixture(t)
	b := f.binding("ROOT", map[string]any{})
	changed := b
	changed.Canonical = bytes.Clone(b.Canonical)
	changed.Canonical[0] = 'x'
	if _, err := f.provider.Reserve(t.Context(), changed); err == nil {
		t.Fatal("changed binding")
	}
	scope := b.Context
	scope.Session = id(99).String()
	if _, err := f.provider.Resolve(t.Context(), scope, b.Call.Claim); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("scope %v", err)
	}
	cfg := f.pc
	cfg.Authorization.OperatorSignature = bytes.Clone(cfg.Authorization.OperatorSignature)
	cfg.Authorization.OperatorSignature[0] ^= 1
	if _, err := New(t.Context(), cfg); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("signature %v", err)
	}
	if p, err := New(t.Context(), f.pc); err == nil {
		_ = p.Close()
		t.Fatal("concurrent ledger writer")
	}
}

func TestConcurrentDuplicateExecutesOnce(t *testing.T) {
	f := newFixture(t)
	b := f.binding("CREATE", map[string]any{})
	c := createCommand(f, "once")
	var wg sync.WaitGroup
	var mu sync.Mutex
	calls := 0
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := f.provider.Execute(t.Context(), b, func(cut guestbridge.Captured) (guestbridge.Prepared, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				return cut.Prepare(t.Context(), b, c)
			})
			if err != nil || out.Result.Attr == nil {
				t.Errorf("duplicate %v", err)
			}
		}()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("callbacks=%d", calls)
	}
}

func TestCancellationAndMissingEvidenceKeepGatesClosed(t *testing.T) {
	f := newFixture(t)
	b := f.binding("CREATE", map[string]any{})
	if _, err := f.provider.Cancel(t.Context(), b.Context, b.Call.Claim); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(f, b, createCommand(f, "cancelled")); err == nil {
		t.Fatal("cancelled no-effect claim executed")
	}
	b = f.binding("CREATE", map[string]any{})
	stopped := errors.New("stopped after plan")
	f.provider.config.Failpoint = func(name string) error {
		if name == "plan-protected" {
			return stopped
		}
		return nil
	}
	if _, err := execute(f, b, createCommand(f, "pending")); !errors.Is(err, stopped) {
		t.Fatalf("stop %v", err)
	}
	f.provider.config.Failpoint = nil
	state, err := f.provider.Cancel(t.Context(), b.Context, b.Call.Claim)
	if err != nil || state.Status != "PENDING" {
		t.Fatalf("pending cancellation %s %v", state.Status, err)
	}
	if _, err = f.provider.ResolveGap(t.Context(), b.Context, b.Call.Claim); err == nil {
		t.Fatal("pending gap resolved without evidence")
	}
	if _, ready, err := f.provider.RecoverOutcome(t.Context(), b); err == nil || ready {
		t.Fatalf("missing original UUID evidence accepted: %v %v", ready, err)
	}
	b = f.binding("ROOT", map[string]any{})
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = f.provider.Execute(canceled, b, func(guestbridge.Captured) (guestbridge.Prepared, error) {
		t.Fatal("cancelled request planned")
		return guestbridge.Prepared{}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled %v", err)
	}
	state, err = f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil || state.Status != "RESERVED" {
		t.Fatalf("cancellation erased claim %s %v", state.Status, err)
	}
}

func TestMissingOrReplacedLedgerCannotInitializeImplicitly(t *testing.T) {
	f := newFixture(t)
	cfg := f.pc
	cfg.Directory = tempDirectory(t)
	if err := os.Chmod(cfg.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(t.Context(), cfg); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("missing ledger reused counters: %v", err)
	}
	cfg.Initialize = true
	p, err := New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = New(t.Context(), cfg); err == nil {
		t.Fatal("explicit init overwrote ledger")
	}
}

func TestRealNamespaceMetadataAndOpenSemantics(t *testing.T) {
	f := newFixture(t)
	run := func(op string, c fsapi.Command) guestbridge.Outcome {
		t.Helper()
		out, err := execute(f, f.binding(op, map[string]any{}), c)
		if err != nil || out.Result.Code != fsapi.OK {
			t.Fatalf("%s result=%v err=%v", op, out.Result.Code, err)
		}
		return out
	}
	c := command(f, fsapi.Mkdir)
	c.Parent = f.config.Storage.Root
	c.Name = "dir"
	c.Mode = 0770
	directory := run("MKDIR", c).Result.Attr.Object
	c = createCommand(f, "file")
	created := run("CREATE", c)
	object := created.Result.Attr.Object
	b := f.binding("WRITE", map[string]any{"max_end": 64})
	c = command(f, fsapi.Write)
	c.Object = object
	c.Handle = created.Result.Handle
	c.Data = []byte("content")
	if out, err := execute(f, b, c); err != nil || out.Result.Count != 7 {
		t.Fatalf("write %v %v", out, err)
	}
	mode := uint16(0600)
	c = command(f, fsapi.Setattr)
	c.Object = object
	c.Attributes.Mode = &mode
	if out := run("SETATTR", c); out.Result.Attr.Mode != mode {
		t.Fatal("mode did not change")
	}
	c = command(f, fsapi.Truncate)
	c.Object = object
	c.Size = 2
	if out := run("TRUNCATE", c); out.Result.Attr.Size != 2 {
		t.Fatal("truncate did not run")
	}
	c = command(f, fsapi.Open)
	c.Object = object
	c.Flags = fsapi.OpenFlags{Access: fsapi.ReadWrite, Truncate: true}
	if out := run("OPEN", c); out.Result.Attr.Size != 0 || out.Result.Handle == 0 {
		t.Fatalf("O_TRUNC %+v", out.Result)
	}
	c = createCommand(f, "file")
	c.Flags.Exclusive = false
	existing := run("CREATE", c)
	if existing.Result.Attr.Object != object || existing.Result.Handle == 0 {
		t.Fatal("nonexclusive create did not open existing")
	}
	c = command(f, fsapi.LinkOp)
	c.Object = object
	c.Destination = f.config.Storage.Root
	c.DestinationMount = id(1)
	c.NewName = "alias"
	run("LINK", c)
	c = command(f, fsapi.RenameOp)
	c.Parent = f.config.Storage.Root
	c.Name = "alias"
	c.Destination = directory
	c.DestinationMount = id(1)
	c.NewName = "moved"
	run("RENAME", c)
	c = command(f, fsapi.Readdir)
	c.Object = directory
	c.Limit = 16
	listed := run("READDIR", c)
	if len(listed.Result.Entries) != 1 || listed.Result.Entries[0].Name != "moved" || len(listed.EntryAttributes) != 1 {
		t.Fatalf("readdir %+v", listed)
	}
	c = command(f, fsapi.Unlink)
	c.Parent = directory
	c.Name = "moved"
	run("UNLINK", c)
	c = command(f, fsapi.Rmdir)
	c.Parent = f.config.Storage.Root
	c.Name = "dir"
	run("RMDIR", c)
	c = command(f, fsapi.SymlinkOp)
	c.Parent = f.config.Storage.Root
	c.Name = "sym"
	c.Data = []byte("file")
	sym := run("SYMLINK", c).Result.Attr.Object
	c = command(f, fsapi.Readlink)
	c.Object = sym
	if out := run("READLINK", c); string(out.Result.Data) != "file" {
		t.Fatalf("readlink %q", out.Result.Data)
	}
	c = command(f, fsapi.Sync)
	c.Object = object
	c.Durability = fsapi.DirectoryDurability
	out, err := execute(f, f.binding("SYNC", map[string]any{}), c)
	if err != nil || out.Result.Code != fsapi.Invalid {
		t.Fatalf("invalid sync was predicted success %+v %v", out, err)
	}
}

func TestOldRecoveryCannotRollbackNewerProtectedSnapshot(t *testing.T) {
	f := newFixture(t)
	first := f.binding("CREATE", map[string]any{})
	crash := errors.New("crash")
	f.provider.config.Failpoint = func(name string) error {
		if name == "effect-before-outcome" {
			return crash
		}
		return nil
	}
	if _, err := f.provider.ExecuteValidation(t.Context(), first, func(cut guestbridge.Captured) (guestbridge.Prepared, error) {
		return cut.Prepare(t.Context(), first, createCommand(f, "first"))
	}, protect); !errors.Is(err, crash) {
		t.Fatal(err)
	}
	f.provider.config.Failpoint = nil
	secondAllocated := f.newBinding("CREATE", map[string]any{}, false)
	if _, err := f.provider.Reserve(t.Context(), secondAllocated); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("pending stage admitted newer snapshot: %v", err)
	}
	if _, ready, err := f.provider.RecoverOutcome(t.Context(), first); err != nil || !ready {
		t.Fatalf("recover before new admission: %v %v", ready, err)
	}
	admitted, err := f.provider.Reserve(t.Context(), secondAllocated)
	if err != nil {
		t.Fatal(err)
	}
	second := admitted.Binding
	newer := json.RawMessage(`{"protected":"newer-snapshot"}`)
	if _, err := f.provider.ExecuteValidation(t.Context(), second, func(cut guestbridge.Captured) (guestbridge.Prepared, error) {
		return cut.Prepare(t.Context(), second, createCommand(f, "second"))
	}, func(out guestbridge.Outcome) (json.RawMessage, json.RawMessage, error) {
		raw, _, err := protect(out)
		return raw, newer, err
	}); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := f.provider.RecoverOutcome(t.Context(), first); err != nil || !ready {
		t.Fatalf("recover %v %v", ready, err)
	}
	actual, err := f.provider.Restore(t.Context(), first.Context)
	if err != nil || !bytes.Equal(actual, newer) {
		t.Fatalf("snapshot rolled back %s %v", actual, err)
	}
}

func TestReservationCreditsPrecedeEffectsAndRetainRejectedGap(t *testing.T) {
	f := newFixture(t)
	f.provider.config.MaxSourceBytes = f.provider.config.SourceCreditBytes
	first := f.binding("ROOT", map[string]any{})
	second := f.newBinding("CREATE", map[string]any{}, false)
	if _, err := f.provider.Reserve(t.Context(), second); !errors.Is(err, fsapi.NoSpace) {
		t.Fatalf("unreserved source credits %v", err)
	}
	state, err := f.provider.Resolve(t.Context(), second.Context, second.Call.Claim)
	if err != nil || state.Status != "UNKNOWN" {
		t.Fatalf("rejected allocation lost %s %v", state.Status, err)
	}
	if _, err = f.provider.ResolveGap(t.Context(), second.Context, second.Call.Claim); err != nil {
		t.Fatal(err)
	}
	// Failed admission cannot decrement or recycle the allocated public counter.
	reused := second.Call.Claim
	reused.OriginalHash = first.Call.Claim.OriginalHash
	if err = f.provider.Allocate(t.Context(), second.Context, reused); err == nil {
		t.Fatal("reused allocated number")
	}
	if _, err = f.provider.Cancel(t.Context(), first.Context, first.Call.Claim); err != nil {
		t.Fatal(err)
	}
}

func tempDirectory(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp(os.Getenv("HFS_CONTROLLED_TEST_ROOT"), "hfs-controlled-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("preserved failed controlled resources: %s", directory)
			return
		}
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove owned successful fixture %s: %v", directory, err)
		}
	})
	return directory
}

func TestAdmittedCancellationUsesOwnedContextAndExactOriginalBinding(t *testing.T) {
	f := newFixture(t)
	b := f.binding("CREATE", map[string]any{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	out, err := f.provider.Execute(ctx, b, func(cut guestbridge.Captured) (guestbridge.Prepared, error) {
		cancel()
		return cut.Prepare(ctx, b, createCommand(f, "admitted"))
	})
	if err != nil || out.Result.Attr == nil || out.Result.Code != fsapi.OK {
		t.Fatalf("admitted cancellation changed execution: %+v %v", out, err)
	}
	changed := b
	changed.Call.Credentials.FSUID = 0
	if _, err = f.provider.Execute(t.Context(), changed, func(guestbridge.Captured) (guestbridge.Prepared, error) {
		t.Fatal("mutable call bypassed canonical original")
		return guestbridge.Prepared{}, nil
	}); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("modified original binding %v", err)
	}
	b = f.binding("CREATE", map[string]any{})
	out, err = f.provider.Execute(t.Context(), b, func(cut guestbridge.Captured) (guestbridge.Prepared, error) {
		wrong := b
		wrong.Call.Credentials.FSUID = 0
		return cut.Prepare(t.Context(), wrong, createCommand(f, "forged"))
	})
	if err != nil || out.Result.Code != fsapi.Permission {
		t.Fatalf("captured credential substitution %+v %v", out, err)
	}
}
