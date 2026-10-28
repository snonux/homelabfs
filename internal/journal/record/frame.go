package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"reflect"
)

// Encode returns owned complete frame bytes for a typed record.
func Encode(r Record) ([]byte, error) {
	kind := kindOf(r.Payload)
	if kind == 0 {
		return nil, &Error{Unsupported, "payload type"}
	}
	if err := validateHeader(r.Header); err != nil {
		return nil, err
	}
	if err := validateRecord(r); err != nil {
		return nil, err
	}
	p, err := marshal(r.Payload)
	if err != nil {
		return nil, err
	}
	if len(p) > MaxFrame-148 {
		return nil, failure("frame size")
	}
	b := make([]byte, 144, 148+len(p))
	copy(b, []byte("HFSJREC\x00"))
	binary.BigEndian.PutUint16(b[8:], 1)
	binary.BigEndian.PutUint16(b[12:], 144)
	binary.BigEndian.PutUint16(b[14:], kind)
	binary.BigEndian.PutUint32(b[20:], uint32(148+len(p)))
	binary.BigEndian.PutUint32(b[24:], uint32(len(p)))
	header, err := marshal(r.Header)
	if err != nil {
		return nil, err
	}
	copy(b[32:], header)
	b = append(b, p...)
	b = binary.BigEndian.AppendUint32(b, crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli)))
	return b, nil
}

// Decode validates one exact complete frame. A Torn error is provisional until
// the caller establishes this is the final frame of the last unsealed segment.
func Decode(data []byte) (Record, error) { return decode(data, false) }

// EncodeIntent exposes canonical intent bytes for internal request identity.
func EncodeIntent(v Intent) ([]byte, error) { return marshal(v) }

// EncodeInternal returns canonical internal bytes and their domain-prefixed hash.
func EncodeInternal(v InternalRequest) ([]byte, Hash, error) {
	b, err := marshal(v)
	if err != nil {
		return nil, Hash{}, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("HOMELABFS-INTERNAL\x00"))
	_, _ = h.Write(b)
	var hash Hash
	copy(hash[:], h.Sum(nil))
	return b, hash, nil
}

// DecodeInternal validates exact canonical internal request bytes.
func DecodeInternal(data []byte) (InternalRequest, error) {
	var v InternalRequest
	if len(data) > MaxFrame {
		return v, failure("internal request size")
	}
	err := unmarshal(data, &v, false)
	return v, err
}

// EncodeOriginal returns public canonical request bytes.
func EncodeOriginal(v OriginalRequest) ([]byte, error) { return marshal(v) }

// DecodeOriginal validates exact public canonical request bytes.
func DecodeOriginal(data []byte) (OriginalRequest, error) {
	var v OriginalRequest
	if len(data) > MaxFrame {
		return v, failure("original request size")
	}
	err := unmarshal(data, &v, false)
	return v, err
}

// EncodeArguments encodes only one mutation's typed arguments.
func EncodeArguments(kind uint16, args any) ([]byte, error) {
	if reflect.TypeOf(argument(kind)) != reflect.TypeOf(args) || args == nil {
		return nil, failure("argument kind/type")
	}
	return marshal(args)
}

// ReferenceTo derives a stream-aware exact-byte reference. SEAL does not encode
// its stream role, so callers must use SealReference with verified OPEN context.
func ReferenceTo(r Record, data []byte) (FrameRef, error) {
	if _, ok := r.Payload.(SegmentSeal); ok {
		return FrameRef{}, failure("SEAL requires explicit stream context")
	}
	if err := checkReferenceBytes(r, data); err != nil {
		return FrameRef{}, err
	}
	return referenceDecoded(r, data), nil
}

// SealReference derives a SEAL reference using the verified segment's OPEN
// payload. The caller must separately verify OPEN and SEAL history continuity.
func SealReference(r Record, data []byte, open SegmentOpen) (FrameRef, error) {
	seal, ok := r.Payload.(SegmentSeal)
	if !ok {
		return FrameRef{}, failure("expected SEAL")
	}
	if err := checkReferenceBytes(r, data); err != nil {
		return FrameRef{}, err
	}
	if _, err := marshal(open); err != nil {
		return FrameRef{}, err
	}
	if seal.Segment != open.Segment || seal.First != open.First || open.StreamKind == 2 && *open.ReporterIncarnation != r.Header.Incarnation {
		return FrameRef{}, failure("SEAL/OPEN context")
	}
	ref := referenceDecoded(r, data)
	ref.StreamKind = open.StreamKind
	ref.Reporter = open.Reporter
	ref.ReporterIncarnation = open.ReporterIncarnation
	return ref, nil
}
func checkReferenceBytes(r Record, data []byte) error {
	if _, err := Decode(data); err != nil {
		return err
	}
	encoded, err := Encode(r)
	if err != nil {
		return err
	}
	if !bytes.Equal(encoded, data) {
		return failure("reference record/bytes mismatch")
	}
	return nil
}
func referenceDecoded(r Record, data []byte) FrameRef {
	ref := FrameRef{StreamKind: 1, Position: Position{Volume: r.Header.Volume, Generation: r.Header.Generation, Configuration: r.Header.Configuration, Epoch: r.Header.Epoch, Sequence: r.Header.Sequence}, SHA256: sha256.Sum256(data)}
	switch p := r.Payload.(type) {
	case Promise:
		ref.StreamKind = 2
		ref.Reporter = &p.Reporter
		ref.ReporterIncarnation = &p.ReporterIncarnation
	case Accept:
		ref.StreamKind = 2
		ref.Reporter = &p.Reporter
		ref.ReporterIncarnation = &p.ReporterIncarnation
	case TailReport:
		ref.StreamKind = 2
		ref.Reporter = &p.Reporter
		ref.ReporterIncarnation = &p.ReporterIncarnation
	case SegmentOpen:
		ref.StreamKind = p.StreamKind
		ref.Reporter = p.Reporter
		ref.ReporterIncarnation = p.ReporterIncarnation
	}
	return ref
}

func decode(data []byte, embedded bool) (Record, error) {
	var r Record
	if len(data) < 144 {
		return r, &Error{Torn, "incomplete fixed header"}
	}
	if !bytes.Equal(data[:8], []byte("HFSJREC\x00")) {
		return r, failure("magic")
	}
	if binary.BigEndian.Uint16(data[8:]) != 1 || binary.BigEndian.Uint16(data[10:]) != 0 || binary.BigEndian.Uint16(data[12:]) != 144 {
		return r, &Error{Unsupported, "version/header length"}
	}
	kind := binary.BigEndian.Uint16(data[14:])
	p := payload(kind)
	if p == nil {
		return r, &Error{Unsupported, "record kind"}
	}
	if binary.BigEndian.Uint32(data[16:]) != 0 || binary.BigEndian.Uint32(data[28:]) != 0 || binary.BigEndian.Uint32(data[140:]) != 0 {
		return r, failure("flags/reserved bits")
	}
	total := uint64(binary.BigEndian.Uint32(data[20:]))
	size := uint64(binary.BigEndian.Uint32(data[24:]))
	if total < 148 || total > MaxFrame || total != 148+size {
		return r, failure("frame lengths")
	}
	// Validate the bounded header before calling a short claimed frame torn.
	if err := unmarshal(data[32:140], &r.Header, embedded); err != nil {
		return Record{}, err
	}
	if err := validateHeader(r.Header); err != nil {
		return Record{}, err
	}
	if total > uint64(len(data)) {
		return Record{}, &Error{Torn, "incomplete bounded frame"}
	}
	if total != uint64(len(data)) {
		return Record{}, failure("trailing frame bytes")
	}
	if crc32.Checksum(data[:len(data)-4], crc32.MakeTable(crc32.Castagnoli)) != binary.BigEndian.Uint32(data[len(data)-4:]) {
		return Record{}, failure("CRC32C")
	}
	v := reflect.New(reflect.TypeOf(p))
	if err := unmarshal(data[144:len(data)-4], v.Interface(), embedded); err != nil {
		return Record{}, err
	}
	r.Payload = v.Elem().Interface()
	if err := validateRecord(r); err != nil {
		return Record{}, err
	}
	return r, nil
}
func validateHeader(h Header) error {
	if !h.Volume.Valid() || !h.Generation.Valid() || !h.Configuration.Valid() || !h.Operation.Valid() || !h.Incarnation.Valid() || h.Sequence == 0 || !h.Observed.Valid() {
		return failure("header identity/sequence/time")
	}
	return nil
}
