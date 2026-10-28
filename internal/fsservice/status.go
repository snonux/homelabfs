package fsservice

import "github.com/snonux/homelabfs/internal/fsapi"

// Status is an immutable local observation. Positions retain full journal
// identities; backing slots are separate redo counters. It confers no authority.
// Replica progress is deliberately absent: no normal protocol supplies it yet.
type Status struct {
	State           State
	WaitReason      string
	SourcePosition  fsapi.Position
	AppliedPosition fsapi.Position
	BackingSlots    uint64
}

// Status returns immediately without using the worker queue, storage, or mount.
// A snapshot is published only by the worker (or startup before worker launch).
func (s *Service) Status() Status {
	status := Status{State: Recovering, WaitReason: "starting"}
	if snapshot := s.status.Load(); snapshot != nil {
		status = *snapshot
	}
	if reason := s.stopReason.Load(); reason != nil {
		status.State = Blocked
		status.WaitReason = *reason
	}
	return status
}

// BlockAdmission permanently rejects new and queued workload requests while
// preserving the worker, frontend lease and backing resources for inspection.
// Already running work retains ownership of its resources; this does not drain,
// cancel or repair it. Already parked waits can retain requests/resources until
// the worker wakes; this is neither a drain nor a fence. Only a separately
// admitted new service can reopen access.
func (s *Service) BlockAdmission(reason string) {
	if reason == "" {
		reason = "external-admission-stopped"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopReason.CompareAndSwap(nil, &reason)
}

func (s *Service) setState(state State, reason string) {
	s.state.Store(uint32(state))
	s.publishStatus(reason)
}
func (s *Service) publishStatus(reason string) {
	s.status.Store(&Status{State: s.State(), WaitReason: reason, SourcePosition: s.observedSource, AppliedPosition: s.applied.Applied.Position, BackingSlots: s.applied.Slots})
}
