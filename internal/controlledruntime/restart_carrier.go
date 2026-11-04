//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// New barrier workspace ceiling; the normal C ledger still permits65MiB.
const exporterRestartLimit = 16 << 20

type recoveryFrame struct {
	op       byte
	sequence uint32
	payload  []byte
}

func writeRecovery(c *net.UnixConn, op byte, sequence uint32, payload []byte) error {
	if len(payload) > ControlMaxPayload {
		return fsapi.Invalid
	}
	b := make([]byte, 16+len(payload))
	copy(b, "HSR1")
	b[4] = 1
	b[5] = op
	binary.BigEndian.PutUint32(b[8:], sequence)
	binary.BigEndian.PutUint32(b[12:], uint32(len(payload)))
	copy(b[16:], payload)
	for len(b) > 0 {
		n, err := c.Write(b)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func readRecovery(c *net.UnixConn) (recoveryFrame, error) {
	var h [16]byte
	if err := readRecoveryBytes(c, h[:]); err != nil {
		return recoveryFrame{}, err
	}
	n := binary.BigEndian.Uint32(h[12:])
	if string(h[:4]) != "HSR1" || h[4] != 1 || h[6] != 0 || h[7] != 0 || n > ControlMaxPayload {
		return recoveryFrame{}, fsapi.Invalid
	}
	f := recoveryFrame{op: h[5], sequence: binary.BigEndian.Uint32(h[8:]), payload: make([]byte, n)}
	return f, readRecoveryBytes(c, f.payload)
}
func readRecoveryBytes(c *net.UnixConn, b []byte) error {
	for len(b) > 0 {
		oob := make([]byte, unix.CmsgSpace(16*4))
		n, noob, flags, _, err := c.ReadMsgUnix(b, oob)
		if noob > 0 {
			messages, parseErr := unix.ParseSocketControlMessage(oob[:noob])
			if parseErr == nil {
				for _, m := range messages {
					fds, e := unix.ParseUnixRights(&m)
					if e == nil {
						for _, fd := range fds {
							_ = unix.Close(fd)
						}
					}
				}
			}
			return fsapi.Permission
		}
		if flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
			return fsapi.Permission
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrUnexpectedEOF
		}
		b = b[n:]
	}
	return nil
}
func restartDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(60 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return deadline
}
func (r *Restart) pauseExporter(ctx context.Context) error {
	s := r.supervisor
	s.mu.Lock()
	if s.child == nil || s.child.recovery == nil || s.checkLive() != nil {
		s.mu.Unlock()
		return fsapi.Unavailable
	}
	channel := s.child.recovery
	endpoint, controller := s.record.Endpoint, s.record.Controller
	s.mu.Unlock()
	if err := channel.SetDeadline(restartDeadline(ctx)); err != nil {
		return err
	}
	payload := append(endpointBytes(endpoint), controller[:]...)
	payload = append(payload, r.barrier[:]...)
	if err := writeRecovery(channel, 1, 1, payload); err != nil {
		return err
	}
	frame, err := readRecovery(channel)
	if err != nil {
		return err
	}
	if frame.op != 0x81 || frame.sequence != 1 || len(frame.payload) != 40 {
		return fsapi.Stale
	}
	total := binary.BigEndian.Uint64(frame.payload)
	if total < 64 || total > exporterRestartLimit {
		return fsapi.NoSpace
	}
	copy(r.exporterDigest[:], frame.payload[8:])
	s.mu.Lock()
	file, err := ownedFile(s.directory, "restart-exporter", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, 0600)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	r.exporter = file // Preserve failed resources and their actual retained descriptor.
	for offset := uint64(0); offset < total; {
		frame, err = readRecovery(channel)
		if err != nil {
			return err
		}
		if frame.op != 0x83 || frame.sequence != 1 || len(frame.payload) <= 8 || binary.BigEndian.Uint64(frame.payload) != offset || uint64(len(frame.payload)-8) > total-offset {
			return fsapi.Stale
		}
		n, e := file.Write(frame.payload[8:])
		if e != nil {
			return e
		}
		if n != len(frame.payload)-8 {
			return io.ErrShortWrite
		}
		offset += uint64(n)
	}
	if err = file.Sync(); err != nil {
		return err
	}
	r.exporterBytes = int64(total)
	return nil
}
func (r *Restart) bindExporter(ctx context.Context) error {
	digest, err := decodeHash(r.funding.State().Digest)
	if err != nil {
		return err
	}
	r.bound = append(append(append([]byte{}, r.barrier[:]...), digest...), r.exporterDigest[:]...)
	channel := r.supervisor.child.recovery
	if err = channel.SetDeadline(restartDeadline(ctx)); err != nil {
		return err
	}
	if err = writeRecovery(channel, 2, 2, r.bound); err != nil {
		return err
	}
	response, err := readRecovery(channel)
	if err != nil {
		return err
	}
	if response.op != 0x82 || response.sequence != 2 || string(response.payload) != string(r.bound) {
		return fsapi.Stale
	}
	return nil
}
func (r *Restart) resumeExporter(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	channel := r.supervisor.child.recovery
	if err := channel.SetDeadline(restartDeadline(ctx)); err != nil {
		return err
	}
	if err := writeRecovery(channel, 3, 3, r.bound); err != nil {
		return err
	}
	response, err := readRecovery(channel)
	if err != nil {
		return err
	}
	if response.op != 0x84 || response.sequence != 3 || string(response.payload) != string(r.bound) {
		return fsapi.Stale
	}
	return nil
}
