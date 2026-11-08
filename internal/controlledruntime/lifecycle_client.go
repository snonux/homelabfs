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
)

// lifecycleClient holds the explicit client-mode flags. Every binding value is
// copied from the runtime's own READY/OBSERVE records; the client invents none.
type lifecycleClient struct {
	socket, operation, runtime, cut string
	sequence, epoch                 uint64
}

func lifecycleClientFlags(f *flag.FlagSet) *lifecycleClient {
	c := &lifecycleClient{}
	f.StringVar(&c.socket, "control", "", "client mode: private lifecycle socket of a READY runtime")
	f.StringVar(&c.operation, "operation", "", "client mode: observe or restart")
	f.StringVar(&c.runtime, "runtime", "", "client mode: server-issued runtime UUID")
	f.StringVar(&c.cut, "cut", "", "client mode (restart): server-issued current cut UUID")
	f.Uint64Var(&c.sequence, "sequence", 0, "client mode: strictly increasing command sequence")
	f.Uint64Var(&c.epoch, "epoch", 0, "client mode (restart): server-issued current epoch")
	return c
}

// runLifecycleClient sends exactly one HSC1 request and prints the raw reply.
// A refused or frame-level refusal reply is printed and returned as an error.
func runLifecycleClient(ctx context.Context, f *flag.FlagSet, c *lifecycleClient, stdout io.Writer) error {
	allowed := map[string]bool{"control": true, "operation": true, "runtime": true, "sequence": true, "cut": c.operation == "restart", "epoch": c.operation == "restart"}
	extra := false
	f.Visit(func(fl *flag.Flag) { extra = extra || !allowed[fl.Name] })
	runtime, err := fsapi.ParseUUID(c.runtime)
	if extra || f.NArg() != 0 || err != nil || c.sequence == 0 || c.sequence > uint64(^uint32(0)) {
		return fmt.Errorf("client requires --control SOCKET --operation observe|restart --runtime UUID --sequence N [--cut UUID --epoch N]")
	}
	var cut fsapi.UUID
	opcode := uint8(LifecycleObserve)
	switch c.operation {
	case "observe":
	case "restart":
		opcode = LifecycleRestart
		if cut, err = fsapi.ParseUUID(c.cut); err != nil || c.epoch == 0 {
			return fmt.Errorf("restart requires the current --cut and --epoch")
		}
	default:
		return fmt.Errorf("unknown --operation %q", c.operation)
	}
	reply, raw, err := lifecycleCommand(ctx, c.socket, opcode, uint32(c.sequence), runtime, cut, c.epoch)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(stdout, "%s\n", raw); err != nil {
		return err
	}
	var status struct{ Status string }
	if reply != opcode|0x80 || json.Unmarshal(raw, &status) != nil || status.Status == "REFUSED" {
		return fmt.Errorf("lifecycle command refused: %w", fsapi.Permission)
	}
	return nil
}

// lifecycleCommand performs one bounded request/reply exchange. The reply
// length is checked against lifecycleReplyBytes before allocation.
func lifecycleCommand(ctx context.Context, socket string, opcode uint8, sequence uint32, runtime, cut fsapi.UUID, epoch uint64) (uint8, []byte, error) {
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
	payload := append([]byte(nil), runtime[:]...)
	if opcode == LifecycleRestart {
		payload = append(payload, cut[:]...)
		payload = binary.BigEndian.AppendUint64(payload, epoch)
	}
	if err = writeLifecycleFrame(conn, opcode, sequence, payload); err != nil {
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
