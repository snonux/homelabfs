package storage

import (
	"errors"
	"reflect"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestSourceSizeMatchesRealProjectionIncludingPinReceiptAndFraming(t *testing.T) {
	f := fixtureFor(t, "simulator")
	file := objectID(2)
	f.apply(f.create("source", fsapi.Regular, file))
	before, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	write := f.write(file, 4096, []byte("x"))
	f.apply(write)
	pin := f.op(HandlePin, file)
	pin.Pins = []PinChange{{After: Pin{ID: uuid(900), Object: file, Generation: uuid(901), Incarnation: uuid(902), Active: true}}}
	f.apply(pin)
	after, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := EncodeImage(after.Image, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(8 + len(catalog))
	for _, data := range after.Data {
		want += 8 + int64(len(data))
	}
	got, err := ProjectedSourceBytes(t.Context(), before.Image, []Operation{write, pin}, nil, 0, Limits{}, want)
	if err != nil || got != want {
		t.Fatalf("bounded projection differs from independently applied exact source: %d %d %v", got, want, err)
	}
	if _, err = ProjectedSourceBytes(t.Context(), before.Image, []Operation{write, pin}, nil, 0, Limits{}, want-1); !errors.Is(err, fsapi.NoSpace) {
		t.Fatalf("framing/catalog/receipt cap ignored %v", err)
	}
	original, err := f.store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, after) {
		t.Fatal("sizing changed backing data")
	}
}

func TestSourceFutureBoundsAccountAggregateGrowthWithoutHugePayloadAllocation(t *testing.T) {
	f := fixtureFor(t, "simulator")
	a, b := objectID(2), objectID(3)
	f.apply(f.create("a", fsapi.Regular, a))
	f.apply(f.create("b", fsapi.Regular, b))
	image, err := f.store.Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		writes []FutureWrite
		ok     bool
	}{
		{"one bounded extent", []FutureWrite{{a, 32768}}, true},
		{"same object pages use maximum extent", []FutureWrite{{a, 16384}, {a, 32768}}, true},
		{"independent object aggregate", []FutureWrite{{a, 32768}, {b, 32768}}, false},
		{"huge sparse extent", []FutureWrite{{a, 1 << 40}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ProjectedSourceBytes(t.Context(), image, nil, test.writes, 0, Limits{}, 65536)
			if test.ok && err != nil {
				t.Fatal(err)
			}
			if !test.ok && !errors.Is(err, fsapi.NoSpace) {
				t.Fatalf("growth escaped aggregate bound %v", err)
			}
		})
	}
}
