//go:build linux || freebsd

package vfs

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
)

func renameRoot(root *os.Root, oldname, newname string) error {
	// Go's os.Root.Rename rejects every existing directory destination before
	// invoking renameat. Use rooted parent descriptors to retain the kernel's
	// atomic empty-directory replacement semantics on Linux and FreeBSD.
	oldparent, err := root.Open(path.Dir(oldname))
	if err != nil {
		return err
	}
	defer func() { _ = oldparent.Close() }()
	newparent, err := root.Open(path.Dir(newname))
	if err != nil {
		return err
	}
	defer func() { _ = newparent.Close() }()
	// Neither descriptor escapes this function; deferred Close keeps both
	// alive and prevents concurrent descriptor release during the syscall.
	for {
		err = unix.Renameat(int(oldparent.Fd()), path.Base(oldname), int(newparent.Fd()), path.Base(newname))
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		return &os.LinkError{Op: "renameat", Old: oldname, New: newname, Err: err}
	}
	return nil
}
