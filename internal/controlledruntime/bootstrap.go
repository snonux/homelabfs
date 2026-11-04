//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// supplyBootstrap writes only operator-authorized complete scopes through the
// retained inherited pipe. The child obtains no arbitrary path or descriptor.
// Actual construction still requires its native tuple, CLAIM and fresh VERIFY.
func (s *Supervisor) supplyBootstrap(ctx context.Context, scopes []guestbridge.Context, protocol guestbridge.Protocol) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.child == nil || s.child.bootstrap == nil || s.bootstrapScopes != nil || len(scopes) < 1 || len(scopes) > 16 || !guestbridge.IsMappedProtocol(protocol) {
		return fsapi.Permission
	}
	s.bootstrapScopes = map[string]guestbridge.Context{}
	s.bootstrapClaimed = map[string]bool{}
	raw := make([]byte, 16)
	copy(raw, "HSB1")
	binary.BigEndian.PutUint16(raw[4:6], uint16(len(scopes)))
	binary.BigEndian.PutUint16(raw[6:8], 4)
	// The only supported runtime tuple includes terminal closure.
	if protocol != guestbridge.TerminalProtocol() {
		return fsapi.Unsupported
	}
	binary.BigEndian.PutUint32(raw[8:12], 32767)
	for _, scope := range scopes {
		if s.bootstrapScopes[scope.Session].Session != "" || scope.FuseSession != s.record.Endpoint.FuseSession || scope.ExporterIncarnation != s.record.ProcessID.String() || scope.GuestBoot != scopes[0].GuestBoot || scope.VM != scopes[0].VM {
			return fsapi.Permission
		}
		b, err := json.Marshal(scope)
		if err != nil || len(b) > 4096 {
			return fsapi.Invalid
		}
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(len(b)))
		raw = append(raw, size[:]...)
		raw = append(raw, b...)
		s.bootstrapScopes[scope.Session] = scope
	}
	binary.BigEndian.PutUint32(raw[12:16], uint32(len(raw)-16))
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := s.child.bootstrap.SetWriteDeadline(deadline); err != nil {
		return err
	}
	n, err := s.child.bootstrap.Write(raw)
	if err == nil && n != len(raw) {
		err = fsapi.Unavailable
	}
	if e := s.child.bootstrap.Close(); err == nil {
		err = e
	}
	s.child.bootstrap = nil
	return err
}

func (s *Supervisor) waitBootstrap(ctx context.Context) error {
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		s.mu.Lock()
		err := s.checkLive()
		ready := err == nil && s.controlStarted && s.record.Stage == "claimed" && s.child.tupleVerified && len(s.bootstrapScopes) > 0 && len(s.bootstrapClaimed) == len(s.bootstrapScopes)
		s.mu.Unlock()
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}
