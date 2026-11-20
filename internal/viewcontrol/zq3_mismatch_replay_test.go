package viewcontrol

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// pinReplay replays the T1 credentials pattern past the mount roots: m0
// created the case files, m1 stats them (LOOKUP pins each object, GETATTR),
// m0 keeps observing its root, and m1's dentry eviction FORGETs them. Every
// syscall is one guest observation with expected_target = the mount's
// installed view; a PENDING acquisition waits for its recall, is BLOCKED for
// the old view and is retried on the newly installed one, as the guest's
// hfs_mmap_begin does. installed follows every recall's participants. After
// each observation the mount retires its answered view tickets, as the
// guest's cleanup does within the zq3 spec's 16 live tickets; compaction then
// drops the released leases and pruning the cuts they pinned, so a mismatch
// is classified from the remembered digest of a pruned cut (observed_cuts.go).
type pinReplay struct {
	r         *rig
	installed [2]guestbridge.ViewTarget
	seen      int
	acquired  [2][]string // reply ids of the acquisition family, by number
	released  [2][]string // reply ids of the control family, by number
	floors    [2][2]int   // retired acquisition and control floors
}

// follow updates the installed views of every domain a recall since the
// last call reinstalled. Recalls complete inline and cut-changing ops wait
// for them, so the current cut is what the last one installed.
func (p *pinReplay) follow() {
	records := history(p.r.t, p.r.controller).Recalls
	for _, record := range records[p.seen:] {
		for _, session := range record.Participants {
			for i, scope := range p.r.scopes {
				if scope.Session == session {
					p.installed[i] = p.r.currentTarget(i)
				}
			}
		}
	}
	p.seen = len(records)
}

// observation runs op on mount i under one observation lease, retrying the
// acquisition after each mismatch recall at most twice, then retires the
// mount's answered tickets.
func (p *pinReplay) observation(i int, op string, args map[string]any) result {
	p.r.t.Helper()
	for range 3 {
		ticket := p.r.ticket(i)
		reply := p.r.view(i, "VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": p.r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": p.installed[i]})
		if reply.Status == "PENDING" {
			p.r.completeCleanRecall()
			reply = p.r.view(i, "VIEW_QUERY", map[string]any{"request": ticket})
			if reply.Status != "BLOCKED" {
				p.r.t.Fatalf("m%d %s stale ticket after its recall: %+v", i, op, reply)
			}
			p.acquired[i] = append(p.acquired[i], reply.ID)
			p.follow()
			continue
		}
		if reply.Status != "GRANTED" {
			p.r.t.Fatalf("m%d %s observation: %+v", i, op, reply)
		}
		p.acquired[i] = append(p.acquired[i], reply.ID)
		res := p.r.call(i, op, args)
		released := p.r.release(i, reply.Lease)
		if released.Status != "RELEASED" {
			p.r.t.Fatalf("m%d %s release: %+v", i, op, released)
		}
		p.released[i] = append(p.released[i], released.ID)
		p.follow()
		p.retire(i)
		return res
	}
	p.r.t.Fatalf("m%d %s: no grant after two recalls", i, op)
	return result{}
}

// retire retires mount i's control and acquisition tickets answered since
// the last retirement and requires RETIRED for both.
func (p *pinReplay) retire(i int) {
	p.r.t.Helper()
	ctl, acq := p.released[i], p.acquired[i]
	if receipt := p.r.retireControl(i, uint64(len(ctl)), ctl[p.floors[i][1]:]); !bytes.Contains(receipt, []byte(`"RETIRED"`)) {
		p.r.t.Fatalf("m%d control retirement: %s", i, receipt)
	}
	if receipt := p.r.retire(i, uint64(len(acq)), acq[p.floors[i][0]:]); !bytes.Contains(receipt, []byte(`"RETIRED"`)) {
		p.r.t.Fatalf("m%d acquisition retirement: %s", i, receipt)
	}
	p.floors[i] = [2]int{len(acq), len(ctl)}
}

// run replays the pattern and returns the all-domain and scoped recalls it
// started after the setup.
func (p *pinReplay) run(files []string) (all, scoped int) {
	for _, name := range files {
		path, description := p.r.create(0, name, false)
		p.r.dropLifetimes(0, path, description)
	}
	for i := range p.r.roots {
		p.installed[i] = p.r.currentTarget(i)
		p.observation(i, "GETATTR", map[string]any{"object": p.r.roots[i]})
	}
	p.follow()
	start := p.seen
	root := p.r.call(1, "ROOT", map[string]any{}).Capabilities[0]
	for _, name := range files {
		object := p.observation(1, "LOOKUP", map[string]any{"parent": root, "name": hex.EncodeToString([]byte(name))}).Capabilities[0]
		p.observation(1, "GETATTR", map[string]any{"object": object})
		p.observation(0, "GETATTR", map[string]any{"object": p.r.roots[0]})
		p.r.call(1, "FORGET", map[string]any{"object": object, "count": 1})
		p.observation(0, "GETATTR", map[string]any{"object": p.r.roots[0]})
		p.observation(1, "GETATTR", map[string]any{"object": p.r.roots[1]})
	}
	for _, record := range history(p.r.t, p.r.controller).Recalls[start:] {
		if record.Scope == "ALL" {
			all++
		} else {
			scoped++
		}
	}
	return all, scoped
}

// TestZQ3PinningObservationsScopeMismatchRecalls replays the pattern twice at
// the zq3 spec bounds: with the pre-e84 all-domain mismatch recall
// (allDomainMismatch) and with e84. Each pinning LOOKUP and each FORGET moves
// the cut by pins only; the peer's next observation then mismatches. Before
// e84 each such mismatch stopped both mounts (6 all-domain + 3 scoped
// recalls); with e84 none does (0 + 12: each mount now reinstalls only its
// own view, so more recalls but fewer domain stops and no global barrier),
// and the same observations all succeed. The retirements let pruning drop
// the expected cuts before their mismatches, so the scoping rests on the
// remembered digests; without them the replay still needs 6 all-domain
// recalls.
func TestZQ3PinningObservationsScopeMismatchRecalls(t *testing.T) {
	files := []string{"setgid-created", "group", "group-tail"}
	baseline := newZQ3Rig(t, 0)
	baseline.controller.allDomainMismatch = true
	beforeAll, beforeScoped := (&pinReplay{r: baseline}).run(files)
	allNow, scopedNow := (&pinReplay{r: newZQ3Rig(t, 0)}).run(files)
	t.Logf("pinning observations: before e84 %d all-domain + %d scoped recalls, with e84 %d all-domain + %d scoped", beforeAll, beforeScoped, allNow, scopedNow)
	if beforeAll == 0 || allNow != 0 {
		t.Fatalf("all-domain recalls: before e84 %d (want > 0), with e84 %d (want 0)", beforeAll, allNow)
	}
}
