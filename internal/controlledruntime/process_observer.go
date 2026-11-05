//go:build linux || freebsd

package controlledruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

type processObserverReport struct {
	Version     string   `json:"version"`
	Platform    string   `json:"platform"`
	UID         uint32   `json:"uid"`
	GID         uint32   `json:"gid"`
	PID         uint32   `json:"pid"`
	Groups      []uint32 `json:"groups"`
	Incarnation string   `json:"incarnation"`
	Executable  string   `json:"executable"`
	SHA256      string   `json:"sha256"`
	TextVnode   uint64   `json:"text_vnode"`
	Publication any      `json:"publication"`
}

// Qualification observes this process and its protected executable only. It
// never opens runtime configuration, creates an owner, mounts, or starts a child.
func qualifyProcessObserver(output io.Writer) error {
	if runtime.GOOS != "freebsd" {
		return fsapi.Unsupported
	}
	identity, err := checkProcessObserver(observeProcess)
	if err != nil {
		return err
	}
	path, err := os.Executable()
	if err != nil {
		return err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG || st.Size < 1 || st.Size > maxExecutable ||
		(st.Uid != 0 && st.Uid != uint32(os.Geteuid())) {
		return fsapi.Permission
	}
	if err = sameExecutable(file, identity); err != nil {
		return fmt.Errorf("protected qualification executable: %w", err)
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, maxExecutable+1))
	if err != nil {
		return err
	}
	if n != st.Size {
		return fsapi.Stale
	}
	after, err := checkProcessObserver(observeProcess)
	if err != nil {
		return err
	}
	if !sameProcessIdentity(identity, after) {
		return fsapi.Stale
	}
	if err = sameExecutable(file, after); err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(processObserverReport{
		Version: "homelabfs-process-observer/1", Platform: runtime.GOOS + "/" + runtime.GOARCH,
		UID: after.UID, GID: after.GID, PID: after.PID, Groups: after.Groups,
		Incarnation: after.Incarnation, Executable: path, SHA256: hex.EncodeToString(hash.Sum(nil)), TextVnode: after.TextVnode,
	})
}

func checkProcessObserver(observe func(int) (processIdentity, error)) (processIdentity, error) {
	identity, err := observe(os.Getpid())
	if err != nil {
		return processIdentity{}, err
	}
	if identity.PID != uint32(os.Getpid()) || identity.UID != uint32(os.Getuid()) ||
		identity.UID != uint32(os.Geteuid()) || identity.GID != uint32(os.Getgid()) ||
		identity.GID != uint32(os.Getegid()) || identity.Incarnation == "" {
		return processIdentity{}, fsapi.Permission
	}
	groups, err := os.Getgroups()
	if err != nil {
		return processIdentity{}, err
	}
	want := make([]uint32, len(groups))
	for i, group := range groups {
		want[i] = uint32(group)
	}
	slices.Sort(want)
	if !slices.Equal(want, identity.Groups) {
		return processIdentity{}, fsapi.Permission
	}
	return identity, nil
}

func sameProcessIdentity(a, b processIdentity) bool {
	return a.PID == b.PID && a.UID == b.UID && a.GID == b.GID &&
		a.Incarnation == b.Incarnation && slices.Equal(a.Groups, b.Groups) &&
		a.Device == b.Device && a.Inode == b.Inode &&
		a.ExecutablePath == b.ExecutablePath && a.TextVnode == b.TextVnode
}
