//go:build freebsd

package labrunner

import "golang.org/x/sys/unix"

func observeChild(pid int) (func() (bool, error), func() error, error) {
	fd, err := unix.Kqueue()
	if err != nil {
		return nil, nil, err
	}
	change := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ENABLE, Fflags: unix.NOTE_EXIT}
	_, err = unix.Kevent(fd, []unix.Kevent_t{change}, nil, nil)
	if err != nil {
		_ = unix.Close(fd)
		return nil, nil, err
	}
	exited := false
	return func() (bool, error) {
		if exited {
			return true, nil
		}
		events := make([]unix.Kevent_t, 1)
		count, err := unix.Kevent(fd, nil, events, &unix.Timespec{})
		if count > 0 {
			exited = true
		}
		return exited, err
	}, func() error { return unix.Close(fd) }, nil
}
