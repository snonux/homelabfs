package daemoncli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fsapi"
)

func adminFixture(t *testing.T, hooks bool) (Config, string, *localService, *adminService) {
	t.Helper()
	base, err := os.MkdirTemp("", "hfc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	node, err := initNode(t.Context(), filepath.Join(base, "control"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := freshID()
	if err != nil {
		t.Fatal(err)
	}
	c := Config{FormatVersion: 1, NodeID: node.NodeID, VolumeID: id.String(), ControlDir: filepath.Join(base, "control"), VolumeDir: filepath.Join(base, "volume"), Mountpoint: filepath.Join(base, "mount"), EnableTestHooks: hooks}
	if err := os.Mkdir(c.Mountpoint, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(base, "config.json")
	if err := writeNewJSON(name, c); err != nil {
		t.Fatal(err)
	}
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
	t.Cleanup(func() {
		if err := server.close(); err != nil {
			t.Error(err)
		}
	})
	return c, name, local, server
}

func TestAdminStatusHelper(t *testing.T) {
	name := os.Getenv("HOMELABFS_ADMIN_STATUS_CONFIG")
	if name == "" {
		t.Skip("subprocess fixture only")
	}
	if err := Run(t.Context(), []string{"status", "--config", name}, os.Stdout, os.Stderr); err != nil {
		t.Fatal(err)
	}
}
func TestAdminStatus(t *testing.T) {
	c, name, local, _ := adminFixture(t, false)
	file := send(t, local, fsapi.Command{Operation: fsapi.Create, Parent: local.service.FrontendInfo().Root, Name: "status-file", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}})
	send(t, local, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("synthetic")})
	// Status must not inspect the backing tree or data plane. Even nonexistent
	// paths remain lexically checked but never opened by the administrative client.
	if err := os.Rename(c.Mountpoint, c.Mountpoint+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("unavailable-data-plane", c.Mountpoint); err != nil {
		t.Fatal(err)
	}
	c.VolumeDir = filepath.Join(filepath.Dir(c.VolumeDir), "never-open-this-volume")
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(t.Context(), []string{"status", "--config", name}, &out, &out); err != nil {
		t.Fatal(err, out.String())
	}
	var status admin.Status
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if len(status.Volumes) != 1 {
		t.Fatal(status)
	}
	volume := status.Volumes[0]
	if volume.State != "SERVING" || volume.WaitReason != "" || volume.Profile != validationName || volume.NormalPublication || volume.SourcePosition == nil || volume.AppliedPosition == nil || volume.BackingSlots == 0 || volume.SyncPosition != nil || volume.AsyncPosition != nil || status.TestHooksEnabled {
		t.Fatal(status)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bin, "-test.run=^TestAdminStatusHelper$")
	command.Env = append(os.Environ(), "HOMELABFS_ADMIN_STATUS_CONFIG="+name)
	output, err := command.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte(`"normal_publication":false`)) {
		t.Fatal("separate admin client failed", err, string(output))
	}
}

func TestAdminFaultHook(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default-disabled", true: "explicit-enabled"}[enabled], func(t *testing.T) {
			c, _, local, _ := adminFixture(t, enabled)
			_, err := admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "test-fault", Fault: "journal-before-sync", Enabled: true})
			if !enabled {
				if err == nil || local.faults.journalBeforeSync.Load() {
					t.Fatal("default hook enabled", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			request := fsapi.NewRequest(fsapi.Cred{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())})
			if err := local.service.Dispatch(t.Context(), request, fsapi.Command{Mount: local.service.FrontendInfo().Volume, Operation: fsapi.Mkdir, Parent: local.service.FrontendInfo().Root, Name: "injected", Mode: 0o700}); err != nil {
				t.Fatal(err)
			}
			result, err := request.Completion().Wait(t.Context())
			if err != nil || result.Code == fsapi.OK {
				t.Fatal("fault did not affect journal", result, err)
			}
			response, err := admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "status"})
			if err != nil || response.Status == nil || response.Status.Volumes[0].State != "BLOCKED" || response.Status.Volumes[0].WaitReason == "" {
				t.Fatal("missing blocked status", response, err)
			}
			if _, err := admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "test-fault", Fault: "journal-before-sync", Enabled: false}); err != nil {
				t.Fatal(err)
			}
			if local.faults.journalBeforeSync.Load() {
				t.Fatal("false toggle omitted")
			}
			if _, err := admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "fence-host"}); err == nil {
				t.Fatal("dangerous placeholder implemented")
			}
			raw, err := os.ReadFile(filepath.Join(c.ControlDir, "admin-"+c.VolumeID+".audit.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			for _, text := range []string{`"outcome":"requested"`, `"outcome":"applied"`, `"outcome":"denied"`, `"operation":"fence-host"`, `"uid":`, `"generation":"` + local.service.FrontendInfo().Generation.String() + `"`} {
				if !strings.Contains(string(raw), text) {
					t.Fatal("audit missing evidence", text, string(raw))
				}
			}
		})
	}
}

func TestAdminStatusConfigStillStrict(t *testing.T) {
	c, name, _, _ := adminFixture(t, false)
	for _, scenario := range []string{"relative-mount", "overlap", "unknown-hook-key", "null-hook", "case-hook", "foreign-node", "unsafe-control"} {
		t.Run(scenario, func(t *testing.T) {
			changed := c
			switch scenario {
			case "relative-mount":
				changed.Mountpoint = "relative"
			case "overlap":
				changed.Mountpoint = filepath.Join(c.ControlDir, "child")
			case "foreign-node":
				changed.NodeID = "11111111-1111-4111-8111-111111111111"
			case "unsafe-control":
				changed.ControlDir = c.Mountpoint
			}
			raw, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "unknown-hook-key":
				raw = append([]byte(`{"test_hooks":true,`), raw[1:]...)
			case "null-hook":
				raw = append([]byte(`{"enable_test_hooks":null,`), raw[1:]...)
			case "case-hook":
				raw = append([]byte(`{"ENABLE_TEST_HOOKS":true,`), raw[1:]...)
			}
			if err := os.WriteFile(name, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Run(t.Context(), []string{"status", "--config", name}, &out, &out); err == nil {
				t.Fatal("status accepted invalid admission")
			}
		})
	}
}

func TestFailedUnmountPreservesOwnership(t *testing.T) {
	c, _, local, server := adminFixture(t, false)
	lifetime := &mountLifetime{local: local, admin: server, retained: true}
	if err := lifetime.unmount(func() error { return errors.New("synthetic normal-unmount refusal") }); err == nil {
		t.Fatal("expected unmount failure")
	}
	if err := lifetime.close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openLocal(t.Context(), c); err == nil {
		t.Fatal("failed unmount released source ownership")
	}
	response, err := admin.Call(t.Context(), socketPath(c), admin.Request{Version: 1, Operation: "status"})
	if err != nil || response.Status == nil || response.Status.Volumes[0].State != "BLOCKED" || response.Status.Volumes[0].WaitReason != "normal-unmount-failed" {
		t.Fatal("failed resources/status not retained", response, err)
	}
	request := fsapi.NewRequest(fsapi.Cred{})
	if err := local.service.Dispatch(t.Context(), request, fsapi.Command{Operation: fsapi.Statfs}); err != fsapi.Unavailable {
		t.Fatal("failed unmount kept admission open", err)
	}
}
