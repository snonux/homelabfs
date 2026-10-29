package sim

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Random is a seeded, labelled deterministic byte source. Give independent core
// components separate labels; call ordering within a component is controlled by
// the scenario. It is concurrency safe, but does not select concurrent callers.
type Random struct {
	mu      sync.Mutex
	seed    uint64
	label   string
	counter uint64
	buffer  []byte
}

// NewRandom constructs a source independent of all other component call orders.
func NewRandom(seed uint64, label string) *Random { return &Random{seed: seed, label: label} }

// Read fills p with deterministic fixture bytes; it is not cryptographic entropy.
func (r *Random) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	size := len(p)
	for len(p) > 0 {
		if len(r.buffer) == 0 {
			var b [16]byte
			binary.BigEndian.PutUint64(b[:], r.seed)
			binary.BigEndian.PutUint64(b[8:], r.counter)
			r.counter++
			h := sha256.New()
			_, _ = h.Write(b[:])
			_, _ = h.Write([]byte(r.label))
			r.buffer = h.Sum(nil)
		}
		n := copy(p, r.buffer)
		p = p[n:]
		r.buffer = r.buffer[n:]
	}
	return size, nil
}

// UUID derives one stable semantic fixture identity from a seed and label.
func UUID(seed uint64, label string) fsapi.UUID {
	var id fsapi.UUID
	_, _ = NewRandom(seed, label).Read(id[:])
	id[6] = id[6]&15 | 64
	id[8] = id[8]&63 | 128
	return id
}
