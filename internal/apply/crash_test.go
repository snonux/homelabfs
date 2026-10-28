package apply

import (
	"context"
	"errors"
	"fmt"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"io"
	"io/fs"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type crashFS struct {
	storage.Filesystem
	hit func(string) error
}
type crashFile struct {
	vfs.File
	hit  func(string) error
	name string
}

func (f crashFS) Open(ctx context.Context, name string, flag int, perm fs.FileMode) (vfs.File, error) {
	file, err := f.Filesystem.Open(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	return crashFile{file, f.hit, name}, nil
}
func (f crashFS) Rename(ctx context.Context, a, b string) error {
	if err := f.Filesystem.Rename(ctx, a, b); err != nil {
		return err
	}
	return f.hit("rename")
}
func (f crashFS) Mkdir(ctx context.Context, name string, perm fs.FileMode) error {
	if err := f.Filesystem.Mkdir(ctx, name, perm); err != nil {
		return err
	}
	return f.hit("mkdir")
}
func (f crashFS) SyncDir(ctx context.Context, name string) error {
	if err := f.Filesystem.SyncDir(ctx, name); err != nil {
		return err
	}
	return f.hit("dirsync")
}
func (f crashFS) Unlink(ctx context.Context, name string) error {
	if err := f.Filesystem.Unlink(ctx, name); err != nil {
		return err
	}
	return f.hit("unlink")
}
func (f crashFile) WriteAt(ctx context.Context, p []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(ctx, p, off)
	if err != nil {
		return n, err
	}
	name := "write"
	if strings.HasSuffix(f.name, "/payload") {
		name = "payload-write"
	}
	return n, f.hit(name)
}
func (f crashFile) Truncate(ctx context.Context, size int64) error {
	if err := f.File.Truncate(ctx, size); err != nil {
		return err
	}
	return f.hit("truncate")
}
func (f crashFile) Sync(ctx context.Context) error {
	if err := f.File.Sync(ctx); err != nil {
		return err
	}
	return f.hit("sync")
}

func TestEveryApplyBoundaryCrash(t *testing.T) {
	f := newFixture(t, "sim")
	f.history()
	fault := errors.New("injected replay crash")
	boundaries := []Boundary{BeforeApply, AfterApply, EffectsDurable, ImageDurable, BeforePublish, AfterRename, AppliedDurable}
	for _, dataLoss := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
		for _, namespaceLoss := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
			for _, boundary := range boundaries {
				for nth := 1; nth <= len(f.selection.Effects); nth++ {
					t.Run(fmt.Sprintf("data%d/ns%d/%s/%d", dataLoss, namespaceLoss, boundary, nth), func(t *testing.T) {
						// Every occurrence is tested, not just the first mutation in the stream.
						c := vfs.Config{Seed: 91 + uint64(nth), DataLoss: dataLoss, NamespaceLoss: namespaceLoss}
						d, j, p := fresh(t, "sim", c), fresh(t, "sim", c), fresh(t, "sim", c)
						config := f.config
						hits := 0
						config.Hook = func(_ context.Context, b Boundary) error {
							if b == boundary {
								hits++
								if hits == nth {
									return fault
								}
							}
							return nil
						}
						_, err := f.replay(d, j, p, config)
						if !errors.Is(err, fault) {
							t.Fatalf("boundary did not fire: %v", err)
						}
						d.(*vfs.Simulator).Crash()
						j.(*vfs.Simulator).Crash()
						p.(*vfs.Simulator).Crash()
						config.Hook = nil
						config.Workspace = uid(1001)
						result, err := f.replay(fresh(t, "sim", c), fresh(t, "sim", c), p, config)
						if err != nil {
							t.Fatal(err)
						}
						f.oracle(result.Store)
					})
				}
			}
		}
	}
}

func TestSeededPartialPersistenceCrashOracle(t *testing.T) {
	const seed uint64 = 0x5eed0123
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	f.write(oid(20), 0, []byte("0123456789"))
	f.truncate(oid(20), 2)
	f.truncate(oid(20), 14)
	f.write(oid(20), 8, []byte("abcd"))
	f.rename("a", "b")
	f.create("a", oid(21))
	// Count each VFS fault site on a fault-free run, then reproducibly choose a
	// reachable occurrence at every site under every persistence mode.
	counts := map[string]int{}
	count := func(name string) error { counts[name]++; return nil }
	c := vfs.Config{Seed: seed}
	_, err := f.replay(crashFS{fresh(t, "sim", c), count}, crashFS{fresh(t, "sim", c), count}, crashFS{fresh(t, "sim", c), count}, f.config)
	if err != nil {
		t.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(seed, seed^23))
	sites := []string{"mkdir", "write", "payload-write", "truncate", "sync", "dirsync", "rename"}
	for _, mode := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
		for _, ns := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
			for _, site := range sites {
				for sample := range 3 {
					nth := 1
					if sample == 1 {
						nth = counts[site]
					}
					if sample == 2 {
						nth = 1 + rng.IntN(counts[site])
					}
					t.Run(fmt.Sprintf("seed%x/data%d/ns%d/%s/%d", seed, mode, ns, site, nth), func(t *testing.T) {
						c := vfs.Config{Seed: seed + uint64(nth), DataLoss: mode, NamespaceLoss: ns}
						d, j, p := fresh(t, "sim", c), fresh(t, "sim", c), fresh(t, "sim", c)
						hits := 0
						fault := errors.New("partial persistence crash")
						hook := func(name string) error {
							if name == site {
								hits++
								if hits == nth {
									return fault
								}
							}
							return nil
						}
						_, err := f.replay(crashFS{d, hook}, crashFS{j, hook}, crashFS{p, hook}, f.config)
						if !errors.Is(err, fault) {
							t.Fatalf("site %s/%d did not fire: %v", site, nth, err)
						}
						for _, root := range []storage.Filesystem{d, j, p} {
							root.(*vfs.Simulator).Crash()
						}
						config := f.config
						config.Workspace = uid(1001)
						result, err := f.replay(fresh(t, "sim", c), fresh(t, "sim", c), p, config)
						if err != nil {
							t.Fatal(err)
						}
						f.oracle(result.Store)
					})
				}
			}
		}
	}
}

func TestSeededCommandHistoryCrashOracle(t *testing.T) {
	for _, seed := range []uint64{7, 93, 0xc0ffee} {
		t.Run(fmt.Sprintf("seed%x", seed), func(t *testing.T) {
			f := newFixture(t, "sim")
			rng := rand.New(rand.NewPCG(seed, seed^123))
			f.create("a", oid(20))
			nextObject := uint64(21)
			for step := range 18 {
				live := []string{}
				for name, state := range f.names {
					if state.Target != nil {
						live = append(live, name)
					}
				}
				sort.Strings(live)
				if len(live) == 0 || rng.IntN(5) == 0 {
					name := fmt.Sprintf("n%d", step)
					f.create(name, oid(nextObject))
					nextObject++
					continue
				}
				name := live[rng.IntN(len(live))]
				id := *f.names[name].Target
				switch rng.IntN(4) {
				case 0:
					data := make([]byte, 1+rng.IntN(19))
					for i := range data {
						data[i] = byte(rng.Uint32())
					}
					f.write(id, rng.IntN(20), data)
				case 1:
					f.truncate(id, rng.IntN(35))
				case 2:

					f.rename(name, fmt.Sprintf("m%d", step))
				case 3:
					f.unlink(name)
				}
			}
			for _, mode := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
				for _, ns := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
					config := f.config
					c := vfs.Config{Seed: seed, DataLoss: mode, NamespaceLoss: ns}
					d, j, p := fresh(t, "sim", c), fresh(t, "sim", c), fresh(t, "sim", c)
					// Seed chooses both the applied effect and its publication boundary.
					chosen := []Boundary{BeforeApply, AfterApply, EffectsDurable, BeforePublish, AfterRename, AppliedDurable}[rng.IntN(6)]
					nth := 1 + rng.IntN(len(f.selection.Effects))
					count := 0
					fault := errors.New("seeded command crash")
					config.Hook = func(_ context.Context, b Boundary) error {
						if b == chosen {
							count++
							if count == nth {
								return fault
							}
						}
						return nil
					}
					if _, err := f.replay(d, j, p, config); !errors.Is(err, fault) {
						t.Fatalf("crash %s/%d: %v", chosen, nth, err)
					}
					for _, root := range []storage.Filesystem{d, j, p} {
						root.(*vfs.Simulator).Crash()
					}
					config.Hook = nil
					config.Workspace = uid(1001)
					result, err := f.replay(fresh(t, "sim", c), fresh(t, "sim", c), p, config)
					if err != nil {
						t.Fatal(err)
					}
					f.oracle(result.Store)
				}
			}
		})
	}
}

func TestAppendCrashBeforeApplyPreservesSelection(t *testing.T) {
	for _, stage := range []string{"before-append", "after-append", "before-sync", "after-sync"} {
		for _, mode := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
			for _, ns := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
				t.Run(fmt.Sprintf("%s/data%d/ns%d", stage, mode, ns), func(t *testing.T) {
					c := vfs.Config{Seed: 0x12345, DataLoss: mode, NamespaceLoss: ns}
					f := newFixture(t, "sim", c)
					f.create("a", oid(20))
					f.write(oid(20), 0, []byte("selected durable content"))
					old := f.objects[oid(20)].meta
					after := old
					after.Revision++
					after.Size = max(after.Size, 8)
					edit := record.ObjectEdit{Object: oid(20), ExpectedRevision: old.Revision, After: after}
					candidate := record.Mutation{Intent: record.Intent{Kind: 8, Arguments: record.Write{Object: oid(20), Data: []byte("MUST-NOT")}, SelectedMetadata: []record.ObjectEdit{edit}}, Redo: record.Redo{Objects: []record.ObjectEdit{edit}}}
					fault := errors.New("append crash")
					f.journalHook = func(point string) error {
						if point == stage {
							return fault
						}
						return nil
					}
					if _, err := f.writer.Append(t.Context(), record.Record{Header: f.header(uid(900)), Payload: candidate}); !errors.Is(err, fault) {
						t.Fatalf("append hook %s: %v", stage, err)
					}
					f.source.(*vfs.Simulator).Crash()
					reader, err := segment.OpenReader(t.Context(), f.source, f.stream, segment.RecoveryOptions{}, f.position(1))
					if err != nil {
						// Ambiguous partial framing is deliberately preserved. Only the trusted
						// reconciliation caller could authorize mq3 tail repair; apply never does.
						var tail *segment.TornTail
						if !errors.As(err, &tail) && !errors.Is(err, segment.ErrCorrupt) && !isRecordError(err) {
							t.Fatalf("unexpected recovery gate: %v", err)
						}
						return
					}
					defer func() { _ = reader.Close() }()
					for {
						entry, err := reader.Next(t.Context())
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						f.selection.Through = entry.Reference
					}
					config := f.config
					config.Workspace = uid(1001)
					result, err := f.replay(fresh(t, "sim", c), fresh(t, "sim", c), fresh(t, "sim", c), config)
					if err != nil {
						t.Fatal(err)
					}
					f.oracle(result.Store)
				})
			}
		}
	}
}

func isRecordError(err error) bool { var rec *record.Error; return errors.As(err, &rec) }
