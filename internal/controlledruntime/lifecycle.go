//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
)

// Lifecycle stages. SERVING is the original READY assembly. RESTARTED means the
// one supported C9 same-owner restart activated its reopened owners. FENCED
// means an attempted restart failed: the supervisor and every reachable service
// are fenced and all original/partial owners are retained for inspection.
const (
	lifecycleServing   = "SERVING"
	lifecycleRestarted = "RESTARTED"
	lifecycleFenced    = "FENCED"
)

// lifecycleUnreportable is a reply status, never a stage: the command was
// consumed and may have taken effect (for RESTART, Outcome names the actual
// RESTARTED/FENCED result), but its full record does not fit the funded bound.
const lifecycleUnreportable = "EFFECT_UNREPORTABLE"

// runtimeLifecycle owns the actual current owners of one READY disposable
// runtime: the surviving Supervisor and Admissions plus either the original
// Assembly or the C9-restored endpoint. It is reachable only through the
// private lifecycle endpoint; it accepts no Facts, native handles, sources or
// owner objects from callers. Its server-issued runtime/cut/epoch binding is the
// only command target. There is no rollback: a failed attempt stays fenced.
type runtimeLifecycle struct {
	mu          sync.Mutex
	supervisor  *Supervisor
	admissions  []*Admission
	sessions    []string
	replace     shellSwitch
	sourceBytes int64
	runtime     fsapi.UUID
	cut         fsapi.UUID
	epoch       uint64
	stage       string
	current     lifecycleOwners
	restored    *RestoredEndpoint // nonnil after Reopen ran; possibly partial
	outcome     *lifecycleRestartView
	lostReply   *lifecycleLostReply // the single successful ARM, if any (v24)
}

// lifecycleOwners are the actual objects that observation reads. They are
// never decoded from files; restart replaces them only after activation.
type lifecycleOwners struct {
	service    *fsservice.Service
	facts      *Facts
	providers  map[string]*controlledvalidation.Provider
	controller *viewcontrol.Controller
	producer   *Producer
	transport  *guestbridge.Transport
}

// lifecycleRestartView is the bounded, inert outcome of the single restart
// attempt. Step names the first failed C9 stage; Partial names nonnil partial
// owners that remain open (and are closed only at runtime exit, files kept).
// Status is RESTARTED or FENCED, or EFFECT_UNREPORTABLE when the post-attempt
// evidence did not fit: then Outcome keeps the actual result, Evidence is
// omitted and EvidenceError says why.
type lifecycleRestartView struct {
	Status   string             `json:"status"`
	Outcome  string             `json:"outcome,omitempty"`
	Step     string             `json:"step,omitempty"`
	Error    string             `json:"error,omitempty"`
	Barrier  string             `json:"barrier,omitempty"`
	Physical string             `json:"physical,omitempty"`
	Epoch    uint64             `json:"epoch"`
	Cut      string             `json:"cut"`
	Partial  []string           `json:"partial_owners,omitempty"`
	Evidence *lifecycleEvidence `json:"evidence,omitempty"`
	// EvidenceError explains an omitted Evidence (EFFECT_UNREPORTABLE only).
	EvidenceError string `json:"evidence_error,omitempty"`
	// ProtectError is set in the reply (and in later OBSERVE restart sections)
	// when control-restart.json could not be protected; the file itself never
	// claims its own protection.
	ProtectError string `json:"protect_error,omitempty"`
}

// newRuntimeLifecycle binds the actual READY assembly. replace is the native
// shell-switch primitive; nil keeps RESTART unsupported (refused before effect).
func newRuntimeLifecycle(s *Supervisor, service *fsservice.Service, a *Assembly, admissions []*Admission, replace shellSwitch, sourceBytes int64) (*runtimeLifecycle, error) {
	if s == nil || service == nil || a == nil || a.Producer == nil || a.Transport == nil || a.Facts == nil || len(admissions) == 0 || sourceBytes < 1 {
		return nil, fsapi.Invalid
	}
	l := &runtimeLifecycle{supervisor: s, admissions: append([]*Admission(nil), admissions...), sessions: sortedSessions(a.Facts.enrollment), replace: replace, sourceBytes: sourceBytes, epoch: 1, stage: lifecycleServing}
	l.current = lifecycleOwners{service: service, facts: a.Facts, providers: a.Providers, controller: a.Controller, producer: a.Producer, transport: a.Transport}
	if _, err := rand.Read(l.runtime[:]); err != nil {
		return nil, err
	}
	if _, err := rand.Read(l.cut[:]); err != nil {
		return nil, err
	}
	return l, nil
}

// binding returns the current server-issued command target.
func (l *runtimeLifecycle) binding() (runtime, cut fsapi.UUID, epoch uint64, stage string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.runtime, l.cut, l.epoch, l.stage
}

// observe refreshes every actual Admission and captures current owner evidence.
// It holds the lifecycle mutex, so no restart can switch owners mid-capture.
func (l *runtimeLifecycle) observe(ctx context.Context) lifecycleEvidence {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.observeLocked(ctx)
}

func (l *runtimeLifecycle) observeLocked(ctx context.Context) lifecycleEvidence {
	e := lifecycleEvidence{Version: lifecycleEvidenceVersion, Runtime: l.runtime.String(), Epoch: l.epoch, Cut: l.cut.String(), Stage: l.stage}
	e.Supervisor = captureSupervisor(l.supervisor)
	for _, a := range l.admissions {
		e.Admissions = append(e.Admissions, captureAdmission(ctx, a))
	}
	owners := l.inspectedOwners()
	e.Service = captureService(ctx, owners.service, l.sourceBytes)
	e.Providers = captureProviders(ctx, owners.providers, l.sessions)
	e.Controller = captureController(ctx, owners.controller)
	e.Transport = captureTransport(owners.transport)
	e.LostReply = l.lostReplyViewLocked()
	if l.outcome != nil {
		view := *l.outcome
		view.Evidence = nil
		e.Restart = &view
	}
	e.Complete = e.complete() && l.stage != lifecycleFenced
	return e
}

// inspectedOwners selects the owners an observation describes. After any
// Reopen ran, its (possibly partial) owners are reported, including absences,
// rather than the suspended originals, which would only report closure.
func (l *runtimeLifecycle) inspectedOwners() lifecycleOwners {
	a := l.restored
	if a == nil {
		return l.current
	}
	return lifecycleOwners{service: a.Service, facts: a.Facts, providers: a.Providers, controller: a.Controller, producer: a.Producer, transport: a.Transport}
}

// errLifecycleRefused marks refusals that consumed no restart attempt.
var errLifecycleRefused = errors.New("lifecycle command refused")

// restart performs the single supported C9 transition through the actual
// SuspendEndpoint, Restart.Reopen and Restart.Activate methods on the retained
// owners. The binding must name the exact current cut and epoch. Once checks
// pass, the attempt is consumed: a fresh cut/epoch is issued whatever happens.
func (l *runtimeLifecycle) restart(ctx context.Context, cut fsapi.UUID, epoch uint64) (lifecycleRestartView, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stage != lifecycleServing {
		return lifecycleRestartView{}, fmt.Errorf("%w: restart already attempted (%s)", errLifecycleRefused, l.stage)
	}
	if cut != l.cut || epoch != l.epoch {
		return lifecycleRestartView{}, fmt.Errorf("%w: stale cut/epoch binding", errLifecycleRefused)
	}
	if l.replace == nil {
		return lifecycleRestartView{}, fmt.Errorf("%w: native shell switch unsupported: %v", errLifecycleRefused, fsapi.Unsupported)
	}
	next := fsapi.UUID{}
	if _, err := rand.Read(next[:]); err != nil {
		return lifecycleRestartView{}, fmt.Errorf("%w: %v", errLifecycleRefused, err)
	}
	l.epoch++
	l.cut = next
	view := l.runRestart(ctx)
	view.Epoch, view.Cut = l.epoch, l.cut.String()
	return l.reportRestartLocked(ctx, view), nil
}

// reportRestartLocked records the consumed attempt, attaches its post-attempt
// evidence, bounds the record and protects it before any reply exists. The
// stored outcome, which later OBSERVEs report without evidence, keeps the
// actual RESTARTED/FENCED status and also learns why the protected record
// lacks evidence (EvidenceError) or protection (ProtectError).
func (l *runtimeLifecycle) reportRestartLocked(ctx context.Context, view lifecycleRestartView) lifecycleRestartView {
	outcome := view
	l.outcome = &outcome
	evidence := l.observeLocked(ctx)
	view.Evidence = &evidence
	view, raw := boundRestartOutcome(view, lifecycleOutcomeBytes)
	if err := protectRestartOutcome(l.supervisor, raw); err != nil {
		view.ProtectError = evidenceError(err)
	}
	outcome.EvidenceError, outcome.ProtectError = view.EvidenceError, view.ProtectError
	return view
}

// boundRestartOutcome returns the reportable outcome and its encoding of at
// most limit bytes. The attempt has already taken effect, so an outcome whose
// post-attempt evidence does not fit is never refused: Status becomes
// EFFECT_UNREPORTABLE, Outcome keeps the actual RESTARTED/FENCED result, every
// bounded field (step, error, barrier, physical, epoch, cut, partial owners)
// is kept and only Evidence is replaced by its encoded size. Those fields are
// bounded by construction (512-byte error text, at most six owner names plus
// one per enrolled session, at most 16), so the reduced record always fits; a
// nil encoding is refused by protectRestartOutcome and never written.
func boundRestartOutcome(view lifecycleRestartView, limit int) (lifecycleRestartView, []byte) {
	raw, err := json.Marshal(view)
	if err == nil && len(raw) <= limit {
		return view, raw
	}
	reason := fmt.Sprintf("post-attempt evidence encodes to %d bytes, over the %d-byte outcome bound; OBSERVE reports the current owners", len(raw), limit)
	if err != nil {
		reason = "post-attempt evidence not encodable: " + evidenceError(err)
	}
	view.Outcome, view.Status = view.Status, lifecycleUnreportable
	view.Evidence, view.EvidenceError = nil, reason
	if raw, err = json.Marshal(view); err != nil || len(raw) > limit {
		return view, nil
	}
	return view, raw
}

// runRestart drives the three C9 stages. Success switches owners; failure
// fences the supervisor (closing its management channel, which ends the
// runtime after the reply) and blocks every reachable service.
func (l *runtimeLifecycle) runRestart(ctx context.Context) lifecycleRestartView {
	view := lifecycleRestartView{Status: lifecycleFenced}
	// An armed fixture belongs to the consumed cut; it never crosses C9.
	l.expireLostReplyLocked()
	r, err := l.supervisor.SuspendEndpoint(ctx, l.current.producer, l.current.transport)
	if err != nil {
		view.Step, view.Error = "suspend", evidenceError(err)
		l.fenceLocked()
		return view
	}
	view.Barrier = r.barrier.String()
	a, err := r.Reopen(ctx)
	if a != nil {
		l.restored = a
	}
	if err == nil {
		view.Physical = r.physical
		err = r.activate(ctx, a, l.replace)
		if err != nil {
			view.Step = "activate"
		}
	} else {
		view.Step = "reopen"
	}
	if err != nil {
		view.Error = evidenceError(err)
		view.Partial = partialOwners(a)
		l.fenceLocked()
		return view
	}
	l.current = lifecycleOwners{service: a.Service, facts: a.Facts, providers: a.Providers, controller: a.Controller, producer: a.Producer, transport: a.Transport}
	l.stage, view.Status = lifecycleRestarted, lifecycleRestarted
	return view
}

// fenceLocked keeps failures fenced: no exporter resume, no rollback, no owner
// close. Files, native mount and VM resources remain for inspection.
func (l *runtimeLifecycle) fenceLocked() {
	l.stage = lifecycleFenced
	l.supervisor.fence()
	l.blockLocked("controlled-c9-restart-fenced")
}

// blockAdmission stops admission on every reachable actual service owner.
func (l *runtimeLifecycle) blockAdmission(reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.blockLocked(reason)
}

func (l *runtimeLifecycle) blockLocked(reason string) {
	l.current.service.BlockAdmission(reason)
	if l.restored != nil && l.restored.Service != nil {
		l.restored.Service.BlockAdmission(reason)
	}
}

// fenced reports whether a restart attempt failed.
func (l *runtimeLifecycle) fenced() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stage == lifecycleFenced
}

// close joins restored transport workers, closes reopened owners and returns
// the lost-reply fixture funding. It never removes a ledger or infers session
// retirement. Original Assembly owners stay with the driver, whose closes are
// idempotent after suspension. It runs only after the endpoint worker joined.
func (l *runtimeLifecycle) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.releaseLostReplyLocked()
	a := l.restored
	if a == nil {
		return err
	}
	if a.Transport != nil {
		a.Transport.Close()
	}
	if a.Controller != nil {
		err = errors.Join(err, a.Controller.Close())
	}
	for _, p := range a.Providers {
		if p != nil {
			err = errors.Join(err, p.Close())
		}
	}
	if a.Service != nil {
		err = errors.Join(err, a.Service.Close())
	}
	return err
}

func partialOwners(a *RestoredEndpoint) []string {
	if a == nil {
		return nil
	}
	names := []string{}
	for name, present := range map[string]bool{"service": a.Service != nil, "facts": a.Facts != nil, "controller": a.Controller != nil, "producer": a.Producer != nil, "bridge": a.Bridge != nil, "transport": a.Transport != nil} {
		if present {
			names = append(names, name)
		}
	}
	for session := range a.Providers {
		names = append(names, "provider:"+session)
	}
	sort.Strings(names)
	return names
}

// protectRestartOutcome writes the single bounded restart outcome encoding
// (boundRestartOutcome) to the exclusive private owner file
// control-restart.json before the reply is sent, so a lost reply cannot lose
// the record. An existing file is preserved and refused. A protection failure
// is reported in the reply; it never changes or retries the attempt itself.
func protectRestartOutcome(s *Supervisor, raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("restart outcome not encodable: %w", fsapi.Invalid)
	}
	if len(raw) > lifecycleOutcomeBytes {
		return fmt.Errorf("restart outcome %d bytes: %w", len(raw), fsapi.NoSpace)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.directory == nil {
		return fsapi.Unavailable
	}
	f, err := ownedFile(s.directory, "control-restart.json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.directory.Sync()
	}
	return err
}
