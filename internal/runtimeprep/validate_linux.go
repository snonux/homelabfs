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
func validateRuntimeConfig(ctx context.Context, c controlledruntime.RuntimeConfig) error {
	sandbox, err := os.MkdirTemp("", "hfs-runtime-prep-")
	if err != nil {
		return err
	}
	// The sandbox holds a copy of the host receipt keys; remove only this
	// tool's own fresh temporary directory, never anything it did not create.
	defer func() { _ = os.RemoveAll(sandbox) }()
	if err := os.Mkdir(filepath.Join(sandbox, "handoff"), 0o700); err != nil {
		return err
	}
	c.VolumeConfig = filepath.Join(sandbox, "volume.json")
	c.OwnerDirectory = filepath.Join(sandbox, "owner")
	c.EnrollmentFile = filepath.Join(sandbox, "handoff", "enrollment.json")
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	path := filepath.Join(sandbox, "runtime-config.json")
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
