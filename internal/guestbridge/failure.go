package guestbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// A failed transport answers every later Read and Write with Unavailable and
// never dispatches again. Before nc4 nothing said why: zq3-r12 R12-1 fenced
// the runtime transport about 23.5 s into the guest boot, runtime.stderr stayed
// empty and the frozen transport.db only showed where the traffic stopped. The
// first fence now leaves one bounded, content-free record in three places:
//
//   - one line on the runtime's stderr (failureLog, os.Stderr by default);
//   - the transport section of the lifecycle evidence (Transport.Failure);
//   - a best-effort "failure" row in the owned transport.db, so a lab owner
//     snapshot shows it even when nobody observed the live runtime.
//
// The row is written next to the last protected cut and never changes it:
// the index and cell rows keep their exact bytes. A ledger whose own commit
// failed may refuse this write as well; stderr and evidence still have it.

// transportFailureKey names the optional failure row. It cannot collide with
// the index, a session row ("session:") or a nine-byte cell key ('c' + ID).
var transportFailureKey = []byte("failure")

const (
	// transportFailureTextLimit bounds the copied error text. The fencing
	// paths fail with errno names, bbolt/OS errors or Go codec errors, none of
	// which echo request or response bytes; the bound still keeps a
	// pathological chain from growing the row or the log line.
	transportFailureTextLimit = 256
	// transportFailureSiteLimit bounds the recorded code location.
	transportFailureSiteLimit = 160
	// transportFailureCellLimit bounds the recorded cell numbers. One
	// protection names at most a cell and its reclaim target, or the donors
	// of one terminal manifest.
	transportFailureCellLimit = 8
	// transportFailureRowLimit bounds the encoded row during inspection.
	transportFailureRowLimit = 1 << 10
)

// TransportFailure is the inert record of the first fence of a transport.
// Site is the code location that fenced it (for a failed protection, the
// caller of the protection), Cells the native cells it was protecting or
// publishing, Class the fsapi errno name or Go error type (as BlockedError)
// and Error the bounded error text. Time is the host UTC time of the fence.
// It grants no authority and is never used for restoration.
type TransportFailure struct {
	Site  string
	Cells []uint64 `json:",omitempty"`
	Class string
	Error string
	Time  string
}

// Failure returns the first fence of this transport, if any.
func (t *Transport) Failure() (TransportFailure, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.failure == nil {
		return TransportFailure{}, false
	}
	f := *t.failure
	f.Cells = append([]uint64(nil), f.Cells...)
	return f, true
}

// failLocked fences the transport with err and records why, exactly once:
// a transport that is already failed keeps its first reason, and later
// refusals (Unavailable) are consequences, not new causes. site names the
// fencing code location (see callerSite). The caller holds t.mu, or is the
// constructor before any worker started.
func (t *Transport) failLocked(site string, err error, cells ...uint64) {
	if err == nil || t.failed != nil {
		return
	}
	t.failed = err
	f := newTransportFailure(site, err, cells)
	t.failure = &f
	durable := "not recorded (volatile transport)"
	if t.ledger != nil {
		durable = "recorded in transport.db"
		if e := t.ledger.recordFailure(f); e != nil {
			durable = "transport.db record failed: " + boundText(e.Error(), transportFailureTextLimit)
		}
	}
	log := t.failureLog
	if log == nil {
		log = os.Stderr
	}
	_, _ = fmt.Fprintf(log, "guestbridge: transport failed at %s cells=%v class=%s error=%q time=%s; %s\n", f.Site, f.Cells, f.Class, f.Error, f.Time, durable)
}

func newTransportFailure(site string, err error, cells []uint64) TransportFailure {
	if len(cells) > transportFailureCellLimit {
		cells = cells[:transportFailureCellLimit]
	}
	return TransportFailure{Site: boundText(site, transportFailureSiteLimit), Cells: append([]uint64(nil), cells...), Class: blockedErrorClass(err), Error: boundText(err.Error(), transportFailureTextLimit), Time: time.Now().UTC().Format(time.RFC3339Nano)}
}

// callerSite names the function and file:line skip frames above its caller:
// callerSite(0) is the function calling callerSite, callerSite(1) its caller.
// Only the package-relative function name and the file's base name are kept.
func callerSite(skip int) string {
	pc, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}
	name := "unknown"
	if fn := runtime.FuncForPC(pc); fn != nil {
		name = fn.Name()
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
	}
	return fmt.Sprintf("%s (%s:%d)", name, filepath.Base(file), line)
}

// boundText truncates s to limit bytes and replaces control characters, so
// one record stays one log line.
func boundText(s string, limit int) string {
	if len(s) > limit {
		s = s[:limit]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

// recordFailure writes the failure row in its own transaction. It never
// touches the index or a cell row, and it refuses when the file is already at
// its operating ceiling.
func (l *transportLedger) recordFailure(f TransportFailure) error {
	if l == nil || l.db == nil || l.file == nil {
		return fsapi.Unavailable
	}
	info, err := l.file.Stat()
	if err != nil {
		return err
	}
	if info.Size() >= transportFileCeiling {
		return fsapi.NoSpace
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return l.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(transportBucket)
		if b == nil {
			return fsapi.Stale
		}
		return b.Put(transportFailureKey, raw)
	})
}

// inspectFailure decodes and bounds an optional failure row.
func inspectFailure(raw []byte) (*TransportFailure, error) {
	if raw == nil {
		return nil, nil
	}
	var f TransportFailure
	if err := decodeTransportJSON(raw, transportFailureRowLimit, &f); err != nil {
		return nil, err
	}
	if f.Site == "" || len(f.Site) > transportFailureSiteLimit || f.Class == "" || len(f.Class) > blockedErrorLimit || len(f.Error) > transportFailureTextLimit || len(f.Cells) > transportFailureCellLimit || len(f.Time) > 64 {
		return nil, fsapi.Stale
	}
	return &f, nil
}
