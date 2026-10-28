package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type baselineAuthority func(context.Context, BaselineAuthorization) error

func (f baselineAuthority) AuthorizeBaseline(ctx context.Context, a BaselineAuthorization) error {
	return f(ctx, a)
}

func (f *fixture) sealSelection() {
	f.t.Helper()
	if err := f.writer.Seal(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	r := f.reader()
	for {
		e, err := r.Next(f.t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			f.t.Fatal(err)
		}
		f.selection.Through = e.Reference
	}
	if err := r.Close(); err != nil {
		f.t.Fatal(err)
	}
}

func TestBaselineImmutablePayloadSuffixAndRetry(t *testing.T) {
	for _, backend := range []string{"sim", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := newFixture(t, backend)
			f.create("a", oid(20))
			f.write(oid(20), 0, []byte("immutable before shrink"))
			f.sealSelection()
			result, err := f.replay(fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), f.config)
			if err != nil {
				t.Fatal(err)
			}
			base, err := result.Capture(t.Context(), f.stream, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := EncodeBaseline(base)
			if err != nil {
				t.Fatal(err)
			}
			authority := baselineAuthority(func(_ context.Context, a BaselineAuthorization) error {
				expected := f.config.Authorizer
				return expected.Authorize(t.Context(), a.Authorization)
			})
			f.truncate(oid(20), 2)
			f.truncate(oid(20), 31)
			f.write(oid(20), 25, []byte("suffix"))
			// Reuse an original selected COMMIT after the baseline. It must neither
			// consume a slot nor restore its old name or old data.
			old, err := record.Decode(base.Frames[base.Selection.Effects[1].Position.Sequence-1])
			if err != nil {
				t.Fatal(err)
			}
			f.unlink("a")
			f.create("a", oid(21))
			f.write(oid(21), 0, []byte("replacement"))
			f.append(old.Header.Operation, old.Payload, true)
			if err = result.Advance(t.Context(), f.reader(), f.selection); err != nil {
				t.Fatal(err)
			}
			after, err := EncodeBaseline(base)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(frozen, after) {
				t.Fatal("later payload mutation changed immutable base")
			}
			suffix, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, base.Frames)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = suffix.Close() }()
			config := f.config
			config.Workspace = uid(1001)
			restored, err := Resume(t.Context(), fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), suffix, f.selection, config, base, 1<<20, authority)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(restored.Store)
			if restored.Slots != result.Slots {
				t.Fatal("suffix retry changed slot mapping")
			}
			f.write(oid(20), 5, []byte("old id survives name reuse"))
			next, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, base.Frames)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = next.Close() }()
			if err = restored.Advance(t.Context(), next, f.selection); err != nil {
				t.Fatal(err)
			}
			f.oracle(restored.Store)
		})
	}
}

func TestBaselineRejectsCorruptionBeforeRestore(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	f.write(oid(20), 0, []byte("baseline"))
	f.sealSelection()
	result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
	if err != nil {
		t.Fatal(err)
	}
	base, err := result.Capture(t.Context(), f.stream, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	reject := baselineAuthority(func(context.Context, BaselineAuthorization) error { return fsapi.Permission })
	allow := baselineAuthority(func(context.Context, BaselineAuthorization) error { return nil })
	raw, err := EncodeBaseline(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"payload", "source", "slots", "applied", "authority", "selection"} {
		t.Run(kind, func(t *testing.T) {
			bad, err := DecodeBaseline(raw)
			if err != nil {
				t.Fatal(err)
			}
			authority := allow
			switch kind {
			case "payload":
				bad.Data[0][0] ^= 1
			case "source":
				bad.Frames[1][20] ^= 1
			case "slots":
				bad.Slots++
			case "applied":
				bad.Applied.Position.Sequence++
			case "authority":
				authority = reject
			case "selection":
				bad.Selection.Effects = bad.Selection.Effects[:1]
			}
			d := fresh(t, "sim", vfs.Config{})
			suffix, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, base.Frames)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = suffix.Close() }()
			_, err = Resume(t.Context(), d, fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), suffix, f.selection, f.config, bad, 1<<20, authority)
			if err == nil {
				t.Fatal("accepted corrupt baseline")
			}
			if kind == "authority" && !errors.Is(err, fsapi.Permission) {
				t.Fatal(err)
			}
			dir, err := d.OpenDir(t.Context(), ".")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = dir.Close() }()
			entries, err := dir.ReadDir(t.Context(), 1)
			if err != nil && err != io.EOF {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("wrote destination before validating base")
			}
		})
	}
}

func TestBaselineRetainsPreparedAndLifetimeEvidence(t *testing.T) {
	for _, kind := range []string{"prepared", "lifetime"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "sim")
			f.create("a", oid(20))
			if kind == "lifetime" {
				f.write(oid(20), 0, []byte("retained"))
			}
			var install record.Install
			if kind == "prepared" {
				install = preparedInstall(f, true)
			} else {
				f.append(uid(850), record.HandlePin{Pin: uid(70), Object: oid(20), ServingGeneration: uid(71), Incarnation: uid(7), Reasons: 1}, true)
				f.unlink("a")
			}
			f.sealSelection()
			result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
			if err != nil {
				t.Fatal(err)
			}
			base, err := result.Capture(t.Context(), f.stream, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "prepared" {
				f.append(uid(800), install, true)
				obj := f.objects[oid(20)]
				obj.meta = install.Redo.Objects[0].After
				obj.data = []byte("tx!")
				f.objects[oid(20)] = obj
			} else {
				f.append(uid(851), record.HandleRelease{Pin: uid(70), Object: oid(20), ServingGeneration: uid(71), Incarnation: uid(7), Reason: 1}, true)
				obj := f.objects[oid(20)]
				f.append(uid(852), record.Reclaim{Object: oid(20), ExpectedObjectRevision: obj.meta.Revision, ExpectedLedgerRevision: obj.ledger, Retention: f.selection.Effects[0]}, true)
				delete(f.objects, oid(20))
			}
			f.sealSelection()
			authority := baselineAuthority(func(context.Context, BaselineAuthorization) error { return nil })
			suffix, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, base.Frames)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = suffix.Close() }()
			config := f.config
			config.Workspace = uid(1001)
			restored, err := Resume(t.Context(), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), suffix, f.selection, config, base, 1<<20, authority)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(restored.Store)
			// A second clean baseline retains released pin tombstones and retired
			// allocations too; restoring it cannot resurrect reclaimed payloads.
			second, err := restored.Capture(t.Context(), f.stream, 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			suffix2, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, second.Frames)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = suffix2.Close() }()
			config.Workspace = uid(1002)
			again, err := Resume(t.Context(), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), suffix2, f.selection, config, second, 1<<20, authority)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(again.Store)
			image, err := again.Store.Export(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "lifetime" && (len(image.Retired) != 1 || len(image.Pins) != 1 || image.Pins[0].Active) {
				t.Fatalf("lost lifetime evidence %+v", image)
			}
		})
	}
}

type progressAdmissionFS struct {
	storage.Filesystem
	wrongOwner bool
}

func (f progressAdmissionFS) Stat(ctx context.Context, name string) (vfs.Info, error) {
	info, err := f.Filesystem.Stat(ctx, name)
	if err == nil && name == "." {
		if f.wrongOwner {
			info.UID++
		} else {
			info.Mode |= 0004
		}
	}
	return info, err
}

func TestResumeRejectsUnprivateProgressBeforeRestore(t *testing.T) {
	for _, backend := range []string{"sim", "os"} {
		for _, wrongOwner := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/owner%t", backend, wrongOwner), func(t *testing.T) {
				f := newFixture(t, backend)
				f.create("a", oid(20))
				f.sealSelection()
				r, err := f.replay(fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), f.config)
				if err != nil {
					t.Fatal(err)
				}
				b, err := r.Capture(t.Context(), f.stream, 1<<20)
				if err != nil {
					t.Fatal(err)
				}
				suffix, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, b.Frames)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = suffix.Close() }()
				data := fresh(t, backend, vfs.Config{})
				_, err = Resume(t.Context(), data, fresh(t, backend, vfs.Config{}), progressAdmissionFS{fresh(t, backend, vfs.Config{}), wrongOwner}, suffix, f.selection, f.config, b, 1<<20, baselineAuthority(func(context.Context, BaselineAuthorization) error { return nil }))
				if !errors.Is(err, fsapi.Permission) {
					t.Fatalf("unprivate progress admitted: %v", err)
				}
				if _, err = data.Stat(t.Context(), "format.json"); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("destination changed before progress admission: %v", err)
				}
			})
		}
	}
}

func TestResumeRejectsOversizedSelectionBeforeRestore(t *testing.T) {
	f := newFixture(t, "sim")
	f.sealSelection()
	r, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Capture(t.Context(), f.stream, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"proof", "effects", "default-effects", "empty-effects", "invalid-maxframes"} {
		t.Run(kind, func(t *testing.T) {
			selection := cloneSelection(f.selection)
			config := f.config
			switch kind {
			case "proof":
				selection.Proof = make([]byte, (1<<20)+1)
			case "effects":
				config.MaxFrames = 1
				selection.Effects = append(selection.Effects, selection.Effects[0])
			case "default-effects":
				selection.Effects = make([]record.FrameRef, 4097)
			case "empty-effects":
				selection.Effects = nil
			case "invalid-maxframes":
				config.MaxFrames = -1
			}
			suffix, err := segment.OpenCheckpointSuffix(t.Context(), f.source, f.stream, segment.Limits{}, b.Frames)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = suffix.Close() }()
			roots := []storage.Filesystem{fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{})}
			authority := baselineAuthority(func(context.Context, BaselineAuthorization) error {
				t.Fatal("selection was not admitted before baseline authorization")
				return fsapi.Permission
			})
			_, err = Resume(t.Context(), roots[0], roots[1], roots[2], suffix, selection, config, b, 1<<20, authority)
			if !errors.Is(err, fsapi.Invalid) {
				t.Fatalf("oversized/invalid selection admitted: %v", err)
			}
			for _, root := range roots {
				dir, err := root.OpenDir(t.Context(), ".")
				if err != nil {
					t.Fatal(err)
				}
				entries, readErr := dir.ReadDir(t.Context(), 1)
				closeErr := dir.Close()
				if readErr != nil && readErr != io.EOF {
					t.Fatal(readErr)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
				if len(entries) != 0 {
					t.Fatal("selection rejection changed destination")
				}
			}
		})
	}
}
