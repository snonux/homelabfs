package guestbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/snonux/homelabfs/internal/fsservice"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func (s *Service) result(ctx context.Context, b Binding, out Outcome) (json.RawMessage, error) {
	var e error
	if s.config.Validation != nil {
		if string(out.Publication) != "null" {
			return nil, fsapi.Unavailable
		}
	} else {
		publication, err := Canonical(out.Publication)
		if err != nil || validatePublication(publication) != nil {
			return nil, fsapi.Unavailable
		}
		pub := publication.(map[string]any)
		if text(pub["authority_hash"]) != b.Context.AuthorityHash {
			return nil, fsapi.Stale
		}
		if protectedMutation(b.Call.Intent.Opcode) && (pub["result_ref"] == nil || !uintRange(pub["serial"], 1, ^uint64(0))) {
			return nil, fsapi.Unavailable
		}
	}
	if !out.Result.Code.Valid() || out.Result.Count < 0 || len(out.Result.Data) > 65536 {
		return nil, fsapi.IO
	}
	resultCode := errnoNames()[out.Result.Code]
	if out.DelegatedError != nil {
		if !errors.Is(out.DelegatedError, fsservice.ErrDelegatedFileTooLarge) || b.Call.Intent.Opcode != "WRITE" || out.Result.Code != fsapi.OK || out.Result.Count != 0 || len(out.Result.Data) != 0 || out.Result.Attr != nil || len(out.EntryAttributes) != 0 {
			return nil, fsapi.IO
		}
		resultCode = "FileTooLarge"
	}
	r := map[string]any{"code": resultCode, "count": uint64(out.Result.Count), "data": hex.EncodeToString(out.Result.Data), "attributes": nil, "capabilities": []any{}, "entries": []any{}, "capacity": nil, "publication": json.RawMessage(out.Publication)}
	if out.Result.Code == fsapi.OK && out.DelegatedError == nil {
		op := b.Call.Intent.Opcode
		var a arguments
		if e = json.Unmarshal(b.Call.Intent.Args, &a); e != nil {
			return nil, e
		}
		var ancestors []ancestor
		if oneOf(op, "LOOKUP", "CREATE", "MKDIR", "SYMLINK") {
			parent := a.cap("parent")
			s.mu.Lock()
			g := s.sessions[b.Context.Session].Grants[parent.Token]
			if g != nil {
				ancestors = append(ancestors, g.Ancestors...)
			}
			s.mu.Unlock()
			if g == nil || out.Result.Attr == nil {
				return nil, fsapi.IO
			}
			ancestors = append(ancestors, ancestor{Parent: capObject(parent), Object: out.Result.Attr.Object, ObjectRevision: out.Result.Attr.Revision, EntryRevision: out.EntryRevision, Name: a.raw("name")})
		}
		if oneOf(op, "OPEN", "GETATTR", "TRUNCATE", "SETATTR") {
			cap := a.cap("object")
			s.mu.Lock()
			g := s.sessions[b.Context.Session].Grants[cap.Token]
			if g != nil {
				ancestors = append(ancestors, g.Ancestors...)
			}
			s.mu.Unlock()
		}
		caps := []any{}
		if out.Result.Attr != nil {
			if e = out.Result.Attr.Validate(); e != nil {
				return nil, e
			}
			r["attributes"] = wireAttr(*out.Result.Attr)
		}
		if oneOf(op, "ROOT", "LOOKUP", "CREATE", "MKDIR", "SYMLINK") {
			if out.Result.Attr == nil {
				return nil, fsapi.IO
			}
			kind := "PATH"
			if op == "ROOT" {
				kind = "ROOT"
			}
			cap, e := s.issue(b, *out.Result.Attr, kind, 63, 0, ancestors)
			if e != nil {
				return nil, e
			}
			caps = append(caps, cap)
			if op == "ROOT" {
				domain, e := s.issue(b, *out.Result.Attr, "DOMAIN", 0, 0, nil)
				if e != nil {
					return nil, e
				}
				caps = append(caps, domain)
			}
		}
		if op == "OPEN" || op == "CREATE" {
			if out.Result.Attr == nil || out.Result.Handle == 0 {
				return nil, fsapi.IO
			}
			rights := uint32(32)
			flags := a.flags()
			if flags.Access != fsapi.WriteOnly {
				rights |= 1
			}
			if flags.Access != fsapi.ReadOnly {
				rights |= 2
			}
			cap, e := s.issue(b, *out.Result.Attr, "DESCRIPTION", rights, out.Result.Handle, ancestors)
			if e != nil {
				return nil, e
			}
			caps = append(caps, cap)
		}
		r["capabilities"] = caps
		if op == "READDIR" {
			if len(out.EntryAttributes) != len(out.Result.Entries) {
				return nil, fsapi.IO
			}
			cap := a.cap("object")
			s.mu.Lock()
			parent := s.sessions[b.Context.Session].Grants[cap.Token]
			var prefix []ancestor
			if parent != nil {
				prefix = append(prefix, parent.Ancestors...)
			}
			s.mu.Unlock()
			if parent == nil {
				return nil, fsapi.Stale
			}
			entries := []any{}
			last := ""
			for i, entry := range out.Result.Entries {
				attr := out.EntryAttributes[i]
				if entry.Name <= last || entry.Name == ControlName || entry.Object != attr.Object || attr.Validate() != nil {
					return nil, fsapi.IO
				}
				last = entry.Name
				path := append(append([]ancestor(nil), prefix...), ancestor{Parent: capObject(cap), Object: entry.Object, ObjectRevision: attr.Revision, EntryRevision: entry.Revision, Name: entry.Name})
				g, e := s.issue(b, attr, "PATH", 63, 0, path)
				if e != nil {
					return nil, e
				}
				entries = append(entries, map[string]any{"name": hex.EncodeToString([]byte(entry.Name)), "capability": g, "attributes": wireAttr(attr)})
			}
			r["entries"] = entries
		}
		if out.Result.Capacity != nil {
			c := out.Result.Capacity
			if c.ObjectsFree > c.Objects || c.BytesFree > c.Bytes {
				return nil, fsapi.IO
			}
			r["capacity"] = map[string]any{"objects": c.Objects, "objects_free": c.ObjectsFree, "bytes": c.Bytes, "bytes_free": c.BytesFree}
		}
		s.finishLifetime(b, a)
	} else {
		r["count"] = uint64(0)
		r["data"] = ""
	}
	raw, e := marshal(r)
	if e != nil {
		return nil, e
	}
	r["id"] = digest(raw)
	return marshal(r)
}
func (s *Service) issue(b Binding, a fsapi.Attr, kind string, rights uint32, handle fsapi.OpenHandle, path []ancestor) (Capability, error) {
	if len(path) > 256 {
		return Capability{}, fsapi.NoSpace
	}
	total := 0
	wire := []any{}
	for _, p := range path {
		total += len(p.Name)
		wire = append(wire, []any{[]string{p.Object.Home.String(), p.Object.Allocation.String()}, p.ObjectRevision, p.EntryRevision, hex.EncodeToString([]byte(p.Name))})
	}
	if total > 16384 {
		return Capability{}, fsapi.NameTooLong
	}
	raw, e := marshal(wire)
	if e != nil {
		return Capability{}, e
	}
	cred, e := marshal(b.Call.Credentials)
	if e != nil {
		return Capability{}, e
	}
	token, e := randomToken()
	if e != nil {
		return Capability{}, e
	}
	cap := Capability{Token: token, Kind: kind, Scope: b.Context, Home: a.Object.Home.String(), Allocation: a.Object.Allocation.String(), Rights: rights, Revision: a.Revision, CredentialHash: digest(cred), AncestorsHash: digest(raw)}
	if kind == "DESCRIPTION" {
		id, e := randomUUID()
		if e != nil {
			return Capability{}, e
		}
		cap.Description = &id
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[b.Context.Session]
	if len(ss.Grants) >= s.config.MaxCapabilities {
		return Capability{}, fsapi.NoSpace
	}
	ss.Grants[token] = &grant{Cap: cap, Ancestors: append([]ancestor(nil), path...), Handle: handle}
	return cap, nil
}
func (s *Service) finishLifetime(b Binding, a arguments) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[b.Context.Session]
	switch b.Call.Intent.Opcode {
	case "DESCRIPTION_RELEASE":
		if g := ss.Grants[a.cap("description").Token]; g != nil {
			if s.privateValidation() {
				g.Sequence = a.number("release_sequence")
			}
			g.Released = true
		}
	case "DESCRIPTION_CLOSE":
		if s.privateValidation() {
			ss.CloseSequences[a.cap("description").Token+":"+a.text("process")] = a.number("close_sequence")
		}
	case "PROCESS_EXIT":
		ss.Exited[a.text("process")] = a.number("exit_sequence")
	}
}
func capNeeds(in Intent) int {
	switch in.Opcode {
	case "ROOT", "CREATE":
		return 2
	case "LOOKUP", "MKDIR", "SYMLINK", "OPEN":
		return 1
	case "READDIR":
		var a arguments
		_ = json.Unmarshal(in.Args, &a)
		return int(a.number("limit"))
	}
	return 0
}
func wireAttr(a fsapi.Attr) any {
	return map[string]any{"object_home": a.Object.Home.String(), "object_allocation": a.Object.Allocation.String(), "kind": map[fsapi.Kind]string{fsapi.Regular: "REGULAR", fsapi.Directory: "DIRECTORY", fsapi.Symlink: "SYMLINK"}[a.Kind], "uid": a.UID, "gid": a.GID, "mode": a.Mode, "size": a.Size, "nlink": a.Nlink, "revision": a.Revision, "atime": wireTime(a.Atime), "mtime": wireTime(a.Mtime), "ctime": wireTime(a.Ctime)}
}
func wireTime(t fsapi.Timestamp) any {
	return map[string]any{"seconds": t.Seconds, "nanos": t.Nanoseconds}
}
func errnoNames() map[fsapi.Errno]string {
	return map[fsapi.Errno]string{fsapi.OK: "OK", fsapi.Invalid: "Invalid", fsapi.NotFound: "NotFound", fsapi.Exists: "Exists", fsapi.Permission: "Permission", fsapi.ReadOnlyFS: "ReadOnlyFS", fsapi.NotDirectory: "NotDirectory", fsapi.IsDirectory: "IsDirectory", fsapi.NotEmpty: "NotEmpty", fsapi.CrossDevice: "CrossDevice", fsapi.Unsupported: "Unsupported", fsapi.NoSpace: "NoSpace", fsapi.Quota: "Quota", fsapi.IO: "IO", fsapi.Unavailable: "Unavailable", fsapi.Stale: "Stale", fsapi.Interrupted: "Interrupted", fsapi.Busy: "Busy", fsapi.WouldBlock: "WouldBlock", fsapi.TooManyLinks: "TooManyLinks", fsapi.NameTooLong: "NameTooLong"}
}

func protectedMutation(op string) bool {
	return oneOf(op, "CREATE", "MKDIR", "SYMLINK", "WRITE", "DRAIN_WRITE", "TRUNCATE", "SETATTR", "RENAME", "LINK", "UNLINK", "RMDIR", "SYNC")
}
