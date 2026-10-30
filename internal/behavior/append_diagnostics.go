package behavior

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Append diagnostics are emitted only by the CLI helper, on its separate stderr
// stream. Each command has at most 38 records (open, 16 writes, fsync, close,
// each before/after). No pathname or payload is logged. A missing end record
// localizes the last observed boundary; it does not prove a syscall is blocked.
type appendDiagnostics struct {
	encoder *json.Encoder
	started time.Time
	marker  string
	err     error
}

type appendDiagnostic struct {
	Event     string    `json:"event"`
	Time      time.Time `json:"time"`
	PID       int       `json:"pid"`
	UID       int       `json:"uid"`
	Marker    string    `json:"marker"`
	Phase     string    `json:"phase"`
	Record    int       `json:"record"`
	ElapsedNS int64     `json:"elapsed_ns"`
	Errno     int       `json:"errno,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func newAppendDiagnostics(out io.Writer, marker string) *appendDiagnostics {
	if out == nil {
		return nil
	}
	return &appendDiagnostics{encoder: json.NewEncoder(out), started: time.Now(), marker: marker}
}

func (d *appendDiagnostics) record(phase string, record int, err error) {
	if d == nil || d.err != nil {
		return
	}
	now := time.Now()
	event := appendDiagnostic{Event: "behavior-append", Time: now.UTC(), PID: os.Getpid(), UID: os.Geteuid(), Marker: d.marker, Phase: phase, Record: record, ElapsedNS: now.Sub(d.started).Nanoseconds()}
	if err != nil {
		event.Error = err.Error()
		var errno unix.Errno
		if errors.As(err, &errno) {
			event.Errno = int(errno)
		}
	}
	if err := d.encoder.Encode(event); err != nil {
		d.err = fmt.Errorf("append diagnostic stream: %w", err)
	}
}

func (d *appendDiagnostics) failure() error {
	if d == nil {
		return nil
	}
	return d.err
}
