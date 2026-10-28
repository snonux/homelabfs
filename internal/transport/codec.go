package transport

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/fsapi"
)

const (
	// HeaderSize includes the total length prefix and fixed versioned envelope.
	HeaderSize = 144
	// MaxMessage is the absolute allocation bound, including the envelope.
	MaxMessage = 16 << 20
)

// Kind identifies a closed protocol 1.0 message set.
type Kind uint16

const (
	Hello Kind = iota + 1
	HelloOK
	OpenStream
	Resume
	Ready
	Data
	AcceptRequest
	Accepted
	Receipt
	Progress
	Need
	Fetch
	ArtifactChunk
	Credit
	Wait
	Status
	StatusReply
	Error
	Close
	Ping
	Pong
)

// Class isolates control, current data, and historical transfer sockets.
type Class uint8

const (
	Control Class = iota + 1
	Live
	Bulk
)

// Frame is an owned transport envelope. Payload journal bytes are immutable and
// retain their historical creator; SourceIncarnation identifies the forwarder.
// Stream body semantics must be validated by a replication Authorizer.
type Frame struct {
	Kind                                Kind
	Class                               Class
	Session, Connection                 fsapi.UUID
	Source, SourceIncarnation           fsapi.UUID
	Destination, DestinationIncarnation fsapi.UUID
	Correlation, Binding                fsapi.UUID
	Body                                []byte
}

// Encode creates one exact frame; unknown tags and excessive lengths fail closed.
func Encode(f Frame, limit uint32) ([]byte, error) {
	if err := validateFrame(f, limit); err != nil {
		return nil, err
	}
	b := make([]byte, HeaderSize+len(f.Body))
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	copy(b[4:8], "HFST")
	binary.BigEndian.PutUint16(b[8:10], 1)
	binary.BigEndian.PutUint16(b[12:14], uint16(f.Kind))
	b[14] = byte(f.Class)
	ids := []fsapi.UUID{f.Session, f.Connection, f.Source, f.SourceIncarnation, f.Destination, f.DestinationIncarnation, f.Correlation, f.Binding}
	for i, id := range ids {
		copy(b[16+i*16:32+i*16], id[:])
	}
	copy(b[HeaderSize:], f.Body)
	return b, nil
}

// Decode validates one exact complete frame without accepting trailing bytes.
func Decode(b []byte, limit uint32) (Frame, error) {
	var f Frame
	if len(b) < HeaderSize || uint64(len(b)) > uint64(limit) || limit > MaxMessage || limit < HeaderSize {
		return f, fmt.Errorf("transport frame length")
	}
	if binary.BigEndian.Uint32(b) != uint32(len(b)) || string(b[4:8]) != "HFST" || binary.BigEndian.Uint16(b[8:10]) != 1 || binary.BigEndian.Uint16(b[10:12]) != 0 || b[15] != 0 {
		return f, fmt.Errorf("transport version, flags, magic or length")
	}
	f.Kind, f.Class = Kind(binary.BigEndian.Uint16(b[12:14])), Class(b[14])
	ids := []*fsapi.UUID{&f.Session, &f.Connection, &f.Source, &f.SourceIncarnation, &f.Destination, &f.DestinationIncarnation, &f.Correlation, &f.Binding}
	for i, id := range ids {
		copy(id[:], b[16+i*16:32+i*16])
	}
	f.Body = b[HeaderSize:]
	if err := validateFrame(f, limit); err != nil {
		return Frame{}, err
	}
	return f, nil
}

// ReadFrame checks the length before allocating a bounded message body.
func ReadFrame(r io.Reader, limit uint32) (Frame, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if limit < HeaderSize || limit > MaxMessage || n < HeaderSize || n > limit {
		return Frame{}, fmt.Errorf("transport frame length %d", n)
	}
	b := make([]byte, int(n))
	copy(b, prefix[:])
	if _, err := io.ReadFull(r, b[4:]); err != nil {
		return Frame{}, err
	}
	return Decode(b, limit)
}

func validateFrame(f Frame, limit uint32) error {
	if limit < HeaderSize || limit > MaxMessage || uint64(len(f.Body))+HeaderSize > uint64(limit) {
		return fmt.Errorf("transport message bound")
	}
	if f.Kind < Hello || f.Kind > Pong || f.Class < Control || f.Class > Bulk {
		return fmt.Errorf("unknown transport kind or class")
	}
	if !f.Connection.Valid() || !f.Source.Valid() || !f.SourceIncarnation.Valid() || !f.Destination.Valid() || !f.Correlation.Valid() || f.Source == f.Destination {
		return fmt.Errorf("invalid transport endpoint identities")
	}
	if f.Kind != Hello && (!f.Session.Valid() || !f.DestinationIncarnation.Valid()) {
		return fmt.Errorf("unbound transport message")
	}
	if f.Kind >= OpenStream && f.Kind <= StatusReply && !f.Binding.Valid() {
		return fmt.Errorf("missing stream binding")
	}
	return nil
}
