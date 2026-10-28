package daemoncli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestCrashWriterHelper(t *testing.T) {
	name := os.Getenv("HOMELABFS_CRASH_CONFIG")
	if name == "" {
		t.Skip("subprocess crash fixture only")
	}
	c, err := loadConfig(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	l, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	file := send(t, l, fsapi.Command{Operation: fsapi.Create, Parent: l.service.FrontendInfo().Root, Name: "crash-acknowledged", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	send(t, l, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("acknowledged before abrupt exit")})
	if _, err := fmt.Fprintln(os.Stdout, "acknowledged"); err != nil {
		t.Fatal(err)
	}
	// Deliberately omit Close: the parent terminates this disposable process.
	// A live timer keeps the Go test runtime from reporting an idle deadlock.
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	<-timer.C
	t.Fatal("parent failed to terminate crash fixture")
}

func TestAbruptProcessLossReplaysAcknowledgedBytes(t *testing.T) {
	c, configPath := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, bin, "-test.run=^TestCrashWriterHelper$")
	cmd.Env = append(os.Environ(), "HOMELABFS_CRASH_CONFIG="+configPath)
	cmd.Stderr = os.Stderr
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
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	line, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil || line != "acknowledged\n" {
		t.Fatal("child did not acknowledge durable write", line, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	waited = true
	if err == nil {
		t.Fatal("crash fixture unexpectedly exited cleanly")
	}
	l, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.close() })
	file := send(t, l, fsapi.Command{Operation: fsapi.Lookup, Parent: l.service.FrontendInfo().Root, Name: "crash-acknowledged"})
	h := send(t, l, fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	got := send(t, l, fsapi.Command{Operation: fsapi.Read, Handle: h.Handle, Size: 100})
	if string(got.Data) != "acknowledged before abrupt exit" {
		t.Fatal("lost acknowledged bytes", string(got.Data))
	}
}

func TestTornSourceRefusedAndPreserved(t *testing.T) {
	c, _ := fixtureConfig(t)
	v, err := initVolume(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	fc, err := serviceConfig(v)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(c.VolumeDir, "source", filepath.FromSlash(fc.Stream.Directory()))
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatal(err, entries)
	}
	name := filepath.Join(dir, entries[0].Name())
	before, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	torn := append(before, []byte("torn")...)
	if err := os.WriteFile(name, torn, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openLocal(t.Context(), c); err == nil {
		t.Fatal("torn source admitted")
	}
	after, err := os.ReadFile(name)
	if err != nil || string(after) != string(torn) {
		t.Fatal("torn source automatically repaired", err)
	}
}

func TestInvalidMountpointRejectedBeforeRecovery(t *testing.T) {
	for _, scenario := range []string{"nonempty", "permissions", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			c, name := fixtureConfig(t)
			if _, err := initVolume(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "nonempty":
				if err := os.WriteFile(filepath.Join(c.Mountpoint, "keep"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(c.Mountpoint, 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(c.Mountpoint, c.Mountpoint+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(c.Mountpoint+"-saved", c.Mountpoint); err != nil {
					t.Fatal(err)
				}
			}
			var output strings.Builder
			if err := Run(t.Context(), []string{"mount", "--config", name, "--disposable-validation", "--self-client", "--self-test"}, &output, &output); err == nil {
				t.Fatal("invalid mountpoint admitted")
			}
			workspaces, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
			if err != nil || len(workspaces) != 0 {
				t.Fatal("invalid mountpoint modified recovery state", err, workspaces)
			}
		})
	}
}
