package apply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestReplayRejectsChecksummedProgressSemantics(t *testing.T) {
	for _, test := range []string{"source-reference", "selected-prefix", "image-cut", "workspace-reuse"} {
		t.Run(test, func(t *testing.T) {
			f := newFixture(t, "sim")
			f.create("a", oid(20))
			f.write(oid(20), 0, []byte("not applied yet"))
			failedData, failedJournal, progress := fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{})
			stop := errors.New("failed attempt before write")
			config := f.config
			hits := 0
			config.Hook = func(_ context.Context, b Boundary) error {
				if b == BeforeApply {
					hits++
					if hits == 3 {
						return stop
					}
				}
				return nil
			}
			if _, err := f.replay(failedData, failedJournal, progress, config); !errors.Is(err, stop) {
				t.Fatal(err)
			}
			raw, err := readFile(t.Context(), progress, "APPLIED", 0, progressSize)
			if err != nil {
				t.Fatal(err)
			}
			pointer, err := decodeProgress(raw)
			if err != nil {
				t.Fatal(err)
			}
			imagePath := imageName(pointer.workspace, pointer.index)
			imageBytes, err := readFile(t.Context(), progress, imagePath, 0, maxImageBytes(config))
			if err != nil {
				t.Fatal(err)
			}
			image, err := storage.DecodeImage(imageBytes, config.Storage.Limits)
			if err != nil {
				t.Fatal(err)
			}
			config.Hook = nil
			config.Workspace = uid(1001)
			switch test {
			case "source-reference":
				pointer.source.SHA256[0] ^= 1
			case "selected-prefix":
				pointer.prefix[0] ^= 1
			case "image-cut":
				pointer.slots-- // Valid pointer, incompatible with its valid image.
			case "workspace-reuse":
				config.Workspace = pointer.workspace
			}
			poisoned := encodeProgress(pointer)
			if _, err := decodeProgress(poisoned); err != nil {
				t.Fatalf("negative failed checksum/schema before semantic check: %v", err)
			}
			if err := writeFile(t.Context(), progress, "replaced-progress", poisoned); err != nil {
				t.Fatal(err)
			}
			if err := progress.Rename(t.Context(), "replaced-progress", "APPLIED"); err != nil {
				t.Fatal(err)
			}
			if err := progress.SyncDir(t.Context(), "."); err != nil {
				t.Fatal(err)
			}
			data, journal := fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{})
			result, err := f.replay(data, journal, progress, config)
			if !errors.Is(err, fsapi.Stale) || result.Store != nil {
				t.Fatalf("semantic progress mismatch returned store or wrong error: %+v %v", result, err)
			}
			for _, root := range []storage.Filesystem{data, journal} {
				if _, err := root.Stat(t.Context(), "format.json"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("rejected recovery formatted destination: %v", err)
				}
			}
			retained, err := readFile(t.Context(), progress, "APPLIED", 0, progressSize)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(retained, poisoned) {
				t.Fatal("recovery rewrote invalid APPLIED evidence")
			}
			retained, err = readFile(t.Context(), progress, imagePath, 0, maxImageBytes(config))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(retained, imageBytes) {
				t.Fatal("recovery changed the failed attempt's immutable image")
			}
			// The previous failed workspace stays intact and reopens at its exact clean
			// published cut. No write payload or metadata from the rejected attempt leaks.
			old, err := storage.Open(t.Context(), failedData, failedJournal, config.Storage, image)
			if err != nil {
				t.Fatal(err)
			}
			exported, err := old.Export(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			after, err := storage.EncodeImage(exported, config.Storage.Limits)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, imageBytes) {
				t.Fatal("recovery modified preserved failed roots")
			}
		})
	}
}
