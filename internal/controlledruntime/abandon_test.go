//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// allocateOnly allocates the rig's next ordinary number without its CALL,
// so the entry stays ALLOCATED with its original credit charged.
func (r *sourceRig) allocateOnly(op string, args any) guestbridge.Call {
	r.t.Helper()
	r.claims++
	raw, err := canonicalJSON(args)
	require(r.t, err)
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	original, err := canonicalJSON(intent)
	require(r.t, err)
	call := guestbridge.Call{Claim: guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: r.claims, OriginalHash: hashBytes(original)}, Credentials: r.credentials, Intent: intent}
	_, _, err = r.execute("ALLOCATE", call)
	require(r.t, err)
	return call
}

// abandonOutcomes sends one ABANDON of call and returns its outcomes.
func (r *sourceRig) abandonOutcomes(call guestbridge.Call) []struct {
	Number    uint64
	Status    string
	ProofHash *string `json:"proof_hash"`
} {
	r.t.Helper()
	c := call.Claim
	payload := map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "claims": []any{map[string]any{"number": c.Number, "original_hash": c.OriginalHash}}}
	reply, receipt, err := r.execute("ABANDON", payload)
	require(r.t, err)
	if receipt != nil {
		r.t.Fatalf("ABANDON carried a verification receipt %+v", receipt)
	}
	var out struct {
		Claims []struct {
			Number    uint64
			Status    string
			ProofHash *string `json:"proof_hash"`
		}
	}
	require(r.t, json.Unmarshal(reply, &out))
	return out.Claims
}

// TestRuntimeAbandonThroughRouterAndProducer runs ABANDON through the real
// runtime (ProviderRouter, Producer preparation, Facts authorization and,
// for 1.4, the mapped transport): the first session's allocated number is
// declined with a proof that retires it, its OBSERVE counters move, and the
// second session naming the same number reaches only its own ledger
// (UNKNOWN there) and leaves the first session's claim alone.
func TestRuntimeAbandonThroughRouterAndProducer(t *testing.T) {
	for _, selected := range []struct {
		name     string
		protocol guestbridge.Protocol
	}{{"controlled1.2", guestbridge.ControlledProtocol()}, {"mapped1.4", guestbridge.MappedProtocol()}} {
		t.Run(selected.name, func(t *testing.T) {
			r := newSourceRigDomains(t, selected.protocol, 2)
			first, second := r.peers[0], r.peers[1]
			var root struct{ ID string }
			require(t, json.Unmarshal(first.call("ROOT", map[string]any{}), &root))
			call := first.allocateOnly("ROOT", map[string]any{})
			if out := second.abandonOutcomes(call); len(out) != 1 || out[0].Status != "UNKNOWN" {
				t.Fatalf("another session's ABANDON reached the first ledger: %+v", out)
			}
			out := first.abandonOutcomes(call)
			if len(out) != 1 || out[0].Status != "ABANDONED" || out[0].ProofHash == nil {
				t.Fatalf("own ABANDON %+v", out)
			}
			evidence, err := first.provider.LedgerEvidence(t.Context())
			require(t, err)
			if evidence.Claims.Abandoned != 1 || evidence.Claims.AbandonedCredit == 0 {
				t.Fatalf("OBSERVE counters %d/%d", evidence.Claims.Abandoned, evidence.Claims.AbandonedCredit)
			}
			c := call.Claim
			retire := map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "through": c.Number, "result_hashes": []string{root.ID, *out[0].ProofHash}, "credentials": first.credentials}
			reply, _, err := first.execute("RETIRE", retire)
			require(t, err)
			var floor struct{ Status string }
			require(t, json.Unmarshal(reply, &floor))
			if floor.Status != "RETIRED" {
				t.Fatalf("abandon proof did not retire: %s", reply)
			}
		})
	}
}
