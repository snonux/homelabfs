package vfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync"
	"syscall"
)

type osDir struct {
	backend *OS
	file    *os.File
	mu      sync.Mutex
	closed  bool
}

var (
	_ Discovery = (*OS)(nil)
	_ Statter   = (*osFile)(nil)
	_ Dir       = (*osDir)(nil)
)

// Stat returns no-follow physical information for a regular file or directory.
func (s *OS) Stat(ctx context.Context, name string) (Info, error) {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if err := s.checkPath(name, false); err != nil {
		return Info{}, err
	}
	info, err := s.root.Lstat(name)
	if err != nil {
		return Info{}, err
	}
	return physicalInfo(info)
}

// OpenDir opens a directory for bounded discovery without following symlinks.
func (s *OS) OpenDir(ctx context.Context, name string) (Dir, error) {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.checkPath(name, false); err != nil {
		return nil, err
	}
	info, err := s.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, pathError("opendir", name, syscall.ENOTDIR)
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, err
	}
	return &osDir{backend: s, file: f}, nil
}

// Rmdir removes one empty directory, never a regular file or the backing root.
func (s *OS) Rmdir(ctx context.Context, name string) error {
	s.namespaceMu.Lock()
	defer s.namespaceMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkPath(name, false); err != nil {
		return err
	}
	if name == "." {
		return pathError("rmdir", name, fs.ErrInvalid)
	}
	info, err := s.root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return pathError("rmdir", name, syscall.ENOTDIR)
	}
	return s.root.Remove(name)
}

func (f *osFile) Stat(ctx context.Context) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	info, err := f.file.Stat()
	if err != nil {
		return Info{}, err
	}
	return physicalInfo(info)
}

func (d *osDir) Stat(ctx context.Context) (Info, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if d.closed {
		return Info{}, fs.ErrClosed
	}
	info, err := d.file.Stat()
	if err != nil {
		return Info{}, err
	}
	return physicalInfo(info)
}

func (d *osDir) ReadDir(ctx context.Context, n int) ([]Entry, error) {
	d.backend.namespaceMu.Lock()
	defer d.backend.namespaceMu.Unlock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.closed {
		return nil, fs.ErrClosed
	}
	if n < 1 || n > MaxReadDir {
		return nil, fs.ErrInvalid
	}
	// Read names without triggering os.DirEntry's eager or pathname-based
	// metadata lookup, whose behavior differs between Go 1.26 patch releases.
	names, readErr := d.file.Readdirnames(n)
	result, err := physicalEntries(ctx, names, func(name string) (Info, error) {
		return statDirEntry(d.file, name)
	})
	if err != nil {
		return result, err
	}
	return result, readErr
}

// physicalEntries checks cancellation before each actual metadata lookup. Its
// small lookup seam also lets tests count native descriptor-relative calls.
func physicalEntries(ctx context.Context, names []string, stat func(string) (Info, error)) ([]Entry, error) {
	result := make([]Entry, 0, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		info, err := stat(name)
		if err != nil {
			return result, err
		}
		result = append(result, Entry{Name: name, Info: info})
	}
	return result, nil
}

func (d *osDir) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return fs.ErrClosed
	}
	d.closed = true
	return d.file.Close()
}

// checkPath rejects every symlink component, including links whose targets are
// inside the root. The namespace lock protects the check/use sequence only from
// this backend; admission must establish exclusive ownership of the tree.
func (s *OS) checkPath(name string, missingFinal bool) error {
	if err := validName(name); err != nil {
		return err
	}
	components := strings.Split(name, "/")
	for i := range components {
		current := strings.Join(components[:i+1], "/")
		info, err := s.root.Lstat(current)
		if err != nil {
			if missingFinal && i == len(components)-1 && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return pathError("nofollow", current, syscall.ELOOP)
		}
		if !info.IsDir() && (!info.Mode().IsRegular() || i < len(components)-1) {
			return pathError("type", current, syscall.EINVAL)
		}
	}
	return nil
}
