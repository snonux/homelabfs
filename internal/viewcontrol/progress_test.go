package viewcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func TestStalePrunedExpectationInstallsActualCutBeforeFreshGrant(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "expected", false)
	_, old := r.acquire(0, page(description, 0), "READ")
	released := r.release(0, old.Lease)
	r.retireControl(0, 1, []string{released.ID})
	r.retire(0, 1, []string{old.ID})
	r.call(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61", "max_end": 4096})
	_, current := r.acquire(0, page(description, 0), "READ")
	closed := r.release(0, current.Lease)
	r.retireControl(0, 2, []string{closed.ID})
	r.retire(0, 2, []string{current.ID})
	if len(r.controller.state.Sources) != 0 {
		t.Fatal("expected archive was not pruned at exact floors")
	}
	before, err := r.controller.capture(t.Context())
	must(t, err)
	r.call(0, "READ", map[string]any{"description": description, "offset": 0, "count": 1})
	r.call(0, "GETATTR", map[string]any{"object": r.call(0, "ROOT", map[string]any{}).Capabilities[0]})
	after, err := r.controller.capture(t.Context())
	must(t, err)
	if !sameSource(before, after) {
		t.Fatal("parked READ/GETATTR changed source/atime and would loop recalls")
	}
	// Re-prune the read-only recapture under a new verified ticket floor.
	_, temporary := r.acquire(0, page(description, 0), "READ")
	done := r.release(0, temporary.Lease)
	r.retireControl(0, 3, []string{done.ID})
	r.retire(0, 3, []string{temporary.ID})
	staleTicket := r.ticket(0)
	pending := r.view(0, "VIEW_ACQUIRE", map[string]any{"request": staleTicket, "domain": r.domains[0], "access": page(description, 0), "mode": "READ", "target": old.Lease.Target})
	if pending.Status != "PENDING" || pending.Lease != nil {
		t.Fatalf("stale expectation failed to trigger stopped install %+v", pending)
	}
	// This domain is the only one with physical grants; the independently enrolled
	// second domain has never received any lease and cannot possess cache rights.
	r.ack(0, "STOPPED", r.emptyManifest())
	r.poll(0)
	r.ack(0, "INSTALLED_DRAIN", nil)
	r.ack(0, "DRAINED", r.emptyManifest())
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
	blocked := r.view(0, "VIEW_QUERY", map[string]any{"request": staleTicket})
	if blocked.Status != "BLOCKED" {
		t.Fatalf("stale ticket granted old cut %+v", blocked)
	}
	freshTicket := r.ticket(0)
	actualSource, err := r.controller.capture(t.Context())
	must(t, err)
	actual := r.controller.target(r.scopes[0], actualSource)
	granted := r.view(0, "VIEW_ACQUIRE", map[string]any{"request": freshTicket, "domain": r.domains[0], "access": page(description, 0), "mode": "READ", "target": actual})
	if granted.Status != "GRANTED" || !equal(granted.Lease.Target, actual) {
		t.Fatalf("fresh current cut did not grant %+v", granted)
	}
	replay := r.view(0, "VIEW_ACQUIRE", map[string]any{"request": staleTicket, "domain": r.domains[0], "access": page(description, 0), "mode": "READ", "target": old.Lease.Target})
	if replay.Status != "PENDING" {
		t.Fatal("stale admission initial replay changed")
	}
}

func TestAcquisitionGapCannotSpendPreownedControlProgress(t *testing.T) {
	r := newRig(t, func(c *Config) {
		b := &c.Authorization.Enrollment.Bounds
		b.Tickets = 3
		b.ControlTickets = 8
		b.Reservations = 2
		b.Identities = 64
		b.Sources = 16
		b.LedgerBytes = 12 << 20
		b.WorkBytes = 192 << 20
	})
	_, description := r.create(0, "credits", false)
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	_, reservePayload := r.reserve(0, dirty.Lease, description)
	reserved := r.view(0, "DIRTY_RESERVE", reservePayload)
	_, other := r.acquire(0, page(description, 4096), "EXCLUSIVE_DIRTY")
	originalTicket := r.ticket(0)
	number := guestbridge.ClaimNumber{Realm: uid(201).String(), Subject: "616263", Anchor: uid(202).String(), Nonce: uid(203).String(), Number: 1}
	original := map[string]any{"request": originalTicket, "lease": other.Lease, "description": description, "offset": 4096, "length": 4096, "claim_number": number}
	if _, err := r.execute(0, "DIRTY_RESERVE", original); !errors.Is(err, fsapi.NoSpace) {
		t.Fatalf("acquisition arena did not saturate %v", err)
	}
	_, family := ticketKeys(r.scopes[0], originalTicket)
	if r.controller.state.Families[family].High != 3 {
		t.Fatal("unadmitted acquisition counter skipped")
	}
	// Entirely guest initiated: a preowned RELEASE ticket starts recall even
	// though pressure READ cannot pass the acquisition-family counter hole.
	releaseTicket := r.controlTicket(0)
	releasePayload := map[string]any{"request": releaseTicket, "lease": dirty.Lease}
	r.physical("VIEW_RELEASE", releasePayload)
	pendingRelease := r.view(0, "VIEW_RELEASE", releasePayload)
	if pendingRelease.Status != "PENDING" || r.controller.state.Recall == nil {
		t.Fatal("funded guest release did not trigger pressure recall")
	}
	polled := r.poll(0)
	if !bytes.Contains(polled, []byte(`"phase":"RECALL"`)) {
		t.Fatal("guest poll did not receive recall")
	}
	frozenPayload := freezeRequest(r.controlTicket(0), *reserved.Reservation, r.controller.state.Recall.ID, "616263")
	frozenReply := r.view(0, "DIRTY_FREEZE", frozenPayload)
	manifest, _ := canonical(map[string]any{"hash": digest([]json.RawMessage{frozenReply.Entry}), "entries": []json.RawMessage{frozenReply.Entry}})
	r.ack(0, "STOPPED", manifest)
	raw, _ := canonical(frozenPayload)
	var parsed request
	must(t, json.Unmarshal(raw, &parsed))
	_, call, err := frozen(parsed)
	must(t, err)
	r.sendCall(0, call)
	r.poll(0)
	r.ack(0, "INSTALLED_DRAIN", nil)
	r.ack(0, "DRAINED", manifest)
	r.ack(0, "INSTALLED_FINAL", nil)
	r.ack(0, "REOPENED", nil)
	firstClosed := r.view(0, "VIEW_QUERY", map[string]any{"request": releaseTicket})
	secondClosed := r.release(0, other.Lease)
	if firstClosed.Status != "RELEASED" || secondClosed.Status != "RELEASED" {
		t.Fatal("funded releases did not close after actual source closure")
	}
	r.retireControl(0, 3, []string{firstClosed.ID, frozenReply.ID, secondClosed.ID})
	r.retire(0, 3, []string{dirty.ID, reserved.ID, other.ID})
	// Retry EXACT original bytes/number. Its stale lease has no authority; source-
	// backed cancellation must close the untouched future ordinary number too.
	resolved := r.view(0, "DIRTY_RESERVE", original)
	if resolved.Status != "BLOCKED" {
		t.Fatal("old original request gained new authority")
	}
	if retired := r.retire(0, 4, []string{resolved.ID}); !bytes.Contains(retired, []byte(`"status":"BLOCKED"`)) {
		t.Fatal("view floor orphaned declined ordinary number")
	}
	r.physical("VIEW_CANCEL", original)
	closed := r.view(0, "VIEW_CANCEL", map[string]any{"request": originalTicket})
	if closed.Status != "CANCELLED" {
		t.Fatal("original gap did not close")
	}
	if gap := r.retireGap(closed.ID); !bytes.Contains(gap, []byte(`"floor":1`)) {
		t.Fatalf("ordinary unbound gap not retired %s", gap)
	}
	if floor := r.retire(0, 4, []string{closed.ID}); !bytes.Contains(floor, []byte(`"floor":4`)) {
		t.Fatal("original view gap not retired")
	}
}

func TestActualJournalCorruptionAndRecoveryFactsFailClosed(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "journal", false)
	_, grant := r.acquire(0, page(description, 0), "READ")
	directory, err := r.config.Source.OpenDir(t.Context(), r.config.Stream.Directory())
	must(t, err)
	entries, readErr := directory.ReadDir(t.Context(), 16)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		t.Fatal(readErr)
	}
	must(t, directory.Close())
	if len(entries) == 0 {
		t.Fatal("no actual journal segments")
	}
	file, err := r.config.Source.Open(t.Context(), path.Join(r.config.Stream.Directory(), entries[0].Name), os.O_RDWR, 0)
	must(t, err)
	original := make([]byte, 1)
	_, err = file.ReadAt(t.Context(), original, 16)
	must(t, err)
	changed := []byte{original[0] ^ 1}
	_, err = file.WriteAt(t.Context(), changed, 16)
	must(t, err)
	must(t, file.Sync(t.Context()))
	if err = r.controller.CheckTarget(t.Context(), r.scopes[0], grant.Lease.Target); err == nil {
		t.Fatal("actual corrupted original frame supplied source proof")
	}
	_, err = file.WriteAt(t.Context(), original, 16)
	must(t, err)
	must(t, file.Sync(t.Context()))
	must(t, file.Close())
	must(t, r.controller.Disconnect(t.Context(), r.scopes[0]))
	if err = r.controller.Recover(t.Context()); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("missing actual continuity facts reopened %v", err)
	}
	state, err := r.controller.rawState()
	must(t, err)
	r.facts.mu.Lock()
	r.facts.recovery = hash(state)
	r.facts.mu.Unlock()
	must(t, r.controller.Recover(t.Context()))
	must(t, r.controller.CheckTarget(t.Context(), r.scopes[0], grant.Lease.Target))
}

func TestConcurrentScopedQueriesAndReservedNonblockingAdmission(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "race", false)
	ticket, grant := r.acquire(0, page(description, 0), "READ")
	query, _ := canonical(map[string]any{"request": ticket})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 8 {
				reply, err := r.controller.ExchangeView(context.Background(), r.scopes[0], "VIEW_QUERY", query)
				if errors.Is(err, fsapi.WouldBlock) {
					continue
				}
				if err != nil {
					t.Error(err)
					return
				}
				if status(reply) != "GRANTED" {
					t.Errorf("changed protected query %s", reply)
				}
			}
		})
	}
	wg.Wait()
	// Holding a real admitted cut does not make competing acquisitions wait while
	// owning service resources: they return WouldBlock without allocating a ticket.
	native := fsapi.Command{Operation: fsapi.Getattr, Mount: uid(1), Object: r.service.FrontendInfo().Root}
	permit, err := r.controller.AdmitNative(t.Context(), fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}, native)
	must(t, err)
	acquire, _ := canonical(map[string]any{"request": r.ticket(1), "domain": r.domains[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": grant.Lease.Target})
	// Target scope is intentionally wrong; use null discovery to test contention.
	var object map[string]any
	must(t, json.Unmarshal(acquire, &object))
	object["target"] = nil
	acquire, _ = canonical(object)
	_, err = r.controller.ExchangeView(t.Context(), r.scopes[1], "VIEW_ACQUIRE", acquire)
	permit.Close()
	if !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("acquisition waited/bypassed held serialized permit %v", err)
	}
}
