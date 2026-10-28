// gofuse is a Linux-only, fixed-size synthetic FUSE capability probe.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
	"homelabfs.local/experiments/fuse-library/internal/model"
)

type probe struct {
	fuse.RawFileSystem
	state    *model.State
	mu       sync.Mutex
	fds      map[uint64]int
	next     uint64
	lockPath string
	direct   bool
}

var _ fuse.RawFileSystem = (*probe)(nil)

func main() {
	run := flag.String("run-dir", "", "fresh run directory containing empty mnt and lock backing file")
	direct := flag.Bool("direct", false, "disable data page cache for every file")
	flag.Parse()
	if *run == "" {
		log.Fatal("-run-dir is required")
	}
	p := &probe{RawFileSystem: fuse.NewDefaultRawFileSystem(), state: model.New(), fds: make(map[uint64]int), lockPath: filepath.Join(*run, "lock-backing"), direct: *direct}
	srv, err := fuse.NewServer(p, filepath.Join(*run, "mnt"), &fuse.MountOptions{Name: "hfs-library-probe", FsName: "hfs-library-probe", EnableLocks: true, DisableXAttrs: true})
	if err != nil {
		log.Fatal(err)
	}
	changes := make(chan os.Signal, 1)
	signal.Notify(changes, syscall.SIGUSR1)
	defer signal.Stop(changes)
	go func() {
		for range changes {
			p.state.Mutate()
			log.Printf("event=invalidate inode=%s entry=%s", srv.InodeNotify(2, 0, model.Size), srv.EntryNotify(1, "data"))
		}
	}()
	log.Print("event=ready")
	srv.Serve()
}

func (p *probe) Lookup(_ <-chan struct{}, _ *fuse.InHeader, name string, out *fuse.EntryOut) fuse.Status {
	id := nodeID(name)
	log.Printf("event=lookup name=%s", name)
	out.SetEntryTimeout(time.Minute)
	if id == 0 {
		return fuse.ENOENT
	}
	out.NodeId = id
	out.SetAttrTimeout(time.Minute)
	fillAttr(id, &out.Attr)
	out.Size = p.state.Len(name)
	return fuse.OK
}

func (p *probe) GetAttr(_ <-chan struct{}, in *fuse.GetAttrIn, out *fuse.AttrOut) fuse.Status {
	fillAttr(in.NodeId, &out.Attr)
	if in.NodeId != 1 {
		out.Size = p.state.Len(nodeName(in.NodeId))
	}
	out.SetTimeout(time.Minute)
	return fuse.OK
}

func (p *probe) Open(_ <-chan struct{}, in *fuse.OpenIn, out *fuse.OpenOut) fuse.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	out.Fh = p.next
	// Cancellation must target a foreground request. Cached Linux readahead
	// can run as background FUSE work unrelated to the signalled reader.
	if p.direct || in.NodeId == nodeID("direct") || in.NodeId == nodeID("cancel") {
		out.OpenFlags = fuse.FOPEN_DIRECT_IO
	} else {
		out.OpenFlags = fuse.FOPEN_KEEP_CACHE
	}
	if in.NodeId == nodeID("lock") {
		fd, err := unix.Open(p.lockPath, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			return fuse.ToStatus(err)
		}
		p.fds[out.Fh] = fd
	}
	return fuse.OK
}

func (p *probe) Read(cancel <-chan struct{}, in *fuse.ReadIn, _ []byte) (fuse.ReadResult, fuse.Status) {
	name := nodeName(in.NodeId)
	model.Credentials(in.Uid, in.Gid, in.Pid, name)
	if name == "delayed" || name == "cancel" {
		log.Printf("event=read_wait name=%s", name)
		delay := 250 * time.Millisecond
		if name == "cancel" {
			delay = 3 * time.Second
		}
		// The worker owns its data; request memory never escapes the callback.
		result := make(chan []byte, 1)
		data := p.state.Read(name, int64(in.Offset), int(in.Size))
		go func() { time.Sleep(delay); result <- data }()
		select {
		case data := <-result:
			log.Printf("event=worker_reply name=%s", name)
			return fuse.ReadResultData(data), fuse.OK
		case <-cancel:
			log.Printf("event=cancelled name=%s", name)
			return nil, fuse.EINTR
		}
	}
	return fuse.ReadResultData(p.state.Read(name, int64(in.Offset), int(in.Size))), fuse.OK
}

func (p *probe) Write(_ <-chan struct{}, in *fuse.WriteIn, data []byte) (uint32, fuse.Status) {
	return uint32(p.state.Write(nodeName(in.NodeId), int64(in.Offset), data)), fuse.OK
}

func (p *probe) Fsync(_ <-chan struct{}, _ *fuse.FsyncIn) fuse.Status { return fuse.OK }
func (p *probe) Flush(_ <-chan struct{}, _ *fuse.FlushIn) fuse.Status { return fuse.OK }

func (p *probe) Release(_ <-chan struct{}, in *fuse.ReleaseIn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if fd, ok := p.fds[in.Fh]; ok {
		if err := unix.Close(fd); err != nil {
			log.Printf("event=close_error error=%q", err)
		}
		delete(p.fds, in.Fh)
	}
}

func (p *probe) GetLk(_ <-chan struct{}, in *fuse.LkIn, out *fuse.LkOut) fuse.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	lk, status := lockToFlock(in.Lk)
	if status != fuse.OK {
		return status
	}
	fd, ok := p.fds[in.Fh]
	if !ok {
		return fuse.EBADF
	}
	err := syscall.FcntlFlock(uintptr(fd), unix.F_OFD_GETLK, &lk)
	out.Lk = in.Lk
	out.Lk.FromFlockT(&lk)
	log.Printf("event=getlk error=%v", err)
	return fuse.ToStatus(err)
}

func (p *probe) SetLk(_ <-chan struct{}, in *fuse.LkIn) fuse.Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	fd, ok := p.fds[in.Fh]
	if !ok {
		return fuse.EBADF
	}
	var err error
	if in.LkFlags&fuse.FUSE_LK_FLOCK != 0 {
		op := unix.LOCK_UN
		if in.Lk.Typ == unix.F_WRLCK {
			op = unix.LOCK_EX | unix.LOCK_NB
		}
		if in.Lk.Typ == unix.F_RDLCK {
			op = unix.LOCK_SH | unix.LOCK_NB
		}
		err = unix.Flock(fd, op)
	} else {
		lk, status := lockToFlock(in.Lk)
		if status != fuse.OK {
			return status
		}
		err = syscall.FcntlFlock(uintptr(fd), unix.F_OFD_SETLK, &lk)
	}
	log.Printf("event=setlk flags=%d type=%d error=%v", in.LkFlags, in.Lk.Typ, err)
	return fuse.ToStatus(err)
}

func (p *probe) SetLkw(cancel <-chan struct{}, in *fuse.LkIn) fuse.Status {
	// Linux may route flock(LOCK_UN) through FUSE_SETLKW even though unlock
	// does not block. Forward that operation without changing its semantics.
	if in.Lk.Typ == unix.F_UNLCK {
		log.Print("event=setlkw_unlock")
		return p.SetLk(cancel, in)
	}
	// This bounded probe exercises only nonblocking lock acquisition.
	// Blocking ownership/cancellation needs a separate implementation and run.
	log.Print("event=setlkw_unsupported")
	return fuse.ENOSYS
}

func lockToFlock(in fuse.FileLock) (syscall.Flock_t, fuse.Status) {
	// FUSE uses signed off_t's maximum as its EOF sentinel, whereas fcntl
	// represents through-EOF with length zero. The pinned binding helper knows
	// this mapping. Validate ranges before it converts unsigned arithmetic.
	if in.Start > math.MaxInt64 || in.End > math.MaxInt64 || in.End < in.Start {
		return syscall.Flock_t{}, fuse.EINVAL
	}
	if in.Typ != unix.F_RDLCK && in.Typ != unix.F_WRLCK && in.Typ != unix.F_UNLCK {
		return syscall.Flock_t{}, fuse.EINVAL
	}
	var out syscall.Flock_t
	in.ToFlockT(&out)
	return out, fuse.OK
}

func nodeID(name string) uint64 {
	for i, n := range []string{"data", "direct", "delayed", "cancel", "credentials", "lock"} {
		if name == n {
			return uint64(i + 2)
		}
	}
	return 0
}

func nodeName(id uint64) string {
	for _, n := range []string{"data", "direct", "delayed", "cancel", "credentials", "lock"} {
		if nodeID(n) == id {
			return n
		}
	}
	return fmt.Sprintf("unknown-%d", id)
}

func fillAttr(id uint64, out *fuse.Attr) {
	out.Ino, out.Mode, out.Nlink = id, syscall.S_IFREG|0600, 1
	out.Size = model.Size
	out.Uid, out.Gid = uint32(os.Getuid()), uint32(os.Getgid())
	if id == 1 {
		out.Mode, out.Size = syscall.S_IFDIR|0700, 0
	}
}
