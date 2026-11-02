package fsservice

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"runtime"
	"slices"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

func TestLogicalDirectStageAllocationBoundAndRecovery(t *testing.T) {
	for _, test := range []struct {
		name       string
		identities int
		reserve    bool
		wrongClaim bool
		homes      int
		want       error
	}{
		{"exhausted", 2, false, false, 1, fsapi.NoSpace},
		{"direct at boundary", 3, false, false, 1, nil},
		{"matching reservation at boundary", 3, true, false, 1, nil},
		// Leave room for the incoming operation ID so allocation ownership,
		// rather than operation-ID exhaustion, supplies the rejection.
		{"other claim reservation", 4, true, true, 1, fsapi.Stale},
		{"whole two-home batch exhausted", 5, false, false, 2, fsapi.NoSpace},
		{"two-home batch at boundary", 6, false, false, 2, nil},
		{"two-home batch partly reserved", 6, true, false, 2, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newLogicalFixture(t)
			root := f.local.c.Storage.Root
			planned := f.plan(logicalCred(), fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "direct", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
			// Build a valid generic Stage through the real planner/fixture, then
			// send it to an independent view that never reserved this identity.
			stage, _ := f.stage(planned, uid(11))
			config := f.config
			if test.homes == 2 {
				foreign := otherHome(config.Baseline[0], uid(21))
				config.Baseline = append(slices.Clone(config.Baseline), foreign)
				config.Authority.Volumes = append(slices.Clone(config.Authority.Volumes), record.NormalVolume{Volume: foreign.Image.Volume, Generation: foreign.Image.Position.Generation, Configuration: foreign.Image.Position.Configuration, Epoch: foreign.Image.Position.Epoch, Replicas: []fsapi.UUID{uid(6), uid(8), uid(9)}})
				var err error
				stage.Slot.Authority, err = record.NormalAuthorityHash(config.Authority)
				if err != nil {
					t.Fatal(err)
				}
				stage.Operations = append(stage.Operations, createInOtherHome(planned.Operation, foreign.Image.Volume))
			}
			config.Limits.Identities = test.identities
			config.Budget = freshLogicalBudget(t)
			view, err := logicalview.New(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			if test.reserve {
				claim := stage.Claim
				if test.wrongClaim {
					claim = uid(4900)
				}
				if err = view.ReserveIdentity(t.Context(), claim, planned.Operation.Object); err != nil {
					t.Fatal(err)
				}
			}
			_, err = view.Stage(t.Context(), stage)
			if !errors.Is(err, test.want) {
				t.Fatalf("direct Stage: got %v, want %v", err, test.want)
			}
			var encoded bytes.Buffer
			if err = view.WriteRecovery(t.Context(), &encoded); err != nil {
				t.Fatal(err)
			}
			var state logicalview.RecoveryState
			if err = json.Unmarshal(encoded.Bytes(), &state); err != nil {
				t.Fatal(err)
			}
			if len(state.Used) > config.Limits.Identities {
				t.Fatal("permanent allocation ledger exceeded its reservation", len(state.Used))
			}
			config.Budget = freshLogicalBudget(t)
			restored, err := logicalview.Restore(t.Context(), config, bytes.NewReader(encoded.Bytes()), []byte("verified full source recovery"))
			if err != nil {
				t.Fatal("view rejected its own recovery output", err)
			}
			wantClaims := 0
			if test.want == nil {
				wantClaims = 1
			}
			if len(restored.Claims()) != wantClaims {
				t.Fatal("recovery lost admitted/rejected candidate state", restored.Claims())
			}
			observation, err := restored.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = observation.Close() }()
			if _, err = observation.Lookup(t.Context(), root, "direct"); !errors.Is(err, fsapi.NotFound) {
				t.Fatal("unselected direct stage became visible", err)
			}
		})
	}
}

func TestLogicalBaselineMetadataRejectedBeforeSnapshotCopy(t *testing.T) {
	f := newLogicalFixture(t)
	baseline := storage.Snapshot{Image: storage.CopyImage(f.config.Baseline[0].Image), Data: f.config.Baseline[0].Data}
	for i := uint64(0); i < 1024; i++ {
		id := fsapi.ObjectID{Home: baseline.Image.Volume, Allocation: uid(10000 + i)}
		text := bytes.Repeat([]byte("x"), 4096)
		baseline.Image.Objects = append(baseline.Image.Objects, storage.Object{Descriptor: storage.Descriptor{Object: id, Kind: fsapi.Symlink, Creation: uid(20000 + i)}, Attr: fsapi.Attr{Object: id, Kind: fsapi.Symlink, Mode: 0o777, Size: int64(len(text)), Revision: 1}, Text: text, LedgerRevision: 1})
	}
	slices.SortFunc(baseline.Image.Objects, func(a, b storage.Object) int {
		return slices.Compare(a.Attr.Object.Allocation[:], b.Attr.Object.Allocation[:])
	})
	if err := storage.ValidateSnapshot(baseline, storage.Limits{}, 128<<10); err != nil {
		t.Fatal("oversized metadata fixture must otherwise be semantically valid", err)
	}
	config := f.config
	config.Baseline = []storage.Snapshot{baseline}
	config.Limits.MaxSnapshotBytes = 128 << 10
	config.Limits.Identities = 2048
	config.Budget = freshLogicalBudget(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := logicalview.New(t.Context(), config)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("metadata escaped complete snapshot bound", err)
	}
	// The input contains 4 MiB of symlink text. A generous 2 MiB ceiling
	// catches a deep copy or complete catalog encoding before rejection.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 2<<20 {
		t.Fatal("rejected metadata was copied/encoded before admission", allocated)
	}
	if used := config.Budget.Usage().Used; used.Bytes != 0 || used.OwnerRAM != 0 {
		t.Fatal("rejected baseline retained an arena", used)
	}
}

func TestLogicalCompleteSnapshotExactBoundary(t *testing.T) {
	f := newLogicalFixture(t)
	size, err := storage.SnapshotBytes(f.config.Baseline[0], storage.Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []int64{0, -1} {
		config := f.config
		config.Limits.MaxSnapshotBytes = size + extra
		config.Budget = freshLogicalBudget(t)
		view, err := logicalview.New(t.Context(), config)
		if extra < 0 {
			if !errors.Is(err, fsapi.NoSpace) {
				t.Fatal("baseline exceeded exact snapshot boundary", err)
			}
			continue
		}
		if err != nil {
			t.Fatal("exact snapshot boundary rejected", err)
		}
		if err = view.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLogicalCandidateMetadataPreflightBeforePayloadCopy(t *testing.T) {
	f := newLogicalFixture(t)
	root := f.local.c.Storage.Root
	planned := f.plan(logicalCred(), fsapi.Command{Operation: fsapi.SymlinkOp, Parent: root, Name: "metadata-growth", Data: []byte("target")})
	stage, _ := f.stage(planned, uid(11))
	baseline := storage.Snapshot{Image: storage.CopyImage(f.config.Baseline[0].Image)}
	// The shared baseline verifier admits at most 1 MiB of payload.
	data := bytes.Repeat([]byte("p"), 1<<20)
	for i := range baseline.Image.Objects {
		if baseline.Image.Objects[i].Attr.Kind == fsapi.Regular {
			baseline.Image.Objects[i].Attr.Size = int64(len(data))
		}
	}
	baseline.Image.Payloads[0].Size = int64(len(data))
	baseline.Image.Payloads[0].SHA256 = sha256.Sum256(data)
	baseline.Data = [][]byte{data}
	after, err := storage.Project(t.Context(), baseline, stage.Operations, storage.Limits{}, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	size, err := storage.SnapshotBytes(after, storage.Limits{}, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, extra := range []int64{-1, 0} {
		config := f.config
		config.Baseline = []storage.Snapshot{baseline}
		config.Limits.MaxSnapshotBytes = size + extra
		config.Limits.Versions = 2
		config.Limits.Claims = 2
		config.Limits.Readers = 1
		config.Budget = freshLogicalBudget(t)
		view, err := logicalview.New(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err = view.Stage(t.Context(), stage)
		runtime.ReadMemStats(&after)
		if extra == 0 {
			if err != nil {
				t.Fatal("exact candidate boundary rejected", err)
			}
			continue
		}
		if !errors.Is(err, fsapi.NoSpace) {
			t.Fatal("candidate metadata exceeded complete snapshot cap", err)
		}
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 512<<10 {
			t.Fatal("rejected candidate copied its 1 MiB baseline payload", allocated)
		}
		if len(view.Claims()) != 0 {
			t.Fatal("rejected candidate retained a claim")
		}
		if err = view.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func createInOtherHome(input storage.Operation, volume fsapi.UUID) storage.Operation {
	op := storage.CopyOperation(input)
	move := func(id fsapi.ObjectID) fsapi.ObjectID {
		if id.Valid() {
			id.Home = volume
		}
		return id
	}
	op.Position.Volume = volume
	op.Object = move(op.Object)
	op.Participants = []fsapi.UUID{volume}
	for i := range op.Objects {
		o := &op.Objects[i].After
		o.Attr.Object = move(o.Attr.Object)
		o.Descriptor.Object = move(o.Descriptor.Object)
		if o.Parent != nil {
			o.Parent.Object = move(o.Parent.Object)
		}
		for j := range o.References {
			o.References[j].Parent = move(o.References[j].Parent)
		}
	}
	for i := range op.Entries {
		for _, entry := range []*storage.Entry{&op.Entries[i].Before, &op.Entries[i].After} {
			entry.Parent = move(entry.Parent)
			if entry.Target != nil {
				target := move(*entry.Target)
				entry.Target = &target
			}
		}
	}
	return op
}

func freshLogicalBudget(t *testing.T) *admission.Budget {
	t.Helper()
	budget, err := admission.New(logicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	return budget
}
