package viewcontrol

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// Recall history (task ce4) is a bounded, timestamped, diagnostic record of
// the controller's recent recalls. zq3-r14..r16 could not reconstruct recall
// latency from OBSERVE: the protected ledger keeps only the current recall,
// and pruning (pruneAckArchive, compaction, retirement recycling) drops
// everything about finished ones. This history lives only in controller
// memory, outside the protected ledger, so it changes no ledger byte, digest,
// reply or recall decision, and no pruning or compaction can drop it. A
// reopened controller (restart) starts with an empty history.
//
// Times are taken from the controller clock (time.Now, whose monotonic reading
// is kept in memory; c.now in tests). Every offset and duration is computed
// monotonically from those values; only the start of each recall is rendered
// as a wall-clock UTC timestamp, and every other time is an offset in
// microseconds from that start.
//
// Memory bound: at most RecallHistoryLimit records. Each record holds fixed
// fields, one participant session per enrolled domain, at most
// recallPhaseCount phase rows and at most RecallAckLimit ACK rows of about
// 130 bytes each (with their session and phase strings), so one record is at
// most about 5 KiB plus its participants and the ring at most about 320 KiB
// (about 160 KiB with 3 domains, whose recalls have 15 ACKs). OBSERVE carries
// only the newest records that fit RecallEvidenceBytes (see History).
const (
	// RecallHistoryLimit is the number of recalls the ring keeps.
	RecallHistoryLimit = 64
	// RecallAckLimit bounds the ACK rows of one record; further accepted ACKs
	// are only counted in AcksDropped. 5 ACK phases x 3 domains need 15.
	RecallAckLimit = 32
	// RecallEvidenceBytes bounds the complete encoded RecallHistory one
	// OBSERVE carries (wrapper fields and separators included). It bounds
	// only the history's share of a lifecycle reply (128 KiB) or RESTART
	// outcome (124 KiB); if the other sections still overflow those, the
	// documented fallbacks apply: an oversized OBSERVE reply is refused and
	// RESTART reports EFFECT_UNREPORTABLE. After a RESTART the reopened
	// controller's history is empty anyway.
	RecallEvidenceBytes = 32 << 10
	// recallPhaseCount is the number of recall phases (RECALL, DRAIN,
	// INSTALL_DRAIN, WAIT_DRAINED, INSTALL_FINAL, REOPEN).
	recallPhaseCount = 6
)

// Recall trigger kinds: why a recall started.
const (
	TriggerAcquireConflict    = "ACQUIRE_CONFLICT"    // VIEW_ACQUIRE conflicts with an active lease
	TriggerExpectedTarget     = "EXPECTED_TARGET"     // VIEW_ACQUIRE expected a different cut
	TriggerRelease            = "RELEASE"             // release of a lineage that still has an active lease
	TriggerReleaseReservation = "RELEASE_RESERVATION" // release of a lineage with an open reservation
	TriggerCallConflict       = "CALL_CONFLICT"       // a guest CALL conflicts with leases
	TriggerAuthority          = "AUTHORITY_REDUCTION" // an authority reduction meets its own open reservation
	TriggerNativeConflict     = "NATIVE_CONFLICT"     // a native command conflicts with leases
)

// recallTrigger is what started a recall: its kind, the opcode of the
// request and the requesting domain session ("" for a native command, which
// belongs to no guest domain).
type recallTrigger struct{ Kind, Opcode, Domain string }

// RecallPhase is one phase start, OffsetMicros after the recall started.
// IssueMicros is how long issuing that phase's events took (source capture
// and event build, events()); 0 for the phases that issue no events.
type RecallPhase struct {
	Phase        string `json:"phase"`
	OffsetMicros int64  `json:"at_us"`
	IssueMicros  int64  `json:"issue_us,omitempty"`
}

// RecallAck is one accepted VIEW_ACK: the acknowledging domain session, its
// ACK phase and event sequence and when it was accepted (OffsetMicros after
// the recall started). Blocked counts the BLOCKED replies the same ACK bytes
// got before (the controller waiting for the physical observation, or an
// early DRAINED waiting for its peers) and BlockedMicros the time from the
// first of them to acceptance. CheckMicros is the duration of the accepting
// call's validation, including the physical Facts check.
type RecallAck struct {
	Domain        string `json:"domain"`
	Phase         string `json:"phase"`
	Sequence      uint64 `json:"sequence"`
	OffsetMicros  int64  `json:"at_us"`
	Blocked       int    `json:"blocked,omitempty"`
	BlockedMicros int64  `json:"blocked_us,omitempty"`
	CheckMicros   int64  `json:"check_us"`
}

// RecallFence is one participant the controller fenced as stalled (task
// 0a4, participant_fence.go): its domain session, the phase it was dropped
// from and when (OffsetMicros after the recall started). A record holds at
// most one row per participant, since a fenced session gets no event again.
type RecallFence struct {
	Domain       string `json:"domain"`
	Phase        string `json:"phase"`
	OffsetMicros int64  `json:"at_us"`
}

// Recall outcomes.
const (
	RecallOpen      = "OPEN"      // still running at capture
	RecallCompleted = "COMPLETED" // every REOPENED ACK was in and the recall was removed
	RecallAbandoned = "ABANDONED" // a different recall started without this one completing
)

// RecallRecord is one recall's history. Scope is "ALL" for the all-domain
// recall or "DOMAIN" for a scoped one; Participants are the sessions that
// received its events. EndMicros is set once it ended.
type RecallRecord struct {
	ID           string        `json:"id"`
	Kind         string        `json:"kind"`
	Opcode       string        `json:"opcode"`
	Domain       string        `json:"domain,omitempty"`
	Scope        string        `json:"scope"`
	Participants []string      `json:"participants"`
	StartedUTC   string        `json:"started_utc"`
	EndMicros    int64         `json:"end_us,omitempty"`
	Outcome      string        `json:"outcome"`
	Phases       []RecallPhase `json:"phases"`
	Acks         []RecallAck   `json:"acks"`
	AcksDropped  int           `json:"acks_dropped,omitempty"`
	Fenced       []RecallFence `json:"fenced,omitempty"`

	started time.Time // monotonic origin of every offset
}

// RecallHistory is the OBSERVE view of the ring: Capacity and Total (recalls
// started since this controller opened), Advances (VIEW_ACQUIREs granted on
// a newer cut than their expected target by advance since this controller
// opened, task xf4: each one an expected-target recall that did not run),
// Blocked (the BLOCKED decisions by reason, task 2h4, blocked_reasons.go;
// omitted until the first one, so older consumers see the pre-2h4 shape),
// the newest records oldest first and Omitted, the older ring records left
// out by RecallEvidenceBytes.
type RecallHistory struct {
	Capacity int             `json:"capacity"`
	Total    uint64          `json:"total"`
	Advances uint64          `json:"advances"`
	Blocked  *BlockedHistory `json:"blocked,omitempty"`
	Omitted  int             `json:"omitted"`
	Recalls  []RecallRecord  `json:"recalls"`
}

// recallHistory is the in-memory ring, guarded by Controller.mu. issue holds
// the duration of the last events() issue until the phase it entered is
// recorded (issuePhase -> progress). advances counts advance grants (acquire,
// task xf4); like the ring it is diagnostic memory only, so it counts a grant
// whose admission later failed to save too, and a reopen starts it at 0.
// blocked records the BLOCKED decisions by reason (task 2h4) on the same
// terms.
type recallHistory struct {
	records  []*RecallRecord
	total    uint64
	advances uint64
	blocked  blockedHistory
	issue    time.Duration
}

// clock is the controller's time source: the test clock when set.
func (c *Controller) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// micros is d in whole microseconds.
func micros(d time.Duration) int64 { return d.Microseconds() }

// begin records a protected new recall r started at start by trigger, whose
// RECALL events took issue. An older record still OPEN can only have been
// left by a recall that never completed in this controller; it is marked
// ABANDONED. When the ring is full the oldest record goes.
func (h *recallHistory) begin(r *recallState, trigger recallTrigger, start time.Time, issue time.Duration) {
	h.issue = 0
	if n := len(h.records); n > 0 && h.records[n-1].Outcome == RecallOpen {
		h.records[n-1].Outcome = RecallAbandoned
	}
	scope := "ALL"
	if r.Domain != "" {
		scope = "DOMAIN"
	}
	participants := make([]string, 0, len(r.Events))
	for session := range r.Events {
		participants = append(participants, session)
	}
	sort.Strings(participants)
	record := &RecallRecord{ID: r.ID, Kind: trigger.Kind, Opcode: trigger.Opcode, Domain: trigger.Domain, Scope: scope, Participants: participants, StartedUTC: start.UTC().Format(time.RFC3339Nano), Outcome: RecallOpen, started: start, Acks: []RecallAck{}}
	record.Phases = append(make([]RecallPhase, 0, recallPhaseCount), RecallPhase{Phase: r.Phase, IssueMicros: micros(issue)})
	if len(h.records) == RecallHistoryLimit {
		copy(h.records, h.records[1:])
		h.records = h.records[:RecallHistoryLimit-1]
	}
	h.records = append(h.records, record)
	h.total++
}

// current returns the record of recall id while it is the newest and OPEN.
func (h *recallHistory) current(id string) *RecallRecord {
	if n := len(h.records); n > 0 && h.records[n-1].ID == id && h.records[n-1].Outcome == RecallOpen {
		return h.records[n-1]
	}
	return nil
}

// phase records that recall id entered phase at now, or ended when ended.
func (h *recallHistory) phase(id, phase string, ended bool, now time.Time) {
	issue := h.issue
	h.issue = 0
	record := h.current(id)
	if record == nil {
		return
	}
	offset := micros(now.Sub(record.started))
	if ended {
		record.EndMicros, record.Outcome = offset, RecallCompleted
		return
	}
	if len(record.Phases) < recallPhaseCount {
		record.Phases = append(record.Phases, RecallPhase{Phase: phase, OffsetMicros: offset, IssueMicros: micros(issue)})
	}
}

// acked records one accepted ACK of recall id. A repeated acceptance of the
// same domain, phase and sequence (an exact replay) is not recorded again.
func (h *recallHistory) acked(id string, row RecallAck, at time.Time) {
	record := h.current(id)
	if record == nil {
		return
	}
	for _, old := range record.Acks {
		if old.Domain == row.Domain && old.Phase == row.Phase && old.Sequence == row.Sequence {
			return
		}
	}
	if len(record.Acks) >= RecallAckLimit {
		record.AcksDropped++
		return
	}
	row.OffsetMicros = micros(at.Sub(record.started))
	record.Acks = append(record.Acks, row)
}

// fenced records that recall id dropped participant session in phase at now.
func (h *recallHistory) fenced(id, session, phase string, now time.Time) {
	record := h.current(id)
	if record == nil {
		return
	}
	record.Fenced = append(record.Fenced, RecallFence{Domain: session, Phase: phase, OffsetMicros: micros(now.Sub(record.started))})
}

// copyAll copies every ring record, oldest first, sharing no slice with the
// ring. It only copies memory, so History can hold the controller mutex for
// it and encode the copy after releasing it.
func (h *recallHistory) copyAll() RecallHistory {
	out := RecallHistory{Capacity: RecallHistoryLimit, Total: h.total, Advances: h.advances, Blocked: h.blocked.view(), Recalls: make([]RecallRecord, 0, len(h.records))}
	for _, r := range h.records {
		row := *r
		row.Participants = append([]string{}, r.Participants...)
		row.Phases = append([]RecallPhase{}, r.Phases...)
		row.Acks = append([]RecallAck{}, r.Acks...)
		if r.Fenced != nil {
			row.Fenced = append([]RecallFence{}, r.Fenced...)
		}
		out.Recalls = append(out.Recalls, row)
	}
	return out
}

// fitEvidence keeps the newest records of h whose complete encoding, wrapper
// and separators included, fits RecallEvidenceBytes, and counts the others
// in Omitted. The wrapper, which includes the bounded BLOCKED reasons
// (fitBlocked trims them first should they ever not fit), is measured with
// Omitted at its largest possible value, so the actual (never longer)
// encoding stays within the bound.
func fitEvidence(h RecallHistory) RecallHistory {
	n := len(h.Recalls)
	base, err := fitBlocked(&h, n)
	if err != nil {
		h.Omitted, h.Recalls = n, []RecallRecord{}
		return h
	}
	used, first := base, n
	for first > 0 {
		raw, err := json.Marshal(h.Recalls[first-1])
		size := len(raw)
		if first < n {
			size++ // the comma between this record and the next kept one
		}
		if err != nil || used+size > RecallEvidenceBytes {
			break
		}
		used += size
		first--
	}
	h.Omitted, h.Recalls = first, h.Recalls[first:]
	return h
}

// snapshot is the OBSERVE view of h: its copy trimmed to the evidence bound.
func (h *recallHistory) snapshot() RecallHistory { return fitEvidence(h.copyAll()) }

// History returns the bounded recall history (task ce4). It copies the ring
// under the controller mutex and measures and encodes outside it: guest
// calls take that mutex with TryLock, so a held mutex is their WouldBlock
// window. Unlike LedgerEvidence it is also answered by a closed or poisoned
// controller: the history is inert memory, and a failed controller is exactly
// when its recent recalls matter. It never touches the protected ledger.
func (c *Controller) History(ctx context.Context) (RecallHistory, error) {
	if err := ctx.Err(); err != nil {
		return RecallHistory{}, err
	}
	c.mu.Lock()
	all := c.history.copyAll()
	c.mu.Unlock()
	return fitEvidence(all), nil
}

// recordAck records the ACK raw of scope accepted at now after a validation
// of check. Its BLOCKED history is the session's blockedACKs entry, which
// still names these exact bytes when they were BLOCKED before (ackReply
// clears it only afterwards).
func (c *Controller) recordAck(ack acknowledgement, raw json.RawMessage, check time.Duration, now time.Time) {
	row := RecallAck{Domain: ack.Session, Phase: ack.Phase, Sequence: ack.Sequence, CheckMicros: micros(check)}
	if b, ok := c.blockedACKs[ack.Session]; ok && b.hash == hash(raw) {
		row.Blocked, row.BlockedMicros = b.count, micros(now.Sub(b.since))
	}
	c.history.acked(ack.Recall, row, now)
}
