package transactions

import (
	"context"
	"fmt"
	"sort"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// RecoveryRequest binds a proposed or replayed recovery record to the complete
// bounded local source graph, installed successor vector and original stream.
// Before append Candidate has a provisional sequence; exact payload, creator,
// stream and every referenced frame are final. The verifier must authenticate
// control authorization/artifacts, positive fencing, reporter histories and
// complete intersecting recovery majorities, including accepted-tail continuity.
// It must authorize only this request's exact evidence, never a bare higher term.
type RecoveryRequest struct {
	Candidate  segment.Entry
	Frames     []segment.Entry
	Successors []record.Authority
	Stream     segment.Stream
}

// RecoveryScope binds recovery-only storage admission to the complete configured
// successor, predecessor and reporter stream set. Authorization grants no serving
// rights and is distinct from normal partner readiness/Protector.Check.
type RecoveryScope struct {
	Successors, Predecessors []record.Authority
	Streams                  []segment.Stream
}

// RecoveryVerifier is the trusted takeover validation boundary. It does not
// perform publication. Nil, unavailable or rejected validation gates recovery.
// Engine independently checks hashes, graph completeness, quorum identities,
// term floors and preservation of every known accepted/installed outcome.
type RecoveryVerifier interface {
	Authorize(context.Context, RecoveryScope) error
	Verify(context.Context, RecoveryRequest) error
}

func authorizeRecovery(ctx context.Context, o Options) error {
	if o.Recovery == nil {
		return nil
	}
	scope := RecoveryScope{}
	for _, g := range o.Groups {
		scope.Successors = append(scope.Successors, cloneBinding(g.Binding).Authority)
		scope.Streams = append(scope.Streams, g.Stream)
	}
	for _, g := range o.HistoryGroups {
		scope.Predecessors = append(scope.Predecessors, cloneBinding(g.Binding).Authority)
		scope.Streams = append(scope.Streams, g.Stream)
	}
	for _, g := range o.EvidenceGroups {
		scope.Streams = append(scope.Streams, g.Stream)
	}
	if err := o.Recovery.Authorize(ctx, scope); err != nil {
		return fmt.Errorf("%w: recovery-only scope: %w", ErrEvidence, err)
	}
	return nil
}

// ProposeRecovery persists a verifier-authorized PROMISE/TAIL_REPORT/FENCE or
// RECOVERY_SELECT in its correct explicitly configured stream. Durable promises
// can be returned to the recovery caller only after the local sync. This does
// not activate the successor or replace normal decision ACCEPT protection.
func (e *Engine) ProposeRecovery(ctx context.Context, stream segment.Stream, payload any) (segment.Entry, error) {
	if err := e.mu.LockContext(ctx); err != nil {
		return segment.Entry{}, fmt.Errorf("%w: recovery admission wait: %w", ErrEvidence, err)
	}
	defer e.mu.Unlock()
	if e.closed {
		return segment.Entry{}, segment.ErrClosed
	}
	if e.storageErr != nil {
		return segment.Entry{}, missing("reopen failed journal")
	}
	if e.opts.Recovery == nil {
		return segment.Entry{}, missing("recovery verifier")
	}
	g, ok := e.streams[streamKey(stream)]
	if !ok {
		return segment.Entry{}, missing("authorized recovery stream")
	}
	if !recoveryPayload(payload) {
		return segment.Entry{}, fmt.Errorf("%w: recovery payload", ErrTransition)
	}
	h := e.opts.Header()
	h.Volume = stream.Volume
	h.Generation = stream.Generation
	h.Configuration = stream.Configuration
	h.Epoch = stream.Epoch
	h.Incarnation = g.Binding.Authority.OwnerIncarnation
	h.Sequence = 1
	if stream.Kind == 2 {
		h.Incarnation = stream.ReporterIncarnation
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
	if err != nil {
		return segment.Entry{}, err
	}
	entry := segment.Entry{Bytes: bytes, Record: r, Reference: ref}
	if entryStream(entry) != streamKey(stream) || stream.Kind == 1 && streamKey(stream) != streamKey(e.groups[stream.Volume].Stream) {
		return segment.Entry{}, conflict("recovery writer stream/creator")
	}
	if p, ok := entry.Record.Payload.(record.Promise); ok && p.PromisedTerm < e.state.promises[p.Authority.Volume] {
		return segment.Entry{}, conflict("durable promise regression")
	}
	if err := e.current(entry); err != nil {
		return segment.Entry{}, err
	}
	if previous := e.existing(entry); previous != nil {
		if !equal(previous.Record.Payload, entry.Record.Payload) {
			return segment.Entry{}, conflict("recovery retry")
		}
		if err := e.verifyRecovery(ctx, *previous); err != nil {
			return segment.Entry{}, err
		}
		return cloneEntry(*previous), nil
	}
	if err := e.verifyRecovery(ctx, entry); err != nil {
		return segment.Entry{}, err
	}
	if err := e.state.admit(entry, false); err != nil {
		return segment.Entry{}, err
	}
	if err := e.capacity(len(bytes) + 1024); err != nil || len(e.entries)+3 > e.opts.MaximumFrames {
		return segment.Entry{}, ErrLimit
	}
	w := e.recoveryWriters[streamKey(stream)]
	if stream.Kind == 1 {
		w = e.writers[stream.Volume]
	}
	if w == nil {
		return segment.Entry{}, missing("writable recovery stream")
	}
	r.Header.Sequence = 0
	receipt, err := w.Append(ctx, r)
	if err != nil {
		e.blocked = err
		e.storageErr = err
		return segment.Entry{}, fmt.Errorf("%w: recovery append: %w", ErrEvidence, err)
	}
	if err := receipt.Wait(ctx); err != nil {
		e.blocked = err
		e.storageErr = err
		return segment.Entry{}, fmt.Errorf("%w: recovery sync: %w", ErrEvidence, err)
	}
	if err := e.refresh(ctx, g); err != nil {
		e.blocked = err
		e.storageErr = err
		return segment.Entry{}, err
	}
	stored, err := e.state.lookup(receipt.Reference)
	if err != nil {
		return segment.Entry{}, err
	}
	e.blocked = ErrEvidence
	return cloneEntry(stored), nil
}

func (e *Engine) verifyRecovery(ctx context.Context, entry segment.Entry) error {
	if e.opts.Recovery == nil {
		return missing("trusted recovery verifier")
	}
	var successors []record.Authority
	for _, id := range sortedGroups(e.groups) {
		successors = append(successors, cloneBinding(e.groups[id].Binding).Authority)
	}
	frames := make([]segment.Entry, 0, len(e.entries))
	for _, frame := range e.entries {
		frames = append(frames, cloneEntry(frame))
	}
	g, ok := e.streams[entryStream(entry)]
	if !ok {
		return missing("authorized historical stream")
	}
	if err := e.opts.Recovery.Verify(ctx, RecoveryRequest{Candidate: cloneEntry(entry), Frames: frames, Successors: successors, Stream: g.Stream}); err != nil {
		return fmt.Errorf("%w: fenced recovery validation: %w", ErrEvidence, err)
	}
	return nil
}

func recoveryPayload(payload any) bool {
	switch payload.(type) {
	case record.Promise, record.TailReport, record.RecoverySelect, record.Fence:
		return true
	}
	return false
}
func streamKey(s segment.Stream) key {
	return key{volume: s.Volume, generation: s.Generation, configuration: s.Configuration, epoch: s.Epoch, kind: s.Kind, reporter: s.Reporter, incarnation: s.ReporterIncarnation}
}
func entryStream(entry segment.Entry) key { k := frameKey(entry.Reference); k.sequence = 0; return k }

func (e *Engine) addHistorical(g Group) error {
	var err error
	g, err = e.storageGroup(g)
	if err != nil {
		return err
	}
	current := e.groups[g.Stream.Volume].Binding.Authority
	if g.Stream.Kind != 1 || g.Backend == nil || g.Binding.Authority.Volume != g.Stream.Volume || g.Binding.Authority.Generation != g.Stream.Generation || g.Binding.Authority.Configuration != g.Stream.Configuration || g.Binding.Authority.Epoch != g.Stream.Epoch || e.groups[g.Stream.Volume].Backend == nil || g.Stream.Generation != e.groups[g.Stream.Volume].Stream.Generation || g.Stream.Epoch >= current.Epoch || g.Binding.Authority.CoordinatorTerm >= current.CoordinatorTerm || !validAuthority(g.Binding.Authority) {
		return missing("historical owner lineage")
	}
	if _, exists := e.streams[streamKey(g.Stream)]; exists {
		return conflict("duplicate authorized stream")
	}
	g.Binding = cloneBinding(g.Binding)
	e.streams[streamKey(g.Stream)] = g
	return nil
}

func (e *Engine) addEvidence(ctx context.Context, g Group) error {
	var err error
	g, err = e.storageGroup(g)
	if err != nil {
		return err
	}
	if g.Stream.Kind != 2 || g.Backend == nil || g.Binding.Authority.Volume != g.Stream.Volume || g.Binding.Authority.Generation != g.Stream.Generation || g.Binding.Authority.Configuration != g.Stream.Configuration || g.Binding.Authority.Epoch != g.Stream.Epoch || !member(g.Binding.Authority.Members, g.Stream.Reporter) || e.groups[g.Stream.Volume].Backend == nil || !validAuthority(g.Binding.Authority) || g.Writer.GroupCommit {
		return missing("historical reporter lineage")
	}
	if _, exists := e.streams[streamKey(g.Stream)]; exists {
		return conflict("duplicate reporter stream")
	}
	w, err := segment.NewWriter(ctx, g.Backend, g.Stream, g.Writer)
	if err != nil {
		return err
	}
	g.Binding = cloneBinding(g.Binding)
	e.streams[streamKey(g.Stream)] = g
	e.recoveryWriters[streamKey(g.Stream)] = w
	return nil
}

func validAuthority(a record.Authority) bool {
	if !a.Volume.Valid() || !a.Generation.Valid() || !a.Configuration.Valid() || a.Epoch == 0 || a.CoordinatorTerm == 0 || !a.OwnerIncarnation.Valid() || !a.PartnerIncarnation.Valid() || a.Owner == a.Partner || len(a.Members) != 3 || !member(a.Members, a.Owner) || !member(a.Members, a.Partner) {
		return false
	}
	for i, id := range a.Members {
		if !id.Valid() || i > 0 && !uuidLess(a.Members[i-1], id) {
			return false
		}
	}
	return true
}

func sortedGroups(groups map[record.UUID]Group) []record.UUID {
	var ids []record.UUID
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return uuidLess(ids[i], ids[j]) })
	return ids
}
func (e *Engine) orderedStreams() []Group {
	var groups []Group
	for _, g := range e.streams {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		a, b := groups[i].Stream, groups[j].Stream
		if a.Volume != b.Volume {
			return uuidLess(a.Volume, b.Volume)
		}
		if a.Configuration != b.Configuration {
			return uuidLess(a.Configuration, b.Configuration)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return uuidLess(a.Reporter, b.Reporter)
	})
	return groups
}
