package storage

import (
	"encoding/hex"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type imageCounter struct {
	total, max int64
}

// SnapshotBytes counts the exact canonical catalog encoding and supplied
// payload bytes without copying the catalog, encoding variable-sized fields or
// allocating payloads. It checks structural resource bounds, not semantic image
// validity; Project and ValidateSnapshot remain responsible for that validation.
func SnapshotBytes(snapshot Snapshot, limits Limits, maxBytes int64) (int64, error) {
	if maxBytes < 1 {
		return 0, fsapi.NoSpace
	}
	if err := projectionBounds(snapshot, nil, limits, maxBytes); err != nil {
		return 0, err
	}
	total, err := imageBytes(snapshot.Image, limits, maxBytes)
	if err != nil {
		return 0, err
	}
	for _, data := range snapshot.Data {
		if int64(len(data)) > maxBytes-total {
			return 0, fsapi.NoSpace
		}
		total += int64(len(data))
	}
	return total, nil
}

func imageHeader(image Image) imageWire {
	return imageWire{Magic: "HOMELABFS-CATALOG", Major: 1, Minor: 0, Volume: image.Volume.String(), Root: toID(image.Root), Position: positionWire{image.Position.Volume.String(), image.Position.Generation.String(), image.Position.Configuration.String(), image.Position.Epoch, image.Position.Sequence}, Objects: []objectWire{}, Entries: []entryWire{}, Pins: []pinWire{}, Retired: []descriptorWire{}, Receipts: []receiptWire{}, Payloads: []payloadWire{}}
}
func objectHeader(o Object) objectWire {
	a := o.Attr
	wire := objectWire{Descriptor: toDescriptor(o.Descriptor), Attr: attrWire{toID(a.Object), a.Kind, a.UID, a.GID, a.Mode, a.Size, a.Nlink, a.Revision, a.Atime, a.Mtime, a.Ctime}, Ledger: o.LedgerRevision, References: []referenceWire{}}
	if o.Parent != nil {
		wire.Parent = &parentWire{o.Parent.Revision, toID(o.Parent.Object), o.Parent.EntryRevision}
	}
	return wire
}
func referenceHeader(r Reference) referenceWire {
	return referenceWire{Parent: toID(r.Parent), Revision: r.Revision}
}
func entryHeader(e Entry) entryWire {
	wire := entryWire{Parent: toID(e.Parent), Revision: e.Revision}
	if e.Target != nil {
		id := toID(*e.Target)
		wire.Target = &id
	}
	return wire
}

func imageBytes(image Image, limits Limits, maxBytes int64) (int64, error) {
	fileBytes := limits.FileBytes
	if fileBytes == 0 {
		fileBytes = 16 << 20
	}
	counter := imageCounter{max: min(maxBytes, fileBytes)}
	// EncodeImage appends one newline to the JSON catalog.
	if err := counter.record(imageHeader(image), 1); err != nil {
		return 0, err
	}
	for i, o := range image.Objects {
		if err := counter.record(objectHeader(o), arrayComma(i)); err != nil {
			return 0, err
		}
		if err := counter.base64(len(o.Text)); err != nil {
			return 0, err
		}
		for j, ref := range o.References {
			if err := counter.record(referenceHeader(ref), arrayComma(j)); err != nil {
				return 0, err
			}
			if err := counter.base64(len(ref.Name)); err != nil {
				return 0, err
			}
		}
	}
	for i, e := range image.Entries {
		if err := counter.record(entryHeader(e), arrayComma(i)); err != nil {
			return 0, err
		}
		if err := counter.base64(len(e.Name)); err != nil {
			return 0, err
		}
	}
	for i, p := range image.Pins {
		if err := counter.record(pinWire{p.ID.String(), toID(p.Object), p.Generation.String(), p.Incarnation.String(), p.Active}, arrayComma(i)); err != nil {
			return 0, err
		}
	}
	for i, d := range image.Retired {
		if err := counter.record(toDescriptor(d), arrayComma(i)); err != nil {
			return 0, err
		}
	}
	for i, r := range image.Receipts {
		if err := counter.record(receiptWire{r.Sequence, r.ID.String(), hex.EncodeToString(r.SHA256[:])}, arrayComma(i)); err != nil {
			return 0, err
		}
	}
	for i, p := range image.Payloads {
		if err := counter.record(payloadWire{toID(p.Object), p.Size, hex.EncodeToString(p.SHA256[:])}, arrayComma(i)); err != nil {
			return 0, err
		}
	}
	return counter.total, nil
}
func (c *imageCounter) record(value any, extra int64) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.add(int64(len(raw)) + extra)
}
func (c *imageCounter) base64(length int) error {
	// All variable fields use padded base64, whose alphabet needs no JSON
	// escaping. Check the input first so arithmetic also stays bounded.
	if int64(length) > c.max-c.total {
		return fsapi.NoSpace
	}
	return c.add((int64(length) + 2) / 3 * 4)
}
func (c *imageCounter) add(n int64) error {
	if n > c.max-c.total {
		return fsapi.NoSpace
	}
	c.total += n
	return nil
}
func arrayComma(index int) int64 {
	if index > 0 {
		return 1
	}
	return 0
}
