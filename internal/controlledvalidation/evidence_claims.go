package controlledvalidation

import (
	"encoding/json"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
)

// Claim evidence (task b84, and its l64 follow-up for claim ages).
//
// zq3-r8's post-READY usability gate passed a mount root whose lstat had
// returned while the probe child's PROCESS_EXIT claim stayed
// RESERVED/UNPREPARED; the guest blocked that session when its 30 s QUERY
// window ran out. The ledger digest alone cannot show such a claim, so
// OBSERVE now also carries a bounded, inert summary of the claim states:
// enough for an external driver to require that the claims a probe caused
// reached TERMINAL, and nothing that identifies a file, a credential or a
// process.
//
// Each Pending (open) summary also carries its Age (l64): whole seconds from
// the entry's Opened time, stored once when the number was allocated, to the
// clock reading this OBSERVE took, so a stuck claim shows how long the guest
// has been waiting on it without the driver tracking first-seen times. It is
// a pure function of the stored entry and the OBSERVE's own time, so a
// replay with a pinned clock (Provider.now) yields the same ages. It is
// wall-clock based: a backward step omits it, a forward (NTP) jump inflates
// it, which is acceptable for a diagnostic. Recent never carries it: a
// settled claim's allocation age says nothing.
//
// Claims listed among the lowest claimEvidenceTraced Pending numbers also
// carry the a84 refusal trace (refusal_trace.go) of an uncertain refusal
// that held them back: its class, a count and the Unix seconds of its first
// and latest recorded refusal. Age and the trace are omitempty, so an older
// reader, a claim that was never refused and an entry written before l64
// (no Opened) see the b84 shape unchanged.

const (
	// claimEvidencePending bounds the open claims listed by number. MaxClaims
	// is 128 per provider at the zq3 spec, so the count can exceed the list.
	claimEvidencePending = 16
	// claimEvidenceRecent bounds the newest present claims listed. A probe of
	// one mount root adds two (GETATTR and the child's PROCESS_EXIT), so
	// eight covers both probes of the usable gate with room to spare.
	claimEvidenceRecent = 8
)

// ClaimEvidence summarizes the provider's claim entries at the instant of
// its LedgerEvidence. Allocated and Retired are sums over every principal's
// High and Floor; both only grow (principals are never deleted), so a driver
// can tell how many numbers were allocated between two OBSERVEs even after
// retirement recycled their entries (a recycled number is at or below Floor
// and therefore settled).
type ClaimEvidence struct {
	Principals int
	Allocated  uint64
	Retired    uint64
	// Entries counts the claim entries still present (not yet recycled).
	Entries int
	// Open counts present entries that are neither TERMINAL nor a declined
	// gap (the same settled test terminalEmpty uses): ALLOCATED, RESERVED
	// and any other in-flight status.
	Open int
	// Pending lists up to claimEvidencePending open entries, sorted by
	// number; which ones are cut is bbolt key order (principal, then number),
	// which is number order for the guest's single principal per session.
	// Recent lists up to claimEvidenceRecent present entries, newest number
	// first, whatever their status.
	Pending []ClaimSummary
	Recent  []ClaimSummary
}

// ClaimSummary is one claim entry without its binding: the public number,
// the guest intent opcode (empty for an ALLOCATED number whose CALL has not
// arrived), the raw ledger status and the preparation stage.
//
// Age is set only on Pending summaries: the seconds since the number was
// allocated, at the OBSERVE's own clock reading (claimAge). It is omitted
// when under one second, when the entry predates l64 (no Opened time) and
// when the clock reads earlier than Opened (a backwards step).
//
// Refusal, Refusals, RefusedFirst and RefusedLast copy the claim's a84
// refusal trace (refusal_trace.go): the last uncertain refusal class
// ("ADMISSION:WouldBlock", ...), how many refusals of it were recorded (at
// most one a second, so a lower bound) and the Unix seconds of the first and
// the latest. To keep OBSERVE bounded (MaxClaimEvidenceBytes) they are set
// only on the first claimEvidenceTraced Pending summaries, the lowest open
// numbers, which are the ones a stuck guest waits on. Recent never carries
// them; a claim that moved on keeps its trace in the ledger only.
type ClaimSummary struct {
	Number       uint64
	Opcode       string
	Status       string
	Stage        string
	Refusal      string `json:",omitempty"`
	Refusals     uint64 `json:",omitempty"`
	RefusedFirst int64  `json:",omitempty"`
	RefusedLast  int64  `json:",omitempty"`
	Age          int64  `json:",omitempty"`
}

const (
	// claimEvidenceTraced bounds the Pending summaries that carry a refusal
	// trace, so 16 providers add at most about 10 KiB to an OBSERVE reply.
	claimEvidenceTraced = 4
	// claimSummaryText bounds each copied string (opcode, status, stage and
	// refusal class). Real values are at most 22 bytes (the longest opcode,
	// DESCRIPTION_RELEASE, has 19; a class like ADMISSION:TooManyLinks 22);
	// the bound only keeps a damaged entry from growing OBSERVE past
	// MaxClaimEvidenceBytes.
	claimSummaryText = 24
	// MaxClaimEvidenceBytes bounds the JSON encoding of one provider's
	// ClaimEvidence at the list bounds above, with every number and string
	// at its maximum, Age included on every Pending summary
	// (TestClaimEvidenceWorstCaseBound). The lifecycle reply
	// budget (controlledruntime TestObserveClaimEvidenceFitsReply) counts it
	// once per provider.
	MaxClaimEvidenceBytes = 4608
)

// claimEvidence reads the principals and claims buckets inside the caller's
// read transaction. That transaction is the consistency boundary: Allocate,
// Reserve and Retire do not take the execution mutex the caller holds, but
// each commits atomically, so the snapshot never shows half a transition.
// now is the OBSERVE's clock reading (Unix seconds) the Pending ages count to.
func (p *Provider) claimEvidence(tx *bolt.Tx, now int64) (ClaimEvidence, error) {
	// Empty, not nil, lists: a settled ledger encodes "Pending":[] so a
	// reader can tell "nothing open" from a missing field.
	e := ClaimEvidence{Pending: []ClaimSummary{}}
	if err := p.bucket(tx, "principals").ForEach(func(_, raw []byte) error {
		var family principal
		if json.Unmarshal(raw, &family) != nil {
			return fsapi.Stale
		}
		e.Principals++
		e.Allocated += family.High
		e.Retired += family.Floor
		return nil
	}); err != nil {
		return ClaimEvidence{}, err
	}
	present, err := p.claimSummaries(tx, &e, now)
	if err != nil {
		return ClaimEvidence{}, err
	}
	e.Recent = newestClaims(present, claimEvidenceRecent)
	return e, nil
}

// claimSummaries walks every present claim entry, counting it into e and
// listing the first open ones with their age at now; it returns all
// summaries, without ages, for newestClaims.
func (p *Provider) claimSummaries(tx *bolt.Tx, e *ClaimEvidence, now int64) ([]ClaimSummary, error) {
	present := []ClaimSummary{}
	err := p.bucket(tx, "claims").ForEach(func(_, raw []byte) error {
		var stored entry
		if json.Unmarshal(raw, &stored) != nil {
			return fsapi.Stale
		}
		summary := summarizeClaim(stored)
		present = append(present, summary)
		e.Entries++
		if settledClaim(stored) {
			return nil
		}
		e.Open++
		if len(e.Pending) < claimEvidencePending {
			summary.Age = claimAge(stored.Opened, now)
			e.Pending = append(e.Pending, summary)
		}
		return nil
	})
	sort.SliceStable(e.Pending, func(i, j int) bool { return e.Pending[i].Number < e.Pending[j].Number })
	for i := claimEvidenceTraced; i < len(e.Pending); i++ {
		e.Pending[i] = untraced(e.Pending[i])
	}
	for i := range present {
		present[i] = untraced(present[i])
	}
	return present, err
}

// untraced drops the refusal trace from a summary (see ClaimSummary).
func untraced(s ClaimSummary) ClaimSummary {
	s.Refusal, s.Refusals, s.RefusedFirst, s.RefusedLast = "", 0, 0, 0
	return s
}

// claimAge is the whole seconds from opened to now, or 0 (omitted) when the
// entry has no Opened time or the clock reads earlier than it.
func claimAge(opened, now int64) int64 {
	if opened <= 0 || now < opened {
		return 0
	}
	return now - opened
}

// boundedText cuts s to claimSummaryText bytes.
func boundedText(s string) string {
	if len(s) > claimSummaryText {
		return s[:claimSummaryText]
	}
	return s
}

// settledClaim is terminalEmpty's per-entry status test: TERMINAL, or a gap
// whose no-effect was proven (DECLINED). Anything else still owes the guest
// an answer.
func settledClaim(e entry) bool {
	return e.Status == "TERMINAL" || e.Status == "DECLINED" && e.Gap
}

// summarizeClaim keeps only the number, opcode, status, stage and refusal
// trace, each string cut to claimSummaryText (claimSummaries then drops the
// trace where ClaimSummary says). The opcode comes from the reserved binding,
// or from the original binding an AllocateOriginal number carries before its
// CALL arrives.
func summarizeClaim(e entry) ClaimSummary {
	opcode := e.Binding.Call.Intent.Opcode
	if opcode == "" && e.Original != nil {
		opcode = e.Original.Call.Intent.Opcode
	}
	s := ClaimSummary{Number: e.Claim.Number, Opcode: boundedText(opcode), Status: boundedText(e.Status), Stage: boundedText(e.PreparationStage)}
	if r := e.Refusal; r != nil {
		s.Refusal, s.Refusals, s.RefusedFirst, s.RefusedLast = boundedText(r.Class), r.Count, r.First, r.Last
	}
	return s
}

// newestClaims returns up to limit summaries, highest number first.
func newestClaims(all []ClaimSummary, limit int) []ClaimSummary {
	sort.SliceStable(all, func(i, j int) bool { return all[i].Number > all[j].Number })
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}
