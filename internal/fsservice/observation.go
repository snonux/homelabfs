package fsservice

import (
	"context"
	"errors"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/storage"
	"io"
	"slices"
)

// committedReader keeps metadata planning and payload reads on one captured
// committed cut. The local v1 service uses its applied store; LogicalPlanner
// uses an owner RAM observation and never touches backing storage.
type committedReader interface {
	Read(context.Context, fsapi.ObjectID, []byte, int64) (int, error)
	ReadDir(context.Context, fsapi.ObjectID, string, int) ([]storage.Entry, error)
}

func observeCommitted(ctx context.Context, c fsapi.Command, p *plan, image storage.Image, source committedReader, limits Limits, storageLimits storage.Limits) (fsapi.Result, bool, error) {
	return observeView(ctx, c, p, image, source, limits, storageLimits, false)
}
func observeView(ctx context.Context, c fsapi.Command, p *plan, image storage.Image, source committedReader, limits Limits, storageLimits storage.Limits, foreign bool) (fsapi.Result, bool, error) {
	result := fsapi.Result{}
	switch c.Operation {
	case fsapi.Getattr:
		o, err := p.object(c.Object)
		if err != nil {
			return result, true, err
		}
		result.Attr = &o.Attr
	case fsapi.Read:
		if c.Handle == 0 {
			return result, true, fsapi.Stale
		}
		if c.Size > int64(limits.IOBytes) || fsapi.ValidateRange(c.Offset, c.Size) != nil {
			return result, true, fsapi.Invalid
		}
		o, err := p.object(c.Object)
		if err != nil {
			return result, true, err
		}
		if o.Attr.Kind != fsapi.Regular {
			return result, true, fsapi.IsDirectory
		}
		data := make([]byte, int(c.Size))
		n, err := source.Read(ctx, c.Object, data, c.Offset)
		if err != nil && !errors.Is(err, io.EOF) {
			return result, true, observationIO{err}
		}
		result.Data = data[:n]
		result.Count = int64(n)
	case fsapi.Readlink:
		o, err := p.object(c.Object)
		if err != nil {
			return result, true, err
		}
		if o.Attr.Kind != fsapi.Symlink {
			return result, true, fsapi.Invalid
		}
		result.Data = slices.Clone(o.Text)
	case fsapi.Readdir:
		if c.After != "" && c.DirectoryRevision == 0 {
			return result, true, fsapi.Invalid
		}
		o, err := p.directory(c.Object)
		if err != nil {
			return result, true, err
		}
		if c.DirectoryRevision != 0 && c.DirectoryRevision != o.Attr.Revision {
			return result, true, fsapi.Stale
		}
		entries, err := source.ReadDir(ctx, c.Object, c.After, c.Limit)
		if err != nil {
			return result, true, err
		}
		result.DirectoryRevision = o.Attr.Revision
		for _, e := range entries {
			if e.Target.Home != c.Mount && !foreign {
				return result, true, fsapi.Unsupported
			}
			target, err := p.object(*e.Target)
			if err != nil {
				return result, true, err
			}
			result.Entries = append(result.Entries, fsapi.DirEntry{Name: e.Name, Object: *e.Target, Revision: e.Revision, Kind: target.Attr.Kind})
		}
	case fsapi.Statfs:
		var used uint64
		for _, o := range image.Objects {
			used += uint64(o.Attr.Size)
		}
		maxObjects := storageLimits.Objects
		if maxObjects == 0 {
			maxObjects = 4096
		}
		totalObjects := len(image.Objects) + len(image.Retired)
		result.Capacity = &fsapi.Capacity{Objects: uint64(maxObjects), ObjectsFree: uint64(max(0, maxObjects-totalObjects)), Bytes: uint64(limits.LogicalBytes), BytesFree: uint64(limits.LogicalBytes) - used}
	default:
		return result, false, nil
	}
	return result, true, nil
}

type observationIO struct{ error }

func (e observationIO) Unwrap() error { return e.error }
