//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/json"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Facts binds installed guest observations, actual native callbacks and complete
// retained controller/service sources. It supplies no recovery or VM fence.
type Facts struct {
	service    *fsservice.Service
	enrollment viewcontrol.Enrollment
	admissions []*Admission
}

type factsLedger struct {
	Fence   uint64
	Sources map[string]fsservice.ControlledViewSource
	Domains map[string]struct {
		Domain   guestbridge.Capability
		Sequence uint64
	}
	Leases map[string]struct {
		Lease  guestbridge.ViewLease
		Active bool
	}
	Reservations map[string]struct {
		Reservation               guestbridge.DirtyReservation
		Status, Recall, TicketKey string
		Call                      *guestbridge.Call
		Entry                     json.RawMessage
	}
	Tickets map[string]struct {
		Scope                     guestbridge.Context
		Request                   guestbridge.Ticket
		Operation                 string
		Payload, Current, Initial json.RawMessage
	}
	Recall *struct {
		ID, Phase, Inventory, Closure string
		Events, Acks, Manifests       map[string]json.RawMessage
	}
}

type physicalACK struct {
	Domain    guestbridge.Capability  `json:"domain"`
	Sequence  uint64                  `json:"sequence"`
	Recall    string                  `json:"recall"`
	Phase     string                  `json:"phase"`
	Authority string                  `json:"authority_hash"`
	Closure   string                  `json:"closure_hash"`
	Inventory string                  `json:"inventory_hash"`
	Session   string                  `json:"session"`
	Manifest  json.RawMessage         `json:"manifest"`
	Target    *guestbridge.ViewTarget `json:"target"`
}

var _ controlledvalidation.Facts = (*Facts)(nil)
var _ viewcontrol.Facts = (*Facts)(nil)

// NewFacts requires a measured, still-live complete inventory for every domain.
// No callback or claimed JSON can stand in for ObserveAdmission's private owner.
func NewFacts(ctx context.Context, service *fsservice.Service, enrollment viewcontrol.Enrollment, admissions []*Admission) (*Facts, error) {
	if service == nil || len(admissions) == 0 || len(admissions) > 64 || len(enrollment.Domains) == 0 {
		return nil, fsapi.Invalid
	}
	raw, err := json.Marshal(enrollment)
	if err != nil {
		return nil, err
	}
	var owned viewcontrol.Enrollment
	if err = json.Unmarshal(raw, &owned); err != nil {
		return nil, err
	}
	for _, domain := range owned.Domains {
		if domain.Protocol == nil || !runtimeProtocol(*domain.Protocol) || !domain.Profile.Disposable {
			return nil, fsapi.Permission
		}
	}
	f := &Facts{service: service, enrollment: owned, admissions: append([]*Admission(nil), admissions...)}
	if err = f.CheckEnrollment(ctx, enrollment); err != nil {
		return nil, err
	}
	return f, nil
}

// Check rechecks the exact original provider scope and actual sealed service.
func (f *Facts) Check(ctx context.Context, e controlledvalidation.Enrollment) error {
	for _, expected := range f.enrollment.Domains {
		if sameJSON(expected, e) {
			if err := f.checkScope(ctx, e.Scope); err != nil {
				return err
			}
			journal, err := f.service.ControlledJournalInfo(ctx, e.Profile)
			if err != nil {
				return err
			}
			if journal != e.Journal || f.service.FrontendInfo().Generation != e.ServingGeneration {
				return fsapi.Stale
			}
			return nil
		}
	}
	return fsapi.Permission
}

// CheckEnrollment checks all original domains, including old stopped owners.
func (f *Facts) CheckEnrollment(ctx context.Context, enrollment viewcontrol.Enrollment) error {
	if !sameJSON(enrollment, f.enrollment) || f.service.State() != fsservice.Serving {
		return fsapi.Permission
	}
	count := 0
	seen := map[string]bool{}
	for _, admission := range f.admissions {
		if admission == nil {
			return fsapi.Permission
		}
		admission.mu.Lock()
		domains := append([]guestbridge.Context(nil), admission.installed.Admission.Domains...)
		admission.mu.Unlock()
		for _, scope := range domains {
			if seen[scope.Session] {
				return fsapi.Permission
			}
			seen[scope.Session] = true
			count++
			found := false
			for _, expected := range enrollment.Domains {
				if expected.Scope == scope {
					found = true
				}
			}
			if !found || admission.check(scope) != nil {
				return fsapi.Permission
			}
			expected, ok := f.protocol(scope)
			admission.mu.Lock()
			actual := admission.installed.Admission.Protocols[scope.Session]
			admission.mu.Unlock()
			if !ok || expected != actual {
				return fsapi.Permission
			}
		}
	}
	if count != len(enrollment.Domains) {
		return fsapi.Permission
	}
	for _, domain := range enrollment.Domains {
		if err := f.Check(ctx, domain); err != nil {
			return err
		}
	}
	return nil
}

// Authorize requires Service.Execute's private observation and independently
// rechecks its actual native process/controller. Credential/grant Unix policy
// remains in the bridge/service; source/physical facts remain separate below.
func (f *Facts) Authorize(ctx context.Context, scope guestbridge.Context, op string, raw json.RawMessage) error {
	observed, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || observed.Scope != scope || observed.Operation != op || observed.PayloadHash != hashBytes(raw) || observed.Protocol == nil || !f.matchesProtocol(scope, *observed.Protocol) {
		return fsapi.Permission
	}
	return f.authorizeNative(ctx, scope, observed)
}

// CheckPhysical accepts ACKs only when the fixed read-only observer measured the
// same admitted kernel's completed exact phase before this dispatch. All source
// bytes and all ledger obligations are checked synchronously without source RPC.
func (f *Facts) CheckPhysical(ctx context.Context, scope guestbridge.Context, op string, raw, ledger json.RawMessage) error {
	if op != "VIEW_CANCEL" {
		if err := f.Authorize(ctx, scope, op, raw); err != nil {
			// A query may resume only the exact protected release selected by
			// this controller. The actual outer/native observation stays intact.
			if viewcontrol.VerifyPhysicalContinuation(ctx, f.service, f, scope, op, raw) != nil {
				return err
			}
			observed, _ := guestbridge.VerifiedRequest(ctx)
			if err := f.authorizeNative(ctx, scope, observed); err != nil {
				return err
			}
		}
	}
	l, err := f.ledger(ctx, ledger)
	if err != nil {
		return err
	}
	if op == "VIEW_ACK" {
		return f.checkACK(ctx, scope, raw, l)
	}
	if op == "VIEW_CANCEL" {
		return f.checkCancellation(ctx, scope, raw, l)
	}
	if op != "VIEW_RELEASE" && op != "VIEW_RETIRE" {
		return fsapi.Unsupported
	}
	// Release/retirement cannot clear an active physical or dirty owner. The
	// controller's actual STOP/install/reopen loop closes these first.
	for _, lease := range l.Leases {
		if lease.Lease.Scope == scope && lease.Active {
			return fsapi.WouldBlock
		}
	}
	for _, reservation := range l.Reservations {
		if reservation.Reservation.Scope == scope && reservation.Status != "CLOSED" {
			return fsapi.WouldBlock
		}
	}
	return nil
}

// CheckRecovery only permits a genuinely initial empty controller. Live seals
// disappear on persistent reopen; full C9 reconstruction/exclusion is mandatory
// for every existing physical owner. Nothing here fabricates a positive fence.
func (f *Facts) CheckRecovery(ctx context.Context, enrollment viewcontrol.Enrollment, raw json.RawMessage) error {
	if err := f.CheckEnrollment(ctx, enrollment); err != nil {
		return err
	}
	l, err := f.ledger(ctx, raw)
	if err != nil {
		return err
	}
	if l.Fence != 1 || len(l.Sources)+len(l.Domains)+len(l.Leases)+len(l.Reservations)+len(l.Tickets) != 0 || l.Recall != nil {
		return fsapi.Unavailable
	}
	return nil
}

// CheckUnadopted checks the complete universal exclusion set against actual
// retained reservations and the measured admitted kernel STOP/adoption barrier.
func (f *Facts) CheckUnadopted(ctx context.Context, u viewcontrol.Unadopted) error {
	if !sameJSON(u.Enrollment, f.enrollment) {
		return fsapi.Permission
	}
	if err := f.Authorize(ctx, u.Scope, "VIEW_ACK", u.Acknowledgement); err != nil {
		return err
	}
	l, err := f.ledger(ctx, u.Ledger)
	if err != nil {
		return err
	}
	if l.Fence != u.OldFence || l.Recall == nil || hashBytes(l.Recall.Events[u.Scope.Session]) != hashBytes(u.Event) {
		return fsapi.Stale
	}
	if err = f.checkACK(ctx, u.Scope, u.Acknowledgement, l); err != nil {
		return err
	}
	var ack physicalACK
	if json.Unmarshal(u.Acknowledgement, &ack) != nil || ack.Phase != "STOPPED" {
		return fsapi.Permission
	}
	var manifest struct{ Entries []json.RawMessage }
	if json.Unmarshal(ack.Manifest, &manifest) != nil {
		return fsapi.Invalid
	}
	included := map[string]bool{}
	for _, entry := range manifest.Entries {
		var r struct{ Reservation string }
		if json.Unmarshal(entry, &r) != nil || included[r.Reservation] {
			return fsapi.Stale
		}
		saved, ok := l.Reservations[r.Reservation]
		if !ok || saved.Reservation.Scope != u.Scope || saved.Call == nil || saved.Status != "FROZEN" || saved.Recall != ack.Recall || !sameJSON(saved.Entry, entry) {
			return fsapi.Stale
		}
		included[r.Reservation] = true
	}
	omitted := map[string]guestbridge.DirtyReservation{}
	for _, r := range l.Reservations {
		if r.Reservation.Scope != u.Scope || r.Status == "CLOSED" || included[r.Reservation.ID] {
			continue
		}
		if r.Call != nil || r.Status != "RESERVED" && r.Status != "RESERVING" && r.Status != "CANCELLING" {
			return fsapi.Stale
		}
		omitted[r.Reservation.ID] = r.Reservation
	}
	if len(omitted) != len(u.Omitted) {
		return fsapi.Stale
	}
	for _, r := range u.Omitted {
		saved, ok := omitted[r.ID]
		if !ok || !sameJSON(saved, r) {
			return fsapi.Stale
		}
		delete(omitted, r.ID)
	}
	return nil
}

func (f *Facts) authorizeNative(ctx context.Context, scope guestbridge.Context, observed guestbridge.CommandObservation) error {
	if observed.Scope != scope || observed.Protocol == nil || !f.matchesProtocol(scope, *observed.Protocol) {
		return fsapi.Permission
	}
	if err := f.checkScope(ctx, scope); err != nil {
		return err
	}
	a := f.admission(scope)
	if a == nil || observed.Native.Endpoint != a.endpoint {
		return fsapi.Permission
	}
	return a.supervisor.Verify(ctx, observed.Native, observed.Controller)
}

func (f *Facts) checkScope(ctx context.Context, scope guestbridge.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a := f.admission(scope)
	if a == nil {
		return fsapi.Permission
	}
	return a.check(scope)
}
func (f *Facts) admission(scope guestbridge.Context) *Admission {
	for _, a := range f.admissions {
		if a == nil {
			continue
		}
		a.mu.Lock()
		found := false
		for _, enrolled := range a.installed.Admission.Domains {
			if enrolled == scope {
				found = true
			}
		}
		a.mu.Unlock()
		if found {
			return a
		}
	}
	return nil
}
func (f *Facts) ledger(ctx context.Context, raw json.RawMessage) (factsLedger, error) {
	var l factsLedger
	if err := f.CheckEnrollment(ctx, f.enrollment); err != nil {
		return l, err
	}
	if len(raw) > f.enrollment.Bounds.LedgerBytes || json.Unmarshal(raw, &l) != nil || l.Fence == 0 {
		return l, fsapi.Invalid
	}
	if err := viewcontrol.VerifyRetainedSources(ctx, f.service, raw); err != nil {
		return l, err
	}
	for session, domain := range l.Domains {
		if domain.Domain.Scope.Session != session || f.admission(domain.Domain.Scope) == nil {
			return l, fsapi.Stale
		}
	}
	for _, lease := range l.Leases {
		if !sameJSON(l.Domains[lease.Lease.Scope.Session].Domain, lease.Lease.Domain) {
			return l, fsapi.Stale
		}
	}
	for _, r := range l.Reservations {
		if f.admission(r.Reservation.Scope) == nil || l.Domains[r.Reservation.Scope.Session].Domain.Token == "" {
			return l, fsapi.Stale
		}
	}
	if l.Recall != nil {
		if len(l.Recall.Events) != len(l.Domains) {
			return l, fsapi.Stale
		}
		for session := range l.Domains {
			if len(l.Recall.Events[session]) == 0 {
				return l, fsapi.Stale
			}
		}
	}
	return l, nil
}
func (f *Facts) checkACK(_ context.Context, scope guestbridge.Context, raw json.RawMessage, l factsLedger) error {
	var ack physicalACK
	if json.Unmarshal(raw, &ack) != nil || l.Recall == nil || ack.Session != scope.Session || ack.Recall != l.Recall.ID || ack.Inventory != l.Recall.Inventory || ack.Closure != l.Recall.Closure || ack.Authority != scope.AuthorityHash || ack.Domain.Scope != scope {
		return fsapi.Stale
	}
	var event struct {
		Sequence uint64
		Domain   guestbridge.Capability
		Phase    string
		Target   *guestbridge.ViewTarget
	}
	issued := l.Recall.Events[scope.Session]
	if json.Unmarshal(issued, &event) != nil || event.Sequence != ack.Sequence || !sameJSON(event.Domain, ack.Domain) {
		return fsapi.Stale
	}
	phase := map[string]string{"STOPPED": "RECALL", "INSTALLED_DRAIN": "INSTALL_DRAIN", "DRAINED": "INSTALL_DRAIN", "INSTALLED_FINAL": "INSTALL_FINAL", "REOPENED": "REOPEN"}[ack.Phase]
	if phase == "" || event.Phase != phase || !sameJSON(event.Target, ack.Target) {
		return fsapi.Stale
	}
	a := f.admission(scope)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, observation := range a.installed.Admission.Observations {
		if observation.Scope == scope && observation.Fence == l.Fence && observation.Phase == ack.Phase && observation.EventHash == hashBytes(issued) && observation.AcknowledgementHash == hashBytes(raw) {
			return nil
		}
	}
	return fsapi.Permission
}
func (f *Facts) checkCancellation(ctx context.Context, scope guestbridge.Context, original json.RawMessage, l factsLedger) error {
	var r struct {
		Request guestbridge.Ticket `json:"request"`
	}
	if json.Unmarshal(original, &r) != nil {
		return fsapi.Invalid
	}
	actual, err := canonicalJSON(map[string]any{"request": r.Request})
	if err != nil {
		return err
	}
	if err = f.Authorize(ctx, scope, "VIEW_CANCEL", actual); err != nil {
		return err
	}
	for _, ticket := range l.Tickets {
		if ticket.Scope != scope || !sameJSON(ticket.Request, r.Request) || !sameJSON(ticket.Payload, original) {
			continue
		}
		var reply struct {
			Status      string
			Lease       *guestbridge.ViewLease
			Reservation *guestbridge.DirtyReservation
		}
		if json.Unmarshal(ticket.Current, &reply) != nil {
			return fsapi.Invalid
		}
		if reply.Status == "BLOCKED" || reply.Status == "PENDING" {
			return nil
		}
		// A previously granted owner needs separately observed stopped exclusion;
		// a plain CANCEL request carries no physical acknowledgement.
		return fsapi.WouldBlock
	}
	return fsapi.Stale
}

func (f *Facts) protocol(scope guestbridge.Context) (guestbridge.Protocol, bool) {
	for _, domain := range f.enrollment.Domains {
		if domain.Scope == scope && domain.Protocol != nil {
			return *domain.Protocol, true
		}
	}
	return guestbridge.Protocol{}, false
}
func (f *Facts) matchesProtocol(scope guestbridge.Context, p guestbridge.Protocol) bool {
	expected, ok := f.protocol(scope)
	return ok && expected == p
}
func runtimeProtocol(p guestbridge.Protocol) bool {
	return guestbridge.IsControlledProtocol(p)
}
