package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"math/rand/v2"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
)

// LossMode selects recovery of changes made after the most recent barrier.
type LossMode uint8

const (
	// Lose restores the last synced state. This is the default.
	Lose LossMode = iota
	// Keep retains all changes, as if background writeback completed.
	Keep
	// Tear combines old and new bytes and may partially persist a size change.
	// It is only valid for DataLoss.
	Tear
	// Random chooses Lose, Keep or Tear for each file, and Lose or Keep for
	// each directory, using the simulator's reproducible PRNG stream.
	Random
)

// Config sets simulator recovery behavior. MaxFileSize bounds each file's
// in-memory allocation; zero selects 16 MiB. Callers must also bound the number
// of files and operations. Seed and the serialized operation schedule together
// reproduce a run; concurrent arrival order is not deterministic.
type Config struct {
	Seed          uint64
	DataLoss      LossMode
	NamespaceLoss LossMode
	MaxFileSize   int64
}

// Simulator is an in-memory, inode-based local crash model. Each method is
// atomic with respect to other methods and Crash. The zero value is not usable;
// construct it with NewSimulator. It must not be copied after first use.
type Simulator struct {
	mu         sync.Mutex
	config     Config
	rng        *rand.Rand
	nodes      []*memNode
	generation uint64
	namespace  []namespaceChange
	lastChange map[int]int
}

type memNode struct {
	id            int
	dir           bool
	mode          fs.FileMode
	data          []byte
	syncedData    []byte
	entries       map[string]int
	syncedEntries map[string]int
}

type entryChange struct {
	parent int
	name   string
	target int
}

type namespaceChange struct {
	entries      []entryChange
	parents      map[int]bool
	dependencies []int
}

type memFile struct {
	sim        *Simulator
	id         int
	generation uint64
	flag       int
	closed     bool
}

var (
	_ FS   = (*Simulator)(nil)
	_ File = (*memFile)(nil)
)

// NewSimulator creates an empty root directory with a durable initial state.
func NewSimulator(config Config) (*Simulator, error) {
	if config.DataLoss > Random || config.NamespaceLoss > Random || config.NamespaceLoss == Tear || config.MaxFileSize < 0 {
		return nil, fmt.Errorf("simulator configuration: %w", fs.ErrInvalid)
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 16 << 20
	}
	// Avoid int overflow when allocating on either supported architecture.
	if uint64(config.MaxFileSize) >= uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("simulator file size limit: %w", fs.ErrInvalid)
	}
	s := &Simulator{config: config, rng: rand.New(rand.NewPCG(config.Seed, config.Seed^0x9e3779b97f4a7c15)), lastChange: make(map[int]int)}
	s.nodes = []*memNode{{dir: true, mode: fs.ModeDir | 0o700, entries: make(map[string]int), syncedEntries: make(map[string]int)}}
	return s, nil
}

// Open opens or creates a regular file with the same flags as OS.Open. Creation
// changes only the live parent directory; even syncing the new file does not
// make its name durable. Existing handles survive rename and unlink.
func (s *Simulator) Open(ctx context.Context, name string, flag int, perm fs.FileMode) (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validOpen(flag, perm); err != nil {
		return nil, err
	}
	parent, base, err := s.parent(name)
	if err != nil {
		return nil, err
	}
	id, exists := parent.entries[base]
	if exists {
		if flag&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL {
			return nil, pathError("open", name, fs.ErrExist)
		}
		if s.nodes[id].dir {
			return nil, pathError("open", name, syscall.EISDIR)
		}
	} else {
		if flag&os.O_CREATE == 0 {
			return nil, pathError("open", name, fs.ErrNotExist)
		}
		id = len(s.nodes)
		s.nodes = append(s.nodes, &memNode{id: id, mode: perm})
		s.change([]entryChange{{parent.id, base, id}}, []int{parent.id, id})
	}
	if flag&os.O_TRUNC != 0 {
		s.nodes[id].data = nil
	}
	return &memFile{sim: s, id: id, generation: s.generation, flag: flag}, nil
}

// Rename atomically replaces a file or directory name in the live namespace.
// Different parent directories require separate barriers. Crash retains or
// loses an entire rename, including replacement, with its namespace dependencies.
func (s *Simulator) Rename(ctx context.Context, oldname, newname string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	oldparent, oldbase, err := s.parent(oldname)
	if err != nil {
		return err
	}
	newparent, newbase, err := s.parent(newname)
	if err != nil {
		return err
	}
	id, exists := oldparent.entries[oldbase]
	if !exists {
		return pathError("rename", oldname, fs.ErrNotExist)
	}
	touched := []int{oldparent.id, newparent.id, id}
	if target, ok := newparent.entries[newbase]; ok {
		if target == id {
			return nil
		}
		if s.nodes[target].dir != s.nodes[id].dir {
			return pathError("rename", newname, syscall.EINVAL)
		}
		if s.nodes[target].dir && len(s.nodes[target].entries) != 0 {
			return pathError("rename", newname, syscall.ENOTEMPTY)
		}
		touched = append(touched, target)
	}
	if s.nodes[id].dir && s.contains(id, newparent.id) {
		return pathError("rename", newname, syscall.EINVAL)
	}
	s.change([]entryChange{{oldparent.id, oldbase, -1}, {newparent.id, newbase, id}}, touched)
	return nil
}

// Unlink removes a regular-file name. It does not destroy open handles or
// persist the parent directory; Crash may resurrect an unsynced removal.
func (s *Simulator) Unlink(ctx context.Context, name string) error {
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
		return pathError("unlink", name, fs.ErrNotExist)
	}
	if s.nodes[id].dir {
		return pathError("unlink", name, syscall.EINVAL)
	}
	s.change([]entryChange{{parent.id, base, -1}}, []int{parent.id, id})
	return nil
}

// Mkdir creates an empty directory. A barrier on its parent persists the new
// name; a barrier on the directory itself persists entries subsequently added.
func (s *Simulator) Mkdir(ctx context.Context, name string, perm fs.FileMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if perm & ^fs.FileMode(0o777) != 0 {
		return syscall.EINVAL
	}
	parent, base, err := s.parent(name)
	if err != nil {
		return err
	}
	if _, exists := parent.entries[base]; exists {
		return pathError("mkdir", name, fs.ErrExist)
	}
	id := len(s.nodes)
	s.nodes = append(s.nodes, &memNode{id: id, dir: true, mode: fs.ModeDir | perm, entries: make(map[string]int), syncedEntries: make(map[string]int)})
	// A child barrier must not implicitly persist this directory's own name.
	s.change([]entryChange{{parent.id, base, id}}, []int{parent.id})
	return nil
}

// SyncDir marks preceding namespace changes to this directory as synced.
// A cross-directory rename requires both parents. Persisting an operation also
// persists its preceding namespace dependencies, but never file contents.
func (s *Simulator) SyncDir(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	node, err := s.lookup(name)
	if err != nil {
		return err
	}
	if !node.dir {
		return pathError("syncdir", name, syscall.ENOTDIR)
	}
	for i := range s.namespace {
		if _, touches := s.namespace[i].parents[node.id]; touches {
			s.namespace[i].parents[node.id] = true
		}
	}
	return nil
}

// Crash discards all handles and recovers data and directory entries according
// to Config. Recovered state becomes the new durable baseline. It never touches
// OS files or exits the process. Record Seed and operation order in evidence.
func (s *Simulator) Crash() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	// Slice order makes PRNG consumption independent of Go map iteration.
	for _, node := range s.nodes {
		if node == nil {
			continue
		}
		if node.dir {
			node.entries = maps.Clone(node.syncedEntries)
			continue
		}
		switch s.mode(s.config.DataLoss, false) {
		case Lose:
			node.data = append([]byte(nil), node.syncedData...)
		case Tear:
			node.data = s.torn(node.syncedData, node.data)
		}
		node.syncedData = append([]byte(nil), node.data...)
	}
	s.recoverNamespace()
	s.collectUnreachable()
	s.namespace = nil
	clear(s.lastChange)
}

func (f *memFile) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	f.sim.mu.Lock()
	defer f.sim.mu.Unlock()
	if err := f.check(ctx); err != nil {
		return 0, err
	}
	if f.flag&os.O_WRONLY != 0 {
		return 0, syscall.EBADF
	}
	if off < 0 {
		return 0, syscall.EINVAL
	}
	if len(p) == 0 {
		return 0, nil
	}
	data := f.sim.nodes[f.id].data
	if off >= int64(len(data)) {
		return 0, io.EOF
	}
	n := copy(p, data[int(off):])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *memFile) WriteAt(ctx context.Context, p []byte, off int64) (int, error) {
	f.sim.mu.Lock()
	defer f.sim.mu.Unlock()
	if err := f.check(ctx); err != nil {
		return 0, err
	}
	if f.flag&(os.O_WRONLY|os.O_RDWR) == 0 {
		return 0, syscall.EBADF
	}
	if off < 0 {
		return 0, syscall.EINVAL
	}
	if off > f.sim.config.MaxFileSize || int64(len(p)) > f.sim.config.MaxFileSize-off {
		return 0, syscall.EFBIG
	}
	if len(p) == 0 {
		return 0, nil
	}
	node := f.sim.nodes[f.id]
	end := int(off) + len(p)
	if end > len(node.data) {
		node.data = append(node.data, make([]byte, end-len(node.data))...)
	}
	return copy(node.data[int(off):], p), nil
}

func (f *memFile) Truncate(ctx context.Context, size int64) error {
	f.sim.mu.Lock()
	defer f.sim.mu.Unlock()
	if err := f.check(ctx); err != nil {
		return err
	}
	if f.flag&(os.O_WRONLY|os.O_RDWR) == 0 {
		return syscall.EBADF
	}
	if size < 0 {
		return syscall.EINVAL
	}
	if size > f.sim.config.MaxFileSize {
		return syscall.EFBIG
	}
	node := f.sim.nodes[f.id]
	if size <= int64(len(node.data)) {
		node.data = node.data[:int(size)]
	} else {
		node.data = append(node.data, make([]byte, int(size)-len(node.data))...)
	}
	return nil
}

func (f *memFile) Sync(ctx context.Context) error {
	f.sim.mu.Lock()
	defer f.sim.mu.Unlock()
	if err := f.check(ctx); err != nil {
		return err
	}
	node := f.sim.nodes[f.id]
	node.syncedData = append([]byte(nil), node.data...)
	return nil
}

func (f *memFile) DataSync(ctx context.Context) error { return f.Sync(ctx) }

func (f *memFile) Close() error {
	f.sim.mu.Lock()
	defer f.sim.mu.Unlock()
	if f.closed || f.generation != f.sim.generation {
		return fs.ErrClosed
	}
	f.closed = true
	return nil
}

func (s *Simulator) lookup(name string) (*memNode, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	node := s.nodes[0]
	if name == "." {
		return node, nil
	}
	for _, component := range strings.Split(name, "/") {
		if !node.dir {
			return nil, pathError("lookup", name, syscall.ENOTDIR)
		}
		id, exists := node.entries[component]
		if !exists {
			return nil, pathError("lookup", name, fs.ErrNotExist)
		}
		node = s.nodes[id]
	}
	return node, nil
}

func (s *Simulator) parent(name string) (*memNode, string, error) {
	if err := validName(name); err != nil {
		return nil, "", err
	}
	if name == "." {
		return nil, "", pathError("parent", name, syscall.EINVAL)
	}
	parent, err := s.lookup(path.Dir(name))
	if err != nil {
		return nil, "", err
	}
	if !parent.dir {
		return nil, "", pathError("parent", name, syscall.ENOTDIR)
	}
	return parent, path.Base(name), nil
}

func (f *memFile) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.closed || f.generation != f.sim.generation {
		return fs.ErrClosed
	}
	return nil
}

func (s *Simulator) mode(mode LossMode, dir bool) LossMode {
	if mode != Random {
		return mode
	}
	if dir {
		return LossMode(s.rng.IntN(2))
	}
	return LossMode(s.rng.IntN(3))
}

func (s *Simulator) torn(stable, live []byte) []byte {
	low, high := min(len(stable), len(live)), max(len(stable), len(live))
	size := low + s.rng.IntN(high-low+1)
	result := make([]byte, size)
	copy(result, stable)
	prefix := s.rng.IntN(min(size, len(live)) + 1)
	copy(result[:prefix], live[:prefix])
	return result
}

func (s *Simulator) change(entries []entryChange, touched []int) {
	change := namespaceChange{entries: entries, parents: make(map[int]bool)}
	for _, id := range touched {
		if previous, ok := s.lastChange[id]; ok {
			change.dependencies = append(change.dependencies, previous)
		}
	}
	for _, entry := range entries {
		change.parents[entry.parent] = false
	}
	for _, id := range touched {
		s.lastChange[id] = len(s.namespace)
	}
	s.namespace = append(s.namespace, change)
	s.applyEntries(entries)
}

func (s *Simulator) applyEntries(entries []entryChange) {
	for _, entry := range entries {
		if entry.target < 0 {
			delete(s.nodes[entry.parent].entries, entry.name)
		} else {
			s.nodes[entry.parent].entries[entry.name] = entry.target
		}
	}
}

func (s *Simulator) recoverNamespace() {
	keep := make([]bool, len(s.namespace))
	for i, change := range s.namespace {
		durable := true
		for _, synced := range change.parents {
			durable = durable && synced
		}
		keep[i] = durable || s.mode(s.config.NamespaceLoss, true) == Keep
	}
	// Dependencies always precede the dependent operation. A reverse pass
	// closes the graph without recursion, then replay preserves live ordering.
	for i := len(keep) - 1; i >= 0; i-- {
		if keep[i] {
			for _, dependency := range s.namespace[i].dependencies {
				keep[dependency] = true
			}
		}
	}
	for i, change := range s.namespace {
		if keep[i] {
			s.applyEntries(change.entries)
		}
	}
	for _, node := range s.nodes {
		if node != nil && node.dir {
			node.syncedEntries = maps.Clone(node.entries)
		}
	}
}

func (s *Simulator) contains(root, candidate int) bool {
	if root == candidate {
		return true
	}
	for _, id := range s.nodes[root].entries {
		if s.nodes[id].dir && s.contains(id, candidate) {
			return true
		}
	}
	return false
}

func (s *Simulator) collectUnreachable() {
	reachable := make(map[int]bool)
	var visit func(int)
	visit = func(id int) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, child := range s.nodes[id].entries {
			visit(child)
		}
	}
	visit(0)
	for id := range s.nodes {
		if !reachable[id] {
			s.nodes[id] = nil
		}
	}
}

func pathError(op, name string, err error) error { return &fs.PathError{Op: op, Path: name, Err: err} }
