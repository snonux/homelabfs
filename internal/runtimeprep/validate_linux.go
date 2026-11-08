//go:build linux

package runtimeprep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// linuxRefusal is the exact error text controlledruntime.Run returns on Linux
// after readRuntimeFile and validateRuntimeConfig have both accepted a config.
const linuxRefusal = "native controlled runtime requires actual FreeBSD FUSE"

// validateRuntimeConfig runs the actual runtime entry point on the Linux
// workstation, where it is side-effect free: after the strict file decoder and
// validateRuntimeConfig accept the config it refuses with fsapi.Unsupported
// before creating any owner, service, process or mount. Only the three host
// paths are swapped for a private sandbox (their parents must exist for the
// runtime's path checks); the generator checks the real paths itself.
//
// The sandbox is created beside the output bundle, not in a shared $TMPDIR,
// so the transient copy of the host receipt keys stays where the bundle's own
// keys will live. Its names are short because the runtime also checks the
// sandbox owner directory against sockaddr_un.
func validateRuntimeConfig(ctx context.Context, c controlledruntime.RuntimeConfig, output string) error {
	sandbox, err := os.MkdirTemp(filepath.Dir(output), ".v")
	if err != nil {
		return fmt.Errorf("create validation sandbox beside the output: %w", err)
	}
	// Remove only this tool's own fresh sandbox, never anything it did not create.
	defer func() { _ = os.RemoveAll(sandbox) }()
	if len(filepath.Join(sandbox, "o", controlledruntime.LifecycleSocketName)) > socketPathLimit {
		return fmt.Errorf("output parent path too long for the validation sandbox's lifecycle socket; use a shorter --output")
	}
	if err := os.Mkdir(filepath.Join(sandbox, "h"), 0o700); err != nil {
		return err
	}
	c.VolumeConfig = filepath.Join(sandbox, "v.json")
	c.OwnerDirectory = filepath.Join(sandbox, "o")
	c.EnrollmentFile = filepath.Join(sandbox, "h", "e.json")
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	path := filepath.Join(sandbox, "c.json")
	if err := writeExclusive(path, raw); err != nil {
		return err
	}
	err = controlledruntime.Run(ctx, []string{"--config", path, "--disposable-validation"}, io.Discard, io.Discard)
	if errors.Is(err, fsapi.Unsupported) && strings.Contains(err.Error(), linuxRefusal) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("runtime validation unexpectedly returned success on Linux")
	}
	return err
}
