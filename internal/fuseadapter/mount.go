//go:build linux || freebsd

package fuseadapter

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

const identityAttribute = "user.homelabfs.identity"

// MountIdentity identifies the exact volume, service generation and connection.
// It must be checked through the mount before consumers publish/use its path.
type MountIdentity struct{ Volume, Generation, Session fsapi.UUID }

// Capabilities explicitly describes this restricted mount profile. False values
// are unsupported contracts, not claims inferred from successful ordinary I/O.
type Capabilities struct {
	DirectIO, DefaultPermissions, LocalLocks, FileSync, DirectorySync                bool
	Mmap, Writeback, NormalPublication, VerifiedAncestorSearch, ArbitraryCredentials bool
}

// Mount owns a go-fuse server and waits for normal unmount plus adapter cleanup.
type Mount struct {
	server  *fuse.Server
	adapter *Adapter
	done    chan struct{}
	mu      sync.Mutex
	detach  func()
}

// Mount starts a restricted disposable mount at an existing empty directory.
// Writeback-cache negotiation, keep-cache, symlink cache and ID mapping stay
// disabled. Only explicit root-owned controlled multiuser/unprivileged
// validation enables allow_other.
func (a *Adapter) Mount(path string) (*Mount, error) {
	a.mu.Lock()
	if a.mountStarted {
		a.mu.Unlock()
		return nil, fmt.Errorf("%w: adapter session already used", fsapi.Stale)
	}
	a.mountStarted = true
	a.mu.Unlock()
	full, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%w: mountpoint must be a directory", fsapi.Invalid)
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	if len(entries) != 0 {
		return nil, fmt.Errorf("%w: mountpoint must be empty", fsapi.NotEmpty)
	}
	if a.publication == nil {
		return nil, fsapi.Unsupported
	}
	detach, err := a.publication.AttachFrontend()
	if err != nil {
		return nil, fmt.Errorf("attach frontend: %w", err)
	}
	if participants, ok := a.credentials.(interface{ seal() }); ok {
		participants.seal()
	}
	options := mountOptions()
	options.AllowOther = a.multiuser
	server, err := fuse.NewServer(a, full, options)
	if err != nil {
		// Preserve publication admission after an uncertain native mount error;
		// the owner must inspect/restart this service before trying again.
		return nil, fmt.Errorf("mount %s: %w", full, err)
	}
	m := &Mount{server: server, adapter: a, done: make(chan struct{}), detach: detach}
	go func() { server.Serve(); close(m.done) }()
	if err = server.WaitMount(); err != nil {
		unmountErr := m.Unmount()
		return nil, fmt.Errorf("mount readiness: %w", errors.Join(err, unmountErr))
	}
	return m, nil
}

// Identity returns the immutable identity exposed by the mounted root xattr.
func (a *Adapter) Identity() MountIdentity {
	return MountIdentity{Volume: a.info.Volume, Generation: a.info.Generation, Session: a.session}
}

// Capabilities reports the selected disposable contract. Normal publication and
// cache fencing remain unsupported, including when buffered mmap is admitted.
func (a *Adapter) Capabilities() Capabilities {
	return Capabilities{DirectIO: !a.buffered, Mmap: a.buffered, DefaultPermissions: true, LocalLocks: true, FileSync: true, DirectorySync: true}
}

// Unmount requests a normal unmount. Failure preserves the mount/resources and
// is returned to the caller; no forced/lazy fallback or backing removal occurs.
func (m *Mount) Unmount() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.server.Unmount(); err != nil {
		return fmt.Errorf("normal unmount: %w", err)
	}
	<-m.done
	if err := m.adapter.Err(); err != nil {
		return err
	}
	m.detach()
	return nil
}

// OnUnmount drains retained service obligations after all callbacks have exited.
// This also covers kernel-disconnected late replies that had no FORGET/RELEASE.
func (a *Adapter) OnUnmount() {
	a.blocked.Store(true)
	if err := a.cleanup(fsapi.Command{Operation: fsapi.SessionExit, Lock: fsapi.Lock{Owner: fsapi.LockOwner{Session: a.session, Generation: a.info.Generation}}}); err != nil {
		a.fail(err)
		return
	}
	a.mu.Lock()
	handles := make([]uint64, 0, len(a.handles))
	for id := range a.handles {
		handles = append(handles, id)
	}
	lookups := make(map[fsapi.ObjectID]uint64)
	for _, n := range a.nodes {
		if n.lookups > 0 {
			lookups[n.object] = n.lookups
		}
	}
	a.mu.Unlock()
	for _, id := range handles {
		a.releaseHandle(id)
	}
	for id, count := range lookups {
		if err := a.cleanup(fsapi.Command{Operation: fsapi.Forget, Object: id, Count: count}); err != nil {
			a.fail(err)
		}
	}
}

// GetXAttr exposes only an immutable mount identity on the root; it still checks
// service authority. All mutable/other xattrs remain unsupported.
func (a *Adapter) GetXAttr(cancel <-chan struct{}, h *fuse.InHeader, name string, dest []byte) (uint32, fuse.Status) {
	if h.NodeId != 1 || name != identityAttribute {
		return 0, status(fsapi.Unsupported)
	}
	_, s := a.call(cancel, h, fsapi.Command{Operation: fsapi.Statfs})
	if s != fuse.OK {
		return 0, s
	}
	data := a.Identity().bytes()
	if len(dest) == 0 {
		return uint32(len(data)), fuse.OK
	}
	if len(dest) < len(data) {
		return 0, fuse.ERANGE
	}
	copy(dest, data)
	return uint32(len(data)), fuse.OK
}

// CheckMount pins an opened directory, verifies its native statfs type and reads
// the exact volume/generation/session identity through that same mounted root.
// An unmounted empty fallback directory is rejected before consumer access.
func CheckMount(path string, expected MountIdentity) error {
	if !expected.Volume.Valid() || !expected.Generation.Valid() || !expected.Session.Valid() {
		return fsapi.Invalid
	}
	root, err := OpenMount(path, expected)
	if err != nil {
		return err
	}
	return root.Close()
}

// OpenMount returns a directory fd pinned to the verified FUSE connection.
// Controlled helpers must access children with openat on this fd, so later
// unmount cannot redirect a path lookup into an empty backing directory.
func OpenMount(path string, expected MountIdentity) (*os.File, error) {
	if !expected.Volume.Valid() || !expected.Generation.Valid() || !expected.Session.Valid() {
		return nil, fsapi.Invalid
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open mounted root: %w", err)
	}
	root := os.NewFile(uintptr(fd), "verified HomelabFS mount")
	data, err := mountedIdentity(fd)
	if err == nil && !bytes.Equal(data, expected.bytes()) {
		err = fmt.Errorf("%w: mounted volume/generation/session mismatch", fsapi.Stale)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("mount identity: %w", err), root.Close())
	}
	return root, nil
}

func (id MountIdentity) bytes() []byte {
	return []byte("homelabfs-validation-v1\n" + id.Volume.String() + "\n" + id.Generation.String() + "\n" + id.Session.String() + "\n")
}

func mountOptions() *fuse.MountOptions {
	return &fuse.MountOptions{FsName: "homelabfs", Name: "homelabfs", Options: []string{"default_permissions", "nosuid", "nodev"}, MaxWrite: nativeRequestBytes, MaxBackground: 16, EnableLocks: true, DisableReadDirPlus: true, DisableSplice: true}
}
