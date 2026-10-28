package fsservice

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func unixFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t, "sim")
	p, err := NewUnixPermissions(uid(50))
	if err != nil {
		t.Fatal(err)
	}
	f.c.Permissions = p
	f.c.Segment.MaximumBytes = 1 << 20
	return f
}
func as(f *fixture, cred fsapi.Cred, c fsapi.Command) fsapi.Result {
	f.t.Helper()
	c.Mount = f.c.Storage.Volume
	if c.Handle != 0 && !c.Generation.Valid() {
		c.Generation = f.c.Workspace.ID
	}
	if c.Operation == fsapi.RenameOp || c.Operation == fsapi.LinkOp {
		c.DestinationMount = c.Mount
	}
	req := fsapi.NewRequest(cred)
	var api fsapi.Filesystem = f.service
	if err := api.Dispatch(f.t.Context(), req, c); err != nil {
		f.t.Fatal(err)
	}
	r, err := req.Completion().Wait(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}
func chmod(f *fixture, id fsapi.ObjectID, mode uint16) {
	f.t.Helper()
	r := as(f, fsapi.Cred{}, fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: fsapi.Attributes{Mode: &mode}})
	if r.Code != fsapi.OK {
		f.t.Fatal(r)
	}
}

func TestUnixAccessThroughService(t *testing.T) {
	f := unixFixture(t)
	f.start()
	file := f.create(f.c.Storage.Root, "file")
	owner := fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}
	chmod(f, file.Attr.Object, 0o640)
	for _, test := range []struct {
		name   string
		cred   fsapi.Cred
		access fsapi.AccessMode
		want   fsapi.Errno
	}{
		{"owner-read", owner, fsapi.ReadOnly, fsapi.OK},
		{"owner-write", owner, fsapi.WriteOnly, fsapi.OK},
		{"primary-group-read", fsapi.Cred{UID: 2000, GID: 200}, fsapi.ReadOnly, fsapi.OK},
		{"supplementary-group-read", fsapi.Cred{UID: 2000, GID: 201, Groups: []uint32{500, 200}}, fsapi.ReadOnly, fsapi.OK},
		{"group-write-denied", fsapi.Cred{UID: 2000, GID: 200}, fsapi.WriteOnly, fsapi.Permission},
		{"other-read-denied", fsapi.Cred{UID: 2000, GID: 201}, fsapi.ReadOnly, fsapi.Permission},
		{"root-write", fsapi.Cred{}, fsapi.WriteOnly, fsapi.OK},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := as(f, test.cred, fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: test.access}})
			if r.Code != test.want {
				t.Fatalf("got %v want %v", r.Code, test.want)
			}
		})
	}
	// Owner class takes precedence over more permissive group/other classes.
	chmod(f, file.Attr.Object, 0o044)
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}}); r.Code != fsapi.Permission {
		t.Fatal(r)
	}
	// The verified descriptor remains usable after chmod, even when passed to a
	// different caller. Fresh opens and handle access mode checks still deny.
	chmod(f, file.Attr.Object, 0)
	if r := as(f, fsapi.Cred{UID: 9000}, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("retained")}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Read, Handle: file.Handle, Size: 8}); string(r.Data) != "retained" {
		t.Fatal(r)
	}
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}}); r.Code != fsapi.Permission {
		t.Fatal(r)
	}
}

func TestUnixDirectoryAndStickyChecks(t *testing.T) {
	f := unixFixture(t)
	f.start()
	root := f.c.Storage.Root
	owner := fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}
	other := fsapi.Cred{UID: 2000, GID: 200}
	d := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "directory", Mode: 0o777}).Attr.Object
	f.create(d, "a")
	b := as(f, other, fsapi.Command{Operation: fsapi.Create, Parent: d, Name: "b", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}})
	if b.Code != fsapi.OK {
		t.Fatal(b)
	}
	chmod(f, d, 0o666)
	for _, op := range []fsapi.Command{
		{Operation: fsapi.Lookup, Parent: d, Name: "a"},
		{Operation: fsapi.Create, Parent: d, Name: "new", Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}},
		{Operation: fsapi.Unlink, Parent: d, Name: "a"},
	} {
		if r := as(f, owner, op); r.Code != fsapi.Permission {
			t.Fatal(op, r)
		}
	}
	// Readdir needs read; lookup needs search. Neither implies the other.
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Readdir, Object: d, Limit: 10}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	chmod(f, d, 0o111)
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Lookup, Parent: d, Name: "a"}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Readdir, Object: d, Limit: 10}); r.Code != fsapi.Permission {
		t.Fatal(r)
	}
	// Existing O_CREAT opens need search and file access, not parent write.
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Create, Parent: d, Name: "a", Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly, Create: true}}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	chmod(f, d, 0o1777)
	for _, op := range []fsapi.Command{
		{Operation: fsapi.Unlink, Parent: d, Name: "a"},
		{Operation: fsapi.RenameOp, Parent: d, Name: "a", Destination: root, NewName: "moved"},
		{Operation: fsapi.RenameOp, Parent: d, Name: "b", Destination: d, NewName: "a"},
	} {
		before := f.service.selection.Through
		if r := as(f, other, op); r.Code != fsapi.Permission {
			t.Fatal(op, r)
		}
		if before != f.service.selection.Through {
			t.Fatal("denial authored journal records")
		}
	}
	if r := as(f, other, fsapi.Command{Operation: fsapi.Unlink, Parent: d, Name: "b"}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Unlink, Parent: d, Name: "a"}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
}

func TestUnixMetadataPrivileges(t *testing.T) {
	f := unixFixture(t)
	f.start()
	file := f.create(f.c.Storage.Root, "file")
	id := file.Attr.Object
	owner := fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200, 300}}
	other := fsapi.Cred{UID: 2000, GID: 200}
	mode := uint16(0o6750)
	sticky := uint16(0o1600)
	foreignGroupMode := uint16(0o2600)
	ownUID, foreignUID, memberGroup, foreignGroup := uint32(1000), uint32(2000), uint32(300), uint32(900)
	timestamp := fsapi.Timestamp{Seconds: 7}
	for _, test := range []struct {
		name string
		cred fsapi.Cred
		a    fsapi.Attributes
		want fsapi.Errno
	}{
		{"foreign-chmod", other, fsapi.Attributes{Mode: &mode}, fsapi.Permission},
		{"owner-setid", owner, fsapi.Attributes{Mode: &mode}, fsapi.OK},
		{"owner-sticky-file", owner, fsapi.Attributes{Mode: &sticky}, fsapi.Permission},
		{"owner-chown-other", owner, fsapi.Attributes{UID: &foreignUID}, fsapi.Permission},
		{"owner-chgrp-foreign", owner, fsapi.Attributes{GID: &foreignGroup}, fsapi.Permission},
		{"owner-chown-self", owner, fsapi.Attributes{UID: &ownUID}, fsapi.OK},
		{"owner-chgrp-supplementary", owner, fsapi.Attributes{GID: &memberGroup}, fsapi.OK},
		{"foreign-explicit-time", other, fsapi.Attributes{Mtime: &timestamp}, fsapi.Permission},
		{"foreign-time-now-denied", other, fsapi.Attributes{MtimeNow: true}, fsapi.Permission},
		{"root-chgrp", fsapi.Cred{}, fsapi.Attributes{GID: &foreignGroup}, fsapi.OK},
		{"owner-setgid-foreign", owner, fsapi.Attributes{Mode: &foreignGroupMode}, fsapi.Permission},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := f.service.selection.Through
			r := as(f, test.cred, fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: test.a})
			if r.Code != test.want {
				t.Fatalf("got %v want %v", r.Code, test.want)
			}
			if r.Code != fsapi.OK && before != f.service.selection.Through {
				t.Fatal("denied metadata mutation journaled")
			}
			if (test.a.UID != nil || test.a.GID != nil) && r.Code == fsapi.OK && r.Attr.Mode&0o6000 != 0 {
				t.Fatal("chown retained set-ID")
			}
		})
	}
	chmod(f, id, 0o666)
	if r := as(f, other, fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: fsapi.Attributes{AtimeNow: true, MtimeNow: true}}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	chmod(f, id, 0o6750)
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("x")}); r.Code != fsapi.OK || r.Attr.Mode&0o6000 != 0 {
		t.Fatal(r)
	}
	chmod(f, id, 0o6750)
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Size: 0}); r.Code != fsapi.OK || r.Attr.Mode&0o6000 != 0 {
		t.Fatal(r)
	}
}

func TestUnixCreationInheritanceAndSpecialBits(t *testing.T) {
	f := unixFixture(t)
	f.start()
	root := f.c.Storage.Root
	// FreeBSD policy always inherits parent GID, including without setgid.
	chmod(f, root, 0o777)
	file := f.create(root, "ordinary")
	if file.Attr.GID != 200 || file.Attr.Mode != 0o640 {
		t.Fatal(file)
	}
	outsider := fsapi.Cred{UID: 2000, GID: 900}
	if r := as(f, outsider, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "setgid", Mode: 0o2666, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}}); r.Code != fsapi.Permission {
		t.Fatal(r)
	}
	chmod(f, root, 0o2777)
	d := as(f, outsider, fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "inherited", Mode: 0o777, Umask: 0o027})
	if d.Code != fsapi.OK || d.Attr.GID != 200 || d.Attr.Mode != 0o2750 {
		t.Fatal(d)
	}
}

func TestUnixPermissionCrashReplay(t *testing.T) {
	for _, boundary := range []apply.Boundary{apply.AfterApply, apply.EffectsDurable} {
		t.Run(string(boundary), func(t *testing.T) {
			f := unixFixture(t)
			armed := false
			f.c.Apply.Hook = func(_ context.Context, b apply.Boundary) error {
				if armed && b == boundary {
					armed = false
					return fsapi.IO
				}
				return nil
			}
			f.start()
			file := f.create(f.c.Storage.Root, "file")
			id := file.Attr.Object
			owner := fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{300, 200, 300}}
			gid := uint32(300)
			if r := as(f, owner, fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: fsapi.Attributes{GID: &gid}}); r.Code != fsapi.OK {
				t.Fatal(r)
			}
			mode := uint16(0o600)
			armed = true
			if r := as(f, owner, fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: fsapi.Attributes{Mode: &mode}}); r.Code != fsapi.IO {
				t.Fatal(r)
			}
			if err := f.service.Close(); err != nil {
				t.Fatal(err)
			}
			f.service = nil
			for _, root := range f.roots {
				root.(*vfs.Simulator).Crash()
			}
			f.workspace()
			f.start()
			got := as(f, owner, fsapi.Command{Operation: fsapi.Getattr, Object: id})
			if got.Code != fsapi.OK || got.Attr.Mode != 0o600 || got.Attr.GID != 300 {
				t.Fatal(got)
			}
			if r := as(f, fsapi.Cred{UID: 2000, GID: 300}, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}}); r.Code != fsapi.Permission {
				t.Fatal(r)
			}
			reader, err := f.service.reader(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reader.Close() }()
			found := 0
			for {
				e, err := reader.Next(t.Context())
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				m, ok := e.Record.Payload.(record.Mutation)
				if !ok || m.Intent.Kind != 10 {
					continue
				}
				found++
				if len(m.Authorizations) != 1 {
					t.Fatal(m.Authorizations)
				}
				a := m.Authorizations[0]
				if a.Object != id || a.ObjectRevision == 0 || a.View != uid(50) || a.PolicyRevision != 1 || a.Credentials.UID != 1000 || a.Credentials.GID != 1001 || !slices.Equal(a.Credentials.Groups, []uint32{200, 300}) {
					t.Fatal(a)
				}
			}
			if found != 2 {
				t.Fatal("missing permission changes", found)
			}
		})
	}
}

func TestUnixOriginalCommandAndJournalInputs(t *testing.T) {
	f := unixFixture(t)
	policy := f.c.Permissions
	var observed PermissionContext
	var credential fsapi.Cred
	f.c.Validation = nil
	f.c.Protocol = &contractProtocol{}
	f.c.Permissions = permissionFunc(func(ctx context.Context, cred fsapi.Cred, input PermissionContext) (PermissionDecision, error) {
		decision, err := policy.Check(ctx, cred, input)
		observed = input
		observed.Command = input.Command.Clone()
		observed.Attributes = slices.Clone(input.Attributes)
		credential = cred.Clone()
		// Hooks own their arguments. Mutation cannot alter subsequent planning or
		// the immutable credentials and revisions captured for journal authorization.
		if len(cred.Groups) > 0 {
			cred.Groups[0] = 999
		}
		for i := range input.Attributes {
			input.Attributes[i].Revision = 999
		}
		for i := range input.Command.Data {
			input.Command.Data[i] = '!'
		}
		return decision, err
	})
	f.start()
	file := f.ok(fsapi.Command{Operation: fsapi.Create, Parent: f.c.Storage.Root, Name: "append", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Append: true}})
	original := fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Offset: 999, Data: []byte("exact")}
	result := f.ok(original)
	if observed.Command.Operation != fsapi.Write || observed.Command.Object.Valid() || observed.Command.Offset != 999 || observed.Object != file.Attr.Object || !observed.Handle || string(observed.Command.Data) != "exact" {
		t.Fatal(observed)
	}
	if credential.UID != 1000 || credential.GID != 1001 || !slices.Equal(credential.Groups, []uint32{200, 300}) {
		t.Fatal(credential)
	}
	before := observed.Attributes[0]
	reader, err := f.service.reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	found := false
	for {
		e, err := reader.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		m, ok := e.Record.Payload.(record.Mutation)
		if !ok || m.Intent.Kind != 8 {
			continue
		}
		write := m.Intent.Arguments.(record.Write)
		if write.Offset != 0 || string(write.Data) != "exact" {
			t.Fatal(write)
		}
		if len(m.Authorizations) != 1 {
			t.Fatal(m.Authorizations)
		}
		a := m.Authorizations[0]
		if a.Object != file.Attr.Object || a.ObjectRevision != before.Revision || !slices.Equal(a.Credentials.Groups, []uint32{200, 300}) {
			t.Fatal(a)
		}
		found = true
	}
	if !found || result.Attr.Size != 5 {
		t.Fatal("write authorization missing", result)
	}
	// O_TRUNC is checked as the original open command, before its journaled
	// truncate plan. A new open cannot inherit an old handle's authorization.
	f.ok(fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.WriteOnly, Truncate: true}})
	if observed.Command.Operation != fsapi.Open || !observed.Command.Flags.Truncate || observed.Handle {
		t.Fatal(observed)
	}
}

func TestUnixNamespaceAndHandleDenials(t *testing.T) {
	f := unixFixture(t)
	f.start()
	root := f.c.Storage.Root
	owner := fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}
	file := f.create(root, "file")
	destination := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "dest", Mode: 0o755}).Attr.Object
	chmod(f, destination, 0o555)
	for _, c := range []fsapi.Command{
		{Operation: fsapi.Create, Parent: destination, Name: "new", Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}},
		{Operation: fsapi.Mkdir, Parent: destination, Name: "new", Mode: 0o755},
		{Operation: fsapi.SymlinkOp, Parent: destination, Name: "new", Data: []byte("text")},
		{Operation: fsapi.LinkOp, Object: file.Attr.Object, Destination: destination, NewName: "alias"},
		{Operation: fsapi.RenameOp, Parent: root, Name: "file", Destination: destination, NewName: "file"},
	} {
		before := f.service.selection.Through
		if r := as(f, owner, c); r.Code != fsapi.Permission {
			t.Fatal(c, r)
		}
		if before != f.service.selection.Through {
			t.Fatal("denial journaled")
		}
	}
	chmod(f, file.Attr.Object, 0o400)
	for _, c := range []fsapi.Command{
		{Operation: fsapi.Create, Parent: root, Name: "file", Flags: fsapi.OpenFlags{Access: fsapi.WriteOnly, Create: true, Truncate: true}},
		{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.WriteOnly, Truncate: true}},
		{Operation: fsapi.Truncate, Object: file.Attr.Object, Size: 10},
	} {
		if r := as(f, owner, c); r.Code != fsapi.Permission {
			t.Fatal(c, r)
		}
	}
	read := as(f, owner, fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	if read.Code != fsapi.OK {
		t.Fatal(read)
	}
	for _, c := range []fsapi.Command{
		{Operation: fsapi.Write, Handle: read.Handle, Data: []byte("denied")},
		{Operation: fsapi.Truncate, Handle: read.Handle, Size: 10},
	} {
		if r := as(f, owner, c); r.Code != fsapi.Permission {
			t.Fatal(c, r)
		}
	}
	chmod(f, file.Attr.Object, 0o6750)
	if r := as(f, fsapi.Cred{}, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("privileged")}); r.Code != fsapi.OK || r.Attr.Mode != 0o6750 {
		t.Fatal(r)
	}
	gid := uint32(900)
	if r := as(f, fsapi.Cred{}, fsapi.Command{Operation: fsapi.Setattr, Object: file.Attr.Object, Attributes: fsapi.Attributes{GID: &gid}}); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	if r := as(f, owner, fsapi.Command{Operation: fsapi.Setattr, Object: file.Attr.Object, Attributes: fsapi.Attributes{GID: &gid}}); r.Code != fsapi.OK {
		t.Fatal("unchanged GID denied", r)
	}
	mode := uint16(0o6755)
	before := f.service.selection.Through
	if r := as(f, fsapi.Cred{}, fsapi.Command{Operation: fsapi.Setattr, Object: file.Attr.Object, Attributes: fsapi.Attributes{Mode: &mode, GID: &gid}}); r.Code != fsapi.Unsupported {
		t.Fatal(r)
	}
	if before != f.service.selection.Through {
		t.Fatal("unsupported combined form journaled")
	}
}

func TestUnixDirectoryMoveAndStickyOwner(t *testing.T) {
	f := unixFixture(t)
	f.start()
	root := f.c.Storage.Root
	owner := fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}
	other := fsapi.Cred{UID: 2000, GID: 200}
	source := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "source", Mode: 0o555}).Attr.Object
	destination := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "destination", Mode: 0o777}).Attr.Object
	move := fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "source", Destination: destination, NewName: "moved"}
	if r := as(f, owner, move); r.Code != fsapi.Permission {
		t.Fatal("dotdot mutation did not require write", r)
	}
	chmod(f, source, 0o755)
	if r := as(f, owner, move); r.Code != fsapi.OK {
		t.Fatal(r)
	}
	chmod(f, destination, 0o1777)
	child := as(f, other, fsapi.Command{Operation: fsapi.Mkdir, Parent: destination, Name: "child", Mode: 0o755})
	if child.Code != fsapi.OK {
		t.Fatal(child)
	}
	stranger := fsapi.Cred{UID: 3000, GID: 200}
	remove := fsapi.Command{Operation: fsapi.Rmdir, Parent: destination, Name: "child"}
	if r := as(f, stranger, remove); r.Code != fsapi.Permission {
		t.Fatal(r)
	}
	if r := as(f, owner, remove); r.Code != fsapi.OK {
		t.Fatal("sticky directory owner denied", r)
	}
	// A hard link shares the selected inode permission metadata.
	file := f.create(root, "file")
	f.ok(fsapi.Command{Operation: fsapi.LinkOp, Object: file.Attr.Object, Destination: destination, NewName: "alias"})
	alias := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: destination, Name: "alias"})
	chmod(f, alias.Attr.Object, 0o600)
	if r := as(f, other, fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}}); r.Code != fsapi.Permission {
		t.Fatal(r)
	}
}
