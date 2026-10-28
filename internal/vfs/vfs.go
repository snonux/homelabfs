package vfs

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// FS is the local persistence seam. Names are slash-separated, relative paths
// accepted by fs.ValidPath; "." names the root for SyncDir. Open and Unlink
// operate on regular files. Rename supports files and directories; Mkdir
// creates one directory at a time.
// File sync never substitutes for SyncDir on a modified parent directory.
// Implementations are safe for concurrent use. A canceled context prevents an
// operation from starting but cannot interrupt an OS syscall already in flight.
type FS interface {
	Open(context.Context, string, int, fs.FileMode) (File, error)
	Rename(context.Context, string, string) error
	Unlink(context.Context, string) error
	Mkdir(context.Context, string, fs.FileMode) error
	SyncDir(context.Context, string) error
}

// File provides offset-based I/O without a shared seek position. ReadAt follows
// io.ReaderAt's short-read/EOF contract; WriteAt follows io.WriterAt's contract.
// Sync persists contents and retrieval metadata; DataSync may conservatively do
// the same. Close releases the handle and adds no persistence guarantee.
type File interface {
	ReadAt(context.Context, []byte, int64) (int, error)
	WriteAt(context.Context, []byte, int64) (int, error)
	Truncate(context.Context, int64) error
	Sync(context.Context) error
	DataSync(context.Context) error
	Close() error
}

func validName(name string) error {
	if !fs.ValidPath(name) || strings.ContainsRune(name, '\\') || strings.ContainsRune(name, 0) {
		return &fs.PathError{Op: "path", Path: name, Err: fs.ErrInvalid}
	}
	return nil
}

func validOpen(flag int, perm fs.FileMode) error {
	const allowed = os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_EXCL | os.O_TRUNC
	if flag & ^allowed != 0 || flag&3 == 3 || perm & ^fs.FileMode(0o777) != 0 {
		return syscall.EINVAL
	}
	if flag&os.O_TRUNC != 0 && flag&(os.O_WRONLY|os.O_RDWR) == 0 {
		return syscall.EINVAL
	}
	return nil
}
