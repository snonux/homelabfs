package storage

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestProjectionMatchesBackingWithoutChangingBaseline(t *testing.T) {
	f := fixtureFor(t, "simulator")
	baseline, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var operations []Operation
	apply := func(op Operation) { operations = append(operations, CopyOperation(op)); f.apply(op) }
	file := objectID(2)
	apply(f.create("raw-\xff", fsapi.Regular, file))
	apply(f.write(file, 5, []byte("abc")))
	apply(f.truncate(file, 6))
	apply(f.truncate(file, 10))
	apply(f.link(file, "alias"))
	apply(f.rename("alias", "renamed"))
	apply(f.unlink("raw-\xff"))
	actual, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := Project(t.Context(), baseline, operations, Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(projected, actual) {
		t.Fatal("logical projection differs from independently installed backing history")
	}
	if len(baseline.Image.Objects) != 1 || len(baseline.Data) != 0 {
		t.Fatal("input baseline mutated")
	}
	if len(projected.Data) != 1 || !bytes.Equal(projected.Data[0], []byte{0, 0, 0, 0, 0, 'a', 0, 0, 0, 0}) {
		t.Fatal(projected.Data)
	}
	// Every old duplicate is a no-op at the later state; it cannot restore bytes.
	duplicate, err := Project(t.Context(), projected, operations, Limits{}, 1<<20)
	if err != nil || !reflect.DeepEqual(duplicate, actual) {
		t.Fatal("old duplicate changed later logical state", err)
	}
	operations[1].Data[0] = 'z'
	if projected.Data[0][5] != 'a' {
		t.Fatal("caller changed owned payload")
	}
	projected.Data[0][5] = 'x'
	if actual.Data[0][5] != 'a' {
		t.Fatal("projection aliases backing capture")
	}
}

func TestProjectionRejectsGapsConflictsAndExpansion(t *testing.T) {
	f := fixtureFor(t, "simulator")
	id := objectID(2)
	f.apply(f.create("file", fsapi.Regular, id))
	baseline, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*Operation)
		max    int64
	}{
		{"gap", func(op *Operation) { op.Position.Sequence++ }, 1 << 20},
		{"foreign history", func(op *Operation) { op.Position.Generation = uuid(999) }, 1 << 20},
		{"stale revision", func(op *Operation) { op.Objects[0].Before++ }, 1 << 20},
		{"payload disguised as metadata", func(op *Operation) { op.Kind = Setattr }, 1 << 20},
		{"huge hole", func(op *Operation) {
			op.Kind = Truncate
			op.Data = nil
			op.Offset = 0
			op.Size = 1 << 40
			op.Objects[0].After.Attr.Size = 1 << 40
		}, 1 << 20},
		{"negative limit", func(*Operation) {}, -1},
		{"payload over cap", func(*Operation) {}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			op := f.write(id, 3, []byte("data"))
			test.change(&op)
			if _, err := Project(t.Context(), baseline, []Operation{op}, Limits{}, test.max); err == nil {
				t.Fatal("invalid projection accepted")
			}
			again, err := f.store.Capture(t.Context(), 1<<20)
			if err != nil || !reflect.DeepEqual(again, baseline) {
				t.Fatal("rejected projection changed backing", err)
			}
		})
	}
	op := f.write(id, 0, []byte("ok"))
	projected, err := Project(t.Context(), baseline, []Operation{op}, Limits{}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	op.Data[0] = 'X'
	if _, err := Project(t.Context(), projected, []Operation{op}, Limits{}, 1<<20); !errors.Is(err, fsapi.Stale) {
		t.Fatal("changed duplicate accepted", err)
	}
}

func TestPrimitiveTransferPreservesRawNamesAndRejectsChangedEncoding(t *testing.T) {
	f := fixtureFor(t, "simulator")
	op := f.create("\xff", fsapi.Regular, objectID(2))
	raw, err := EncodeOperation(op)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeOperation(raw, int64(len(raw)))
	if err != nil || !reflect.DeepEqual(got, op) {
		t.Fatal("primitive lost raw name", err)
	}
	for _, data := range [][]byte{append(bytes.Clone(raw), '\n'), append(bytes.Clone(raw), raw...), bytes.Replace(raw, []byte("\"ID\":"), []byte("\"Unknown\":"), 1), bytes.Replace(raw, []byte("\"Kind\":"), []byte("\"Kind\":1,\"Kind\":"), 1)} {
		if _, err := DecodeOperation(data, int64(len(data))); err == nil {
			t.Fatal("noncanonical primitive accepted")
		}
	}
}
