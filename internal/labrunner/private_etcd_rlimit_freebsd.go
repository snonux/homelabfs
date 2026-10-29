//go:build freebsd

package labrunner

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func setEtcdRlimit(resource int, value, hard uint64) error {
	if value == 0 || hard < value || hard > 1<<63-1 {
		return errors.New("private etcd rlimit exceeds positive signed native range")
	}
	return syscall.Setrlimit(resource, &syscall.Rlimit{Cur: int64(value), Max: int64(hard)})
}

func checkEtcdPlatform(root string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(root, &stat); err != nil {
		return err
	}
	if string(bytes.TrimRight(stat.Fstypename[:], "\x00")) != "ufs" {
		return errors.New("private etcd must use the independent guest UFS OS disk")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, "/bin/freebsd-version", "-ku").Output()
	if err != nil {
		return err
	}
	lines := strings.Fields(string(version))
	if len(lines) != 2 {
		return errors.New("native kernel/userland versions unavailable")
	}
	for _, line := range lines {
		if line != "15.1-RELEASE" && !strings.HasPrefix(line, "15.1-RELEASE-p") {
			return errors.New("private etcd requires FreeBSD15.1 kernel and userland")
		}
	}
	return nil
}
