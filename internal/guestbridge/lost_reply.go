package guestbridge

import (
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Lost-reply fixture states. ARMED waits for the next matching exposure;
// FIRED dropped exactly one; EXPIRED was disarmed (by a C9 restart barrier)
// before any matching exposure, so it can never fire.
const (
	LostReplyArmed   = "ARMED"
	LostReplyFired   = "FIRED"
	LostReplyExpired = "EXPIRED"
)

// LostReplyOperation is the only native command whose response exposure the
// disposable fixture may drop. A CALL carries the original filesystem effect
// and its claim, so losing its reply leaves the caller's result uncertain while
// the protected claim/response remain the single origin result.
const LostReplyOperation = "CALL"

// lostReplyUncertain is the caller-visible outcome of a dropped exposure: the
// native reader received an I/O error and no bytes, so it cannot tell whether
// the already committed effect happened.
const lostReplyUncertain = "UNCERTAIN"

// LostReplyEvidence is the inert, fixed-shape record of the one-shot fixture.
// It names the actual cell, claim and protected response identities observed
// at the dropped exposure. It is never decoded back into authority, restores
// nothing and carries no pass/fail value.
type LostReplyEvidence struct {
	State     string `json:"state"`
	Session   string `json:"session"`
	Operation string `json:"operation"`
	// ArmedHigh is the native allocator high-water at arming. Only cells
	// accepted after arming match, so retained older responses never fire.
	ArmedHigh     uint64 `json:"armed_high"`
	Cell          uint64 `json:"cell,omitempty"`
	Claim         *Claim `json:"claim,omitempty"`
	CommandHash   string `json:"command_hash,omitempty"`
	ResponseHash  string `json:"response_hash,omitempty"`
	ResponseBytes int    `json:"response_bytes,omitempty"`
	WireBytes     int    `json:"wire_bytes,omitempty"`
	UnreadBytes   int    `json:"unread_bytes,omitempty"`
	DroppedOffset uint64 `json:"dropped_offset,omitempty"`
	DroppedBytes  int    `json:"dropped_bytes,omitempty"`
	DroppedHash   string `json:"dropped_hash,omitempty"`
	JobPhase      string `json:"job_phase,omitempty"`
	Outcome       string `json:"outcome,omitempty"`
}

// ArmLostReply arms the disposable one-shot fixture that drops the first
// actual Read exposure of the next CALL response accepted for session after
// this call. The caller must already have reserved the fixture's funding.
// Only a disposable validation service can arm it, only once per transport,
// and only for an enrolled session; the predicate takes no response, result,
// Facts or replacement bytes, so it can only withhold real protected bytes.
func (t *Transport) ArmLostReply(session, operation string) error {
	if operation != LostReplyOperation {
		return fsapi.Invalid
	}
	if v := t.service.config.Validation; v == nil || !v.Disposable {
		return fsapi.Permission
	}
	// scope takes the service mutex; like Write, resolve it before t.mu.
	if t.scope(session) == nil {
		return fsapi.Permission
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil {
		return fsapi.Unavailable
	}
	if t.lostReply != nil {
		return fsapi.Busy
	}
	t.lostReply = &LostReplyEvidence{State: LostReplyArmed, Session: session, Operation: operation, ArmedHigh: t.high}
	return nil
}

// ExpireLostReply permanently disarms a fixture that has not fired. It is
// called before a C9 suspension, so the fixture cannot outlive the exact cut it
// was armed for. A fired record is left unchanged.
func (t *Transport) ExpireLostReply() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lostReply != nil && t.lostReply.State == LostReplyArmed {
		t.lostReply.State = LostReplyExpired
	}
}

// LostReply returns a copy of the fixture record, if one was ever armed. It
// remains readable after Close or Suspend, as pure evidence.
func (t *Transport) LostReply() (LostReplyEvidence, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.lostReply == nil {
		return LostReplyEvidence{}, false
	}
	e := *t.lostReply
	if e.Claim != nil {
		claim := *e.Claim
		e.Claim = &claim
	}
	return e, true
}

// dropExposureLocked decides, under t.mu, whether this Read is the armed
// exposure. It matches only the first nonempty result-node exposure of a fully
// responded (and therefore protected) CALL cell accepted after arming in the
// armed session, before any of its bytes were covered. On a match it records
// the actual identities and disarms; the caller then returns an I/O error and
// leaves read coverage untouched, because no byte reached the native reader.
func (t *Transport) dropExposureLocked(c *cell, id uint64, kind uint16, chunk, within uint64, data []byte) bool {
	f := t.lostReply
	if f == nil || f.State != LostReplyArmed || kind != 2 || len(data) == 0 || id <= f.ArmedHigh {
		return false
	}
	if c.command == nil || c.command.Op != f.Operation || c.command.Context.Session != f.Session || c.unread != wireBytes(c.wire) {
		return false
	}
	var call Call
	if json.Unmarshal(c.command.Payload, &call) != nil {
		return false
	}
	claim := call.Claim
	f.State, f.Cell, f.Claim, f.JobPhase, f.Outcome = LostReplyFired, id, &claim, c.jobPhase, lostReplyUncertain
	f.CommandHash, f.ResponseHash, f.ResponseBytes = digest(c.body), digest(c.response), len(c.response)
	f.WireBytes, f.UnreadBytes = wireBytes(c.wire), c.unread
	f.DroppedOffset = id*CellStride + chunk*(HeaderBytes+ChunkBytes) + within
	f.DroppedBytes, f.DroppedHash = len(data), digest(data)
	return true
}

func wireBytes(wire [][]byte) int {
	n := 0
	for _, chunk := range wire {
		n += len(chunk)
	}
	return n
}
