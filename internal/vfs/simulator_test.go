package vfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestCrashFileAndDirectoryBarriers(t *testing.T) {
	for _, tc := range []struct {
		name              string
		syncFile, syncDir bool
		exists            bool
		data              string
	}{
		{"neither", false, false, false, ""},
		{"file-only", true, false, false, ""},
		{"directory-only", false, true, true, ""},
		{"both", true, true, true, "payload"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newSim(t, Config{Seed: 1})
			f := openFile(t, s, "file", os.O_CREATE|os.O_RDWR)
			writeAt(t, f, "payload", 0)
			if tc.syncFile {
				must(t, f.Sync(ctx))
			}
			if tc.syncDir {
				must(t, s.SyncDir(ctx, "."))
			}
			s.Crash()
			if err := f.Sync(ctx); !errors.Is(err, fs.ErrClosed) {
				t.Fatalf("pre-crash handle: %v", err)
			}
			g, err := s.Open(ctx, "file", os.O_RDONLY, 0)
			if !tc.exists {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("unsynced name: %v", err)
				}
				return
			}
			must(t, err)
			defer func() { must(t, g.Close()) }()
			if got := readFile(t, g); got != tc.data {
				t.Fatalf("recovered %q, want %q", got, tc.data)
			}
		})
	}
}

func TestCrashDataBarriersAndTruncate(t *testing.T) {
	ctx := context.Background()
	s := newSim(t, Config{Seed: 2})
	f := openFile(t, s, "file", os.O_CREATE|os.O_RDWR)
	writeAt(t, f, "abcdef", 0)
	must(t, f.DataSync(ctx))
	must(t, s.SyncDir(ctx, "."))
	writeAt(t, f, "ZZ", 1)
	must(t, f.Truncate(ctx, 3))
	s.Crash()
	f = openFile(t, s, "file", os.O_RDWR)
	if got := readFile(t, f); got != "abcdef" {
		t.Fatalf("unsynced overwrite/truncate: %q", got)
	}
	must(t, f.Truncate(ctx, 2))
	must(t, f.DataSync(ctx))
	s.Crash()
	f = openFile(t, s, "file", os.O_RDWR)
	if got := readFile(t, f); got != "ab" {
		t.Fatalf("synced truncate: %q", got)
	}
	writeAt(t, f, "Z", 4)
	must(t, f.Sync(ctx))
	s.Crash()
	f = openFile(t, s, "file", os.O_RDONLY)
	if got := readFile(t, f); got != "ab\x00\x00Z" {
		t.Fatalf("synced sparse extension: %q", got)
	}
}

func TestCrashNamespacePublicationAndRemoval(t *testing.T) {
	ctx := context.Background()
	s := newSim(t, Config{Seed: 3})
	f := openFile(t, s, "checkpoint", os.O_CREATE|os.O_RDWR)
	writeAt(t, f, "old", 0)
	must(t, f.Sync(ctx))
	must(t, s.SyncDir(ctx, "."))
	g := openFile(t, s, "staging", os.O_CREATE|os.O_RDWR)
	writeAt(t, g, "new", 0)
	must(t, g.Sync(ctx))
	must(t, s.Rename(ctx, "staging", "checkpoint"))
	s.Crash()
	if got := readFile(t, openFile(t, s, "checkpoint", os.O_RDONLY)); got != "old" {
		t.Fatalf("unsynced publication: %q", got)
	}
	g = openFile(t, s, "staging", os.O_CREATE|os.O_RDWR)
	writeAt(t, g, "new", 0)
	must(t, g.Sync(ctx))
	must(t, s.Rename(ctx, "staging", "checkpoint"))
	must(t, s.SyncDir(ctx, "."))
	s.Crash()
	if got := readFile(t, openFile(t, s, "checkpoint", os.O_RDONLY)); got != "new" {
		t.Fatalf("synced publication: %q", got)
	}
	must(t, s.Unlink(ctx, "checkpoint"))
	s.Crash()
	_ = openFile(t, s, "checkpoint", os.O_RDONLY)
	must(t, s.Unlink(ctx, "checkpoint"))
	must(t, s.SyncDir(ctx, "."))
	s.Crash()
	if _, err := s.Open(ctx, "checkpoint", os.O_RDONLY, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("synced unlink: %v", err)
	}
}

func TestCrashCrossDirectoryRename(t *testing.T) {
	for _, syncBoth := range []bool{false, true} {
		t.Run(map[bool]string{false: "one-parent", true: "both-parents"}[syncBoth], func(t *testing.T) {
			ctx := context.Background()
			s := newSim(t, Config{Seed: 4})
			must(t, s.Mkdir(ctx, "a", 0o700))
			must(t, s.Mkdir(ctx, "b", 0o700))
			must(t, s.Mkdir(ctx, "a/child", 0o700))
			f := openFile(t, s, "a/child/file", os.O_CREATE|os.O_RDWR)
			writeAt(t, f, "durable", 0)
			must(t, f.Sync(ctx))
			for _, dir := range []string{".", "a", "a/child"} {
				must(t, s.SyncDir(ctx, dir))
			}
			s.Crash()
			must(t, s.Rename(ctx, "a/child", "b/child"))
			must(t, s.SyncDir(ctx, "a"))
			if syncBoth {
				must(t, s.SyncDir(ctx, "b"))
			}
			s.Crash()
			path, missing := "a/child/file", "b/child/file"
			if syncBoth {
				path, missing = missing, path
			}
			if got := readFile(t, openFile(t, s, path, os.O_RDONLY)); got != "durable" {
				t.Fatalf("directory identity: %q", got)
			}
			if _, err := s.Open(ctx, missing, os.O_RDONLY, 0); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("rename not atomic: %v", err)
			}
		})
	}
}

func TestCrashDirectoryBarrierDoesNotPersistParentName(t *testing.T) {
	ctx := context.Background()
	s := newSim(t, Config{Seed: 5})
	must(t, s.Mkdir(ctx, "parent", 0o700))
	f := openFile(t, s, "parent/file", os.O_CREATE|os.O_RDWR)
	writeAt(t, f, "reachable", 0)
	must(t, f.Sync(ctx))
	must(t, s.SyncDir(ctx, "parent"))
	s.Crash()
	if _, err := s.Open(ctx, "parent/file", os.O_RDONLY, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("directory sync persisted parent name: %v", err)
	}
}

func TestSeededCrashModes(t *testing.T) {
	for _, mode := range []LossMode{Lose, Keep, Tear, Random} {
		for _, seed := range []uint64{1, 7, 42, 999} {
			first := crashResult(t, Config{Seed: seed, DataLoss: mode, NamespaceLoss: Random})
			second := crashResult(t, Config{Seed: seed, DataLoss: mode, NamespaceLoss: Random})
			if first != second {
				t.Fatalf("mode %d seed %d not reproducible: %q != %q", mode, seed, first, second)
			}
		}
	}
	if got := crashResult(t, Config{DataLoss: Lose}); got != "AAAAAAAA" {
		t.Fatalf("Lose: %q", got)
	}
	if got := crashResult(t, Config{DataLoss: Keep}); got != "BBBBBBBB" {
		t.Fatalf("Keep: %q", got)
	}
	seenTear := false
	for seed := uint64(1); seed <= 32; seed++ {
		got := crashResult(t, Config{Seed: seed, DataLoss: Tear})
		if strings.Trim(got, "AB") != "" || len(got) != 8 {
			t.Fatalf("invalid tear: %q", got)
		}
		seenTear = seenTear || (got != "AAAAAAAA" && got != "BBBBBBBB")
	}
	if !seenTear {
		t.Fatal("fixed seeds did not exercise mixed old/new bytes")
	}
	seen := make(map[string]bool)
	for seed := uint64(1); seed <= 32; seed++ {
		seen[crashResult(t, Config{Seed: seed, DataLoss: Random})] = true
	}
	if !seen["AAAAAAAA"] || !seen["BBBBBBBB"] || len(seen) <= 2 {
		t.Fatalf("Random did not exercise loss, retention and tearing across fixed seeds: %v", seen)
	}
}

func TestSimulatorBoundsAndConfig(t *testing.T) {
	for _, config := range []Config{{DataLoss: 4}, {NamespaceLoss: Tear}, {MaxFileSize: -1}} {
		if _, err := NewSimulator(config); !errors.Is(err, fs.ErrInvalid) {
			t.Fatalf("accepted config %+v: %v", config, err)
		}
	}
	s := newSim(t, Config{MaxFileSize: 4})
	f := openFile(t, s, "file", os.O_CREATE|os.O_RDWR)
	if _, err := f.WriteAt(context.Background(), []byte("ab"), 3); !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("write bound: %v", err)
	}
	if err := f.Truncate(context.Background(), 5); !errors.Is(err, syscall.EFBIG) {
		t.Fatalf("truncate bound: %v", err)
	}
}

func TestSeededNamespaceRecovery(t *testing.T) {
	for _, mode := range []LossMode{Lose, Keep, Random} {
		for _, seed := range []uint64{1, 7, 42, 999} {
			first := namespaceResult(t, Config{Seed: seed, NamespaceLoss: mode})
			second := namespaceResult(t, Config{Seed: seed, NamespaceLoss: mode})
			if first != second {
				t.Fatalf("namespace mode %d seed %d: %q != %q", mode, seed, first, second)
			}
			if mode == Lose && first != "old" {
				t.Fatalf("lost durable old name: %q", first)
			}
			if mode == Keep && first != "new" {
				t.Fatalf("lost live new name: %q", first)
			}
		}
	}
	seen := make(map[string]bool)
	for seed := uint64(1); seed <= 32; seed++ {
		seen[namespaceResult(t, Config{Seed: seed, NamespaceLoss: Random})] = true
	}
	if !seen["old"] || !seen["new"] {
		t.Fatalf("Random did not exercise both namespace outcomes across fixed seeds: %v", seen)
	}
}

func TestCrashDependentDirectoryMoves(t *testing.T) {
	for _, mode := range []LossMode{Lose, Keep, Random} {
		ctx := context.Background()
		s := newSim(t, Config{Seed: 42, NamespaceLoss: mode})
		for _, dir := range []string{"a", "a/b", "c"} {
			must(t, s.Mkdir(ctx, dir, 0o700))
		}
		must(t, s.SyncDir(ctx, "."))
		must(t, s.SyncDir(ctx, "a"))
		s.Crash()
		must(t, s.Rename(ctx, "a/b", "c/b"))
		must(t, s.Rename(ctx, "a", "c/b/a"))
		// Persisting the second move must also preserve the first move that
		// made its destination valid, even with Lose as the dirty policy.
		must(t, s.SyncDir(ctx, "."))
		must(t, s.SyncDir(ctx, "c/b"))
		s.Crash()
		must(t, s.SyncDir(ctx, "c/b/a"))
		if err := s.SyncDir(ctx, "a"); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("duplicate directory parent: %v", err)
		}
	}
}

func namespaceResult(t *testing.T, config Config) string {
	t.Helper()
	ctx := context.Background()
	s := newSim(t, config)
	_ = openFile(t, s, "old", os.O_CREATE|os.O_RDWR)
	must(t, s.SyncDir(ctx, "."))
	must(t, s.Rename(ctx, "old", "new"))
	s.Crash()
	result := ""
	for _, name := range []string{"old", "new"} {
		f, err := s.Open(ctx, name, os.O_RDONLY, 0)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		must(t, err)
		must(t, f.Close())
		result += name
	}
	if result != "old" && result != "new" {
		t.Fatalf("non-atomic namespace recovery: %q", result)
	}
	return result
}

func crashResult(t *testing.T, config Config) string {
	t.Helper()
	s := newSim(t, config)
	f := openFile(t, s, "file", os.O_CREATE|os.O_RDWR)
	writeAt(t, f, "AAAAAAAA", 0)
	must(t, f.Sync(context.Background()))
	must(t, s.SyncDir(context.Background(), "."))
	writeAt(t, f, "BBBBBBBB", 0)
	s.Crash()
	return readFile(t, openFile(t, s, "file", os.O_RDONLY))
}
