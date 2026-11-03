//go:build linux || freebsd

// Package controlledruntime owns disposable native bootstrap. It provides no
// default Facts, guest attestor, source verifier, recovery or publication policy.
package controlledruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"golang.org/x/sys/unix"
)

const maxExecutable = 64 << 20

// Supervisor is the exclusive process, connection and issuance owner. Close
// fences admission but never deletes resources or retires endpoint obligations.
type Supervisor struct {
	mu             sync.Mutex
	root           string
	directory      *os.File
	lock           *os.File
	child          *exporter
	mount          *nativeMount
	record         issuance
	closed         bool
	controlStarted bool
}

type issuance struct {
	ProcessID  fsapi.UUID           `json:"process_id"`
	Version    int                  `json:"version"`
	Stage      string               `json:"stage"`
	Endpoint   guestbridge.Endpoint `json:"endpoint"`
	Controller fsapi.UUID           `json:"controller"`
	Process    string               `json:"process"`
	Mount      string               `json:"mount"`
}

// Create creates a new disposable ownership root exclusively. Reusing an old
// root, missing issuance files or uncertain old ownership requires recovery,
// which this bootstrap intentionally does not implement.
func Create(ctx context.Context, root string) (*Supervisor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.Mkdir(root, 0700); err != nil {
		return nil, err
	}
	s, err := openOwner(root, true)
	if err != nil {
		return nil, err
	}
	s.record = issuance{Version: 1, Stage: "created"}
	if err = s.protect(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Open acquires an existing root for inspection only. Issued or failed roots
// cannot start a replacement exporter or claim a new controller.
func Open(ctx context.Context, root string) (*Supervisor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, err := openOwner(root, false)
	if err != nil {
		return nil, err
	}
	f, err := ownedFile(s.directory, "issuance.json", unix.O_RDONLY, 0)
	if err == nil {
		dec := json.NewDecoder(io.LimitReader(f, 4097))
		dec.DisallowUnknownFields()
		err = dec.Decode(&s.record)
		if err == nil && (s.record.Version != 1 || s.record.Stage == "") {
			err = fsapi.Stale
		}
		if err == nil {
			var extra any
			if dec.Decode(&extra) != io.EOF {
				err = fsapi.Stale
			}
		}
		_ = f.Close()
	}
	if err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("ownership ledger: %w", err)
	}
	return s, nil
}

// Close releases the owner lock only after admission is fenced. It does not kill
// a child, unmount a connection or remove a failed bootstrap's evidence.
func (s *Supervisor) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.child != nil {
		_ = s.child.control.Close()
	}
	var err error
	if s.lock != nil {
		err = s.lock.Close()
		s.lock = nil
	}
	if s.directory != nil {
		if e := s.directory.Close(); err == nil {
			err = e
		}
		s.directory = nil
	}
	return err
}

func openOwner(root string, create bool) (*Supervisor, error) {
	fd, err := unix.Open(root, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), root)
	if err = checkOwned(dir, true); err != nil {
		_ = dir.Close()
		return nil, err
	}
	flags := unix.O_RDWR
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
	}
	lock, err := ownedFile(dir, "owner.lock", flags, 0600)
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		_ = dir.Close()
		return nil, err
	}
	return &Supervisor{root: root, directory: dir, lock: lock}, nil
}

func ownedFile(dir *os.File, name string, flags int, mode uint32) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_CLOEXEC, mode)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), filepath.Join(dir.Name(), name))
	if err = checkOwned(f, false); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func checkOwned(f *os.File, directory bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	if st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return fsapi.Permission
	}
	kind := uint32(unix.S_IFREG)
	if directory {
		kind = unix.S_IFDIR
	}
	if uint32(st.Mode)&unix.S_IFMT != kind || (!directory && st.Nlink != 1) {
		return fsapi.Permission
	}
	return nil
}

func (s *Supervisor) protect() error {
	b, err := json.Marshal(s.record)
	if err != nil {
		return err
	}
	f, err := ownedFile(s.directory, "issuance.next", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = unix.Renameat(int(s.directory.Fd()), "issuance.next", int(s.directory.Fd()), "issuance.json")
	}
	if err == nil {
		err = s.directory.Sync()
	}
	return err
}
