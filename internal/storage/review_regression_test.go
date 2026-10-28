package storage

import (
	"bytes"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestReclaimFormerlyPopulatedDirectory(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := fixtureFor(t, backend)
			dir, file := objectID(2), objectID(3)
			var operations []Operation
			apply := func(op Operation) { f.apply(op); operations = append(operations, cloneOperation(op)) }
			apply(f.create("dir", fsapi.Directory, dir))
			child := Object{Descriptor: Descriptor{Object: file, Kind: fsapi.Regular}, Attr: attrs(file, fsapi.Regular, 1), LedgerRevision: 1, References: []Reference{{Parent: dir, Name: "child", Revision: 1}}}
			create := f.op(Create, file)
			child.Descriptor.Creation = create.ID
			entry := Entry{Parent: dir, Name: "child", Revision: 1, Target: &file}
			create.Objects = []ObjectChange{edit(f.get(dir)), {After: child}}
			create.Entries = []EntryChange{{Before: Entry{Parent: dir, Name: "child"}, After: entry}}
			apply(create)
			unlink := f.op(Unlink, file)
			removed := edit(f.get(file))
			removed.After.References = nil
			removed.After.Attr.Nlink = 0
			removed.After.LedgerRevision++
			unlink.Objects = []ObjectChange{edit(f.get(dir)), removed}
			tombstone := Entry{Parent: dir, Name: "child", Revision: 2}
			unlink.Entries = []EntryChange{{Before: entry, After: tombstone}}
			apply(unlink)
			apply(f.unlink("dir"))
			apply(f.op(Reclaim, dir))
			for _, op := range operations {
				if err := f.store.Apply(t.Context(), op); err != nil {
					t.Fatalf("old kind=%d retry: %v", op.Kind, err)
				}
			}
			if _, err := f.store.Stat(t.Context(), dir); !errors.Is(err, fsapi.NotFound) {
				t.Fatalf("directory remained live: %v", err)
			}
			retained, ok := f.store.entries[entryKey{parent: dir, name: "child"}]
			if !ok || retained.Target != nil || retained.Revision != 2 {
				t.Fatalf("lost child name incarnation: %+v", retained)
			}
			image := f.image()
			reopened, err := Open(t.Context(), f.data, f.journal, f.config, image)
			if err != nil {
				t.Fatal(err)
			}
			f.store = reopened
			for _, op := range operations {
				if err = f.store.Apply(t.Context(), op); err != nil {
					t.Fatal(err)
				}
			}
			roundtrip := f.image()
			before, err := EncodeImage(image, f.config.Limits)
			if err != nil {
				t.Fatal(err)
			}
			after, err := EncodeImage(roundtrip, f.config.Limits)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("reclaimed-directory image changed: %v", err)
			}
			if _, err = f.store.Lookup(t.Context(), dir, "child"); !errors.Is(err, fsapi.NotFound) {
				t.Fatalf("retired directory became traversable: %v", err)
			}
			t.Run("requires-retired-directory-proof", func(t *testing.T) {
				missing := image
				missing.Retired = nil
				if _, err := EncodeImage(missing, f.config.Limits); err == nil {
					t.Fatal("accepted tombstone under unknown parent")
				}
				wrongType := image
				wrongType.Retired = append([]Descriptor(nil), image.Retired...)
				wrongType.Retired[0].Kind = fsapi.Regular
				if _, err := EncodeImage(wrongType, f.config.Limits); err == nil {
					t.Fatal("accepted tombstone under retired regular file")
				}
				live := image
				live.Entries = append([]Entry(nil), image.Entries...)
				for i := range live.Entries {
					if live.Entries[i].Parent == dir {
						live.Entries[i].Target = &file
					}
				}
				if _, err := EncodeImage(live, f.config.Limits); err == nil {
					t.Fatal("accepted live entry under retired directory")
				}
			})
		})
	}
}

func TestConfiguredLargeImageLimitsRoundtripAndPublication(t *testing.T) {
	f := fixtureFor(t, "simulator")
	image := f.image()
	// Use a verified synthetic catalog prefix so exercising a legitimate larger
	// admission cap does not require 16K filesystem operations in the test.
	const operations = 16385
	image.Position.Sequence = operations
	image.Receipts = make([]Receipt, operations)
	for i := range image.Receipts {
		image.Receipts[i] = Receipt{Sequence: uint64(i) + 1, ID: uuid(uint64(i) + 100000)}
	}
	limits := Limits{Operations: operations}
	if _, err := EncodeImage(image, Limits{}); err == nil {
		t.Fatal("default admission cap ignored")
	}
	raw, err := EncodeImage(image, limits)
	if err != nil {
		t.Fatal("configured encode rejected valid catalog", err)
	}
	decoded, err := DecodeImage(raw, limits)
	if err != nil {
		t.Fatal("configured decode rejected valid catalog", err)
	}
	again, err := EncodeImage(decoded, limits)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatalf("larger-limit roundtrip: %v", err)
	}
	if _, err = EncodeImage(image, Limits{Operations: operations, FileBytes: int64(len(raw)) - 1}); !errors.Is(err, fsapi.NoSpace) {
		t.Fatalf("encode byte limit ignored: %v", err)
	}
	f.config.Limits = limits
	f.store, err = Open(t.Context(), f.data, f.journal, f.config, decoded)
	if err != nil {
		t.Fatal("configured open rejected valid catalog", err)
	}
	exported, err := f.store.Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := uuid(90000)
	if err = f.store.StageCheckpoint(t.Context(), checkpoint, exported, nil); err != nil {
		t.Fatal("configured staging rejected valid catalog", err)
	}
	if err = f.store.PublishCheckpoint(t.Context(), checkpoint); err != nil {
		t.Fatal(err)
	}
	if err = f.store.PublishCurrent(t.Context(), checkpoint, uuid(90001)); err != nil {
		t.Fatal(err)
	}
	selected, published, err := f.store.ReadCurrent(t.Context())
	if err != nil || selected != checkpoint || len(published.Receipts) != operations {
		t.Fatalf("larger-limit publication: selected=%s receipts=%d error=%v", selected, len(published.Receipts), err)
	}
}
