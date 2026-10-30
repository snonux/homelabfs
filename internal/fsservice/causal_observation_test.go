package fsservice

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type observedRoot struct {
	storage.Filesystem
	opens, reads, syncs, dirs int
	readBytes                 int
	syncTime, readTime        time.Duration
}
type observedFile struct {
	vfs.File
	root *observedRoot
}

func (r *observedRoot) Open(ctx context.Context, name string, flags int, perm fs.FileMode) (vfs.File, error) {
	r.opens++
	f, e := r.Filesystem.Open(ctx, name, flags, perm)
	if e != nil {
		return nil, e
	}
	return &observedFile{f, r}, nil
}
func (r *observedRoot) SyncDir(ctx context.Context, name string) error {
	r.dirs++
	start := time.Now()
	e := r.Filesystem.SyncDir(ctx, name)
	r.syncTime += time.Since(start)
	return e
}
func (f *observedFile) Sync(ctx context.Context) error {
	f.root.syncs++
	start := time.Now()
	e := f.File.Sync(ctx)
	f.root.syncTime += time.Since(start)
	return e
}
func (f *observedFile) ReadAt(ctx context.Context, b []byte, off int64) (int, error) {
	f.root.reads++
	start := time.Now()
	n, e := f.File.ReadAt(ctx, b, off)
	f.root.readBytes += n
	f.root.readTime += time.Since(start)
	return n, e
}

// TestAppendCausalObservation keeps a bounded real-filesystem workload for
// profiling append cost at two retained catalog/history sizes. Timing is an
// observation, never a flaky pass threshold. Both cuts require exact content.
func TestAppendCausalObservation(t *testing.T) {
	for _, objects := range []int{1, 24} {
		t.Run(fmt.Sprint(objects), func(t *testing.T) {
			f := setup(t, "os")
			// Use a large segment so this observation is independent of rotation stress.
			f.c.Segment.MaximumBytes = 16 << 20
			data := &observedRoot{Filesystem: f.c.Workspace.Data}
			source := &observedRoot{Filesystem: f.c.Source}
			f.c.Workspace.Data = data
			f.c.Source = source
			f.start()
			var handle fsapi.OpenHandle
			for i := 0; i < objects; i++ {
				c := f.create(f.c.Storage.Root, fmt.Sprintf("object-%02d", i))
				handle = c.Handle
				f.ok(fsapi.Command{Operation: fsapi.Write, Handle: handle, Data: []byte("seeded synthetic payload\n")})
			}
			opened := f.ok(fsapi.Command{Operation: fsapi.Open, Object: f.service.handles[handle].object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Append: true}})
			*data = observedRoot{Filesystem: data.Filesystem}
			*source = observedRoot{Filesystem: source.Filesystem}
			want := []byte("seeded synthetic payload\n")
			start := time.Now()
			for i := 0; i < 32; i++ {
				data := []byte(fmt.Sprintf("synthetic-append-%02d\n", i))
				want = append(want, data...)
				f.ok(fsapi.Command{Operation: fsapi.Write, Handle: opened.Handle, Data: data})
			}
			t.Logf("objects=%d writes=32 wall=%v data_file_sync=%d data_dir_sync=%d data_sync_time=%v data_read_calls=%d data_bytes=%d source_read_calls=%d source_bytes=%d source_read_time=%v", objects, time.Since(start), data.syncs, data.dirs, data.syncTime, data.reads, data.readBytes, source.reads, source.readBytes, source.readTime)
			f.ok(fsapi.Command{Operation: fsapi.Sync, Handle: opened.Handle, Durability: fsapi.FileDurability})
			actual := f.ok(fsapi.Command{Operation: fsapi.Read, Handle: opened.Handle, Size: int64(len(want) + 1)})
			if !bytes.Equal(actual.Data, want) {
				t.Fatal("append content differs from complete ordered oracle")
			}
		})
	}
}
