package fsservice

import (
	"context"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// ControlledAdmission governs native observations and mutations in the explicit
// view profile. Admission runs outside the service worker and never waits on a
// recall; only a fully closed cut may enter serialized execution.
type ControlledAdmission interface {
	AdmitNative(context.Context, fsapi.Cred, fsapi.Command) (ControlledPermit, error)
}

// ControlledPermit owns one admitted cut. Check also runs on lock retries;
// CheckSource preflights full planned source growth before any effect. None of
// these methods may wait for physical recall or reenter the worker.
type ControlledPermit interface {
	Check(context.Context, *ControlledCut) error
	CheckSource(context.Context, *ControlledCut, []ControlledEffect) error
	Close()
}

// BindControlledAdmission installs exactly one controller before the explicit
// profile can admit any native or delegated effects. It never activates a mount.
func (s *Service) BindControlledAdmission(profile ValidationProfile, admission ControlledAdmission) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.config.RequireControlledViews || s.config.Validation == nil || *s.config.Validation != profile || admission == nil || s.viewAdmission != nil && !closedControlledAdmission(s.viewAdmission) || s.closed {
		return fsapi.Permission
	}
	s.viewAdmission = admission
	return nil
}

// ControlledViewsRequired identifies the explicit immutable service profile.
func (s *Service) ControlledViewsRequired() bool { return s.config.RequireControlledViews }

// ControlledAdmitted checks an already acquired permit on the serialized worker.
// Source capture remains available through Controlled for the controller itself;
// ordinary Image/Stat/Plan/Apply methods refuse that unadmitted callback.
func (s *Service) ControlledAdmitted(ctx context.Context, profile ValidationProfile, permit ControlledPermit, run func(context.Context, *ControlledCut) error) error {
	if permit == nil {
		return fsapi.Permission
	}
	return s.Controlled(ctx, profile, func(owned context.Context, cut *ControlledCut) error {
		if err := permit.Check(owned, cut); err != nil {
			return err
		}
		cut.admitted = true
		cut.permit = permit
		previous := s.activePermit
		s.activePermit = permit
		defer func() { s.activePermit = previous }()
		return run(owned, cut)
	})
}

func (c *ControlledCut) accessAllowed() bool {
	return c.active && (!c.service.config.RequireControlledViews || c.admitted)
}

func closedControlledAdmission(value any) bool {
	old, ok := value.(interface{ IsClosed() bool })
	return ok && old.IsClosed()
}
