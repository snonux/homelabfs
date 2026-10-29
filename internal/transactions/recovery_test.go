package transactions

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

type testRecovery struct {
	fixture                    *fixture
	recovery                   record.UUID
	authorization, attestation record.Artifact
	reject                     bool
}

var _ RecoveryVerifier = (*testRecovery)(nil)

func (v *testRecovery) Authorize(ctx context.Context, scope RecoveryScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.reject {
		return fmt.Errorf("recovery scope unavailable")
	}
	if len(scope.Successors) != 2 || len(scope.Streams) < 2 {
		return fmt.Errorf("complete recovery scope")
	}
	return nil
}

func (v *testRecovery) Verify(ctx context.Context, r RecoveryRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.reject {
		return fmt.Errorf("control evidence unavailable")
	}
	check := func(artifact record.Artifact) error {
		f, err := v.fixture.fs.Open(ctx, "attestation-"+artifact.ID.String(), os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		bytes := make([]byte, artifact.Length)
		if _, err := f.ReadAt(ctx, bytes, 0); err != nil {
			return err
		}
		if sha256.Sum256(bytes) != artifact.SHA256 {
			return fmt.Errorf("attestation hash")
		}
		return nil
	}
	switch p := r.Candidate.Record.Payload.(type) {
	case record.Promise:
		if p.RecoveryID != v.recovery || p.Authorization != v.authorization {
			return fmt.Errorf("promise control authorization")
		}
		return check(p.Authorization)
	case record.Fence:
		if p.RecoveryID != v.recovery || p.Attestation != v.attestation || p.OldOwner != uuid(10) || p.OldIncarnation != uuid(20) {
			return fmt.Errorf("positive fence domain")
		}
		return check(p.Attestation)
	case record.TailReport:
		if p.RecoveryID != v.recovery {
			return fmt.Errorf("tail recovery identity")
		}
	case record.RecoverySelect:
		if p.RecoveryID != v.recovery || !equal(p.NewAuthorities, r.Successors) {
			return fmt.Errorf("allocated successor vector")
		}
		if p.AbsenceAbort {
			continuous := false
			for _, frame := range r.Frames {
				report, ok := frame.Record.Payload.(record.TailReport)
				if ok && report.Reporter == uuid(11) {
					for _, tail := range report.Tails {
						continuous = continuous || tail.Continuous && len(tail.Missing) == 0
					}
				}
			}
			if !continuous {
				return fmt.Errorf("missing accepted-tail continuity")
			}
		}
		return check(v.attestation)
	}
	// The fixture's historical stream list represents authenticated immutable
	// imports; engine independently verifies creator/stream/authority and graph.
	return nil
}

func (f *fixture) artifact(id byte, text string) record.Artifact {
	f.t.Helper()
	bytes := []byte(text)
	a := record.Artifact{Kind: 6, ID: uuid(id), Length: uint64(len(bytes)), SHA256: sha256.Sum256(bytes)}
	file, err := f.fs.Open(context.Background(), "attestation-"+a.ID.String(), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteAt(context.Background(), bytes, 0); err != nil {
		f.t.Fatal(err)
	}
	if err := file.Sync(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	if err := f.fs.SyncDir(context.Background(), "."); err != nil {
		f.t.Fatal(err)
	}
	return a
}

func (f *fixture) successor(withAcceptance bool) *testRecovery {
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
	v := &testRecovery{fixture: f, recovery: uuid(71), authorization: f.artifact(72, "allocated complete disposable recovery vector"), attestation: f.artifact(73, "positive old owner and cached path fence with relaunch inhibition")}
	f.options.Recovery = v
	f.options.HistoryGroups = append([]Group(nil), f.options.Groups...)
	f.options.EvidenceGroups = nil
	for i, old := range f.options.HistoryGroups {
		a := old.Binding.Authority
		a.Configuration = uuid(70)
		a.Epoch = 2
		a.CoordinatorTerm = 2
		a.Owner = uuid(11)
		a.Partner = uuid(12)
		a.OwnerIncarnation = uuid(22)
		a.PartnerIncarnation = uuid(23)
		g := old
		g.Stream.Configuration = a.Configuration
		g.Stream.Epoch = a.Epoch
		g.Binding = Binding{Session: uuid(74), Connection: uuid(75), Authority: a}
		g.Writer = segment.Options{MaximumBytes: 1 << 20, NewSegment: f.nextID, Structural: func(uint64) record.Header { h := f.header(); h.Incarnation = uuid(22); return h }}
		mkdir(f.t, f.fs, g.Stream.Directory())
		f.options.Groups[i] = g
		for _, reporter := range []record.UUID{uuid(11), uuid(12)} {
			inc := uuid(24)
			if reporter == uuid(11) {
				inc = uuid(21)
			}
			stream := old.Stream
			stream.Kind = 2
			stream.Reporter = reporter
			stream.ReporterIncarnation = inc
			mkdir(f.t, f.fs, stream.Directory())
			evidence := old
			evidence.Stream = stream
			evidence.Writer = segment.Options{MaximumBytes: 1 << 20, NewSegment: f.nextID, Structural: func(uint64) record.Header { h := f.header(); h.Incarnation = inc; return h }}
			f.options.EvidenceGroups = append(f.options.EvidenceGroups, evidence)
		}
	}
	if !withAcceptance {
		f.protector.accepted = make(map[key]segment.Entry)
	}
	var err error
	f.engine, err = Open(context.Background(), f.options)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *fixture) recoverSelection(v *testRecovery, absence bool, continuous bool) segment.Entry {
	selection := f.recoveryProposal(v, absence, continuous)
	entry, err := f.engine.ProposeRecovery(context.Background(), f.options.Groups[0].Stream, selection)
	if err != nil {
		f.t.Fatal(err)
	}
	return entry
}

func (f *fixture) recoveryProposal(v *testRecovery, absence bool, continuous bool) record.RecoverySelect {
	f.t.Helper()
	ctx := context.Background()
	var promises, reports []record.FrameRef
	for _, g := range f.options.EvidenceGroups {
		old := g.Binding.Authority
		position := record.Position{Volume: old.Volume, Generation: old.Generation, Configuration: old.Configuration, Epoch: old.Epoch}
		for _, entry := range f.engine.entries {
			if entryStream(entry) == streamKey(f.options.HistoryGroups[groupIndex(old.Volume)].Stream) && entry.Reference.Position.Sequence > position.Sequence {
				position = entry.Reference.Position
			}
		}
		tail := record.Tail{Received: position, Durable: position, Accepted: position, Applied: position, Incarnation: g.Stream.ReporterIncarnation, Continuous: continuous}
		var accepts []record.FrameRef
		for _, entry := range f.engine.entries {
			if a, ok := entry.Record.Payload.(record.Accept); ok && a.Authority.Volume == old.Volume && a.Reporter == g.Stream.Reporter {
				accepts = append(accepts, entry.Reference)
			}
		}
		SortReferences(accepts)
		promise := record.Promise{RecoveryID: v.recovery, Authority: old, PromisedEpoch: 2, PromisedTerm: 2, Reporter: g.Stream.Reporter, ReporterIncarnation: g.Stream.ReporterIncarnation, Tails: []record.Tail{tail}, PriorAcceptances: accepts, Authorization: v.authorization}
		entry, err := f.engine.ProposeRecovery(ctx, g.Stream, promise)
		if err != nil {
			f.t.Fatal(err)
		}
		promises = append(promises, entry.Reference)
		report := record.TailReport{RecoveryID: v.recovery, Reporter: g.Stream.Reporter, ReporterIncarnation: g.Stream.ReporterIncarnation, Promises: []record.FrameRef{entry.Reference}, Tails: []record.Tail{tail}, Dependencies: accepts}
		if old.Volume == uuid(1) && len(f.decision.Bytes) > 0 {
			report.Candidates = []record.FrameRef{f.decision.Reference}
		}
		entry, err = f.engine.ProposeRecovery(ctx, g.Stream, report)
		if err != nil {
			f.t.Fatal(err)
		}
		reports = append(reports, entry.Reference)
	}
	SortReferences(promises)
	SortReferences(reports)
	fence := record.Fence{RecoveryID: v.recovery, Authorities: f.binding.Authorities, OldOwner: uuid(10), OldIncarnation: uuid(20), ServingGenerations: []record.UUID{uuid(61)}, Views: []record.UUID{uuid(51)}, Mechanism: uuid(76), Attestation: v.attestation, Dependencies: promises}
	entry, err := f.engine.ProposeRecovery(ctx, f.options.Groups[0].Stream, fence)
	if err != nil {
		f.t.Fatal(err)
	}
	var authorities []record.Authority
	for _, g := range f.options.Groups {
		authorities = append(authorities, g.Binding.Authority)
	}
	selection := record.RecoverySelect{RecoveryID: v.recovery, SlotKind: 2, Operation: f.begin.Record.Header.Operation, Binding: &f.binding, NewAuthorities: authorities, Promises: promises, Reports: reports, AbsenceAbort: absence, Fence: []record.FrameRef{entry.Reference}}
	if !absence {
		selection.Selected = &f.decision.Reference
		selection.Operation = f.decision.Record.Header.Operation
	}
	return selection
}

func groupIndex(volume record.UUID) int {
	if volume == uuid(1) {
		return 0
	}
	return 1
}

func TestRecoveryPreservesOriginalCandidateAndNewBallot(t *testing.T) {
	for _, crash := range []string{"selection", "decision", "install", "result"} {
		t.Run(crash, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:8] {
				step()
			}
			original := cloneEntry(f.decision)
			v := f.successor(true)
			selection := f.recoverSelection(v, false, true)
			if crash == "selection" {
				f.restart()
			} else if err := f.engine.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			d := original.Record.Payload.(record.Decision)
			a := f.options.Groups[0].Binding.Authority
			d.ID.Generation = a.Generation
			d.ID.Configuration = a.Configuration
			d.ID.Epoch = a.Epoch
			d.ID.Term = a.CoordinatorTerm
			d.Authority = a
			d.Recovery = &selection.Reference
			f.decision = f.propose(uuid(1), d)
			if f.decision.Reference == original.Reference || f.decision.Record.Header.Operation != original.Record.Header.Operation {
				t.Fatal("ballot restamped original candidate identity")
			}
			if crash == "decision" {
				f.restart()
			}
			steps[8]()
			if crash == "install" {
				f.restart()
			}
			steps[9]()
			steps[10]()
			if crash == "result" {
				f.restart()
			}
			status, err := f.engine.GetStatus(context.Background(), f.id)
			if err != nil || status.Result == nil || status.Result.Status != 1 {
				t.Fatalf("recovered result %+v: %v", status, err)
			}
			if ref := status.Result.Decision; ref == nil || ref.ID.Term != 2 || !sameRef(ref.Decision, f.decision.Reference) {
				t.Fatal("result lost current chosen ballot")
			}
			found := false
			for _, entry := range f.engine.Retained() {
				if sameRef(entry.Reference, original.Reference) {
					found = bytesEqual(entry.Bytes, original.Bytes)
				}
			}
			if !found {
				t.Fatal("original candidate bytes lost")
			}
			if _, err := f.engine.Cancel(context.Background(), f.id, 15); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecoveryDefaultDenyAndExactEvidence(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:8] {
		step()
	}
	v := f.successor(true)
	g := f.options.EvidenceGroups[0]
	promise := record.Promise{RecoveryID: v.recovery, Authority: g.Binding.Authority, PromisedEpoch: 2, PromisedTerm: 2, Reporter: g.Stream.Reporter, ReporterIncarnation: g.Stream.ReporterIncarnation, Authorization: v.authorization}
	before := len(f.engine.Retained())
	v.reject = true
	if _, err := f.engine.ProposeRecovery(context.Background(), g.Stream, promise); !errors.Is(err, ErrEvidence) {
		t.Fatalf("verifier rejection %v", err)
	}
	if len(f.engine.Retained()) != before {
		t.Fatal("rejected proof persisted")
	}
	v.reject = false
	promise.PromisedTerm = 1
	if _, err := f.engine.ProposeRecovery(context.Background(), g.Stream, promise); !errors.Is(err, ErrConflict) {
		t.Fatalf("term floor %v", err)
	}
	selection := f.recoverSelection(v, false, true)
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := f.decision.Record.Payload.(record.Decision)
	a := f.options.Groups[0].Binding.Authority
	d.ID.Configuration = a.Configuration
	d.ID.Epoch = a.Epoch
	d.ID.Term = a.CoordinatorTerm
	d.Authority = a
	d.Recovery = &selection.Reference
	d.Outcome = 2
	d.Result = record.TerminalResult{Status: 2, Code: 15}
	if _, err := f.engine.Propose(context.Background(), uuid(1), d); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed selected outcome %v", err)
	}
	if err := f.engine.Close(); err != nil {
		t.Fatal(err)
	}
	v.reject = true
	engine, err := Open(context.Background(), f.options)
	if engine != nil {
		_ = engine.Close()
	}
	if !errors.Is(err, ErrEvidence) {
		t.Fatalf("replay bypassed trusted verifier %v", err)
	}
}

func TestRecoveryAbsenceRequiresContinuousHistory(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:7] {
		step()
	}
	v := f.successor(false)
	selection := f.recoverSelection(v, true, true)
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := f.options.Groups[0].Binding.Authority
	d := record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: f.id, Term: 2}, Digest: f.digest, Begin: f.begin.Reference, Outcome: 2, Result: record.TerminalResult{Status: 2, Code: 15}, Authority: a, Recovery: &selection.Reference}
	f.decision = f.propose(uuid(1), d)
	f.restart()
	if f.engine.state.requests[f.id].decision.Record.Payload.(record.Decision).Outcome != 2 {
		t.Fatal("absence abort lost")
	}
}

func TestHistoricalRetentionNeverComparesCrossHistorySequence(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:8] {
		step()
	}
	v := f.successor(true)
	f.recoverSelection(v, false, true)
	f.protector.mutate = func(r *ProtectionReceipt) {
		var keep []record.FrameRef
		for _, ref := range r.Histories {
			if ref.Position.Configuration == uuid(70) {
				keep = append(keep, ref)
			}
		}
		r.Histories = keep
	}
	if err := f.engine.Reconcile(context.Background()); !errors.Is(err, ErrConflict) {
		t.Fatalf("lost predecessor history accepted: %v", err)
	}
	f.protector.mutate = nil
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, histories, _, err := f.engine.protectionGraph()
	if err != nil {
		t.Fatal(err)
	}
	old, new := false, false
	for _, ref := range histories {
		if ref.Position.Volume != uuid(1) || ref.StreamKind != 1 {
			continue
		}
		if ref.Position.Configuration == uuid(4) {
			old = true
		}
		if ref.Position.Configuration == uuid(70) {
			new = true
		}
	}
	if !old || !new {
		t.Fatal("same-volume histories collapsed")
	}
}

func TestUnavailableRecoveryVerifierKeepsStatusUncertain(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	for _, step := range steps[:8] {
		step()
	}
	v := f.successor(true)
	g := f.options.EvidenceGroups[0]
	v.reject = true
	_, err := f.engine.ProposeRecovery(context.Background(), g.Stream, record.Promise{RecoveryID: v.recovery, Authority: g.Binding.Authority, PromisedEpoch: 2, PromisedTerm: 2, Reporter: g.Stream.Reporter, ReporterIncarnation: g.Stream.ReporterIncarnation, Authorization: v.authorization})
	if err == nil {
		t.Fatal("unsupported proof accepted")
	}
	if _, err := f.engine.GetStatus(context.Background(), f.id); !errors.Is(err, ErrEvidence) {
		t.Fatalf("unreconciled recovery status %v", err)
	}
}

func TestCompletedResultsRemainImmutableAfterAuthorityRebinding(t *testing.T) {
	f := newFixture(t)
	for _, step := range f.steps() {
		step()
	}
	first := f.id
	originalFirst := cloneEntry(*f.engine.state.requests[first].result)
	f.id.Number++
	f.data = []byte("other bytes")
	f.revision = 2
	f.prepares = nil
	f.installs = nil
	for _, step := range f.steps() {
		step()
	}
	second := f.id
	originalSecond := cloneEntry(*f.engine.state.requests[second].result)
	v := f.successor(true)
	_, err := f.engine.ProposeRecovery(context.Background(), f.options.Groups[0].Stream, record.Fence{RecoveryID: v.recovery, Authorities: f.binding.Authorities, OldOwner: uuid(10), OldIncarnation: uuid(20), ServingGenerations: []record.UUID{uuid(61)}, Views: []record.UUID{uuid(51)}, Mechanism: uuid(76), Attestation: v.attestation})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.restart()
	for _, original := range []segment.Entry{originalFirst, originalSecond} {
		p := original.Record.Payload.(record.RequestResult)
		status, err := f.engine.GetStatus(context.Background(), p.Request)
		if err != nil || status.Result == nil || !equal(*status.Result, p.Result) {
			t.Fatalf("rebound terminal result %+v %v", status, err)
		}
		stored := f.engine.state.requests[p.Request].result
		if !sameRef(stored.Reference, original.Reference) || !bytesEqual(stored.Bytes, original.Bytes) {
			t.Fatal("terminal bytes/reference restamped")
		}
		if status.Result.Decision.ID.Term != 1 || status.Result.Decision.ID.Configuration != uuid(4) || status.Result.Committed[0].Configuration != uuid(4) {
			t.Fatal("historical decision/cuts replaced by current ballot")
		}
		if f.engine.state.requests[p.Request].selection != nil {
			t.Fatal("completed slot acquired a new selection")
		}
	}
	counts := map[string]int{}
	for _, entry := range f.engine.Retained() {
		switch entry.Record.Payload.(type) {
		case record.Decision:
			counts["decision"]++
		case record.Complete:
			counts["complete"]++
		case record.RequestResult:
			counts["result"]++
		}
	}
	for _, name := range []string{"decision", "complete", "result"} {
		if counts[name] != 2 {
			t.Fatalf("rebind created another %s: %d", name, counts[name])
		}
	}
	file, err := f.fs.Open(context.Background(), "applied-"+uuid(1).String(), os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	bytes := make([]byte, 11)
	if _, err := file.ReadAt(context.Background(), bytes, 0); err != nil || string(bytes) != "other bytes" {
		t.Fatalf("old complete replay regressed later write: %q %v", bytes, err)
	}
}
