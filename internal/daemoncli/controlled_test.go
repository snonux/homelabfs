package daemoncli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsservice"
)

func TestOpenControlledRealVolumeLifecycle(t *testing.T) {
	c, path := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	limits := fsservice.Limits{Queue: 8, IOBytes: 65536, Handles: 64, Lookups: 128, Reservations: 128, Locks: 64, LogicalBytes: 4 << 20}
	for _, bad := range []fsservice.Limits{{}, {Queue: 8, IOBytes: 65536, Handles: 64, Lookups: 128, Reservations: 128, Locks: 64, LogicalBytes: 1 << 20}} {
		if owner, err := OpenControlled(t.Context(), path, bad); err == nil {
			_ = owner.Close()
			t.Fatal("implicit or reduced canary bounds accepted")
		}
	}
	owner, err := OpenControlled(t.Context(), path, limits)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	}()
	if !owner.Service().ControlledViewsRequired() || owner.Mountpoint() != c.Mountpoint || owner.Service().State() != fsservice.Serving {
		t.Fatal("actual controlled service missing")
	}
	if owner.CheckRuntimePaths(c.VolumeDir+"/runtime", c.ControlDir+"/consent") == nil {
		t.Fatal("runtime overlaps existing lifecycle root")
	}
	if second, err := OpenControlled(t.Context(), path, limits); err == nil {
		_ = second.Close()
		t.Fatal("second volume writer admitted")
	}
}

func TestControlledRuntimePathsRejectParentAliases(t *testing.T) {
	root := t.TempDir()
	volume, mount := filepath.Join(root, "volume"), filepath.Join(root, "mount")
	for _, path := range []string{volume, mount, filepath.Join(volume, "source")} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	service := &ControlledService{paths: []string{volume, mount}}
	owner, handoff := filepath.Join(root, "new-runtime"), filepath.Join(root, "missing-consent.json")
	if err := service.CheckRuntimePaths(owner, handoff); err != nil {
		t.Fatal("normal new paths refused", err)
	}
	for name, target := range map[string]string{"volume": volume, "source": filepath.Join(volume, "source"), "mount": mount} {
		t.Run(name, func(t *testing.T) {
			alias := filepath.Join(root, "alias-"+name)
			if err := os.Symlink(target, alias); err != nil {
				t.Fatal(err)
			}
			if err := service.CheckRuntimePaths(filepath.Join(alias, "runtime"), handoff); err == nil {
				t.Fatal("owner parent alias admitted")
			}
			if err := service.CheckRuntimePaths(owner, filepath.Join(alias, "consent.json")); err == nil {
				t.Fatal("handoff parent alias admitted")
			}
		})
	}
	// Mutual aliases outside lifecycle roots must obey the same isolation rule.
	outside := filepath.Join(root, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "outside-alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	if err := CheckControlledRuntimePaths(filepath.Join(outside, "runtime"), filepath.Join(alias, "consent")); err == nil {
		t.Fatal("mutual parent alias admitted")
	}
	if err := os.WriteFile(handoff, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckRuntimePaths(owner, handoff); err != nil {
		t.Fatal("existing real handoff refused", err)
	}
	link := filepath.Join(root, "consent-link")
	if err := os.Symlink(handoff, link); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckRuntimePaths(owner, link); err == nil {
		t.Fatal("final handoff symlink admitted")
	}
}
