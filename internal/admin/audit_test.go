package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAuditDurablePrivateAndBounded(t *testing.T) {
	c := testConfig(t)
	name := filepath.Join(filepath.Dir(c.SocketPath), "audit.jsonl")
	log, err := OpenAudit(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	event := Event{Peer: Peer{UID: 123}, VolumeID: "volume", Generation: "generation", Operation: "test-fault", Outcome: "requested"}
	if err := log.Record(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var observed Event
	if err := json.Unmarshal(raw, &observed); err != nil || observed.Peer.UID != 123 {
		t.Fatal(err, observed)
	}
	if err := os.Truncate(name, maxAuditBytes); err != nil {
		t.Fatal(err)
	}
	if err := log.Record(t.Context(), event); err == nil {
		t.Fatal("audit grew past bound")
	}
	for _, scenario := range []string{"public", "hardlink", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			c := testConfig(t)
			name := filepath.Join(filepath.Dir(c.SocketPath), "audit")
			if err := os.WriteFile(name, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "public":
				if err := os.Chmod(name, 0o644); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(name, name+"-link"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(name, name+"-link"); err != nil {
					t.Fatal(err)
				}
				name += "-link"
			}
			if l, err := OpenAudit(t.Context(), name); err == nil {
				_ = l.Close()
				t.Fatal("accepted unsafe audit")
			}
		})
	}
}

// faultingAuditFile changes only one syscall result while retaining a real
// bounded temporary file. It does not expose a daemon/configuration failpoint.
type faultingAuditFile struct {
	*os.File
	mu    sync.Mutex
	fault string
}

func (f *faultingAuditFile) Write(raw []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch f.fault {
	case "short-write":
		return f.File.Write(raw[:len(raw)/2])
	case "partial-error":
		n, err := f.File.Write(raw[:len(raw)/2])
		if err != nil {
			return n, err
		}
		return n, errors.New("injected uncertain partial append")
	}
	return f.File.Write(raw)
}
func (f *faultingAuditFile) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.File.Sync(); err != nil {
		return err
	}
	if f.fault == "sync-error" {
		return errors.New("injected uncertain sync result")
	}
	return nil
}

func (f *faultingAuditFile) clearFault() { f.mu.Lock(); defer f.mu.Unlock(); f.fault = "" }

func TestAuditFileFailuresRemainSticky(t *testing.T) {
	for _, fault := range []string{"stat-error", "write-error", "short-write", "partial-error", "sync-error", "capacity"} {
		t.Run(fault, func(t *testing.T) {
			c := testConfig(t)
			name := filepath.Join(filepath.Dir(c.SocketPath), "audit")
			log, err := OpenAudit(t.Context(), name)
			if err != nil {
				t.Fatal(err)
			}
			file := log.file.(*os.File)
			var injected *faultingAuditFile
			switch fault {
			case "stat-error":
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			case "write-error":
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
				readonly, err := os.Open(name)
				if err != nil {
					t.Fatal(err)
				}
				log.file = readonly
			case "capacity":
				if err := file.Truncate(maxAuditBytes); err != nil {
					t.Fatal(err)
				}
			default:
				injected = &faultingAuditFile{File: file, fault: fault}
				log.file = injected
			}
			if err := log.Record(t.Context(), Event{Operation: "test-fault", Outcome: "requested"}); err == nil {
				t.Fatal("expected file failure")
			}
			before, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			// Restore healthy I/O. A capacity refusal is tested again after space returns;
			// all other cases must leave their possibly partial bytes untouched.
			switch fault {
			case "stat-error", "write-error":
				_ = log.file.Close()
				healthy, err := os.OpenFile(name, os.O_RDWR|os.O_APPEND, 0)
				if err != nil {
					t.Fatal(err)
				}
				log.file = healthy
			case "capacity":
				if err := file.Truncate(0); err != nil {
					t.Fatal(err)
				}
				before = nil
			default:
				injected.clearFault()
			}
			if err := log.Record(t.Context(), Event{Operation: "test-fault", Outcome: "requested"}); err == nil || !strings.Contains(err.Error(), "earlier file failure") {
				t.Fatal("audit resumed after uncertainty", err)
			}
			after, err := os.ReadFile(name)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("poisoned handle appended", err)
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenAudit(t.Context(), name)
			incomplete := fault == "short-write" || fault == "partial-error"
			if incomplete {
				if err == nil {
					_ = reopened.Close()
					t.Fatal("reopened partial record")
				}
				preserved, readErr := os.ReadFile(name)
				if readErr != nil || !bytes.Equal(after, preserved) {
					t.Fatal("reopen changed failed evidence", readErr)
				}
			} else {
				if err != nil {
					t.Fatal("healthy complete audit restart refused", err)
				}
				if err := reopened.Record(t.Context(), Event{Operation: "test-fault", Outcome: "requested"}); err != nil {
					t.Fatal(err)
				}
				if err := reopened.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestAuditReopenValidatesCompleteEventLinesWithoutRepair(t *testing.T) {
	event := Event{Operation: "test-fault", Outcome: "requested"} // Empty identity values remain writer-compatible.
	record, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	valid := append(append([]byte{}, record...), '\n')
	for _, scenario := range []string{"complete", "no-newline", "partial-tail", "invalid-json", "invalid-event", "unknown-field", "multiple-objects", "null", "array", "blank-line", "invalid-utf8"} {
		t.Run(scenario, func(t *testing.T) {
			c := testConfig(t)
			name := filepath.Join(filepath.Dir(c.SocketPath), "audit")
			raw := append([]byte{}, valid...)
			switch scenario {
			case "complete":
				raw = append(raw, valid...)
			case "no-newline":
				raw = raw[:len(raw)-1]
			case "partial-tail":
				raw = append(raw, []byte(`{"time":`)...)
			case "invalid-json":
				raw = append(raw, []byte("{\n")...)
			case "invalid-event":
				raw = append(raw, []byte("{\"time\":3}\n")...)
			case "unknown-field":
				raw = append(raw, []byte("{\"unrecognized\":true}\n")...)
			case "multiple-objects":
				raw = append(raw, []byte("{} {}\n")...)
			case "null":
				raw = append(raw, []byte("null\n")...)
			case "array":
				raw = append(raw, []byte("[]\n")...)
			case "blank-line":
				raw = append(raw, '\n')
			case "invalid-utf8":
				raw = append(raw, []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}', '\n'}...)
			}
			if err := os.WriteFile(name, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			log, err := OpenAudit(t.Context(), name)
			if scenario == "complete" {
				if err != nil {
					t.Fatal(err)
				}
				if err := log.Record(t.Context(), event); err != nil {
					t.Fatal(err)
				}
				if err := log.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					_ = log.Close()
					t.Fatal("accepted corrupt/incomplete audit")
				}
				preserved, readErr := os.ReadFile(name)
				if readErr != nil || !bytes.Equal(raw, preserved) {
					t.Fatal("refusal repaired audit", readErr)
				}
			}
		})
	}
}

func TestPoisonedAuditDeniesControlsWhileStatusRemainsAvailable(t *testing.T) {
	c := testConfig(t)
	name := filepath.Join(filepath.Dir(c.SocketPath), "audit")
	log, err := OpenAudit(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	file := &faultingAuditFile{File: log.file.(*os.File), fault: "short-write"}
	log.file = file
	c.Audit = log.Record
	c.EnableTestHooks = true
	var toggles atomic.Int32
	c.ToggleFault = func(context.Context, string, bool) error { toggles.Add(1); return nil }
	server := testServer(t, c)
	defer func() { _ = server.Close() }()
	if _, err := call(t, c, Request{Operation: "test-fault", Fault: "example", Enabled: true}); err == nil {
		t.Fatal("uncertain audit permitted effect")
	}
	file.clearFault()
	before, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, c, Request{Operation: "test-fault", Fault: "example", Enabled: true}); err == nil {
		t.Fatal("poisoned audit permitted effect")
	}
	if response, err := call(t, c, Request{Operation: "status"}); err != nil || response.Status == nil {
		t.Fatal("audit failure blocked observation", response, err)
	}
	after, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(before, after) || toggles.Load() != 0 {
		t.Fatal("failed audit lost control gate/evidence", toggles.Load(), err)
	}
}
