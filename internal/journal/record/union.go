package record

import "reflect"

func argument(kind uint16) any {
	switch kind {
	case 1:
		return Create{}
	case 2:
		return Mkdir{}
	case 3:
		return Symlink{}
	case 4:
		return Link{}
	case 5:
		return Unlink{}
	case 6:
		return Rmdir{}
	case 7:
		return Rename{}
	case 8:
		return Write{}
	case 9:
		return Truncate{}
	case 10:
		return Setattr{}
	default:
		return nil
	}
}
func payload(kind uint16) any {
	switch kind {
	case 1:
		return VolumeInit{}
	case 2:
		return Mutation{}
	case 3:
		return Commit{}
	case 4:
		return Barrier{}
	case 5:
		return HandlePin{}
	case 6:
		return HandleRelease{}
	case 7:
		return Reclaim{}
	case 8:
		return Pin{}
	case 9:
		return OrdinaryResult{}
	case 16:
		return Session{}
	case 17:
		return RequestClaim{}
	case 18:
		return RequestBind{}
	case 19:
		return RequestResult{}
	case 20:
		return SessionRetire{}
	case 21:
		return SessionClose{}
	case 22:
		return AnchorRetire{}
	case 32:
		return Affinity{}
	case 33:
		return Begin{}
	case 34:
		return TxAnchor{}
	case 35:
		return Prepared{}
	case 36:
		return Decision{}
	case 37:
		return Install{}
	case 38:
		return Complete{}
	case 48:
		return Promise{}
	case 49:
		return Accept{}
	case 50:
		return TailReport{}
	case 51:
		return RecoverySelect{}
	case 52:
		return Fence{}
	case 53:
		return Configuration{}
	case 54:
		return LossRecovery{}
	case 55:
		return ViewBarrier{}
	case 64:
		return CheckpointAccept{}
	case 65:
		return Retention{}
	case 66:
		return ReplicaProgress{}
	case 67:
		return SegmentOpen{}
	case 68:
		return SegmentSeal{}
	default:
		return nil
	}
}
func kindOf(v any) uint16 {
	for k := uint16(1); k <= 68; k++ {
		if p := payload(k); p != nil && reflect.TypeOf(p) == reflect.TypeOf(v) {
			return k
		}
	}
	return 0
}
func (w *wire) special(v reflect.Value) bool {
	switch v.Type() {
	case reflect.TypeFor[Intent]():
		w.intent(v)
		return true
	case reflect.TypeFor[Setattr]():
		w.setattr(v)
		return true
	case reflect.TypeFor[RequestDocument]():
		w.document(v)
		return true
	case reflect.TypeFor[Evidence]():
		w.evidence(v)
		return true
	case reflect.TypeFor[Decision]():
		// DECISION has a deliberately provisional TerminalResult. Walk it explicitly
		// to avoid incorrectly requiring the self-referential final decision/visibility.
		fields := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if fields.Field(i).Name == "Result" {
				w.provisional(v.Field(i))
			} else {
				w.value(v.Field(i), fields.Field(i).Tag.Get("wire"))
			}
		}
		if w.err == nil {
			w.err = validate(v.Interface())
		}
		return true
	}
	return false
}
func (w *wire) intent(v reflect.Value) {
	kind := uint16(w.number(uint64(v.Field(0).Uint()), 2))
	arg := argument(kind)
	if arg == nil {
		w.bad("unknown mutation kind")
		return
	}
	if w.decoding {
		v.Field(0).SetUint(uint64(kind))
		p := reflect.New(reflect.TypeOf(arg))
		w.value(p.Elem(), "")
		v.Field(1).Set(p.Elem())
	} else {
		a := v.Field(1)
		if a.IsNil() || a.Elem().Type() != reflect.TypeOf(arg) {
			w.bad("mutation argument type mismatch")
			return
		}
		w.value(a.Elem(), "")
	}
	w.value(v.Field(2), "max=256,sort")
	if w.err == nil {
		w.err = validate(v.Interface())
	}
}
func (w *wire) setattr(v reflect.Value) {
	w.value(v.Field(0), "")
	w.value(v.Field(1), "min=1,max=31")
	if w.err != nil {
		return
	}
	mask := v.Field(1).Uint()
	for i := 0; i < 5; i++ {
		field := v.Field(i + 2)
		if mask&(1<<i) == 0 {
			if !field.IsNil() {
				w.bad("unmasked SETATTR value")
			}
			continue
		}
		if w.decoding {
			field.Set(reflect.New(field.Type().Elem()))
		} else if field.IsNil() {
			w.bad("missing SETATTR value")
			return
		}
		tag := ""
		if i == 0 {
			tag = "max=4095"
		}
		w.value(field.Elem(), tag)
	}
}
func (w *wire) document(v reflect.Value) {
	class := w.number(v.Field(0).Uint(), 1)
	if class != 1 && class != 2 {
		w.bad("request document class")
		return
	}
	if w.decoding {
		v.Field(0).SetUint(class)
	}
	selected := v.Field(int(class))
	other := v.Field(3 - int(class))
	if !other.IsNil() {
		w.bad("request document union")
		return
	}
	if w.decoding {
		selected.Set(reflect.New(selected.Type().Elem()))
	} else if selected.IsNil() {
		w.bad("missing request document")
		return
	}
	w.value(selected.Elem(), "")
}
func (w *wire) provisional(v reflect.Value) {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		w.value(v.Field(i), t.Field(i).Tag.Get("wire"))
	}
	if w.err == nil {
		w.err = validateResult(v.Interface().(TerminalResult), true)
	}
}
func (w *wire) evidence(v reflect.Value) {
	if w.embedded {
		w.bad("recursive evidence field")
		return
	}
	frames := v.Field(0)
	count := w.number(uint64(frames.Len()), 4)
	if w.err != nil {
		return
	}
	if count > 4096 {
		w.bad("evidence frame count")
		return
	}
	if w.decoding {
		if count > uint64((len(w.data)-w.offset)/152) {
			w.bad("evidence count exceeds enclosing bytes")
			return
		}
		if !w.reserve(int(count) * int(frames.Type().Elem().Size())) {
			return
		}
		frames.Set(reflect.MakeSlice(frames.Type(), int(count), int(count)))
	}
	total := 0
	var previous []byte
	for i := 0; i < int(count); i++ {
		w.blob(frames.Index(i), limits{min: 148, max: uint64(MaxBlob - total)})
		if w.err != nil {
			return
		}
		data := frames.Index(i).Bytes()
		total += len(data)
		rec, err := decode(data, true)
		if err != nil {
			w.err = err
			return
		}
		if kindOf(rec.Payload) == 67 || kindOf(rec.Payload) == 68 {
			w.bad("structural evidence frame")
			return
		}
		ref := referenceDecoded(rec, data)
		key, err := sortKey(ref)
		if err != nil {
			w.err = err
			return
		}
		if previous != nil && string(previous) >= string(key) {
			w.bad("evidence duplicate or unsorted stream key")
			return
		}
		previous = key
	}
}
