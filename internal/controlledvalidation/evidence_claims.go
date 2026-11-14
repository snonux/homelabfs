package controlledvalidation

import (
	"encoding/json"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
)

// Claim evidence (task b84, the l64 daemon feature).
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
// There is no age: the ledger stores no time, and adding one would change
// the protected ledger format. A driver that needs an age measures it from
// its own probe time across successive OBSERVEs.

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
	// Pending lists up to claimEvidencePending open entries, oldest number
	// first; Recent lists up to claimEvidenceRecent present entries, newest
	// number first, whatever their status.
	Pending []ClaimSummary
	Recent  []ClaimSummary
}

// ClaimSummary is one claim entry without its binding: the public number,
// the guest intent opcode (empty for an ALLOCATED number whose CALL has not
// arrived), the raw ledger status and the preparation stage.
type ClaimSummary struct {
	Number uint64
	Opcode string
	Status string
	Stage  string
}

// claimEvidence reads the principals and claims buckets in one read
// transaction. The caller holds the execution mutex, so no provider
// transition is half applied.
func (p *Provider) claimEvidence(tx *bolt.Tx) (ClaimEvidence, error) {
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
	present, err := p.claimSummaries(tx, &e)
	if err != nil {
		return ClaimEvidence{}, err
	}
	e.Recent = newestClaims(present, claimEvidenceRecent)
	return e, nil
}

// claimSummaries walks every present claim entry, counting it into e and
// listing the first open ones; it returns all summaries for newestClaims.
func (p *Provider) claimSummaries(tx *bolt.Tx, e *ClaimEvidence) ([]ClaimSummary, error) {
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
			e.Pending = append(e.Pending, summary)
		}
		return nil
	})
	sort.SliceStable(e.Pending, func(i, j int) bool { return e.Pending[i].Number < e.Pending[j].Number })
	return present, err
}

// settledClaim is terminalEmpty's per-entry status test: TERMINAL, or a gap
// whose no-effect was proven (DECLINED). Anything else still owes the guest
// an answer.
func settledClaim(e entry) bool {
	return e.Status == "TERMINAL" || e.Status == "DECLINED" && e.Gap
}

// summarizeClaim keeps only the number, opcode, status and stage. The opcode
// comes from the reserved binding, or from the original binding an
// AllocateOriginal number carries before its CALL arrives.
func summarizeClaim(e entry) ClaimSummary {
	opcode := e.Binding.Call.Intent.Opcode
	if opcode == "" && e.Original != nil {
		opcode = e.Original.Call.Intent.Opcode
	}
	return ClaimSummary{Number: e.Claim.Number, Opcode: opcode, Status: e.Status, Stage: e.PreparationStage}
}

// newestClaims returns up to limit summaries, highest number first.
func newestClaims(all []ClaimSummary, limit int) []ClaimSummary {
	sort.SliceStable(all, func(i, j int) bool { return all[i].Number > all[j].Number })
	if len(all) > limit {
		all = all[:limit]
	}
	return all
}
