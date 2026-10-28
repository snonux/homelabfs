package daemoncli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/vfs"
	"golang.org/x/sys/unix"
)

type localService struct {
	service *fsservice.Service
	roots   []*vfs.OS
	lock    *os.File
	faults  *testFaults
}

type readyMessage struct {
	Profile           string `json:"profile"`
	Mountpoint        string `json:"mountpoint"`
	VolumeID          string `json:"volume_id"`
	Generation        string `json:"generation"`
	Session           string `json:"session"`
	NormalPublication bool   `json:"normal_publication"`
}

func openLocal(ctx context.Context, c Config) (_ *localService, retErr error) {
	v, err := inspectVolume(c)
	if err != nil {
		return nil, err
	}
	local := &localService{faults: &testFaults{}}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, local.close())
		}
	}()
	fd, err := unix.Open(filepath.Join(c.VolumeDir, "volume.json"), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	local.lock = os.NewFile(uintptr(fd), "volume lifecycle lock")
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("exclusive volume lifecycle: %w", err)
	}
	fc, err := serviceConfig(v)
	if err != nil {
		return nil, err
	}
	workspace, err := freshID()
	if err != nil {
		return nil, err
	}
	base := filepath.Join(c.VolumeDir, "workspaces", workspace.String())
	if err := createPrivate(base); err != nil {
		return nil, err
	}
	for _, name := range []string{"data", "journal", "progress"} {
		if err := createPrivate(filepath.Join(base, name)); err != nil {
			return nil, err
		}
	}
	paths := []string{filepath.Join(c.VolumeDir, "source"), filepath.Join(c.VolumeDir, "reservations"), filepath.Join(base, "data"), filepath.Join(base, "journal"), filepath.Join(base, "progress")}
	for _, name := range paths {
		root, err := vfs.NewOS(ctx, name)
		if err != nil {
			return nil, err
		}
		local.roots = append(local.roots, root)
	}
	fc.Source, fc.Reservations = local.roots[0], local.roots[1]
	fc.Workspace = fsservice.Workspace{ID: workspace, Data: local.roots[2], Journal: local.roots[3], Progress: local.roots[4]}
	if c.EnableTestHooks {
		fc.Segment.Failpoint = local.faults.journal
	}
	local.service, err = fsservice.New(ctx, fc)
	if err != nil {
		return nil, fmt.Errorf("recover genesis into retained workspace %s: %w", base, err)
	}
	return local, nil
}

func (l *localService) close() error {
	var err error
	if l.service != nil {
		err = l.service.Close()
		l.service = nil
	}
	for i := len(l.roots) - 1; i >= 0; i-- {
		err = errors.Join(err, l.roots[i].Close())
	}
	l.roots = nil
	if l.lock != nil {
		err = errors.Join(err, l.lock.Close())
		l.lock = nil
	}
	return err
}

func serve(ctx context.Context, c Config, selfTest bool, output io.Writer, logger *slog.Logger) (retErr error) {
	if err := privateDir(c.Mountpoint); err != nil {
		return err
	}
	entries, err := os.ReadDir(c.Mountpoint)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("mountpoint must be empty")
	}
	credentials, err := newSelfCredentials()
	if err != nil {
		return err
	}
	local, err := openLocal(ctx, c)
	if err != nil {
		return err
	}
	lifetime := &mountLifetime{local: local}
	defer func() { retErr = errors.Join(retErr, lifetime.close()) }()
	administration, err := startAdmin(ctx, c, local)
	if err != nil {
		return err
	}
	lifetime.admin = administration
	a, err := fuseadapter.New(fuseadapter.Config{Service: local.service, Validation: local.service.FrontendInfo().Validation, Credentials: credentials})
	if err != nil {
		return err
	}
	// The native adapter may retain admission even when Mount returns an error.
	lifetime.retained = true
	m, err := a.Mount(c.Mountpoint)
	if err != nil {
		local.service.BlockAdmission("native-mount-uncertain")
		return fmt.Errorf("mount failed; resources and admin status retained at %s until process exit/inspection: %w", c.VolumeDir, err)
	}
	defer func() {
		if err := lifetime.unmount(m.Unmount); err != nil {
			logger.Error("normal unmount failed; preserve mount and volume for inspection", "mountpoint", c.Mountpoint, "volume_dir", c.VolumeDir, "error", err)
			retErr = errors.Join(retErr, err)
			return
		}
		logger.Info("validation mount normally unmounted", "mountpoint", c.Mountpoint)
	}()
	if err := fuseadapter.CheckMount(c.Mountpoint, a.Identity()); err != nil {
		return fmt.Errorf("mounted identity check failed: %w", err)
	}
	logger.Info("disposable self-client mount ready", "mountpoint", c.Mountpoint, "volume", c.VolumeID, "normal_publication", false, "mmap", false)
	identity := a.Identity()
	if err := json.NewEncoder(output).Encode(readyMessage{Profile: validationName, Mountpoint: c.Mountpoint, VolumeID: identity.Volume.String(), Generation: identity.Generation.String(), Session: identity.Session.String()}); err != nil {
		return err
	}
	if selfTest {
		if err := exerciseClient(c.Mountpoint); err != nil {
			return fmt.Errorf("self-client I/O (resources preserved): %w", err)
		}
		logger.Info("bounded self-client I/O passed", "bytes", len("synthetic HomelabFS CLI validation\n"))
		return nil
	}
	<-ctx.Done()
	logger.Info("stopping validation mount", "reason", ctx.Err())
	return nil
}

func exerciseClient(mountpoint string) (retErr error) {
	id, err := freshID()
	if err != nil {
		return err
	}
	name := filepath.Join(mountpoint, "cli-validation-"+id.String())
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	data := []byte("synthetic HomelabFS CLI validation\n")
	if n, err := f.Write(data); err != nil || n != len(data) {
		return errors.Join(err, io.ErrShortWrite)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	got := make([]byte, len(data))
	n, err := f.ReadAt(got, 0)
	if err != nil {
		return fmt.Errorf("readback: %w", err)
	}
	if n != len(data) || string(got) != string(data) {
		return fmt.Errorf("readback mismatch")
	}
	if err := os.Rename(name, name+"-verified"); err != nil {
		return err
	}
	return syncDir(mountpoint)
}

// Prevent accidentally replacing the service with a filesystem that bypasses
// its admission, permission and journal gates.
var _ fsapi.Filesystem = (*fsservice.Service)(nil)
