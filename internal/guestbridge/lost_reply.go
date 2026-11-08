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

// LostReplyIntentMkdir narrows the fixture to a CALL whose original guest
// intent opcode is MKDIR (d44). The Linux guest's mkdir(2) always issues
// GETATTR CALLs (may_lookup and may_create both reach hfs_permission, which
// refreshes attributes over a CALL) and a LOOKUP CALL (hfs_d_revalidate never
// keeps a dentry) before MKDIR in the same syscall, and its background FORGET
// and PROCESS_EXIT CALLs can race in, so an unfiltered fixture practically
// never drops the mutating reply (see README.md). The filter reads only the
// accepted cell's own decoded intent opcode, never a response, result or
// caller-supplied replacement bytes.
const LostReplyIntentMkdir = "MKDIR"

// lostReplyIntents is the closed set of intent filters ArmLostReply accepts:
// "" matches any CALL (the original v24 behavior), MKDIR only that opcode.
var lostReplyIntents = map[string]bool{"": true, LostReplyIntentMkdir: true}

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
	ArmedHigh uint64 `json:"armed_high"`
	// Intent is the armed intent-opcode filter; empty matches any CALL.
	Intent string `json:"intent,omitempty"`
	// DroppedOpcode is the actual intent opcode of the CALL whose exposure
	// was dropped, so an oracle need not infer it from source effects.
	DroppedOpcode string `json:"dropped_opcode,omitempty"`
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
// actual Read exposure of a CALL response accepted for session after this
// call and, when intent is nonempty, whose original guest intent opcode equals
// intent (see lostReplyIntents). Non-matching CALLs are exposed normally.
// The caller must already have reserved the fixture's funding.
// Only a disposable validation service can arm it, only once per transport,
// and only for an enrolled session; the predicate takes no response, result,
// Facts or replacement bytes, so it can only withhold real protected bytes.
func (t *Transport) ArmLostReply(session, operation, intent string) error {
	if operation != LostReplyOperation || !lostReplyIntents[intent] {
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
	t.lostReply = &LostReplyEvidence{State: LostReplyArmed, Session: session, Operation: operation, ArmedHigh: t.high, Intent: intent}
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
// armed session that matches the armed intent filter, before any of its bytes
// were covered. On a match it records the actual identities and disarms; the
// caller then returns an I/O error and leaves read coverage untouched, because
// no byte reached the native reader.
func (t *Transport) dropExposureLocked(c *cell, id uint64, kind uint16, chunk, within uint64, data []byte) bool {
	f := t.lostReply
	if f == nil || f.State != LostReplyArmed || kind != 2 || len(data) == 0 || id <= f.ArmedHigh {
		return false
	}
	call, ok := armedCall(f, c)
	if !ok {
		return false
	}
	claim := call.Claim
	f.State, f.Cell, f.Claim, f.JobPhase, f.Outcome = LostReplyFired, id, &claim, c.jobPhase, lostReplyUncertain
	f.DroppedOpcode = call.Intent.Opcode
	f.CommandHash, f.ResponseHash, f.ResponseBytes = digest(c.body), digest(c.response), len(c.response)
	f.WireBytes, f.UnreadBytes = wireBytes(c.wire), c.unread
	f.DroppedOffset = id*CellStride + chunk*(HeaderBytes+ChunkBytes) + within
	f.DroppedBytes, f.DroppedHash = len(data), digest(data)
	return true
}

// armedCall returns the accepted CALL of cell c when it belongs to the armed
// session, has no covered byte yet and matches the armed intent filter. The
// opcode comes from the cell's own decoded command, whose intent is bound to
// the protected claim's original hash; a non-matching CALL is exposed normally
// and leaves the fixture armed.
func armedCall(f *LostReplyEvidence, c *cell) (Call, bool) {
	var call Call
	if c.command == nil || c.command.Op != f.Operation || c.command.Context.Session != f.Session || c.unread != wireBytes(c.wire) {
		return call, false
	}
	if json.Unmarshal(c.command.Payload, &call) != nil {
		return call, false
	}
	return call, f.Intent == "" || call.Intent.Opcode == f.Intent
}

func wireBytes(wire [][]byte) int {
	n := 0
	for _, chunk := range wire {
		n += len(chunk)
	}
	return n
}
