package viewcontrol

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// mutator drives lease-free mutations from one domain. Each one advances the
// journal, so without pruning each one kept a retained source cut.
type mutator struct {
	r           *rig
	domain      int
	root        guestbridge.Capability
	file        guestbridge.Capability
	description guestbridge.Capability
	dir         guestbridge.Capability
	done        int
}

func (m *mutator) name(format string, n int) string {
	return hex.EncodeToString([]byte(fmt.Sprintf("%d-"+format, m.domain, n)))
}

// newMutator CREATEs the domain's write target "w<domain>" (its first
// mutation).
func newMutator(r *rig, domain int) *mutator {
	m := &mutator{r: r, domain: domain, root: r.call(domain, "ROOT", map[string]any{}).Capabilities[0]}
	m.file, m.description = r.create(domain, fmt.Sprintf("w%d", domain), false)
	m.done = 1
	return m
}

// next is mutation k of a six-step cycle: WRITE, SETATTR, MKDIR, FORGET of
// that directory (its last lookup, so the grant does not leak), RENAME and
// RMDIR. Every step advances the journal.
func (m *mutator) next(k int) (string, map[string]any) {
	start := k - k%6
	switch k % 6 {
	case 0:
		return "WRITE", map[string]any{"description": m.description, "offset": k, "data": "61", "max_end": 4096}
	case 1:
		attributes := map[string]any{"mode": 0600 + (k/6%2)*040, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
		return "SETATTR", map[string]any{"object": m.file, "attributes": attributes}
	case 2:
		return "MKDIR", map[string]any{"parent": m.root, "name": m.name("d%d", start), "mode": 0770, "umask": 0}
	case 3:
		return "FORGET", map[string]any{"object": m.dir, "count": 1}
	case 4:
		return "RENAME", map[string]any{"source_parent": m.root, "source_name": m.name("d%d", start), "destination_parent": m.root, "destination_name": m.name("m%d", start), "no_replace": true}
	default:
		return "RMDIR", map[string]any{"parent": m.root, "name": m.name("m%d", start)}
	}
}

// run performs n mutations, requiring each to reach TERMINAL OK.
func (m *mutator) run(n int) {
	m.r.t.Helper()
	for range n {
		op, args := m.next(m.done)
		result := m.r.call(m.domain, op, args)
		if result.Code != "OK" {
			m.r.t.Fatalf("domain %d mutation %d %s: %+v", m.domain, m.done, op, result)
		}
		if op == "MKDIR" {
			m.dir = result.Capabilities[0]
		}
		m.done++
	}
}

// openClose OPENs and closes domain 0's "w0" from domain 1 (no new cut) and
// returns the opened description for a later READ lease.
func openClose(r *rig) guestbridge.Capability {
	r.t.Helper()
	description := r.open(1, "w0")
	closed := r.call(1, "DESCRIPTION_CLOSE", map[string]any{"description": description, "process": r.credentials[1].Process, "close_sequence": 1})
	if closed.Code != "OK" {
		r.t.Fatalf("DESCRIPTION_CLOSE: %+v", closed)
	}
	return description
}

// cleanTwoDomainRecall makes domain 1's READ request conflict with domain 0's
// write lease, runs the clean all-domain recall and leaves no lease active.
func (m *mutator) cleanTwoDomainRecall(other guestbridge.Capability) {
	m.r.t.Helper()
	if _, dirty := m.r.acquire(0, page(m.description, 0), "EXCLUSIVE_DIRTY"); dirty.Status != "GRANTED" {
		m.r.t.Fatalf("write lease: %+v", dirty)
	}
	if _, pending := m.r.acquire(1, page(other, 0), "READ"); pending.Status != "PENDING" {
		m.r.t.Fatalf("conflicting READ was not parked: %+v", pending)
	}
	if len(m.r.controller.state.Recall.Events) != 2 {
		m.r.t.Fatal("recall did not cover both domains")
	}
	m.r.completeCleanRecall()
}

// TestZQ3LeaseFreeMutationsOutliveSources runs 111 lease-free mutations at
// the zq3 spec's view bounds, with an OPEN/CLOSE, a two-domain recall and
// both PROCESS_EXITs mixed in. Before admissions pruned unpinned cuts (i64,
// then at Sources 12), every mutation kept its cut until a VIEW_RETIRE that
// ordinary I/O never sends: after the CREATE and nine more (Sources-3), the
// next mutation got NoSpace and stayed INDETERMINATE (zq3 T1 alone makes
// about 20 mutations).
// This rig's providers keep their default credit budgets (256 MiB / 4 MiB
// retained and 128 MiB / 2 MiB source credit, so 64 claims per domain), and
// this test sends no RETIRE, so each domain's CALLs stay below 64; the zq3
// provider values and retirement are TestZQ3RunWorkloadFitsLifetimeBounds'.
func TestZQ3LeaseFreeMutationsOutliveSources(t *testing.T) {
	r := newZQ3Rig(t, 0)
	m0 := newMutator(r, 0)
	m0.run(30)
	m0.cleanTwoDomainRecall(openClose(r))
	m1 := newMutator(r, 1)
	m1.run(50)
	r.exit(1)
	m0.run(29)
	if total := m0.done + m1.done; total < 100 {
		t.Fatalf("only %d mutations", total)
	}
	// The recall's inactive write lease pins its cut; the rest is current.
	if n := len(r.controller.state.Sources); n != 2 {
		t.Fatalf("%d cuts retained after lease-free mutations, want the lease cut plus current", n)
	}
	r.exit(0)
}

// TestPruningIsDurableAcrossReopen checks that a pruned ledger is the
// protected one: reopening recovers exactly the pruned set plus the current
// cut that New captures (the last mutation advanced it).
func TestPruningIsDurableAcrossReopen(t *testing.T) {
	r := newZQ3Rig(t, 0)
	m := newMutator(r, 0)
	m.run(14)
	before := map[string]bool{}
	for key := range r.controller.state.Sources {
		before[key] = true
	}
	if len(before) != 1 {
		t.Fatalf("%d cuts retained before reopen, want 1", len(before))
	}
	r.reopen()
	for key := range before {
		if _, ok := r.controller.state.Sources[key]; !ok {
			t.Fatal("reopen lost the protected cut")
		}
	}
	// The rig's bridges still name the closed controller, so the check stops
	// at the recovered ledger (as TestRealLocalTwoDomainFreezeDrainAppendAndRecall does).
	if after := len(r.controller.state.Sources); after != 2 {
		t.Fatalf("%d cuts after reopen, want the protected one plus current", after)
	}
}

// TestZQ3T1LikeCutUsage replays nine of zq3 T1's mutations (mkdir, fchmod,
// create, fchown, fchmod, pwrite, create, fchown, fchmod) at the real bounds
// and records the retained cuts after each: with pruning a lease-free
// sequence never holds more than the one current cut. T1 itself makes about
// 20 mutations plus lookups and closes (TestZQ3RunWorkloadFitsLifetimeBounds
// drives all of them).
func TestZQ3T1LikeCutUsage(t *testing.T) {
	r := newZQ3Rig(t, 0)
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	peak := 0
	step := func(name string, result result) result {
		if result.Code != "OK" {
			t.Fatalf("%s: %+v", name, result)
		}
		peak = max(peak, len(r.controller.state.Sources))
		t.Logf("%-8s cuts=%d", name, len(r.controller.state.Sources))
		return result
	}
	setattr := func(object guestbridge.Capability, mode, gid any) map[string]any {
		return map[string]any{"object": object, "attributes": map[string]any{"mode": mode, "uid": nil, "gid": gid, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}}
	}
	dir := step("mkdir", r.call(0, "MKDIR", map[string]any{"parent": root, "name": hex.EncodeToString([]byte("t1")), "mode": 0700, "umask": 0})).Capabilities[0]
	step("fchmod", r.call(0, "SETATTR", setattr(dir, 0750, nil)))
	for i, name := range []string{"t1-a", "t1-b"} {
		object, description := r.create(0, name, false)
		step("create", result{Code: "OK"})
		step("fchown", r.call(0, "SETATTR", setattr(object, nil, 200)))
		step("fchmod", r.call(0, "SETATTR", setattr(object, 0640, nil)))
		if i == 0 {
			step("pwrite", r.call(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61", "max_end": 4096}))
		}
	}
	if peak != 1 {
		t.Fatalf("T1-like sequence peaked at %d retained cuts, want 1", peak)
	}
}
