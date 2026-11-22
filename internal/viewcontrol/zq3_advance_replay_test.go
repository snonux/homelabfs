package viewcontrol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/guestviewmodel"
)

// advanceReplay replays the T1 (guest.py credentials()) case body of
// zq3-r19 at the observation level, with and without the guest-requested
// view advance (task xf4, advance.go). Every guest read RPC (GETATTR, LOOKUP,
// READ) is one observation: a DOMAIN READ VIEW_ACQUIRE expecting the mount's
// installed view, the CALL, then the VIEW_RELEASE. Mutations (MKDIR, CREATE,
// SETATTR, WRITE, OPEN, closes, FORGET) are direct CALLs, as in the guest.
//
// A stale acquisition takes the guest's path: without advance PENDING, the
// expected-target recall, BLOCKED, and a retry on the reinstalled view; with
// advance it is granted on the current cut and the guest adopts that target.
// installed follows every recall's participants (follow) and every adopted
// advance grant.
//
// The advance flag is the guest's support for advance, not a promise that it
// is sent: like hfs_mmap_observe_lease, an acquisition carries "advance":
// true only when the mount's guest view is cacheless under rule (task uj4,
// guestviewmodel). views model each mount's struct hfs_view: the mount root
// and every inode a LOOKUP, MKDIR or CREATE instantiated stay registered
// until their FORGET (the root fds and the case leaf fd keep theirs for the
// whole case), and the published observation counts while its lease is
// held. zq3-r20's guest counted every registered inode
// (guestviewmodel.CachelessR20), so it never sent advance while this replay
// still reported 37 advances.
//
// CALL_CONFLICT recalls: in the lab a mutation often finds the mount's own
// previous observation lease still active because its release is queued
// behind the guest's cleanup worker (follow-up F). The replay models that
// for the creating syscalls (MKDIR, CREATE): the last observation before
// them is released only after the mutation, so the mutation conflicts with
// it and starts the all-domain recall, in both runs alike.
//
// Dentries are never revalidated (hfs_d_revalidate returns 0): every walk
// unhashes the cached dentry, whose unpinned inode FORGETs its grant, and
// LOOKUPs again (invalidate). A LOOKUP of a name whose grants are all gone
// journals a handle pin and so moves the cut under its own observation
// lease, which is what made every such release recall before xf4.
//
// The calls run under the rig's mount credentials (the case's credential
// children are folded into the parent: their permission outcomes do not
// change the view traffic). The post-case inventory and the case's release
// process are left out: they repeat the walk pattern with no creating
// syscall, so no CALL_CONFLICT recall moves the fences there, and their
// pinning LOOKUPs may reach lineageBudget lineages at one fence, which
// costs one RELEASE recall per budget (the design expects 0-1 in T1; the
// lab run zq3-r20 measures it).
type advanceReplay struct {
	r         *rig
	advance   bool
	rule      guestviewmodel.Rule // the guest's cacheless test; nil is tj4's
	views     [2]*guestviewmodel.View
	sent      [2]int // acquisitions that carried "advance": true, per mount
	installed [2]guestbridge.ViewTarget
	seen      uint64         // recalls followed (RecallHistory.Total)
	kinds     map[string]int // followed recalls by trigger kind
	acquired  [2][]string
	released  [2][]string
	floors    [2][2]int
	closes    [2]int        // per-mount close and release sequences
	pending   [2][]zq3Claim // answered, unretired ordinary claims
	// dcache holds each mount's cached dentries and files the dentry each
	// open description pins (by description token), as zq3Guest does.
	dcache [2]map[string]*replayDentry
	files  map[string]*replayDentry
}

// replayDentry is a cached guest dentry: its grant, the open files that pin
// it, and whether it was unhashed (its grant is FORGOTTEN at the last close).
type replayDentry struct {
	cap  guestbridge.Capability
	pins int
	dead bool
}

// follow updates the installed views of every domain a recall since the
// last call reinstalled (recalls complete inline, so the current cut is what
// the last one installed) and counts those recalls by trigger kind. The
// ring keeps only RecallHistoryLimit records, so new ones are found by the
// history's Total; follow runs after every recall, long before the ring
// wraps past one it has not seen.
func (p *advanceReplay) follow() {
	h := history(p.r.t, p.r.controller)
	fresh := int(h.Total - p.seen)
	if fresh > len(h.Recalls) {
		p.r.t.Fatalf("%d recalls since the last follow, ring holds %d", fresh, len(h.Recalls))
	}
	for _, record := range h.Recalls[len(h.Recalls)-fresh:] {
		p.kinds[record.Kind]++
		for _, session := range record.Participants {
			for i, scope := range p.r.scopes {
				if scope.Session == session {
					p.installed[i] = p.r.currentTarget(i)
				}
			}
		}
	}
	p.seen = h.Total
}

// acquire takes mount i's observation lease on its installed view, retrying
// after each expected-target recall at most twice, and returns the granted
// lease, whose target the guest then has installed.
func (p *advanceReplay) acquire(i int) *guestbridge.ViewLease {
	r := p.r
	r.t.Helper()
	for range 3 {
		ticket := r.ticket(i)
		payload := map[string]any{"request": ticket, "domain": r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": p.installed[i]}
		if p.sendsAdvance(i) {
			payload["advance"] = true
			p.sent[i]++
		}
		reply := r.view(i, "VIEW_ACQUIRE", payload)
		if reply.Status == "PENDING" {
			r.completeCleanRecall()
			p.follow()
			reply = r.view(i, "VIEW_QUERY", map[string]any{"request": ticket})
			if reply.Status != "BLOCKED" {
				r.t.Fatalf("m%d stale ticket after its recall: %+v", i, reply)
			}
			p.acquired[i] = append(p.acquired[i], reply.ID)
			continue
		}
		if reply.Status != "GRANTED" || reply.Lease == nil {
			r.t.Fatalf("m%d observation: %+v", i, reply)
		}
		p.acquired[i] = append(p.acquired[i], reply.ID)
		p.installed[i] = reply.Lease.Target // adopted (equal unless advanced)
		p.views[i].Observations++           // published until its release
		return reply.Lease
	}
	r.t.Fatalf("m%d: no grant after two recalls", i)
	return nil
}

// release releases mount i's observation lease (r.release completes a
// release recall where one starts), then retires the answered tickets.
func (p *advanceReplay) release(i int, lease *guestbridge.ViewLease) {
	r := p.r
	r.t.Helper()
	p.views[i].Observations-- // hfs_mmap_end, before the release goes out
	released := r.release(i, lease)
	if released.Status != "RELEASED" {
		r.t.Fatalf("m%d release: %+v", i, released)
	}
	p.released[i] = append(p.released[i], released.ID)
	p.follow()
	p.retire(i)
}

// retire retires mount i's control and acquisition tickets answered since
// the last retirement and requires RETIRED for both (pinReplay.retire).
func (p *advanceReplay) retire(i int) {
	r := p.r
	r.t.Helper()
	ctl, acq := p.released[i], p.acquired[i]
	if receipt := r.retireControl(i, uint64(len(ctl)), ctl[p.floors[i][1]:]); !bytes.Contains(receipt, []byte(`"RETIRED"`)) {
		r.t.Fatalf("m%d control retirement: %s", i, receipt)
	}
	if receipt := r.retire(i, uint64(len(acq)), acq[p.floors[i][0]:]); !bytes.Contains(receipt, []byte(`"RETIRED"`)) {
		r.t.Fatalf("m%d acquisition retirement: %s", i, receipt)
	}
	p.floors[i] = [2]int{len(acq), len(ctl)}
}

// sendsAdvance is the guest's decision for mount i's next acquisition: the
// advance flag (support) and the cacheless rule over its view, sampled
// before the request as hfs_view_capture_ex does.
func (p *advanceReplay) sendsAdvance(i int) bool {
	rule := p.rule
	if rule == nil {
		rule = guestviewmodel.Cacheless
	}
	return p.views[i].Advance(rule, p.advance)
}

// instantiate registers the inode a LOOKUP, MKDIR or CREATE result gives
// mount i (hfs_inode_get registers every inode), keyed by its grant, which
// its FORGET evicts.
func (p *advanceReplay) instantiate(i int, object guestbridge.Capability) {
	p.views[i].Register(object.Token)
}

// observe runs one read RPC under its own observation lease.
func (p *advanceReplay) observe(i int, op string, args map[string]any) result {
	lease := p.acquire(i)
	res := p.do(i, op, args)
	p.release(i, lease)
	return res
}

// do runs one CALL. A conflict starts the all-domain recall (only a
// mutation meets one here); it is completed, and the CALL's QUERY then
// admits it. Every answered claim is retired once 32 are pending, as the
// guest's hfs_call retires when its provider reserve runs full.
func (p *advanceReplay) do(i int, op string, args map[string]any) result {
	r := p.r
	r.t.Helper()
	call := r.originalCall(i, op, args)
	first, err := r.execute(i, "CALL", call)
	must(r.t, err)
	if !bytes.Contains(first, []byte(`"ADMITTED"`)) {
		r.t.Fatalf("CALL %s: %s", op, first)
	}
	q := p.claim(i, call)
	if q.Status != "TERMINAL" {
		r.completeCleanRecall()
		p.follow()
		q = p.claim(i, call)
	}
	if q.Status != "TERMINAL" {
		r.t.Fatalf("%s after its recall: %s", op, q.Status)
	}
	r.facts.mu.Lock()
	for _, c := range q.Result.Capabilities {
		r.facts.caps[c.Token] = c
	}
	r.facts.mu.Unlock()
	p.pending[i] = append(p.pending[i], zq3Claim{number: call.Claim.Number, id: q.Result.ID})
	if len(p.pending[i]) >= 32 {
		p.retireClaims(i)
	}
	return q.Result
}

// retireClaims retires every answered claim of mount i (one credential, so
// one run) and requires RETIRED through the last of them.
func (p *advanceReplay) retireClaims(i int) {
	r := p.r
	r.t.Helper()
	var hashes []string
	for _, c := range p.pending[i] {
		hashes = append(hashes, c.id)
	}
	through := p.pending[i][len(p.pending[i])-1].number
	payload := map[string]any{"realm": uid(100).String(), "subject": "616263", "anchor": uid(byte(101 + i)).String(), "nonce": uid(byte(103 + i)).String(), "through": through, "result_hashes": hashes}
	raw, err := r.execute(i, "RETIRE", payload)
	must(r.t, err)
	var reply struct {
		Status string
		Floor  uint64
	}
	must(r.t, json.Unmarshal(raw, &reply))
	if reply.Status != "RETIRED" || reply.Floor != through {
		r.t.Fatalf("m%d RETIRE: %s", i, raw)
	}
	p.pending[i] = nil
}

// claim queries a CALL's claim.
func (p *advanceReplay) claim(i int, call guestbridge.Call) zq3Query {
	raw, err := p.r.execute(i, "QUERY", map[string]any{"claim": call.Claim})
	must(p.r.t, err)
	var q zq3Query
	must(p.r.t, json.Unmarshal(raw, &q))
	return q
}

// invalidate unhashes mount i's cached dentry of n in dir before a fresh
// LOOKUP (hfs_d_revalidate returns 0, so every walk looks up again): an
// unpinned dentry's inode is evicted at once and FORGETs its grant. Once a
// name's last grant is gone, the next LOOKUP journals a handle pin, which
// moves the cut under that LOOKUP's own observation lease.
func (p *advanceReplay) invalidate(i int, dir guestbridge.Capability, n string) {
	key := dentryKey(dir, n)
	if e := p.dcache[i][key]; e != nil {
		delete(p.dcache[i], key)
		e.dead = true
		p.evict(i, e)
	}
}

// evict FORGETs a dead dentry's grant once no open file pins it.
func (p *advanceReplay) evict(i int, e *replayDentry) {
	if e.dead && e.pins == 0 {
		p.do(i, "FORGET", map[string]any{"object": e.cap, "count": 1})
		p.views[i].Evict(e.cap.Token)
	}
}

// creating is open(O_CREAT|O_EXCL) or mkdir in dir: GETATTR, LOOKUP NotFound
// and GETATTR observations, then the creating CALL while the last
// observation's release is still queued (a CALL_CONFLICT recall). The new
// dentry is cached, pinned by the created file's description.
func (p *advanceReplay) creating(op string, dir guestbridge.Capability, n string, args map[string]any) []guestbridge.Capability {
	p.observe(0, "GETATTR", map[string]any{"object": dir})
	p.invalidate(0, dir, n)
	p.observe(0, "LOOKUP", map[string]any{"parent": dir, "name": name(n)})
	queued := p.acquire(0)
	p.do(0, "GETATTR", map[string]any{"object": dir})
	args["parent"], args["name"] = dir, name(n)
	res := p.do(0, op, args)
	if res.Code != "OK" {
		p.r.t.Fatalf("%s %s: %+v", op, n, res)
	}
	p.release(0, queued)
	e := &replayDentry{cap: res.Capabilities[0]}
	p.dcache[0][dentryKey(dir, n)] = e
	p.instantiate(0, e.cap)
	if op == "CREATE" {
		e.pins = 1
		p.files[res.Capabilities[1].Token] = e
	}
	return res.Capabilities
}

// mkdir creates directory n in dir and returns its capability.
func (p *advanceReplay) mkdir(dir guestbridge.Capability, n string, mode int) guestbridge.Capability {
	return p.creating("MKDIR", dir, n, map[string]any{"mode": mode, "umask": 0})[0]
}

// create creates file n in dir and returns its object and description.
func (p *advanceReplay) create(dir guestbridge.Capability, n string, mode, umask int) (guestbridge.Capability, guestbridge.Capability) {
	caps := p.creating("CREATE", dir, n, map[string]any{"mode": mode, "umask": umask, "flags": flags("READWRITE", true)})
	return caps[0], caps[1]
}

// open walks to n in dir on mount i and opens it: GETATTR, LOOKUP and
// may_open GETATTR observations, then OPEN, whose description pins the
// dentry.
func (p *advanceReplay) open(i int, dir guestbridge.Capability, n, access string) (guestbridge.Capability, guestbridge.Capability) {
	object := p.stat(i, dir, n)
	res := p.do(i, "OPEN", map[string]any{"object": object, "flags": flags(access, false)})
	e := p.dcache[i][dentryKey(dir, n)]
	e.pins++
	p.files[res.Capabilities[0].Token] = e
	return object, res.Capabilities[0]
}

// stat walks to n in dir on mount i (GETATTR of dir, a fresh LOOKUP) and
// GETATTRs it.
func (p *advanceReplay) stat(i int, dir guestbridge.Capability, n string) guestbridge.Capability {
	p.observe(i, "GETATTR", map[string]any{"object": dir})
	p.invalidate(i, dir, n)
	object := p.observe(i, "LOOKUP", map[string]any{"parent": dir, "name": name(n)}).Capabilities[0]
	p.dcache[i][dentryKey(dir, n)] = &replayDentry{cap: object}
	p.instantiate(i, object)
	p.observe(i, "GETATTR", map[string]any{"object": object})
	return object
}

// chmod is the permission refresh, then SETATTR of the mode. The case's
// chowns are replayed as chmods too: the rig's mount credential is no root,
// and either one is a mutation that moves the cut.
func (p *advanceReplay) chmod(object guestbridge.Capability, mode int) {
	p.observe(0, "GETATTR", map[string]any{"object": object})
	attributes := map[string]any{"mode": mode, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
	if res := p.do(0, "SETATTR", map[string]any{"object": object, "attributes": attributes}); res.Code != "OK" {
		p.r.t.Fatalf("SETATTR %o: %+v", mode, res)
	}
}

// close is DESCRIPTION_CLOSE plus the last fput's DESCRIPTION_RELEASE.
func (p *advanceReplay) close(i int, description guestbridge.Capability) {
	p.closes[i]++
	p.do(i, "DESCRIPTION_CLOSE", map[string]any{"description": description, "process": p.r.credentials[i].Process, "close_sequence": p.closes[i]})
	p.do(i, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": p.closes[i]})
	if e := p.files[description.Token]; e != nil {
		delete(p.files, description.Token)
		e.pins--
		p.evict(i, e)
	}
}

// groupFile is groupFile of T1: create, chown and chmod, write, close, then
// the child with the group opens and reads it and the child without it is
// refused after its walk.
func (p *advanceReplay) groupFile(dir guestbridge.Capability, file string) {
	object, description := p.create(dir, file, 0640, 022)
	p.chmod(object, 0640)
	p.chmod(object, 0640)
	data := hex.EncodeToString([]byte(file + "-secret"))
	p.do(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": data, "max_end": 1 << 40})
	p.close(0, description)
	_, read := p.open(0, dir, file, "READ")
	p.observe(0, "READ", map[string]any{"description": read, "offset": 0, "count": 64})
	p.close(0, read)
	p.stat(0, dir, file)
}

// run replays the T1 case body after guest.py's admission (open and fstat
// of both roots) and returns the recalls it started, by trigger kind.
func (p *advanceReplay) run() map[string]int {
	r := p.r
	p.kinds = map[string]int{}
	p.dcache = [2]map[string]*replayDentry{{}, {}}
	p.files = map[string]*replayDentry{}
	p.initViews()
	var roots [2]guestbridge.Capability
	for i := range r.roots {
		if r.next[i] != 1 {
			r.t.Fatalf("m%d starts after claim %d, want only its mount ROOT", i, r.next[i])
		}
		root := p.claim(i, guestbridge.Call{Claim: guestbridge.Claim{Realm: uid(100).String(), Subject: "616263", Anchor: uid(byte(101 + i)).String(), Nonce: uid(byte(103 + i)).String(), Number: 1, OriginalHash: digest(guestbridge.Intent{Opcode: "ROOT", Args: json.RawMessage("{}")})}})
		p.pending[i] = []zq3Claim{{number: 1, id: root.Result.ID}}
		p.installed[i] = r.currentTarget(i)
		p.observe(i, "GETATTR", map[string]any{"object": r.roots[i]})
		roots[i] = p.do(i, "OPEN", map[string]any{"object": r.roots[i], "flags": flags("READ", false)}).Capabilities[0]
		p.observe(i, "GETATTR", map[string]any{"object": r.roots[i]})
	}
	p.follow()
	p.kinds = map[string]int{} // count the case body only
	p.mkdir(r.roots[0], "credentials", 0777)
	leaf, leafDescription := p.open(0, r.roots[0], "credentials", "READ")
	p.chmod(leaf, 0777)
	p.groupFile(leaf, "group")
	p.groupFile(leaf, "group-tail")
	p.setgid(leaf)
	m1 := p.stat(1, r.roots[1], "credentials")
	m1 = p.stat(1, m1, "setgid")
	p.stat(1, m1, "created")
	p.sticky(leaf)
	group, groupDescription := p.open(0, leaf, "group", "READWRITE")
	p.chmod(group, 06750)
	p.observe(0, "GETATTR", map[string]any{"object": group})
	p.chmod(group, 0750)
	p.observe(0, "GETATTR", map[string]any{"object": group})
	p.close(0, groupDescription)
	p.close(0, leafDescription)
	for i := range roots {
		p.close(i, roots[i])
	}
	p.follow()
	return p.kinds
}

// initViews gives each mount a fresh guest view holding only its mount root
// (registered as long as the mount lives, never mmapped), unless the test
// prepared the views already.
func (p *advanceReplay) initViews() {
	for i := range p.views {
		if p.views[i] == nil {
			p.views[i] = guestviewmodel.New(p.r.roots[i].Token)
		}
	}
}

// setgid is setgidCreate of T1: mkdir, open, chown and chmod, the child's
// create and fstat, close.
func (p *advanceReplay) setgid(leaf guestbridge.Capability) {
	p.mkdir(leaf, "setgid", 02777)
	dir, description := p.open(0, leaf, "setgid", "READ")
	p.chmod(dir, 02777)
	p.chmod(dir, 02777)
	created, createdDescription := p.create(dir, "created", 0666, 027)
	p.observe(0, "GETATTR", map[string]any{"object": created})
	p.close(0, createdDescription)
	p.close(0, description)
}

// sticky is stickyDenial of T1: mkdir, open, chmod, create and chown of the
// owned file, close, and the child's refused unlink after its walk.
func (p *advanceReplay) sticky(leaf guestbridge.Capability) {
	p.mkdir(leaf, "sticky", 01777)
	dir, description := p.open(0, leaf, "sticky", "READ")
	p.chmod(dir, 01777)
	owned, ownedDescription := p.create(dir, "owned", 0666, 022)
	p.chmod(owned, 0644)
	p.close(0, ownedDescription)
	p.stat(0, dir, "owned")
	p.close(0, description)
}

// TestZQ3AdvanceRemovesExpectedTargetRecalls replays the T1 credentials case
// body twice at the zq3 spec bounds: without advance (the guest before xf4
// step B) and with it, under tj4's cacheless test. Without advance every
// observation that follows a mutation or pin recalls its own domain, and
// every pinning LOOKUP's release recalls it again: 7 CALL_CONFLICT + 28
// EXPECTED_TARGET + 9 RELEASE recalls (zq3-r19 saw about 10 mismatch recalls
// in the part of T1 it reached). With advance: 7 CALL_CONFLICT recalls only,
// which advance does not touch, and 37 advance grants. The RELEASE recalls go
// because an Advance lease's release skips the recall although its LOOKUP
// moved the cut (without that relaxation the advance run still shows the 9
// RELEASE recalls), and the lineage budget is not reached between the
// CALL_CONFLICT recalls that move both fences. The guest views keep the
// mount roots, the root fds and the leaf fd registered throughout, so the
// advances also show that tj4's rule lets such a mount ask (uj4).
func TestZQ3AdvanceRemovesExpectedTargetRecalls(t *testing.T) {
	t.Parallel()
	before := (&advanceReplay{r: newZQ3Rig(t, 0)}).run()
	withAdvance := &advanceReplay{r: newZQ3Rig(t, 0), advance: true}
	after := withAdvance.run()
	t.Logf("T1 credentials recalls: without advance %v, with advance %v (%d advances, %v sent)", before, after, withAdvance.r.advances(), withAdvance.sent)
	for _, problem := range advanceVerdict(before, after, withAdvance.r.advances()) {
		t.Error(problem)
	}
}

// advanceVerdict lists how a run with advance falls short of what xf4
// promises against the baseline run without it; none means it holds.
func advanceVerdict(before, after map[string]int, advances uint64) []string {
	var problems []string
	if before[TriggerExpectedTarget] < 10 {
		problems = append(problems, fmt.Sprintf("baseline EXPECTED_TARGET recalls %d, want >= 10", before[TriggerExpectedTarget]))
	}
	if after[TriggerExpectedTarget] != 0 || after[TriggerRelease] != 0 {
		problems = append(problems, fmt.Sprintf("with advance: EXPECTED_TARGET %d, RELEASE %d, want 0 and 0", after[TriggerExpectedTarget], after[TriggerRelease]))
	}
	if before[TriggerCallConflict] == 0 || after[TriggerCallConflict] != before[TriggerCallConflict] {
		problems = append(problems, fmt.Sprintf("CALL_CONFLICT recalls: without advance %d, with advance %d", before[TriggerCallConflict], after[TriggerCallConflict]))
	}
	if advances < uint64(before[TriggerExpectedTarget]) {
		problems = append(problems, fmt.Sprintf("advances %d below the %d mismatch recalls they replace", advances, before[TriggerExpectedTarget]))
	}
	return problems
}

// TestZQ3AdvanceReplayDetectsNonCachelessGuest replays T1 with advance
// support against guest views the guest would never call cacheless and
// requires that no advance is sent or granted there. zq3-r20's rule (every
// registered inode counts, guestviewmodel.CachelessR20) never holds with the
// mount root registered: the replay must then pay the baseline's
// EXPECTED_TARGET recalls and advanceVerdict must fail, as the lab run did.
// A mount whose view holds an mmapped inode never sends advance under tj4's
// rule either; the other mount still does.
func TestZQ3AdvanceReplayDetectsNonCachelessGuest(t *testing.T) {
	t.Parallel()
	baseline := (&advanceReplay{r: newZQ3Rig(t, 0)}).run()
	r20 := &advanceReplay{r: newZQ3Rig(t, 0), advance: true, rule: guestviewmodel.CachelessR20}
	after := r20.run()
	t.Logf("r20 rule: recalls %v, %d advances, %v sent", after, r20.r.advances(), r20.sent)
	if r20.sent != [2]int{} || r20.r.advances() != 0 || after[TriggerExpectedTarget] == 0 {
		t.Errorf("r20 rule: %v advances sent, %d granted, %d EXPECTED_TARGET recalls; want none, none and some", r20.sent, r20.r.advances(), after[TriggerExpectedTarget])
	}
	if problems := advanceVerdict(baseline, after, r20.r.advances()); len(problems) == 0 {
		t.Error("advanceVerdict passed a guest that never sends advance")
	} else {
		t.Logf("detected: %v", problems)
	}

	mapped := &advanceReplay{r: newZQ3Rig(t, 0), advance: true}
	mapped.initViews()
	mapped.views[0].Register("m0-mapped-file")
	mapped.views[0].Mmap("m0-mapped-file")
	mapped.run()
	t.Logf("m0 with an mmapped inode: %v advances sent, %d granted", mapped.sent, mapped.r.advances())
	if mapped.sent[0] != 0 || mapped.sent[1] == 0 {
		t.Errorf("mmapped m0: advances sent %v, want none on m0 and some on m1", mapped.sent)
	}
}
