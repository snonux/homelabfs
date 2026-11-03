//go:build freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"golang.org/x/sys/unix"
)

type nativeConnection struct {
	identity string
	device   *os.File
	path     string
	stopped  chan struct{}
}

// mountNative creates the association itself. Neither fstat(/dev/fuse), a
// supplied fsid nor a configuration boolean can manufacture this witness.
func mountNative(ctx context.Context, path string, shell *fuseadapter.Shell) (*nativeMount, error) {
	c := &nativeConnection{path: path, stopped: make(chan struct{})}
	m := &nativeMount{shell: shell, witness: c}
	if err := ctx.Err(); err != nil {
		return m, err
	}
	full, err := filepath.Abs(path)
	if err != nil {
		return m, err
	}
	c.path = full
	st, err := os.Lstat(full)
	if err != nil {
		return m, err
	}
	if !st.IsDir() || st.Mode().Perm() != 0700 {
		return m, fsapi.Permission
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return m, err
	}
	if len(entries) != 0 {
		return m, fsapi.NotEmpty
	}
	before, err := mountIdentity(full)
	if err != nil {
		return m, err
	}
	if before != "" {
		return m, fsapi.Busy
	}
	fd, err := unix.Open("/dev/fuse", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return m, err
	}
	c.device = os.NewFile(uintptr(fd), "owned-fuse-connection")
	helper, err := os.Open("/sbin/mount_fusefs")
	if err != nil {
		return m, err
	}
	defer func() { _ = helper.Close() }()
	var hs unix.Stat_t
	if err = unix.Fstat(int(helper.Fd()), &hs); err != nil {
		return m, err
	}
	if hs.Uid != 0 || hs.Mode&0022 != 0 || uint32(hs.Mode)&unix.S_IFMT != unix.S_IFREG {
		return m, fsapi.Permission
	}
	// Execute the pinned descriptor, not a path that can change after inspection.
	cmd := exec.Command("/dev/fd/4", "--safe", "-o", "default_permissions,nosuid,subtype=homelabfs", "3", full)
	cmd.Env = []string{"MOUNT_FUSEFS_CALL_BY_LIB=1"}
	cmd.ExtraFiles = []*os.File{c.device, helper}
	if err = cmd.Run(); err != nil {
		return m, fmt.Errorf("owned mount_fusefs (resources retained): %w", err)
	}
	m.identity, err = mountIdentity(full)
	if err != nil {
		return m, err
	}
	if m.identity == "" {
		return m, fsapi.Unavailable
	}
	// The exact same open /dev/fuse description is passed to go-fuse. Keep our
	// independent reference alive; go-fuse exclusively owns and closes the dup.
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return m, err
	}
	options := &fuse.MountOptions{Name: "homelabfs", FsName: "homelabfs-controlled", MaxWrite: 64 << 10, MaxBackground: 4, DisableXAttrs: true}
	server, err := fuse.NewServer(shell, "/dev/fd/"+strconv.Itoa(duplicate), options)
	if err != nil {
		return m, fmt.Errorf("owned FUSE INIT (resources retained): %w", err)
	}
	m.server = server
	m.witness = c
	c.identity = m.identity
	go func() { server.Serve(); shell.OnUnmount(); close(c.stopped) }()
	return m, nil
}

func mountIdentity(path string) (string, error) {
	count, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return "", err
	}
	if count > 4096 {
		return "", fsapi.NoSpace
	}
	mounts := make([]unix.Statfs_t, count+1)
	n, err := unix.Getfsstat(mounts, unix.MNT_NOWAIT)
	if err != nil {
		return "", err
	}
	if n > len(mounts) {
		return "", fsapi.NoSpace
	}
	identity := ""
	for _, mount := range mounts[:n] {
		if string(bytes.TrimRight(mount.Mntonname[:], "\x00")) != path {
			continue
		}
		if identity != "" {
			return "", fsapi.Stale
		}
		kind := string(bytes.TrimRight(mount.Fstypename[:], "\x00"))
		if kind != "fusefs" && kind != "fusefs.homelabf" {
			return "", fsapi.Permission
		}
		identity = fmt.Sprintf("%08x:%08x", uint32(mount.Fsid.Val[0]), uint32(mount.Fsid.Val[1]))
	}
	return identity, nil
}

func (c *nativeConnection) check() error {
	select {
	case <-c.stopped:
		return fsapi.Unavailable
	default:
	}
	if c.device == nil {
		return fsapi.Unavailable
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(c.device.Fd()), &st); err != nil {
		return err
	}
	if uint32(st.Mode)&unix.S_IFMT != unix.S_IFCHR {
		return fsapi.Permission
	}
	identity, err := mountIdentity(c.path)
	if err != nil {
		return err
	}
	if identity != c.identity {
		return fsapi.Stale
	}
	return nil
}

func openNativeTuple(ctx context.Context, m *nativeMount) error {
	c, ok := m.witness.(*nativeConnection)
	if !ok {
		return fsapi.Unavailable
	}
	endpoint, err := m.shell.Endpoint()
	if err != nil {
		return err
	}
	names := []string{"command", "result", "recall"}
	for i, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		flags := unix.O_RDONLY | unix.O_DIRECT | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if i == 0 {
			flags = flags&^unix.O_ACCMODE | unix.O_WRONLY
		}
		fd, e := unix.Open(filepath.Join(c.path, guestbridge.ControlName, name), flags, 0)
		if e != nil {
			return e
		}
		m.files[i] = os.NewFile(uintptr(fd), name)
	}
	nodes := [3]uint64{endpoint.CommandNode, endpoint.ResultNode, endpoint.RecallNode}
	for i, file := range m.files {
		var st unix.Stat_t
		if err = unix.Fstat(int(file.Fd()), &st); err != nil {
			return err
		}
		var fs unix.Statfs_t
		if err = unix.Fstatfs(int(file.Fd()), &fs); err != nil {
			return err
		}
		identity := fmt.Sprintf("%08x:%08x", uint32(fs.Fsid.Val[0]), uint32(fs.Fsid.Val[1]))
		if uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG || st.Ino != nodes[i] || st.Size != int64(guestbridge.VirtualSize) || identity != m.identity {
			return fsapi.Permission
		}
		access := uint32(2)
		if i == 0 {
			access = 1
		}
		m.expected[i] = descriptorIdentity{Device: uint64(st.Dev), Inode: st.Ino, Size: uint64(st.Size), Access: access}
		rights := []uint64{unix.CAP_FSTAT, unix.CAP_FSTATFS, unix.CAP_FCNTL, unix.CAP_SEEK}
		if i == 0 {
			rights = append(rights, unix.CAP_PWRITE)
		} else {
			rights = append(rights, unix.CAP_PREAD)
		}
		cap, err := unix.CapRightsInit(rights)
		if err != nil {
			return err
		}
		if err = unix.CapRightsLimit(file.Fd(), cap); err != nil {
			return err
		}
		if _, _, errno := unix.Syscall(unix.SYS_CAP_FCNTLS_LIMIT, file.Fd(), unix.CAP_FCNTL_GETFL, 0); errno != 0 {
			return errno
		}
	}
	return nil
}
