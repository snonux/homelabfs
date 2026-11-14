//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"fmt"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// The guest's per-mount cleanup worker in the recall workload model
// (recall_workload_test.go).
//
// The guest allocates one single-threaded "9p-hfs-cleanup" workqueue per
// mount (patch 0004, alloc_workqueue(..., 1)). hfs_mmap_cleanup holds that
// worker through hfs_lease_release's whole VIEW_QUERY loop, then (after
// RELEASED) through hfs_retire (one RETIRE of the session's oldest retirable
// requests) and hfs_view_retire (one VIEW_RETIRE per ticket family with
// retirable tickets). So a mount's cleanup exchanges go out one at a time,
// in order, each only once the previous one ended. A release that expired
// or ended other than RELEASED fails the worker (hfs_session_block, then
// hfs_mmap_fail sets control->error and every later cleanup returns at
// once); so does a RETIRE / VIEW_RETIRE the host never answers within the
// window (hfs_transport's deadline blocks the session).

// cleanupJob is one queued unit of cleanup work. It sends at most one
// exchange and returns its wait, or nil when it had nothing to send.
type cleanupJob func() *guestWait

// claimRec is one request in a mount's guest request list (session->requests).
type claimRec struct {
	number uint64
	cred   guestbridge.Credentials
	wait   *guestWait
}

// familyRec is one guest view ticket family of a mount: a process's
// acquisition or control family (ticketKeys: scope, credentials, anchor,
// nonce), its tickets in number order and its retired floor.
type familyRec struct {
	anchor, nonce string
	cred          guestbridge.Credentials
	tickets       []*ticketRec
	floor         uint64
}

// ticketRec is one view ticket. An acquisition ticket stays referenced by
// its lease (ticket->users) until the lease is released and freed.
type ticketRec struct {
	number  uint64
	wait    *guestWait
	acquire bool
	release *guestWait // the lease's VIEW_RELEASE (acquisition tickets)
}

// ticket returns process p's next ticket of family on mount and records it
// in the mount's family list.
func (m *recallModel) ticket(mount *modelMount, p *modelProcess, kind string, family viewcontrol.TicketFamily) (guestbridge.Ticket, *ticketRec) {
	t := p.ticket(mount.index, kind, family)
	key := p.name + "/" + kind
	f := mount.familyIndex[key]
	if f == nil {
		f = &familyRec{anchor: family.Anchor, nonce: family.Nonce, cred: p.cred}
		mount.familyIndex[key] = f
		mount.families = append(mount.families, f)
	}
	rec := &ticketRec{number: t.Number}
	f.tickets = append(f.tickets, rec)
	return t, rec
}

// queueRelease hands a lease's VIEW_RELEASE to its mount's cleanup worker.
// Its 30 s window starts when the worker actually sends it.
func (m *recallModel) queueRelease(mount *modelMount, p *modelProcess, lease guestbridge.ViewLease, acquired *ticketRec) {
	m.race()
	w := m.register(m.releaseWait(mount, p, lease, acquired))
	mount.jobs = append(mount.jobs, func() *guestWait { m.sendRelease(w); return w })
	m.dispatchCleanup()
}

// releaseWait builds an unsent VIEW_RELEASE wait for lease and links it to
// the lease's acquisition ticket.
func (m *recallModel) releaseWait(mount *modelMount, p *modelProcess, lease guestbridge.ViewLease, acquired *ticketRec) *guestWait {
	control, rec := m.ticket(mount, p, "control", m.families[mount.index].Control)
	w := m.viewWait(fmt.Sprintf("m%d %s VIEW_RELEASE", mount.index, p.name), mount, control)
	w.release, w.status = true, "UNSENT"
	w.send = func() json.RawMessage {
		return m.noReply(mount.chain, "VIEW_RELEASE", map[string]any{"request": control, "lease": lease})
	}
	rec.wait, acquired.release = w, w
	return w
}

// sendRelease sends w's preowned VIEW_RELEASE and starts its window.
func (m *recallModel) sendRelease(w *guestWait) {
	m.tickets++
	w.start = m.startWait()
	m.launch(w, w.send())
}

// dispatchCleanup runs every mount's cleanup worker as far as it can go now.
func (m *recallModel) dispatchCleanup() {
	for _, mount := range m.mounts {
		m.runCleanup(mount)
	}
}

// runCleanup finishes the mount's in-flight exchange if it ended (a
// RELEASED release continues with its RETIRE and VIEW_RETIREs, any other end
// of a release and an expired exchange fail the worker) and sends the next
// queued job until one is in flight or nothing is left.
func (m *recallModel) runCleanup(mount *modelMount) {
	for {
		if w := mount.inflight; w != nil {
			if !w.done {
				return
			}
			mount.inflight = nil
			if w.expired || (w.release && w.status != "RELEASED") {
				mount.cleanupFailed = true
			}
			if !mount.cleanupFailed && w.release {
				retire := m.retireJobs(mount, w.phase, w.label[:len(w.label)-len(" VIEW_RELEASE")]+" cleanup")
				mount.jobs = append(retire, mount.jobs...)
			}
		}
		if mount.cleanupFailed || len(mount.jobs) == 0 {
			return
		}
		job := mount.jobs[0]
		mount.jobs = mount.jobs[1:]
		mount.inflight = job()
	}
}

// cleanupPending reports whether a cleanup worker still has work it can run.
func (m *recallModel) cleanupPending() bool {
	for _, mount := range m.mounts {
		if !mount.cleanupFailed && (len(mount.jobs) > 0 || mount.inflight != nil) {
			return true
		}
	}
	return false
}

// retireJobs returns hfs_mmap_cleanup's tail: hfs_retire, then
// hfs_view_retire over the families that exist now. owner labels the waits,
// which are accounted to phase.
func (m *recallModel) retireJobs(mount *modelMount, phase *modelPhase, owner string) []cleanupJob {
	n := len(mount.families)
	return []cleanupJob{
		func() *guestWait { return m.retireClaims(mount, phase, owner) },
		func() *guestWait { return m.retireViews(mount, phase, owner, 0, n) },
	}
}

// retireClaims sends hfs_retire's RETIRE: the mount's oldest unretired
// requests, in order, while they share the first one's credentials and are
// TERMINAL, with their result ids. The model assumes no request is held or
// borrowed once TERMINAL (request->held, request->users); the ROOT result's
// capabilities are copied into the root inode.
func (m *recallModel) retireClaims(mount *modelMount, phase *modelPhase, owner string) *guestWait {
	var cred *guestbridge.Credentials
	hashes, through := []string{}, mount.claimFloor
	for _, c := range mount.claims {
		if c.number <= mount.claimFloor {
			continue
		}
		if cred == nil {
			cred = &c.cred
		}
		id := resultID(c.wait)
		if !sameJSON(*cred, c.cred) || id == "" {
			break
		}
		hashes, through = append(hashes, id), c.number
	}
	if len(hashes) == 0 {
		return nil
	}
	request := map[string]any{"realm": testUUID(100).String(), "subject": "616263", "anchor": testUUID(101).String(), "nonce": testUUID(103).String(), "through": through, "result_hashes": hashes, "credentials": *cred}
	return m.retireWait(mount, phase, owner+" RETIRE", "RETIRE", request, func(floor uint64) { mount.claimFloor = floor })
}

// retireViews sends hfs_view_retire's next VIEW_RETIRE, for the first of
// families [i, n) with retirable tickets, and queues the rest of the walk.
// A ticket is retirable once terminal with its reply id, and an acquisition
// ticket once its lease is RELEASED too.
func (m *recallModel) retireViews(mount *modelMount, phase *modelPhase, owner string, i, n int) *guestWait {
	for ; i < n; i++ {
		f := mount.families[i]
		hashes, through := []string{}, f.floor
		for _, t := range f.tickets {
			if t.number <= f.floor {
				continue
			}
			id := ticketID(t)
			if t.number != through+1 || id == "" {
				break
			}
			hashes, through = append(hashes, id), t.number
		}
		if len(hashes) == 0 {
			continue
		}
		next := i + 1
		mount.jobs = append([]cleanupJob{func() *guestWait { return m.retireViews(mount, phase, owner, next, n) }}, mount.jobs...)
		request := map[string]any{"anchor": f.anchor, "nonce": f.nonce, "credentials": f.cred, "through": through, "result_hashes": hashes}
		return m.retireWait(mount, phase, owner+" VIEW_RETIRE", "VIEW_RETIRE", request, func(floor uint64) { f.floor = floor })
	}
	return nil
}

// retireWait sends one RETIRE / VIEW_RETIRE. Any reply ends it: RETIRED
// advances the floor (advance), BLOCKED leaves it (the guest tries again at
// its next cleanup). No reply (Facts.CheckPhysical WouldBlock retried in the
// control worker's ring) expires it at the window.
func (m *recallModel) retireWait(mount *modelMount, phase *modelPhase, label, op string, request map[string]any, advance func(uint64)) *guestWait {
	w := &guestWait{label: label, phase: phase, mount: mount.index, kind: "retire", terminal: func(string) bool { return true }}
	phase.waits = append(phase.waits, w)
	w.onDone = func() {
		var reply struct {
			Status string
			Floor  uint64
		}
		require(m.t, json.Unmarshal(w.payload, &reply))
		if reply.Status == "RETIRED" {
			advance(reply.Floor)
		}
	}
	w.start = m.startWait()
	m.launch(w, m.noReply(mount.chain, op, request))
	return w
}

// resultID returns a TERMINAL claim's result id, or "".
func resultID(w *guestWait) string {
	if !w.done || w.expired || w.status != "TERMINAL" {
		return ""
	}
	var reply struct{ Result struct{ ID string } }
	if json.Unmarshal(w.payload, &reply) != nil {
		return ""
	}
	return reply.Result.ID
}

// ticketID returns a retirable view ticket's reply id, or "".
func ticketID(t *ticketRec) string {
	w := t.wait
	if w == nil || !w.done || w.expired || !viewTerminal[w.status] {
		return ""
	}
	if t.acquire && w.status == "GRANTED" && (t.release == nil || !t.release.done || t.release.status != "RELEASED") {
		return ""
	}
	var reply struct{ ID string }
	if json.Unmarshal(w.payload, &reply) != nil {
		return ""
	}
	return reply.ID
}
