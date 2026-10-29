package transactions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/journal/record"
)

type blockingProtection struct {
	underlying       Protector
	entered, release chan struct{}
	once             sync.Once
}

func (p *blockingProtection) Check(ctx context.Context, b Binding) error {
	return p.underlying.Check(ctx, b)
}
func (p *blockingProtection) Protect(ctx context.Context, r ProtectionRequest) (ProtectionReceipt, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-ctx.Done():
		return ProtectionReceipt{}, ctx.Err()
	case <-p.release:
	}
	return p.underlying.Protect(ctx, r)
}

func TestCanceledQueuedWaitDoesNotAbortDurableCandidate(t *testing.T) {
	f := newFixture(t)
	blocking := &blockingProtection{underlying: f.protector, entered: make(chan struct{}), release: make(chan struct{})}
	f.engine.opts.Protector = blocking
	defer func() {
		select {
		case <-blocking.release:
		default:
			close(blocking.release)
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := f.engine.Propose(context.Background(), uuid(1), record.Session{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, State: 1})
		done <- err
	}()
	select {
	case <-blocking.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("protected candidate did not arrive")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := make(chan error, 1)
	go func() { _, err := f.engine.GetStatus(ctx, f.id); response <- err }()
	select {
	case err := <-response:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrEvidence) {
			t.Fatalf("queued wait %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued cancellation blocked behind transport")
	}
	close(blocking.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	f.restart()
	if f.engine.state.sessions[f.id.Session] == nil {
		t.Fatal("canceled waiter discarded durable registration")
	}
	for _, entry := range f.engine.Retained() {
		if _, ok := entry.Record.Payload.(record.Decision); ok {
			t.Fatal("canceled waiter invented abort")
		}
	}
}

var _ Protector = (*blockingProtection)(nil)

func TestReplayOldInstallDoesNotRegressLaterWrite(t *testing.T) {
	f := newFixture(t)
	for _, step := range f.steps() {
		step()
	}
	first := f.id
	f.id.Number++
	f.data = []byte("other bytes")
	f.revision = 2
	f.prepares = nil
	f.installs = nil
	for _, step := range f.steps() {
		step()
	}
	second := f.id
	f.restart()
	if err := f.engine.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []record.RequestID{first, second} {
		status, err := f.engine.GetStatus(context.Background(), id)
		if err != nil || status.Result == nil || status.Result.Status != 1 {
			t.Fatalf("receipt %d: %+v %v", id.Number, status, err)
		}
	}
	file, err := f.fs.Open(context.Background(), "applied-"+uuid(1).String(), os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	bytes := make([]byte, 11)
	if _, err := file.ReadAt(context.Background(), bytes, 0); err != nil || string(bytes) != "other bytes" {
		t.Fatalf("old install regressed later data: %q %v", bytes, err)
	}
}

func TestCrashDuringParticipantFanout(t *testing.T) {
	for _, phase := range []string{"anchor", "prepare", "install"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:5] {
				step()
			}
			if phase == "anchor" {
				f.propose(uuid(1), record.TxAnchor{Request: f.id, Digest: f.digest, Binding: f.binding, Begin: f.begin.Reference, CoordinatorTerm: 1})
				f.restart()
			}
			steps[5]()
			if phase == "prepare" {
				m := record.Mutation{Intent: f.binding.Intent, Redo: record.Redo{Objects: f.binding.Intent.SelectedMetadata}}
				p := record.Prepared{Request: f.id, Digest: f.digest, Begin: f.begin.Reference, Binding: f.binding, Term: 1, Operation: m, RedoSHA256: redoHash(t, f.engine.groups[uuid(1)], m)}
				f.propose(uuid(1), p)
				f.restart()
			}
			steps[6]()
			steps[7]()
			if phase == "install" {
				entry := f.prepares[0]
				p := entry.Record.Payload.(record.Prepared)
				d := f.decision.Record.Payload.(record.Decision)
				accept, err := f.engine.Acceptance(f.decision.Reference)
				if err != nil {
					t.Fatal(err)
				}
				pr := record.PrepareRef{Participant: uuid(1), Prepare: entry.Reference, RedoSHA256: p.RedoSHA256}
				f.propose(uuid(1), record.Install{Request: f.id, Digest: f.digest, Binding: f.binding, Decision: record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}, Outcome: 1, Prepare: &pr, Redo: &p.Operation.Redo, Applied: entry.Reference.Position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}})
				f.restart()
			}
			steps[8]()
			steps[9]()
			steps[10]()
			f.restart()
			status, err := f.engine.GetStatus(context.Background(), f.id)
			if err != nil || status.Result == nil {
				t.Fatalf("fanout result %+v %v", status, err)
			}
		})
	}
}

func TestSegmentRotationRetainsEveryStructuralPrefix(t *testing.T) {
	f := newFixture(t)
	for _, w := range f.engine.writers {
		if err := w.Seal(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.engine.Close(); err != nil {
		t.Fatal(err)
	}
	for i := range f.options.Groups {
		f.options.Groups[i].Writer.MaximumBytes = 5000
	}
	var err error
	f.engine, err = Open(context.Background(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range f.steps() {
		step()
	}
	seals := 0
	for _, entry := range f.engine.Retained() {
		if _, ok := entry.Record.Payload.(record.SegmentSeal); ok {
			seals++
		}
	}
	if seals <= 2 {
		t.Fatal("fixture did not rotate")
	}
	f.restart()
	status, err := f.engine.GetStatus(context.Background(), f.id)
	if err != nil || status.Result == nil {
		t.Fatalf("rotated replay %+v %v", status, err)
	}
}

func TestAffinityPartialJoinAndGrowingPredecessors(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	steps[0]()
	steps[1]()
	var authorities []record.Authority
	for _, g := range f.options.Groups {
		authorities = append(authorities, g.Binding.Authority)
	}
	manifest := record.Affinity{Closure: uuid(60), ManifestVersion: 1, Members: []record.UUID{uuid(1), uuid(2)}, Authorities: authorities}
	first := f.propose(uuid(1), manifest)
	f.restart()
	binding := record.Binding{Request: f.id, RequestDigest: f.digest, Affinity: affinityRef(manifest, first.Bytes), Coordinator: uuid(1), Intent: f.document.Internal.Intent, Participants: manifest.Members, Authorities: authorities}
	if _, err := f.engine.Propose(context.Background(), uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: binding}); !errors.Is(err, ErrEvidence) {
		t.Fatalf("partial affinity admitted %v", err)
	}
	f.propose(uuid(2), manifest)
	ref := affinityRef(manifest, first.Bytes)
	for version := uint64(2); version <= 3; version++ {
		manifest.ManifestVersion = version
		manifest.Predecessors = []record.AffinityRef{ref}
		first = f.propose(uuid(1), manifest)
		f.propose(uuid(2), manifest)
		ref = affinityRef(manifest, first.Bytes)
	}
	f.restart()
	binding.Affinity = ref
	f.propose(uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: binding})
	f.begin = f.propose(uuid(1), record.Begin{Binding: binding, Bind: f.engine.state.requests[f.id].bind.Reference, Document: f.document, Term: 1})
	manifest.ManifestVersion++
	manifest.Predecessors = []record.AffinityRef{ref}
	if _, err := f.engine.Propose(context.Background(), uuid(1), manifest); !errors.Is(err, ErrTransition) {
		t.Fatalf("active closure affinity changed %v", err)
	}
}

func TestEveryLogicalTransitionRetryIsIdempotent(t *testing.T) {
	f := newFixture(t)
	for i, step := range f.steps() {
		step()
		before := len(f.engine.Retained())
		for _, entry := range f.engine.Retained() {
			if structural(entry) {
				continue
			}
			if _, ok := entry.Record.Payload.(record.ViewBarrier); ok {
				continue
			}
			if _, ok := entry.Record.Payload.(record.Accept); ok {
				continue
			}
			retry, err := f.engine.Propose(context.Background(), entry.Record.Header.Volume, entry.Record.Payload)
			if err != nil || !sameRef(retry.Reference, entry.Reference) {
				t.Fatalf("retry phase %d %T: %v", i, entry.Record.Payload, err)
			}
		}
		if len(f.engine.Retained()) != before {
			t.Fatal("retry appended")
		}
	}
	f.restart()
}

func TestViewBarrierAndApplyFailuresKeepCommittedOutcome(t *testing.T) {
	for _, phase := range []string{"apply", "barrier"} {
		t.Run(phase, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:8] {
				step()
			}
			if phase == "barrier" {
				steps[8]()
			}
			f.apply.fail = true
			if phase == "apply" {
				entry := f.prepares[0]
				p := entry.Record.Payload.(record.Prepared)
				d := f.decision.Record.Payload.(record.Decision)
				accept, err := f.engine.Acceptance(f.decision.Reference)
				if err != nil {
					t.Fatal(err)
				}
				pr := record.PrepareRef{Participant: uuid(1), Prepare: entry.Reference, RedoSHA256: p.RedoSHA256}
				_, err = f.engine.Propose(context.Background(), uuid(1), record.Install{Request: f.id, Digest: f.digest, Binding: f.binding, Decision: record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}, Outcome: 1, Prepare: &pr, Redo: &p.Operation.Redo, Applied: entry.Reference.Position, Acceptance: []record.FrameRef{accept.Reference}, Evidence: record.Evidence{Frames: [][]byte{accept.Bytes}}})
				if !errors.Is(err, ErrEvidence) {
					t.Fatal(err)
				}
			} else {
				var refs []record.FrameRef
				var vector record.Vector
				for _, entry := range f.installs {
					refs = append(refs, entry.Reference)
					vector = append(vector, entry.Reference.Position)
				}
				SortReferences(refs)
				visibility := record.Visibility{Affinity: f.binding.Affinity, Generation: 2}
				barrier := f.propose(uuid(1), record.ViewBarrier{Affinity: f.binding.Affinity, OldVisibility: record.Visibility{Affinity: f.binding.Affinity, Generation: 1}, NewVisibility: visibility, ServingGeneration: uuid(61), Incarnation: uuid(20), Drained: true, Mechanism: uuid(62), Attestation: record.Artifact{Kind: 7, ID: uuid(63)}, Dependencies: refs})
				d := f.decision.Record.Payload.(record.Decision)
				result := d.Result
				ref := record.DecisionRef{ID: d.ID, Decision: f.decision.Reference}
				result.Decision = &ref
				result.Visibility = &visibility
				result.Committed = vector
				_, err := f.engine.Propose(context.Background(), uuid(1), record.Complete{Request: f.id, Digest: f.digest, Decision: ref, Installs: refs, Installed: vector, Result: result, Visibility: visibility, CacheBarriers: []record.FrameRef{barrier.Reference}})
				if !errors.Is(err, ErrEvidence) {
					t.Fatal(err)
				}
			}
			status, err := f.engine.GetStatus(context.Background(), f.id)
			if err != nil || status.Result != nil {
				t.Fatalf("failed effect published %+v %v", status, err)
			}
			entry, err := f.engine.Cancel(context.Background(), f.id, 15)
			if err != nil || entry.Record.Payload.(record.Decision).Outcome != 1 {
				t.Fatalf("committed outcome changed %v", err)
			}
			f.apply.fail = false
			f.restart()
			if phase == "apply" {
				steps[8]()
			}
			steps[9]()
			steps[10]()
			status, err = f.engine.GetStatus(context.Background(), f.id)
			if err != nil || status.Result == nil {
				t.Fatalf("resumed completion %+v %v", status, err)
			}
		})
	}
}

func TestRecoveryProposalStreamRoleIsExplicit(t *testing.T) {
	f := newFixture(t)
	_, err := f.engine.ProposeRecovery(context.Background(), f.options.Groups[0].Stream, record.Promise{})
	if !errors.Is(err, ErrEvidence) {
		t.Fatal(fmt.Sprint(err))
	}
}
