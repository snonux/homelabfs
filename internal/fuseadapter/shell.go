//go:build linux || freebsd

package fuseadapter

import (
	"crypto/rand"
	"sync"
	"sync/atomic"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Shell is an unpublished raw frontend. Before Bind every request is refused;
// after Bind it dispatches to exactly one constructed PrivateControl. It never
// enables normal publication. Init and OnUnmount belong to go-fuse's lifetime.
type Shell struct {
	mu        sync.Mutex
	endpoint  guestbridge.Endpoint
	directory uint64
	server    *fuse.Server
	ended     bool
	bound     bool
	dispatch  atomic.Pointer[PrivateControl]
}

var _ fuse.RawFileSystem = (*Shell)(nil)

// NewShell reserves fixed private inode IDs without claiming a FUSE connection.
func NewShell(directory, command, result, recall uint64) (*Shell, error) {
	ids := []uint64{directory, command, result, recall}
	for i, id := range ids {
		if id <= 1 {
			return nil, fsapi.Invalid
		}
		for _, other := range ids[:i] {
			if id == other {
				return nil, fsapi.Invalid
			}
		}
	}
	var id fsapi.UUID
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	return &Shell{directory: directory, endpoint: guestbridge.Endpoint{FuseSession: id.String(), CommandNode: command, ResultNode: result, RecallNode: recall}}, nil
}

// Endpoint returns a tuple only after the actual server completed kernel INIT.
// This observation alone is not a supervised native enrollment.
func (s *Shell) Endpoint() (guestbridge.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil || s.ended {
		return guestbridge.Endpoint{}, fsapi.Unavailable
	}
	return s.endpoint, nil
}

// Bind atomically installs the actual constructed transport once. Failed bind
// leaves the shell unpublished; a disconnected or previously bound shell can
// never be rebound. Native enrollment must already have completed in New.
func (s *Shell) Bind(c PrivateControlConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil || s.ended || s.bound {
		return fsapi.Unavailable
	}
	if c.Transport == nil || c.Transport.Endpoint() != s.endpoint || c.Directory != s.directory || c.Ordinary != nil {
		return fsapi.Permission
	}
	adapter, err := NewPrivateControl(c)
	if err != nil {
		return err
	}
	s.bound = true
	s.dispatch.Store(adapter)
	return nil
}

// Init records one live, initialized server; fabricated zero-value servers fail.
func (s *Shell) Init(server *fuse.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil || s.ended || server == nil || server.KernelSettings().Major != 7 || server.KernelSettings().Minor < 13 {
		s.ended = true
		s.dispatch.Store(nil)
		return
	}
	s.server = server
}

// OnUnmount permanently fences the shell without retiring any retained state.
func (s *Shell) OnUnmount() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
	s.dispatch.Store(nil)
}

// String names the controlled shell without claiming readiness.
func (s *Shell) String() string { return "homelabfs-controlled" }

// SetDebug does not change admission.
func (s *Shell) SetDebug(bool) {}

// Directory returns the reserved private directory inode, never mount authority.
func (s *Shell) Directory() uint64 { return s.directory }

// ReplaceEndpoint atomically replaces a fully quiesced predecessor using only
// its privately funded, same-controller restored successor. The original shell,
// native connection, credentials and inode tuple remain the exact same owners.
// It cannot bind a new connection or turn an ordinary NewTransport into recovery.
func (s *Shell) ReplaceEndpoint(old, next *guestbridge.Transport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.dispatch.Load()
	if s.server == nil || s.ended || !s.bound || current == nil || current.transport != old || !next.Restores(old) {
		return fsapi.Permission
	}
	replacement, err := NewPrivateControl(PrivateControlConfig{Transport: next, Credentials: current.credentials, Directory: s.directory})
	if err != nil {
		return err
	}
	s.dispatch.Store(replacement)
	return nil
}
