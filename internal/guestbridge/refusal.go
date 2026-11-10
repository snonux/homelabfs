package guestbridge

import (
	"encoding/json"
	"errors"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Cleanup of permanently refused cells (task n64).
//
// The lib9p exporter (tools/integration/lib9p/src/hfs9p.c, _permanent_refusal)
// fails some refused native writes back to the guest instead of pinning its
// single writer on bytes that will never be admitted. Since w64 only a
// refusal of the final chunk qualifies, the one that completes the cell and
// reaches validation here; a refusal of an earlier chunk keeps the candidate
// pinned, because this transport still holds the chunks it accepted:
//
//   - a donor send (reclaim workspace, e64) on EINVAL, or on ESTALE or EACCES
//     of a first attempt. Its native number was not spent: the exporter keeps
//     its high-water and offers the same number to the next cell.
//   - a non-donor cell (p64) on ESTALE of a first attempt, or on EINVAL after
//     an earlier chunk of the same write was accepted. Its number was spent at
//     the first chunk: the exporter moves its high-water past it, never writes
//     it again and no longer counts its credit.
//
// The exporter cannot tell an authentication ESTALE or credential EACCES of
// the final chunk, which comes before validation and leaves the cell here,
// from a validation refusal; that residual race stays one chunk wide.
//
// Before n64 the runtime kept both kinds of refused cell for good. A refused
// donor frame stayed in the single reclaim workspace, so every later new cell
// of any session got WouldBlock. A refused numbered cell stayed incomplete
// with its credit: k such cells made a fresh cell's allocation WouldBlock,
// which pins the exporter's writer for good (a SESSION_RETIRE cannot pass
// it), and for minor 4 its low guest watermark kept every later marker of its
// session from being pruned, so fresh minor-4 admission closed for every
// session after about mappingRecentLimit more admitted cells.
//
// discardRefused mirrors the exporter after its fail-back. It only acts on a
// complete cell whose validation failed (acceptCell), never on a refused chunk
// of an incomplete cell, a frame or numbering refusal, or WouldBlock, so an
// uncertain or transient outcome stays exactly as it was and an identical
// replay revalidates the kept cell. The exporter fails back only on the final
// chunk (w64) for the same reason. A replay can also revalidate a complete,
// still unadmitted cell from an earlier chunk (storeChunk) and be discarded
// here; the exporter keeps that replay pinned (see refusedResend).

// pinnedByExporter reports whether the exporter never fails the decoded
// command back and keeps it pinned on any refusal (e64, p64:
// _permanent_refusal excludes terminal and poll candidates): a
// SESSION_RETIRE, whose refusals depend on later state (the guest high-water
// and settled cells), or a VIEW_WAIT continuation poll. The runtime keeps
// such a refused cell or workspace exactly as before for the replay. When
// the command did not decode (nil, for example a SESSION_RETIRE whose
// settled-cell list is malformed), its op and poll index are read leniently
// from the body; a body that is not even a JSON object is classified by its
// errno alone.
func pinnedByExporter(body []byte, command *Command) bool {
	var loose struct {
		Op         string `json:"op"`
		Delegation *struct {
			PollIndex uint64 `json:"poll_index"`
		} `json:"delegation"`
	}
	if command != nil {
		loose.Op = command.Op
		if command.Delegation != nil {
			loose.Delegation = &struct {
				PollIndex uint64 `json:"poll_index"`
			}{command.Delegation.PollIndex}
		}
	} else if json.Unmarshal(body, &loose) != nil {
		return false
	}
	return loose.Op == "SESSION_RETIRE" || loose.Delegation != nil && loose.Delegation.PollIndex > 0
}

// donorPermanent reports whether a refused donor send's workspace is cleared:
// the errnos the exporter can fail a donor send back on (for a command it
// does not always pin, see pinnedByExporter). On a replay the exporter keeps
// ESTALE and EACCES pinned and resends every chunk from the first, which
// rebuilds the workspace and revalidates the identical bytes, so clearing it
// never changes what that replay gets for a content refusal.
func donorPermanent(e error) bool {
	return errors.Is(e, fsapi.Stale) || errors.Is(e, fsapi.Invalid) || errors.Is(e, fsapi.Permission)
}

// numberedPermanent returns the refusal under which a refused numbered
// (non-donor) cell is removed, or OK when it stays. It matches the exporter's
// non-donor fail-back (p64, _permanent_original):
//   - Stale from validation is the guest freshness or duplicate check
//     (prepareMapping), which never admits that guest cell later. A
//     SESSION_RETIRE's Stale (prepareTerminal) can change with later state
//     and the exporter pins terminal cells, so it stays (pinnedByExporter).
//   - Invalid on a chunk after the first: the content of a complete cell
//     (hash, decode, frame binding) is refused for good, and the exporter
//     fails it back when this is its final chunk (w64), because an earlier
//     chunk of its write was accepted. A
//     single-chunk cell refused Invalid stays: the exporter cannot tell that
//     from a refusal before allocation and pins it.
//
// Permission (scope, reclaim flag, credit class), NoSpace and WouldBlock keep
// the cell, as the exporter keeps those candidates pinned and replays them.
func numberedPermanent(f Frame, e error) fsapi.Errno {
	if errors.Is(e, fsapi.Stale) {
		return fsapi.Stale
	}
	if errors.Is(e, fsapi.Invalid) && f.Offset > 0 {
		return fsapi.Invalid
	}
	return fsapi.OK
}

// discardRefused cleans up after acceptCell refused the complete cell c.
// A refused donor send leaves the reclaim workspace free for the next cell; its
// native number stays unallocated. A permanently refused numbered cell is
// removed and its credit returned, so incomplete-cell limits, credit and
// guest-marker pruning no longer count it, and its number stays spent (the
// high-water does not move back). The refusal is kept in t.refused so that a
// later write to that number sees what it would have seen had the cell been
// kept (refusedResend). Write protects the removal and the record with the
// rest of the call (commitWrite).
func (t *Transport) discardRefused(c *cell, f Frame, command *Command, e error, w *writeEffects) {
	if pinnedByExporter(c.body, command) {
		return
	}
	if c == t.workspace {
		if donorPermanent(e) {
			t.workspace = nil
		}
		return
	}
	code := numberedPermanent(f, e)
	if code == fsapi.OK || t.cells[f.Cell] != c || c.command != nil {
		return
	}
	delete(t.cells, f.Cell)
	if c.reservation.Control {
		t.control--
	} else {
		t.ordinary--
	}
	header := c.frame
	header.Offset, header.Data = 0, nil
	t.refused = &ProtectedRefusal{Header: header, Error: code}
	w.dropped = c
}

// refusedResend answers a write to a number at or below the high-water that
// has no cell. The most recently removed refused cell answers its own errno
// for an identical frame (Invalid for another frame, as a kept cell did), so
// removing the cell does not change the answer for its number, also after a
// restore, and the answer still names the original refusal. Every other
// spent or released number is Stale.
//
// The record does not unpin the exporter. The case it answers is a replay
// after an exporter crash before its fail-back was protected. That replay
// resends from the first chunk, so it gets Stale (which the exporter keeps
// pinned on a replay, since authentication can return Stale for an admitted
// cell) or Invalid at offset 0 (which it keeps pinned, since a frame or
// numbering refusal before allocation looks the same). The candidate then
// stays pinned until an operator or a later exporter change resolves it; the
// record only keeps the runtime's answer consistent and attributable. Only
// the latest refusal is kept: the exporter's single writer replays only its
// pending candidate, before it writes any later number.
func (t *Transport) refusedResend(f Frame) error {
	if r := t.refused; r != nil && r.Header.Cell == f.Cell {
		if !sameFrame(r.Header, f) {
			return fsapi.Invalid
		}
		return r.Error
	}
	return fsapi.Stale
}

// ProtectedRefusal records the most recent numbered cell that Transport.Write
// removed after a permanent refusal (n64): its frame header without data and
// the errno it was refused with, Stale or Invalid. It is part of the transport
// index so that a restored transport answers a resend of that cell the same
// (refusedResend; it does not let a pinned exporter replay through). It is
// evidence of a refusal, not of any admitted state.
type ProtectedRefusal struct {
	Header Frame
	Error  fsapi.Errno
}

// validateRefusal checks the index's refusal record: a spent number at or
// below High without a live cell, a header-only frame and a removable errno.
func validateRefusal(s ProtectedTransportState) error {
	r := s.Refused
	if r == nil {
		return nil
	}
	h := r.Header
	if h.Cell < 1 || h.Cell > s.High || h.Kind != 1 || h.Offset != 0 || len(h.Data) != 0 || h.Total == 0 || h.Total > BodyBytes || h.Flags&2 != 0 {
		return fsapi.Stale
	}
	if r.Error != fsapi.Stale && r.Error != fsapi.Invalid {
		return fsapi.Stale
	}
	for _, id := range s.Cells {
		if id == h.Cell {
			return fsapi.Stale
		}
	}
	return nil
}
