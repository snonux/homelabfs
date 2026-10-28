package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// Server owns a private listener and at most sixteen short-lived requests.
type Server struct {
	config   Config
	listener *net.UnixListener
	socket   os.FileInfo
	slots    chan struct{}
	done     chan struct{}
	workers  sync.WaitGroup
	once     sync.Once
	closeErr error
	control  sync.Mutex
}

// Listen creates a new socket exclusively. Existing sockets/resources are preserved.
// Caller must Close it before releasing the observed service/backing roots.
func Listen(ctx context.Context, c Config) (*Server, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Status == nil || c.Audit == nil || c.NodeID == "" || c.VolumeID == "" || c.Generation == "" {
		return nil, fmt.Errorf("admin status/audit and target identities required")
	}
	if err := privateParent(c.SocketPath); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(c.SocketPath); err == nil {
		return nil, fmt.Errorf("existing admin socket path preserved")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect admin socket path: %w", err)
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: c.SocketPath, Net: "unix"})
	if err != nil {
		return nil, err
	}
	l.SetUnlinkOnClose(false)
	if err := os.Chmod(c.SocketPath, 0o600); err != nil {
		_ = l.Close()
		return nil, err
	}
	st, err := os.Lstat(c.SocketPath)
	if err != nil {
		_ = l.Close()
		return nil, err
	}
	s := &Server{config: c, listener: l, socket: st, slots: make(chan struct{}, maxConnections), done: make(chan struct{})}
	go s.accept()
	return s, nil
}

// Close stops admission, drains in-flight requests and removes only this
// listener's unchanged socket. Network inactivity is bounded; injected callbacks
// and native audit I/O retain their own cancellation behavior. Replacements survive.
func (s *Server) Close() error {
	s.once.Do(func() {
		s.closeErr = s.listener.Close()
		<-s.done
		s.workers.Wait()
		st, err := os.Lstat(s.config.SocketPath)
		if err == nil && os.SameFile(st, s.socket) {
			s.closeErr = errors.Join(s.closeErr, os.Remove(s.config.SocketPath))
		} else if !errors.Is(err, os.ErrNotExist) {
			s.closeErr = errors.Join(s.closeErr, fmt.Errorf("admin socket replaced or inaccessible; preserved"), err)
		}
	})
	return s.closeErr
}
func (s *Server) accept() {
	defer close(s.done)
	for {
		c, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		select {
		case s.slots <- struct{}{}:
			s.workers.Add(1)
			go func() { defer s.workers.Done(); defer func() { <-s.slots; _ = c.Close() }(); s.handle(c) }()
		default:
			_ = c.Close()
		}
	}
}
func (s *Server) handle(c *net.UnixConn) {
	if err := c.SetDeadline(time.Now().Add(ioTimeout)); err != nil {
		return
	}
	peer, err := credentials(c)
	if err != nil {
		_ = writeFrame(c, Response{Version: protocolVersion, Error: "peer credentials unavailable"})
		return
	}
	raw, err := readFrame(c)
	if err != nil {
		_ = writeFrame(c, Response{Version: protocolVersion, Error: "invalid request frame"})
		return
	}
	request, err := decodeRequest(raw)
	if err != nil {
		_ = writeFrame(c, Response{Version: protocolVersion, Error: "invalid request"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ioTimeout)
	defer cancel()
	response := s.dispatch(ctx, peer, request)
	_ = writeFrame(c, response)
}
func (s *Server) dispatch(ctx context.Context, peer Peer, r Request) Response {
	response := Response{Version: protocolVersion}
	level := Recovery
	switch r.Operation {
	case "status":
		level = Read
	case "test-fault":
		level = Control
	}
	authorized := peer.UID == uint32(os.Geteuid()) && (s.config.Authorize == nil || s.config.Authorize(peer, level))
	if level == Read {
		if !authorized {
			response.Error = "permission denied"
			return response
		}
		status, err := s.config.Status(ctx)
		if err != nil {
			response.Error = "status unavailable"
		} else {
			response.Status = &status
		}
		return response
	}
	// Serialize controls so intent and result bracket each effect in the audit.
	s.control.Lock()
	defer s.control.Unlock()
	outcome := "denied"
	if authorized && level == Control && s.config.EnableTestHooks && s.config.ToggleFault != nil {
		outcome = "requested"
	}
	if err := s.audit(ctx, peer, r, outcome); err != nil {
		response.Error = "audit unavailable; control denied"
		return response
	}
	if outcome == "denied" {
		response.Error = "control denied or unsupported"
		return response
	}
	err := s.config.ToggleFault(ctx, r.Fault, r.Enabled)
	outcome = "applied"
	if err != nil {
		outcome = "rejected"
		response.Error = "test fault rejected"
	}
	if err := s.audit(ctx, peer, r, outcome); err != nil {
		response.Error = "audit completion failed; inspect audit and status before retry"
	}
	return response
}
func (s *Server) audit(ctx context.Context, p Peer, r Request, outcome string) error {
	return s.config.Audit(ctx, Event{Time: time.Now().UTC(), Peer: p, NodeID: s.config.NodeID, VolumeID: s.config.VolumeID, Generation: s.config.Generation, Operation: r.Operation, Fault: r.Fault, Enabled: r.Enabled, Outcome: outcome})
}
