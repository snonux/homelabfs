// A disposable compatibility probe, not a HomelabFS implementation.
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

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

func main() {
	debug := flag.Bool("debug", false, "log FUSE requests")
	flag.Parse()
	if flag.NArg() != 2 {
		log.Fatal("usage: fuse-passthrough [-debug] MOUNTPOINT BACKING_DIRECTORY")
	}
	mountpoint, err := filepath.EvalSymlinks(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	backing, err := filepath.EvalSymlinks(flag.Arg(1))
	if err != nil {
		log.Fatal(err)
	}
	mountpoint, err = filepath.Abs(mountpoint)
	if err != nil {
		log.Fatal(err)
	}
	backing, err = filepath.Abs(backing)
	if err != nil {
		log.Fatal(err)
	}
	// Refuse overlap: the backing tree must not contain its own FUSE mount.
	for _, pair := range [][2]string{{mountpoint, backing}, {backing, mountpoint}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			log.Fatal(err)
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			log.Fatal("mountpoint and backing directory must be disjoint")
		}
	}
	entries, err := os.ReadDir(mountpoint)
	if err != nil {
		log.Fatal(err)
	}
	if len(entries) != 0 {
		log.Fatal("mountpoint must be empty")
	}
	root, err := fs.NewLoopbackRoot(backing)
	if err != nil {
		log.Fatal(err)
	}
	zero := time.Duration(0)
	server, err := fs.Mount(mountpoint, root, &fs.Options{
		AttrTimeout: &zero, EntryTimeout: &zero, NegativeTimeout: &zero,
		NullPermissions: true,
		MountOptions: fuse.MountOptions{
			AllowOther: true, Options: []string{"default_permissions"},
			Debug: *debug, FsName: "homelabfs-poc", Name: "homelabfs-poc",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("READY pid=%d mount=%s backing=%s", os.Getpid(), mountpoint, backing)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		if err := server.Unmount(); err != nil {
			log.Printf("unmount failed: %v", err)
		}
	}()
	server.Wait()
}
