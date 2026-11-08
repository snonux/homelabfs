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
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
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
	// LifecycleArmLostReply arms the disposable one-shot response-exposure
	// lost-reply fixture (v24). Payload: runtime UUID, current cut UUID,
	// big-endian uint64 epoch, enrolled session UUID and one operation code
	// (1 = CALL).
	LifecycleArmLostReply = 3
	// LifecycleRefused is the reply opcode for frame-level refusals.
	LifecycleRefused = 0xff
	// LifecycleSocketName is the fixed socket name in the owner directory.
	LifecycleSocketName = "control.sock"
)

const (
	lifecycleHeaderBytes  = 16
	lifecycleObserveBytes = 16
	lifecycleRestartBytes = 40
	lifecycleArmBytes     = 57
	// lifecycleReplyBytes bounds every reply payload, including restart outcome
	// plus its post-attempt evidence. Replies are never truncated: an oversized
	// OBSERVE refuses, and a RESTART outcome drops only its evidence and reports
	// EFFECT_UNREPORTABLE (boundRestartOutcome).
	lifecycleReplyBytes = 128 << 10
	// lifecycleOutcomeBytes bounds the protected restart outcome. The reply may
	// add protect_error: at most lifecycleErrorBytes of text, which JSON escaping
	// can grow sixfold, plus its key, so 4 KiB of the reply bound stay free.
	lifecycleOutcomeBytes = lifecycleReplyBytes - (4 << 10)
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
	// A transient accept failure is retried after 5 ms, doubling up to 1 s, so
	// descriptor exhaustion neither spins the worker nor ends the endpoint.
	lifecycleAcceptBackoffMin = 5 * time.Millisecond
	lifecycleAcceptBackoffMax = time.Second
)

// lifecycleBudget is the protected record of the control endpoint's bounds.
type lifecycleBudget struct {
	MemoryBytes, ReplyBytes, TransportPeakBytes, FileBytes uint64
	Workers, Connections                                   uint64
	IOTimeoutMillis, ObserveMillis, RestartMillis          int64
	// LostReplyBytes is the separate fixture lease reserved by ARM (v24).
	LostReplyBytes uint64
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
	return lifecycleBudget{MemoryBytes: memory, ReplyBytes: lifecycleReplyBytes, TransportPeakBytes: lifecycleTransportPeakBytes, FileBytes: lifecycleReplyBytes, Workers: lifecycleWorkers, Connections: lifecycleConnections, IOTimeoutMillis: lifecycleIOTimeout.Milliseconds(), ObserveMillis: lifecycleObserveTime.Milliseconds(), RestartMillis: lifecycleRestartTime.Milliseconds(), LostReplyBytes: lostReplyMemoryBytes}
}

// peerCredentials is the kernel peer-credential OS boundary.
type peerCredentials func(*net.UnixConn) (admin.Peer, error)

// acceptUnix is the listener accept OS boundary. Production uses
// (*net.UnixListener).AcceptUnix; tests may substitute it to simulate
// descriptor exhaustion or a broken listener explicitly.
type acceptUnix func(*net.UnixListener) (*net.UnixConn, error)

// lifecycleOS are the endpoint's operating-system boundaries. Both are
// required; serveReady supplies the native calls.
type lifecycleOS struct {
	peer   peerCredentials
	accept acceptUnix
}

// acceptDiagnostics counts accept failures for OBSERVE. It is worker-owned:
// only the serving worker writes it, and OBSERVE is served by that worker.
type acceptDiagnostics struct {
	errors uint64
	last   string
}

// lifecycleRequest is one decoded, length-checked HSC1 request.
type lifecycleRequest struct {
	opcode    uint8
	sequence  uint32
	runtime   fsapi.UUID
	cut       fsapi.UUID
	epoch     uint64
	session   fsapi.UUID // ARM only
	operation uint8      // ARM only
}

// lifecycleServer is the single-worker private endpoint. One connection carries
// exactly one request and reply. Connections are served strictly serially, so
// observation and restart never overlap and at most one is funded at a time.
type lifecycleServer struct {
	lifecycle *runtimeLifecycle
	listener  *net.UnixListener
	socket    *lifecycleSocket
	path      string
	boundary  lifecycleOS
	uid       uint32
	last      uint32            // highest accepted command sequence; worker-owned
	accepts   acceptDiagnostics // worker-owned
	// acceptErr is the permanent accept failure that ended the worker. It is
	// written before done closes and read only after done.
	acceptErr error
	ctx       context.Context
	cancel    context.CancelFunc
	closing   chan struct{} // closed first by close; ends an accept backoff
	done      chan struct{}
	once      sync.Once
}

// listenLifecycle creates the socket exclusively in the private owner root
// (bindLifecycleSocket): an existing name is preserved and refused, and the
// mode is set without following a symlink. No guest/network listener exists.
func listenLifecycle(ctx context.Context, owner string, l *runtimeLifecycle, boundary lifecycleOS) (*lifecycleServer, error) {
	if l == nil || boundary.peer == nil || boundary.accept == nil {
		return nil, fsapi.Invalid
	}
	listener, socket, err := bindLifecycleSocket(owner)
	if err != nil {
		return nil, err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	s := &lifecycleServer{lifecycle: l, listener: listener, socket: socket, path: socket.path, boundary: boundary, uid: uint32(os.Geteuid()), ctx: serveCtx, cancel: cancel, closing: make(chan struct{}), done: make(chan struct{})}
	go s.serve()
	return s, nil
}

// close stops accepting, joins the one worker and removes only this listener's
// unchanged socket name. The in-flight command (bounded by its own observe or
// restart deadline) is not cancelled here: a failed restart fences the native
// owner, which itself triggers shutdown, and its post-attempt evidence, protected
// outcome and reply must not be truncated by that shutdown. Cancellation of the
// runtime's parent context still propagates into the command. A permanent
// accept failure that ended the worker is reported here. Only the first call
// reports errors, so explicit and deferred closes do not duplicate them.
func (s *lifecycleServer) close() error {
	var result error
	s.once.Do(func() {
		close(s.closing)
		result = s.listener.Close()
		<-s.done
		s.cancel()
		result = errors.Join(result, s.acceptErr, s.socket.close())
	})
	return result
}

// serve accepts and serves connections strictly serially. A closed listener
// ends the worker normally. A transient accept failure (descriptor, buffer or
// memory exhaustion) is counted as an OBSERVE diagnostic and retried after a
// bounded exponential backoff, so it never silently ends the endpoint. Any
// other accept failure is permanent: it ends the worker and is kept for close;
// serveReady then ends the runtime with it, retaining every owner.
func (s *lifecycleServer) serve() {
	defer close(s.done)
	var backoff time.Duration
	for {
		c, err := s.boundary.accept(s.listener)
		if err == nil {
			backoff = 0
			s.handle(c)
			_ = c.Close()
			continue
		}
		if errors.Is(err, net.ErrClosed) {
			return
		}
		s.accepts.errors++
		s.accepts.last = evidenceError(err)
		if !transientAcceptError(err) {
			s.acceptErr = fmt.Errorf("lifecycle endpoint accept failed permanently: %w", err)
			return
		}
		backoff = nextAcceptBackoff(backoff)
		if !s.pause(backoff) {
			return
		}
	}
}

// pause waits one backoff step unless close started. It deliberately does not
// watch the parent context: cancellation reaches the worker through close,
// which keeps a normal stop distinct from a lost endpoint in serveReady.
func (s *lifecycleServer) pause(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.closing:
		return false
	}
}

// transientAcceptError reports accept failures caused by momentary resource
// exhaustion or an aborted/interrupted peer, which a later accept can survive.
func transientAcceptError(err error) bool {
	var errno unix.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case unix.EMFILE, unix.ENFILE, unix.ENOBUFS, unix.ENOMEM, unix.ECONNABORTED, unix.EINTR, unix.EAGAIN, unix.EPROTO:
		return true
	}
	return false
}

// nextAcceptBackoff doubles the accept retry delay within its fixed bounds.
func nextAcceptBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return lifecycleAcceptBackoffMin
	}
	return min(2*d, lifecycleAcceptBackoffMax)
}

// handle authenticates the kernel peer before reading any request byte.
func (s *lifecycleServer) handle(c *net.UnixConn) {
	if err := c.SetDeadline(time.Now().Add(lifecycleIOTimeout)); err != nil {
		return
	}
	peer, err := s.boundary.peer(c)
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
		raw, _ = json.Marshal(oversizedReply(request.opcode))
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
		return s.observeReply(r)
	case LifecycleArmLostReply:
		return s.armReply(r)
	default:
		return s.restartReply(r)
	}
}

// observeReply captures the actual owners and adds the endpoint's own accept
// diagnostics, which this worker owns (restart evidence omits that section).
func (s *lifecycleServer) observeReply(r lifecycleRequest) any {
	ctx, cancel := context.WithTimeout(s.ctx, lifecycleObserveTime)
	defer cancel()
	e := s.lifecycle.observe(ctx)
	e.Endpoint = &endpointEvidence{AcceptErrors: s.accepts.errors, LastAcceptError: s.accepts.last}
	return struct {
		Status   string            `json:"status"`
		Sequence uint32            `json:"sequence"`
		Evidence lifecycleEvidence `json:"evidence"`
	}{"OBSERVED", r.sequence, e}
}

func (s *lifecycleServer) restartReply(r lifecycleRequest) any {
	ctx, cancel := context.WithTimeout(s.ctx, lifecycleRestartTime)
	defer cancel()
	view, err := s.lifecycle.restart(ctx, r.cut, r.epoch)
	if err != nil {
		return refusal(evidenceError(err))
	}
	return view
}

// armReply arms the lost-reply fixture. Its source baseline capture uses the
// observation deadline; ARMED carries no publication and no acceptance value.
func (s *lifecycleServer) armReply(r lifecycleRequest) any {
	ctx, cancel := context.WithTimeout(s.ctx, lifecycleObserveTime)
	defer cancel()
	view, err := s.lifecycle.armLostReply(ctx, r)
	if err != nil {
		return refusal(evidenceError(err))
	}
	return struct {
		Status    string        `json:"status"`
		Sequence  uint32        `json:"sequence"`
		LostReply lostReplyView `json:"lost_reply"`
	}{"ARMED", r.sequence, view}
}

// lifecycleNotice is a reply without evidence: a REFUSED command, or an
// EFFECT_UNREPORTABLE one whose effect may exist but cannot be reported.
type lifecycleNotice struct {
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Publication any    `json:"publication"`
}

func refusal(reason string) lifecycleNotice {
	return lifecycleNotice{Status: "REFUSED", Reason: reason}
}

// oversizedReply replaces a reply that does not fit the funded frame. OBSERVE
// has no command effect, so it refuses. RESTART and ARM may already have taken
// effect, so they must never claim a refusal. RESTART bounds its own outcome
// (boundRestartOutcome) and ARMED is a fixed-size record, so for them this is
// only a backstop that keeps the reply truthful.
func oversizedReply(opcode uint8) lifecycleNotice {
	if opcode == LifecycleObserve {
		return refusal("reply exceeds funded bound")
	}
	return lifecycleNotice{Status: lifecycleUnreportable, Reason: "reply exceeds funded bound after the command may have taken effect; a later OBSERVE reports current owners if its own reply fits the bound"}
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
	want := map[uint8]uint32{LifecycleObserve: lifecycleObserveBytes, LifecycleRestart: lifecycleRestartBytes, LifecycleArmLostReply: lifecycleArmBytes}[req.opcode]
	if want == 0 || length != want {
		return lifecycleRequest{}, fsapi.Invalid
	}
	var payload [lifecycleArmBytes]byte
	if _, err := io.ReadFull(r, payload[:length]); err != nil {
		return lifecycleRequest{}, err
	}
	copy(req.runtime[:], payload[:16])
	if req.opcode != LifecycleObserve {
		copy(req.cut[:], payload[16:32])
		req.epoch = binary.BigEndian.Uint64(payload[32:40])
	}
	if req.opcode == LifecycleArmLostReply {
		copy(req.session[:], payload[40:56])
		req.operation = payload[56]
	}
	return req, nil
}

// encodeLifecyclePayload is the client-side inverse of readLifecycleRequest.
func encodeLifecyclePayload(r lifecycleRequest) []byte {
	payload := append(make([]byte, 0, lifecycleArmBytes), r.runtime[:]...)
	if r.opcode == LifecycleObserve {
		return payload
	}
	payload = append(payload, r.cut[:]...)
	payload = binary.BigEndian.AppendUint64(payload, r.epoch)
	if r.opcode == LifecycleArmLostReply {
		payload = append(payload, r.session[:]...)
		payload = append(payload, r.operation)
	}
	return payload
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
