package guestbridge

import (
	"bytes"
	"context"
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
//     old token reusable in practice (issuance still refuses a live one);
//   - endpoint restore stays consistent: after a RETIRED reply the bridge
//     reloads the pruned snapshot (Service.retire) before the reply and its
//     session rows are persisted, and a session row persisted before the
//     prune (a crash between the provider commit and that persist) still
//     restores, because the provider's CheckRestoredBridge prunes the row
//     and its own snapshot by the same rule at every family's floor
//     (PruneRetiredSession) before comparing them.
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
	pruned := pruneGrantRows(state.Grants, state.CloseSequences, []ClaimNumber{retired})
	if pruned == 0 {
		return snapshot, 0, nil
	}
	raw, err := json.Marshal(state)
	return raw, pruned, err
}

// PruneRetiredSession applies the same rule for every family floor in floors
// to the Grants and CloseSequences of a persisted bridge session row or of a
// protected provider snapshot, and returns both maps re-encoded. The
// provider's restore check prunes both sides this way before comparing them
// (u64), so a row saved before a prune still matches the pruned snapshot.
func PruneRetiredSession(row json.RawMessage, floors []ClaimNumber) (grants, closes json.RawMessage, err error) {
	var state struct {
		Grants         map[string]*grant
		CloseSequences map[string]uint64
	}
	if err = json.Unmarshal(row, &state); err != nil {
		return nil, nil, err
	}
	pruneGrantRows(state.Grants, state.CloseSequences, floors)
	if grants, err = json.Marshal(state.Grants); err != nil {
		return nil, nil, err
	}
	closes, err = json.Marshal(state.CloseSequences)
	return grants, closes, err
}

// pruneGrantRows deletes the released grants owned by a retired claim of any
// floor's family, and their close sequences; it returns how many it deleted.
func pruneGrantRows(grants map[string]*grant, closes map[string]uint64, floors []ClaimNumber) int {
	pruned := map[string]bool{}
	for token, g := range grants {
		for _, floor := range floors {
			if g != nil && g.Released && retiredOwner(g.Owner, floor) {
				pruned[token] = true
				delete(grants, token)
				break
			}
		}
	}
	for key := range closes {
		token, _, _ := strings.Cut(key, ":")
		if pruned[token] {
			delete(closes, key)
		}
	}
	return len(pruned)
}

// retire runs RETIRE. On a private-validation bridge a RETIRED reply means
// the provider pruned its protected snapshot in the same transaction, so the
// session is reloaded from it (as after READDIR_CLEANUP) before the reply and
// the session rows that go with it are persisted. resultMu keeps a CALL from
// protecting a snapshot copied from the pre-prune state meanwhile.
func (s *Service) retire(ctx context.Context, c Command) (json.RawMessage, error) {
	private := s.privateValidation()
	if private {
		s.resultMu.Lock()
		defer s.resultMu.Unlock()
	}
	var b json.RawMessage
	var err error
	if c.Protocol != nil && IsControlledProtocol(*c.Protocol) {
		b, err = s.retireControlled(ctx, c)
	} else {
		b, err = s.config.Claims.Retire(ctx, c.Context, bytes.Clone(c.Payload))
	}
	if err == nil {
		err = validateControlResponse(c, b)
	}
	if err != nil || !private {
		return b, err
	}
	var reply struct{ Status string }
	if json.Unmarshal(b, &reply) == nil && reply.Status == "RETIRED" {
		err = s.restoreValidation(context.WithoutCancel(ctx), c.Context)
	}
	return b, err
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
