package vfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"syscall"
	"testing"
)

func TestBackends(t *testing.T) {
	for _, backend := range []string{"os", "simulator"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var store FS
			if backend == "os" {
				s, err := NewOS(ctx, t.TempDir())
				must(t, err)
				t.Cleanup(func() { must(t, s.Close()) })
				store = s
			} else {
				store = newSim(t, Config{})
			}
			testFileOperations(t, store)
			testDirectoryRename(t, store)
			testRejectedOperations(t, store)
		})
	}
}

func TestOSRootContainment(t *testing.T) {
	ctx := context.Background()
	root, outside := t.TempDir(), t.TempDir()
	must(t, os.WriteFile(outside+"/protected", []byte("safe"), 0o600))
	must(t, os.Symlink(outside, root+"/escape"))
	s, err := NewOS(ctx, root)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	if f, err := s.Open(ctx, "escape/protected", os.O_RDWR|os.O_TRUNC, 0o600); err == nil {
		_ = f.Close()
		t.Fatal("escaped root")
	}
	_ = openFile(t, s, "inside", os.O_CREATE|os.O_RDWR)
	for _, names := range [][2]string{{"escape/protected", "stolen"}, {"inside", "escape/protected"}} {
		if err := s.Rename(ctx, names[0], names[1]); err == nil {
			t.Fatalf("rename escaped root: %q -> %q", names[0], names[1])
		}
	}
	data, err := os.ReadFile(outside + "/protected")
	must(t, err)
	if string(data) != "safe" {
		t.Fatalf("outside file changed: %q", data)
	}
}

func testFileOperations(t *testing.T, s FS) {
	t.Helper()
	ctx := context.Background()
	must(t, s.Mkdir(ctx, "files", 0o700))
	f := openFile(t, s, "files/a", os.O_CREATE|os.O_EXCL|os.O_RDWR)
	writeAt(t, f, "abc", 2)
	if got := readFile(t, f); got != "\x00\x00abc" {
		t.Fatalf("sparse write: %q", got)
	}
	must(t, f.Truncate(ctx, 3))
	must(t, f.Truncate(ctx, 5))
	if got := readFile(t, f); got != "\x00\x00a\x00\x00" {
		t.Fatalf("truncate: %q", got)
	}
	must(t, f.Sync(ctx))
	must(t, f.DataSync(ctx))
	must(t, s.SyncDir(ctx, "files"))
	must(t, s.Rename(ctx, "files/a", "files/b"))
	other := openFile(t, s, "files/a", os.O_CREATE|os.O_EXCL|os.O_RDWR)
	writeAt(t, other, "replacement", 0)
	must(t, s.Rename(ctx, "files/b", "files/a"))
	if got := readFile(t, other); got != "replacement" {
		t.Fatalf("replacement changed old handle: %q", got)
	}
	must(t, s.Unlink(ctx, "files/a"))
	writeAt(t, f, "Z", 0)
	if got := readFile(t, f); got != "Z\x00a\x00\x00" {
		t.Fatalf("unlinked handle: %q", got)
	}
	if _, err := s.Open(ctx, "files/a", os.O_RDONLY, 0); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unlinked path: %v", err)
	}
}

func testDirectoryRename(t *testing.T, s FS) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{"tree", "tree/child", "empty"} {
		must(t, s.Mkdir(ctx, name, 0o700))
	}
	f := openFile(t, s, "tree/child/data", os.O_CREATE|os.O_RDWR)
	writeAt(t, f, "identity", 0)
	if err := s.Rename(ctx, "tree", "tree/child/cycle"); err == nil {
		t.Fatal("accepted directory cycle")
	}
	must(t, s.Rename(ctx, "tree", "empty"))
	must(t, s.Rename(ctx, "empty", "empty"))
	g := openFile(t, s, "empty/child/data", os.O_RDONLY)
	if got := readFile(t, g); got != "identity" {
		t.Fatalf("directory move: %q", got)
	}
	if err := s.Rename(ctx, "files", "empty"); err == nil {
		t.Fatal("replaced nonempty directory")
	}
	_ = openFile(t, s, "regular", os.O_CREATE|os.O_RDWR)
	for _, names := range [][2]string{{"regular", "empty"}, {"files", "regular"}} {
		if err := s.Rename(ctx, names[0], names[1]); err == nil {
			t.Fatalf("accepted file/directory replacement %q -> %q", names[0], names[1])
		}
	}
}

func testRejectedOperations(t *testing.T, s FS) {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{"../escape", "/absolute", "files//bad", "files/../bad", "bad\\name", "bad\x00name"} {
		if f, err := s.Open(ctx, name, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
			_ = f.Close()
			t.Fatalf("accepted path %q", name)
		}
	}
	for _, flag := range []int{os.O_RDWR | os.O_APPEND, os.O_RDONLY | os.O_TRUNC, 3} {
		if f, err := s.Open(ctx, "bad", flag, 0o600); err == nil {
			_ = f.Close()
			t.Fatalf("accepted flags %d", flag)
		}
	}
	f := openFile(t, s, "readonly", os.O_CREATE|os.O_RDONLY)
	if _, err := f.WriteAt(ctx, []byte("x"), 0); err == nil {
		t.Fatal("write on read-only handle")
	}
	if err := f.Truncate(ctx, 1); err == nil {
		t.Fatal("truncate on read-only handle")
	}
	if err := s.SyncDir(ctx, "readonly"); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("syncdir(file): %v", err)
	}
	if err := s.Unlink(ctx, "empty"); err == nil {
		t.Fatal("unlinked directory")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.Mkdir(canceled, "canceled", 0o700); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled mkdir: %v", err)
	}
	if _, err := f.ReadAt(canceled, make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
	must(t, f.Close())
	if _, err := f.ReadAt(ctx, make([]byte, 1), 0); err == nil {
		t.Fatal("read closed handle")
	}
}

func newSim(t *testing.T, config Config) *Simulator {
	t.Helper()
	s, err := NewSimulator(config)
	must(t, err)
	return s
}

func openFile(t *testing.T, s FS, name string, flag int) File {
	t.Helper()
	f, err := s.Open(context.Background(), name, flag, 0o600)
	must(t, err)
	t.Cleanup(func() {
		if err := f.Close(); err != nil && !errors.Is(err, fs.ErrClosed) {
			t.Error(err)
		}
	})
	return f
}

func writeAt(t *testing.T, f File, value string, off int64) {
	t.Helper()
	n, err := f.WriteAt(context.Background(), []byte(value), off)
	must(t, err)
	if n != len(value) {
		t.Fatalf("short write: %d of %d", n, len(value))
	}
}

func readFile(t *testing.T, f File) string {
	t.Helper()
	buf := make([]byte, 256)
	n, err := f.ReadAt(context.Background(), buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	return string(buf[:n])
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
