package transactions

import (
	"context"
	"fmt"
	"sort"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func (e *Engine) protect(ctx context.Context, entry segment.Entry) error {
	g := e.groups[entry.Record.Header.Volume]
	if err := e.opts.Protector.Check(ctx, cloneBinding(g.Binding)); err != nil {
		return fmt.Errorf("%w: authority: %w", ErrEvidence, err)
	}
	frames, required, histories, artifacts, err := e.protectionGraph()
	if err != nil {
		return err
	}
	_, decision := entry.Record.Payload.(record.Decision)
	decision = decision && entryStream(entry) == streamKey(g.Stream)
	receipt, err := e.opts.Protector.Protect(ctx, ProtectionRequest{Binding: cloneBinding(g.Binding), Candidate: cloneEntry(entry), Frames: frames, Required: append(record.Vector(nil), required...), Histories: copyReferences(histories), Artifacts: artifacts, Decision: decision})
	if err != nil {
		return fmt.Errorf("%w: retention: %w", ErrEvidence, err)
	}
	if err := e.opts.Protector.Check(ctx, cloneBinding(g.Binding)); err != nil {
		return fmt.Errorf("%w: authority changed: %w", ErrEvidence, err)
	}
	if receipt.Session != g.Binding.Session || receipt.Connection != g.Binding.Connection || receipt.Partner != g.Binding.Authority.Partner || receipt.Incarnation != g.Binding.Authority.PartnerIncarnation || !sameRef(receipt.Candidate, entry.Reference) || !covers(receipt.Retained, required) || !hasRefs(receipt.Histories, histories) {
		return conflict("receipt session/candidate/prefix")
	}
	if decision {
		accepted, err := decodeAcceptance(receipt.Acceptance, entry, e.state.decisionRequired(&entry))
		if err != nil {
			return err
		}
		if known, ok := e.state.frames[frameKey(accepted.Reference)]; ok && !sameRef(known.Reference, accepted.Reference) {
			return conflict("contradictory reporter acceptance position")
		}
		e.accepts[frameKey(entry.Reference)] = accepted
		if e.retainedSize() > e.opts.MaximumBytes {
			delete(e.accepts, frameKey(entry.Reference))
			return ErrLimit
		}
		e.state.frames[frameKey(accepted.Reference)] = accepted
	} else if len(receipt.Acceptance) != 0 {
		return conflict("unexpected slot acceptance")
	}
	e.protected[frameKey(entry.Reference)] = true
	return nil
}

func (e *Engine) protectionGraph() ([]segment.Entry, record.Vector, []record.FrameRef, []record.Artifact, error) {
	// Retaining every source prefix is deliberately conservative. The bounded
	// transport also resolves artifacts, reporter prefixes and async obligations;
	// the engine never substitutes a hash or a local durable scalar for them.
	var frames []segment.Entry
	var artifacts []record.Artifact
	positions := make(map[key]record.FrameRef)
	seen := make(map[key]record.FrameRef)
	for _, entry := range e.entries {
		frames = append(frames, cloneEntry(entry))
		seen[frameKey(entry.Reference)] = entry.Reference
		k := entryStream(entry)
		if entry.Reference.Position.Sequence > positions[k].Position.Sequence {
			positions[k] = entry.Reference
		}
		switch p := entry.Record.Payload.(type) {
		case record.Prepared:
			artifacts = append(artifacts, p.Artifacts...)
		case record.Promise:
			artifacts = append(artifacts, p.Authorization)
		case record.Fence:
			artifacts = append(artifacts, p.Attestation)
		case record.ViewBarrier:
			artifacts = append(artifacts, p.Attestation)
		}
	}
	for _, entry := range e.accepts {
		if known, ok := seen[frameKey(entry.Reference)]; ok {
			if !sameRef(known, entry.Reference) {
				return nil, nil, nil, nil, conflict("contradictory cached acceptance")
			}
		} else {
			frames = append(frames, cloneEntry(entry))
			seen[frameKey(entry.Reference)] = entry.Reference
		}
		k := entryStream(entry)
		if entry.Reference.Position.Sequence > positions[k].Position.Sequence {
			positions[k] = entry.Reference
		}
	}
	sort.Slice(frames, func(i, j int) bool {
		return referenceLess(frames[i].Reference, frames[j].Reference)
	})
	artifacts, err := uniqueArtifacts(artifacts)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if len(frames) > e.opts.MaximumFrames {
		return nil, nil, nil, nil, ErrLimit
	}
	var required record.Vector
	var histories []record.FrameRef
	for stream, ref := range positions {
		histories = append(histories, ref)
		if g, ok := e.groups[ref.Position.Volume]; ok && stream == streamKey(g.Stream) {
			required = append(required, ref.Position)
		}
	}
	SortReferences(histories)
	sort.Slice(required, func(i, j int) bool { return uuidLess(required[i].Volume, required[j].Volume) })
	return frames, required, histories, artifacts, nil
}

func (l *ledger) acceptance(p record.Install, decision *segment.Entry) error {
	required := l.decisionRequired(decision)
	for _, ref := range p.Acceptance {
		var bytes []byte
		if entry, err := l.lookup(ref); err == nil {
			bytes = entry.Bytes
		}
		for _, frame := range p.Evidence.Frames {
			r, err := record.Decode(frame)
			if err != nil {
				return err
			}
			candidate, err := record.ReferenceTo(r, frame)
			if err != nil {
				return err
			}
			if sameRef(candidate, ref) {
				bytes = frame
			}
		}
		if len(bytes) == 0 {
			return missing("complete ACCEPT frame")
		}
		entry, err := decodeAcceptance(bytes, *decision, required)
		if err != nil {
			return err
		}
		if !sameRef(entry.Reference, ref) {
			return conflict("acceptance reference")
		}
	}
	return nil
}

func decodeAcceptance(bytes []byte, decision segment.Entry, required []record.FrameRef) (segment.Entry, error) {
	r, err := record.Decode(bytes)
	if err != nil {
		return segment.Entry{}, fmt.Errorf("%w: ACCEPT bytes: %w", ErrEvidence, err)
	}
	a, ok := r.Payload.(record.Accept)
	if !ok {
		return segment.Entry{}, conflict("expected typed ACCEPT")
	}
	d := decision.Record.Payload.(record.Decision)
	if !covers(a.Retained, record.Vector{decision.Reference.Position}) {
		return segment.Entry{}, conflict("ACCEPT lacks candidate owner prefix")
	}
	if a.SlotKind != 2 || a.Request == nil || *a.Request != d.ID.Request || a.Term != d.ID.Term || !equal(a.Authority, d.Authority) || a.Reporter != d.Authority.Partner || a.ReporterIncarnation != d.Authority.PartnerIncarnation || !sameRef(a.Candidate, decision.Reference) || !acceptCovers(a, required) || !coveredBy(a.Durable, a.Retained) {
		return segment.Entry{}, conflict("ACCEPT candidate/term/partner/graph")
	}
	if r.Header.Volume != d.ID.Coordinator || r.Header.Generation != d.ID.Generation || r.Header.Configuration != d.ID.Configuration || r.Header.Epoch != d.ID.Epoch {
		return segment.Entry{}, conflict("ACCEPT reporter history")
	}
	ref, err := record.ReferenceTo(r, bytes)
	if err != nil {
		return segment.Entry{}, err
	}
	return segment.Entry{Record: r, Reference: ref, Bytes: append([]byte(nil), bytes...)}, nil
}

func (l *ledger) decisionRequired(decision *segment.Entry) []record.FrameRef {
	d := decision.Record.Payload.(record.Decision)
	r := l.requests[d.ID.Request]
	positions := make(map[key]record.FrameRef)
	add := func(ref record.FrameRef) {
		k := frameKey(ref)
		k.sequence = 0
		if positions[k].Position.Sequence < ref.Position.Sequence {
			positions[k] = ref
		}
	}
	add(decision.Reference)
	for _, pr := range d.Prepares {
		add(pr.Prepare)
		if e := r.prepares[pr.Participant]; e != nil {
			for _, p := range e.Record.Payload.(record.Prepared).Operation.Dependencies {
				if entry, ok := l.frames[frameKey(record.FrameRef{StreamKind: 1, Position: p})]; ok {
					add(entry.Reference)
				}
			}
		}
	}
	for _, anchor := range r.anchors {
		add(anchor.Reference)
	}
	if r.bind != nil {
		add(r.bind.Reference)
		for _, p := range r.bind.Record.Payload.(record.RequestBind).Binding.Dependencies {
			if entry, ok := l.frames[frameKey(record.FrameRef{StreamKind: 1, Position: p})]; ok {
				add(entry.Reference)
			}
		}
	}
	if d.Recovery != nil {
		add(*d.Recovery)
	}
	var vector []record.FrameRef
	for _, p := range positions {
		vector = append(vector, p)
	}
	SortReferences(vector)
	return vector
}

func acceptCovers(a record.Accept, required []record.FrameRef) bool {
	for _, ref := range required {
		if !covers(a.Retained, record.Vector{ref.Position}) && !prefixCovers(a.Dependencies, ref) {
			return false
		}
	}
	return true
}

func prefixCovers(have []record.FrameRef, want record.FrameRef) bool {
	for _, ref := range have {
		a, b := frameKey(ref), frameKey(want)
		a.sequence = 0
		b.sequence = 0
		if a == b && (ref.Position.Sequence > want.Position.Sequence || sameRef(ref, want)) {
			return true
		}
	}
	return false
}
func hasRefs(have, want []record.FrameRef) bool {
	for _, ref := range want {
		if !hasRef(have, ref) {
			return false
		}
	}
	return true
}

func covers(have, required record.Vector) bool {
	for _, want := range required {
		found := false
		for _, p := range have {
			if sameHistory(p, want) && p.Sequence >= want.Sequence {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func coveredBy(durable, retained record.Vector) bool { return covers(retained, durable) }
func sameHistory(a, b record.Position) bool          { a.Sequence = 0; b.Sequence = 0; return a == b }
func uuidLess(a, b record.UUID) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
func cloneBinding(b Binding) Binding {
	b.Authority.Members = append([]record.UUID(nil), b.Authority.Members...)
	return b
}
func cloneEntry(e segment.Entry) segment.Entry {
	r, err := record.Decode(e.Bytes)
	if err != nil {
		panic("verified immutable transaction entry: " + err.Error())
	}
	ref := copyReferences([]record.FrameRef{e.Reference})[0]
	return segment.Entry{Record: r, Reference: ref, Bytes: append([]byte(nil), e.Bytes...)}
}

func copyReferences(refs []record.FrameRef) []record.FrameRef {
	result := make([]record.FrameRef, len(refs))
	for i, ref := range refs {
		result[i] = ref
		if ref.Reporter != nil {
			value := *ref.Reporter
			result[i].Reporter = &value
		}
		if ref.ReporterIncarnation != nil {
			value := *ref.ReporterIncarnation
			result[i].ReporterIncarnation = &value
		}
	}
	return result
}

func (e *Engine) retainedSize() int {
	size := e.bytes
	for _, entry := range e.accepts {
		size += len(entry.Bytes)
	}
	return size
}

func uniqueArtifacts(input []record.Artifact) ([]record.Artifact, error) {
	type artifactKey struct {
		Kind uint16
		ID   record.UUID
	}
	values := make(map[artifactKey]record.Artifact)
	for _, a := range input {
		k := artifactKey{a.Kind, a.ID}
		if old, ok := values[k]; ok && old != a {
			return nil, conflict("artifact identity changed")
		}
		values[k] = a
	}
	var result []record.Artifact
	for _, a := range values {
		result = append(result, a)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return uuidLess(result[i].ID, result[j].ID)
	})
	return result, nil
}
