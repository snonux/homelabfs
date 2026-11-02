package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Project computes an independently owned logical afterimage without touching
// backing storage. Inputs must already be selected by the caller's protocol;
// this function validates primitive, revision, allocation, alias and pin rules,
// but supplies no commitment, source-retention or publication authority. Holes
// are represented by zero bytes and count against maxBytes before allocation.
func Project(ctx context.Context, before Snapshot, operations []Operation, limits Limits, maxBytes int64) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if err := projectionBounds(before, operations, limits, maxBytes); err != nil {
		return Snapshot{}, err
	}
	if err := ValidateSnapshot(before, limits, maxBytes); err != nil {
		return Snapshot{}, err
	}
	s, err := projectionCatalog(before.Image, limits, true)
	if err != nil {
		return Snapshot{}, err
	}
	payloads := make(map[fsapi.ObjectID][]byte, len(before.Data))
	for i, p := range before.Image.Payloads {
		payloads[p.Object] = slices.Clone(before.Data[i])
	}
	for _, supplied := range operations {
		if err = ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		op := cloneOperation(supplied)
		if err = projectOperation(s, payloads, op, maxBytes); err != nil {
			return Snapshot{}, err
		}
	}
	out := projectionSnapshot(s, payloads)
	if err = ValidateSnapshot(out, limits, maxBytes); err != nil {
		return Snapshot{}, err
	}
	return out, nil
}

// ProjectedBytes validates the same semantic history as Project and counts its
// exact final catalog and logical payload bytes before any snapshot/payload copy.
// It borrows immutable baseline fields; operation afterimages are bounded by
// projectionBounds before the shared catalog validator takes ownership of them.
func ProjectedBytes(ctx context.Context, before Snapshot, operations []Operation, limits Limits, maxBytes int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if _, err := SnapshotBytes(before, limits, maxBytes); err != nil {
		return 0, err
	}
	if err := projectionBounds(before, operations, limits, maxBytes); err != nil {
		return 0, err
	}
	if err := ValidateSnapshot(before, limits, maxBytes); err != nil {
		return 0, err
	}
	s, err := projectionCatalog(before.Image, limits, false)
	if err != nil {
		return 0, err
	}
	for _, op := range operations {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		if err = projectOperation(s, nil, op, maxBytes); err != nil {
			return 0, err
		}
	}
	image := projectionImage(s)
	total, err := imageBytes(image, limits, maxBytes)
	if err != nil {
		return 0, err
	}
	for _, p := range image.Payloads {
		if p.Size > maxBytes-total {
			return 0, fsapi.NoSpace
		}
		total += p.Size
	}
	return total, nil
}

func projectionCatalog(image Image, limits Limits, owned bool) (*Store, error) {
	s, err := newStore(noopFS{}, noopFS{}, Config{Volume: image.Volume, Root: image.Root, Limits: limits})
	if err != nil {
		return nil, err
	}
	s.position = image.Position
	for _, o := range image.Objects {
		if owned {
			o = cloneObject(o)
		}
		s.objects[o.Attr.Object] = o
	}
	for _, e := range image.Entries {
		if owned {
			e = cloneEntry(e)
		}
		s.entries[entryKey{e.Parent, e.Name}] = e
	}
	for _, p := range image.Pins {
		s.pins[p.ID] = p
	}
	for _, d := range image.Retired {
		s.retired[d.Object] = d
	}
	for _, r := range image.Receipts {
		s.receipts[r.Sequence] = r
	}
	return s, nil
}

func projectOperation(s *Store, payloads map[fsapi.ObjectID][]byte, op Operation, maxBytes int64) error {
	raw, err := operationBytes(op)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if old, ok := s.receipts[op.Position.Sequence]; ok {
		if old.ID == op.ID && old.SHA256 == digest {
			return nil
		}
		return fmt.Errorf("%w: conflicting logical duplicate", fsapi.Stale)
	}
	if !op.ID.Valid() || !op.Object.Valid() || !validPosition(op.Position, s.config.Volume) || !sameHistory(op.Position, s.position) || op.Kind < Create || op.Kind > Reclaim {
		return fsapi.Invalid
	}
	if s.position.Sequence == math.MaxUint64 || op.Position.Sequence != s.position.Sequence+1 {
		return fsapi.Stale
	}
	for _, old := range s.receipts {
		if old.ID == op.ID {
			return fsapi.Stale
		}
	}
	if _, err = s.operationHomes(op); err != nil {
		return err
	}
	next := s.copyCatalog()
	if err = next.installCatalog(op); err != nil {
		return err
	}
	if err = next.validatePrimitive(s, op); err != nil {
		return err
	}
	if err = next.validateCatalog(); err != nil {
		return err
	}
	if len(s.receipts) >= s.config.Limits.Operations {
		return fsapi.NoSpace
	}
	var total int64
	for _, o := range next.objects {
		if o.Attr.Kind == fsapi.Regular {
			if o.Attr.Size > maxBytes-total {
				return fsapi.NoSpace
			}
			total += o.Attr.Size
		}
	}
	// A nil payload map is the metadata-only resource preflight. Catalog and
	// primitive validation above remain identical to the owning projection.
	if payloads != nil {
		projectPayloads(next, payloads, op)
	}
	s.objects, s.entries, s.pins, s.retired = next.objects, next.entries, next.pins, next.retired
	s.receipts[op.Position.Sequence] = Receipt{Sequence: op.Position.Sequence, ID: op.ID, SHA256: digest}
	s.position = op.Position
	return nil
}

func projectPayloads(next *Store, payloads map[fsapi.ObjectID][]byte, op Operation) {
	for _, c := range op.Objects {
		if c.Before == 0 && c.After.Attr.Kind == fsapi.Regular {
			payloads[c.After.Attr.Object] = []byte{}
		}
	}
	if op.Kind == Write || op.Kind == Truncate {
		old := payloads[op.Object]
		size := next.objects[op.Object].Attr.Size
		data := make([]byte, int(size))
		copy(data, old)
		if op.Kind == Write {
			copy(data[op.Offset:], op.Data)
		}
		payloads[op.Object] = data
	}
	if op.Kind == Reclaim {
		delete(payloads, op.Object)
	}
}

func projectionSnapshot(s *Store, data map[fsapi.ObjectID][]byte) Snapshot {
	image := projectionImage(s)
	for i := range image.Objects {
		image.Objects[i] = cloneObject(image.Objects[i])
	}
	for i := range image.Entries {
		image.Entries[i] = cloneEntry(image.Entries[i])
	}
	for i := range image.Payloads {
		image.Payloads[i].SHA256 = sha256.Sum256(data[image.Payloads[i].Object])
	}
	sortImage(&image)
	out := Snapshot{Image: image}
	for _, p := range image.Payloads {
		out.Data = append(out.Data, data[p.Object])
	}
	return out
}

func projectionImage(s *Store) Image {
	image := Image{Version: 1, Volume: s.config.Volume, Root: s.config.Root, Position: s.position}
	for _, o := range s.objects {
		image.Objects = append(image.Objects, o)
		if o.Attr.Kind == fsapi.Regular {
			// Hash content has fixed encoded length; the owning projection fills
			// the actual digest after preflight, without allocating holes here.
			image.Payloads = append(image.Payloads, Payload{Object: o.Attr.Object, Size: o.Attr.Size})
		}
	}
	for _, e := range s.entries {
		image.Entries = append(image.Entries, e)
	}
	for _, p := range s.pins {
		image.Pins = append(image.Pins, p)
	}
	for _, d := range s.retired {
		image.Retired = append(image.Retired, d)
	}
	for _, r := range s.receipts {
		image.Receipts = append(image.Receipts, r)
	}
	return image
}

// CopyImage returns an independent catalog image without I/O or authority.
func CopyImage(image Image) Image {
	image.Objects = slices.Clone(image.Objects)
	for i := range image.Objects {
		image.Objects[i] = cloneObject(image.Objects[i])
	}
	image.Entries = slices.Clone(image.Entries)
	for i := range image.Entries {
		image.Entries[i] = cloneEntry(image.Entries[i])
	}
	image.Pins = slices.Clone(image.Pins)
	image.Retired = slices.Clone(image.Retired)
	image.Receipts = slices.Clone(image.Receipts)
	image.Payloads = slices.Clone(image.Payloads)
	return image
}

// CopyOperation returns independently owned redo bytes and afterimages.
func CopyOperation(op Operation) Operation { return cloneOperation(op) }

func projectionBounds(before Snapshot, operations []Operation, limits Limits, maxBytes int64) error {
	if maxBytes < 1 || maxBytes > 1<<30 {
		return fsapi.Invalid
	}
	normalized, err := newStore(noopFS{}, noopFS{}, Config{Volume: before.Image.Volume, Root: before.Image.Root, Limits: limits})
	if err != nil {
		return err
	}
	limits = normalized.config.Limits
	image := before.Image
	if len(operations) > limits.Operations || len(image.Objects)+len(image.Retired) > limits.Objects || len(image.Entries) > limits.Entries || len(image.Receipts) > limits.Operations || len(image.Pins) > limits.Pins || len(image.Payloads) > limits.Objects || len(before.Data) > limits.Objects {
		return fsapi.NoSpace
	}
	var payload int64
	for _, data := range before.Data {
		if int64(len(data)) > maxBytes-payload {
			return fsapi.NoSpace
		}
		payload += int64(len(data))
	}
	var refs int
	for _, object := range image.Objects {
		if len(object.Text) > 4096 || len(object.References) > limits.Entries-refs {
			return fsapi.NoSpace
		}
		refs += len(object.References)
	}
	for _, entry := range image.Entries {
		if len(entry.Name) > 255 {
			return fsapi.NameTooLong
		}
	}
	var cost int64
	for _, op := range operations {
		if len(op.Objects) > limits.Objects || len(op.Entries) > limits.Entries || len(op.Pins) > limits.Pins || len(op.Participants) > 64 || int64(len(op.Data)) > maxBytes {
			return fsapi.NoSpace
		}
		cost += int64(len(op.Data)) + int64(len(op.Objects))*512 + int64(len(op.Entries))*512 + int64(len(op.Pins))*256
		for _, c := range op.Objects {
			if len(c.After.Text) > 4096 || len(c.After.References) > limits.Entries {
				return fsapi.NoSpace
			}
			cost += int64(len(c.After.Text)) + int64(len(c.After.References))*512
		}
		if cost > limits.FileBytes {
			return fsapi.NoSpace
		}
	}
	return nil
}

// EncodeOperation returns deterministic private primitive bytes, preserving raw
// names. These are the existing semantic receipt bytes, not a journal envelope
// or a distributed commitment certificate.
func EncodeOperation(op Operation) ([]byte, error) { return operationBytes(op) }

// DecodeOperation accepts only the exact canonical private primitive encoding.
// Duplicate/unknown fields, alternate encodings and trailing input fail before
// an operation can be used. Semantic validation still belongs to Project/Apply.
func DecodeOperation(raw []byte, maxBytes int64) (Operation, error) {
	if maxBytes < 1 || int64(len(raw)) > maxBytes {
		return Operation{}, fsapi.NoSpace
	}
	var op Operation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&op); err != nil {
		return Operation{}, err
	}
	for i := range op.Objects {
		for j := range op.Objects[i].After.References {
			r := &op.Objects[i].After.References[j]
			name, err := base64.StdEncoding.Strict().DecodeString(r.Name)
			if err != nil {
				return Operation{}, fsapi.Invalid
			}
			r.Name = string(name)
		}
	}
	for i := range op.Entries {
		for _, e := range []*Entry{&op.Entries[i].Before, &op.Entries[i].After} {
			name, err := base64.StdEncoding.Strict().DecodeString(e.Name)
			if err != nil {
				return Operation{}, fsapi.Invalid
			}
			e.Name = string(name)
		}
	}
	encoded, err := operationBytes(op)
	if err != nil {
		return Operation{}, err
	}
	if !bytes.Equal(encoded, raw) {
		return Operation{}, fsapi.Invalid
	}
	return op, nil
}
