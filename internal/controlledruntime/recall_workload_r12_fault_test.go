//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Transport save failure injection for the zq3-r12 replay
// (recall_workload_r12_test.go, task qc4).

// saveFault fails exactly one transport.db save: the first protection whose
// caller is guestbridge.(*Transport).<site>, at the ledger's before-commit
// failpoint (nothing of that protection is committed). log stands in for the
// runtime's stderr, where failLocked reports the fence. injected is closed
// when the hook returns the failure; the saving goroutine still holds the
// transport's lock then and records the fence (failLocked) before it lets
// go, so a Failure() call after injected sees the record.
type saveFault struct {
	site     string
	err      error
	log      *syncBuffer
	fired    atomic.Bool
	injected chan struct{}
}

func newSaveFault(site string) *saveFault {
	return &saveFault{site: site, err: errors.New("injected transport.db save failure"), log: &syncBuffer{}, injected: make(chan struct{})}
}

// hook is the ledger failpoint. It runs with the transport's lock held, on
// whichever goroutine saves (the writer for commitWrite, a lane worker for
// publishResponse), so it selects its target by the caller on the stack.
// It cannot see the cell being saved: injectSaveFailure arms it only when
// the targeted cell alone can reach the site, and requireFailureRecord
// checks the recorded cells, so another cell getting there first fails the
// test instead of passing it.
func (f *saveFault) hook(name string) error {
	if name == "before-commit" && onStack("guestbridge.(*Transport)."+f.site) && f.fired.CompareAndSwap(false, true) {
		close(f.injected)
		return f.err
	}
	return nil
}

// onStack reports whether a function named fn (package-qualified suffix) is
// on the calling goroutine's stack.
func onStack(fn string) bool {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, fn) {
			return true
		}
		if !more {
			return false
		}
	}
}

// armTransportFault installs fault on the assembled transport's ledger
// failpoint and failure log. Assemble builds its transport with
// NewDurableTransport, which takes no failure hook (only
// NewDurableTransportWithFailureHook does, at construction), and this task
// changes no production code, so the test sets the two unexported fields by
// reflection under the transport's own lock, which every save and every
// failLocked holds. A renamed or retyped field fails the test with a message
// (unexportedField), not with a reflect panic.
func armTransportFault(t *testing.T, tr *guestbridge.Transport, fault *saveFault) {
	t.Helper()
	v := reflect.ValueOf(tr).Elem()
	hook := fault.hook
	mu := unexportedField(t, v, "guestbridge.Transport", "mu", reflect.TypeFor[sync.Mutex]())
	failureLog := unexportedField(t, v, "guestbridge.Transport", "failureLog", reflect.TypeFor[io.Writer]())
	ledger := v.FieldByName("ledger")
	if !ledger.IsValid() || ledger.Kind() != reflect.Pointer || ledger.IsNil() {
		t.Fatal(`guestbridge.Transport has no durable "ledger" (renamed, retyped or a volatile transport): update armTransportFault`)
	}
	failpoint := unexportedField(t, ledger.Elem(), "guestbridge.transportLedger", "failpoint", reflect.TypeOf(hook))
	lock := (*sync.Mutex)(unsafe.Pointer(mu.UnsafeAddr()))
	lock.Lock()
	defer lock.Unlock()
	reflect.NewAt(failpoint.Type(), unsafe.Pointer(failpoint.UnsafeAddr())).Elem().Set(reflect.ValueOf(hook))
	var log io.Writer = fault.log
	reflect.NewAt(failureLog.Type(), unsafe.Pointer(failureLog.UnsafeAddr())).Elem().Set(reflect.ValueOf(&log).Elem())
}

// unexportedField returns v's field name, failing the test with a clear
// message when it no longer exists or no longer has type want.
func unexportedField(t *testing.T, v reflect.Value, owner, name string, want reflect.Type) reflect.Value {
	t.Helper()
	f := v.FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("%s has no field %q any more: update armTransportFault", owner, name)
	}
	if f.Type() != want {
		t.Fatalf("%s.%s is %s, want %s: update armTransportFault", owner, name, f.Type(), want)
	}
	return f
}

// injectSaveFailure arms fault at its site with m1's next VIEW_WAIT leg
// outstanding and returns that leg and the native cells the failed save
// protected:
//   - commitWrite: the leg is written first, then the mount thread's next
//     VIEW_QUERY, whose save (the new cell and the previous VIEW_QUERY it
//     reclaims, zq3-r12's write 170 releasing 168) fails; the writer gets
//     the error, so the exporter answers that SEND with an ERROR at once;
//   - publishResponse: the leg's response publication (zq3-r12's leg 169)
//     fails on its lane worker; the leg stays unanswered. While the hook is
//     armed no other cell can publish: every earlier cell was answered and
//     fetched (its publication is done) and the STOPPED ACK is still held
//     in Prepare, before its operation and publication.
func (r *r12Run) injectSaveFailure(fault *saveFault) (*writtenLeg, []uint64) {
	if fault.site == "publishResponse" {
		armTransportFault(r.t, r.m.a.Transport, fault)
		leg, err := r.wait.write()
		require(r.t, err)
		r.awaitFence(fault)
		return leg, []uint64{leg.native.Cell}
	}
	leg, err := r.wait.write()
	require(r.t, err)
	armTransportFault(r.t, r.m.a.Transport, fault)
	chain := r.m1.chain
	reclaimed := chain.native.Cell
	chain.peer.cells++
	_, err = chain.peer.writeLeg(chain.peer.cells, 0, "VIEW_QUERY", map[string]any{"request": r.control}, chain.guest, chain.native, nil)
	if !errors.Is(err, fault.err) {
		r.t.Fatalf("mount thread's VIEW_QUERY write: %v, want the injected save failure", err)
	}
	return leg, []uint64{*chain.peer.mappingNativeCell, reclaimed}
}

// awaitFence waits until a lane worker's save hit fault and fenced the
// transport (saveFault.injected says why Failure is recorded by then).
func (r *r12Run) awaitFence(fault *saveFault) {
	select {
	case <-fault.injected:
	case <-time.After(answerBound):
		r.t.Fatal("the injected save failure was never reached")
	}
	if _, failed := r.m.a.Transport.Failure(); !failed {
		r.t.Fatal("the injected save failure did not fence the transport")
	}
}

// requireGuestErrors requires the guest-visible outcome R12-1 lacked: every
// exchange of either mount fails at once with Unavailable. The outstanding
// VIEW_WAIT leg's fetch, the mount thread's next VIEW_QUERY, m0's next
// VIEW_WAIT and, once its observation ends, the STOPPED ACK's fetch are each
// one nonblocking call.
func (r *r12Run) requireGuestErrors(leg *writtenLeg) {
	tr, peer := r.m.a.Transport, r.m1.peer
	_, err := tr.Read(r.t.Context(), peer.native, peer.native.Endpoint.RecallNode, leg.native.Cell*guestbridge.CellStride, 32768)
	requireUnavailable(r.t, "m1's outstanding VIEW_WAIT leg fetch", err)
	chain := r.m1.chain
	chain.peer.cells++
	_, err = chain.peer.writeLeg(chain.peer.cells, 0, "VIEW_QUERY", map[string]any{"request": r.control}, chain.guest, chain.native, nil)
	requireUnavailable(r.t, "m1's next VIEW_QUERY write", err)
	m0 := r.m.mounts[0]
	m0.peer.cells++
	_, err = m0.peer.writeLeg(m0.peer.cells, 0, "VIEW_WAIT", map[string]any{"domain": m0.domain, "after_sequence": 0}, m0.chain.guest, m0.chain.native, nil)
	requireUnavailable(r.t, "m0's next VIEW_WAIT write", err)
	r.held.gate.open()
	_, err = tr.Read(r.t.Context(), peer.native, peer.native.Endpoint.ResultNode, r.ack.native.Cell*guestbridge.CellStride, 32768)
	requireUnavailable(r.t, "m1's STOPPED ACK fetch", err)
}

// requireFailureRecord requires the fence's one reason (site, class, error,
// the cells of the failed save) in every place nc4 records it: the failure
// log (one line, the runtime's stderr), the lifecycle evidence's transport
// section (OBSERVE transport.failure) and, after the transport closed, the
// transport.db failure row, which restoration refuses.
func (r *r12Run) requireFailureRecord(fault *saveFault, cells []uint64) {
	t := r.t
	failure, failed := r.m.a.Transport.Failure()
	if !failed || !strings.Contains(failure.Site, "protection from guestbridge.(*Transport)."+fault.site) || failure.Class != "UNCLASSIFIED:*errors.errorString" || failure.Error != fault.err.Error() || !slices.Equal(failure.Cells, cells) || failure.Time == "" {
		t.Fatalf("transport failure %+v (failed %v), want site %s cells %v", failure, failed, fault.site, cells)
	}
	lines := strings.Split(strings.TrimSpace(fault.log.String()), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], failureLogFields(failure)) || !strings.Contains(lines[0], "recorded in transport.db") {
		t.Errorf("failure log (runtime stderr) %q, want one line with %q", fault.log.String(), failureLogFields(failure))
	}
	evidence := captureTransport(r.m.a.Transport)
	raw, err := json.Marshal(evidence)
	require(t, err)
	if evidence.Failure == nil || !reflect.DeepEqual(*evidence.Failure, failure) || !bytes.Contains(raw, []byte(`"failure":{`)) {
		t.Errorf("OBSERVE transport section %s, want failure %+v", raw, failure)
	}
	r.m.a.Transport.Close()
	root := filepath.Join(r.m.directory, "transport")
	state, err := guestbridge.InspectTransport(root, nil)
	require(t, err)
	if state.Failure == nil || !reflect.DeepEqual(*state.Failure, failure) {
		t.Errorf("transport.db failure row %+v, want %+v", state.Failure, failure)
	}
	if _, err = guestbridge.InspectRestorationEndpoint(root, nil, nil); !errors.Is(err, fsapi.Unavailable) {
		t.Errorf("fenced transport.db offered for restoration: %v", err)
	}
	t.Logf("fence recorded: %s; OBSERVE %s", lines[0], raw)
}

// failureLogFields is the part of failLocked's log line that carries the
// whole record: site, cells, class, error and time, in its format.
func failureLogFields(f guestbridge.TransportFailure) string {
	return fmt.Sprintf("transport failed at %s cells=%v class=%s error=%q time=%s;", f.Site, f.Cells, f.Class, f.Error, f.Time)
}

// syncBuffer is a failure log the test reads while a worker may write.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
