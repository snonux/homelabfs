package vfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"syscall"
	"testing"
)

func TestOSConcurrentFileDirectoryReplacement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := NewOS(ctx, t.TempDir())
	must(t, err)
	defer func() { must(t, s.Close()) }()
	must(t, s.Mkdir(ctx, "parked", 0o700))
	done := make(chan error, 1)
	go func() { done <- inspectAndUnlink(ctx, s) }()
	moveErr := alternateFileAndDirectory(ctx, s)
	cancel()
	inspectErr := <-done
	must(t, moveErr)
	must(t, inspectErr)
}

func inspectAndUnlink(ctx context.Context, s *OS) error {
	for i := 0; i < 4000 && ctx.Err() == nil; i++ {
		f, err := s.Open(ctx, "target", os.O_RDONLY, 0)
		if err == nil {
			info, statErr := f.(*osFile).file.Stat()
			closeErr := f.Close()
			if statErr != nil {
				return statErr
			}
			if closeErr != nil {
				return closeErr
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("Open returned a %s handle", info.Mode().Type())
			}
		} else if !expectedConcurrentError(err) {
			return err
		}
		if err := s.Unlink(ctx, "target"); err != nil && !expectedConcurrentError(err) {
			return err
		}
		runtime.Gosched()
	}
	return nil
}

func alternateFileAndDirectory(ctx context.Context, s *OS) error {
	for i := 0; i < 1000; i++ {
		if err := s.Unlink(ctx, "target"); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := s.Rename(ctx, "parked", "target"); err != nil {
			return err
		}
		runtime.Gosched()
		// The competing goroutine can unlink only regular files. Losing this
		// empty directory is a contract violation under every legal schedule;
		// no assertion depends on winning a race or meeting a timing threshold.
		if err := s.Rename(ctx, "target", "parked"); err != nil {
			return fmt.Errorf("directory lost during concurrent Unlink: %w", err)
		}
		f, err := s.Open(ctx, "target", os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	return nil
}

func expectedConcurrentError(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EISDIR) || errors.Is(err, context.Canceled)
}
