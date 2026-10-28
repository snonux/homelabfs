package apply

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

const progressSize = 242

type progressRecord struct {
	workspace    fsapi.UUID
	index, slots uint64
	source       record.FrameRef
	prefix       [32]byte
	imageBytes   uint64
	imageHash    [32]byte
}

func checkProgress(ctx context.Context, f storage.Filesystem, c Config, h history) (uint64, error) {
	if f == nil {
		return 0, fsapi.Invalid
	}
	root, err := f.Stat(ctx, ".")
	if err != nil {
		return 0, err
	}
	if root.Mode != fs.ModeDir|0o700 || root.UID != c.Storage.OwnerUID {
		return 0, fsapi.Permission
	}
	raw, err := readFile(ctx, f, "APPLIED", c.Storage.OwnerUID, progressSize)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	p, err := decodeProgress(raw)
	if err != nil {
		return 0, err
	}
	if p.workspace == c.Workspace || p.index >= uint64(len(h.selected)) || p.source != h.entries[h.selected[p.index]].Reference || p.prefix != h.prefixes[p.index] {
		return 0, fmt.Errorf("%w: applied history conflicts with selection or workspace reused", fsapi.Stale)
	}
	image, err := readFile(ctx, f, imageName(p.workspace, p.index), c.Storage.OwnerUID, maxImageBytes(c))
	if err != nil {
		return 0, err
	}
	if uint64(len(image)) != p.imageBytes || sha256.Sum256(image) != p.imageHash {
		return 0, fmt.Errorf("%w: applied image hash", fsapi.IO)
	}
	decoded, err := storage.DecodeImage(image, c.Storage.Limits)
	if err != nil {
		return 0, err
	}
	if decoded.Volume != c.Storage.Volume || decoded.Root != c.Storage.Root || decoded.Position.Sequence != p.slots || !sameHistory(decoded.Position, p.source.Position) {
		return 0, fsapi.Stale
	}
	return p.index, nil
}
func publish(ctx context.Context, f storage.Filesystem, c Config, source record.FrameRef, index, slots uint64, prefix [32]byte, image storage.Image) (int64, error) {
	raw, err := storage.EncodeImage(image, c.Storage.Limits)
	if err != nil {
		return 0, err
	}
	p := progressRecord{workspace: c.Workspace, index: index, slots: slots, source: source, prefix: prefix, imageBytes: uint64(len(raw)), imageHash: sha256.Sum256(raw)}
	name := imageName(c.Workspace, index)
	if err = writeFile(ctx, f, name, raw); err != nil {
		return 0, err
	}
	if err = f.SyncDir(ctx, "."); err != nil {
		return 0, err
	}
	if err = hit(ctx, c, ImageDurable); err != nil {
		return 0, err
	}
	tmp := fmt.Sprintf("APPLIED.tmp-%s-%020d", c.Workspace.String(), index)
	if err = writeFile(ctx, f, tmp, encodeProgress(p)); err != nil {
		return 0, err
	}
	if err = hit(ctx, c, BeforePublish); err != nil {
		return 0, err
	}
	if err = f.Rename(ctx, tmp, "APPLIED"); err != nil {
		return 0, err
	}
	if err = hit(ctx, c, AfterRename); err != nil {
		return 0, err
	}
	if err = f.SyncDir(ctx, "."); err != nil {
		return 0, err
	}
	if err = hit(ctx, c, AppliedDurable); err != nil {
		return 0, err
	}
	return int64(len(raw)) + progressSize, nil
}
func encodeProgress(p progressRecord) []byte {
	raw := append([]byte("HFSAPPL\x00"), 0, 1)
	raw = append(raw, p.workspace[:]...)
	raw = binary.BigEndian.AppendUint64(raw, p.index)
	raw = binary.BigEndian.AppendUint64(raw, p.slots)
	pos := p.source.Position
	for _, id := range []fsapi.UUID{pos.Volume, pos.Generation, pos.Configuration} {
		raw = append(raw, id[:]...)
	}
	raw = binary.BigEndian.AppendUint64(raw, pos.Epoch)
	raw = binary.BigEndian.AppendUint64(raw, pos.Sequence)
	raw = append(raw, p.source.SHA256[:]...)
	raw = append(raw, p.prefix[:]...)
	raw = binary.BigEndian.AppendUint64(raw, p.imageBytes)
	raw = append(raw, p.imageHash[:]...)
	sum := sha256.Sum256(raw)
	return append(raw, sum[:]...)
}
func decodeProgress(raw []byte) (progressRecord, error) {
	var p progressRecord
	if len(raw) != progressSize || !bytes.Equal(raw[:10], []byte("HFSAPPL\x00\x00\x01")) {
		return p, fsapi.Invalid
	}
	sum := sha256.Sum256(raw[:progressSize-32])
	if !bytes.Equal(sum[:], raw[progressSize-32:]) {
		return p, fsapi.IO
	}
	off := 10
	uuid := func(out *fsapi.UUID) { copy(out[:], raw[off:off+16]); off += 16 }
	number := func() uint64 { n := binary.BigEndian.Uint64(raw[off:]); off += 8; return n }
	uuid(&p.workspace)
	p.index = number()
	p.slots = number()
	uuid(&p.source.Position.Volume)
	uuid(&p.source.Position.Generation)
	uuid(&p.source.Position.Configuration)
	p.source.Position.Epoch = number()
	p.source.Position.Sequence = number()
	p.source.StreamKind = 1
	copy(p.source.SHA256[:], raw[off:off+32])
	off += 32
	copy(p.prefix[:], raw[off:off+32])
	off += 32
	p.imageBytes = number()
	copy(p.imageHash[:], raw[off:off+32])
	if !p.workspace.Valid() || !p.source.Position.Volume.Valid() || !p.source.Position.Generation.Valid() || !p.source.Position.Configuration.Valid() || p.source.Position.Epoch == 0 || p.source.Position.Sequence == 0 || p.slots > p.index || p.imageBytes == 0 {
		return progressRecord{}, fsapi.Invalid
	}
	return p, nil
}
func imageName(id fsapi.UUID, index uint64) string {
	return fmt.Sprintf("image-%s-%020d.json", id.String(), index)
}
func maxImageBytes(c Config) int64 {
	if c.Storage.Limits.FileBytes > 0 {
		return c.Storage.Limits.FileBytes
	}
	return 16 << 20
}
func readFile(ctx context.Context, f storage.Filesystem, name string, owner uint32, max int64) ([]byte, error) {
	info, err := f.Stat(ctx, name)
	if err != nil {
		return nil, err
	}
	root, err := f.Stat(ctx, ".")
	if err != nil {
		return nil, err
	}
	if info.Mode != 0o600 || info.UID != owner || info.Links != 1 || info.Identity.Device != root.Identity.Device || info.Size < 1 || info.Size > max {
		return nil, fsapi.Invalid
	}
	file, err := f.Open(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw := make([]byte, int(info.Size))
	n, err := file.ReadAt(ctx, raw, 0)
	if err != nil {
		return nil, err
	}
	if n != len(raw) {
		return nil, io.ErrUnexpectedEOF
	}
	return raw, nil
}
func writeFile(ctx context.Context, f storage.Filesystem, name string, raw []byte) error {
	file, err := f.Open(ctx, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	n, err := file.WriteAt(ctx, raw, 0)
	if err != nil {
		return err
	}
	if n != len(raw) {
		return io.ErrShortWrite
	}
	if err = file.Sync(ctx); err != nil {
		return err
	}
	return file.Close()
}
