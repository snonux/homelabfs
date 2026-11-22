//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"errors"
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
// runtime's stderr, where failLocked reports the fence.
type saveFault struct {
	site  string
	err   error
	log   *syncBuffer
	fired atomic.Bool
}

func newSaveFault(site string) *saveFault {
	return &saveFault{site: site, err: errors.New("injected transport.db save failure"), log: &syncBuffer{}}
}

// hook is the ledger failpoint. It runs with the transport's lock held, on
// whichever goroutine saves (the writer for commitWrite, a lane worker for
// publishResponse), so it selects its target by the caller on the stack.
func (f *saveFault) hook(name string) error {
	if name == "before-commit" && onStack("guestbridge.(*Transport)."+f.site) && f.fired.CompareAndSwap(false, true) {
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
// failLocked holds. A renamed or retyped field fails here, not silently.
func armTransportFault(t *testing.T, tr *guestbridge.Transport, fault *saveFault) {
	t.Helper()
	v := reflect.ValueOf(tr).Elem()
	mu, ledger, failureLog := v.FieldByName("mu"), v.FieldByName("ledger"), v.FieldByName("failureLog")
	if mu.Type() != reflect.TypeFor[sync.Mutex]() || ledger.Kind() != reflect.Pointer || ledger.IsNil() || failureLog.Type() != reflect.TypeFor[io.Writer]() {
		t.Fatal("guestbridge.Transport no longer has mu, a durable ledger and failureLog")
	}
	failpoint := ledger.Elem().FieldByName("failpoint")
	hook := fault.hook
	if failpoint.Type() != reflect.TypeOf(hook) {
		t.Fatal("transport ledger failpoint is no longer func(string) error")
	}
	lock := (*sync.Mutex)(unsafe.Pointer(mu.UnsafeAddr()))
	lock.Lock()
	defer lock.Unlock()
	reflect.NewAt(failpoint.Type(), unsafe.Pointer(failpoint.UnsafeAddr())).Elem().Set(reflect.ValueOf(hook))
	var log io.Writer = fault.log
	reflect.NewAt(failureLog.Type(), unsafe.Pointer(failureLog.UnsafeAddr())).Elem().Set(reflect.ValueOf(&log).Elem())
}

// injectSaveFailure arms fault at its site with m1's next VIEW_WAIT leg
// outstanding and returns that leg and the native cells the failed save
// protected:
//   - commitWrite: the leg is written first, then the mount thread's next
//     VIEW_QUERY, whose save (the new cell and the previous VIEW_QUERY it
//     reclaims, zq3-r12's write 170 releasing 168) fails; the writer gets
//     the error, so the exporter answers that SEND with an ERROR at once;
//   - publishResponse: the leg's response publication (zq3-r12's leg 169)
//     fails on its lane worker; the leg stays unanswered.
func (r *r12Run) injectSaveFailure(fault *saveFault) (*writtenLeg, []uint64) {
	if fault.site == "publishResponse" {
		armTransportFault(r.t, r.m.a.Transport, fault)
		leg, err := r.wait.write()
		require(r.t, err)
		r.awaitFence()
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

// awaitFence waits until a lane worker fenced the transport.
func (r *r12Run) awaitFence() {
	deadline := time.Now().Add(answerBound)
	for {
		if _, failed := r.m.a.Transport.Failure(); failed {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatal("the injected save failure never fenced the transport")
		}
		time.Sleep(time.Millisecond)
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
	if len(lines) != 1 || !strings.Contains(lines[0], failure.Site) || !strings.Contains(lines[0], fault.err.Error()) || !strings.Contains(lines[0], "recorded in transport.db") {
		t.Errorf("failure log (runtime stderr) %q", fault.log.String())
	}
	evidence := captureTransport(r.m.a.Transport)
	raw, err := json.Marshal(evidence)
	require(t, err)
	if evidence.Failure == nil || evidence.Failure.Site != failure.Site || !bytes.Contains(raw, []byte(`"failure":{`)) {
		t.Errorf("OBSERVE transport section %s", raw)
	}
	r.m.a.Transport.Close()
	root := filepath.Join(r.m.directory, "transport")
	state, err := guestbridge.InspectTransport(root, nil)
	require(t, err)
	if state.Failure == nil || state.Failure.Site != failure.Site || state.Failure.Error != failure.Error || !slices.Equal(state.Failure.Cells, cells) {
		t.Errorf("transport.db failure row %+v, want %+v", state.Failure, failure)
	}
	if _, err = guestbridge.InspectRestorationEndpoint(root, nil, nil); !errors.Is(err, fsapi.Unavailable) {
		t.Errorf("fenced transport.db offered for restoration: %v", err)
	}
	t.Logf("fence recorded: %s; OBSERVE %s", lines[0], raw)
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
