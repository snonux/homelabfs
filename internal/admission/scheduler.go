package admission

import "slices"

// volumeQueue and peerQueue form round-robin rings, rotated only after dispatch.
// Queue metadata contains leases, never uncharged caller payloads.
type volumeQueue struct {
	volume string
	peers  []*peerQueue
}

type peerQueue struct {
	peer  string
	items []*Lease
}

// Enqueue reserves the complete future retained footprint before the caller
// copies a payload, grants writable permission or accepts a protocol obligation.
// Workers are acquired by Next; parked work holds no worker. Queue records count
// against Operations, including accepted queued dirty-page packages.
func (b *Budget) Enqueue(req Request) (*Lease, error) {
	if req.Class == Memory || req.Cost.Workers == 0 {
		return nil, ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	l, err := b.reserve(req, true)
	if err != nil {
		return nil, err
	}
	req = l.req
	var v *volumeQueue
	for _, candidate := range b.queues[req.Class] {
		if candidate.volume == req.Volume {
			v = candidate
			break
		}
	}
	if v == nil {
		v = &volumeQueue{volume: req.Volume}
		b.queues[req.Class] = append(b.queues[req.Class], v)
	}
	for _, p := range v.peers {
		if p.peer == req.Peer {
			p.items = append(p.items, l)
			return l, nil
		}
	}
	v.peers = append(v.peers, &peerQueue{peer: req.Peer, items: []*Lease{l}})
	return l, nil
}

// Next fairly dispatches one runnable item. It scans past blocked volumes,
// closures and peer queues; a stalled closure keeps its serial grant while
// independent closures proceed. Repeated calls service each runnable class,
// volume and origin in turn. Progress assumes workers finish/yield and callers
// repeatedly invoke Next; these CPU/I/O rates are not bandwidth guarantees.
func (b *Budget) Next() (*Lease, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for n := Class(0); n < ClassCount; n++ {
		c := (b.nextClass + n) % ClassCount
		if l := b.nextInClass(c); l != nil {
			b.nextClass = (c + 1) % ClassCount
			return l, true
		}
	}
	return nil, false
}

func (b *Budget) nextInClass(c Class) *Lease {
	for vi, v := range b.queues[c] {
		for pi, p := range v.peers {
			for _, l := range p.items {
				if !l.ready || (l.req.Mutating && b.active[l.req.Closure] != nil) {
					continue
				}
				worker := Resources{Workers: l.req.Cost.Workers}
				if !b.fits(l.req, worker) {
					continue
				}
				b.charge(l.req, worker, true)
				l.charge.Workers = worker.Workers
				l.state = running
				l.generation++
				if l.req.Mutating {
					b.active[l.req.Closure] = l
				}
				// Rotate both rings before removing the dispatched item.
				v.peers = rotate(v.peers, pi)
				b.queues[c] = rotate(b.queues[c], vi)
				b.unqueue(l)
				return l
			}
		}
	}
	return nil
}

func (b *Budget) unqueue(l *Lease) {
	c := l.req.Class
	for vi, v := range b.queues[c] {
		if v.volume != l.req.Volume {
			continue
		}
		for pi, p := range v.peers {
			if p.peer != l.req.Peer {
				continue
			}
			for i, item := range p.items {
				if item != l {
					continue
				}
				p.items = remove(p.items, i)
				if len(p.items) == 0 {
					v.peers = remove(v.peers, pi)
				}
				if len(v.peers) == 0 {
					b.queues[c] = remove(b.queues[c], vi)
				}
				return
			}
		}
	}
}

func rotate[T any](s []T, after int) []T {
	// Queue size is capped by charged Operations; rotation never allocates.
	slices.Reverse(s[:after+1])
	slices.Reverse(s[after+1:])
	slices.Reverse(s)
	return s
}

func remove[T any](s []T, i int) []T {
	copy(s[i:], s[i+1:])
	var zero T
	s[len(s)-1] = zero
	s = s[:len(s)-1]
	if len(s) == 0 {
		return nil
	}
	// Prevent historical high-water capacities accumulating under scope churn.
	if len(s) < cap(s)/4 {
		return slices.Clone(s)
	}
	return s
}
