package normalpublic

import (
	"fmt"

	"github.com/snonux/homelabfs/internal/journal/record"
)

type sourcePosition struct {
	stream   record.OriginStream
	sequence uint64
}

// VerifyGraph checks exact complete source bytes and all public typed links. It
// returns the root's original binding and immutable result, independently owned.
// It proves neither contiguous durable prefixes nor external retention/barriers.
func VerifyGraph(g Graph, maxFrames int) (Binding, []byte, error) {
	if maxFrames <= 0 || maxFrames > 4096 || len(g.Frames) == 0 || len(g.Frames) > maxFrames || !validRef(g.Root, ResultKind) {
		return Binding{}, nil, fmt.Errorf("public graph bounds/root")
	}
	frames := make(map[Ref]Frame, len(g.Frames))
	positions := make(map[sourcePosition]Ref, len(g.Frames))
	total := 0
	if len(g.NormalFrames)+len(g.Frames) > maxFrames || (g.NormalRecovery == nil) != (len(g.NormalFrames) == 0) {
		return Binding{}, nil, fmt.Errorf("public normal source graph shape")
	}
	normalFrames := make(map[record.NormalRef][]byte, len(g.NormalFrames))
	for _, data := range g.NormalFrames {
		if len(data) > MaxFrame-total {
			return Binding{}, nil, fmt.Errorf("public normal source byte bound")
		}
		total += len(data)
		ref, err := record.NormalReference(data)
		if err != nil {
			return Binding{}, nil, err
		}
		if _, ok := normalFrames[ref]; ok {
			return Binding{}, nil, fmt.Errorf("public duplicate normal source")
		}
		normalFrames[ref] = data
	}
	if g.NormalRecovery != nil {
		if err := record.VerifyNormalGraph(*g.NormalRecovery, normalFrames); err != nil {
			return Binding{}, nil, err
		}
		if len(g.NormalRecovery.Graph) != len(normalFrames) {
			return Binding{}, nil, fmt.Errorf("public normal graph extraneous bytes")
		}
	}
	for _, data := range g.Frames {
		if len(data) > MaxFrame-total {
			return Binding{}, nil, fmt.Errorf("public graph byte bound")
		}
		total += len(data)
		ref, err := Reference(data)
		if err != nil {
			return Binding{}, nil, err
		}
		if _, exists := frames[ref]; exists {
			return Binding{}, nil, fmt.Errorf("public duplicate source")
		}
		position := sourcePosition{ref.Stream, ref.Sequence}
		if _, exists := positions[position]; exists {
			return Binding{}, nil, fmt.Errorf("public conflicting source position")
		}
		positions[position] = ref
		frame, err := Decode(data)
		if err != nil {
			return Binding{}, nil, err
		}
		frames[ref] = frame
	}
	visited := make(map[Ref]uint8, len(frames))
	if err := walkGraph(g.Root, frames, visited); err != nil {
		return Binding{}, nil, err
	}
	if len(visited) != len(frames) {
		return Binding{}, nil, fmt.Errorf("public graph extraneous sources")
	}
	operations := make(map[record.UUID]Ref)
	claims := make(map[string]Ref)
	slots := make(map[record.OrderedSlot]Ref)
	choices := make(map[Ref]Ref)
	results := make(map[Ref]Ref)
	selections := make(map[Ref]Ref)
	for ref, frame := range frames {
		if err := verifyLinks(ref, frame, frames); err != nil {
			return Binding{}, nil, err
		}
		if frame.Binding != nil {
			b := frame.Binding
			key := fmt.Sprintf("%s/%d", anchorKey(b.Claim.Anchor), b.Claim.Number)
			if _, ok := operations[b.Operation]; ok {
				return Binding{}, nil, fmt.Errorf("public operation has multiple originals")
			}
			if _, ok := claims[key]; ok {
				return Binding{}, nil, fmt.Errorf("public claim has multiple originals")
			}
			operations[b.Operation] = ref
			claims[key] = ref
			for _, source := range b.Sources {
				if _, ok := normalFrames[source]; !ok {
					return Binding{}, nil, fmt.Errorf("public missing normal source bytes")
				}
			}
		}
		if frame.Choice != nil {
			if _, ok := choices[frame.Choice.Binding]; ok {
				return Binding{}, nil, fmt.Errorf("public conflicting origin choices")
			}
			choices[frame.Choice.Binding] = ref
		}
		if frame.Result != nil {
			if _, ok := results[frame.Result.Selection]; ok {
				return Binding{}, nil, fmt.Errorf("public conflicting results")
			}
			results[frame.Result.Selection] = ref
		}
		if frame.Selection != nil {
			if _, ok := selections[frame.Selection.Choice]; ok {
				return Binding{}, nil, fmt.Errorf("public choice selected more than once")
			}
			selections[frame.Selection.Choice] = ref
			slot := frame.Selection.Slot
			if _, ok := slots[slot]; ok {
				return Binding{}, nil, fmt.Errorf("public conflicting ordered selection")
			}
			slots[slot] = ref
		}
	}
	result := frames[g.Root].Result
	selection := frames[result.Selection].Selection
	choice := frames[selection.Choice].Choice
	return *frames[choice.Binding].Binding, append([]byte(nil), result.Bytes...), nil
}

func walkGraph(ref Ref, frames map[Ref]Frame, visited map[Ref]uint8) error {
	if visited[ref] == 1 {
		return fmt.Errorf("public cyclic source graph")
	}
	if visited[ref] == 2 {
		return nil
	}
	f, ok := frames[ref]
	if !ok {
		return fmt.Errorf("public missing exact source bytes")
	}
	visited[ref] = 1
	for _, dependency := range dependencies(f) {
		if err := walkGraph(dependency, frames, visited); err != nil {
			return err
		}
	}
	visited[ref] = 2
	return nil
}

func dependencies(f Frame) []Ref {
	switch f.Kind {
	case BindingKind:
		return f.Binding.Dependencies
	case ChoiceKind:
		return []Ref{f.Choice.Binding}
	case SelectionKind:
		refs := []Ref{f.Selection.Choice}
		if f.Selection.Predecessor != nil {
			refs = append(refs, *f.Selection.Predecessor)
		}
		return refs
	case ResultKind:
		return []Ref{f.Result.Selection}
	}
	return nil
}

func original(f Frame, frames map[Ref]Frame) *Binding {
	switch f.Kind {
	case BindingKind:
		return f.Binding
	case ChoiceKind:
		return frames[f.Choice.Binding].Binding
	case SelectionKind:
		return original(frames[f.Selection.Choice], frames)
	case ResultKind:
		return original(frames[f.Result.Selection], frames)
	}
	return nil
}

func verifyLinks(ref Ref, f Frame, frames map[Ref]Frame) error {
	b := original(f, frames)
	if b == nil || f.Operation != b.Operation || !sameHistory(f.Stream, b.Origin) {
		return fmt.Errorf("public source operation/history mismatch")
	}
	if f.Kind != SelectionKind && (f.Stream != b.Origin || f.Creator != b.OriginLaunch) {
		return fmt.Errorf("public source original O/creator mismatch")
	}
	switch f.Kind {
	case ChoiceKind:
		if f.Choice.Binding.Sequence >= ref.Sequence {
			return fmt.Errorf("public choice precedes binding")
		}
	case SelectionKind:
		s := f.Selection
		if s.Slot.Authority != b.Scope.Authority || s.Slot.Sequencer != b.Authority.Sequencer || f.Creator != b.Authority.Sequencer {
			return fmt.Errorf("public selection authority/closure mismatch")
		}
		if s.Predecessor != nil {
			previous := frames[*s.Predecessor].Selection
			if previous == nil || previous.Slot.Authority != s.Slot.Authority || previous.Slot.Sequencer != s.Slot.Sequencer || previous.Slot.Serial != s.Slot.Serial-1 {
				return fmt.Errorf("public selection skipped predecessor")
			}
		}
	case ResultKind:
		selection := frames[f.Result.Selection].Selection
		choice := frames[selection.Choice]
		if choice.Sequence >= f.Sequence {
			return fmt.Errorf("public result precedes choice")
		}
	}
	return nil
}

func sameHistory(a, b record.OriginStream) bool {
	return a.Cluster == b.Cluster && a.Volume == b.Volume && a.Generation == b.Generation && a.Configuration == b.Configuration && a.Epoch == b.Epoch
}
