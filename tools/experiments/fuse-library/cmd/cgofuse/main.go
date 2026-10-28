// cgofuse is a Linux-only, fixed-size synthetic FUSE capability probe.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/winfsp/cgofuse/fuse"
	"homelabfs.local/experiments/fuse-library/internal/model"
)

type probe struct {
	fuse.FileSystemBase
	state *model.State
}

var _ fuse.FileSystemInterface = (*probe)(nil)

func main() {
	run := flag.String("run-dir", "", "fresh run directory containing empty mnt")
	direct := flag.Bool("direct", false, "disable page caching through FUSE3 config")
	flag.Parse()
	if *run == "" {
		log.Fatal("-run-dir is required")
	}
	p := &probe{state: model.New()}
	host := fuse.NewFileSystemHost(p)
	host.SetDirectIO(*direct)
	options := []string{"-o", "attr_timeout=60,entry_timeout=60,negative_timeout=60,kernel_cache"}
	if *direct && bindingABI == "FUSE2" {
		options = append(options, "-o", "direct_io")
	}
	log.Printf("event=binding abi=%s direct=%t", bindingABI, *direct)
	changes := make(chan os.Signal, 1)
	signal.Notify(changes, syscall.SIGUSR1)
	defer signal.Stop(changes)
	go func() {
		for range changes {
			p.state.Mutate()
			log.Printf("event=invalidate notify=%t", host.Notify("/data", fuse.NOTIFY_TRUNCATE))
		}
	}()
	if !host.Mount(filepath.Join(*run, "mnt"), options) {
		log.Fatal("mount failed")
	}
}

func (p *probe) Init() { log.Print("event=ready") }

func (p *probe) Getattr(path string, stat *fuse.Stat_t, _ uint64) int {
	if path == "/" {
		stat.Mode, stat.Nlink = fuse.S_IFDIR|0700, 2
	} else {
		if !p.state.Exists(strings.TrimPrefix(path, "/")) {
			return -fuse.ENOENT
		}
		stat.Mode, stat.Nlink, stat.Size = fuse.S_IFREG|0600, 1, int64(p.state.Len(strings.TrimPrefix(path, "/")))
	}
	stat.Uid, stat.Gid = uint32(os.Getuid()), uint32(os.Getgid())
	return 0
}

func (p *probe) Open(path string, _ int) (int, uint64) {
	if !p.state.Exists(strings.TrimPrefix(path, "/")) {
		return -fuse.ENOENT, 0
	}
	return 0, 0
}

func (p *probe) Read(path string, buf []byte, off int64, _ uint64) int {
	name := strings.TrimPrefix(path, "/")
	// Capture thread-local libfuse credentials before handing work to Go.
	uid, gid, pid := fuse.Getcontext()
	model.Credentials(uid, gid, uint32(pid), name)
	if name == "delayed" || name == "cancel" {
		log.Printf("event=read_wait name=%s", name)
		delay := 250 * time.Millisecond
		if name == "cancel" {
			delay = 3 * time.Second
		}
		result := make(chan []byte, 1)
		data := p.state.Read(name, off, len(buf))
		go func() { time.Sleep(delay); result <- data }()
		// cgofuse exposes no request cancellation or deferred-reply token.
		data = <-result
		log.Printf("event=worker_reply name=%s", name)
		return copy(buf, data)
	}
	return copy(buf, p.state.Read(name, off, len(buf)))
}

func (p *probe) Write(path string, data []byte, off int64, _ uint64) int {
	return p.state.Write(strings.TrimPrefix(path, "/"), off, data)
}

func (p *probe) Fsync(_ string, _ bool, _ uint64) int { return 0 }
func (p *probe) Flush(_ string, _ uint64) int         { return 0 }
func (p *probe) Release(_ string, _ uint64) int       { return 0 }
