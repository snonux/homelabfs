//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Guest side of the recall workload model (recall_workload_test.go): the
// processes zq3-r8 ran and the requests each one sends.

// mount replays one mount(8): HELLO, ROOT, the early VIEW_WAIT (EMPTY before
// any lease), the bootstrap lease and its release, which mount(8) itself
// sends and waits out (the hfs_mmap_bootstrap loop, not the cleanup worker),
// and the asynchronous PROCESS_EXIT.
func (m *recallModel) mount(i int) {
	m.begin(fmt.Sprintf("m%d mount(8)", i))
	mount := m.mounts[i]
	p := m.process("mount(8)", &mount.peer.credentials)
	_, _, err := mount.peer.execute("HELLO", guestbridge.TerminalProtocol())
	require(m.t, err)
	root := m.claim(mount, p, "ROOT", map[string]any{})
	m.await(root)
	var result struct{ Capabilities []guestbridge.Capability }
	require(m.t, json.Unmarshal(claimResult(m.t, root), &result))
	if len(result.Capabilities) != 2 {
		m.t.Fatalf("m%d ROOT: %s", i, root.payload)
	}
	mount.root, mount.domain = result.Capabilities[0], result.Capabilities[1]
	mount.chain.exec(m.t, "VIEW_WAIT", map[string]any{"domain": mount.domain, "after_sequence": 0})
	lease := m.lease(mount, p, nil)
	if lease == nil {
		m.t.Fatalf("m%d bootstrap lease refused", i)
	}
	w := m.register(m.releaseWait(mount, p, *lease))
	m.race()
	m.sendRelease(w)
	m.await(w)
	m.claim(mount, p, "PROCESS_EXIT", map[string]any{"process": p.cred.Process, "exit_sequence": 1})
}

// usableProbe replays one k64 lstat child on mount i: hfs_getattr's
// observation lease (expected_target = the installed view, captured once the
// view is LIVE), the GETATTR, the cleanup work's VIEW_RELEASE and the child's
// PROCESS_EXIT, the last two asynchronous. The view must not move between
// the capture and the GETATTR's end: the guest would answer ESTALE
// (hfs_mmap_begin's revision check, hfs_attr_install) and check_usable would
// fail although the claim is TERMINAL.
func (m *recallModel) usableProbe(i int) {
	mount := m.mounts[i]
	child := m.process(fmt.Sprintf("lstat child %d", m.processes+1), nil)
	m.enterView(mount)
	fence, revision := mount.fence, mount.revision
	lease := m.lease(mount, child, mount.target)
	if lease == nil {
		// Refused, and lease failed the test. An expected-target mismatch
		// (the cut moved under the installed view) would land here after
		// the all-domain recall it starts (e84).
		return
	}
	getattr := m.claim(mount, child, "GETATTR", map[string]any{"object": mount.root})
	m.await(getattr)
	if mount.fence != fence || mount.revision != revision {
		m.viewMoves = append(m.viewMoves, fmt.Sprintf("%s: fence %d->%d, revision %d->%d", getattr.label, fence, mount.fence, revision, mount.revision))
	}
	m.queueRelease(mount, child, *lease)
	m.claim(mount, child, "PROCESS_EXIT", map[string]any{"process": child.cred.Process, "exit_sequence": 1})
}

// enterView models hfs_mmap_begin's hfs_view_enter: an observation waits,
// with no deadline, until its mount's view is LIVE again, i.e. while a
// recall's event for the mount is outstanding (the guest's event worker stops
// the view as soon as the RECALL event arrives, long before the next lstat
// over SSH). Only then does the guest capture the installed target and send
// VIEW_ACQUIRE, whose 30 s window starts there.
func (m *recallModel) enterView(mount *modelMount) {
	deadline := time.Now().Add(modelDeadline)
	for m.recallActive() && m.inRecall(mount) {
		if time.Now().After(deadline) {
			m.t.Fatalf("model deadline: m%d view never reopened", mount.index)
		}
		m.step()
	}
}

// inRecall reports whether mount has a recall event outstanding.
func (m *recallModel) inRecall(mount *modelMount) bool {
	payload := mount.chain.exec(m.t, "VIEW_WAIT", map[string]any{"domain": mount.domain, "after_sequence": 0})
	var poll struct{ Event map[string]json.RawMessage }
	require(m.t, json.Unmarshal(payload, &poll))
	return len(poll.Event) > 0
}

// race lets the guest's retry loops and workers run one round before a
// process sends its next request, under the interleaved schedule only.
func (m *recallModel) race() {
	if m.interleave {
		m.step()
	}
}

// lease acquires one DOMAIN READ lease (hfs_lease_acquire) and waits for its
// terminal reply. It returns nil when the reply was not GRANTED.
func (m *recallModel) lease(mount *modelMount, p *modelProcess, expected json.RawMessage) *guestbridge.ViewLease {
	ticket := p.ticket(mount.index, "acquire", m.families[mount.index].Acquisition)
	var target any
	if len(expected) > 0 {
		target = expected
	}
	mount.registered = true
	m.race()
	m.tickets++
	w := m.register(m.viewWait(fmt.Sprintf("m%d %s VIEW_ACQUIRE", mount.index, p.name), mount, ticket))
	w.start = m.startWait()
	reply, _, _, _, err := mount.peer.mappedExecute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": mount.domain, "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": target})
	require(m.t, err)
	m.launch(w, reply)
	m.await(w)
	if w.status != "GRANTED" {
		m.t.Errorf("%s: %s (expected target %s)", w.label, w.payload, expected)
		return nil
	}
	var granted struct{ Lease guestbridge.ViewLease }
	require(m.t, json.Unmarshal(w.payload, &granted))
	return &granted.Lease
}

// queueRelease hands a lease's VIEW_RELEASE to its mount's cleanup worker.
// The guest allocates one single-threaded "9p-hfs-cleanup" workqueue per
// mount (patch 0004, alloc_workqueue(..., 1)), and hfs_mmap_cleanup holds
// that worker through hfs_lease_release's whole VIEW_QUERY loop, so a
// mount's releases are sent one at a time, in order, each only once the
// previous one ended. Its 30 s window starts when it is actually sent.
func (m *recallModel) queueRelease(mount *modelMount, p *modelProcess, lease guestbridge.ViewLease) {
	m.race()
	w := m.register(m.releaseWait(mount, p, lease))
	mount.cleanup = append(mount.cleanup, w)
	m.dispatchReleases()
}

// dispatchReleases runs each mount's cleanup worker: once its in-flight
// release ended, it sends the next queued one. A release that expired fails
// the worker (hfs_session_block, then hfs_mmap_fail sets control->error and
// every later cleanup returns at once), so nothing behind it is ever sent.
func (m *recallModel) dispatchReleases() {
	for _, mount := range m.mounts {
		if mount.inflight != nil && mount.inflight.done {
			if mount.inflight.expired {
				mount.cleanupFailed = true
			}
			mount.inflight = nil
		}
		if mount.inflight == nil && !mount.cleanupFailed && len(mount.cleanup) > 0 {
			mount.inflight, mount.cleanup = mount.cleanup[0], mount.cleanup[1:]
			m.sendRelease(mount.inflight)
		}
	}
}

// releasesDispatchable reports whether a cleanup worker can still send a
// queued release.
func (m *recallModel) releasesDispatchable() bool {
	for _, mount := range m.mounts {
		if !mount.cleanupFailed && len(mount.cleanup) > 0 {
			return true
		}
	}
	return false
}

// releaseWait builds an unsent VIEW_RELEASE wait for lease.
func (m *recallModel) releaseWait(mount *modelMount, p *modelProcess, lease guestbridge.ViewLease) *guestWait {
	control := p.ticket(mount.index, "control", m.families[mount.index].Control)
	w := m.viewWait(fmt.Sprintf("m%d %s VIEW_RELEASE", mount.index, p.name), mount, control)
	w.release, w.status = true, "UNSENT"
	w.send = func() json.RawMessage {
		return mount.chain.exec(m.t, "VIEW_RELEASE", map[string]any{"request": control, "lease": lease})
	}
	return w
}

// sendRelease sends w's preowned VIEW_RELEASE and starts its window.
func (m *recallModel) sendRelease(w *guestWait) {
	m.tickets++
	w.start = m.startWait()
	m.launch(w, w.send())
}

// viewWait builds a view ticket exchange that retries VIEW_QUERY of ticket
// until a terminal status.
func (m *recallModel) viewWait(label string, mount *modelMount, ticket guestbridge.Ticket) *guestWait {
	w := &guestWait{label: label, phase: m.phase, mount: mount.index, terminal: func(s string) bool { return viewTerminal[s] }}
	w.query = func() json.RawMessage {
		return m.noReply(mount.chain, "VIEW_QUERY", map[string]any{"request": ticket})
	}
	return w
}

// noReply sends one control command on chain and returns its reply, or nil
// when the host never answered it. A release that Facts.CheckPhysical holds
// (an active lease of its domain, no recall to clear it) fails with
// WouldBlock, which the transport retries in its pending ring every 10 ms
// without answering the cell (recordExecuteFailure); the fixture's read
// gives up after 5 s. The chain keeps reclaiming its last answered cell.
//
// Observed while building the g84 case: while such a cell sits in the
// control worker's pending ring, the control lane answered nothing else
// either; fresh VIEW_WAITs to both mounts got no reply within 5 s. The
// worker gives a due retry priority over its queue (guestbridge endpoint.go
// worker) and a retry takes longer than the 10 ms ticker, so the queue is
// never read. That is why the peer mount's pending release also dies there.
func (m *recallModel) noReply(chain *r5Chain, op string, value any) json.RawMessage {
	m.t.Helper()
	payload, _, _, translated, err := chain.peer.mappedExecuteReclaim(op, value, chain.guest, chain.native, nil)
	if errors.Is(err, fsapi.WouldBlock) {
		return nil
	}
	require(m.t, err)
	chain.guest = &guestbridge.Ack{Cell: chain.peer.cells, ResponseHash: hashBytes(translated)}
	chain.native = chain.peer.mappingLastNative
	return payload
}

// claim runs hfs_request_run's ALLOCATE and CALL on the ordinary lane and
// tracks the claim's QUERY retries on the control lane until TERMINAL.
func (m *recallModel) claim(mount *modelMount, p *modelProcess, op string, args any) *guestWait {
	peer := mount.peer
	m.race()
	w := m.register(&guestWait{label: fmt.Sprintf("m%d %s %s claim", mount.index, p.name, op), phase: m.phase, mount: mount.index, claim: true, terminal: func(s string) bool { return s == "TERMINAL" }})
	w.start = m.startWait()
	raw, err := canonicalJSON(args)
	require(m.t, err)
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	original, err := canonicalJSON(intent)
	require(m.t, err)
	peer.claims++
	call := guestbridge.Call{Claim: guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: peer.claims, OriginalHash: hashBytes(original)}, Credentials: p.cred, Intent: intent}
	_, _, err = peer.execute("ALLOCATE", call)
	require(m.t, err)
	reply, _, err := peer.execute("CALL", call)
	require(m.t, err)
	if !bytes.Contains(reply, []byte(`"status":"ADMITTED"`)) {
		m.t.Fatalf("m%d %s %s CALL: %s", mount.index, p.name, op, reply)
	}
	w.query = func() json.RawMessage {
		return mount.chain.exec(m.t, "QUERY", map[string]any{"claim": call.Claim})
	}
	m.launch(w, w.query())
	return w
}

// claimResult returns a TERMINAL claim's result.
func claimResult(t *testing.T, w *guestWait) json.RawMessage {
	var reply struct{ Result json.RawMessage }
	if err := json.Unmarshal(w.payload, &reply); err != nil || w.status != "TERMINAL" {
		t.Fatalf("%s: %s %v", w.label, w.payload, err)
	}
	return reply.Result
}
