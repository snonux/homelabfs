package record

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

const minimumObjectEdit = 32 + 8 + 75

func FuzzDecode(f *testing.F) {
	for _, name := range []string{"open", "seal", "write_candidate"} {
		f.Add(golden(f, name))
	}
	for _, r := range recordFixtures(f) {
		b, err := Encode(r)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Add([]byte("HFSJREC\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		verifyDecoded(t, data)
		// Valid-CRC mutations exercise schemas rather than stopping at checksum.
		// Recompute no sizes or identities: malformed framing must still fail first.
		if len(data) >= 148 && len(data) <= MaxFrame {
			canonicalCRC := bytes.Clone(data)
			fixCRC(canonicalCRC)
			verifyDecoded(t, canonicalCRC)
		}
	})
}
func FuzzInternalRequest(f *testing.F) {
	for _, name := range []string{"internal_create_0600", "internal_create_0644", "internal_setattr_uid1000", "internal_setattr_uid1001"} {
		f.Add(golden(f, name))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := DecodeInternal(data)
		if err != nil {
			return
		}
		encoded, _, err := EncodeInternal(v)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatal("noncanonical internal roundtrip")
		}
	})
}
func FuzzOriginalRequest(f *testing.F) {
	f.Add(golden(f, "original_link"))
	f.Fuzz(func(t *testing.T, data []byte) {
		v, err := DecodeOriginal(data)
		if err != nil {
			return
		}
		encoded, err := EncodeOriginal(v)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatal("noncanonical original roundtrip")
		}
	})
}
func FuzzAllocationBounds(f *testing.F) {
	for _, count := range []uint32{0, 1, 64, 256, 4096, 0xffffffff} {
		f.Add(count, []byte{})
	}
	f.Fuzz(func(t *testing.T, count uint32, remaining []byte) {
		if len(remaining) > MaxFrame-4 {
			return
		}
		data := binary.BigEndian.AppendUint32(nil, count)
		data = append(data, remaining...)
		var value []ObjectEdit
		w := wire{data: data, budget: MaxFrame * 8, decoding: true}
		w.value(reflect.ValueOf(&value).Elem(), "max=256,sort")
		if w.budget < 0 || len(value) > 256 {
			t.Fatal("allocation limit violated")
		}
		if uint64(count) > uint64(len(remaining)/minimumObjectEdit) && value != nil {
			t.Fatal("allocated before minimum byte check")
		}
	})
}

func verifyDecoded(t testing.TB, data []byte) {
	t.Helper()
	v, err := Decode(data)
	if err != nil {
		return
	}
	encoded, err := Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatal("noncanonical frame roundtrip")
	}
}
