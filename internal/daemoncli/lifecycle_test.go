package daemoncli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestOpenLocalCanceledRecoveryReleasesLifecycleLock(t *testing.T) {
	c, _ := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := openLocal(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("open canceled = %v", err)
	}
	workspaces, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
	if err != nil || len(workspaces) != 1 {
		t.Fatal("failed workspace not preserved", workspaces, err)
	}
	failed := filepath.Join(c.VolumeDir, "workspaces", workspaces[0].Name())
	local, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal("failed recovery retained exclusive lock", err)
	}
	defer func() {
		if err := local.close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := os.Stat(failed); err != nil {
		t.Fatal("retry removed failed workspace", err)
	}
	if local.service == nil || len(local.roots) != 5 {
		t.Fatal("recovery did not open full workspace")
	}
	if err := local.close(); err != nil {
		t.Fatal(err)
	}
	if local.lock != nil || local.service != nil || len(local.roots) != 0 {
		t.Fatal("close retained resources")
	}
	if err := local.close(); err != nil {
		t.Fatal("close is not idempotent", err)
	}
}

func TestOpenLocalCompetingOpenDoesNotCreateWorkspaceOrDropOwner(t *testing.T) {
	c, _ := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	owner, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := openLocal(t.Context(), c); err == nil || !strings.Contains(err.Error(), "exclusive volume lifecycle") {
		t.Fatal("competing open", err)
	}
	workspaces, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
	if err != nil || len(workspaces) != 1 {
		t.Fatal("competing open created workspace", workspaces, err)
	}
	// The rejected contender's cleanup must not close the original service.
	send(t, owner, fsapi.Command{Operation: fsapi.Statfs})
	if _, err := openLocal(t.Context(), c); err == nil {
		t.Fatal("rejected contender released original lock")
	}
}

func TestServePreflightNeverStartsMountOrWorkspace(t *testing.T) {
	for _, scenario := range []string{"nonempty", "missing", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := fixtureConfig(t)
			if _, err := initVolume(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "nonempty":
				if err := os.WriteFile(filepath.Join(c.Mountpoint, "keep"), []byte("sentinel"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(c.Mountpoint); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				actual := c.Mountpoint + "-actual"
				if err := os.Rename(c.Mountpoint, actual); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(actual, c.Mountpoint); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			if err := serve(t.Context(), c, false, &output, logger); err == nil {
				t.Fatal("accepted invalid mountpoint")
			}
			if output.Len() != 0 {
				t.Fatal("preflight emitted readiness")
			}
			workspaces, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
			if err != nil || len(workspaces) != 0 {
				t.Fatal("preflight opened workspace", workspaces, err)
			}
			if _, err := os.Lstat(socketPath(c)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("preflight created admin socket", err)
			}
			if scenario == "nonempty" {
				raw, err := os.ReadFile(filepath.Join(c.Mountpoint, "keep"))
				if err != nil || string(raw) != "sentinel" {
					t.Fatal("preflight changed mountpoint", err)
				}
			}
		})
	}
}

func TestExerciseClientWritesSyncsAndRenamesLocalFile(t *testing.T) {
	dir := t.TempDir()
	if err := exerciseClient(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "cli-validation-") || !strings.HasSuffix(entries[0].Name(), "-verified") {
		t.Fatal(entries, err)
	}
	name := filepath.Join(dir, entries[0].Name())
	raw, err := os.ReadFile(name)
	if err != nil || string(raw) != "synthetic HomelabFS CLI validation\n" {
		t.Fatal(string(raw), err)
	}
	info, err := os.Stat(name)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	if err := exerciseClient(filepath.Join(dir, "absent")); err == nil {
		t.Fatal("accepted missing client directory")
	}
}
