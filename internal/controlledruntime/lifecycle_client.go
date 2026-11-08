//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// lifecycleClient holds the explicit client-mode flags. Every binding value is
// copied from the runtime's own READY/OBSERVE records; the client invents none.
type lifecycleClient struct {
	socket, operation, runtime, cut string
	session, lostOperation          string
	sequence, epoch                 uint64
}

func lifecycleClientFlags(f *flag.FlagSet) *lifecycleClient {
	c := &lifecycleClient{}
	f.StringVar(&c.socket, "control", "", "client mode: private lifecycle socket of a READY runtime")
	f.StringVar(&c.operation, "operation", "", "client mode: observe, restart or arm-lost-reply")
	f.StringVar(&c.runtime, "runtime", "", "client mode: server-issued runtime UUID")
	f.StringVar(&c.cut, "cut", "", "client mode (restart, arm-lost-reply): server-issued current cut UUID")
	f.StringVar(&c.session, "session", "", "client mode (arm-lost-reply): enrolled session UUID")
	f.StringVar(&c.lostOperation, "lost-operation", "", "client mode (arm-lost-reply): CALL")
	f.Uint64Var(&c.sequence, "sequence", 0, "client mode: strictly increasing command sequence")
	f.Uint64Var(&c.epoch, "epoch", 0, "client mode (restart, arm-lost-reply): server-issued current epoch")
	return c
}

// runLifecycleClient sends exactly one HSC1 request and prints the raw reply.
// A refused or frame-level refusal reply is printed and returned as an error.
func runLifecycleClient(ctx context.Context, f *flag.FlagSet, c *lifecycleClient, stdout io.Writer) error {
	request, err := c.request(f)
	if err != nil {
		return err
	}
	reply, raw, err := lifecycleCommand(ctx, c.socket, request)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(stdout, "%s\n", raw); err != nil {
		return err
	}
	var status struct{ Status string }
	if reply != request.opcode|0x80 || json.Unmarshal(raw, &status) != nil || status.Status == "REFUSED" {
		return fmt.Errorf("lifecycle command refused: %w", fsapi.Permission)
	}
	return nil
}

// request validates the closed flag set for the selected operation and builds
// the one HSC1 request. Flags belonging to another operation refuse.
func (c *lifecycleClient) request(f *flag.FlagSet) (lifecycleRequest, error) {
	bound := c.operation == "restart" || c.operation == "arm-lost-reply"
	arm := c.operation == "arm-lost-reply"
	allowed := map[string]bool{"control": true, "operation": true, "runtime": true, "sequence": true, "cut": bound, "epoch": bound, "session": arm, "lost-operation": arm}
	extra := false
	f.Visit(func(fl *flag.Flag) { extra = extra || !allowed[fl.Name] })
	runtime, err := fsapi.ParseUUID(c.runtime)
	if extra || f.NArg() != 0 || err != nil || c.sequence == 0 || c.sequence > uint64(^uint32(0)) {
		return lifecycleRequest{}, fmt.Errorf("client requires --control SOCKET --operation observe|restart|arm-lost-reply --runtime UUID --sequence N [--cut UUID --epoch N [--session UUID --lost-operation CALL]]")
	}
	r := lifecycleRequest{opcode: LifecycleObserve, sequence: uint32(c.sequence), runtime: runtime, epoch: c.epoch}
	switch c.operation {
	case "observe":
		return r, nil
	case "restart":
		r.opcode = LifecycleRestart
	case "arm-lost-reply":
		r.opcode = LifecycleArmLostReply
		if r.session, err = fsapi.ParseUUID(c.session); err != nil || c.lostOperation != guestbridge.LostReplyOperation {
			return lifecycleRequest{}, fmt.Errorf("arm-lost-reply requires an enrolled --session and --lost-operation CALL")
		}
		r.operation = lostReplyOperationCall
	default:
		return lifecycleRequest{}, fmt.Errorf("unknown --operation %q", c.operation)
	}
	if r.cut, err = fsapi.ParseUUID(c.cut); err != nil || c.epoch == 0 {
		return lifecycleRequest{}, fmt.Errorf("%s requires the current --cut and --epoch", c.operation)
	}
	return r, nil
}

// lifecycleCommand performs one bounded request/reply exchange. The reply
// length is checked against lifecycleReplyBytes before allocation.
func lifecycleCommand(ctx context.Context, socket string, r lifecycleRequest) (uint8, []byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = conn.Close() }()
	deadline := time.Now().Add(lifecycleRestartTime + 2*lifecycleIOTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return 0, nil, err
	}
	if err = writeLifecycleFrame(conn, r.opcode, r.sequence, encodeLifecyclePayload(r)); err != nil {
		return 0, nil, err
	}
	return readLifecycleReply(conn)
}

func readLifecycleReply(r io.Reader) (uint8, []byte, error) {
	var header [lifecycleHeaderBytes]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(header[12:])
	if string(header[:4]) != "HSC1" || header[4] != LifecycleVersion || header[6] != 0 || header[7] != 0 || length > lifecycleReplyBytes {
		return 0, nil, fsapi.Invalid
	}
	raw := make([]byte, length)
	if _, err := io.ReadFull(r, raw); err != nil {
		return 0, nil, err
	}
	return header[5], raw, nil
}
