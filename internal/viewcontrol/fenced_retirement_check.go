package viewcontrol

import (
	"crypto/ed25519"
	"encoding/json"
	"strings"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Checks of a retired fence (task fh4) shared by the controller and the
// controlled runtime's Facts, so the two can never drift: the statement the
// operator signs (NewFencedRetirement, Verify) and the rows a retirement
// must leave behind (RetiredScopeClean, which both apply to the same raw
// ledger bytes). The Facts verify the operator signature themselves with
// the operator key the enrollment was signed with; they never trust the
// tombstone's presence alone.

// FencedRetirementVersion is the version of the operator statement.
const FencedRetirementVersion = "homelabfs-viewcontrol-retire-fenced/1"

// FencedRetirement is the statement the operator signs to retire one fenced
// session: the controller's enrollment and the session's exact fence row.
type FencedRetirement struct {
	Version        string
	EnrollmentHash string
	Session        string
	Recall, Phase  string
	Sequence       uint64
	Fence          uint64
}

// NewFencedRetirement is the statement retiring the fence row (recall,
// phase, sequence, fence) of session under the enrollment enrollmentHash.
func NewFencedRetirement(enrollmentHash, session, recall, phase string, sequence, fence uint64) FencedRetirement {
	return FencedRetirement{Version: FencedRetirementVersion, EnrollmentHash: enrollmentHash, Session: session, Recall: recall, Phase: phase, Sequence: sequence, Fence: fence}
}

// Message returns the canonical bytes the operator signs.
func (r FencedRetirement) Message() ([]byte, error) { return canonical(r) }

// Verify reports whether signature is operator's signature over r.
func (r FencedRetirement) Verify(operator ed25519.PublicKey, signature []byte) bool {
	msg, err := r.Message()
	return err == nil && len(operator) == ed25519.PublicKeySize && ed25519.Verify(operator, msg, signature)
}

// SignFencedRetirement signs r with the operator's private key. It is the
// helper for operator tooling and tests; the controller only verifies.
func SignFencedRetirement(key ed25519.PrivateKey, r FencedRetirement) ([]byte, error) {
	msg, err := r.Message()
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(key, msg), nil
}

// EnrollmentHash is the hash a controller binds its ledger and every
// FencedRetirement to: the hash of the enrollment's JSON encoding.
func EnrollmentHash(e Enrollment) (string, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return hash(raw), nil
}

// retiredRowsView is the part of a ledger RetiredScopeClean reads.
type retiredRowsView struct {
	Tickets  map[string]struct{ Scope guestbridge.Context }
	Families map[string]family
	Leases   map[string]struct {
		Lease struct{ Scope guestbridge.Context }
	}
	Reservations map[string]struct {
		Reservation struct{ Scope guestbridge.Context }
	}
	LineageFences map[string]lineageFence
	AckArchive    map[string]string
}

// RetiredScopeClean reports whether the ledger raw holds nothing of the
// retired scope but its minimal family rows: no ticket, lease, reservation,
// lineage fence or archived ACK, and every family it owns reduced to
// {High, Floor = High, Scope} with no receipt (retireSessionFamilies). An
// undecodable ledger is not clean.
func RetiredScopeClean(raw json.RawMessage, scope guestbridge.Context) bool {
	var v retiredRowsView
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	session := scope.Session
	for _, t := range v.Tickets {
		if t.Scope.Session == session {
			return false
		}
	}
	for _, f := range v.Families {
		if f.Scope != nil && f.Scope.Session == session && (f.Floor != f.High || f.Receipt != "") {
			return false
		}
	}
	for _, l := range v.Leases {
		if l.Lease.Scope.Session == session {
			return false
		}
	}
	for _, r := range v.Reservations {
		if r.Reservation.Scope.Session == session {
			return false
		}
	}
	for _, at := range v.LineageFences {
		if at.Session == session {
			return false
		}
	}
	return !ArchivedACKs(v.AckArchive, scope)
}

// ArchivedACKs reports whether archive (a ledger's AckArchive) holds any
// row of scope.
func ArchivedACKs(archive map[string]string, scope guestbridge.Context) bool {
	prefix := digest(scope) + "/"
	for key := range archive {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
