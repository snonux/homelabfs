package transactions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
)

func (f *fixture) finishAbort() {
	f.t.Helper()
	d := f.decision.Record.Payload.(record.Decision)
	accept, err := f.engine.Acceptance(f.decision.Reference)
	if err != nil {
		f.t.Fatal(err)
	}
	var installs []record.FrameRef
	var vector record.Vector
	for _, id := range f.binding.Participants {
		g := f.engine.groups[id]
		position := record.Position{Volume: id, Generation: g.Stream.Generation, Configuration: g.Stream.Configuration, Epoch: g.Stream.Epoch}
		for _, entry := range f.engine.entries {
			if entryStream(entry) == streamKey(g.Stream) && entry.Reference.Position.Sequence > position.Sequence {
				position = entry.Reference.Position
			}
		}
		entry := f.propose(id, record.Install{Request: f.id, Digest: f.digest, Binding: f.binding, Decision: record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}, Outcome: 2, Applied: position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}})
		installs = append(installs, entry.Reference)
		vector = append(vector, entry.Reference.Position)
	}
	SortReferences(installs)
	ref := record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}
	result := d.Result
	result.Decision = &ref
	visibility := record.Visibility{Affinity: f.binding.Affinity, Generation: f.engine.state.visibility[f.binding.Affinity.Closure]}
	f.complete = f.propose(f.binding.Coordinator, record.Complete{Request: f.id, Digest: f.digest, Decision: ref, Installs: installs, Installed: vector, Result: result, Visibility: visibility})
	bind := f.bind.Reference
	f.propose(f.id.Session.Anchor, record.RequestResult{Request: f.id, Digest: f.digest, Claim: f.claim.Reference, Binding: &bind, Result: result, Dependencies: []record.FrameRef{f.complete.Reference}})
}

func TestProtectedAbortCleansPartialAdmissionAndRetainsOutcome(t *testing.T) {
	for _, phase := range []string{"begin", "partial_anchor", "partial_prepare", "all_prepared"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:5] {
				step()
			}
			if phase == "partial_anchor" {
				f.propose(uuid(1), record.TxAnchor{Request: f.id, Digest: f.digest, Binding: f.binding, Begin: f.begin.Reference, CoordinatorTerm: 1})
			}
			if phase == "partial_prepare" || phase == "all_prepared" {
				steps[5]()
				if phase == "all_prepared" {
					steps[6]()
				} else {
					m := record.Mutation{Intent: f.binding.Intent, Redo: record.Redo{Objects: f.binding.Intent.SelectedMetadata}}
					f.propose(uuid(1), record.Prepared{Request: f.id, Digest: f.digest, Begin: f.begin.Reference, Binding: f.binding, Term: 1, Operation: m, RedoSHA256: redoHash(t, f.engine.groups[uuid(1)], m)})
				}
			}
			for _, id := range f.binding.Participants {
				name := fmt.Sprintf("staging-%s-%s-%d", id, f.id.Session.Nonce, f.id.Number)
				file, err := f.fs.Open(context.Background(), name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteAt(context.Background(), []byte("private staging"), 0); err != nil {
					t.Fatal(err)
				}
				if err := file.Sync(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				if err := f.fs.SyncDir(context.Background(), "."); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			f.decision, err = f.engine.Cancel(context.Background(), f.id, 15)
			if err != nil {
				t.Fatal(err)
			}
			f.restart()
			f.finishAbort()
			f.restart()
			status, err := f.engine.GetStatus(context.Background(), f.id)
			if err != nil || status.Result == nil || status.Result.Status != 2 || status.Result.Code != 15 {
				t.Fatalf("abort result %+v %v", status, err)
			}
			if f.engine.state.visibility[f.binding.Affinity.Closure] != 1 {
				t.Fatal("abort advanced visibility")
			}
			for _, id := range f.binding.Participants {
				name := fmt.Sprintf("staging-%s-%s-%d", id, f.id.Session.Nonce, f.id.Number)
				if _, err := f.fs.Open(context.Background(), name, os.O_RDONLY, 0); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("abort did not durably clean staging")
				}
			}
			if _, err := f.fs.Open(context.Background(), "applied-"+uuid(1).String(), os.O_RDONLY, 0); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("abort applied namespace/data redo")
			}
		})
	}
}

func TestFencedRecoveryAbortsIncompleteBeginFanout(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:5] {
		step()
	}
	f.propose(uuid(1), record.TxAnchor{Request: f.id, Digest: f.digest, Binding: f.binding, Begin: f.begin.Reference, CoordinatorTerm: 1})
	v := f.successor(false)
	selection := f.recoverSelection(v, true, true)
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := f.options.Groups[0].Binding.Authority
	f.decision = f.propose(a.Volume, record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: f.id, Term: a.CoordinatorTerm}, Digest: f.digest, Begin: f.begin.Reference, Outcome: 2, Result: record.TerminalResult{Status: 2, Code: 15}, Authority: a, Recovery: &selection.Reference})
	f.finishAbort()
	f.restart()
	status, err := f.engine.GetStatus(context.Background(), f.id)
	if err != nil || status.Result == nil || status.Result.Status != 2 {
		t.Fatalf("incomplete fanout recovery %+v %v", status, err)
	}
}
