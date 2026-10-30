package fsservice

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"syscall"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type limitedSource struct {
	storage.Filesystem
	failure *error
}
type limitedSourceFile struct {
	vfs.File
	failure *error
}

func (s limitedSource) Open(ctx context.Context, name string, flag int, mode fs.FileMode) (vfs.File, error) {
	f, err := s.Filesystem.Open(ctx, name, flag, mode)
	if err != nil {
		return nil, err
	}
	return limitedSourceFile{f, s.failure}, nil
}
func (f limitedSourceFile) WriteAt(ctx context.Context, p []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(ctx, p, off)
	if err != nil && *f.failure == nil {
		*f.failure = fmt.Errorf("source write offset=%d bytes=%d wrote=%d: %w", off, len(p), n, err)
	}
	return n, err
}

func TestInheritedSourceJournalFileLimit(t *testing.T) {
	for _, limit := range []string{"262144", "134217728"} {
		t.Run(limit, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestInheritedSourceJournalFileLimitChild$", "-test.v")
			cmd.Env = append(os.Environ(), "HFS_TEST_SOURCE_FILE_LIMIT="+limit)
			out, err := cmd.CombinedOutput()
			t.Logf("%s", out)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestInheritedSourceJournalFileLimitChild(t *testing.T) {
	mode := os.Getenv("HFS_TEST_SOURCE_FILE_LIMIT")
	if mode == "" {
		t.Skip("child only")
	}
	var inherited syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &inherited); err != nil {
		t.Fatal(err)
	}
	inherited.Cur = 134217728
	if mode == "262144" {
		inherited.Cur, inherited.Max = 262144, 262144
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &inherited); err != nil {
		t.Fatal(err)
	}
	f := setup(t, "os")
	f.c.Segment.MaximumBytes = 1 << 20
	var raw error
	f.c.Source = limitedSource{f.c.Source, &raw}
	f.start()
	a := f.create(f.c.Storage.Root, "growth")
	for n := 0; n < 80; n++ {
		r := f.request(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: make([]byte, 4096)})
		if r.Code != fsapi.OK {
			if mode != "262144" || r.Code != fsapi.IO || f.service.State() != Blocked || !errors.Is(raw, syscall.EFBIG) {
				t.Fatalf("write %d code=%v state=%v raw=%v", n, r.Code, f.service.State(), raw)
			}
			next := fsapi.NewRequest(fsapi.Cred{})
			if err := f.service.Dispatch(t.Context(), next, fsapi.Command{Operation: fsapi.Getattr}); err != fsapi.Unavailable {
				t.Fatalf("next dispatch: %v", err)
			}
			t.Logf("controlled cap reproduced write=%d code=%v state=%v next=UNAVAILABLE raw=%v", n, r.Code, f.service.State(), raw)
			return
		}
	}
	if mode == "262144" {
		t.Fatal("cap did not trigger")
	}
	if raw != nil || f.service.State() != Serving {
		t.Fatalf("fixed-limit failure: %v state=%v", raw, f.service.State())
	}
	t.Log("same 80 source mutations passed with 128 MiB child file limit; wrote >327680 journal payload bytes and stayed Serving")
}
