package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxAuditBytes = 4 << 20

type auditFile interface {
	Stat() (os.FileInfo, error)
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// AuditLog is a private append-only, synced, bounded local control audit.
// File errors permanently deny controls through this handle; rotation is external.
type AuditLog struct {
	mu      sync.Mutex
	file    auditFile
	failure error
}

// OpenAudit opens or creates an owned, singly linked mode 0600 file.
func OpenAudit(ctx context.Context, name string) (*AuditLog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := privateParent(name); err != nil {
		return nil, err
	}
	fd, err := unix.Open(name, unix.O_RDWR|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0o600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		_ = f.Close()
		return nil, err
	}
	if st.Mode != unix.S_IFREG|0o600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Size > maxAuditBytes {
		_ = f.Close()
		return nil, fmt.Errorf("audit must be bounded owned singly linked mode 0600 file")
	}
	if err := validateAudit(ctx, f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("existing audit preserved: %w", err)
	}
	// Persist a newly created directory entry before any control is acknowledged.
	dir, err := os.Open(filepath.Dir(name))
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil || closeErr != nil {
		_ = f.Close()
		return nil, errors.Join(syncErr, closeErr)
	}
	return &AuditLog{file: f}, nil
}

// Record durably appends the event before returning success. The first file
// failure permanently disables this handle; later calls cannot append behind
// an uncertain partial record. Context/marshal refusals do not touch the file.
func (a *AuditLog) Record(ctx context.Context, event Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failure != nil {
		return fmt.Errorf("audit disabled after earlier file failure: %w", a.failure)
	}
	st, err := a.file.Stat()
	if err != nil {
		return a.poison(err)
	}
	if st.Size()+int64(len(raw)) > maxAuditBytes {
		return a.poison(fmt.Errorf("audit capacity exhausted"))
	}
	n, err := a.file.Write(raw)
	if err != nil {
		return a.poison(err)
	}
	if n != len(raw) {
		return a.poison(io.ErrShortWrite)
	}
	if err := a.file.Sync(); err != nil {
		return a.poison(err)
	}
	return nil
}

// Close releases the audit after the listener has drained requests.
func (a *AuditLog) Close() error { a.mu.Lock(); defer a.mu.Unlock(); return a.file.Close() }

// poison is called only while mu is held. A later filesystem recovery must not
// turn an uncertain append into a successful control in this daemon lifetime.
func (a *AuditLog) poison(err error) error {
	a.failure = err
	return err
}

func validateAudit(ctx context.Context, f *os.File) error {
	raw, err := io.ReadAll(io.LimitReader(f, maxAuditBytes+1))
	if err != nil {
		return err
	}
	if len(raw) > maxAuditBytes || !utf8.Valid(raw) {
		return fmt.Errorf("audit must be bounded UTF-8 JSONL")
	}
	if len(raw) == 0 {
		return nil
	}
	if raw[len(raw)-1] != '\n' {
		return fmt.Errorf("incomplete audit tail")
	}
	for line := range bytes.SplitSeq(raw[:len(raw)-1], []byte{'\n'}) {
		if err := ctx.Err(); err != nil {
			return err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			return fmt.Errorf("audit record must be a JSON Event object")
		}
		d := json.NewDecoder(bytes.NewReader(line))
		d.DisallowUnknownFields()
		var event Event
		if err := d.Decode(&event); err != nil {
			return fmt.Errorf("invalid audit record: %w", err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return fmt.Errorf("trailing audit record data")
		}
	}
	return nil
}
