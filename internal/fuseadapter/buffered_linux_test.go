//go:build linux

package fuseadapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/vfs"
	"golang.org/x/sys/unix"
)

type fixtureCommand struct {
	Op         string
	Mount      string
	Identity   MountIdentity
	Name, Data string
	Size       int64
}
type fixtureReply struct{ Data, Error string }
type fixtureChild struct {
	process *Participant
	input   *io.PipeWriter
	output  *json.Decoder
	reader  *io.PipeReader
}

func startFixtureChild(t *testing.T, p *Participants, credential *syscall.Credential) *fixtureChild {
	return startFixtureExecutable(t, p, credential, "")
}

func startFixtureExecutable(t *testing.T, p *Participants, credential *syscall.Credential, executable string) *fixtureChild {
	t.Helper()
	in, write := io.Pipe()
	read, out := io.Pipe()
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			t.Fatal(err)
		}
	}
	child, err := p.Start(t.Context(), ParticipantSpec{Executable: executable, Args: []string{"-test.run=^TestControlledParticipantHelper$"}, Env: append(os.Environ(), "HOMELABFS_PARTICIPANT_HELPER=1"), Credentials: credential, Stdin: in, Stdout: out, Stderr: os.Stderr})
	if err != nil {
		_ = in.Close()
		_ = write.Close()
		_ = read.Close()
		_ = out.Close()
		t.Fatal(err)
	}
	result := &fixtureChild{process: child, input: write, output: json.NewDecoder(read), reader: read}
	t.Cleanup(func() {
		_ = write.Close()
		_ = child.Signal(syscall.SIGTERM)
		_ = child.Wait()
		_ = in.Close()
		_ = out.Close()
		_ = read.Close()
	})
	return result
}

func (c *fixtureChild) send(t *testing.T, command fixtureCommand) {
	t.Helper()
	if err := json.NewEncoder(c.input).Encode(command); err != nil {
		t.Fatal(err)
	}
}
func (c *fixtureChild) receive(t *testing.T) fixtureReply {
	t.Helper()
	result := make(chan fixtureReply, 1)
	go func() {
		var reply fixtureReply
		if err := c.output.Decode(&reply); err != nil {
			reply.Error = err.Error()
		}
		result <- reply
	}()
	select {
	case reply := <-result:
		return reply
	case <-time.After(5 * time.Second):
		t.Fatal("bounded participant response timed out")
		return fixtureReply{}
	}
}
func (c *fixtureChild) call(t *testing.T, command fixtureCommand) fixtureReply {
	t.Helper()
	c.send(t, command)
	reply := c.receive(t)
	if reply.Error != "" {
		t.Fatalf("helper %s: %s", command.Op, reply.Error)
	}
	if command.Op == "exit" {
		if err := c.input.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return reply
}

// TestControlledParticipantHelper is a trusted fixed-credential executable. It
// waits on its private inherited pipe, opens only the exact verified mount and
// uses openat thereafter. It never receives backing roots or executes programs.
func TestControlledParticipantHelper(t *testing.T) {
	if os.Getenv("HOMELABFS_PARTICIPANT_HELPER") != "1" {
		t.Skip("trusted child entry point")
	}
	decoder := json.NewDecoder(bufio.NewReader(os.Stdin))
	encoder := json.NewEncoder(os.Stdout)
	var root *os.File
	fd, duplicate := -1, -1
	var mapping []byte
	defer func() {
		if mapping != nil {
			_ = unix.Munmap(mapping)
		}
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		if duplicate >= 0 {
			_ = unix.Close(duplicate)
		}
		if root != nil {
			_ = root.Close()
		}
	}()
	for {
		var command fixtureCommand
		if err := decoder.Decode(&command); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			t.Fatal(err)
		}
		reply := fixtureReply{}
		if command.Size < 0 || command.Size > 4096 || len(command.Data) > 4096 || command.Op == "map-write" && command.Size+int64(len(command.Data)) > 4096 {
			t.Fatal("fixture request exceeds one-page synthetic bound")
		}
		var err error
		switch command.Op {
		case "ready": // private helper synchronization, not daemon readiness
		case "bind":
			root, err = OpenMount(command.Mount, command.Identity)
		case "open", "open-read", "create":
			if root == nil {
				err = fsapi.Permission
				break
			}
			if command.Name != "file" && command.Name != "alias" && command.Name != "owned" && command.Name != "groupfile" {
				err = fsapi.Invalid
				break
			}
			flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW
			if command.Op == "open-read" {
				flags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
			}
			if command.Op == "create" {
				flags |= unix.O_CREAT | unix.O_EXCL
				unix.Umask(0007)
			}
			fd, err = unix.Openat(int(root.Fd()), command.Name, flags, 0666)
		case "mmap":
			mapping, err = unix.Mmap(fd, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		case "map-write":
			copy(mapping[command.Size:], []byte(command.Data))
		case "map-read":
			reply.Data = string(mapping[:command.Size])
		case "msync":
			err = unix.Msync(mapping, unix.MS_SYNC)
		case "munmap":
			err = unix.Munmap(mapping)
			mapping = nil
		case "read":
			data := make([]byte, command.Size)
			var n int
			n, err = unix.Pread(fd, data, 0)
			reply.Data = string(data[:n])
		case "unlink-owned":
			if root == nil {
				err = fsapi.Permission
			} else {
				err = unix.Unlinkat(int(root.Fd()), "owned", 0)
			}
		case "write":
			_, err = unix.Pwrite(fd, []byte(command.Data), 0)
		case "truncate":
			err = unix.Ftruncate(fd, command.Size)
		case "dup":
			duplicate, err = unix.Dup(fd)
		case "close-dup":
			err = unix.Close(duplicate)
			duplicate = -1
		case "close":
			err = unix.Close(fd)
			fd = -1
		case "fcntl-try", "fcntl-wait", "fcntl-unlock":
			lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
			operation := unix.F_SETLK
			if command.Op == "fcntl-wait" {
				operation = unix.F_SETLKW
			}
			if command.Op == "fcntl-unlock" {
				lock.Type = unix.F_UNLCK
			}
			err = unix.FcntlFlock(uintptr(fd), operation, &lock)
		case "flock-try":
			err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		case "flock-unlock":
			err = unix.Flock(fd, unix.LOCK_UN)
		case "exit":
			if fd >= 0 {
				err = unix.Close(fd)
				fd = -1
			}
			if duplicate >= 0 {
				err = errors.Join(err, unix.Close(duplicate))
				duplicate = -1
			}
			if root != nil {
				err = errors.Join(err, root.Close())
				root = nil
			}
			if err != nil {
				reply.Error = err.Error()
			}
			if err := encoder.Encode(reply); err != nil {
				t.Fatal(err)
			}
			os.Exit(0)
		default:
			err = fsapi.Unsupported
		}
		if err != nil {
			reply.Error = err.Error()
		}
		if err := encoder.Encode(reply); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLinuxBufferedIntegration(t *testing.T) {
	requireLocalFuse(t)
	base, err := os.MkdirTemp("", "homelabfs-buffered-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("run-owned resources: %s", base)
	clean := false
	t.Cleanup(func() {
		if clean && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved resources: %s", base)
		}
	})
	mountpoint := filepath.Join(base, "mount")
	if err := os.Mkdir(mountpoint, 0700); err != nil {
		t.Fatal(err)
	}
	config := serviceConfiguration(t, base, nativeRequestBytes)
	service := startService(t, config)
	participants, a := newBuffered(t, service, false)
	var m *Mount
	t.Cleanup(func() {
		if m != nil {
			if err := m.Unmount(); err != nil {
				t.Errorf("normal unmount; resources retained: %v", err)
			}
		}
	})
	child := startFixtureChild(t, participants, nil)
	interrupted := startFixtureChild(t, participants, nil)
	fallback := startFixtureChild(t, participants, nil)
	// A real kernel cache write must carry an issued writable FH, not caller IDs.
	cacheWrites := make(chan fsapi.Cred, 16)
	waits := make(chan struct{}, 4)
	cancels := make(chan struct{}, 4)
	actual := a.service
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		err := actual.Dispatch(ctx, r, c)
		if err == nil {
			switch c.Operation {
			case fsapi.Write:
				select {
				case cacheWrites <- r.Credentials():
				default:
				}
			case fsapi.WaitLock:
				waits <- struct{}{}
			case fsapi.CancelLock:
				cancels <- struct{}{}
			}
		}
		return err
	})
	m, err = a.Mount(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := OpenMount(mountpoint, a.Identity())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if pinned != nil {
			if err := pinned.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	fd, err := unix.Openat(int(pinned.Fd()), "file", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	file := os.NewFile(uintptr(fd), "synthetic mapped file")
	defer func() {
		if file != nil {
			if err := file.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	if err := file.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	if err := unix.Linkat(int(pinned.Fd()), "file", int(pinned.Fd()), "alias", 0); err != nil {
		t.Fatal(err)
	}
	child.call(t, fixtureCommand{Op: "bind", Mount: mountpoint, Identity: a.Identity()})
	child.call(t, fixtureCommand{Op: "open", Name: "alias"})
	child.call(t, fixtureCommand{Op: "mmap"})
	child.call(t, fixtureCommand{Op: "map-write", Data: "mapped synthetic bytes"})
	// Dirty stores have no journal promise. MS_SYNC must issue cache writes.
	child.call(t, fixtureCommand{Op: "msync"})
	select {
	case cred := <-cacheWrites:
		if cred.UID != uint32(os.Geteuid()) || cred.PID == 0 {
			t.Fatal(cred)
		}
	case <-time.After(time.Second):
		t.Fatal("msync did not send a service write")
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	var got [22]byte
	if _, err := file.ReadAt(got[:], 0); err != nil || string(got[:]) != "mapped synthetic bytes" {
		t.Fatal(string(got[:]), err)
	}
	if _, err := file.WriteAt([]byte("alias"), 0); err != nil {
		t.Fatal(err)
	}
	if got := child.call(t, fixtureCommand{Op: "map-read", Size: 5}).Data; got != "alias" {
		t.Fatalf("mapping missed alias write: %q", got)
	}
	child.call(t, fixtureCommand{Op: "map-write", Size: 5, Data: "-map"})
	child.call(t, fixtureCommand{Op: "msync"})
	if got := child.call(t, fixtureCommand{Op: "read", Size: 9}).Data; got != "alias-map" {
		t.Fatal(got)
	}
	child.call(t, fixtureCommand{Op: "truncate", Size: 4})
	child.call(t, fixtureCommand{Op: "truncate", Size: 4096})
	if got := child.call(t, fixtureCommand{Op: "read", Size: 9}).Data; got != "alia\x00\x00\x00\x00\x00" {
		t.Fatalf("truncate read %q", got)
	}
	if got := child.call(t, fixtureCommand{Op: "map-read", Size: 9}).Data; got != "alia\x00\x00\x00\x00\x00" {
		t.Fatalf("truncate mapping %q", got)
	}
	exerciseProcessLocks(t, fd, child, interrupted, mountpoint, a.Identity(), waits, cancels)
	// The child keeps the mapping after closing its source description.
	child.call(t, fixtureCommand{Op: "close"})
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	file = nil
	child.call(t, fixtureCommand{Op: "map-write", Data: "replayed!"})
	child.call(t, fixtureCommand{Op: "msync"})
	child.call(t, fixtureCommand{Op: "munmap"})
	syncfd, err := unix.Openat(int(pinned.Fd()), "file", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Fsync(syncfd); err != nil {
		_ = unix.Close(syncfd)
		t.Fatal(err)
	}
	if err := unix.Close(syncfd); err != nil {
		t.Fatal(err)
	}
	child.call(t, fixtureCommand{Op: "exit"})
	if err := child.process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := pinned.Close(); err != nil {
		t.Fatal(err)
	}
	pinned = nil
	if err := m.Unmount(); err != nil {
		t.Fatal(err)
	}
	fallback.send(t, fixtureCommand{Op: "bind", Mount: mountpoint, Identity: a.Identity()})
	if reply := fallback.receive(t); reply.Error == "" {
		t.Fatal("helper accepted empty fallback")
	}
	fallback.send(t, fixtureCommand{Op: "create", Name: "owned"})
	if reply := fallback.receive(t); reply.Error == "" {
		t.Fatal("helper wrote without a verified mount")
	}
	fallback.call(t, fixtureCommand{Op: "exit"})
	if err := fallback.process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := participants.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.Close(); err != nil {
		t.Fatal(err)
	}
	// Reconstruct into distinct private roots with a fresh service generation.
	config.Workspace = fsservice.Workspace{ID: testID(12), Data: replayRoot(t, base, "replay-data"), Journal: replayRoot(t, base, "replay-journal"), Progress: replayRoot(t, base, "replay-progress")}
	replay := startService(t, config)
	p2, a2, m2 := mountBuffered(t, replay, mountpoint, false)
	if a2.Identity().Generation == a.Identity().Generation {
		t.Fatal("reused generation")
	}
	if err := CheckMount(mountpoint, a.Identity()); err == nil {
		t.Fatal("old identity survived replay")
	}
	root, err := OpenMount(mountpoint, a2.Identity())
	if err != nil {
		t.Fatal(err)
	}
	readfd, err := unix.Openat(int(root.Fd()), "alias", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	var data [9]byte
	n, readErr := unix.Read(readfd, data[:])
	if err := errors.Join(readErr, unix.Close(readfd), root.Close()); err != nil {
		t.Fatal(err)
	}
	if n != 9 || string(data[:]) != "replayed!" {
		t.Fatalf("fresh replay: %q, %d", data, n)
	}
	if err := m2.Unmount(); err != nil {
		t.Fatal(err)
	}
	if err := p2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CheckMount(mountpoint, a2.Identity()); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("unmounted fallback: %v", err)
	}
	entries, err := os.ReadDir(mountpoint)
	if err != nil || len(entries) != 0 {
		t.Fatal("fallback received writes", entries, err)
	}
	clean = true
}

func exerciseProcessLocks(t *testing.T, fd int, child, interrupted *fixtureChild, mountpoint string, id MountIdentity, waits, cancels <-chan struct{}) {
	t.Helper()
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: unix.SEEK_SET}
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	child.send(t, fixtureCommand{Op: "fcntl-try"})
	if reply := child.receive(t); reply.Error != unix.EAGAIN.Error() {
		t.Fatalf("process fcntl conflict: %+v", reply)
	}
	interrupted.call(t, fixtureCommand{Op: "bind", Mount: mountpoint, Identity: id})
	interrupted.call(t, fixtureCommand{Op: "open", Name: "file"})
	interrupted.send(t, fixtureCommand{Op: "fcntl-wait"})
	select {
	case <-waits:
	case <-time.After(time.Second):
		t.Fatal("blocking process lock not admitted")
	}
	if err := interrupted.process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	// Close the parent pipe so exec's stdin copier cannot keep Wait parked.
	if err := interrupted.input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := interrupted.process.Wait(); err == nil {
		t.Fatal("interrupted helper unexpectedly survived SIGTERM")
	}
	select {
	case <-cancels:
	case <-time.After(time.Second):
		t.Fatal("kernel did not cancel interrupted lock")
	}
	lock.Type = unix.F_UNLCK
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	child.call(t, fixtureCommand{Op: "fcntl-try"})
	child.call(t, fixtureCommand{Op: "dup"})
	child.call(t, fixtureCommand{Op: "close-dup"})
	lock.Type = unix.F_WRLCK
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		t.Fatalf("close-any-fd did not drop child POSIX lock: %v", err)
	}
	lock.Type = unix.F_UNLCK
	if err := unix.FcntlFlock(uintptr(fd), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	child.call(t, fixtureCommand{Op: "flock-try"})
	child.call(t, fixtureCommand{Op: "dup"})
	child.call(t, fixtureCommand{Op: "close"})
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatalf("flock lost before last duplicate close: %v", err)
	}
	child.call(t, fixtureCommand{Op: "close-dup"})
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatalf("live mapping lost its description flock: %v", err)
	}
	child.call(t, fixtureCommand{Op: "munmap"})
	acquired := make(chan error, 1)
	go func() { acquired <- unix.Flock(fd, unix.LOCK_EX) }()
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("last mapping release retained flock")
	}
	if err := unix.Flock(fd, unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	child.call(t, fixtureCommand{Op: "open", Name: "alias"})
	child.call(t, fixtureCommand{Op: "mmap"})
}

func requireLocalFuse(t *testing.T) {
	t.Helper()
	if os.Getenv("HOMELABFS_FUSE_INTEGRATION") != "1" {
		t.Skip("requires explicit HOMELABFS_FUSE_INTEGRATION=1 authorization")
	}
	device, err := os.OpenFile("/dev/fuse", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("/dev/fuse unavailable: %v", err)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
}
func newBuffered(t *testing.T, service *fsservice.Service, multiuser bool) (*Participants, *Adapter) {
	t.Helper()
	p, err := NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	a, err := New(Config{Service: service, Validation: service.FrontendInfo().Validation, Credentials: p, BufferedValidation: true, MultiuserValidation: multiuser})
	if err != nil {
		t.Fatal(err)
	}
	return p, a
}
func mountBuffered(t *testing.T, service *fsservice.Service, path string, multiuser bool) (*Participants, *Adapter, *Mount) {
	t.Helper()
	p, a := newBuffered(t, service, multiuser)
	m, err := a.Mount(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Unmount(); err != nil {
			t.Errorf("normal unmount; resources retained: %v", err)
		}
	})
	return p, a, m
}
func replayRoot(t *testing.T, base, name string) *vfs.OS {
	t.Helper()
	path := filepath.Join(base, name)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := vfs.NewOS(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	})
	return root
}

func TestLinuxMultiuserBufferedIntegration(t *testing.T) {
	requireLocalFuse(t)
	if os.Geteuid() != 0 {
		t.Skip("root-only synthetic fixed-UID/GID/groups allow_other fixture; no system configuration changes")
	}
	base, err := os.MkdirTemp("/tmp", "homelabfs-multiuser-validation-")
	if err != nil {
		t.Fatal(err)
	}
	clean := false
	t.Cleanup(func() {
		if clean && !t.Failed() {
			if err := os.RemoveAll(base); err != nil {
				t.Error(err)
			}
		} else {
			t.Logf("preserved resources: %s", base)
		}
	})
	// Ancestor traversal admits only this synthetic mount/helper. Every backing
	// root beneath base remains private 0700, never passed to a child.
	if err := os.Chmod(base, 0711); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(base, "helper")
	if err := os.WriteFile(helper, binary, 0555); err != nil {
		t.Fatal(err)
	}
	mountpoint := filepath.Join(base, "mount")
	if err := os.Mkdir(mountpoint, 0755); err != nil {
		t.Fatal(err)
	}
	service := serviceWithIO(t, base, nativeRequestBytes)
	p, a := newBuffered(t, service, true)
	var m *Mount
	t.Cleanup(func() {
		if m != nil {
			if err := m.Unmount(); err != nil {
				t.Error(err)
			}
		}
	})
	member := startFixtureExecutable(t, p, &syscall.Credential{Uid: 20001, Gid: 20001, Groups: []uint32{20002}}, helper)
	outsider := startFixtureExecutable(t, p, &syscall.Credential{Uid: 20003, Gid: 20003, Groups: []uint32{}}, helper)
	// Authenticate the expected exact credentials before native publication.
	member.call(t, fixtureCommand{Op: "ready"})
	outsider.call(t, fixtureCommand{Op: "ready"})
	m, err = a.Mount(mountpoint)
	if err != nil {
		t.Fatal(err)
	}
	member.call(t, fixtureCommand{Op: "bind", Mount: mountpoint, Identity: a.Identity()})
	outsider.call(t, fixtureCommand{Op: "bind", Mount: mountpoint, Identity: a.Identity()})
	file, err := os.OpenFile(filepath.Join(mountpoint, "groupfile"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0640)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("synthetic group access"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Chown(0, 20002); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	member.call(t, fixtureCommand{Op: "open-read", Name: "groupfile"})
	if got := member.call(t, fixtureCommand{Op: "read", Size: 22}).Data; got != "synthetic group access" {
		t.Fatal(got)
	}
	member.call(t, fixtureCommand{Op: "close"})
	outsider.send(t, fixtureCommand{Op: "open-read", Name: "groupfile"})
	if reply := outsider.receive(t); reply.Error != unix.EACCES.Error() {
		t.Fatalf("nonmember permission result: %+v", reply)
	}
	member.call(t, fixtureCommand{Op: "create", Name: "owned"})
	member.call(t, fixtureCommand{Op: "write", Data: "owned by synthetic UID 20001"})
	member.call(t, fixtureCommand{Op: "close"})
	st, err := os.Stat(filepath.Join(mountpoint, "owned"))
	if err != nil || st.Mode().Perm() != 0660 {
		t.Fatal(st, err)
	}
	if err := os.Chmod(mountpoint, os.ModeSticky|0777); err != nil {
		t.Fatal(err)
	}
	outsider.send(t, fixtureCommand{Op: "unlink-owned"})
	if reply := outsider.receive(t); reply.Error != unix.EPERM.Error() && reply.Error != unix.EACCES.Error() {
		t.Fatalf("sticky permission result: %+v", reply)
	}
	outsider.send(t, fixtureCommand{Op: "open-read", Name: "owned"})
	if reply := outsider.receive(t); reply.Error != unix.EACCES.Error() {
		t.Fatalf("cached permission denial: %+v", reply)
	}
	if err := os.Chmod(filepath.Join(mountpoint, "owned"), 0644); err != nil {
		t.Fatal(err)
	}
	outsider.call(t, fixtureCommand{Op: "open-read", Name: "owned"})
	outsider.call(t, fixtureCommand{Op: "close"})
	member.call(t, fixtureCommand{Op: "exit"})
	outsider.call(t, fixtureCommand{Op: "exit"})
	if err := member.process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := outsider.process.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := m.Unmount(); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	clean = true
}
