package sim

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

const (
	concurrentTraceBytes    = 128 << 20
	concurrentCapturedBytes = 64 << 20
	concurrentViewBytes     = 8 << 20
	concurrentFileBytes     = 8 << 20
)

// EncodeConcurrentTrace validates bounded evidence before JSON encoding. In
// particular it never silently replaces invalid UTF-8 namespace/principal bytes.
// Binary payloads/results use JSON's lossless base64 representation. Up to 64 MiB
// of captured binary data fits the separate 128 MiB encoded-evidence budget.
func EncodeConcurrentTrace(w io.Writer, trace ConcurrentTrace) error {
	if _, err := validateConcurrentTrace(trace); err != nil {
		return fmt.Errorf("encode concurrent trace: %w", err)
	}
	return json.NewEncoder(&concurrentWriter{destination: w, remaining: concurrentTraceBytes}).Encode(trace)
}

type concurrentWriter struct {
	destination io.Writer
	remaining   int
}

func (w *concurrentWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("concurrent encoded trace budget")
	}
	n, err := w.destination.Write(p)
	w.remaining -= n
	if err == nil && n != len(p) {
		return n, io.ErrShortWrite
	}
	return n, err
}

func concurrentUTF8(t ConcurrentTrace) bool {
	valid := func(values ...string) bool {
		for _, value := range values {
			if !utf8.ValidString(value) {
				return false
			}
		}
		return true
	}
	path := func(p ConcurrentPath) bool { return valid(p.Name) }
	identity := func(id ConcurrentIdentity) bool { return valid(id.Principal) }
	view := func(v ConcurrentView) bool {
		for _, n := range v.Names {
			if !path(n.Path) {
				return false
			}
		}
		for _, id := range v.Cut {
			if !identity(id) {
				return false
			}
		}
		return true
	}
	if !view(t.Baseline) {
		return false
	}
	for _, c := range t.Calls {
		if !valid(c.Attempt, c.Intent.Kind) || !identity(c.Identity) || !path(c.Intent.Source) || !path(c.Intent.Destination) || c.Reply != nil && !valid(c.Reply.Status, c.Reply.Code) {
			return false
		}
	}
	for _, r := range t.Reads {
		if !valid(r.ID, r.Reason) || !view(r.View) {
			return false
		}
		if p := r.Probe; p != nil {
			if !valid(p.Kind, p.Code) || !path(p.Path) {
				return false
			}
			for _, name := range p.Names {
				if !path(name.Path) {
					return false
				}
			}
			for _, id := range p.Cut {
				if !identity(id) {
					return false
				}
			}
		}
	}
	for _, f := range t.Faults {
		if !valid(f.Kind, f.Detail) {
			return false
		}
	}
	return true
}

// encoding/json accepts unpaired surrogate escapes by replacing them with U+FFFD.
// Reject those spellings before decode so two distinct raw intents cannot collapse.
func concurrentJSONUnicode(raw []byte) bool {
	for index := 0; index < len(raw); index++ {
		if raw[index] != '"' {
			continue
		}
		index++
		for index < len(raw) && raw[index] != '"' {
			if raw[index] != '\\' {
				index++
				continue
			}
			index++
			if index >= len(raw) {
				return false
			}
			if raw[index] != 'u' {
				index++
				continue
			}
			if index+4 >= len(raw) {
				return false
			}
			value, err := strconv.ParseUint(string(raw[index+1:index+5]), 16, 16)
			if err != nil {
				return false
			}
			index += 5
			if value >= 0xDC00 && value <= 0xDFFF {
				return false
			}
			if value >= 0xD800 && value <= 0xDBFF {
				if index+6 > len(raw) || raw[index] != '\\' || raw[index+1] != 'u' {
					return false
				}
				low, err := strconv.ParseUint(string(raw[index+2:index+6]), 16, 16)
				if err != nil || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				index += 6
			}
		}
	}
	return true
}

func concurrentCaptureBudget(t ConcurrentTrace) bool {
	total := 0
	add := func(size int) bool {
		if size > concurrentCapturedBytes-total {
			return false
		}
		total += size
		return true
	}
	for _, o := range t.Baseline.Objects {
		if !add(len(o.Data)) {
			return false
		}
	}
	for _, c := range t.Calls {
		if !add(len(c.Intent.Data)) {
			return false
		}
		if c.Reply != nil && !add(len(c.Reply.StableResult)) {
			return false
		}
	}
	for _, r := range t.Reads {
		for _, o := range r.View.Objects {
			if !add(len(o.Data)) {
				return false
			}
		}
		if r.Probe != nil && (!add(len(r.Probe.Data)) || !add(len(r.Probe.Observed.Data))) {
			return false
		}
	}
	return true
}
