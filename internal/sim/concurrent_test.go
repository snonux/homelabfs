package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type concurrentFixture struct {
	trace                 ConcurrentTrace
	a, b, w               ConcurrentOrigin
	volume, other         fsapi.UUID
	file, root, otherRoot fsapi.ObjectID
}

func concurrentFixtureForTest() concurrentFixture {
	f := concurrentFixture{volume: UUID(77, "volume"), other: UUID(77, "other")}
	f.a = ConcurrentOrigin{UUID(77, "owner"), UUID(77, "owner-lifetime")}
	f.b = ConcurrentOrigin{UUID(77, "remote"), UUID(77, "remote-lifetime")}
	f.w = ConcurrentOrigin{UUID(77, "outside"), UUID(77, "outside-lifetime")}
	f.root = fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "root")}
	f.otherRoot = fsapi.ObjectID{Home: f.other, Allocation: UUID(77, "other-root")}
	f.file = fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "file")}
	f.trace = ConcurrentTrace{Version: 1, Seed: 77, Generation: UUID(77, "generation"), Members: []fsapi.UUID{f.volume, f.other}}
	f.trace.Baseline = ConcurrentView{Generation: f.trace.Generation, CutKnown: true, Names: []ConcurrentName{{ConcurrentPath{f.volume, "."}, f.root}, {ConcurrentPath{f.other, "."}, f.otherRoot}, {ConcurrentPath{f.volume, "file"}, f.file}}, Objects: []ConcurrentObject{{ID: f.root, Kind: fsapi.Directory, Mode: 0o777, UID: 1000, GID: 2000, Nlink: 2}, {ID: f.otherRoot, Kind: fsapi.Directory, Mode: 0o777, UID: 1000, GID: 2000, Nlink: 2}, {ID: f.file, Kind: fsapi.Regular, Mode: 0o666, UID: 1000, GID: 1000, Nlink: 1, Data: []byte("abcd"), Size: 4, Pins: 1}}}
	return f
}

func (f concurrentFixture) call(id, kind string, origin ConcurrentOrigin, start, end uint64) ConcurrentCall {
	return ConcurrentCall{Attempt: id, Identity: ConcurrentIdentity{Principal: "alice", Operation: UUID(77, "op/"+id)}, Origin: origin, Invoked: start, Returned: end, Intent: ConcurrentIntent{Kind: kind, Source: ConcurrentPath{f.volume, "file"}, Cred: fsapi.Cred{UID: 1000, GID: 1000}}, Reply: &ConcurrentResult{Status: "committed", Code: "ok", Origin: origin, Object: f.file}}
}

func (f concurrentFixture) view(data string, cut ...ConcurrentIdentity) ConcurrentView {
	view := cloneConcurrentView(f.trace.Baseline)
	view.Cut = cut
	for i := range view.Objects {
		if view.Objects[i].ID == f.file {
			view.Objects[i].Data = []byte(data)
			view.Objects[i].Size = int64(len(data))
		}
	}
	return view
}

func (f concurrentFixture) read(id string, at uint64, view ConcurrentView) ConcurrentRead {
	return ConcurrentRead{ID: id, Origin: f.w, Invoked: at, Returned: at + 1, View: view}
}

func concurrentRequire(t *testing.T, trace ConcurrentTrace, want ConcurrentVerdict) ConcurrentReport {
	t.Helper()
	report := CheckConcurrentHistory(t.Context(), trace, ConcurrentLimits{States: 100000})
	if report.Verdict != want {
		raw, _ := json.Marshal(trace)
		t.Fatalf("got %+v want %s\n%s", report, want, raw)
	}
	return report
}

func TestConcurrentAppendOrdersAndBadObservations(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		f := concurrentFixtureForTest()
		a := f.call("a", "append", f.a, 10, 30)
		b := f.call("b", "append", f.w, 11, 31)
		a.Intent.Data = []byte("AA")
		b.Intent.Data = []byte("BB")
		a.Reply.Count = 2
		b.Reply.Count = 2
		a.Reply.Offset = 4
		b.Reply.Offset = 6
		data := "abcdAABB"
		cut := []ConcurrentIdentity{a.Identity, b.Identity}
		if reverse {
			a.Reply.Offset = 6
			b.Reply.Offset = 4
			data = "abcdBBAA"
			cut = []ConcurrentIdentity{b.Identity, a.Identity}
		}
		f.trace.Calls = []ConcurrentCall{a, b}
		f.trace.Reads = []ConcurrentRead{f.read("final", 40, f.view(data, cut...))}
		concurrentRequire(t, f.trace, ConcurrentPass)
		for _, mutation := range []string{"duplicate offset", "wrong bytes", "stale", "wrong order", "wrong home", "speculative extra cut"} {
			t.Run(mutation, func(t *testing.T) {
				bad := f.trace.Clone()
				switch mutation {
				case "duplicate offset":
					bad.Calls[1].Reply.Offset = bad.Calls[0].Reply.Offset
				case "wrong bytes":
					for i := range bad.Reads[0].View.Objects {
						if bad.Reads[0].View.Objects[i].ID == f.file {
							bad.Reads[0].View.Objects[i].Data = []byte("abcdAAAA")
						}
					}
				case "stale":
					bad.Reads[0].View = f.view("abcd")
				case "wrong order":
					slices.Reverse(bad.Reads[0].View.Cut)
				case "wrong home":
					newID := fsapi.ObjectID{Home: f.other, Allocation: f.file.Allocation}
					for i := range bad.Reads[0].View.Names {
						if bad.Reads[0].View.Names[i].Object == f.file {
							bad.Reads[0].View.Names[i].Object = newID
						}
					}
					for i := range bad.Reads[0].View.Objects {
						if bad.Reads[0].View.Objects[i].ID == f.file {
							bad.Reads[0].View.Objects[i].ID = newID
						}
					}
				case "speculative extra cut":
					bad.Reads[0].View.Cut = append(bad.Reads[0].View.Cut, ConcurrentIdentity{Principal: "alice", Operation: UUID(99, "never-invoked")})
				}
				concurrentRequire(t, bad, ConcurrentFail)
			})
		}
	}
}

func TestConcurrentReadIntervalsAndUnknownTimeout(t *testing.T) {
	f := concurrentFixtureForTest()
	call := f.call("late", "append", f.w, 10, 0)
	call.Intent.Data = []byte("Z")
	call.Reply = nil
	call.WaitUntil = 12
	f.trace.Calls = []ConcurrentCall{call}
	f.trace.Reads = []ConcurrentRead{f.read("before", 15, f.view("abcd")), f.read("after", 20, f.view("abcdZ", call.Identity))}
	report := concurrentRequire(t, f.trace, ConcurrentPass)
	if len(report.Linearization) != 3 {
		t.Fatal(report)
	}
	// No response was observed at the deadline. The effect can complete between
	// these two later reads, rather than being discarded or bounded at ordinal 12.
	bad := f.trace.Clone()
	bad.Reads = append(bad.Reads, f.read("rollback", 30, f.view("abcd")))
	concurrentRequire(t, bad, ConcurrentFail)
	// A genuinely overlapping read may see either side of a known completion.
	call.Reply = &ConcurrentResult{Status: "committed", Code: "ok", Origin: f.w, Object: f.file, Count: 1, Offset: 4}
	call.Returned = 20
	f.trace.Calls = []ConcurrentCall{call}
	for _, data := range []string{"abcd", "abcdZ"} {
		cut := []ConcurrentIdentity{}
		if data == "abcdZ" {
			cut = append(cut, call.Identity)
		}
		read := f.read("overlap", 15, f.view(data, cut...))
		read.Returned = 25
		f.trace.Reads = []ConcurrentRead{read}
		concurrentRequire(t, f.trace, ConcurrentPass)
	}
	f.trace.Reads = []ConcurrentRead{f.read("postreply", 21, f.view("abcd"))}
	concurrentRequire(t, f.trace, ConcurrentFail)
	// Cut omission does not mean genesis; bytes and all terminal replies still count.
	view := f.view("abcdZ")
	view.CutKnown = false
	view.Cut = nil
	f.trace.Reads = []ConcurrentRead{f.read("no cut supplied", 30, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	view = f.view("abcd")
	view.CutKnown = false
	view.Cut = nil
	f.trace.Reads[0].View = view
	concurrentRequire(t, f.trace, ConcurrentFail)
}

func TestConcurrentLostReplyCrossOriginRetryAndClaim(t *testing.T) {
	f := concurrentFixtureForTest()
	original := f.call("original", "append", f.w, 10, 0)
	original.Intent.Data = []byte("Z")
	original.Reply = nil
	original.WaitUntil = 12
	original.ClaimedAt = 11
	retry := original
	retry.Attempt = "retry"
	retry.Origin = f.b
	retry.Invoked = 20
	retry.Returned = 30
	retry.WaitUntil = 0
	retry.ClaimedAt = 0
	retry.Reply = &ConcurrentResult{Status: "committed", Code: "ok", Origin: f.w, Object: f.file, Count: 1, Offset: 4}
	f.trace.Calls = []ConcurrentCall{original, retry}
	f.trace.Reads = []ConcurrentRead{f.read("final", 40, f.view("abcdZ", original.Identity))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	for _, kind := range []string{"double effect", "wrong retry offset", "new durable origin", "lost known retry", "changed canonical", "conflict same intent"} {
		t.Run(kind, func(t *testing.T) {
			bad := f.trace.Clone()
			switch kind {
			case "double effect":
				bad.Reads[0].View = f.view("abcdZZ", original.Identity)
			case "wrong retry offset":
				bad.Calls[1].Reply.Offset = 5
			case "new durable origin":
				bad.Calls[1].Reply.Origin = f.b
			case "lost known retry":
				bad.Reads[0].View = f.view("abcd")
			case "changed canonical":
				bad.Calls[1].Intent.Data = []byte("different")
			case "conflict same intent":
				bad.Calls[1].Reply = &ConcurrentResult{Status: "conflict", Code: "id_conflict", Origin: f.w}
			}
			concurrentRequire(t, bad, ConcurrentFail)
		})
	}
	// Changed immutable input gets ID_CONFLICT even though the original can still
	// finish after the retry. The known original claim cannot be erased by timeout.
	retry.Intent.Data = []byte("changed")
	retry.Reply = &ConcurrentResult{Status: "conflict", Code: "id_conflict", Origin: f.w}
	f.trace.Calls = []ConcurrentCall{original, retry}
	f.trace.Reads = []ConcurrentRead{f.read("still pending", 40, f.view("abcd"))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	retry.Reply = &ConcurrentResult{Status: "committed", Code: "ok", Origin: f.b, Object: f.file, Count: 7, Offset: 4}
	f.trace.Calls[1] = retry
	f.trace.Reads[0].View = f.view("abcdchanged", original.Identity)
	concurrentRequire(t, f.trace, ConcurrentFail)
}

func TestConcurrentCandidateLaterAbortedAndUnknownStatus(t *testing.T) {
	f := concurrentFixtureForTest()
	call := f.call("candidate", "write", f.w, 10, 30)
	call.Intent.Data = []byte("X")
	call.AbortRequested = true
	call.Reply = &ConcurrentResult{Status: "aborted", Code: "cancelled", Origin: f.w}
	f.trace.Calls = []ConcurrentCall{call}
	f.trace.Reads = []ConcurrentRead{f.read("during", 20, f.view("abcd"))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	// External definitive abort, not implementation stage, excludes the candidate
	// as a possible explanation for this otherwise ambiguous overlapping read.
	f.trace.Reads[0].View = f.view("Xbcd", call.Identity)
	concurrentRequire(t, f.trace, ConcurrentFail)
	call.Reply = &ConcurrentResult{Status: "indeterminate", Code: "missing source"}
	f.trace.Calls = []ConcurrentCall{call}
	f.trace.Reads[0].View = f.view("Xbcd", call.Identity)
	concurrentRequire(t, f.trace, ConcurrentPass)
	// An observed protected abort constrains the history even without Cancel.
	// A timeout alone has no such terminal status.
	call.AbortRequested = false
	call.Reply = &ConcurrentResult{Status: "aborted", Code: "cancelled", Origin: f.w}
	f.trace.Calls = []ConcurrentCall{call}
	f.trace.Reads[0].View = f.view("abcd")
	concurrentRequire(t, f.trace, ConcurrentPass)
}

func TestConcurrentWriteTruncateAndPermissions(t *testing.T) {
	f := concurrentFixtureForTest()
	write := f.call("write", "write", f.w, 10, 20)
	write.Intent.Data = []byte("XY")
	write.Intent.Offset = 1
	write.Reply.Count = 2
	write.Reply.Offset = 1
	shrink := f.call("shrink", "truncate", f.b, 11, 30)
	shrink.Intent.Size = 2
	extend := f.call("extend", "truncate", f.a, 31, 40)
	extend.Intent.Size = 4
	f.trace.Calls = []ConcurrentCall{write, shrink, extend}
	f.trace.Reads = []ConcurrentRead{f.read("whole", 50, f.view("aX\x00\x00", write.Identity, shrink.Identity, extend.Identity))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	f.trace.Reads[0].View = f.view("aXYd", write.Identity, shrink.Identity, extend.Identity)
	concurrentRequire(t, f.trace, ConcurrentFail)
	chmod := f.call("chmod", "chmod", f.a, 10, 20)
	chmod.Intent.Mode = 0o400
	write = f.call("denied", "write", f.w, 11, 30)
	write.Intent.Data = []byte("Z")
	write.Intent.Cred = fsapi.Cred{UID: 2000, GID: 2000}
	write.Reply = &ConcurrentResult{Status: "rejected", Code: "permission", Origin: f.w}
	view := f.view("abcd", chmod.Identity)
	for i := range view.Objects {
		if view.Objects[i].ID == f.file {
			view.Objects[i].Mode = 0o400
		}
	}
	f.trace.Calls = []ConcurrentCall{chmod, write}
	f.trace.Reads = []ConcurrentRead{f.read("permissions", 40, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Calls[1].Reply = &ConcurrentResult{Status: "committed", Code: "ok", Origin: f.w, Object: f.file, Count: 1}
	bad.Reads[0].View.Cut = append(bad.Reads[0].View.Cut, write.Identity)
	for i := range bad.Reads[0].View.Objects {
		if bad.Reads[0].View.Objects[i].ID == f.file {
			bad.Reads[0].View.Objects[i].Data = []byte("Zbcd")
		}
	}
	concurrentRequire(t, bad, ConcurrentFail)
}

func TestConcurrentRenameChmodClosureAliasesAndPartialViews(t *testing.T) {
	f := concurrentFixtureForTest()
	rename := f.call("rename", "rename", f.w, 10, 20)
	rename.Intent.Destination = ConcurrentPath{f.other, "moved"}
	rename.Intent.Transaction = true
	chmod := f.call("chmod", "chmod", f.b, 11, 30)
	chmod.Intent.Source = rename.Intent.Destination
	chmod.Intent.Mode = 0o600
	view := f.view("abcd", rename.Identity, chmod.Identity)
	for i := range view.Names {
		if view.Names[i].Object == f.file {
			view.Names[i].Path = rename.Intent.Destination
		}
	}
	for i := range view.Objects {
		if view.Objects[i].ID == f.file {
			view.Objects[i].Mode = 0o600
		}
	}
	f.trace.Calls = []ConcurrentCall{rename, chmod}
	f.trace.Reads = []ConcurrentRead{f.read("both groups", 40, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	partial := f.trace.Clone()
	partial.Reads[0].View.Names = append(partial.Reads[0].View.Names, ConcurrentName{ConcurrentPath{f.volume, "file"}, f.file})
	for i := range partial.Reads[0].View.Objects {
		if partial.Reads[0].View.Objects[i].ID == f.file {
			partial.Reads[0].View.Objects[i].Nlink = 2
		}
	}
	concurrentRequire(t, partial, ConcurrentFail)
	// Native cross-mount calls cannot quietly use the application transaction API.
	rename.Intent.Transaction = false
	rename.Reply = &ConcurrentResult{Status: "rejected", Code: "cross_volume", Origin: f.w}
	f.trace.Calls = []ConcurrentCall{rename}
	f.trace.Reads = []ConcurrentRead{f.read("native", 40, f.view("abcd"))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	link := f.call("link", "link", f.w, 10, 20)
	link.Intent.Transaction = true
	link.Intent.Destination = ConcurrentPath{f.other, "alias"}
	write := f.call("alias write", "write", f.b, 11, 30)
	write.Intent.Source = link.Intent.Destination
	write.Intent.Data = []byte("Q")
	write.Reply.Count = 1
	view = f.view("Qbcd", link.Identity, write.Identity)
	view.Names = append(view.Names, ConcurrentName{link.Intent.Destination, f.file})
	for i := range view.Objects {
		if view.Objects[i].ID == f.file {
			view.Objects[i].Nlink = 2
		}
	}
	f.trace.Calls = []ConcurrentCall{link, write}
	f.trace.Reads = []ConcurrentRead{f.read("shared home", 40, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	for i := range bad.Reads[0].View.Objects {
		if bad.Reads[0].View.Objects[i].ID == f.file {
			bad.Reads[0].View.Objects[i].Data = []byte("abcd")
		}
	}
	concurrentRequire(t, bad, ConcurrentFail)
}

func TestConcurrentOwnerRAMLossAndHandoffNeverExcuseStaleSuccess(t *testing.T) {
	for _, fault := range []string{"owner_backing_apply_blocked", "owner_loss", "origin_source_loss", "origin_disk_and_owner_ram_loss", "cooperative_handoff"} {
		t.Run(fault, func(t *testing.T) {
			f := concurrentFixtureForTest()
			call := f.call("committed", "append", f.w, 10, 20)
			call.Intent.Data = []byte("Z")
			call.Reply.Count = 1
			call.Reply.Offset = 4
			f.trace.Calls = []ConcurrentCall{call}
			f.trace.Faults = []ConcurrentFault{{At: 25, Kind: fault, Origin: f.a, Detail: "externally scheduled fixture boundary; no inferred absence"}}
			f.trace.Reads = []ConcurrentRead{f.read("new owner logical", 30, f.view("abcdZ", call.Identity))}
			concurrentRequire(t, f.trace, ConcurrentPass)
			unavailable := f.trace.Clone()
			unavailable.Reads = []ConcurrentRead{{ID: "blocked", Origin: f.b, Invoked: 30, Returned: 31, Unavailable: true, Reason: "required source unavailable"}}
			concurrentRequire(t, unavailable, ConcurrentPass)
			unavailable.Reads = append(unavailable.Reads, f.read("stale success", 40, f.view("abcd")))
			concurrentRequire(t, unavailable, ConcurrentFail)
		})
	}
}

func TestConcurrentBudgetsTraceCodecAndReplay(t *testing.T) {
	f := concurrentFixtureForTest()
	f.trace.Reads = []ConcurrentRead{f.read("baseline", 10, f.view("abcd"))}
	if got := CheckConcurrentHistory(t.Context(), f.trace, ConcurrentLimits{States: 1}); got.Verdict != ConcurrentInconclusive {
		t.Fatal(got)
	}
	if got := CheckConcurrentHistory(t.Context(), f.trace, ConcurrentLimits{}); got.Verdict != ConcurrentInconclusive {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := CheckConcurrentHistory(ctx, f.trace, ConcurrentLimits{States: 100}); got.Verdict != ConcurrentInconclusive {
		t.Fatal(got)
	}
	data, err := json.Marshal(f.trace)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeConcurrentTrace(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.trace, decoded) {
		t.Fatal("trace changed in replay")
	}
	concurrentRequire(t, decoded, ConcurrentPass)
	for _, bad := range []string{string(data) + "{}", `{"unknown":1}`} {
		if _, err := DecodeConcurrentTrace(strings.NewReader(bad)); err == nil {
			t.Fatal("invalid trace accepted")
		}
	}
	first, err := GenerateConcurrentPlan(42, 6, f.volume, []ConcurrentOrigin{f.a, f.w})
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateConcurrentPlan(42, 6, f.volume, []ConcurrentOrigin{f.a, f.w})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("seed did not replay", err)
	}
	third, err := GenerateConcurrentPlan(43, 6, f.volume, []ConcurrentOrigin{f.a, f.w})
	if err != nil || reflect.DeepEqual(first, third) {
		t.Fatal("seed did not change workload", err)
	}
	copy := f.trace.Clone()
	copy.Baseline.Objects[2].Data[0] = 'X'
	if string(f.trace.Baseline.Objects[2].Data) != "abcd" {
		t.Fatal("borrowed trace bytes")
	}
}

func TestConcurrentOfflineMinimizationPreservesCallsAndRealFailure(t *testing.T) {
	f := concurrentFixtureForTest()
	call := f.call("append", "append", f.w, 10, 20)
	call.Intent.Data = []byte("Z")
	call.Reply.Count = 1
	call.Reply.Offset = 4
	f.trace.Calls = []ConcurrentCall{call}
	f.trace.Reads = []ConcurrentRead{f.read("valid", 30, f.view("abcdZ", call.Identity)), f.read("stale", 40, f.view("abcd"))}
	concurrentRequire(t, f.trace, ConcurrentFail)
	reduced, attempts, err := MinimizeConcurrentTrace(t.Context(), f.trace, ConcurrentLimits{States: 10000}, 16, nil)
	if err != nil || attempts > 16 || len(reduced.Calls) != 1 || len(reduced.Reads) != 1 || reduced.Reads[0].ID != "stale" {
		t.Fatal(reduced, attempts, err)
	}
	concurrentRequire(t, reduced, ConcurrentFail)
}

func TestConcurrentPlanRequiresDistinctOriginLifetimes(t *testing.T) {
	f := concurrentFixtureForTest()
	for _, test := range []struct {
		name    string
		origins []ConcurrentOrigin
		wantErr bool
	}{
		{"same lifetime", []ConcurrentOrigin{f.a, f.a}, true},
		{"duplicate among distinct origins", []ConcurrentOrigin{f.a, f.w, f.a}, true},
		{"invalid lifetime", []ConcurrentOrigin{f.a, {}}, true},
		{"distinct nodes", []ConcurrentOrigin{f.a, f.w}, false},
		{"distinct incarnations", []ConcurrentOrigin{f.a, {Node: f.a.Node, Incarnation: f.b.Incarnation}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, err := GenerateConcurrentPlan(42, 2, f.volume, test.origins)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, want error %v", err, test.wantErr)
			}
			if test.wantErr {
				if len(plan.Calls) != 0 {
					t.Fatal("invalid origins produced a partial workload")
				}
				return
			}
			if len(plan.Calls) != 2 || plan.Calls[0].Origin != test.origins[0] || plan.Calls[1].Origin != test.origins[1] || plan.Calls[0].Origin == plan.Calls[1].Origin {
				t.Fatal("workload did not retain distinct origin lifetimes", plan)
			}
		})
	}
}

func TestConcurrentShortWritesAndKnownPendingClaim(t *testing.T) {
	f := concurrentFixtureForTest()
	call := f.call("short", "append", f.w, 10, 20)
	call.Intent.Data = []byte("XYZ")
	call.Reply.Count = 1
	call.Reply.Offset = 4
	f.trace.Calls = []ConcurrentCall{call}
	f.trace.Reads = []ConcurrentRead{f.read("range", 30, f.view("abcdX", call.Identity))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Reads[0].View = f.view("abcdXYZ", call.Identity)
	concurrentRequire(t, bad, ConcurrentFail)
	// Unknown short writes retain the accepted range; a wait error says nothing
	// about whether the whole input, a prefix, or no bytes were committed.
	call.Reply = nil
	call.Returned = 0
	call.WaitUntil = 12
	f.trace.Calls = []ConcurrentCall{call}
	concurrentRequire(t, f.trace, ConcurrentPass)
	// PENDING is itself known claim evidence, even without a separate claim probe.
	call.Returned = 12
	call.Reply = &ConcurrentResult{Status: "pending", Code: "claimed"}
	retry := call
	retry.Attempt = "changed pending retry"
	retry.Origin = f.b
	retry.Invoked = 20
	retry.WaitUntil = 0
	retry.Returned = 25
	retry.Intent.Data = []byte("other")
	retry.Reply = &ConcurrentResult{Status: "conflict", Code: "id_conflict", Origin: f.w}
	f.trace.Calls = []ConcurrentCall{call, retry}
	f.trace.Reads = []ConcurrentRead{f.read("pending", 30, f.view("abcd"))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	retry.Reply = &ConcurrentResult{Status: "committed", Code: "ok", Origin: f.b, Object: f.file, Count: 5, Offset: 4}
	f.trace.Calls[1] = retry
	f.trace.Reads[0].View = f.view("abcdother", call.Identity)
	concurrentRequire(t, f.trace, ConcurrentFail)
}

func TestConcurrentCreateGroupPolicyAndDirectoryClosureMove(t *testing.T) {
	f := concurrentFixtureForTest()
	create := f.call("create", "create", f.w, 10, 20)
	create.Intent.Source.Name = "new"
	create.Intent.Mode = 0o666
	create.Intent.Umask = 0o027
	created := fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "new")}
	create.Reply.Object = created
	view := f.view("abcd", create.Identity)
	view.Names = append(view.Names, ConcurrentName{create.Intent.Source, created})
	view.Objects = append(view.Objects, ConcurrentObject{ID: created, Kind: fsapi.Regular, Mode: 0o640, UID: 1000, GID: 2000, Nlink: 1})
	f.trace.Calls = []ConcurrentCall{create}
	f.trace.Reads = []ConcurrentRead{f.read("created", 30, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Reads[0].View.Objects[len(view.Objects)-1].GID = 1000
	concurrentRequire(t, bad, ConcurrentFail)
	// Concurrent exclusive creates cannot both allocate the same name.
	second := create
	second.Attempt = "second create"
	second.Identity.Operation = UUID(77, "secondcreate")
	second.Origin = f.b
	second.Invoked = 11
	second.Returned = 25
	second.Reply = &ConcurrentResult{Status: "rejected", Code: "exists", Origin: f.b}
	f.trace.Calls = append(f.trace.Calls, second)
	concurrentRequire(t, f.trace, ConcurrentPass)
	// Move a populated directory to another view while retaining every child's
	// home/identity and correcting the logical parent and root link counts.
	directory := fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "directory")}
	f.trace.Baseline.Names = append(f.trace.Baseline.Names, ConcurrentName{ConcurrentPath{f.volume, "dir"}, directory})
	for i := range f.trace.Baseline.Names {
		if f.trace.Baseline.Names[i].Object == f.file {
			f.trace.Baseline.Names[i].Path.Name = "dir/file"
		}
	}
	for i := range f.trace.Baseline.Objects {
		if f.trace.Baseline.Objects[i].ID == f.root {
			f.trace.Baseline.Objects[i].Nlink = 3
		}
	}
	f.trace.Baseline.Objects = append(f.trace.Baseline.Objects, ConcurrentObject{ID: directory, Kind: fsapi.Directory, Mode: 0o700, UID: 1000, GID: 2000, Nlink: 2, Parent: f.root})
	rename := f.call("directory move", "rename", f.w, 10, 20)
	rename.Intent.Source.Name = "dir"
	rename.Intent.Destination = ConcurrentPath{f.other, "moved"}
	rename.Intent.Transaction = true
	rename.Reply.Object = directory
	view = cloneConcurrentView(f.trace.Baseline)
	view.Cut = []ConcurrentIdentity{rename.Identity}
	for i := range view.Names {
		if view.Names[i].Path.Name == "dir" {
			view.Names[i].Path = rename.Intent.Destination
		}
		if view.Names[i].Path.Name == "dir/file" {
			view.Names[i].Path = ConcurrentPath{f.other, "moved/file"}
		}
	}
	for i := range view.Objects {
		switch view.Objects[i].ID {
		case directory:
			view.Objects[i].Parent = f.otherRoot
		case f.root:
			view.Objects[i].Nlink = 2
		case f.otherRoot:
			view.Objects[i].Nlink = 3
		}
	}
	f.trace.Calls = []ConcurrentCall{rename}
	f.trace.Reads = []ConcurrentRead{f.read("directory closure", 30, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad = f.trace.Clone()
	for i := range bad.Reads[0].View.Objects {
		if bad.Reads[0].View.Objects[i].ID == directory {
			bad.Reads[0].View.Objects[i].Mode = 0o777
		}
	}
	concurrentRequire(t, bad, ConcurrentFail)
}

func TestConcurrentUnlinkedPinnedHomeAndRejectedBarriers(t *testing.T) {
	f := concurrentFixtureForTest()
	unlink := f.call("unlink", "unlink", f.w, 10, 20)
	view := f.view("abcd", unlink.Identity)
	view.Names = slices.DeleteFunc(view.Names, func(name ConcurrentName) bool { return name.Object == f.file })
	for i := range view.Objects {
		if view.Objects[i].ID == f.file {
			view.Objects[i].Nlink = 0
		}
	}
	f.trace.Calls = []ConcurrentCall{unlink}
	f.trace.Reads = []ConcurrentRead{f.read("open unlinked", 30, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Reads[0].View.Objects = slices.DeleteFunc(bad.Reads[0].View.Objects, func(o ConcurrentObject) bool { return o.ID == f.file })
	concurrentRequire(t, bad, ConcurrentFail)
	barrier := f.call("barrier", "barrier", f.a, 21, 25)
	barrier.Reply.Object = fsapi.ObjectID{}
	f.trace.Calls = append(f.trace.Calls, barrier)
	concurrentRequire(t, f.trace, ConcurrentPass)
	// A barrier cannot manufacture state or turn a lost source into stale success.
	f.trace.Reads[0].View = f.view("abcd")
	concurrentRequire(t, f.trace, ConcurrentFail)
}
