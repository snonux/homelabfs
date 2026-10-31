package cluster

import (
	"net/netip"
	"testing"
)

func TestOriginEligibilityIndependentOfReplicaPlacement(t *testing.T) {
	c := testConfig()
	outside := testID(99)
	c.Nodes = append(c.Nodes, Node{ID: outside, Admission: Admitted, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.99")}})
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	v := c.Volumes[0]
	if err := r.CheckOrigin(c.Cluster, outside, v.ID, v.History); err != nil {
		t.Fatal(err)
	}
	for _, role := range []Role{Primary, Sync, Async} {
		if err := r.CheckAssignment(c.Cluster, outside, v.ID, v.History, role); err == nil {
			t.Fatal("outside origin acquired placement", role)
		}
	}
	stale := v.History
	stale.Epoch++
	if err := r.CheckOrigin(c.Cluster, outside, v.ID, stale); err == nil {
		t.Fatal("socket epoch overrode history")
	}
	for _, admission := range []Admission{Pending, Removed} {
		c.Nodes[len(c.Nodes)-1].Admission = admission
		r, err := New(c)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.CheckOrigin(c.Cluster, outside, v.ID, v.History); err == nil {
			t.Fatal("unadmitted origin accepted")
		}
	}
}
