//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"sync"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// NativeCredentials authenticates exact supervised exporter incarnation and full
// actual kernel credentials, independently of all guest payload credentials.
// Snapshot must be bounded and request-bound; no /proc/account fallback exists.
type NativeCredentials interface {
	Snapshot(context.Context, Caller, guestbridge.Endpoint) (guestbridge.Native, error)
}

// NativeRequestCredentials optionally supplies a private request observation for
// the exact bootstrap metadata operation. Ordinary Snapshot never authorizes a
// provisioning caller, and the returned context is used only for authentication.
type NativeRequestCredentials interface {
	SnapshotRequest(context.Context, Caller, guestbridge.Endpoint, uint64, string) (context.Context, guestbridge.Native, error)
}

// PrivateControlConfig composes synthetic control inodes with an existing
// restricted native adapter. This is component construction, not mount readiness.
// Directory and leaf IDs must exceed the ordinary adapter's entire retained map.
type PrivateControlConfig struct {
	Ordinary    *Adapter
	Transport   *guestbridge.Transport
	Credentials NativeCredentials
	Directory   uint64
}

// PrivateControl routes private inodes through real raw FUSE callbacks. fh is
// intentionally shared per inode, never an application descriptor identity.
// No constructor/mount CLI selects this component or opens normal publication.
type PrivateControl struct {
	fuse.RawFileSystem
	transport   *guestbridge.Transport
	credentials NativeCredentials
	directory   uint64
	privateRoot bool
	mu          sync.Mutex
	failure     error
}

var _ fuse.RawFileSystem = (*PrivateControl)(nil)

// NewPrivateControl requires actual enrollment and separate native attestation.
func NewPrivateControl(c PrivateControlConfig) (*PrivateControl, error) {
	if c.Transport == nil || c.Credentials == nil || c.Directory <= 1 {
		return nil, fsapi.Invalid
	}
	e := c.Transport.Endpoint()
	for _, id := range []uint64{e.CommandNode, e.ResultNode, e.RecallNode} {
		if id == c.Directory {
			return nil, fsapi.Invalid
		}
	}
	ordinary := fuse.NewDefaultRawFileSystem()
	if c.Ordinary != nil {
		if c.Ordinary.publication == nil {
			return nil, fsapi.Unavailable
		}
		if err := c.Ordinary.publication.CheckPrivateControlNamespace(context.Background()); err != nil {
			return nil, err
		}
		limit := uint64(c.Ordinary.maxNodes) + 1
		for _, id := range []uint64{c.Directory, e.CommandNode, e.ResultNode, e.RecallNode} {
			if id <= limit {
				return nil, fsapi.Invalid
			}
		}
		ordinary = c.Ordinary
	}
	return &PrivateControl{RawFileSystem: ordinary, transport: c.Transport, credentials: c.Credentials, directory: c.Directory, privateRoot: c.Ordinary == nil}, nil
}

// Err reports unauthenticated kernel close events, which cannot retire a cell,
// claim, session, cache domain or allocator. Such resources remain retained.
func (a *PrivateControl) Err() error { a.mu.Lock(); defer a.mu.Unlock(); return a.failure }

// Lookup hides the reserved directory and names from all ordinary callers.
func (a *PrivateControl) Lookup(cancel <-chan struct{}, h *fuse.InHeader, name string, out *fuse.EntryOut) fuse.Status {
	if h.NodeId == 1 && a.privateRoot && name != guestbridge.ControlName {
		return fuse.ENOENT
	}
	if h.NodeId == 1 && name == guestbridge.ControlName || a.private(h.NodeId) {
		n, s := a.native(h, "LOOKUP:"+name)
		if s != fuse.OK {
			return s
		}
		id := a.directory
		if h.NodeId == a.directory {
			e := a.transport.Endpoint()
			id = map[string]uint64{"command": e.CommandNode, "result": e.ResultNode, "recall": e.RecallNode}[name]
			if id == 0 {
				return fuse.ENOENT
			}
		} else if h.NodeId != 1 {
			return fuse.ENOENT
		}
		*out = fuse.EntryOut{NodeId: id, Generation: 1, Attr: a.attr(id, n)}
		return fuse.OK
	}
	return a.RawFileSystem.Lookup(cancel, h, name, out)
}

// GetAttr advertises a direct-I/O virtual size without workload storage entries.
func (a *PrivateControl) GetAttr(cancel <-chan struct{}, in *fuse.GetAttrIn, out *fuse.AttrOut) fuse.Status {
	if !a.private(in.NodeId) {
		return a.RawFileSystem.GetAttr(cancel, in, out)
	}
	n, s := a.native(&in.InHeader, "GETATTR")
	if s != fuse.OK {
		return s
	}
	*out = fuse.AttrOut{Attr: a.attr(in.NodeId, n)}
	return fuse.OK
}

// Open never derives a session, allocator or description from fh.
func (a *PrivateControl) Open(cancel <-chan struct{}, in *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	if !a.private(in.NodeId) {
		return a.RawFileSystem.Open(cancel, in, out)
	}
	_, s := a.native(&in.InHeader, "OPEN")
	if s != fuse.OK {
		return s
	}
	if in.NodeId == a.directory || in.NodeId == 1 {
		return fuse.EISDIR
	}
	if in.Flags&uint32(syscall.O_APPEND|syscall.O_TRUNC|syscall.O_CREAT) != 0 {
		return fuse.EACCES
	}
	access := in.Flags & uint32(syscall.O_ACCMODE)
	if in.NodeId == a.transport.Endpoint().CommandNode {
		if access != uint32(syscall.O_WRONLY) {
			return fuse.EACCES
		}
	} else if access != uint32(syscall.O_RDONLY) {
		return fuse.EACCES
	}
	*out = fuse.OpenOut{Fh: in.NodeId, OpenFlags: fuse.FOPEN_DIRECT_IO}
	return fuse.OK
}

// Read is a nonblocking positional immutable-snapshot fetch.
func (a *PrivateControl) Read(cancel <-chan struct{}, in *fuse.ReadIn, buf []byte) (fuse.ReadResult, fuse.Status) {
	if !a.private(in.NodeId) {
		return a.RawFileSystem.Read(cancel, in, buf)
	}
	n, s := a.native(&in.InHeader, "READ")
	if s != fuse.OK {
		return nil, s
	}
	b, e := a.transport.Read(context.Background(), n, in.NodeId, in.Offset, in.Size)
	if e != nil {
		return nil, errorStatus(e)
	}
	return fuse.ReadResultData(b), fuse.OK
}

// Write copies/enqueues one complete chunk and returns transport receipt only.
func (a *PrivateControl) Write(cancel <-chan struct{}, in *fuse.WriteIn, data []byte) (uint32, fuse.Status) {
	if !a.private(in.NodeId) {
		return a.RawFileSystem.Write(cancel, in, data)
	}
	n, s := a.native(&in.InHeader, "WRITE")
	if s != fuse.OK {
		return 0, s
	}
	if len(data) != int(in.Size) || in.WriteFlags&fuse.WRITE_CACHE != 0 {
		return 0, fuse.EINVAL
	}
	count, e := a.transport.Write(context.Background(), n, in.NodeId, in.Offset, data)
	return count, errorStatus(e)
}

// Forget has no native credentials. It cannot change protocol lifetimes or IDs.
func (a *PrivateControl) Forget(id, count uint64) {
	if !a.private(id) {
		a.RawFileSystem.Forget(id, count)
	}
}

// Release authenticates, but never retires protocol state based on native close.
func (a *PrivateControl) Release(cancel <-chan struct{}, in *fuse.ReleaseIn) {
	if !a.private(in.NodeId) {
		a.RawFileSystem.Release(cancel, in)
		return
	}
	if _, s := a.native(&in.InHeader, "RELEASE"); s != fuse.OK {
		a.mu.Lock()
		a.failure = fsapi.Permission
		a.mu.Unlock()
	}
}

// ReleaseDir follows the same no-retirement rule as Release.
func (a *PrivateControl) ReleaseDir(in *fuse.ReleaseIn) {
	if !a.private(in.NodeId) {
		a.RawFileSystem.ReleaseDir(in)
		return
	}
	a.Release(nil, in)
}
func (a *PrivateControl) private(id uint64) bool {
	e := a.transport.Endpoint()
	return id == 1 && a.privateRoot || id == a.directory || id == e.CommandNode || id == e.ResultNode || id == e.RecallNode
}
func (a *PrivateControl) native(h *fuse.InHeader, operation string) (guestbridge.Native, fuse.Status) {
	ctx := context.Background()
	caller := Caller{UID: h.Uid, GID: h.Gid, Thread: h.Pid, Unique: h.Unique}
	var n guestbridge.Native
	var e error
	if observer, ok := a.credentials.(NativeRequestCredentials); ok {
		ctx, n, e = observer.SnapshotRequest(observeNativeRequest(ctx, caller, h.NodeId, operation), caller, a.transport.Endpoint(), h.NodeId, operation)
	} else {
		n, e = a.credentials.Snapshot(ctx, caller, a.transport.Endpoint())
	}
	if e != nil || n.UID != h.Uid || n.GID != h.Gid || n.PID != h.Pid || n.Unique != h.Unique || n.Endpoint != a.transport.Endpoint() {
		return guestbridge.Native{}, fuse.EACCES
	}
	if e = a.transport.Authenticate(ctx, n); e != nil {
		return guestbridge.Native{}, errorStatus(e)
	}
	return n, fuse.OK
}
func (a *PrivateControl) deny(h *fuse.InHeader) fuse.Status {
	_, s := a.native(h, "DENY")
	if s != fuse.OK {
		return s
	}
	return fuse.EACCES
}
func (a *PrivateControl) attr(id uint64, n guestbridge.Native) fuse.Attr {
	mode := uint32(syscall.S_IFREG | 0600)
	size := guestbridge.VirtualSize
	if id == a.directory || id == 1 {
		mode = uint32(syscall.S_IFDIR | 0700)
		size = 0
	}
	return fuse.Attr{Ino: id, Size: size, Mode: mode, Nlink: 1, Owner: fuse.Owner{Uid: n.UID, Gid: n.GID}}
}

// SetAttr authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) SetAttr(cancel <-chan struct{}, in *fuse.SetAttrIn, out *fuse.AttrOut) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.SetAttr(cancel, in, out)
}

// Mknod authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Mknod(cancel <-chan struct{}, in *fuse.MknodIn, name string, out *fuse.EntryOut) fuse.Status {
	if a.private(in.NodeId) || name == guestbridge.ControlName {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Mknod(cancel, in, name, out)
}

// Mkdir authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Mkdir(cancel <-chan struct{}, in *fuse.MkdirIn, name string, out *fuse.EntryOut) fuse.Status {
	if a.private(in.NodeId) || name == guestbridge.ControlName {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Mkdir(cancel, in, name, out)
}

// Create authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Create(cancel <-chan struct{}, in *fuse.CreateIn, name string, out *fuse.CreateOut) fuse.Status {
	if a.private(in.NodeId) || name == guestbridge.ControlName {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Create(cancel, in, name, out)
}

// Unlink authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Unlink(cancel <-chan struct{}, h *fuse.InHeader, name string) fuse.Status {
	if a.private(h.NodeId) || name == guestbridge.ControlName {
		return a.deny(h)
	}
	return a.RawFileSystem.Unlink(cancel, h, name)
}

// Rmdir authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Rmdir(cancel <-chan struct{}, h *fuse.InHeader, name string) fuse.Status {
	if a.private(h.NodeId) || name == guestbridge.ControlName {
		return a.deny(h)
	}
	return a.RawFileSystem.Rmdir(cancel, h, name)
}

// Rename authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Rename(cancel <-chan struct{}, in *fuse.RenameIn, oldName, newName string) fuse.Status {
	if a.private(in.NodeId) || a.private(in.Newdir) || oldName == guestbridge.ControlName || newName == guestbridge.ControlName {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Rename(cancel, in, oldName, newName)
}

// Link authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Link(cancel <-chan struct{}, in *fuse.LinkIn, name string, out *fuse.EntryOut) fuse.Status {
	if a.private(in.NodeId) || a.private(in.Oldnodeid) || name == guestbridge.ControlName {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Link(cancel, in, name, out)
}

// Symlink authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Symlink(cancel <-chan struct{}, h *fuse.InHeader, target, name string, out *fuse.EntryOut) fuse.Status {
	if a.private(h.NodeId) || name == guestbridge.ControlName {
		return a.deny(h)
	}
	return a.RawFileSystem.Symlink(cancel, h, target, name, out)
}

// Readlink authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Readlink(cancel <-chan struct{}, h *fuse.InHeader) ([]byte, fuse.Status) {
	if a.private(h.NodeId) {
		return nil, a.deny(h)
	}
	return a.RawFileSystem.Readlink(cancel, h)
}

// Access authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Access(cancel <-chan struct{}, in *fuse.AccessIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Access(cancel, in)
}

// GetXAttr authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) GetXAttr(cancel <-chan struct{}, h *fuse.InHeader, name string, dest []byte) (uint32, fuse.Status) {
	if a.private(h.NodeId) {
		return 0, a.deny(h)
	}
	return a.RawFileSystem.GetXAttr(cancel, h, name, dest)
}

// ListXAttr authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) ListXAttr(cancel <-chan struct{}, h *fuse.InHeader, dest []byte) (uint32, fuse.Status) {
	if a.private(h.NodeId) {
		return 0, a.deny(h)
	}
	return a.RawFileSystem.ListXAttr(cancel, h, dest)
}

// SetXAttr authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) SetXAttr(cancel <-chan struct{}, in *fuse.SetXAttrIn, name string, data []byte) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.SetXAttr(cancel, in, name, data)
}

// RemoveXAttr authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) RemoveXAttr(cancel <-chan struct{}, h *fuse.InHeader, name string) fuse.Status {
	if a.private(h.NodeId) {
		return a.deny(h)
	}
	return a.RawFileSystem.RemoveXAttr(cancel, h, name)
}

// Lseek authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Lseek(cancel <-chan struct{}, in *fuse.LseekIn, out *fuse.LseekOut) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Lseek(cancel, in, out)
}

// GetLk authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) GetLk(cancel <-chan struct{}, in *fuse.LkIn, out *fuse.LkOut) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.GetLk(cancel, in, out)
}

// SetLk authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) SetLk(cancel <-chan struct{}, in *fuse.LkIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.SetLk(cancel, in)
}

// SetLkw authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) SetLkw(cancel <-chan struct{}, in *fuse.LkIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.SetLkw(cancel, in)
}

// CopyFileRange authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) CopyFileRange(cancel <-chan struct{}, in *fuse.CopyFileRangeIn) (uint32, fuse.Status) {
	if a.private(in.NodeId) || a.private(in.NodeIdOut) {
		return 0, a.deny(&in.InHeader)
	}
	return a.RawFileSystem.CopyFileRange(cancel, in)
}

// Ioctl authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Ioctl(cancel <-chan struct{}, in *fuse.IoctlIn, inbuf []byte, out *fuse.IoctlOut, outbuf []byte) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Ioctl(cancel, in, inbuf, out, outbuf)
}

// Flush authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Flush(cancel <-chan struct{}, in *fuse.FlushIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Flush(cancel, in)
}

// Fsync authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Fsync(cancel <-chan struct{}, in *fuse.FsyncIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Fsync(cancel, in)
}

// Fallocate authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Fallocate(cancel <-chan struct{}, in *fuse.FallocateIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Fallocate(cancel, in)
}

// OpenDir authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) OpenDir(cancel <-chan struct{}, in *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.OpenDir(cancel, in, out)
}

// ReadDir authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) ReadDir(cancel <-chan struct{}, in *fuse.ReadIn, out *fuse.DirEntryList) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.ReadDir(cancel, in, out)
}

// ReadDirPlus authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) ReadDirPlus(cancel <-chan struct{}, in *fuse.ReadIn, out *fuse.DirEntryList) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.ReadDirPlus(cancel, in, out)
}

// FsyncDir authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) FsyncDir(cancel <-chan struct{}, in *fuse.FsyncIn) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.FsyncDir(cancel, in)
}

// StatFs authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) StatFs(cancel <-chan struct{}, h *fuse.InHeader, out *fuse.StatfsOut) fuse.Status {
	if a.privateRoot && h.NodeId == 1 {
		if _, status := a.native(h, "STATFS"); status != fuse.OK {
			return status
		}
		*out = fuse.StatfsOut{Bsize: 4096, Frsize: 4096, NameLen: 255}
		return fuse.OK
	}
	if a.private(h.NodeId) {
		return a.deny(h)
	}
	return a.RawFileSystem.StatFs(cancel, h, out)
}

// Statx authenticates and refuses private-inode operations outside the transport.
func (a *PrivateControl) Statx(cancel <-chan struct{}, in *fuse.StatxIn, out *fuse.StatxOut) fuse.Status {
	if a.private(in.NodeId) {
		return a.deny(&in.InHeader)
	}
	return a.RawFileSystem.Statx(cancel, in, out)
}
