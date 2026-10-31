package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"reflect"
)

const normalHeaderBytes = 172

type normalHeader struct {
	Stream    OriginStream
	Sequence  uint64 `wire:"min=1"`
	Operation UUID
	Creator   Launch
	Observed  Time
}

// EncodeNormal returns complete, owned, canonical normal 3.0 creator bytes.
// It neither negotiates a capability nor establishes authority or commitment.
func EncodeNormal(r NormalRecord) ([]byte, error) {
	kind := normalKind(r.Payload)
	if kind == 0 {
		return nil, &Error{Unsupported, "normal payload type"}
	}
	if err := normalRecordContext(r); err != nil {
		return nil, err
	}
	header, err := marshal(normalHeader{r.Stream, r.Sequence, r.Operation, r.Creator, r.Observed})
	if err != nil {
		return nil, err
	}
	payload, err := marshal(r.Payload)
	if err != nil {
		return nil, err
	}
	return normalEnvelope("HFSNREC\x00", kind, append(header, payload...))
}

// DecodeNormal validates one exact normal 3.0 frame and owns decoded slices.
// It rejects both legacy 1.0 and emergency 2.0 rather than reinterpreting them.
func DecodeNormal(data []byte) (NormalRecord, error) {
	var r NormalRecord
	kind, body, err := normalBody(data, "HFSNREC\x00")
	if err != nil {
		return r, err
	}
	p := normalPayload(kind)
	if p == nil {
		return r, &Error{Unsupported, "normal record kind"}
	}
	if len(body) < normalHeaderBytes {
		return r, failure("normal creator header size")
	}
	var h normalHeader
	if err := unmarshal(body[:normalHeaderBytes], &h, false); err != nil {
		return r, err
	}
	v := reflect.New(reflect.TypeOf(p))
	if err := unmarshal(body[normalHeaderBytes:], v.Interface(), false); err != nil {
		return r, err
	}
	r = NormalRecord{h.Stream, h.Sequence, h.Operation, h.Creator, h.Observed, v.Elem().Interface()}
	if err := normalRecordContext(r); err != nil {
		return NormalRecord{}, err
	}
	return r, nil
}

// EncodeNormalAuthority serializes installed authority as a distinct 3.0 object.
// The slot authority hash is SHA-256 of its canonical payload, not this envelope.
func EncodeNormalAuthority(a NormalAuthority) ([]byte, error) {
	b, err := marshal(a)
	if err != nil {
		return nil, err
	}
	return normalEnvelope("HFSNOBJ\x00", 1, b)
}

// DecodeNormalAuthority rejects unsupported object/version tags before use.
func DecodeNormalAuthority(data []byte) (NormalAuthority, error) {
	var a NormalAuthority
	kind, body, err := normalBody(data, "HFSNOBJ\x00")
	if err != nil {
		return a, err
	}
	if kind != 1 {
		return a, &Error{Unsupported, "normal object kind"}
	}
	err = unmarshal(body, &a, false)
	return a, err
}

// NormalAuthorityHash returns the exact canonical authority payload identity.
func NormalAuthorityHash(a NormalAuthority) (Hash, error) {
	b, err := marshal(a)
	if err != nil {
		return Hash{}, err
	}
	return sha256.Sum256(b), nil
}

// NormalReference derives identity exclusively from verified creator bytes.
func NormalReference(data []byte) (NormalRef, error) {
	r, err := DecodeNormal(data)
	if err != nil {
		return NormalRef{}, err
	}
	return NormalRef{r.Stream, r.Sequence, r.Creator, normalKind(r.Payload), sha256.Sum256(data)}, nil
}

// VerifyNormalGraph checks actual complete bytes against every exact graph ref.
// It proves byte presence, not storage sync, authenticated assertions or retention.
func VerifyNormalGraph(recovery NormalRecovery, frames map[NormalRef][]byte) error {
	if _, err := marshal(recovery); err != nil {
		return err
	}
	decoded := make(map[NormalRef]NormalRecord, len(recovery.Graph))
	for _, node := range recovery.Graph {
		data, ok := frames[node.Frame]
		if !ok {
			return failure("normal graph missing creator bytes")
		}
		ref, err := NormalReference(data)
		if err != nil {
			return err
		}
		if ref != node.Frame {
			return failure("normal graph creator bytes differ")
		}
		r, err := DecodeNormal(data)
		if err != nil {
			return err
		}
		decoded[ref] = r
		for _, required := range normalDependencies(r.Payload) {
			found := false
			for _, dependency := range node.Dependencies {
				found = found || dependency == required
			}
			if !found {
				return failure("normal graph omitted typed dependency")
			}
		}
	}
	if err := verifyNormalTypedReferences(recovery, decoded); err != nil {
		return err
	}
	for ref, r := range decoded {
		if err := verifyNormalTypedReferences(r.Payload, decoded); err != nil {
			return err
		}
		if err := verifyNormalLinks(ref, r, decoded); err != nil {
			return err
		}
	}
	if err := verifyNormalUniqueness(decoded); err != nil {
		return err
	}
	return nil
}

func normalEnvelope(magic string, kind uint16, body []byte) ([]byte, error) {
	if len(body) > MaxFrame-28 {
		return nil, failure("normal frame size")
	}
	b := make([]byte, 24, 28+len(body))
	copy(b, magic)
	binary.BigEndian.PutUint16(b[8:], NormalMajor)
	binary.BigEndian.PutUint16(b[12:], kind)
	binary.BigEndian.PutUint32(b[16:], uint32(28+len(body)))
	binary.BigEndian.PutUint32(b[20:], uint32(len(body)))
	b = append(b, body...)
	return binary.BigEndian.AppendUint32(b, crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli))), nil
}

func normalBody(data []byte, magic string) (uint16, []byte, error) {
	if len(data) < 24 {
		return 0, nil, &Error{Torn, "normal incomplete envelope"}
	}
	if !bytes.Equal(data[:8], []byte(magic)) {
		return 0, nil, failure("normal magic")
	}
	if binary.BigEndian.Uint16(data[8:]) != NormalMajor || binary.BigEndian.Uint16(data[10:]) != 0 {
		return 0, nil, &Error{Unsupported, "normal version"}
	}
	if binary.BigEndian.Uint16(data[14:]) != 0 {
		return 0, nil, failure("normal reserved bits")
	}
	n, size := uint64(binary.BigEndian.Uint32(data[16:])), uint64(binary.BigEndian.Uint32(data[20:]))
	if n < 28 || n > MaxFrame || n != size+28 {
		return 0, nil, failure("normal bounded length")
	}
	if n > uint64(len(data)) {
		return 0, nil, &Error{Torn, "normal incomplete bounded frame"}
	}
	if n != uint64(len(data)) {
		return 0, nil, failure("normal trailing bytes")
	}
	if crc32.Checksum(data[:len(data)-4], crc32.MakeTable(crc32.Castagnoli)) != binary.BigEndian.Uint32(data[len(data)-4:]) {
		return 0, nil, failure("normal CRC32C")
	}
	return binary.BigEndian.Uint16(data[12:]), data[24 : len(data)-4], nil
}

func normalPayload(kind uint16) any {
	switch kind {
	case NormalOpen:
		return OriginOpen{}
	case NormalSeal:
		return OriginSeal{}
	case NormalGrantKind:
		return NormalGrant{}
	case NormalChoiceKind:
		return OriginChoice{}
	case NormalAcceptanceKind:
		return MemoryAcceptance{}
	case NormalSelectionKind:
		return SlotSelection{}
	case NormalResultKind:
		return NormalResult{}
	case NormalBarrierKind:
		return NormalBarrier{}
	case NormalFrontierKind:
		return NormalFrontier{}
	case NormalRecoveryKind:
		return NormalRecovery{}
	case NormalFragmentKind:
		return OriginFragment{}
	case NormalCloseKind:
		return OriginCloseReport{}
	case NormalPromiseKind:
		return TransitionPromise{}
	}
	return nil
}

func normalKind(payload any) uint16 {
	for kind := NormalOpen; kind <= NormalPromiseKind; kind++ {
		if reflect.TypeOf(payload) == reflect.TypeOf(normalPayload(kind)) {
			return kind
		}
	}
	return 0
}

func normalRecordContext(r NormalRecord) error {
	if r.Creator.Node != r.Stream.Origin {
		return failure("normal creator is not stream origin")
	}
	switch p := r.Payload.(type) {
	case NormalGrant:
		if r.Creator != p.Authority.Sequencer || r.Operation != p.Operation || !sameOriginHistory(r.Stream, p.Origin) {
			return failure("grant creator/operation differs from issuer")
		}
	case OriginChoice:
		if p.Prepared != nil && p.Prepared.Creator.Node == r.Creator.Node {
			return failure("self prepared acceptance")
		}
	case MemoryAcceptance:
		if r.Creator != p.Recipient {
			return failure("acceptance creator differs from recipient")
		}
	case SlotSelection:
		if r.Creator != p.Slot.Sequencer {
			return failure("selection creator differs from sequencer")
		}
	case OriginCloseReport:
		if r.Creator != p.Reporter {
			return failure("normal close report issuer binding")
		}
	case OriginFragment:
		// Inspect the closed kind before recursive decoding. A fragment cannot
		// contain a fragment, even temporarily while validating hostile bytes.
		if len(p.GrantBytes) < 24 || !bytes.Equal(p.GrantBytes[:8], []byte("HFSNREC\x00")) || binary.BigEndian.Uint16(p.GrantBytes[12:]) != NormalGrantKind {
			return failure("origin fragment requires a grant frame")
		}
		grant, err := DecodeNormal(p.GrantBytes)
		if err != nil {
			return err
		}
		g, ok := grant.Payload.(NormalGrant)
		if !ok || r.Stream != g.Origin || r.Creator != g.OriginLaunch || r.Operation != g.Operation {
			return failure("origin fragment grant/creator binding")
		}
	case OriginOpen:
		if p.Previous == nil && r.Sequence != 1 {
			return failure("normal initial OPEN sequence")
		}
		if p.Previous != nil && (p.Previous.Stream != r.Stream || p.Previous.Sequence == ^uint64(0) || p.Previous.Sequence+1 != r.Sequence) {
			return failure("normal OPEN predecessor")
		}
	case OriginSeal:
		if p.Last == ^uint64(0) || p.Last+1 != r.Sequence {
			return failure("normal SEAL sequence")
		}
	}
	return nil
}
