//go:build linux

package controlledruntime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TestChmodSocketByHandle exercises the pre-fchmodat2 Linux path directly
// (this host's kernel takes the fchmodat2 path): only the recorded socket
// inode changes mode; another identity or a symlink name is refused untouched.
func TestChmodSocketByHandle(t *testing.T) {
	owner := privateOwner(t)
	listener, socket, err := bindLifecycleSocket(owner)
	require(t, err)
	defer func() { _ = listener.Close(); _ = socket.close() }()
	dirfd := int(socket.dir.Fd())
	require(t, os.Chmod(socket.path, 0o644)) // test setup: a widened socket
	require(t, chmodSocketByHandle(dirfd, LifecycleSocketName, socket.dev, socket.ino, 0o600))
	checkSocketMode(t, socket.path, 0o600)
	if err := chmodSocketByHandle(dirfd, LifecycleSocketName, socket.dev, socket.ino+1, 0o644); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("foreign socket identity changed: %v", err)
	}
	checkSocketMode(t, socket.path, 0o600)
	target := filepath.Join(t.TempDir(), "outside")
	require(t, os.WriteFile(target, []byte("outside"), 0o644))
	require(t, os.Chmod(target, 0o644))
	require(t, os.Symlink(target, filepath.Join(owner, "link")))
	if err := chmodSocketByHandle(dirfd, "link", socket.dev, socket.ino, 0o600); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("symlink name followed: %v", err)
	}
	checkOutsideTarget(t, target)
}

func checkSocketMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	require(t, err)
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != mode {
		t.Fatalf("socket mode %v, want %v", info.Mode(), mode)
	}
}
