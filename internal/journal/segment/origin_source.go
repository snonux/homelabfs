package segment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"sync"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

// OriginSource is a bounded immutable v3 source-fragment store. Each complete
// frame has a separate exclusive file. It preserves uncertain failed files;
// restart verifies every byte behind closed external gates. The caller must
// exclude concurrent other instances for this directory's complete lifetime.
// It provides local sync only, never selection, serving, retention or GC rights.
type OriginSource struct {
	mu      sync.Mutex
	backend Backend
	stream  record.OriginStream
	next    uint64
	bytes   int64
	limit   Limits
	err     error
}

// OriginDirectory keeps all v3 bytes outside immutable v1/E2 segment paths.
func OriginDirectory(s record.OriginStream) string {
	return fmt.Sprintf("origin-v3/%s/%s/%s/%s/e%020d/%s/%s", s.Cluster, s.Volume, s.Generation, s.Configuration, s.Epoch, s.Origin, s.Journal)
}

// OpenOriginSource verifies an existing source directory in full. Callers create
// and sync private parents explicitly; opening never formats or repairs history.
// Zero limits select 4096 frames / 64 MiB, also the hard maximum for this seam.
func OpenOriginSource(ctx context.Context, backend Backend, stream record.OriginStream, limit Limits) (*OriginSource, error) {
	probe := record.NormalRecord{Stream: stream, Sequence: 1, Operation: stream.Journal, Creator: record.Launch{Node: stream.Origin, Incarnation: stream.Journal}, Payload: record.OriginOpen{Segment: stream.Journal, MaximumBytes: 1024}}
	if _, err := record.EncodeNormal(probe); err != nil {
		return nil, err
	}
	if limit.Segments == 0 {
		limit.Segments = 4096
	}
	if limit.Bytes == 0 {
		limit.Bytes = 64 << 20
	}
	if limit.Segments < 1 || limit.Segments > 4096 || limit.Bytes < 1 || limit.Bytes > 64<<20 {
		return nil, ErrLimit
	}
	s := &OriginSource{backend: backend, stream: stream, next: 1, limit: limit}
	names, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if name != originName(s.next) {
			return nil, fmt.Errorf("origin source gap/foreign filename: %w", ErrCorrupt)
		}
		data, err := s.read(ctx, name)
		if err != nil {
			return nil, err
		}
		r, err := record.DecodeNormal(data)
		if err != nil {
			return nil, err
		}
		if r.Stream != stream || r.Sequence != s.next {
			return nil, fmt.Errorf("origin source identity: %w", ErrCorrupt)
		}
		// A prior uncertain attempt may have left complete but unsynced bytes.
		// Establish fresh local durability before exposing this verified prefix.
		if err := syncExisting(ctx, backend, OriginDirectory(stream)+"/"+name, nil); err != nil {
			return nil, err
		}
		s.bytes += int64(len(data))
		if s.bytes > limit.Bytes || s.next == math.MaxUint64 {
			return nil, ErrLimit
		}
		s.next++
	}
	if err := backend.SyncDir(ctx, OriginDirectory(stream)); err != nil {
		return nil, err
	}
	return s, nil
}

// Append syncs exact creator bytes before returning local durable coverage.
// Identical duplicate positions are idempotent; conflicting bytes fail closed.
// Cancellation or any uncertain I/O poisons this instance until full reopen.
// The caller must not mutate data while Append is executing.
func (s *OriginSource) Append(ctx context.Context, data []byte) (record.NormalRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return record.NormalRef{}, s.err
	}
	ref, err := record.NormalReference(data)
	if err != nil {
		return record.NormalRef{}, err
	}
	if ref.Stream != s.stream || ref.Sequence > s.next {
		return record.NormalRef{}, fmt.Errorf("origin source identity/gap")
	}
	if ref.Sequence < s.next {
		old, err := s.read(ctx, originName(ref.Sequence))
		if err != nil {
			return record.NormalRef{}, err
		}
		if !bytes.Equal(old, data) {
			return record.NormalRef{}, fmt.Errorf("origin source duplicate conflict: %w", ErrCorrupt)
		}
		return ref, nil
	}
	if s.next > uint64(s.limit.Segments) || s.next == math.MaxUint64 || int64(len(data)) > s.limit.Bytes-s.bytes {
		return record.NormalRef{}, ErrLimit
	}
	if err := s.write(ctx, originName(s.next), data); err != nil {
		s.err = err
		return record.NormalRef{}, err
	}
	s.next++
	s.bytes += int64(len(data))
	return ref, nil
}

// Read returns owned exact bytes after complete identity and hash verification.
func (s *OriginSource) Read(ctx context.Context, ref record.NormalRef) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ref.Stream != s.stream || ref.Sequence == 0 || ref.Sequence >= s.next {
		return nil, fmt.Errorf("origin source reference outside verified prefix")
	}
	data, err := s.read(ctx, originName(ref.Sequence))
	if err != nil {
		return nil, err
	}
	actual, err := record.NormalReference(data)
	if err != nil {
		return nil, err
	}
	if actual != ref {
		return nil, fmt.Errorf("origin source exact reference mismatch: %w", ErrCorrupt)
	}
	return data, nil
}

func (s *OriginSource) discover(ctx context.Context) (_ []string, retErr error) {
	d, err := s.backend.OpenDir(ctx, OriginDirectory(s.stream))
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, d.Close()) }()
	names := []string{}
	var size int64
	for {
		entries, err := d.ReadDir(ctx, min(64, s.limit.Segments+1-len(names)))
		for _, entry := range entries {
			if entry.Info.Mode != 0o600 || entry.Info.Links != 1 || entry.Info.Size < 200 || entry.Info.Size > record.MaxFrame {
				return nil, fmt.Errorf("origin source file metadata: %w", ErrCorrupt)
			}
			size += entry.Info.Size
			names = append(names, entry.Name)
			if len(names) > s.limit.Segments || size > s.limit.Bytes {
				return nil, ErrLimit
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *OriginSource) read(ctx context.Context, name string) (_ []byte, retErr error) {
	f, err := s.backend.Open(ctx, OriginDirectory(s.stream)+"/"+name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	statter, ok := f.(vfs.Statter)
	if !ok {
		return nil, fmt.Errorf("origin source requires descriptor metadata")
	}
	st, err := statter.Stat(ctx)
	if err != nil {
		return nil, err
	}
	if st.Mode != 0o600 || st.Links != 1 || st.Size < 200 || st.Size > record.MaxFrame {
		return nil, ErrCorrupt
	}
	b := make([]byte, int(st.Size))
	n, err := f.ReadAt(ctx, b, 0)
	if err != nil || n != len(b) {
		return nil, errors.Join(io.ErrUnexpectedEOF, err)
	}
	return b, nil
}

func (s *OriginSource) write(ctx context.Context, name string, data []byte) (retErr error) {
	dir := OriginDirectory(s.stream)
	f, err := s.backend.Open(ctx, dir+"/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, f.Close()) }()
	n, err := f.WriteAt(ctx, data, 0)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if err := f.Sync(ctx); err != nil {
		return err
	}
	return s.backend.SyncDir(ctx, dir)
}

func originName(sequence uint64) string { return fmt.Sprintf("s%020d.hfsn", sequence) }
