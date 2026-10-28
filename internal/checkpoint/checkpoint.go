// Package checkpoint publishes immutable recoverable local baselines and reclaims
// replaced closed source segments behind explicit trusted retention gates. It
// supplies no normal-mode ACCEPT, replication verifier or implicit authority.
package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

// Boundary names a synchronous crash hook; hooks cannot reenter or mutate files.
type Boundary string

const (
	// BaselineDurable follows independent payload/evidence file and parent sync.
	BaselineDurable Boundary = "baseline_durable"
	// ManifestDurable follows immutable manifest file and parent sync.
	ManifestDurable Boundary = "manifest_durable"
	// BeforePublish follows pointer sync, before atomic CURRENT rename.
	BeforePublish Boundary = "before_publish"
	// AfterPublish follows CURRENT rename, before its parent sync.
	AfterPublish Boundary = "after_publish"
	// Published follows durable publication; reclamation may now be considered.
	Published Boundary = "published"
	// BoundaryDurable follows a durable reclaimable-boundary record.
	BoundaryDurable Boundary = "boundary_durable"
	// AfterUnlink precedes the source directory barrier after each ordered unlink.
	AfterUnlink Boundary = "after_unlink"
	// TrimDurable follows each source directory barrier.
	TrimDurable Boundary = "trim_durable"
)

// Manifest binds an immutable snapshot to volume history, exact sealed source
// boundary and required suffix. Evidence is trusted external checkpoint proof;
// local publication barriers alone never establish its validity or retention.
type Manifest struct {
	Version        uint16
	ID             fsapi.UUID
	Volume         fsapi.UUID
	Generation     fsapi.UUID
	Through        record.FrameRef
	BaselineSHA256 [32]byte
	BaselineBytes  int64
	RequiredSuffix uint64
	Evidence       []byte
}

// Authority verifies the exact manifest/proof and baseline correspondence and
// serializes ownership, selection, checkpoint, source and membership transitions.
// Enter returns a nonnil release only while all those gates are held. It must
// authenticate accepted baseline digests on recovery, not approve arbitrary
// snapshots because their hashes or local COMMIT records are well formed.
type Authority interface {
	apply.BaselineAuthorizer
	Enter(context.Context, Manifest) (func(), error)
}

// Grant binds all required durable obligations to one exact checkpoint. Missing
// replica, transfer/promotion pin, recovery hold or advertised recovery-point
// knowledge is unknown and blocks reclamation. This is an explicit trust seam,
// not a claim that currently assigned replicas have reached this position.
type Grant struct {
	CheckpointSHA256                     [32]byte
	Through                              record.FrameRef
	ReplicasKnown, PinsKnown, HoldsKnown bool
	RecoveryPointsKnown                  bool
	Proof                                []byte
	Release                              func()
}

// RetentionDependency must retain all dependency gates until Release. It must
// account for every assigned replica (durable coverage, never memory ACK), active
// catch-up/snapshot/promotion/emergency/checkpoint reference, recovery hold and
// retained recovery plan. There is intentionally no permissive default provider.
// The initial supported deployment is single-node with explicit no-other-peer
// configuration; multi-node retention implementations remain future work.
type RetentionDependency interface {
	Acquire(context.Context, Manifest) (Grant, error)
}

// Config admits finite artifacts and supplies trusted external gates. The caller
// owns a separate private checkpoint root, excludes external mutation/aliasing,
// preserves baseline artifacts permanently and uses baseline-aware recovery.
// In particular, never trim beneath fsservice's genesis-only startup path.
type Config struct {
	Apply            apply.Config
	MaxPayloadBytes  int64
	MaxArtifactBytes int64
	SourceLimits     segment.Limits
	Authority        Authority
	Dependencies     RetentionDependency
	Hook             func(context.Context, Boundary) error
}

// Publish verifies an independently captured baseline and its source, writes
// immutable baseline/manifest files and atomically publishes CURRENT. It does
// not delete anything. ID must be fresh, even after an interrupted attempt.
func Publish(ctx context.Context, root storage.Filesystem, source segment.Backend, b apply.Baseline, id fsapi.UUID, evidence []byte, config Config) (Manifest, error) {
	if source == nil {
		return Manifest{}, fsapi.Invalid
	}
	config, err := admit(ctx, root, config)
	if err != nil {
		return Manifest{}, err
	}
	if !id.Valid() || len(evidence) == 0 || len(evidence) > 1<<20 {
		return Manifest{}, fsapi.Invalid
	}
	if err = preflight(b, config); err != nil {
		return Manifest{}, err
	}
	raw, err := apply.EncodeBaseline(b)
	if err != nil {
		return Manifest{}, err
	}
	if int64(len(raw)) > config.MaxArtifactBytes {
		return Manifest{}, fsapi.NoSpace
	}
	m := Manifest{1, id, b.Stream.Volume, b.Stream.Generation, b.Selection.Through, sha256.Sum256(raw), int64(len(raw)), b.Selection.Through.Position.Sequence + 1, append([]byte(nil), evidence...)}
	release, err := config.Authority.Enter(ctx, cloneManifest(m))
	if err != nil {
		return Manifest{}, err
	}
	if release == nil {
		return Manifest{}, fsapi.Unavailable
	}
	defer release()
	if err = advancing(ctx, root, b, config); err != nil {
		return Manifest{}, err
	}
	if err = apply.VerifyBaseline(ctx, b, config.Apply, config.MaxPayloadBytes, config.Authority); err != nil {
		return Manifest{}, err
	}
	if _, err = segment.VerifyCheckpointSource(ctx, source, b.Stream, config.SourceLimits, b.Frames); err != nil {
		return Manifest{}, err
	}
	if err = write(ctx, root, baselineName(id), raw); err != nil {
		return Manifest{}, err
	}
	if err = hit(ctx, config, BaselineDurable); err != nil {
		return Manifest{}, err
	}
	manifest, err := encode(m)
	if err != nil {
		return Manifest{}, err
	}
	if err = write(ctx, root, manifestName(id), manifest); err != nil {
		return Manifest{}, err
	}
	if err = hit(ctx, config, ManifestDurable); err != nil {
		return Manifest{}, err
	}
	if err = publishPointer(ctx, root, "CURRENT", id, manifest, config); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Recover revalidates the published immutable bytes and obtains fresh external
// authority. It tolerates obsolete prefix files left by interrupted trim and
// returns the authenticated baseline for apply.Resume into new private roots.
// A stale RECLAIM record never supplies authority and is not consulted.
func Recover(ctx context.Context, root storage.Filesystem, source segment.Backend, config Config) (Manifest, apply.Baseline, error) {
	if source == nil {
		return Manifest{}, apply.Baseline{}, fsapi.Invalid
	}
	config, err := admit(ctx, root, config)
	if err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	m, b, err := load(ctx, root, config)
	if err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	release, err := config.Authority.Enter(ctx, cloneManifest(m))
	if err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	if release == nil {
		return Manifest{}, apply.Baseline{}, fsapi.Unavailable
	}
	defer release()
	if err = stillCurrent(ctx, root, m, config); err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	if err = verify(ctx, source, m, b, config); err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	return m, b, nil
}

// Trim revalidates CURRENT, immutable payload/evidence and source on every call,
// acquires fresh complete retention obligations, durably records the boundary,
// then unlinks only covered closed segments in ascending source order. Each
// unlink is parent-synced before the next; restart safely tolerates any leftover
// leading suffix of obsolete files. No file age/highest-record heuristic exists.
func Trim(ctx context.Context, root storage.Filesystem, source segment.Backend, config Config) (int, error) {
	if source == nil {
		return 0, fsapi.Invalid
	}
	config, err := admit(ctx, root, config)
	if err != nil {
		return 0, err
	}
	if config.Dependencies == nil {
		return 0, fsapi.Unavailable
	}
	m, b, err := load(ctx, root, config)
	if err != nil {
		return 0, err
	}
	release, err := config.Authority.Enter(ctx, cloneManifest(m))
	if err != nil {
		return 0, err
	}
	if release == nil {
		return 0, fsapi.Unavailable
	}
	defer release()
	if err = stillCurrent(ctx, root, m, config); err != nil {
		return 0, err
	}
	if err = verify(ctx, source, m, b, config); err != nil {
		return 0, err
	}
	grant, err := config.Dependencies.Acquire(ctx, cloneManifest(m))
	if err != nil {
		return 0, err
	}
	if grant.Release == nil {
		return 0, fsapi.Unavailable
	}
	defer grant.Release()
	manifest, err := encode(m)
	if err != nil {
		return 0, err
	}
	if grant.CheckpointSHA256 != sha256.Sum256(manifest) || grant.Through != m.Through || !grant.ReplicasKnown || !grant.PinsKnown || !grant.HoldsKnown || !grant.RecoveryPointsKnown || len(grant.Proof) == 0 || len(grant.Proof) > 1<<20 {
		return 0, fmt.Errorf("%w: incomplete retention dependencies", fsapi.Unavailable)
	}
	// Publication might have survived a crash before its final parent sync. The
	// verified referenced files are already durable; persist the pointer again.
	if err = syncFile(ctx, root, "CURRENT"); err != nil {
		return 0, err
	}
	if err = root.SyncDir(ctx, "."); err != nil {
		return 0, err
	}
	// An exclusive attempt-specific boundary file is only evidence. Repeated trim
	// uses the same exact bytes safely; no dependency grant is reused.
	boundary := "reclaim-" + m.ID.String() + ".json"
	if err = immutable(ctx, root, boundary, manifest, config); err != nil {
		return 0, err
	}
	if err = hit(ctx, config, BoundaryDurable); err != nil {
		return 0, err
	}
	recovery, err := segment.VerifyCheckpointSource(ctx, source, b.Stream, config.SourceLimits, b.Frames)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, s := range recovery.Segments {
		if s.Seal == nil || s.Last > m.Through.Position.Sequence {
			break
		}
		if err = source.Unlink(ctx, s.Name); err != nil {
			return deleted, err
		}
		deleted++
		if err = hit(ctx, config, AfterUnlink); err != nil {
			return deleted, err
		}
		if err = source.SyncDir(ctx, path.Dir(s.Name)); err != nil {
			return deleted, err
		}
		if err = hit(ctx, config, TrimDurable); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func verify(ctx context.Context, source segment.Backend, m Manifest, b apply.Baseline, c Config) error {
	if m.Version != 1 || !m.ID.Valid() || m.Volume != c.Apply.Storage.Volume || m.Volume != b.Stream.Volume || m.Generation != b.Stream.Generation || m.Through != b.Selection.Through || m.RequiredSuffix != m.Through.Position.Sequence+1 || m.RequiredSuffix == 0 || len(m.Evidence) == 0 || len(m.Evidence) > 1<<20 {
		return fsapi.Invalid
	}
	if err := apply.VerifyBaseline(ctx, b, c.Apply, c.MaxPayloadBytes, c.Authority); err != nil {
		return err
	}
	_, err := segment.VerifyCheckpointSource(ctx, source, b.Stream, c.SourceLimits, b.Frames)
	return err
}

func stillCurrent(ctx context.Context, root storage.Filesystem, m Manifest, c Config) error {
	current, _, err := load(ctx, root, c)
	if err != nil {
		return err
	}
	want, err := encode(m)
	if err != nil {
		return err
	}
	got, err := encode(current)
	if err != nil {
		return err
	}
	if !bytes.Equal(want, got) {
		return fsapi.Stale
	}
	return nil
}

func advancing(ctx context.Context, root storage.Filesystem, b apply.Baseline, c Config) error {
	_, err := root.Stat(ctx, "CURRENT")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, old, err := load(ctx, root, c)
	if err != nil {
		return err
	}
	if b.Stream != old.Stream || len(b.Frames) < len(old.Frames) || len(b.Selection.Effects) < len(old.Selection.Effects) {
		return fsapi.Stale
	}
	for i, raw := range old.Frames {
		if !bytes.Equal(raw, b.Frames[i]) {
			return fsapi.Stale
		}
	}
	for i, ref := range old.Selection.Effects {
		if ref != b.Selection.Effects[i] {
			return fsapi.Stale
		}
	}
	return nil
}

func hit(ctx context.Context, c Config, b Boundary) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Hook != nil {
		return c.Hook(ctx, b)
	}
	return nil
}

func cloneManifest(m Manifest) Manifest { m.Evidence = append([]byte(nil), m.Evidence...); return m }
func baselineName(id fsapi.UUID) string { return "baseline-" + id.String() + ".hfb" }
func manifestName(id fsapi.UUID) string { return "manifest-" + id.String() + ".json" }
