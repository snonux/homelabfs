package viewcontrol

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

type retainedSourcesKey struct{}
type retainedSources struct {
	service *fsservice.Service
	profile fsservice.ValidationProfile
	sources map[string]fsservice.ControlledViewSource
}

// VerifyRetainedSources verifies ALL original retained sources carried privately
// by the locked controller, including exact agreement with the complete ledger.
// Exported JSON cannot create this context. The references are borrowed only for
// the synchronous Facts call; no source bytes or ledger credits are duplicated.
func VerifyRetainedSources(ctx context.Context, service *fsservice.Service, raw json.RawMessage) error {
	owned, ok := ctx.Value(retainedSourcesKey{}).(retainedSources)
	if !ok || owned.service != service {
		return fsapi.Permission
	}
	var saved struct {
		Sources map[string]fsservice.ControlledViewSource
	}
	if json.Unmarshal(raw, &saved) != nil || len(saved.Sources) != len(owned.sources) {
		return fsapi.Stale
	}
	for key, source := range owned.sources {
		other, ok := saved.Sources[key]
		if !ok || !sameSource(source, other) {
			return fsapi.Stale
		}
		if err := service.VerifyControlledSource(ctx, owned.profile, source); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) factsContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, retainedSourcesKey{}, retainedSources{service: c.config.Service, profile: c.config.Authorization.Enrollment.Domains[0].Profile, sources: c.state.Sources})
}
