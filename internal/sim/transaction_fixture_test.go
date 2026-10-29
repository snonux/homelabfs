package sim

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/transactions"
)

type transactionFixture struct {
	gated bool

	ownerIndex int

	t            *testing.T
	rig          *rig
	disks        [3]storage.Filesystem
	engine       *transactions.Engine
	options      transactions.Options
	protector    *transactionProtector
	apply        *transactionApply
	counter      uint64
	baseline     TransactionView
	history      []TransactionInvocation
	observations []TransactionObservation
	visibility   uint64
	manifest     record.Affinity
	affinity     record.AffinityRef
	states       map[fsapi.TransactionID]*transactionProgress
}
type transactionProtector struct {
	image       map[string][]byte
	reporter    storage.Filesystem
	afterAccept func() error

	f                    *transactionFixture
	retained             map[record.Hash][]byte
	accepted             map[record.Hash]segment.Entry
	writers              map[record.UUID]*segment.Writer
	available, authority bool
	failAfterAccept      bool
	delayed              []transactions.ProtectionReceipt
}
type transactionDiskState struct {
	Objects map[record.ObjectID]transactionPrivateObject
	Entries map[string]record.EntryState
	Parents map[record.ObjectID]record.ParentState
	Applied map[string]bool
	Counts  map[fsapi.TransactionID]map[record.UUID]uint64
}
type transactionPublication struct {
	Generation uint64
	State      transactionDiskState
	Complete   *record.FrameRef
}

type transactionPrivateObject struct {
	Metadata record.Metadata
	Data     []byte
	Pins     uint64
}
type transactionApply struct {
	f                            *transactionFixture
	state                        transactionDiskState
	published                    TransactionView
	failBarrier, double, partial bool
}
type transactionProgress struct {
	input                                          TransactionInput
	document                                       record.RequestDocument
	digest                                         record.Hash
	claim, bind, begin, decision, complete, result segment.Entry
	binding                                        record.Binding
	prepares, installs                             []segment.Entry
	redo                                           map[record.UUID]record.Mutation
}

var _ transactions.Protector = (*transactionProtector)(nil)
var _ transactions.Applier = (*transactionApply)(nil)

func newTransactionFixture(t *testing.T, seed uint64) *transactionFixture {
	t.Helper()
	f := &transactionFixture{t: t, states: make(map[fsapi.TransactionID]*transactionProgress), visibility: 1}
	f.rig = newRigWithTimeout(t, seed, 3, nil, 90*time.Second)
	for i, n := range f.rig.nodes {
		d, err := n.runtime.Disk("transactions")
		if err != nil {
			t.Fatal(err)
		}
		f.disks[i] = d
	}
	f.protector = &transactionProtector{f: f, retained: make(map[record.Hash][]byte), accepted: make(map[record.Hash]segment.Entry), writers: make(map[record.UUID]*segment.Writer), available: true, authority: true}
	f.apply = &transactionApply{f: f, state: transactionDiskState{Objects: make(map[record.ObjectID]transactionPrivateObject), Entries: make(map[string]record.EntryState), Parents: make(map[record.ObjectID]record.ParentState), Applied: make(map[string]bool), Counts: make(map[fsapi.TransactionID]map[record.UUID]uint64)}}
	reporter, err := f.rig.nodes[1].runtime.Disk("volatile-accepts")
	if err != nil {
		t.Fatal(err)
	}
	f.protector.reporter = reporter
	f.options = transactions.Options{Protector: f.protector, Applier: f.apply, Header: f.header, MaximumFrames: 1024, MaximumBytes: 4 << 20}
	members := []record.UUID{f.rig.nodes[0].cfg.Node, f.rig.nodes[1].cfg.Node, f.rig.nodes[2].cfg.Node}
	slices.SortFunc(members, func(a, b record.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, volume := range []record.UUID{txUUID(1), txUUID(3), txUUID(4)} {
		a := record.Authority{Volume: volume, Generation: txUUID(8), Configuration: txUUID(9), Epoch: 1, Owner: f.rig.nodes[0].cfg.Node, Partner: f.rig.nodes[1].cfg.Node, Members: members, CoordinatorTerm: 1, OwnerIncarnation: f.rig.nodes[0].runtime.Identity.Incarnation, PartnerIncarnation: f.rig.nodes[1].runtime.Identity.Incarnation}
		stream := segment.Stream{Volume: volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: 1, Kind: 1}
		txMkdir(t, f.rig.ctx, f.disks[0], stream.Directory())
		f.options.Groups = append(f.options.Groups, transactions.Group{Backend: f.disks[0], Stream: stream, Binding: transactions.Binding{Session: txUUID(10), Connection: txUUID(11), Authority: a}, Writer: segment.Options{MaximumBytes: 1 << 20, NewSegment: f.nextID, Structural: func(uint64) record.Header { h := f.header(); h.Incarnation = a.OwnerIncarnation; return h }}})
		root := txRoot(volume)
		f.apply.state.Objects[root] = transactionPrivateObject{Metadata: record.Metadata{Revision: 1, Type: 2, Mode: 0700, Nlink: 2}}
		f.apply.state.Parents[root] = record.ParentState{Revision: 1, Parent: root}
	}
	f.addObject(txUUID(3), "file", txObject(3, 50), 1, []byte("fixture-data"), 1)
	f.addObject(txUUID(4), "other", txObject(4, 51), 1, []byte("other-data"), 0)
	f.addObject(txUUID(3), "sym", txObject(3, 54), 3, []byte("../file"), 0)
	f.addObject(txUUID(3), "dir", txObject(3, 52), 2, nil, 0)
	f.addObject(txUUID(3), "dir/child", txObject(3, 53), 2, nil, 0)
	f.baseline = transactionBaseline()
	f.apply.published = cloneTransactionView(f.baseline)
	if err := f.apply.persist(f.rig.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.apply.publish(f.rig.ctx, 1, nil); err != nil {
		t.Fatal(err)
	}
	f.engine, err = transactions.Open(f.rig.ctx, f.options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f.engine != nil {
			if err := f.engine.Close(); err != nil {
				t.Error(err)
			}
		}
		for _, w := range f.protector.writers {
			if err := w.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	return f
}

// transactionBaseline declares independent fixture input. It never reads the
// namespace adapter or engine to manufacture an expected recovered answer.
func transactionBaseline() TransactionView {
	v := TransactionView{Generation: 1, Names: make(map[string]fsapi.ObjectID), Objects: make(map[fsapi.ObjectID]TransactionObject)}
	for _, n := range []byte{1, 3, 4} {
		root := txRoot(txUUID(n))
		links := uint64(2)
		if n == 3 {
			links = 3
		}
		v.Names[txUUID(n).String()] = root
		v.Objects[root] = TransactionObject{Type: 2, Hash: sha256.Sum256(nil), Nlink: links, Parent: root}
	}
	v.Names[txUUID(3).String()+"/file"] = txObject(3, 50)
	v.Objects[txObject(3, 50)] = TransactionObject{Type: 1, Hash: sha256.Sum256([]byte("fixture-data")), Size: 12, Nlink: 1, Pins: 1}
	v.Names[txUUID(4).String()+"/other"] = txObject(4, 51)
	v.Objects[txObject(4, 51)] = TransactionObject{Type: 1, Hash: sha256.Sum256([]byte("other-data")), Size: 10, Nlink: 1}
	v.Names[txUUID(3).String()+"/sym"] = txObject(3, 54)
	v.Objects[txObject(3, 54)] = TransactionObject{Type: 3, Hash: sha256.Sum256([]byte("../file")), Size: 7, Nlink: 1}
	v.Names[txUUID(3).String()+"/dir"] = txObject(3, 52)
	v.Objects[txObject(3, 52)] = TransactionObject{Type: 2, Hash: sha256.Sum256(nil), Nlink: 3, Parent: txRoot(txUUID(3))}
	v.Names[txUUID(3).String()+"/dir/child"] = txObject(3, 53)
	v.Objects[txObject(3, 53)] = TransactionObject{Type: 2, Hash: sha256.Sum256(nil), Nlink: 2, Parent: txObject(3, 52)}
	return v
}

func (f *transactionFixture) addObject(volume record.UUID, name string, id record.ObjectID, kind uint8, data []byte, pins uint64) {
	parent, base, err := f.apply.resolveParent(volume, name)
	if err != nil {
		f.t.Fatal(err)
	}
	f.apply.state.Objects[id] = transactionPrivateObject{Metadata: record.Metadata{Revision: 1, Type: kind, Mode: 0600, Nlink: 1, Size: uint64(len(data))}, Data: slices.Clone(data), Pins: pins}
	if kind == 3 {
		o := f.apply.state.Objects[id]
		o.Metadata.SymlinkText = slices.Clone(data)
		f.apply.state.Objects[id] = o
	}
	if kind == 2 {
		o := f.apply.state.Objects[id]
		o.Metadata.Nlink = 2
		f.apply.state.Objects[id] = o
		f.apply.state.Parents[id] = record.ParentState{Revision: 1, Parent: parent, ParentEntryRevision: 1}
		p := f.apply.state.Objects[parent]
		p.Metadata.Nlink++
		f.apply.state.Objects[parent] = p
	}
	f.apply.state.Entries[txEntryKey(parent, base)] = record.EntryState{Revision: 1, Target: &id}
}
func txUUID(n byte) record.UUID { return record.UUID{0: n, 15: n} }
func txObject(home, allocation byte) record.ObjectID {
	return record.ObjectID{Home: txUUID(home), Allocation: txUUID(allocation)}
}
func txRoot(home record.UUID) record.ObjectID {
	return record.ObjectID{Home: home, Allocation: txUUID(20)}
}
func txEntryKey(parent record.ObjectID, name string) string {
	return parent.Home.String() + ":" + parent.Allocation.String() + "/" + name
}
func (f *transactionFixture) nextID() record.UUID {
	f.counter++
	return record.UUID{0: 90, 8: byte(f.counter >> 16), 9: byte(f.counter >> 8), 15: byte(f.counter)}
}
func (f *transactionFixture) header() record.Header { return record.Header{Operation: f.nextID()} }
func (f *transactionFixture) id(n uint64) fsapi.TransactionID {
	return fsapi.TransactionID{Session: fsapi.SessionID{Principal: fsapi.Principal{Realm: txUUID(30), Subject: "synthetic-user"}, Anchor: txUUID(3), Nonce: txUUID(31)}, Number: n}
}
func txMkdir(t *testing.T, ctx context.Context, b segment.Backend, name string) {
	t.Helper()
	if name == "." {
		return
	}
	dir := "."
	for _, part := range strings.Split(name, "/") {
		next := path.Join(dir, part)
		if err := b.Mkdir(ctx, next, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		if err := b.SyncDir(ctx, dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
}

func (p *transactionProtector) Check(ctx context.Context, b transactions.Binding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.authority {
		return transactions.ErrEvidence
	}
	for _, g := range p.f.options.Groups {
		if g.Stream.Volume == b.Authority.Volume && reflect.DeepEqual(g.Binding, b) {
			return nil
		}
	}
	return transactions.ErrConflict
}
func (p *transactionProtector) Protect(ctx context.Context, r transactions.ProtectionRequest) (transactions.ProtectionReceipt, error) {
	if err := p.Check(ctx, r.Binding); err != nil {
		return transactions.ProtectionReceipt{}, err
	}
	if !p.available {
		return transactions.ProtectionReceipt{}, transactions.ErrEvidence
	}
	if len(r.Frames) > 1024 {
		return transactions.ProtectionReceipt{}, transactions.ErrLimit
	}
	for _, entry := range r.Frames {
		p.retained[entry.Reference.SHA256] = slices.Clone(entry.Bytes)
	}
	for _, boundary := range r.Histories {
		found := false
		for _, entry := range r.Frames {
			if reflect.DeepEqual(entry.Reference, boundary) {
				found = true
			}
		}
		if !found {
			return transactions.ProtectionReceipt{}, fmt.Errorf("synthetic complete prefix missing")
		}
	}
	receipt := transactions.ProtectionReceipt{Session: r.Binding.Session, Connection: r.Binding.Connection, Partner: r.Binding.Authority.Partner, Incarnation: r.Binding.Authority.PartnerIncarnation, Candidate: r.Candidate.Reference, Retained: slices.Clone(r.Required), Histories: slices.Clone(r.Histories)}
	image, err := transactionImage(ctx, p.f.disks[0])
	if err != nil {
		return receipt, err
	}
	p.image = image
	if r.Decision {
		entry, ok := p.accepted[r.Candidate.Reference.SHA256]
		if !ok {
			var err error
			entry, err = p.accept(ctx, r)
			if err != nil {
				return receipt, err
			}
			p.accepted[r.Candidate.Reference.SHA256] = entry
		}
		receipt.Acceptance = slices.Clone(entry.Bytes)
		if p.afterAccept != nil {
			hook := p.afterAccept
			p.afterAccept = nil
			if err := hook(); err != nil {
				return receipt, err
			}
		}
	}
	if p.failAfterAccept && r.Decision {
		p.failAfterAccept = false
		p.delayed = append(p.delayed, receipt)
		return receipt, transactions.ErrEvidence
	}
	return receipt, nil
}
func (p *transactionProtector) accept(ctx context.Context, r transactions.ProtectionRequest) (segment.Entry, error) {
	a := r.Binding.Authority
	w := p.writers[a.Volume]
	if w == nil {
		stream := segment.Stream{Volume: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Kind: 2, Reporter: a.Partner, ReporterIncarnation: a.PartnerIncarnation}
		txMkdir(p.f.t, ctx, p.reporter, stream.Directory())
		var err error
		w, err = segment.NewWriter(ctx, p.reporter, stream, segment.Options{MaximumBytes: 1 << 20, NewSegment: p.f.nextID, Structural: func(uint64) record.Header { h := p.f.header(); h.Incarnation = a.PartnerIncarnation; return h }})
		if err != nil {
			return segment.Entry{}, err
		}
		p.writers[a.Volume] = w
	}
	d := r.Candidate.Record.Payload.(record.Decision)
	payload := record.Accept{SlotKind: 2, Candidate: r.Candidate.Reference, Request: &d.ID.Request, Term: d.ID.Term, Authority: a, Reporter: a.Partner, ReporterIncarnation: a.PartnerIncarnation, Retained: r.Required, Dependencies: r.Histories}
	h := r.Candidate.Record.Header
	h.Sequence = 0
	h.Incarnation = a.PartnerIncarnation
	receipt, err := w.Append(ctx, record.Record{Header: h, Payload: payload})
	if err != nil {
		return segment.Entry{}, err
	}
	if err := receipt.Wait(ctx); err != nil {
		return segment.Entry{}, err
	}
	h.Sequence = receipt.Reference.Position.Sequence
	raw, err := record.Encode(record.Record{Header: h, Payload: payload})
	if err != nil {
		return segment.Entry{}, err
	}
	decoded, err := record.Decode(raw)
	return segment.Entry{Bytes: raw, Record: decoded, Reference: receipt.Reference}, err
}

func (a *transactionApply) Apply(ctx context.Context, prepared, decision segment.Entry) error {
	p := prepared.Record.Payload.(record.Prepared)
	member := prepared.Reference.Position.Volume
	key := fmt.Sprintf("%s/%d/%s", p.Request.Session.Nonce, p.Request.Number, member)
	if a.state.Applied[key] {
		if !a.double {
			return nil
		}
		a.state.Counts[p.Request][member]++
		if link, ok := p.Operation.Intent.Arguments.(record.Link); ok && link.Source.Home == member {
			object := a.state.Objects[link.Source]
			object.Metadata.Nlink++
			a.state.Objects[link.Source] = object
		}
		return a.persist(ctx)
	}
	// The adapter consumes participant-local redo only. Expected state is replayed
	// separately from paths and caller input in transaction_history.go.
	for _, edit := range p.Operation.Redo.Entries {
		key := txEntryKey(edit.Parent, edit.Name)
		prior := a.state.Entries[key]
		if !reflect.DeepEqual(prior, edit.Before) {
			return fmt.Errorf("private entry revision changed")
		}
		a.state.Entries[key] = edit.After
	}
	for _, edit := range p.Operation.Redo.Objects {
		previous := a.state.Objects[edit.Object]
		if previous.Metadata.Revision != edit.ExpectedRevision {
			return fmt.Errorf("private object revision changed")
		}
		previous.Metadata = edit.After
		a.state.Objects[edit.Object] = previous
	}
	for _, edit := range p.Operation.Redo.Ledgers {
		o := a.state.Objects[edit.Object]
		if o.Metadata.Nlink != edit.ResultingNlink {
			return fmt.Errorf("private lifetime delta mismatch")
		}
	}
	for _, edit := range p.Operation.Redo.Parents {
		prior, ok := a.state.Parents[edit.Directory]
		if edit.Before != nil && (!ok || prior != *edit.Before) {
			return fmt.Errorf("private ancestry revision changed")
		}
		a.state.Parents[edit.Directory] = edit.After
	}
	if write, ok := p.Operation.Intent.Arguments.(record.Write); ok && write.Object.Home == member {
		o := a.state.Objects[write.Object]
		o.Data = slices.Clone(write.Data)
		a.state.Objects[write.Object] = o
	}
	if a.state.Counts[p.Request] == nil {
		a.state.Counts[p.Request] = make(map[record.UUID]uint64)
	}
	a.state.Counts[p.Request][member]++
	a.state.Applied[key] = true
	if err := a.persist(ctx); err != nil {
		return err
	}
	if a.partial {
		a.published = a.view(a.published.Generation)
	}
	_ = decision
	return nil
}
func (a *transactionApply) Abort(ctx context.Context, r transactions.AbortRequest) error {
	return ctx.Err()
}
func (a *transactionApply) Barrier(ctx context.Context, r transactions.BarrierRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.failBarrier {
		return transactions.ErrEvidence
	}
	if r.Complete.Result.Status == 1 {
		for _, entry := range r.Installs {
			p := entry.Record.Payload.(record.Install)
			if a.state.Counts[p.Request][entry.Reference.Position.Volume] != 1 && !a.double {
				return fmt.Errorf("private barrier missing apply")
			}
		}
	}
	return nil
}
func (a *transactionApply) persist(ctx context.Context) error {
	return transactionPersist(ctx, a.f.disks[0], "private-state", a.state)
}
func (a *transactionApply) load(ctx context.Context) error {
	a.state = transactionDiskState{}
	return transactionLoad(ctx, a.f.disks[0], "private-state", &a.state)
}
func (a *transactionApply) publish(ctx context.Context, generation uint64, complete *record.FrameRef) error {
	source := transactionPublication{Generation: generation, State: a.state, Complete: complete}
	if err := transactionPersist(ctx, a.f.disks[0], "published-state", source); err != nil {
		return err
	}
	a.published = a.view(generation)
	return nil
}
func (a *transactionApply) loadPublication(ctx context.Context) error {
	var source transactionPublication
	if err := transactionLoad(ctx, a.f.disks[0], "published-state", &source); err != nil {
		return err
	}
	// Project newly decoded actual namespace/data bytes. Never keep an old
	// process's projection, and never treat its hashes as recovered object data.
	a.published = a.viewState(source.State, source.Generation)
	return nil
}
func transactionPersist(ctx context.Context, backend storage.Filesystem, name string, value any) error {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(value); err != nil {
		return err
	}
	if b.Len() > 1<<20 {
		return transactions.ErrLimit
	}
	file, err := backend.Open(ctx, name+"-next", os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	if _, err := file.WriteAt(ctx, b.Bytes(), 0); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(ctx); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := backend.Rename(ctx, name+"-next", name); err != nil {
		return err
	}
	return backend.SyncDir(ctx, ".")
}
func transactionLoad(ctx context.Context, backend storage.Filesystem, name string, value any) error {
	file, err := backend.Open(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var raw bytes.Buffer
	block := make([]byte, 4096)
	for off := int64(0); ; {
		n, err := file.ReadAt(ctx, block, off)
		raw.Write(block[:n])
		off += int64(n)
		if raw.Len() > 1<<20 {
			return transactions.ErrLimit
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	return gob.NewDecoder(&raw).Decode(value)
}

func (a *transactionApply) resolve(volume record.UUID, name string) (record.ObjectID, error) {
	current := txRoot(volume)
	if name == "" {
		return current, nil
	}
	for _, part := range strings.Split(name, "/") {
		entry := a.state.Entries[txEntryKey(current, part)]
		if entry.Target == nil {
			return record.ObjectID{}, os.ErrNotExist
		}
		current = *entry.Target
	}
	return current, nil
}
func (a *transactionApply) resolveParent(volume record.UUID, name string) (record.ObjectID, string, error) {
	dir, base := path.Split(name)
	parent, err := a.resolve(volume, strings.TrimSuffix(dir, "/"))
	if err != nil {
		return parent, "", err
	}
	if a.state.Objects[parent].Metadata.Type != 2 {
		return parent, "", fmt.Errorf("parent is not directory")
	}
	return parent, base, nil
}
func (a *transactionApply) view(generation uint64) TransactionView {
	return a.viewState(a.state, generation)
}
func (a *transactionApply) viewState(state transactionDiskState, generation uint64) TransactionView {
	view := TransactionView{Generation: generation, Names: make(map[string]fsapi.ObjectID), Objects: make(map[fsapi.ObjectID]TransactionObject)}
	for id, o := range state.Objects {
		view.Objects[id] = TransactionObject{Type: o.Metadata.Type, Hash: sha256.Sum256(o.Data), Size: o.Metadata.Size, Nlink: o.Metadata.Nlink, Parent: state.Parents[id].Parent, Pins: o.Pins}
	}
	var walk func(record.ObjectID, string, map[record.ObjectID]bool)
	walk = func(parent record.ObjectID, prefix string, ancestors map[record.ObjectID]bool) {
		if ancestors[parent] {
			return
		}
		ancestors = maps.Clone(ancestors)
		ancestors[parent] = true
		for key, entry := range state.Entries {
			if entry.Target != nil && strings.HasPrefix(key, txEntryKey(parent, "")) {
				name := prefix + strings.TrimPrefix(key, txEntryKey(parent, ""))
				view.Names[name] = *entry.Target
				if state.Objects[*entry.Target].Metadata.Type == 2 {
					walk(*entry.Target, name+"/", ancestors)
				}
			}
		}
	}
	for _, g := range a.f.options.Groups {
		root := txRoot(g.Stream.Volume)
		prefix := g.Stream.Volume.String() + "/"
		view.Names[strings.TrimSuffix(prefix, "/")] = root
		walk(root, prefix, make(map[record.ObjectID]bool))
	}
	return view
}
