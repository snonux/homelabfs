package sim

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// ConcurrentFields names exactly the metadata captured by one external syscall.
// Uncaptured fields are never treated as zero-valued expected observations.
type ConcurrentFields uint16

const (
	// ConcurrentIdentityField captures the stable home/allocation identity.
	ConcurrentIdentityField ConcurrentFields = 1 << iota
	// ConcurrentKindField captures the logical object type.
	ConcurrentKindField
	// ConcurrentModeField captures permission and special bits.
	ConcurrentModeField
	// ConcurrentUIDField captures numeric ownership.
	ConcurrentUIDField
	// ConcurrentGIDField captures the inherited/current numeric group.
	ConcurrentGIDField
	// ConcurrentNlinkField captures the object's logical link count.
	ConcurrentNlinkField
	// ConcurrentSizeField captures logical size independently of returned bytes.
	ConcurrentSizeField
	// ConcurrentParentField captures a directory's logical parent identity.
	ConcurrentParentField
	// ConcurrentPinsField captures diagnostic nonexpiring lifetime obligations.
	ConcurrentPinsField
)

// ConcurrentProbe is one granular read, getattr, lookup, access or readdir result.
// Use one ConcurrentRead per actual syscall; separate calls may straddle cuts.
// Path selects a namespace walk, or Object selects an already resolved object.
// HandleAccess is predeclared granted descriptor access and requires Object plus
// a baseline-pinned object; zero performs fresh current-mode authorization.
// Fields explicitly selects captured metadata from Observed. A read compares
// only its returned Data against Offset/Length, including EOF/short reads.
// A readdir compares complete immediate entry names and identities (excluding
// dot/dotdot). Generation/cut are optional evidence, never inferred from absence.
type ConcurrentProbe struct {
	Kind           string
	Path           ConcurrentPath
	Object         fsapi.ObjectID
	Cred           fsapi.Cred
	HandleAccess   uint16
	Offset, Length int64
	Access         uint16
	Fields         ConcurrentFields
	Code           string
	Observed       ConcurrentObject
	Data           []byte
	Names          []ConcurrentName
	Generation     fsapi.UUID
	CutKnown       bool
	Cut            []ConcurrentIdentity
}

func concurrentProbeMatches(state concurrentState, p ConcurrentProbe, generation fsapi.UUID) bool {
	if p.Generation.Valid() && p.Generation != generation || p.CutKnown && !slices.Equal(p.Cut, state.cut) {
		return false
	}
	object, code := concurrentProbeObject(state, p)
	if code == "ok" {
		switch p.Kind {
		case "read":
			if object.Kind != fsapi.Regular {
				code = "type"
			} else if p.HandleAccess&4 == 0 && !concurrentPermission(object, p.Cred, 4) {
				code = "permission"
			}
		case "access":
			if !concurrentPermission(object, p.Cred, p.Access) {
				code = "permission"
			}
		case "readdir":
			if object.Kind != fsapi.Directory {
				code = "type"
			} else if !concurrentPermission(object, p.Cred, 4) {
				code = "permission"
			}
		}
	}
	if code != p.Code {
		return false
	}
	if code != "ok" {
		return p.Fields == 0 && len(p.Data) == 0 && len(p.Names) == 0
	}
	if !concurrentCapturedFieldsMatch(object, p.Observed, p.Fields) {
		return false
	}
	if p.Kind == "read" {
		available := int64(len(object.Data)) - min(p.Offset, int64(len(object.Data)))
		size := min(p.Length, available)
		// A successful read may be short. Its returned range is captured explicitly;
		// a positive-length non-EOF read must make progress in this blocking profile.
		if int64(len(p.Data)) > size || size > 0 && len(p.Data) == 0 {
			return false
		}
		start := min(p.Offset, int64(len(object.Data)))
		if !bytes.Equal(p.Data, object.Data[int(start):int(start)+len(p.Data)]) {
			return false
		}
	}
	if p.Kind == "readdir" {
		expected := make(map[ConcurrentPath]fsapi.ObjectID)
		for name, id := range state.names {
			if name != p.Path && concurrentParent(name) == p.Path {
				expected[name] = id
			}
		}
		actual := make(map[ConcurrentPath]fsapi.ObjectID)
		for _, entry := range p.Names {
			if _, exists := actual[entry.Path]; exists {
				return false
			}
			actual[entry.Path] = entry.Object
		}
		if !maps.Equal(expected, actual) {
			return false
		}
	}
	return true
}

func concurrentProbeObject(state concurrentState, p ConcurrentProbe) (ConcurrentObject, string) {
	if p.Object.Valid() {
		object, ok := state.objects[p.Object]
		if !ok || object.Nlink == 0 && object.Pins == 0 {
			return object, "not_found"
		}
		if p.HandleAccess != 0 && object.Pins == 0 {
			return object, "permission"
		}
		return object, "ok"
	}
	if code := concurrentTraverse(state, p.Path, p.Cred); code != "" {
		return ConcurrentObject{}, code
	}
	id, exists := state.names[p.Path]
	if !exists {
		return ConcurrentObject{}, "not_found"
	}
	return state.objects[id], "ok"
}

func concurrentCapturedFieldsMatch(want, got ConcurrentObject, fields ConcurrentFields) bool {
	return (fields&ConcurrentIdentityField == 0 || want.ID == got.ID) &&
		(fields&ConcurrentKindField == 0 || want.Kind == got.Kind) &&
		(fields&ConcurrentModeField == 0 || want.Mode == got.Mode) &&
		(fields&ConcurrentUIDField == 0 || want.UID == got.UID) &&
		(fields&ConcurrentGIDField == 0 || want.GID == got.GID) &&
		(fields&ConcurrentNlinkField == 0 || want.Nlink == got.Nlink) &&
		(fields&ConcurrentSizeField == 0 || want.Size == got.Size) &&
		(fields&ConcurrentParentField == 0 || want.Parent == got.Parent) &&
		(fields&ConcurrentPinsField == 0 || want.Pins == got.Pins)
}

func validateConcurrentProbe(p ConcurrentProbe, members map[fsapi.UUID]bool) error {
	if !p.Object.Valid() && (!validConcurrentPath(p.Path) || !members[p.Path.Volume]) || p.Object.Valid() && (!members[p.Object.Home] || p.Path != (ConcurrentPath{})) {
		return fmt.Errorf("invalid probe path/object/closure")
	}
	if p.Offset < 0 || p.Length < 0 || p.Offset > concurrentFileBytes || p.Length > concurrentFileBytes-p.Offset || len(p.Data) > concurrentFileBytes || p.Fields&^ConcurrentFields((1<<9)-1) != 0 || len(p.Cred.Groups) > 32 || p.Access > 7 || p.HandleAccess&^uint16(6) != 0 || p.HandleAccess != 0 && !p.Object.Valid() || len(p.Names) > 256 || !p.CutKnown && len(p.Cut) != 0 || len(p.Cut) > 16 {
		return fmt.Errorf("unsupported probe bounds/fields")
	}
	for _, name := range p.Names {
		if !validConcurrentPath(name.Path) || !members[name.Path.Volume] || !name.Object.Valid() || !members[name.Object.Home] {
			return fmt.Errorf("invalid directory entry capture")
		}
	}
	for _, id := range p.Cut {
		if !validConcurrentIdentity(id) {
			return fmt.Errorf("invalid probe cut identity")
		}
	}
	switch p.Kind {
	case "read", "getattr", "lookup", "access", "readdir":
	default:
		return fmt.Errorf("unsupported probe kind")
	}
	switch p.Code {
	case "ok", "permission", "not_found", "type":
	default:
		return fmt.Errorf("unsupported probe result")
	}
	if p.Kind != "read" && len(p.Data) != 0 || p.Kind != "readdir" && len(p.Names) != 0 || p.Kind == "readdir" && p.Object.Valid() || len(p.Observed.Data) != 0 {
		return fmt.Errorf("uncaptured probe data or unsupported object readdir")
	}
	// All metadata fields omitted from a partial capture must be actually absent,
	// so adapters cannot accidentally believe an ignored field was verified.
	zero := ConcurrentObject{}
	if p.Fields&ConcurrentIdentityField != 0 {
		zero.ID = p.Observed.ID
	}
	if p.Fields&ConcurrentKindField != 0 {
		zero.Kind = p.Observed.Kind
	}
	if p.Fields&ConcurrentModeField != 0 {
		zero.Mode = p.Observed.Mode
	}
	if p.Fields&ConcurrentUIDField != 0 {
		zero.UID = p.Observed.UID
	}
	if p.Fields&ConcurrentGIDField != 0 {
		zero.GID = p.Observed.GID
	}
	if p.Fields&ConcurrentNlinkField != 0 {
		zero.Nlink = p.Observed.Nlink
	}
	if p.Fields&ConcurrentSizeField != 0 {
		zero.Size = p.Observed.Size
	}
	if p.Fields&ConcurrentParentField != 0 {
		zero.Parent = p.Observed.Parent
	}
	if p.Fields&ConcurrentPinsField != 0 {
		zero.Pins = p.Observed.Pins
	}
	if !reflect.DeepEqual(zero, p.Observed) {
		return fmt.Errorf("probe contains metadata not declared captured")
	}
	return nil
}

func sameConcurrentResult(a, b ConcurrentResult, stable bool) bool {
	return a.Status == b.Status && a.Code == b.Code && a.Origin == b.Origin && a.Object == b.Object && a.Count == b.Count && a.Offset == b.Offset && (!stable || bytes.Equal(a.StableResult, b.StableResult))
}

func sameConcurrentWorkload(a, b ConcurrentTrace) bool {
	if a.Version != b.Version || a.Seed != b.Seed || a.Generation != b.Generation || !reflect.DeepEqual(a.Members, b.Members) || !reflect.DeepEqual(a.Baseline, b.Baseline) || !reflect.DeepEqual(a.Faults, b.Faults) || len(a.Calls) != len(b.Calls) || len(a.Reads) != len(b.Reads) {
		return false
	}
	for index, x := range a.Calls {
		y := b.Calls[index]
		if x.Attempt != y.Attempt || x.Identity != y.Identity || x.Origin != y.Origin || !sameConcurrentIntent(x.Intent, y.Intent) || !reflect.DeepEqual(x.Intent.Cred, y.Intent.Cred) || x.AbortRequested != y.AbortRequested {
			return false
		}
	}
	for index, x := range a.Reads {
		y := b.Reads[index]
		if x.ID != y.ID || x.Origin != y.Origin || (x.Probe == nil) != (y.Probe == nil) {
			return false
		}
		if x.Probe != nil {
			xp, yp := *x.Probe, *y.Probe
			xp.Code = ""
			yp.Code = ""
			xp.Observed = ConcurrentObject{}
			yp.Observed = ConcurrentObject{}
			xp.Data = nil
			yp.Data = nil
			xp.Names = nil
			yp.Names = nil
			xp.Cut = nil
			yp.Cut = nil
			xp.Generation = fsapi.UUID{}
			yp.Generation = fsapi.UUID{}
			xp.CutKnown = false
			yp.CutKnown = false
			if !reflect.DeepEqual(xp, yp) {
				return false
			}
		}
	}
	return true
}
