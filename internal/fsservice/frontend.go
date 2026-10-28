package fsservice

import (
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// FrontendInfo is immutable admission metadata, not serving-authority evidence.
// Every actual request must still pass Dispatch and the current service gates.
type FrontendInfo struct {
	Volume, Generation fsapi.UUID
	Root               fsapi.ObjectID
	Validation         ValidationProfile
	UnixPermissions    bool
	IOBytes            int
}

// FrontendInfo describes the configured volume without exposing its storage.
func (s *Service) FrontendInfo() FrontendInfo {
	info := FrontendInfo{Volume: s.config.Storage.Volume, Generation: s.config.Workspace.ID, Root: s.config.Storage.Root, IOBytes: s.config.Limits.IOBytes}
	if s.config.Validation != nil {
		info.Validation = *s.config.Validation
	}
	_, info.UnixPermissions = s.config.Permissions.(UnixPermissions)
	return info
}

// AttachFrontend exclusively reserves native publication for this generation.
// Initial adapters have per-connection process sessions, so duplicate mounts
// cannot share one correct POSIX owner domain. The caller must retain the lease
// until normal unmount and all reference cleanup have succeeded. It is not
// ownership/authority evidence and does not bypass any Dispatch gate.
func (s *Service) AttachFrontend() (release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.State() != Serving {
		return nil, fsapi.Unavailable
	}
	if s.frontend {
		return nil, fsapi.Busy
	}
	s.frontend = true
	var once sync.Once
	return func() { once.Do(func() { s.mu.Lock(); s.frontend = false; s.mu.Unlock() }) }, nil
}
