package daemoncli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

const validationName = "homelabfs-cli-disposable-self-client-v1"

func initNode(ctx context.Context, dir string) (nodeIdentity, error) {
	var n nodeIdentity
	if err := ctx.Err(); err != nil {
		return n, err
	}
	id, err := freshID()
	if err != nil {
		return n, err
	}
	n = nodeIdentity{FormatVersion: formatVersion, NodeID: id.String()}
	if err := createPrivate(dir); err != nil {
		return n, err
	}
	return n, writeNewJSON(filepath.Join(dir, "node.json"), n)
}

func initVolume(ctx context.Context, c Config) (volumeIdentity, error) {
	var v volumeIdentity
	if err := ctx.Err(); err != nil {
		return v, err
	}
	if err := checkNode(c); err != nil {
		return v, err
	}
	v = volumeIdentity{FormatVersion: formatVersion, Profile: validationName, NodeID: c.NodeID, VolumeID: c.VolumeID}
	fields := []*string{&v.Generation, &v.Configuration, &v.RootAllocation, &v.FixturePartner, &v.FixtureAsync, &v.OwnerIncarnation, &v.PartnerIncarnation}
	for _, field := range fields {
		id, err := freshID()
		if err != nil {
			return v, err
		}
		*field = id.String()
	}
	fc, err := serviceConfig(v)
	if err != nil {
		return v, err
	}
	if err := createPrivate(c.VolumeDir); err != nil {
		return v, err
	}
	for _, name := range volumePaths(c) {
		if err := createPrivate(name); err != nil {
			return v, err
		}
	}
	source, err := vfs.NewOS(ctx, sourcePath(c))
	if err != nil {
		return v, err
	}
	defer func() { _ = source.Close() }()
	if err := initializeSource(ctx, source, fc); err != nil {
		return v, err
	}
	// Publish the manifest only after the complete genesis is locally durable.
	// An interrupted attempt remains preserved and cannot be reinitialized.
	return v, writeNewJSON(filepath.Join(c.VolumeDir, "volume.json"), v)
}

func checkNode(c Config) error {
	if err := privateDir(c.ControlDir); err != nil {
		return err
	}
	var n nodeIdentity
	if err := readJSON(filepath.Join(c.ControlDir, "node.json"), &n); err != nil {
		return err
	}
	if n.FormatVersion != formatVersion || n.NodeID != c.NodeID {
		return fmt.Errorf("node identity or format mismatch")
	}
	return nil
}

func inspectVolume(c Config) (volumeIdentity, error) {
	var v volumeIdentity
	if err := checkNode(c); err != nil {
		return v, err
	}
	for _, name := range append([]string{c.VolumeDir}, volumePaths(c)...) {
		if err := privateDir(name); err != nil {
			return v, err
		}
	}
	if err := readJSON(filepath.Join(c.VolumeDir, "volume.json"), &v); err != nil {
		return v, err
	}
	if v.FormatVersion != formatVersion || v.Profile != validationName || v.NodeID != c.NodeID || v.VolumeID != c.VolumeID {
		return v, fmt.Errorf("volume identity, profile or format mismatch")
	}
	_, err := serviceConfig(v)
	return v, err
}

func createPrivate(name string) error {
	if err := checkPath(name, true); err != nil {
		return err
	}
	if err := os.Mkdir(name, 0o700); err != nil {
		return fmt.Errorf("create new private root %s (existing resources preserved): %w", name, err)
	}
	if err := privateDir(name); err != nil {
		return err
	}
	return errors.Join(syncDir(name), syncDir(filepath.Dir(name)))
}

func serviceConfig(v volumeIdentity) (fsservice.Config, error) {
	var c fsservice.Config
	fields := []string{v.VolumeID, v.Generation, v.Configuration, v.RootAllocation, v.NodeID, v.FixturePartner, v.FixtureAsync, v.OwnerIncarnation, v.PartnerIncarnation}
	ids := make([]fsapi.UUID, len(fields))
	seen := make(map[fsapi.UUID]bool)
	for i, raw := range fields {
		id, err := fsapi.ParseUUID(raw)
		if err != nil {
			return c, err
		}
		if seen[id] {
			return c, fmt.Errorf("reused manifest identity")
		}
		seen[id], ids[i] = true, id
	}
	volume, generation, configuration := ids[0], ids[1], ids[2]
	policy, err := fsservice.NewUnixPermissions(volume)
	if err != nil {
		return c, err
	}
	members := slices.Clone(ids[4:7])
	slices.SortFunc(members, func(a, b fsapi.UUID) int { return bytes.Compare(a[:], b[:]) })
	c.Stream = segment.Stream{Volume: volume, Generation: generation, Configuration: configuration, Epoch: 1, Kind: 1}
	c.Authority = record.Authority{Volume: volume, Generation: generation, Configuration: configuration, Epoch: 1, Owner: ids[4], Partner: ids[5], Members: members, CoordinatorTerm: 1, OwnerIncarnation: ids[7], PartnerIncarnation: ids[8]}
	c.Storage = storage.Config{Volume: volume, Root: fsapi.ObjectID{Home: volume, Allocation: ids[3]}, OwnerUID: uint32(os.Geteuid())}
	c.Validation = &fsservice.ValidationProfile{Disposable: true, Name: validationName}
	c.Permissions = policy
	c.Limits = fsservice.Limits{LogicalBytes: 4 << 20, IOBytes: 64 << 10}
	c.Segment.MaximumBytes = 1 << 20
	return c, nil
}

func initializeSource(ctx context.Context, source *vfs.OS, c fsservice.Config) error {
	dir := "."
	for _, part := range strings.Split(c.Stream.Directory(), "/") {
		next := path.Join(dir, part)
		if err := source.Mkdir(ctx, next, 0o700); err != nil {
			return err
		}
		if err := source.SyncDir(ctx, dir); err != nil {
			return err
		}
		dir = next
	}
	ids := make([]fsapi.UUID, 4)
	for i := range ids {
		id, err := freshID()
		if err != nil {
			return err
		}
		ids[i] = id
	}
	h := record.Header{Volume: c.Stream.Volume, Generation: c.Stream.Generation, Configuration: c.Stream.Configuration, Epoch: 1, Operation: ids[0], Incarnation: c.Authority.OwnerIncarnation, Observed: (fsapi.WallClock{}).Now()}
	w, err := segment.NewWriter(ctx, source, c.Stream, segment.Options{MaximumBytes: 1 << 20, Structural: func(uint64) record.Header { return h }, NewSegment: func() fsapi.UUID { return ids[1] }})
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	root := c.Storage.Root
	init := record.VolumeInit{Root: root, Initial: record.Metadata{Revision: 1, Type: 2, UID: uint32(os.Geteuid()), GID: uint32(os.Getegid()), Mode: 0o700, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: root}, RootLedger: record.LedgerEdit{Object: root, ResultingRevision: 1}, Authority: c.Authority, Authorization: record.Artifact{Kind: 6, ID: ids[2], Length: 1}}
	initHeader := h
	initHeader.Operation = ids[3]
	r, err := w.Append(ctx, record.Record{Header: initHeader, Payload: init})
	if err != nil {
		return err
	}
	if err := r.Wait(ctx); err != nil {
		return err
	}
	return w.Close()
}
