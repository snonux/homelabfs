//go:build linux || freebsd

package fuseadapter

import (
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// Lookup acquires exactly one service lookup reference for a successful entry.
func (a *Adapter) Lookup(cancel <-chan struct{}, h *fuse.InHeader, name string, out *fuse.EntryOut) fuse.Status {
	p, ok := a.object(h.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	r, s := a.call(cancel, h, fsapi.Command{Operation: fsapi.Lookup, Parent: p, Name: name})
	if s != fuse.OK {
		return s
	}
	return a.entry(r, out)
}

// Forget releases lookup references independently of open descriptions.
func (a *Adapter) Forget(id, count uint64) {
	a.mu.Lock()
	n := a.nodes[id]
	if n == nil || count == 0 || count > n.lookups {
		a.mu.Unlock()
		a.fail(fsapi.Stale)
		return
	}
	n.lookups -= count
	object := n.object
	a.mu.Unlock()
	if err := a.cleanup(fsapi.Command{Operation: fsapi.Forget, Object: object, Count: count}); err != nil {
		a.fail(err)
	}
}

// GetAttr always obtains fresh logical metadata, with zero cache TTL.
func (a *Adapter) GetAttr(cancel <-chan struct{}, in *fuse.GetAttrIn, out *fuse.AttrOut) fuse.Status {
	id, ok := a.object(in.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	c := fsapi.Command{Operation: fsapi.Getattr, Object: id}
	if in.Flags()&fuse.FUSE_GETATTR_FH != 0 {
		a.mu.Lock()
		d := a.handles[in.Fh()]
		a.mu.Unlock()
		if d == nil || d.object != id {
			return status(fsapi.Stale)
		}
		c.Handle = d.handle
	}
	r, s := a.call(cancel, &in.InHeader, c)
	if s != fuse.OK {
		return s
	}
	if r.Attr == nil {
		a.fail(fsapi.IO)
		return fuse.EIO
	}
	*out = fuse.AttrOut{Attr: attr(*r.Attr, in.NodeId)}
	return fuse.OK
}

// Mkdir creates a directory and acquires one lookup reference.
func (a *Adapter) Mkdir(cancel <-chan struct{}, in *fuse.MkdirIn, name string, out *fuse.EntryOut) fuse.Status {
	p, ok := a.object(in.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	r, s := a.call(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.Mkdir, Parent: p, Name: name, Mode: uint16(in.Mode & 07777), Umask: uint16(in.Umask & 0777)})
	if s != fuse.OK {
		return s
	}
	return a.entry(r, out)
}

// Symlink preserves the kernel's raw link text and name bytes.
func (a *Adapter) Symlink(cancel <-chan struct{}, h *fuse.InHeader, text, name string, out *fuse.EntryOut) fuse.Status {
	p, ok := a.object(h.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	r, s := a.call(cancel, h, fsapi.Command{Operation: fsapi.SymlinkOp, Parent: p, Name: name, Data: []byte(text)})
	if s != fuse.OK {
		return s
	}
	return a.entry(r, out)
}

// Readlink reads committed logical symlink text without host path traversal.
func (a *Adapter) Readlink(cancel <-chan struct{}, h *fuse.InHeader) ([]byte, fuse.Status) {
	id, ok := a.object(h.NodeId)
	if !ok {
		return nil, status(fsapi.Stale)
	}
	r, s := a.call(cancel, h, fsapi.Command{Operation: fsapi.Readlink, Object: id})
	return r.Data, s
}

// Unlink removes a name while existing descriptors retain their object.
func (a *Adapter) Unlink(cancel <-chan struct{}, h *fuse.InHeader, name string) fuse.Status {
	return a.remove(cancel, h, name, fsapi.Unlink)
}

// Rmdir removes an empty logical directory.
func (a *Adapter) Rmdir(cancel <-chan struct{}, h *fuse.InHeader, name string) fuse.Status {
	return a.remove(cancel, h, name, fsapi.Rmdir)
}

func (a *Adapter) remove(cancel <-chan struct{}, h *fuse.InHeader, name string, operation fsapi.Operation) fuse.Status {
	p, ok := a.object(h.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	_, s := a.call(cancel, h, fsapi.Command{Operation: operation, Parent: p, Name: name})
	return s
}

// Rename preserves one atomic same-volume service command. Only NOREPLACE is
// supported; EXCHANGE/WHITEOUT are explicitly rejected before mutation.
func (a *Adapter) Rename(cancel <-chan struct{}, in *fuse.RenameIn, oldName, newName string) fuse.Status {
	if in.Flags & ^uint32(1) != 0 {
		return status(fsapi.Unsupported)
	}
	p, ok := a.object(in.NodeId)
	d, destOK := a.object(in.Newdir)
	if !ok || !destOK {
		return status(fsapi.Stale)
	}
	_, s := a.call(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.RenameOp, Parent: p, Destination: d, DestinationMount: a.info.Volume, Name: oldName, NewName: newName, NoReplace: in.Flags&1 != 0})
	return s
}

// Link creates an alias without confusing its object identity with its name.
func (a *Adapter) Link(cancel <-chan struct{}, in *fuse.LinkIn, name string, out *fuse.EntryOut) fuse.Status {
	p, ok := a.object(in.NodeId)
	id, oldOK := a.object(in.Oldnodeid)
	if !ok || !oldOK {
		return status(fsapi.Stale)
	}
	_, s := a.call(cancel, &in.InHeader, fsapi.Command{Operation: fsapi.LinkOp, Object: id, Destination: p, DestinationMount: a.info.Volume, NewName: name})
	if s != fuse.OK {
		return s
	}
	// LinkOp acquires no lookup reference. A second, fresh Lookup does; a
	// concurrent replacement must never retarget the LINK reply to another ID.
	r, s := a.call(nil, &in.InHeader, fsapi.Command{Operation: fsapi.Lookup, Parent: p, Name: name})
	if s != fuse.OK {
		return s
	}
	if r.Attr == nil {
		a.fail(fsapi.IO)
		return fuse.EIO
	}
	if r.Attr.Object != id {
		if err := a.cleanup(fsapi.Command{Operation: fsapi.Forget, Object: r.Attr.Object, Count: 1}); err != nil {
			a.fail(err)
		}
		return status(fsapi.Stale)
	}
	return a.entry(r, out)
}
