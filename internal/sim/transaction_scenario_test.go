package sim

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/transactions"
)

type transactionStep struct {
	name string
	run  func() error
}
type transactionScript struct {
	Seed    uint64
	Actions []transactionAction
}
type transactionAction struct {
	Input    TransactionInput
	Fault    string
	Boundary int
}

func (f *transactionFixture) propose(volume record.UUID, payload any) (segment.Entry, error) {
	var entry segment.Entry
	var operationErr error
	err := f.rig.nodes[f.ownerIndex].Call(f.rig.ctx, fmt.Sprintf("transaction/%T", payload), func(ctx context.Context, _ *Runtime) error {
		entry, operationErr = f.engine.Propose(ctx, volume, payload)
		return nil
	})
	if err != nil {
		return entry, err
	}
	return entry, operationErr
}

func (f *transactionFixture) join(includeLower bool) error {
	f.gated = true
	members := []record.UUID{txUUID(3), txUUID(4)}
	if includeLower {
		members = []record.UUID{txUUID(1), txUUID(3), txUUID(4)}
	}
	manifest := record.Affinity{Closure: txUUID(60), ManifestVersion: 1, Members: members}
	if f.manifest.ManifestVersion > 0 {
		if len(f.manifest.Members) == len(members) {
			manifest = f.manifest
		} else {
			manifest.ManifestVersion = f.manifest.ManifestVersion + 1
			manifest.Predecessors = []record.AffinityRef{f.affinity}
		}
	}
	if manifest.Authorities == nil {
		for _, g := range f.options.Groups {
			if slices.Contains(members, g.Stream.Volume) {
				manifest.Authorities = append(manifest.Authorities, g.Binding.Authority)
			}
		}
	}
	// Persist the intended partial join as harness scheduling state; only protected
	// journal fan-out below establishes any engine binding authority.
	f.manifest = manifest
	for _, id := range members {
		entry, err := f.propose(id, manifest)
		if err != nil {
			return err
		}
		intentBytes, err := record.Encode(record.Record{Header: entry.Record.Header, Payload: manifest})
		if err != nil {
			return err
		}
		f.affinity = record.AffinityRef{Closure: manifest.Closure, ManifestVersion: manifest.ManifestVersion, ManifestSHA256: sha256.Sum256(intentBytes[144 : len(intentBytes)-4])}
	}
	return nil
}
func (f *transactionFixture) start(input TransactionInput) (*transactionProgress, error) {
	if len(f.history) >= 32 || len(input.Data) > 4096 {
		return nil, transactions.ErrLimit
	}
	p := f.states[input.ID]
	if p == nil {
		p = &transactionProgress{input: input}
		f.states[input.ID] = p
	}
	// Public claim construction does not consult paths. Internal home writes and
	// unlink are explicit synthetic resolved operations, outside the public API.
	if input.Kind == "rename" || input.Kind == "link" {
		operation := uint8(1)
		if input.Kind == "link" {
			operation = 2
		}
		original := record.OriginalRequest{Version: 1, ID: input.ID, Operation: operation, Source: record.Endpoint{Volume: input.SourceVolume, View: txUUID(40), Components: splitTransactionPath(input.Source)}, Destination: record.Endpoint{Volume: input.DestinationVolume, View: txUUID(40), Components: splitTransactionPath(input.Destination)}, Replacement: 2, ExpectedSource: input.ExpectedSource}
		p.document = record.RequestDocument{Class: 1, Original: &original}
	} else {
		if err := f.resolve(p); err != nil {
			return nil, err
		}
		original := record.InternalRequest{Version: 1, ID: input.ID, NamespaceVolume: input.SourceVolume, View: txUUID(40), Intent: p.binding.Intent}
		p.document = record.RequestDocument{Class: 2, Internal: &original}
	}
	canonical, digest, err := transactionDocumentBytes(p.document)
	if err != nil {
		return nil, err
	}
	p.digest = digest
	f.history = append(f.history, TransactionInvocation{Input: input, Canonical: canonical, Digest: digest})
	return p, nil
}
func splitTransactionPath(s string) []string {
	var out []string
	for _, part := range bytes.Split([]byte(s), []byte{'/'}) {
		out = append(out, string(part))
	}
	return out
}
func (f *transactionFixture) steps(p *transactionProgress) []transactionStep {
	anchor := p.input.ID.Session.Anchor
	return []transactionStep{
		{"session", func() error {
			_, err := f.propose(anchor, record.Session{Principal: p.input.ID.Session.Principal, Anchor: anchor, Nonce: p.input.ID.Session.Nonce, State: 1})
			return err
		}},
		{"claim", func() error {
			var err error
			p.claim, err = f.propose(anchor, record.RequestClaim{Document: p.document, Digest: p.digest})
			return err
		}},
		{"resolve-and-join", func() error {
			f.gated = true
			if p.redo == nil {
				if err := f.resolve(p); err != nil {
					return err
				}
			}
			if f.manifest.ManifestVersion == 0 {
				return f.join(false)
			}
			return nil
		}},
		{"bind", func() error {
			if !p.binding.Request.Valid() {
				p.binding.Request = p.input.ID
				p.binding.RequestDigest = p.digest
				p.binding.Affinity = f.affinity
				p.binding.Coordinator = f.manifest.Members[0]
				p.binding.Participants = slices.Clone(f.manifest.Members)
				p.binding.Authorities = slices.Clone(f.manifest.Authorities)
			}
			var err error
			p.bind, err = f.propose(anchor, record.RequestBind{Claim: p.claim.Reference, Binding: p.binding})
			return err
		}},
		{"begin", func() error {
			var err error
			p.begin, err = f.propose(p.binding.Coordinator, record.Begin{Binding: p.binding, Bind: p.bind.Reference, Document: p.document, Term: 1})
			if err == nil {
				f.gated = true
			}
			return err
		}},
		{"anchor-0", func() error { return f.participantAnchor(p, 0) }},
		{"anchor-1", func() error { return f.participantAnchor(p, 1) }},
		{"prepare-0", func() error { return f.prepare(p, 0) }},
		{"prepare-1", func() error { return f.prepare(p, 1) }},
		{"decision", func() error { return f.decide(p) }},
		{"install-0", func() error { return f.install(p, 0) }},
		{"install-1", func() error { return f.install(p, 1) }},
		{"complete", func() error { return f.complete(p) }},
		{"publish", func() error {
			if p.complete.Record.Payload.(record.Complete).Result.Status == 1 {
				generation := p.complete.Record.Payload.(record.Complete).Visibility.Generation
				if err := f.apply.publish(f.rig.ctx, generation, &p.complete.Reference); err != nil {
					return err
				}
				f.visibility = generation
				f.gated = false
			}
			return nil
		}},
		{"result", func() error {
			ref := p.bind.Reference
			var err error
			p.result, err = f.propose(anchor, record.RequestResult{Request: p.input.ID, Digest: p.digest, Claim: p.claim.Reference, Binding: &ref, Result: p.complete.Record.Payload.(record.Complete).Result, Dependencies: []record.FrameRef{p.complete.Reference}})
			return err
		}},
	}
}
func (f *transactionFixture) participantAnchor(p *transactionProgress, index int) error {
	// Additional participants after a closure merge use the same precise fan-out.
	for i, id := range p.binding.Participants {
		if i%2 != index {
			continue
		}
		if _, err := f.propose(id, record.TxAnchor{Request: p.input.ID, Digest: p.digest, Binding: p.binding, Begin: p.begin.Reference, CoordinatorTerm: 1}); err != nil {
			return err
		}
	}
	return nil
}
func (f *transactionFixture) prepare(p *transactionProgress, index int) error {
	for i, id := range p.binding.Participants {
		if i%2 != index {
			continue
		}
		m := p.redo[id]
		m.Intent = p.binding.Intent
		hash, err := transactionRedoHash(f.options.Groups, id, m)
		if err != nil {
			return err
		}
		entry, err := f.propose(id, record.Prepared{Request: p.input.ID, Digest: p.digest, Begin: p.begin.Reference, Binding: p.binding, Term: 1, Operation: m, RedoSHA256: hash})
		if err != nil {
			return err
		}
		p.prepares = replaceTransactionEntry(p.prepares, entry)
	}
	return nil
}
func (f *transactionFixture) decide(p *transactionProgress) error {
	var prepares []record.PrepareRef
	for _, entry := range p.prepares {
		pr := entry.Record.Payload.(record.Prepared)
		prepares = append(prepares, record.PrepareRef{Participant: entry.Reference.Position.Volume, Prepare: entry.Reference, RedoSHA256: pr.RedoSHA256})
	}
	sort.Slice(prepares, func(i, j int) bool { return bytes.Compare(prepares[i].Participant[:], prepares[j].Participant[:]) < 0 })
	a := p.binding.Authorities[0]
	object := transactionIntentSource(p.binding.Intent)
	var err error
	p.decision, err = f.propose(a.Volume, record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: p.input.ID, Term: 1}, Digest: p.digest, Begin: p.begin.Reference, Outcome: 1, Prepares: prepares, Result: record.TerminalResult{Status: 1, Source: &object}, Authority: a})
	return err
}
func (f *transactionFixture) install(p *transactionProgress, index int) error {
	accept, err := f.engine.Acceptance(p.decision.Reference)
	if err != nil {
		return err
	}
	d := p.decision.Record.Payload.(record.Decision)
	for i, id := range p.binding.Participants {
		if i%2 != index {
			continue
		}
		var prepared segment.Entry
		for _, entry := range p.prepares {
			if entry.Reference.Position.Volume == id {
				prepared = entry
			}
		}
		pr := prepared.Record.Payload.(record.Prepared)
		ref := record.PrepareRef{Participant: id, Prepare: prepared.Reference, RedoSHA256: pr.RedoSHA256}
		entry, err := f.propose(id, record.Install{Request: p.input.ID, Digest: p.digest, Binding: p.binding, Decision: record.DecisionRef{ID: d.ID, Decision: p.decision.Reference}, Outcome: 1, Prepare: &ref, Redo: &pr.Operation.Redo, Applied: prepared.Reference.Position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}})
		if err != nil {
			return err
		}
		p.installs = replaceTransactionEntry(p.installs, entry)
	}
	return nil
}
func (f *transactionFixture) complete(p *transactionProgress) error {
	var refs []record.FrameRef
	var vector record.Vector
	for _, entry := range p.installs {
		refs = append(refs, entry.Reference)
		vector = append(vector, entry.Reference.Position)
	}
	transactions.SortReferences(refs)
	sort.Slice(vector, func(i, j int) bool { return bytes.Compare(vector[i].Volume[:], vector[j].Volume[:]) < 0 })
	generation := f.visibility + 1
	if len(p.complete.Bytes) > 0 {
		generation = p.complete.Record.Payload.(record.Complete).Visibility.Generation
	}
	visibility := record.Visibility{Affinity: p.binding.Affinity, Generation: generation}
	barrier, err := f.propose(p.binding.Coordinator, record.ViewBarrier{Affinity: p.binding.Affinity, OldVisibility: record.Visibility{Affinity: p.binding.Affinity, Generation: generation - 1}, NewVisibility: visibility, ServingGeneration: txUUID(61), Incarnation: f.options.Groups[0].Binding.Authority.OwnerIncarnation, Drained: true, Mechanism: txUUID(62), Attestation: record.Artifact{Kind: 7, ID: txUUID(63)}, Dependencies: refs})
	if err != nil {
		return err
	}
	d := p.decision.Record.Payload.(record.Decision)
	result := d.Result
	decision := record.DecisionRef{ID: d.ID, Decision: p.decision.Reference}
	result.Decision = &decision
	result.Visibility = &visibility
	result.Committed = vector
	p.complete, err = f.propose(p.binding.Coordinator, record.Complete{Request: p.input.ID, Digest: p.digest, Decision: decision, Installs: refs, Installed: vector, Result: result, Visibility: visibility, CacheBarriers: []record.FrameRef{barrier.Reference}})
	return err
}
func replaceTransactionEntry(entries []segment.Entry, entry segment.Entry) []segment.Entry {
	for i, e := range entries {
		if e.Reference.Position.Volume == entry.Reference.Position.Volume {
			entries[i] = entry
			return entries
		}
	}
	return append(entries, entry)
}
func transactionIntentSource(intent record.Intent) record.ObjectID {
	switch p := intent.Arguments.(type) {
	case record.Rename:
		return p.Source
	case record.Link:
		return p.Source
	case record.Write:
		return p.Object
	case record.Unlink:
		return p.Removed
	}
	return record.ObjectID{}
}
func transactionRedoHash(groups []transactions.Group, id record.UUID, m record.Mutation) (record.Hash, error) {
	var a record.Authority
	for _, g := range groups {
		if g.Stream.Volume == id {
			a = g.Binding.Authority
		}
	}
	h := record.Header{Volume: id, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Sequence: 1, Operation: txUUID(99), Incarnation: a.OwnerIncarnation}
	raw, err := record.Encode(record.Record{Header: h, Payload: m})
	if err != nil {
		return record.Hash{}, err
	}
	intent, err := record.EncodeIntent(m.Intent)
	if err != nil {
		return record.Hash{}, err
	}
	return sha256.Sum256(raw[144+len(intent) : len(raw)-12]), nil
}

func (f *transactionFixture) resolve(p *transactionProgress) error {
	input := p.input
	a := f.apply
	object, err := a.resolve(input.SourceVolume, input.Source)
	if err != nil {
		return err
	}
	if input.Denied || input.ExpectedSource != nil && *input.ExpectedSource != object {
		return fsapi.Permission
	}
	sourceParent, sourceName, err := a.resolveParent(input.SourceVolume, input.Source)
	if err != nil {
		return err
	}
	original := a.state.Objects[object]
	metadata := make(map[record.ObjectID]record.Metadata)
	redo := make(map[record.UUID]record.Redo)
	advance := func(id record.ObjectID) record.Metadata {
		if m, ok := metadata[id]; ok {
			return m
		}
		m := a.state.Objects[id].Metadata
		m.Revision++
		metadata[id] = m
		return m
	}
	sourceEntry := a.state.Entries[txEntryKey(sourceParent, sourceName)]
	remove := record.EntryEdit{Parent: sourceParent, Name: sourceName, Before: sourceEntry, After: record.EntryState{Revision: sourceEntry.Revision + 1}}
	intent := record.Intent{}
	switch input.Kind {
	case "rename", "link":
		destParent, destName, err := a.resolveParent(input.DestinationVolume, input.Destination)
		if err != nil {
			return err
		}
		target := a.state.Entries[txEntryKey(destParent, destName)]
		if target.Target != nil {
			return os.ErrExist
		}
		if input.Kind == "link" && original.Metadata.Type != 1 {
			return fsapi.Invalid
		}
		if original.Metadata.Type == 2 {
			for ancestor := destParent; ; {
				if ancestor == object {
					return fsapi.Invalid
				}
				next := a.state.Parents[ancestor].Parent
				if next == ancestor || !next.Valid() {
					break
				}
				ancestor = next
			}
		}
		install := record.EntryEdit{Parent: destParent, Name: destName, Before: target, After: record.EntryState{Revision: target.Revision + 1, Target: &object}}
		r := redo[destParent.Home]
		r.Entries = append(r.Entries, install)
		redo[destParent.Home] = r
		m := advance(object)
		if input.Kind == "link" {
			intent = record.Intent{Kind: 4, Arguments: record.Link{Source: object, TargetParent: destParent, TargetName: destName}}
			m.Nlink++
			metadata[object] = m
		} else {
			intent = record.Intent{Kind: 7, Arguments: record.Rename{SourceParent: sourceParent, SourceName: sourceName, Source: object, DestinationParent: destParent, DestinationName: destName, Policy: 2}}
			r := redo[sourceParent.Home]
			r.Entries = append(r.Entries, remove)
			redo[sourceParent.Home] = r
			if original.Metadata.Type == 2 {
				before := a.state.Parents[object]
				r := redo[object.Home]
				r.Parents = append(r.Parents, record.ParentEdit{Directory: object, Before: &before, After: record.ParentState{Revision: before.Revision + 1, Parent: destParent, ParentEntryRevision: install.After.Revision}})
				redo[object.Home] = r
				old := advance(sourceParent)
				old.Nlink--
				metadata[sourceParent] = old
				next := advance(destParent)
				next.Nlink++
				metadata[destParent] = next
			}
		}
		_ = advance(sourceParent)
		_ = advance(destParent)
		ledger := record.LedgerEdit{Object: object, ExpectedRevision: original.Metadata.Revision, ResultingRevision: m.Revision, ExpectedNlink: original.Metadata.Nlink, ResultingNlink: m.Nlink, Add: []record.Reference{{Parent: destParent, Name: destName, EntryRevision: install.After.Revision}}}
		if input.Kind == "rename" {
			ledger.Remove = []record.Reference{{Parent: sourceParent, Name: sourceName, EntryRevision: sourceEntry.Revision}}
		}
		r = redo[object.Home]
		r.Ledgers = append(r.Ledgers, ledger)
		redo[object.Home] = r
	case "write":
		if original.Metadata.Type != 1 || len(input.Data) == 0 {
			return fsapi.Invalid
		}
		m := advance(object)
		m.Size = uint64(len(input.Data))
		metadata[object] = m
		intent = record.Intent{Kind: 8, Arguments: record.Write{Object: object, Data: slices.Clone(input.Data)}}
	case "unlink":
		if original.Metadata.Type != 1 {
			return fsapi.Invalid
		}
		m := advance(object)
		m.Nlink--
		metadata[object] = m
		_ = advance(sourceParent)
		intent = record.Intent{Kind: 5, Arguments: record.Unlink{Parent: sourceParent, Name: sourceName, Removed: object}}
		r := redo[sourceParent.Home]
		r.Entries = append(r.Entries, remove)
		redo[sourceParent.Home] = r
		r = redo[object.Home]
		r.Ledgers = append(r.Ledgers, record.LedgerEdit{Object: object, ExpectedRevision: original.Metadata.Revision, ResultingRevision: m.Revision, ExpectedNlink: original.Metadata.Nlink, ResultingNlink: m.Nlink, Remove: []record.Reference{{Parent: sourceParent, Name: sourceName, EntryRevision: sourceEntry.Revision}}})
		redo[object.Home] = r
	default:
		return fsapi.Invalid
	}
	for id, m := range metadata {
		intent.SelectedMetadata = append(intent.SelectedMetadata, record.ObjectEdit{Object: id, ExpectedRevision: m.Revision - 1, After: m})
	}
	sort.Slice(intent.SelectedMetadata, func(i, j int) bool {
		return transactionObjectLess(intent.SelectedMetadata[i].Object, intent.SelectedMetadata[j].Object)
	})
	p.binding.Intent = intent
	p.redo = make(map[record.UUID]record.Mutation)
	for _, g := range f.options.Groups {
		id := g.Stream.Volume
		r := redo[id]
		for _, edit := range intent.SelectedMetadata {
			if edit.Object.Home == id {
				r.Objects = append(r.Objects, edit)
			}
		}
		sort.Slice(r.Entries, func(i, j int) bool {
			if r.Entries[i].Parent != r.Entries[j].Parent {
				return transactionObjectLess(r.Entries[i].Parent, r.Entries[j].Parent)
			}
			return r.Entries[i].Name < r.Entries[j].Name
		})
		p.redo[id] = record.Mutation{Intent: intent, Redo: r}
	}
	return nil
}
func transactionObjectLess(a, b record.ObjectID) bool {
	if a.Home != b.Home {
		return bytes.Compare(a.Home[:], b.Home[:]) < 0
	}
	return bytes.Compare(a.Allocation[:], b.Allocation[:]) < 0
}

func (f *transactionFixture) observe() error {
	counts := make(map[fsapi.TransactionID]map[fsapi.UUID]uint64)
	for id, v := range f.apply.state.Counts {
		counts[id] = maps.Clone(v)
	}
	observation := TransactionObservation{View: cloneTransactionView(f.apply.published), Frames: f.engine.Retained(), Applies: counts, Gated: f.gated, Ordinal: uint64(len(f.observations) + 1)}
	if len(f.observations) > 0 {
		if err := checkTransactionPins(f.observations[len(f.observations)-1].Frames, observation.Frames); err != nil {
			return &CheckFailure{Cause: err}
		}
	}
	f.observations = append(f.observations, observation)
	if err := CheckTransactions(f.baseline, f.history, []TransactionObservation{observation}); err != nil {
		return &CheckFailure{Cause: err}
	}
	return nil
}
func (f *transactionFixture) restart() error {
	if err := f.engine.Close(); err != nil {
		return err
	}
	f.engine = nil
	f.apply.published = TransactionView{}
	f.visibility = 0
	f.gated = true
	if err := f.rig.nodes[0].Crash(); err != nil {
		return err
	}
	if err := f.rig.nodes[0].Restart(f.rig.ctx); err != nil {
		return err
	}
	disk, err := f.rig.nodes[0].runtime.Disk("transactions")
	if err != nil {
		return err
	}
	f.disks[0] = disk
	for i := range f.options.Groups {
		f.options.Groups[i].Backend = disk
	}
	// Creator incarnation is immutable on this reopen. No serving takeover is
	// claimed: installed synthetic authority remains gated until a separate fence.
	if err := f.apply.load(f.rig.ctx); err != nil {
		return err
	}
	if err := f.apply.loadPublication(f.rig.ctx); err != nil {
		return err
	}
	f.engine, err = transactions.Open(f.rig.ctx, f.options)
	if err != nil {
		return err
	}
	if err := f.engine.Reconcile(f.rig.ctx); err != nil {
		return err
	}
	return f.reconstructPublication()
}
func (f *transactionFixture) run(action transactionAction) error {
	p, err := f.start(action.Input)
	if err != nil {
		return err
	}
	steps := f.steps(p)
	for i, step := range steps {
		if action.Fault == "receipt" && i == 9 {
			f.protector.failAfterAccept = true
		}
		if action.Fault == "barrier" && i == 12 {
			f.apply.failBarrier = true
		}
		err := step.run()
		if err != nil {
			if action.Fault == "receipt" && i == 9 || action.Fault == "barrier" && i == 12 {
				f.apply.failBarrier = false
				if err := f.engine.Reconcile(f.rig.ctx); err != nil {
					return fmt.Errorf("fault reconcile: %w", err)
				}
				if err := step.run(); err != nil {
					return fmt.Errorf("fault retry: %w", err)
				}
			} else {
				return fmt.Errorf("%s: %w", step.name, err)
			}
		}
		if err := f.observe(); err != nil {
			return err
		}
		if action.Fault == "crash" && i == action.Boundary {
			if err := f.restart(); err != nil {
				return err
			}
			if err := f.observe(); err != nil {
				return err
			}
		}
	}
	status, err := f.engine.GetStatus(f.rig.ctx, p.input.ID)
	if err != nil || status.Result == nil {
		return fmt.Errorf("terminal status: %v", err)
	}
	invocation := &f.history[len(f.history)-1]
	invocation.Completion = "committed"
	invocation.CompletedAt = uint64(len(f.observations) + 1)
	invocation.Reply = status.Result
	if action.Fault == "reply" {
		invocation.Completion = "uncertain"
		invocation.Reply = nil
		f.history[len(f.history)-1].ReplyLost = true
		if err := f.restart(); err != nil {
			return err
		}
	}
	return f.observe()
}

func TestTransactionHistoryRealEngine(t *testing.T) {
	for _, fault := range []string{"none", "receipt", "barrier", "reply"} {
		t.Run(fault, func(t *testing.T) {
			f := newTransactionFixture(t, 1)
			input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "moved"}
			if err := f.run(transactionAction{Input: input, Fault: fault}); err != nil {
				t.Fatal(err)
			}
			status, err := f.engine.GetStatus(f.rig.ctx, input.ID)
			if err != nil || status.Result == nil {
				t.Fatalf("status %+v %v", status, err)
			}
			if _, err := f.apply.resolve(txUUID(3), "file"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rename left source")
			}
		})
	}
}

func TestTransactionPartialPhases(t *testing.T) {
	for boundary := 0; boundary < 15; boundary++ {
		t.Run(fmt.Sprint(boundary), func(t *testing.T) {
			f := newTransactionFixture(t, 2)
			input := TransactionInput{ID: f.id(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
			if err := f.run(transactionAction{Input: input, Fault: "crash", Boundary: boundary}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTransactionAliasHomeLifetime(t *testing.T) {
	f := newTransactionFixture(t, 3)
	inputs := []TransactionInput{
		{Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"},
		{Kind: "write", SourceVolume: txUUID(4), Source: "alias", Data: []byte("updated through foreign alias")},
		{Kind: "unlink", SourceVolume: txUUID(3), Source: "file"},
		{Kind: "unlink", SourceVolume: txUUID(4), Source: "alias"},
	}
	for i, input := range inputs {
		input.ID = f.id(uint64(i + 1))
		if err := f.run(transactionAction{Input: input, Fault: "crash", Boundary: 11}); err != nil {
			t.Fatal(err)
		}
	}
	object := f.apply.published.Objects[txObject(3, 50)]
	if object.Nlink != 0 || object.Pins != 1 || object.Hash != sha256.Sum256(inputs[1].Data) {
		t.Fatalf("home retained for original handle: %+v", object)
	}
	// There is no verified complete replacement/GC implementation. A request to
	// reclaim/retire the home is blocked while the durable synthetic pin remains.
	if err := f.reclaim(txObject(3, 50)); !errors.Is(err, transactions.ErrEvidence) {
		t.Fatalf("premature home reclaim: %v", err)
	}
	if err := f.engine.Reconcile(f.rig.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
}
func (f *transactionFixture) reclaim(id record.ObjectID) error {
	o := f.apply.state.Objects[id]
	if o.Metadata.Nlink != 0 || o.Pins != 0 || len(f.engine.Retained()) > 0 {
		return transactions.ErrEvidence
	}
	delete(f.apply.state.Objects, id)
	return nil
}

func TestTransactionPartialAffinityAndAnchorRetries(t *testing.T) {
	f := newTransactionFixture(t, 4)
	input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "moved"}
	p, err := f.start(input)
	if err != nil {
		t.Fatal(err)
	}
	steps := f.steps(p)
	for _, s := range steps[:2] {
		if err := s.run(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.resolve(p); err != nil {
		t.Fatal(err)
	}
	var authorities []record.Authority
	for _, g := range f.options.Groups {
		if g.Stream.Volume != txUUID(1) {
			authorities = append(authorities, g.Binding.Authority)
		}
	}
	f.manifest = record.Affinity{Closure: txUUID(60), ManifestVersion: 1, Members: []record.UUID{txUUID(3), txUUID(4)}, Authorities: authorities}
	entry, err := f.propose(txUUID(3), f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	f.affinity = record.AffinityRef{Closure: txUUID(60), ManifestVersion: 1, ManifestSHA256: sha256.Sum256(entry.Bytes[144 : len(entry.Bytes)-4])}
	if err := f.restart(); err != nil {
		t.Fatal(err)
	}
	if err := steps[3].run(); !errors.Is(err, transactions.ErrEvidence) {
		t.Fatalf("partial join admitted: %v", err)
	}
	if err := f.join(false); err != nil {
		t.Fatal(err)
	}
	for _, s := range steps[3:] {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
	status, err := f.engine.GetStatus(f.rig.ctx, input.ID)
	if err != nil || status.Result == nil {
		t.Fatalf("terminal: %v", err)
	}
	f.history[0].Completion = "committed"
	f.history[0].CompletedAt = 1
	f.history[0].Reply = status.Result
	before := f.apply.view(f.visibility)
	if err := f.join(true); err != nil {
		t.Fatal(err)
	}
	if f.manifest.Members[0] == p.binding.Coordinator {
		t.Fatal("merge did not change current coordinator")
	}
	// Retry the exact claim/bind/result after source removal and a lower-UUID
	// closure merge. No pathname resolution is performed by these engine calls.
	for _, index := range []int{1, 3, 14} {
		if err := steps[index].run(); err != nil {
			t.Fatalf("retry %s: %v", steps[index].name, err)
		}
	}
	if !reflect.DeepEqual(before, f.apply.view(f.visibility)) {
		t.Fatal("retry changed namespace")
	}
	status, err = f.engine.GetStatus(f.rig.ctx, input.ID)
	if err != nil || status.Binding.Coordinator != txUUID(3) {
		t.Fatalf("immutable coordinator: %+v %v", status, err)
	}
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	// A conflicting request aimed at the other owner still meets this anchor.
	// A nonanchor proposal cannot reserve an identity or create effects.
	conflict := *p.document.Original
	conflict.Source.Volume = txUUID(4)
	conflict.Source.Components = []string{"other"}
	conflict.Destination.Volume = txUUID(1)
	conflict.Destination.Components = []string{"disjoint"}
	canonical, err := record.EncodeOriginal(conflict)
	if err != nil {
		t.Fatal(err)
	}
	claim := record.RequestClaim{Document: record.RequestDocument{Class: 1, Original: &conflict}, Digest: sha256.Sum256(canonical)}
	if _, err := f.propose(txUUID(1), claim); err == nil {
		t.Fatal("nonanchor claim admitted")
	}
	if _, err := f.propose(txUUID(3), claim); !errors.Is(err, transactions.ErrConflict) {
		t.Fatalf("digest conflict: %v", err)
	}
	// Retirement changes admission, never the pinned home/history obligations.
	retirement, err := f.propose(txUUID(3), record.SessionRetire{Principal: input.ID.Session.Principal, Anchor: txUUID(3), Nonce: input.ID.Session.Nonce, NewFloor: 1, TerminalResults: []record.FrameRef{p.result.Reference}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.propose(txUUID(3), record.RequestClaim{Document: p.document, Digest: p.digest}); !errors.Is(err, transactions.ErrRetired) {
		t.Fatalf("retired retry: %v", err)
	}
	if _, err := f.propose(txUUID(3), record.SessionClose{Principal: input.ID.Session.Principal, Anchor: txUUID(3), Nonce: input.ID.Session.Nonce, Floor: 1, Retirement: &retirement.Reference}); err != nil {
		t.Fatal(err)
	}
	if err := steps[0].run(); !errors.Is(err, transactions.ErrRetired) {
		t.Fatalf("closed registration: %v", err)
	}
}

func TestTransactionDirectoryAndAuthorization(t *testing.T) {
	f := newTransactionFixture(t, 5)
	rejected := []TransactionInput{
		{Kind: "rename", SourceVolume: txUUID(3), Source: "dir", DestinationVolume: txUUID(3), Destination: "dir/child/cycle"},
		{Kind: "link", SourceVolume: txUUID(3), Source: "dir", DestinationVolume: txUUID(4), Destination: "directory-link"},
		{Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "denied", Denied: true},
	}
	for i, input := range rejected {
		input.ID = f.id(uint64(i + 1))
		p, err := f.start(input)
		if err != nil {
			t.Fatal(err)
		}
		steps := f.steps(p)
		for _, s := range steps[:2] {
			if err := s.run(); err != nil {
				t.Fatal(err)
			}
		}
		if err := steps[2].run(); err == nil {
			t.Fatal("preflight unexpectedly succeeded")
		}
		entry, err := f.propose(input.ID.Session.Anchor, record.RequestResult{Request: input.ID, Digest: p.digest, Claim: p.claim.Reference, Result: record.TerminalResult{Status: 3, Code: 1}})
		if err != nil {
			t.Fatal(err)
		}
		_ = entry
		f.history[len(f.history)-1].Completion = "rejected"
		f.history[len(f.history)-1].CompletedAt = uint64(len(f.observations) + 1)
		result := entry.Record.Payload.(record.RequestResult).Result
		f.history[len(f.history)-1].Reply = &result
		if err := f.observe(); err != nil {
			t.Fatal(err)
		}
	}
	moves := []TransactionInput{{Kind: "rename", SourceVolume: txUUID(3), Source: "dir/child", DestinationVolume: txUUID(4), Destination: "child"}, {Kind: "rename", SourceVolume: txUUID(3), Source: "dir", DestinationVolume: txUUID(4), Destination: "child/parent"}}
	for i, input := range moves {
		input.ID = f.id(uint64(i + 4))
		if err := f.run(transactionAction{Input: input, Fault: "crash", Boundary: 10}); err != nil {
			t.Fatal(err)
		}
	}
	cycle := TransactionInput{ID: f.id(6), Kind: "rename", SourceVolume: txUUID(4), Source: "child", DestinationVolume: txUUID(4), Destination: "child/parent/cycle"}
	p, err := f.start(cycle)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.resolve(p); err == nil {
		t.Fatal("opposing directory move created cycle")
	}
	if f.apply.published.Objects[txObject(3, 52)].Parent != txObject(3, 53) {
		t.Fatal("moved directory lost stable home/parent")
	}
}

func TestTransactionPartialPrepareAbort(t *testing.T) {
	f := newTransactionFixture(t, 20)
	input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "never-visible"}
	p, err := f.start(input)
	if err != nil {
		t.Fatal(err)
	}
	steps := f.steps(p)
	for _, s := range steps[:8] {
		if err := s.run(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.decide(p); !errors.Is(err, transactions.ErrEvidence) {
		t.Fatalf("partial prepares committed: %v", err)
	}
	status, err := f.engine.GetStatus(f.rig.ctx, input.ID)
	if err != nil || status.Result != nil {
		t.Fatalf("timeout invented terminal outcome: %+v %v", status, err)
	}
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	p.decision, err = f.engine.Cancel(f.rig.ctx, input.ID, 15)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.restart(); err != nil {
		t.Fatal(err)
	}
	accept, err := f.engine.Acceptance(p.decision.Reference)
	if err != nil {
		t.Fatal(err)
	}
	d := p.decision.Record.Payload.(record.Decision)
	var refs []record.FrameRef
	var vector record.Vector
	for _, id := range p.binding.Participants {
		var position record.Position
		for _, entry := range f.engine.Retained() {
			r := entry.Reference
			if r.StreamKind == 1 && r.Position.Volume == id && r.Position.Sequence > position.Sequence {
				position = r.Position
			}
		}
		install, err := f.propose(id, record.Install{Request: input.ID, Digest: p.digest, Binding: p.binding, Decision: record.DecisionRef{ID: d.ID, Decision: p.decision.Reference}, Outcome: 2, Applied: position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}})
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, install.Reference)
		vector = append(vector, install.Reference.Position)
	}
	transactions.SortReferences(refs)
	ref := record.DecisionRef{ID: d.ID, Decision: p.decision.Reference}
	result := d.Result
	result.Decision = &ref
	complete, err := f.propose(p.binding.Coordinator, record.Complete{Request: input.ID, Digest: p.digest, Decision: ref, Installs: refs, Installed: vector, Result: result, Visibility: record.Visibility{Affinity: p.binding.Affinity, Generation: 1}})
	if err != nil {
		t.Fatal(err)
	}
	bind := p.bind.Reference
	if _, err := f.propose(input.ID.Session.Anchor, record.RequestResult{Request: input.ID, Digest: p.digest, Claim: p.claim.Reference, Binding: &bind, Result: result, Dependencies: []record.FrameRef{complete.Reference}}); err != nil {
		t.Fatal(err)
	}
	f.gated = false
	f.history[0].Completion = "aborted"
	f.history[0].CompletedAt = uint64(len(f.observations) + 1)
	f.history[0].Reply = &result
	if err := f.restart(); err != nil {
		t.Fatal(err)
	}
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	missing := f.observations[len(f.observations)-1]
	missing.Frames = nil
	if err := CheckTransactions(f.baseline, f.history, []TransactionObservation{missing}); err == nil {
		t.Fatal("lost acknowledged abort passed")
	}
}

func TestTransactionDecisionCapacityAndRevisionFailures(t *testing.T) {
	for _, fault := range []string{"capacity", "stale-redo"} {
		t.Run(fault, func(t *testing.T) {
			f := newTransactionFixture(t, 21)
			input := TransactionInput{ID: f.id(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
			p, err := f.start(input)
			if err != nil {
				t.Fatal(err)
			}
			steps := f.steps(p)
			for _, s := range steps[:10] {
				if err := s.run(); err != nil {
					t.Fatal(err)
				}
			}
			switch fault {
			case "capacity":
				maximum := len(f.engine.Retained())
				if err := f.engine.Close(); err != nil {
					t.Fatal(err)
				}
				f.options.MaximumFrames = maximum
				f.engine, err = transactions.Open(f.rig.ctx, f.options)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.engine.Reconcile(f.rig.ctx); err != nil {
					t.Fatal(err)
				}
				if err := steps[10].run(); !errors.Is(err, transactions.ErrLimit) {
					t.Fatalf("postdecision capacity: %v", err)
				}
			case "stale-redo":
				key := txEntryKey(txRoot(txUUID(4)), "alias")
				entry := f.apply.state.Entries[key]
				entry.Revision++
				f.apply.state.Entries[key] = entry
				// The foreign entry is participant 4 (second install). Participant 3 can
				// apply privately; the exact old-name revision prevents a wrong-object edit.
				if err := steps[10].run(); err != nil {
					t.Fatal(err)
				}
				if err := steps[11].run(); !errors.Is(err, transactions.ErrEvidence) {
					t.Fatalf("stale prepare applied: %v", err)
				}
			}
			cancelled, err := f.engine.Cancel(f.rig.ctx, input.ID, 15)
			if err != nil || cancelled.Record.Payload.(record.Decision).Outcome != 1 {
				t.Fatalf("COMMIT reversed to free resources: %v", err)
			}
			if err := f.observe(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTransactionConflictingAnchorClaimsAndPlacement(t *testing.T) {
	f := newTransactionFixture(t, 22)
	input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "moved"}
	p, err := f.start(input)
	if err != nil {
		t.Fatal(err)
	}
	steps := f.steps(p)
	for _, s := range steps[:2] {
		if err := s.run(); err != nil {
			t.Fatal(err)
		}
	}
	competing := *p.document.Original
	competing.Source.Volume = txUUID(4)
	competing.Source.Components = []string{"other"}
	competing.Destination.Volume = txUUID(1)
	competing.Destination.Components = []string{"disjoint"}
	canonical, err := record.EncodeOriginal(competing)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	if _, err := f.propose(txUUID(1), record.RequestClaim{Document: record.RequestDocument{Class: 1, Original: &competing}, Digest: digest}); err == nil {
		t.Fatal("disjoint nonowner admitted anchor ID")
	}
	if _, err := f.propose(txUUID(3), record.RequestClaim{Document: record.RequestDocument{Class: 1, Original: &competing}, Digest: digest}); !errors.Is(err, transactions.ErrConflict) {
		t.Fatalf("simultaneous digest did not meet one claim: %v", err)
	}
	f.history = append(f.history, TransactionInvocation{Input: TransactionInput{ID: input.ID, Kind: "rename", SourceVolume: txUUID(4), Source: "other", DestinationVolume: txUUID(1), Destination: "disjoint"}, Canonical: canonical, Digest: digest, Completion: "conflict", CompletedAt: 1})
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	// Placement is checked from the full vector; a conflicting owner cannot
	// establish even an affinity manifest, let alone namespace effects.
	manifest := record.Affinity{Closure: txUUID(80), ManifestVersion: 1, Members: []record.UUID{txUUID(3), txUUID(4)}}
	for _, g := range f.options.Groups {
		if g.Stream.Volume != txUUID(1) {
			manifest.Authorities = append(manifest.Authorities, g.Binding.Authority)
		}
	}
	manifest.Authorities[1].Owner = f.rig.nodes[2].cfg.Node
	if _, err := f.propose(txUUID(3), manifest); err == nil {
		t.Fatal("split owner manifest admitted")
	}
	for _, s := range steps[2:] {
		if err := s.run(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
	}
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
}

func TestTransactionSymlinkAndEmptySourceRoot(t *testing.T) {
	f := newTransactionFixture(t, 23)
	inputs := []TransactionInput{
		{Kind: "rename", SourceVolume: txUUID(3), Source: "sym", DestinationVolume: txUUID(4), Destination: "moved-symlink"},
		{Kind: "unlink", SourceVolume: txUUID(3), Source: "file"},
		{Kind: "rename", SourceVolume: txUUID(3), Source: "dir", DestinationVolume: txUUID(4), Destination: "subtree"},
	}
	for i, input := range inputs {
		input.ID = f.id(uint64(i + 1))
		if err := f.run(transactionAction{Input: input, Fault: "reply"}); err != nil {
			t.Fatal(err)
		}
	}
	for name := range f.apply.published.Names {
		if strings.HasPrefix(name, txUUID(3).String()+"/") {
			t.Fatalf("source namespace not empty: %s", name)
		}
	}
	directory := f.apply.published.Names[txUUID(4).String()+"/subtree"]
	if directory.Home != txUUID(3) || f.apply.published.Objects[txObject(3, 54)].Hash != sha256.Sum256([]byte("../file")) {
		t.Fatal("move changed original home or symlink text")
	}
	if err := f.reclaim(directory); !errors.Is(err, transactions.ErrEvidence) {
		t.Fatalf("empty source retired required home: %v", err)
	}
}

// reconstructPublication reads the actual recovered active/complete graph.
// Incomplete installs retain the last whole durable source behind the gate;
// completed work observes freshly recovered private bytes, never old hashes.
func (f *transactionFixture) reconstructPublication() error {
	affinities := make(map[record.AffinityRef]map[record.UUID]bool)
	members := make(map[record.AffinityRef]int)
	begun := make(map[record.RequestID]bool)
	completed := make(map[record.RequestID]bool)
	generation := uint64(1)
	var latest *record.FrameRef
	for _, entry := range f.engine.Retained() {
		switch p := entry.Record.Payload.(type) {
		case record.Affinity:
			ref := record.AffinityRef{Closure: p.Closure, ManifestVersion: p.ManifestVersion, ManifestSHA256: sha256.Sum256(entry.Bytes[144 : len(entry.Bytes)-4])}
			if affinities[ref] == nil {
				affinities[ref] = make(map[record.UUID]bool)
			}
			affinities[ref][entry.Record.Header.Volume] = true
			members[ref] = len(p.Members)
		case record.Begin:
			begun[p.Binding.Request] = true
		case record.Complete:
			completed[p.Request] = true
			if p.Result.Status == 1 && p.Visibility.Generation >= generation {
				generation = p.Visibility.Generation
				ref := entry.Reference
				latest = &ref
			}
		}
	}
	for ref, fanout := range affinities {
		if len(fanout) != members[ref] {
			f.gated = true
			f.visibility = f.apply.published.Generation
			return nil
		}
	}
	for id := range begun {
		if !completed[id] {
			f.gated = true
			f.visibility = f.apply.published.Generation
			return nil
		}
	}
	if err := f.apply.publish(f.rig.ctx, generation, latest); err != nil {
		return err
	}
	f.visibility = generation
	f.gated = false
	return nil
}

func TestTransactionRecoveredBytesAndPartialPublication(t *testing.T) {
	t.Run("corruption-masked-by-old-projection", func(t *testing.T) {
		f := newTransactionFixture(t, 25)
		input := TransactionInput{ID: f.id(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
		if err := f.run(transactionAction{Input: input}); err != nil {
			t.Fatal(err)
		}
		object := f.apply.state.Objects[txObject(3, 50)]
		object.Data = []byte("corrupt-data")
		f.apply.state.Objects[txObject(3, 50)] = object
		if err := f.apply.persist(f.rig.ctx); err != nil {
			t.Fatal(err)
		}
		// This is the old false-pass: the volatile projection still looks correct.
		if err := f.observe(); err != nil {
			t.Fatalf("old projection control: %v", err)
		}
		if err := f.restart(); err != nil {
			t.Fatal(err)
		}
		var invariant *CheckFailure
		if err := f.observe(); !errors.As(err, &invariant) {
			t.Fatalf("recovered object corruption was hidden: %v", err)
		}
	})
	t.Run("partial-install-stays-hidden", func(t *testing.T) {
		f := newTransactionFixture(t, 26)
		input := TransactionInput{ID: f.id(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
		p, err := f.start(input)
		if err != nil {
			t.Fatal(err)
		}
		steps := f.steps(p)
		for _, step := range steps[:11] {
			if err := step.run(); err != nil {
				t.Fatal(err)
			}
		}
		if reflect.DeepEqual(f.apply.view(1), f.baseline) {
			t.Fatal("control did not partially install")
		}
		// Poison every volatile publication field. Recovery must use durable bytes.
		f.apply.published = TransactionView{}
		f.visibility = 99
		f.gated = false
		if err := f.restart(); err != nil {
			t.Fatal(err)
		}
		if !f.gated || f.visibility != 1 || !reflect.DeepEqual(f.apply.published, f.baseline) {
			t.Fatal("partial recovery exposed private installation")
		}
		if err := f.observe(); err != nil {
			t.Fatal(err)
		}
		for _, step := range steps[11:] {
			if err := step.run(); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.observe(); err != nil {
			t.Fatal(err)
		}
	})
}
