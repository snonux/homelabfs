package storage

import (
	"bytes"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TestObservableCatalogDropsOnlyLifetimeState (task e84): position, pins and
// receipts do not reach the observable encoding, every other field does, and
// undecodable or foreign input is refused.
func TestObservableCatalogDropsOnlyLifetimeState(t *testing.T) {
	base := []byte(`{"magic":"HOMELABFS-CATALOG","major":1,"minor":0,"volume_uuid":"v","position":{"sequence":1},"pins":[{"id":"p1"}],"receipts":[{"sequence":1}],"objects":[],"entries":[],"retired_allocations":[],"payloads":[]}`)
	moved := []byte(`{"magic":"HOMELABFS-CATALOG","major":1,"minor":0,"volume_uuid":"v","position":{"sequence":2},"pins":[{"id":"p1","active":true},{"id":"p2"}],"receipts":[{"sequence":1},{"sequence":2}],"objects":[],"entries":[],"retired_allocations":[],"payloads":[]}`)
	mutated := []byte(`{"magic":"HOMELABFS-CATALOG","major":1,"minor":0,"volume_uuid":"v","position":{"sequence":2},"pins":[],"receipts":[],"objects":[],"entries":[{"name_base64":"eA==","revision":1}],"retired_allocations":[],"payloads":[]}`)
	a, okA := ObservableCatalog(base)
	b, okB := ObservableCatalog(moved)
	c, okC := ObservableCatalog(mutated)
	if !okA || !okB || !okC {
		t.Fatal("valid catalogs refused")
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("pin/position/receipt change is observable:\n%s\n%s", a, b)
	}
	if bytes.Equal(a, c) {
		t.Fatal("entry change is not observable")
	}
	for _, bad := range [][]byte{[]byte("x"), []byte(`{"magic":"OTHER"}`)} {
		if _, ok := ObservableCatalog(bad); ok {
			t.Fatalf("accepted %s", bad)
		}
	}
}

// TestObservableCatalogOnRealStore applies real primitives to a store (task
// e84): a HandlePin and HandleRelease of an open-unlinked file leave the
// observable catalog unchanged; the Reclaim that later retires it (the
// fsservice reaper, which needs a ReclamationPolicy) changes it, so a
// mismatch across a Reclaim keeps the all-domain recall.
func TestObservableCatalogOnRealStore(t *testing.T) {
	f := fixtureFor(t, "simulator")
	f.apply(f.create("victim", fsapi.Regular, objectID(2)))
	f.apply(f.unlink("victim"))
	observable := func() []byte {
		raw, err := EncodeImage(f.image(), f.config.Limits)
		if err != nil {
			t.Fatal(err)
		}
		out, ok := ObservableCatalog(raw)
		if !ok {
			t.Fatal("own catalog refused")
		}
		return out
	}
	unlinked := observable()
	pin := Pin{ID: uuid(900), Object: objectID(2), Generation: uuid(901), Incarnation: uuid(902), Active: true}
	pinned := f.op(HandlePin, objectID(2))
	pinned.Pins = []PinChange{{After: pin}}
	f.apply(pinned)
	released := f.op(HandleRelease, objectID(2))
	inactive := pin
	inactive.Active = false
	released.Pins = []PinChange{{Before: &pin, After: inactive}}
	f.apply(released)
	if !bytes.Equal(unlinked, observable()) {
		t.Fatal("handle pin/release changed the observable catalog")
	}
	f.apply(f.op(Reclaim, objectID(2)))
	if bytes.Equal(unlinked, observable()) {
		t.Fatal("Reclaim left the observable catalog unchanged")
	}
}
