package transactions

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// Open replays exact versioned journal records without assuming local durability
// proves protection. Reconcile must establish current authority and retention
// before another proposal or terminal status can succeed. Missing/corrupt replay
// dependencies fail closed and are never silently dropped or repaired.
func Open(ctx context.Context, options Options) (*Engine, error) {
	if options.Protector == nil || options.Applier == nil || options.Header == nil || len(options.Groups) == 0 || len(options.Groups) > 64 {
		return nil, fmt.Errorf("%w: mandatory engine capabilities", ErrEvidence)
	}
	if options.MaximumFrames == 0 {
		options.MaximumFrames = 4096
	}
	if options.MaximumBytes == 0 {
		options.MaximumBytes = 32 << 20
	}
	if options.MaximumFrames < 1 || options.MaximumBytes < 1 || len(options.Groups)+len(options.HistoryGroups)+len(options.EvidenceGroups) > 256 {
		return nil, ErrLimit
	}
	if (len(options.HistoryGroups) > 0 || len(options.EvidenceGroups) > 0) && options.Recovery == nil {
		return nil, missing("historical recovery verifier")
	}
	if err := authorizeRecovery(ctx, options); err != nil {
		return nil, err
	}
	e := &Engine{mu: newGate(), opts: options, groups: make(map[record.UUID]Group), writers: make(map[record.UUID]*segment.Writer), protected: make(map[key]bool), accepts: make(map[key]segment.Entry), state: newLedger(), streams: make(map[key]Group), recoveryWriters: make(map[key]*segment.Writer)}
	var pending [][]segment.Entry
	pendingBytes, pendingFrames := 0, 0
	for _, g := range options.Groups {
		if err := e.addGroup(ctx, g); err != nil {
			_ = e.Close()
			return nil, err
		}
	}
	for _, g := range options.HistoryGroups {
		if err := e.addHistorical(g); err != nil {
			_ = e.Close()
			return nil, err
		}
	}
	for _, g := range options.EvidenceGroups {
		if err := e.addEvidence(ctx, g); err != nil {
			_ = e.Close()
			return nil, err
		}
	}
	for _, g := range e.orderedStreams() {
		entries, err := e.read(ctx, g)
		if err != nil {
			_ = e.Close()
			return nil, err
		}
		pending = append(pending, entries)
		pendingFrames += len(entries)
		for _, entry := range entries {
			pendingBytes += len(entry.Bytes)
		}
		if pendingFrames > e.opts.MaximumFrames || pendingBytes > e.opts.MaximumBytes {
			_ = e.Close()
			return nil, ErrLimit
		}
	}
	if err := e.replay(ctx, pending); err != nil {
		_ = e.Close()
		return nil, err
	}
	for _, entry := range e.entries {
		if !structural(entry) {
			e.blocked = ErrEvidence
			break
		}
	}
	return e, nil
}

// Propose validates and locally syncs one logical transition, then requires the
// current common partner's exact retention/acceptance. Identical proposals reuse
// the existing frame; conflicting candidates never append. Cancellation after
// append returns uncertainty and blocks later work until Reconcile or reopen.
func (e *Engine) Propose(ctx context.Context, volume record.UUID, payload any) (segment.Entry, error) {
	if err := e.mu.LockContext(ctx); err != nil {
		return segment.Entry{}, fmt.Errorf("%w: admission wait: %w", ErrEvidence, err)
	}
	defer e.mu.Unlock()
	if e.closed {
		return segment.Entry{}, segment.ErrClosed
	}
	g, ok := e.groups[volume]
	if !ok {
		return segment.Entry{}, missing("canonical volume owner")
	}
	if err := e.opts.Protector.Check(ctx, cloneBinding(g.Binding)); err != nil {
		return segment.Entry{}, fmt.Errorf("%w: authority: %w", ErrEvidence, err)
	}
	if g.Binding.Authority.CoordinatorTerm < e.state.promises[volume] {
		return segment.Entry{}, conflict("durable promise fences old owner term")
	}
	if recoveryPayload(payload) {
		return segment.Entry{}, missing("use verifier-gated recovery proposal")
	}
	entry, err := e.authored(g, payload)
	if err != nil {
		return segment.Entry{}, err
	}
	if p, ok := entry.Record.Payload.(record.Session); ok {
		if s := e.state.sessions[sessionID(p.Principal, p.Anchor, p.Nonce)]; s != nil && s.close != nil {
			return segment.Entry{}, ErrRetired
		}
	}
	if p, ok := entry.Record.Payload.(record.RequestClaim); ok {
		id := documentID(p.Document)
		if err := e.state.live(id.Session, volume, id.Number); err != nil {
			return segment.Entry{}, err
		}
	}
	if previous := e.existing(entry); previous != nil {
		if !equal(previous.Record.Payload, entry.Record.Payload) {
			return segment.Entry{}, conflict("immutable transition retry")
		}
		if e.blocked != nil {
			return cloneEntry(*previous), fmt.Errorf("%w: reconcile prior uncertainty", ErrEvidence)
		}
		return cloneEntry(*previous), nil
	}
	if e.blocked != nil {
		return segment.Entry{}, fmt.Errorf("%w: reconcile prior uncertainty", ErrEvidence)
	}
	if err := e.current(entry); err != nil {
		return segment.Entry{}, err
	}
	if err := e.state.fresh(entry); err != nil {
		return segment.Entry{}, err
	}
	if err := e.state.admit(entry, false); err != nil {
		return segment.Entry{}, err
	}
	if err := e.capacity(len(entry.Bytes) + 1024); err != nil || len(e.entries)+3 > e.opts.MaximumFrames {
		if err == nil {
			err = ErrLimit
		}
		return segment.Entry{}, err
	}
	if err := e.effect(ctx, entry); err != nil {
		return segment.Entry{}, fmt.Errorf("%w: private apply/barrier: %w", ErrEvidence, err)
	}
	entry.Record.Header.Sequence = 0
	receipt, err := e.writers[volume].Append(ctx, entry.Record)
	if err != nil {
		e.blocked = err
		e.storageErr = err
		return segment.Entry{}, fmt.Errorf("%w: uncertain journal append: %w", ErrEvidence, err)
	}
	entry.Record.Header.Sequence = receipt.Reference.Position.Sequence
	entry.Bytes, err = record.Encode(entry.Record)
	if err != nil {
		e.blocked = err
		e.storageErr = err
		return segment.Entry{}, err
	}
	entry.Reference = receipt.Reference
	if err := receipt.Wait(ctx); err != nil {
		e.blocked = err
		e.storageErr = err
		return cloneEntry(entry), fmt.Errorf("%w: local barrier: %w", ErrEvidence, err)
	}
	if err := e.refresh(ctx, g); err != nil {
		e.blocked = err
		e.storageErr = err
		return cloneEntry(entry), err
	}
	if err := e.protect(ctx, entry); err != nil {
		e.blocked = err
		return cloneEntry(entry), err
	}
	return cloneEntry(entry), nil
}

// Reconcile re-establishes the full retained graph under current sessions and
// repeats private redo/cleanup idempotently. It does not choose a new outcome,
// increment a term, publish visibility, or classify missing history as abort.
func (e *Engine) Reconcile(ctx context.Context) error {
	if err := e.mu.LockContext(ctx); err != nil {
		return fmt.Errorf("%w: recovery wait: %w", ErrEvidence, err)
	}
	defer e.mu.Unlock()
	if e.closed {
		return segment.ErrClosed
	}
	if e.storageErr != nil {
		return fmt.Errorf("%w: reopen after journal uncertainty: %w", ErrEvidence, e.storageErr)
	}
	for _, entry := range e.entries {
		if structural(entry) {
			continue
		}
		if e.groups[entry.Record.Header.Volume].Binding.Authority.CoordinatorTerm < e.state.promises[entry.Record.Header.Volume] {
			return missing("installed authority is fenced by durable promise")
		}
		if err := e.current(entry); err != nil {
			e.blocked = err
			return err
		}
		if err := e.effect(ctx, entry); err != nil {
			e.blocked = err
			return fmt.Errorf("%w: replay apply: %w", ErrEvidence, err)
		}
		if err := e.protect(ctx, entry); err != nil {
			e.blocked = err
			return err
		}
	}
	e.blocked = nil
	return nil
}

// GetStatus routes only by the immutable anchor. Unknown IDs never execute, and
// authority or protection uncertainty never produces a terminal result.
func (e *Engine) GetStatus(ctx context.Context, id fsapi.TransactionID) (Status, error) {
	if err := e.mu.LockContext(ctx); err != nil {
		return Status{ID: id, Uncertain: true}, fmt.Errorf("%w: status wait: %w", ErrEvidence, err)
	}
	defer e.mu.Unlock()
	status := Status{ID: id}
	if !id.Valid() {
		return status, fsapi.Invalid
	}
	if e.closed {
		return status, segment.ErrClosed
	}
	g, ok := e.groups[id.Session.Anchor]
	if !ok {
		return status, missing("anchor owner")
	}
	if err := e.opts.Protector.Check(ctx, cloneBinding(g.Binding)); err != nil {
		status.Uncertain = true
		return status, fmt.Errorf("%w: anchor authority: %w", ErrEvidence, err)
	}
	if e.blocked != nil {
		status.Uncertain = true
		return status, ErrEvidence
	}
	s := e.state.sessions[id.Session]
	if s == nil {
		return status, missing("registered session")
	}
	if s.close != nil || id.Number <= s.floor {
		return status, ErrRetired
	}
	r := e.state.requests[id]
	if r == nil {
		return status, missing("unknown request")
	}
	status.Digest = r.claim.Record.Payload.(record.RequestClaim).Digest
	status.Stage = fsapi.Claimed
	if r.bind != nil {
		b := cloneEntry(*r.bind).Record.Payload.(record.RequestBind).Binding
		status.Binding = &b
		status.Stage = fsapi.Bound
	}
	if r.begin != nil {
		status.Stage = fsapi.Bound
	}
	if len(r.prepares) > 0 {
		status.Stage = fsapi.Preparing
	}
	if r.decision != nil {
		status.Stage = fsapi.Deciding
	}
	if len(r.installs) > 0 {
		status.Stage = fsapi.Installing
	}
	if r.result != nil && e.protected[frameKey(r.result.Reference)] {
		result := cloneEntry(*r.result).Record.Payload.(record.RequestResult).Result
		status.Result = &result
	}
	return status, nil
}

// Cancel requests a protected abort only before any decision candidate exists.
// A prior COMMIT proposal remains immutable even when its acceptance is unknown.
// The caller still installs/completes the abort and records its anchor result.
// A concurrent COMMIT may win between inspection and the abort proposal, yielding
// ErrConflict; retry Cancel or GetStatus with the same ID to learn its outcome.
func (e *Engine) Cancel(ctx context.Context, id fsapi.TransactionID, code uint16) (segment.Entry, error) {
	if err := e.mu.LockContext(ctx); err != nil {
		return segment.Entry{}, fmt.Errorf("%w: cancellation wait: %w", ErrEvidence, err)
	}
	if e.closed {
		e.mu.Unlock()
		return segment.Entry{}, segment.ErrClosed
	}
	if e.blocked != nil {
		e.mu.Unlock()
		return segment.Entry{}, ErrEvidence
	}
	if err := e.state.live(id.Session, id.Session.Anchor, id.Number); err != nil {
		e.mu.Unlock()
		return segment.Entry{}, err
	}
	g, ok := e.groups[id.Session.Anchor]
	if !ok {
		e.mu.Unlock()
		return segment.Entry{}, ErrEvidence
	}
	if err := e.opts.Protector.Check(ctx, cloneBinding(g.Binding)); err != nil {
		e.mu.Unlock()
		return segment.Entry{}, fmt.Errorf("%w: cancel authority: %w", ErrEvidence, err)
	}
	r := e.state.requests[id]
	if r == nil || r.begin == nil {
		e.mu.Unlock()
		return segment.Entry{}, missing("admitted request")
	}
	if r.decision != nil {
		result := cloneEntry(*r.decision)
		e.mu.Unlock()
		return result, nil
	}
	b := r.begin.Record.Payload.(record.Begin)
	a := authority(b.Binding, b.Binding.Coordinator)
	d := record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: id, Term: b.Term}, Digest: b.Binding.RequestDigest, Begin: r.begin.Reference, Outcome: 2, Result: record.TerminalResult{Status: 2, Code: code}, Authority: a}
	e.mu.Unlock()
	return e.Propose(ctx, a.Volume, d)
}

// Acceptance returns an owned exact partner ACCEPT after chosen protection.
// It is suitable for INSTALL.Evidence; it is never a plain receipt substitute.
func (e *Engine) Acceptance(decision record.FrameRef) (segment.Entry, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	entry, ok := e.accepts[frameKey(decision)]
	if !ok || !e.protected[frameKey(decision)] {
		return segment.Entry{}, missing("protected decision acceptance")
	}
	return cloneEntry(entry), nil
}

// Retained returns all immutable source frames as conservative nonexpiring pins.
// No retirement floor, terminal receipt or COMPLETE releases these dependencies.
func (e *Engine) Retained() []segment.Entry {
	e.mu.Lock()
	defer e.mu.Unlock()
	result := make([]segment.Entry, 0, len(e.entries))
	for _, entry := range e.entries {
		result = append(result, cloneEntry(entry))
	}
	for _, entry := range e.accepts {
		found := false
		for _, existing := range result {
			if sameRef(existing.Reference, entry.Reference) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, cloneEntry(entry))
		}
	}
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && referenceLess(result[j].Reference, result[j-1].Reference); j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}

// Close releases local descriptors without changing outcomes or adding a sync.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var errs []error
	for _, w := range e.writers {
		errs = append(errs, w.Close())
	}
	for _, w := range e.recoveryWriters {
		errs = append(errs, w.Close())
	}
	return errors.Join(errs...)
}

func (e *Engine) authored(g Group, payload any) (segment.Entry, error) {
	h := e.opts.Header()
	a := g.Binding.Authority
	h.Volume = a.Volume
	h.Generation = a.Generation
	h.Configuration = a.Configuration
	h.Epoch = a.Epoch
	h.Incarnation = a.OwnerIncarnation
	h.Sequence = 1
	if p, ok := payload.(record.Decision); ok {
		if r := e.state.requests[p.ID.Request]; r != nil && r.begin != nil {
			h.Operation = r.begin.Record.Header.Operation
		}
	}
	if p, ok := payload.(record.Decision); ok && p.Recovery != nil {
		if selection, err := e.state.lookup(*p.Recovery); err == nil {
			if s, ok := selection.Record.Payload.(record.RecoverySelect); ok && s.Selected != nil {
				if selected, err := e.state.lookup(*s.Selected); err == nil {
					h.Operation = selected.Record.Header.Operation
				}
			}
			if s, ok := selection.Record.Payload.(record.RecoverySelect); ok && s.Selected == nil {
				h.Operation = s.Operation
			}
		}
	}
	bytes, err := record.Encode(record.Record{Header: h, Payload: payload})
	if err != nil {
		return segment.Entry{}, err
	}
	r, err := record.Decode(bytes)
	if err != nil {
		return segment.Entry{}, err
	}
	ref, err := record.ReferenceTo(r, bytes)
	return segment.Entry{Record: r, Bytes: bytes, Reference: ref}, err
}

func (e *Engine) current(entry segment.Entry) error {
	g, ok := e.streams[entryStream(entry)]
	if !ok {
		return missing("closure member authority")
	}
	h := entry.Record.Header
	a := g.Binding.Authority
	incarnation := a.OwnerIncarnation
	if entry.Reference.StreamKind == 2 {
		incarnation = g.Stream.ReporterIncarnation
	}
	if h.Generation != a.Generation || h.Configuration != a.Configuration || h.Epoch != a.Epoch || h.Incarnation != incarnation {
		return conflict("stale creator authority/incarnation")
	}
	var authorities []record.Authority
	currentVector := false
	switch p := entry.Record.Payload.(type) {
	case record.Affinity:
		authorities = p.Authorities
		currentVector = true
	case record.RequestBind:
		authorities = p.Binding.Authorities
		currentVector = true
	case record.Begin:
		authorities = p.Binding.Authorities
		currentVector = true
	case record.TxAnchor:
		authorities = p.Binding.Authorities
		currentVector = true
	case record.Prepared:
		authorities = p.Binding.Authorities
		currentVector = true
	case record.Decision:
		authorities = []record.Authority{p.Authority}
	case record.Install:
		authorities = p.Binding.Authorities
	case record.RecoverySelect:
		for _, a := range p.NewAuthorities {
			if !equal(e.groups[a.Volume].Binding.Authority, a) {
				return conflict("recovery vector differs from installed successors")
			}
		}
	}
	for _, a := range authorities {
		if currentVector && entryStream(entry) == streamKey(e.groups[h.Volume].Stream) && !equal(e.groups[a.Volume].Binding.Authority, a) {
			return conflict("new admission uses predecessor authority")
		}
		found := false
		for _, group := range e.streams {
			if equal(group.Binding.Authority, a) {
				found = true
				break
			}
		}
		if !found {
			return conflict("authority vector differs from installed closure")
		}
	}
	return nil
}

func (e *Engine) existing(entry segment.Entry) *segment.Entry {
	want := identity(entry)
	for i := range e.entries {
		if reflect.TypeOf(e.entries[i].Record.Payload) == reflect.TypeOf(entry.Record.Payload) && e.entries[i].Record.Header.Volume == entry.Record.Header.Volume && equal(identity(e.entries[i]), want) {
			return &e.entries[i]
		}
	}
	return nil
}

func (e *Engine) effect(ctx context.Context, entry segment.Entry) error {
	entry = cloneEntry(entry)
	switch p := entry.Record.Payload.(type) {
	case record.Install:
		r := e.state.requests[p.Request]
		decision, err := e.state.lookup(p.Decision.Decision)
		if r == nil || err != nil || !e.protected[frameKey(p.Decision.Decision)] {
			return missing("chosen decision before apply")
		}
		d := cloneEntry(decision)
		if p.Outcome == 1 {
			return e.opts.Applier.Apply(ctx, cloneEntry(*r.prepares[entry.Record.Header.Volume]), d)
		}
		request := AbortRequest{Participant: entry.Record.Header.Volume, Begin: cloneEntry(*r.begin), Decision: d}
		if anchor := r.anchors[request.Participant]; anchor != nil {
			copy := cloneEntry(*anchor)
			request.Anchor = &copy
		}
		if prepared := r.prepares[request.Participant]; prepared != nil {
			copy := cloneEntry(*prepared)
			request.Prepared = &copy
		}
		return e.opts.Applier.Abort(ctx, request)
	case record.Complete:
		request := BarrierRequest{Complete: p}
		for _, ref := range p.Installs {
			entry, err := e.state.lookup(ref)
			if err != nil {
				return err
			}
			request.Installs = append(request.Installs, cloneEntry(entry))
		}
		for _, ref := range p.CacheBarriers {
			entry, err := e.state.lookup(ref)
			if err != nil {
				return err
			}
			request.Barriers = append(request.Barriers, cloneEntry(entry))
		}
		return e.opts.Applier.Barrier(ctx, request)
	}
	return nil
}

func (e *Engine) refresh(ctx context.Context, g Group) error {
	entries, err := e.read(ctx, g)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if known, ok := e.state.frames[frameKey(entry.Reference)]; ok {
			if !sameRef(known.Reference, entry.Reference) {
				return conflict("journal bytes changed")
			}
			continue
		}
		if err := e.capacity(len(entry.Bytes)); err != nil {
			return err
		}
		if recoveryPayload(entry.Record.Payload) {
			if err := e.verifyRecovery(ctx, entry); err != nil {
				return err
			}
		}
		if err := e.state.admit(entry, true); err != nil {
			return err
		}
		e.entries = append(e.entries, entry)
		e.bytes += len(entry.Bytes)
	}
	return nil
}

func (e *Engine) capacity(extra int) error {
	if len(e.state.frames) >= e.opts.MaximumFrames || extra > e.opts.MaximumBytes-e.retainedSize() {
		return ErrLimit
	}
	return nil
}
func structural(entry segment.Entry) bool {
	switch entry.Record.Payload.(type) {
	case record.SegmentOpen, record.SegmentSeal:
		return true
	}
	return false
}
func identity(entry segment.Entry) any {
	switch p := entry.Record.Payload.(type) {
	case record.Session:
		return sessionID(p.Principal, p.Anchor, p.Nonce)
	case record.RequestClaim:
		return documentID(p.Document)
	case record.RequestBind:
		return p.Binding.Request
	case record.Begin:
		return p.Binding.Request
	case record.TxAnchor:
		return p.Request
	case record.Prepared:
		return p.Request
	case record.Decision:
		return struct {
			Request record.RequestID
			Term    uint64
		}{p.ID.Request, p.ID.Term}
	case record.Install:
		return struct {
			Request record.RequestID
			Term    uint64
		}{p.Request, p.Decision.ID.Term}
	case record.Complete:
		return p.Request
	case record.RequestResult:
		return p.Request
	case record.SessionRetire:
		return struct {
			ID    fsapi.SessionID
			Floor uint64
		}{sessionID(p.Principal, p.Anchor, p.Nonce), p.NewFloor}
	case record.SessionClose:
		return sessionID(p.Principal, p.Anchor, p.Nonce)
	case record.Affinity:
		return struct {
			Closure record.UUID
			Version uint64
		}{p.Closure, p.ManifestVersion}
	case record.ViewBarrier:
		return p.NewVisibility
	case record.Promise:
		return struct {
			Recovery, Reporter record.UUID
			Volume             record.UUID
		}{p.RecoveryID, p.Reporter, p.Authority.Volume}
	case record.TailReport:
		return struct {
			Recovery, Reporter record.UUID
			Volume             record.UUID
		}{p.RecoveryID, p.Reporter, entry.Record.Header.Volume}
	case record.Fence:
		return p.RecoveryID
	case record.RecoverySelect:
		return p.RecoveryID
	}
	return nil
}
