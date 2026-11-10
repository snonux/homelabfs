package viewcontrol

import (
	"encoding/json"
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// bootstrapRecall replays the zq3-r6 mount-0 bootstrap on domain i: acquire a
// READ lease, release it (which recalls), complete the recall and close the
// release. It returns the bootstrap description and its lease's fence.
func (r *rig) bootstrapRecall(i int, name string) (guestbridge.Capability, uint64) {
	r.t.Helper()
	_, description := r.create(i, name, false)
	_, read := r.acquire(i, page(description, 0), "READ")
	if read.Status != "GRANTED" {
		r.t.Fatalf("bootstrap acquire %+v", read)
	}
	payload := map[string]any{"request": r.controlTicket(i), "lease": read.Lease}
	if pending := r.view(i, "VIEW_RELEASE", payload); pending.Status != "PENDING" {
		r.t.Fatalf("bootstrap release %+v", pending)
	}
	r.poll(i)
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	return description, read.Lease.Fence
}

// TestDomainFenceAdvancesOnlyForRecallParticipants is the R6-1 regression:
// the guest kernel keeps one fence per mount, starting at 1 and advancing only
// on that mount's own REOPENED, so a domain that took no part in an earlier
// recall must still be issued fence 1.
func TestDomainFenceAdvancesOnlyForRecallParticipants(t *testing.T) {
	r := newRig(t)
	description, first := r.bootstrapRecall(0, "domain-fence")
	if first != 1 {
		t.Fatalf("domain A first lease fence %d, want 1", first)
	}
	other := r.open(1, "domain-fence")
	_, b := r.acquire(1, page(other, 0), "READ")
	if b.Status != "GRANTED" || b.Lease.Fence != 1 {
		t.Fatalf("domain B first lease after A's recall: %+v, want fence 1", b)
	}
	_, a := r.acquire(0, page(description, 4096), "READ")
	if a.Status != "GRANTED" || a.Lease.Fence != 2 {
		t.Fatalf("domain A lease after its recall: %+v, want fence 2", a)
	}
	// The per-domain fences are protected state and survive a reopen.
	r.reopen()
	r.controller.mu.Lock()
	fa, fb := r.controller.fence(r.scopes[0].Session), r.controller.fence(r.scopes[1].Session)
	r.controller.mu.Unlock()
	if fa != 2 || fb != 1 {
		t.Fatalf("reopened fences A=%d B=%d, want 2 and 1", fa, fb)
	}
}

// TestLedgerSchemaOneRefusedOnReopen keeps persisted-ledger compatibility
// explicit: a schema-1 ledger (one controller-wide Fence, no Schema) cannot
// be split into per-domain fences, so reopening it refuses with Permission.
func TestLedgerSchemaOneRefusedOnReopen(t *testing.T) {
	r := newRig(t)
	r.bootstrapRecall(0, "schema-one")
	must(t, r.controller.Close())
	db, err := bolt.Open(r.cc.Directory+"/viewcontrol-v1.db", 0600, nil)
	must(t, err)
	must(t, db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(version))
		var state map[string]json.RawMessage
		if err := json.Unmarshal(b.Get([]byte("state")), &state); err != nil {
			return err
		}
		delete(state, "Schema")
		delete(state, "Fences")
		state["Fence"] = json.RawMessage("2")
		raw, err := json.Marshal(state)
		if err != nil {
			return err
		}
		return b.Put([]byte("state"), raw)
	}))
	must(t, db.Close())
	if _, err = New(t.Context(), r.cc); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("schema-1 ledger reopened: %v", err)
	}
}
