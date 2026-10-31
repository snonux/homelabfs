package record

import "reflect"

func normalDependencies(payload any) []NormalRef {
	refs := []NormalRef{}
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if !v.IsNil() {
				walk(v.Elem())
			}
			return
		}
		if v.Type() == reflect.TypeFor[NormalRef]() {
			refs = append(refs, v.Interface().(NormalRef))
			return
		}
		if v.Kind() == reflect.Struct {
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		}
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() != reflect.Uint8 {
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(payload))
	if p, ok := payload.(OriginFragment); ok {
		if ref, err := NormalReference(p.GrantBytes); err == nil {
			refs = append(refs, ref)
		}
	}
	return refs
}

func verifyNormalTypedReferences(value any, graph map[NormalRef]NormalRecord) error {
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if v.IsNil() {
				return nil
			}
			return walk(v.Elem())
		}
		switch v.Type() {
		case reflect.TypeFor[SelectedRef]():
			ref := v.Interface().(SelectedRef)
			selected, ok := graph[ref.Certificate].Payload.(SlotSelection)
			if !ok || selected.Slot != ref.Slot {
				return failure("normal selected reference differs from certificate slot")
			}
			return nil
		case reflect.TypeFor[ResultObservation]():
			observation := v.Interface().(ResultObservation)
			if observation.Result != nil {
				result, ok := graph[*observation.Result].Payload.(NormalResult)
				if !ok || result.Grant != observation.Grant {
					return failure("normal terminal observation differs from result grant")
				}
			}
			return nil
		}
		if v.Kind() == reflect.Struct {
			for i := 0; i < v.NumField(); i++ {
				if err := walk(v.Field(i)); err != nil {
					return err
				}
			}
		}
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() != reflect.Uint8 {
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(value))
}

func verifyNormalLinks(ref NormalRef, r NormalRecord, graph map[NormalRef]NormalRecord) error {
	var grantRef NormalRef
	switch p := r.Payload.(type) {
	case OriginChoice:
		grantRef = p.Grant
	case MemoryAcceptance:
		grantRef = p.Grant
	case SlotSelection:
		grantRef = p.Grant
	case NormalResult:
		grantRef = p.Grant
	default:
		return nil
	}
	g, ok := graph[grantRef].Payload.(NormalGrant)
	if !ok || r.Operation != g.Operation || !sameOriginHistory(r.Stream, g.Origin) {
		return failure("normal graph missing grant/operation binding")
	}
	switch p := r.Payload.(type) {
	case OriginChoice:
		if r.Stream != g.Origin || r.Creator != g.OriginLaunch || p.Slot != g.Slot {
			return failure("normal choice origin/slot binding")
		}
		fragment := false
		for _, f := range p.Fragment {
			frame := graph[f]
			if part, ok := frame.Payload.(OriginFragment); ok && frame.Stream == g.Origin {
				original, err := NormalReference(part.GrantBytes)
				fragment = fragment || err == nil && original == p.Grant && f.Sequence < ref.Sequence
			}
		}
		if !fragment {
			return failure("normal choice missing complete origin-local grant fragment")
		}
		if p.Prepared != nil {
			prepared, ok := graph[*p.Prepared].Payload.(MemoryAcceptance)
			if !ok || prepared.Phase != 1 || prepared.Grant != p.Grant {
				return failure("normal choice prepared binding")
			}
		}
	case MemoryAcceptance:
		if p.Slot != g.Slot || p.Origin != g.Origin || p.Recipient != g.Recipient {
			return failure("normal acceptance grant/recipient binding")
		}
		if p.Phase == 2 {
			choice, ok := graph[p.Target].Payload.(OriginChoice)
			if !ok || choice.Grant != p.Grant {
				return failure("normal acceptance choice binding")
			}
		}
		if p.Phase == 3 {
			result, ok := graph[p.Target].Payload.(NormalResult)
			if !ok || result.Grant != p.Grant {
				return failure("normal acceptance result binding")
			}
		}
	case SlotSelection:
		choice, ok := graph[p.Choice].Payload.(OriginChoice)
		acceptance, accepted := graph[p.Acceptance].Payload.(MemoryAcceptance)
		if !ok || !accepted || p.Slot != g.Slot || !equalEncoding(p.Predecessor, g.Predecessor) || choice.Grant != p.Grant || choice.Choice != p.Outcome || acceptance.Phase != 2 || acceptance.Target != p.Choice || acceptance.Grant != p.Grant {
			return failure("normal selection choice/acceptance binding")
		}
		if !contains(p.Volumes, g.Origin.Volume) {
			return failure("normal selection missing affected origin volume")
		}
		for _, volume := range p.Volumes {
			found := false
			for _, member := range g.Authority.Volumes {
				found = found || member.Volume == volume
			}
			if !found {
				return failure("normal selection foreign affected volume")
			}
		}
	case NormalResult:
		selection, ok := graph[p.Selected.Certificate].Payload.(SlotSelection)
		expected := g.Result
		if ok && selection.Outcome == 2 {
			expected = g.NoEffectResult
		}
		if !ok || selection.Grant != p.Grant || selection.Slot != p.Selected.Slot || !equalEncoding(p.Value, expected) || r.Stream != g.Origin {
			return failure("normal result immutable selection/value binding")
		}
	}
	return nil
}

func sameOriginHistory(a, b OriginStream) bool {
	return a.Cluster == b.Cluster && a.Volume == b.Volume && a.Generation == b.Generation && a.Configuration == b.Configuration && a.Epoch == b.Epoch
}

func verifyNormalUniqueness(graph map[NormalRef]NormalRecord) error {
	slots := make(map[OrderedSlot]NormalRef)
	choices := make(map[NormalRef]NormalRef)
	selections := make(map[OrderedSlot]NormalRef)
	results := make(map[NormalRef]NormalRef)
	operations := make(map[UUID]NormalRef)
	claims := make(map[RequestID]NormalRef)
	for ref, frame := range graph {
		switch p := frame.Payload.(type) {
		case NormalGrant:
			if old, ok := operations[p.Operation]; ok && old != ref {
				return failure("normal operation allocated multiple grants")
			}
			operations[p.Operation] = ref
			id := documentID(p.Document)
			if old, ok := claims[id]; ok && old != ref {
				return failure("normal anchored claim allocated multiple grants")
			}
			claims[id] = ref
			if old, ok := slots[p.Slot]; ok && old != ref {
				return failure("normal conflicting grants at one ordered slot")
			}
			slots[p.Slot] = ref
		case OriginChoice:
			if old, ok := choices[p.Grant]; ok && old != ref {
				return failure("normal multiple durable choices for one grant")
			}
			choices[p.Grant] = ref
		case SlotSelection:
			if old, ok := selections[p.Slot]; ok && old != ref {
				return failure("normal conflicting selection certificates")
			}
			selections[p.Slot] = ref
		case NormalResult:
			if old, ok := results[p.Grant]; ok && old != ref {
				return failure("normal multiple stable result records")
			}
			results[p.Grant] = ref
		}
	}
	return nil
}
