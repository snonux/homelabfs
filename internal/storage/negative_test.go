package storage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

type closureProbe struct {
	calls int
	homes []fsapi.UUID
	fail  bool
}

func (c *closureProbe) Enter(_ context.Context, _ Operation, homes []fsapi.UUID) (func(), error) {
	c.calls++
	c.homes = append([]fsapi.UUID(nil), homes...)
	if c.fail {
		return nil, fsapi.Unavailable
	}
	return func() {}, nil
}

func TestInvalidOperationsPreserveCatalog(t *testing.T) {
	cases := map[string]func(*fixture, Operation) Operation{
		"gap":                 func(_ *fixture, o Operation) Operation { o.Position.Sequence++; return o },
		"wrong-history":       func(_ *fixture, o Operation) Operation { o.Position.Generation = uuid(900); return o },
		"wrong-configuration": func(_ *fixture, o Operation) Operation { o.Position.Configuration = uuid(900); return o },
		"revision":            func(_ *fixture, o Operation) Operation { o.Objects[0].Before++; return o },
		"revision-wrap":       func(_ *fixture, o Operation) Operation { o.Objects[0].After.Attr.Revision = 0; return o },
		"name-dot": func(_ *fixture, o Operation) Operation {
			o.Entries[0].Before.Name = ".."
			o.Entries[0].After.Name = ".."
			return o
		},
		"name-slash": func(_ *fixture, o Operation) Operation {
			o.Entries[0].Before.Name = "x/y"
			o.Entries[0].After.Name = "x/y"
			return o
		},
		"name-nul": func(_ *fixture, o Operation) Operation {
			o.Entries[0].Before.Name = "x\x00y"
			o.Entries[0].After.Name = "x\x00y"
			return o
		},
		"name-long": func(_ *fixture, o Operation) Operation {
			o.Entries[0].Before.Name = strings.Repeat("a", 256)
			o.Entries[0].After.Name = o.Entries[0].Before.Name
			return o
		},
		"creation-descriptor": func(_ *fixture, o Operation) Operation { o.Objects[1].After.Descriptor.Creation = uuid(909); return o },
		"mode":                func(_ *fixture, o Operation) Operation { o.Objects[1].After.Attr.Mode = 0o10000; return o },
		"timestamp": func(_ *fixture, o Operation) Operation {
			o.Objects[1].After.Attr.Ctime.Nanoseconds = 1000000000
			return o
		},
		"link-ledger":         func(_ *fixture, o Operation) Operation { o.Objects[1].After.Attr.Nlink = 5; return o },
		"entry-incarnation":   func(_ *fixture, o Operation) Operation { o.Entries[0].After.Revision = 3; return o },
		"entry-target":        func(_ *fixture, o Operation) Operation { x := objectID(333); o.Entries[0].After.Target = &x; return o },
		"missing-parent-edit": func(_ *fixture, o Operation) Operation { o.Objects = o.Objects[1:]; return o },
		"participant-set":     func(_ *fixture, o Operation) Operation { o.Participants = nil; return o },
		"duplicate-edit":      func(_ *fixture, o Operation) Operation { o.Objects = append(o.Objects, o.Objects[1]); return o },
		"disguised-write":     func(_ *fixture, o Operation) Operation { o.Data = []byte("x"); return o },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixtureFor(t, "simulator")
			before := f.image()
			op := corrupt(f, f.create("x", fsapi.Regular, objectID(2)))
			if err := f.store.Apply(t.Context(), op); err == nil {
				t.Fatal("accepted invalid operation")
			}
			after := f.image()
			a, _ := EncodeImage(before, f.config.Limits)
			b, _ := EncodeImage(after, f.config.Limits)
			if !bytes.Equal(a, b) {
				t.Fatal("invalid operation changed catalog")
			}
		})
	}
}

func TestFormatMarkerStrictness(t *testing.T) {
	for _, corrupt := range []struct {
		name string
		fn   func([]byte) []byte
	}{
		{"duplicate", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"magic":`), []byte(`"magic":"HOMELABFS-STORE","magic":`), 1)
		}},
		{"alias-case", func(b []byte) []byte { return bytes.Replace(b, []byte(`"magic":`), []byte(`"Magic":`), 1) }},
		{"semantic-duplicate", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"magic":`), []byte(`"Magic":"HOMELABFS-STORE","magic":`), 1)
		}},
		{"unknown", func(b []byte) []byte { return bytes.Replace(b, []byte(`"magic":`), []byte(`"extra":1,"magic":`), 1) }},
		{"missing-minor", func(b []byte) []byte { return bytes.Replace(b, []byte(`"minor":0,`), nil, 1) }},
		{"unknown-version", func(b []byte) []byte { return bytes.Replace(b, []byte(`"major":1`), []byte(`"major":2`), 1) }},
		{"role", func(b []byte) []byte { return bytes.Replace(b, []byte(`"role":"data"`), []byte(`"role":"journal"`), 1) }},
		{"invalid-utf8", func(b []byte) []byte { return append([]byte{0xff}, b...) }},
		{"no-newline", func(b []byte) []byte { return b[:len(b)-1] }},
		{"trailing", func(b []byte) []byte { return append(b, []byte("{}\n")...) }},
	} {
		t.Run(corrupt.name, func(t *testing.T) {
			f := fixtureFor(t, "simulator")
			image := f.image()
			raw, err := readPrivate(t.Context(), f.data, "format.json", 0, 10000)
			if err != nil {
				t.Fatal(err)
			}
			replaceFile(t, f.data, "format.json", corrupt.fn(raw))
			if _, err = Open(t.Context(), f.data, f.journal, f.config, image); err == nil {
				t.Fatal("accepted invalid marker")
			}
			if _, err = Format(t.Context(), f.data, f.journal, f.config, image.Objects[0], image.Position); err == nil {
				t.Fatal("reformatted existing roots")
			}
		})
	}
}

func replaceFile(t *testing.T, f Filesystem, p string, raw []byte) {
	t.Helper()
	file, err := f.Open(t.Context(), p, os.O_RDWR|os.O_TRUNC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if _, err = file.WriteAt(t.Context(), raw, 0); err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAdmissionAndPhysicalAliases(t *testing.T) {
	t.Run("same-root", func(t *testing.T) {
		f := fixtureFor(t, "simulator")
		if _, err := Open(t.Context(), f.data, f.data, f.config, f.image()); err == nil {
			t.Fatal("aliased roots accepted")
		}
	})
	for _, kind := range []string{"external-hardlink", "internal-hardlink", "symlink", "payload-permissions", "root-permissions", "unexpected-file", "wrong-identity", "overlap"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureFor(t, "os")
			id := objectID(2)
			f.apply(f.create("x", fsapi.Regular, id))
			image := f.image()
			p := filepath.Join(f.dataDir, objectPath(id), "payload")
			var err error
			switch kind {
			case "external-hardlink":
				err = os.Link(p, filepath.Join(t.TempDir(), "alias"))
			case "internal-hardlink":
				err = os.Link(p, filepath.Join(f.dataDir, "staging", "alias"))
			case "symlink":
				err = os.Remove(p)
				if err == nil {
					err = os.Symlink(filepath.Join(t.TempDir(), "outside"), p)
				}
			case "payload-permissions":
				err = os.Chmod(p, 0o644)
			case "root-permissions":
				err = os.Chmod(f.dataDir, 0o755)
			case "unexpected-file":
				err = os.WriteFile(filepath.Join(f.dataDir, "unexpected"), nil, 0o600)
			case "wrong-identity":
				raw, e := os.ReadFile(filepath.Join(f.dataDir, objectPath(id), "identity.json"))
				if e != nil {
					t.Fatal(e)
				}
				err = os.WriteFile(filepath.Join(f.dataDir, objectPath(id), "identity.json"), bytes.ReplaceAll(raw, []byte(`"type":1`), []byte(`"type":2`)), 0o600)
			case "overlap":
				p := filepath.Join(f.dataDir, "nested")
				err = os.Mkdir(p, 0o700)
				if err == nil {
					j, e := vfs.NewOS(t.Context(), p)
					if e != nil {
						t.Fatal(e)
					}
					t.Cleanup(func() { _ = j.Close() })
					f.journal = j
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Open(t.Context(), f.data, f.journal, f.config, image); err == nil {
				t.Fatal("unsafe physical tree admitted")
			}
		})
	}
}

func TestImageValidation(t *testing.T) {
	f := fixtureFor(t, "simulator")
	f.apply(f.create("x", fsapi.Regular, objectID(2)))
	image := f.image()
	raw, err := EncodeImage(image, f.config.Limits)
	if err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][]byte{
		"duplicate-object":  bytes.Replace(raw, []byte(`"objects":[`), []byte(`"objects":[`), 1),
		"base64":            bytes.Replace(raw, []byte(`"name_base64":"eA=="`), []byte(`"name_base64":"eA"`), 1),
		"future-version":    bytes.Replace(raw, []byte(`"major":1`), []byte(`"major":4`), 1),
		"negative-position": bytes.Replace(raw, []byte(`"sequence":1`), []byte(`"sequence":-1`), 1),
	} {
		if name == "duplicate-object" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeImage(bad, Limits{}); err == nil {
				t.Fatal("invalid image accepted")
			}
		})
	}
	copyImage := image
	copyImage.Objects = append(append([]Object(nil), image.Objects...), image.Objects[0])
	if _, err = EncodeImage(copyImage, f.config.Limits); err == nil {
		t.Fatal("duplicate objects accepted")
	}
	if _, err = DecodeImage(raw, Limits{FileBytes: 4}); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal(err)
	}
}

func TestForeignEffectsRequireClosure(t *testing.T) {
	f := fixtureFor(t, "simulator")
	foreign := fsapi.ObjectID{Home: uuid(77), Allocation: uuid(78)}
	op := f.op(Link, foreign)
	parent := edit(f.get(f.config.Root))
	op.Objects = []ObjectChange{parent}
	op.Entries = []EntryChange{f.entry("foreign", &foreign)}
	op.Participants = []fsapi.UUID{uuid(1), uuid(77)}
	if err := f.store.Apply(t.Context(), op); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal(err)
	}
	if _, err := f.store.Lookup(t.Context(), f.config.Root, "foreign"); !errors.Is(err, fsapi.NotFound) {
		t.Fatal("partial foreign publish")
	}
	probe := &closureProbe{fail: true}
	f.store.config.Closure = probe
	if err := f.store.Apply(t.Context(), op); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal(err)
	}
	probe.fail = false
	f.apply(op)
	e, err := f.store.Lookup(t.Context(), f.config.Root, "foreign")
	if err != nil || *e.Target != foreign || probe.calls != 2 {
		t.Fatalf("foreign route: %v %v calls=%d", e, err, probe.calls)
	}
}

func TestOldForeignLedgerDependenciesRetained(t *testing.T) {
	f := fixtureFor(t, "simulator")
	id := objectID(2)
	f.apply(f.create("local", fsapi.Regular, id))
	probe := &closureProbe{}
	f.store.config.Closure = probe
	// A coordinated home participant adds a foreign namespace reference; its
	// matching remote entry is installed by the external closure coordinator.
	foreignParent := fsapi.ObjectID{Home: uuid(77), Allocation: uuid(78)}
	link := f.op(Link, id)
	c := edit(f.get(id))
	c.After.Attr.Nlink++
	c.After.LedgerRevision++
	c.After.References = append(c.After.References, Reference{foreignParent, "remote", 1})
	link.Objects = []ObjectChange{c}
	link.Participants = []fsapi.UUID{uuid(1), uuid(77)}
	f.apply(link)
	unlink := f.op(Unlink, id)
	c = edit(f.get(id))
	c.After.Attr.Nlink--
	c.After.LedgerRevision++
	c.After.References = c.After.References[:1]
	unlink.Objects = []ObjectChange{c}
	unlink.Participants = []fsapi.UUID{uuid(1)}
	if err := f.store.Apply(t.Context(), unlink); !errors.Is(err, fsapi.Invalid) {
		t.Fatalf("omitted old foreign home accepted: %v", err)
	}
	unlink.Participants = []fsapi.UUID{uuid(1), uuid(77)}
	f.apply(unlink)
	if len(probe.homes) != 2 {
		t.Fatal("old foreign home omitted from closure")
	}
}

func TestNamespaceReplacementPreconditions(t *testing.T) {
	t.Run("file-directory-kind", func(t *testing.T) {
		f := fixtureFor(t, "simulator")
		f.apply(f.create("file", fsapi.Regular, objectID(2)))
		f.apply(f.create("dir", fsapi.Directory, objectID(3)))
		if err := f.store.Apply(t.Context(), f.rename("file", "dir")); err == nil {
			t.Fatal("file replaced directory")
		}
	})
	t.Run("same-object-noop", func(t *testing.T) {
		f := fixtureFor(t, "simulator")
		id := objectID(2)
		f.apply(f.create("file", fsapi.Regular, id))
		f.apply(f.link(id, "alias"))
		op := f.op(Rename, id)
		f.apply(op)
		if f.get(id).Attr.Nlink != 2 {
			t.Fatal("same-object no-op changed references")
		}
	})
	t.Run("stale-entry-replacement", func(t *testing.T) {
		f := fixtureFor(t, "simulator")
		first := objectID(2)
		f.apply(f.create("file", fsapi.Regular, first))
		old := f.unlink("file")
		f.apply(old)
		f.apply(f.create("file", fsapi.Regular, objectID(3)))
		stale := cloneOperation(old)
		stale.ID = uuid(999)
		stale.Position = f.store.position
		stale.Position.Sequence++
		if err := f.store.Apply(t.Context(), stale); !errors.Is(err, fsapi.Stale) {
			t.Fatal(err)
		}
		entry, err := f.store.Lookup(t.Context(), f.config.Root, "file")
		if err != nil || *entry.Target != objectID(3) {
			t.Fatal("stale unlink touched replacement")
		}
	})
}
