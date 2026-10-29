package transactions

import (
	"context"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func TestRecoveryRejectsIncompleteAndContradictoryProof(t *testing.T) {
	for _, name := range []string{"promise_majority", "report_majority", "fence", "wrong_fence_type", "successor", "operation", "absence_hides_accepted", "binding", "noncontinuous_absence"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			count := 8
			if name == "noncontinuous_absence" {
				count = 7
			}
			for _, step := range steps[:count] {
				step()
			}
			v := f.successor(count == 8)
			selection := f.recoveryProposal(v, name == "noncontinuous_absence", name != "noncontinuous_absence")
			switch name {
			case "promise_majority":
				selection.Promises = selection.Promises[1:]
			case "report_majority":
				selection.Reports = selection.Reports[1:]
			case "fence":
				selection.Fence = nil
			case "wrong_fence_type":
				selection.Fence = []record.FrameRef{f.begin.Reference}
			case "successor":
				selection.NewAuthorities[0].OwnerIncarnation = uuid(100)
			case "operation":
				selection.Operation = uuid(100)
			case "absence_hides_accepted":
				selection.Selected = nil
				selection.AbsenceAbort = true
			case "binding":
				copy := *selection.Binding
				copy.RequestDigest[0]++
				selection.Binding = &copy
			}
			before := len(f.engine.Retained())
			if _, err := f.engine.ProposeRecovery(context.Background(), f.options.Groups[0].Stream, selection); err == nil {
				t.Fatal("bad recovery proof accepted")
			}
			if len(f.engine.Retained()) != before {
				t.Fatal("bad selection persisted")
			}
			if r := f.engine.state.requests[f.id]; r.selection != nil {
				t.Fatal("rejected proof changed selected state")
			}
		})
	}
}

func TestCrashBeforeRecoverySelectionPreservesPromisesAndCandidate(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:8] {
		step()
	}
	original := cloneEntry(f.decision)
	v := f.successor(true)
	selection := f.recoveryProposal(v, false, true)
	before := len(f.engine.Retained())
	f.restart()
	if len(f.engine.Retained()) != before {
		t.Fatal("crash lost durable recovery evidence")
	}
	if f.engine.state.promises[uuid(1)] != 2 || f.engine.state.promises[uuid(2)] != 2 {
		t.Fatal("durable promise floors lost")
	}
	selected, err := f.engine.ProposeRecovery(context.Background(), f.options.Groups[0].Stream, selection)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Record.Payload.(record.RecoverySelect).Selected == nil || !sameRef(*selected.Record.Payload.(record.RecoverySelect).Selected, original.Reference) {
		t.Fatal("selected candidate changed")
	}
	f.restart()
}

func TestMissingPredecessorHistoryCannotBecomeAbsence(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:8] {
		step()
	}
	v := f.successor(true)
	f.recoverSelection(v, false, true)
	if err := f.engine.Close(); err != nil {
		t.Fatal(err)
	}
	for _, writer := range f.engine.recoveryWriters {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	g := f.options.HistoryGroups[0]
	recovery, err := segmentRecovery(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range recovery {
		if err := g.Backend.Unlink(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Backend.SyncDir(context.Background(), g.Stream.Directory()); err != nil {
		t.Fatal(err)
	}
	f.fs.Crash()
	engine, err := Open(context.Background(), f.options)
	if engine != nil {
		_ = engine.Close()
	}
	if !errors.Is(err, ErrEvidence) {
		t.Fatalf("missing predecessor admitted %v", err)
	}
}

func segmentRecovery(ctx context.Context, g Group) ([]string, error) {
	r, err := segment.Recover(ctx, g.Backend, g.Stream, g.Writer.Recovery)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, summary := range r.Segments {
		names = append(names, summary.Name)
	}
	return names, nil
}
