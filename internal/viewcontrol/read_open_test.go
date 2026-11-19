package viewcontrol

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Read-only OPEN conflict tests (task z94, zq3-r11 R11-1, zq3-r14 R14-1):
// guest.py's open(O_RDONLY|O_DIRECTORY) of a mount root conflicts like a
// read, so neither the caller's own observation lease nor a peer's READ
// lease starts a recall, while an EXCLUSIVE_DIRTY lease and an in-progress
// recall still hold it.

// openRootRead sends domain i's read-only OPEN of its mount root and returns
// the claim's QUERY status.
func (r *rig) openRootRead(i int) (guestbridge.Call, string) {
	r.t.Helper()
	return r.pendingCall(i, "OPEN", map[string]any{"object": r.roots[i], "flags": flags("READ", false)})
}

// requireNoRecall requires that no recall exists and every lease is active.
func (r *rig) requireNoRecall(t *testing.T, what string, leases ...*guestbridge.ViewLease) {
	t.Helper()
	if _, _, exists := r.recallSnapshot(); exists {
		t.Fatalf("%s started a recall", what)
	}
	for _, lease := range leases {
		if !r.leaseActive(lease) {
			t.Fatalf("%s revoked lease %s", what, lease.ID)
		}
	}
}

// TestReadOnlyOpenIgnoresOwnLease is R11-1: the root OPEN under the
// caller's own DOMAIN READ observation lease used to recall all domains.
func TestReadOnlyOpenIgnoresOwnLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	own := r.domainRead(0)
	if _, status := r.openRootRead(0); status != "TERMINAL" {
		t.Fatalf("read-only root OPEN under its own lease: %s", status)
	}
	r.requireNoRecall(t, "read-only OPEN", own)
}

// TestReadOnlyOpenIgnoresPeerReadLease: a peer's READ lease caches nothing
// a read-only OPEN changes, so it starts no recall either.
func TestReadOnlyOpenIgnoresPeerReadLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	own, peer := r.domainRead(0), r.domainRead(1)
	if _, status := r.openRootRead(0); status != "TERMINAL" {
		t.Fatalf("read-only root OPEN under a peer READ lease: %s", status)
	}
	r.requireNoRecall(t, "read-only OPEN", own, peer)
}

// TestWritableOpenStillRecallsReadLease: an OPEN with write access stays a
// non-read and still recalls a peer's READ lease.
func TestWritableOpenStillRecallsReadLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, _ := r.create(0, "writable", false)
	r.domainRead(1)
	if _, status := r.pendingCall(0, "OPEN", map[string]any{"object": path, "flags": flags("READWRITE", false)}); status != "INDETERMINATE" {
		t.Fatalf("writable OPEN under a peer READ lease: %s", status)
	}
	if _, _, exists := r.recallSnapshot(); !exists {
		t.Fatal("writable OPEN started no recall")
	}
}

// TestReadOnlyOpenRecallsPeerExclusiveDirty: a peer's EXCLUSIVE_DIRTY lease
// may hold a dirty overlay the OPEN's attributes would miss, so the OPEN
// waits and starts the all-domain recall, as a read does.
func TestReadOnlyOpenRecallsPeerExclusiveDirty(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "dirty", false)
	if _, write := r.acquire(1, page(description, 0), "EXCLUSIVE_DIRTY"); write.Status != "GRANTED" {
		t.Fatalf("write lease %+v", write)
	}
	if _, status := r.openRootRead(0); status != "INDETERMINATE" {
		t.Fatalf("read-only OPEN under a peer EXCLUSIVE_DIRTY lease: %s", status)
	}
	if scope, _, exists := r.recallSnapshot(); !exists || scope != "" {
		t.Fatalf("recall scope %q exists %v", scope, exists)
	}
}

// TestReadOnlyOpenWaitsForRecall: the OPEN may pin and so move the cut, so
// even the other domain's scoped recall holds it until REOPEN; it then
// completes without a recall of its own.
func TestReadOnlyOpenWaitsForRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	own := r.domainRead(0)
	payload := r.startRelease(1, r.domainRead(1))
	open, status := r.openRootRead(0)
	if status != "INDETERMINATE" {
		t.Fatalf("read-only OPEN during a scoped recall: %s", status)
	}
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("scoped release %+v", reply)
	}
	if status := r.queryStatus(0, open); status != "TERMINAL" {
		t.Fatalf("read-only OPEN after the recall: %s", status)
	}
	r.requireNoRecall(t, "read-only OPEN after the recall", own)
}

// TestReadOnlyOpenClassification pins which OPEN flag sets count as reads.
func TestReadOnlyOpenClassification(t *testing.T) {
	withFlag := func(key string) map[string]any {
		f := flags("READ", false)
		f[key] = true
		return f
	}
	cases := []struct {
		name string
		args any
		want bool
	}{
		{"read", map[string]any{"flags": flags("READ", false)}, true},
		{"write", map[string]any{"flags": flags("WRITE", false)}, false},
		{"readwrite", map[string]any{"flags": flags("READWRITE", false)}, false},
		{"create", map[string]any{"flags": withFlag("create")}, false},
		{"exclusive without create", map[string]any{"flags": withFlag("exclusive")}, false},
		{"truncate", map[string]any{"flags": withFlag("truncate")}, false},
		{"append", map[string]any{"flags": withFlag("append")}, false},
		{"no flags", map[string]any{}, false},
	}
	for _, c := range cases {
		raw, err := json.Marshal(c.args)
		must(t, err)
		if got := readOnlyOpen(guestbridge.Intent{Opcode: "OPEN", Args: raw}); got != c.want {
			t.Errorf("%s: readOnlyOpen %v, want %v", c.name, got, c.want)
		}
	}
	if readOnlyOpen(guestbridge.Intent{Opcode: "CREATE", Args: json.RawMessage(`{"flags":{"access":"READ"}}`)}) {
		t.Error("CREATE classified as a read-only OPEN")
	}
}

// observeInstalled is a guest observation acquire of domain i: a DOMAIN READ
// VIEW_ACQUIRE whose expected target is the domain's installed view, as the
// guest sends it (Retained-9p-view-leases.md), unlike rig.acquire's nil.
func (r *rig) observeInstalled(i int, installed guestbridge.ViewTarget) viewReply {
	r.t.Helper()
	return r.view(i, "VIEW_ACQUIRE", map[string]any{"request": r.ticket(i), "domain": r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": installed})
}

// requireObserved requires that both domains' observations of their
// installed views are granted without a recall.
func (r *rig) requireObserved(t *testing.T, what string, installed [2]guestbridge.ViewTarget) {
	t.Helper()
	for i := range installed {
		if reply := r.observeInstalled(i, installed[i]); reply.Status != "GRANTED" {
			t.Fatalf("%s: m%d observation of its installed view: %+v", what, i, reply)
		}
	}
	r.requireNoRecall(t, what)
}

// TestZQ3CredentialsAdmissionKeepsLeases replays T1 (credentials)
// admission at the zq3 spec limits with real expected targets: both domains
// hold DOMAIN READ observation leases on their installed views; the guest.py
// request process opens and fstats both mount roots, then both domains
// observe again with expected_target = the installed view, inside the
// process and after its closes and exit. Before z94 each root OPEN recalled
// every domain (a conflict with the observation leases), and with that fixed
// the root's HandlePin/HandleRelease still moved the cut, so the next
// observation hit the expected-target mismatch and recalled every domain.
// The mount(8) processes stay alive (no bootstrap): the rig's view tickets
// carry their credentials.
func TestZQ3CredentialsAdmissionKeepsLeases(t *testing.T) {
	limits, bounds := zq3SpecLimits(t), zq3SpecBounds(t)
	r := newRigWith(t, limits, func(c *Config) { c.Authorization.Enrollment.Bounds = bounds })
	w := newZQ3World(r, zq3GuestWindow)
	installed := [2]guestbridge.ViewTarget{r.domainRead(0).Target, r.domainRead(1).Target}
	w.begin("T credentials admission")
	w.guestPy(func(p *zq3Proc, roots [2]*zq3File) {
		r.requireObserved(t, "after the root OPENs", installed)
	})
	r.requireObserved(t, "after the root closes and exit", installed)
}

// TestRootOpenKeepsInstalledTarget contrasts the pinless root with a real
// namespace change: after m1's clean release recall installed the current
// cut, a root OPEN and its final DESCRIPTION_RELEASE by m0 leave that cut, so
// m1's observation of it is granted; a CREATE by m0 moves it, so the same
// observation hits the expected-target mismatch and starts the all-domain
// recall.
func TestRootOpenKeepsInstalledTarget(t *testing.T) {
	for _, change := range []string{"root-open", "create"} {
		t.Run(change, func(t *testing.T) {
			r := newZQ3Rig(t, 0)
			lease := r.domainRead(1)
			if reply := r.release(1, lease); reply.Status != "RELEASED" {
				t.Fatalf("release %+v", reply)
			}
			if change == "create" {
				r.create(0, "moves-target", false)
			} else {
				opened := r.call(0, "OPEN", map[string]any{"object": r.roots[0], "flags": flags("READ", false)})
				r.call(0, "DESCRIPTION_RELEASE", map[string]any{"description": opened.Capabilities[0], "release_sequence": 1})
			}
			reply := r.observeInstalled(1, lease.Target)
			_, _, recall := r.recallSnapshot()
			if want := change == "root-open"; (reply.Status == "GRANTED") != want || recall == want {
				t.Fatalf("%s: observation %+v recall %v", change, reply, recall)
			}
		})
	}
}
