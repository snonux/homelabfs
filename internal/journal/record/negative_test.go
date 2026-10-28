package record

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"reflect"
	"testing"
)

func fixCRC(b []byte) {
	binary.BigEndian.PutUint32(b[len(b)-4:], crc32.Checksum(b[:len(b)-4], crc32.MakeTable(crc32.Castagnoli)))
}
func requireError(t testing.TB, err error, kind ErrorKind) {
	t.Helper()
	var format *Error
	if err == nil || !errors.As(err, &format) || format.Kind != kind {
		t.Fatalf("want typed error %d; got %v", kind, err)
	}
}
func TestTornClassification(t *testing.T) {
	for _, name := range []string{"open", "write_candidate"} {
		raw := golden(t, name)
		for cut := 0; cut < len(raw); cut++ {
			_, err := Decode(raw[:cut])
			requireError(t, err, Torn)
		}
	}
	// Impossible complete headers and unknown formats are never torn even when
	// their claimed frame is incomplete. Segment context may elevate Torn to an
	// interior gap; the codec deliberately does not make recovery decisions.
	raw := golden(t, "open")
	binary.BigEndian.PutUint32(raw[20:], 147)
	_, err := Decode(raw[:144])
	requireError(t, err, Corrupt)
	raw = golden(t, "open")
	binary.BigEndian.PutUint16(raw[8:], 2)
	_, err = Decode(raw[:144])
	requireError(t, err, Unsupported)
	raw = golden(t, "open")
	clear(raw[32:48])
	_, err = Decode(raw[:144])
	requireError(t, err, Corrupt)
}
func TestEveryCoveredBitRejected(t *testing.T) {
	for _, name := range []string{"open", "seal", "write_candidate"} {
		raw := golden(t, name)
		for offset := range raw {
			for bit := uint(0); bit < 8; bit++ {
				mutated := bytes.Clone(raw)
				mutated[offset] ^= 1 << bit
				if _, err := Decode(mutated); err == nil {
					t.Fatalf("accepted %s offset %d bit %d", name, offset, bit)
				}
			}
		}
	}
}
func TestValidCRCSemanticCorruption(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]byte)
		kind   ErrorKind
	}{
		{"major", func(b []byte) { binary.BigEndian.PutUint16(b[8:], 2) }, Unsupported},
		{"minor", func(b []byte) { binary.BigEndian.PutUint16(b[10:], 1) }, Unsupported},
		{"header_length", func(b []byte) { binary.BigEndian.PutUint16(b[12:], 145) }, Unsupported},
		{"record_kind", func(b []byte) { binary.BigEndian.PutUint16(b[14:], 10) }, Unsupported},
		{"flags", func(b []byte) { b[19] = 1 }, Corrupt},
		{"prefix_reserved", func(b []byte) { b[31] = 1 }, Corrupt},
		{"header_reserved", func(b []byte) { b[143] = 1 }, Corrupt},
		{"zero_volume", func(b []byte) { clear(b[32:48]) }, Corrupt},
		{"zero_sequence", func(b []byte) { clear(b[88:96]) }, Corrupt},
		{"nanos", func(b []byte) { binary.BigEndian.PutUint32(b[136:], 1_000_000_000) }, Corrupt},
		{"selected_count", func(b []byte) { binary.BigEndian.PutUint32(b[193:], 257) }, Corrupt},
		{"selected_count_remaining", func(b []byte) { binary.BigEndian.PutUint32(b[193:], 256) }, Corrupt},
		{"write_data_limit", func(b []byte) { binary.BigEndian.PutUint32(b[186:], 1_048_577) }, Corrupt},
		{"empty_write", func(b []byte) { clear(b[186:190]) }, Corrupt},
		{"offset_overflow", func(b []byte) { binary.BigEndian.PutUint64(b[178:], math.MaxInt64) }, Corrupt},
		{"size_limit", func(b []byte) { binary.BigEndian.PutUint64(b[256:], math.MaxUint64) }, Corrupt},
		{"metadata_mode", func(b []byte) { binary.BigEndian.PutUint16(b[254:], 0x1000) }, Corrupt},
		{"metadata_type", func(b []byte) { b[245] = 4 }, Corrupt},
		{"selected_revision", func(b []byte) { binary.BigEndian.PutUint64(b[237:], 5) }, Corrupt},
		{"altered_selection", func(b []byte) { binary.BigEndian.PutUint32(b[246:], 1001) }, Corrupt},
		{"altered_local_redo", func(b []byte) { binary.BigEndian.PutUint32(b[369:], 1001) }, Corrupt},
		{"foreign_local_redo", func(b []byte) { b[320] = 0x22 }, Corrupt},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := golden(t, "write_candidate")
			test.mutate(raw)
			fixCRC(raw)
			_, err := Decode(raw)
			requireError(t, err, test.kind)
		})
	}
}
func TestFramingLengthBounds(t *testing.T) {
	for _, total := range []uint32{0, 147, MaxFrame + 1, math.MaxUint32} {
		raw := golden(t, "open")
		binary.BigEndian.PutUint32(raw[20:], total)
		fixCRC(raw)
		_, err := Decode(raw)
		requireError(t, err, Corrupt)
	}
	raw := golden(t, "open")
	binary.BigEndian.PutUint32(raw[24:], math.MaxUint32)
	fixCRC(raw)
	_, err := Decode(raw)
	requireError(t, err, Corrupt)
	raw = golden(t, "open")
	raw = append(raw, 0)
	_, err = Decode(raw)
	requireError(t, err, Corrupt)
	raw = golden(t, "open")
	raw = append(raw[:len(raw)-4], append([]byte{0}, raw[len(raw)-4:]...)...)
	binary.BigEndian.PutUint32(raw[20:], uint32(len(raw)))
	binary.BigEndian.PutUint32(raw[24:], uint32(len(raw)-148))
	fixCRC(raw)
	_, err = Decode(raw)
	requireError(t, err, Corrupt)
}
func TestSharedSchemaNegatives(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"unknown_stream", FrameRef{StreamKind: 3, Position: fixturePosition()}},
		{"owner_with_reporter", FrameRef{StreamKind: 1, Reporter: ptr(fixtureUUID(1)), Position: fixturePosition()}},
		{"reporter_missing_incarnation", FrameRef{StreamKind: 2, Reporter: ptr(fixtureUUID(1)), Position: fixturePosition()}},
		{"zero_reference", FrameRef{StreamKind: 1, Position: Position{Volume: fixtureUUID(1), Generation: fixtureUUID(2), Configuration: fixtureUUID(3)}}},
		{"duplicate_groups", Credentials{Groups: []uint32{1, 1}}},
		{"unsorted_groups", Credentials{Groups: []uint32{2, 1}}},
		{"too_many_groups", Credentials{Groups: make([]uint32, 1025)}},
		{"name_dot", Reference{Parent: fixtureObject(), Name: ".", EntryRevision: 1}},
		{"name_nul", Reference{Parent: fixtureObject(), Name: "a\x00b", EntryRevision: 1}},
		{"name_slash", Reference{Parent: fixtureObject(), Name: "a/b", EntryRevision: 1}},
		{"bad_endpoint", Endpoint{Volume: fixtureUUID(1), View: fixtureUUID(2), Components: []string{".."}}},
		{"principal_subject", Principal{Realm: fixtureUUID(1)}},
		{"directory_size", fixtureMetadata(1, 2, 0, 0755, 1, 2)},
		{"regular_symlink_text", func() Metadata { v := fixtureMetadata(1, 1, 0, 0644, 1, 1); v.SymlinkText = []byte("a"); return v }()},
		{"metadata_time", func() Metadata {
			v := fixtureMetadata(1, 1, 0, 0644, 1, 1)
			v.Ctime.Nanoseconds = 1_000_000_000
			return v
		}()},
		{"symlink_length", func() Metadata { v := fixtureMetadata(1, 3, 0, 0777, 2, 1); v.SymlinkText = []byte("a"); return v }()},
		{"symlink_nul", Symlink{Parent: fixtureParent(), Name: "a", Object: fixtureObject(), Text: []byte{0}}},
		{"revision_wrap", ObjectEdit{Object: fixtureObject(), ExpectedRevision: math.MaxUint64, After: fixtureMetadata(1, 1, 0, 0644, 1, 1)}},
		{"entry_revision", EntryEdit{Parent: fixtureParent(), Name: "a", After: EntryState{Revision: 2, Target: ptr(fixtureObject())}}},
		{"ledger_count", LedgerEdit{Object: fixtureObject(), ResultingRevision: 1, ResultingNlink: 1}},
		{"setattr_empty", Setattr{Object: fixtureObject()}},
		{"setattr_unknown", Setattr{Object: fixtureObject(), Mask: 32}},
		{"setattr_missing", Setattr{Object: fixtureObject(), Mask: 1}},
		{"setattr_extra", Setattr{Object: fixtureObject(), Mask: 2, Mode: ptr(uint16(0600)), UID: ptr(uint32(1))}},
		{"time_source", TimeSelection{Source: 3, Value: fixtureHeader(1).Observed}},
		{"blob_limit", Write{Object: fixtureObject(), Data: make([]byte, 1_048_577)}},
		{"artifact_kind", Artifact{Kind: 8, ID: fixtureUUID(1)}},
		{"artifact_length", Artifact{Kind: 1, ID: fixtureUUID(1), Length: 1_073_741_825}},
		{"alias_missing_parent", Alias{View: fixtureUUID(1), Name: "a", Invalidate: 1}},
		{"alias_missing_object", Alias{View: fixtureUUID(1), Invalidate: 4}},
		{"terminal_missing_decision", TerminalResult{Status: 1, Source: ptr(fixtureObject())}},
		{"retire_backwards", SessionRetire{Principal: fixtureRequestID().Session.Principal, Anchor: fixtureUUID(1), Nonce: fixtureUUID(2), PreviousFloor: 2, NewFloor: 1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := marshal(test.value); err == nil {
				t.Fatal("accepted invalid schema")
			}
		})
	}
	// Untrusted counts must be rejected using remaining bytes before MakeSlice.
	for _, n := range []uint32{256, 4096, math.MaxUint32} {
		raw := binary.BigEndian.AppendUint32(nil, n)
		var v []ObjectEdit
		if err := unmarshal(raw, &v, false); err == nil {
			t.Fatal("count with no enclosing bytes")
		}
		if v != nil {
			t.Fatal("allocated before checking remaining bytes")
		}
	}
	var presence *UUID
	if err := unmarshal([]byte{2}, &presence, false); err == nil {
		t.Fatal("presence 2")
	}
	var boolean bool
	if err := unmarshal([]byte{2}, &boolean, false); err == nil {
		t.Fatal("boolean 2")
	}
}
func TestEvidenceAndReferences(t *testing.T) {
	fixtures := recordFixtures(t)
	var ordinary Record
	var promise Record
	for _, r := range fixtures {
		if _, ok := r.Payload.(OrdinaryResult); ok {
			ordinary = r
		}
		if _, ok := r.Payload.(Promise); ok {
			promise = r
		}
	}
	nested, err := Encode(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	var evidence Evidence
	// Even an empty Evidence field in an embedded frame is forbidden.
	p := ordinary.Payload.(OrdinaryResult)
	p.Evidence = Evidence{}
	ordinary.Payload = p
	emptyNested, err := Encode(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	for _, frames := range [][][]byte{{nested}, {emptyNested}, {golden(t, "open")}, {golden(t, "seal")}, {golden(t, "write_candidate"), golden(t, "write_candidate")}} {
		evidence.Frames = frames
		if _, err := marshal(evidence); err == nil {
			t.Fatal("accepted nested/structural/duplicate evidence")
		}
	}
	raw, err := Encode(promise)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Frames = [][]byte{raw}
	if _, err := marshal(evidence); err != nil {
		t.Fatal(err)
	}
	ref, err := ReferenceTo(promise, raw)
	if err != nil {
		t.Fatal(err)
	}
	if ref.StreamKind != 2 || *ref.ReporterIncarnation != promise.Header.Incarnation {
		t.Fatal("reporter reference")
	}
	// Nil and empty arrays have one canonical representation; direct callers need
	// not first decode their own naturally constructed record to derive a ref.
	natural := Record{Header: fixtureHeader(2), Payload: fixtureWrite()}
	raw, err = Encode(natural)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReferenceTo(natural, raw); err != nil {
		t.Fatal(err)
	}
	seal, err := Decode(golden(t, "seal"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReferenceTo(seal, golden(t, "seal")); err == nil {
		t.Fatal("context-free seal assumed owner")
	}
	open := SegmentOpen{Segment: fixtureUUID(0x66), StreamKind: 2, Reporter: ptr(fixtureUUID(0xaa)), ReporterIncarnation: ptr(seal.Header.Incarnation), First: 1, MaximumBytes: MaxSegment}
	ref, err = SealReference(seal, golden(t, "seal"), open)
	if err != nil {
		t.Fatal(err)
	}
	if ref.StreamKind != 2 || !reflect.DeepEqual(ref.Reporter, open.Reporter) || !reflect.DeepEqual(ref.ReporterIncarnation, open.ReporterIncarnation) {
		t.Fatal("reporter seal aliased owner stream")
	}
}
func TestArraySetKeys(t *testing.T) {
	pos := fixturePosition()
	other := pos
	other.Sequence++
	vectors := []Vector{{pos, pos}, {pos, other}}
	for _, v := range vectors {
		if _, err := marshal(v); err == nil {
			t.Fatal("duplicate volume in vector")
		}
	}
	refs := []FrameRef{fixtureRef(), fixtureRef()}
	refs[1].SHA256[0] ^= 1
	if _, err := marshal(struct {
		Refs []FrameRef `wire:"sort"`
	}{refs}); err == nil {
		t.Fatal("unequal hash at one stream key")
	}
	objects := []ObjectEdit{{Object: fixtureObject(), ExpectedRevision: 3, After: fixtureMetadata(4, 1, 1000, 0644, 5, 1)}, {Object: fixtureObject(), ExpectedRevision: 3, After: fixtureMetadata(4, 1, 1001, 0644, 5, 1)}}
	if _, err := marshal(Redo{Objects: objects}); err == nil {
		t.Fatal("duplicate object key")
	}
}
func ptr[T any](v T) *T { return &v }

func TestSegmentCapacityIncludesActualOpen(t *testing.T) {
	ref := fixtureRef()
	ref.Position.Sequence = 2
	reporter := fixtureUUID(0xaa)
	incarnation := fixtureHeader(1).Incarnation
	evidenceRef := ref
	evidenceRef.StreamKind = 2
	evidenceRef.Reporter = &reporter
	evidenceRef.ReporterIncarnation = &incarnation
	for _, open := range []SegmentOpen{
		{Segment: fixtureUUID(0x66), StreamKind: 1, First: 1, MaximumBytes: MaxSegment},
		{Segment: fixtureUUID(0x66), StreamKind: 2, Reporter: &reporter, ReporterIncarnation: &incarnation, First: 1, MaximumBytes: MaxSegment},
		{Segment: fixtureUUID(0x66), StreamKind: 1, First: 3, PreviousSeal: &ref, MaximumBytes: MaxSegment},
		{Segment: fixtureUUID(0x66), StreamKind: 2, Reporter: &reporter, ReporterIncarnation: &incarnation, First: 3, PreviousSeal: &evidenceRef, MaximumBytes: MaxSegment},
	} {
		payload, err := marshal(open)
		if err != nil {
			t.Fatal(err)
		}
		open.MaximumBytes = uint64(148 + len(payload) + 228)
		r := Record{Header: fixtureHeader(open.First), Payload: open}
		encoded, err := Encode(r)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(len(encoded)+228) != open.MaximumBytes {
			t.Fatal("minimum formula")
		}
		open.MaximumBytes--
		r.Payload = open
		if _, err := Encode(r); err == nil {
			t.Fatal("accepted capacity one byte below actual OPEN + SEAL")
		}
		binary.BigEndian.PutUint64(encoded[len(encoded)-12:], open.MaximumBytes)
		fixCRC(encoded)
		if _, err := Decode(encoded); err == nil {
			t.Fatal("decoder accepted insufficient capacity with valid CRC")
		}
	}
}

func TestPositionListsRetainDistinctHistories(t *testing.T) {
	a := fixturePosition()
	b := a
	b.Configuration = fixtureUUID(0x44)
	// Only Vector has the one-entry-per-volume rule. A generic Position set can
	// retain distinct history cuts for the same group, as LOSS_RECOVERY requires.
	value := struct {
		Positions []Position `wire:"sort"`
	}{[]Position{a, b}}
	if _, err := marshal(value); err != nil {
		t.Fatal(err)
	}
	if _, err := marshal(Vector{a, b}); err == nil {
		t.Fatal("vector collapsed histories")
	}
}
