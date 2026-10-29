package behavior

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Ordinary Unix callers retry an EINTR with no completed effect. The controlled
// adapter additionally proves this boundary with TestLinuxBehaviorEINTRBoundary:
// rejected admission gives EINTR and zero bytes; admitted cancellation returns
// the definitive successful result. Three attempts remain a driver bound.
// Never put close(2) through retryInterrupted: the fd may already be released.
func openAt(dir int, name string, flags int, mode uint32) (fd int, err error) {
	err = retryInterrupted(func() error { var e error; fd, e = unix.Openat(dir, name, flags, mode); return e })
	if err != nil {
		err = fmt.Errorf("openat %s flags=%#x: %w", name, flags, err)
	}
	return fd, err
}

func readAt(fd int, data []byte, offset int64) (n int, err error) {
	err = retryInterrupted(func() error { var e error; n, e = unix.Pread(fd, data, offset); return e })
	if err != nil {
		err = fmt.Errorf("pread offset=%d: %w", offset, err)
	}
	return n, err
}

func truncateFile(fd int, size int64) error {
	return native("ftruncate", func() error { return unix.Ftruncate(fd, size) })
}
func syncFile(fd int) error { return native("fsync", func() error { return unix.Fsync(fd) }) }
func linkAt(oldDir int, old string, newDir int, new string, flags int) error {
	return native("linkat "+old+" -> "+new, func() error { return unix.Linkat(oldDir, old, newDir, new, flags) })
}
func renameAt(oldDir int, old string, newDir int, new string) error {
	return native("renameat "+old+" -> "+new, func() error { return unix.Renameat(oldDir, old, newDir, new) })
}
func unlinkAt(dir int, name string, flags int) error {
	return native("unlinkat "+name, func() error { return unix.Unlinkat(dir, name, flags) })
}

func native(name string, call func() error) error {
	if err := retryInterrupted(call); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
