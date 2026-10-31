package transport

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// NormalCapability names the indivisible normal all-origin representation.
// No live Endpoint negotiates this capability yet. Emergency 2.0 is separate;
// support for normal 3.0 does not imply emergency or mixed-version service.
type NormalCapability struct {
	Journal, Protocol, Control, Recovery uint16
	Features                             uint64
}

// EncodeNormalCapability returns the one supported exact 24-byte descriptor.
func EncodeNormalCapability(c NormalCapability) ([]byte, error) {
	if err := validateNormalCapability(c); err != nil {
		return nil, err
	}
	b := make([]byte, 24)
	copy(b, "HFSNCAP\x00")
	for i, v := range []uint16{c.Journal, c.Protocol, c.Control, c.Recovery} {
		binary.BigEndian.PutUint16(b[8+i*2:], v)
	}
	binary.BigEndian.PutUint64(b[16:], c.Features)
	return b, nil
}

// DecodeNormalCapability rejects every unknown/missing component or feature.
func DecodeNormalCapability(b []byte) (NormalCapability, error) {
	if len(b) != 24 || !bytes.Equal(b[:8], []byte("HFSNCAP\x00")) {
		return NormalCapability{}, fmt.Errorf("normal capability descriptor")
	}
	c := NormalCapability{binary.BigEndian.Uint16(b[8:]), binary.BigEndian.Uint16(b[10:]), binary.BigEndian.Uint16(b[12:]), binary.BigEndian.Uint16(b[14:]), binary.BigEndian.Uint64(b[16:])}
	return c, validateNormalCapability(c)
}

// EncodeNormalFrame represents a 3.0 transport envelope. Source is always the
// authenticated forwarder; creator identities inside Body are preserved exactly.
// Capability validation is mandatory but is not peer authentication/readiness.
func EncodeNormalFrame(f Frame, limit uint32, c NormalCapability) ([]byte, error) {
	if err := validateNormalCapability(c); err != nil {
		return nil, err
	}
	if err := validateNormalBody(f); err != nil {
		return nil, err
	}
	if f.Kind != Data && f.Kind != Accepted && f.Kind != Progress && f.Kind != StatusReply {
		return nil, fmt.Errorf("normal typed-frame envelope kind")
	}
	if f.Kind == Data && f.Class != Live && f.Class != Bulk || f.Kind != Data && f.Class != Control {
		return nil, fmt.Errorf("normal message class")
	}
	b, err := Encode(f, limit)
	if err != nil {
		return nil, err
	}
	binary.BigEndian.PutUint16(b[8:], 3)
	return b, nil
}

// DecodeNormalFrame owns envelope and creator bytes. Existing Decode and every
// current Endpoint remain strictly 1.0 and reject this envelope.
func DecodeNormalFrame(b []byte, limit uint32, c NormalCapability) (Frame, error) {
	if err := validateNormalCapability(c); err != nil {
		return Frame{}, err
	}
	if len(b) < HeaderSize || len(b) > MaxMessage || uint64(len(b)) > uint64(limit) || limit > MaxMessage || binary.BigEndian.Uint16(b[8:]) != 3 || binary.BigEndian.Uint16(b[10:]) != 0 {
		return Frame{}, fmt.Errorf("normal transport version/size")
	}
	owned := bytes.Clone(b)
	binary.BigEndian.PutUint16(owned[8:], 1)
	f, err := Decode(owned, limit)
	if err != nil {
		return Frame{}, err
	}
	if f.Kind != Data && f.Kind != Accepted && f.Kind != Progress && f.Kind != StatusReply {
		return Frame{}, fmt.Errorf("normal typed-frame envelope kind")
	}
	if f.Kind == Data && f.Class != Live && f.Class != Bulk || f.Kind != Data && f.Class != Control {
		return Frame{}, fmt.Errorf("normal message class")
	}
	if err := validateNormalBody(f); err != nil {
		return Frame{}, err
	}
	return f, nil
}

func validateNormalCapability(c NormalCapability) error {
	if c != (NormalCapability{3, 3, 3, 3, 1}) {
		return fmt.Errorf("unsupported normal component version/features")
	}
	return nil
}

func validateNormalBody(f Frame) error {
	r, err := record.DecodeNormal(f.Body)
	if err != nil {
		return err
	}
	valid := f.Kind == Data
	switch r.Payload.(type) {
	case record.MemoryAcceptance:
		valid = valid || f.Kind == Accepted
	case record.NormalFrontier:
		valid = valid || f.Kind == Progress || f.Kind == StatusReply
	case record.NormalRecovery:
		valid = valid || f.Kind == StatusReply
	}
	if !valid {
		return fmt.Errorf("normal message/body kind mismatch")
	}
	return nil
}
