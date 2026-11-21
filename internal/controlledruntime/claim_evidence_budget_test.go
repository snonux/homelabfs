//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// maxEnrolledDomains is assembly's domain bound (one provider per domain).
const maxEnrolledDomains = 16

// claimEvidenceReserveBytes is the share of an OBSERVE reply left for every
// section other than the provider claim summaries and the recall history:
// the transport index, the controller ledger and the envelope. Those are not
// bounded by this test; an OBSERVE that still overflows is refused whole
// (oversizedReply), never truncated.
const claimEvidenceReserveBytes = 16 << 10

// TestObserveClaimEvidenceFitsReply (a84 review): with the refusal traces,
// the provider claim summaries of the maximum number of providers, each at
// controlledvalidation.MaxClaimEvidenceBytes (pinned by its
// TestClaimEvidenceWorstCaseBound), plus their provider wrappers and the
// bounded recall history still leave claimEvidenceReserveBytes of the
// lifecycle reply bound for everything else.
func TestObserveClaimEvidenceFitsReply(t *testing.T) {
	wrapper := providerEvidence{
		Session: strings.Repeat("f", 36),
		Error:   "",
		Ledger: &controlledvalidation.LedgerEvidence{
			Ledger: strings.Repeat("f", 36), Digest: strings.Repeat("f", 64),
			Device: ^uint64(0), Inode: ^uint64(0), Bytes: 1<<63 - 1,
		},
	}
	raw, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	perProvider := len(raw) + controlledvalidation.MaxClaimEvidenceBytes + 1 // separator
	used := maxEnrolledDomains*perProvider + viewcontrol.RecallEvidenceBytes
	if used+claimEvidenceReserveBytes > lifecycleReplyBytes {
		t.Fatalf("claim summaries %d + recall history %d leave %d of %d reply bytes, want at least %d",
			maxEnrolledDomains*perProvider, viewcontrol.RecallEvidenceBytes, lifecycleReplyBytes-used, lifecycleReplyBytes, claimEvidenceReserveBytes)
	}
	t.Logf("%d providers x %d + recall %d = %d of %d bytes", maxEnrolledDomains, perProvider, viewcontrol.RecallEvidenceBytes, used, lifecycleReplyBytes)
}
