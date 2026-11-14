package viewcontrol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Recall scope tests (task 784, zq3-r8 R8-1). The first two are the offline
// repros of the two stranded zq3-r8 claims; they used to stay INDETERMINATE
// behind a recall and must now be TERMINAL without any recall that involves
// the claiming domain. The negative tests pin the barriers that must stay
// global: dirty and exclusive releases, and cut-changing ops.

// pendingCall sends a fresh CALL, requires the ADMITTED answer the bridge
// gives even when admission refused, and returns the claim's QUERY status.
func (r *rig) pendingCall(i int, op string, args any) (guestbridge.Call, string) {
	r.t.Helper()
	call := r.originalCall(i, op, args)
	first, err := r.execute(i, "CALL", call)
	must(r.t, err)
	if !bytes.Contains(first, []byte(`"ADMITTED"`)) {
		r.t.Fatalf("CALL %s: %s", op, first)
	}
	return call, r.queryStatus(i, call)
}

// queryStatus returns the current QUERY status of an earlier CALL.
func (r *rig) queryStatus(i int, call guestbridge.Call) string {
	r.t.Helper()
	raw, err := r.execute(i, "QUERY", map[string]any{"claim": call.Claim})
	must(r.t, err)
	var query struct{ Status string }
	must(r.t, json.Unmarshal(raw, &query))
	return query.Status
}

// domainRead grants domain i a DOMAIN READ lease, as every short-lived guest
// process that touches the mount takes one.
func (r *rig) domainRead(i int) *guestbridge.ViewLease {
	r.t.Helper()
	_, granted := r.acquire(i, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	if granted.Status != "GRANTED" || granted.Lease == nil {
		r.t.Fatalf("DOMAIN READ %d: %+v", i, granted)
	}
	return granted.Lease
}

// pageRead registers domain i with a PAGE READ lease on its own new file, so
// it participates in an all-domain recall without conflicting with another
// domain's EXCLUSIVE_DIRTY page (a DOMAIN lease would conflict with it).
func (r *rig) pageRead(i int) {
	r.t.Helper()
	_, description := r.create(i, "peer-page", false)
	if _, granted := r.acquire(i, page(description, 0), "READ"); granted.Status != "GRANTED" {
		r.t.Fatalf("PAGE READ %d: %+v", i, granted)
	}
}

// recallSnapshot returns the current recall's scope and the indexes of the
// domains that received its events, and whether a recall exists at all.
func (r *rig) recallSnapshot() (scope string, participants []int, exists bool) {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	recall := r.controller.state.Recall
	if recall == nil {
		return "", nil, false
	}
	for i, s := range r.scopes {
		if len(recall.Events[s.Session]) > 0 {
			participants = append(participants, i)
		}
	}
	return recall.Domain, participants, true
}

// leaseActive reports whether a lease is still active in the live ledger.
func (r *rig) leaseActive(lease *guestbridge.ViewLease) bool {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	return r.controller.state.Leases[lease.ID].Active
}

// startRelease sends a preowned VIEW_RELEASE that must answer PENDING and
// returns its payload for the later physical fact and QUERY.
func (r *rig) startRelease(i int, lease *guestbridge.ViewLease) map[string]any {
	r.t.Helper()
	payload := map[string]any{"request": r.controlTicket(i), "lease": lease}
	if reply := r.view(i, "VIEW_RELEASE", payload); reply.Status != "PENDING" {
		r.t.Fatalf("release %d: %+v", i, reply)
	}
	return payload
}

// TestProcessExitIgnoresPeerReadLease is zq3-r8's m0 claim: the PROCESS_EXIT
// of a probe child while m1 holds an active DOMAIN READ lease. An authority
// reduction does not conflict, so it reaches TERMINAL with no recall and
// m1's lease stays active.
func TestProcessExitIgnoresPeerReadLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.domainRead(0)
	peer := r.domainRead(1)
	before, err := r.controller.capture(t.Context())
	must(t, err)
	_, status := r.pendingCall(0, "PROCESS_EXIT", map[string]any{"process": r.credentials[0].Process, "exit_sequence": 1})
	if status != "TERMINAL" {
		t.Fatalf("PROCESS_EXIT behind a peer READ lease: %s", status)
	}
	if _, _, exists := r.recallSnapshot(); exists {
		t.Fatal("PROCESS_EXIT started a recall")
	}
	if !r.leaseActive(peer) {
		t.Fatal("PROCESS_EXIT revoked the peer lease")
	}
	// PROCESS_EXIT never waits for a recall because it keeps the cut.
	after, err := r.controller.capture(t.Context())
	must(t, err)
	if sourceKey(before) != sourceKey(after) {
		t.Fatal("PROCESS_EXIT changed the source cut")
	}
}

// TestScopedReleaseRecallAdmitsPeerGetattr is zq3-r8's m1 claim: a root
// GETATTR while the other domain's clean READ release recall runs. The
// release recalls only its own domain, so the GETATTR, a peer READ
// acquisition and a peer PROCESS_EXIT all proceed, and completing the recall
// advances only the releasing domain's fence.
func TestScopedReleaseRecallAdmitsPeerGetattr(t *testing.T) {
	r := newZQ3Rig(t, 0)
	own := r.domainRead(0)
	released := r.domainRead(1)
	payload := r.startRelease(1, released)
	scope, participants, exists := r.recallSnapshot()
	if !exists || scope != r.scopes[1].Session || len(participants) != 1 || participants[0] != 1 {
		t.Fatalf("release recall scope %q participants %v", scope, participants)
	}
	if polled := r.poll(0); bytes.Contains(polled, []byte(`"EVENT"`)) {
		t.Fatalf("peer received an event of the scoped recall: %s", polled)
	}
	if _, status := r.pendingCall(0, "GETATTR", map[string]any{"object": r.roots[0]}); status != "TERMINAL" {
		t.Fatalf("peer GETATTR during a scoped recall: %s", status)
	}
	if !r.leaseActive(own) {
		t.Fatal("scoped recall revoked the peer lease")
	}
	r.domainRead(0) // a peer READ acquisition is not closed either
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("scoped release %+v", reply)
	}
	if r.controller.fence(r.scopes[0].Session) != 1 || r.controller.fence(r.scopes[1].Session) != 2 {
		t.Fatalf("fences m0 %d m1 %d", r.controller.fence(r.scopes[0].Session), r.controller.fence(r.scopes[1].Session))
	}
	if _, status := r.pendingCall(0, "PROCESS_EXIT", map[string]any{"process": r.credentials[0].Process, "exit_sequence": 1}); status != "TERMINAL" {
		t.Fatalf("PROCESS_EXIT after the scoped recall: %s", status)
	}
}

// TestAuthorityReductionsStartNoRecall closes a description and a path grant
// while the peer holds an active DOMAIN READ lease: neither starts a recall.
func TestAuthorityReductionsStartNoRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "reduce", false)
	peer := r.domainRead(1)
	for _, step := range []struct {
		op   string
		args map[string]any
	}{
		{"DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1}},
		{"FORGET", map[string]any{"object": path, "count": 1}},
	} {
		if _, status := r.pendingCall(0, step.op, step.args); status != "TERMINAL" {
			t.Fatalf("%s behind a peer READ lease: %s", step.op, status)
		}
		if _, _, exists := r.recallSnapshot(); exists {
			t.Fatalf("%s started a recall", step.op)
		}
	}
	if !r.leaseActive(peer) {
		t.Fatal("authority reduction revoked the peer lease")
	}
}

// TestScopedRecallHoldsCutChangingOps: during a scoped recall of m1, the
// other domain's ops that may change the cut wait (a CREATE, and a FORGET
// that appends lifetime state), because the guest refuses a REOPEN whose
// target is not the INSTALL_FINAL cut; an EXCLUSIVE_DIRTY acquisition waits
// too. All of them proceed once the scoped recall reopened.
func TestScopedRecallHoldsCutChangingOps(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "held", false)
	released := r.domainRead(1)
	payload := r.startRelease(1, released)
	forget, status := r.pendingCall(0, "FORGET", map[string]any{"object": path, "count": 1})
	if status != "INDETERMINATE" {
		t.Fatalf("FORGET during a scoped recall: %s", status)
	}
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0] // a read: admitted
	create, status := r.pendingCall(0, "CREATE", map[string]any{"parent": root, "name": "68656c642d32", "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	if status != "INDETERMINATE" {
		t.Fatalf("CREATE during a scoped recall: %s", status)
	}
	_, write := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	if write.Status != "PENDING" {
		t.Fatalf("EXCLUSIVE_DIRTY acquisition during a scoped recall: %+v", write)
	}
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("scoped release %+v", reply)
	}
	for _, call := range []guestbridge.Call{forget, create} {
		if status := r.queryStatus(0, call); status != "TERMINAL" {
			t.Fatalf("%s after the scoped recall: %s", call.Intent.Opcode, status)
		}
	}
}

// TestDirtyReleaseRecallStaysGlobal: releasing an EXCLUSIVE_DIRTY lease with
// an open reservation recalls every domain, and the peer's read waits.
func TestDirtyReleaseRecallStaysGlobal(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "dirty", false)
	r.pageRead(0)
	_, write := r.acquire(1, page(description, 0), "EXCLUSIVE_DIRTY")
	if write.Status != "GRANTED" {
		t.Fatalf("write lease %+v", write)
	}
	_, reservePayload := r.reserve(1, write.Lease, description)
	if reserved := r.view(1, "DIRTY_RESERVE", reservePayload); reserved.Status != "GRANTED" {
		t.Fatalf("reservation %+v", reserved)
	}
	r.startRelease(1, write.Lease)
	r.requireGlobalRecallBlocksPeer(t)
	// PROCESS_EXIT keeps the cut, so even an all-domain recall never holds it.
	if _, status := r.pendingCall(0, "PROCESS_EXIT", map[string]any{"process": r.credentials[0].Process, "exit_sequence": 1}); status != "TERMINAL" {
		t.Fatalf("PROCESS_EXIT during an all-domain recall: %s", status)
	}
}

// TestExclusiveDomainReadReleaseStaysGlobal: a clean READ release of a
// domain that also holds an EXCLUSIVE_DIRTY lease recalls every domain.
func TestExclusiveDomainReadReleaseStaysGlobal(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "exclusive", false)
	r.pageRead(0)
	_, read := r.acquire(1, page(description, 4096), "READ")
	if read.Status != "GRANTED" {
		t.Fatalf("read lease %+v", read)
	}
	_, write := r.acquire(1, page(description, 0), "EXCLUSIVE_DIRTY")
	if write.Status != "GRANTED" {
		t.Fatalf("write lease %+v", write)
	}
	r.startRelease(1, read.Lease)
	r.requireGlobalRecallBlocksPeer(t)
	// After both STOPPED ACKs every lease is inactive and no reservation is
	// open, so nothing conflicts: only the all-domain barrier holds the read.
	r.ack(0, "STOPPED", r.emptyManifest())
	r.ack(1, "STOPPED", r.emptyManifest())
	r.poll(0)
	if r.leaseActive(write.Lease) {
		t.Fatal("write lease still active after both STOPPED")
	}
	if _, status := r.pendingCall(0, "GETATTR", map[string]any{"object": r.roots[0]}); status != "INDETERMINATE" {
		t.Fatalf("peer GETATTR after STOPPED in an all-domain recall: %s", status)
	}
}

// TestExpectedMismatchRecallStaysGlobal isolates the all-domain barrier with
// no exclusive lease and no reservation anywhere: the final
// DESCRIPTION_RELEASE and FORGET of an object under the peer's lease move the
// cut (lifetime state), m1's next observation acquire expects its old target,
// and the expected-target mismatch starts an all-domain recall. m0's root
// GETATTR conflicts with nothing, yet it waits; its PROCESS_EXIT does not.
func TestExpectedMismatchRecallStaysGlobal(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "moves-cut", false)
	r.domainRead(0)
	installed := r.domainRead(1)
	for _, step := range []struct {
		op   string
		args map[string]any
	}{
		{"DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1}},
		{"FORGET", map[string]any{"object": path, "count": 1}},
	} {
		if _, status := r.pendingCall(0, step.op, step.args); status != "TERMINAL" {
			t.Fatalf("%s under peer leases: %s", step.op, status)
		}
	}
	stale := r.view(1, "VIEW_ACQUIRE", map[string]any{"request": r.ticket(1), "domain": r.domains[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": installed.Target})
	if stale.Status != "PENDING" {
		t.Fatalf("stale observation acquire %+v", stale)
	}
	r.requireGlobalRecallBlocksPeer(t)
	if _, status := r.pendingCall(0, "PROCESS_EXIT", map[string]any{"process": r.credentials[0].Process, "exit_sequence": 1}); status != "TERMINAL" {
		t.Fatalf("PROCESS_EXIT during an all-domain recall: %s", status)
	}
}

// TestScopedRecallHoldsPeerLookup is the review repro of task 784: a peer
// LOOKUP journals a HandlePin and so moves the cut. Admitted between a scoped
// recall's INSTALL_FINAL and REOPEN it made the REOPEN target differ from the
// installed one, which the guest refuses. It must wait, and the REOPEN
// target must equal the INSTALL_FINAL target.
func TestScopedRecallHoldsPeerLookup(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "pinme", false)
	// Drop every lifetime of the object, so the next LOOKUP must pin it.
	r.call(0, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	r.call(0, "FORGET", map[string]any{"object": path, "count": 1})
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	payload := r.startRelease(1, r.domainRead(1))
	r.ack(1, "STOPPED", r.emptyManifest())
	r.poll(1)
	r.ack(1, "INSTALLED_DRAIN", nil)
	r.ack(1, "DRAINED", r.emptyManifest())
	r.poll(1)
	final := r.eventTarget(1)
	lookup, status := r.pendingCall(0, "LOOKUP", map[string]any{"parent": root, "name": hex.EncodeToString([]byte("pinme"))})
	if status != "INDETERMINATE" {
		t.Fatalf("peer LOOKUP during a scoped recall: %s", status)
	}
	r.ack(1, "INSTALLED_FINAL", nil)
	r.poll(1)
	if reopen := r.eventTarget(1); reopen != final {
		t.Fatalf("REOPEN target %s differs from INSTALL_FINAL target %s", reopen, final)
	}
	r.ack(1, "REOPENED", nil)
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("scoped release %+v", reply)
	}
	if status := r.queryStatus(0, lookup); status != "TERMINAL" {
		t.Fatalf("peer LOOKUP after the scoped recall: %s", status)
	}
}

// eventTarget returns the target of domain i's current recall event.
func (r *rig) eventTarget(i int) string {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	var e map[string]json.RawMessage
	must(r.t, json.Unmarshal(r.controller.state.Recall.Events[r.scopes[i].Session], &e))
	return string(e["target"])
}

// requireGlobalRecallBlocksPeer requires an all-domain recall with events for
// both domains, under which m0's root GETATTR waits. (Where a lease or
// reservation still conflicts, the conflict alone would also hold it; the
// STOPPED and expected-mismatch tests isolate the barrier.)
func (r *rig) requireGlobalRecallBlocksPeer(t *testing.T) {
	t.Helper()
	scope, participants, exists := r.recallSnapshot()
	if !exists || scope != "" || len(participants) != 2 {
		t.Fatalf("recall scope %q participants %v exists %v", scope, participants, exists)
	}
	if _, status := r.pendingCall(0, "GETATTR", map[string]any{"object": r.roots[0]}); status != "INDETERMINATE" {
		t.Fatalf("peer GETATTR during an all-domain recall: %s", status)
	}
}
