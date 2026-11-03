//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/rand"
	"os"
	"slices"
	"sync/atomic"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type descriptorIdentity struct {
	Device, Inode, Size uint64
	Access              uint32
}
type connectionWitness interface{ check() error }
type nativeMount struct {
	witness      connectionWitness
	expected     [3]descriptorIdentity
	shell        *fuseadapter.Shell
	server       *fuse.Server
	files        [3]*os.File
	identity     string
	provisioner  processIdentity
	provisioning bool
}

type provisionKey struct{}
type provisionObservation struct {
	used   atomic.Bool
	owner  *Supervisor
	native guestbridge.Native
}

var _ guestbridge.NativeVerifier = (*Supervisor)(nil)
var _ fuseadapter.NativeCredentials = (*Supervisor)(nil)

// MountShell performs the native owned mount association. It is FreeBSD-only;
// Linux process/file fixtures can never call it to mint native authority.
// Failure preserves any created device, mount and shell for operator inspection.
func (s *Supervisor) MountShell(ctx context.Context, path string, shell *fuseadapter.Shell) (guestbridge.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.record.Stage != "started" || s.mount != nil || shell == nil {
		return guestbridge.Endpoint{}, fsapi.Unavailable
	}
	if err := s.child.live(); err != nil {
		return guestbridge.Endpoint{}, err
	}
	s.record.Stage = "mounting"
	if err := s.protect(); err != nil {
		return guestbridge.Endpoint{}, err
	}
	mounted, err := mountNative(ctx, path, shell)
	// Preserve even partially initialized native resources on failure.
	s.mount = mounted
	if err != nil {
		return guestbridge.Endpoint{}, err
	}
	endpoint, err := shell.Endpoint()
	if err != nil {
		return guestbridge.Endpoint{}, err
	}
	s.record.Endpoint = endpoint
	s.record.Mount = mounted.identity
	s.record.Stage = "mounted"
	if err = s.protect(); err != nil {
		return guestbridge.Endpoint{}, err
	}
	return endpoint, nil
}

// Enroll is called by the actual bridge constructor with its fresh controller.
// The endpoint must be this supervisor's real mounted shell, issued exactly once.
func (s *Supervisor) Enroll(ctx context.Context, endpoint guestbridge.Endpoint, controller fsapi.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !controller.Valid() || s.record.Stage != "mounted" || s.record.Controller.Valid() || endpoint != s.record.Endpoint {
		return fsapi.Permission
	}
	if err := s.checkLive(); err != nil {
		return err
	}
	s.record.Controller = controller
	s.record.Stage = "enrolled"
	return s.protect()
}

// Verify checks current native process, full groups, live connection and exact
// controller on every callback, including reads of previously protected results.
func (s *Supervisor) Verify(ctx context.Context, n guestbridge.Native, controller fsapi.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if controller != s.record.Controller || !controller.Valid() {
		return fsapi.Permission
	}
	return s.verifyNative(ctx, n)
}

// Snapshot derives process/groups from OS observations, never delegated payloads.
func (s *Supervisor) Snapshot(ctx context.Context, c fuseadapter.Caller, endpoint guestbridge.Endpoint) (guestbridge.Native, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return guestbridge.Native{}, err
	}
	if s.child == nil || c.Thread != s.child.identity.PID || !fuseadapter.ObservedNativeCaller(ctx, c) {
		return guestbridge.Native{}, fsapi.Permission
	}
	identity := s.child.identity
	n := guestbridge.Native{UID: c.UID, GID: c.GID, PID: c.Thread, Unique: c.Unique, Groups: slices.Clone(identity.Groups), Process: identity.PublicID.String(), Endpoint: endpoint}
	if err := s.verifyNative(ctx, n); err != nil {
		return guestbridge.Native{}, err
	}
	return n, nil
}

// Bind installs the already constructed endpoint and opens/exports exactly its
// verified descriptors. No guest path is opened by the C backend.
func (s *Supervisor) Bind(ctx context.Context, transport *guestbridge.Transport) error {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.record.Stage != "enrolled" || s.checkLive() != nil || transport == nil || transport.Endpoint() != s.record.Endpoint {
		s.mu.Unlock()
		return fsapi.Unavailable
	}
	s.record.Stage = "binding"
	if err := s.protect(); err != nil {
		s.mu.Unlock()
		return err
	}
	mount := s.mount
	provisioner, err := observeProcess(os.Getpid())
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if _, err = rand.Read(provisioner.PublicID[:]); err != nil {
		s.mu.Unlock()
		return err
	}
	mount.provisioner = provisioner
	mount.provisioning = true
	s.mu.Unlock()
	// Native callbacks re-enter Snapshot/Verify; never hold the supervisor mutex.
	err = mount.shell.Bind(fuseadapter.PrivateControlConfig{Transport: transport, Credentials: s, Directory: mount.shell.Directory()})
	if err == nil {
		err = openNativeTuple(ctx, mount)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mount.provisioning = false
	if err != nil {
		s.closed = true
		return err
	}
	if err = s.checkLive(); err != nil {
		return err
	}
	s.record.Stage = "exported"
	if err = s.protect(); err != nil {
		return err
	}
	var fds [3]int
	for i, f := range mount.files {
		fds[i] = int(f.Fd())
	}
	if err = s.child.control.SetWriteDeadline(controlDeadline(ctx)); err != nil {
		s.closed = true
		return err
	}
	err = sendTuple(s.child.control, s.record.Endpoint, s.record.Controller, fds)
	if err != nil {
		s.closed = true
		_ = s.child.control.Close()
	}
	return err
}

func (s *Supervisor) childIdentity(pid uint32) processIdentity {
	if s.mount != nil && s.mount.provisioning && pid == s.mount.provisioner.PID {
		return s.mount.provisioner
	}
	if s.child == nil {
		return processIdentity{}
	}
	return s.child.identity
}

func (s *Supervisor) verifyNative(ctx context.Context, n guestbridge.Native) error {
	if err := s.checkLive(); err != nil {
		return err
	}
	if n.Endpoint != s.record.Endpoint || n.Unique == 0 {
		return fsapi.Permission
	}
	identity := s.childIdentity(n.PID)
	if n.PID == s.child.identity.PID {
		if s.record.Stage != "claimed" {
			return fsapi.Permission
		}
	}
	if n.UID != identity.UID || n.GID != identity.GID || n.PID != identity.PID || n.Process != identity.PublicID.String() || !slices.Equal(n.Groups, identity.Groups) {
		return fsapi.Permission
	}
	if s.mount.provisioning && n.PID == s.mount.provisioner.PID {
		if !consumeProvisionObservation(ctx, s, n) {
			return fsapi.Permission
		}
		now, err := observeProcess(int(n.PID))
		if err != nil {
			return err
		}
		if now.Incarnation != identity.Incarnation || now.UID != identity.UID || now.GID != identity.GID || !slices.Equal(now.Groups, identity.Groups) {
			return fsapi.Permission
		}
	}
	return nil
}

func (s *Supervisor) checkLive() error {
	if s.closed || s.child == nil || s.mount == nil || s.mount.server == nil {
		return fsapi.Unavailable
	}
	if err := s.child.live(); err != nil {
		return err
	}
	endpoint, err := s.mount.shell.Endpoint()
	if err != nil || endpoint != s.record.Endpoint {
		return fsapi.Unavailable
	}
	if s.mount.witness == nil {
		return fsapi.Unavailable
	}
	return s.mount.witness.check()
}

// SnapshotRequest permits only the exact metadata operations required to open
// the supervisor-owned tuple. It never admits provisioning READ/WRITE/commands.
func (s *Supervisor) SnapshotRequest(ctx context.Context, c fuseadapter.Caller, e guestbridge.Endpoint, node uint64, operation string) (context.Context, guestbridge.Native, error) {
	s.mu.Lock()
	if s.mount == nil {
		s.mu.Unlock()
		return ctx, guestbridge.Native{}, fsapi.Permission
	}
	if !s.mount.provisioning || c.Thread != s.mount.provisioner.PID {
		if s.child == nil || c.Thread != s.child.identity.PID || !fuseadapter.ObservedNativeRequest(ctx, c, node, operation) {
			s.mu.Unlock()
			return ctx, guestbridge.Native{}, fsapi.Permission
		}
		metadata := operation == "GETATTR" && (node == 1 || node == s.mount.shell.Directory() || node == e.CommandNode || node == e.ResultNode || node == e.RecallNode) || operation == "STATFS" && node == 1
		data := operation == "WRITE" && node == e.CommandNode || operation == "READ" && (node == e.ResultNode || node == e.RecallNode)
		if !metadata && !data {
			s.mu.Unlock()
			return ctx, guestbridge.Native{}, fsapi.Permission
		}
		if data {
			if !s.child.consumeObservation(c.Unique) {
				s.mu.Unlock()
				return ctx, guestbridge.Native{}, fsapi.Permission
			}
		}
		s.mu.Unlock()
		n, err := s.Snapshot(ctx, c, e)
		return ctx, n, err
	}
	identity := s.mount.provisioner
	allowed := provisionMetadata(node, operation, e, s.mount.shell.Directory())
	if !allowed || e != s.record.Endpoint || !fuseadapter.ObservedNativeRequest(ctx, c, node, operation) {
		s.mu.Unlock()
		return ctx, guestbridge.Native{}, fsapi.Permission
	}
	n := guestbridge.Native{UID: c.UID, GID: c.GID, PID: c.Thread, Unique: c.Unique, Groups: slices.Clone(identity.Groups), Process: identity.PublicID.String(), Endpoint: e}
	ctx = context.WithValue(ctx, provisionKey{}, &provisionObservation{owner: s, native: n})
	err := s.checkLive()
	if err == nil && (n.UID != identity.UID || n.GID != identity.GID || n.Unique == 0) {
		err = fsapi.Permission
	}
	s.mu.Unlock()
	return ctx, n, err
}

func consumeProvisionObservation(ctx context.Context, s *Supervisor, n guestbridge.Native) bool {
	observation, ok := ctx.Value(provisionKey{}).(*provisionObservation)
	if !ok || observation.owner != s {
		return false
	}
	original := observation.native
	if original.UID != n.UID || original.GID != n.GID || original.PID != n.PID || original.Unique != n.Unique || original.Process != n.Process || original.Endpoint != n.Endpoint || !slices.Equal(original.Groups, n.Groups) {
		return false
	}
	return observation.used.CompareAndSwap(false, true)
}

func provisionMetadata(node uint64, operation string, e guestbridge.Endpoint, directory uint64) bool {
	switch operation {
	case "LOOKUP:" + guestbridge.ControlName:
		return node == 1
	case "LOOKUP:command", "LOOKUP:result", "LOOKUP:recall":
		return node == directory
	case "GETATTR":
		return node == 1 || node == directory || node == e.CommandNode || node == e.ResultNode || node == e.RecallNode
	case "OPEN":
		return node == e.CommandNode || node == e.ResultNode || node == e.RecallNode
	case "STATFS":
		return node == 1
	default:
		return false
	}
}

// ExporterIncarnation identifies only the current actually observed live child.
// Its UUID labels retained OS start/text identity; it supplies no guest enrollment.
func (s *Supervisor) ExporterIncarnation(ctx context.Context) (fsapi.UUID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return fsapi.UUID{}, err
	}
	if s.closed || s.child == nil {
		return fsapi.UUID{}, fsapi.Unavailable
	}
	if err := s.child.live(); err != nil {
		return fsapi.UUID{}, err
	}
	return s.child.identity.PublicID, nil
}
