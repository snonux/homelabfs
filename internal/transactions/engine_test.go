package transactions

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/vfs"
)

type testProtector struct {
	mu              sync.Mutex
	fixture         *fixture
	retained        map[key][]byte
	accepted        map[key]segment.Entry
	fail, authority error
	mutate          func(*ProtectionReceipt)
	writers         map[record.UUID]*segment.Writer
}

type testApply struct {
	fs   *vfs.Simulator
	fail bool
}

type fixture struct {
	t                            *testing.T
	fs                           *vfs.Simulator
	options                      Options
	protector                    *testProtector
	apply                        *testApply
	engine                       *Engine
	id                           record.RequestID
	document                     record.RequestDocument
	digest                       record.Hash
	binding                      record.Binding
	claim, bind, begin, decision segment.Entry
	prepares, installs           []segment.Entry
	complete                     segment.Entry
	counter                      uint64
	data                         []byte
	revision                     uint64
}

var _ Protector = (*testProtector)(nil)
var _ Applier = (*testApply)(nil)

func newFixture(t *testing.T) *fixture {
	return newFixtureWithLoss(t, vfs.Lose)
}

func newFixtureWithLoss(t *testing.T, loss vfs.LossMode) *fixture {
	t.Helper()
	sim, err := vfs.NewSimulator(vfs.Config{Seed: 1, DataLoss: loss})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, fs: sim, id: record.RequestID{Session: fsapi.SessionID{Principal: record.Principal{Realm: uuid(30), Subject: "user"}, Anchor: uuid(1), Nonce: uuid(31)}, Number: 1}}
	f.apply = &testApply{fs: sim}
	f.protector = &testProtector{fixture: f, retained: make(map[key][]byte), accepted: make(map[key]segment.Entry), writers: make(map[record.UUID]*segment.Writer)}
	f.options = Options{Protector: f.protector, Applier: f.apply, Header: f.header}
	for _, volume := range []record.UUID{uuid(1), uuid(2)} {
		a := record.Authority{Volume: volume, Generation: uuid(3), Configuration: uuid(4), Epoch: 1, Owner: uuid(10), Partner: uuid(11), Members: []record.UUID{uuid(10), uuid(11), uuid(12)}, CoordinatorTerm: 1, OwnerIncarnation: uuid(20), PartnerIncarnation: uuid(21)}
		stream := segment.Stream{Volume: volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Kind: 1}
		mkdir(t, sim, stream.Directory())
		writer := segment.Options{MaximumBytes: 1 << 20, NewSegment: f.nextID, Structural: func(uint64) record.Header { h := f.header(); h.Incarnation = uuid(20); return h }}
		f.options.Groups = append(f.options.Groups, Group{Backend: sim, Stream: stream, Writer: writer, Binding: Binding{Session: uuid(40), Connection: uuid(41), Authority: a}})
	}
	f.engine, err = Open(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f.engine != nil {
			_ = f.engine.Close()
		}
		for _, w := range f.protector.writers {
			_ = w.Close()
		}
	})
	return f
}

func (f *fixture) header() record.Header {
	return record.Header{Operation: f.nextID(), Incarnation: uuid(20)}
}
func (f *fixture) nextID() record.UUID {
	f.counter++
	return record.UUID{0: 90, 8: byte(f.counter >> 8), 15: byte(f.counter)}
}
func uuid(n byte) record.UUID { return record.UUID{0: n, 15: n} }
func mkdir(t *testing.T, b segment.Backend, name string) {
	t.Helper()
	dir := "."
	for _, part := range strings.Split(name, "/") {
		next := path.Join(dir, part)
		if err := b.Mkdir(context.Background(), next, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		if err := b.SyncDir(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
}

func (p *testProtector) Check(ctx context.Context, b Binding) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.authority
}
func (p *testProtector) Protect(ctx context.Context, r ProtectionRequest) (ProtectionReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ProtectionReceipt{}, err
	}
	for _, entry := range r.Frames {
		if existing, ok := p.retained[frameKey(entry.Reference)]; ok && !bytesEqual(existing, entry.Bytes) {
			return ProtectionReceipt{}, ErrConflict
		}
		p.retained[frameKey(entry.Reference)] = append([]byte(nil), entry.Bytes...)
	}
	for _, position := range r.Required {
		for sequence := uint64(1); sequence <= position.Sequence; sequence++ {
			position.Sequence = sequence
			if _, ok := p.retained[frameKey(record.FrameRef{StreamKind: 1, Position: position})]; !ok {
				return ProtectionReceipt{}, fmt.Errorf("missing owned prefix %s/%d", position.Volume, sequence)
			}
		}
	}
	for _, boundary := range r.Histories {
		if boundary.StreamKind == 2 {
			stream := segment.Stream{Volume: boundary.Position.Volume, Generation: boundary.Position.Generation, Configuration: boundary.Position.Configuration, Epoch: boundary.Position.Epoch, Kind: 2, Reporter: *boundary.Reporter, ReporterIncarnation: *boundary.ReporterIncarnation}
			first := boundary.Position
			first.Sequence = 1
			reader, err := segment.OpenReader(ctx, p.fixture.fs, stream, segment.RecoveryOptions{}, first)
			if err != nil {
				return ProtectionReceipt{}, err
			}
			for {
				entry, readErr := reader.Next(ctx)
				if errors.Is(readErr, io.EOF) {
					break
				}
				if readErr != nil {
					_ = reader.Close()
					return ProtectionReceipt{}, readErr
				}
				p.retained[frameKey(entry.Reference)] = append([]byte(nil), entry.Bytes...)
			}
			if err := reader.Close(); err != nil {
				return ProtectionReceipt{}, err
			}
		}
		for sequence := uint64(1); sequence <= boundary.Position.Sequence; sequence++ {
			ref := boundary
			ref.Position.Sequence = sequence
			if _, ok := p.retained[frameKey(ref)]; !ok {
				return ProtectionReceipt{}, fmt.Errorf("missing complete historical prefix %d", sequence)
			}
		}
		if bytes := p.retained[frameKey(boundary)]; sha256.Sum256(bytes) != boundary.SHA256 {
			return ProtectionReceipt{}, fmt.Errorf("retained boundary hash mismatch")
		}
	}
	receipt := ProtectionReceipt{Session: r.Binding.Session, Connection: r.Binding.Connection, Partner: r.Binding.Authority.Partner, Incarnation: r.Binding.Authority.PartnerIncarnation, Candidate: r.Candidate.Reference, Retained: r.Required, Histories: r.Histories}
	if r.Decision {
		entry, ok := p.accepted[frameKey(r.Candidate.Reference)]
		if !ok {
			d := r.Candidate.Record.Payload.(record.Decision)
			a := r.Binding.Authority
			w := p.writers[a.Volume]
			if w == nil {
				s := segment.Stream{Volume: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Kind: 2, Reporter: a.Partner, ReporterIncarnation: a.PartnerIncarnation}
				mkdir(p.fixture.t, p.fixture.fs, s.Directory())
				var err error
				w, err = segment.NewWriter(ctx, p.fixture.fs, s, segment.Options{MaximumBytes: 1 << 20, NewSegment: p.fixture.nextID, Structural: func(uint64) record.Header { h := p.fixture.header(); h.Incarnation = a.PartnerIncarnation; return h }})
				if err != nil {
					return receipt, err
				}
				p.writers[a.Volume] = w
			}
			accept := record.Accept{SlotKind: 2, Candidate: r.Candidate.Reference, Request: &d.ID.Request, Term: d.ID.Term, Authority: a, Reporter: a.Partner, ReporterIncarnation: a.PartnerIncarnation, Retained: r.Required, Dependencies: r.Histories}
			h := r.Candidate.Record.Header
			h.Sequence = 0
			h.Incarnation = a.PartnerIncarnation
			rc, err := w.Append(ctx, record.Record{Header: h, Payload: accept})
			if err != nil {
				return receipt, err
			}
			if err := rc.Wait(ctx); err != nil {
				return receipt, err
			}
			h.Sequence = rc.Reference.Position.Sequence
			bytes, err := record.Encode(record.Record{Header: h, Payload: accept})
			if err != nil {
				return receipt, err
			}
			decoded, err := record.Decode(bytes)
			if err != nil {
				return receipt, err
			}
			entry = segment.Entry{Bytes: bytes, Record: decoded, Reference: rc.Reference}
			p.accepted[frameKey(r.Candidate.Reference)] = entry
		}
		receipt.Acceptance = append([]byte(nil), entry.Bytes...)
	}
	if p.mutate != nil {
		p.mutate(&receipt)
	}
	return receipt, p.fail
}

func (a *testApply) Apply(ctx context.Context, prepared segment.Entry, decision segment.Entry) error {
	if a.fail {
		return io.ErrUnexpectedEOF
	}
	p := prepared.Record.Payload.(record.Prepared)
	marker := fmt.Sprintf("applied-operation-%s-%s-%d", prepared.Record.Header.Volume, p.Request.Session.Nonce, p.Request.Number)
	if f, err := a.fs.Open(ctx, marker, os.O_RDONLY, 0); err == nil {
		return f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	write, ok := p.Operation.Intent.Arguments.(record.Write)
	if !ok {
		return nil
	}
	if p.Binding.Request.Session.Anchor != write.Object.Home {
		return fmt.Errorf("fixture home")
	}
	if prepared.Record.Header.Volume != write.Object.Home {
		return nil
	}
	name := "applied-" + write.Object.Home.String()
	f, err := a.fs.Open(ctx, name, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err = f.WriteAt(ctx, write.Data, int64(write.Offset)); err != nil {
		return err
	}
	if err := f.Sync(ctx); err != nil {
		return err
	}
	mark, err := a.fs.Open(ctx, marker, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = mark.Close() }()
	if _, err := mark.WriteAt(ctx, []byte{1}, 0); err != nil {
		return err
	}
	if err := mark.Sync(ctx); err != nil {
		return err
	}
	return a.fs.SyncDir(ctx, ".")
}
func (a *testApply) Abort(ctx context.Context, r AbortRequest) error {
	if a.fail {
		return io.ErrUnexpectedEOF
	}
	id := r.Begin.Record.Payload.(record.Begin).Binding.Request
	name := fmt.Sprintf("staging-%s-%s-%d", r.Participant, id.Session.Nonce, id.Number)
	if err := a.fs.Unlink(ctx, name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return a.fs.SyncDir(ctx, ".")
}
func (a *testApply) Barrier(ctx context.Context, r BarrierRequest) error {
	if a.fail {
		return io.ErrUnexpectedEOF
	}
	marker := fmt.Sprintf("barrier-%s-%d-%d", r.Complete.Visibility.Affinity.Closure, r.Complete.Visibility.Generation, r.Complete.Request.Number)
	if file, err := a.fs.Open(ctx, marker, os.O_RDONLY, 0); err == nil {
		return file.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if r.Complete.Result.Status == 1 && len(r.Installs) > 0 {
		p := r.Installs[0].Record.Payload.(record.Install)
		if write, ok := p.Binding.Intent.Arguments.(record.Write); ok {
			file, err := a.fs.Open(ctx, "applied-"+write.Object.Home.String(), os.O_RDONLY, 0)
			if err != nil {
				return err
			}
			bytes := make([]byte, len(write.Data))
			_, readErr := file.ReadAt(ctx, bytes, int64(write.Offset))
			closeErr := file.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			if !bytesEqual(bytes, write.Data) {
				return fmt.Errorf("cache barrier precedes exact apply")
			}
		}
	}
	file, err := a.fs.Open(ctx, marker, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteAt(ctx, []byte("private drain complete"), 0); err != nil {
		return err
	}
	if err := file.Sync(ctx); err != nil {
		return err
	}
	return a.fs.SyncDir(ctx, ".")
}

func (f *fixture) propose(volume record.UUID, payload any) segment.Entry {
	f.t.Helper()
	entry, err := f.engine.Propose(context.Background(), volume, payload)
	if err != nil {
		f.t.Fatalf("propose %T: %v", payload, err)
	}
	return entry
}

func (f *fixture) steps() []func() {
	return []func(){
		func() {
			f.propose(uuid(1), record.Session{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, State: 1})
		},
		func() {
			object := record.ObjectID{Home: uuid(1), Allocation: uuid(50)}
			data := f.data
			if data == nil {
				data = []byte("exact bytes")
			}
			revision := f.revision
			if revision == 0 {
				revision = 1
			}
			intent := record.Intent{Kind: 8, Arguments: record.Write{Object: object, Data: data}, SelectedMetadata: []record.ObjectEdit{{Object: object, ExpectedRevision: revision, After: record.Metadata{Revision: revision + 1, Type: 1, Mode: 0600, Size: uint64(len(data)), Nlink: 1}}}}
			original := record.InternalRequest{Version: 1, ID: f.id, NamespaceVolume: uuid(1), View: uuid(51), Intent: intent}
			_, digest, err := record.EncodeInternal(original)
			if err != nil {
				f.t.Fatal(err)
			}
			f.document = record.RequestDocument{Class: 2, Internal: &original}
			f.digest = digest
			f.claim = f.propose(uuid(1), record.RequestClaim{Document: f.document, Digest: digest})
		},
		func() {
			var authorities []record.Authority
			for _, g := range f.options.Groups {
				authorities = append(authorities, g.Binding.Authority)
			}
			manifest := record.Affinity{Closure: uuid(60), ManifestVersion: 1, Members: []record.UUID{uuid(1), uuid(2)}, Authorities: authorities}
			entry := f.propose(uuid(1), manifest)
			f.binding = record.Binding{Request: f.id, RequestDigest: f.digest, Affinity: affinityRef(manifest, entry.Bytes), Coordinator: uuid(1), Intent: f.document.Internal.Intent, Participants: manifest.Members, Authorities: authorities}
			f.propose(uuid(2), manifest)
		},
		func() { f.bind = f.propose(uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: f.binding}) },
		func() {
			f.begin = f.propose(uuid(1), record.Begin{Binding: f.binding, Bind: f.bind.Reference, Document: f.document, Term: 1})
		},
		func() {
			for _, id := range f.binding.Participants {
				f.propose(id, record.TxAnchor{Request: f.id, Digest: f.digest, Binding: f.binding, Begin: f.begin.Reference, CoordinatorTerm: 1})
			}
		},
		func() {
			for _, id := range f.binding.Participants {
				mutation := record.Mutation{Intent: f.binding.Intent}
				for _, edit := range f.binding.Intent.SelectedMetadata {
					if edit.Object.Home == id {
						mutation.Redo.Objects = append(mutation.Redo.Objects, edit)
					}
				}
				redo := redoHash(f.t, f.engine.groups[id], mutation)
				f.prepares = append(f.prepares, f.propose(id, record.Prepared{Request: f.id, Digest: f.digest, Begin: f.begin.Reference, Binding: f.binding, Term: 1, Operation: mutation, RedoSHA256: redo}))
			}
		},
		func() {
			var prepares []record.PrepareRef
			for _, entry := range f.prepares {
				p := entry.Record.Payload.(record.Prepared)
				prepares = append(prepares, record.PrepareRef{Participant: entry.Record.Header.Volume, Prepare: entry.Reference, RedoSHA256: p.RedoSHA256})
			}
			a := f.binding.Authorities[0]
			object := f.binding.Intent.Arguments.(record.Write).Object
			f.decision = f.propose(uuid(1), record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: f.id, Term: 1}, Digest: f.digest, Begin: f.begin.Reference, Outcome: 1, Prepares: prepares, Result: record.TerminalResult{Status: 1, Source: &object}, Authority: a})
		},
		func() {
			accept, err := f.engine.Acceptance(f.decision.Reference)
			if err != nil {
				f.t.Fatal(err)
			}
			d := f.decision.Record.Payload.(record.Decision)
			for _, entry := range f.prepares {
				p := entry.Record.Payload.(record.Prepared)
				pr := record.PrepareRef{Participant: entry.Record.Header.Volume, Prepare: entry.Reference, RedoSHA256: p.RedoSHA256}
				f.installs = append(f.installs, f.propose(pr.Participant, record.Install{Request: f.id, Digest: f.digest, Binding: f.binding, Decision: record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}, Outcome: 1, Prepare: &pr, Redo: &p.Operation.Redo, Applied: entry.Reference.Position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}}))
			}
		},
		func() {
			var refs []record.FrameRef
			var vector record.Vector
			for _, entry := range f.installs {
				refs = append(refs, entry.Reference)
				vector = append(vector, entry.Reference.Position)
			}
			SortReferences(refs)
			prior := f.engine.state.visibility[f.binding.Affinity.Closure]
			visibility := record.Visibility{Affinity: f.binding.Affinity, Generation: prior + 1}
			barrier := f.propose(uuid(1), record.ViewBarrier{Affinity: f.binding.Affinity, OldVisibility: record.Visibility{Affinity: f.binding.Affinity, Generation: prior}, NewVisibility: visibility, ServingGeneration: uuid(61), Incarnation: f.engine.groups[uuid(1)].Binding.Authority.OwnerIncarnation, Drained: true, Mechanism: uuid(62), Attestation: record.Artifact{Kind: 7, ID: uuid(63)}, Dependencies: refs})
			d := f.decision.Record.Payload.(record.Decision)
			result := d.Result
			ref := record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}
			result.Decision = &ref
			result.Visibility = &visibility
			result.Committed = vector
			f.complete = f.propose(uuid(1), record.Complete{Request: f.id, Digest: f.digest, Decision: ref, Installs: refs, Installed: vector, Result: result, Visibility: visibility, CacheBarriers: []record.FrameRef{barrier.Reference}})
		},
		func() {
			ref := f.bind.Reference
			f.propose(uuid(1), record.RequestResult{Request: f.id, Digest: f.digest, Claim: f.claim.Reference, Binding: &ref, Result: f.complete.Record.Payload.(record.Complete).Result, Dependencies: []record.FrameRef{f.complete.Reference}})
		},
	}
}

func redoHash(t *testing.T, g Group, m record.Mutation) record.Hash {
	t.Helper()
	h := record.Header{Volume: g.Stream.Volume, Generation: g.Stream.Generation, Configuration: g.Stream.Configuration, Epoch: g.Stream.Epoch, Sequence: 1, Operation: uuid(99), Incarnation: uuid(20)}
	bytes, err := record.Encode(record.Record{Header: h, Payload: m})
	if err != nil {
		t.Fatal(err)
	}
	intent, err := record.EncodeIntent(m.Intent)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(bytes[144+len(intent) : len(bytes)-4-8])
}

func (f *fixture) restart() {
	f.t.Helper()
	if err := f.engine.Close(); err != nil {
		f.t.Fatal(err)
	}
	for _, w := range f.protector.writers {
		if err := w.Close(); err != nil {
			f.t.Fatal(err)
		}
	}
	f.protector.writers = make(map[record.UUID]*segment.Writer)
	f.fs.Crash()
	var err error
	f.engine, err = Open(context.Background(), f.options)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.engine.Reconcile(context.Background()); err != nil {
		f.t.Fatal(err)
	}
}

func TestPersistentTransitionsCrashReplay(t *testing.T) {
	for crash := 0; crash <= 11; crash++ {
		t.Run(fmt.Sprint(crash), func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for i, step := range steps {
				if i == crash {
					f.restart()
				}
				step()
			}
			if crash == len(steps) {
				f.restart()
			}
			status, err := f.engine.GetStatus(context.Background(), f.id)
			if err != nil || status.Result == nil || status.Result.Status != 1 {
				t.Fatalf("terminal result: %+v %v", status, err)
			}
			before := len(f.engine.Retained())
			steps[10]()
			if len(f.engine.Retained()) != before {
				t.Fatal("retry appended a second result")
			}
			file, err := f.fs.Open(context.Background(), "applied-"+uuid(1).String(), os.O_RDONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			bytes := make([]byte, 11)
			if _, err := file.ReadAt(context.Background(), bytes, 0); err != nil || string(bytes) != "exact bytes" {
				t.Fatalf("replayed data %q: %v", bytes, err)
			}
		})
	}
}
