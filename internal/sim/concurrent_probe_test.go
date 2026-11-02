package sim

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestConcurrentGranularPOSIXCanaryProfileAndReplay(t *testing.T) {
	f := concurrentFixtureForTest()
	data := bytes.Repeat([]byte("x"), 4<<20)
	for i := range f.trace.Baseline.Objects {
		if f.trace.Baseline.Objects[i].ID == f.file {
			f.trace.Baseline.Objects[i].Data = data
			f.trace.Baseline.Objects[i].Size = int64(len(data))
			f.trace.Baseline.Objects[i].Mode = 0o640
		}
	}
	call := f.call("canary tail", "write", f.w, 10, 20)
	call.Intent.Offset = int64(len(data) - 2)
	call.Intent.Data = []byte("XY")
	call.Reply.Offset = call.Intent.Offset
	call.Reply.Count = 2
	f.trace.Calls = []ConcurrentCall{call}
	read := ConcurrentRead{ID: "read tail", Origin: f.b, Invoked: 30, Returned: 31, Probe: &ConcurrentProbe{Kind: "read", Path: ConcurrentPath{f.volume, "file"}, Cred: fsapi.Cred{UID: 2000, GID: 1000}, Offset: int64(len(data) - 2), Length: 64, Code: "ok", Data: []byte("XY")}}
	stat := ConcurrentRead{ID: "stat size", Origin: f.a, Invoked: 32, Returned: 33, Probe: &ConcurrentProbe{Kind: "getattr", Path: ConcurrentPath{f.volume, "file"}, Cred: fsapi.Cred{UID: 2000, GID: 1000}, Code: "ok", Fields: ConcurrentSizeField | ConcurrentModeField, Observed: ConcurrentObject{Size: int64(len(data)), Mode: 0o640}}}
	access := ConcurrentRead{ID: "access denied", Origin: f.w, Invoked: 34, Returned: 35, Probe: &ConcurrentProbe{Kind: "access", Path: ConcurrentPath{f.volume, "file"}, Cred: fsapi.Cred{UID: 2000, GID: 1000}, Access: 2, Code: "permission"}}
	f.trace.Reads = []ConcurrentRead{read, stat, access}
	concurrentRequire(t, f.trace, ConcurrentPass)
	var encoded bytes.Buffer
	if err := EncodeConcurrentTrace(&encoded, f.trace); err != nil {
		t.Fatal(err)
	}
	if encoded.Len() <= 4<<20 {
		t.Fatal("did not exercise baseline larger than the old encoded cap")
	}
	replay, err := DecodeConcurrentTrace(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	concurrentRequire(t, replay, ConcurrentPass)
	for _, kind := range []string{"stale data", "wrong stat size", "stale permission", "wrong offset", "uncaptured metadata"} {
		t.Run(kind, func(t *testing.T) {
			bad := f.trace.Clone()
			switch kind {
			case "stale data":
				bad.Reads[0].Probe.Data = []byte("xx")
			case "wrong stat size":
				bad.Reads[1].Probe.Observed.Size--
			case "stale permission":
				bad.Reads[2].Probe.Code = "ok"
			case "wrong offset":
				bad.Reads[0].Probe.Offset--
			case "uncaptured metadata":
				bad.Reads[1].Probe.Observed.UID = 777
				concurrentRequire(t, bad, ConcurrentInconclusive)
				return
			}
			concurrentRequire(t, bad, ConcurrentFail)
		})
	}
}

func TestConcurrentGranularReadsCanStraddlePublicationAndHandles(t *testing.T) {
	f := concurrentFixtureForTest()
	chmod := f.call("revoke", "chmod", f.w, 10, 20)
	chmod.Intent.Mode = 0o000
	first := ConcurrentRead{ID: "before", Origin: f.a, Invoked: 5, Returned: 6, Probe: &ConcurrentProbe{Kind: "read", Path: ConcurrentPath{f.volume, "file"}, Cred: fsapi.Cred{UID: 1000, GID: 1000}, Length: 2, Code: "ok", Data: []byte("ab")}}
	second := ConcurrentRead{ID: "after", Origin: f.b, Invoked: 30, Returned: 31, Probe: &ConcurrentProbe{Kind: "access", Path: ConcurrentPath{f.volume, "file"}, Cred: fsapi.Cred{UID: 1000, GID: 1000}, Access: 4, Code: "permission"}}
	// An already-open pinned descriptor retains its original granted access.
	handle := ConcurrentRead{ID: "descriptor", Origin: f.w, Invoked: 32, Returned: 33, Probe: &ConcurrentProbe{Kind: "read", Object: f.file, Cred: fsapi.Cred{UID: 1000, GID: 1000}, HandleAccess: 4, Length: 4, Code: "ok", Data: []byte("abcd")}}
	f.trace.Calls = []ConcurrentCall{chmod}
	f.trace.Reads = []ConcurrentRead{first, second, handle}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Reads[1].Probe.Code = "ok"
	concurrentRequire(t, bad, ConcurrentFail)
	bad = f.trace.Clone()
	bad.Reads[2].Probe.HandleAccess = 0
	concurrentRequire(t, bad, ConcurrentFail)
	// EOF is a real zero-byte result, not a successful unknown-length observation.
	eof := handle
	eof.ID = "eof"
	eof.Invoked = 34
	eof.Returned = 35
	p := *handle.Probe
	eof.Probe = &p
	eof.Probe.Offset = 4
	eof.Probe.Data = nil
	f.trace.Reads = append(f.trace.Reads, eof)
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad = f.trace.Clone()
	bad.Reads[3].Probe.Offset = 0
	concurrentRequire(t, bad, ConcurrentFail)
}

func TestConcurrentGranularClosureReaddirAndLookup(t *testing.T) {
	f := concurrentFixtureForTest()
	rename := f.call("move", "rename", f.w, 10, 20)
	rename.Intent.Transaction = true
	rename.Intent.Destination = ConcurrentPath{f.other, "moved"}
	old := ConcurrentRead{ID: "old entry", Origin: f.a, Invoked: 21, Returned: 22, Probe: &ConcurrentProbe{Kind: "lookup", Path: rename.Intent.Source, Code: "not_found"}}
	next := ConcurrentRead{ID: "new entry", Origin: f.b, Invoked: 23, Returned: 24, Probe: &ConcurrentProbe{Kind: "lookup", Path: rename.Intent.Destination, Code: "ok", Fields: ConcurrentIdentityField | ConcurrentNlinkField, Observed: ConcurrentObject{ID: f.file, Nlink: 1}}}
	listing := ConcurrentRead{ID: "new root", Origin: f.w, Invoked: 25, Returned: 26, Probe: &ConcurrentProbe{Kind: "readdir", Path: ConcurrentPath{f.other, "."}, Code: "ok", Names: []ConcurrentName{{rename.Intent.Destination, f.file}}}}
	f.trace.Calls = []ConcurrentCall{rename}
	f.trace.Reads = []ConcurrentRead{old, next, listing}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Reads[2].Probe.Names = nil
	concurrentRequire(t, bad, ConcurrentFail)
	bad = f.trace.Clone()
	bad.Reads[0].Probe.Code = "ok"
	concurrentRequire(t, bad, ConcurrentFail)
}

func TestConcurrentAnchoredStableReceiptAndUnknownAllocation(t *testing.T) {
	f := concurrentFixtureForTest()
	call := f.call("anchor", "append", f.w, 10, 20)
	call.Identity = ConcurrentIdentity{Principal: "alice", Anchor: f.other, Session: UUID(77, "session"), Number: 1}
	call.Intent.Data = []byte("Z")
	call.Reply.Count = 1
	call.Reply.Offset = 4
	call.Reply.StableResult = []byte("canonical decision/result bytes")
	retry := call
	retry.Attempt = "other origin retry"
	retry.Origin = f.b
	retry.Invoked = 21
	retry.Returned = 22
	f.trace.Calls = []ConcurrentCall{call, retry}
	f.trace.Reads = []ConcurrentRead{f.read("receipt", 30, f.view("abcdZ", call.Identity))}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Calls[1].Reply.StableResult = []byte("different decision with same semantic projection")
	concurrentRequire(t, bad, ConcurrentFail)
	bad = f.trace.Clone()
	bad.Calls[1].Reply.StableResult = nil
	concurrentRequire(t, bad, ConcurrentInconclusive)
	// An original lost response cannot erase later known canonical receipt bytes.
	f.trace.Calls[0].Reply = nil
	f.trace.Calls[0].Returned = 0
	f.trace.Calls[0].WaitUntil = 12
	f.trace.Calls[0].ClaimedAt = 11
	concurrentRequire(t, f.trace, ConcurrentPass)
	knownAgain := retry
	knownAgain.Attempt = "changed receipt"
	knownAgain.Invoked = 23
	knownAgain.Returned = 24
	value := *retry.Reply
	value.StableResult = []byte("wrong result")
	knownAgain.Reply = &value
	f.trace.Calls = append(f.trace.Calls, knownAgain)
	concurrentRequire(t, f.trace, ConcurrentFail)
	// A lost-create allocation may only be observed in a later unlink response;
	// it must not be invented as absent because the final namespace is empty.
	f = concurrentFixtureForTest()
	created := fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "unknown allocation")}
	create := f.call("lost create", "create", f.w, 10, 0)
	create.Intent.Source.Name = "new"
	create.Intent.Mode = 0o600
	create.Reply = nil
	create.WaitUntil = 12
	unlink := f.call("unlink new", "unlink", f.b, 20, 25)
	unlink.Intent.Source.Name = "new"
	unlink.Reply.Object = created
	f.trace.Calls = []ConcurrentCall{create, unlink}
	f.trace.Reads = []ConcurrentRead{f.read("removed", 30, f.view("abcd", create.Identity, unlink.Identity))}
	concurrentRequire(t, f.trace, ConcurrentPass)
}

func TestConcurrentLosslessNamespaceCodecAndUnsupportedInput(t *testing.T) {
	f := concurrentFixtureForTest()
	f.trace.Reads = []ConcurrentRead{f.read("baseline", 10, f.view("abcd"))}
	bad := f.trace.Clone()
	bad.Baseline.Names[2].Path.Name = string([]byte{'b', 0xff})
	concurrentRequire(t, bad, ConcurrentInconclusive)
	if err := EncodeConcurrentTrace(io.Discard, bad); err == nil {
		t.Fatal("invalid UTF-8 was silently replaced")
	}
	for _, raw := range [][]byte{[]byte("{\"Version\":1,\"unknown\":\"\xff\"}"), []byte(`{"Version":1,"Seed":"\ud800"}`), []byte(`{"Version":1,"Seed":"\udc00"}`)} {
		if _, err := DecodeConcurrentTrace(bytes.NewReader(raw)); err == nil {
			t.Fatal("lossy JSON accepted")
		}
	}
	encoded, err := json.Marshal(f.trace)
	if err != nil {
		t.Fatal(err)
	}
	validPair := bytes.ReplaceAll(encoded, []byte(`"file"`), []byte(`"\ud83d\ude00"`))
	trace, err := DecodeConcurrentTrace(bytes.NewReader(validPair))
	if err != nil {
		t.Fatal("valid surrogate pair rejected", err)
	}
	concurrentRequire(t, trace, ConcurrentPass)
	// Valid Unicode normalization variants remain distinct names.
	call := f.call("composed", "create", f.a, 10, 20)
	call.Intent.Source.Name = "é"
	call.Intent.Mode = 0o600
	call.Reply.Object = fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "composed")}
	other := f.call("decomposed", "create", f.w, 11, 21)
	other.Intent.Source.Name = "e\u0301"
	other.Intent.Mode = 0o600
	other.Reply.Object = fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "decomposed")}
	view := f.view("abcd", call.Identity, other.Identity)
	for _, c := range []ConcurrentCall{call, other} {
		view.Names = append(view.Names, ConcurrentName{c.Intent.Source, c.Reply.Object})
		view.Objects = append(view.Objects, ConcurrentObject{ID: c.Reply.Object, Kind: fsapi.Regular, Mode: 0o600, UID: 1000, GID: 2000, Nlink: 1})
	}
	f.trace.Calls = []ConcurrentCall{call, other}
	f.trace.Reads = []ConcurrentRead{f.read("distinct bytes", 30, view)}
	concurrentRequire(t, f.trace, ConcurrentPass)
	writer := concurrentWriter{destination: io.Discard, remaining: 1}
	if _, err := writer.Write([]byte("ab")); err == nil {
		t.Fatal("writer exceeded byte bound")
	}
}

func TestConcurrentFreshMinimizationPreservesInputs(t *testing.T) {
	f := concurrentFixtureForTest()
	barrier := f.call("barrier", "barrier", f.a, 10, 20)
	barrier.Reply.Object = fsapi.ObjectID{}
	f.trace.Calls = []ConcurrentCall{barrier}
	f.trace.Reads = []ConcurrentRead{f.read("bad data", 30, f.view("wrong"))}
	concurrentRequire(t, f.trace, ConcurrentFail)
	// These are actual oracle executions of fresh evidence for a no-effect
	// barrier workload; input mutation/incomplete reproduction must not qualify.
	reproduced := func(_ context.Context, candidate ConcurrentTrace) (ConcurrentTrace, error) {
		candidate.Calls = slices.Clone(candidate.Calls)
		return candidate, nil
	}
	reduced, used, err := MinimizeConcurrentTrace(t.Context(), f.trace, ConcurrentLimits{States: 10000}, 8, reproduced)
	if err != nil || used > 8 || len(reduced.Calls) != 0 || len(reduced.Reads) != 1 {
		t.Fatal(reduced, used, err)
	}
	concurrentRequire(t, reduced, ConcurrentFail)
	for _, mutation := range []string{"baseline", "seed", "fault", "workload", "incomplete", "script error"} {
		t.Run(mutation, func(t *testing.T) {
			result, _, err := MinimizeConcurrentTrace(t.Context(), f.trace, ConcurrentLimits{States: 10000}, 1, func(_ context.Context, candidate ConcurrentTrace) (ConcurrentTrace, error) {
				switch mutation {
				case "baseline":
					candidate.Baseline.Objects[0].UID++
				case "seed":
					candidate.Seed++
				case "fault":
					candidate.Faults = append(candidate.Faults, ConcurrentFault{At: 1, Kind: "new", Origin: f.a})
				case "workload":
					candidate.Reads[0].ID = "unrelated"
				case "incomplete":
					candidate.Reads = nil
				case "script error":
					return candidate, errors.New("invalid reduced script")
				}
				return candidate, nil
			})
			if err != nil || len(result.Calls) != 1 {
				t.Fatal(result, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := MinimizeConcurrentTrace(ctx, f.trace, ConcurrentLimits{States: 1000}, 1, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Unknown captured-field bits and malformed per-attempt deadlines stay unsupported.
	bad := f.trace.Clone()
	bad.Calls[0].WaitUntil = 1
	concurrentRequire(t, bad, ConcurrentInconclusive)
	if !strings.Contains(CheckConcurrentHistory(t.Context(), bad, ConcurrentLimits{States: 100}).Reason, "deadline") {
		t.Fatal("deadline validation weakened")
	}
}

func TestConcurrentUnknownCreateGranularAllocationDiscovery(t *testing.T) {
	for _, kind := range []string{"lookup", "object getattr", "readdir"} {
		t.Run(kind, func(t *testing.T) {
			f := concurrentFixtureForTest()
			created := fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "probe-only allocation")}
			create := f.call("lost create", "create", f.w, 10, 0)
			create.Intent.Source.Name = "new"
			create.Intent.Mode = 0o600
			create.Reply = nil
			create.WaitUntil = 12
			probe := ConcurrentProbe{Kind: "lookup", Path: create.Intent.Source, Code: "ok", Fields: ConcurrentIdentityField | ConcurrentModeField | ConcurrentUIDField | ConcurrentGIDField | ConcurrentSizeField, Observed: ConcurrentObject{ID: created, Mode: 0o600, UID: 1000, GID: 2000}}
			switch kind {
			case "object getattr":
				probe = ConcurrentProbe{Kind: "getattr", Object: created, Code: "ok", Fields: ConcurrentModeField | ConcurrentSizeField, Observed: ConcurrentObject{Mode: 0o600}}
			case "readdir":
				probe = ConcurrentProbe{Kind: "readdir", Path: ConcurrentPath{f.volume, "."}, Code: "ok", Names: []ConcurrentName{{ConcurrentPath{f.volume, "file"}, f.file}, {create.Intent.Source, created}}}
			}
			f.trace.Calls = []ConcurrentCall{create}
			f.trace.Reads = []ConcurrentRead{{ID: "allocation captured", Origin: f.b, Invoked: 20, Returned: 21, Probe: &probe}}
			concurrentRequire(t, f.trace, ConcurrentPass)
			bad := f.trace.Clone()
			if kind == "readdir" {
				bad.Reads[0].Probe.Names[0].Object = created
			} else {
				bad.Reads[0].Probe.Observed.Mode = 0o777
			}
			concurrentRequire(t, bad, ConcurrentFail)
			bad = f.trace.Clone()
			bad.Reads[0].Probe.Fields |= ConcurrentFields(1 << 15)
			concurrentRequire(t, bad, ConcurrentInconclusive)
		})
	}
}

func TestConcurrentUnknownCreateWithoutCapturedAllocation(t *testing.T) {
	f := concurrentFixtureForTest()
	create := f.call("lost create", "create", f.w, 10, 0)
	create.Intent.Source.Name = "new"
	create.Intent.Mode = 0o600
	create.Reply = nil
	create.WaitUntil = 12
	second := f.call("exclusive create", "create", f.b, 30, 40)
	second.Intent.Source = create.Intent.Source
	second.Intent.Mode = 0o600
	second.Reply = &ConcurrentResult{Status: "rejected", Code: "exists", Origin: f.b}
	f.trace.Calls = []ConcurrentCall{create, second}
	// Neither response exposes the first allocation. Its existence and metadata
	// still constrain later operations; omitting the unknown create cannot pass.
	f.trace.Reads = []ConcurrentRead{{ID: "metadata only", Origin: f.a, Invoked: 20, Returned: 21, Probe: &ConcurrentProbe{Kind: "lookup", Path: create.Intent.Source, Code: "ok", Fields: ConcurrentModeField | ConcurrentUIDField | ConcurrentGIDField, Observed: ConcurrentObject{Mode: 0o600, UID: 1000, GID: 2000}}}}
	concurrentRequire(t, f.trace, ConcurrentPass)
	bad := f.trace.Clone()
	bad.Reads[0].Probe.Observed.Mode = 0o777
	concurrentRequire(t, bad, ConcurrentFail)
	bad = f.trace.Clone()
	bad.Calls[1].Reply = &ConcurrentResult{Status: "committed", Code: "ok", Origin: f.b, Object: fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "duplicate create")}}
	concurrentRequire(t, bad, ConcurrentFail)
	// A future immutable object precondition can be the only identity evidence.
	expected := fsapi.ObjectID{Home: f.volume, Allocation: UUID(77, "expected unknown allocation")}
	chmod := f.call("chmod new", "chmod", f.b, 30, 40)
	chmod.Intent.Source = create.Intent.Source
	chmod.Intent.Mode = 0o400
	chmod.Intent.Expected = &expected
	chmod.Intent.Cred = fsapi.Cred{UID: 2000, GID: 2000}
	chmod.Reply = &ConcurrentResult{Status: "rejected", Code: "permission", Origin: f.b}
	f.trace.Calls[1] = chmod
	concurrentRequire(t, f.trace, ConcurrentPass)
}
