package behavior

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const pageBytes = 4096

type helper struct {
	root, work *os.File
	lock       int
	run        string
}

// Helper serves trusted test requests with fixed credentials and no exec. Every
// target access after bind is relative to a pinned directory, never a pathname
// that could resolve to a missing mount's underlying directory. Mmap runs here,
// outside the daemon runtime. Payloads and names are restricted to synthetic data.
func Helper(ctx context.Context, in io.Reader, out io.Writer) error {
	h := &helper{lock: -1}
	defer func() { _ = h.close() }()
	decoder, encoder := json.NewDecoder(in), json.NewEncoder(out)
	for {
		var c Command
		if err := decoder.Decode(&c); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(c.Data) > pageBytes || len(c.Path) > 4096 || !safeName(c.Run, true) || !safeName(c.Name, true) {
			return fmt.Errorf("unsafe helper request")
		}
		reply := Reply{}
		err := h.execute(c, &reply)
		if err != nil {
			reply.Error = err.Error()
			var errno unix.Errno
			if errors.As(err, &errno) {
				reply.Errno = int(errno)
			}
		}
		if err := encoder.Encode(reply); err != nil {
			return err
		}
	}
}

func (h *helper) execute(c Command, reply *Reply) error {
	if c.Op == "bind" {
		return h.bind(c)
	}
	if c.Op == "close" {
		return h.close()
	}
	if h.work == nil {
		return fmt.Errorf("helper has no pinned test directory")
	}
	fd := int(h.work.Fd())
	switch c.Op {
	case "close-lock":
		if h.lock < 0 {
			return nil
		}
		fd := h.lock
		h.lock = -1
		return unix.Close(fd)
	case "cleanup":
		if h.lock >= 0 {
			return fmt.Errorf("cleanup requires closed lock fd")
		}
		if err := clearDirectory(h.work); err != nil {
			return err
		}
		if err := h.work.Close(); err != nil {
			return err
		}
		h.work = nil
		return removeAt(int(h.root.Fd()), h.run, unix.AT_REMOVEDIR)
	case "namespace":
		return namespace(fd)
	case "permissions":
		return permissions(fd)
	case "sync":
		return syncIO(fd)
	case "mmap":
		return mappedIO(fd)
	case "append":
		return appendRecords(fd, c.Data)
	case "append-once":
		return appendOnce(fd, c.Data)
	case "verify-append":
		return verifyAppend(fd)
	case "prepare-users":
		return prepareUsers(fd)
	case "user-member":
		return memberAccess(fd)
	case "user-outsider":
		return outsiderAccess(fd)
	case "user-public":
		return publicAccess(fd)
	case "publicize":
		return native("fchmodat", func() error { return unix.Fchmodat(fd, "group-file", 0644, 0) })
	case "open-lock":
		if h.lock >= 0 {
			return fmt.Errorf("lock fd already open")
		}
		var err error
		h.lock, err = openAt(fd, "locks", unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		return err
	case "fcntl-try", "fcntl-unlock", "fcntl-wait":
		if h.lock < 0 {
			return unix.EBADF
		}
		lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
		op := unix.F_SETLK
		if c.Op == "fcntl-unlock" {
			lock.Type = unix.F_UNLCK
		}
		if c.Op == "fcntl-wait" {
			op = unix.F_SETLKW
		}
		return unix.FcntlFlock(uintptr(h.lock), op, &lock)
	case "flock-try":
		return unix.Flock(h.lock, unix.LOCK_EX|unix.LOCK_NB)
	case "flock-unlock":
		return unix.Flock(h.lock, unix.LOCK_UN)
	default:
		return fmt.Errorf("unknown helper operation %q", c.Op)
	}
}

func (h *helper) bind(c Command) error {
	if h.root != nil || c.Run == "" {
		return fmt.Errorf("duplicate or empty bind")
	}
	root, err := openRoot(c.Path, c.Identity)
	if err != nil {
		return err
	}
	h.root = root
	if c.Create {
		if err := native("mkdirat", func() error { return unix.Mkdirat(int(root.Fd()), c.Run, 0777) }); err != nil {
			return err
		}
		// Synthetic root-only helpers need access; the caller-provided target's
		// ancestors remain the operator's responsibility in standalone mode.
		if err := native("fchmodat", func() error { return unix.Fchmodat(int(root.Fd()), c.Run, 0777, 0) }); err != nil {
			return err
		}
	}
	fd, err := unix.Openat(int(root.Fd()), c.Run, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	h.work = os.NewFile(uintptr(fd), c.Run)
	h.run = c.Run
	return nil
}

func (h *helper) close() error {
	var errs []error
	if h.lock >= 0 {
		errs = append(errs, unix.Close(h.lock))
		h.lock = -1
	}
	if h.work != nil {
		errs = append(errs, h.work.Close())
		h.work = nil
	}
	if h.root != nil {
		errs = append(errs, h.root.Close())
		h.root = nil
	}
	return errors.Join(errs...)
}

func safeName(s string, empty bool) bool {
	return (empty && s == "") || (s != "" && s != "." && s != ".." && len(s) <= 128 && !strings.ContainsAny(s, "/\x00"))
}

func openRoot(path, expected string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "behavior target")
	if expected != "" {
		actual, err := mountIdentity(fd)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("target identity: %w", err), f.Close())
		}
		if actual != expected {
			return nil, errors.Join(fmt.Errorf("target volume/generation/session mismatch"), f.Close())
		}
	}
	return f, nil
}

func clearDirectory(dir *os.File) error {
	if _, err := dir.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind cleanup directory: %w", err)
	}
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("scan cleanup directory: %w", err)
	}
	for _, name := range names {
		fd, err := openDirectoryAt(int(dir.Fd()), name)
		if err == nil {
			child := os.NewFile(uintptr(fd), name)
			err = clearDirectory(child)
			err = errors.Join(err, child.Close())
			if err != nil {
				return err
			}
			err = removeAt(int(dir.Fd()), name, unix.AT_REMOVEDIR)
		} else if errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
			err = removeAt(int(dir.Fd()), name, 0)
		}
		if err != nil {
			return fmt.Errorf("cleanup entry %s: %w", name, err)
		}
	}
	return nil
}

func openDirectoryAt(dir int, name string) (int, error) {
	for range 3 {
		fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
	return -1, unix.EINTR
}

// Cleanup is idempotent within this run's private directory. An interrupted
// unlink may already have completed; ENOENT on retry therefore means success.
func removeAt(dir int, name string, flags int) error {
	interrupted := false
	for range 3 {
		err := unix.Unlinkat(dir, name, flags)
		if interrupted && errors.Is(err, unix.ENOENT) {
			return nil
		}
		if !errors.Is(err, unix.EINTR) {
			return err
		}
		interrupted = true
	}
	return unix.EINTR
}
