package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

type pointer struct {
	Version uint16
	ID      fsapi.UUID
	Bytes   int64
	SHA256  [32]byte
}

func admit(ctx context.Context, root storage.Filesystem, c Config) (Config, error) {
	if root == nil || c.Authority == nil || c.Apply.Authorizer == nil {
		return c, fsapi.Invalid
	}
	if c.MaxPayloadBytes == 0 {
		c.MaxPayloadBytes = 64 << 20
	}
	if c.MaxArtifactBytes == 0 {
		c.MaxArtifactBytes = 192 << 20
	}
	if c.MaxPayloadBytes < 1 || c.MaxArtifactBytes < 1 || c.MaxArtifactBytes > 1<<30 {
		return c, fsapi.Invalid
	}
	info, err := root.Stat(ctx, ".")
	if err != nil {
		return c, err
	}
	if info.Mode != fs.ModeDir|0o700 || info.UID != c.Apply.Storage.OwnerUID {
		return c, fsapi.Permission
	}
	return c, nil
}

// preflight admits all nested variable-length baseline inputs before encoding.
// The exact encoded-size check remains necessary for base64/JSON overhead.
func preflight(b apply.Baseline, c Config) error {
	maxFrames, maxSource := c.Apply.MaxFrames, c.Apply.MaxBytes
	maxObjects, maxCatalog := c.Apply.Storage.Limits.Objects, c.Apply.Storage.Limits.FileBytes
	if maxFrames == 0 {
		maxFrames = 4096
	}
	if maxSource == 0 {
		maxSource = 64 << 20
	}
	if maxObjects == 0 {
		maxObjects = 4096
	}
	if maxCatalog == 0 {
		maxCatalog = 16 << 20
	}
	if maxFrames < 1 || maxSource < 1 || maxObjects < 1 || maxCatalog < 1 {
		return fsapi.Invalid
	}
	if len(b.Frames) == 0 || len(b.Frames) > maxFrames || len(b.Selection.Effects) == 0 || len(b.Selection.Effects) > maxFrames || len(b.Data) > maxObjects || len(b.Selection.Proof) == 0 || len(b.Selection.Proof) > 1<<20 || len(b.Catalog) == 0 || int64(len(b.Catalog)) > maxCatalog {
		return fsapi.NoSpace
	}
	// Charge every vector element even if its byte slice is empty. This bounds
	// nested slice overhead independently of a caller's larger frame/object caps.
	total := int64(len(b.Data)) + int64(len(b.Frames)) + int64(len(b.Selection.Effects))
	add := func(n int64) error {
		if n > c.MaxArtifactBytes-total {
			return fsapi.NoSpace
		}
		total += n
		return nil
	}
	if err := add(int64(len(b.Catalog))); err != nil {
		return err
	}
	if err := add(int64(len(b.Selection.Proof))); err != nil {
		return err
	}
	var sourceBytes, payloadBytes int64
	for _, raw := range b.Frames {
		n := int64(len(raw))
		if n == 0 || n > record.MaxFrame || n > maxSource-sourceBytes {
			return fsapi.NoSpace
		}
		sourceBytes += n
		if err := add(n); err != nil {
			return err
		}
	}
	for _, raw := range b.Data {
		n := int64(len(raw))
		if n > c.MaxPayloadBytes-payloadBytes {
			return fsapi.NoSpace
		}
		payloadBytes += n
		if err := add(n); err != nil {
			return err
		}
	}
	return nil
}

func load(ctx context.Context, root storage.Filesystem, c Config) (Manifest, apply.Baseline, error) {
	raw, err := read(ctx, root, "CURRENT", c, 4096)
	if err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	var p pointer
	if err = decode(raw, &p); err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	if p.Version != 1 || !p.ID.Valid() || p.Bytes < 1 || p.Bytes > 2<<20 {
		return Manifest{}, apply.Baseline{}, fsapi.Invalid
	}
	raw, err = read(ctx, root, manifestName(p.ID), c, 2<<20)
	if err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	if int64(len(raw)) != p.Bytes || sha256.Sum256(raw) != p.SHA256 {
		return Manifest{}, apply.Baseline{}, fsapi.IO
	}
	var m Manifest
	if err = decode(raw, &m); err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	if m.ID != p.ID || m.BaselineBytes < 1 || m.BaselineBytes > c.MaxArtifactBytes {
		return Manifest{}, apply.Baseline{}, fsapi.Invalid
	}
	raw, err = read(ctx, root, baselineName(p.ID), c, c.MaxArtifactBytes)
	if err != nil {
		return Manifest{}, apply.Baseline{}, err
	}
	if int64(len(raw)) != m.BaselineBytes || sha256.Sum256(raw) != m.BaselineSHA256 {
		return Manifest{}, apply.Baseline{}, fsapi.IO
	}
	b, err := apply.DecodeBaseline(raw)
	return m, b, err
}

func publishPointer(ctx context.Context, root storage.Filesystem, target string, id fsapi.UUID, manifest []byte, c Config) error {
	raw, err := encode(pointer{1, id, int64(len(manifest)), sha256.Sum256(manifest)})
	if err != nil {
		return err
	}
	temp := target + ".tmp-" + id.String()
	if err = write(ctx, root, temp, raw); err != nil {
		return err
	}
	if err = hit(ctx, c, BeforePublish); err != nil {
		return err
	}
	if err = root.Rename(ctx, temp, target); err != nil {
		return err
	}
	if err = hit(ctx, c, AfterPublish); err != nil {
		return err
	}
	if err = root.SyncDir(ctx, "."); err != nil {
		return err
	}
	return hit(ctx, c, Published)
}

func read(ctx context.Context, root storage.Filesystem, name string, c Config, maxBytes int64) ([]byte, error) {
	info, err := root.Stat(ctx, name)
	if err != nil {
		return nil, err
	}
	r, err := root.Stat(ctx, ".")
	if err != nil {
		return nil, err
	}
	if info.Mode != 0o600 || info.Links != 1 || info.UID != c.Apply.Storage.OwnerUID || info.Identity.Device != r.Identity.Device || info.Size < 1 || info.Size > maxBytes {
		return nil, fsapi.Invalid
	}
	f, err := root.Open(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw := make([]byte, info.Size)
	n, err := f.ReadAt(ctx, raw, 0)
	if err != nil {
		return nil, err
	}
	if n != len(raw) {
		return nil, io.ErrUnexpectedEOF
	}
	return raw, nil
}

func write(ctx context.Context, root storage.Filesystem, name string, raw []byte) error {
	f, err := root.Open(ctx, name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	n, err := f.WriteAt(ctx, raw, 0)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	if err = f.Sync(ctx); err != nil {
		return err
	}
	return root.SyncDir(ctx, ".")
}

func immutable(ctx context.Context, root storage.Filesystem, name string, raw []byte, c Config) error {
	err := write(ctx, root, name, raw)
	if !errors.Is(err, fs.ErrExist) {
		return err
	}
	old, err := read(ctx, root, name, c, 2<<20)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, raw) {
		return fsapi.Stale
	}
	if err = syncFile(ctx, root, name); err != nil {
		return err
	}
	return root.SyncDir(ctx, ".")
}

func syncFile(ctx context.Context, root storage.Filesystem, name string) error {
	f, err := root.Open(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync(ctx)
}

func encode(v any) ([]byte, error) { return json.Marshal(v) }

func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	canonical, err := encode(v)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, raw) {
		return fsapi.Invalid
	}
	return nil
}
