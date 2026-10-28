package cluster

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
	"golang.org/x/sys/unix"
)

// Identity identifies one fresh local daemon lifetime. It is neither a current
// peer session nor proof that an earlier daemon/owner has been fenced.
type Identity struct {
	Cluster, Node, Incarnation fsapi.UUID
}

// Lifetime holds exclusive control-directory admission until Close. It must not
// be copied. All sockets and services using its identity must stop before Close.
// No API resumes a persisted incarnation or treats restart as tail continuity.
type Lifetime struct {
	mu       sync.Mutex
	identity Identity
	lock     *os.File
}

type nodeManifest struct {
	FormatVersion uint64 `json:"format_version"`
	Node          string `json:"node_id"`
}

type clusterManifest struct {
	FormatVersion uint64 `json:"format_version"`
	Cluster       string `json:"cluster_id"`
	Node          string `json:"node_id"`
}

type incarnationManifest struct {
	FormatVersion uint64 `json:"format_version"`
	Cluster       string `json:"cluster_id"`
	Node          string `json:"node_id"`
	Incarnation   string `json:"incarnation_id"`
}

type lifetimeDisk interface {
	vfs.FS
	vfs.Discovery
}

// Start locks an existing node-init control directory, durably binds its cluster,
// then persists a fresh exclusive incarnation record before returning. The
// caller first validates its registry/local-node admission, and starts each new
// daemon in RECOVERING. Failed attempts preserve files and never reuse an ID.
// No volume, mount, socket, protocol or serving authority is created here.
func Start(ctx context.Context, controlDir string, cluster, node fsapi.UUID) (_ *Lifetime, retErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !cluster.Valid() || !node.Valid() || cluster == node {
		return nil, fmt.Errorf("invalid cluster/node identity")
	}
	if err := checkControl(controlDir); err != nil {
		return nil, err
	}
	lock, err := openPrivate(filepath.Join(controlDir, "node.json"))
	if err != nil {
		return nil, err
	}
	l := &Lifetime{lock: lock}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, l.Close())
		}
	}()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("exclusive node lifecycle: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(lock, 4097))
	if err != nil {
		return nil, err
	}
	var n nodeManifest
	if len(data) > 4096 {
		return nil, fmt.Errorf("oversized node identity")
	}
	if err := decodeStrict(data, &n); err != nil {
		return nil, fmt.Errorf("node identity: %w", err)
	}
	if n.FormatVersion != 1 || n.Node != node.String() {
		return nil, fmt.Errorf("node identity mismatch")
	}
	disk, err := vfs.NewOS(ctx, controlDir)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, disk.Close()) }()
	incarnation, err := persistLifetime(ctx, disk, cluster, node, freshUUID)
	if err != nil {
		return nil, fmt.Errorf("persist fresh node incarnation (resources preserved): %w", err)
	}
	l.identity = Identity{Cluster: cluster, Node: node, Incarnation: incarnation}
	return l, nil
}

// Identity returns this lifetime's immutable identity. Its use must remain
// bounded by the caller's held Lifetime; a copied value cannot prove liveness.
func (l *Lifetime) Identity() Identity { return l.identity }

// Close releases local exclusivity. Call only after draining all consumers.
// Close is idempotent and does not erase or rewrite the incarnation history.
func (l *Lifetime) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lock == nil {
		return nil
	}
	err := l.lock.Close()
	l.lock = nil
	return err
}

func persistLifetime(ctx context.Context, disk lifetimeDisk, cluster, node fsapi.UUID, allocate func() (fsapi.UUID, error)) (fsapi.UUID, error) {
	var zero fsapi.UUID
	if err := bindCluster(ctx, disk, cluster, node); err != nil {
		return zero, err
	}
	if err := disk.Mkdir(ctx, "incarnations", 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return zero, err
	}
	// Revalidate even an existing directory before writing. vfs.Open rejects
	// symlink traversal; Start's private administrative tree excludes replacement.
	st, err := disk.Stat(ctx, "incarnations")
	if err != nil {
		return zero, err
	}
	if st.Mode != fs.ModeDir|0o700 || st.UID != uint32(os.Geteuid()) {
		return zero, fmt.Errorf("incarnations must be a private daemon-owned directory")
	}
	if err := disk.SyncDir(ctx, "."); err != nil {
		return zero, err
	}
	id, err := allocate()
	if err != nil {
		return zero, err
	}
	if !id.Valid() || id == cluster || id == node {
		return zero, fmt.Errorf("invalid allocated incarnation")
	}
	m := incarnationManifest{FormatVersion: 1, Cluster: cluster.String(), Node: node.String(), Incarnation: id.String()}
	// O_EXCL prevents reusing any prior or uncertain attempt, even with a repeated
	// random UUID. A collision fails; it never overwrites or resumes that file.
	if err := writeNew(ctx, disk, "incarnations/"+id.String()+".json", m); err != nil {
		return zero, err
	}
	if err := disk.SyncDir(ctx, "incarnations"); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return id, nil
}

func bindCluster(ctx context.Context, disk vfs.FS, cluster, node fsapi.UUID) error {
	f, err := disk.Open(ctx, "cluster.json", os.O_RDONLY, 0)
	if errors.Is(err, fs.ErrNotExist) {
		m := clusterManifest{FormatVersion: 1, Cluster: cluster.String(), Node: node.String()}
		if err := writeNew(ctx, disk, "cluster.json", m); err != nil {
			return err
		}
		return disk.SyncDir(ctx, ".")
	}
	if err != nil {
		return err
	}
	var st vfs.Info
	if statter, ok := f.(vfs.Statter); ok {
		st, err = statter.Stat(ctx)
	} else {
		err = fmt.Errorf("cluster manifest requires descriptor metadata")
	}
	if err == nil && (st.Mode != 0o600 || st.UID != uint32(os.Geteuid()) || st.Links != 1 || st.Size > 4096) {
		err = fmt.Errorf("invalid private cluster manifest")
	}
	var raw [4097]byte
	n := 0
	if err == nil {
		n, err = f.ReadAt(ctx, raw[:], 0)
		if errors.Is(err, io.EOF) {
			err = nil
		}
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	var m clusterManifest
	if n > 4096 {
		return fmt.Errorf("oversized cluster manifest")
	}
	if err := decodeStrict(raw[:n], &m); err != nil {
		return err
	}
	if m.FormatVersion != 1 || m.Cluster != cluster.String() || m.Node != node.String() {
		return fmt.Errorf("control directory belongs to another cluster/node")
	}
	// An earlier failed attempt may have left a valid but unsynced binding.
	// Resync its exact bytes and directory before creating another incarnation.
	f, err = disk.Open(ctx, "cluster.json", os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	err = errors.Join(f.Sync(ctx), f.Close())
	if err != nil {
		return err
	}
	return disk.SyncDir(ctx, ".")
}

func writeNew(ctx context.Context, disk vfs.FS, name string, value any) (retErr error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f, err := disk.Open(ctx, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	raw = append(raw, '\n')
	n, err := f.WriteAt(ctx, raw, 0)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	return f.Sync(ctx)
}

func checkControl(dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" {
		return fmt.Errorf("control path must be clean, absolute and below root")
	}
	if err := checkParents(filepath.Join(dir, "node.json")); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Lstat(dir, &st); err != nil {
		return err
	}
	if st.Mode != unix.S_IFDIR|0o700 || st.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("control directory must be mode 0700 and owned by daemon UID")
	}
	return nil
}

func freshUUID() (fsapi.UUID, error) {
	var id fsapi.UUID
	if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
		return id, err
	}
	id[6], id[8] = id[6]&15|64, id[8]&63|128
	return id, nil
}
