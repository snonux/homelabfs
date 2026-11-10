package guestbridge

import (
	"encoding/json"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

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

// SessionEvidence returns, keyed by session, every bridge session as it is in
// memory now (encoded exactly as the next commit would persist it) and as it
// is committed in the owned ledger, both under the transport mutex so no
// commit falls between the two reads. Like ProtectedEvidence it is inert and
// bounded by bridgeSessionLimit; it lets a caller check that bridge state
// agrees with another owner's protected copy (u64: after a pruning RETIRE
// both must equal the provider's snapshot) without restoring anything.
// The rows include grant capability tokens and credentials, so this is for
// tests and offline evidence only: never log or publish its result. It is
// exported because its caller (controlledruntime tests) is another package.
func (t *Transport) SessionEvidence() (live, committed map[string]ProtectedBridgeSession, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil || t.ledger == nil {
		return nil, nil, fsapi.Unavailable
	}
	rows, err := t.service.protectedSessions()
	if err != nil {
		return nil, nil, err
	}
	live = make(map[string]ProtectedBridgeSession, len(rows))
	for key, raw := range rows {
		var row ProtectedBridgeSession
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, nil, err
		}
		live[strings.TrimPrefix(key, string(bridgeSessionKey("")))] = row
	}
	committed = map[string]ProtectedBridgeSession{}
	_, err = inspectEndpointLedger(t.ledger, nil, func(scope Context, row ProtectedBridgeSession) error {
		committed[scope.Session] = row
		return nil
	})
	return live, committed, err
}
