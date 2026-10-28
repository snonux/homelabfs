package fsservice

import (
	"context"
	"math"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func owner(f *fixture, n uint64) fsapi.LockOwner {
	return fsapi.LockOwner{Session: uid(51000), Generation: f.c.Workspace.ID, Token: n}
}
func lockCommand(op fsapi.Operation, h fsapi.OpenHandle, o fsapi.LockOwner, domain fsapi.LockDomain, kind fsapi.LockKind, start, length int64) fsapi.Command {
	return fsapi.Command{Operation: op, Handle: h, Lock: fsapi.Lock{Domain: domain, Kind: kind, Owner: o, Start: start, Length: length}}
}
func assertCode(t *testing.T, r fsapi.Result, want fsapi.Errno) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("code %v want %v", r.Code, want)
	}
}
func pendingLock(f *fixture, c fsapi.Command) *fsapi.Request {
	f.t.Helper()
	c.Mount = f.c.Storage.Volume
	c.Generation = f.c.Workspace.ID
	r := fsapi.NewRequest(fsapi.Cred{})
	if err := f.service.Dispatch(f.t.Context(), r, c); err != nil {
		f.t.Fatal(err)
	}
	// A following command is a worker barrier; the blocked lock cannot hold it.
	f.ok(fsapi.Command{Operation: fsapi.Statfs})
	select {
	case <-r.Completion().Done():
		f.t.Fatal("contended lock completed")
	default:
	}
	return r
}
func awaitLock(t *testing.T, r *fsapi.Request, want fsapi.Errno) {
	t.Helper()
	v, e := r.Completion().Wait(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	assertCode(t, v, want)
}

func TestPOSIXRangesAliasesConversionAndCloseAnyDescriptor(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	id := a.Attr.Object
	f.ok(fsapi.Command{Operation: fsapi.LinkOp, Object: id, Destination: f.c.Storage.Root, NewName: "alias"})
	alias := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: f.c.Storage.Root, Name: "alias"})
	b := f.ok(fsapi.Command{Operation: fsapi.Open, Object: alias.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
	p, q := owner(f, 1), owner(f, 2)
	cmd := func(op fsapi.Operation, h fsapi.OpenHandle, o fsapi.LockOwner, k fsapi.LockKind, start, length int64) fsapi.Command {
		return lockCommand(op, h, o, fsapi.POSIXLock, k, start, length)
	}
	f.ok(cmd(fsapi.TryLock, a.Handle, p, fsapi.ExclusiveLock, 10, 20))
	f.ok(cmd(fsapi.TryLock, b.Handle, q, fsapi.SharedLock, 0, 10))
	assertCode(t, f.request(cmd(fsapi.TryLock, b.Handle, q, fsapi.SharedLock, 29, 1)), fsapi.WouldBlock)
	f.ok(cmd(fsapi.TryLock, b.Handle, p, fsapi.SharedLock, 15, 5)) // replace middle, split old exclusive range
	query := f.ok(cmd(fsapi.GetLock, b.Handle, q, fsapi.ExclusiveLock, 15, 5))
	if query.Conflict == nil || query.Conflict.Lock.Kind != fsapi.SharedLock || query.Conflict.Lock.Start != 15 {
		t.Fatal(query)
	}
	f.ok(cmd(fsapi.TryLock, b.Handle, q, fsapi.SharedLock, 15, 5))
	assertCode(t, f.request(cmd(fsapi.TryLock, b.Handle, q, fsapi.ExclusiveLock, 0, 30)), fsapi.WouldBlock)
	// Failed POSIX conversion preserves q's old shared range.
	assertCode(t, f.request(cmd(fsapi.TryLock, a.Handle, p, fsapi.ExclusiveLock, 15, 5)), fsapi.WouldBlock)
	f.ok(cmd(fsapi.Unlock, a.Handle, p, fsapi.ExclusiveLock, 0, 0))
	f.ok(cmd(fsapi.TryLock, a.Handle, p, fsapi.SharedLock, 100, 0))
	assertCode(t, f.request(cmd(fsapi.TryLock, b.Handle, q, fsapi.ExclusiveLock, math.MaxInt64, 0)), fsapi.WouldBlock)
	f.ok(fsapi.Command{Operation: fsapi.Release, Handle: b.Handle, Lock: fsapi.Lock{Owner: p}}) // closing an unrelated fd removes all p ranges
	f.ok(cmd(fsapi.TryLock, a.Handle, q, fsapi.ExclusiveLock, 100, 0))
	if r := f.ok(cmd(fsapi.GetLock, a.Handle, p, fsapi.ExclusiveLock, 100, 0)); r.Conflict == nil || r.Conflict.Lock.Owner != q {
		t.Fatal(r)
	}
}

func TestFlockOpenDescriptionDomainAndConversion(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	b := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	p, q := owner(f, 1), owner(f, 2)
	cmd := func(op fsapi.Operation, h fsapi.OpenHandle, o fsapi.LockOwner, k fsapi.LockKind) fsapi.Command {
		return lockCommand(op, h, o, fsapi.FlockLock, k, 0, 0)
	}
	f.ok(cmd(fsapi.TryLock, a.Handle, p, fsapi.SharedLock))
	f.ok(cmd(fsapi.TryLock, b.Handle, p, fsapi.SharedLock))
	assertCode(t, f.request(cmd(fsapi.TryLock, a.Handle, p, fsapi.ExclusiveLock)), fsapi.WouldBlock)
	// BSD conversion drops a's shared lock, so b can upgrade now.
	f.ok(cmd(fsapi.TryLock, b.Handle, q, fsapi.ExclusiveLock))
	// Process-scoped fcntl and open-description flock domains are independent.
	f.ok(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 0))
	f.ok(fsapi.Command{Operation: fsapi.ProcessExit, Lock: fsapi.Lock{Owner: q}})
	assertCode(t, f.request(cmd(fsapi.TryLock, a.Handle, p, fsapi.SharedLock)), fsapi.WouldBlock) // process exit alone doesn't close shared descriptions
	f.ok(fsapi.Command{Operation: fsapi.Release, Handle: b.Handle, Lock: fsapi.Lock{Owner: p}})
	f.ok(cmd(fsapi.TryLock, a.Handle, p, fsapi.ExclusiveLock))
}

func TestBlockingLocksCancellationExitAndClose(t *testing.T) {
	for _, domain := range []fsapi.LockDomain{fsapi.POSIXLock, fsapi.FlockLock} {
		t.Run(map[fsapi.LockDomain]string{fsapi.POSIXLock: "fcntl", fsapi.FlockLock: "flock"}[domain], func(t *testing.T) {
			f := setup(t, "sim")
			f.start()
			a := f.create(f.c.Storage.Root, "file")
			b := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
			p, q := owner(f, 1), owner(f, 2)
			f.ok(lockCommand(fsapi.TryLock, a.Handle, p, domain, fsapi.ExclusiveLock, 0, 0))
			c := lockCommand(fsapi.WaitLock, b.Handle, q, domain, fsapi.ExclusiveLock, 0, 0)
			c.Lock.WaitID = uid(52001)
			pending := pendingLock(f, c)
			canceled, stop := context.WithCancel(t.Context())
			stop()
			if _, err := pending.Completion().Wait(canceled); err == nil {
				t.Fatal("wait should cancel")
			}
			f.ok(fsapi.Command{Operation: fsapi.CancelLock, Lock: c.Lock})
			awaitLock(t, pending, fsapi.Interrupted)
			f.ok(lockCommand(fsapi.Unlock, a.Handle, p, domain, fsapi.ExclusiveLock, 0, 0))
			if r := f.ok(lockCommand(fsapi.GetLock, a.Handle, p, domain, fsapi.ExclusiveLock, 0, 0)); r.Conflict != nil {
				t.Fatal("canceled waiter acquired", r)
			}
			assertCode(t, f.request(c), fsapi.Invalid) // token cannot acquire a second time
			f.ok(lockCommand(fsapi.TryLock, a.Handle, p, domain, fsapi.ExclusiveLock, 0, 0))
			c.Lock.WaitID = uid(52002)
			pending = pendingLock(f, c)
			f.ok(lockCommand(fsapi.Unlock, a.Handle, p, domain, fsapi.ExclusiveLock, 0, 0))
			awaitLock(t, pending, fsapi.OK)
			f.ok(fsapi.Command{Operation: fsapi.CancelLock, Lock: c.Lock})
			awaitLock(t, pending, fsapi.OK) // grant won; adapter must release it
			f.ok(lockCommand(fsapi.Unlock, b.Handle, q, domain, fsapi.ExclusiveLock, 0, 0))
			f.ok(lockCommand(fsapi.TryLock, a.Handle, p, domain, fsapi.ExclusiveLock, 0, 0))
			c.Lock.WaitID = uid(52003)
			pending = pendingLock(f, c)
			f.ok(fsapi.Command{Operation: fsapi.ProcessExit, Lock: fsapi.Lock{Owner: q}})
			awaitLock(t, pending, fsapi.Interrupted)
			assertCode(t, f.request(lockCommand(fsapi.TryLock, b.Handle, q, domain, fsapi.SharedLock, 0, 0)), fsapi.Stale)
			c.Lock.Owner = owner(f, 3)
			c.Lock.WaitID = uid(52004)
			pending = pendingLock(f, c)
			f.ok(fsapi.Command{Operation: fsapi.Release, Handle: b.Handle, Lock: fsapi.Lock{Owner: owner(f, 3)}})
			awaitLock(t, pending, fsapi.Interrupted)
		})
	}
}

func TestLockNegativeAdmissionAndSessionRetirement(t *testing.T) {
	f := setup(t, "sim")
	f.c.Limits.Locks = 3
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	p := owner(f, 1)
	for _, change := range []func(*fsapi.Command){func(c *fsapi.Command) { c.Handle = 0 }, func(c *fsapi.Command) { c.Lock.Owner.Generation = uid(555) }, func(c *fsapi.Command) { c.Lock.Start = -1 }, func(c *fsapi.Command) { c.Lock.Start = math.MaxInt64; c.Lock.Length = 2 }, func(c *fsapi.Command) { c.Lock.Domain = fsapi.FlockLock; c.Lock.Length = 1 }} {
		c := lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 1)
		change(&c)
		r := f.request(c)
		if r.Code != fsapi.Invalid && r.Code != fsapi.Stale {
			t.Fatal(r)
		}
	}
	f.ok(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 1))
	assertCode(t, f.request(fsapi.Command{Operation: fsapi.Release, Handle: a.Handle}), fsapi.Invalid)
	f.ok(fsapi.Command{Operation: fsapi.SessionExit, Lock: fsapi.Lock{Owner: p}})
	assertCode(t, f.request(lockCommand(fsapi.TryLock, a.Handle, owner(f, 999), fsapi.POSIXLock, fsapi.SharedLock, 0, 1)), fsapi.Stale)
	f.ok(fsapi.Command{Operation: fsapi.Release, Handle: a.Handle})
}

func TestDescriptorClosePreservesSharedDescriptionFlock(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	p, q := owner(f, 1), owner(f, 2)
	f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("shared")})
	f.ok(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 0))
	f.ok(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.FlockLock, fsapi.ExclusiveLock, 0, 0))
	// One duplicate fd closes; another fd still owns this description.
	f.ok(fsapi.Command{Operation: fsapi.DescriptorClose, Handle: a.Handle, Lock: fsapi.Lock{Owner: p}})
	f.ok(lockCommand(fsapi.TryLock, a.Handle, q, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 0))
	b := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
	assertCode(t, f.request(lockCommand(fsapi.TryLock, b.Handle, q, fsapi.FlockLock, fsapi.SharedLock, 0, 0)), fsapi.WouldBlock)
	f.data(a.Handle, []byte("shared"))
	f.ok(fsapi.Command{Operation: fsapi.Release, Handle: a.Handle, Lock: fsapi.Lock{Owner: q}})
	f.ok(lockCommand(fsapi.TryLock, b.Handle, q, fsapi.FlockLock, fsapi.ExclusiveLock, 0, 0))
}

func TestLockRangeCapacityAtomicityAndWaiterShutdown(t *testing.T) {
	f := setup(t, "sim")
	f.c.Limits.Locks = 2
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	p, q := owner(f, 1), owner(f, 2)
	f.ok(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 100))
	f.ok(lockCommand(fsapi.Unlock, a.Handle, p, fsapi.POSIXLock, 0, 25, 50))
	assertCode(t, f.request(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.SharedLock, 0, 1)), fsapi.NoSpace)
	r := f.ok(lockCommand(fsapi.GetLock, a.Handle, q, fsapi.POSIXLock, fsapi.SharedLock, 0, 1))
	if r.Conflict == nil || r.Conflict.Lock.Kind != fsapi.ExclusiveLock {
		t.Fatal("capacity failure replaced existing range", r)
	}
	c := lockCommand(fsapi.WaitLock, a.Handle, q, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 1)
	c.Lock.WaitID = uid(57000)
	pending := pendingLock(f, c)
	assertCode(t, f.request(fsapi.Command{Operation: fsapi.Release, Handle: a.Handle}), fsapi.Invalid)
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	awaitLock(t, pending, fsapi.Unavailable)
}

func TestLockModeAndChmodPreservedDescriptorAccess(t *testing.T) {
	f := setup(t, "sim")
	policy, err := NewUnixPermissions(uid(58000))
	if err != nil {
		t.Fatal(err)
	}
	f.c.Permissions = policy
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	p := owner(f, 1)
	read := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	write := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.WriteOnly}})
	mode := uint16(0)
	f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: a.Attr.Object, Attributes: fsapi.Attributes{Mode: &mode}})
	assertCode(t, f.request(lockCommand(fsapi.TryLock, read.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 1)), fsapi.Permission)
	assertCode(t, f.request(lockCommand(fsapi.TryLock, write.Handle, p, fsapi.POSIXLock, fsapi.SharedLock, 0, 1)), fsapi.Permission)
	f.ok(lockCommand(fsapi.TryLock, a.Handle, p, fsapi.POSIXLock, fsapi.ExclusiveLock, 0, 1))
	f.ok(lockCommand(fsapi.TryLock, read.Handle, p, fsapi.FlockLock, fsapi.ExclusiveLock, 0, 0))
}
