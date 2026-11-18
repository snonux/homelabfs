package guestbridge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// racingReceiptProducer refuses the mapped receipt of one cell once with a
// transient WouldBlock, the way the view controller's ReceiptEvidence answers
// while another lane holds its lock. onRefuse runs on the worker goroutine at
// that moment, so a test can change what the operation would answer next.
type racingReceiptProducer struct {
	*mappingProducerPrimitive
	refuseCell uint64
	refused    bool
	onRefuse   func()
}

// BindTransport binds this wrapper, the configured producer, not the
// embedded primitive.
func (p *racingReceiptProducer) BindTransport(_ context.Context, tr *Transport) error {
	_, err := tr.VerificationBinding(p)
	return err
}

func (p *racingReceiptProducer) ProduceMapped(ctx context.Context, c Command, base json.RawMessage) (*VerificationReceiptV2, error) {
	if c.Cell == p.refuseCell && !p.refused {
		p.refused = true
		if p.onRefuse != nil {
			p.onRefuse()
		}
		return nil, fsapi.WouldBlock
	}
	return p.mappingProducerPrimitive.ProduceMapped(ctx, c, base)
}

// zq3-r12 R12-1 (nc4), synthesized from the decoded r12 records: the frozen
// transport.db holds capability tokens, so it is not copied here. m1's
// VIEW_WAIT polled EMPTY through native legs 155..167; its ninth leg (native
// 169, poll index 8) protected an EMPTY base, then signing the receipt was
// refused with WouldBlock while m1's VIEW_ACK STOPPED (native 156) held the
// view controller. That ACK made the controller post m1's sequence 2 event,
// so the retried poll executed again, answered EVENT, and finishMappingBase
// fenced the whole transport for a changed base: every later Read and Write
// (responses 156/166, m1's next VIEW_QUERY as native 170) got Unavailable.
//
// Here legs 0..7 poll EMPTY, leg 8 hits the refused receipt while the view
// boundary starts answering EVENT, and a later cell still gets through.
// Before the fix the leg 8 read failed with Unavailable.
func TestMappedWaitRetryReusesProtectedBase(t *testing.T) {
	x := newViewDispatchFixture(t)
	protocol := MappedProtocol()
	x.bridge.s.config.Protocol = &protocol
	const racingLeg = 8
	producer := &racingReceiptProducer{mappingProducerPrimitive: &mappingProducerPrimitive{}, refuseCell: 2*racingLeg + 1}
	x.bridge.s.config.Verification = producer
	empty := []byte(`{"event":null,"status":"EMPTY"}`)
	x.views.reply = empty
	_ = x.control(t, "VIEW_POLL") // sets x.views.reply to a valid EVENT
	event := x.views.reply
	x.views.reply = empty
	producer.onRefuse = func() { x.views.reply = event }
	tr, err := NewDurableTransport(x.bridge.s, durableBudget(t), filepath.Join(t.TempDir(), "wait-mapping"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	var ack *Ack
	var raw []byte
	for index := uint64(0); index <= racingLeg; index++ {
		native := index*2 + 1
		// Read before the write: the write hands the cell to the worker,
		// which updates the counter on its own goroutine.
		callsBefore := x.views.calls
		if err = mappingWrite(t, tr, x.bridge.native, waitLeg(t, x, native, index, ack), 1+2*mappedContinuationFlag(index), 0, -1); err != nil {
			t.Fatal(err)
		}
		raw = fetchNativeAt(t, tr, x.bridge.native, native, x.bridge.native.Endpoint.RecallNode)
		ack = &Ack{Cell: native, ResponseHash: digest(raw)}
		if index == racingLeg && x.views.calls != callsBefore+1 {
			t.Fatal("retry ran the protected poll operation again")
		}
		// An unrelated cell between legs, as the guest's other traffic.
		if err = mappingWrite(t, tr, x.bridge.native, mappedHello(t, x.bridge, native+1, 100+index), 0, 0, -1); err != nil {
			t.Fatal(err)
		}
		_ = fetchNative(t, tr, x.bridge.native, native+1)
	}
	if !producer.refused {
		t.Fatal("oracle did not refuse the racing leg's receipt")
	}
	fields, err := envelopeFields(raw)
	if err != nil || string(fields["payload"]) != string(empty) {
		t.Fatalf("retried leg did not answer its protected EMPTY base: %s", raw)
	}
	tr.mu.Lock()
	fence := tr.failed
	tr.mu.Unlock()
	if fence != nil {
		t.Fatal("transport fenced", fence)
	}
	// The next poll sees the event that arrived meanwhile.
	next := uint64(2*racingLeg + 3)
	if err = mappingWrite(t, tr, x.bridge.native, waitLeg(t, x, next, racingLeg+1, ack), 3, 0, -1); err != nil {
		t.Fatal(err)
	}
	raw = fetchNativeAt(t, tr, x.bridge.native, next, x.bridge.native.Endpoint.RecallNode)
	if fields, err = envelopeFields(raw); err != nil || string(fields["payload"]) != string(event) {
		t.Fatalf("poll after the retried leg missed the event: %s", raw)
	}
}
