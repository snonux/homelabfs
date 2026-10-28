package coord

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Memory is a linearizable simulator with controllable faults, not durable storage.
// Its zero value is unavailable; initialize it with NewMemory.
type Memory struct {
	mu                  sync.Mutex
	cluster             fsapi.UUID
	quorum              bool
	revision, compacted int64
	values              map[string]value
	history             map[string][]value
	watchers            map[*memoryWatch]bool
}

// MemoryClient is an independently partitionable simulation connection.
type MemoryClient struct {
	*Client
	connection *memoryBackend
}
type memoryBackend struct {
	memory                       *Memory
	partition, closed, loseReply bool
}
type memoryWatch struct {
	owner *memoryBackend
	key   string
	out   chan change
	done  chan struct{}
	after int64
}

var _ backend = (*memoryBackend)(nil)

// NewMemory initializes a simulator with quorum and no installed policy.
func NewMemory(cluster fsapi.UUID) (*Memory, error) {
	if !cluster.Valid() {
		return nil, fmt.Errorf("invalid cluster UUID")
	}
	return &Memory{cluster: cluster, quorum: true, values: make(map[string]value), history: make(map[string][]value), watchers: make(map[*memoryWatch]bool)}, nil
}

// Connect returns a distinct logical principal connection.
func (m *Memory) Connect(capability Capability) (*MemoryClient, error) {
	if m == nil || !m.cluster.Valid() || capability < Daemon || capability > Operator {
		return nil, ErrDenied
	}
	b := &memoryBackend{memory: m}
	c := &Client{store: b, cluster: m.cluster, capability: capability, prefix: keyPrefix(m.cluster), close: b.close}
	return &MemoryClient{Client: c, connection: b}, nil
}

// SetQuorum fails closed on loss and terminates existing watches. Restoration
// enables fresh requests but never restores a watch or grants serving authority.
func (m *Memory) SetQuorum(available bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quorum = available
	if !available {
		for w := range m.watchers {
			m.finish(w, ErrUnavailable)
		}
	}
}

// Compact discards replay history through a valid store revision.
func (m *Memory) Compact(revision int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if revision <= m.compacted || revision > m.revision {
		return fmt.Errorf("invalid compaction revision")
	}
	m.compacted = revision
	for key, history := range m.history {
		m.history[key] = slices.DeleteFunc(history, func(v value) bool { return v.revision <= revision })
	}
	return nil
}

// SetPartition disconnects only this connection and terminates its watches.
func (c *MemoryClient) SetPartition(partitioned bool) {
	b := c.connection
	b.memory.mu.Lock()
	defer b.memory.mu.Unlock()
	b.partition = partitioned
	if partitioned {
		for w := range b.memory.watchers {
			if w.owner == b {
				b.memory.finish(w, ErrUnavailable)
			}
		}
	}
}

// LoseNextCommitReply injects one committed-but-unknown result. The next successful
// transaction commits atomically, then returns ErrUnavailable instead of its revision.
func (c *MemoryClient) LoseNextCommitReply() {
	c.connection.memory.mu.Lock()
	defer c.connection.memory.mu.Unlock()
	c.connection.loseReply = true
}

func (b *memoryBackend) get(ctx context.Context, key string) (value, error) {
	m := b.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := b.ready(ctx); err != nil {
		return value{}, err
	}
	v, ok := m.values[key]
	if !ok {
		return value{}, ErrNotFound
	}
	v = copyValue(v)
	v.observed = m.revision
	return v, nil
}

func (b *memoryBackend) commit(ctx context.Context, checks []condition, updates []update) (int64, error) {
	m := b.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := b.ready(ctx); err != nil {
		return 0, err
	}
	for _, check := range checks {
		if m.values[check.key].revision != check.revision {
			return 0, ErrConflict
		}
	}
	m.revision++
	for _, update := range updates {
		v := value{bytes: slices.Clone(update.bytes), revision: m.revision}
		m.values[update.key] = v
		m.history[update.key] = append(m.history[update.key], v)
		for w := range m.watchers {
			if w.key == update.key && v.revision > w.after {
				select {
				case w.out <- change{value: copyValue(v)}:
				default:
					m.finish(w, ErrUnavailable)
				}
			}
		}
	}
	if b.loseReply {
		b.loseReply = false
		return 0, fmt.Errorf("commit reply lost: %w", ErrUnavailable)
	}
	return m.revision, nil
}

func (b *memoryBackend) watch(ctx context.Context, key string, after int64) (<-chan change, error) {
	m := b.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := b.ready(ctx); err != nil {
		return nil, err
	}
	out := make(chan change, 32)
	w := &memoryWatch{owner: b, key: key, out: out, done: make(chan struct{}), after: after}
	m.watchers[w] = true
	if after < m.compacted {
		m.finish(w, ErrCompacted)
		return out, nil
	}
	for _, v := range m.history[key] {
		if v.revision > after {
			select {
			case out <- change{value: copyValue(v)}:
			default:
				m.finish(w, ErrUnavailable)
				return out, nil
			}
		}
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-w.done:
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.watchers[w] {
			m.finish(w, ctx.Err())
		}
	}()
	return out, nil
}

func (b *memoryBackend) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.closed || b.partition || !b.memory.quorum {
		return ErrUnavailable
	}
	return nil
}

func (b *memoryBackend) close() error {
	m := b.memory
	m.mu.Lock()
	defer m.mu.Unlock()
	b.closed = true
	for w := range m.watchers {
		if w.owner == b {
			m.finish(w, ErrUnavailable)
		}
	}
	return nil
}

func (m *Memory) finish(w *memoryWatch, err error) {
	delete(m.watchers, w)
	close(w.done)
	// Preserve a terminal error even when the consumer has fallen behind.
	for {
		select {
		case <-w.out:
		default:
			w.out <- change{err: err}
			close(w.out)
			return
		}
	}

}

func copyValue(v value) value {
	return value{bytes: slices.Clone(v.bytes), revision: v.revision, observed: v.observed}
}
