package fsservice

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/snonux/homelabfs/internal/storage"
)

// ObservableDigest returns a digest of the client-observable state of the
// captured cut (task e84): its journal incarnation, epoch and serving
// generation, the observable catalog (storage.ObservableCatalog: everything
// but applied position, handle pins and duplicate-operation receipts) and
// every payload byte. Two cuts have equal digests exactly when they show a
// client the same objects, attributes, names, tombstones, retired
// allocations and data. A cut that moved only by HandlePin and HandleRelease
// records (a pinning LOOKUP or OPEN of a non-root object, a FORGET or final
// DESCRIPTION_RELEASE that reclaims nothing) keeps the digest of the cut
// before it; any mutation or Reclaim changes it.
//
// It reads only the captured State bytes laid out by captureState
// (length-prefixed catalog, then every payload), so it also works for cuts
// reloaded from a ledger. Malformed State returns false.
func (s ControlledViewSource) ObservableDigest() (string, bool) {
	catalog, payloads, ok := splitState(s.State)
	if !ok {
		return "", false
	}
	observable, ok := storage.ObservableCatalog(catalog)
	if !ok {
		return "", false
	}
	h := sha256.New()
	for _, part := range [][]byte{[]byte(s.Journal.Incarnation.String()), []byte(s.Visibility.String()), observable, payloads} {
		_ = binary.Write(h, binary.BigEndian, uint64(len(part)))
		_, _ = h.Write(part)
	}
	_ = binary.Write(h, binary.BigEndian, s.Through.Position.Epoch)
	return hex.EncodeToString(h.Sum(nil)), true
}

// ObservablyEqual reports whether both cuts have the same ObservableDigest.
func (s ControlledViewSource) ObservablyEqual(o ControlledViewSource) bool {
	left, ok := s.ObservableDigest()
	if !ok {
		return false
	}
	right, ok := o.ObservableDigest()
	return ok && left == right
}

// splitState splits captureState's bytes into the catalog and the payload
// section that follows it.
func splitState(state []byte) (catalog, payloads []byte, ok bool) {
	if len(state) < 8 {
		return nil, nil, false
	}
	n := binary.BigEndian.Uint64(state[:8])
	if n > uint64(len(state)-8) {
		return nil, nil, false
	}
	return state[8 : 8+n], state[8+n:], true
}
