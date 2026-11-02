package fsservice

import (
	"errors"
	"slices"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

func TestLogicalForeignAliasResolvesFullHomeWithoutPublicMutationAdmission(t *testing.T) {
	f := newLogicalFixture(t)
	left := f.config.Baseline[0]
	right := otherHome(left, uid(21))
	home := left.Image.Volume
	foreign := right.Image.Volume
	root := right.Image.Root
	var file storage.Object
	for _, o := range left.Image.Objects {
		if o.Attr.Kind == fsapi.Regular {
			file = o
		}
	}
	if !file.Attr.Object.Valid() {
		t.Fatal("missing baseline file")
	}
	claim := uid(2200)
	references := append(slices.Clone(file.References), storage.Reference{Parent: root, Name: "foreign", Revision: 1})
	slices.SortFunc(references, func(a, b storage.Reference) int {
		if a.Parent.Home != b.Parent.Home {
			return slices.Compare(a.Parent.Home[:], b.Parent.Home[:])
		}
		return slices.Compare([]byte(a.Name), []byte(b.Name))
	})
	after := file
	after.Attr.Revision++
	after.Attr.Nlink++
	after.LedgerRevision++
	after.References = references
	position := left.Image.Position
	position.Sequence++
	homeOp := storage.Operation{ID: claim, Position: position, Kind: storage.Link, Object: file.Attr.Object, Objects: []storage.ObjectChange{{Before: file.Attr.Revision, After: after}}, Participants: []fsapi.UUID{home, foreign}}
	var parent storage.Object
	for _, o := range right.Image.Objects {
		if o.Attr.Object == root {
			parent = o
		}
	}
	parentAfter := parent
	parentAfter.Attr.Revision++
	position = right.Image.Position
	position.Sequence++
	target := file.Attr.Object
	aliasOp := storage.Operation{ID: claim, Position: position, Kind: storage.Link, Object: target, Objects: []storage.ObjectChange{{Before: parent.Attr.Revision, After: parentAfter}}, Entries: []storage.EntryChange{{Before: storage.Entry{Parent: root, Name: "foreign"}, After: storage.Entry{Parent: root, Name: "foreign", Revision: 1, Target: &target}}}, Participants: []fsapi.UUID{home, foreign}}
	left, err := storage.Project(t.Context(), left, []storage.Operation{homeOp}, storage.Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	right, err = storage.Project(t.Context(), right, []storage.Operation{aliasOp}, storage.Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	config := f.config
	config.Baseline = []storage.Snapshot{left, right}
	config.Authority.Volumes = append(slices.Clone(config.Authority.Volumes), record.NormalVolume{Volume: foreign, Generation: right.Image.Position.Generation, Configuration: right.Image.Position.Configuration, Epoch: right.Image.Position.Epoch, Replicas: []fsapi.UUID{uid(6), uid(8), uid(9)}})
	budget, err := admission.New(logicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	config.Budget = budget
	view, err := logicalview.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewUnixPermissions(uid(419))
	if err != nil {
		t.Fatal(err)
	}
	planner, err := NewLogicalPlanner(LogicalConfig{View: view, Permissions: policy, Clock: fixedClock{}})
	if err != nil {
		t.Fatal(err)
	}
	observation, err := view.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = observation.Close() }()
	lookup, err := planner.Observe(t.Context(), observation, logicalCred(), fsapi.Command{Mount: foreign, Operation: fsapi.Lookup, Parent: root, Name: "foreign"})
	if err != nil || lookup.Attr.Object != file.Attr.Object || lookup.Attr.Nlink != 2 {
		t.Fatal("foreign alias lost its immutable home", lookup, err)
	}
	opened, err := planner.Observe(t.Context(), observation, logicalCred(), fsapi.Command{Mount: foreign, Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = planner.Observe(t.Context(), observation, logicalCred(), fsapi.Command{Mount: foreign, Generation: view.Generation(), Operation: fsapi.Read, Handle: opened.Handle, Size: 16}); err != nil {
		t.Fatal(err)
	}
	// The generic home/alias foundation does not silently extend ordinary grants
	// to a public cross-home mutation before the typed successor protocol exists.
	if _, err = planner.Plan(t.Context(), observation, logicalCred(), fsapi.Command{Mount: foreign, Generation: view.Generation(), Operation: fsapi.Write, Handle: opened.Handle, Data: []byte("x")}, uid(2201), fsapi.ObjectID{}); !errors.Is(err, fsapi.Unsupported) {
		t.Fatal("foreign-home ordinary grant opened", err)
	}
	broken := config
	broken.Baseline = []storage.Snapshot{f.config.Baseline[0], right}
	otherBudget, err := admission.New(logicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	broken.Budget = otherBudget
	if _, err = logicalview.New(t.Context(), broken); !errors.Is(err, fsapi.Stale) {
		t.Fatal("foreign entry without exact home ledger accepted", err)
	}
}

func otherHome(input storage.Snapshot, volume fsapi.UUID) storage.Snapshot {
	out := storage.Snapshot{Image: storage.CopyImage(input.Image)}
	move := func(id fsapi.ObjectID) fsapi.ObjectID {
		if id.Valid() {
			id.Home = volume
		}
		return id
	}
	out.Image.Volume = volume
	out.Image.Root = move(out.Image.Root)
	out.Image.Position.Volume = volume
	for i := range out.Image.Objects {
		o := &out.Image.Objects[i]
		o.Attr.Object = move(o.Attr.Object)
		o.Descriptor.Object = move(o.Descriptor.Object)
		if o.Parent != nil {
			o.Parent.Object = move(o.Parent.Object)
		}
		for j := range o.References {
			o.References[j].Parent = move(o.References[j].Parent)
		}
	}
	for i := range out.Image.Entries {
		e := &out.Image.Entries[i]
		e.Parent = move(e.Parent)
		if e.Target != nil {
			id := move(*e.Target)
			e.Target = &id
		}
	}
	for i := range out.Image.Payloads {
		out.Image.Payloads[i].Object = move(out.Image.Payloads[i].Object)
	}
	for i := range out.Image.Pins {
		out.Image.Pins[i].Object = move(out.Image.Pins[i].Object)
	}
	for i := range out.Image.Retired {
		out.Image.Retired[i].Object = move(out.Image.Retired[i].Object)
	}
	for _, data := range input.Data {
		out.Data = append(out.Data, slices.Clone(data))
	}
	return out
}
