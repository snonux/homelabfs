//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// HSC1 is the private post-READY lifecycle control protocol. It is host-local,
// unrelated to guest H9B/HSM1/HSR1, and reachable only through an AF_UNIX socket
// inside the private 0700 runtime owner directory, checked by kernel peer UID.
const (
	// LifecycleVersion is the HSC1 frame version.
	LifecycleVersion = 1
	// LifecycleObserve refreshes admissions and returns actual owner evidence.
	// Payload: the 16-byte server-issued runtime UUID.
	LifecycleObserve = 1
	// LifecycleRestart runs the single supported C9 same-owner restart.
	// Payload: runtime UUID, current cut UUID and big-endian uint64 epoch.
	LifecycleRestart = 2
	// LifecycleRefused is the reply opcode for frame-level refusals.
	LifecycleRefused = 0xff
	// LifecycleSocketName is the fixed socket name in the owner directory.
	LifecycleSocketName = "control.sock"
)

const (
	lifecycleHeaderBytes  = 16
	lifecycleObserveBytes = 16
	lifecycleRestartBytes = 40
	// lifecycleReplyBytes bounds every reply payload, including restart outcome
	// plus its post-attempt evidence; larger replies refuse, never truncate.
	lifecycleReplyBytes = 128 << 10
	// lifecycleTransportPeakBytes is the C8 evidence read peak: 128 KiB index,
	// at most 8 MiB complete bridge rows and one decoded 8 MiB cell.
	lifecycleTransportPeakBytes = (128 << 10) + (8 << 20) + (8 << 20)
	// lifecycleFixedBytes covers frames, two reply encodings, the worker stack
	// and connection metadata.
	lifecycleFixedBytes  = 1 << 20
	lifecycleWorkers     = 1
	lifecycleConnections = 1
	lifecycleIOTimeout   = 5 * time.Second
	lifecycleObserveTime = 30 * time.Second
	// lifecycleRestartTime exceeds the internal 60 s restart grant deadline so
	// the C9 stages, not this wrapper, decide most timeouts.
	lifecycleRestartTime = 90 * time.Second
)

// lifecycleBudget is the protected record of the control endpoint's bounds.
type lifecycleBudget struct {
	MemoryBytes, ReplyBytes, TransportPeakBytes, FileBytes uint64
	Workers, Connections                                   uint64
	IOTimeoutMillis, ObserveMillis, RestartMillis          int64
}

// lifecycleMemoryBytes funds the endpoint: fixed frames/replies, the C8
// inspection peak, two controller-ledger copies (protected bytes plus a live
// comparison) and three conservative copies of one bounded view source.
func lifecycleMemoryBytes(v viewcontrol.Bounds) (uint64, error) {
	if v.LedgerBytes <= 0 || v.SourceBytes <= 0 || uint64(v.SourceBytes) > math.MaxUint64/3 {
		return 0, fsapi.Invalid
	}
	return checkedBudgetSum(lifecycleFixedBytes, lifecycleTransportPeakBytes, 2*uint64(v.LedgerBytes), 3*uint64(v.SourceBytes))
}

func fixedLifecycleBudget(v viewcontrol.Bounds) lifecycleBudget {
	memory, _ := lifecycleMemoryBytes(v)
	return lifecycleBudget{MemoryBytes: memory, ReplyBytes: lifecycleReplyBytes, TransportPeakBytes: lifecycleTransportPeakBytes, FileBytes: lifecycleReplyBytes, Workers: lifecycleWorkers, Connections: lifecycleConnections, IOTimeoutMillis: lifecycleIOTimeout.Milliseconds(), ObserveMillis: lifecycleObserveTime.Milliseconds(), RestartMillis: lifecycleRestartTime.Milliseconds()}
}

// peerCredentials is the kernel peer-credential OS boundary.
type peerCredentials func(*net.UnixConn) (admin.Peer, error)

// lifecycleRequest is one decoded, length-checked HSC1 request.
type lifecycleRequest struct {
	opcode   uint8
	sequence uint32
	runtime  fsapi.UUID
	cut      fsapi.UUID
	epoch    uint64
}

// lifecycleServer is the single-worker private endpoint. One connection carries
// exactly one request and reply. Connections are served strictly serially, so
// observation and restart never overlap and at most one is funded at a time.
type lifecycleServer struct {
	lifecycle *runtimeLifecycle
	listener  *net.UnixListener
	path      string
	socket    os.FileInfo
	peer      peerCredentials
	uid       uint32
	last      uint32 // highest accepted command sequence; worker-owned
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	once      sync.Once
}

// listenLifecycle creates the socket exclusively in the private owner root. An
// existing name is preserved and refused. No guest/network listener exists.
func listenLifecycle(ctx context.Context, owner string, l *runtimeLifecycle, peer peerCredentials) (*lifecycleServer, error) {
	if l == nil || peer == nil {
		return nil, fsapi.Invalid
	}
	path := filepath.Join(owner, LifecycleSocketName)
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("existing lifecycle socket preserved: %w", fsapi.Permission)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	st, err := os.Lstat(path)
	if err == nil {
		err = os.Chmod(path, 0o600)
	}
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	s := &lifecycleServer{lifecycle: l, listener: listener, path: path, socket: st, peer: peer, uid: uint32(os.Geteuid()), ctx: serveCtx, cancel: cancel, done: make(chan struct{})}
	go s.serve()
	return s, nil
}

// close stops accepting, joins the one worker and removes only this listener's
// unchanged socket name. The in-flight command (bounded by its own observe or
// restart deadline) is not cancelled here: a failed restart fences the native
// owner, which itself triggers shutdown, and its post-attempt evidence, protected
// outcome and reply must not be truncated by that shutdown. Cancellation of the
// runtime's parent context still propagates into the command. Only the first
// call reports errors, so explicit and deferred closes do not duplicate them.
func (s *lifecycleServer) close() error {
	var result error
	s.once.Do(func() {
		result = s.listener.Close()
		<-s.done
		s.cancel()
		st, err := os.Lstat(s.path)
		if err == nil && os.SameFile(st, s.socket) {
			result = errors.Join(result, os.Remove(s.path))
		} else if !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("lifecycle socket replaced or inaccessible; preserved"), err)
		}
	})
	return result
}

func (s *lifecycleServer) serve() {
	defer close(s.done)
	for {
		c, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		s.handle(c)
		_ = c.Close()
	}
}

// handle authenticates the kernel peer before reading any request byte.
func (s *lifecycleServer) handle(c *net.UnixConn) {
	if err := c.SetDeadline(time.Now().Add(lifecycleIOTimeout)); err != nil {
		return
	}
	peer, err := s.peer(c)
	if err != nil || peer.UID != s.uid {
		_ = writeLifecycleReply(c, LifecycleRefused, 0, refusal("peer credentials unauthorized"))
		return
	}
	request, err := readLifecycleRequest(c)
	if err != nil {
		_ = writeLifecycleReply(c, LifecycleRefused, 0, refusal("invalid request frame"))
		return
	}
	reply := s.dispatch(request)
	raw, err := json.Marshal(reply)
	if err != nil || len(raw) > lifecycleReplyBytes {
		raw, _ = json.Marshal(refusal("reply exceeds funded bound"))
	}
	if err = c.SetWriteDeadline(time.Now().Add(lifecycleIOTimeout)); err != nil {
		return
	}
	_ = writeLifecycleFrame(c, request.opcode|0x80, request.sequence, raw)
}

// dispatch checks the runtime binding and strictly increasing sequence before
// consuming the sequence. Refused bindings consume nothing.
func (s *lifecycleServer) dispatch(r lifecycleRequest) any {
	runtime, _, _, _ := s.lifecycle.binding()
	if r.runtime != runtime {
		return refusal("runtime binding mismatch")
	}
	if r.sequence <= s.last {
		return refusal(fmt.Sprintf("non-monotone sequence %d (last %d)", r.sequence, s.last))
	}
	s.last = r.sequence
	switch r.opcode {
	case LifecycleObserve:
		ctx, cancel := context.WithTimeout(s.ctx, lifecycleObserveTime)
		defer cancel()
		e := s.lifecycle.observe(ctx)
		return struct {
			Status   string            `json:"status"`
			Sequence uint32            `json:"sequence"`
			Evidence lifecycleEvidence `json:"evidence"`
		}{"OBSERVED", r.sequence, e}
	default:
		ctx, cancel := context.WithTimeout(s.ctx, lifecycleRestartTime)
		defer cancel()
		view, err := s.lifecycle.restart(ctx, r.cut, r.epoch)
		if err != nil {
			return refusal(evidenceError(err))
		}
		return view
	}
}

type lifecycleRefusal struct {
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Publication any    `json:"publication"`
}

func refusal(reason string) lifecycleRefusal {
	return lifecycleRefusal{Status: "REFUSED", Reason: reason}
}

// readLifecycleRequest decodes one closed, fixed-length request. Every length
// is checked against the opcode before any payload byte is read; no length from
// the wire selects an allocation.
func readLifecycleRequest(r io.Reader) (lifecycleRequest, error) {
	var header [lifecycleHeaderBytes]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return lifecycleRequest{}, err
	}
	req := lifecycleRequest{opcode: header[5], sequence: binary.BigEndian.Uint32(header[8:])}
	length := binary.BigEndian.Uint32(header[12:])
	if string(header[:4]) != "HSC1" || header[4] != LifecycleVersion || header[6] != 0 || header[7] != 0 || req.sequence == 0 {
		return lifecycleRequest{}, fsapi.Invalid
	}
	want := map[uint8]uint32{LifecycleObserve: lifecycleObserveBytes, LifecycleRestart: lifecycleRestartBytes}[req.opcode]
	if want == 0 || length != want {
		return lifecycleRequest{}, fsapi.Invalid
	}
	var payload [lifecycleRestartBytes]byte
	if _, err := io.ReadFull(r, payload[:length]); err != nil {
		return lifecycleRequest{}, err
	}
	copy(req.runtime[:], payload[:16])
	if req.opcode == LifecycleRestart {
		copy(req.cut[:], payload[16:32])
		req.epoch = binary.BigEndian.Uint64(payload[32:40])
	}
	return req, nil
}

func writeLifecycleReply(w io.Writer, opcode uint8, sequence uint32, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeLifecycleFrame(w, opcode, sequence, raw)
}

func writeLifecycleFrame(w io.Writer, opcode uint8, sequence uint32, payload []byte) error {
	if len(payload) > lifecycleReplyBytes {
		return fsapi.NoSpace
	}
	b := make([]byte, lifecycleHeaderBytes+len(payload))
	copy(b, "HSC1")
	b[4], b[5] = LifecycleVersion, opcode
	binary.BigEndian.PutUint32(b[8:], sequence)
	binary.BigEndian.PutUint32(b[12:], uint32(len(payload)))
	copy(b[lifecycleHeaderBytes:], payload)
	n, err := w.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return err
}
