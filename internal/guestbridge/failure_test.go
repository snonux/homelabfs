package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// lockedBuffer is a failureLog that the test goroutine can read while a
// worker writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// writeCommand writes one complete minor-0 command and returns Write's error.
func writeCommand(t *testing.T, tr *Transport, n Native, c Command) error {
	t.Helper()
	raw := mustJSON(t, c)
	wire, err := EncodeFrame(Frame{Kind: 1, Total: uint32(len(raw)), Session: parseID(c.Context.Session), Cell: c.Cell, Hash: sha256.Sum256(raw), Data: raw})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tr.Write(t.Context(), n, n.Endpoint.CommandNode, c.Cell*CellStride, wire)
	return err
}

// nc4: the first fence of a transport is reported exactly once on the
// failure log, kept for evidence and written as the ledger's failure row,
// which inspection returns and restoration refuses. Later refusals
// (Unavailable) and later fences do not replace or repeat it.
func TestTransportFailureRecordedOnce(t *testing.T) {
	f, tr, root := durableFixture(t)
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 1)
	log := &lockedBuffer{}
	injected := errors.New("injected commit failure")
	tr.mu.Lock()
	tr.failureLog = log
	tr.ledger.failpoint = func(name string) error {
		if name == "before-commit" {
			return injected
		}
		return nil
	}
	tr.mu.Unlock()
	second := helloCommand(t, f)
	second.Cell = 2
	if err := writeCommand(t, tr, f.native, second); !errors.Is(err, injected) {
		t.Fatal("failed protection was not reported to the writer", err)
	}
	if _, err := tr.Read(t.Context(), f.native, f.native.Endpoint.ResultNode, CellStride, 32768); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("fenced transport still exposed a response", err)
	}
	tr.mu.Lock()
	tr.failLocked("later fence", fsapi.Stale, 9)
	tr.mu.Unlock()
	failure, failed := tr.Failure()
	if !failed || !strings.Contains(failure.Site, "protection from guestbridge.(*Transport).commitWrite") || len(failure.Cells) != 1 || failure.Cells[0] != 2 || failure.Class != "UNCLASSIFIED:*errors.errorString" || failure.Error != injected.Error() || failure.Time == "" {
		t.Fatalf("failure record %+v", failure)
	}
	lines := strings.Split(strings.TrimSpace(log.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "cells=[2]") || !strings.Contains(lines[0], injected.Error()) || !strings.Contains(lines[0], "recorded in transport.db") {
		t.Fatalf("failure log %q", log.String())
	}
	tr.Close()
	state, cells := inspectCells(t, root)
	if state.Failure == nil || state.Failure.Site != failure.Site || state.Failure.Error != failure.Error || len(cells) != 1 || cells[0].Header.Cell != 1 {
		t.Fatalf("durable failure row %+v cells %d", state.Failure, len(cells))
	}
	if _, err := InspectRestorationEndpoint(root, nil, nil); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("fenced ledger offered for restoration", err)
	}
}

// The failure row is bounded and content-free: oversized or unknown fields
// make the ledger unreadable as evidence rather than silently accepted.
func TestTransportFailureRowBounds(t *testing.T) {
	long := strings.Repeat("x", transportFailureTextLimit+1)
	record := newTransportFailure(long, errors.New(long+"\nsecond line"), []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9})
	if len(record.Site) != transportFailureSiteLimit || len(record.Error) != transportFailureTextLimit || len(record.Cells) != transportFailureCellLimit || strings.ContainsAny(record.Error, "\n") {
		t.Fatalf("record not bounded %+v", record)
	}
	raw := mustJSON(t, record)
	if got, err := inspectFailure(raw); err != nil || got.Site != record.Site {
		t.Fatal("bounded record refused", err)
	}
	// A multi-byte rune across the limit, or invalid UTF-8, must not grow the
	// text past the limit, or inspection would refuse the evidence row.
	for _, text := range []string{strings.Repeat("a", transportFailureTextLimit-1) + "é", strings.Repeat("\xff", transportFailureTextLimit+8), strings.Repeat("é", transportFailureTextLimit)} {
		record = newTransportFailure("site", errors.New(text), nil)
		if len(record.Error) > transportFailureTextLimit || !utf8.ValidString(record.Error) {
			t.Fatalf("error text not bounded: %d bytes %q", len(record.Error), record.Error)
		}
		if _, err := inspectFailure(mustJSON(t, record)); err != nil {
			t.Fatalf("bounded record %q refused: %v", record.Error, err)
		}
	}
	for _, bad := range []string{`{"Site":"","Class":"STALE","Error":"","Time":""}`, `{"Site":"s","Class":"STALE","Error":"","Time":"","Extra":1}`} {
		if _, err := inspectFailure([]byte(bad)); err == nil {
			t.Fatalf("invalid failure row accepted: %s", bad)
		}
	}
}

// A retried mapped cell whose retained base was lost (or whose operation ran
// again with a different answer) still fences: a protected leg never changes.
// The fence now records why (see failure_test.go for the record itself).
func TestMappedBaseChangeStillFences(t *testing.T) {
	x, tr, _ := mappedWaitFixture(t)
	if err := mappingWrite(t, tr, x.bridge.native, waitLeg(t, x, 1, 0, nil), 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	_ = fetchNativeAt(t, tr, x.bridge.native, 1, x.bridge.native.Endpoint.RecallNode)
	tr.mu.Lock()
	c := tr.cells[1]
	c.response, c.wire, c.seen, c.unread, c.resultKind = nil, nil, nil, 0, 0
	c.mapping.NativeFullHash, c.mapping.GuestFullHash = "", ""
	c.jobPhase = "executing"
	invocation := &workerInvocation{live: true}
	c.invocation = invocation
	command := *c.command
	tr.failureLog = &lockedBuffer{}
	tr.mu.Unlock()
	raw, err := marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), mappingWorkerKey{}, mappingWorker{transport: tr, cell: 1, invocation: invocation})
	ctx = observeCommand(ctx, x.bridge.native, tr.service.controller, command, raw)
	changed, err := marshal(map[string]any{"cell": command.Cell, "context": command.Context, "op": command.Op, "payload": json.RawMessage(`{"event":null,"status":"OTHER"}`), "protocol": command.Protocol})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tr.finishMappingBase(ctx, command, changed); !errors.Is(err, fsapi.Stale) {
		t.Fatal("changed base accepted", err)
	}
	failure, failed := tr.Failure()
	if !failed || failure.Class != "STALE" || len(failure.Cells) != 1 || failure.Cells[0] != 1 {
		t.Fatalf("changed base fence not recorded: %+v", failure)
	}
}
