package vfs

import (
	"os"
	"syscall"
)

func syncData(f *os.File) error {
	// Keep the descriptor pinned for the duration of fdatasync, including a
	// concurrent Close, instead of obtaining a raw fd with File.Fd.
	raw, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var syncErr error
	if err := raw.Control(func(fd uintptr) { syncErr = syscall.Fdatasync(int(fd)) }); err != nil {
		return err
	}
	return syncErr
}
