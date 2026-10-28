//go:build linux || freebsd

package fuseadapter

import (
	"math"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// SetAttr preserves metadata operation atomicity. The service's separate
// Truncate and Setattr cannot atomically apply a combined size/metadata request,
// so that form and unsupported native fields are rejected before any effect.
func (a *Adapter) SetAttr(cancel <-chan struct{}, in *fuse.SetAttrIn, out *fuse.AttrOut) fuse.Status {
	id, ok := a.object(in.NodeId)
	if !ok {
		return status(fsapi.Stale)
	}
	allowed := uint32(fuse.FATTR_MODE | fuse.FATTR_UID | fuse.FATTR_GID | fuse.FATTR_SIZE | fuse.FATTR_ATIME | fuse.FATTR_MTIME | fuse.FATTR_FH | fuse.FATTR_ATIME_NOW | fuse.FATTR_MTIME_NOW | fuse.FATTR_LOCKOWNER)
	if in.Valid & ^allowed != 0 {
		return status(fsapi.Unsupported)
	}
	metadata := in.Valid & uint32(fuse.FATTR_MODE|fuse.FATTR_UID|fuse.FATTR_GID|fuse.FATTR_ATIME|fuse.FATTR_MTIME|fuse.FATTR_ATIME_NOW|fuse.FATTR_MTIME_NOW)
	if in.Valid&fuse.FATTR_SIZE != 0 && (metadata != 0 || in.Size > math.MaxInt64) {
		return status(fsapi.Unsupported)
	}
	c := fsapi.Command{Operation: fsapi.Setattr, Object: id}
	if in.Valid&fuse.FATTR_FH != 0 {
		a.mu.Lock()
		d := a.handles[in.Fh]
		a.mu.Unlock()
		if d == nil || d.object != id {
			return status(fsapi.Stale)
		}
		c.Handle = d.handle
	}
	if in.Valid&fuse.FATTR_SIZE != 0 {
		c.Operation = fsapi.Truncate
		c.Size = int64(in.Size)
	}
	if in.Valid&fuse.FATTR_MODE != 0 {
		v := uint16(in.Mode & 07777)
		c.Attributes.Mode = &v
	}
	if in.Valid&fuse.FATTR_UID != 0 {
		v := in.Uid
		c.Attributes.UID = &v
	}
	if in.Valid&fuse.FATTR_GID != 0 {
		v := in.Gid
		c.Attributes.GID = &v
	}
	if in.Valid&fuse.FATTR_ATIME_NOW != 0 {
		c.Attributes.AtimeNow = true
	} else if in.Valid&fuse.FATTR_ATIME != 0 {
		v := fsapi.Timestamp{Seconds: int64(in.Atime), Nanoseconds: in.Atimensec}
		c.Attributes.Atime = &v
	}
	if in.Valid&fuse.FATTR_MTIME_NOW != 0 {
		c.Attributes.MtimeNow = true
	} else if in.Valid&fuse.FATTR_MTIME != 0 {
		v := fsapi.Timestamp{Seconds: int64(in.Mtime), Nanoseconds: in.Mtimensec}
		c.Attributes.Mtime = &v
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

// Access is handled by mandatory default_permissions. If a platform forwards
// it anyway, reject it explicitly: the component API has no atomic access-check
// command, and no ENOSYS result may authorize future cached access requests.
func (a *Adapter) Access(_ <-chan struct{}, in *fuse.AccessIn) fuse.Status {
	if in.Mask & ^uint32(7) != 0 {
		return fuse.EINVAL
	}
	return status(fsapi.Unsupported)
}
