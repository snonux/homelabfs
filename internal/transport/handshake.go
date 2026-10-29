package transport

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/fsapi"
)

const helloSize = 48

// Limits are finite per-connection bounds negotiated down on each handshake.
// Queue is also the maximum number of locally queued messages per connection.
type Limits struct {
	Message    uint32
	Queue      uint32
	Bytes      uint64
	Operations uint32
}

type greeting struct {
	revision uint64
	limits   Limits
	cluster  fsapi.UUID
}

func (e *Endpoint) newUUID() (fsapi.UUID, error) {
	var id fsapi.UUID
	if _, err := io.ReadFull(e.random(), id[:]); err != nil {
		return id, err
	}
	id[6], id[8] = id[6]&15|64, id[8]&63|128
	return id, nil
}
func (l Limits) valid() bool {
	return l.Message >= HeaderSize+helloSize && l.Message <= MaxMessage && l.Queue > 0 && l.Queue <= 1024 && l.Bytes >= uint64(l.Message) && l.Bytes <= 64<<20 && l.Operations > 0 && l.Operations <= 1024
}
func (l Limits) lower(r Limits) Limits {
	return Limits{min(l.Message, r.Message), min(l.Queue, r.Queue), min(l.Bytes, r.Bytes), min(l.Operations, r.Operations)}
}
func encodeGreeting(g greeting) []byte {
	b := make([]byte, 48)
	// Protocol envelope is 1.0; journal version is explicitly 1.0 here.
	binary.BigEndian.PutUint16(b, 1)
	binary.BigEndian.PutUint64(b[4:], g.revision)
	binary.BigEndian.PutUint32(b[12:], g.limits.Message)
	binary.BigEndian.PutUint32(b[16:], g.limits.Queue)
	binary.BigEndian.PutUint64(b[20:], g.limits.Bytes)
	binary.BigEndian.PutUint32(b[28:], g.limits.Operations)
	copy(b[32:], g.cluster[:])
	return b
}
func decodeGreeting(b []byte) (g greeting, err error) {
	if len(b) != 48 || binary.BigEndian.Uint16(b) != 1 || binary.BigEndian.Uint16(b[2:]) != 0 {
		return g, fmt.Errorf("unsupported journal version or greeting length")
	}
	copy(g.cluster[:], b[32:48])
	g.revision = binary.BigEndian.Uint64(b[4:])
	g.limits = Limits{binary.BigEndian.Uint32(b[12:]), binary.BigEndian.Uint32(b[16:]), binary.BigEndian.Uint64(b[20:]), binary.BigEndian.Uint32(b[28:])}
	if !g.cluster.Valid() || g.revision == 0 || !g.limits.valid() {
		return greeting{}, fmt.Errorf("invalid greeting limits or registry revision")
	}
	return g, nil
}

func (e *Endpoint) random() io.Reader {
	if e.cfg.Random != nil {
		return e.cfg.Random
	}
	return rand.Reader
}
