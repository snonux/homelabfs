//go:build freebsd

package runtimeprep

import (
	"context"
	"fmt"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// validateRuntimeConfig refuses on FreeBSD: there controlledruntime.Run would
// continue past validation into the native runtime, so the side-effect-free
// validation path exists only on the Linux workstation. Generation is a
// workstation step (zq3 Phase 0); nothing here starts a runtime.
func validateRuntimeConfig(context.Context, controlledruntime.RuntimeConfig, string) error {
	return fmt.Errorf("generate runs only on the Linux workstation, where runtime validation has no effects: %w", fsapi.Unsupported)
}
