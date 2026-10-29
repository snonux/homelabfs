//go:build linux

package labrunner

import "golang.org/x/sys/unix"

func observeChild(pid int) (func() (bool, error), func() error, error) {
	return func() (bool, error) {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		return info.Signo != 0, err
	}, func() error { return nil }, nil
}
