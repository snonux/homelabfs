package behavior

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func prepareUsers(dir int) (err error) {
	fd, err := openAt(dir, "group-file", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0640)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	if err = writeFull(fd, []byte("supplementary group access")); err != nil {
		return err
	}
	if err = native("fchown", func() error { return unix.Fchown(fd, 0, 20002) }); err != nil {
		return err
	}
	if err = native("fchmod", func() error { return unix.Fchmod(fd, 0640) }); err != nil {
		return err
	}
	return native("fchmod", func() error { return unix.Fchmod(dir, unix.S_ISVTX|0777) })
}

func memberAccess(dir int) (err error) {
	fd, err := openAt(dir, "group-file", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("supplementary group read: %w", err)
	}
	err = readEqual(fd, []byte("supplementary group access"))
	err = errors.Join(err, unix.Close(fd))
	if err != nil {
		return err
	}
	old := unix.Umask(0007)
	defer unix.Umask(old)
	fd, err = openAt(dir, "member-owned", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0666)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	var st unix.Stat_t
	if err = native("fstat", func() error { return unix.Fstat(fd, &st) }); err != nil {
		return err
	}
	if st.Uid != 20001 || st.Mode&0777 != 0660 {
		return fmt.Errorf("synthetic owner/umask mismatch")
	}
	return nil
}

func outsiderAccess(dir int) error {
	fd, err := openAt(dir, "group-file", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("outsider bypassed group permissions")
	}
	if !errors.Is(err, unix.EACCES) {
		return err
	}
	err = unix.Unlinkat(dir, "member-owned", 0)
	if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
		return fmt.Errorf("sticky directory deletion: %v", err)
	}
	fd, err = openAt(dir, "group-file", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("cached denial changed without chmod")
	}
	if !errors.Is(err, unix.EACCES) {
		return err
	}
	return nil
}

func publicAccess(dir int) error {
	fd, err := openAt(dir, "group-file", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("chmod did not invalidate permission denial: %w", err)
	}
	return unix.Close(fd)
}
