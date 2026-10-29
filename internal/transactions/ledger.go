package transactions

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func newLedger() *ledger {
	return &ledger{sessions: make(map[fsapi.SessionID]*session), requests: make(map[fsapi.TransactionID]*request), affinities: make(map[record.AffinityRef]map[record.UUID]*segment.Entry), active: make(map[record.UUID]fsapi.TransactionID), visibility: make(map[record.UUID]uint64), frames: make(map[key]segment.Entry), promises: make(map[record.UUID]uint64)}
}

func (l *ledger) admit(e segment.Entry, apply bool) error {
	if err := l.validate(e); err != nil {
		return err
	}
	if !apply {
		return nil
	}
	l.frames[frameKey(e.Reference)] = e
	v := e.Record.Header.Volume
	switch p := e.Record.Payload.(type) {
	case record.Session:
		l.sessions[sessionID(p.Principal, p.Anchor, p.Nonce)] = &session{registration: &e}
	case record.RequestClaim:
		l.requests[documentID(p.Document)] = &request{claim: &e, anchors: make(map[record.UUID]*segment.Entry), prepares: make(map[record.UUID]*segment.Entry), installs: make(map[record.UUID]*segment.Entry), candidates: make(map[uint64]*segment.Entry)}
	case record.RequestBind:
		l.requests[p.Binding.Request].bind = &e
	case record.Begin:
		l.requests[p.Binding.Request].begin = &e
		for _, id := range p.Binding.Participants {
			l.active[id] = p.Binding.Request
		}
	case record.TxAnchor:
		l.requests[p.Request].anchors[v] = &e
	case record.Prepared:
		l.requests[p.Request].prepares[v] = &e
	case record.Decision:
		l.requests[p.ID.Request].decision = &e
		l.requests[p.ID.Request].candidates[p.ID.Term] = &e
	case record.Promise:
		if l.promises[p.Authority.Volume] < p.PromisedTerm {
			l.promises[p.Authority.Volume] = p.PromisedTerm
		}
	case record.RecoverySelect:
		l.requests[p.Binding.Request].selection = &e
	case record.Install:
		l.requests[p.Request].installs[v] = &e
	case record.Complete:
		l.requests[p.Request].complete = &e
		for _, id := range l.requests[p.Request].bind.Record.Payload.(record.RequestBind).Binding.Participants {
			delete(l.active, id)
		}
		if p.Result.Status == 1 {
			l.visibility[p.Visibility.Affinity.Closure] = p.Visibility.Generation
		}
	case record.RequestResult:
		l.requests[p.Request].result = &e
	case record.SessionRetire:
		s := l.sessions[sessionID(p.Principal, p.Anchor, p.Nonce)]
		s.floor = p.NewFloor
		s.retirement = &e
	case record.SessionClose:
		l.sessions[sessionID(p.Principal, p.Anchor, p.Nonce)].close = &e
	case record.Affinity:
		ref := affinityRef(p, e.Bytes)
		if l.affinities[ref] == nil {
			l.affinities[ref] = make(map[record.UUID]*segment.Entry)
		}
		l.affinities[ref][v] = &e
		if l.visibility[p.Closure] == 0 {
			l.visibility[p.Closure] = 1
		}
	}
	return nil
}

func (l *ledger) validate(e segment.Entry) error {
	v := e.Record.Header.Volume
	switch p := e.Record.Payload.(type) {
	case record.SegmentOpen, record.SegmentSeal:
		return nil
	case record.Session:
		if v != p.Anchor {
			return conflict("session at wrong anchor")
		}
		if l.sessions[sessionID(p.Principal, p.Anchor, p.Nonce)] != nil {
			return conflict("duplicate session")
		}
	case record.RequestClaim:
		id := documentID(p.Document)
		if err := l.live(id.Session, v, id.Number); err != nil {
			return err
		}
		if l.requests[id] != nil {
			return conflict("request already claimed")
		}
	case record.RequestBind:
		r, err := l.claim(p.Binding.Request, p.Binding.RequestDigest)
		if err != nil {
			return err
		}
		if v != p.Binding.Request.Session.Anchor || !sameRef(p.Claim, r.claim.Reference) || r.bind != nil || r.result != nil {
			return conflict("immutable request binding")
		}
		if err := l.live(p.Binding.Request.Session, v, p.Binding.Request.Number); err != nil {
			return err
		}
		if err := l.affinity(p.Binding); err != nil {
			return err
		}
		if err := bindingDocument(e, p, r.claim.Record.Payload.(record.RequestClaim).Document); err != nil {
			return err
		}
		if !completeClosure(p.Binding, r.claim.Record.Payload.(record.RequestClaim).Document) {
			return conflict("binding omits endpoint/object-home dependency")
		}
		return l.vector(p.Binding.Dependencies)
	case record.Begin:
		r, err := l.claim(p.Binding.Request, p.Binding.RequestDigest)
		if err != nil {
			return err
		}
		if r.bind == nil {
			return missing("request binding")
		}
		b := r.bind.Record.Payload.(record.RequestBind)
		if v != p.Binding.Coordinator || r.begin != nil || r.result != nil || !sameRef(p.Bind, r.bind.Reference) || !equal(p.Binding, b.Binding) || !equal(p.Document, r.claim.Record.Payload.(record.RequestClaim).Document) || !equal(p.Authorizations, b.Authorizations) {
			return conflict("BEGIN binding/document")
		}
		for _, a := range p.Binding.Authorities {
			if a.CoordinatorTerm != p.Term {
				return conflict("BEGIN term")
			}
		}
		for _, id := range p.Binding.Participants {
			if _, busy := l.active[id]; busy {
				return fmt.Errorf("%w: closure busy", ErrTransition)
			}
		}
	case record.TxAnchor:
		r, err := l.begun(p.Request, p.Digest, p.Binding, p.Begin, p.CoordinatorTerm, v)
		if err != nil {
			return err
		}
		if !participantCreator(e, p.Binding) {
			return conflict("participant anchor creator authority")
		}
		if r.anchors[v] != nil || r.decision != nil && r.decision.Record.Payload.(record.Decision).ID.Term <= p.CoordinatorTerm {
			return conflict("duplicate/late participant anchor")
		}
	case record.Prepared:
		r, err := l.begun(p.Request, p.Digest, p.Binding, p.Begin, p.Term, v)
		if err != nil {
			return err
		}
		if len(r.anchors) != len(p.Binding.Participants) {
			return missing("complete participant anchor fanout")
		}
		if !participantCreator(e, p.Binding) {
			return conflict("prepare creator authority")
		}
		if r.prepares[v] != nil || r.decision != nil && r.decision.Record.Payload.(record.Decision).ID.Term <= p.Term {
			return conflict("duplicate/late prepare")
		}
		if err := l.refs(p.Retained); err != nil {
			return err
		}
		for _, position := range p.Operation.Dependencies {
			if !member(p.Binding.Participants, position.Volume) {
				return conflict("prepare dependency outside bound closure")
			}
		}
		return l.vector(p.Operation.Dependencies)
	case record.Decision:
		return l.validateDecision(e, p)
	case record.Install:
		return l.validateInstall(e, p)
	case record.ViewBarrier:
		return l.validateBarrier(e, p)
	case record.Complete:
		return l.validateComplete(e, p)
	case record.RequestResult:
		return l.validateResult(e, p)
	case record.SessionRetire:
		return l.validateRetire(e, p)
	case record.SessionClose:
		id := sessionID(p.Principal, p.Anchor, p.Nonce)
		s := l.sessions[id]
		if s == nil {
			return missing("session registration")
		}
		if v != p.Anchor || s.close != nil || s.floor != p.Floor || (s.retirement == nil) != (p.Retirement == nil) || s.retirement != nil && !sameRef(s.retirement.Reference, *p.Retirement) {
			return conflict("session close floor")
		}
		for id, r := range l.requests {
			if id.Session == sessionID(p.Principal, p.Anchor, p.Nonce) && (r.result == nil || id.Number > s.floor) {
				return fmt.Errorf("%w: unretired claim", ErrTransition)
			}
		}
	case record.Affinity:
		return l.validateAffinity(e, p)
	case record.Accept:
		return l.validateAccept(e, p)
	case record.Promise:
		return l.validatePromise(e, p)
	case record.TailReport:
		if err := l.refs(p.Promises); err != nil {
			return err
		}
		if err := l.refs(p.Candidates); err != nil {
			return err
		}
		for _, tail := range p.Tails {
			if err := l.vector(record.Vector{tail.Received}); err != nil {
				return err
			}
		}
		return l.refs(p.Dependencies)
	case record.Fence:
		return l.refs(p.Dependencies)
	case record.RecoverySelect:
		return l.validateRecoverySelect(e, p)
	default:
		return fmt.Errorf("%w: unsupported protocol record %T", ErrTransition, p)
	}
	return nil
}

func (l *ledger) validateResult(e segment.Entry, p record.RequestResult) error {
	r, err := l.claim(p.Request, p.Digest)
	if err != nil {
		return err
	}
	if e.Record.Header.Volume != p.Request.Session.Anchor || r.result != nil || !sameRef(p.Claim, r.claim.Reference) {
		return conflict("anchor result identity")
	}
	if p.Result.Status == 3 {
		if r.begin != nil {
			return conflict("rejection after admission")
		}
	} else {
		if r.complete == nil {
			return missing("COMPLETE")
		}
		if !equal(p.Result, r.complete.Record.Payload.(record.Complete).Result) || !hasRef(p.Dependencies, r.complete.Reference) {
			return conflict("terminal result differs from COMPLETE")
		}
	}
	if (r.bind == nil) != (p.Binding == nil) || r.bind != nil && !sameRef(*p.Binding, r.bind.Reference) {
		return conflict("result binding")
	}
	return l.refs(p.Dependencies)
}

func (l *ledger) validateRetire(e segment.Entry, p record.SessionRetire) error {
	id := sessionID(p.Principal, p.Anchor, p.Nonce)
	s := l.sessions[id]
	if s == nil {
		return missing("session")
	}
	if e.Record.Header.Volume != p.Anchor || s.close != nil || p.PreviousFloor != s.floor || p.Replacement != nil {
		return conflict("retirement predecessor/replacement")
	}
	var expected []record.FrameRef
	for rid, r := range l.requests {
		if rid.Session == id && rid.Number > s.floor && rid.Number <= p.NewFloor {
			if r.result == nil {
				return missing("terminal receipt below floor")
			}
			expected = append(expected, r.result.Reference)
		}
	}
	if len(expected) != len(p.TerminalResults) {
		return conflict("retirement receipt coverage")
	}
	for _, ref := range expected {
		if !hasRef(p.TerminalResults, ref) {
			return conflict("retirement receipt omitted")
		}
	}
	return l.refs(p.TerminalResults)
}

func (l *ledger) validateAffinity(e segment.Entry, p record.Affinity) error {
	if !member(p.Members, e.Record.Header.Volume) || len(p.Authorities) != len(p.Members) {
		return conflict("affinity member/authority vector")
	}
	for _, id := range p.Members {
		if _, active := l.active[id]; active {
			return missing("affinity fanout waits for historical active closure")
		}
	}
	for i, a := range p.Authorities {
		if a.Volume != p.Members[i] || i > 0 && (a.Owner != p.Authorities[0].Owner || a.Partner != p.Authorities[0].Partner || a.OwnerIncarnation != p.Authorities[0].OwnerIncarnation || a.PartnerIncarnation != p.Authorities[0].PartnerIncarnation || !equal(a.Members, p.Authorities[0].Members)) {
			return conflict("incompatible affinity placement")
		}
	}
	ref := affinityRef(p, e.Bytes)
	if l.affinities[ref][e.Record.Header.Volume] != nil {
		return conflict("duplicate affinity")
	}
	for old, copies := range l.affinities {
		for _, prior := range copies {
			manifest := prior.Record.Payload.(record.Affinity)
			if !overlap(manifest.Members, p.Members) {
				continue
			}
			if old == ref {
				continue
			}
			if !l.manifestAncestor(p.Predecessors, old, make(map[record.AffinityRef]bool)) || !subset(manifest.Members, p.Members) || old.Closure == p.Closure && old.ManifestVersion >= p.ManifestVersion {
				return conflict("affinity lost predecessor/member")
			}
		}
	}
	for _, predecessor := range p.Predecessors {
		if len(l.affinities[predecessor]) == 0 {
			return missing("affinity predecessor")
		}
		for _, entry := range l.affinities[predecessor] {
			if len(l.affinities[predecessor]) != len(entry.Record.Payload.(record.Affinity).Members) {
				return missing("complete predecessor affinity")
			}
			break
		}
	}
	return nil
}

func (l *ledger) manifestAncestor(refs []record.AffinityRef, want record.AffinityRef, seen map[record.AffinityRef]bool) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		for _, entry := range l.affinities[ref] {
			if l.manifestAncestor(entry.Record.Payload.(record.Affinity).Predecessors, want, seen) {
				return true
			}
			break
		}
	}
	return false
}

func resolvedResult(result record.TerminalResult, intent record.Intent) bool {
	var source record.ObjectID
	var replacement *record.ObjectID
	switch a := intent.Arguments.(type) {
	case record.Rename:
		source = a.Source
		replacement = a.Replaced
	case record.Link:
		source = a.Source
	case record.Create:
		source = a.Object
	case record.Mkdir:
		source = a.Object
	case record.Symlink:
		source = a.Object
	case record.Unlink:
		source = a.Removed
	case record.Rmdir:
		source = a.Removed
	case record.Write:
		source = a.Object
	case record.Truncate:
		source = a.Object
	case record.Setattr:
		source = a.Object
	default:
		return false
	}
	return result.Source != nil && *result.Source == source && equal(result.Replacement, replacement)
}

func completeClosure(b record.Binding, document record.RequestDocument) bool {
	var objects []record.ObjectID
	switch a := b.Intent.Arguments.(type) {
	case record.Create:
		objects = []record.ObjectID{a.Parent, a.Object}
	case record.Mkdir:
		objects = []record.ObjectID{a.Parent, a.Object}
	case record.Symlink:
		objects = []record.ObjectID{a.Parent, a.Object}
	case record.Link:
		objects = []record.ObjectID{a.Source, a.TargetParent}
	case record.Unlink:
		objects = []record.ObjectID{a.Parent, a.Removed}
	case record.Rmdir:
		objects = []record.ObjectID{a.Parent, a.Removed}
	case record.Rename:
		objects = []record.ObjectID{a.SourceParent, a.Source, a.DestinationParent}
		if a.Replaced != nil {
			objects = append(objects, *a.Replaced)
		}
	case record.Write:
		objects = []record.ObjectID{a.Object}
	case record.Truncate:
		objects = []record.ObjectID{a.Object}
	case record.Setattr:
		objects = []record.ObjectID{a.Object}
	}
	for _, edit := range b.Intent.SelectedMetadata {
		objects = append(objects, edit.Object)
	}
	for _, object := range objects {
		if !member(b.Participants, object.Home) {
			return false
		}
	}
	for _, position := range b.Dependencies {
		if !member(b.Participants, position.Volume) {
			return false
		}
	}
	if document.Original != nil {
		return member(b.Participants, document.Original.Source.Volume) && member(b.Participants, document.Original.Destination.Volume)
	}
	return document.Internal != nil && member(b.Participants, document.Internal.NamespaceVolume)
}

func bindingDocument(entry segment.Entry, b record.RequestBind, document record.RequestDocument) error {
	// BEGIN owns the canonical document/intent compatibility rules. Validate the
	// future BEGIN representation before fixing the binding; these temporary
	// bytes are neither appended nor admitted as evidence. The provisional bind
	// reference is used only for its codec-valid owner-reference representation.
	a := authority(b.Binding, b.Binding.Coordinator)
	h := entry.Record.Header
	h.Volume = a.Volume
	h.Generation = a.Generation
	h.Configuration = a.Configuration
	h.Epoch = a.Epoch
	h.Incarnation = a.OwnerIncarnation
	_, err := record.Encode(record.Record{Header: h, Payload: record.Begin{Binding: b.Binding, Bind: entry.Reference, Document: document, Authorizations: b.Authorizations, Term: a.CoordinatorTerm}})
	if err != nil {
		return fmt.Errorf("%w: binding differs from original document: %w", ErrConflict, err)
	}
	return nil
}

func (l *ledger) live(id fsapi.SessionID, v record.UUID, n uint64) error {
	if v != id.Anchor {
		return conflict("immutable anchor routing")
	}
	s := l.sessions[id]
	if s == nil {
		return missing("session registration")
	}
	if s.close != nil || n != 0 && n <= s.floor {
		return ErrRetired
	}
	return nil
}

func (l *ledger) claim(id record.RequestID, digest record.Hash) (*request, error) {
	r := l.requests[id]
	if r == nil {
		return nil, missing("request claim")
	}
	if r.claim.Record.Payload.(record.RequestClaim).Digest != digest {
		return nil, conflict("request digest")
	}
	return r, nil
}

func (l *ledger) begun(id record.RequestID, digest record.Hash, b record.Binding, begin record.FrameRef, term uint64, v record.UUID) (*request, error) {
	r, err := l.claim(id, digest)
	if err != nil {
		return nil, err
	}
	if r.begin == nil {
		return nil, missing("BEGIN")
	}
	p := r.begin.Record.Payload.(record.Begin)
	if !member(b.Participants, v) || !equal(b, p.Binding) || !sameRef(begin, r.begin.Reference) || term != p.Term {
		return nil, conflict("participant binding/term")
	}
	return r, nil
}

func (l *ledger) affinity(b record.Binding) error {
	copies := l.affinities[b.Affinity]
	if len(copies) != len(b.Participants) {
		return missing("protected affinity fanout")
	}
	for _, id := range b.Participants {
		e := copies[id]
		if e == nil {
			return missing("affinity member")
		}
		p := e.Record.Payload.(record.Affinity)
		if !equal(p.Members, b.Participants) || !equal(p.Authorities, b.Authorities) {
			return conflict("binding affinity vector")
		}
	}
	return nil
}

// fresh applies discovery gates only to newly authored admission. Retained old
// bindings remain immutable and replayable even when a later stream exposes a
// successor's manifest first. Partial fanout is discovery evidence, not absence.
func (l *ledger) fresh(e segment.Entry) error {
	switch p := e.Record.Payload.(type) {
	case record.Affinity:
		for _, id := range p.Members {
			if _, active := l.active[id]; active {
				return fmt.Errorf("%w: affinity joins active closure", ErrTransition)
			}
		}
	case record.RequestBind:
		return l.freshAffinity(p.Binding)
	}
	return nil
}

func (l *ledger) freshAffinity(b record.Binding) error {
	if err := l.affinity(b); err != nil {
		return err
	}
	members := append([]record.UUID(nil), b.Participants...)
	for changed := true; changed; {
		changed = false
		for _, copies := range l.affinities {
			for _, entry := range copies {
				manifest := entry.Record.Payload.(record.Affinity)
				if overlap(members, manifest.Members) {
					for _, id := range manifest.Members {
						if !member(members, id) {
							members = append(members, id)
							changed = true
						}
					}
				}
				break
			}
		}
	}
	if len(members) != len(b.Participants) {
		return missing("binding omits discovered affinity members")
	}
	var selected record.Affinity
	for _, entry := range l.affinities[b.Affinity] {
		selected = entry.Record.Payload.(record.Affinity)
		break
	}
	for ref, copies := range l.affinities {
		for _, entry := range copies {
			manifest := entry.Record.Payload.(record.Affinity)
			if !overlap(members, manifest.Members) {
				break
			}
			if len(copies) != len(manifest.Members) {
				return missing("partial affinity successor remains gated")
			}
			for _, id := range manifest.Members {
				if copies[id] == nil {
					return missing("partial affinity member remains gated")
				}
			}
			if ref != b.Affinity && !l.manifestAncestor(selected.Predecessors, ref, make(map[record.AffinityRef]bool)) {
				return missing("binding uses superseded affinity")
			}
			break
		}
	}
	return nil
}

func (l *ledger) lookup(ref record.FrameRef) (segment.Entry, error) {
	e, ok := l.frames[frameKey(ref)]
	if !ok {
		return e, missing("referenced frame")
	}
	if !sameRef(e.Reference, ref) {
		return segment.Entry{}, conflict("frame hash/identity")
	}
	return e, nil
}

func (l *ledger) refs(refs []record.FrameRef) error {
	for _, ref := range refs {
		if _, err := l.lookup(ref); err != nil {
			return err
		}
	}
	return nil
}

func (l *ledger) vector(vector record.Vector) error {
	for _, p := range vector {
		if p.Sequence == 0 {
			continue
		}
		if _, ok := l.frames[frameKey(record.FrameRef{StreamKind: 1, Position: p})]; !ok {
			return missing("dependency prefix")
		}
	}
	return nil
}

func frameKey(ref record.FrameRef) key {
	k := key{volume: ref.Position.Volume, generation: ref.Position.Generation, configuration: ref.Position.Configuration, epoch: ref.Position.Epoch, sequence: ref.Position.Sequence, kind: ref.StreamKind}
	if ref.Reporter != nil {
		k.reporter = *ref.Reporter
	}
	if ref.ReporterIncarnation != nil {
		k.incarnation = *ref.ReporterIncarnation
	}
	return k
}

func sessionID(principal record.Principal, anchor, nonce record.UUID) fsapi.SessionID {
	return fsapi.SessionID{Principal: principal, Anchor: anchor, Nonce: nonce}
}
func documentID(d record.RequestDocument) record.RequestID {
	if d.Original != nil {
		return d.Original.ID
	}
	if d.Internal != nil {
		return d.Internal.ID
	}
	return record.RequestID{}
}
func authority(b record.Binding, id record.UUID) record.Authority {
	for _, a := range b.Authorities {
		if a.Volume == id {
			return a
		}
	}
	return record.Authority{}
}
func equal(a, b any) bool               { return reflect.DeepEqual(a, b) }
func sameRef(a, b record.FrameRef) bool { return equal(a, b) }
func member(ids []record.UUID, id record.UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func subset(a, b []record.UUID) bool {
	for _, id := range a {
		if !member(b, id) {
			return false
		}
	}
	return true
}
func overlap(a, b []record.UUID) bool {
	for _, id := range a {
		if member(b, id) {
			return true
		}
	}
	return false
}
func hasRef(refs []record.FrameRef, ref record.FrameRef) bool {
	for _, v := range refs {
		if sameRef(v, ref) {
			return true
		}
	}
	return false
}
func affinityRef(p record.Affinity, data []byte) record.AffinityRef {
	return record.AffinityRef{Closure: p.Closure, ManifestVersion: p.ManifestVersion, ManifestSHA256: sha256.Sum256(data[144 : len(data)-4])}
}
func conflict(s string) error { return fmt.Errorf("%w: %s", ErrConflict, s) }
func missing(s string) error  { return fmt.Errorf("%w: %s", ErrEvidence, s) }
func resultCore(a, b record.TerminalResult) bool {
	a.Decision = nil
	a.Committed = nil
	a.Visibility = nil
	b.Decision = nil
	b.Committed = nil
	b.Visibility = nil
	return equal(a, b)
}
func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }
