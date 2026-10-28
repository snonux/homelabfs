//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"errors"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

func status(code fsapi.Errno) fuse.Status {
	switch code {
	case fsapi.OK:
		return fuse.OK
	case fsapi.Invalid:
		return fuse.EINVAL
	case fsapi.NotFound:
		return fuse.ENOENT
	case fsapi.Exists:
		return fuse.Status(syscall.EEXIST)
	case fsapi.Permission:
		return fuse.EACCES
	case fsapi.ReadOnlyFS:
		return fuse.EROFS
	case fsapi.NotDirectory:
		return fuse.ENOTDIR
	case fsapi.IsDirectory:
		return fuse.EISDIR
	case fsapi.NotEmpty:
		return fuse.Status(syscall.ENOTEMPTY)
	case fsapi.CrossDevice:
		return fuse.EXDEV
	case fsapi.Unsupported:
		return fuse.Status(syscall.EOPNOTSUPP)
	case fsapi.NoSpace:
		return fuse.Status(syscall.ENOSPC)
	case fsapi.Quota:
		return fuse.Status(syscall.EDQUOT)
	case fsapi.IO:
		return fuse.EIO
	case fsapi.Unavailable:
		return fuse.Status(syscall.EHOSTDOWN)
	case fsapi.Stale:
		return fuse.Status(syscall.ESTALE)
	case fsapi.Interrupted:
		return fuse.EINTR
	case fsapi.Busy:
		return fuse.EBUSY
	case fsapi.WouldBlock:
		return fuse.EAGAIN
	case fsapi.TooManyLinks:
		return fuse.Status(syscall.EMLINK)
	case fsapi.NameTooLong:
		return fuse.Status(syscall.ENAMETOOLONG)
	default:
		return fuse.EIO
	}
}

func errorStatus(err error) fuse.Status {
	if err == nil {
		return fuse.OK
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fuse.EINTR
	}
	var code fsapi.Errno
	if errors.As(err, &code) {
		return status(code)
	}
	return fuse.EIO
}

func kindMode(kind fsapi.Kind) uint32 {
	switch kind {
	case fsapi.Regular:
		return syscall.S_IFREG
	case fsapi.Directory:
		return syscall.S_IFDIR
	case fsapi.Symlink:
		return syscall.S_IFLNK
	default:
		return 0
	}
}

func attr(v fsapi.Attr, ino uint64) fuse.Attr {
	return fuse.Attr{Ino: ino, Size: uint64(v.Size), Blocks: (uint64(v.Size) + 511) / 512, Atime: uint64(v.Atime.Seconds), Mtime: uint64(v.Mtime.Seconds), Ctime: uint64(v.Ctime.Seconds), Atimensec: v.Atime.Nanoseconds, Mtimensec: v.Mtime.Nanoseconds, Ctimensec: v.Ctime.Nanoseconds, Mode: kindMode(v.Kind) | uint32(v.Mode), Nlink: uint32(v.Nlink), Owner: fuse.Owner{Uid: v.UID, Gid: v.GID}, Blksize: 4096}
}

func openFlags(native uint32, create bool) (fsapi.OpenFlags, fuse.Status) {
	f := fsapi.OpenFlags{Create: create, Exclusive: create && native&syscall.O_EXCL != 0, Append: native&syscall.O_APPEND != 0, Truncate: native&syscall.O_TRUNC != 0}
	switch native & syscall.O_ACCMODE {
	case syscall.O_RDONLY:
		f.Access = fsapi.ReadOnly
	case syscall.O_WRONLY:
		f.Access = fsapi.WriteOnly
	case syscall.O_RDWR:
		f.Access = fsapi.ReadWrite
	default:
		return f, fuse.EINVAL
	}
	if native&syscall.O_SYNC == syscall.O_SYNC {
		f.Sync = fsapi.IntegrityWrite
	} else if native&dataSyncFlag != 0 {
		f.Sync = fsapi.DataWrite
	}
	if err := f.Validate(); err != nil {
		return f, errorStatus(err)
	}
	return f, fuse.OK
}
