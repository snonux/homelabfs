package transactions

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func thirdGroup(t *testing.T, f *fixture) {
	t.Helper()
	if err := f.engine.Close(); err != nil {
		t.Fatal(err)
	}
	g := f.options.Groups[1]
	g.Stream.Volume = uuid(3)
	g.Binding.Authority.Volume = uuid(3)
	mkdir(t, f.fs, g.Stream.Directory())
	f.options.Groups = append(f.options.Groups, g)
	var err error
	f.engine, err = Open(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
}

func oldPairTransaction(t *testing.T, f *fixture) (record.AffinityRef, segment.Entry, segment.Entry) {
	t.Helper()
	steps := f.steps()
	steps[0]()
	steps[1]()
	authorities := []record.Authority{f.options.Groups[0].Binding.Authority, f.options.Groups[1].Binding.Authority}
	manifest := record.Affinity{Closure: uuid(60), ManifestVersion: 4, Members: []record.UUID{uuid(1), uuid(2)}, Authorities: authorities}
	first := f.propose(uuid(1), manifest)
	f.propose(uuid(2), manifest)
	ref := affinityRef(manifest, first.Bytes)
	f.binding = record.Binding{Request: f.id, RequestDigest: f.digest, Affinity: ref, Coordinator: uuid(1), Intent: f.document.Internal.Intent, Participants: manifest.Members, Authorities: authorities}
	f.bind = f.propose(uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: f.binding})
	oldBind := cloneEntry(f.bind)
	for _, step := range steps[4:] {
		step()
	}
	return ref, oldBind, cloneEntry(*f.engine.state.requests[f.id].result)
}

func TestFreshBindingUsesDiscoveredGrownClosure(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "partial"}[partial], func(t *testing.T) {
			f := newFixture(t)
			thirdGroup(t, f)
			oldRef, oldBind, oldResult := oldPairTransaction(t, f)
			third := record.Affinity{Closure: uuid(80), ManifestVersion: 1, Members: []record.UUID{uuid(3)}, Authorities: []record.Authority{f.options.Groups[2].Binding.Authority}}
			thirdEntry := f.propose(uuid(3), third)
			var authorities []record.Authority
			for _, g := range f.options.Groups {
				authorities = append(authorities, g.Binding.Authority)
			}
			grown := record.Affinity{Closure: uuid(59), ManifestVersion: 1, Members: []record.UUID{uuid(1), uuid(2), uuid(3)}, Authorities: authorities, Predecessors: []record.AffinityRef{oldRef, affinityRef(third, thirdEntry.Bytes)}}
			grownEntry := f.propose(uuid(3), grown)
			if !partial {
				f.propose(uuid(1), grown)
				f.propose(uuid(2), grown)
			}
			f.restart()
			// Retained old bindings/results survive early discovery from member 3's
			// stream; they are not fresh admission and must not be retroactively widened.
			retry, err := f.engine.Propose(context.Background(), uuid(1), oldBind.Record.Payload)
			if err != nil || !sameRef(retry.Reference, oldBind.Reference) {
				t.Fatalf("old binding retry: %v", err)
			}
			status, err := f.engine.GetStatus(context.Background(), oldResult.Record.Payload.(record.RequestResult).Request)
			if err != nil || status.Result == nil || !equal(*status.Result, oldResult.Record.Payload.(record.RequestResult).Result) {
				t.Fatalf("old result: %+v %v", status, err)
			}
			f.id.Number++
			f.revision = 2
			f.steps()[1]()
			binding := oldBind.Record.Payload.(record.RequestBind).Binding
			binding.Request = f.id
			binding.RequestDigest = f.digest
			binding.Intent = f.document.Internal.Intent
			before, prefix := len(f.engine.Retained()), len(f.protector.retained)
			if _, err := f.engine.Propose(context.Background(), uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: binding}); !errors.Is(err, ErrEvidence) {
				t.Fatalf("old subset admitted after discovery: %v", err)
			}
			if len(f.engine.Retained()) != before || len(f.protector.retained) != prefix || f.engine.state.requests[f.id].bind != nil {
				t.Fatal("superseded binding appended/protected")
			}
			binding.Affinity = affinityRef(grown, grownEntry.Bytes)
			binding.Participants = grown.Members
			binding.Authorities = authorities
			if partial {
				if _, err := f.engine.Propose(context.Background(), uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: binding}); !errors.Is(err, ErrEvidence) {
					t.Fatalf("partial grown fanout admitted: %v", err)
				}
				f.propose(uuid(1), grown)
				f.propose(uuid(2), grown)
			}
			bind := f.propose(uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: binding})
			f.restart()
			stored := f.engine.state.requests[f.id].bind
			if stored == nil || !sameRef(stored.Reference, bind.Reference) || len(stored.Record.Payload.(record.RequestBind).Binding.Participants) != 3 {
				t.Fatal("grown binding not durably retained")
			}
		})
	}
}

func TestAbsenceSelectionRequiresStableBeginOperation(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:7] {
		step()
	}
	v := f.successor(false)
	selection := f.recoveryProposal(v, true, true)
	selection.Operation = uuid(100)
	before, prefix := len(f.engine.Retained()), len(f.protector.retained)
	_, err := f.engine.ProposeRecovery(context.Background(), f.options.Groups[0].Stream, selection)
	if !errors.Is(err, ErrConflict) || len(f.engine.Retained()) != before || len(f.protector.retained) != prefix || f.engine.state.requests[f.id].selection != nil {
		t.Fatalf("wrong absence operation appended/selected: %v", err)
	}
}

func TestFreshBindingWaitsForSameMemberSuccessorTip(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:3] {
		step()
	}
	old := f.binding.Affinity
	manifest := record.Affinity{Closure: old.Closure, ManifestVersion: old.ManifestVersion + 1, Members: f.binding.Participants, Authorities: f.binding.Authorities, Predecessors: []record.AffinityRef{old}}
	entry := f.propose(uuid(2), manifest)
	f.restart()
	for _, partial := range []bool{true, false} {
		if !partial {
			f.propose(uuid(1), manifest)
		}
		before := len(f.engine.Retained())
		if _, err := f.engine.Propose(context.Background(), uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: f.binding}); !errors.Is(err, ErrEvidence) || len(f.engine.Retained()) != before {
			t.Fatalf("superseded same-member affinity admitted (partial=%v): %v", partial, err)
		}
	}
	f.binding.Affinity = affinityRef(manifest, entry.Bytes)
	f.propose(uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: f.binding})
	f.restart()
}

func TestPersistedRecoveredDecisionRejectsChangedOperation(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "absence", true: "selected"}[selected], func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			count := 7
			if selected {
				count = 8
			}
			for _, step := range steps[:count] {
				step()
			}
			v := f.successor(selected)
			selection := f.recoverSelection(v, !selected, true)
			if err := f.engine.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			a := f.options.Groups[0].Binding.Authority
			d := record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: f.id, Term: a.CoordinatorTerm}, Digest: f.digest, Begin: f.begin.Reference, Outcome: 2, Result: record.TerminalResult{Status: 2, Code: 15}, Authority: a, Recovery: &selection.Reference}
			if selected {
				original := f.decision.Record.Payload.(record.Decision)
				d.Outcome = original.Outcome
				d.Prepares = original.Prepares
				d.Result = original.Result
			}
			if err := f.engine.Close(); err != nil {
				t.Fatal(err)
			}
			g := f.options.Groups[0]
			w, err := segment.NewWriter(context.Background(), g.Backend, g.Stream, g.Writer)
			if err != nil {
				t.Fatal(err)
			}
			h := f.header()
			h.Volume = a.Volume
			h.Generation = a.Generation
			h.Configuration = a.Configuration
			h.Epoch = a.Epoch
			h.Incarnation = a.OwnerIncarnation
			h.Operation = uuid(100)
			receipt, err := w.Append(context.Background(), record.Record{Header: h, Payload: d})
			if err != nil {
				t.Fatal(err)
			}
			if err := receipt.Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
			h.Sequence = receipt.Reference.Position.Sequence
			bytes, err := record.Encode(record.Record{Header: h, Payload: d})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := record.Decode(bytes); err != nil {
				t.Fatalf("fixture must be CRC-valid representation: %v", err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			f.fs.Crash()
			engine, err := Open(context.Background(), f.options)
			if engine != nil {
				_ = engine.Close()
			}
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("persisted recovered operation mismatch accepted: %v", err)
			}
		})
	}
}

func installParticipant(t *testing.T, f *fixture, id record.UUID) segment.Entry {
	t.Helper()
	var prepared segment.Entry
	for _, entry := range f.prepares {
		if entry.Record.Header.Volume == id {
			prepared = entry
			break
		}
	}
	p := prepared.Record.Payload.(record.Prepared)
	d := f.decision.Record.Payload.(record.Decision)
	accept, err := f.engine.Acceptance(f.decision.Reference)
	if err != nil {
		t.Fatal(err)
	}
	ref := record.PrepareRef{Participant: id, Prepare: prepared.Reference, RedoSHA256: p.RedoSHA256}
	return f.propose(id, record.Install{Request: f.id, Digest: f.digest, Binding: f.binding, Decision: record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}, Outcome: 1, Prepare: &ref, Redo: &p.Operation.Redo, Applied: prepared.Reference.Position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}})
}

func TestRecoveryReinstallsPartialOldBallotBeforeComplete(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:8] {
		step()
	}
	oldInstall := installParticipant(t, f, uuid(1))
	originalDecision := cloneEntry(f.decision)
	v := f.successor(true)
	selection := f.recoverSelection(v, false, true)
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := f.options.Groups[0].Binding.Authority
	d := originalDecision.Record.Payload.(record.Decision)
	d.ID.Configuration = a.Configuration
	d.ID.Epoch = a.Epoch
	d.ID.Term = a.CoordinatorTerm
	d.Authority = a
	d.Recovery = &selection.Reference
	f.decision = f.propose(uuid(1), d)
	second := installParticipant(t, f, uuid(2))
	mixedRefs := []record.FrameRef{oldInstall.Reference, second.Reference}
	SortReferences(mixedRefs)
	visibility := record.Visibility{Affinity: f.binding.Affinity, Generation: 2}
	barrier := f.propose(uuid(1), record.ViewBarrier{Affinity: f.binding.Affinity, OldVisibility: record.Visibility{Affinity: f.binding.Affinity, Generation: 1}, NewVisibility: visibility, ServingGeneration: uuid(61), Incarnation: a.OwnerIncarnation, Drained: true, Mechanism: uuid(62), Attestation: record.Artifact{Kind: 7, ID: uuid(63)}, Dependencies: mixedRefs})
	ref := record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}
	result := d.Result
	result.Decision = &ref
	result.Visibility = &visibility
	result.Committed = record.Vector{oldInstall.Reference.Position, second.Reference.Position}
	complete := record.Complete{Request: f.id, Digest: f.digest, Decision: ref, Installs: mixedRefs, Installed: result.Committed, Result: result, Visibility: visibility, CacheBarriers: []record.FrameRef{barrier.Reference}}
	before := len(f.engine.Retained())
	_, err := f.engine.Propose(context.Background(), uuid(1), complete)
	if !errors.Is(err, ErrEvidence) || !strings.Contains(err.Error(), "selected ballot participant installs") || len(f.engine.Retained()) != before {
		t.Fatalf("mixed ballot COMPLETE accepted or wrong failure: %v", err)
	}
	f.restart()
	first := installParticipant(t, f, uuid(1))
	retry, err := f.engine.Propose(context.Background(), uuid(1), oldInstall.Record.Payload)
	if err != nil || !sameRef(retry.Reference, oldInstall.Reference) {
		t.Fatalf("old immutable install retry: %v", err)
	}
	changed := first.Record.Payload.(record.Install)
	changed.Dependencies = []record.FrameRef{f.begin.Reference}
	before = len(f.engine.Retained())
	if _, err := f.engine.Propose(context.Background(), uuid(1), changed); !errors.Is(err, ErrConflict) || len(f.engine.Retained()) != before {
		t.Fatalf("same-ballot install changed: %v", err)
	}
	complete.Installs = []record.FrameRef{first.Reference, second.Reference}
	SortReferences(complete.Installs)
	complete.Installed = record.Vector{first.Reference.Position, second.Reference.Position}
	complete.Result.Committed = complete.Installed
	f.complete = f.propose(uuid(1), complete)
	steps[10]()
	f.restart()
	status, err := f.engine.GetStatus(context.Background(), f.id)
	if err != nil || status.Result == nil || status.Result.Decision.ID.Term != 2 {
		t.Fatalf("recovered completion: %+v %v", status, err)
	}
	installs := 0
	foundOld := false
	for _, entry := range f.engine.Retained() {
		if _, ok := entry.Record.Payload.(record.Install); ok {
			installs++
		}
		if sameRef(entry.Reference, oldInstall.Reference) {
			foundOld = bytesEqual(entry.Bytes, oldInstall.Bytes)
		}
	}
	if installs != 3 || !foundOld {
		t.Fatal("old install lost or new ballot duplicated installs")
	}
	file, err := f.fs.Open(context.Background(), "applied-"+uuid(1).String(), os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	bytes := make([]byte, 11)
	if _, err := file.ReadAt(context.Background(), bytes, 0); err != nil || string(bytes) != "exact bytes" {
		t.Fatalf("redo replay changed data: %q %v", bytes, err)
	}
}
