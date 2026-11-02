package sim

import (
	"bytes"
	"fmt"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type concurrentClaim struct {
	Intent      ConcurrentIntent
	Origin      ConcurrentOrigin
	Result      *ConcurrentResult
	StableKnown bool
}
type concurrentState struct {
	names   map[ConcurrentPath]fsapi.ObjectID
	objects map[fsapi.ObjectID]ConcurrentObject
	claims  map[ConcurrentIdentity]concurrentClaim
	cut     []ConcurrentIdentity
}

func newConcurrentState(view ConcurrentView) (concurrentState, error) {
	state := concurrentState{names: make(map[ConcurrentPath]fsapi.ObjectID), objects: make(map[fsapi.ObjectID]ConcurrentObject), claims: make(map[ConcurrentIdentity]concurrentClaim)}
	for _, name := range view.Names {
		if _, ok := state.names[name.Path]; ok {
			return state, fmt.Errorf("duplicate name")
		}
		state.names[name.Path] = name.Object
	}
	for _, object := range view.Objects {
		if _, ok := state.objects[object.ID]; ok {
			return state, fmt.Errorf("duplicate object")
		}
		object.Data = slices.Clone(object.Data)
		state.objects[object.ID] = object
	}
	return state, nil
}

func cloneConcurrentState(s concurrentState) concurrentState {
	s.names = maps.Clone(s.names)
	s.objects = maps.Clone(s.objects)
	s.claims = maps.Clone(s.claims)
	s.cut = slices.Clone(s.cut)
	// Payloads and claim results are immutable; writes always allocate new bytes.
	return s
}

func cloneConcurrentView(v ConcurrentView) ConcurrentView {
	v.Cut = slices.Clone(v.Cut)
	v.Names = slices.Clone(v.Names)
	v.Objects = slices.Clone(v.Objects)
	for i := range v.Objects {
		v.Objects[i].Data = slices.Clone(v.Objects[i].Data)
	}
	return v
}

func concurrentParent(p ConcurrentPath) ConcurrentPath { p.Name = path.Dir(p.Name); return p }

func validConcurrentPath(p ConcurrentPath) bool {
	if !p.Volume.Valid() || len(p.Name) == 0 || len(p.Name) > 1024 || strings.ContainsRune(p.Name, '\x00') || strings.HasPrefix(p.Name, "/") {
		return false
	}
	if p.Name == "." {
		return true
	}
	if path.Clean(p.Name) != p.Name {
		return false
	}
	for _, component := range strings.Split(p.Name, "/") {
		if component == ".." || len(component) > 255 {
			return false
		}
	}
	return !strings.HasPrefix(p.Name, "../")
}

func sameConcurrentIntent(a, b ConcurrentIntent) bool {
	return a.Kind == b.Kind && a.Source == b.Source && a.Destination == b.Destination && bytes.Equal(a.Data, b.Data) && a.Offset == b.Offset && a.Size == b.Size && a.Mode == b.Mode && a.Umask == b.Umask && a.Transaction == b.Transaction && a.NoReplace == b.NoReplace && reflect.DeepEqual(a.Expected, b.Expected)
}

func concurrentPermission(o ConcurrentObject, c fsapi.Cred, bits uint16) bool {
	if c.UID == 0 {
		return true
	}
	shift := uint(0)
	if c.UID == o.UID {
		shift = 6
	} else if c.GID == o.GID || slices.Contains(c.Groups, o.GID) {
		shift = 3
	}
	return (o.Mode>>shift)&bits == bits
}

func concurrentTraverse(s concurrentState, p ConcurrentPath, c fsapi.Cred) string {
	if p.Name == "." {
		return ""
	}
	current := ConcurrentPath{Volume: p.Volume, Name: "."}
	parts := strings.Split(p.Name, "/")
	for index := 0; index < len(parts); index++ {
		id, ok := s.names[current]
		if !ok {
			return "not_found"
		}
		object := s.objects[id]
		if object.Kind != fsapi.Directory {
			return "type"
		}
		if !concurrentPermission(object, c, 1) {
			return "permission"
		}
		if index < len(parts)-1 {
			if current.Name == "." {
				current.Name = parts[index]
			} else {
				current.Name += "/" + parts[index]
			}
		}
	}
	return ""
}

func concurrentMutableParent(s concurrentState, p ConcurrentPath, c fsapi.Cred) string {
	if code := concurrentTraverse(s, p, c); code != "" {
		return code
	}
	id, ok := s.names[concurrentParent(p)]
	if !ok {
		return "not_found"
	}
	if !concurrentPermission(s.objects[id], c, 3) {
		return "permission"
	}
	return ""
}

func concurrentSticky(s concurrentState, p ConcurrentPath, c fsapi.Cred) bool {
	parent := s.objects[s.names[concurrentParent(p)]]
	child := s.objects[s.names[p]]
	return parent.Mode&0o1000 == 0 || c.UID == 0 || c.UID == parent.UID || c.UID == child.UID
}

// concurrentTransition is a small independent Unix model. It consumes only
// immutable high-level input and a fresh allocation choice, never redo/apply code.
func concurrentTransition(s *concurrentState, i ConcurrentIntent, allocation fsapi.ObjectID, count int64) ConcurrentResult {
	result := ConcurrentResult{Status: "rejected", Code: "ok"}
	if i.Kind == "barrier" {
		result.Status = "committed"
		return result
	}
	if i.Source.Name == "." {
		result.Code = "type"
		return result
	}
	if code := concurrentTraverse(*s, i.Source, i.Cred); code != "" {
		result.Code = code
		return result
	}
	id, exists := s.names[i.Source]
	object := s.objects[id]
	if i.Expected != nil && (!exists || *i.Expected != id) {
		result.Code = "stale"
		return result
	}
	if i.Kind == "create" || i.Kind == "mkdir" {
		return concurrentCreate(s, i, allocation)
	}
	if !exists {
		result.Code = "not_found"
		return result
	}
	switch i.Kind {
	case "append", "write", "truncate":
		if object.Kind != fsapi.Regular {
			result.Code = "type"
			return result
		}
		if !concurrentPermission(object, i.Cred, 2) {
			result.Code = "permission"
			return result
		}
		offset := i.Offset
		if i.Kind == "append" {
			offset = int64(len(object.Data))
		}
		size := int64(len(object.Data))
		if i.Kind == "truncate" {
			size = i.Size
		} else if count > 0 && offset+count > size {
			size = offset + count
		}
		data := make([]byte, int(size))
		copy(data, object.Data)
		if i.Kind != "truncate" {
			if count > 0 {
				copy(data[int(offset):], i.Data[:int(count)])
			}
			result.Count = count
			result.Offset = offset
		}
		object.Data = data
		object.Size = int64(len(data))
		if i.Cred.UID != 0 && (i.Kind == "truncate" || count > 0) {
			object.Mode &^= 0o6000
		}
		s.objects[id] = object
	case "chmod":
		if i.Cred.UID != 0 && i.Cred.UID != object.UID {
			result.Code = "permission"
			return result
		}
		if object.Kind == fsapi.Symlink {
			result.Code = "type"
			return result
		}
		if i.Cred.UID != 0 && (i.Mode&0o2000 != 0 && i.Cred.GID != object.GID && !slices.Contains(i.Cred.Groups, object.GID) || i.Mode&0o1000 != 0 && object.Kind != fsapi.Directory) {
			result.Code = "permission"
			return result
		}
		object.Mode = i.Mode
		s.objects[id] = object
	case "rename", "link":
		if code := concurrentNamespace(s, i, id); code != "" {
			result.Code = code
			return result
		}
	case "unlink":
		if object.Kind == fsapi.Directory {
			result.Code = "type"
			return result
		}
		if code := concurrentMutableParent(*s, i.Source, i.Cred); code != "" {
			result.Code = code
			return result
		}
		if !concurrentSticky(*s, i.Source, i.Cred) {
			result.Code = "permission"
			return result
		}
		delete(s.names, i.Source)
		object.Nlink--
		s.objects[id] = object
	default:
		result.Code = "unsupported"
		return result
	}
	result.Status = "committed"
	result.Object = id
	return result
}

func concurrentCreate(s *concurrentState, i ConcurrentIntent, allocation fsapi.ObjectID) ConcurrentResult {
	result := ConcurrentResult{Status: "rejected", Code: "exists"}
	if _, exists := s.names[i.Source]; exists {
		return result
	}
	if code := concurrentMutableParent(*s, i.Source, i.Cred); code != "" {
		result.Code = code
		return result
	}
	parentID := s.names[concurrentParent(i.Source)]
	parent := s.objects[parentID]
	if i.Cred.UID != 0 && (i.Mode&0o2000 != 0 && i.Cred.GID != parent.GID && !slices.Contains(i.Cred.Groups, parent.GID) || i.Mode&0o1000 != 0 && i.Kind != "mkdir") {
		result.Code = "permission"
		return result
	}
	if !allocation.Valid() || allocation.Home != i.Source.Volume {
		result.Code = "allocation"
		return result
	}
	if _, used := s.objects[allocation]; used {
		result.Code = "allocation"
		return result
	}
	object := ConcurrentObject{ID: allocation, Kind: fsapi.Regular, Mode: i.Mode &^ i.Umask, UID: i.Cred.UID, GID: parent.GID, Nlink: 1}
	if i.Kind == "mkdir" {
		object.Kind = fsapi.Directory
		object.Nlink = 2
		object.Parent = parentID
		if parent.Mode&0o2000 != 0 {
			object.Mode |= 0o2000
		}
		parent.Nlink++
		s.objects[parentID] = parent
	}
	s.names[i.Source] = allocation
	s.objects[allocation] = object
	return ConcurrentResult{Status: "committed", Code: "ok", Object: allocation}
}

func concurrentNamespace(s *concurrentState, i ConcurrentIntent, id fsapi.ObjectID) string {
	if i.Source.Volume != i.Destination.Volume && !i.Transaction {
		return "cross_volume"
	}
	if i.Destination.Name == "." {
		return "type"
	}
	if code := concurrentMutableParent(*s, i.Destination, i.Cred); code != "" {
		return code
	}
	object := s.objects[id]
	old, occupied := s.names[i.Destination]
	if i.Kind == "link" {
		if object.Kind != fsapi.Regular {
			return "type"
		}
		if i.Cred.UID != 0 && i.Cred.UID != object.UID && !concurrentPermission(object, i.Cred, 6) {
			return "permission"
		}
		if occupied {
			return "exists"
		}
		s.names[i.Destination] = id
		object.Nlink++
		s.objects[id] = object
		return ""
	}
	if code := concurrentMutableParent(*s, i.Source, i.Cred); code != "" {
		return code
	}
	if !concurrentSticky(*s, i.Source, i.Cred) || occupied && !concurrentSticky(*s, i.Destination, i.Cred) {
		return "permission"
	}
	if occupied && i.NoReplace {
		return "exists"
	}
	if occupied && old == id {
		return ""
	}
	if object.Kind == fsapi.Directory && object.Parent != s.names[concurrentParent(i.Destination)] && !concurrentPermission(object, i.Cred, 2) {
		return "permission"
	}
	if object.Kind == fsapi.Directory && i.Source.Volume == i.Destination.Volume && strings.HasPrefix(i.Destination.Name, i.Source.Name+"/") {
		return "cycle"
	}
	if occupied {
		previous := s.objects[old]
		if (previous.Kind == fsapi.Directory) != (object.Kind == fsapi.Directory) {
			return "type"
		}
		if previous.Kind == fsapi.Directory {
			for name := range s.names {
				if name.Volume == i.Destination.Volume && strings.HasPrefix(name.Name, i.Destination.Name+"/") {
					return "not_empty"
				}
			}
			previous.Nlink = 0
			concurrentAdjustParent(s, i.Destination, -1)
		} else {
			previous.Nlink--
		}
		s.objects[old] = previous
	}
	delete(s.names, i.Source)
	s.names[i.Destination] = id
	if object.Kind == fsapi.Directory {
		for name, child := range maps.Clone(s.names) {
			if name.Volume == i.Source.Volume && strings.HasPrefix(name.Name, i.Source.Name+"/") {
				delete(s.names, name)
				next := ConcurrentPath{Volume: i.Destination.Volume, Name: i.Destination.Name + strings.TrimPrefix(name.Name, i.Source.Name)}
				s.names[next] = child
			}
		}
		concurrentAdjustParent(s, i.Source, -1)
		concurrentAdjustParent(s, i.Destination, 1)
		object.Parent = s.names[concurrentParent(i.Destination)]
		s.objects[id] = object
	}
	return ""
}

func concurrentAdjustParent(s *concurrentState, p ConcurrentPath, delta int) {
	id := s.names[concurrentParent(p)]
	object := s.objects[id]
	if delta < 0 {
		object.Nlink--
	} else {
		object.Nlink++
	}
	s.objects[id] = object
}

func concurrentViewMatches(state concurrentState, v ConcurrentView, generation fsapi.UUID) bool {
	if v.Generation != generation || v.CutKnown && !slices.Equal(v.Cut, state.cut) {
		return false
	}
	observed, err := newConcurrentState(v)
	if err != nil || !maps.Equal(state.names, observed.names) {
		return false
	}
	// Retained zero-link allocations without a declared pin are not externally
	// observable; the model still remembers them to prohibit allocation reuse.
	expected := make(map[fsapi.ObjectID]ConcurrentObject)
	for id, object := range state.objects {
		if object.Nlink > 0 || object.Pins > 0 {
			expected[id] = object
		}
	}
	return maps.EqualFunc(expected, observed.objects, func(a, b ConcurrentObject) bool {
		return a.ID == b.ID && a.Kind == b.Kind && a.Mode == b.Mode && a.UID == b.UID && a.GID == b.GID && a.Nlink == b.Nlink && a.Size == b.Size && bytes.Equal(a.Data, b.Data) && a.Parent == b.Parent && a.Pins == b.Pins
	})
}
