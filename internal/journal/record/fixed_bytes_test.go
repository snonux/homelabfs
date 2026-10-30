package record

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestFixedBytesPreserveScalarWireEncoding(t *testing.T) {
	for _, test := range []struct {
		name          string
		value, scalar any
	}{
		{"uuid", UUID{0: 1, 6: 0x40, 8: 0x80, 15: 255}, [16]byte{0: 1, 6: 0x40, 8: 0x80, 15: 255}},
		{"hash", Hash{0: 255, 15: 128, 31: 1}, [32]byte{0: 255, 15: 128, 31: 1}},
		{"zero-hash", Hash{}, [32]byte{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			want, err := marshal(test.scalar)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("bulk=%x scalar=%x", got, want)
			}
			target := reflect.New(reflect.TypeOf(test.value))
			if err := unmarshal(got, target.Interface(), false); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(target.Elem().Interface(), test.value) {
				t.Fatal("fixed-byte round trip changed value")
			}
			for n := 0; n < len(got); n++ {
				target := reflect.New(reflect.TypeOf(test.value))
				err := unmarshal(got[:n], target.Interface(), false)
				var format *Error
				if !errors.As(err, &format) || format.Kind != Corrupt {
					t.Fatalf("truncated at%d: %v", n, err)
				}
			}
		})
	}
}

func TestFixedUUIDStillRejectsZero(t *testing.T) {
	if _, err := marshal(UUID{}); err == nil {
		t.Fatal("encoded zero UUID")
	}
	var id UUID
	if err := unmarshal(make([]byte, 16), &id, false); err == nil {
		t.Fatal("decoded zero UUID")
	}
}

// BenchmarkWriteCandidateCodec measures a complete validated mutation frame,
// including its duplicate metadata and references, rather than only raw arrays.
func BenchmarkWriteCandidateCodec(b *testing.B) {
	var candidate Record
	for _, r := range recordFixtures(b) {
		if _, ok := r.Payload.(Mutation); ok {
			candidate = r
			break
		}
	}
	raw, err := Encode(candidate)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for b.Loop() {
		encoded, err := Encode(candidate)
		if err != nil {
			b.Fatal(err)
		}
		if !bytes.Equal(encoded, raw) {
			b.Fatal("candidate bytes changed")
		}
		if _, err := Decode(encoded); err != nil {
			b.Fatal(err)
		}
	}
}
