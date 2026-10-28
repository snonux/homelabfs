//go:build linux

package daemoncli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"golang.org/x/sys/unix"
)

func TestLinuxCLIIntegration(t *testing.T) {
	if os.Getenv("HOMELABFS_CLI_INTEGRATION") != "1" {
		t.Skip("requires explicit HOMELABFS_CLI_INTEGRATION=1 authorization")
	}
	device, err := os.OpenFile("/dev/fuse", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("/dev/fuse unavailable: %v", err)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	base, err := os.MkdirTemp("", "hfs-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("run-owned evidence: %s", base)
	clean := false
	t.Cleanup(func() {
		if clean && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved evidence: %s", base)
		}
	})
	bin := filepath.Join(base, "homelabfs")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "../../cmd/homelabfs")
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, bin, args...)
		var logs bytes.Buffer
		cmd.Stderr = &logs
		out, err := cmd.Output()
		if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("command-%d.log", time.Now().UnixNano())), append(logs.Bytes(), out...), 0o600); err != nil {
			t.Fatal(err)
		}
		if err != nil {
			t.Fatal(args, err, logs.String(), string(out))
		}
		return out
	}
	var n nodeIdentity
	if err := json.Unmarshal(run("node-init", "--control", filepath.Join(base, "control")), &n); err != nil {
		t.Fatal(err)
	}
	volume, err := freshID()
	if err != nil {
		t.Fatal(err)
	}
	c := Config{FormatVersion: 1, NodeID: n.NodeID, VolumeID: volume.String(), ControlDir: filepath.Join(base, "control"), VolumeDir: filepath.Join(base, "volume"), Mountpoint: filepath.Join(base, "mount")}
	if err := os.Mkdir(c.Mountpoint, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(base, "config.json")
	if err := writeNewJSON(configPath, c); err != nil {
		t.Fatal(err)
	}
	run("volume-init", "--config", configPath, "--disposable-validation")
	run("inspect", "--config", configPath)
	run("mount", "--config", configPath, "--disposable-validation", "--self-client", "--self-test")
	// Reopen the real CLI and signal it after its mandatory exact mount check.
	cmd := exec.CommandContext(ctx, bin, "serve", "--config", configPath, "--disposable-validation", "--self-client")
	logFile, err := os.OpenFile(filepath.Join(base, "foreground.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logFile.Close() }()
	cmd.Stderr = logFile
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil || !strings.Contains(line, `"normal_publication":false`) {
		t.Fatal("missing checked validation readiness", err, line)
	}
	statusRaw := run("status", "--config", configPath)
	var status admin.Status
	if err := json.Unmarshal(statusRaw, &status); err != nil || len(status.Volumes) != 1 || status.Volumes[0].State != "SERVING" || status.Volumes[0].NormalPublication {
		t.Fatal("admin status through active self-client mount", err, string(statusRaw))
	}
	// Even this same-account parent is outside the closed fixture. It cannot
	// create a file through the mount by borrowing daemon credentials.
	if err := os.WriteFile(filepath.Join(c.Mountpoint, "foreign-process"), []byte("reject"), 0o600); err == nil {
		t.Fatal("admitted external process")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Fatal("signal shutdown failed", err)
	}
	entries, err := os.ReadDir(c.Mountpoint)
	if err != nil || len(entries) != 0 {
		t.Fatal("normal shutdown left mount exposed", err, entries)
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(c.Mountpoint, &stat); err != nil || stat.Type == 0x65735546 {
		t.Fatal("shutdown did not restore underlying non-FUSE mountpoint", err, stat.Type)
	}
	run("mount", "--config", configPath, "--disposable-validation", "--self-client", "--self-test")
	workspaces, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
	if err != nil || len(workspaces) != 3 {
		t.Fatal("fresh retained replay generations", err, workspaces)
	}
	clean = true
}
