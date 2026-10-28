//go:build linux || freebsd

package vfs

import (
	"io/fs"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

func physicalInfo(info fs.FileInfo) (Info, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Info{}, fs.ErrInvalid
	}
	return Info{Mode: info.Mode(), Size: info.Size(), Links: uint64(stat.Nlink), UID: stat.Uid, GID: stat.Gid,
		Identity: Identity{Device: uint64(stat.Dev), File: uint64(stat.Ino)}}, nil
}

// statDirEntry is anchored to the retained directory descriptor, independent of
// its original pathname or os.DirEntry.Info behavior in a Go patch release.
func statDirEntry(dir *os.File, name string) (Info, error) {
	var stat unix.Stat_t
	for {
		err := unix.Fstatat(int(dir.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		runtime.KeepAlive(dir)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return Info{}, pathError("fstatat", name, err)
		}
		break
	}
	mode := fs.FileMode(stat.Mode & 0o777)
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		mode |= fs.ModeDir
	case unix.S_IFREG:
	default:
		return Info{}, pathError("fstatat", name, syscall.EINVAL)
	}
	if stat.Mode&unix.S_ISUID != 0 {
		mode |= fs.ModeSetuid
	}
	if stat.Mode&unix.S_ISGID != 0 {
		mode |= fs.ModeSetgid
	}
	if stat.Mode&unix.S_ISVTX != 0 {
		mode |= fs.ModeSticky
	}
	return Info{Mode: mode, Size: stat.Size, Links: uint64(stat.Nlink), UID: stat.Uid, GID: stat.Gid,
		Identity: Identity{Device: uint64(stat.Dev), File: uint64(stat.Ino)}}, nil
}
