package apply

import (
	"errors"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestAdvanceVerifiesPrefixAndPoisonsFailure(t *testing.T) {
	for _, kind := range []string{"source-rewrite", "selection-rewrite", "authority-denied", "budget"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "sim")
			f.create("a", oid(20))
			result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
			if err != nil {
				t.Fatal(err)
			}
			selection := cloneSelection(f.selection)
			switch kind {
			case "source-rewrite":
				reader := f.reader()
				entry, err := reader.Next(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if err = reader.Close(); err != nil {
					t.Fatal(err)
				}
				entry.Record.Header.Observed.Seconds++
				raw, err := record.Encode(entry.Record)
				if err != nil {
					t.Fatal(err)
				}
				recovery, err := segment.Recover(t.Context(), f.source, f.stream, segment.RecoveryOptions{})
				if err != nil {
					t.Fatal(err)
				}
				file, err := f.source.Open(t.Context(), recovery.Segments[0].Name, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.WriteAt(t.Context(), raw, 0); err != nil {
					t.Fatal(err)
				}
				if err = file.Sync(t.Context()); err != nil {
					t.Fatal(err)
				}
				if err = file.Close(); err != nil {
					t.Fatal(err)
				}
			case "selection-rewrite":
				f.write(oid(20), 0, []byte("new"))
				selection = cloneSelection(f.selection)
				selection.Effects = append(selection.Effects[:1], selection.Effects[2:]...)
			case "authority-denied":
				selection.Proof = []byte("not authorized")
			case "budget":
				f.write(oid(20), 0, []byte("new"))
				selection = cloneSelection(f.selection)
				result.config.MaxProgressBytes = result.used
			}
			err = result.Advance(t.Context(), f.reader(), selection)
			if err == nil {
				t.Fatal("invalid extension succeeded")
			}
			if err = result.Advance(t.Context(), f.reader(), f.selection); !errors.Is(err, fsapi.Unavailable) {
				t.Fatal("failed result reused", err)
			}
		})
	}
}

func TestAdvanceInstallsSuffixAndPreservesOracle(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	f.write(oid(20), 0, []byte("first"))
	result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
	if err != nil {
		t.Fatal(err)
	}
	before := result.Slots
	f.write(oid(20), 0, []byte("later"))
	f.create("b", oid(21))
	f.rename("a", "b")
	if err = result.Advance(t.Context(), f.reader(), f.selection); err != nil {
		t.Fatal(err)
	}
	if result.Slots != before+3 {
		t.Fatal("wrong suffix slots", result.Slots)
	}
	f.oracle(result.Store)
}
