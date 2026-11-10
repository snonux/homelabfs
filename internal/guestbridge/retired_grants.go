package guestbridge

import (
	"encoding/json"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Retired grant pruning (task o64).
//
// A released grant used to stay in the session snapshot forever as a
// tombstone, so MaxCapabilityIdentities bounded every grant a session ever
// issued (each LOOKUP, CREATE, OPEN and READDIR entry), and every claim's
// protected snapshot carried all of them. The zq3 spec's 128 identities did
// not last through T1.
//
// PruneRetiredGrants drops a tombstone once the claim that released it is
// retired. That is replay-safe:
//   - a new CALL naming a pruned token is refused with Permission, exactly as
//     for the tombstone (resolve and the view checks treat a missing and a
//     released grant alike);
//   - a retired claim is never executed or recovered again, and an unretired
//     claim's recovery uses its own protected plan and result bytes, so no
//     replay needs the tombstone;
//   - a READDIR's transient grants are owned by that READDIR, whose
//     retirement requires its READDIR_CLEANUP to be closed, so no cleanup
//     replay can still need them;
//   - tokens are 256 random bits, so dropping a tombstone does not make an
//     old token reusable in practice (issuance still refuses a live one).
//
// The close sequences of a pruned description go with it: a CLOSE naming it
// is refused at resolve before its sequence is read. Credentials and exited
// processes are kept (they are bounded by MaxCredentialRevisions).

// PruneRetiredGrants removes from snapshot every released grant whose Owner
// belongs to retired's principal at or below retired.Number, and the close
// sequences of those grants. It returns the re-encoded snapshot and the
// number of grants removed (0 leaves snapshot untouched).
func PruneRetiredGrants(scope Context, snapshot json.RawMessage, retired ClaimNumber) (json.RawMessage, int, error) {
	var state validationSnapshot
	if json.Unmarshal(snapshot, &state) != nil || state.CapabilityLifetimes != 2 || state.Context != scope {
		return nil, 0, fsapi.Stale
	}
	pruned := map[string]bool{}
	for token, g := range state.Grants {
		if g != nil && g.Released && retiredOwner(g.Owner, retired) {
			pruned[token] = true
			delete(state.Grants, token)
		}
	}
	if len(pruned) == 0 {
		return snapshot, 0, nil
	}
	for key := range state.CloseSequences {
		token, _, _ := strings.Cut(key, ":")
		if pruned[token] {
			delete(state.CloseSequences, key)
		}
	}
	raw, err := json.Marshal(state)
	return raw, len(pruned), err
}

// retiredOwner reports whether owner is a claim of retired's principal at or
// below its floor.
func retiredOwner(owner *ClaimNumber, retired ClaimNumber) bool {
	if owner == nil {
		return false
	}
	floor := retired
	floor.Number = owner.Number
	return *owner == floor && owner.Number <= retired.Number
}

func claimNumber(c Claim) *ClaimNumber {
	return &ClaimNumber{Realm: c.Realm, Subject: c.Subject, Anchor: c.Anchor, Nonce: c.Nonce, Number: c.Number}
}
