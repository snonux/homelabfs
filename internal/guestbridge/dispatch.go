package guestbridge

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type arguments map[string]json.RawMessage

// capInput names one capability argument, the rights it must carry and the
// command field that receives the object it grants.
type capInput struct {
	key    string
	rights uint32
	dst    *fsapi.ObjectID
}

// prepare decodes one CALL into an fsapi.Command: it maps the opcode, checks
// every capability argument, binds an optional open description and hands the
// command to the captured cut for planning.
func (s *Service) prepare(ctx context.Context, b Binding, o Captured) (Prepared, error) {
	var a arguments
	if e := json.Unmarshal(b.Call.Intent.Args, &a); e != nil {
		return Prepared{}, fsapi.Invalid
	}
	c := baseCommand(b, a, s.generation(b.Context.Session))
	inputs, e := s.decodeOperation(ctx, b, a, &c, o)
	if e != nil {
		return Prepared{}, e
	}
	transientForget, e := s.resolveInputs(ctx, b, a, &c, o, inputs)
	if e != nil {
		return Prepared{}, e
	}
	if e = s.attachDescription(ctx, b, a, &c, o); e != nil {
		return Prepared{}, e
	}
	if e = s.decodeExtras(ctx, b, a, &c); e != nil {
		return Prepared{}, e
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

// baseCommand copies the opcode-independent scalar arguments. All names are
// decoded raw bytes; no backing pathname or symlink is followed.
func baseCommand(b Binding, a arguments, generation fsapi.UUID) fsapi.Command {
	c := fsapi.Command{Mount: parseID(b.Context.Volume), DestinationMount: parseID(b.Context.Volume), Generation: generation}
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
	return c
}

// decodeOperation maps the opcode to its fsapi operation and lists the
// path/root capability arguments it carries. Description arguments are not
// listed here; attachDescription binds them.
func (s *Service) decodeOperation(ctx context.Context, b Binding, a arguments, c *fsapi.Command, o Captured) ([]capInput, error) {
	switch op := b.Call.Intent.Opcode; op {
	case "ROOT":
		image, e := o.Image(ctx, c.Mount)
		if e != nil {
			return nil, e
		}
		c.Operation = fsapi.Getattr
		c.Object = image.Root
	case "LOOKUP":
		c.Operation = fsapi.Lookup
		return []capInput{{"parent", 4, &c.Parent}}, nil
	case "GETATTR", "READLINK", "READDIR", "OPEN", "SETATTR", "TRUNCATE", "SYNC":
		c.Operation = map[string]fsapi.Operation{"GETATTR": fsapi.Getattr, "READLINK": fsapi.Readlink, "READDIR": fsapi.Readdir, "OPEN": fsapi.Open, "SETATTR": fsapi.Setattr, "TRUNCATE": fsapi.Truncate, "SYNC": fsapi.Sync}[op]
		return []capInput{{"object", 0, &c.Object}}, nil
	case "CREATE", "MKDIR", "SYMLINK", "UNLINK", "RMDIR":
		c.Operation = map[string]fsapi.Operation{"CREATE": fsapi.Create, "MKDIR": fsapi.Mkdir, "SYMLINK": fsapi.SymlinkOp, "UNLINK": fsapi.Unlink, "RMDIR": fsapi.Rmdir}[op]
		if op == "SYMLINK" {
			c.Data = a.bytes("target")
		}
		return []capInput{{"parent", 8, &c.Parent}}, nil
	case "RENAME":
		c.Operation = fsapi.RenameOp
		c.Name = a.raw("source_name")
		return []capInput{{"source_parent", 8, &c.Parent}, {"destination_parent", 8, &c.Destination}}, nil
	case "LINK":
		c.Operation = fsapi.LinkOp
		c.NewName = c.Name
		return []capInput{{"object", 0, &c.Object}, {"parent", 8, &c.Destination}}, nil
	case "FORGET":
		c.Operation = fsapi.Forget
		return []capInput{{"object", 0, &c.Object}}, nil
	case "PROCESS_EXIT":
		c.Operation = fsapi.ProcessExit
		return nil, s.processExitFresh(b, a)
	case "STATFS":
		c.Operation = fsapi.Statfs
		return []capInput{{"root", 0, &c.Object}}, nil
	default:
		if !decodeDescriptionOperation(op, c) {
			return nil, fsapi.Unsupported
		}
	}
	return nil, nil
}

// decodeDescriptionOperation maps the opcodes whose only capability is a
// mandatory description (bound later by attachDescription); it reports
// false for an unknown opcode.
func decodeDescriptionOperation(op string, c *fsapi.Command) bool {
	switch op {
	case "READ":
		c.Operation = fsapi.Read
		c.Size = int64(c.Count)
	case "WRITE", "DRAIN_WRITE":
		c.Operation = fsapi.Write
	case "DESCRIPTION_CLOSE":
		c.Operation = fsapi.DescriptorClose
	case "DESCRIPTION_RELEASE":
		c.Operation = fsapi.Release
		c.DescriptionOnly = true
	default:
		return false
	}
	return true
}

// processExitFresh admits a PROCESS_EXIT only from the exiting process itself
// and only with a sequence newer than the last one recorded for it.
func (s *Service) processExitFresh(b Binding, a arguments) error {
	if a.text("process") != b.Call.Credentials.Process {
		return fsapi.Permission
	}
	s.mu.Lock()
	sequence := s.sessions[b.Context.Session].Exited[a.text("process")]
	s.mu.Unlock()
	if a.number("exit_sequence") <= sequence {
		return fsapi.Stale
	}
	return nil
}

// resolveInputs checks every listed capability argument as a live grant and,
// where pathAuthority says the path is the authority, revalidates its
// recorded ancestry. It reports whether a FORGET closes a transient
// (READDIR-issued) grant.
func (s *Service) resolveInputs(ctx context.Context, b Binding, a arguments, c *fsapi.Command, o Captured, inputs []capInput) (bool, error) {
	op := b.Call.Intent.Opcode
	transientForget := false
	for _, in := range inputs {
		g, e := s.liveGrant(b, a.cap(in.key), in.rights, false)
		if e != nil {
			return false, e
		}
		if pathAuthority(op, in.key, a.hasDescription()) {
			if e = s.revalidatePath(ctx, b, g, o); e != nil {
				return false, e
			}
		}
		if in.key == "parent" && oneOf(op, "LOOKUP", "CREATE", "MKDIR", "SYMLINK") {
			if e = checkNameDepth(c.Name, g); e != nil {
				return false, e
			}
		}
		if op == "FORGET" {
			// Every wire grant owns exactly one capability reference. Counts
			// cannot consume another token's lookup obligation.
			if a.number("count") != 1 || g.Cap.Kind != "PATH" {
				return false, fsapi.Invalid
			}
			transientForget = g.NoLookup
		}
		*in.dst = capObject(g.Cap)
	}
	return transientForget, nil
}

// pathAuthority reports whether a capability argument authorizes by its
// recorded path, so that path must still resolve to the same object at this
// cut. Two cases authorize by possession instead and skip the re-check:
//
//   - FORGET closes the exact issued token; it releases a reference and
//     grants no access, so it must work after rename/unlink too.
//   - The object argument of a CALL that also carries a non-null description.
//     Only TRUNCATE (ftruncate) and SYNC (fsync/fdatasync) have both an object
//     and an (optional) description argument; READ, WRITE, DRAIN_WRITE,
//     DESCRIPTION_CLOSE and DESCRIPTION_RELEASE carry a description but no
//     path capability, so they never reach this check. The open description
//     is the authority: DESCRIPTION grants retain issued rights across
//     chmod/rename/unlink (Retained-9p-protocol.md), and attachDescription
//     requires the object to be the description's own object. The object
//     must still be a live, unforged grant in this scope (liveGrant).
//
// A null description keeps the path re-check: truncate(2) or a path-based
// sync names the file through its current path.
//
// The description exemption is limited to TRUNCATE and SYNC by name, not
// inferred from the argument shape: today only validateArgs' exact field
// sets stop OPEN, SETATTR or LINK from carrying a description, and a later
// schema change must not silently turn their object into possession
// authority (defence in depth).
func pathAuthority(op, key string, described bool) bool {
	if op == "FORGET" {
		return false
	}
	return !(key == "object" && described && oneOf(op, "TRUNCATE", "SYNC"))
}

// checkNameDepth bounds the ancestry a new entry name would extend.
func checkNameDepth(name string, g grant) error {
	size := len(name)
	for _, a := range g.Ancestors {
		size += len(a.Name)
	}
	if len(g.Ancestors) >= 256 || size > 16384 {
		return fsapi.NameTooLong
	}
	return nil
}

// attachDescription binds a non-null description argument: the description
// must be a live DESCRIPTION grant with the rights the operation needs, and
// any object argument already resolved must be that description's object
// (otherwise Stale). Descriptions are never path re-checked.
func (s *Service) attachDescription(ctx context.Context, b Binding, a arguments, c *fsapi.Command, o Captured) error {
	if !a.hasDescription() {
		return nil
	}
	rights := uint32(0)
	if c.Operation == fsapi.Read {
		rights = 1
	}
	if c.Operation == fsapi.Write || c.Operation == fsapi.Truncate {
		rights = 2
	}
	g, e := s.liveGrant(b, a.cap("description"), rights, true)
	if e != nil {
		return e
	}
	if c.Object.Valid() && c.Object != capObject(g.Cap) {
		return fsapi.Stale
	}
	c.Object = capObject(g.Cap)
	c.Handle = g.Handle
	return s.lifetime(b, a, g)
}

// decodeExtras decodes the opcode-specific arguments that are not
// capabilities.
func (s *Service) decodeExtras(ctx context.Context, b Binding, a arguments, c *fsapi.Command) error {
	switch b.Call.Intent.Opcode {
	case "DRAIN_WRITE":
		return s.config.Views.ValidateDrain(ctx, b)
	case "SETATTR":
		if e := json.Unmarshal(a["attributes"], new(any)); e != nil {
			return fsapi.Invalid
		}
		c.Attributes = a.attributes()
	case "TRUNCATE":
		c.TruncateTimes = a.times()
	case "SYNC":
		c.Durability = map[string]fsapi.DurabilityRequest{"DATA": fsapi.DataDurability, "FILE": fsapi.FileDurability, "DIRECTORY": fsapi.DirectoryDurability}[a.text("scope")]
		if c.Durability == 0 {
			return fsapi.Unsupported
		}
	}
	return nil
}

// liveGrant checks a capability against the session's issuance ledger: it
// must be issued, unreleased, unaltered, scoped to this context, carry the
// required rights and be of the expected DESCRIPTION/non-DESCRIPTION kind.
// Any mismatch is Permission. It returns a private copy of the grant.
func (s *Service) liveGrant(b Binding, cap Capability, rights uint32, description bool) (grant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[b.Context.Session]
	g := ss.Grants[cap.Token]
	if g == nil || g.Released || !equalCap(g.Cap, cap) || cap.Scope != b.Context || cap.Rights&rights != rights || description != (cap.Kind == "DESCRIPTION") {
		return grant{}, fsapi.Permission
	}
	owned := *g
	owned.Ancestors = append([]ancestor(nil), g.Ancestors...)
	return owned, nil
}

// revalidatePath re-walks a path grant's recorded ancestry at this cut:
// every ancestor directory must still be searchable by the caller (else
// Permission) and every entry must still name the same object at the same
// entry revision (else Stale). Possession is not timeless path permission.
func (s *Service) revalidatePath(ctx context.Context, b Binding, g grant, o Captured) error {
	cred := guestCred(b.Call.Credentials)
	for _, a := range g.Ancestors {
		parent, e := o.Stat(ctx, a.Parent)
		if e != nil {
			return e
		}
		if !search(cred, parent.Attr) {
			return fsapi.Permission
		}
		entry, e := o.Lookup(ctx, a.Parent, a.Name)
		if e != nil {
			return e
		}
		if entry.Target == nil || *entry.Target != a.Object || entry.Revision != a.EntryRevision {
			return fsapi.Stale
		}
	}
	return nil
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

// hasDescription reports a present, non-null description argument.
func (a arguments) hasDescription() bool {
	raw, ok := a["description"]
	return ok && string(raw) != "null"
}
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
