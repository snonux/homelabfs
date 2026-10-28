//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"crypto/rand"
	"math"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// GetLk queries the shared service domain, including hard-link aliases.
func (a *Adapter) GetLk(cancel <-chan struct{}, in *fuse.LkIn, out *fuse.LkOut) fuse.Status {
	r, s := a.lock(cancel, in, fsapi.GetLock)
	if s != fuse.OK {
		return s
	}
	out.Lk = fuse.FileLock{Typ: syscall.F_UNLCK}
	if r.Conflict != nil {
		l := r.Conflict.Lock
		end := uint64(math.MaxInt64)
		if l.Length != 0 {
			end = uint64(l.Start + l.Length - 1)
		}
		typ := uint32(syscall.F_RDLCK)
		if l.Kind == fsapi.ExclusiveLock {
			typ = syscall.F_WRLCK
		}
		a.mu.Lock()
		pid := a.processes[l.Owner.Token]
		a.mu.Unlock()
		out.Lk = fuse.FileLock{Start: uint64(l.Start), End: end, Typ: typ, Pid: pid}
	}
	return fuse.OK
}

// SetLk applies a nonblocking acquisition/conversion or unlock.
func (a *Adapter) SetLk(cancel <-chan struct{}, in *fuse.LkIn) fuse.Status {
	_, s := a.lock(cancel, in, fsapi.TryLock)
	return s
}

// SetLkw waits without holding the volume worker and explicitly cancels on
// interruption. An acquisition that wins cancellation is returned as success.
func (a *Adapter) SetLkw(cancel <-chan struct{}, in *fuse.LkIn) fuse.Status {
	_, s := a.lock(cancel, in, fsapi.WaitLock)
	return s
}

func (a *Adapter) owner(i Identity) fsapi.LockOwner {
	return fsapi.LockOwner{Session: a.session, Generation: a.info.Generation, Token: i.Process}
}

// bindPOSIXOwner binds the kernel's opaque file-table owner to an independently
// authenticated process incarnation. Never treat a native owner as a PID. This
// initial profile refuses token reuse, shared file tables across processes and
// file-table replacement within a process; those need a richer admitted native
// identity contract. Flock and RELEASE owner fields have a different meaning.
func (a *Adapter) bindPOSIXOwner(native uint64, i Identity) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if process, exists := a.nativeOwners[native]; exists && process != i.Process {
		return fsapi.Stale
	}
	if previous, exists := a.processOwners[i.Process]; exists && previous != native {
		return fsapi.Unsupported
	}
	if _, exists := a.nativeOwners[native]; !exists && len(a.nativeOwners) >= a.maxNodes {
		return fsapi.NoSpace
	}
	a.nativeOwners[native] = i.Process
	a.processOwners[i.Process] = native
	return nil
}

func (a *Adapter) lock(cancel <-chan struct{}, in *fuse.LkIn, operation fsapi.Operation) (fsapi.Result, fuse.Status) {
	if a.blocked.Load() {
		return fsapi.Result{}, status(fsapi.Unavailable)
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		return fsapi.Result{}, fuse.EAGAIN
	}
	d, ok := a.description(in.Fh, in.NodeId, false)
	if !ok {
		return fsapi.Result{}, status(fsapi.Stale)
	}
	ctx := &fuse.Context{Caller: in.Caller, Cancel: cancel}
	i, err := a.identity(ctx, &in.InHeader)
	if err != nil {
		return fsapi.Result{}, errorStatus(err)
	}
	l, s := nativeLock(in, a.owner(i))
	if s != fuse.OK {
		return fsapi.Result{}, s
	}
	if l.Domain == fsapi.POSIXLock {
		if err := a.bindPOSIXOwner(in.Owner, i); err != nil {
			return fsapi.Result{}, errorStatus(err)
		}
	}
	if in.Lk.Typ == syscall.F_UNLCK {
		operation = fsapi.Unlock
	}
	if operation == fsapi.WaitLock {
		if _, err = rand.Read(l.WaitID[:]); err != nil {
			return fsapi.Result{}, fuse.EIO
		}
	}
	c := fsapi.Command{Operation: operation, Object: d.object, Handle: d.handle, Lock: l}
	r, err := a.dispatch(ctx, i.Credentials, c)
	if err != nil {
		return fsapi.Result{}, errorStatus(err)
	}
	if operation != fsapi.WaitLock {
		v, e := r.Completion().Wait(context.Background())
		if e != nil {
			return fsapi.Result{}, errorStatus(e)
		}
		return v, status(v.Code)
	}
	v, err := r.Completion().Wait(ctx)
	if err == nil {
		return v, status(v.Code)
	}
	// CancelLock and the original completion must BOTH resolve. Admission can
	// be temporarily full when another frontend shares the same service.
	c.Operation = fsapi.CancelLock
	if err = a.control(i.Credentials, c); err != nil {
		a.fail(err)
		return fsapi.Result{}, fuse.EIO
	}
	v, err = r.Completion().Wait(context.Background())
	if err != nil {
		a.fail(err)
		return fsapi.Result{}, fuse.EIO
	}
	return v, status(v.Code)
}

func (a *Adapter) control(cred fsapi.Cred, c fsapi.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		r, err := a.dispatch(ctx, cred, c)
		if err == fsapi.NoSpace {
			timer := time.NewTimer(time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
				continue
			}
		}
		if err != nil {
			return err
		}
		v, err := r.Completion().Wait(context.Background())
		if err != nil {
			return err
		}
		if v.Code != fsapi.OK {
			return v.Code
		}
		return nil
	}
}

func nativeLock(in *fuse.LkIn, owner fsapi.LockOwner) (fsapi.Lock, fuse.Status) {
	l := fsapi.Lock{Owner: owner, Domain: fsapi.POSIXLock}
	if in.LkFlags & ^uint32(fuse.FUSE_LK_FLOCK) != 0 {
		return l, status(fsapi.Unsupported)
	}
	if in.LkFlags&fuse.FUSE_LK_FLOCK != 0 {
		l.Domain = fsapi.FlockLock
	}
	switch in.Lk.Typ {
	case syscall.F_RDLCK:
		l.Kind = fsapi.SharedLock
	case syscall.F_WRLCK:
		l.Kind = fsapi.ExclusiveLock
	case syscall.F_UNLCK:
	default:
		return l, fuse.EINVAL
	}
	if l.Domain == fsapi.POSIXLock {
		// FUSE has already normalized SEEK_CUR/END and negative native lengths.
		// Its inclusive EOF sentinel is signed OFF_MAX (upstream ToFlockT).
		if in.Lk.Start > math.MaxInt64 || in.Lk.End > math.MaxInt64 || in.Lk.End < in.Lk.Start {
			return l, fuse.EINVAL
		}
		l.Start = int64(in.Lk.Start)
		if in.Lk.End != math.MaxInt64 {
			l.Length = int64(in.Lk.End - in.Lk.Start + 1)
		}
	}
	return l, fuse.OK
}
