package viewcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func (r *rig) stoppedPayload(i int, manifest json.RawMessage) json.RawMessage {
	r.t.Helper()
	event := r.controller.state.Recall.Events[r.scopes[i].Session]
	var e map[string]json.RawMessage
	must(r.t, json.Unmarshal(event, &e))
	raw, err := canonical(map[string]any{"domain": r.domains[i], "sequence": e["sequence"], "recall": e["recall"], "phase": "STOPPED", "authority_hash": e["authority_hash"], "closure_hash": e["closure_hash"], "inventory_hash": e["inventory_hash"], "session": r.scopes[i].Session, "manifest": manifest, "target": nil})
	must(r.t, err)
	return raw
}
func (r *rig) proveExclusion(i int, ack json.RawMessage, reservations ...guestbridge.DirtyReservation) {
	r.t.Helper()
	ledger, err := r.controller.rawState()
	must(r.t, err)
	statement := Unadopted{Enrollment: r.controller.config.Authorization.Enrollment, Event: r.controller.state.Recall.Events[r.scopes[i].Session], Acknowledgement: ack, Ledger: ledger, Scope: r.scopes[i], OldFence: r.controller.state.Fence, Omitted: reservations}
	raw, err := canonical(statement)
	must(r.t, err)
	r.facts.mu.Lock()
	r.facts.exclusions[digest(statement)] = raw
	r.facts.mu.Unlock()
}
func (r *rig) isolatedReserve(lease *guestbridge.ViewLease, description guestbridge.Capability) (guestbridge.Ticket, map[string]any) {
	r.t.Helper()
	ticket, payload := r.reserve(0, lease, description)
	r.next[0]--
	payload["claim_number"] = guestbridge.ClaimNumber{Realm: uid(201).String(), Subject: "616263", Anchor: uid(202).String(), Nonce: uid(203).String(), Number: 1}
	return ticket, payload
}
func (r *rig) retireGap(proof string) json.RawMessage {
	r.t.Helper()
	raw, err := canonical(map[string]any{"realm": uid(201).String(), "subject": "616263", "anchor": uid(202).String(), "nonce": uid(203).String(), "through": 1, "result_hashes": []string{proof}})
	must(r.t, err)
	result, err := r.providers[0].Retire(r.t.Context(), r.scopes[0], raw)
	must(r.t, err)
	return result
}

func TestStoppedOmissionRequiresActualExclusionAndClosesExactTypedGap(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "unreceived", false)
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	ticket, payload := r.isolatedReserve(dirty.Lease, description)
	reserved := r.view(0, "DIRTY_RESERVE", payload)
	// Treat the protected grant reply as delayed/unreceived by the guest; a real
	// kernel proof must establish its old revision can never adopt it into a PTE.
	_, pending := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	if pending.Status != "PENDING" {
		t.Fatal("missing recall")
	}
	ack := r.stoppedPayload(0, r.emptyManifest())
	r.physical("VIEW_ACK", ack)
	if _, err := r.execute(0, "VIEW_ACK", ack); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("omission/hash alone supplied exclusion %v", err)
	}
	if r.controller.state.Reservations[reserved.Reservation.ID].Status != "RESERVED" {
		t.Fatal("false omission changed durable obligation")
	}
	r.proveExclusion(0, ack, *reserved.Reservation)
	_, err := r.execute(0, "VIEW_ACK", ack)
	must(t, err)
	cancelled := r.view(0, "VIEW_QUERY", map[string]any{"request": ticket})
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("missing protected no-effect outcome %+v", cancelled)
	}
	if receipt := r.retireGap(cancelled.ID); !bytes.Contains(receipt, []byte(`"status":"RETIRED"`)) {
		t.Fatalf("typed gap did not use actual CANCELLED id %s", receipt)
	}
	replay := r.view(0, "DIRTY_RESERVE", payload)
	if replay.Status != "GRANTED" || replay.Reservation.ID != reserved.Reservation.ID || r.controller.state.Reservations[reserved.Reservation.ID].Status != "CLOSED" {
		t.Fatal("late immutable grant reactivated old physical epoch")
	}
	// A changed STOPPED manifest is never substituted after the protected ACK.
	changed := bytes.Replace(ack, []byte(`"sequence":1`), []byte(`"sequence":2`), 1)
	if _, err = r.execute(0, "VIEW_ACK", changed); err == nil {
		t.Fatal("changed stopped binding advanced state")
	}
}

func TestUnadoptedReservingWithoutNumberSurvivesBothClosureCrashWindows(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "no-number", false)
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	ticket, payload := r.isolatedReserve(dirty.Lease, description)
	raw, _ := canonical(payload)
	crash := errors.New("component crash at protected boundary")
	r.controller.config.Failpoint = func(point string) error {
		if point == "state-protected" {
			return crash
		}
		return nil
	}
	if _, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_RESERVE", raw); !errors.Is(err, crash) {
		t.Fatalf("missing pre-earmark crash %v", err)
	}
	var reservation guestbridge.DirtyReservation
	for _, s := range r.controller.state.Reservations {
		reservation = s.Reservation
	}
	r.reopen()
	acquire, _ := canonical(map[string]any{"request": r.ticket(0), "domain": r.domains[0], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
	_, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_ACQUIRE", acquire)
	must(t, err)
	ack := r.stoppedPayload(0, r.emptyManifest())
	r.physical("VIEW_ACK", ack)
	r.proveExclusion(0, ack, reservation)
	r.controller.config.Failpoint = func(point string) error {
		if point == "earmark-closed-before-ticket" {
			return crash
		}
		return nil
	}
	if _, err = r.controller.Exchange(t.Context(), r.scopes[0], "VIEW_ACK", ack); !errors.Is(err, crash) {
		t.Fatalf("missing cross-ledger close crash %v", err)
	}
	if r.controller.state.Reservations[reservation.ID].Status != "CANCELLING" {
		t.Fatal("published cancellation before both ledgers")
	}
	r.reopen()
	beforeACKQuery, _ := canonical(map[string]any{"request": ticket})
	recovered, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_QUERY", beforeACKQuery)
	must(t, err)
	if status(recovered) != "CANCELLED" {
		t.Fatalf("original query did not reconcile protected closure %s", recovered)
	}
	r.proveExclusion(0, ack, reservation)
	_, err = r.controller.Exchange(t.Context(), r.scopes[0], "VIEW_ACK", ack)
	must(t, err)
	query, _ := canonical(map[string]any{"request": ticket})
	reply, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_QUERY", query)
	must(t, err)
	var cancelled viewReply
	must(t, json.Unmarshal(reply, &cancelled))
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("lost close outcome %s", reply)
	}
	if receipt := r.retireGap(cancelled.ID); !bytes.Contains(receipt, []byte(`"floor":1`)) {
		t.Fatalf("next exact high-water gap not protected %s", receipt)
	}
	// No ordinary Claim or placeholder OriginalHash was ever created for this gap.
	late, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_RESERVE", raw)
	must(t, err)
	if status(late) != "BLOCKED" {
		t.Fatalf("unissued grant appeared after exclusion %s", late)
	}
}

func TestStoppedCannotOmitBoundOrFrozenSource(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(map[bool]string{false: "FROZEN", true: "FROZEN_PENDING"}[pending], func(t *testing.T) {
			r := newRig(t)
			_, description := r.create(0, "bound", false)
			_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
			_, payload := r.reserve(0, dirty.Lease, description)
			reserved := r.view(0, "DIRTY_RESERVE", payload)
			r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
			request := freezeRequest(r.controlTicket(0), *reserved.Reservation, r.controller.state.Recall.ID, "616263")
			raw, _ := canonical(request)
			if pending {
				r.controller.config.Failpoint = func(point string) error {
					if point == "frozen-claim-protected" {
						return fsapi.IO
					}
					return nil
				}
			}
			_, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_FREEZE", raw)
			if pending {
				if !errors.Is(err, fsapi.IO) {
					t.Fatal(err)
				}
			} else {
				must(t, err)
			}
			r.controller.config.Failpoint = nil
			ack := r.stoppedPayload(0, r.emptyManifest())
			r.physical("VIEW_ACK", ack)
			r.proveExclusion(0, ack, *reserved.Reservation)
			if _, err = r.execute(0, "VIEW_ACK", ack); !errors.Is(err, fsapi.Stale) {
				t.Fatalf("bound source omitted despite concrete frozen call %v", err)
			}
		})
	}
}

func TestReopenRebindsOnlyTheExactRealController(t *testing.T) {
	r := newRig(t)
	old := r.controller
	r.reopen()
	for _, provider := range r.providers {
		must(t, provider.CheckViewProtocol(r.controller, r.controller))
		if err := provider.CheckViewProtocol(old, old); !errors.Is(err, fsapi.Permission) {
			t.Fatalf("closed prior controller remained bound %v", err)
		}
	}
}

func TestRequiredRealProviderRejectsMixedLegacyScopeOmission(t *testing.T) {
	r := newRig(t)
	protocol := guestbridge.ViewProtocol()
	p := r.providers[0]
	config := guestbridge.Config{Validation: r.config.Validation, SessionProtocols: map[string]guestbridge.Protocol{r.scopes[1].Session: protocol}, ViewProtocol: r.controller, Endpoint: r.natives[0].Endpoint, Native: &nativePrimitive{native: r.natives[0]}, Attestor: attestorPrimitive{r.scopes[0], r.credentials[0]}, Claims: p, Execution: p, Views: r.controller, Scopes: append([]guestbridge.Context(nil), r.scopes...)}
	// prepareProtocols accepts the mixed selection, but real required p0 refuses
	// the other scope through CheckValidation before any endpoint enrollment. A
	// real single-scope provider cannot hide a legacy default in this configuration.
	if _, err := guestbridge.New(t.Context(), config); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("mixed legacy omission accepted %v", err)
	}
}
