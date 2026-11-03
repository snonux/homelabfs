package guestbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type arguments map[string]json.RawMessage

func (s *Service) prepare(ctx context.Context, b Binding, o Captured) (Prepared, error) {
	var a arguments
	if e := json.Unmarshal(b.Call.Intent.Args, &a); e != nil {
		return Prepared{}, fsapi.Invalid
	}
	c := fsapi.Command{Mount: parseID(b.Context.Volume), DestinationMount: parseID(b.Context.Volume), Generation: s.generation(b.Context.Session)}
	op := b.Call.Intent.Opcode
	// All names are decoded raw bytes; no backing pathname or symlink is followed.
	c.Name = a.raw("name")
	c.NewName = a.raw("destination_name")
	c.Data = a.bytes("data")
	c.Offset = int64(a.number("offset"))
	c.Size = int64(a.number("size"))
	c.Count = a.number("count")
	c.Limit = int(a.number("limit"))
	c.After = a.raw("after")
	c.DirectoryRevision = a.number("revision")
	c.Mode = uint16(a.number("mode"))
	c.Umask = uint16(a.number("umask"))
	c.Flags = a.flags()
	c.NoReplace = a.boolean("no_replace")
	type capInput struct {
		key    string
		rights uint32
		dst    *fsapi.ObjectID
	}
	var transientForget bool
	var inputs []capInput
	switch op {
	case "ROOT":
		image, e := o.Image(ctx, c.Mount)
		if e != nil {
			return Prepared{}, e
		}
		c.Operation = fsapi.Getattr
		c.Object = image.Root
	case "LOOKUP":
		c.Operation = fsapi.Lookup
		inputs = []capInput{{"parent", 4, &c.Parent}}
	case "GETATTR", "READLINK", "READDIR", "OPEN", "SETATTR", "TRUNCATE", "SYNC":
		c.Operation = map[string]fsapi.Operation{"GETATTR": fsapi.Getattr, "READLINK": fsapi.Readlink, "READDIR": fsapi.Readdir, "OPEN": fsapi.Open, "SETATTR": fsapi.Setattr, "TRUNCATE": fsapi.Truncate, "SYNC": fsapi.Sync}[op]
		inputs = []capInput{{"object", 0, &c.Object}}
	case "CREATE", "MKDIR", "SYMLINK", "UNLINK", "RMDIR":
		c.Operation = map[string]fsapi.Operation{"CREATE": fsapi.Create, "MKDIR": fsapi.Mkdir, "SYMLINK": fsapi.SymlinkOp, "UNLINK": fsapi.Unlink, "RMDIR": fsapi.Rmdir}[op]
		inputs = []capInput{{"parent", 8, &c.Parent}}
		if op == "SYMLINK" {
			c.Data = a.bytes("target")
		}
	case "RENAME":
		c.Operation = fsapi.RenameOp
		c.Name = a.raw("source_name")
		inputs = []capInput{{"source_parent", 8, &c.Parent}, {"destination_parent", 8, &c.Destination}}
	case "LINK":
		c.Operation = fsapi.LinkOp
		c.NewName = c.Name
		inputs = []capInput{{"object", 0, &c.Object}, {"parent", 8, &c.Destination}}
	case "READ", "WRITE", "DRAIN_WRITE":
		c.Operation = fsapi.Write
		if op == "READ" {
			c.Operation = fsapi.Read
			c.Size = int64(c.Count)
		}
	case "DESCRIPTION_CLOSE":
		c.Operation = fsapi.DescriptorClose
	case "DESCRIPTION_RELEASE":
		c.Operation = fsapi.Release
		c.DescriptionOnly = true
	case "FORGET":
		c.Operation = fsapi.Forget
		inputs = []capInput{{"object", 0, &c.Object}}
	case "PROCESS_EXIT":
		if a.text("process") != b.Call.Credentials.Process {
			return Prepared{}, fsapi.Permission
		}
		s.mu.Lock()
		sequence := s.sessions[b.Context.Session].Exited[a.text("process")]
		s.mu.Unlock()
		if a.number("exit_sequence") <= sequence {
			return Prepared{}, fsapi.Stale
		}
		c.Operation = fsapi.ProcessExit
	case "STATFS":
		c.Operation = fsapi.Statfs
		inputs = []capInput{{"root", 0, &c.Object}}
	default:
		return Prepared{}, fsapi.Unsupported
	}
	for _, in := range inputs {
		g, e := s.resolve(ctx, b, a.cap(in.key), in.rights, o, false)
		if e != nil {
			return Prepared{}, e
		}
		if in.key == "parent" && oneOf(op, "LOOKUP", "CREATE", "MKDIR", "SYMLINK") {
			size := len(c.Name)
			for _, a := range g.Ancestors {
				size += len(a.Name)
			}
			if len(g.Ancestors) >= 256 || size > 16384 {
				return Prepared{}, fsapi.NameTooLong
			}
		}
		if op == "FORGET" {
			// Every wire grant owns exactly one capability reference. Counts
			// cannot consume another token's lookup obligation.
			if a.number("count") != 1 || g.Cap.Kind != "PATH" {
				return Prepared{}, fsapi.Invalid
			}
			transientForget = g.NoLookup
		}
		*in.dst = capObject(g.Cap)
	}
	if raw, ok := a["description"]; ok && string(raw) != "null" {
		rights := uint32(0)
		if c.Operation == fsapi.Read {
			rights = 1
		}
		if c.Operation == fsapi.Write || c.Operation == fsapi.Truncate {
			rights = 2
		}
		g, e := s.resolve(ctx, b, a.cap("description"), rights, o, true)
		if e != nil {
			return Prepared{}, e
		}
		if c.Object.Valid() && c.Object != capObject(g.Cap) {
			return Prepared{}, fsapi.Stale
		}
		c.Object = capObject(g.Cap)
		c.Handle = g.Handle
		if e = s.lifetime(b, a, g); e != nil {
			return Prepared{}, e
		}
	}
	if op == "DRAIN_WRITE" {
		if e := s.config.Views.ValidateDrain(ctx, b); e != nil {
			return Prepared{}, e
		}
	}
	if op == "SETATTR" {
		if e := json.Unmarshal(a["attributes"], new(any)); e != nil {
			return Prepared{}, fsapi.Invalid
		}
		c.Attributes = a.attributes()
	}
	if op == "TRUNCATE" {
		c.TruncateTimes = a.times()
	}
	if op == "SYNC" {
		c.Durability = map[string]fsapi.DurabilityRequest{"DATA": fsapi.DataDurability, "FILE": fsapi.FileDurability, "DIRECTORY": fsapi.DirectoryDurability}[a.text("scope")]
		if c.Durability == 0 {
			return Prepared{}, fsapi.Unsupported
		}
	}
	if transientForget {
		preparer, ok := o.(TransientForgetCaptured)
		if !ok {
			return Prepared{}, fsapi.Unsupported
		}
		return preparer.PrepareTransientForget(ctx, b, c)
	}
	return o.Prepare(ctx, b, c)
}

func (s *Service) resolve(ctx context.Context, b Binding, cap Capability, rights uint32, o Captured, description bool) (grant, error) {
	s.mu.Lock()
	ss := s.sessions[b.Context.Session]
	g := ss.Grants[cap.Token]
	if g == nil || g.Released || !equalCap(g.Cap, cap) || cap.Scope != b.Context || cap.Rights&rights != rights || description != (cap.Kind == "DESCRIPTION") {
		s.mu.Unlock()
		return grant{}, fsapi.Permission
	}
	owned := *g
	owned.Ancestors = append([]ancestor(nil), g.Ancestors...)
	s.mu.Unlock()
	if description || b.Call.Intent.Opcode == "FORGET" {
		return owned, nil
	}
	cred := guestCred(b.Call.Credentials)
	for _, a := range owned.Ancestors {
		parent, e := o.Stat(ctx, a.Parent)
		if e != nil {
			return grant{}, e
		}
		if !search(cred, parent.Attr) {
			return grant{}, fsapi.Permission
		}
		entry, e := o.Lookup(ctx, a.Parent, a.Name)
		if e != nil {
			return grant{}, e
		}
		if entry.Target == nil || *entry.Target != a.Object || entry.Revision != a.EntryRevision {
			return grant{}, fsapi.Stale
		}
	}
	return owned, nil
}
func (s *Service) lifetime(b Binding, a arguments, g grant) error {
	op := b.Call.Intent.Opcode
	if !oneOf(op, "DESCRIPTION_CLOSE", "DESCRIPTION_RELEASE", "PROCESS_EXIT") {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[b.Context.Session]
	if op == "DESCRIPTION_CLOSE" {
		process := a.text("process")
		if process != b.Call.Credentials.Process {
			return fsapi.Permission
		}
		key := g.Cap.Token + ":" + process
		seq := a.number("close_sequence")
		if _, known := ss.CloseSequences[key]; !known && len(ss.CloseSequences) >= s.config.MaxCredentialRevisions {
			return fsapi.NoSpace
		}
		if seq <= ss.CloseSequences[key] {
			return fsapi.Stale
		}
		if !s.privateValidation() {
			ss.CloseSequences[key] = seq
		}
	}
	if op == "DESCRIPTION_RELEASE" {
		original := ss.Grants[g.Cap.Token]
		seq := a.number("release_sequence")
		if seq <= original.Sequence {
			return fsapi.Stale
		}
		if !s.privateValidation() {
			original.Sequence = seq
		}
	}
	return nil
}
func search(c fsapi.Cred, a fsapi.Attr) bool {
	if a.Kind != fsapi.Directory {
		return false
	}
	if c.UID == 0 {
		return true
	}
	mode := a.Mode
	if c.UID == a.UID {
		mode >>= 6
	} else {
		group := c.GID == a.GID
		for _, g := range c.Groups {
			group = group || g == a.GID
		}
		if group {
			mode >>= 3
		}
	}
	return mode&1 != 0
}
func capObject(c Capability) fsapi.ObjectID {
	return fsapi.ObjectID{Home: parseID(c.Home), Allocation: parseID(c.Allocation)}
}
func parseID(s string) fsapi.UUID           { id, _ := fsapi.ParseUUID(s); return id }
func (a arguments) text(k string) string    { var s string; _ = json.Unmarshal(a[k], &s); return s }
func (a arguments) number(k string) uint64  { n, _ := strconv.ParseUint(string(a[k]), 10, 64); return n }
func (a arguments) boolean(k string) bool   { return string(a[k]) == "true" }
func (a arguments) bytes(k string) []byte   { b, _ := hex.DecodeString(a.text(k)); return b }
func (a arguments) raw(k string) string     { return string(a.bytes(k)) }
func (a arguments) cap(k string) Capability { var c Capability; _ = json.Unmarshal(a[k], &c); return c }
func (a arguments) flags() fsapi.OpenFlags {
	var f struct {
		Access, Sync                        string
		Append, Create, Exclusive, Truncate bool
	}
	_ = json.Unmarshal(a["flags"], &f)
	return fsapi.OpenFlags{Access: map[string]fsapi.AccessMode{"READ": fsapi.ReadOnly, "WRITE": fsapi.WriteOnly, "READWRITE": fsapi.ReadWrite}[f.Access], Sync: map[string]fsapi.SyncMode{"ORDINARY": fsapi.OrdinaryWrite, "DATA": fsapi.DataWrite, "FILE": fsapi.IntegrityWrite}[f.Sync], Append: f.Append, Create: f.Create, Exclusive: f.Exclusive, Truncate: f.Truncate}
}
func (a arguments) attributes() fsapi.Attributes {
	var x map[string]json.RawMessage
	_ = json.Unmarshal(a["attributes"], &x)
	b := arguments(x)
	out := fsapi.Attributes{Atime: b.timestamp("atime"), Mtime: b.timestamp("mtime"), AtimeNow: b.boolean("atime_now"), MtimeNow: b.boolean("mtime_now")}
	if string(x["mode"]) != "null" {
		v := uint16(b.number("mode"))
		out.Mode = &v
	}
	if string(x["uid"]) != "null" {
		v := uint32(b.number("uid"))
		out.UID = &v
	}
	if string(x["gid"]) != "null" {
		v := uint32(b.number("gid"))
		out.GID = &v
	}
	return out
}
func (a arguments) times() fsapi.TruncateTimes {
	var x arguments
	_ = json.Unmarshal(a["times"], &x)
	return fsapi.TruncateTimes{Atime: x.timestamp("atime"), Mtime: x.timestamp("mtime"), Ctime: x.timestamp("ctime"), AtimeNow: x.boolean("atime_now"), MtimeNow: x.boolean("mtime_now")}
}
func (a arguments) timestamp(k string) *fsapi.Timestamp {
	if len(a[k]) == 0 || string(a[k]) == "null" {
		return nil
	}
	var x struct {
		Seconds int64  `json:"seconds"`
		Nanos   uint32 `json:"nanos"`
	}
	if json.Unmarshal(a[k], &x) != nil {
		return nil
	}
	return &fsapi.Timestamp{Seconds: x.Seconds, Nanoseconds: x.Nanos}
}

func (s *Service) generation(sessionID string) fsapi.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[sessionID].Generation
}
