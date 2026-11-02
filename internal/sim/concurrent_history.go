package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// ConcurrentVerdict distinguishes a counterexample from an incomplete search.
type ConcurrentVerdict string

const (
	// ConcurrentPass means one complete legal linearization explains every observation.
	ConcurrentPass ConcurrentVerdict = "PASS"
	// ConcurrentFail means exhaustive search found no legal linearization.
	ConcurrentFail ConcurrentVerdict = "FAIL"
	// ConcurrentInconclusive means invalid/unsupported evidence, cancellation or a resource limit.
	ConcurrentInconclusive ConcurrentVerdict = "INCONCLUSIVE"
)

// ConcurrentIdentity is either an ordinary operation UUID or an anchored request.
// Principal belongs to identity; receiving origin and wait deadlines do not.
type ConcurrentIdentity struct {
	Principal       string
	Operation       fsapi.UUID
	Anchor, Session fsapi.UUID
	Number          uint64
}

// ConcurrentOrigin identifies a receiving daemon lifetime, including outside origins.
type ConcurrentOrigin struct{ Node, Incarnation fsapi.UUID }

// ConcurrentPath names a canonical root-relative path in a fixed volume view.
// Root is ".". Paths never follow symlinks in this deliberately bounded oracle.
type ConcurrentPath struct {
	Volume fsapi.UUID
	Name   string
}

// ConcurrentObject is the independently checked stable home/Unix projection.
// Pins represent predeclared nonexpiring open-handle/snapshot obligations.
// Revisions, timestamps and kernel inode numbers are outside this projection.
type ConcurrentObject struct {
	ID       fsapi.ObjectID
	Kind     fsapi.Kind
	Mode     uint16
	UID, GID uint32
	Nlink    uint64
	Size     int64
	Data     []byte
	Parent   fsapi.ObjectID
	Pins     uint64
}

// ConcurrentName binds an observed namespace entry to its immutable home object.
type ConcurrentName struct {
	Path   ConcurrentPath
	Object fsapi.ObjectID
}

// ConcurrentView is one atomic external observation across the complete closure.
// Cut contains successful logical effect identities since the declared baseline,
// in order, not origin-journal sequence maxima. It is evidence, not expected truth.
type ConcurrentView struct {
	Generation fsapi.UUID
	CutKnown   bool
	Cut        []ConcurrentIdentity
	Names      []ConcurrentName
	Objects    []ConcurrentObject
}

// ConcurrentIntent retains original semantic arguments before execution. Kind is
// create, mkdir, append, write, truncate, chmod, rename, link, unlink or barrier.
// Transaction explicitly selects the application cross-volume rename/link API.
// Expected is an optional original source-object precondition, not resolved redo.
type ConcurrentIntent struct {
	Kind                   string
	Source, Destination    ConcurrentPath
	Data                   []byte
	Offset, Size           int64
	Mode, Umask            uint16
	Cred                   fsapi.Cred
	Transaction, NoReplace bool
	Expected               *fsapi.ObjectID
}

// ConcurrentResult retains a client-visible result. Status is committed,
// rejected, conflict, aborted, pending or indeterminate. Code is the semantic
// result (ok, permission, not_found, exists, cross_volume, stale, type, cycle,
// not_empty or cancelled). Origin is the original durable origin, never the retry
// receiver. Object/Count/Offset are immutable terminal result fields.
// StableResult holds exact canonical application result/decision bytes and is
// required for anchored terminal replies; adapters separately validate authority.
type ConcurrentResult struct {
	Status, Code  string
	Origin        ConcurrentOrigin
	Object        fsapi.ObjectID
	Count, Offset int64
	StableResult  []byte
}

// ConcurrentCall is one attempt, not one logical effect. Attempt is unique;
// Identity may repeat through other origins. Invoked/Returned/ClaimedAt are
// globally ordered observer ordinals. Returned bounds only terminal responses.
// WaitUntil and pending/indeterminate replies NEVER bound operation completion.
// ClaimedAt is optional independent evidence that this exact claim was reserved.
// AbortRequested is external cancellation input; only an explicit aborted result
// can establish its no-effect outcome. An unknown abort request remains unknown.
type ConcurrentCall struct {
	Attempt                                 string
	Identity                                ConcurrentIdentity
	Origin                                  ConcurrentOrigin
	Intent                                  ConcurrentIntent
	Invoked, Returned, WaitUntil, ClaimedAt uint64
	AbortRequested                          bool
	Reply                                   *ConcurrentResult
}

// ConcurrentRead captures one granular syscall Probe, one coherent diagnostic
// whole-closure View, or a failed observation. Separate syscalls get separate
// intervals. Unavailable needs a reason and no fabricated view/probe; it never
// excuses stale successful reads. Whole views require a real coherent-cut seam.
type ConcurrentRead struct {
	ID                string
	Origin            ConcurrentOrigin
	Invoked, Returned uint64
	Unavailable       bool
	Reason            string
	View              ConcurrentView
	Probe             *ConcurrentProbe
}

// ConcurrentFault retains externally scheduled owner/source loss or handoff
// evidence for replay. Faults NEVER reset the model, erase outcomes or authorize
// stale success. Missing data may yield unavailability. Authorized rollback into
// a new generation requires a separate baseline/trace and is unsupported here.
type ConcurrentFault struct {
	At           uint64
	Kind, Detail string
	Origin       ConcurrentOrigin
}

// ConcurrentTrace is a JSON-serializable, harness-owned external history.
// The baseline is fixture input, never reconstructed from the tested service.
// Generation and Members are fixed for one trace, including anchor/home groups.
type ConcurrentTrace struct {
	Version    uint32
	Seed       uint64
	Generation fsapi.UUID
	Members    []fsapi.UUID
	Baseline   ConcurrentView
	Calls      []ConcurrentCall
	Reads      []ConcurrentRead
	Faults     []ConcurrentFault
}

// ConcurrentLimits bounds exhaustive search; exhausting either limit is
// INCONCLUSIVE even if some partial prefix was consistent.
type ConcurrentLimits struct{ States uint64 }

// ConcurrentReport preserves the verdict, explored states and a complete witness.
type ConcurrentReport struct {
	Verdict       ConcurrentVerdict
	Reason        string
	States        uint64
	Linearization []string
}

// DecodeConcurrentTrace reads at most 128 MiB, rejects unknown/trailing fields and
// returns owned evidence. Semantic validation occurs in CheckConcurrentHistory.
func DecodeConcurrentTrace(r io.Reader) (ConcurrentTrace, error) {
	var trace ConcurrentTrace
	raw, err := io.ReadAll(io.LimitReader(r, concurrentTraceBytes+1))
	if err != nil {
		return trace, fmt.Errorf("read concurrent trace: %w", err)
	}
	if len(raw) > concurrentTraceBytes {
		return trace, fmt.Errorf("concurrent trace byte budget")
	}
	if !utf8.Valid(raw) || !concurrentJSONUnicode(raw) {
		return trace, fmt.Errorf("unsupported invalid UTF-8 or unpaired JSON surrogate")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&trace); err != nil {
		return trace, fmt.Errorf("decode concurrent trace: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return trace, fmt.Errorf("trailing concurrent trace data")
	}
	return trace, nil
}

// Clone returns independently owned trace evidence, including all payloads.
func (t ConcurrentTrace) Clone() ConcurrentTrace {
	t.Members = slices.Clone(t.Members)
	t.Baseline = cloneConcurrentView(t.Baseline)
	t.Calls = slices.Clone(t.Calls)
	t.Reads = slices.Clone(t.Reads)
	t.Faults = slices.Clone(t.Faults)
	for i := range t.Calls {
		c := &t.Calls[i]
		c.Intent.Data = slices.Clone(c.Intent.Data)
		c.Intent.Cred = c.Intent.Cred.Clone()
		if c.Intent.Expected != nil {
			v := *c.Intent.Expected
			c.Intent.Expected = &v
		}
		if c.Reply != nil {
			v := *c.Reply
			v.StableResult = slices.Clone(v.StableResult)
			c.Reply = &v
		}
	}
	for i := range t.Reads {
		t.Reads[i].View = cloneConcurrentView(t.Reads[i].View)
		if t.Reads[i].Probe != nil {
			p := *t.Reads[i].Probe
			p.Cred = p.Cred.Clone()
			p.Data = slices.Clone(p.Data)
			p.Observed.Data = slices.Clone(p.Observed.Data)
			p.Names = slices.Clone(p.Names)
			p.Cut = slices.Clone(p.Cut)
			t.Reads[i].Probe = &p
		}
	}
	return t
}

// MinimizeConcurrentTrace removes calls/reads using fresh reproduced evidence.
// reproduce must execute the reduced workload from its original baseline. An
// invalid reduced script returns an error; it never counts as a counterexample.
// With nil reproduce only read observations are removed, which cannot fabricate
// absent writes. Calls cannot be deleted from an offline trace while retaining
// their dependent observed data. This is bounded, not globally minimal.
func MinimizeConcurrentTrace(ctx context.Context, trace ConcurrentTrace, limits ConcurrentLimits, attempts int, reproduce func(context.Context, ConcurrentTrace) (ConcurrentTrace, error)) (ConcurrentTrace, int, error) {
	if err := ctx.Err(); err != nil {
		return trace, 0, err
	}
	if attempts < 1 || attempts > 128 {
		return trace, 0, fmt.Errorf("minimization attempt budget")
	}
	original := CheckConcurrentHistory(ctx, trace, limits)
	if original.Verdict != ConcurrentFail {
		return trace, 0, fmt.Errorf("minimization requires a complete counterexample: %s", original.Verdict)
	}
	out := trace.Clone()
	used := 0
	start := 0
	if reproduce == nil {
		start = len(out.Calls)
	}
	for index := start; index < len(out.Calls)+len(out.Reads) && used < attempts; {
		if err := ctx.Err(); err != nil {
			return out, used, err
		}
		candidate := out.Clone()
		if index < len(candidate.Calls) {
			candidate.Calls = slices.Delete(candidate.Calls, index, index+1)
		} else {
			j := index - len(candidate.Calls)
			candidate.Reads = slices.Delete(candidate.Reads, j, j+1)
		}
		used++
		if reproduce != nil {
			reproduced, err := reproduce(ctx, candidate.Clone())
			if err != nil {
				if ctx.Err() != nil {
					return out, used, ctx.Err()
				}
				index++
				continue
			}
			if !sameConcurrentWorkload(candidate, reproduced) {
				index++
				continue
			}
			candidate = reproduced
		}
		if CheckConcurrentHistory(ctx, candidate, limits).Verdict == ConcurrentFail {
			out = candidate
		} else {
			index++
		}
	}
	return out, used, nil
}
