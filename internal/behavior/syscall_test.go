package behavior

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHelperOpenLockRetriesInterrupted(t *testing.T) {
	// These subtests mutate the syscall seam and must remain serial.
	for _, tc := range []struct {
		name       string
		interrupts int
		attempts   int
		wantErr    error
	}{
		{name: "interrupted then opened", interrupts: 1, attempts: 2},
		{name: "retry bound exhausted", interrupts: 3, attempts: 3, wantErr: unix.EINTR},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir()
			work, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			h := helper{work: work, lock: -1}
			t.Cleanup(func() {
				if err := h.close(); err != nil {
					t.Error(err)
				}
			})
			actualOpenat := openat
			t.Cleanup(func() { openat = actualOpenat })
			attempts := 0
			openat = func(dir int, name string, flags int, mode uint32) (int, error) {
				attempts++
				if dir != int(work.Fd()) || name != "locks" || flags != unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW || mode != 0600 {
					t.Fatalf("unexpected open-lock arguments: %d %q %#x %#o", dir, name, flags, mode)
				}
				if attempts <= tc.interrupts {
					return -1, unix.EINTR
				}
				return actualOpenat(dir, name, flags, mode)
			}
			err = h.execute(Command{Op: "open-lock"}, &Reply{})
			if !errors.Is(err, tc.wantErr) || attempts != tc.attempts {
				t.Fatalf("open-lock: err=%v attempts=%d; want err=%v attempts=%d", err, attempts, tc.wantErr, tc.attempts)
			}
			if tc.wantErr != nil {
				if h.lock >= 0 {
					t.Fatalf("retained lock fd after exhausted retries: %d", h.lock)
				}
				if _, err := os.Stat(filepath.Join(path, "locks")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("interrupted open created a file: %v", err)
				}
				return
			}
			const payload = "lock fd from retried open"
			if n, err := unix.Pwrite(h.lock, []byte(payload), 0); err != nil || n != len(payload) {
				t.Fatalf("write retained lock fd: n=%d err=%v", n, err)
			}
			data, err := os.ReadFile(filepath.Join(path, "locks"))
			if err != nil || string(data) != payload {
				t.Fatalf("lock file readback: %q err=%v", data, err)
			}
		})
	}
}
