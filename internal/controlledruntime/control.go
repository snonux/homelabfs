//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"golang.org/x/sys/unix"
)

const (
	// ControlVersion is the host-local protocol, unrelated to guest H9B versions.
	ControlVersion = 1
	// ControlMaxPayload bounds every management allocation, including refusals.
	ControlMaxPayload = 184
	// ControlClaim requests sole fresh ownership (endpoint + zero digest).
	ControlClaim = 1
	// ControlReconcile requests restored ownership; currently always BLOCKED.
	ControlReconcile = 2
	// ControlVerify authenticates three actual child descriptor numbers.
	ControlVerify = 3
	// ControlTuple carries the admitted tuple/controller and exactly three rights.
	ControlTuple = 4
)

type controlFrame struct {
	opcode   uint8
	sequence uint32
	payload  []byte
}

// ServeControl serves only the retained inherited socketpair. No caller supplies
// a socket, native endpoint, process facts or reconnect path. Any framing/I/O or
// authentication error permanently fences this supervisor; obligations remain.
// A second invocation is rejected without touching the active reader or socket.
func (s *Supervisor) ServeControl(ctx context.Context) error {
	s.mu.Lock()
	if s.controlStarted || s.closed || s.child == nil || s.mount == nil || !s.record.Controller.Valid() || s.record.Stage != "exported" || s.mount.files[0] == nil {
		s.mu.Unlock()
		return fsapi.Unavailable
	}
	s.controlStarted = true
	child := s.child
	s.mu.Unlock()
	defer s.fence()
	sequence := uint32(1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		if err := child.control.SetDeadline(deadline); err != nil {
			return err
		}
		frame, err := readControl(child.control)
		if err != nil {
			return err
		}
		if frame.sequence != sequence || sequence == math.MaxUint32 {
			return fsapi.Permission
		}
		sequence++
		status := s.controlRequest(frame)
		payload := make([]byte, 4)
		binary.BigEndian.PutUint32(payload, status)
		if err = writeControl(child.control, controlFrame{opcode: frame.opcode | 0x80, sequence: frame.sequence, payload: payload}); err != nil {
			return err
		}
		if status != 0 {
			return fsapi.Permission
		}
	}
}

func (s *Supervisor) controlRequest(frame controlFrame) uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkLive() != nil || s.child.live() != nil {
		return 1
	}
	if len(frame.payload) < 40 || !sameEndpoint(frame.payload[:40], s.record.Endpoint) {
		return 1
	}
	switch frame.opcode {
	case ControlClaim:
		if len(frame.payload) != 72 || s.record.Stage != "exported" {
			return 1
		}
		for _, b := range frame.payload[40:] {
			if b != 0 {
				return 3
			}
		}
		s.record.Stage = "claimed"
		if s.protect() != nil {
			return 3
		}
		return 0
	case ControlReconcile:
		return 3 // Complete host transport restoration belongs to C8/C9.
	case ControlVerify:

		if len(frame.payload) != 184 || s.record.Stage != "claimed" {
			return 1
		}
		fds, err := validateFDObservation(frame.payload, s.record.Endpoint, s.record.Controller, s.mount.expected, frame.sequence)
		if err != nil {
			return 1
		}
		if s.child.tupleVerified && s.child.nativeFDs != fds {
			return 1
		}
		s.child.nativeFDs = fds
		s.child.tupleVerified = true
		s.child.observationConsumed = false
		return 0
	default:
		return 2
	}
}

func readControl(r io.Reader) (controlFrame, error) {
	var header [16]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return controlFrame{}, err
	}
	length := binary.BigEndian.Uint32(header[12:])
	if string(header[:4]) != "HSM1" || header[4] != ControlVersion || header[6] != 0 || header[7] != 0 || length > ControlMaxPayload {
		return controlFrame{}, fsapi.Invalid
	}
	opcode := header[5]
	if opcode < ControlClaim || opcode > ControlVerify {
		return controlFrame{}, fsapi.Invalid
	}
	expected := uint32(72)
	if opcode == ControlVerify {
		expected = 184
	}
	if length != expected {
		return controlFrame{}, fsapi.Invalid
	}
	f := controlFrame{opcode: opcode, sequence: binary.BigEndian.Uint32(header[8:]), payload: make([]byte, length)}
	_, err := io.ReadFull(r, f.payload)
	return f, err
}

func writeControl(w io.Writer, frame controlFrame) error {
	if len(frame.payload) > ControlMaxPayload {
		return fsapi.Invalid
	}
	b := encodeControl(frame)
	n, err := w.Write(b)
	if err == nil && n != len(b) {
		err = io.ErrShortWrite
	}
	return err
}

func encodeControl(frame controlFrame) []byte {
	b := make([]byte, 16+len(frame.payload))
	copy(b, "HSM1")
	b[4] = ControlVersion
	b[5] = frame.opcode
	binary.BigEndian.PutUint32(b[8:], frame.sequence)
	binary.BigEndian.PutUint32(b[12:], uint32(len(frame.payload)))
	copy(b[16:], frame.payload)
	return b
}

func endpointBytes(e guestbridge.Endpoint) []byte {
	b := make([]byte, 40)
	id, _ := fsapi.ParseUUID(e.FuseSession)
	copy(b, id[:])
	for i, n := range []uint64{e.CommandNode, e.ResultNode, e.RecallNode} {
		binary.BigEndian.PutUint64(b[16+i*8:], n)
	}
	return b
}

func sameEndpoint(b []byte, e guestbridge.Endpoint) bool {
	return string(b) == string(endpointBytes(e))
}

func sendTuple(conn *net.UnixConn, endpoint guestbridge.Endpoint, controller fsapi.UUID, fds [3]int) error {
	payload := append(endpointBytes(endpoint), controller[:]...)
	b := encodeControl(controlFrame{opcode: ControlTuple, sequence: 0, payload: payload})
	n, oob, err := conn.WriteMsgUnix(b, unix.UnixRights(fds[:]...), nil)
	if err == nil && (n != len(b) || oob != len(unix.UnixRights(fds[:]...))) {
		return io.ErrShortWrite
	}
	return err
}

func (s *Supervisor) fence() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.child != nil {
		_ = s.child.control.Close()
	}
}

func controlDeadline(ctx context.Context) time.Time {
	d := time.Now().Add(5 * time.Second)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(d) {
		d = deadline
	}
	return d
}

func validateFDObservation(payload []byte, endpoint guestbridge.Endpoint, controller fsapi.UUID, expected [3]descriptorIdentity, sequence uint32) ([3]int, error) {
	var fds [3]int
	if len(payload) != 184 || !sameEndpoint(payload[:40], endpoint) || string(payload[40:56]) != string(controller[:]) || binary.BigEndian.Uint64(payload[56:64]) != uint64(sequence) || sequence == 0 {
		return fds, fsapi.Permission
	}
	for i, want := range expected {
		p := payload[64+i*40 : 64+(i+1)*40]
		fds[i] = int(binary.BigEndian.Uint32(p))
		flags := binary.BigEndian.Uint32(p[4:])
		rights := binary.BigEndian.Uint32(p[32:])
		if fds[i] < 4 || flags != want.Access|4 || binary.BigEndian.Uint64(p[8:]) != want.Device || binary.BigEndian.Uint64(p[16:]) != want.Inode || binary.BigEndian.Uint64(p[24:]) != want.Size || rights != 1 || binary.BigEndian.Uint32(p[36:]) != 0 {
			return fds, fsapi.Permission
		}
		for _, fd := range fds[:i] {
			if fd == fds[i] {
				return fds, fsapi.Permission
			}
		}
	}
	return fds, nil
}
