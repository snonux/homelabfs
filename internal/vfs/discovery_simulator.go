package vfs

import (
	"context"
	"io"
	"io/fs"
	"syscall"
)

type memDir struct {
	sim        *Simulator
	id         int
	generation uint64
	last       string
	closed     bool
}

var (
	_ Discovery = (*Simulator)(nil)
	_ Statter   = (*memFile)(nil)
	_ Dir       = (*memDir)(nil)
)

// Stat reports the simulator's physical identity, type and configured mode.
func (s *Simulator) Stat(ctx context.Context, name string) (Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	node, err := s.lookup(name)
	if err != nil {
		return Info{}, err
	}
	return node.info(), nil
}

// OpenDir binds a discovery handle to the directory's local identity.
func (s *Simulator) OpenDir(ctx context.Context, name string) (Dir, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	node, err := s.lookup(name)
	if err != nil {
		return nil, err
	}
	if !node.dir {
		return nil, pathError("opendir", name, syscall.ENOTDIR)
	}
	return &memDir{sim: s, id: node.id, generation: s.generation}, nil
}

// Rmdir removes an empty directory; its parent barrier makes removal durable.
func (s *Simulator) Rmdir(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	parent, base, err := s.parent(name)
	if err != nil {
		return err
	}
	id, exists := parent.entries[base]
	if !exists {
		return pathError("rmdir", name, fs.ErrNotExist)
	}
	node := s.nodes[id]
	if !node.dir {
		return pathError("rmdir", name, syscall.ENOTDIR)
	}
	if len(node.entries) != 0 {
		return pathError("rmdir", name, syscall.ENOTEMPTY)
	}
	s.change([]entryChange{{parent.id, base, -1}}, []int{parent.id, id})
	return nil
}

func (f *memFile) Stat(ctx context.Context) (Info, error) {
	f.sim.mu.Lock()
	defer f.sim.mu.Unlock()
	if err := f.check(ctx); err != nil {
		return Info{}, err
	}
	return f.sim.nodes[f.id].info(), nil
}

func (d *memDir) Stat(ctx context.Context) (Info, error) {
	d.sim.mu.Lock()
	defer d.sim.mu.Unlock()
	if err := d.check(ctx); err != nil {
		return Info{}, err
	}
	return d.sim.nodes[d.id].info(), nil
}

func (d *memDir) ReadDir(ctx context.Context, n int) ([]Entry, error) {
	d.sim.mu.Lock()
	defer d.sim.mu.Unlock()
	if err := d.check(ctx); err != nil {
		return nil, err
	}
	if n < 1 || n > MaxReadDir {
		return nil, fs.ErrInvalid
	}
	result := make([]Entry, 0, n)
	// Select one successor at a time instead of sorting/materializing the entire
	// directory. This trades scan time for O(n) allocation regardless of size.
	for len(result) < n {
		next := ""
		for name := range d.sim.nodes[d.id].entries {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			if name > d.last && (next == "" || name < next) {
				next = name
			}
		}
		if next == "" {
			return result, io.EOF
		}
		id := d.sim.nodes[d.id].entries[next]
		result = append(result, Entry{Name: next, Info: d.sim.nodes[id].info()})
		d.last = next
	}
	return result, nil
}

func (d *memDir) Close() error {
	d.sim.mu.Lock()
	defer d.sim.mu.Unlock()
	if err := d.check(context.Background()); err != nil {
		return err
	}
	d.closed = true
	return nil
}

func (d *memDir) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.closed || d.generation != d.sim.generation {
		return fs.ErrClosed
	}
	return nil
}

func (node *memNode) info() Info {
	size := int64(len(node.data))
	if node.dir {
		size = 0
	}
	return Info{Mode: node.mode, Size: size, Identity: Identity{Device: 1, File: uint64(node.id) + 1}}
}
