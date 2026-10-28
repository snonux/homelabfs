package record

import "reflect"

func validateRecord(r Record) error {
	h := r.Header
	local := func(m Mutation) error {
		selected := []ObjectEdit{}
		for _, e := range m.Intent.SelectedMetadata {
			if e.Object.Home == h.Volume {
				selected = append(selected, e)
			}
		}
		if !equalEncoding(selected, m.Redo.Objects) {
			return failure("redo differs from immutable local metadata selection")
		}
		for _, e := range m.Redo.Entries {
			if e.Parent.Home != h.Volume {
				return failure("foreign entry edit")
			}
		}
		for _, e := range m.Redo.Ledgers {
			if e.Object.Home != h.Volume {
				return failure("foreign ledger edit")
			}
		}
		for _, e := range m.Redo.Parents {
			if e.Directory.Home != h.Volume {
				return failure("foreign parent edit")
			}
		}
		return nil
	}
	var reporter *UUID
	switch p := r.Payload.(type) {
	case Mutation:
		if err := local(p); err != nil {
			return err
		}
	case Prepared:
		if p.Binding.Intent.Kind != p.Operation.Intent.Kind {
			return failure("prepare intent mismatch")
		}
		if err := local(p.Operation); err != nil {
			return err
		}
	case Install:
		if p.Prepare != nil && p.Prepare.Participant != h.Volume {
			return failure("INSTALL prepare participant differs from header volume")
		}
		if p.Redo != nil {
			if err := local(Mutation{Intent: p.Binding.Intent, Redo: *p.Redo}); err != nil {
				return err
			}
		}
	case Decision:
		if p.ID.Coordinator != h.Volume || p.ID.Generation != h.Generation || p.ID.Configuration != h.Configuration || p.ID.Epoch != h.Epoch || p.ID.Coordinator != p.Authority.Volume || p.ID.Generation != p.Authority.Generation || p.ID.Configuration != p.Authority.Configuration || p.ID.Epoch != p.Authority.Epoch {
			return failure("DECISION history/authority differs from coordinator identity")
		}
	case VolumeInit:
		if p.Root.Home != h.Volume || p.Authority.Volume != h.Volume {
			return failure("root/authority home")
		}
	case Promise:
		reporter = &p.ReporterIncarnation
	case Accept:
		reporter = &p.ReporterIncarnation
	case TailReport:
		reporter = &p.ReporterIncarnation
	case SegmentOpen:
		encoded, err := marshal(p)
		if err != nil {
			return err
		}
		if p.MaximumBytes < uint64(148+len(encoded)+228) {
			return failure("segment maximum cannot hold this OPEN and SEAL")
		}
		if h.Sequence != p.First {
			return failure("OPEN sequence")
		}
		if p.StreamKind == 2 {
			reporter = p.ReporterIncarnation
		}
		if p.PreviousSeal != nil && !sameStream(p.PreviousSeal.Position, Position{Volume: h.Volume, Generation: h.Generation, Configuration: h.Configuration, Epoch: h.Epoch}) {
			return failure("OPEN predecessor stream")
		}
	case SegmentSeal:
		if !next(p.LastBeforeSeal, h.Sequence) {
			return failure("SEAL sequence")
		}
	}
	if reporter != nil && *reporter != h.Incarnation {
		return failure("reporter/header incarnation")
	}
	// Every directly typed reference whose role names a known owner record must
	// use the owner stream. Generic dependency/evidence lists retain either kind.
	return validateReferenceRoles(reflect.ValueOf(r.Payload))
}
func validateReferenceRoles(v reflect.Value) error {
	if v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return validateReferenceRoles(v.Elem())
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return validateReferenceRoles(v.Elem())
	}
	if v.Type() == reflect.TypeFor[FrameRef]() {
		return nil
	}
	if v.Kind() == reflect.Struct {
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			name := t.Field(i).Name
			required := uint8(0)
			switch name {
			case "Mutation", "Commit", "Claim", "Bind", "Begin", "Binding", "Decision", "Prepare", "Retention", "Retirement", "Fence", "Fences", "Recovery", "TerminalResults", "ClosedSessions", "Installs", "CacheBarriers", "PinsReleased", "CheckpointAccepts", "Selected":
				required = 1
			case "Acceptance", "PriorAcceptances", "Promises", "Reports":
				required = 2
			}
			// TAIL_REPORT.Promises and RECOVERY_SELECT.Reports are reporter evidence;
			// generic dependencies/candidates/pins/replacements may refer to either role.
			if required != 0 {
				if err := checkRole(f, required); err != nil {
					return err
				}
			}

			if err := validateReferenceRoles(f); err != nil {
				return err
			}
		}
	} else if v.Kind() == reflect.Slice && v.Type().Elem().Kind() != reflect.Uint8 {
		for i := 0; i < v.Len(); i++ {
			if err := validateReferenceRoles(v.Index(i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkRole(v reflect.Value, required uint8) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return checkRole(v.Elem(), required)
	}
	if v.Type() == reflect.TypeFor[FrameRef]() {
		if v.Interface().(FrameRef).StreamKind != required {
			return failure("reference stream role")
		}
		return nil
	}
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if err := checkRole(v.Index(i), required); err != nil {
				return err
			}
		}
	}
	return nil
}
