package apply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

// BarrieredRestart retains a private exact clean cut from the actual old Result.
// It is not serializable and carries no publication/native/physical authority.
// Replay's fresh-root/workspace requirements remain completely unchanged.
type BarrieredRestart struct {
	mu                sync.Mutex
	used              bool
	config            Config
	selection         Selection
	history           [32]byte
	snapshot          storage.Snapshot
	catalog, progress []byte
	applied           record.FrameRef
	slots             uint64
	progressBytes     int64
	maxBytes          int64
}

// SuspendRestart captures the actual clean store, full journal and protected
// APPLIED/catalog while the owner excludes ALL writers and retains its roots.
// Dirty/partial results and checkpoint-history archives are unsupported.
func (r *Result) SuspendRestart(ctx context.Context, source *segment.Reader, maxBytes int64) (*BarrieredRestart, error) {
	if r == nil || r.Store == nil || r.blocked || len(r.archive) != 0 || source == nil || maxBytes < 1 || maxBytes > 16<<20 {
		return nil, fsapi.Unavailable
	}
	h, err := readHistory(ctx, source, r.selection, r.config)
	if err != nil {
		return nil, err
	}
	if h.sum != r.history.sum {
		return nil, fsapi.Stale
	}
	if err = r.config.Authorizer.Authorize(ctx, Authorization{cloneSelection(r.selection), h.sum}); err != nil {
		return nil, err
	}
	snapshot, err := r.Store.Capture(ctx, maxBytes)
	if err != nil {
		return nil, err
	}
	catalog, err := storage.EncodeImage(snapshot.Image, r.config.Storage.Limits)
	if err != nil {
		return nil, err
	}
	total := int64(8 + len(catalog))
	for _, data := range snapshot.Data {
		total += 8 + int64(len(data))
	}
	if total > maxBytes {
		return nil, fsapi.NoSpace
	}
	raw, err := readFile(ctx, r.progress, "APPLIED", r.config.Storage.OwnerUID, progressSize)
	if err != nil {
		return nil, err
	}
	token := &BarrieredRestart{config: r.config, selection: cloneSelection(r.selection), history: h.sum, snapshot: snapshot, catalog: catalog, progress: raw, applied: r.Applied, slots: r.Slots, progressBytes: r.used, maxBytes: maxBytes}
	if _, err = token.readCatalog(ctx, r.progress, h); err != nil {
		return nil, err
	}
	return token, nil
}

// Reopen reconstructs a NEW store from the actual immutable protected catalog,
// verifying all payload bytes, APPLIED and complete original journal first.
// It never formats, replays, applies an effect or substitutes a saved RAM map.
func (r *BarrieredRestart) Reopen(ctx context.Context, data, journal, progress storage.Filesystem, source *segment.Reader, authorizer Authorizer) (Result, error) {
	if r == nil {
		return Result{}, fsapi.Permission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used || source == nil || authorizer == nil {
		return Result{}, fsapi.Permission
	}
	r.used = true
	h, err := readHistory(ctx, source, r.selection, r.config)
	if err != nil {
		return Result{}, err
	}
	if h.sum != r.history {
		return Result{}, fsapi.Stale
	}
	if err = authorizer.Authorize(ctx, Authorization{cloneSelection(r.selection), h.sum}); err != nil {
		return Result{}, err
	}
	image, err := r.readCatalog(ctx, progress, h)
	if err != nil {
		return Result{}, err
	}
	store, err := storage.Open(ctx, data, journal, r.config.Storage, image)
	if err != nil {
		return Result{}, err
	}
	actual, err := store.Capture(ctx, r.maxBytes)
	if err != nil {
		return Result{}, err
	}
	catalog, err := storage.EncodeImage(actual.Image, r.config.Storage.Limits)
	if err != nil {
		return Result{}, err
	}
	if !bytes.Equal(catalog, r.catalog) || len(actual.Data) != len(r.snapshot.Data) {
		return Result{}, fsapi.Stale
	}
	for i, body := range actual.Data {
		if !bytes.Equal(body, r.snapshot.Data[i]) {
			return Result{}, fsapi.Stale
		}
	}
	config := r.config
	config.Authorizer = authorizer
	// This one-use comparison copy no longer owns live state after the new
	// actual store and complete prefix have been verified.
	r.snapshot = storage.Snapshot{}
	r.catalog = nil
	r.progress = nil
	return Result{Store: store, Applied: r.applied, Slots: r.slots, selection: cloneSelection(r.selection), history: h, config: config, progress: progress, used: r.progressBytes}, nil
}

func (r *BarrieredRestart) readCatalog(ctx context.Context, progress storage.Filesystem, h history) (storage.Image, error) {
	raw, err := readFile(ctx, progress, "APPLIED", r.config.Storage.OwnerUID, progressSize)
	if err != nil {
		return storage.Image{}, err
	}
	if !bytes.Equal(raw, r.progress) {
		return storage.Image{}, fsapi.Stale
	}
	p, err := decodeProgress(raw)
	if err != nil {
		return storage.Image{}, err
	}
	if p.workspace != r.config.Workspace || p.index != uint64(len(h.selected)-1) || p.slots != r.slots || p.source != r.applied || p.prefix != h.prefixes[p.index] {
		return storage.Image{}, fsapi.Stale
	}
	catalog, err := readFile(ctx, progress, imageName(p.workspace, p.index), r.config.Storage.OwnerUID, maxImageBytes(r.config))
	if err != nil {
		return storage.Image{}, err
	}
	if uint64(len(catalog)) != p.imageBytes || sha256.Sum256(catalog) != p.imageHash || !bytes.Equal(catalog, r.catalog) {
		return storage.Image{}, fsapi.Stale
	}
	image, err := storage.DecodeImage(catalog, r.config.Storage.Limits)
	if err != nil {
		return storage.Image{}, err
	}
	if image.Position != r.snapshot.Image.Position || image.Volume != r.config.Storage.Volume || image.Root != r.config.Storage.Root {
		return storage.Image{}, fsapi.Stale
	}
	return image, nil
}
