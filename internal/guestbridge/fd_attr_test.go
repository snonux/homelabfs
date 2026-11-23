package guestbridge

import (
	"errors"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Task s64 (docs/design/s64-fd-attr.md): GETATTR and SETATTR may carry the
// open file's DESCRIPTION, so fstat, fchmod, fchown and futimens keep working
// on a file that was renamed or unlinked while open, as POSIX requires.

// fdAttrFile is one created file with its path and description grants.
type fdAttrFile struct {
	parent, object, description Capability
}

var fdAttrCreate = map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}

// fdAttrSetup creates file "6f6c64" ("old"), mode 0644, as cred in the
// fixture root.
func fdAttrSetup(t *testing.T, f *bridgeFixture, cred Credentials) fdAttrFile {
	t.Helper()
	return fdAttrSetupNamed(t, f, cred, "6f6c64")
}

// fdAttrSetupNamed creates the file with the given hex name.
func fdAttrSetupNamed(t *testing.T, f *bridgeFixture, cred Credentials, name string) fdAttrFile {
	t.Helper()
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	parent := capabilityAt(t, root, 0)
	_, created := f.call(t, cred, "CREATE", map[string]any{"parent": parent, "name": name, "mode": 0o644, "umask": 0, "flags": fdAttrCreate})
	if created["code"] != "OK" {
		t.Fatal(created)
	}
	return fdAttrFile{parent: parent, object: capabilityAt(t, created, 0), description: capabilityAt(t, created, 1)}
}

// attrs is a SETATTR "attributes" argument; only the given fields change.
func attrs(change map[string]any) map[string]any {
	a := map[string]any{"mode": nil, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
	for k, v := range change {
		a[k] = v
	}
	return a
}

// getattr sends GETATTR, described when description is non-nil.
func getattr(t *testing.T, f *bridgeFixture, cred Credentials, object Capability, description *Capability) map[string]any {
	t.Helper()
	args := map[string]any{"object": object}
	if description != nil {
		args["description"] = *description
	}
	_, r := f.call(t, cred, "GETATTR", args)
	return r
}

// setattr sends SETATTR, described when description is non-nil.
func setattr(t *testing.T, f *bridgeFixture, cred Credentials, object Capability, description *Capability, change map[string]any) map[string]any {
	t.Helper()
	args := map[string]any{"object": object, "attributes": attrs(change)}
	if description != nil {
		args["description"] = *description
	}
	_, r := f.call(t, cred, "SETATTR", args)
	return r
}

// fdAttrDetach renames or unlinks the open file and reuses its old name for a
// different file, so a path re-check finds another target.
func fdAttrDetach(t *testing.T, f *bridgeFixture, cred Credentials, file fdAttrFile, change string) {
	t.Helper()
	var r map[string]any
	if change == "RENAME" {
		_, r = f.call(t, cred, "RENAME", map[string]any{"source_parent": file.parent, "source_name": "6f6c64", "destination_parent": file.parent, "destination_name": "6e6577", "no_replace": false})
	} else {
		_, r = f.call(t, cred, "UNLINK", map[string]any{"parent": file.parent, "name": "6f6c64"})
	}
	if r["code"] != "OK" {
		t.Fatal(change, r)
	}
	if _, r = f.call(t, cred, "CREATE", map[string]any{"parent": file.parent, "name": "6f6c64", "mode": 0o600, "umask": 0, "flags": fdAttrCreate}); r["code"] != "OK" {
		t.Fatal("reuse:", r)
	}
}

// TestDescribedAttrCallsSurviveRenameAndUnlink: after rename or unlink the
// path forms stay Stale, while fstat, fchmod, fchown, futimens and ftruncate
// through the description reach the original object, by its owner (no root).
func TestDescribedAttrCallsSurviveRenameAndUnlink(t *testing.T) {
	for _, change := range []string{"RENAME", "UNLINK"} {
		t.Run(change, func(t *testing.T) {
			f := newBridge(t)
			f.hello(t)
			cred := f.cred()
			file := fdAttrSetup(t, f, cred)
			if r := getattr(t, f, cred, file.object, nil); r["code"] != "OK" {
				t.Fatal("legacy getattr before the change:", r)
			}
			fdAttrDetach(t, f, cred, file, change)
			if r := getattr(t, f, cred, file.object, nil); r["code"] != "Stale" {
				t.Fatal("path getattr:", r)
			}
			if r := setattr(t, f, cred, file.object, nil, map[string]any{"mode": 0o600}); r["code"] != "Stale" {
				t.Fatal("path setattr:", r)
			}
			fdAttrDescribedCalls(t, f, cred, file, change == "UNLINK")
		})
	}
}

// fdAttrDescribedCalls runs the described forms and checks their results.
func fdAttrDescribedCalls(t *testing.T, f *bridgeFixture, cred Credentials, file fdAttrFile, unlinked bool) {
	t.Helper()
	d := &file.description
	if r := setattr(t, f, cred, file.object, d, map[string]any{"mode": 0o604}); r["code"] != "OK" {
		t.Fatal("fchmod:", r)
	}
	if r := setattr(t, f, cred, file.object, d, map[string]any{"uid": cred.FSUID, "gid": cred.FSGID}); r["code"] != "OK" {
		t.Fatal("fchown:", r)
	}
	if r := setattr(t, f, cred, file.object, d, map[string]any{"atime_now": true, "mtime_now": true}); r["code"] != "OK" {
		t.Fatal("futimens:", r)
	}
	if _, r := f.call(t, cred, "TRUNCATE", map[string]any{"object": file.object, "description": file.description, "size": 7, "times": truncateTimes}); r["code"] != "OK" {
		t.Fatal("ftruncate:", r)
	}
	r := getattr(t, f, cred, file.object, d)
	if r["code"] != "OK" {
		t.Fatal("fstat:", r)
	}
	a := r["attributes"].(map[string]any)
	nlink := float64(1)
	if unlinked {
		nlink = 0
	}
	if a["object_allocation"] != file.object.Allocation || a["mode"] != float64(0o604) || a["size"] != float64(7) || a["gid"] != float64(cred.FSGID) || a["nlink"] != nlink {
		t.Fatal("fstat attributes:", a)
	}
}

// TestDescribedSetattrKeepsOwnerRules: an open description proves the open
// file, not ownership. A non-owner holding the owner's description (and a
// read-only description of its own) can fstat but not chmod, chown, set
// explicit times or, without write permission, set the current time.
func TestDescribedSetattrKeepsOwnerRules(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	owner := f.cred()
	file := fdAttrSetup(t, f, owner)
	other := owner
	other.FSUID, other.FSGID, other.Process, other.Revision = 2000, 2000, uid(90).String(), 1
	reader := fdAttrOpenReadOnly(t, f, other, file.object)
	for _, d := range []Capability{file.description, reader} {
		if r := getattr(t, f, other, file.object, &d); r["code"] != "OK" {
			t.Fatal("non-owner fstat:", r)
		}
		for _, change := range []map[string]any{{"mode": 0o777}, {"uid": uint32(2000)}, {"gid": uint32(2000)}, {"mtime": map[string]any{"seconds": 1, "nanos": 0}}, {"mtime_now": true}} {
			if r := setattr(t, f, other, file.object, &d, change); r["code"] != "Permission" {
				t.Fatal("non-owner", change, r)
			}
		}
	}
	// The owner may chmod through a read-only description: fchmod needs no
	// write access, only ownership.
	ownerReader := fdAttrOpenReadOnly(t, f, owner, file.object)
	if r := setattr(t, f, owner, file.object, &ownerReader, map[string]any{"mode": 0o600}); r["code"] != "OK" {
		t.Fatal("owner fchmod through a read-only description:", r)
	}
	root := owner
	root.FSUID, root.Process, root.Revision = 0, uid(91).String(), 1
	if r := setattr(t, f, root, file.object, &reader, map[string]any{"uid": uint32(2000)}); r["code"] != "OK" {
		t.Fatal("root fchown:", r)
	}
}

// fdAttrOpenReadOnly opens object read-only as cred and returns its description.
func fdAttrOpenReadOnly(t *testing.T, f *bridgeFixture, cred Credentials, object Capability) Capability {
	t.Helper()
	flags := map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}
	_, opened := f.call(t, cred, "OPEN", map[string]any{"object": object, "flags": flags})
	if opened["code"] != "OK" {
		t.Fatal("open:", opened)
	}
	return descriptionIn(t, opened)
}

// TestDescribedAttrCallsRefuseDeadOrMismatchedDescriptions: a released
// description authorizes nothing (Permission), nor does a forged object
// token; a description of another file is Stale, as for TRUNCATE.
func TestDescribedAttrCallsRefuseDeadOrMismatchedDescriptions(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	cred := f.cred()
	file := fdAttrSetup(t, f, cred)
	_, b := f.call(t, cred, "CREATE", map[string]any{"parent": file.parent, "name": "62", "mode": 0o600, "umask": 0, "flags": fdAttrCreate})
	other := capabilityAt(t, b, 1)
	if r := getattr(t, f, cred, file.object, &other); r["code"] != "Stale" {
		t.Fatal("mismatched getattr:", r)
	}
	if r := setattr(t, f, cred, file.object, &other, map[string]any{"mode": 0o600}); r["code"] != "Stale" {
		t.Fatal("mismatched setattr:", r)
	}
	forged := file.object
	forged.Token = digest([]byte("never-issued"))
	if r := getattr(t, f, cred, forged, &file.description); r["code"] != "Permission" {
		t.Fatal("forged object:", r)
	}
	if _, r := f.call(t, cred, "DESCRIPTION_RELEASE", map[string]any{"description": file.description, "release_sequence": 1}); r["code"] != "OK" {
		t.Fatal(r)
	}
	if r := getattr(t, f, cred, file.object, &file.description); r["code"] != "Permission" {
		t.Fatal("released getattr:", r)
	}
	if r := setattr(t, f, cred, file.object, &file.description, map[string]any{"mode": 0o600}); r["code"] != "Permission" {
		t.Fatal("released setattr:", r)
	}
}

// TestDescribedAttrCallsRefuseForeignSessionDescription: a genuine description
// issued to another session cannot back GETATTR/SETATTR here, neither as
// issued nor re-scoped to this session.
func TestDescribedAttrCallsRefuseForeignSessionDescription(t *testing.T) {
	f := newBridge(t)
	second := f.c
	second.Session, second.GuestBoot, second.GuestMount, second.VM = uid(150).String(), uid(151).String(), uid(152).String(), uid(153).String()
	second.OriginNode, second.OriginIncarnation, second.OriginJournal = uid(21).String(), uid(154).String(), uid(155).String()
	f.driver.allowed = map[Context]bool{second: true}
	config := f.s.config
	config.Scopes = []Context{f.c, second}
	service, e := New(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	f.s = service
	first := f.c
	f.hello(t)
	own := fdAttrSetup(t, f, f.cred())
	f.c = second
	f.hello(t)
	foreign := fdAttrSetupNamed(t, f, f.cred(), "62") // both sessions share the volume
	rescoped := foreign.description
	rescoped.Scope = first
	f.c = first
	for _, d := range []Capability{foreign.description, rescoped} {
		if r := getattr(t, f, f.cred(), own.object, &d); r["code"] != "Permission" {
			t.Fatal("foreign getattr:", r)
		}
		if r := setattr(t, f, f.cred(), own.object, &d, map[string]any{"mode": 0o600}); r["code"] != "Permission" {
			t.Fatal("foreign setattr:", r)
		}
	}
}

// fdAttrArgs canonically decodes test Args the way the command codec does.
func fdAttrArgs(t *testing.T, args map[string]any) any {
	t.Helper()
	v, e := Canonical(mustJSON(t, args))
	if e != nil {
		t.Fatal(e)
	}
	return v
}

// TestDescribedAttrSchema pins the two closed GETATTR/SETATTR shapes: the
// original one is unchanged, the described one needs a real DESCRIPTION
// (a null or a PATH capability is Invalid), and no other path opcode
// gained a description key.
func TestDescribedAttrSchema(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	file := fdAttrSetup(t, f, f.cred())
	set := attrs(map[string]any{"mode": 0o600})
	type shape struct {
		op   string
		args map[string]any
	}
	for _, c := range []shape{
		{"GETATTR", map[string]any{"object": file.object}},
		{"GETATTR", map[string]any{"object": file.object, "description": file.description}},
		{"SETATTR", map[string]any{"object": file.object, "attributes": set}},
		{"SETATTR", map[string]any{"object": file.object, "attributes": set, "description": file.description}},
	} {
		if e := validateArgs(c.op, fdAttrArgs(t, c.args)); e != nil {
			t.Fatal(c.op, c.args, e)
		}
	}
	for _, c := range []shape{
		{"GETATTR", map[string]any{"object": file.object, "description": nil}},
		{"SETATTR", map[string]any{"object": file.object, "attributes": set, "description": nil}},
		{"GETATTR", map[string]any{"object": file.object, "description": file.object}},
		{"GETATTR", map[string]any{"description": file.description}},
		{"READLINK", map[string]any{"object": file.object, "description": file.description}},
		{"OPEN", map[string]any{"object": file.object, "flags": fdAttrCreate, "description": file.description}},
	} {
		if e := validateArgs(c.op, fdAttrArgs(t, c.args)); !errors.Is(e, fsapi.Invalid) {
			t.Fatal(c.op, c.args, e)
		}
	}
}

// TestDescribedAttrOldVersionBehaviour covers both mixed pairs. A pre-s64
// host checks GETATTR/SETATTR against its closed field sets alone, so it
// refuses the described shape as Invalid at decode, before any claim is
// admitted. A pre-s64 guest sends only the original shape, which an s64
// host still accepts as path authority, as before.
func TestDescribedAttrOldVersionBehaviour(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	file := fdAttrSetup(t, f, f.cred())
	described := map[string]map[string]any{
		"GETATTR": {"object": file.object, "description": file.description},
		"SETATTR": {"object": file.object, "attributes": attrs(map[string]any{"mode": 0o600}), "description": file.description},
	}
	for op, args := range described {
		if _, e := object(fdAttrArgs(t, args), strings.Split(argFields[op], " ")...); !errors.Is(e, fsapi.Invalid) {
			t.Fatal("pre-s64 field set accepted described", op, e)
		}
		delete(args, "description")
		if _, e := object(fdAttrArgs(t, args), strings.Split(argFields[op], " ")...); e != nil {
			t.Fatal("original", op, "shape changed:", e)
		}
	}
	if r := getattr(t, f, f.cred(), file.object, nil); r["code"] != "OK" {
		t.Fatal("original getattr:", r)
	}
	if r := setattr(t, f, f.cred(), file.object, nil, map[string]any{"mode": 0o600}); r["code"] != "OK" {
		t.Fatal("original setattr:", r)
	}
}

// TestDescribedAttrDecodesUnderControlledAllocation: the described shape is
// part of the shared Args union, so a controlled ALLOCATE (frame minor 2,
// claim issuance) carrying it decodes, and its null-description variant is
// refused before any number is issued.
func TestDescribedAttrDecodesUnderControlledAllocation(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	file := fdAttrSetup(t, f, f.cred())
	for _, d := range []any{file.description, nil} {
		intent := Intent{Opcode: "GETATTR", Args: mustJSON(t, map[string]any{"object": file.object, "description": d})}
		call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "616263", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 1, OriginalHash: wireHash(intent)}, Credentials: f.cred(), Intent: intent}
		protocol := ControlledProtocol()
		raw, e := marshal(Command{Protocol: &protocol, Cell: 7, Context: f.c, Op: "ALLOCATE", Payload: mustJSON(t, call)})
		if e != nil {
			t.Fatal(e)
		}
		_, e = DecodeCommandVersion(raw, 2)
		if d != nil && e != nil || d == nil && !errors.Is(e, fsapi.Invalid) {
			t.Fatalf("described=%v: %v", d != nil, e)
		}
	}
}
