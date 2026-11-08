package guestbridge

import "github.com/snonux/homelabfs/internal/fsapi"

// ProtectedEvidence reads the actual live C8 ledger cut owned by this Transport
// and returns its inert, fully validated index, including the digest over the
// index, every bridge-session row and every cell row. It holds the transport
// mutex for the bounded read so the cut equals the last protected transition
// (protectLocked commits under the same mutex); workers wait rather than race.
//
// Memory is transient and bounded by the ledger limits: the 128 KiB index, at
// most 8 MiB of complete bridge rows and one decoded 8 MiB cell at a time.
// Callers fund that peak. The result cannot construct a VerifiedRequest,
// restore an endpoint, release a cell or authorize RESUME. A volatile, closed,
// suspended or failed transport truthfully reports Unavailable.
func (t *Transport) ProtectedEvidence() (ProtectedTransportState, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil || t.ledger == nil {
		return ProtectedTransportState{}, fsapi.Unavailable
	}
	return inspectEndpointLedger(t.ledger, nil, nil)
}
