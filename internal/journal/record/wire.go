package record

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Limits are hard representation bounds, independent of admission profiles.
const (
	MaxFrame   = 8_388_608
	MaxBlob    = 4_194_304
	HeaderSize = 144
	MaxSegment = 268_435_456
)

// ErrorKind classifies bytes that must be retained, never silently skipped.
type ErrorKind uint8

const (
	// Torn means a potentially incomplete final frame; segment context decides recovery.
	Torn ErrorKind = iota + 1
	// Corrupt means present bytes violate the known representation.
	Corrupt
	// Unsupported means a version or record kind requires an explicit upgrader.
	Unsupported
)

// Error provides a stable category and contextual diagnostic.
type Error struct {
	Kind   ErrorKind
	Detail string
}

// Error returns the diagnostic.
func (e *Error) Error() string { return fmt.Sprintf("journal format %d: %s", e.Kind, e.Detail) }

type wire struct {
	data           []byte
	offset, budget int
	decoding       bool
	embedded       bool
	err            error
}
type limits struct {
	min, max           uint64
	sorted, name, text bool
	enum               bool
}

func failure(detail string) error { return &Error{Corrupt, detail} }
func rules(tag string, defaultMax uint64) limits {
	l := limits{max: defaultMax}
	for _, part := range strings.Split(tag, ",") {
		switch {
		case part == "sort":
			l.sorted = true
		case part == "name":
			l.name = true
			l.min = 1
			l.max = 255
		case part == "text":
			l.text = true
		case strings.HasPrefix(part, "min="):
			l.min, _ = strconv.ParseUint(part[4:], 10, 64)
		case strings.HasPrefix(part, "max="):
			l.max, _ = strconv.ParseUint(part[4:], 10, 64)
		case strings.HasPrefix(part, "enum="):
			fields := strings.Split(part[5:], ":")
			l.min, _ = strconv.ParseUint(fields[0], 10, 64)
			l.max, _ = strconv.ParseUint(fields[1], 10, 64)
			l.enum = true
		}
	}
	return l
}
func (w *wire) bad(detail string) {
	if w.err == nil {
		w.err = failure(detail)
	}
}
func (w *wire) reserve(n int) bool {
	if n < 0 || n > w.budget {
		w.bad("nested allocation budget")
		return false
	}
	w.budget -= n
	return true
}
func (w *wire) raw(b []byte) {
	if w.err != nil {
		return
	}
	if w.decoding {
		if len(b) > len(w.data)-w.offset {
			w.bad("truncated payload field")
			return
		}
		copy(b, w.data[w.offset:w.offset+len(b)])
		w.offset += len(b)
	} else {
		if len(b) > MaxFrame-len(w.data) {
			w.bad("encoded frame too large")
			return
		}
		w.data = append(w.data, b...)
	}
}
func (w *wire) number(value uint64, size int) uint64 {
	var b [8]byte
	if !w.decoding {
		binary.BigEndian.PutUint64(b[:], value)
	}
	w.raw(b[8-size:])
	return binary.BigEndian.Uint64(b[:])
}
func (w *wire) blob(v reflect.Value, l limits) {
	n := uint64(v.Len())
	n = w.number(n, 4)
	if w.err != nil {
		return
	}
	if n < l.min || n > l.max {
		w.bad("blob length")
		return
	}
	if w.decoding {
		if n > uint64(len(w.data)-w.offset) || !w.reserve(int(n)) {
			w.bad("blob exceeds enclosing bytes")
			return
		}
		b := w.data[w.offset : w.offset+int(n)]
		w.offset += int(n)
		if v.Kind() == reflect.String {
			v.SetString(string(b))
		} else {
			v.SetBytes(bytes.Clone(b))
		}
	} else {
		if v.Kind() == reflect.String {
			w.raw([]byte(v.String()))
		} else {
			w.raw(v.Bytes())
		}
	}
	if l.name && !validName(stringValue(v)) {
		w.bad("name component")
	}
	if l.text && bytes.IndexByte([]byte(stringValue(v)), 0) >= 0 {
		w.bad("NUL in symlink text")
	}
}
func stringValue(v reflect.Value) string {
	if v.Kind() == reflect.String {
		return v.String()
	}
	return string(v.Bytes())
}
func validName(s string) bool {
	return len(s) >= 1 && len(s) <= 255 && s != "." && s != ".." && !strings.ContainsAny(s, "/\x00")
}
func (w *wire) value(v reflect.Value, tag string) {
	if w.err != nil {
		return
	}
	if w.special(v) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		present := uint64(0)
		if !v.IsNil() {
			present = 1
		}
		present = w.number(present, 1)
		if present > 1 {
			w.bad("optional presence")
			return
		}
		if present == 0 {
			return
		}
		if w.decoding {
			if !w.reserve(int(v.Type().Elem().Size())) {
				return
			}
			v.Set(reflect.New(v.Type().Elem()))
		}
		w.value(v.Elem(), tag)
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			tag := t.Field(i).Tag.Get("wire")
			if t == reflect.TypeFor[Principal]() && t.Field(i).Name == "Subject" {
				tag = "min=1,max=1024"
			}
			w.value(v.Field(i), tag)
		}
		if w.err == nil {
			w.err = validate(v.Interface())
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			w.value(v.Index(i), "")
		}
		if v.Type() == reflect.TypeFor[UUID]() && v.Interface().(UUID) == (UUID{}) {
			w.bad("zero UUID")
		}
	case reflect.String:
		w.blob(v, rules(tag, MaxBlob))
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			w.blob(v, rules(tag, MaxBlob))
			return
		}
		l := rules(tag, 4096)
		if v.Type() == reflect.TypeFor[Vector]() {
			l.max = 64
			l.sorted = true
		}
		n := w.number(uint64(v.Len()), 4)
		if w.err != nil {
			return
		}
		if n < l.min || n > l.max {
			w.bad("array count")
			return
		}
		if w.decoding {
			min := minimum(v.Type().Elem())
			if n > uint64((len(w.data)-w.offset)/min) {
				w.bad("array exceeds enclosing bytes")
				return
			}
			size := uint64(v.Type().Elem().Size())
			if size > 0 && n > uint64(w.budget)/size {
				w.bad("array allocation budget")
				return
			}
			if !w.reserve(int(n * size)) {
				return
			}
			v.Set(reflect.MakeSlice(v.Type(), int(n), int(n)))
		}
		var previous []byte
		pathBytes := 0
		for i := 0; i < int(n); i++ {
			elementTag := ""
			if v.Type().Elem().Kind() == reflect.String {
				maximum := min(255, 16384-pathBytes)
				elementTag = "name,max=" + strconv.Itoa(maximum)
			}
			w.value(v.Index(i), elementTag)
			if v.Type().Elem().Kind() == reflect.String {
				pathBytes += v.Index(i).Len()
			}
			if w.err != nil {
				return
			}
			if l.sorted {
				key, err := sortKey(v.Index(i).Interface())
				if v.Type() == reflect.TypeFor[Vector]() {
					key, err = marshal(v.Index(i).Interface().(Position).Volume)
				}
				if err != nil {
					w.err = err
					return
				}
				if i > 0 && bytes.Compare(previous, key) >= 0 {
					w.bad("duplicate or unsorted set key")
					return
				}
				previous = key
			}
		}
	case reflect.Bool:
		n := uint64(0)
		if v.Bool() {
			n = 1
		}
		n = w.number(n, 1)
		if n > 1 {
			w.bad("boolean")
			return
		}
		if w.decoding {
			v.SetBool(n == 1)
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := w.number(v.Uint(), int(v.Type().Size()))
		l := rules(tag, math.MaxUint64)
		if n < l.min || n > l.max {
			if l.enum {
				w.bad("unknown enum")
			} else {
				w.bad("numeric bound")
			}
			return
		}
		if w.decoding {
			v.SetUint(n)
		}
	case reflect.Int64:
		n := w.number(uint64(v.Int()), 8)
		if w.decoding {
			v.SetInt(int64(n))
		}
	default:
		w.bad("unsupported schema type " + v.Type().String())
	}
}
func minimum(t reflect.Type) int {
	switch t {
	case reflect.TypeFor[Intent]():
		return 2 + 4
	case reflect.TypeFor[RequestDocument]():
		return 1 + 2
	case reflect.TypeFor[Setattr]():
		return 32 + 2 + 2
	}
	switch t.Kind() {
	case reflect.Pointer:
		return 1
	case reflect.String, reflect.Slice:
		return 4
	case reflect.Struct:
		n := 0
		for i := 0; i < t.NumField(); i++ {
			n += minimum(t.Field(i).Type)
		}
		return n
	case reflect.Array:
		return t.Len() * minimum(t.Elem())
	default:
		return int(t.Size())
	}
}
func marshal(v any) ([]byte, error) {
	w := wire{budget: MaxFrame * 8}
	w.value(reflect.ValueOf(v), "")
	return w.data, w.err
}
func unmarshal(data []byte, target any, embedded bool) error {
	w := wire{data: data, budget: MaxFrame * 8, decoding: true, embedded: embedded}
	w.value(reflect.ValueOf(target).Elem(), "")
	if w.err == nil && w.offset != len(data) {
		w.bad("trailing payload bytes")
	}
	return w.err
}
func sortKey(value any) ([]byte, error) {
	switch v := value.(type) {
	case Position:
		return marshal(v)
	case FrameRef:
		v.SHA256 = Hash{}
		return marshal(v)
	case ObjectEdit:
		return marshal(v.Object)
	case EntryEdit:
		return marshal(struct {
			Parent ObjectID
			Name   string
		}{v.Parent, v.Name})
	case LedgerEdit:
		return marshal(v.Object)
	case ParentEdit:
		return marshal(v.Directory)
	case Reference:
		return marshal(struct {
			Parent ObjectID
			Name   string
		}{v.Parent, v.Name})
	case Authority:
		return marshal(v.Volume)
	case Authorization:
		return marshal(struct {
			Object ObjectID
			View   UUID
		}{v.Object, v.View})
	case PrepareRef:
		return marshal(v.Participant)
	case DecisionRef:
		return sortKey(v.Decision)
	case Tail:
		return marshal(v.Received)
	case Alias:
		return marshal(struct {
			View   UUID
			Parent *ObjectID
			Name   string
			Object *ObjectID
		}{v.View, v.Parent, v.Name, v.Object})
	case Coverage:
		return marshal(struct{ Replica, Incarnation UUID }{v.Replica, v.Incarnation})
	case Artifact:
		return marshal(struct {
			Kind uint16
			ID   UUID
		}{v.Kind, v.ID})
	case AffinityRef:
		return marshal(struct {
			Closure UUID
			Version uint64
		}{v.Closure, v.ManifestVersion})
	default:
		return marshal(value)
	}
}
