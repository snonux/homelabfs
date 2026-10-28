package apply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

// Baseline owns immutable payload bytes and all exact source-prefix evidence.
// Catalog uses storage's canonical codec, preserving raw namespace bytes. Frames
// includes unselected candidates and all protocol/allocation/lifetime evidence.
// This initial bounded implementation retains that evidence permanently; it is
// segment reclamation, not semantic-history compaction or unbounded operation.
type Baseline struct {
	Version   uint16
	Stream    segment.Stream
	Catalog   []byte
	Data      [][]byte
	Frames    [][]byte
	Selection Selection
	Slots     uint64
	Applied   record.FrameRef
}

// BaselineAuthorization binds the independent payload/catalog copy to the exact
// selected source prefix. A trusted verifier must establish correspondence of
// the snapshot to those effects and all permanent evidence, not just hashes.
type BaselineAuthorization struct {
	Authorization
	SHA256 [32]byte
}

// BaselineAuthorizer authenticates a published recoverable baseline. No default
// implementation exists: local APPLIED/CURRENT/checksums alone are insufficient.
type BaselineAuthorizer interface {
	AuthorizeBaseline(context.Context, BaselineAuthorization) error
}

type baselineReader struct {
	entries []segment.Entry
	suffix  entryReader
}

// Capture freezes this successful result at a verified segment SEAL. The caller
// excludes source/apply/read/administrative mutation for this call. The returned
// bytes are independent of subsequent writes. It grants no retention permission.
func (r *Result) Capture(ctx context.Context, stream segment.Stream, maxPayloadBytes int64) (Baseline, error) {
	if r.blocked || r.Store == nil || len(r.history.entries) == 0 {
		return Baseline{}, fsapi.Unavailable
	}
	frames := make([][]byte, len(r.history.entries))
	for i, e := range r.history.entries {
		frames[i] = append([]byte(nil), e.Bytes...)
	}
	if _, err := segment.VerifyFrames(ctx, stream, frames); err != nil {
		return Baseline{}, err
	}
	snapshot, err := r.Store.Capture(ctx, maxPayloadBytes)
	if err != nil {
		return Baseline{}, err
	}
	catalog, err := storage.EncodeImage(snapshot.Image, r.config.Storage.Limits)
	if err != nil {
		return Baseline{}, err
	}
	return Baseline{1, stream, catalog, snapshot.Data, frames, cloneSelection(r.selection), r.Slots, r.Applied}, nil
}

// EncodeBaseline produces canonical bounded-checkpoint input. The caller bounds
// the encoded size as well as decoded frame/payload budgets before persistence.
func EncodeBaseline(b Baseline) ([]byte, error) { return json.Marshal(b) }

// DecodeBaseline rejects unknown/missing/duplicate fields, alternate encodings,
// invalid UTF-8 and trailing bytes by requiring exact canonical re-encoding.
func DecodeBaseline(raw []byte) (Baseline, error) {
	var b Baseline
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&b); err != nil {
		return Baseline{}, err
	}
	canonical, err := EncodeBaseline(b)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Baseline{}, fsapi.Invalid
	}
	return b, nil
}

// VerifyBaseline validates the entire sealed prefix, selection, image identity,
// backing position and independent payload bytes, then obtains explicit trusted
// correspondence authorization. maxPayloadBytes is a finite admission bound.
func VerifyBaseline(ctx context.Context, b Baseline, config Config, maxPayloadBytes int64, authority BaselineAuthorizer) error {
	_, _, err := verifyBaseline(ctx, b, config, maxPayloadBytes, authority)
	return err
}

// Resume restores a published immutable base into fresh private data/journal and
// progress roots, then authorizes and installs only new selected suffix effects.
// suffix must be verified with PreviousSeal equal to b.Selection.Through. The
// source prefix remains verifiable in b.Frames even after source segment removal.
// The caller retains all external recovery/visibility gates throughout this call.
func Resume(ctx context.Context, data, journal, progress storage.Filesystem, suffix *segment.Reader, selection Selection, config Config, b Baseline, maxPayloadBytes int64, authority BaselineAuthorizer) (Result, error) {
	if suffix == nil || config.Authorizer == nil || !config.Workspace.Valid() {
		return Result{}, fsapi.Invalid
	}
	maxFrames := config.MaxFrames
	if maxFrames == 0 {
		maxFrames = 4096
	}
	if len(selection.Proof) == 0 || len(selection.Proof) > 1<<20 || maxFrames < 1 || len(selection.Effects) == 0 || len(selection.Effects) > maxFrames || config.MaxBytes < 0 || config.MaxProgressBytes < 0 {
		return Result{}, fsapi.Invalid
	}
	selection = cloneSelection(selection)
	if err := distinctRoots(ctx, data, journal, progress); err != nil {
		return Result{}, err
	}
	entries, snapshot, err := verifyBaseline(ctx, b, config, maxPayloadBytes, authority)
	if err != nil {
		return Result{}, err
	}
	// Verify/authorize the exact extension before writing a destination. Old prefix
	// entries remain available to resolve earlier MUTATION/PREPARED and dedup IDs.
	h, err := readHistory(ctx, &baselineReader{entries: entries, suffix: suffix}, selection, config)
	if err != nil {
		return Result{}, err
	}
	if len(selection.Effects) < len(b.Selection.Effects) {
		return Result{}, fsapi.Stale
	}
	for i, e := range b.Selection.Effects {
		if selection.Effects[i] != e {
			return Result{}, fsapi.Stale
		}
	}
	if err = config.Authorizer.Authorize(ctx, Authorization{cloneSelection(selection), h.sum}); err != nil {
		return Result{}, err
	}
	if _, err = checkProgress(ctx, progress, config, h); err != nil {
		return Result{}, err
	}
	// A new progress namespace avoids regressing any old APPLIED pointer and keeps
	// uncertain prior attempts inspectable. Never infer a safe cut from APPLIED.
	dir, err := progress.OpenDir(ctx, ".")
	if err != nil {
		return Result{}, err
	}
	page, readErr := dir.ReadDir(ctx, 1)
	closeErr := dir.Close()
	if readErr != nil && readErr != io.EOF {
		return Result{}, readErr
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	if len(page) != 0 {
		return Result{}, fsapi.Exists
	}
	s, err := storage.Restore(ctx, data, journal, config.Storage, snapshot, maxPayloadBytes)
	if err != nil {
		return Result{}, err
	}
	baseHistory, err := readHistory(ctx, &baselineReader{entries: entries}, b.Selection, config)
	if err != nil {
		return Result{}, err
	}
	r := Result{Store: s, Applied: b.Applied, Slots: b.Slots, selection: cloneSelection(b.Selection), history: baseHistory, config: config, progress: progress, archive: entries}
	index := len(baseHistory.selected) - 1
	encoded, err := storage.EncodeImage(snapshot.Image, config.Storage.Limits)
	if err != nil {
		return Result{}, err
	}
	budget := config.MaxProgressBytes
	if budget == 0 {
		budget = 256 << 20
	}
	if int64(len(encoded))+progressSize > budget {
		return Result{}, fsapi.NoSpace
	}
	r.used, err = publish(ctx, progress, config, b.Applied, uint64(index), b.Slots, baseHistory.prefixes[index], snapshot.Image)
	if err != nil {
		return Result{}, err
	}
	if err = r.advance(ctx, &baselineReader{entries: h.entries}, selection); err != nil {
		return Result{}, err
	}
	return r, nil
}

func verifyBaseline(ctx context.Context, b Baseline, c Config, maxPayload int64, authority BaselineAuthorizer) ([]segment.Entry, storage.Snapshot, error) {
	if b.Version != 1 || authority == nil || len(b.Selection.Proof) == 0 || len(b.Selection.Proof) > 1<<20 {
		return nil, storage.Snapshot{}, fsapi.Invalid
	}
	maxFrames, maxBytes := c.MaxFrames, c.MaxBytes
	if maxFrames == 0 {
		maxFrames = 4096
	}
	if maxBytes == 0 {
		maxBytes = 64 << 20
	}
	if maxFrames < 1 || maxBytes < 1 || len(b.Frames) > maxFrames {
		return nil, storage.Snapshot{}, fsapi.NoSpace
	}
	var total int64
	for _, raw := range b.Frames {
		if int64(len(raw)) > maxBytes-total {
			return nil, storage.Snapshot{}, fsapi.NoSpace
		}
		total += int64(len(raw))
	}
	entries, err := segment.VerifyFrames(ctx, b.Stream, b.Frames)
	if err != nil {
		return nil, storage.Snapshot{}, err
	}
	h, err := readHistory(ctx, &baselineReader{entries: entries}, b.Selection, c)
	if err != nil {
		return nil, storage.Snapshot{}, err
	}
	image, err := storage.DecodeImage(b.Catalog, c.Storage.Limits)
	if err != nil {
		return nil, storage.Snapshot{}, err
	}
	snapshot := storage.Snapshot{Image: image, Data: b.Data}
	if err = storage.ValidateSnapshot(snapshot, c.Storage.Limits, maxPayload); err != nil {
		return nil, storage.Snapshot{}, err
	}
	if image.Volume != c.Storage.Volume || image.Root != c.Storage.Root || image.Position.Sequence != b.Slots || !sameHistory(image.Position, b.Selection.Through.Position) || b.Applied != b.Selection.Effects[len(b.Selection.Effects)-1] {
		return nil, storage.Snapshot{}, fmt.Errorf("%w: baseline identity/position", fsapi.Stale)
	}
	raw, err := EncodeBaseline(b)
	if err != nil {
		return nil, storage.Snapshot{}, err
	}
	if err = authority.AuthorizeBaseline(ctx, BaselineAuthorization{Authorization{cloneSelection(b.Selection), h.sum}, sha256.Sum256(raw)}); err != nil {
		return nil, storage.Snapshot{}, err
	}
	return entries, snapshot, nil
}

func (r *baselineReader) Next(ctx context.Context) (segment.Entry, error) {
	if err := ctx.Err(); err != nil {
		return segment.Entry{}, err
	}
	if len(r.entries) != 0 {
		e := r.entries[0]
		r.entries = r.entries[1:]
		return e, nil
	}
	if r.suffix != nil {
		return r.suffix.Next(ctx)
	}
	return segment.Entry{}, io.EOF
}
