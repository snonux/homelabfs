//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

// nativeRequestBytes is the fixed read/write cap advertised through go-fuse's
// MaxWrite option. Services must admit the whole request at one state cut.
const nativeRequestBytes = 64 << 10

// Caller identifies one kernel request. Thread may be a native thread ID, not
// the process leader PID. Unique is scoped to this FUSE connection.
type Caller struct {
	UID, GID, Thread uint32
	Unique           uint64
}

// Identity contains an exact request-bound credential snapshot and a nonzero
// process-incarnation token. ProcessID is its native process leader ID (used
// only for advisory conflict reporting). The provider must never reuse Process
// within this mount session, including after PID reuse or descriptor transfer.
type Identity struct {
	Credentials fsapi.Cred
	Process     uint64
	ProcessID   uint32
}

// Credentials supplies complete request-bound groups and process identity.
// Header UID/GID/PID, /proc account lookups or daemon credentials alone cannot
// implement this contract for arbitrary changing workload credentials.
type Credentials interface {
	Snapshot(context.Context, Caller) (Identity, error)
}

// Config admits only an explicitly named disposable local service. Credentials
// is mandatory even in validation. No built-in account/group discovery is used.
type Config struct {
	Service     *fsservice.Service
	Validation  fsservice.ValidationProfile
	Credentials Credentials
	// BufferedValidation admits same-connection buffered I/O and mmap only for
	// disposable fixtures. Kernel writeback-cache negotiation remains disabled.
	BufferedValidation bool
	// MultiuserValidation admits allow_other solely for root-owned synthetic
	// fixed-credential participants; it requires BufferedValidation.
	MultiuserValidation bool
	// MaxNodes bounds each retained per-generation node/process/owner identity
	// map, including forgotten objects and exited processes; it is not active-only.
	MaxNodes, MaxHandles, MaxRequests int
}

type node struct {
	object  fsapi.ObjectID
	lookups uint64
}
type description struct {
	mu         sync.Mutex
	object     fsapi.ObjectID
	handle     fsapi.OpenHandle
	generation fsapi.UUID
	directory  bool
	grant      fsapi.Cred
	writable   bool
	// Directory cookies retain raw exclusive cursors, never lookup references.
	revision uint64
	cursors  []string
}

// Adapter owns one generation's node and description tables. It must not be
// reused after unmount or service recovery. Service and roots remain caller-owned.
type Adapter struct {
	fuse.RawFileSystem
	service              fsapi.Filesystem
	publication          *fsservice.Service
	credentials          Credentials
	info                 fsservice.FrontendInfo
	session              fsapi.UUID
	mu                   sync.Mutex
	openMu               sync.Mutex
	nodes                map[uint64]*node
	objects              map[fsapi.ObjectID]uint64
	handles              map[uint64]*description
	writers              map[fsapi.ObjectID]fsapi.Cred
	processes            map[uint64]uint32
	nativeOwners         map[uint64]uint64
	processOwners        map[uint64]uint64
	nextNode, nextHandle uint64
	maxNodes, maxHandles int
	slots                chan struct{}
	blocked              atomic.Bool
	failure              error
	mountStarted         bool
	buffered             bool
	multiuser            bool
}

var _ fuse.RawFileSystem = (*Adapter)(nil)

// New constructs the restricted adapter, rejecting normal service publication.
// Adding normal mounts requires verified credential/traversal/cache admission,
// plus the real Protocol already required by fsservice normal construction.
func New(c Config) (*Adapter, error) {
	if c.MultiuserValidation && (!c.BufferedValidation || !multiuserAvailable()) {
		return nil, fmt.Errorf("%w: multiuser validation requires root and buffered validation", fsapi.Unsupported)
	}
	if c.Service == nil || c.Credentials == nil {
		return nil, fmt.Errorf("%w: service and credentials required", fsapi.Invalid)
	}
	info := c.Service.FrontendInfo()
	if !c.Validation.Disposable || c.Validation.Name == "" || c.Validation != info.Validation || !info.UnixPermissions {
		return nil, fmt.Errorf("%w: matching disposable validation and UnixPermissions required; normal publication gates are unverified", fsapi.Unsupported)
	}
	if info.IOBytes < nativeRequestBytes {
		return nil, fmt.Errorf("%w: service I/O cap %d is below native request cap %d", fsapi.Unsupported, info.IOBytes, nativeRequestBytes)
	}
	a, err := newAdapter(c, c.Service, info)
	if err != nil {
		return nil, err
	}
	if c.BufferedValidation {
		binder, ok := c.Credentials.(interface {
			BindMount(MountIdentity) error
			seal()
		})
		if !ok {
			return nil, fmt.Errorf("%w: buffered validation requires connection-bound controlled participants", fsapi.Unsupported)
		}
		if err := binder.BindMount(a.Identity()); err != nil {
			return nil, err
		}
	}
	a.publication = c.Service
	return a, nil
}

func newAdapter(c Config, service fsapi.Filesystem, info fsservice.FrontendInfo) (*Adapter, error) {
	if c.MaxNodes == 0 {
		c.MaxNodes = 4096
	}
	if c.MaxHandles == 0 {
		c.MaxHandles = 1024
	}
	if c.MaxRequests == 0 {
		c.MaxRequests = 32
	}
	if c.MaxNodes < 1 || c.MaxHandles < 1 || c.MaxRequests < 1 || c.MaxNodes > 65536 || c.MaxHandles > 65536 || c.MaxRequests > 256 {
		return nil, fsapi.Invalid
	}
	a := &Adapter{buffered: c.BufferedValidation, multiuser: c.MultiuserValidation, RawFileSystem: fuse.NewDefaultRawFileSystem(), service: service, credentials: c.Credentials, info: info, nodes: map[uint64]*node{1: {object: info.Root}}, objects: map[fsapi.ObjectID]uint64{info.Root: 1}, handles: make(map[uint64]*description), writers: make(map[fsapi.ObjectID]fsapi.Cred), processes: make(map[uint64]uint32), nativeOwners: make(map[uint64]uint64), processOwners: make(map[uint64]uint64), nextNode: 1, maxNodes: c.MaxNodes, maxHandles: c.MaxHandles, slots: make(chan struct{}, c.MaxRequests)}
	if _, err := rand.Read(a.session[:]); err != nil {
		return nil, fmt.Errorf("mount session: %w", err)
	}
	return a, nil
}

// Err reports a callback cleanup failure that could not be returned to the
// kernel (FORGET/RELEASE have no error reply). Failures close adapter admission.
func (a *Adapter) Err() error { a.mu.Lock(); defer a.mu.Unlock(); return a.failure }

func (a *Adapter) fail(err error) {
	a.blocked.Store(true)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failure = errors.Join(a.failure, err)
}

func (a *Adapter) identity(ctx context.Context, h *fuse.InHeader) (Identity, error) {
	i, err := a.credentials.Snapshot(ctx, Caller{UID: h.Uid, GID: h.Gid, Thread: h.Pid, Unique: h.Unique})
	if err != nil {
		return Identity{}, err
	}
	if i.Process == 0 || i.ProcessID == 0 || i.Credentials.UID != h.Uid || i.Credentials.GID != h.Gid || i.Credentials.PID != h.Pid || len(i.Credentials.Groups) > 1024 {
		return Identity{}, fsapi.Permission
	}
	a.mu.Lock()
	pid, exists := a.processes[i.Process]
	if exists && pid != i.ProcessID {
		a.mu.Unlock()
		return Identity{}, fsapi.Stale
	}
	if !exists && len(a.processes) >= a.maxNodes {
		a.mu.Unlock()
		return Identity{}, fsapi.NoSpace
	}
	a.processes[i.Process] = i.ProcessID
	a.mu.Unlock()
	i.Credentials = i.Credentials.Clone()
	return i, nil
}

func (a *Adapter) call(cancel <-chan struct{}, h *fuse.InHeader, c fsapi.Command) (fsapi.Result, fuse.Status) {
	r, code, _ := a.callIssued(cancel, h, c)
	return r, code
}

// callIssued returns the exact identity used at open admission, so a later
// cache write can use only the authorization of that issued description.
func (a *Adapter) callIssued(cancel <-chan struct{}, h *fuse.InHeader, c fsapi.Command) (fsapi.Result, fuse.Status, Identity) {
	if a.blocked.Load() {
		return fsapi.Result{}, status(fsapi.Unavailable), Identity{}
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		return fsapi.Result{}, status(fsapi.WouldBlock), Identity{}
	}
	ctx := &fuse.Context{Caller: h.Caller, Cancel: cancel}
	i, err := a.identity(ctx, h)
	if err != nil {
		return fsapi.Result{}, errorStatus(err), Identity{}
	}
	if a.buffered && c.Operation == fsapi.Open && c.Flags.Access != fsapi.ReadOnly {
		a.mu.Lock()
		previous, exists := a.writers[c.Object]
		a.mu.Unlock()
		groups := slices.Clone(i.Credentials.Groups)
		slices.Sort(groups)
		if exists && (previous.UID != i.Credentials.UID || previous.GID != i.Credentials.GID || !slices.Equal(previous.Groups, groups)) {
			return fsapi.Result{}, status(fsapi.Unsupported), Identity{}
		}
	}
	r, code := a.submit(ctx, i.Credentials, c)
	return r, code, i
}

func (a *Adapter) callAuthorized(ctx context.Context, cred fsapi.Cred, c fsapi.Command) (fsapi.Result, fuse.Status) {
	if a.blocked.Load() {
		return fsapi.Result{}, status(fsapi.Unavailable)
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		return fsapi.Result{}, status(fsapi.WouldBlock)
	}
	return a.submit(ctx, cred, c)
}

func (a *Adapter) submit(ctx context.Context, cred fsapi.Cred, c fsapi.Command) (fsapi.Result, fuse.Status) {
	r, err := a.dispatch(ctx, cred, c)
	if err != nil {
		return fsapi.Result{}, errorStatus(err)
	}
	// Never abandon references or an admitted mutation on interruption.
	result, err := r.Completion().Wait(context.Background())
	if err != nil {
		return fsapi.Result{}, errorStatus(err)
	}
	if result.Code == fsapi.OK && result.Generation != a.info.Generation {
		a.fail(fsapi.Stale)
		return fsapi.Result{}, status(fsapi.Stale)
	}
	return result, status(result.Code)
}

func (a *Adapter) dispatch(ctx context.Context, cred fsapi.Cred, c fsapi.Command) (*fsapi.Request, error) {
	c.Mount = a.info.Volume
	c.Generation = a.info.Generation
	r := fsapi.NewRequest(cred)
	if err := a.service.Dispatch(ctx, r, c); err != nil {
		return nil, err
	}
	return r, nil
}

func (a *Adapter) cleanup(c fsapi.Command) error {
	// These are transport-owned reference drains with no workload caller.
	// Zero credential fields grant no policy bypass: UnixPermissions explicitly
	// permits only these cleanup operations regardless of user identity.
	switch c.Operation {
	case fsapi.Forget, fsapi.Release, fsapi.SessionExit:
	default:
		return fsapi.Invalid
	}
	return a.control(fsapi.Cred{}, c)
}

func (a *Adapter) object(id uint64) (fsapi.ObjectID, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n, ok := a.nodes[id]
	if !ok {
		return fsapi.ObjectID{}, false
	}
	return n.object, true
}
func (a *Adapter) description(id, nodeID uint64, directory bool) (*description, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, ok := a.handles[id]
	n := a.nodes[nodeID]
	return d, ok && n != nil && d.object == n.object && d.generation == a.info.Generation && d.directory == directory
}

func (a *Adapter) entry(r fsapi.Result, out *fuse.EntryOut) fuse.Status {
	if r.Attr == nil {
		a.fail(fsapi.IO)
		return fuse.EIO
	}
	a.mu.Lock()
	id, exists := a.objects[r.Attr.Object]
	if !exists {
		if len(a.nodes) >= a.maxNodes {
			a.mu.Unlock()
			if err := a.cleanup(fsapi.Command{Operation: fsapi.Forget, Object: r.Attr.Object, Count: 1}); err != nil {
				a.fail(err)
			}
			return status(fsapi.NoSpace)
		}
		a.nextNode++
		id = a.nextNode
		a.nodes[id] = &node{object: r.Attr.Object}
		a.objects[r.Attr.Object] = id
	}
	a.nodes[id].lookups++
	a.mu.Unlock()
	*out = fuse.EntryOut{NodeId: id, Generation: 1, Attr: attr(*r.Attr, id)} // zero entry/attribute TTLs
	return fuse.OK
}

func (a *Adapter) addHandle(r fsapi.Result, directory bool, identity Identity, flags fsapi.OpenFlags, out *fuse.OpenOut) fuse.Status {
	if r.Attr == nil || r.Handle == 0 || r.Generation != a.info.Generation {
		a.fail(fsapi.IO)
		return fuse.EIO
	}
	a.mu.Lock()
	if len(a.handles) >= a.maxHandles {
		a.mu.Unlock()
		if err := a.cleanup(fsapi.Command{Operation: fsapi.Release, Handle: r.Handle, DescriptionOnly: true}); err != nil {
			a.fail(err)
		}
		return status(fsapi.NoSpace)
	}
	// Linux selects a writable description from the inode, not necessarily
	// the fd that created a dirty mapping. Never mix distinct writer credentials
	// for one object during this connection, even after the first fd closes.
	if a.buffered && !directory && flags.Access != fsapi.ReadOnly {
		cred := identity.Credentials.Clone()
		slices.Sort(cred.Groups)
		previous, exists := a.writers[r.Attr.Object]
		if exists && (previous.UID != cred.UID || previous.GID != cred.GID || !slices.Equal(previous.Groups, cred.Groups)) || !exists && len(a.writers) >= a.maxNodes {
			a.mu.Unlock()
			if err := a.cleanup(fsapi.Command{Operation: fsapi.Release, Handle: r.Handle, DescriptionOnly: true}); err != nil {
				a.fail(err)
			}
			return status(fsapi.Unsupported)
		}
		a.writers[r.Attr.Object] = cred
	}
	a.nextHandle++
	id := a.nextHandle
	a.handles[id] = &description{object: r.Attr.Object, handle: r.Handle, generation: r.Generation, directory: directory, grant: identity.Credentials.Clone(), writable: flags.Access != fsapi.ReadOnly, cursors: []string{""}}
	a.mu.Unlock()
	*out = fuse.OpenOut{Fh: id}
	if !directory && !a.buffered {
		out.OpenFlags = fuse.FOPEN_DIRECT_IO
	}
	return fuse.OK
}
