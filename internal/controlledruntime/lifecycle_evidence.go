//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"sort"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// lifecycleEvidenceVersion names the closed observation record below.
const lifecycleEvidenceVersion = "homelabfs-controlled-evidence/1"

// lifecycleErrorBytes bounds every error text copied into evidence, so a
// pathological error chain cannot grow a reply beyond its funded size.
const lifecycleErrorBytes = 512

// lifecycleEvidence is one inert observation of the actual current owners. It
// carries no acceptance flag: Complete only says every section was captured
// without error. Publication is always null. An external reply oracle must
// correlate these records; nothing here proves native/guest behavior.
type lifecycleEvidence struct {
	Version     string                `json:"version"`
	Runtime     string                `json:"runtime"`
	Epoch       uint64                `json:"epoch"`
	Cut         string                `json:"cut"`
	Stage       string                `json:"stage"`
	Publication any                   `json:"publication"`
	Complete    bool                  `json:"complete"`
	Supervisor  supervisorEvidence    `json:"supervisor"`
	Admissions  []admissionEvidence   `json:"admissions"`
	Service     serviceEvidence       `json:"service"`
	Providers   []providerEvidence    `json:"providers"`
	Controller  controllerEvidence    `json:"controller"`
	Transport   transportEvidence     `json:"transport"`
	Restart     *lifecycleRestartView `json:"restart,omitempty"`
	// LostReply is the armed response-exposure fixture record (v24), if any.
	LostReply *lostReplyView `json:"lost_reply,omitempty"`
	// Endpoint is the control endpoint's own accept health. Only OBSERVE
	// replies carry it (the serving worker owns those counters); it does not
	// affect Complete, because a retried transient failure loses no owner.
	Endpoint *endpointEvidence `json:"endpoint,omitempty"`
}

// endpointEvidence counts accept failures of the lifecycle endpoint since
// READY. Transient failures were retried with bounded backoff; a permanent one
// ends the runtime and is reported by its exit error instead.
type endpointEvidence struct {
	AcceptErrors    uint64 `json:"accept_errors"`
	LastAcceptError string `json:"last_accept_error,omitempty"`
}

type supervisorEvidence struct {
	Stage       string               `json:"stage"`
	Process     string               `json:"process"`
	Controller  string               `json:"controller"`
	Endpoint    guestbridge.Endpoint `json:"endpoint"`
	Live        bool                 `json:"live"`
	Error       string               `json:"error,omitempty"`
	Diagnostics *ExporterDiagnostics `json:"diagnostics,omitempty"`
}

type admissionEvidence struct {
	RefreshError  string                   `json:"refresh_error,omitempty"`
	Fenced        bool                     `json:"fenced"`
	BootID        string                   `json:"boot_id"`
	ProviderNonce string                   `json:"provider_nonce"`
	KernelBuildID string                   `json:"kernel_build_id"`
	Physical      string                   `json:"physical"`
	Domains       []domainObservationProof `json:"domains"`
}

type domainObservationProof struct {
	Session  string `json:"session"`
	Phase    string `json:"phase"`
	Revision uint64 `json:"revision"`
	Fence    uint64 `json:"fence"`
}

type serviceEvidence struct {
	Error         string           `json:"error,omitempty"`
	State         string           `json:"state"`
	Journal       string           `json:"journal,omitempty"`
	Owner         string           `json:"owner,omitempty"`
	Generation    string           `json:"generation,omitempty"`
	Configuration string           `json:"configuration,omitempty"`
	Through       *record.FrameRef `json:"through,omitempty"`
	Applied       *fsapi.Position  `json:"applied,omitempty"`
	Visibility    string           `json:"visibility,omitempty"`
	StateHash     string           `json:"state_hash,omitempty"`
	StateBytes    int              `json:"state_bytes,omitempty"`
}

type providerEvidence struct {
	Session string                               `json:"session"`
	Error   string                               `json:"error,omitempty"`
	Ledger  *controlledvalidation.LedgerEvidence `json:"ledger,omitempty"`
}

type controllerEvidence struct {
	Error  string                      `json:"error,omitempty"`
	Ledger *viewcontrol.LedgerEvidence `json:"ledger,omitempty"`
}

// transportEvidence is the compact C8 index. Scope bodies and cell bytes are
// deliberately omitted; Digest covers the complete protected rows.
type transportEvidence struct {
	Error      string            `json:"error,omitempty"`
	Controller string            `json:"controller,omitempty"`
	Session    string            `json:"fuse_session,omitempty"`
	High       uint64            `json:"high"`
	Cells      int               `json:"cells"`
	Ordinary   int               `json:"ordinary"`
	Control    int               `json:"control"`
	Workspace  bool              `json:"workspace"`
	Terminal   int               `json:"terminal"`
	Hellos     int               `json:"hellos"`
	GuestHigh  map[string]uint64 `json:"guest_high,omitempty"`
	Digest     string            `json:"digest,omitempty"`
}

// evidenceError truncates error text to its funded bound.
func evidenceError(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if len(text) > lifecycleErrorBytes {
		text = text[:lifecycleErrorBytes]
	}
	return text
}

func serviceStateName(s fsservice.State) string {
	switch s {
	case fsservice.Recovering:
		return "RECOVERING"
	case fsservice.Serving:
		return "SERVING"
	case fsservice.Blocked:
		return "BLOCKED"
	default:
		return "UNKNOWN"
	}
}

// captureSupervisor reads the actual supervisor issuance record, its live
// check (which includes the child's own liveness) and the bounded s24
// diagnostics snapshot under one supervisor lock hold, so all three describe
// the same moment. It never kills or waits.
func captureSupervisor(s *Supervisor) supervisorEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := supervisorEvidence{Stage: s.record.Stage, Process: s.record.ProcessID.String(), Controller: s.record.Controller.String(), Endpoint: s.record.Endpoint}
	err := s.checkLive()
	e.Live, e.Error = err == nil, evidenceError(err)
	if diagnostics, ok := s.diagnosticsLocked(); ok {
		e.Diagnostics = &diagnostics
	}
	return e
}

// captureAdmission refreshes one actual Admission through its pinned observer
// and then reports the resulting installed inventory. A failed refresh fences
// that Admission exactly as any other refresh would; nothing is retried here.
func captureAdmission(ctx context.Context, a *Admission) admissionEvidence {
	refreshErr := a.Refresh(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	installed := a.installed
	e := admissionEvidence{RefreshError: evidenceError(refreshErr), Fenced: a.fenced, BootID: installed.BootID, ProviderNonce: installed.Admission.ProviderNonce, KernelBuildID: installed.KernelBuildID}
	if raw, err := canonicalJSON(installed.Admission); err == nil {
		e.Physical = hashBytes(raw)
	}
	for _, row := range installed.Admission.Observations {
		e.Domains = append(e.Domains, domainObservationProof{Session: row.Scope.Session, Phase: row.Phase, Revision: row.Revision, Fence: row.Fence})
	}
	return e
}

// captureService takes the actual service journal identity and a fresh
// complete view source on the service worker, bounded by sourceBytes.
func captureService(ctx context.Context, service *fsservice.Service, sourceBytes int64) serviceEvidence {
	if service == nil {
		return serviceEvidence{State: "ABSENT", Error: "service owner absent"}
	}
	e := serviceEvidence{State: serviceStateName(service.State())}
	info := service.FrontendInfo()
	journal, err := service.ControlledJournalInfo(ctx, info.Validation)
	if err != nil {
		e.Error = evidenceError(err)
		return e
	}
	e.Journal, e.Owner = journal.Incarnation.String(), journal.Owner.String()
	e.Generation, e.Configuration = journal.Stream.Generation.String(), journal.Stream.Configuration.String()
	err = service.Controlled(ctx, info.Validation, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		source, err := cut.ViewSource(ctx, sourceBytes)
		if err != nil {
			return err
		}
		through, applied := source.Through, source.Applied
		e.Through, e.Applied, e.Visibility = &through, &applied, source.Visibility.String()
		e.StateHash, e.StateBytes = hashBytes(source.State), len(source.State)
		return nil
	})
	e.Error = evidenceError(err)
	return e
}

// captureProviders reads every actual provider ledger in stable session order.
func captureProviders(ctx context.Context, providers map[string]*controlledvalidation.Provider, sessions []string) []providerEvidence {
	out := make([]providerEvidence, 0, len(sessions))
	for _, session := range sessions {
		row := providerEvidence{Session: session}
		p := providers[session]
		if p == nil {
			row.Error = "provider owner absent"
		} else if ledger, err := p.LedgerEvidence(ctx); err != nil {
			row.Error = evidenceError(err)
		} else {
			row.Ledger = &ledger
		}
		out = append(out, row)
	}
	return out
}

func captureController(ctx context.Context, c *viewcontrol.Controller) controllerEvidence {
	if c == nil {
		return controllerEvidence{Error: "controller owner absent"}
	}
	ledger, err := c.LedgerEvidence(ctx)
	if err != nil {
		return controllerEvidence{Error: evidenceError(err)}
	}
	return controllerEvidence{Ledger: &ledger}
}

func captureTransport(t *guestbridge.Transport) transportEvidence {
	if t == nil {
		return transportEvidence{Error: "transport owner absent"}
	}
	state, err := t.ProtectedEvidence()
	if err != nil {
		return transportEvidence{Error: evidenceError(err)}
	}
	return transportEvidence{Controller: state.Controller.String(), Session: state.Endpoint.FuseSession, High: state.High, Cells: len(state.Cells), Ordinary: state.Ordinary, Control: state.Control, Workspace: state.Workspace, Terminal: len(state.Terminal), Hellos: len(state.RuntimeHellos), GuestHigh: state.GuestHigh, Digest: state.Digest}
}

// complete reports whether every captured section is free of errors.
func (e *lifecycleEvidence) complete() bool {
	if e.Supervisor.Error != "" || e.Service.Error != "" || e.Controller.Error != "" || e.Transport.Error != "" {
		return false
	}
	for _, a := range e.Admissions {
		if a.RefreshError != "" || a.Fenced {
			return false
		}
	}
	for _, p := range e.Providers {
		if p.Error != "" {
			return false
		}
	}
	return true
}

func sortedSessions(e viewcontrol.Enrollment) []string {
	sessions := make([]string, 0, len(e.Domains))
	for _, d := range e.Domains {
		sessions = append(sessions, d.Scope.Session)
	}
	sort.Strings(sessions)
	return sessions
}
