//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"math"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// Create returns distinct kernel lookup and open-description references.
func (a *Adapter) Create(cancel <-chan struct{}, in *fuse.CreateIn, name string, out *fuse.CreateOut) fuse.Status {
	if a.buffered {
		a.openMu.Lock()
		defer a.openMu.Unlock()
	}
	p, ok := a.object(in.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	f, s := openFlags(in.Flags, true)
	if s != fuse.OK {
		return s
	}
	// Linux can select any writable description for mmap cache writes. An
	// append description would make the service move the supplied page offset
	// to EOF, so buffered append is unsupported even for exclusive creates.
	if a.buffered && (f.Append || f.Truncate && !f.Exclusive) {
		return status(fsapi.Unsupported)
	}
	r, s, identity := a.callIssued(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.Create, Parent: p, Name: name, Flags: f, Mode: uint16(in.Mode & 07777), Umask: uint16(in.Umask & 0777)})
	if s != fuse.OK {
		return s
	}
	if s = a.addHandle(r, false, identity, f, &out.OpenOut); s != fuse.OK {
		if r.Attr != nil {
			if err := a.cleanup(fsapi.Command{Operation: fsapi.Forget, Object: r.Attr.Object, Count: 1}); err != nil {
				a.fail(err)
			}
		}
		return s
	}
	if s = a.entry(r, &out.EntryOut); s != fuse.OK {
		a.releaseHandle(out.Fh)
		return s
	}
	return fuse.OK
}

// Open checks fresh logical access; the default profile disables data caching.
func (a *Adapter) Open(cancel <-chan struct{}, in *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	return a.open(cancel, in, out, false)
}

// OpenDir acquires an independently releasable directory description.
func (a *Adapter) OpenDir(cancel <-chan struct{}, in *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	return a.open(cancel, in, out, true)
}

func (a *Adapter) open(cancel <-chan struct{}, in *fuse.OpenIn, out *fuse.OpenOut, directory bool) fuse.Status {
	if a.buffered {
		a.openMu.Lock()
		defer a.openMu.Unlock()
	}
	id, ok := a.object(in.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	f, s := openFlags(in.Flags, false)
	if s != fuse.OK {
		return s
	}
	// Reject before dispatch: O_APPEND|O_TRUNC must not truncate a file while
	// admitting a description that cannot safely supply mmap page offsets.
	if a.buffered && f.Append {
		return status(fsapi.Unsupported)
	}
	r, s, identity := a.callIssued(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: f})
	if s != fuse.OK {
		return s
	}
	if r.Attr != nil && (r.Attr.Kind == fsapi.Directory) != directory {
		if err := a.cleanup(fsapi.Command{Operation: fsapi.Release, Handle: r.Handle, DescriptionOnly: true}); err != nil {
			a.fail(err)
		}
		if directory {
			return fuse.ENOTDIR
		}
		return fuse.EISDIR
	}
	return a.addHandle(r, directory, identity, f, out)
}

// Read returns completion-owned bytes; no kernel input buffer escapes.
func (a *Adapter) Read(cancel <-chan struct{}, in *fuse.ReadIn, _ []byte) (fuse.ReadResult, fuse.Status) {
	d, ok := a.description(in.Fh, in.NodeId, false)
	if !ok {
		return nil, status(fsapi.Stale)
	}
	if in.Offset > math.MaxInt64 {
		return nil, fuse.EINVAL
	}
	r, s := a.call(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.Read, Object: d.object, Handle: d.handle, Offset: int64(in.Offset), Size: int64(in.Size)})
	if s != fuse.OK {
		return nil, s
	}
	return fuse.ReadResultData(r.Data), fuse.OK
}

// Write snapshots bytes at service admission and waits for the full service gate.
func (a *Adapter) Write(cancel <-chan struct{}, in *fuse.WriteIn, data []byte) (uint32, fuse.Status) {
	d, ok := a.description(in.Fh, in.NodeId, false)
	if !ok {
		return 0, status(fsapi.Stale)
	}
	if in.Offset > math.MaxInt64 || len(data) != int(in.Size) {
		return 0, fuse.EINVAL
	}
	command := fsapi.Command{Operation: fsapi.Write, Object: d.object, Handle: d.handle, Offset: int64(in.Offset), Data: data}
	var r fsapi.Result
	var s fuse.Status
	if in.WriteFlags&fuse.WRITE_CACHE != 0 {
		// Native cache writes have no trustworthy current workload identity. The
		// kernel names an existing writable description, possibly a different one
		// from the mapping's original fd. Use only its retained issued grant.
		if !a.buffered || !d.writable || d.grant.PID == 0 {
			return 0, fuse.EACCES
		}
		r, s = a.callAuthorized(context.Background(), d.grant, command)
	} else {
		r, s = a.call(cancel, &in.InHeader, command)
	}
	if s != fuse.OK {
		return 0, s
	}
	if r.Count > int64(len(data)) {
		a.fail(fsapi.IO)
		return 0, fuse.EIO
	}
	return uint32(r.Count), fuse.OK
}

// Flush implements per-fd close semantics, independently of final Release.
// Kernel LockOwner is not a PID or a service owner; the snapshot provider binds
// this actual request to its process incarnation (also after fd transfer).
func (a *Adapter) Flush(_ <-chan struct{}, in *fuse.FlushIn) fuse.Status {
	d, ok := a.description(in.Fh, in.NodeId, false)
	if !ok {
		return status(fsapi.Stale)
	}
	// A close event must drain its process locks even if its wait was signaled.
	ctx := &fuse.Context{Caller: in.Caller}
	i, err := a.identity(ctx, &in.InHeader)
	if err != nil {
		a.fail(err)
		return errorStatus(err)
	}
	if err := a.bindPOSIXOwner(in.LockOwner, i); err != nil {
		a.fail(err)
		return errorStatus(err)
	}
	err = a.control(i.Credentials, fsapi.Command{Operation: fsapi.DescriptorClose, Object: d.object, Handle: d.handle, Lock: fsapi.Lock{Owner: a.owner(i)}})
	if err != nil {
		a.fail(err)
		return errorStatus(err)
	}
	return fuse.OK
}

// Release is the kernel's final open-description event, including dup/fork.
// It cannot report an error to the application; Err records any failed cleanup.
func (a *Adapter) Release(_ <-chan struct{}, in *fuse.ReleaseIn) {
	_, ok := a.description(in.Fh, in.NodeId, false)
	if !ok {
		a.fail(fsapi.Stale)
		return
	}
	// Linux may issue asynchronous RELEASE with Pid=0, long after the
	// requesting task exits. This is a kernel-owned description drain, not a
	// new caller operation. Each actual fd close already supplied credentials
	// through FLUSH. Never repeat its process cleanup: the same process may
	// already have reacquired locks on another fd. No task-0 credentials exist.
	a.releaseHandle(in.Fh)
}

// ReleaseDir releases the final directory description.
func (a *Adapter) ReleaseDir(in *fuse.ReleaseIn) {
	if _, ok := a.description(in.Fh, in.NodeId, true); !ok {
		a.fail(fsapi.Stale)
		return
	}
	a.releaseHandle(in.Fh)
}

func (a *Adapter) releaseHandle(id uint64) {
	a.mu.Lock()
	d := a.handles[id]
	a.mu.Unlock()
	if d == nil {
		a.fail(fsapi.Stale)
		return
	}
	if err := a.cleanup(fsapi.Command{Operation: fsapi.Release, Object: d.object, Handle: d.handle, DescriptionOnly: true}); err != nil {
		a.fail(err)
		return
	}
	a.mu.Lock()
	delete(a.handles, id)
	a.mu.Unlock()
}

// Fsync orders earlier volume commands under the service's durability contract.
func (a *Adapter) Fsync(cancel <-chan struct{}, in *fuse.FsyncIn) fuse.Status {
	return a.sync(cancel, in, false)
}

// FsyncDir supplies an explicit namespace durability barrier.
func (a *Adapter) FsyncDir(cancel <-chan struct{}, in *fuse.FsyncIn) fuse.Status {
	return a.sync(cancel, in, true)
}

func (a *Adapter) sync(cancel <-chan struct{}, in *fuse.FsyncIn, directory bool) fuse.Status {
	d, ok := a.description(in.Fh, in.NodeId, directory)
	if !ok {
		return status(fsapi.Stale)
	}
	durability := fsapi.FileDurability
	if directory {
		durability = fsapi.DirectoryDurability
	} else if in.FsyncFlags&1 != 0 {
		durability = fsapi.DataDurability
	}
	_, s := a.call(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.Sync, Object: d.object, Handle: d.handle, Durability: durability})
	return s
}

// ReadDir binds every resumed native cookie to an exact directory revision.
// READDIRPLUS is disabled; plain entries acquire no lookup references.
func (a *Adapter) ReadDir(cancel <-chan struct{}, in *fuse.ReadIn, out *fuse.DirEntryList) fuse.Status {
	d, ok := a.description(in.Fh, in.NodeId, true)
	if !ok {
		return status(fsapi.Stale)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if in.Offset == 0 {
		d.revision = 0
		d.cursors = []string{""}
	}
	if in.Offset >= uint64(len(d.cursors)) {
		return status(fsapi.Stale)
	}
	r, s := a.call(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.Readdir, Object: d.object, Handle: d.handle, After: d.cursors[in.Offset], DirectoryRevision: d.revision, Limit: 128})
	if s != fuse.OK {
		return s
	}
	d.revision = r.DirectoryRevision
	index := int(in.Offset)
	for _, e := range r.Entries {
		// Service caps the complete namespace; also bound retained cookies.
		if index+1 >= a.maxNodes {
			return status(fsapi.NoSpace)
		}
		if !out.AddDirEntry(fuse.DirEntry{Name: e.Name, Mode: kindMode(e.Kind), Off: uint64(index + 1)}) {
			break
		}
		index++
		if index < len(d.cursors) {
			d.cursors[index] = e.Name
		} else {
			d.cursors = append(d.cursors, e.Name)
		}
	}
	return fuse.OK
}

// StatFs reports the service's admitted logical capacity, never host ZFS space.
func (a *Adapter) StatFs(cancel <-chan struct{}, h *fuse.InHeader, out *fuse.StatfsOut) fuse.Status {
	r, s := a.call(cancel, h, fsapi.Command{Operation: fsapi.Statfs})
	if s != fuse.OK {
		return s
	}
	if r.Capacity == nil {
		a.fail(fsapi.IO)
		return fuse.EIO
	}
	c := r.Capacity
	*out = fuse.StatfsOut{Blocks: c.Bytes / 4096, Bfree: c.BytesFree / 4096, Bavail: c.BytesFree / 4096, Files: c.Objects, Ffree: c.ObjectsFree, Bsize: 4096, Frsize: 4096, NameLen: 255}
	return fuse.OK
}
