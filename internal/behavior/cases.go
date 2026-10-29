package behavior

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	appendCount       = 16
	appendRecordBytes = 32
)

func namespace(dir int) (err error) {
	fd, err := openAt(dir, "original", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	duplicate, createErr := openAt(dir, "original", unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY, 0600)
	if createErr == nil {
		_ = unix.Close(duplicate)
		return fmt.Errorf("exclusive create succeeded twice")
	}
	if !errors.Is(createErr, unix.EEXIST) {
		return fmt.Errorf("exclusive create: %w", createErr)
	}
	if err = writeFull(fd, []byte("original")); err != nil {
		return err
	}
	if err = linkAt(dir, "original", dir, "alias", 0); err != nil {
		return err
	}
	var before, alias unix.Stat_t
	if err = native("fstat", func() error { return unix.Fstat(fd, &before) }); err != nil {
		return err
	}
	if err = native("fstatat", func() error { return unix.Fstatat(dir, "alias", &alias, 0) }); err != nil {
		return err
	}
	if before.Ino != alias.Ino || before.Nlink != 2 {
		return fmt.Errorf("hard link identity/count mismatch")
	}
	other, err := openAt(dir, "replacement", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(other)) }()
	if err = writeFull(other, []byte("replaced")); err != nil {
		return err
	}
	if err = renameAt(dir, "original", dir, "renamed"); err != nil {
		return err
	}
	if err = verifyRename(dir, "original", "renamed", fd, []byte("original")); err != nil {
		return err
	}
	if err = renameAt(dir, "renamed", dir, "replacement"); err != nil {
		return err
	}
	if err = verifyRename(dir, "renamed", "replacement", fd, []byte("original")); err != nil {
		return err
	}
	if err = readEqual(other, []byte("replaced")); err != nil {
		return fmt.Errorf("replaced open identity: %w", err)
	}
	if err = unlinkAt(dir, "replacement", 0); err != nil {
		return err
	}
	if err = unlinkAt(dir, "alias", 0); err != nil {
		return err
	}
	if err = readEqual(fd, []byte("original")); err != nil {
		return fmt.Errorf("unlink-open: %w", err)
	}
	if err = truncateFile(fd, 3); err != nil {
		return err
	}
	if err = readEqual(fd, []byte("ori")); err != nil {
		return err
	}
	if err = truncateFile(fd, pageBytes); err != nil {
		return err
	}
	var hole [1]byte
	n, err := readAt(fd, hole[:], pageBytes-1)
	if err != nil {
		return err
	}
	if n != 1 || hole[0] != 0 {
		return fmt.Errorf("truncate extension is not zero-filled")
	}
	return nil
}

func verifyRename(dir int, source, destination string, original int, want []byte) (err error) {
	var absent unix.Stat_t
	err = native("stat renamed source "+source, func() error { return unix.Fstatat(dir, source, &absent, unix.AT_SYMLINK_NOFOLLOW) })
	if err == nil {
		return fmt.Errorf("rename source %s still exists", source)
	}
	if !errors.Is(err, unix.ENOENT) {
		return err
	}
	fd, err := openAt(dir, destination, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	var before, after unix.Stat_t
	if err = native("stat original rename fd", func() error { return unix.Fstat(original, &before) }); err != nil {
		return err
	}
	if err = native("stat renamed destination", func() error { return unix.Fstat(fd, &after) }); err != nil {
		return err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino {
		return fmt.Errorf("rename destination %s has wrong object identity", destination)
	}
	if err = readEqual(fd, want); err != nil {
		return fmt.Errorf("rename destination %s data: %w", destination, err)
	}
	return nil
}

func permissions(dir int) (err error) {
	old := unix.Umask(0027)
	defer unix.Umask(old)
	fd, err := openAt(dir, "mode", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0666)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	var st unix.Stat_t
	if err = native("fstat", func() error { return unix.Fstat(fd, &st) }); err != nil {
		return err
	}
	if st.Mode&0777 != 0640 {
		return fmt.Errorf("umask mode: got %o want 640", st.Mode&0777)
	}
	if err = native("fchmod", func() error { return unix.Fchmod(fd, 0600) }); err != nil {
		return err
	}
	if err = native("fchown", func() error { return unix.Fchown(fd, unix.Geteuid(), unix.Getegid()) }); err != nil {
		return err
	}
	if err = native("fstat", func() error { return unix.Fstat(fd, &st) }); err != nil {
		return err
	}
	if st.Mode&0777 != 0600 || st.Uid != uint32(unix.Geteuid()) || st.Gid != uint32(unix.Getegid()) {
		return fmt.Errorf("chmod/chown read-back mismatch")
	}
	if err = native("mkdirat", func() error { return unix.Mkdirat(dir, "setgid", 0777) }); err != nil {
		return err
	}
	groups, err := unix.Getgroups()
	if err != nil {
		return err
	}
	group := unix.Getegid()
	for _, candidate := range groups {
		if candidate != group {
			group = candidate
			break
		}
	}
	if unix.Geteuid() == 0 {
		group = 20002
	}
	if err = native("fchownat", func() error { return unix.Fchownat(dir, "setgid", -1, group, 0) }); err != nil {
		return err
	}
	if err = native("fchmodat", func() error { return unix.Fchmodat(dir, "setgid", unix.S_ISGID|0770, 0) }); err != nil {
		return err
	}
	sub, err := openAt(dir, "setgid", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(sub)) }()
	child, err := openAt(sub, "child", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0660)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(child)) }()
	var parent unix.Stat_t
	if err = native("fstat", func() error { return unix.Fstat(sub, &parent) }); err != nil {
		return err
	}
	if err = native("fstat", func() error { return unix.Fstat(child, &st) }); err != nil {
		return err
	}
	if parent.Mode&unix.S_ISGID == 0 || st.Gid != parent.Gid {
		return fmt.Errorf("setgid directory group inheritance mismatch")
	}
	return nil
}

func syncIO(dir int) (err error) {
	fd, err := openAt(dir, "sync", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR|unix.O_SYNC, 0600)
	if err != nil {
		return fmt.Errorf("open O_SYNC: %w", err)
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	if err = writeFull(fd, []byte("synchronous synthetic record")); err != nil {
		return fmt.Errorf("write O_SYNC: %w", err)
	}
	if err = retryInterrupted(func() error { return dataSync(fd) }); err != nil {
		return fmt.Errorf("fdatasync: %w", err)
	}
	if err = syncFile(fd); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}
	if err = syncFile(dir); err != nil {
		return fmt.Errorf("directory fsync: %w", err)
	}
	return readEqual(fd, []byte("synchronous synthetic record"))
}

// appendOnce intentionally does not retry; the owned Linux admission probe uses
// its native return to distinguish interrupted pre-admission from admitted I/O.
func appendOnce(dir int, data string) (err error) {
	fd, err := unix.Openat(dir, "append-probe", unix.O_CREAT|unix.O_WRONLY|unix.O_APPEND|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("open raw append probe: %w", err)
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	n, err := unix.Write(fd, []byte(data))
	if err != nil {
		return fmt.Errorf("raw append write: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("raw append short write %d/%d", n, len(data))
	}
	return nil
}

func mappedIO(dir int) (err error) {
	fd, err := openAt(dir, "mapped", unix.O_CREAT|unix.O_EXCL|unix.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	if err = truncateFile(fd, pageBytes); err != nil {
		return err
	}
	data, err := unix.Mmap(fd, 0, pageBytes, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("mmap required: %w", err)
	}
	defer func() { err = errors.Join(err, unix.Munmap(data)) }()
	want := []byte("synthetic shared mmap write")
	copy(data, want)
	if err = retryInterrupted(func() error { return unix.Msync(data, unix.MS_SYNC) }); err != nil {
		return fmt.Errorf("msync: %w", err)
	}
	if err = syncFile(fd); err != nil {
		return err
	}
	read, err := openAt(dir, "mapped", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(read)) }()
	got := make([]byte, len(want))
	n, err := readAt(read, got, 0)
	if err != nil {
		return err
	}
	if n != len(want) || !bytes.Equal(got, want) {
		return fmt.Errorf("mmap/msync reopen mismatch")
	}
	return nil
}

func appendRecords(dir int, marker string) (err error) {
	if marker != "A" && marker != "B" {
		return fmt.Errorf("unknown synthetic append marker")
	}
	fd, err := openAt(dir, "append", unix.O_CREAT|unix.O_WRONLY|unix.O_APPEND|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("open append: %w", err)
	}
	defer func() {
		closeErr := unix.Close(fd)
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close append: %w", closeErr))
		}
	}()
	for i := range appendCount {
		if err = writeFull(fd, []byte(appendRecord(marker, i)+"\n")); err != nil {
			return fmt.Errorf("append record %s/%d: %w", marker, i, err)
		}
	}
	return syncFile(fd)
}

func verifyAppend(dir int) (err error) {
	fd, err := openAt(dir, "append", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, unix.Close(fd)) }()
	wantBytes := appendCount * 2 * appendRecordBytes
	data := make([]byte, wantBytes+1)
	n, err := readAt(fd, data, 0)
	if err != nil {
		return err
	}
	if n != wantBytes {
		return fmt.Errorf("append size: got %d want %d", n, wantBytes)
	}
	counts := map[string]int{}
	for offset := 0; offset < n; offset += appendRecordBytes {
		record := data[offset : offset+appendRecordBytes]
		if record[appendRecordBytes-1] != '\n' {
			return fmt.Errorf("append record %d has invalid delimiter", offset/appendRecordBytes)
		}
		counts[string(record[:appendRecordBytes-1])]++
	}
	if len(counts) != appendCount*2 {
		return fmt.Errorf("append has %d unique records, want %d", len(counts), appendCount*2)
	}
	for _, marker := range []string{"A", "B"} {
		for i := range appendCount {
			if counts[appendRecord(marker, i)] != 1 {
				return fmt.Errorf("append missing/duplicated/interleaved record %s/%d", marker, i)
			}
		}
	}
	return nil
}

func appendRecord(marker string, index int) string {
	return fmt.Sprintf("%s:%04d:%s", marker, index, strings.Repeat(marker, 24))
}

func writeFull(fd int, data []byte) error {
	var n int
	err := retryInterrupted(func() error { var writeErr error; n, writeErr = unix.Write(fd, data); return writeErr })
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("short write %d/%d", n, len(data))
	}
	return nil
}

func retryInterrupted(call func() error) error {
	for range 3 {
		err := call()
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
	return unix.EINTR
}

func readEqual(fd int, want []byte) error {
	data := make([]byte, len(want)+1)
	n, err := readAt(fd, data, 0)
	if err != nil {
		return err
	}
	if !bytes.Equal(data[:n], want) {
		return fmt.Errorf("read-back mismatch: %q", data[:n])
	}
	return nil
}
