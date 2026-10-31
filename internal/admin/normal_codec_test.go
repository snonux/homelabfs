package admin

import (
	"bytes"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
)

func TestNormalStatusCannotClaimPublicationOrLoseOriginIdentity(t *testing.T) {
	id := func(n byte) record.UUID { return record.UUID{15: n} }
	s := record.OriginStream{Cluster: id(1), Volume: id(2), Generation: id(3), Configuration: id(4), Epoch: 1, Origin: id(5), Journal: id(6)}
	b, err := record.EncodeNormal(record.NormalRecord{Stream: s, Sequence: 1, Operation: id(7), Creator: record.Launch{Node: id(5), Incarnation: id(8)}, Payload: record.NormalFrontier{Stream: s, Received: 4, Durable: 3}})
	if err != nil {
		t.Fatal(err)
	}
	v := NormalObservation{Version: 3, Frontier: b}
	encoded, err := EncodeNormalObservation(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeNormalObservation(encoded)
	if err != nil || !bytes.Equal(got.Frontier, b) {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{bytes.Replace(encoded, []byte("\"version\":3"), []byte("\"version\":2"), 1), bytes.Replace(encoded, []byte("false"), []byte("true"), 1), append(encoded, 0), append([]byte("{\"version\":3,"), encoded[1:]...), bytes.Replace(encoded, []byte("\"version\":3"), []byte("\"Version\":3"), 1)} {
		if _, err := DecodeNormalObservation(bad); err == nil {
			t.Fatal("malformed/unsupported observation accepted")
		}
	}
}
