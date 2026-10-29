package transactions

import (
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func (l *ledger) validateDecision(e segment.Entry, p record.Decision) error {
	r, err := l.claim(p.ID.Request, p.Digest)
	if err != nil {
		return err
	}
	if r.begin == nil {
		return missing("BEGIN")
	}
	b := r.begin.Record.Payload.(record.Begin)
	if e.Record.Header.Operation != r.begin.Record.Header.Operation {
		return conflict("decision stable operation identity")
	}
	if r.candidates[p.ID.Term] != nil || !sameRef(p.Begin, r.begin.Reference) || p.ID.Coordinator != b.Binding.Coordinator {
		return conflict("decision slot/authority")
	}
	if p.Recovery == nil {
		if p.ID.Term != b.Term || !equal(p.Authority, authority(b.Binding, p.ID.Coordinator)) {
			return conflict("normal decision term/authority")
		}
	} else if err := l.validateRecoveredDecision(p, r, b); err != nil {
		return err
	}
	if p.Outcome == 1 && len(r.anchors) != len(b.Binding.Participants) {
		return missing("participant anchors")
	}
	if p.Outcome == 1 && len(p.Prepares) != len(b.Binding.Participants) {
		return missing("complete prepare vector")
	}
	for _, pr := range p.Prepares {
		prepared := r.prepares[pr.Participant]
		if prepared == nil {
			return missing("participant prepare")
		}
		if !sameRef(pr.Prepare, prepared.Reference) || pr.RedoSHA256 != prepared.Record.Payload.(record.Prepared).RedoSHA256 {
			return conflict("prepare evidence")
		}
	}
	if p.Outcome == 1 {
		if !resolvedResult(p.Result, b.Binding.Intent) {
			return conflict("decision result differs from resolved intent")
		}
		for i, id := range b.Binding.Participants {
			if p.Prepares[i].Participant != id {
				return conflict("prepare participant coverage")
			}
		}
	}
	return nil
}

func participantCreator(e segment.Entry, b record.Binding) bool {
	a := authority(b, e.Record.Header.Volume)
	h := e.Record.Header
	return h.Volume == a.Volume && h.Generation == a.Generation && h.Configuration == a.Configuration && h.Epoch == a.Epoch && h.Incarnation == a.OwnerIncarnation
}

func (l *ledger) validateInstall(e segment.Entry, p record.Install) error {
	r, err := l.claim(p.Request, p.Digest)
	if err != nil {
		return err
	}
	if r.decision == nil {
		return missing("coordinator decision")
	}
	if _, err := l.lookup(p.Decision.Decision); err != nil {
		return err
	}
	d := r.decision.Record.Payload.(record.Decision)
	b := r.begin.Record.Payload.(record.Begin).Binding
	v := e.Record.Header.Volume
	if prior := r.installs[v]; prior != nil {
		previous := prior.Record.Payload.(record.Install)
		if previous.Decision.ID.Term >= p.Decision.ID.Term || previous.Outcome != p.Outcome {
			return conflict("INSTALL changes selected value")
		}
	}
	if !member(b.Participants, v) || !equal(p.Binding, b) || !sameRef(p.Decision.Decision, r.decision.Reference) || p.Decision.ID != d.ID || p.Outcome != d.Outcome {
		return conflict("INSTALL decision/binding")
	}
	if len(p.Acceptance) == 0 {
		return missing("decision ACCEPT")
	}
	if err := l.acceptance(p, r.decision); err != nil {
		return err
	}
	if err := l.refs(p.Dependencies); err != nil {
		return err
	}
	if p.Outcome == 1 {
		prepared := r.prepares[v]
		if prepared == nil || p.Prepare == nil {
			return missing("local prepare")
		}
		pr := prepared.Record.Payload.(record.Prepared)
		if !sameRef(p.Prepare.Prepare, prepared.Reference) || p.Prepare.RedoSHA256 != pr.RedoSHA256 || !equal(*p.Redo, pr.Operation.Redo) || p.Applied != prepared.Reference.Position {
			return conflict("INSTALL exact redo/applied position")
		}
	}
	return nil
}

func (l *ledger) validateBarrier(e segment.Entry, p record.ViewBarrier) error {
	if p.Incarnation != e.Record.Header.Incarnation || p.ServingGeneration != (record.UUID{}) && !p.ServingGeneration.Valid() {
		return conflict("barrier incarnation")
	}
	for _, r := range l.requests {
		if r.begin == nil || r.decision == nil || r.complete != nil {
			continue
		}
		b := r.begin.Record.Payload.(record.Begin).Binding
		if b.Affinity != p.Affinity {
			continue
		}
		if r.decision.Record.Payload.(record.Decision).Outcome != 1 || len(r.installs) != len(b.Participants) {
			return missing("installed closure for barrier")
		}
		if p.OldVisibility.Generation != l.visibility[p.Affinity.Closure] || p.NewVisibility.Generation != p.OldVisibility.Generation+1 {
			return conflict("visibility generation")
		}
		return l.refs(p.Dependencies)
	}
	return missing("active committed closure")
}

func (l *ledger) validateComplete(e segment.Entry, p record.Complete) error {
	r, err := l.claim(p.Request, p.Digest)
	if err != nil {
		return err
	}
	if r.decision == nil {
		return missing("decision")
	}
	d := r.decision.Record.Payload.(record.Decision)
	b := r.begin.Record.Payload.(record.Begin).Binding
	if r.complete != nil || e.Record.Header.Volume != b.Coordinator || !sameRef(p.Decision.Decision, r.decision.Reference) || p.Decision.ID != d.ID || p.Result.Status != d.Outcome || !resultCore(p.Result, d.Result) || p.Visibility.Affinity != b.Affinity {
		return conflict("COMPLETE decision/result")
	}
	if len(r.installs) != len(b.Participants) || len(p.Installs) != len(b.Participants) || len(p.Installed) != len(b.Participants) {
		return missing("complete install vector")
	}
	for i, id := range b.Participants {
		entry := r.installs[id]
		install := entry.Record.Payload.(record.Install)
		if install.Decision.ID != p.Decision.ID || !sameRef(install.Decision.Decision, p.Decision.Decision) {
			return missing("selected ballot participant installs")
		}
		if !hasRef(p.Installs, entry.Reference) || p.Installed[i] != entry.Reference.Position {
			return conflict("COMPLETE participant positions")
		}
	}
	if d.Outcome == 1 {
		if p.Visibility.Generation != l.visibility[b.Affinity.Closure]+1 || len(p.CacheBarriers) == 0 {
			return missing("visibility barrier")
		}
		for _, ref := range p.CacheBarriers {
			entry, err := l.lookup(ref)
			if err != nil {
				return err
			}
			barrier, ok := entry.Record.Payload.(record.ViewBarrier)
			if !ok || barrier.Affinity != b.Affinity || barrier.NewVisibility != p.Visibility {
				return conflict("cache barrier evidence")
			}
		}
	} else if p.Visibility.Generation != l.visibility[b.Affinity.Closure] || len(p.CacheBarriers) != 0 {
		return conflict("abort changes visibility")
	}
	return nil
}
