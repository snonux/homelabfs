package vfs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// OS confines local operations to an existing root using os.Root. The root
// should be a private directory without externally managed symlinks or mounts.
// Close it after all consumers have stopped using the filesystem. An OS must
// not be copied after first use.
type OS struct {
	root *os.Root
	// Keep type checks and subsequent syscalls atomic with respect to all
	// namespace operations through this backend, including parent resolution.
	namespaceMu sync.Mutex
}

type osFile struct{ file *os.File }

var (
	_ FS   = (*OS)(nil)
	_ File = (*osFile)(nil)
)

// NewOS opens an existing root directory; it never creates or clears it.
func NewOS(ctx context.Context, dir string) (*OS, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir = filepath.Clean(dir)
	before, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect I/O root: %w", err)
	}
	if !before.IsDir() {
		return nil, pathError("root", dir, syscall.ENOTDIR)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open I/O root: %w", err)
	}
	rootFile, err := r.Open(".")
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	defer func() { _ = rootFile.Close() }()
	after, err := rootFile.Stat()
	if err != nil || !os.SameFile(before, after) {
		_ = r.Close()
		if err != nil {
			return nil, err
		}
		return nil, pathError("root changed", dir, fs.ErrInvalid)
	}
	return &OS{root: r}, nil
}

// Open opens or creates a regular file. Supported flags are O_RDONLY,
// O_WRONLY, O_RDWR, O_CREATE, O_EXCL and O_TRUNC; append and implicit sync flags
// are excluded because this seam uses explicit offsets and barriers.
func (s *OS) Open(ctx context.Context, name string, flag int, perm fs.FileMode) (File, error) {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkPath(name, true); err != nil {
		return nil, err
	}
	if err := validOpen(flag, perm); err != nil {
		return nil, err
	}
	// Inspect before opening so a FIFO/device in an incorrectly populated root
	// cannot block or be truncated. The root is private, not an untrusted tree.
	if info, err := s.root.Stat(name); err == nil {
		if !info.Mode().IsRegular() {
			return nil, &fs.PathError{Op: "open", Path: name, Err: syscall.EINVAL}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := s.root.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &osFile{file: f}, nil
}

// Rename atomically replaces a file or directory name. Sync both modified
// parents for durability when the parents differ.
func (s *OS) Rename(ctx context.Context, oldname, newname string) error {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkPath(oldname, false); err != nil {
		return err
	}
	if err := s.checkPath(newname, true); err != nil {
		return err
	}
	if oldname == "." || newname == "." {
		return pathError("rename", oldname, fs.ErrInvalid)
	}
	return renameRoot(s.root, oldname, newname)
}

// Unlink removes a regular-file name without invalidating existing handles.
func (s *OS) Unlink(ctx context.Context, name string) error {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkPath(name, false); err != nil {
		return err
	}
	if err := s.regular(name); err != nil {
		return err
	}
	return s.root.Remove(name)
}

// Mkdir creates a directory; sync its parent to persist its name.
func (s *OS) Mkdir(ctx context.Context, name string, perm fs.FileMode) error {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkPath(name, true); err != nil {
		return err
	}
	if perm & ^fs.FileMode(0o777) != 0 {
		return syscall.EINVAL
	}
	return s.root.Mkdir(name, perm)
}

// SyncDir syncs a directory descriptor. Unsupported kernel/filesystem errors
// propagate to the caller; an unavailable barrier is never reported as success.
func (s *OS) SyncDir(ctx context.Context, name string) error {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkPath(name, false); err != nil {
		return err
	}
	f, err := s.root.Open(name)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &fs.PathError{Op: "syncdir", Path: name, Err: syscall.ENOTDIR}
	}
	return f.Sync()
}

// Close releases the root descriptor. Existing file handles remain independent.
func (s *OS) Close() error {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	return s.root.Close()
}

func (f *osFile) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.ReadAt(p, off)
}

func (f *osFile) WriteAt(ctx context.Context, p []byte, off int64) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.WriteAt(p, off)
}

func (f *osFile) Truncate(ctx context.Context, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.file.Truncate(size)
}

func (f *osFile) Sync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.file.Sync()
}

func (f *osFile) DataSync(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return syncData(f.file)
}

func (f *osFile) Close() error { return f.file.Close() }

func (s *OS) regular(name string) error {
	info, err := s.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return &fs.PathError{Op: "regular", Path: name, Err: syscall.EINVAL}
	}
	return nil
}
