package normalpublic

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// testProofs deliberately supplies fixture-only external assertions. It is not
// shipped as a production retention, attestation or absence implementation.
type testProofs struct {
	expectedRecovery                                 []byte
	mu                                               sync.Mutex
	binding                                          Binding
	admissions                                       int
	denyProtection, denyGap, denyFloor, denyRecovery bool
}

var (
	_ Authentication = (*testProofs)(nil)
	_ Admission      = (*testProofs)(nil)
	_ Retention      = (*testProofs)(nil)
)

func (p *testProofs) Lookup(_ context.Context, s Scope, a Anchor) error {
	if !equal(s, p.binding.Scope) || !equal(a, p.binding.Claim.Anchor) {
		return ErrBlocked
	}
	return nil
}
func (p *testProofs) Allocation(_ context.Context, _ Scope, _ Anchor, previous uint64, allocations []Allocation, evidence []byte) error {
	if len(allocations) == 0 || allocations[0].Number != previous+1 || !bytes.Equal(evidence, []byte("authenticated serialized allocation")) {
		return ErrBlocked
	}
	return nil
}
func (p *testProofs) VerifyAdmission(_ context.Context, b Binding) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.admissions++
	if !equal(b, p.binding) {
		return ErrConflict
	}
	return nil
}
func (p *testProofs) VerifyProtection(_ context.Context, b Binding, proof Protection) error {
	if p.denyProtection || !equal(b, p.binding) || !hmac.Equal(proof.Evidence, sign(proof.Graph)) {
		return ErrBlocked
	}
	return nil
}
func (p *testProofs) VerifyGap(_ context.Context, _ Scope, _ Anchor, _ uint64, evidence []byte) error {
	if p.denyGap || !bytes.Equal(evidence, []byte("protected serialized no-effect")) {
		return ErrBlocked
	}
	return nil
}
func (p *testProofs) VerifyRetirement(_ context.Context, _ Scope, r Retirement) error {
	if p.denyFloor || !bytes.Equal(r.Evidence, []byte("protected permanent floor")) {
		return ErrBlocked
	}
	return nil
}
func (p *testProofs) VerifyRecovery(_ context.Context, _ record.UUID, data []byte) error {
	if p.denyRecovery || p.expectedRecovery != nil && !bytes.Equal(p.expectedRecovery, data) {
		return ErrBlocked
	}
	return nil
}

func sign(value any) []byte {
	data, _ := json.Marshal(value)
	h := hmac.New(sha256.New, []byte("fixture-only-retention-key"))
	_, _ = h.Write(data)
	return h.Sum(nil)
}

func id(n byte) record.UUID { return record.UUID{0: n, 15: n} }

func fixture(t testing.TB) (Binding, *testProofs, Config) {
	t.Helper()
	a := record.NormalAuthority{Cluster: id(1), Registry: id(2), RegistryRevision: 1, RecoveryGeneration: id(3), Closure: id(4), ManifestVersion: 1, ManifestSHA256: sha256.Sum256([]byte("closure")), Admitted: []record.UUID{id(0x44), id(0x66), id(0x88), id(0xaa)}, Volumes: []record.NormalVolume{{Volume: id(0x11), Generation: id(0x22), Configuration: id(0x33), Epoch: 7, Replicas: []record.UUID{id(0x44), id(0x66), id(0x88)}}}, Sequencer: record.Launch{Node: id(0x44), Incarnation: id(0x55)}, OwnerPartner: record.Launch{Node: id(0x66), Incarnation: id(0x77)}, FenceLineage: []record.Hash{sha256.Sum256([]byte("positive fence"))}}
	hash, err := record.NormalAuthorityHash(a)
	if err != nil {
		t.Fatal(err)
	}
	b := Binding{Claim: Claim{Anchor: Anchor{Realm: id(8), Subject: []byte("alice"), Anchor: id(9), Nonce: id(10)}, Number: 1}, Scope: Scope{Export: id(11), ExporterIncarnation: id(12), FuseSession: id(13), GuestBoot: id(14), GuestMount: id(15), Session: id(16), VM: id(17), Policy: sha256.Sum256([]byte("policy")), Registry: sha256.Sum256([]byte("registry")), Authority: hash}, Credentials: Credentials{FSUID: 1000, FSGID: 100, Groups: []uint32{20, 100, 200}, Process: id(18), UserNamespace: id(19), Revision: 7, IDMap: sha256.Sum256([]byte("all-ids")), Provenance: []byte("complete attested VFS/process/namespace provenance")}, Intent: []byte(`{"opcode":"WRITE","args":{"data":"YWJj","max_end":99}}`), Operation: id(20), Origin: record.OriginStream{Cluster: a.Cluster, Volume: id(0x11), Generation: id(0x22), Configuration: id(0x33), Epoch: 7, Origin: id(0xaa), Journal: id(0xab)}, OriginLaunch: record.Launch{Node: id(0xaa), Incarnation: id(0xac)}, Authority: a}
	b.Claim.Original = sha256.Sum256(b.Intent)
	p := &testProofs{binding: b}
	return b, p, Config{LedgerID: id(30), Authentication: p, Admission: p, Retention: p, Limits: Limits{Anchors: 8, Entries: 16, GraphFrames: 32, JournalBytes: 32 << 20}}
}

func protection(t testing.TB, b Binding, outcome uint8) Protection {
	t.Helper()
	var frames [][]byte
	add := func(f Frame) Ref {
		data, err := Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := Reference(data)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, data)
		return ref
	}
	f := Frame{Stream: b.Origin, Sequence: 1, Creator: b.OriginLaunch, Operation: b.Operation, Kind: BindingKind, Binding: &b}
	br := add(f)
	f.Sequence = 2
	f.Kind = ChoiceKind
	f.Binding = nil
	f.Choice = &Choice{Binding: br, Outcome: outcome}
	cr := add(f)
	f.Sequence = 1
	f.Kind = SelectionKind
	f.Choice = nil
	f.Creator = b.Authority.Sequencer
	f.Stream.Origin = f.Creator.Node
	f.Stream.Journal = id(0xb0)
	f.Selection = &Selection{Choice: cr, Slot: record.OrderedSlot{Authority: b.Scope.Authority, Sequencer: b.Authority.Sequencer, Serial: 1}, Baseline: sha256.Sum256([]byte("verified baseline"))}
	sr := add(f)
	f.Stream = b.Origin
	f.Creator = b.OriginLaunch
	f.Sequence = 3
	f.Kind = ResultKind
	f.Selection = nil
	f.Result = &Result{Selection: sr, Bytes: []byte(`{"code":"OK","count":3}`)}
	if outcome == 2 {
		f.Result.Bytes = []byte(`{"code":"Interrupted","count":0}`)
	}
	rr := add(f)
	g := Graph{Root: rr, Frames: frames}
	return Protection{Graph: g, Evidence: sign(g)}
}

func openFixture(t testing.TB, path string, c Config) *Ledger {
	t.Helper()
	l, err := Open(context.Background(), path, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	return l
}

func allocate(t testing.TB, l *Ledger, b Binding, through uint64) {
	t.Helper()
	if err := l.Allocate(context.Background(), b.Scope, b.Claim.Anchor, allocated(b, through), []byte("authenticated serialized allocation")); err != nil {
		t.Fatal(err)
	}
}

func reserve(t testing.TB, l *Ledger, b Binding) {
	t.Helper()
	entry, err := l.Reserve(context.Background(), b)
	if err != nil || entry.Status != Pending {
		t.Fatalf("reserve %s: %v", entry.Status, err)
	}
}

func TestDurableOriginalTerminalGapAndFloor(t *testing.T) {
	ctx := context.Background()
	b, p, c := fixture(t)
	path := filepath.Join(runDir(t), "claims")
	l := openFixture(t, path, c)
	entry, err := l.Resolve(ctx, b.Scope, b.Claim)
	if err != nil || entry.Status != Unknown {
		t.Fatal(entry, err)
	}
	if _, err := l.Reserve(ctx, b); !errors.Is(err, ErrBlocked) {
		t.Fatal("unallocated number admitted", err)
	}
	allocate(t, l, b, 2)
	reserve(t, l, b)
	if _, err := l.Cancel(ctx, b.Scope, b.Claim); err != nil {
		t.Fatal(err)
	}
	if _, err := l.MarkIndeterminate(ctx, b.Scope, b.Claim); err != nil {
		t.Fatal(err)
	}
	entry, err = l.Resolve(ctx, b.Scope, b.Claim)
	if err != nil || entry.Status != CancelRequested {
		t.Fatal(entry, err)
	}
	if err := l.Retire(ctx, b.Scope, b.Claim.Anchor, 2, []byte("protected permanent floor")); !errors.Is(err, ErrBlocked) {
		t.Fatal("pending retired", err)
	}
	proof := protection(t, b, 1)
	p.denyProtection = true
	if _, err := l.Protect(ctx, b.Scope, b.Claim, proof); !errors.Is(err, ErrBlocked) {
		t.Fatal("retention not required", err)
	}
	p.denyProtection = false
	if _, err := l.Protect(ctx, b.Scope, b.Claim, proof); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = openFixture(t, path, c)
	entry, err = l.Reserve(ctx, b)
	if err != nil || entry.Status != Terminal || p.admissions != 1 {
		t.Fatalf("known retry performed fresh admission: %s %d %v", entry.Status, p.admissions, err)
	}
	entry.Result[0] ^= 1
	entry.Binding.Credentials.Groups[0]++
	got, err := l.Resolve(ctx, b.Scope, b.Claim)
	if err != nil || !bytes.Equal(got.Result, []byte(`{"code":"OK","count":3}`)) || got.Binding.Credentials.Groups[0] != 20 {
		t.Fatal("query aliases retained bytes", err)
	}
	if err := l.Retire(ctx, b.Scope, b.Claim.Anchor, 2, []byte("protected permanent floor")); !errors.Is(err, ErrBlocked) {
		t.Fatal("missing gap skipped", err)
	}
	p.denyGap = true
	if _, err := l.ProtectGap(ctx, b.Scope, b.Claim.Anchor, 2, []byte("protected serialized no-effect")); !errors.Is(err, ErrBlocked) {
		t.Fatal("gap proof ignored", err)
	}
	p.denyGap = false
	if _, err := l.ProtectGap(ctx, b.Scope, b.Claim.Anchor, 2, []byte("protected serialized no-effect")); err != nil {
		t.Fatal(err)
	}
	p.denyFloor = true
	if err := l.Retire(ctx, b.Scope, b.Claim.Anchor, 2, []byte("protected permanent floor")); !errors.Is(err, ErrBlocked) {
		t.Fatal("floor proof ignored", err)
	}
	p.denyFloor = false
	if err := l.Retire(ctx, b.Scope, b.Claim.Anchor, 2, []byte("protected permanent floor")); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = openFixture(t, path, c)
	for _, n := range []uint64{1, 2} {
		claim := b.Claim
		claim.Number = n
		if _, err := l.Resolve(ctx, b.Scope, claim); !errors.Is(err, ErrRetired) {
			t.Fatal("floor lost", n, err)
		}
	}
	if _, err := l.Reserve(ctx, b); !errors.Is(err, ErrRetired) {
		t.Fatal("delayed original reused", err)
	}
	if err := l.Allocate(ctx, b.Scope, b.Claim.Anchor, allocated(b, 1), []byte("authenticated serialized allocation")); !errors.Is(err, ErrBlocked) {
		t.Fatal("anchor allocation reset", err)
	}
}

func TestImmutableOriginalAndLookupScope(t *testing.T) {
	ctx := context.Background()
	b, _, c := fixture(t)
	l := openFixture(t, filepath.Join(runDir(t), "claims"), c)
	allocate(t, l, b, 1)
	reserve(t, l, b)
	for _, tc := range []struct {
		name   string
		mutate func(*Binding)
	}{
		{"principal", func(b *Binding) { b.Claim.Anchor.Subject = []byte("mallory") }},
		{"realm", func(b *Binding) { b.Claim.Anchor.Realm = id(99) }},
		{"origin", func(b *Binding) { b.Origin.Origin = id(0x88); b.OriginLaunch.Node = id(0x88) }},
		{"operation", func(b *Binding) { b.Operation = id(90) }},
		{"credentials", func(b *Binding) { b.Credentials.Groups[0]++ }},
		{"provenance", func(b *Binding) { b.Credentials.Provenance = []byte("changed") }},
		{"intent", func(b *Binding) { b.Intent = []byte("changed"); b.Claim.Original = sha256.Sum256(b.Intent) }},
		{"scope", func(b *Binding) { b.Scope.Session = id(91) }},
		{"authority", func(b *Binding) { b.Authority.Closure = id(92) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := clone(b)
			tc.mutate(&changed)
			if _, err := l.Reserve(ctx, changed); err == nil {
				t.Fatal("changed original accepted")
			}
		})
	}
	wrong := b.Scope
	wrong.Session = id(98)
	if _, err := l.Resolve(ctx, wrong, b.Claim); err == nil {
		t.Fatal("unauthenticated old lookup")
	}
	claim := b.Claim
	claim.Original[0] ^= 1
	for _, fn := range []func() error{func() error { _, err := l.Cancel(ctx, b.Scope, claim); return err }, func() error { _, err := l.MarkIndeterminate(ctx, b.Scope, claim); return err }, func() error { _, err := l.Protect(ctx, b.Scope, claim, protection(t, b, 1)); return err }} {
		if !errors.Is(fn(), ErrConflict) {
			t.Fatal("different claim intent accepted")
		}
	}
}

func TestUnknownCancelAndProtectedDecline(t *testing.T) {
	ctx := context.Background()
	b, _, c := fixture(t)
	l := openFixture(t, filepath.Join(runDir(t), "claims"), c)
	if _, err := l.Cancel(ctx, b.Scope, b.Claim); !errors.Is(err, ErrBlocked) {
		t.Fatal("unknown cancel invented effect state", err)
	}
	allocate(t, l, b, 2)
	reserve(t, l, b)
	if _, err := l.ProtectGap(ctx, b.Scope, b.Claim.Anchor, 1, []byte("protected serialized no-effect")); !errors.Is(err, ErrConflict) {
		t.Fatal("admitted choice erased by gap", err)
	}
	if _, err := l.MarkIndeterminate(ctx, b.Scope, b.Claim); err != nil {
		t.Fatal(err)
	}
	if err := l.Retire(ctx, b.Scope, b.Claim.Anchor, 1, []byte("protected permanent floor")); !errors.Is(err, ErrBlocked) {
		t.Fatal("unknown retired", err)
	}
	entry, err := l.Protect(ctx, b.Scope, b.Claim, protection(t, b, 2))
	if err != nil || entry.Status != Terminal || !bytes.Contains(entry.Result, []byte("Interrupted")) {
		t.Fatal(entry, err)
	}
	if _, err := l.Protect(ctx, b.Scope, b.Claim, protection(t, b, 1)); !errors.Is(err, ErrConflict) {
		t.Fatal("second terminal choice accepted", err)
	}
	if _, err := l.Cancel(ctx, b.Scope, b.Claim); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDuplicateReservation(t *testing.T) {
	ctx := context.Background()
	b, p, c := fixture(t)
	l := openFixture(t, filepath.Join(runDir(t), "claims"), c)
	allocate(t, l, b, 1)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Go(func() { _, err := l.Reserve(ctx, b); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if p.admissions != 1 {
		t.Fatal("duplicate admitted twice", p.admissions)
	}
}

func TestCrashProcess(t *testing.T) {
	if path := os.Getenv("HFS_PUBLIC_CRASH_JOURNAL"); path != "" {
		b, _, c := fixture(t)
		l, err := Open(context.Background(), path, c)
		if err != nil {
			t.Fatal(err)
		}
		allocate(t, l, b, 2)
		reserve(t, l, b)
		if _, err := l.Protect(context.Background(), b.Scope, b.Claim, protection(t, b, 1)); err != nil {
			t.Fatal(err)
		}
		// Exit without Close/deferred cleanup. The parent reopens actual bytes.
		os.Exit(0)
	}
	path := filepath.Join(runDir(t), "claims")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashProcess$")
	cmd.Env = append(os.Environ(), "HFS_PUBLIC_CRASH_JOURNAL="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("crash helper: %v %s", err, out)
	}
	b, _, c := fixture(t)
	l := openFixture(t, path, c)
	entry, err := l.Resolve(context.Background(), b.Scope, b.Claim)
	if err != nil || entry.Status != Terminal {
		t.Fatal("crash lost terminal", entry, err)
	}
	if err := l.Retire(context.Background(), b.Scope, b.Claim.Anchor, 2, []byte("protected permanent floor")); !errors.Is(err, ErrBlocked) {
		t.Fatal("crash dropped unsent gap", err)
	}
}

func TestJournalOwnershipTornTailRecoveryAndPoison(t *testing.T) {
	ctx := context.Background()
	b, p, c := fixture(t)
	path := filepath.Join(runDir(t), "claims")
	l := openFixture(t, path, c)
	allocate(t, l, b, 1)
	reserve(t, l, b)
	if other, err := Open(ctx, path, c); err == nil {
		_ = other.Close()
		t.Fatal("second writer admitted")
	}
	if err := l.journal.file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Cancel(ctx, b.Scope, b.Claim); err == nil {
		t.Fatal("closed file append succeeded")
	}
	if _, err := l.Resolve(ctx, b.Scope, b.Claim); err == nil {
		t.Fatal("ambiguous I/O handle remained usable")
	}
	l.journal.file = nil
	l = openFixture(t, path, c)
	entry, err := l.Resolve(ctx, b.Scope, b.Claim)
	if err != nil || entry.Status != Pending {
		t.Fatal("failed append changed recovered state", entry, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	p.denyRecovery = true
	if opened, err := Open(ctx, path, c); err == nil {
		_ = opened.Close()
		t.Fatal("missing recovery proof accepted")
	}
	p.denyRecovery = false
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(before, []byte("HFSPLOG1torn")...), 0600); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(ctx, path, c); err == nil {
		_ = opened.Close()
		t.Fatal("torn tail silently repaired")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, append(before, []byte("HFSPLOG1torn")...)) {
		t.Fatal("failed resources altered")
	}
}

func allocated(b Binding, through uint64) []Allocation {
	var result []Allocation
	for n := uint64(1); n <= through; n++ {
		result = append(result, Allocation{Number: n, Original: b.Claim.Original})
	}
	return result
}

func TestUnsentRejectedAndRetiredIdentitiesCannotBeReused(t *testing.T) {
	ctx := context.Background()
	b, p, c := fixture(t)
	l := openFixture(t, filepath.Join(runDir(t), "claims"), c)
	allocate(t, l, b, 1)
	rejected := clone(b)
	rejected.Credentials.FSUID++
	if _, err := l.Reserve(ctx, rejected); !errors.Is(err, ErrConflict) {
		t.Fatal("admission accepted changed credentials", err)
	}
	changed := clone(b)
	changed.Intent = []byte("a different otherwise admitted intent")
	changed.Claim.Original = sha256.Sum256(changed.Intent)
	p.binding = changed
	if _, err := l.Reserve(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("rejected allocated number reused", err)
	}
	if _, err := l.Resolve(ctx, b.Scope, changed.Claim); !errors.Is(err, ErrConflict) {
		t.Fatal("unsent original digest not retained", err)
	}
	p.binding = b
	reserve(t, l, b)
	if _, err := l.Protect(ctx, b.Scope, b.Claim, protection(t, b, 1)); err != nil {
		t.Fatal(err)
	}
	if err := l.Retire(ctx, b.Scope, b.Claim.Anchor, 1, []byte("protected permanent floor")); err != nil {
		t.Fatal(err)
	}
	if err := l.Allocate(ctx, b.Scope, b.Claim.Anchor, []Allocation{{Number: 2, Original: b.Claim.Original}}, []byte("authenticated serialized allocation")); err != nil {
		t.Fatal(err)
	}
	reused := clone(b)
	reused.Claim.Number = 2
	p.binding = reused
	if _, err := l.Reserve(ctx, reused); !errors.Is(err, ErrConflict) {
		t.Fatal("retired operation UUID reused", err)
	}
}

func TestRecoveryCannotInferAbsenceFromCompleteShorterJournal(t *testing.T) {
	ctx := context.Background()
	b, p, c := fixture(t)
	path := filepath.Join(runDir(t), "claims")
	l := openFixture(t, path, c)
	allocate(t, l, b, 1)
	shorter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reserve(t, l, b)
	if _, err := l.Protect(ctx, b.Scope, b.Claim, protection(t, b, 1)); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	p.expectedRecovery, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	shortPath := filepath.Join(runDir(t), "complete-shorter-claims")
	if err := os.WriteFile(shortPath, shorter, 0600); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(ctx, shortPath, c); err == nil {
		_ = opened.Close()
		t.Fatal("valid shorter source was treated as absence")
	}
	wrong := c
	wrong.LedgerID = id(99)
	if opened, err := Open(ctx, path, wrong); err == nil {
		_ = opened.Close()
		t.Fatal("journal accepted under another enrolled ledger")
	}
	l = openFixture(t, path, c)
	entry, err := l.Resolve(ctx, b.Scope, b.Claim)
	if err != nil || entry.Status != Terminal {
		t.Fatal(entry, err)
	}
}
