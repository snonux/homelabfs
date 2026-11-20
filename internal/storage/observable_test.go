package storage

import (
	"bytes"
	"testing"
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
