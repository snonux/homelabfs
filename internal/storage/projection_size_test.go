package storage

import (
	"errors"
	"reflect"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestSnapshotBytesMatchesCanonicalEncodingAtExactBoundary(t *testing.T) {
	f := fixtureFor(t, "simulator")
	f.apply(f.create("raw-\xff", fsapi.Regular, objectID(2)))
	f.apply(f.write(objectID(2), 3, []byte("payload")))
	f.apply(f.link(objectID(2), "alias"))
	f.apply(f.create("symlink", fsapi.Symlink, objectID(3)))
	f.apply(f.create("directory", fsapi.Directory, objectID(4)))
	f.apply(f.create("retired", fsapi.Regular, objectID(5)))
	f.apply(f.unlink("retired"))
	f.apply(f.op(Reclaim, objectID(5)))
	pin := Pin{ID: uuid(900), Object: objectID(2), Generation: uuid(901), Incarnation: uuid(902), Active: true}
	op := f.op(HandlePin, objectID(2))
	op.Pins = []PinChange{{After: pin}}
	f.apply(op)
	snapshot, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := EncodeImage(snapshot.Image, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len(raw))
	for _, data := range snapshot.Data {
		want += int64(len(data))
	}
	if got, err := SnapshotBytes(snapshot, Limits{}, want); err != nil || got != want {
		t.Fatal("count differs from canonical catalog plus payloads", got, want, err)
	}
	for _, limits := range []Limits{{}, {FileBytes: int64(len(raw))}} {
		if _, err := ProjectedBytes(t.Context(), snapshot, nil, limits, want); err != nil {
			t.Fatal("exact boundary rejected", err)
		}
	}
	if _, err := SnapshotBytes(snapshot, Limits{}, want-1); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("snapshot exceeded exact total cap", err)
	}
	if _, err := SnapshotBytes(snapshot, Limits{FileBytes: int64(len(raw)) - 1}, want); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("catalog exceeded exact storage cap", err)
	}
}

func TestProjectedBytesMatchesRealProjectionAndRejectsMetadataGrowth(t *testing.T) {
	f := fixtureFor(t, "simulator")
	before, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	operations := []Operation{f.create("symlink-\xff", fsapi.Symlink, objectID(2))}
	after, err := Project(t.Context(), before, operations, Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	want, err := SnapshotBytes(after, Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ProjectedBytes(t.Context(), before, operations, Limits{}, want); err != nil || got != want {
		t.Fatal("metadata preflight differs from owning projection", got, want, err)
	}
	if _, err := ProjectedBytes(t.Context(), before, operations, Limits{}, want-1); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("metadata growth escaped complete snapshot bound", err)
	}
	bad := CopyOperation(operations[0])
	bad.Objects[0].Before++
	if _, err := ProjectedBytes(t.Context(), before, []Operation{bad}, Limits{}, 1<<20); err == nil {
		t.Fatal("size preflight bypassed semantic revision validation")
	}
	if _, err := ProjectedBytes(t.Context(), after, operations, Limits{}, want); err != nil {
		t.Fatal("matching old duplicate rejected", err)
	}
	bad = CopyOperation(operations[0])
	bad.Entries[0].After.Name = "changed"
	if _, err := ProjectedBytes(t.Context(), after, []Operation{bad}, Limits{}, 1<<20); !errors.Is(err, fsapi.Stale) {
		t.Fatal("changed duplicate escaped preflight", err)
	}
	again, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil || !reflect.DeepEqual(before, again) {
		t.Fatal("metadata preflight changed real backing", err)
	}
}
