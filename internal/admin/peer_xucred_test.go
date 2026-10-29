package admin

import (
	"encoding/binary"
	"testing"
)

func TestFreeBSDXucredSupportedABIAndRejections(t *testing.T) {
	fixture := func() []byte {
		raw := make([]byte, 88)
		binary.LittleEndian.PutUint32(raw[4:8], 1001)
		binary.LittleEndian.PutUint16(raw[8:10], 2)
		binary.LittleEndian.PutUint32(raw[12:16], 1002)
		binary.LittleEndian.PutUint32(raw[80:84], 12345)
		return raw
	}
	for _, release := range []uint32{1404000, 1404001, 1500000, 1501000} {
		peer, err := decodeFreeBSDPeer(fixture(), release, "amd64")
		if err != nil || peer.PID == nil || *peer.PID != 12345 || peer.UID != 1001 || peer.GID != 1002 {
			t.Fatalf("%d: %+v %v", release, peer, err)
		}
	}
	for _, tc := range []struct {
		name    string
		change  func([]byte) []byte
		release uint32
		arch    string
	}{
		{"short", func(raw []byte) []byte { return raw[:84] }, 1501000, "amd64"},
		{"large", func(raw []byte) []byte { return append(raw, 0) }, 1501000, "amd64"},
		{"version", func(raw []byte) []byte { binary.LittleEndian.PutUint32(raw[:4], 1); return raw }, 1501000, "amd64"},
		{"no-groups", func(raw []byte) []byte { binary.LittleEndian.PutUint16(raw[8:10], 0); return raw }, 1501000, "amd64"},
		{"oversized-groups", func(raw []byte) []byte { binary.LittleEndian.PutUint16(raw[8:10], 17); return raw }, 1501000, "amd64"},
		{"absent-pid", func(raw []byte) []byte { binary.LittleEndian.PutUint32(raw[80:84], 0); return raw }, 1501000, "amd64"},
		{"negative-pid", func(raw []byte) []byte { binary.LittleEndian.PutUint32(raw[80:84], 0xffffffff); return raw }, 1501000, "amd64"},
		{"older-release", func(raw []byte) []byte { return raw }, 1403000, "amd64"},
		{"future-release", func(raw []byte) []byte { return raw }, 1502000, "amd64"},
		{"unsupported-arch", func(raw []byte) []byte { return raw }, 1501000, "arm64"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeFreeBSDPeer(tc.change(fixture()), tc.release, tc.arch); err == nil {
				t.Fatal("unproven peer credential accepted")
			}
		})
	}
}
