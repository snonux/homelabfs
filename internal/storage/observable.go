package storage

import (
	"encoding/json"
)

// ObservableCatalog returns the part of an EncodeImage catalog a filesystem
// client can observe (task e84), in a canonical encoding: the volume, root,
// every object (attributes, symlink text, reference ledger, parent), every
// entry including tombstones, every retired allocation and every payload
// identity. It drops exactly three parts of the image:
//
//   - Position: the applied journal position advances with every record;
//   - Pins: handle-pin lifetime state (HandlePin/HandleRelease), which no
//     read, lookup or lease observes. Validation (validatePrimitive) lets a
//     HandlePin or HandleRelease change only one pin, never an object or
//     entry, so a cut move made only of them differs only here;
//   - Receipts: duplicate-operation receipts (one per applied record), which
//     no client operation reads.
//
// A Reclaim removes an object and adds a retired allocation, so it changes
// the result; so does every mutation (each changes an object, an entry or a
// payload). The input is trusted bytes this process encoded (EncodeImage is
// deterministic and sorted), so equal results mean equal observable state.
// Undecodable input or a foreign magic returns false, which keeps callers on
// their conservative path.
func ObservableCatalog(raw []byte) ([]byte, bool) {
	var w imageWire
	if json.Unmarshal(raw, &w) != nil || w.Magic != "HOMELABFS-CATALOG" {
		return nil, false
	}
	w.Position = positionWire{}
	w.Pins = nil
	w.Receipts = nil
	out, err := json.Marshal(w)
	return out, err == nil
}
