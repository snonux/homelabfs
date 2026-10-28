package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
)

type cancelAfterChecks struct {
	context.Context
	remaining atomic.Int64
	cancel    context.CancelFunc
}

type discoveringFS interface {
	FS
	Discovery
}

func discoveryBackends(t *testing.T, test func(*testing.T, discoveringFS)) {
	t.Helper()
	for _, backend := range []string{"os", "simulator"} {
		t.Run(backend, func(t *testing.T) {
			if backend == "simulator" {
				test(t, newSim(t, Config{}))
				return
			}
			s, err := NewOS(context.Background(), t.TempDir())
			must(t, err)
			t.Cleanup(func() { must(t, s.Close()) })
			test(t, s)
		})
	}
}

func TestDiscovery(t *testing.T) {
	discoveryBackends(t, func(t *testing.T, s discoveringFS) {
		ctx := context.Background()
		root, err := s.Stat(ctx, ".")
		must(t, err)
		if !root.Mode.IsDir() {
			t.Fatal("root has wrong type")
		}
		must(t, s.Mkdir(ctx, "tree", 0o700))
		f := openFile(t, s, "tree/data", os.O_CREATE|os.O_RDWR)
		writeAt(t, f, "body", 0)
		before, err := s.Stat(ctx, "tree/data")
		must(t, err)
		fromFD, err := f.(Statter).Stat(ctx)
		must(t, err)
		if before != fromFD || before.Size != 4 || !before.Mode.IsRegular() {
			t.Fatalf("physical file info: %+v / %+v", before, fromFD)
		}
		d, err := s.OpenDir(ctx, "tree")
		must(t, err)
		defer func() { must(t, d.Close()) }()
		dirBefore, err := d.Stat(ctx)
		must(t, err)
		must(t, s.Rename(ctx, "tree", "moved"))
		must(t, s.Mkdir(ctx, "tree", 0o700))
		replacement := openFile(t, s, "tree/data", os.O_CREATE|os.O_RDWR)
		writeAt(t, replacement, "replacement", 0)
		entries, err := d.ReadDir(ctx, 2)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name != "data" || entries[0].Info != before {
			t.Fatalf("enumeration retargeted after rename: %+v", entries)
		}
		must(t, s.Unlink(ctx, "moved/data"))
		must(t, s.Rmdir(ctx, "moved"))
		dirAfter, err := d.Stat(ctx)
		must(t, err)
		if dirAfter.Identity != dirBefore.Identity {
			t.Fatal("directory handle retargeted after removal")
		}
		fileAfter, err := f.(Statter).Stat(ctx)
		must(t, err)
		if fileAfter.Identity != before.Identity || fileAfter.Size != 4 {
			t.Fatal("file handle retargeted after unlink")
		}
		must(t, f.Close())
		if _, err := f.(Statter).Stat(ctx); !errors.Is(err, fs.ErrClosed) {
			t.Fatalf("closed file stat: %v", err)
		}
	})
}

func TestDiscoveryRejectedOperations(t *testing.T) {
	discoveryBackends(t, func(t *testing.T, s discoveringFS) {
		ctx := context.Background()
		must(t, s.Mkdir(ctx, "nonempty", 0o700))
		_ = openFile(t, s, "nonempty/file", os.O_CREATE|os.O_RDWR)
		_ = openFile(t, s, "file", os.O_CREATE|os.O_RDWR)
		for _, name := range []string{".", "nonempty", "file", "missing", "../escape", "/absolute", "nonempty/../file", "bad\\name", "bad\x00name"} {
			if err := s.Rmdir(ctx, name); err == nil {
				t.Fatalf("rmdir accepted %q", name)
			}
		}
		for _, name := range []string{"../escape", "/absolute", "nonempty/../file", "bad\\name", "bad\x00name", "missing"} {
			if _, err := s.Stat(ctx, name); err == nil {
				t.Fatalf("stat accepted %q", name)
			}
			if d, err := s.OpenDir(ctx, name); err == nil {
				_ = d.Close()
				t.Fatalf("opendir accepted %q", name)
			}
		}
		if d, err := s.OpenDir(ctx, "file"); err == nil {
			_ = d.Close()
			t.Fatal("opendir(file)")
		}
		if _, err := s.Stat(ctx, "nonempty/file"); err != nil {
			t.Fatalf("failed rmdir removed data: %v", err)
		}
		d, err := s.OpenDir(ctx, ".")
		must(t, err)
		for _, n := range []int{-1, 0, MaxReadDir + 1, int(^uint(0) >> 1)} {
			if _, err := d.ReadDir(ctx, n); !errors.Is(err, fs.ErrInvalid) {
				t.Fatalf("readdir(%d): %v", n, err)
			}
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.Stat(canceled, "."); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := s.OpenDir(canceled, "."); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if err := s.Rmdir(canceled, "nonempty"); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := d.ReadDir(canceled, 1); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if _, err := d.Stat(canceled); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		must(t, d.Close())
		if _, err := d.ReadDir(ctx, 1); !errors.Is(err, fs.ErrClosed) {
			t.Fatalf("closed readdir: %v", err)
		}
		if _, err := d.Stat(ctx); !errors.Is(err, fs.ErrClosed) {
			t.Fatalf("closed stat: %v", err)
		}
	})
}

func TestDiscoveryPages(t *testing.T) {
	discoveryBackends(t, func(t *testing.T, s discoveringFS) {
		ctx := context.Background()
		for i := 0; i < 73; i++ {
			must(t, s.Mkdir(ctx, fmt.Sprintf("child-%03d", i), 0o700))
		}
		d, err := s.OpenDir(ctx, ".")
		must(t, err)
		defer func() { must(t, d.Close()) }()
		seen := make(map[string]bool)
		for {
			entries, err := d.ReadDir(ctx, 7)
			if len(entries) > 7 || cap(entries) > 7 {
				t.Fatalf("unbounded page %d/%d", len(entries), cap(entries))
			}
			for _, entry := range entries {
				if seen[entry.Name] || !entry.Info.Mode.IsDir() {
					t.Fatalf("invalid entry %+v", entry)
				}
				seen[entry.Name] = true
			}
			if errors.Is(err, io.EOF) {
				break
			}
			must(t, err)
		}
		if len(seen) != 73 {
			t.Fatalf("saw %d entries", len(seen))
		}
	})
}

func TestOSDiscoveryNoFollow(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	must(t, os.Mkdir(root+"/dir", 0o700))
	must(t, os.WriteFile(root+"/dir/data", []byte("safe"), 0o600))
	for _, link := range []struct{ name, target string }{{"dirlink", "dir"}, {"filelink", "dir/data"}, {"escape", t.TempDir()}} {
		must(t, os.Symlink(link.target, root+"/"+link.name))
	}
	s, err := NewOS(ctx, root)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	for _, name := range []string{"dirlink", "dirlink/data", "filelink", "escape"} {
		if _, err := s.Stat(ctx, name); err == nil {
			t.Fatalf("stat followed %q", name)
		}
		if d, err := s.OpenDir(ctx, name); err == nil {
			_ = d.Close()
			t.Fatalf("opendir followed %q", name)
		}
		if err := s.Rmdir(ctx, name); err == nil {
			t.Fatalf("rmdir followed %q", name)
		}
		if f, err := s.Open(ctx, name, os.O_RDWR|os.O_TRUNC, 0); err == nil {
			_ = f.Close()
			t.Fatalf("open followed %q", name)
		}
		if err := s.Unlink(ctx, name); err == nil {
			t.Fatalf("unlink removed %q", name)
		}
		if err := s.SyncDir(ctx, name); err == nil {
			t.Fatalf("syncdir followed %q", name)
		}
		if err := s.Rename(ctx, name, "destination"); err == nil {
			t.Fatalf("rename moved %q", name)
		}
		if err := s.Rename(ctx, "dir/data", name); err == nil {
			t.Fatalf("rename replaced %q", name)
		}
	}
	data, err := os.ReadFile(root + "/dir/data")
	must(t, err)
	if string(data) != "safe" {
		t.Fatal("negative operations changed file")
	}
	d, err := s.OpenDir(ctx, ".")
	must(t, err)
	defer func() { must(t, d.Close()) }()
	entries, err := d.ReadDir(ctx, MaxReadDir)
	if err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("scan accepted symlink tree: %+v %v", entries, err)
	}
	link := filepath.Join(t.TempDir(), "rootlink")
	must(t, os.Symlink(root, link))
	if other, err := NewOS(ctx, link); err == nil {
		_ = other.Close()
		t.Fatal("accepted symlink root")
	}
}

func TestDirectoryRemovalDurability(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []LossMode{Lose, Keep, Random} {
		for _, syncParent := range []bool{false, true} {
			t.Run(fmt.Sprintf("mode-%d/sync-%t", mode, syncParent), func(t *testing.T) {
				s := newSim(t, Config{Seed: 717, NamespaceLoss: mode})
				must(t, s.Mkdir(ctx, "parent", 0o700))
				must(t, s.Mkdir(ctx, "parent/child", 0o700))
				must(t, s.SyncDir(ctx, "."))
				must(t, s.SyncDir(ctx, "parent"))
				s.Crash()
				d, err := s.OpenDir(ctx, "parent/child")
				must(t, err)
				must(t, s.Rmdir(ctx, "parent/child"))
				if syncParent {
					must(t, s.SyncDir(ctx, "parent"))
				}
				s.Crash()
				_, err = s.Stat(ctx, "parent/child")
				if syncParent || mode == Keep {
					if !errors.Is(err, fs.ErrNotExist) {
						t.Fatalf("removal lost: %v", err)
					}
				} else if mode == Lose {
					must(t, err)
				} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("invalid recovered removal state: %v", err)
				}
				if _, err := d.ReadDir(ctx, 1); !errors.Is(err, fs.ErrClosed) {
					t.Fatalf("stale directory handle: %v", err)
				}
				if _, err := d.Stat(ctx); !errors.Is(err, fs.ErrClosed) {
					t.Fatalf("stale directory stat: %v", err)
				}
			})
		}
	}
}

func TestDiscoveryConcurrentNamespace(t *testing.T) {
	discoveryBackends(t, func(t *testing.T, s discoveringFS) {
		ctx := context.Background()
		must(t, s.Mkdir(ctx, "directory", 0o700))
		_ = openFile(t, s, "file", os.O_CREATE|os.O_RDWR)
		var wg sync.WaitGroup
		for worker := 0; worker < 3; worker++ {
			wg.Go(func() {
				for i := 0; i < 200; i++ {
					for _, name := range []string{"directory", "moving"} {
						if d, err := s.OpenDir(ctx, name); err == nil {
							_, _ = d.ReadDir(ctx, 1)
							_ = d.Close()
						}
						_, _ = s.Stat(ctx, name)
					}
					if err := s.Rmdir(ctx, "file"); !errors.Is(err, syscall.ENOTDIR) {
						t.Errorf("rmdir(file): %v", err)
						return
					}
				}
			})
		}
		for i := 0; i < 200; i++ {
			must(t, s.Rename(ctx, "directory", "moving"))
			must(t, s.Rename(ctx, "moving", "directory"))
		}
		wg.Wait()
		if info, err := s.Stat(ctx, "file"); err != nil || !info.Mode.IsRegular() {
			t.Fatalf("wrong-type removal affected file: %+v %v", info, err)
		}
	})
}

func TestOSRootEvidenceAndClosedBackend(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	must(t, os.Chmod(root, 0o700))
	s, err := NewOS(ctx, root)
	must(t, err)
	info, err := s.Stat(ctx, ".")
	must(t, err)
	if info.Mode != fs.ModeDir|0o700 || info.UID != uint32(os.Geteuid()) || info.Identity.File == 0 {
		t.Fatalf("missing private-root evidence: %+v", info)
	}
	d, err := s.OpenDir(ctx, ".")
	must(t, err)
	fromFD, err := d.Stat(ctx)
	must(t, err)
	if info != fromFD {
		t.Fatalf("path and descriptor evidence differ: %+v / %+v", info, fromFD)
	}
	must(t, s.Close())
	if _, err := s.Stat(ctx, "."); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed root stat: %v", err)
	}
	if _, err := s.OpenDir(ctx, "."); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed root opendir: %v", err)
	}
	if err := s.Rmdir(ctx, "absent"); !errors.Is(err, fs.ErrClosed) {
		t.Fatalf("closed root rmdir: %v", err)
	}
	// Root closure does not close independent directory descriptors.
	_, err = d.Stat(ctx)
	must(t, err)
	must(t, d.Close())
}

func TestDiscoveryAllocationBound(t *testing.T) {
	discoveryBackends(t, func(t *testing.T, s discoveringFS) {
		ctx := context.Background()
		for i := 0; i < 2048; i++ {
			must(t, s.Mkdir(ctx, fmt.Sprintf("entry-%04d", i), 0o700))
		}
		result := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				d, err := s.OpenDir(ctx, ".")
				if err != nil {
					b.Fatal(err)
				}
				entries, err := d.ReadDir(ctx, 1)
				if err != nil || len(entries) != 1 {
					b.Fatalf("read one: %+v %v", entries, err)
				}
				if err := d.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
		// One entry plus the OS directory buffer needs much less than the complete
		// 2048-name set. Catch a hidden full-directory snapshot/sort allocation.
		if bytes := result.AllocedBytesPerOp(); bytes > 32<<10 {
			t.Fatalf("one-entry page allocated %d bytes", bytes)
		}
	})
}

func TestDirectoryRemovalSeededRecovery(t *testing.T) {
	ctx := context.Background()
	run := func() []string {
		s := newSim(t, Config{Seed: 515019, NamespaceLoss: Random})
		for i := 0; i < 40; i++ {
			must(t, s.Mkdir(ctx, fmt.Sprintf("dir-%02d", i), 0o700))
		}
		must(t, s.SyncDir(ctx, "."))
		s.Crash()
		for i := 0; i < 40; i++ {
			name := fmt.Sprintf("dir-%02d", i)
			if i%2 == 0 {
				must(t, s.Rmdir(ctx, name))
			} else {
				must(t, s.Rename(ctx, name, name+"-moved"))
				must(t, s.Rmdir(ctx, name+"-moved"))
			}
		}
		s.Crash()
		d, err := s.OpenDir(ctx, ".")
		must(t, err)
		entries, err := d.ReadDir(ctx, MaxReadDir)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		must(t, d.Close())
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name)
		}
		return names
	}
	first, second := run(), run()
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("seeded removal recovery differs: %v / %v", first, second)
	}
}

// Err injects a deterministic cancellation during a scan, without depending on
// goroutine timing or a slow filesystem. All context methods remain concurrent.
func (c *cancelAfterChecks) Err() error {
	if c.remaining.Add(-1) <= 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestDiscoveryCancellationDuringPage(t *testing.T) {
	discoveryBackends(t, func(t *testing.T, s discoveringFS) {
		ctx := context.Background()
		for i := 0; i < 12; i++ {
			must(t, s.Mkdir(ctx, fmt.Sprintf("child-%02d", i), 0o700))
		}
		d, err := s.OpenDir(ctx, ".")
		must(t, err)
		defer func() { must(t, d.Close()) }()
		base, cancel := context.WithCancel(ctx)
		defer cancel()
		stopping := &cancelAfterChecks{Context: base, cancel: cancel}
		stopping.remaining.Store(5)
		entries, err := d.ReadDir(stopping, 12)
		if !errors.Is(err, context.Canceled) || len(entries) >= 12 {
			t.Fatalf("scan ignored cancellation: %d %v", len(entries), err)
		}
	})
}

func TestSimulatorIdentityScopes(t *testing.T) {
	a, err := NewSimulator(Config{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSimulator(Config{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Stat(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	other, err := b.Stat(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity == other.Identity {
		t.Fatal("independent simulator roots share identity scope")
	}
	a.Crash()
	after, err := a.Stat(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity != after.Identity {
		t.Fatal("crash changed surviving root identity")
	}
}
