package viewcontrol

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// zq3 workload model (task o64). zq3-r6 and r7 failed on lifetime budget
// walls nobody saw offline. This model drives the CALLs that a zq3 run makes
// through the real provider, bridge and view controller at the committed
// spec's exact Views, Providers and bridge values, so such a wall fails here.
//
// It maps guest syscalls to hfs CALLs as the guest kernel does (patches
// 0003-0005 under tools/integration/linux9p/patches):
//   - every permission check refreshes attributes: one GETATTR of the
//     directory per path component walked (hfs_permission), and one of the
//     object for open(2) of an existing file (may_open);
//   - dentries are never revalidated (hfs_d_revalidate returns 0), so every
//     walk LOOKUPs again; the old dentry's inode is evicted and FORGETs its
//     grant, unless an open file still pins it (then at its last close);
//   - open(O_CREAT|O_EXCL) is GETATTR, LOOKUP (NotFound), GETATTR, CREATE; the
//     CREATE reply's description serves the open, so there is no OPEN;
//   - close(2) is DESCRIPTION_CLOSE (flush) plus DESCRIPTION_RELEASE (last
//     fput); a dup'ed descriptor's close is a DESCRIPTION_CLOSE only;
//   - unlink/rmdir drop the victim's inode, which FORGETs its grant;
//   - a process that used a mount ends with one PROCESS_EXIT there.
//
// Accuracy: the CALL counts are within about +-25% of the kernel's. Unmodeled
// are the kernel's extra GETATTRs (for example around setattr_prepare or
// mount(8)'s own stat), shell helpers that touch the mount and podman's own
// lookups of the bind source; one READDIR per listing stands in for the
// kernel's page loop (its end-of-directory page is an extra GETATTR here).
// The rig runs ViewProtocol, not the lab's terminal tuple, so a READDIR's
// transient grants are released by FORGETs instead of one READDIR_CLEANUP
// (more CALLs, not fewer). The mapped dirtying of T5 (1024 dirty pages, each
// a DIRTY_RESERVE earmark and a DRAIN_WRITE claim) is not driven: it needs
// view identities the spec cannot hold (see the o64 bound inventory).

// rigLimits carries the spec's provider and bridge values into newRigWith.
type rigLimits struct {
	Claims                                                          int
	RetainedBytes, SourceBytes, ClaimCreditBytes, SourceCreditBytes uint64
	Capabilities, CapabilityIdentities, CredentialRevisions         int
}

func (l *rigLimits) provider(c *controlledvalidation.Config) {
	if l == nil {
		return
	}
	c.MaxClaims = l.Claims
	c.MaxRetainedBytes, c.MaxSourceBytes = l.RetainedBytes, l.SourceBytes
	c.ClaimCreditBytes, c.SourceCreditBytes = l.ClaimCreditBytes, l.SourceCreditBytes
}

func (l *rigLimits) bridge(c *guestbridge.Config) {
	if l == nil {
		return
	}
	c.MaxCapabilities = l.Capabilities
	c.MaxCapabilityIdentities = l.CapabilityIdentities
	c.MaxCredentialRevisions = l.CredentialRevisions
}

// zq3SpecLimits reads the committed spec's Providers and bridge values, as
// controlledruntime.Assemble passes them to the providers and the bridge.
func zq3SpecLimits(t *testing.T) *rigLimits {
	t.Helper()
	raw, err := os.ReadFile("../runtimeprep/testdata/zq3-spec.json")
	must(t, err)
	var spec struct {
		Providers                                               rigLimits
		Capabilities, CapabilityIdentities, CredentialRevisions int
	}
	must(t, json.Unmarshal(raw, &spec))
	l := spec.Providers
	l.Capabilities, l.CapabilityIdentities, l.CredentialRevisions = spec.Capabilities, spec.CapabilityIdentities, spec.CredentialRevisions
	return &l
}

// processRegistry admits the model's guest process credentials per session,
// standing in for the runtime attestor's verified guest process identities.
type processRegistry struct {
	mu      sync.Mutex
	allowed map[string]bool
}

func registryKey(session string, c guestbridge.Credentials) string {
	raw, _ := json.Marshal(c)
	return session + string(raw)
}

func (p *processRegistry) add(session string, c guestbridge.Credentials) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.allowed[registryKey(session, c)] = true
}

func (p *processRegistry) has(session string, c guestbridge.Credentials) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.allowed[registryKey(session, c)]
}

// zq3Proc is one guest process. Its credential is one bridge credential row
// per session it uses, and its exit is one PROCESS_EXIT per such session.
type zq3Proc struct {
	cred guestbridge.Credentials
	used [2]bool
}

type zq3Dentry struct {
	cap    guestbridge.Capability
	issuer *zq3Proc
	pins   int
	// dead marks an unhashed dentry (invalidated by a new lookup, or its
	// entry removed): its inode is evicted, so its grant is FORGOTTEN, once
	// the last open file that pins it closes.
	dead bool
}

// zq3File is an open description; dentry is nil for a mount root.
type zq3File struct {
	obj, desc guestbridge.Capability
	opener    *zq3Proc
	dentry    *zq3Dentry
}

type zq3Claim struct {
	number uint64
	cred   string
	id     string
}

// zq3Guest is one mount (domain) of the guest kernel.
type zq3Guest struct {
	w        *zq3World
	d        int
	root     guestbridge.Capability
	dcache   map[string]*zq3Dentry
	pending  []zq3Claim
	closes   uint64
	releases uint64
	stats    zq3Stats
}

// zq3Stats counts one domain's consumption; peaks are high-water marks.
// The bridge peaks are read from the provider's protected session snapshot
// after each CALL: grant rows (MaxCapabilityIdentities), live grants
// (MaxCapabilities), credential and close rows (MaxCredentialRevisions).
type zq3Stats struct {
	calls, grants, retired, processes, peakUnretired, peakSnapshot int
	peakGrants, peakLive, peakCredentials, peakCloses              int
}

// observeSnapshot records the bridge peaks of one protected snapshot.
func (s *zq3Stats) observeSnapshot(raw []byte) {
	var snapshot struct {
		Credentials    map[string]json.RawMessage
		Grants         map[string]struct{ Released bool }
		CloseSequences map[string]uint64
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return
	}
	live := 0
	for _, g := range snapshot.Grants {
		if !g.Released {
			live++
		}
	}
	s.peakSnapshot = max(s.peakSnapshot, len(raw))
	s.peakGrants = max(s.peakGrants, len(snapshot.Grants))
	s.peakLive = max(s.peakLive, live)
	s.peakCredentials = max(s.peakCredentials, len(snapshot.Credentials))
	s.peakCloses = max(s.peakCloses, len(snapshot.CloseSequences))
}

// zq3World is the whole guest: both mounts, the process table and the
// modeled guest retirement window.
type zq3World struct {
	r     *rig
	g     [2]*zq3Guest
	phase string
	// window is the guest's unretired-request window per domain: the guest
	// sends RETIRE only when its own provider reserve fails (hfs_call ->
	// hfs_retire), and then retires one run of same-credential requests.
	window int
	phases []zq3PhaseCount
	procs  uint64
	// mounts are the two mount(8) processes that issued each ROOT.
	mounts [2]*zq3Proc
	// peakSources is the view controller's retained source cut high-water.
	peakSources int
}

type zq3PhaseCount struct {
	name  string
	calls [2]int
}

func newZQ3World(r *rig, window int) *zq3World {
	w := &zq3World{r: r, window: window}
	for d := range 2 {
		g := &zq3Guest{w: w, d: d, root: r.roots[d], dcache: map[string]*zq3Dentry{}}
		// Claim 1 is the mount's ROOT, issued by mount(8) in newRig.
		mount := &zq3Proc{cred: r.credentials[d]}
		mount.used[d] = true
		g.pending = append(g.pending, zq3Claim{1, credKey(r.credentials[d]), g.rootResultID()})
		g.stats.calls, g.stats.grants, g.stats.processes = 1, 2, 1
		w.g[d] = g
		w.mounts[d] = mount
	}
	return w
}

func credKey(c guestbridge.Credentials) string {
	return c.Process + ":" + strconv.FormatUint(c.Revision, 10)
}

// rootResultID queries the mount ROOT claim for its protected result id.
func (g *zq3Guest) rootResultID() string {
	intent := guestbridge.Intent{Opcode: "ROOT", Args: json.RawMessage("{}")}
	q := g.query(g.claim(1, intent))
	return q.Result.ID
}

func (g *zq3Guest) claim(number uint64, intent guestbridge.Intent) guestbridge.Claim {
	return guestbridge.Claim{Realm: uid(100).String(), Subject: "616263", Anchor: uid(byte(101 + g.d)).String(), Nonce: uid(byte(103 + g.d)).String(), Number: number, OriginalHash: digest(intent)}
}

type zq3Query struct {
	Status string
	Result result
}

func (g *zq3Guest) query(claim guestbridge.Claim) zq3Query {
	r := g.w.r
	r.t.Helper()
	raw, err := r.execute(g.d, "QUERY", map[string]any{"claim": claim})
	must(r.t, err)
	var q zq3Query
	must(r.t, json.Unmarshal(raw, &q))
	return q
}

// spawn starts a guest process with numeric credentials (root: uid 0).
func (w *zq3World) spawn(fsuid, fsgid uint32, groups []uint32) *zq3Proc {
	w.procs++
	process := fmt.Sprintf("00000000-0000-4000-8000-%012x", w.procs)
	if groups == nil {
		groups = []uint32{}
	}
	return &zq3Proc{cred: guestbridge.Credentials{FSUID: fsuid, FSGID: fsgid, Groups: groups, Process: process, Revision: 1, IDMapHash: hash([]byte("map")), UserNamespace: uid(64).String()}}
}

// exit sends the process's PROCESS_EXIT on every mount it used.
func (w *zq3World) exit(p *zq3Proc) {
	for d, used := range p.used {
		if used {
			w.g[d].call(p, "PROCESS_EXIT", map[string]any{"process": p.cred.Process, "exit_sequence": 1})
		}
	}
}

// call runs one CALL to TERMINAL. Before allocating, the guest retires while
// its window is full. Any refusal outside want (default OK), and every
// NoSpace, is a budget wall and fails the test with the phase and counts.
func (g *zq3Guest) call(p *zq3Proc, op string, args map[string]any, want ...string) result {
	r := g.w.r
	r.t.Helper()
	g.retireWhileFull()
	if !p.used[g.d] {
		p.used[g.d] = true
		g.stats.processes++
		r.processes.add(r.scopes[g.d].Session, p.cred)
	}
	raw, err := canonical(args)
	must(r.t, err)
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	r.next[g.d]++
	claim := g.claim(r.next[g.d], intent)
	if err = r.providers[g.d].Allocate(r.t.Context(), r.scopes[g.d], claim); err != nil {
		g.wall(op, "allocate: "+err.Error())
	}
	reply, err := r.execute(g.d, "CALL", guestbridge.Call{Claim: claim, Credentials: p.cred, Intent: intent})
	must(r.t, err)
	var admitted struct{ Status, Code string }
	must(r.t, json.Unmarshal(reply, &admitted))
	if admitted.Status != "ADMITTED" {
		g.wall(op, admitted.Status+" "+admitted.Code)
	}
	q := g.query(claim)
	if q.Status != "TERMINAL" {
		g.wall(op, "QUERY "+q.Status)
	}
	g.expect(op, q.Result.Code, want)
	g.account(p, claim.Number, q.Result)
	return q.Result
}

func (g *zq3Guest) expect(op, code string, want []string) {
	if len(want) == 0 {
		want = []string{"OK"}
	}
	for _, w := range want {
		if code == w {
			return
		}
	}
	g.wall(op, "result "+code)
}

func (g *zq3Guest) account(p *zq3Proc, number uint64, res result) {
	r := g.w.r
	r.facts.mu.Lock()
	for _, cap := range res.Capabilities {
		r.facts.caps[cap.Token] = cap
	}
	for _, entry := range res.Entries {
		r.facts.caps[entry.Capability.Token] = entry.Capability
	}
	r.facts.mu.Unlock()
	g.stats.grants += len(res.Entries)
	g.pending = append(g.pending, zq3Claim{number, credKey(p.cred), res.ID})
	g.stats.calls++
	g.stats.grants += len(res.Capabilities)
	g.stats.peakUnretired = max(g.stats.peakUnretired, len(g.pending))
	g.w.phases[len(g.w.phases)-1].calls[g.d]++
	r.controller.mu.Lock()
	g.w.peakSources = max(g.w.peakSources, len(r.controller.state.Sources))
	r.controller.mu.Unlock()
	if snapshot, err := r.providers[g.d].Restore(r.t.Context(), r.scopes[g.d]); err == nil {
		g.stats.observeSnapshot(snapshot)
	}
}

// wall fails the run at the first budget refusal, naming where it hit.
func (g *zq3Guest) wall(op, why string) {
	g.w.r.t.Helper()
	g.w.r.t.Fatalf("zq3 workload wall: phase %s, m%d CALL %d (%s): %s; %s", g.w.phase, g.d, g.w.r.next[g.d], op, why, g.w.summary())
}

// retireWhileFull models hfs_call: a full reserve makes the guest retire
// the head run of terminal requests that share the head's credentials.
func (g *zq3Guest) retireWhileFull() {
	for len(g.pending) >= g.w.window {
		g.retireRun()
	}
}

func (g *zq3Guest) retireRun() {
	r := g.w.r
	r.t.Helper()
	head := g.pending[0].cred
	var hashes []string
	n := 0
	for n < len(g.pending) && g.pending[n].cred == head && n < 1024 {
		hashes = append(hashes, g.pending[n].id)
		n++
	}
	through := g.pending[n-1].number
	payload := map[string]any{"realm": uid(100).String(), "subject": "616263", "anchor": uid(byte(101 + g.d)).String(), "nonce": uid(byte(103 + g.d)).String(), "through": through, "result_hashes": hashes}
	raw, err := r.execute(g.d, "RETIRE", payload)
	must(r.t, err)
	var reply struct {
		Status string
		Floor  uint64
	}
	must(r.t, json.Unmarshal(raw, &reply))
	if reply.Status != "RETIRED" || reply.Floor != through {
		g.wall("RETIRE", string(raw))
	}
	g.pending = g.pending[n:]
	g.stats.retired += n
}

// summary is the consumption so far, printed with a wall.
func (w *zq3World) summary() string {
	out := ""
	for d, g := range w.g {
		s := g.stats
		out += fmt.Sprintf("m%d: calls %d grants issued %d processes %d retired %d unretired %d (peak %d); snapshot peaks: %d B, grant rows %d, live %d, credentials %d, close rows %d; ",
			d, s.calls, s.grants, s.processes, s.retired, len(g.pending), s.peakUnretired, s.peakSnapshot, s.peakGrants, s.peakLive, s.peakCredentials, s.peakCloses)
	}
	return out
}

// begin starts a named phase for the per-phase CALL counts.
func (w *zq3World) begin(name string) {
	w.phase = name
	w.phases = append(w.phases, zq3PhaseCount{name: name})
}

func (w *zq3World) report(t *testing.T) {
	for _, p := range w.phases {
		t.Logf("%-28s m0 %4d  m1 %4d CALLs", p.name, p.calls[0], p.calls[1])
	}
	t.Log(w.summary())
	identities, uuids, tickets := w.viewUsage()
	t.Logf("view controller: ticket identities %d, lease/reservation UUIDs %d, ticket rows %d, source cuts peak %d", identities, uuids, tickets, w.peakSources)
}

// viewUsage is the controller's lifetime view counters: ticket identities
// (Bounds.Identities) and lease/reservation UUIDs (2*Identities).
func (w *zq3World) viewUsage() (identities, uuids, tickets int) {
	c := w.r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Identities, len(c.state.UUIDs), len(c.state.Tickets)
}

func name(s string) string { return hex.EncodeToString([]byte(s)) }

func dentryKey(dir guestbridge.Capability, n string) string { return dir.Allocation + "/" + n }

// sortedKeys keeps FORGET order deterministic.
func sortedKeys(m map[string]*zq3Dentry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
