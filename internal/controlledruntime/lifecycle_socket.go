//go:build linux || freebsd

package controlledruntime

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// lifecycleSocketMode is the only permission the bound control socket keeps.
const lifecycleSocketMode = 0o600

// lifecycleSocket is the bound control socket name inside a held handle of the
// private owner directory. Every inspection, permission change and removal
// after bind is relative to that handle and never follows a symlink, so a name
// replaced after bind is refused and preserved rather than chmodded or removed.
// dev/ino identify the socket inode this listener bound; both are zero until
// that identity was confirmed.
type lifecycleSocket struct {
	dir      *os.File
	path     string
	dev, ino uint64
}

// bindLifecycleSocket opens the private owner directory, refuses (and
// preserves) any existing control.sock name, binds the listener and restricts
// the new socket to mode 0600. Between bind and that change the socket has the
// process umask's mode, but it is reachable only through the 0700 owner root,
// so no other account can connect in that window. Errors close everything
// this call opened and remove only this listener's own unchanged name.
func bindLifecycleSocket(owner string) (*net.UnixListener, *lifecycleSocket, error) {
	dir, err := openLifecycleDirectory(owner)
	if err != nil {
		return nil, nil, err
	}
	s := &lifecycleSocket{dir: dir, path: filepath.Join(owner, LifecycleSocketName)}
	listener, err := s.bind()
	if err != nil {
		return nil, nil, errors.Join(err, dir.Close())
	}
	return listener, s, nil
}

// openLifecycleDirectory holds the owner root without following a final
// symlink and checks it is this account's private directory.
func openLifecycleDirectory(owner string) (*os.File, error) {
	fd, err := unix.Open(owner, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("lifecycle owner directory: %w", err)
	}
	dir := os.NewFile(uintptr(fd), owner)
	if err = checkOwned(dir, true); err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("lifecycle owner directory not private: %w", err)
	}
	return dir, nil
}

func (s *lifecycleSocket) bind() (*net.UnixListener, error) {
	if _, err := s.stat(); err == nil {
		return nil, fmt.Errorf("existing lifecycle socket preserved: %w", fsapi.Permission)
	} else if !errors.Is(err, unix.ENOENT) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: s.path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	// Removal is explicit and identity-checked (close), never by name only.
	listener.SetUnlinkOnClose(false)
	if err = s.protect(); err != nil {
		return nil, errors.Join(err, listener.Close(), s.remove())
	}
	return listener, nil
}

// protect confirms the bound name is this account's socket, records its
// identity, sets mode 0600 without following a symlink and re-checks that the
// same inode now has exactly that mode. A name swapped in between is refused.
func (s *lifecycleSocket) protect() error {
	st, err := s.stat()
	if err != nil {
		return fmt.Errorf("bound lifecycle socket: %w", err)
	}
	if uint32(st.Mode)&unix.S_IFMT != unix.S_IFSOCK || st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("bound lifecycle socket replaced: %w", fsapi.Permission)
	}
	s.dev, s.ino = uint64(st.Dev), uint64(st.Ino)
	if err = chmodSocketNoFollow(int(s.dir.Fd()), LifecycleSocketName, s.dev, s.ino, lifecycleSocketMode); err != nil {
		return fmt.Errorf("lifecycle socket mode: %w", err)
	}
	if st, err = s.stat(); err != nil {
		return fmt.Errorf("bound lifecycle socket: %w", err)
	}
	if !s.same(st) || uint32(st.Mode)&0o7777 != lifecycleSocketMode {
		return fmt.Errorf("lifecycle socket not exactly mode 0600: %w", fsapi.Permission)
	}
	return nil
}

// close removes only this listener's unchanged socket name and releases the
// directory handle. A replaced or inaccessible name is preserved and reported.
func (s *lifecycleSocket) close() error {
	return errors.Join(s.remove(), s.dir.Close())
}

func (s *lifecycleSocket) remove() error {
	st, err := s.stat()
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil
	case err != nil:
		return fmt.Errorf("lifecycle socket inaccessible; preserved: %w", err)
	case !s.same(st):
		return fmt.Errorf("lifecycle socket replaced; preserved: %w", fsapi.Permission)
	}
	return unix.Unlinkat(int(s.dir.Fd()), LifecycleSocketName, 0)
}

// stat inspects the socket name itself (never a symlink target).
func (s *lifecycleSocket) stat() (unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(int(s.dir.Fd()), LifecycleSocketName, &st, unix.AT_SYMLINK_NOFOLLOW)
	return st, err
}

// same reports whether st is the confirmed socket this listener bound.
func (s *lifecycleSocket) same(st unix.Stat_t) bool {
	return s.ino != 0 && uint32(st.Mode)&unix.S_IFMT == unix.S_IFSOCK && uint64(st.Dev) == s.dev && uint64(st.Ino) == s.ino
}
