package sim

import (
	"context"
	"encoding/binary"
	"fmt"
	"iter"
	"math"
	"reflect"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type concurrentEvent struct {
	name            string
	start, end      uint64
	call, read      int
	claim, optional bool
}
type concurrentSearch struct {
	ctx       context.Context
	trace     ConcurrentTrace
	events    []concurrentEvent
	limits    ConcurrentLimits
	states    uint64
	exhausted bool
	witness   []string
}

// CheckConcurrentHistory searches complete legal linearizations of overlapping
// calls/reads in one fixed dependency closure. Every terminal response is checked,
// including cross-origin retries. Unknown waits may complete after their deadline.
// The oracle does not implement or certify the distributed commit/fence protocol.
func CheckConcurrentHistory(ctx context.Context, trace ConcurrentTrace, limits ConcurrentLimits) ConcurrentReport {
	if limits.States == 0 || limits.States > 1_000_000 {
		return ConcurrentReport{Verdict: ConcurrentInconclusive, Reason: "search state budget must be 1..1000000"}
	}
	events, err := validateConcurrentTrace(trace)
	if err != nil {
		return ConcurrentReport{Verdict: ConcurrentInconclusive, Reason: err.Error()}
	}
	state, err := newConcurrentState(trace.Baseline)
	if err != nil {
		return ConcurrentReport{Verdict: ConcurrentInconclusive, Reason: err.Error()}
	}
	search := concurrentSearch{ctx: ctx, trace: trace, events: events, limits: limits}
	if search.walk(state, 0, 0, nil) {
		return ConcurrentReport{Verdict: ConcurrentPass, States: search.states, Linearization: search.witness}
	}
	if ctx.Err() != nil {
		return ConcurrentReport{Verdict: ConcurrentInconclusive, States: search.states, Reason: ctx.Err().Error()}
	}
	if search.exhausted {
		return ConcurrentReport{Verdict: ConcurrentInconclusive, States: search.states, Reason: "complete search exceeded state budget"}
	}
	return ConcurrentReport{Verdict: ConcurrentFail, States: search.states, Reason: "no complete legal linearization explains every terminal result and successful read"}
}

func (s *concurrentSearch) walk(state concurrentState, done, now uint64, order []string) bool {
	if s.ctx.Err() != nil || s.exhausted {
		return false
	}
	if s.states >= s.limits.States {
		s.exhausted = true
		return false
	}
	s.states++
	if done == (uint64(1)<<len(s.events))-1 {
		s.witness = slices.Clone(order)
		return true
	}
	// Once an unprocessed mandatory event's upper bound has passed, no extension
	// of this branch can satisfy its observed invocation/response interval.
	for index, event := range s.events {
		if done&(uint64(1)<<index) == 0 && !event.optional && event.end < now {
			return false
		}
	}
	for index, event := range s.events {
		bit := uint64(1) << index
		if done&bit != 0 {
			continue
		}
		at := max(now, event.start)
		if at > event.end {
			continue
		}
		if event.optional && s.walk(state, done|bit, now, appendConcurrentOrder(order, "omit:"+event.name)) {
			return true
		}
		for next := range s.advance(state, event) {
			if s.walk(next, done|bit, at, appendConcurrentOrder(order, event.name)) {
				return true
			}
		}
		if s.exhausted || s.ctx.Err() != nil {
			return false
		}
	}
	return false
}

func (s *concurrentSearch) advance(state concurrentState, event concurrentEvent) iter.Seq[concurrentState] {
	return func(yield func(concurrentState) bool) {
		if event.read >= 0 {
			read := s.trace.Reads[event.read]
			if read.Unavailable || read.Probe != nil && concurrentProbeMatches(state, *read.Probe, s.trace.Generation) || read.Probe == nil && concurrentViewMatches(state, read.View, s.trace.Generation) {
				yield(state)
			}
			return
		}
		call := s.trace.Calls[event.call]
		claim, exists := state.claims[call.Identity]
		if exists && !sameConcurrentIntent(claim.Intent, call.Intent) {
			if event.claim {
				return
			}
			if event.optional || call.Reply != nil && call.Reply.Status == "conflict" && call.Reply.Code == "id_conflict" && call.Reply.Origin == claim.Origin && call.Reply.Object == (fsapi.ObjectID{}) && call.Reply.Count == 0 && call.Reply.Offset == 0 {
				yield(state)
			}
			return
		}
		if !exists {
			state = cloneConcurrentState(state)
			claim = concurrentClaim{Intent: call.Intent, Origin: call.Origin}
			state.claims[call.Identity] = claim
		}
		if event.claim {
			yield(state)
			return
		}
		if claim.Result != nil {
			if event.optional {
				yield(state)
			} else if call.Reply != nil && sameConcurrentResult(*call.Reply, *claim.Result, claim.StableKnown) {
				if !claim.StableKnown {
					state = cloneConcurrentState(state)
					saved := *claim.Result
					saved.StableResult = slices.Clone(call.Reply.StableResult)
					claim.Result = &saved
					claim.StableKnown = true
					state.claims[call.Identity] = claim
				}
				yield(state)
			}
			return
		}
		if call.Reply != nil && call.Reply.Status == "conflict" {
			return
		}
		for _, allocation := range s.allocations(call) {
			for _, count := range concurrentWriteCounts(call, event.optional) {
				candidate := cloneConcurrentState(state)
				result := concurrentTransition(&candidate, claim.Intent, allocation, count)
				result.Origin = claim.Origin
				if result.Code == "allocation" {
					continue
				}
				if event.optional || call.Reply != nil && sameConcurrentResult(*call.Reply, result, false) {
					saved := claim
					if !event.optional {
						result.StableResult = slices.Clone(call.Reply.StableResult)
						saved.StableKnown = true
					}
					saved.Result = &result
					candidate.claims[call.Identity] = saved
					if result.Status == "committed" && claim.Intent.Kind != "barrier" {
						candidate.cut = append(candidate.cut, call.Identity)
					}
					if !yield(candidate) {
						return
					}
				}
			}
		}
		if event.optional && call.AbortRequested || call.Reply != nil && call.Reply.Status == "aborted" {
			result := ConcurrentResult{Status: "aborted", Code: "cancelled", Origin: claim.Origin}
			if !event.optional {
				result.Code = call.Reply.Code
			}
			if event.optional || sameConcurrentResult(*call.Reply, result, false) {
				candidate := cloneConcurrentState(state)
				saved := claim
				if !event.optional {
					result.StableResult = slices.Clone(call.Reply.StableResult)
					saved.StableKnown = true
				}
				saved.Result = &result
				candidate.claims[call.Identity] = saved
				yield(candidate)
			}
		}
	}
}

func concurrentWriteCounts(call ConcurrentCall, unknown bool) []int64 {
	if call.Intent.Kind != "write" && call.Intent.Kind != "append" {
		return []int64{0}
	}
	size := int64(len(call.Intent.Data))
	if !unknown && call.Reply != nil && call.Reply.Status == "committed" {
		if call.Reply.Count < 0 || call.Reply.Count > size {
			return nil
		}
		return []int64{call.Reply.Count}
	}
	counts := make([]int64, size+1)
	// Full writes are most common; all permitted short ranges remain alternatives.
	for i := range counts {
		counts[i] = size - int64(i)
	}
	return counts
}

func (s *concurrentSearch) allocations(call ConcurrentCall) []fsapi.ObjectID {
	if call.Intent.Kind != "create" && call.Intent.Kind != "mkdir" {
		return []fsapi.ObjectID{{}}
	}
	// Allocation is legitimately nondeterministic. Only the fresh stable identity
	// is bound from observed output; all semantic fields come from original input.
	if call.Reply != nil && call.Reply.Status == "committed" {
		return []fsapi.ObjectID{call.Reply.Object}
	}
	observed := concurrentObservedObjects(s.trace)
	out := []fsapi.ObjectID{{}}
	for _, id := range observed {
		if id.Home == call.Intent.Source.Volume && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	// An unknown create may be established only by existence or metadata, with
	// no captured allocation ID. One fresh representative per attempt covers
	// those otherwise indistinguishable allocations; all observed identities
	// remain explicit alternatives. Reserve representatives across the workload
	// so two unrelated creates cannot accidentally share an anonymous identity.
	reserved := make(map[fsapi.ObjectID]bool, len(observed)+len(s.trace.Calls))
	for _, id := range observed {
		reserved[id] = true
	}
	for index, attempt := range s.trace.Calls {
		if attempt.Intent.Kind != "create" && attempt.Intent.Kind != "mkdir" {
			continue
		}
		allocation := UUID(s.trace.Seed, fmt.Sprintf("concurrent/anonymous-allocation/%d", index))
		for nonce := 0; nonce <= len(reserved)+1; nonce++ {
			id := fsapi.ObjectID{Home: attempt.Intent.Source.Volume, Allocation: allocation}
			// Distinct suffixes guarantee a free valid representative within
			// the finite observed/reserved set, including a possible zero ID.
			binary.BigEndian.PutUint64(id.Allocation[8:], uint64(nonce))
			if !id.Valid() || reserved[id] {
				continue
			}
			reserved[id] = true
			if attempt.Attempt == call.Attempt {
				out = append(out, id)
			}
			break
		}
	}
	return out
}

func concurrentObservedObjects(trace ConcurrentTrace) []fsapi.ObjectID {
	var out []fsapi.ObjectID
	add := func(id fsapi.ObjectID) {
		if id.Valid() && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	view := func(v ConcurrentView) {
		for _, name := range v.Names {
			add(name.Object)
		}
		for _, object := range v.Objects {
			add(object.ID)
			add(object.Parent)
		}
	}
	view(trace.Baseline)
	for _, read := range trace.Reads {
		view(read.View)
		if p := read.Probe; p != nil {
			add(p.Object)
			if p.Fields&ConcurrentIdentityField != 0 {
				add(p.Observed.ID)
			}
			if p.Fields&ConcurrentParentField != 0 {
				add(p.Observed.Parent)
			}
			for _, name := range p.Names {
				add(name.Object)
			}
		}
	}
	for _, call := range trace.Calls {
		if call.Intent.Expected != nil {
			add(*call.Intent.Expected)
		}
		if call.Reply != nil {
			add(call.Reply.Object)
		}
	}
	return out
}

func appendConcurrentOrder(order []string, name string) []string {
	out := slices.Clone(order)
	return append(out, name)
}

func validConcurrentIdentity(id ConcurrentIdentity) bool {
	if len(id.Principal) == 0 || len(id.Principal) > 128 {
		return false
	}
	if id.Operation.Valid() {
		return !id.Anchor.Valid() && !id.Session.Valid() && id.Number == 0
	}
	return id.Anchor.Valid() && id.Session.Valid() && id.Number > 0
}

func validConcurrentOrigin(origin ConcurrentOrigin) bool {
	return origin.Node.Valid() && origin.Incarnation.Valid()
}

func validateConcurrentTrace(t ConcurrentTrace) ([]concurrentEvent, error) {
	if t.Version != 1 || !t.Generation.Valid() || len(t.Members) < 1 || len(t.Members) > 8 || len(t.Calls) > 16 || len(t.Reads) < 1 || len(t.Reads) > 16 || len(t.Faults) > 32 {
		return nil, fmt.Errorf("unsupported concurrent trace version/identity/bounds")
	}
	if !concurrentUTF8(t) || !concurrentCaptureBudget(t) {
		return nil, fmt.Errorf("unsupported invalid UTF-8 or captured-data budget")
	}
	members := make(map[fsapi.UUID]bool)
	for _, member := range t.Members {
		if !member.Valid() || members[member] {
			return nil, fmt.Errorf("invalid closure members")
		}
		members[member] = true
	}
	if err := validateConcurrentView(t.Baseline, t.Generation, members); err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	if len(t.Baseline.Cut) != 0 {
		return nil, fmt.Errorf("baseline cut must begin this trace")
	}
	events := make([]concurrentEvent, 0, 48)
	identities := make(map[string]bool)
	totalBytes := 0
	for index, call := range t.Calls {
		if !validConcurrentIdentity(call.Identity) || !validConcurrentOrigin(call.Origin) || len(call.Attempt) == 0 || len(call.Attempt) > 128 || identities[call.Attempt] || call.Invoked == 0 {
			return nil, fmt.Errorf("invalid/duplicate call identity or invocation")
		}
		if call.Identity.Anchor.Valid() && !members[call.Identity.Anchor] {
			return nil, fmt.Errorf("anchor outside complete closure")
		}
		identities[call.Attempt] = true
		totalBytes += len(call.Intent.Data)
		if totalBytes > 1<<20 {
			return nil, fmt.Errorf("concurrent payload budget")
		}
		if err := validateConcurrentIntent(call.Intent, members); err != nil {
			return nil, err
		}
		if call.WaitUntil != 0 && call.WaitUntil < call.Invoked {
			return nil, fmt.Errorf("wait deadline before invocation")
		}
		if call.ClaimedAt != 0 {
			if call.ClaimedAt < call.Invoked {
				return nil, fmt.Errorf("claim before invocation")
			}
			events = append(events, concurrentEvent{name: "claim:" + call.Attempt, start: call.Invoked, end: call.ClaimedAt, call: index, read: -1, claim: true})
		}
		event := concurrentEvent{name: "call:" + call.Attempt, start: call.Invoked, end: math.MaxUint64, call: index, read: -1, optional: true}
		if call.Reply != nil {
			if len(call.Reply.Code) > 256 || len(call.Reply.StableResult) > 4096 {
				return nil, fmt.Errorf("reply result budget")
			}
			if call.Returned < call.Invoked || call.Returned == 0 {
				return nil, fmt.Errorf("response outside call interval")
			}
			switch call.Reply.Status {
			case "committed", "rejected", "conflict", "aborted":
				event.end = call.Returned
				event.optional = false
				if call.Identity.Anchor.Valid() && call.Reply.Status != "conflict" && len(call.Reply.StableResult) == 0 {
					return nil, fmt.Errorf("anchored terminal reply needs canonical stable result bytes")
				}
				if !validConcurrentOrigin(call.Reply.Origin) {
					return nil, fmt.Errorf("terminal reply missing original origin")
				}
			case "pending":
				if call.ClaimedAt == 0 {
					events = append(events, concurrentEvent{name: "claim:" + call.Attempt, start: call.Invoked, end: call.Returned, call: index, read: -1, claim: true})
				}
			case "indeterminate":
			default:
				return nil, fmt.Errorf("unsupported reply status")
			}
		} else if call.Returned != 0 {
			return nil, fmt.Errorf("completion without response; use WaitUntil for timeouts")
		}
		events = append(events, event)
	}
	for index, read := range t.Reads {
		if len(read.ID) == 0 || len(read.ID) > 128 || identities[read.ID] || !validConcurrentOrigin(read.Origin) || read.Invoked == 0 || read.Returned < read.Invoked {
			return nil, fmt.Errorf("invalid read identity/interval")
		}
		identities[read.ID] = true
		if read.Unavailable {
			if read.Reason == "" || len(read.Reason) > 1024 || (!reflect.DeepEqual(read.View, ConcurrentView{}) || read.Probe != nil) {
				return nil, fmt.Errorf("unavailable read needs reason and no fabricated view")
			}
		} else if read.Probe != nil {
			if !reflect.DeepEqual(read.View, ConcurrentView{}) {
				return nil, fmt.Errorf("granular read cannot also fabricate whole view")
			}
			if err := validateConcurrentProbe(*read.Probe, members); err != nil {
				return nil, err
			}
		} else if err := validateConcurrentView(read.View, t.Generation, members); err != nil {
			return nil, fmt.Errorf("read %s: %w", read.ID, err)
		}
		events = append(events, concurrentEvent{name: "read:" + read.ID, start: read.Invoked, end: read.Returned, call: -1, read: index})
	}
	for _, fault := range t.Faults {
		if fault.At == 0 || len(fault.Kind) == 0 || len(fault.Kind) > 128 || len(fault.Detail) > 1024 || !validConcurrentOrigin(fault.Origin) {
			return nil, fmt.Errorf("invalid fault evidence")
		}
	}
	return events, nil
}

func validateConcurrentIntent(i ConcurrentIntent, members map[fsapi.UUID]bool) error {
	if !validConcurrentPath(i.Source) || !members[i.Source.Volume] || i.Mode&^uint16(0o7777) != 0 || i.Umask&^uint16(0o777) != 0 || len(i.Data) > 4096 || len(i.Cred.Groups) > 32 || i.Offset < 0 || i.Size < 0 || i.Offset > concurrentFileBytes-int64(len(i.Data)) || i.Size > concurrentFileBytes {
		return fmt.Errorf("unsupported intent bounds/path/metadata")
	}
	switch i.Kind {
	case "create", "mkdir", "chmod", "unlink", "barrier":
		if len(i.Data) != 0 || i.Offset != 0 || i.Size != 0 {
			return fmt.Errorf("unexpected intent payload/range")
		}
	case "append", "write":
		if len(i.Data) == 0 || i.Size != 0 || i.Kind == "append" && i.Offset != 0 {
			return fmt.Errorf("invalid write input")
		}
	case "truncate":
		if len(i.Data) != 0 || i.Offset != 0 {
			return fmt.Errorf("invalid truncate input")
		}
	case "rename", "link":
		if !validConcurrentPath(i.Destination) || !members[i.Destination.Volume] || len(i.Data) != 0 || i.Offset != 0 || i.Size != 0 {
			return fmt.Errorf("invalid namespace input/closure")
		}
	default:
		return fmt.Errorf("unsupported concurrent intent %q", i.Kind)
	}
	if i.Expected != nil && !i.Expected.Valid() {
		return fmt.Errorf("invalid original object precondition")
	}
	return nil
}

func validateConcurrentView(v ConcurrentView, generation fsapi.UUID, members map[fsapi.UUID]bool) error {
	if v.Generation != generation || len(v.Names) > 256 || len(v.Objects) > 256 || len(v.Cut) > 16 || !v.CutKnown && len(v.Cut) != 0 {
		return fmt.Errorf("unsupported view generation/budget/cut")
	}
	bytes := 0
	for _, object := range v.Objects {
		bytes += len(object.Data)
		if !object.ID.Valid() || !members[object.ID.Home] || object.Kind < fsapi.Regular || object.Kind > fsapi.Symlink || object.Mode&^uint16(0o7777) != 0 || object.Size < 0 || object.Size != int64(len(object.Data)) || len(object.Data) > concurrentFileBytes || bytes > concurrentViewBytes || object.Kind == fsapi.Directory && len(object.Data) != 0 {
			return fmt.Errorf("invalid observable object/home/metadata")
		}
	}
	state, err := newConcurrentState(v)
	if err != nil {
		return err
	}
	links := make(map[fsapi.ObjectID]uint64)
	subdirectories := make(map[fsapi.ObjectID]uint64)
	for _, name := range v.Names {
		object, ok := state.objects[name.Object]
		if !validConcurrentPath(name.Path) || !members[name.Path.Volume] || !ok {
			return fmt.Errorf("invalid name or dangling object")
		}
		links[name.Object]++
		if name.Path.Name == "." {
			if object.Kind != fsapi.Directory {
				return fmt.Errorf("root is not a directory")
			}
			continue
		}
		parent, ok := state.names[concurrentParent(name.Path)]
		if !ok || state.objects[parent].Kind != fsapi.Directory {
			return fmt.Errorf("missing directory parent")
		}
		if object.Kind == fsapi.Directory {
			if object.Parent != parent {
				return fmt.Errorf("directory logical parent differs")
			}
			subdirectories[parent]++
		}
	}
	for member := range members {
		if _, ok := state.names[ConcurrentPath{Volume: member, Name: "."}]; !ok {
			return fmt.Errorf("missing closure root")
		}
	}
	for id, object := range state.objects {
		count := links[id]
		if object.Kind == fsapi.Directory && count > 0 {
			if count != 1 || object.Nlink != 2+subdirectories[id] {
				return fmt.Errorf("directory link count")
			}
		} else if object.Nlink != count {
			return fmt.Errorf("object link count")
		}
		if count == 0 && object.Pins == 0 {
			return fmt.Errorf("unobservable unpinned object")
		}
	}
	seen := make(map[ConcurrentIdentity]bool)
	for _, id := range v.Cut {
		if !validConcurrentIdentity(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate read cut identity")
		}
		seen[id] = true
	}
	return nil
}
