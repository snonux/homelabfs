package fsservice

import (
	"context"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// UnixPermissions implements the version-one traditional Unix policy. View is
// an explicit admitted namespace identity; changing the rules requires a new
// policy revision, not reinterpretation of existing journal authorizations.
// UID zero is the only privileged caller; daemon credentials are never consulted.
type UnixPermissions struct{ view fsapi.UUID }

type unixCheck struct {
	cred  fsapi.Cred
	input PermissionContext
	attrs map[fsapi.ObjectID]fsapi.Attr
}

var _ PermissionPolicy = UnixPermissions{}

// NewUnixPermissions binds the policy to an explicit namespace view.
func NewUnixPermissions(view fsapi.UUID) (UnixPermissions, error) {
	if !view.Valid() {
		return UnixPermissions{}, fsapi.Invalid
	}
	return UnixPermissions{view: view}, nil
}

// Check authorizes a command against fresh logical attributes. It does not
// resolve credentials, follow symlinks, inspect backing files or cache decisions.
func (p UnixPermissions) Check(ctx context.Context, cred fsapi.Cred, input PermissionContext) (PermissionDecision, error) {
	if err := ctx.Err(); err != nil {
		return PermissionDecision{}, err
	}
	if !p.view.Valid() || len(cred.Groups) > 1024 {
		return PermissionDecision{}, fsapi.Permission
	}
	check := unixCheck{cred: cred, input: input, attrs: make(map[fsapi.ObjectID]fsapi.Attr, len(input.Attributes))}
	for _, a := range input.Attributes {
		if a.Validate() != nil {
			return PermissionDecision{}, fsapi.Invalid
		}
		if _, exists := check.attrs[a.Object]; exists {
			return PermissionDecision{}, fsapi.Invalid
		}
		check.attrs[a.Object] = a
	}
	if err := check.command(); err != nil {
		return PermissionDecision{}, err
	}
	return PermissionDecision{View: p.view, Revision: 1}, nil
}

func (u unixCheck) command() error {
	c := u.input.Command
	switch c.Operation {
	case fsapi.Lookup:
		return u.directory(c.Parent, 1)
	case fsapi.Create:
		if err := u.directory(c.Parent, 1); err != nil {
			return err
		}
		if u.input.Source.Valid() {
			if c.Flags.Exclusive {
				return nil
			} // Executor returns Exists without opening.
			return u.open(u.input.Source, c.Flags)
		}
		if err := u.directory(c.Parent, 3); err != nil {
			return err
		}
		return u.creation(c.Parent, c.Mode)
	case fsapi.Mkdir, fsapi.SymlinkOp:
		if err := u.directory(c.Parent, 3); err != nil {
			return err
		}
		if c.Operation == fsapi.Mkdir {
			return u.creation(c.Parent, c.Mode)
		}
		return nil
	case fsapi.Open:
		return u.open(u.input.Object, c.Flags)
	case fsapi.Read, fsapi.Write, fsapi.Truncate, fsapi.Readdir:
		if u.input.Handle {
			return nil
		}
		if c.Operation == fsapi.Read || c.Operation == fsapi.Write {
			return fsapi.Stale
		}
		if c.Operation == fsapi.Readdir {
			return u.directory(u.input.Object, 4)
		}
		return u.access(u.input.Object, 2)
	case fsapi.Unlink, fsapi.Rmdir:
		if err := u.directory(c.Parent, 3); err != nil {
			return err
		}
		return u.sticky(c.Parent, u.input.Source)
	case fsapi.RenameOp:
		if err := u.directory(c.Parent, 3); err != nil {
			return err
		}
		if err := u.directory(c.Destination, 3); err != nil {
			return err
		}
		if err := u.sticky(c.Parent, u.input.Source); err != nil {
			return err
		}
		if err := u.sticky(c.Destination, u.input.Replacement); err != nil {
			return err
		}
		if a, ok := u.attrs[u.input.Source]; ok && a.Kind == fsapi.Directory && c.Parent != c.Destination {
			return u.access(u.input.Source, 2) // Reparenting changes the directory's dotdot.
		}
		return nil
	case fsapi.LinkOp:
		if _, err := u.object(u.input.Object); err != nil {
			return err
		}
		return u.directory(c.Destination, 3)
	case fsapi.Setattr:
		return u.setattr()
	case fsapi.Getattr, fsapi.Readlink, fsapi.Sync:
		_, err := u.object(u.input.Object)
		return err
	case fsapi.TryLock, fsapi.WaitLock, fsapi.GetLock, fsapi.Unlock:
		if !u.input.Handle {
			return fsapi.Stale
		}
		return nil
	case fsapi.DescriptorClose, fsapi.CancelLock, fsapi.ProcessExit, fsapi.SessionExit, fsapi.Reap, fsapi.Release, fsapi.Forget, fsapi.Statfs:
		return nil // Lifetime cleanup is not revocable by chmod or credential changes.
	default:
		return fsapi.Unsupported
	}
}
func (u unixCheck) object(id fsapi.ObjectID) (fsapi.Attr, error) {
	a, ok := u.attrs[id]
	if !ok {
		return a, fsapi.NotFound
	}
	return a, nil
}
func (u unixCheck) directory(id fsapi.ObjectID, bits uint16) error {
	a, err := u.object(id)
	if err != nil {
		return err
	}
	if a.Kind != fsapi.Directory {
		return fsapi.NotDirectory
	}
	return u.access(id, bits)
}
func (u unixCheck) access(id fsapi.ObjectID, bits uint16) error {
	a, err := u.object(id)
	if err != nil {
		return err
	}
	if u.cred.UID == 0 {
		return nil
	}
	mode := a.Mode
	if u.cred.UID == a.UID {
		mode >>= 6
	} else if u.group(a.GID) {
		mode >>= 3
	}
	if mode&bits != bits {
		return fsapi.Permission
	}
	return nil
}
func (u unixCheck) group(gid uint32) bool {
	return u.cred.GID == gid || slices.Contains(u.cred.Groups, gid)
}
func (u unixCheck) open(id fsapi.ObjectID, flags fsapi.OpenFlags) error {
	if err := flags.Validate(); err != nil {
		return err
	}
	bits := uint16(4)
	switch flags.Access {
	case fsapi.WriteOnly:
		bits = 2
	case fsapi.ReadWrite:
		bits = 6
	}
	return u.access(id, bits)
}
func (u unixCheck) sticky(parent, target fsapi.ObjectID) error {
	if !target.Valid() {
		return nil
	}
	p, err := u.object(parent)
	if err != nil {
		return err
	}
	a, err := u.object(target)
	if err != nil {
		return err
	}
	if p.Mode&0o1000 != 0 && u.cred.UID != 0 && u.cred.UID != p.UID && u.cred.UID != a.UID {
		return fsapi.Permission
	}
	return nil
}
func (u unixCheck) creation(parent fsapi.ObjectID, mode uint16) error {
	a, err := u.object(parent)
	if err != nil {
		return err
	}
	// Inherited directory setgid is allowed even for nonmembers; explicitly
	// requesting setgid for a group outside caller membership is privileged.
	if u.cred.UID != 0 && mode&0o2000 != 0 && !u.group(a.GID) {
		return fsapi.Permission
	}
	if u.cred.UID != 0 && u.input.Command.Operation == fsapi.Create && mode&0o1000 != 0 {
		return fsapi.Permission
	}
	return nil
}
func (u unixCheck) setattr() error {
	a, err := u.object(u.input.Object)
	if err != nil {
		return err
	}
	change := u.input.Command.Attributes
	owner := u.cred.UID == a.UID
	root := u.cred.UID == 0
	if change.Mode != nil {
		if a.Kind == fsapi.Symlink {
			return fsapi.Unsupported
		}
		if !root && (!owner || *change.Mode&0o2000 != 0 && !u.group(a.GID) || *change.Mode&0o1000 != 0 && a.Kind != fsapi.Directory) {
			return fsapi.Permission
		}
	}
	if change.UID != nil || change.GID != nil {
		if !root && (!owner || change.UID != nil && *change.UID != a.UID || change.GID != nil && *change.GID != a.GID && !u.group(*change.GID)) {
			return fsapi.Permission
		}
	}
	if !root && !owner {
		if change.Atime != nil || change.Mtime != nil {
			return fsapi.Permission
		}
		if change.AtimeNow || change.MtimeNow {
			return u.access(a.Object, 2)
		}
	}
	return nil
}
