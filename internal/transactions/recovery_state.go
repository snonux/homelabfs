package transactions

import (
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func (l *ledger) validatePromise(e segment.Entry, p record.Promise) error {
	if e.Reference.StreamKind != 2 || p.Authority.Volume != e.Record.Header.Volume || p.PromisedEpoch < p.Authority.Epoch || p.PromisedTerm <= p.Authority.CoordinatorTerm || !member(p.Authority.Members, p.Reporter) {
		return conflict("promise authority/term floor")
	}
	if err := l.refs(p.PriorAcceptances); err != nil {
		return err
	}
	for _, ref := range p.PriorAcceptances {
		entry, err := l.lookup(ref)
		if err != nil {
			return err
		}
		a, ok := entry.Record.Payload.(record.Accept)
		if !ok || a.Authority.Volume != p.Authority.Volume {
			return conflict("promise accepted ledger")
		}
	}
	for _, tail := range p.Tails {
		if tail.Received.Volume != p.Authority.Volume || tail.Received.Generation != p.Authority.Generation || tail.Received.Configuration != p.Authority.Configuration || tail.Received.Epoch != p.Authority.Epoch {
			return conflict("promise tail lineage")
		}
	}
	return nil
}

func (l *ledger) validateRecoverySelect(e segment.Entry, p record.RecoverySelect) error {
	if p.SlotKind != 2 || p.Binding == nil {
		return conflict("transaction recovery slot")
	}
	r, err := l.claim(p.Binding.Request, p.Binding.RequestDigest)
	if err != nil {
		return err
	}
	if r.begin == nil {
		return missing("recovery BEGIN")
	}
	b := r.begin.Record.Payload.(record.Begin).Binding
	if p.Operation != r.begin.Record.Header.Operation {
		return conflict("recovery stable BEGIN operation identity")
	}
	if !equal(*p.Binding, b) || e.Record.Header.Volume != b.Coordinator || len(p.NewAuthorities) != len(b.Participants) || r.complete != nil {
		return conflict("recovery immutable binding")
	}
	predecessors := b.Authorities
	if r.selection != nil {
		predecessors = r.selection.Record.Payload.(record.RecoverySelect).NewAuthorities
	}
	if err := l.refs(p.Promises); err != nil {
		return err
	}
	if err := l.refs(p.Reports); err != nil {
		return err
	}
	if err := l.refs(p.Fence); err != nil {
		return err
	}
	if err := l.refs(p.Dependencies); err != nil {
		return err
	}
	if len(p.Fence) == 0 {
		return missing("positive closure fence")
	}
	for _, ref := range p.Fence {
		entry, _ := l.lookup(ref)
		fence, ok := entry.Record.Payload.(record.Fence)
		if !ok || fence.RecoveryID != p.RecoveryID || !equal(fence.Authorities, predecessors) || fence.OldOwner != predecessors[0].Owner || fence.OldIncarnation != predecessors[0].OwnerIncarnation {
			return conflict("positive fence closure/lineage")
		}
	}
	var selected *segment.Entry
	if p.Selected != nil {
		entry, err := l.lookup(*p.Selected)
		if err != nil {
			return err
		}
		d, ok := entry.Record.Payload.(record.Decision)
		if !ok || d.ID.Request != b.Request || d.Digest != b.RequestDigest {
			return conflict("selected decision identity")
		}
		selected = &entry
		if p.Operation != entry.Record.Header.Operation {
			return conflict("recovery stable operation identity")
		}
	}
	for i, a := range p.NewAuthorities {
		old := predecessors[i]
		if a.Volume != b.Participants[i] || a.Generation != old.Generation || a.Epoch <= old.Epoch || a.CoordinatorTerm <= old.CoordinatorTerm || a.Configuration == old.Configuration || !equal(a.Members, old.Members) || i > 0 && (a.Owner != p.NewAuthorities[0].Owner || a.Partner != p.NewAuthorities[0].Partner || !equal(a.Members, p.NewAuthorities[0].Members)) {
			return conflict("recovery successor lineage")
		}
		voters := make(map[record.UUID]bool)
		for _, ref := range p.Promises {
			entry, _ := l.lookup(ref)
			promise, ok := entry.Record.Payload.(record.Promise)
			if !ok {
				return conflict("typed recovery promise")
			}
			if promise.Authority.Volume != a.Volume {
				continue
			}
			if promise.RecoveryID != p.RecoveryID || !equal(promise.Authority, old) || promise.PromisedEpoch != a.Epoch || promise.PromisedTerm != a.CoordinatorTerm || !member(old.Members, promise.Reporter) {
				return conflict("promise recovery vector")
			}
			voters[promise.Reporter] = true
		}
		if len(voters) <= len(old.Members)/2 {
			return missing("intersecting promise majority per group")
		}
		reporters := make(map[record.UUID]bool)
		continuous := false
		for _, ref := range p.Reports {
			entry, _ := l.lookup(ref)
			report, ok := entry.Record.Payload.(record.TailReport)
			if !ok {
				return conflict("typed recovery report")
			}
			if entry.Record.Header.Volume != old.Volume {
				continue
			}
			if report.RecoveryID != p.RecoveryID || !voters[report.Reporter] {
				return conflict("recovery report majority identity")
			}
			matched := false
			for _, promiseRef := range report.Promises {
				promiseEntry, err := l.lookup(promiseRef)
				if err != nil {
					return err
				}
				promise, ok := promiseEntry.Record.Payload.(record.Promise)
				if ok && promise.RecoveryID == p.RecoveryID && promise.Authority.Volume == old.Volume && promise.Reporter == report.Reporter && promise.ReporterIncarnation == report.ReporterIncarnation && hasRef(p.Promises, promiseRef) {
					matched = true
				}
			}
			if !matched {
				return conflict("report does not preserve reporter promise")
			}
			for _, tail := range report.Tails {
				if !sameHistory(tail.Received, record.Position{Volume: old.Volume, Generation: old.Generation, Configuration: old.Configuration, Epoch: old.Epoch}) {
					return conflict("report tail lineage")
				}
				if len(tail.Missing) > 0 {
					return missing("reported incomplete recovery tail")
				}
				if report.Reporter == old.Partner && tail.Continuous {
					continuous = true
				}
			}
			reporters[report.Reporter] = true
		}
		if len(reporters) <= len(old.Members)/2 {
			return missing("complete recovery majority reports per group")
		}
		if p.AbsenceAbort && !continuous {
			return missing("common partner continuous accepted tail for absence")
		}
	}
	var highest *segment.Entry
	for _, ref := range p.Reports {
		entry, _ := l.lookup(ref)
		report, ok := entry.Record.Payload.(record.TailReport)
		if !ok || report.RecoveryID != p.RecoveryID {
			return conflict("typed recovery tail report")
		}
		if err := l.refs(report.Candidates); err != nil {
			return err
		}
		for _, candidate := range append(append([]record.FrameRef(nil), report.Candidates...), report.Dependencies...) {
			entry, _ := l.lookup(candidate)
			a, ok := entry.Record.Payload.(record.Accept)
			if !ok {
				continue
			}
			if a.Request == nil || *a.Request != b.Request {
				continue
			}
			decision, err := l.lookup(a.Candidate)
			if err != nil {
				return err
			}
			if _, err := decodeAcceptance(entry.Bytes, decision, l.decisionRequired(&decision)); err != nil {
				return err
			}
			if highest == nil || decision.Record.Payload.(record.Decision).ID.Term > highest.Record.Payload.(record.Decision).ID.Term {
				copy := decision
				highest = &copy
			} else if decision.Record.Payload.(record.Decision).ID.Term == highest.Record.Payload.(record.Decision).ID.Term && !sameRef(decision.Reference, highest.Reference) {
				return conflict("same-term accepted contradiction")
			}
		}
	}
	// Locally known ACCEPT and INSTALL evidence is never forgotten merely because
	// the supplied majority report omitted it. A known chosen value constrains all
	// successor selections; unavailable dependencies block rather than flip it.
	for _, entry := range l.frames {
		a, ok := entry.Record.Payload.(record.Accept)
		if !ok || a.Request == nil || *a.Request != b.Request {
			continue
		}
		decision, err := l.lookup(a.Candidate)
		if err != nil {
			return err
		}
		if highest == nil || a.Term > highest.Record.Payload.(record.Decision).ID.Term {
			copy := decision
			highest = &copy
		}
	}
	if highest != nil && (selected == nil || !sameDecisionValue(highest.Record.Payload.(record.Decision), selected.Record.Payload.(record.Decision)) || selected.Record.Payload.(record.Decision).ID.Term < highest.Record.Payload.(record.Decision).ID.Term) {
		return conflict("selection loses highest accepted candidate")
	}
	if len(r.installs) > 0 && r.decision != nil && (selected == nil || !sameDecisionValue(r.decision.Record.Payload.(record.Decision), selected.Record.Payload.(record.Decision))) {
		return conflict("selection loses installed decision")
	}
	if p.AbsenceAbort && highest != nil {
		return conflict("absence contradicts acceptance")
	}
	if selected != nil {
		d := selected.Record.Payload.(record.Decision)
		if d.Outcome == 1 {
			if len(d.Prepares) != len(b.Participants) {
				return missing("selected complete prepares")
			}
			for _, ref := range d.Prepares {
				if _, err := l.lookup(ref.Prepare); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (l *ledger) validateRecoveredDecision(p record.Decision, r *request, b record.Begin) error {
	entry, err := l.lookup(*p.Recovery)
	if err != nil {
		return err
	}
	selection, ok := entry.Record.Payload.(record.RecoverySelect)
	if !ok || selection.Binding == nil || selection.Binding.Request != p.ID.Request {
		return conflict("decision recovery selection")
	}
	if r.selection == nil || !sameRef(r.selection.Reference, *p.Recovery) {
		return conflict("decision selection identity")
	}
	var coordinator record.Authority
	for _, a := range selection.NewAuthorities {
		if a.Volume == p.ID.Coordinator {
			coordinator = a
		}
	}
	if !equal(coordinator, p.Authority) || p.ID.Term != coordinator.CoordinatorTerm || p.ID.Generation != coordinator.Generation || p.ID.Configuration != coordinator.Configuration || p.ID.Epoch != coordinator.Epoch {
		return conflict("recovered decision term/authority")
	}
	if selection.Selected != nil {
		prior, err := l.lookup(*selection.Selected)
		if err != nil {
			return err
		}
		d := prior.Record.Payload.(record.Decision)
		if !sameDecisionValue(d, p) {
			return conflict("recovered outcome/prepares/result changed")
		}
	} else if !selection.AbsenceAbort || p.Outcome != 2 {
		return conflict("absence selection cannot commit")
	}
	return nil
}

func sameDecisionValue(a, b record.Decision) bool {
	return a.Outcome == b.Outcome && a.Digest == b.Digest && equal(a.Prepares, b.Prepares) && resultCore(a.Result, b.Result)
}
func (l *ledger) validateAccept(e segment.Entry, p record.Accept) error {
	if p.SlotKind != 2 || p.Request == nil {
		return conflict("transaction ACCEPT slot")
	}
	candidate, err := l.lookup(p.Candidate)
	if err != nil {
		return err
	}
	if _, ok := candidate.Record.Payload.(record.Decision); !ok {
		return conflict("ACCEPT candidate type")
	}
	r := l.requests[*p.Request]
	if r != nil && r.selection != nil {
		selection := r.selection.Record.Payload.(record.RecoverySelect)
		newTerm := uint64(0)
		for _, a := range selection.NewAuthorities {
			if a.Volume == p.Authority.Volume {
				newTerm = a.CoordinatorTerm
			}
		}
		if p.Term < newTerm {
			if selection.Selected == nil {
				return conflict("late ACCEPT contradicts selected absence")
			}
			selected, err := l.lookup(*selection.Selected)
			if err != nil {
				return err
			}
			d := selected.Record.Payload.(record.Decision)
			if p.Term > d.ID.Term || !sameDecisionValue(d, candidate.Record.Payload.(record.Decision)) {
				return conflict("late ACCEPT contradicts recovery selection")
			}
		}
	}
	_, err = decodeAcceptance(e.Bytes, candidate, l.decisionRequired(&candidate))
	return err
}
