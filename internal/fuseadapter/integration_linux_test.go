//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// ownValidationCredentials is ONLY safe for this controlled test: all requests
// must belong to our still-running process, whose credentials never change.
// Linux FUSE headers can identify a nonleader task. /proc/<tid>/status is used
// to assert Tgid and this thread's filesystem IDs/groups, not to implement an
// arbitrary request-bound workload identity provider (the race remains a gate).
func ownValidationCredentials(ctx context.Context, c Caller) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", c.Thread))
	if err != nil {
		return Identity{}, err
	}
	var tgid, uid, gid uint32
	var groups []uint32
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		parse := func(s string) (uint32, error) { n, e := strconv.ParseUint(s, 10, 32); return uint32(n), e }
		switch f[0] {
		case "Tgid:":
			tgid, err = parse(f[1])
		case "Uid:":
			if len(f) != 5 {
				return Identity{}, fsapi.Permission
			}
			uid, err = parse(f[4])
		case "Gid:":
			if len(f) != 5 {
				return Identity{}, fsapi.Permission
			}
			gid, err = parse(f[4])
		case "Groups:":
			for _, raw := range f[1:] {
				n, e := parse(raw)
				if e != nil {
					return Identity{}, e
				}
				groups = append(groups, n)
			}
		}
		if err != nil {
			return Identity{}, err
		}
	}
	if tgid != uint32(os.Getpid()) || uid != c.UID || gid != c.GID {
		return Identity{}, fsapi.Permission
	}
	return Identity{Credentials: fsapi.Cred{UID: uid, GID: gid, Groups: groups, PID: c.Thread}, Process: 1, ProcessID: tgid}, nil
}

func TestLinuxIntegration(t *testing.T) {
	if os.Getenv("HOMELABFS_FUSE_INTEGRATION") != "1" {
		t.Skip("requires explicit HOMELABFS_FUSE_INTEGRATION=1 run authorization")
	}
	device, err := os.OpenFile("/dev/fuse", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("/dev/fuse unavailable: %v", err)
	}
	if err = device.Close(); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("", "homelabfs-fuse-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("run-owned resources: %s", base)
	// Register deletion first: roots/service/unmount must close before removal.
	clean := false
	t.Cleanup(func() {
		if clean && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved resources: %s", base)
		}
	})
	mountpoint := filepath.Join(base, "mount")
	if err := os.Mkdir(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	a, service := serviceFixture(t, base, credentialFunc(ownValidationCredentials))
	if err := CheckMount(mountpoint, a.Identity()); err == nil {
		t.Fatal("accepted unmounted fallback")
	}
	m, err := a.Mount(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Unmount(); err != nil {
			t.Errorf("normal unmount (resources retained): %v", err)
			return
		}
		clean = true
		if err := CheckMount(mountpoint, a.Identity()); err == nil {
			clean = false
			t.Error("accepted fallback after unmount")
		}
	})
	if err := CheckMount(mountpoint, a.Identity()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AttachFrontend(); !errors.Is(err, fsapi.Busy) {
		t.Fatalf("duplicate active publication: %v", err)
	}
	wrong := a.Identity()
	wrong.Volume = testID(99)
	if err := CheckMount(mountpoint, wrong); err == nil {
		t.Fatal("accepted wrong volume")
	}
	file := filepath.Join(mountpoint, "original")
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if f != nil {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	data := []byte("synthetic HomelabFS FUSE bytes\n")
	if n, err := f.Write(data); err != nil || n != len(data) {
		t.Fatal(n, err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(data))
	if n, err := f.ReadAt(got, 0); err != nil || n != len(data) || string(got) != string(data) {
		t.Fatal(n, err, string(got))
	}
	if err := f.Truncate(4096); err != nil {
		t.Fatalf("ordinary ftruncate extend: %v", err)
	}
	var hole [1]byte
	if n, err := f.ReadAt(hole[:], 4095); err != nil || n != 1 || hole[0] != 0 {
		t.Fatal("sparse extension", n, err, hole)
	}
	if err := f.Truncate(int64(len(data))); err != nil {
		t.Fatalf("ordinary ftruncate shrink: %v", err)
	}
	stamp := time.Unix(1700000000, 123456789)
	if err := os.Chtimes(file, stamp, stamp); err != nil {
		t.Fatalf("explicit timestamps: %v", err)
	}
	st, err := f.Stat()
	if err != nil || !st.ModTime().Equal(stamp) || st.Size() != int64(len(data)) {
		t.Fatal("timestamp/size", st, err)
	}
	other, err := os.OpenFile(file, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = other.Close()
		t.Fatal(err)
	}
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != syscall.EWOULDBLOCK {
		_ = other.Close()
		t.Fatalf("independent flock description conflict: %v", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		_ = other.Close()
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if mapping, err := unix.Mmap(int(f.Fd()), 0, 4096, unix.PROT_READ, unix.MAP_SHARED); err == nil {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
		t.Fatal("restricted direct-I/O profile unexpectedly admitted mmap")
	}
	alias := filepath.Join(mountpoint, "alias")
	if err := os.Link(file, alias); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(mountpoint, "renamed")
	if err := os.Rename(file, renamed); err != nil {
		t.Fatal(err)
	}
	if err := f.Chmod(0400); err != nil {
		t.Fatal(err)
	}
	// Actual kernel permission denial, including for a root-run test: execute
	// requires an execute bit even for uid 0. No credential switching is used.
	if err := unix.Access(renamed, unix.X_OK); err != syscall.EACCES {
		t.Fatalf("execute permission denial: %v", err)
	}
	if err := os.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if n, err := f.ReadAt(got, 0); err != nil || n != len(data) || string(got) != string(data) {
		t.Fatal("open-unlink", n, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f = nil
	dir := filepath.Join(mountpoint, "dir")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	d, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(mountpoint)
	if err != nil || len(entries) != 1 || entries[0].Name() != "dir" {
		t.Fatal(entries, err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := m.Unmount(); err != nil {
		t.Fatal(err)
	}
	if err := CheckMount(mountpoint, a.Identity()); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("fallback identity after unmount: %v", err)
	}
	detach, err := service.AttachFrontend()
	if err != nil {
		t.Fatalf("publication lease retained after clean unmount: %v", err)
	}
	detach()
}
