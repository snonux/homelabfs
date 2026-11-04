package daemoncli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snonux/homelabfs/internal/fsservice"
)

// ControlledService retains the existing exclusive volume lock and all backing
// roots. It exposes no self-client frontend and never mounts or publishes.
type ControlledService struct {
	local      *localService
	mountpoint string
	paths      []string
}

// OpenControlled reuses only the disposable volume lifecycle. All limits are
// explicit; the full 4 MiB logical canary is preserved. Controlled view admission
// is mandatory from construction, before any command can enter the worker.
func OpenControlled(ctx context.Context, configPath string, limits fsservice.Limits) (*ControlledService, error) {
	if limits.LogicalBytes != 4<<20 || limits.Queue < 1 || limits.IOBytes < 1 || limits.Handles < 1 || limits.Lookups < 1 || limits.Reservations < 1 || limits.Locks < 1 {
		return nil, fmt.Errorf("controlled service requires explicit finite limits and the full 4 MiB canary")
	}
	c, err := loadConfig(ctx, configPath)
	if err != nil {
		return nil, err
	}
	if c.EnableTestHooks {
		return nil, fmt.Errorf("controlled runtime refuses self-client test hooks")
	}
	l, err := openLocalMode(ctx, c, true, limits)
	if err != nil {
		return nil, err
	}
	return &ControlledService{local: l, mountpoint: c.Mountpoint, paths: []string{c.ControlDir, c.VolumeDir, c.Mountpoint}}, nil
}

// CheckRuntimePaths keeps runtime ledgers and staged consent outside every
// existing source/workspace/control/mount root, including ancestor aliases.
func (c *ControlledService) CheckRuntimePaths(owner, handoff string) error {
	for _, candidate := range []string{owner, handoff} {
		for _, path := range c.paths {
			if candidate == path || strings.HasPrefix(candidate, path+"/") || strings.HasPrefix(path, candidate+"/") {
				return fmt.Errorf("controlled runtime path overlaps volume lifecycle root: %s", candidate)
			}
		}
	}
	return CheckControlledRuntimePaths(owner, handoff)
}

// CheckControlledRuntimePaths validates this profile's two administrative paths
// before creation or waiting. All existing ancestors must be real directories;
// a missing final owner directory or handoff file is permitted. Administrative
// replacement races remain excluded by the existing exclusive-root contract.
func CheckControlledRuntimePaths(owner, handoff string) error {
	for _, path := range []string{owner, handoff} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
			return fmt.Errorf("runtime path must be clean, absolute and below root: %q", path)
		}
	}
	if containsPath(owner, handoff) || containsPath(handoff, owner) {
		return fmt.Errorf("controlled runtime owner and handoff paths overlap")
	}
	if err := checkPath(owner, true); err != nil {
		return err
	}
	parent := filepath.Dir(handoff)
	if parent != "/" {
		if err := checkPath(parent, false); err != nil {
			return err
		}
	}
	st, err := os.Lstat(handoff)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("handoff must be a real regular file: %s", handoff)
	}
	return nil
}

func (c *ControlledService) Mountpoint() string { return c.mountpoint }

func (c *ControlledService) Service() *fsservice.Service { return c.local.service }

// Close closes semantic owners only; it never unmounts or deletes evidence.
func (c *ControlledService) Close() error { return c.local.close() }
