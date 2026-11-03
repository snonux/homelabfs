package storage

import (
	"context"
	"math"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// FutureWrite reserves one possible write receipt and complete sparse payload
// growth to End. It describes sizing obligations, never a selected operation.
type FutureWrite struct {
	Object fsapi.ObjectID
	End    int64
}

// ProjectedSourceBytes checks the exact known primitive projection plus bounded
// outstanding future writes/pins without allocating or copying payload bytes.
// The returned bound includes the source snapshot's eight-byte catalog/payload
// length prefixes. Future scalar widths/receipts/pins are conservative maxima;
// none of these sizing values is a journal record, claim or source proof.
func ProjectedSourceBytes(ctx context.Context, image Image, ops []Operation, future []FutureWrite, extraPins int, limits Limits, maxBytes int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if extraPins < 0 || extraPins > 1 || len(future) > 64 {
		return 0, fsapi.Invalid
	}
	if err := projectionBounds(Snapshot{Image: image}, ops, limits, maxBytes); err != nil {
		return 0, err
	}
	if _, err := framedImageBytes(image, limits, maxBytes); err != nil {
		return 0, err
	}
	catalog, err := projectionCatalog(image, limits, false)
	if err != nil {
		return 0, err
	}
	for _, op := range ops {
		if err = projectOperation(catalog, nil, op, maxBytes); err != nil {
			return 0, err
		}
	}
	projected := projectionImage(catalog)
	projected.Objects = slices.Clone(projected.Objects)
	growth := map[fsapi.ObjectID]int64{}
	for _, write := range future {
		if write.End < 0 || write.End > maxBytes {
			return 0, fsapi.NoSpace
		}
		object, ok := catalog.objects[write.Object]
		if !ok || object.Attr.Kind != fsapi.Regular {
			return 0, fsapi.Stale
		}
		growth[write.Object] = max(growth[write.Object], write.End)
	}
	for i := range projected.Objects {
		object := &projected.Objects[i]
		end, ok := growth[object.Attr.Object]
		if !ok {
			continue
		}
		object.Attr.Size = max(object.Attr.Size, end)
		object.Attr.Revision = math.MaxUint64
		object.Attr.UID = math.MaxUint32
		object.Attr.GID = math.MaxUint32
		object.Attr.Nlink = math.MaxUint64
		object.LedgerRevision = math.MaxUint64
		object.Attr.Atime = fsapi.Timestamp{Seconds: math.MinInt64, Nanoseconds: 999999999}
		object.Attr.Mtime = object.Attr.Atime
		object.Attr.Ctime = object.Attr.Atime
	}
	for i := range projected.Payloads {
		if end, ok := growth[projected.Payloads[i].Object]; ok {
			projected.Payloads[i].Size = max(projected.Payloads[i].Size, end)
		}
	}
	effects := len(future) + extraPins
	if effects > catalog.config.Limits.Operations-len(projected.Receipts) || extraPins > catalog.config.Limits.Pins-len(projected.Pins) {
		return 0, fsapi.NoSpace
	}
	// Fixed-width identities/digests here are representation sizing only. Unknown
	// future bytes are never materialized, and no ordinary Claim is constructed.
	var maximum fsapi.UUID
	for i := range maximum {
		maximum[i] = 255
	}
	for range effects {
		projected.Receipts = append(projected.Receipts, Receipt{Sequence: math.MaxUint64, ID: maximum})
	}
	for range extraPins {
		projected.Pins = append(projected.Pins, Pin{ID: maximum, Object: projected.Root, Generation: maximum, Incarnation: maximum, Active: false})
	}
	if effects > 0 {
		projected.Position.Sequence = math.MaxUint64
	}
	return framedImageBytes(projected, limits, maxBytes)
}

func framedImageBytes(image Image, limits Limits, maxBytes int64) (int64, error) {
	catalog, err := imageBytes(image, limits, maxBytes)
	if err != nil {
		return 0, err
	}
	total := catalog + 8
	if total > maxBytes {
		return 0, fsapi.NoSpace
	}
	for _, payload := range image.Payloads {
		if payload.Size < 0 || total > maxBytes-8 || payload.Size > maxBytes-total-8 {
			return 0, fsapi.NoSpace
		}
		total += 8 + payload.Size
	}
	return total, nil
}
