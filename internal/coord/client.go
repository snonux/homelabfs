package coord

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
)

const maxValueBytes = 1 << 20

// Close releases client connections. Simulation clients have nothing to close.
func (c *Client) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}

// GetPolicy performs a fresh linearizable read; missing or invalid policy fails closed.
func (c *Client) GetPolicy(ctx context.Context) (Snapshot, error) {
	if err := c.check(); err != nil {
		return Snapshot{}, err
	}
	v, err := c.store.get(ctx, c.prefix+"policy")
	if err != nil {
		return Snapshot{}, err
	}
	return c.snapshot(v)
}

// PutPolicy atomically replaces a complete policy at an exact expected revision.
// Expected zero creates only. Changes do not revoke sessions or activate a history.
func (c *Client) PutPolicy(ctx context.Context, expected int64, p Policy) (Snapshot, error) {
	if err := c.check(); err != nil {
		return Snapshot{}, err
	}
	if c.capability != Operator {
		return Snapshot{}, ErrDenied
	}
	if expected < 0 || p.Registry.Cluster != c.cluster {
		return Snapshot{}, ErrDenied
	}
	if err := p.Validate(); err != nil {
		return Snapshot{}, err
	}
	data, err := encode(p)
	if err != nil {
		return Snapshot{}, err
	}
	if expected > 0 {
		old, err := c.GetPolicy(ctx)
		if err != nil {
			return Snapshot{}, err
		}
		if old.Revision != expected {
			return Snapshot{}, ErrConflict
		}
		if p.Registry.Revision <= old.Policy.Registry.Revision {
			return Snapshot{}, fmt.Errorf("policy registry revision must increase")
		}
	}
	key := c.prefix + "policy"
	rev, err := c.store.commit(ctx, []condition{{key, expected}}, []update{{key, data}})
	if err != nil {
		return Snapshot{}, err
	}
	return c.snapshot(value{bytes: data, revision: rev})
}

// GetGrant reads the latest durable allocation, which never proves activation.
func (c *Client) GetGrant(ctx context.Context, volume fsapi.UUID) (Allocation, error) {
	if err := c.check(); err != nil {
		return Allocation{}, err
	}
	if !volume.Valid() {
		return Allocation{}, ErrDenied
	}
	v, err := c.store.get(ctx, c.grantKey(volume))
	if err != nil {
		return Allocation{}, err
	}
	return c.allocation(v, volume)
}

// Allocate reserves a never-reused configuration UUID and increasing epoch/term.
// A lost reply is an unknown outcome: read GetGrant before deciding to retry.
func (c *Client) Allocate(ctx context.Context, req Request) (Allocation, error) {
	if err := c.check(); err != nil {
		return Allocation{}, err
	}
	if c.capability != Controller || !req.Incarnation.Valid() || req.PolicyRevision <= 0 || req.ExpectedGrant < 0 {
		return Allocation{}, ErrDenied
	}
	snapshot, err := c.GetPolicy(ctx)
	if err != nil {
		return Allocation{}, err
	}
	if snapshot.Revision != req.PolicyRevision {
		return Allocation{}, ErrConflict
	}
	volume, err := snapshot.Policy.candidate(req)
	if err != nil {
		return Allocation{}, err
	}
	history, err := c.nextHistory(ctx, req, volume.History)
	if err != nil {
		return Allocation{}, err
	}
	generator := c.newConfiguration
	if generator == nil {
		generator = randomConfiguration
	}
	history.Configuration, err = generator()
	if err != nil {
		return Allocation{}, fmt.Errorf("configuration UUID: %w", err)
	}
	if !history.Configuration.Valid() {
		return Allocation{}, fmt.Errorf("invalid configuration UUID")
	}
	for _, installed := range snapshot.Policy.Registry.Volumes {
		if installed.History.Configuration == history.Configuration {
			return Allocation{}, fmt.Errorf("configuration UUID collides with installed history")
		}
	}

	g := Grant{Format: 1, Cluster: c.cluster, Volume: req.Volume, Owner: req.Owner, Incarnation: req.Incarnation, History: history, PolicyRevision: snapshot.Revision}
	data, err := encode(g)
	if err != nil {
		return Allocation{}, err
	}
	key, reservation := c.grantKey(req.Volume), c.prefix+"configurations/"+history.Configuration.String()
	checks := []condition{{c.prefix + "policy", snapshot.Revision}, {key, req.ExpectedGrant}, {reservation, 0}}
	revision, err := c.store.commit(ctx, checks, []update{{key, data}, {reservation, data}})
	if err != nil {
		return Allocation{}, err
	}
	return Allocation{Grant: g, Revision: revision}, nil
}

// WatchPolicy replays changes strictly after after (Snapshot.WatchRevision). Terminal errors require a
// fresh GetPolicy before a new watch. A caller must cancel abandoned watches.
func (c *Client) WatchPolicy(ctx context.Context, after int64) (<-chan Event, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	if after <= 0 || after == math.MaxInt64 {
		return nil, fmt.Errorf("invalid policy watch revision")
	}
	watchCtx, cancel := context.WithCancel(ctx)
	changes, err := c.store.watch(watchCtx, c.prefix+"policy", after)
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan Event)
	go func() { defer cancel(); c.forwardWatch(watchCtx, changes, out) }()
	return out, nil
}

func (c *Client) forwardWatch(ctx context.Context, changes <-chan change, out chan<- Event) {
	defer close(out)
	for change := range changes {
		event := Event{Err: change.err}
		if event.Err == nil {
			event.Snapshot, event.Err = c.snapshot(change.value)
		}
		select {
		case out <- event:
		case <-ctx.Done():
			return
		}
		if event.Err != nil {
			return
		}
	}
}

func (c *Client) nextHistory(ctx context.Context, req Request, base cluster.History) (cluster.History, error) {
	if req.ExpectedGrant > 0 {
		old, err := c.GetGrant(ctx, req.Volume)
		if err != nil {
			return base, err
		}
		if old.Revision != req.ExpectedGrant {
			return base, ErrConflict
		}
		if old.Grant.History.Generation != base.Generation {
			return base, fmt.Errorf("generation changes require controlled recovery")
		}
		base.Epoch = max(base.Epoch, old.Grant.History.Epoch)
		base.CoordinatorTerm = max(base.CoordinatorTerm, old.Grant.History.CoordinatorTerm)
	}
	if base.Epoch == math.MaxUint64 || base.CoordinatorTerm == math.MaxUint64 {
		return base, fmt.Errorf("coordination counter exhausted")
	}
	base.Epoch++
	base.CoordinatorTerm++
	return base, nil
}

func (c *Client) check() error {
	if c == nil || c.store == nil || !c.cluster.Valid() || c.capability < Daemon || c.capability > Operator {
		return ErrDenied
	}
	return nil
}

func (c *Client) grantKey(volume fsapi.UUID) string { return c.prefix + "grants/" + volume.String() }

func (c *Client) snapshot(v value) (Snapshot, error) {
	var p Policy
	if err := decode(v.bytes, &p); err != nil {
		return Snapshot{}, err
	}
	if v.revision <= 0 || p.Registry.Cluster != c.cluster {
		return Snapshot{}, ErrDenied
	}
	if err := p.Validate(); err != nil {
		return Snapshot{}, err
	}
	cursor := v.observed
	if cursor == 0 {
		cursor = v.revision
	}
	return Snapshot{Policy: p, Revision: v.revision, WatchRevision: cursor}, nil
}

func (c *Client) allocation(v value, volume fsapi.UUID) (Allocation, error) {
	var g Grant
	if err := decode(v.bytes, &g); err != nil {
		return Allocation{}, err
	}
	h := g.History
	if v.revision <= 0 || g.Format != 1 || g.Cluster != c.cluster || g.Volume != volume || !g.Owner.Valid() || !g.Incarnation.Valid() || !h.Generation.Valid() || !h.Configuration.Valid() || h.Epoch == 0 || h.CoordinatorTerm == 0 || g.PolicyRevision <= 0 {
		return Allocation{}, fmt.Errorf("invalid stored grant")
	}
	return Allocation{Grant: g, Revision: v.revision}, nil
}

func encode(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(data) > maxValueBytes {
		return nil, fmt.Errorf("coordination value exceeds 1 MiB")
	}
	return data, nil
}

func decode(data []byte, dest any) error {
	if len(data) == 0 || len(data) > maxValueBytes {
		return fmt.Errorf("invalid coordination value length")
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode coordination value: %w", err)
	}
	canonical, err := encode(dest)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return fmt.Errorf("coordination value is not canonical versioned JSON")
	}
	return nil
}

func randomConfiguration() (fsapi.UUID, error) {
	var id fsapi.UUID
	if _, err := rand.Read(id[:]); err != nil {
		return id, err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return id, nil
}
