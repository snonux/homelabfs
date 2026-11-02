package daemoncli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// These tests exercise local resource ownership with an injected unmount
// callback. They do not exercise FUSE mounting, kernel drain, or FreeBSD's
// unmount lifetime; those contracts still require native lab verification.
func lifetimeFixture(t *testing.T) (Config, *mountLifetime) {
	t.Helper()
	// UNIX socket paths must stay short even when the caller's TMPDIR is long.
	base, err := os.MkdirTemp("/tmp", "hfl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	node, err := initNode(t.Context(), filepath.Join(base, "c"))
	if err != nil {
		t.Fatal(err)
	}
	volume, err := freshID()
	if err != nil {
		t.Fatal(err)
	}
	c := Config{FormatVersion: 1, NodeID: node.NodeID, VolumeID: volume.String(), ControlDir: filepath.Join(base, "c"), VolumeDir: filepath.Join(base, "v"), Mountpoint: filepath.Join(base, "m")}
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	local, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := local.close(); err != nil {
			t.Error(err)
		}
	})
	server, err := startAdmin(t.Context(), c, local)
	if err != nil {
		t.Fatal(err)
	}
	lifetime := &mountLifetime{local: local, admin: server, retained: true}
	t.Cleanup(func() {
		if lifetime.admin != nil {
			if err := lifetime.admin.close(); err != nil {
				t.Error(err)
			}
		}
	})
	return c, lifetime
}

func TestMountLifetimeRetainsUncertainResourcesUntilSuccessfulCallback(t *testing.T) {
	c, lifetime := lifetimeFixture(t)
	lock := lifetime.local.lock
	if err := lifetime.close(); err != nil {
		t.Fatal(err)
	}
	response, err := admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "status"})
	if err != nil || response.Status == nil || response.Status.Volumes[0].State != "SERVING" {
		t.Fatal("uncertain close lost admin/service", response, err)
	}
	failure := errors.New("normal unmount refused")
	calls := 0
	if err := lifetime.unmount(func() error { calls++; return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := lifetime.close(); err != nil {
		t.Fatal(err)
	}
	if lifetime.local.lock != lock {
		t.Fatal("failed callback released lock")
	}
	response, err = admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "status"})
	if err != nil || response.Status == nil || response.Status.Volumes[0].WaitReason != "normal-unmount-failed" {
		t.Fatal(response, err)
	}
	request := fsapi.NewRequest(fsapi.Cred{})
	if err := lifetime.local.service.Dispatch(t.Context(), request, fsapi.Command{Operation: fsapi.Statfs}); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("uncertainty did not gate service", err)
	}
	if _, err := openLocal(t.Context(), c); err == nil {
		t.Fatal("failed callback relinquished ownership")
	}
	if err := lifetime.unmount(func() error {
		calls++
		if lifetime.local.service == nil {
			t.Fatal("service closed before callback")
		}
		if _, err := lock.Stat(); err != nil {
			t.Fatal("lock closed before callback", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("callback count", calls)
	}
	if err := lifetime.close(); err != nil {
		t.Fatal(err)
	}
	lifetime.admin = nil // The lifetime already closed the audit handle.
	if _, err := os.Stat(socketPath(c)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("socket not removed", err)
	}
	if _, err := lock.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("lock still open", err)
	}
	reopened, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal("successful callback did not release ownership", err)
	}
	if err := reopened.close(); err != nil {
		t.Fatal(err)
	}
}

func TestMountLifetimeBeforeMountClosesLocalWithoutAdmin(t *testing.T) {
	c, _ := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	local, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := local.close(); err != nil {
			t.Error(err)
		}
	}()
	lifetime := &mountLifetime{local: local}
	if err := lifetime.close(); err != nil {
		t.Fatal(err)
	}
	if err := lifetime.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal("pre-mount cleanup retained lock", err)
	}
	if err := reopened.close(); err != nil {
		t.Fatal(err)
	}
}
