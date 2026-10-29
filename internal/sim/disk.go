package sim

import (
	"context"
	"fmt"
	"io/fs"
	"sync"

	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type diskBudget struct {
	mu             sync.Mutex
	calls, written int
}
type diskGate struct {
	mu     sync.RWMutex
	dead   bool
	budget *diskBudget
}
type disk struct {
	backend *vfs.Simulator
	gate    *diskGate
}
type file struct {
	backend vfs.File
	gate    *diskGate
}
type directory struct {
	backend vfs.Dir
	gate    *diskGate
}

var _ storage.Filesystem = (*disk)(nil)
var _ vfs.File = (*file)(nil)
var _ vfs.Statter = (*file)(nil)
var _ vfs.Dir = (*directory)(nil)

func (g *diskGate) fence() { g.mu.Lock(); g.dead = true; g.mu.Unlock() }
func (g *diskGate) enter(bytes int) (func(), error) {
	g.mu.RLock()
	if g.dead {
		g.mu.RUnlock()
		return nil, fs.ErrClosed
	}
	g.budget.mu.Lock()
	if g.budget.calls >= 100000 || bytes > 8<<20-g.budget.written {
		g.budget.mu.Unlock()
		g.mu.RUnlock()
		return nil, fmt.Errorf("simulation storage budget exhausted")
	}
	g.budget.calls++
	g.budget.written += bytes
	g.budget.mu.Unlock()
	return g.mu.RUnlock, nil
}
func guarded[T any](g *diskGate, bytes int, fn func() (T, error)) (T, error) {
	var zero T
	release, err := g.enter(bytes)
	if err != nil {
		return zero, err
	}
	defer release()
	return fn()
}
func guard(g *diskGate, fn func() error) error {
	_, err := guarded(g, 0, func() (struct{}, error) { return struct{}{}, fn() })
	return err
}
func (d *disk) Open(ctx context.Context, name string, flags int, mode fs.FileMode) (vfs.File, error) {
	return guarded(d.gate, 0, func() (vfs.File, error) {
		f, err := d.backend.Open(ctx, name, flags, mode)
		if err != nil {
			return nil, err
		}
		return &file{backend: f, gate: d.gate}, nil
	})
}
func (d *disk) Rename(ctx context.Context, a, b string) error {
	return guard(d.gate, func() error { return d.backend.Rename(ctx, a, b) })
}
func (d *disk) Unlink(ctx context.Context, name string) error {
	return guard(d.gate, func() error { return d.backend.Unlink(ctx, name) })
}
func (d *disk) Mkdir(ctx context.Context, name string, mode fs.FileMode) error {
	return guard(d.gate, func() error { return d.backend.Mkdir(ctx, name, mode) })
}
func (d *disk) SyncDir(ctx context.Context, name string) error {
	return guard(d.gate, func() error { return d.backend.SyncDir(ctx, name) })
}
func (d *disk) Stat(ctx context.Context, name string) (vfs.Info, error) {
	return guarded(d.gate, 0, func() (vfs.Info, error) { return d.backend.Stat(ctx, name) })
}
func (d *disk) OpenDir(ctx context.Context, name string) (vfs.Dir, error) {
	return guarded(d.gate, 0, func() (vfs.Dir, error) {
		dir, err := d.backend.OpenDir(ctx, name)
		if err != nil {
			return nil, err
		}
		return &directory{backend: dir, gate: d.gate}, nil
	})
}
func (d *disk) Rmdir(ctx context.Context, name string) error {
	return guard(d.gate, func() error { return d.backend.Rmdir(ctx, name) })
}
func (f *file) ReadAt(ctx context.Context, b []byte, off int64) (int, error) {
	return guarded(f.gate, 0, func() (int, error) { return f.backend.ReadAt(ctx, b, off) })
}
func (f *file) WriteAt(ctx context.Context, b []byte, off int64) (int, error) {
	if off < 0 || off > 8<<20 || int64(len(b)) > 8<<20-off {
		return 0, fmt.Errorf("simulation write range budget")
	}
	// Admit the entire resulting extent conservatively, including sparse gaps.
	// A one-byte write near the file limit must not allocate an uncharged 8 MiB.
	return guarded(f.gate, int(off)+len(b), func() (int, error) { return f.backend.WriteAt(ctx, b, off) })
}
func (f *file) Truncate(ctx context.Context, size int64) error {
	if size < 0 || size > 8<<20 {
		return fmt.Errorf("simulation truncate budget")
	}
	_, err := guarded(f.gate, int(size), func() (struct{}, error) { return struct{}{}, f.backend.Truncate(ctx, size) })
	return err
}
func (f *file) Sync(ctx context.Context) error {
	return guard(f.gate, func() error { return f.backend.Sync(ctx) })
}
func (f *file) DataSync(ctx context.Context) error {
	return guard(f.gate, func() error { return f.backend.DataSync(ctx) })
}
func (f *file) Stat(ctx context.Context) (vfs.Info, error) {
	return guarded(f.gate, 0, func() (vfs.Info, error) { return f.backend.(vfs.Statter).Stat(ctx) })
}
func (f *file) Close() error { return f.backend.Close() }
func (d *directory) ReadDir(ctx context.Context, n int) ([]vfs.Entry, error) {
	return guarded(d.gate, 0, func() ([]vfs.Entry, error) { return d.backend.ReadDir(ctx, n) })
}
func (d *directory) Stat(ctx context.Context) (vfs.Info, error) {
	return guarded(d.gate, 0, func() (vfs.Info, error) { return d.backend.Stat(ctx) })
}
func (d *directory) Close() error { return d.backend.Close() }
