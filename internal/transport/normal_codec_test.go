package transport

import (
	"bytes"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
)

func TestNormalRepresentationKeepsCreatorAndForwarderDistinct(t *testing.T) {
	s := record.OriginStream{Cluster: uid(1), Volume: uid(2), Generation: uid(3), Configuration: uid(4), Epoch: 1, Origin: uid(5), Journal: uid(6)}
	b, err := record.EncodeNormal(record.NormalRecord{Stream: s, Sequence: 1, Operation: uid(7), Creator: record.Launch{Node: uid(5), Incarnation: uid(8)}, Payload: record.OriginOpen{Segment: uid(9), MaximumBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	f := Frame{Kind: Data, Class: Bulk, Session: uid(10), Connection: uid(11), Source: uid(12), SourceIncarnation: uid(13), Destination: uid(14), DestinationIncarnation: uid(15), Correlation: uid(16), Binding: uid(17), Body: b}
	c := NormalCapability{3, 3, 3, 3, 1}
	descriptor, err := EncodeNormalCapability(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeNormalCapability(descriptor); err != nil {
		t.Fatal(err)
	}
	wire, err := EncodeNormalFrame(f, 4096, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(wire, 4096); err == nil {
		t.Fatal("live v1 decoder accepted v3")
	}
	got, err := DecodeNormalFrame(wire, 4096, c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Body, b) || got.Source == s.Origin {
		t.Fatal("historical creator/forwarder rewritten")
	}
	wire[len(wire)-1] ^= 1
	if !bytes.Equal(got.Body, b) {
		t.Fatal("decoder retained caller storage")
	}
	for _, bad := range []NormalCapability{{1, 3, 3, 3, 1}, {3, 2, 3, 3, 1}, {3, 3, 3, 3, 0}, {3, 3, 3, 3, 3}} {
		if _, err := EncodeNormalFrame(f, 4096, bad); err == nil {
			t.Fatal("unsupported component/feature accepted", bad)
		}
	}
	descriptor[8] = 0xff
	if _, err := DecodeNormalCapability(descriptor); err == nil {
		t.Fatal("unknown major accepted")
	}
	if _, err := DecodeNormalFrame(wire, HeaderSize, c); err == nil {
		t.Fatal("message limit ignored")
	}
	f.Kind, f.Class = Accepted, Control
	if _, err := EncodeNormalFrame(f, 4096, c); err == nil {
		t.Fatal("grant/open body masqueraded as acceptance")
	}
}
