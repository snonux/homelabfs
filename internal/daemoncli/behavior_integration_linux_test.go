//go:build linux

package daemoncli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/behavior"
	"golang.org/x/sys/unix"
)

// TestLinuxBehaviorCLIIntegration is an explicit root-only runtime gate for the
// real new command and separately pinned helper. Never enable without approval.
func TestLinuxBehaviorCLIIntegration(t *testing.T) {
	if os.Getenv("HOMELABFS_BEHAVIOR_CLI_INTEGRATION") != "1" {
		t.Skip("requires explicit approved root behavior CLI run")
	}
	if os.Geteuid() != 0 {
		t.Fatal("approved behavior CLI counterpart requires root")
	}
	base, err := os.MkdirTemp("", "hfs-behavior-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture and evidence: %s", base)
	success := false
	t.Cleanup(func() {
		if success && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved fixture/evidence: %s", base)
		}
	})
	if err := os.Chmod(base, 0711); err != nil {
		t.Fatal(err)
	}
	binary, helper := filepath.Join(base, "homelabfs"), filepath.Join(base, "hfs-behavior")
	for _, entry := range []struct{ path, pkg string }{{binary, "./cmd/homelabfs"}, {helper, "./cmd/hfs-behavior"}} {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		command := exec.CommandContext(ctx, "go", "build", "-o", entry.path, entry.pkg)
		command.Dir = "../.."
		output, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("build %s: %v\n%s", entry.pkg, err, output)
		}
	}
	data, err := os.ReadFile(helper)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	digest := hex.EncodeToString(hash[:])
	for _, role := range []string{"root", "unprivileged"} {
		root := filepath.Join(base, role)
		if err := os.Mkdir(root, 0711); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"data", "journal", "control", "mount", "evidence"} {
			if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
				t.Fatal(err)
			}
		}
		var previous readyMessage
		for i, profile := range []string{"hfs-direct", "hfs-buffered"} {
			prefix := filepath.Join(root, "evidence", profile)
			args := []string{"behavior", "--disposable-validation", "--data-root", filepath.Join(root, "data"), "--journal-root", filepath.Join(root, "journal"), "--control-root", filepath.Join(root, "control"), "--mountpoint", filepath.Join(root, "mount"), "--behavior-binary", helper, "--behavior-sha256", digest, "--role", role, "--profile", profile, "--json", prefix + ".json", "--junit", prefix + ".xml"}
			if i == 0 {
				args = append(args, "--initialize")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 85*time.Second)
			command := exec.CommandContext(ctx, binary, args...)
			var output, logs bytes.Buffer
			command.Stdout, command.Stderr = &output, &logs
			runErr := command.Run()
			cancel()
			if err := os.WriteFile(prefix+".stdout", output.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(prefix+".stderr", logs.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if runErr != nil {
				t.Fatalf("%s/%s: %v\n%s", role, profile, runErr, logs.String())
			}
			var ready behaviorReady
			if err := json.Unmarshal(output.Bytes(), &ready); err != nil {
				t.Fatal(err, output.String())
			}
			if ready.Role != role || ready.Profile != profile || ready.DaemonUID != 0 || ready.ClientUID != roleCredentials(role).Uid || ready.ClientGID != roleCredentials(role).Gid || !slices.Equal(ready.ClientGroups, roleCredentials(role).Groups) || ready.NormalPublication || ready.BehaviorSHA256 != digest {
				t.Fatal("wrong controlled readiness", output.String())
			}
			if i == 0 {
				previous = ready.readyMessage
			} else if ready.VolumeID != previous.VolumeID || ready.Generation == previous.Generation || ready.Session == previous.Session {
				t.Fatal("reopen did not preserve volume with fresh generation/session", previous, ready)
			}
			var stat unix.Statfs_t
			if err := unix.Statfs(filepath.Join(root, "mount"), &stat); err != nil {
				t.Fatal(err)
			}
			if stat.Type == 0x65735546 {
				t.Fatal("command exited with a remaining FUSE mount")
			}
			reportData, err := os.ReadFile(prefix + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var report behavior.Report
			if err := json.Unmarshal(reportData, &report); err != nil {
				t.Fatal(err)
			}
			if report.Failed() || report.Fixture["role"] != role || report.Identity != "homelabfs-validation-v1\n"+ready.VolumeID+"\n"+ready.Generation+"\n"+ready.Session+"\n" {
				t.Fatal("incomplete controlled report", string(reportData))
			}
			required := []string{"create-exclusive-rename-replace-hardlink-unlink-open-truncate", "umask-chmod-chown-setgid", "sync-open-fsync-fdatasync-directory-sync", "process-fcntl-flock-contention-exit", "blocked-lock-process-interruption", "normal-unmount", "mount-loss-no-fallback"}
			if profile == "hfs-direct" {
				required = append(required, "concurrent-append")
			} else {
				required = append(required, "mmap-msync-readback")
			}
			if role == "root" && profile == "hfs-buffered" {
				required = append(required, "multiuser-groups-sticky-permission-cache")
			}
			for _, name := range required {
				passed := false
				for _, result := range report.Results {
					if result.Name == name && result.Status == "pass" {
						passed = true
					}
				}
				if !passed {
					t.Fatal("required real syscall case did not pass", role, profile, name, string(reportData))
				}
			}
			// Parse JUnit independently; JSON success alone cannot stand for both files.
			xmlData, err := os.ReadFile(prefix + ".xml")
			if err != nil {
				t.Fatal(err)
			}
			var suite struct {
				XMLName  xml.Name
				Failures int `xml:"failures,attr"`
				Tests    int `xml:"tests,attr"`
			}
			if err := xml.Unmarshal(xmlData, &suite); err != nil {
				t.Fatal(err)
			}
			if suite.XMLName.Local != "testsuite" || suite.Failures != 0 || suite.Tests != len(report.Results) {
				t.Fatal("incomplete JUnit", string(xmlData))
			}
		}
	}
	success = true
}
