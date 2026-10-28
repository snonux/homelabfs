package apply

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

// Selection is an externally chosen complete committed history. Effects starts
// with VOLUME_INIT, followed by every selected COMMIT, committed INSTALL and
// protected HANDLE_PIN/HANDLE_RELEASE/RECLAIM in source order. Proof is opaque to
// this package; Authorizer must verify it, including all dependency closure and
// omitted candidates. A locally durable COMMIT or receipt alone is insufficient.
type Selection struct {
	Through record.FrameRef
	Effects []record.FrameRef
	Proof   []byte
}

// Authorization binds exact history bytes and the complete effect selection.
// HistorySHA256 includes every frame, including unselected candidates/structure.
type Authorization struct {
	Selection     Selection
	HistorySHA256 [32]byte
}

// Authorizer is the trusted replication/recovery boundary. Authorize must verify
// complete selection/protection, retained dependencies, authority and closure
// gates for this exact input. It must retain those gates throughout Replay.
// This package supplies no normal-mode commit or client acknowledgement policy.
type Authorizer interface {
	Authorize(context.Context, Authorization) error
}

// Boundary identifies a crash injection boundary. Hooks run synchronously and
// must not reenter Replay or mutate its files.
type Boundary string

const (
	// BeforeApply precedes each selected effect (genesis is already formatted).
	BeforeApply Boundary = "before_apply"
	// AfterApply follows catalog/payload installation, before durability barriers.
	AfterApply Boundary = "after_apply"
	// EffectsDurable follows Export and all payload/name barriers.
	EffectsDurable Boundary = "effects_durable"
	// ImageDurable follows immutable image file and parent barriers.
	ImageDurable Boundary = "image_durable"
	// BeforePublish follows pointer-file sync, before its rename over APPLIED.
	BeforePublish Boundary = "before_publish"
	// AfterRename follows pointer rename, before parent-directory sync.
	AfterRename Boundary = "after_rename"
	// AppliedDurable follows the final APPLIED parent-directory barrier.
	AppliedDurable Boundary = "applied_durable"
)

// Config admits a bounded replay into a new empty pair of private roots.
// Workspace is fresh per attempt, including failed/restarted attempts. Progress
// has its own exclusively owned private root; it is never a storage CURRENT.
type Config struct {
	Storage    storage.Config
	Workspace  fsapi.UUID
	Authorizer Authorizer
	MaxFrames  int
	MaxBytes   int64
	// MaxProgressBytes bounds immutable progress images written by one attempt.
	// The caller also admits aggregate space across retained failed attempts.
	MaxProgressBytes int64
	Hook             func(context.Context, Boundary) error
}

// Result is a locally installed view, still behind the caller's external gates.
// Applied is a source journal position; Store uses separate contiguous redo slots.
// Once returned, do not copy Result or call Advance concurrently; its Store is
// borrowed under the caller's read/apply/authority gates.
type Result struct {
	Store   *storage.Store
	Applied record.FrameRef
	Slots   uint64

	selection Selection
	history   history
	config    Config
	progress  storage.Filesystem
	used      int64
	blocked   bool
	archive   []segment.Entry
}

type history struct {
	entries  []segment.Entry
	selected []int
	prefixes [][32]byte
	sum      [32]byte
}

// Replay verifies a complete owner stream from genesis, obtains explicit trusted
// authorization and rebuilds into empty data/journal roots. It never reuses dirty
// payloads or skips redo based on progress. Preserve all old workspaces and source
// history until a separate checkpoint/retention protocol permits reclamation.
// source must be an OpenReader with its fixed files retained for this call.
func Replay(ctx context.Context, data, journal, progress storage.Filesystem, source *segment.Reader, selection Selection, config Config) (Result, error) {
	maxFrames := config.MaxFrames
	if maxFrames == 0 {
		maxFrames = 4096
	}
	if maxFrames < 1 || len(selection.Effects) == 0 || len(selection.Effects) > maxFrames || config.MaxBytes < 0 || config.MaxProgressBytes < 0 {
		return Result{}, fsapi.Invalid
	}
	if source == nil || config.Authorizer == nil || !config.Workspace.Valid() || len(selection.Proof) == 0 || len(selection.Proof) > 1<<20 {
		return Result{}, fmt.Errorf("%w: explicit trusted selection required", fsapi.Invalid)
	}
	if err := distinctRoots(ctx, data, journal, progress); err != nil {
		return Result{}, err
	}
	selection = cloneSelection(selection)
	h, err := readHistory(ctx, source, selection, config)
	if err != nil {
		return Result{}, err
	}
	// Decode-owned frames and copied proof prevent a caller from changing the plan
	// while authorization executes. The Authorizer also receives its own copies.
	if err = config.Authorizer.Authorize(ctx, Authorization{cloneSelection(selection), h.sum}); err != nil {
		return Result{}, fmt.Errorf("authorize selected history: %w", err)
	}
	previous, err := checkProgress(ctx, progress, config, h)
	if err != nil {
		return Result{}, err
	}
	initEntry := h.entries[h.selected[0]]
	init, ok := initEntry.Record.Payload.(record.VolumeInit)
	if !ok || init.Root != config.Storage.Root {
		return Result{}, fmt.Errorf("%w: selected genesis", fsapi.Invalid)
	}
	root, err := initialRoot(initEntry.Record.Header.Operation, init)
	if err != nil {
		return Result{}, err
	}
	p := initEntry.Reference.Position
	p.Sequence = 0
	s, err := storage.Format(ctx, data, journal, config.Storage, root, p)
	if err != nil {
		return Result{}, fmt.Errorf("fresh replay destination: %w", err)
	}
	result := Result{Store: s}
	budget := config.MaxProgressBytes
	if budget == 0 {
		budget = 256 << 20
	}
	if budget < 1 {
		return Result{}, fsapi.Invalid
	}
	var used int64
	seen := make(map[fsapi.UUID][32]byte)
	for n, index := range h.selected {
		entry := h.entries[index]
		if err = hit(ctx, config, BeforeApply); err != nil {
			return Result{}, err
		}
		identity, identityErr := effectIdentity(entry.Record)
		if identityErr != nil {
			return Result{}, identityErr
		}
		old, duplicate := seen[entry.Record.Header.Operation]
		if duplicate && old != identity {
			return Result{}, fmt.Errorf("%w: operation identity reused with different effect", fsapi.Stale)
		}
		if n > 0 && !duplicate {
			var op *storage.Operation
			op, err = operation(ctx, s, h, entry, result.Slots+1)
			if err == nil && op != nil {
				err = s.Apply(ctx, *op)
				if err == nil {
					result.Slots++
				}
			}
			if err != nil {
				return Result{}, fmt.Errorf("apply source sequence %d: %w", entry.Reference.Position.Sequence, err)
			}
		}
		seen[entry.Record.Header.Operation] = identity
		if err = hit(ctx, config, AfterApply); err != nil {
			return Result{}, err
		}
		image, e := s.Export(ctx)
		if e != nil {
			return Result{}, fmt.Errorf("sync applied effects: %w", e)
		}
		if err = hit(ctx, config, EffectsDurable); err != nil {
			return Result{}, err
		}
		if uint64(n) >= previous {
			raw, e := storage.EncodeImage(image, config.Storage.Limits)
			if e != nil {
				return Result{}, e
			}
			if int64(len(raw))+progressSize > budget-used {
				return Result{}, fsapi.NoSpace
			}
			var written int64
			written, err = publish(ctx, progress, config, entry.Reference, uint64(n), result.Slots, h.prefixes[n], image)
			if err != nil {
				return Result{}, err
			}
			used += written
		}
		result.Applied = entry.Reference
	}
	result.selection, result.history, result.config, result.progress, result.used = selection, h, config, progress, used
	return result, nil
}

type entryReader interface {
	Next(context.Context) (segment.Entry, error)
}

func readHistory(ctx context.Context, r entryReader, selection Selection, c Config) (history, error) {
	var h history
	maxFrames, maxBytes := c.MaxFrames, c.MaxBytes
	if maxFrames == 0 {
		maxFrames = 4096
	}
	if maxBytes == 0 {
		maxBytes = 64 << 20
	}
	if maxFrames < 1 || maxBytes < 1 || len(selection.Effects) == 0 || len(selection.Effects) > maxFrames {
		return h, fsapi.Invalid
	}
	digest := sha256.New()
	selected := 0
	var bytes int64
	for {
		e, err := r.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return h, fmt.Errorf("read selected history: %w", err)
		}
		if len(h.entries) >= maxFrames || int64(len(e.Bytes)) > maxBytes-bytes {
			return h, fsapi.NoSpace
		}
		bytes += int64(len(e.Bytes))
		if e.Reference.StreamKind != 1 || e.Reference.Reporter != nil || e.Reference.ReporterIncarnation != nil || e.Reference.Position.Volume != c.Storage.Volume || e.Reference.Position.Sequence != uint64(len(h.entries))+1 || (len(h.entries) > 0 && !sameHistory(e.Reference.Position, h.entries[0].Reference.Position)) {
			return h, fmt.Errorf("%w: complete genesis owner history required", fsapi.Stale)
		}
		_, _ = digest.Write(binary.BigEndian.AppendUint64(nil, uint64(len(e.Bytes))))
		_, _ = digest.Write(e.Bytes)
		flag := byte(0)
		if selected < len(selection.Effects) && selection.Effects[selected] == e.Reference {
			h.selected = append(h.selected, len(h.entries))
			selected++
			flag = 1
		} else if selected < len(selection.Effects) && selection.Effects[selected].Position.Sequence <= e.Reference.Position.Sequence {
			return h, fmt.Errorf("%w: missing/conflicting selected effect", fsapi.Stale)
		}
		_, _ = digest.Write([]byte{flag})
		if flag == 1 {
			var sum [32]byte
			copy(sum[:], digest.Sum(nil))
			h.prefixes = append(h.prefixes, sum)
		}
		h.entries = append(h.entries, e)
	}
	if len(h.entries) == 0 || selected != len(selection.Effects) || h.entries[len(h.entries)-1].Reference != selection.Through {
		return h, fmt.Errorf("%w: incomplete selected prefix", fsapi.Stale)
	}
	if _, ok := h.entries[h.selected[0]].Record.Payload.(record.VolumeInit); !ok {
		return h, fsapi.Invalid
	}
	copy(h.sum[:], digest.Sum(nil))
	return h, nil
}
func sameHistory(a, b record.Position) bool { a.Sequence = 0; b.Sequence = 0; return a == b }
func cloneSelection(s Selection) Selection {
	s.Effects = append([]record.FrameRef(nil), s.Effects...)
	s.Proof = append([]byte(nil), s.Proof...)
	return s
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
func initialRoot(id fsapi.UUID, v record.VolumeInit) (storage.Object, error) {
	if v.RootLedger.Object != v.Root || v.RootLedger.ExpectedRevision != 0 || v.RootLedger.ResultingRevision != 1 || v.RootLedger.ExpectedNlink != 0 || v.RootLedger.ResultingNlink != 0 || len(v.RootLedger.Remove)+len(v.RootLedger.Add) != 0 {
		return storage.Object{}, fsapi.Invalid
	}
	return storage.Object{Descriptor: storage.Descriptor{Object: v.Root, Kind: fsapi.Directory, Creation: id}, Attr: metadata(v.Root, v.Initial), LedgerRevision: 1, Parent: &storage.Parent{Revision: v.RootParent.Revision, Object: v.RootParent.Parent, EntryRevision: v.RootParent.ParentEntryRevision}}, nil
}

func distinctRoots(ctx context.Context, roots ...storage.Filesystem) error {
	for i, root := range roots {
		if root == nil {
			return fsapi.Invalid
		}
		info, err := root.Stat(ctx, ".")
		if err != nil {
			return err
		}
		for _, prior := range roots[:i] {
			old, e := prior.Stat(ctx, ".")
			if e != nil {
				return e
			}
			if info.Identity == old.Identity {
				return fmt.Errorf("%w: aliased replay roots", fsapi.Invalid)
			}
		}
	}
	return nil
}

func effectIdentity(r record.Record) ([32]byte, error) {
	r.Header.Sequence = 1
	raw, err := record.Encode(r)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}
