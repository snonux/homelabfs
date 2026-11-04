package apply

import (
	"bytes"
	"errors"
	"os"
	"path"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestBarrieredAppliedReopenActualCleanFiles(t *testing.T) {
	for _, fault := range []string{"none", "payload", "catalog", "APPLIED", "advanced-journal", "missing-prefix"} {
		t.Run(fault, func(t *testing.T) {
			f := newFixture(t, "os")
			f.create("retained", oid(20))
			f.write(oid(20), 0, bytes.Repeat([]byte("original payload"), 128))
			data, journal, progress := fresh(t, "os", vfs.Config{}), fresh(t, "os", vfs.Config{}), fresh(t, "os", vfs.Config{})
			original, err := f.replay(data, journal, progress, f.config)
			if err != nil {
				t.Fatal(err)
			}
			token, err := original.SuspendRestart(t.Context(), f.reader(), 64<<10)
			if err != nil {
				t.Fatal(err)
			}
			oldStore := original.Store
			if fault == "none" {
				if _, err = f.replay(data, journal, progress, f.config); !errors.Is(err, fsapi.Stale) {
					t.Fatalf("normal Replay workspace-reuse gate weakened: %v", err)
				}
			}
			switch fault {
			case "payload":
				allocation := oid(20).Allocation.String()
				file, err := data.Open(t.Context(), path.Join("objects", allocation[:2], allocation, "payload"), os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.WriteAt(t.Context(), []byte("changed"), 0); err != nil {
					t.Fatal(err)
				}
				if err = file.Close(); err != nil {
					t.Fatal(err)
				}
			case "catalog":
				p, err := decodeProgress(token.progress)
				if err != nil {
					t.Fatal(err)
				}
				file, err := progress.Open(t.Context(), imageName(p.workspace, p.index), os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.WriteAt(t.Context(), []byte("!"), 0); err != nil {
					t.Fatal(err)
				}
				if err = file.Close(); err != nil {
					t.Fatal(err)
				}
			case "APPLIED":
				if err = progress.Unlink(t.Context(), "APPLIED"); err != nil {
					t.Fatal(err)
				}
			case "advanced-journal":
				f.write(oid(20), 0, []byte("later source cut"))
			case "missing-prefix":
				if err = f.writer.Close(); err != nil {
					t.Fatal(err)
				}
				dir, err := f.source.OpenDir(t.Context(), f.stream.Directory())
				if err != nil {
					t.Fatal(err)
				}
				entries, err := dir.ReadDir(t.Context(), 64)
				_ = dir.Close()
				if err != nil && len(entries) == 0 {
					t.Fatal(err)
				}
				removed := false
				for _, entry := range entries {
					if entry.Info.Mode.IsRegular() {
						if err = f.source.Unlink(t.Context(), path.Join(f.stream.Directory(), entry.Name)); err != nil {
							t.Fatal(err)
						}
						removed = true
						break
					}
				}
				if !removed {
					t.Fatal("no actual journal prefix file removed")
				}
			}
			source, err := segment.OpenReader(t.Context(), f.source, f.stream, segment.RecoveryOptions{}, f.position(1))
			if err != nil {
				if fault == "missing-prefix" {
					return
				}
				t.Fatal(err)
			}
			defer func() { _ = source.Close() }()
			// The old Result/Store is never supplied to the reopening operation.
			original = Result{}
			reopened, err := token.Reopen(t.Context(), data, journal, progress, source, f.config.Authorizer)
			if fault != "none" {
				if err == nil {
					t.Fatal("changed or missing actual applied input restored")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if reopened.Store == oldStore || reopened.Applied != f.selection.Effects[len(f.selection.Effects)-1] {
				t.Fatal("old RAM store or changed applied identity")
			}
			f.oracle(reopened.Store)
			if _, err = token.Reopen(t.Context(), data, journal, progress, f.reader(), f.config.Authorizer); !errors.Is(err, fsapi.Permission) {
				t.Fatalf("restart token reused: %v", err)
			}
		})
	}
}
