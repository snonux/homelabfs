//go:build linux || freebsd

package vfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestOSDiscoveryRejectsSpecialFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	must(t, syscall.Mkfifo(root+"/fifo", 0o600))
	s, err := NewOS(ctx, root)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	if _, err := s.Stat(ctx, "fifo"); err == nil {
		t.Fatal("stat accepted FIFO")
	}
	if d, err := s.OpenDir(ctx, "fifo"); err == nil {
		_ = d.Close()
		t.Fatal("opendir accepted FIFO")
	}
	if f, err := s.Open(ctx, "fifo", os.O_RDWR|os.O_TRUNC, 0); err == nil {
		_ = f.Close()
		t.Fatal("open accepted FIFO")
	}
	if err := s.Rmdir(ctx, "fifo"); err == nil {
		t.Fatal("rmdir accepted FIFO")
	}
	if err := s.Unlink(ctx, "fifo"); err == nil {
		t.Fatal("unlink accepted FIFO")
	}
	if err := s.Rename(ctx, "fifo", "moved"); err == nil {
		t.Fatal("rename accepted FIFO")
	}
	d, err := s.OpenDir(ctx, ".")
	must(t, err)
	defer func() { must(t, d.Close()) }()
	if _, err := d.ReadDir(ctx, 1); err == nil {
		t.Fatal("readdir accepted FIFO")
	}
	info, err := os.Lstat(root + "/fifo")
	must(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("failed operations changed FIFO")
	}
}

func TestOSMetadataWorkStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := NewOS(ctx, t.TempDir())
	must(t, err)
	defer func() { must(t, s.Close()) }()
	_ = openFile(t, s, "first", os.O_CREATE|os.O_RDWR)
	_ = openFile(t, s, "second", os.O_CREATE|os.O_RDWR)
	handle, err := s.OpenDir(ctx, ".")
	must(t, err)
	defer func() { must(t, handle.Close()) }()
	d := handle.(*osDir)
	calls := 0
	// Invoke the production metadata loop with counted *native* descriptor
	// stats, canceling as the first lookup completes. No second stat may start.
	entries, err := physicalEntries(ctx, []string{"first", "second"}, func(name string) (Info, error) {
		calls++
		info, err := statDirEntry(d.file, name)
		cancel()
		return info, err
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || len(entries) != 1 || entries[0].Name != "first" {
		t.Fatalf("metadata work continued after cancellation: calls=%d entries=%+v err=%v", calls, entries, err)
	}
}

func TestOSDescriptorRelativeEntryStat(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := NewOS(ctx, root)
	must(t, err)
	defer func() { must(t, s.Close()) }()
	must(t, s.Mkdir(ctx, "original", 0o700))
	f := openFile(t, s, "original/file", os.O_CREATE|os.O_RDWR)
	writeAt(t, f, "original", 0)
	original, err := s.Stat(ctx, "original/file")
	must(t, err)
	d, err := s.OpenDir(ctx, "original")
	must(t, err)
	defer func() { must(t, d.Close()) }()
	must(t, s.Rename(ctx, "original", "renamed"))
	must(t, s.Mkdir(ctx, "original", 0o700))
	replacement := openFile(t, s, "original/file", os.O_CREATE|os.O_RDWR)
	writeAt(t, replacement, "replacement-is-longer", 0)
	info, err := statDirEntry(d.(*osDir).file, "file")
	must(t, err)
	if info != original {
		t.Fatalf("native stat followed the old pathname: %+v / %+v", info, original)
	}
	// The raw lookup must itself reject symlinks without following their targets.
	must(t, os.Symlink("file", root+"/renamed/link"))
	if _, err := statDirEntry(d.(*osDir).file, "link"); err == nil {
		t.Fatal("descriptor-relative stat accepted symlink")
	}
	if _, err := statDirEntry(d.(*osDir).file, "missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

func TestOSDiscoveryNativeLinkCount(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "payload"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewOS(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backend.Close() }()
	info, err := backend.Stat(t.Context(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	if info.Links != 1 {
		t.Fatalf("initial links %d", info.Links)
	}
	if err = os.Link(filepath.Join(root, "payload"), filepath.Join(t.TempDir(), "external-alias")); err != nil {
		t.Fatal(err)
	}
	info, err = backend.Stat(t.Context(), "payload")
	if err != nil {
		t.Fatal(err)
	}
	if info.Links != 2 {
		t.Fatalf("external alias links %d", info.Links)
	}
	f, err := backend.Open(t.Context(), "payload", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	statter, ok := f.(Statter)
	if !ok {
		t.Fatal("missing descriptor stat")
	}
	info, err = statter.Stat(t.Context())
	if err != nil || info.Links != 2 {
		t.Fatalf("descriptor links %d: %v", info.Links, err)
	}
	d, err := backend.OpenDir(t.Context(), ".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	entries, err := d.ReadDir(t.Context(), 1)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Info.Links != 2 {
		t.Fatal("enumeration link count")
	}
}
