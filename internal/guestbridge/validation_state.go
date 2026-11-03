package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

// ValidationEnrollment binds the private profile to both authenticated endpoints.
// Providers implementing private finalization must additionally verify the exact
// operator-approved profile and actual FUSE endpoint at component construction.
type ValidationEnrollment interface {
	CheckValidation(context.Context, Context, fsservice.ValidationProfile, Endpoint) error
}

// ValidationState is the private controlled-provider finalization seam. Normal
// providers never use it. A terminal result and the entire grant/process state
// are persisted atomically before reply. Recovery supplies the original local
// outcome, never replans the submitted command or manufactures publication.
type ValidationState interface {
	RecoverOutcome(context.Context, Binding) (Outcome, bool, error)
	Finalize(context.Context, Binding, json.RawMessage, json.RawMessage) error
	Restore(context.Context, Context) (json.RawMessage, error)
}
type validationAncestor struct {
	Parent, Object                fsapi.ObjectID
	ObjectRevision, EntryRevision uint64
	Name                          []byte
}

type validationSnapshot struct {
	Controller     fsapi.UUID
	Context        Context
	Generation     fsapi.UUID
	Credentials    map[string][]byte
	Exited         map[string]uint64
	Grants         map[string]*grant
	CloseSequences map[string]uint64
}

func (s *Service) restoreValidation(ctx context.Context, scope Context) error {
	store, ok := s.config.Claims.(ValidationState)
	if s.config.Validation == nil || !ok {
		return nil
	}
	raw, err := store.Restore(ctx, scope)
	if err != nil || len(raw) == 0 {
		return err
	}
	var snapshot validationSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	if snapshot.Context != scope || !snapshot.Controller.Valid() {
		return fsapi.Permission
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[scope.Session]
	// Exact terminal results remain queryable in a new generation, while old
	// capabilities and descriptions cannot become live grants in that generation.
	// Credential conflicts and exited process incarnations remain protected.

	if len(snapshot.Grants) > s.config.MaxCapabilities || len(snapshot.Credentials) > s.config.MaxCredentialRevisions {
		return fsapi.NoSpace
	}
	for key, g := range snapshot.Grants {
		if g == nil || key != g.Cap.Token || g.Cap.Scope != scope {
			return fsapi.Stale
		}
	}
	for key, value := range ss.Credentials {
		if old, ok := snapshot.Credentials[key]; ok && !bytes.Equal(old, value) {
			return fsapi.Stale
		}
		if snapshot.Credentials == nil {
			snapshot.Credentials = make(map[string][]byte)
		}
		snapshot.Credentials[key] = bytes.Clone(value)
	}
	if len(snapshot.Credentials) > s.config.MaxCredentialRevisions {
		return fsapi.NoSpace
	}
	ss.Credentials = snapshot.Credentials
	ss.Exited = snapshot.Exited
	if snapshot.Generation == ss.Generation {
		ss.Grants = snapshot.Grants
	}
	ss.CloseSequences = snapshot.CloseSequences
	if ss.Credentials == nil {
		ss.Credentials = map[string][]byte{}
	}
	if ss.Exited == nil {
		ss.Exited = map[string]uint64{}
	}
	if ss.Grants == nil {
		ss.Grants = map[string]*grant{}
	}
	if ss.CloseSequences == nil {
		ss.CloseSequences = map[string]uint64{}
	}
	return nil
}
func (s *Service) finalizeResult(ctx context.Context, b Binding, out Outcome) (json.RawMessage, error) {
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	// A concurrent query/call may already have protected the original result.
	old, err := s.config.Claims.Resolve(ctx, b.Context, b.Call.Claim)
	if err != nil {
		return nil, err
	}
	if old.Status == "TERMINAL" {
		return old.Result, nil
	}
	if _, private := s.config.Claims.(ValidationState); s.config.Validation != nil && private {
		return nil, fsapi.Unavailable
	}
	result, err := s.result(ctx, b, out)
	if err != nil {
		return nil, err
	}
	if err = s.config.Claims.ProtectResult(ctx, b, result); err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Service) recoverValidation(ctx context.Context, state ClaimState) (ClaimState, error) {
	store, ok := s.config.Claims.(ValidationState)
	if s.config.Validation == nil || !ok || !state.Binding.Operation.Valid() {
		return state, nil
	}
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	// Only the private provider can authorize a never-planned continuation.
	// Reconciled/stale bindings must remain recovery-only.
	if state.Status == "RESERVED" && state.ReconciledScope == nil {
		if _, ok := s.config.Execution.(ValidationContinuation); ok {
			if err := s.restoreValidation(ctx, state.Binding.Context); err != nil {
				return state, err
			}
			if _, err := s.executeCall(ctx, state.Binding, true); err != nil {
				return state, err
			}
		}
	}
	if state.Status != "TERMINAL" {
		_, ready, err := store.RecoverOutcome(ctx, state.Binding)
		if err != nil {
			return state, err
		}
		if !ready {
			return state, nil
		}
	}
	current, err := s.config.Claims.Resolve(ctx, state.Binding.Context, state.Binding.Call.Claim)
	if err != nil {
		return state, err
	}
	if current.Status == "TERMINAL" {
		if err = s.restoreValidation(ctx, state.Binding.Context); err != nil {
			return state, err
		}
	}
	return current, nil
}

// ValidationExecution protects the original planned semantic result, wire
// capability bytes and full bridge snapshot before the local filesystem effect.
// Only the explicitly selected private controlled profile may use this seam.
type ValidationExecution interface {
	ExecuteValidation(context.Context, Binding, func(Captured) (Prepared, error), func(Outcome) (json.RawMessage, json.RawMessage, error)) (Outcome, error)
}

func (s *Service) prepareValidationResult(ctx context.Context, b Binding, out Outcome) (json.RawMessage, json.RawMessage, error) {
	s.mu.Lock()
	raw, err := json.Marshal(s.sessions[b.Context.Session])
	s.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	var proposed session
	if err = json.Unmarshal(raw, &proposed); err != nil {
		return nil, nil, err
	}
	staged := &Service{controller: s.controller, config: s.config, sessions: map[string]*session{b.Context.Session: &proposed}}
	result, err := staged.result(ctx, b, out)
	if err != nil {
		return nil, nil, err
	}
	snapshot, err := json.Marshal(validationSnapshot{Controller: s.controller, Context: proposed.Context, Generation: proposed.Generation, Credentials: proposed.Credentials, Exited: proposed.Exited, Grants: proposed.Grants, CloseSequences: proposed.CloseSequences})
	return result, snapshot, err
}

func (s *Service) privateValidation() bool {
	_, ok := s.config.Execution.(ValidationExecution)
	return s.config.Validation != nil && ok
}

// MarshalJSON retains raw filesystem name bytes in the private session ledger.
func (a *ancestor) MarshalJSON() ([]byte, error) {
	return json.Marshal(validationAncestor{Parent: a.Parent, Object: a.Object, ObjectRevision: a.ObjectRevision, EntryRevision: a.EntryRevision, Name: []byte(a.Name)})
}

// UnmarshalJSON reconstructs the original bytes without UTF-8 normalization.
func (a *ancestor) UnmarshalJSON(raw []byte) error {
	var encoded validationAncestor
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return err
	}
	*a = ancestor{Parent: encoded.Parent, Object: encoded.Object, ObjectRevision: encoded.ObjectRevision, EntryRevision: encoded.EntryRevision, Name: string(encoded.Name)}
	return nil
}

// ValidationContinuation is a private, sealed-stage continuation. Implementations
// must decline preparation once any plan, effect or result has been protected.
type ValidationContinuation interface {
	ResumeValidation(context.Context, Binding, func(Captured) (Prepared, error), func(Outcome) (json.RawMessage, json.RawMessage, error)) (Outcome, error)
}
