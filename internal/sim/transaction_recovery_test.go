package sim

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path"
	"reflect"
	"slices"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/transactions"
	"github.com/snonux/homelabfs/internal/vfs"
)

type transactionRecovery struct {
	f                          *transactionFixture
	authorization, attestation record.Artifact
	recovery                   record.UUID
	oldOwner, oldIncarnation   record.UUID
	continuous                 bool
}

var _ transactions.RecoveryVerifier = (*transactionRecovery)(nil)

func transactionImage(ctx context.Context, backend storage.Filesystem) (map[string][]byte, error) {
	image := make(map[string][]byte)
	total := 0
	var walk func(string) error
	walk = func(dir string) error {
		d, err := backend.OpenDir(ctx, dir)
		if err != nil {
			return err
		}
		entries, err := d.ReadDir(ctx, 1024)
		closeErr := d.Close()
		if len(entries) == 1024 && err == nil {
			return transactions.ErrLimit
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		for _, entry := range entries {
			name := path.Join(dir, entry.Name)
			if entry.Info.Mode.IsDir() {
				if err := walk(name); err != nil {
					return err
				}
				continue
			}
			file, err := backend.Open(ctx, name, os.O_RDONLY, 0)
			if err != nil {
				return err
			}
			var data []byte
			block := make([]byte, 4096)
			for off := int64(0); ; {
				n, readErr := file.ReadAt(ctx, block, off)
				data = append(data, block[:n]...)
				off += int64(n)
				total += n
				if total > 4<<20 {
					_ = file.Close()
					return transactions.ErrLimit
				}
				if errors.Is(readErr, io.EOF) {
					break
				}
				if readErr != nil {
					_ = file.Close()
					return readErr
				}
			}
			if err := file.Close(); err != nil {
				return err
			}
			image[name] = data
		}
		return nil
	}
	return image, walk(".")
}
func (f *transactionFixture) importImage(backend storage.Filesystem, image map[string][]byte) error {
	names := make([]string, 0, len(image))
	for name := range image {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		txMkdir(f.t, f.rig.ctx, backend, path.Dir(name))
		file, err := backend.Open(f.rig.ctx, name, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		if _, err := file.WriteAt(f.rig.ctx, image[name], 0); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Sync(f.rig.ctx); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		if err := backend.SyncDir(f.rig.ctx, path.Dir(name)); err != nil {
			return err
		}
	}
	return nil
}
func (f *transactionFixture) losePartnerRAM() error {
	for _, w := range f.protector.writers {
		if err := w.Close(); err != nil {
			return err
		}
	}
	f.protector.writers = make(map[record.UUID]*segment.Writer)
	if err := f.rig.nodes[1].Crash(); err != nil {
		return err
	}
	// This root models owned volatile reporter bytes, not a second durable copy.
	// Real Node crash fences all old handles. Its RAM root is explicitly discarded.
	delete(f.rig.nodes[1].roots, "volatile-accepts")
	f.protector.retained = make(map[record.Hash][]byte)
	f.protector.accepted = make(map[record.Hash]segment.Entry)
	f.protector.image = nil
	f.protector.available = false
	if err := f.rig.nodes[1].Restart(f.rig.ctx); err != nil {
		return err
	}
	var err error
	f.protector.reporter, err = f.rig.nodes[1].runtime.Disk("volatile-accepts")
	// The owner is NOT told that B restarted. Installed authority and queued
	// pre-crash receipts stay unchanged until an explicit reauthentication step.
	return err
}
func (f *transactionFixture) loseOwnerDisk() error {
	if err := f.engine.Close(); err != nil {
		return err
	}
	f.engine = nil
	if err := f.rig.nodes[0].Crash(); err != nil {
		return err
	}
	delete(f.rig.nodes[0].roots, "transactions")
	return nil
}
func (v *transactionRecovery) Authorize(ctx context.Context, scope transactions.RecoveryScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(scope.Predecessors) != 3 || len(scope.Successors) != 3 || len(scope.Streams) != 12 {
		return transactions.ErrEvidence
	}
	for _, a := range scope.Successors {
		if a.Owner != v.f.rig.nodes[1].cfg.Node || a.Partner != v.f.rig.nodes[2].cfg.Node || a.Epoch != 2 || a.CoordinatorTerm != 2 {
			return transactions.ErrEvidence
		}
	}
	return nil
}
func (v *transactionRecovery) Verify(ctx context.Context, r transactions.RecoveryRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	artifact := func(a record.Artifact) error {
		file, err := v.f.disks[0].Open(ctx, "proof-"+a.ID.String(), os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		raw := make([]byte, a.Length)
		if _, err := file.ReadAt(ctx, raw, 0); err != nil {
			return err
		}
		if sha256.Sum256(raw) != a.SHA256 {
			return transactions.ErrEvidence
		}
		return nil
	}
	switch p := r.Candidate.Record.Payload.(type) {
	case record.Promise:
		if p.Reporter == v.f.rig.nodes[2].cfg.Node {
			for _, tail := range p.Tails {
				if tail.Received.Sequence != 0 || tail.Accepted.Sequence != 0 || tail.Durable.Sequence != 0 {
					return transactions.ErrEvidence
				}
			}
		}
		if p.RecoveryID != v.recovery || p.Authorization != v.authorization {
			return transactions.ErrEvidence
		}
		return artifact(p.Authorization)
	case record.Fence:
		if p.RecoveryID != v.recovery || p.Attestation != v.attestation || p.OldOwner != v.oldOwner || p.OldIncarnation != v.oldIncarnation {
			return transactions.ErrEvidence
		}
		return artifact(p.Attestation)
	case record.TailReport:
		if p.Reporter == v.f.rig.nodes[2].cfg.Node {
			if len(p.Candidates) != 0 {
				return transactions.ErrEvidence
			}
			for _, tail := range p.Tails {
				if tail.Received.Sequence != 0 || tail.Accepted.Sequence != 0 || tail.Durable.Sequence != 0 {
					return transactions.ErrEvidence
				}
			}
		}
		for _, tail := range p.Tails {
			if tail.Continuous && !v.continuous && p.Reporter == v.f.rig.nodes[1].cfg.Node {
				return transactions.ErrEvidence
			}
		}
	case record.RecoverySelect:
		var bound []record.Authority
		for _, a := range r.Successors {
			if slices.Contains(p.Binding.Participants, a.Volume) {
				bound = append(bound, a)
			}
		}
		if p.RecoveryID != v.recovery || !reflect.DeepEqual(p.NewAuthorities, bound) || p.AbsenceAbort && !v.continuous {
			return transactions.ErrEvidence
		}
		return artifact(v.attestation)
	}
	return nil
}
func (f *transactionFixture) proof(n byte, text string) record.Artifact {
	raw := []byte(text)
	artifact := record.Artifact{Kind: 6, ID: txUUID(n), Length: uint64(len(raw)), SHA256: sha256.Sum256(raw)}
	if err := f.importImage(f.disks[0], map[string][]byte{"proof-" + artifact.ID.String(): raw}); err != nil {
		f.t.Fatal(err)
	}
	return artifact
}
func (f *transactionFixture) successor(p *transactionProgress) (*transactionRecovery, error) {
	oldGroups := slices.Clone(f.options.Groups)
	oldOwner := oldGroups[0].Binding.Authority.Owner
	oldIncarnation := oldGroups[0].Binding.Authority.OwnerIncarnation
	// The new owner receives actual retained bytes, never oracle snapshots or
	// high-level inputs. With an empty partner tail this cannot reconstruct history.
	image := f.protector.image
	if len(image) == 0 {
		return nil, transactions.ErrEvidence
	}
	if err := f.loseOwnerDisk(); err != nil {
		return nil, err
	}
	if err := f.importImage(f.disks[1], image); err != nil {
		return nil, err
	}
	f.disks[0] = f.disks[1]
	f.ownerIndex = 1
	f.apply.published = TransactionView{}
	f.visibility = 0
	f.gated = true
	if err := f.apply.load(f.rig.ctx); err != nil {
		return nil, err
	}
	if err := f.apply.loadPublication(f.rig.ctx); err != nil {
		return nil, err
	}
	f.visibility = f.apply.published.Generation
	v := &transactionRecovery{f: f, recovery: txUUID(71), oldOwner: oldOwner, oldIncarnation: oldIncarnation, continuous: true}
	v.authorization = f.proof(72, "synthetic allocated exact three-group successor; no production authority")
	v.attestation = f.proof(73, "synthetic old execution and all cached view fence; no physical fence claim")
	f.options.Recovery = v
	f.options.HistoryGroups = nil
	f.options.EvidenceGroups = nil
	// Existing B volatile ACCEPT streams are authenticated sources. Recovery
	// appends promises on B and C independently before selecting the old value.
	for i, old := range oldGroups {
		old.Backend = f.disks[0]
		f.options.HistoryGroups = append(f.options.HistoryGroups, old)
		a := old.Binding.Authority
		a.Configuration = txUUID(70)
		a.Epoch = 2
		a.CoordinatorTerm = 2
		a.Owner = f.rig.nodes[1].cfg.Node
		a.Partner = f.rig.nodes[2].cfg.Node
		a.OwnerIncarnation = f.rig.nodes[1].runtime.Identity.Incarnation
		a.PartnerIncarnation = f.rig.nodes[2].runtime.Identity.Incarnation
		g := old
		g.Stream.Configuration = a.Configuration
		g.Stream.Epoch = 2
		g.Binding = transactions.Binding{Session: txUUID(74), Connection: txUUID(75), Authority: a}
		g.Writer = segment.Options{MaximumBytes: 1 << 20, NewSegment: f.nextID, Structural: func(uint64) record.Header { h := f.header(); h.Incarnation = a.OwnerIncarnation; return h }}
		txMkdir(f.t, f.rig.ctx, g.Backend, g.Stream.Directory())
		f.options.Groups[i] = g
		for _, index := range []int{1, 2} {
			reporter := f.rig.nodes[index]
			stream := old.Stream
			stream.Kind = 2
			stream.Reporter = reporter.cfg.Node
			stream.ReporterIncarnation = reporter.runtime.Identity.Incarnation
			backend := f.disks[2]
			if index == 1 {
				backend = f.protector.reporter
			}
			txMkdir(f.t, f.rig.ctx, backend, stream.Directory())
			evidence := old
			evidence.Backend = backend
			evidence.Stream = stream
			evidence.Writer = segment.Options{MaximumBytes: 1 << 20, NewSegment: f.nextID, Structural: func(uint64) record.Header { h := f.header(); h.Incarnation = stream.ReporterIncarnation; return h }}
			f.options.EvidenceGroups = append(f.options.EvidenceGroups, evidence)
		}
	}
	for _, w := range f.protector.writers {
		if err := w.Close(); err != nil {
			return nil, err
		}
	}
	f.protector.writers = make(map[record.UUID]*segment.Writer)
	// New normal ACCEPTs belong to C's own reporter memory, not B's old stream.
	reporter, err := f.rig.nodes[2].runtime.Disk("volatile-accepts")
	if err != nil {
		return nil, err
	}
	f.protector.reporter = reporter
	f.engine, err = transactions.Open(f.rig.ctx, f.options)
	if err != nil {
		return nil, err
	}
	_ = p
	return v, nil
}
func (f *transactionFixture) selectRecovery(p *transactionProgress, v *transactionRecovery, absence bool) (segment.Entry, error) {
	var promises, reports []record.FrameRef
	for _, g := range f.options.EvidenceGroups {
		a := g.Binding.Authority
		if !slices.Contains(p.binding.Participants, a.Volume) {
			continue
		}
		position := record.Position{Volume: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch}
		var accepts []record.FrameRef
		for _, entry := range f.engine.Retained() {
			r := entry.Reference
			if g.Stream.Reporter == f.rig.nodes[1].cfg.Node && r.StreamKind == 1 && r.Position.Volume == a.Volume && r.Position.Configuration == a.Configuration && r.Position.Sequence > position.Sequence {
				position = r.Position
			}
			if acceptance, ok := entry.Record.Payload.(record.Accept); ok && acceptance.Authority.Volume == a.Volume && acceptance.Reporter == g.Stream.Reporter {
				accepts = append(accepts, r)
			}
		}
		transactions.SortReferences(accepts)
		applied := position
		applied.Sequence = 0
		tail := record.Tail{Received: position, Durable: position, Accepted: position, Applied: applied, Incarnation: g.Stream.ReporterIncarnation, Continuous: v.continuous}
		promise := record.Promise{RecoveryID: v.recovery, Authority: a, PromisedEpoch: 2, PromisedTerm: 2, Reporter: g.Stream.Reporter, ReporterIncarnation: g.Stream.ReporterIncarnation, Tails: []record.Tail{tail}, PriorAcceptances: accepts, Authorization: v.authorization}
		entry, err := f.engine.ProposeRecovery(f.rig.ctx, g.Stream, promise)
		if err != nil {
			return entry, err
		}
		promises = append(promises, entry.Reference)
		report := record.TailReport{RecoveryID: v.recovery, Reporter: g.Stream.Reporter, ReporterIncarnation: g.Stream.ReporterIncarnation, Promises: []record.FrameRef{entry.Reference}, Tails: []record.Tail{tail}, Dependencies: accepts}
		if g.Stream.Reporter == f.rig.nodes[1].cfg.Node && a.Volume == p.binding.Coordinator && len(p.decision.Bytes) > 0 {
			report.Candidates = []record.FrameRef{p.decision.Reference}
		}
		entry, err = f.engine.ProposeRecovery(f.rig.ctx, g.Stream, report)
		if err != nil {
			return entry, err
		}
		reports = append(reports, entry.Reference)
	}
	transactions.SortReferences(promises)
	transactions.SortReferences(reports)
	stream := f.options.Groups[1].Stream // bound coordinator is volume 3, not unused group 1.
	fence := record.Fence{RecoveryID: v.recovery, Authorities: p.binding.Authorities, OldOwner: v.oldOwner, OldIncarnation: v.oldIncarnation, ServingGenerations: []record.UUID{txUUID(61)}, Views: []record.UUID{txUUID(40)}, Mechanism: txUUID(76), Attestation: v.attestation, Dependencies: promises}
	entry, err := f.engine.ProposeRecovery(f.rig.ctx, stream, fence)
	if err != nil {
		return entry, err
	}
	var authorities []record.Authority
	for _, g := range f.options.Groups {
		if slices.Contains(p.binding.Participants, g.Stream.Volume) {
			authorities = append(authorities, g.Binding.Authority)
		}
	}
	selection := record.RecoverySelect{RecoveryID: v.recovery, SlotKind: 2, Operation: p.begin.Record.Header.Operation, Binding: &p.binding, NewAuthorities: authorities, Promises: promises, Reports: reports, AbsenceAbort: absence, Fence: []record.FrameRef{entry.Reference}}
	if !absence {
		selection.Selected = &p.decision.Reference
		selection.Operation = p.decision.Record.Header.Operation
	}
	return f.engine.ProposeRecovery(f.rig.ctx, stream, selection)
}

func TestTransactionOwnerLossFencedRecovery(t *testing.T) {
	f := newTransactionFixture(t, 6)
	input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "recovered"}
	p, err := f.start(input)
	if err != nil {
		t.Fatal(err)
	}
	steps := f.steps(p)
	for _, s := range steps[:10] {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
	original := p.decision
	v, err := f.successor(p)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := f.selectRecovery(p, v, false)
	if err != nil {
		t.Fatal(err)
	}
	// Normal authority for the successor becomes ready only after exact fenced
	// evidence. Reconcile does not publish any private state.
	if err := f.engine.Reconcile(f.rig.ctx); err != nil {
		t.Fatal(err)
	}
	old := original.Record.Payload.(record.Decision)
	if _, err := f.engine.Propose(f.rig.ctx, p.binding.Coordinator, old); err == nil {
		t.Fatal("delayed predecessor decision accepted")
	}
	a := f.options.Groups[1].Binding.Authority
	next := old
	next.ID.Configuration = a.Configuration
	next.ID.Epoch = a.Epoch
	next.ID.Term = a.CoordinatorTerm
	next.Authority = a
	next.Recovery = &selection.Reference
	p.decision, err = f.engine.Propose(f.rig.ctx, a.Volume, next)
	if err != nil {
		t.Fatal(err)
	}
	if p.decision.Record.Header.Operation != original.Record.Header.Operation {
		t.Fatal("recovery allocated another effect identity")
	}
	for _, s := range steps[10:] {
		if err := s.run(); err != nil {
			t.Fatalf("recovered %s: %v", s.name, err)
		}
	}
	status, err := f.engine.GetStatus(f.rig.ctx, input.ID)
	if err != nil || status.Result == nil || status.Result.Decision.ID.Term != 2 {
		t.Fatalf("recovered outcome: %+v %v", status, err)
	}
	f.history[0].Completion = "committed"
	f.history[0].CompletedAt = 1
	f.history[0].Reply = status.Result
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionUnobservedPartnerRAMLossAndDelayedReceipt(t *testing.T) {
	f := newTransactionFixture(t, 7)
	input := TransactionInput{ID: f.id(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
	p, err := f.start(input)
	if err != nil {
		t.Fatal(err)
	}
	steps := f.steps(p)
	for _, s := range steps[:9] {
		if err := s.run(); err != nil {
			t.Fatal(err)
		}
	}
	// ACK is serialized first, B loses all owned RAM, then the old ACK arrives.
	// A has no perfect crash detector: this old response may still choose COMMIT.
	f.protector.afterAccept = f.losePartnerRAM
	if err := steps[9].run(); err != nil {
		t.Fatal(err)
	}
	if len(f.protector.retained) != 0 || len(f.protector.image) != 0 {
		t.Fatal("B restart fabricated retained history")
	}
	if _, err := f.engine.Acceptance(p.decision.Reference); err != nil {
		t.Fatal("delayed precrash ACCEPT was automatically revoked")
	}
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	if err := f.loseOwnerDisk(); err != nil {
		t.Fatal(err)
	}
	// C has neither the decision nor dependencies. Recovery cannot be sourced
	// from oracle history or a restarted empty B. A new empty engine has no session.
	backend, err := vfs.NewSimulator(vfs.Config{Seed: 7, MaxFileSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	groups := slices.Clone(f.options.Groups)
	for i := range groups {
		groups[i].Backend = backend
		txMkdir(t, f.rig.ctx, backend, groups[i].Stream.Directory())
	}
	options := f.options
	options.Groups = groups
	f.protector.available = false
	f.engine, err = transactions.Open(f.rig.ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	status, err := f.engine.GetStatus(f.rig.ctx, input.ID)
	if err == nil || status.Result != nil {
		t.Fatalf("accepted-loss gap invented outcome %+v %v", status, err)
	}
	if len(f.apply.published.Names) != len(f.baseline.Names) {
		t.Fatal("accepted-loss gap exposed partial namespace")
	}
}
