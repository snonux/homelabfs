package viewcontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

type permit struct {
	nativeCommand fsapi.Command
	controller    *Controller
	source        fsservice.ControlledViewSource
	once          sync.Once
	passive       bool
	drain         *guestbridge.Binding
}

// Admit acquires recall closure before any provider execution lock. Same-domain
// dirty overlays participate exactly like every other domain. Terminal ordinary
// results are replayed by the provider before reaching this method.
func (c *Controller) Admit(ctx context.Context, b guestbridge.Binding) (controlledvalidation.Admitted, error) {
	if err := c.CheckRequest(ctx, b.Context, "CALL", b.Canonical); err != nil {
		return nil, err
	}
	if !c.mu.TryLock() {
		return nil, fsapi.WouldBlock
	}
	allowed := false
	defer func() {
		if !allowed {
			c.mu.Unlock()
		}
	}()
	if _, sealed := c.state.Terminal[b.Context.Session]; sealed {
		return nil, fsapi.Permission
	}
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return nil, fsapi.Unavailable
	}
	recovery, err := c.provider(b.Context).IsRecoveryBinding(ctx, b)
	if err != nil {
		return nil, err
	}
	if !recovery {
		if err = c.viewReady(ctx); err != nil {
			return nil, err
		}
	}
	drain := b.Call.Intent.Opcode == "DRAIN_WRITE"
	if drain {
		if err := c.validateDrain(b); err != nil {
			return nil, err
		}
		if c.state.Recall.Phase != "DRAIN" || !c.allAcked("STOPPED") {
			return nil, fsapi.WouldBlock
		}
	} else {
		if c.state.Recall != nil {
			return nil, fsapi.WouldBlock
		}
		if c.conflicts(readOperation(b.Call.Intent.Opcode)) {
			if err := c.startRecall(ctx); err != nil {
				return nil, err
			}
			return nil, fsapi.WouldBlock
		}
	}
	if drain {
		if !c.sourceCapacity(ctx, 0) {
			return nil, fsapi.NoSpace
		}
	} else if !readOperation(b.Call.Intent.Opcode) && !c.sourceCapacity(ctx, 1) {
		return nil, fsapi.NoSpace
	}
	source, err := c.capture(ctx)
	if err != nil {
		return nil, err
	}
	if err = c.remember(source); err != nil {
		return nil, err
	}
	if err = c.save(); err != nil {
		return nil, err
	}
	allowed = true
	return &permit{controller: c, source: source, drain: &b}, nil
}

// AdmitNative applies the same all-domain barrier to actual native commands.
// Advisory lock operations hold no cache/data authority, so their bounded async
// waiters use a passive permit rechecked on every retry without monopolizing
// the controller admission lane or resources required by dirty drains.
func (c *Controller) AdmitNative(ctx context.Context, cred fsapi.Cred, command fsapi.Command) (fsservice.ControlledPermit, error) {
	scope := c.config.Authorization.Enrollment.Domains[0].Scope
	authorization := command.Clone()
	authorization.Name = ""
	authorization.NewName = ""
	authorization.After = ""
	raw, err := canonical(struct {
		Credentials                   fsapi.Cred
		Command                       fsapi.Command
		NameHex, NewNameHex, AfterHex string
	}{cred, authorization, hex.EncodeToString([]byte(command.Name)), hex.EncodeToString([]byte(command.NewName)), hex.EncodeToString([]byte(command.After))})
	if err != nil {
		return nil, err
	}
	if err = c.CheckRequest(ctx, scope, "NATIVE", raw); err != nil {
		return nil, err
	}
	if command.Operation >= fsapi.TryLock && command.Operation <= fsapi.CancelLock {
		return &permit{controller: c, passive: true}, nil
	}
	if !c.mu.TryLock() {
		return nil, fsapi.WouldBlock
	}
	allowed := false
	defer func() {
		if !allowed {
			c.mu.Unlock()
		}
	}()
	if _, sealed := c.state.Terminal[scope.Session]; sealed {
		return nil, fsapi.Permission
	}
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return nil, fsapi.Unavailable
	}
	if c.state.Recall != nil {
		return nil, fsapi.WouldBlock
	}
	if err = c.viewReady(ctx); err != nil {
		return nil, err
	}
	read := command.Operation == fsapi.Read || command.Operation == fsapi.Getattr || command.Operation == fsapi.Lookup || command.Operation == fsapi.Readlink || command.Operation == fsapi.Readdir || command.Operation == fsapi.Statfs
	if c.conflicts(read) {
		if err = c.startRecall(ctx); err != nil {
			return nil, err
		}
		return nil, fsapi.WouldBlock
	}
	if !read && !c.sourceCapacity(ctx, 1) {
		return nil, fsapi.NoSpace
	}
	source, err := c.capture(ctx)
	if err != nil {
		return nil, err
	}
	if err = c.remember(source); err != nil {
		return nil, err
	}
	if err = c.save(); err != nil {
		return nil, err
	}
	allowed = true
	return &permit{controller: c, source: source, nativeCommand: command}, nil
}

// Check verifies the exact admitted durable/state cut on the worker. No fresh
// capability lookup, recall wait, physical token or controller lock is acquired.
func (p *permit) Check(ctx context.Context, cut *fsservice.ControlledCut) error {
	c := p.controller
	if p.passive {
		if !c.mu.TryLock() {
			return fsapi.WouldBlock
		}
		defer c.mu.Unlock()
		if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
			return fsapi.Unavailable
		}
		return c.config.Facts.CheckEnrollment(ctx, c.config.Authorization.Enrollment)
	}
	if c.closed.Load() || c.poisoned.Load() || c.state.Recovery {
		return fsapi.Unavailable
	}
	source, err := cut.ViewSource(ctx, c.config.Authorization.Enrollment.Bounds.SourceBytes)
	if err != nil {
		return err
	}
	if !sameSource(source, p.source) {
		return fsapi.Stale
	}
	return c.config.Facts.CheckEnrollment(ctx, c.config.Authorization.Enrollment)
}

// Close releases only the acquisition lane; physical grants, sources and dirty
// obligations remain in the durable ledger until exact verified closure.
func (p *permit) Close() {
	p.once.Do(func() {
		if !p.passive {
			p.controller.mu.Unlock()
		}
	})
}

// Disconnect retains all physical rights and source/claim obligations while
// invalidating learned acknowledgements. Recover must verify complete ledgers.
func (c *Controller) Disconnect(ctx context.Context, scope guestbridge.Context) error {
	if err := c.check(ctx, scope); err != nil {
		return err
	}
	if !c.mu.TryLock() {
		return fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	c.state.Recovery = true
	if c.state.Recall != nil {
		c.state.Recall.Acks = map[string]json.RawMessage{}
	}
	return c.save()
}

// Recover requires live reconstructed full-ledger continuity/positive exclusion;
// a new UUID, timer, disconnect or hash-only inventory can never reopen access.
func (c *Controller) Recover(ctx context.Context) error {
	if !c.mu.TryLock() {
		return fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	raw, err := c.rawState()
	if err != nil {
		return err
	}
	if err = c.config.Facts.CheckRecovery(ctx, c.config.Authorization.Enrollment, raw); err != nil {
		return err
	}
	source, err := c.capture(ctx)
	if err != nil {
		return err
	}
	if err = c.remember(source); err != nil {
		return err
	}
	c.state.Recovery = false
	return c.save()
}

// CheckDrain authenticates the exact original positional mmap write before the
// shared service skips append offset selection. It grants no general bypass.
func (p *permit) CheckDrain(ctx context.Context, command fsapi.Command) error {
	if p.drain == nil || p.drain.Call.Intent.Opcode != "DRAIN_WRITE" || command.Operation != fsapi.Write {
		return fsapi.Permission
	}
	if err := p.controller.validateDrain(*p.drain); err != nil {
		return err
	}
	var a struct {
		Description guestbridge.Capability
		Offset      uint64
		Data        string
	}
	if err := json.Unmarshal(p.drain.Call.Intent.Args, &a); err != nil {
		return err
	}
	data, err := hex.DecodeString(a.Data)
	if err != nil {
		return err
	}
	if command.Object.Home.String() != a.Description.Home || command.Object.Allocation.String() != a.Description.Allocation || command.Offset < 0 || uint64(command.Offset) != a.Offset || !bytes.Equal(command.Data, data) {
		return fsapi.Stale
	}
	return ctx.Err()
}

func (c *Controller) conflicts(read bool) bool {
	for _, l := range c.state.Leases {
		if l.Active && (!read || l.Lease.Mode == "EXCLUSIVE_DIRTY") {
			return true
		}
	}
	for _, r := range c.state.Reservations {
		if r.Status != "CLOSED" {
			return true
		}
	}
	return false
}

func readOperation(op string) bool {
	switch op {
	case "ROOT", "LOOKUP", "GETATTR", "READ", "READLINK", "READDIR", "STATFS":
		return true
	}
	return false
}

func (c *Controller) viewReady(ctx context.Context) error {
	for _, p := range c.config.Providers {
		if err := p.CheckViewReady(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CheckSource reserves the complete representable post-effect source before any
// journal append/apply; outstanding dirty growth remains charged cumulatively.
func (p *permit) CheckSource(ctx context.Context, cut *fsservice.ControlledCut, effects []fsservice.ControlledEffect) error {
	if p.passive && len(effects) > 0 {
		return fsapi.Permission
	}
	exclude := ""
	if p.drain != nil && p.drain.Call.Intent.Opcode == "DRAIN_WRITE" {
		var args struct{ Reservation string }
		if err := json.Unmarshal(p.drain.Call.Intent.Args, &args); err != nil {
			return err
		}
		exclude = args.Reservation
	}
	future := p.controller.futureWrites(ctx, exclude)
	extraPins := 0
	switch p.nativeCommand.Operation {
	case fsapi.Create, fsapi.Mkdir, fsapi.SymlinkOp, fsapi.Open:
		mutation, pin := false, false
		for _, effect := range effects {
			switch effect.Payload.(type) {
			case record.Mutation:
				mutation = true
			case record.HandlePin:
				pin = true
			}
		}
		if mutation && !pin {
			extraPins = 1
		}
	}
	_, err := cut.SourceBytesAfter(ctx, effects, future, extraPins, p.controller.config.Authorization.Enrollment.Bounds.SourceBytes)
	return err
}
func (c *Controller) futureWrites(ctx context.Context, exclude string) []storage.FutureWrite {
	future := []storage.FutureWrite{}
	for id, r := range c.state.Reservations {
		if id == exclude || r.Status == "CLOSED" {
			continue
		}
		if r.Call != nil && c.config.Providers[r.Reservation.Scope.Session].CheckFrozenResult(ctx, r.Reservation.Scope, *r.Call) == nil {
			continue
		}
		home, _ := fsapi.ParseUUID(r.Reservation.Description.Home)
		allocation, _ := fsapi.ParseUUID(r.Reservation.Description.Allocation)
		future = append(future, storage.FutureWrite{Object: fsapi.ObjectID{Home: home, Allocation: allocation}, End: int64(r.Reservation.Offset + r.Reservation.Length)})
	}
	return future
}
func (c *Controller) reserveSourceBytes(ctx context.Context, r request) error {
	future := c.futureWrites(ctx, "")
	home, _ := fsapi.ParseUUID(r.Description.Home)
	allocation, _ := fsapi.ParseUUID(r.Description.Allocation)
	future = append(future, storage.FutureWrite{Object: fsapi.ObjectID{Home: home, Allocation: allocation}, End: int64(r.Offset + r.Length)})
	return c.config.Service.Controlled(ctx, c.config.Authorization.Enrollment.Domains[0].Profile, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		_, err := cut.SourceBytesAfter(ctx, nil, future, 0, c.config.Authorization.Enrollment.Bounds.SourceBytes)
		return err
	})
}
