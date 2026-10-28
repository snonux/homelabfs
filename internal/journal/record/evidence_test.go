package record

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEmbeddedEvidenceErrorClassification(t *testing.T) {
	var ordinary Record
	for _, r := range recordFixtures(t) {
		if _, ok := r.Payload.(OrdinaryResult); ok {
			ordinary = r
			break
		}
	}
	outer, err := Encode(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	inner := golden(t, "write_candidate")
	offset := bytes.Index(outer, inner)
	if offset < 4 || bytes.Count(outer, inner) != 1 {
		t.Fatal("expected one embedded write candidate")
	}
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		kind   ErrorKind
	}{
		{"short_header", func(b []byte) []byte { return b[:HeaderSize-1] }, Corrupt},
		{"missing_checksum_byte", func(b []byte) []byte { return b[:len(b)-1] }, Corrupt},
		{"claimed_length", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[20:], uint32(len(b)+1))
			binary.BigEndian.PutUint32(b[24:], uint32(len(b)-148+1))
			return b
		}, Corrupt},
		{"unknown_version", func(b []byte) []byte {
			binary.BigEndian.PutUint16(b[8:], 2)
			fixCRC(b)
			return b
		}, Unsupported},
		{"unknown_kind", func(b []byte) []byte {
			binary.BigEndian.PutUint16(b[14:], 10)
			fixCRC(b)
			return b
		}, Unsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := test.mutate(bytes.Clone(inner))
			p := ordinary.Payload.(OrdinaryResult)
			p.Evidence = Evidence{Frames: [][]byte{data}}
			invalid := ordinary
			invalid.Payload = p
			t.Run("encode", func(t *testing.T) {
				_, err := Encode(invalid)
				requireError(t, err, test.kind)
			})

			// Construct a complete outer frame independently of Encode's validation.
			// Evidence is a blob; changing its length also adjusts the outer framing.
			raw := append(bytes.Clone(outer[:offset]), data...)
			raw = append(raw, outer[offset+len(inner):]...)
			binary.BigEndian.PutUint32(raw[offset-4:], uint32(len(data)))
			binary.BigEndian.PutUint32(raw[20:], uint32(len(raw)))
			binary.BigEndian.PutUint32(raw[24:], uint32(len(raw)-148))
			fixCRC(raw)
			t.Run("decode", func(t *testing.T) {
				_, err := Decode(raw)
				requireError(t, err, test.kind)
			})
		})
	}
	// A valid bundle remains byte stable, and physical outer truncation remains
	// distinguishable from invalid embedded framing inside a complete frame.
	decoded, err := Decode(outer)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := Encode(decoded)
	if err != nil || !bytes.Equal(outer, reencoded) {
		t.Fatalf("valid evidence round trip: %v", err)
	}
	for cut := 0; cut < len(outer); cut++ {
		_, err := Decode(outer[:cut])
		requireError(t, err, Torn)
	}
}
