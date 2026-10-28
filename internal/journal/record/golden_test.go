package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type vectorInfo struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func TestGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]vectorInfo
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 9 {
		t.Fatal("expected all nine normative vectors")
	}
	for name, info := range manifest {
		t.Run(name, func(t *testing.T) {
			expected := golden(t, name)
			if len(expected) != info.Bytes {
				t.Fatal("literal fixture length")
			}
			hashed := expected
			if strings.HasPrefix(name, "internal_") {
				hashed = append([]byte("HOMELABFS-INTERNAL\x00"), expected...)
			}
			sum := sha256.Sum256(hashed)
			if hex.EncodeToString(sum[:]) != info.SHA256 {
				t.Fatal("literal fixture hash")
			}
			actual := encodeFixture(t, name)
			if !bytes.Equal(expected, actual) {
				t.Fatalf("encoding mismatch\nwant %x\ngot  %x", expected, actual)
			}
			var back []byte
			switch {
			case strings.HasPrefix(name, "internal_"):
				v, err := DecodeInternal(expected)
				if err != nil {
					t.Fatal(err)
				}
				back, _, err = EncodeInternal(v)
				if err != nil {
					t.Fatal(err)
				}
			case name == "original_link":
				v, err := DecodeOriginal(expected)
				if err != nil {
					t.Fatal(err)
				}
				back, err = EncodeOriginal(v)
				if err != nil {
					t.Fatal(err)
				}
				api, err := fsapi.NewOriginalRequest(fsapi.TransactionSpec{Version: 1, ID: v.ID, Operation: fsapi.Link, Source: fsapi.Endpoint{Volume: v.Source.Volume, View: v.Source.View, Components: v.Source.Components}, Destination: fsapi.Endpoint{Volume: v.Destination.Volume, View: v.Destination.View, Components: v.Destination.Components}, Replacement: fsapi.NoReplace})
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(api.CanonicalBytes(), expected) {
					t.Fatal("fsapi canonical disagreement")
				}
			case name == "write_args":
				var v Write
				if err := unmarshal(expected, &v, false); err != nil {
					t.Fatal(err)
				}
				back, err = EncodeArguments(8, v)
				if err != nil {
					t.Fatal(err)
				}
			default:
				v, err := Decode(expected)
				if err != nil {
					t.Fatal(err)
				}
				back, err = Encode(v)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(expected, back) {
				t.Fatal("decode/re-encode differs from literal")
			}
		})
	}
}
func TestImmutableIntentSelection(t *testing.T) {
	for _, pair := range [][2]string{{"internal_create_0600", "internal_create_0644"}, {"internal_setattr_uid1000", "internal_setattr_uid1001"}} {
		a, err := DecodeInternal(golden(t, pair[0]))
		if err != nil {
			t.Fatal(err)
		}
		b, err := DecodeInternal(golden(t, pair[1]))
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != b.ID {
			t.Fatal("fixture must exercise same request ID")
		}
		_, ha, err := EncodeInternal(a)
		if err != nil {
			t.Fatal(err)
		}
		_, hb, err := EncodeInternal(b)
		if err != nil {
			t.Fatal(err)
		}
		if ha == hb {
			t.Fatal("distinct original inputs share digest")
		}
		mutated := Record{Header: fixtureHeader(2), Payload: Mutation{Intent: a.Intent, Redo: Redo{Objects: b.Intent.SelectedMetadata}}}
		if _, err := Encode(mutated); err == nil {
			t.Fatal("accepted altered selected metadata")
		}
	}
	// Requested modes 0644 and 0666 remain distinct even with identical 0644
	// afterimages because the request records original mode and umask.
	a, _ := DecodeInternal(golden(t, "internal_create_0644"))
	b := a
	arg := b.Intent.Arguments.(Create)
	arg.RequestedMode = 0666
	b.Intent.Arguments = arg
	ba, ha, err := EncodeInternal(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, hb, err := EncodeInternal(b)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ba, bb) || ha == hb {
		t.Fatal("omitted original requested mode")
	}
}
func golden(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".hex")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func fixtureUUID(b byte) UUID {
	var id UUID
	for i := range id {
		id[i] = b
	}
	return id
}
func fixtureObject() ObjectID {
	return ObjectID{Home: fixtureUUID(0x11), Allocation: UUID{0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x47, 0x77, 0x87, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77, 0x77}}
}
func fixtureParent() ObjectID {
	return ObjectID{Home: fixtureUUID(0x11), Allocation: UUID{0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0x4a, 0xaa, 0x8a, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa}}
}
func fixtureHeader(seq uint64) Header {
	return Header{Volume: fixtureUUID(0x11), Generation: fixtureUUID(0x22), Configuration: fixtureUUID(0x33), Epoch: 7, Sequence: seq, Operation: fixtureUUID(0x44), Incarnation: fixtureUUID(0x55), Observed: Time{Seconds: -1, Nanoseconds: 123456789}}
}
func fixturePosition() Position {
	h := fixtureHeader(1)
	return Position{Volume: h.Volume, Generation: h.Generation, Configuration: h.Configuration, Epoch: h.Epoch, Sequence: 1}
}
func fixtureMetadata(revision uint64, kind uint8, uid uint32, mode uint16, size, nlink uint64) Metadata {
	tm := fixtureHeader(1).Observed
	return Metadata{Revision: revision, Type: kind, UID: uid, GID: 1000, Mode: mode, Size: size, Nlink: nlink, Atime: tm, Mtime: tm, Ctime: tm}
}
func fixtureRequestID() RequestID {
	return RequestID{Session: fsapi.SessionID{Principal: Principal{Realm: fixtureUUID(0x11), Subject: "u"}, Anchor: fixtureUUID(0x22), Nonce: fixtureUUID(0x33)}, Number: 1}
}
func fixtureInternal(mode uint16, uid uint32, setattr bool) InternalRequest {
	v := InternalRequest{Version: 1, ID: fixtureRequestID(), NamespaceVolume: fixtureUUID(0x44), View: fixtureUUID(0x55)}
	if setattr {
		v.Intent = Intent{Kind: 10, Arguments: Setattr{Object: fixtureObject(), Mask: 2, UID: &uid}, SelectedMetadata: []ObjectEdit{{Object: fixtureObject(), ExpectedRevision: 3, After: fixtureMetadata(4, 1, uid, 0644, 5, 1)}}}
	} else {
		v.Intent = Intent{Kind: 1, Arguments: Create{Parent: fixtureParent(), Name: "new", Object: fixtureObject(), Exclusive: true, RequestedMode: mode, Umask: 0022}, SelectedMetadata: []ObjectEdit{{Object: fixtureObject(), After: fixtureMetadata(1, 1, 1000, mode&^0022, 0, 1)}, {Object: fixtureParent(), ExpectedRevision: 2, After: fixtureMetadata(3, 2, 1000, 0755, 0, 2)}}}
	}
	return v
}
func fixtureWrite() Mutation {
	chosen := []ObjectEdit{{Object: fixtureObject(), ExpectedRevision: 3, After: fixtureMetadata(4, 1, 1000, 0644, 5, 1)}}
	return Mutation{Intent: Intent{Kind: 8, Arguments: Write{Object: fixtureObject(), Offset: 2, Data: []byte{0, 255, 65}}, SelectedMetadata: chosen}, Redo: Redo{Objects: chosen}, Authorizations: []Authorization{{Object: fixtureObject(), ObjectRevision: 3, View: fixtureUUID(0x88), PolicyRevision: 1, Credentials: Credentials{UID: 1000, GID: 1000, Groups: []uint32{1000}}}}, Dependencies: Vector{fixturePosition()}}
}
func encodeFixture(t testing.TB, name string) []byte {
	t.Helper()
	var data []byte
	var err error
	switch name {
	case "open":
		data, err = Encode(Record{Header: fixtureHeader(1), Payload: SegmentOpen{Segment: fixtureUUID(0x66), StreamKind: 1, First: 1, MaximumBytes: MaxSegment}})
	case "seal":
		opened := encodeFixture(t, "open")
		data, err = Encode(Record{Header: fixtureHeader(2), Payload: SegmentSeal{Segment: fixtureUUID(0x66), First: 1, LastBeforeSeal: 1, FrameCount: 1, ByteCount: uint64(len(opened)), ContentSHA256: sha256.Sum256(opened)}})
	case "write_args":
		data, err = EncodeArguments(8, fixtureWrite().Intent.Arguments)
	case "write_candidate":
		data, err = Encode(Record{Header: fixtureHeader(2), Payload: fixtureWrite()})
	case "original_link":
		data, err = EncodeOriginal(OriginalRequest{Version: 1, ID: fixtureRequestID(), Operation: 2, Source: Endpoint{Volume: fixtureUUID(0x44), View: fixtureUUID(0x55), Components: []string{"a\xff"}}, Destination: Endpoint{Volume: fixtureUUID(0x66), View: fixtureUUID(0x77), Components: []string{"b"}}, Replacement: 2})
	case "internal_create_0600":
		data, _, err = EncodeInternal(fixtureInternal(0600, 0, false))
	case "internal_create_0644":
		data, _, err = EncodeInternal(fixtureInternal(0644, 0, false))
	case "internal_setattr_uid1000":
		data, _, err = EncodeInternal(fixtureInternal(0, 1000, true))
	case "internal_setattr_uid1001":
		data, _, err = EncodeInternal(fixtureInternal(0, 1001, true))
	default:
		t.Fatal(name)
	}
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestPayloadKindCoverage(t *testing.T) {
	count := 0
	for k := uint16(1); k <= 68; k++ {
		p := payload(k)
		if p == nil {
			continue
		}
		count++
		if kindOf(p) != k {
			t.Fatalf("kind %d not bijective", k)
		}
		if reflect.TypeOf(p).Kind() != reflect.Struct {
			t.Fatal("opaque payload")
		}
	}
	if count != 36 {
		t.Fatalf("record kinds: %d", count)
	}
}
