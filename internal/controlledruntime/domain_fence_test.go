//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// TestRuntimeAssemblySecondDomainFirstLeaseAfterBootstrapRecall replays the
// zq3-r6 mount order against the assembled runtime (R6-1). Mount 0 (domain
// 1) runs the bootstrap: HELLO, ROOT, an early VIEW_WAIT, VIEW_ACQUIRE,
// VIEW_RELEASE and the complete recall it starts. Mount 1 (domain 2) only
// then sends HELLO and its first VIEW_ACQUIRE. The guest kernel keeps one
// fence per mount, starting at 1, so domain 2's first lease must carry fence
// 1; a controller-wide fence stamped 2 there and the guest refused the mount
// with EKEYREJECTED. Domain 1's next lease carries its own advanced fence 2.
func TestRuntimeAssemblySecondDomainFirstLeaseAfterBootstrapRecall(t *testing.T) {
	r, c := assemblyFixture(t)
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	for _, peer := range r.peers {
		attachAssembly(peer, a, &counter)
	}
	families := c.Authorization.Enrollment.TicketFamilies
	first, domain := bootstrapMount(t, r.peers[0], families[0])
	if first.Fence != 1 {
		t.Fatalf("mount 0 bootstrap lease fence %d, want 1", first.Fence)
	}

	second := r.peers[1]
	_, _, err = second.execute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(second.call("ROOT", map[string]any{}), &root))
	lease := acquireDomainLease(t, second, families[1], 1, root.Capabilities[1])
	if lease.Fence != 1 {
		t.Fatalf("mount 1 first lease after mount 0's recall: fence %d, want 1", lease.Fence)
	}
	if again := acquireDomainLease(t, r.peers[0], families[0], 2, domain); again.Fence != 2 {
		t.Fatalf("mount 0 lease after its own recall: fence %d, want 2", again.Fence)
	}

	// Mount 1's clean READ release recalls only mount 1 (task 784): mount 0
	// gets no event and keeps fence 2. Mount 1's kernel observation reports
	// its own fence 1 while mount 0 is at 2, and the physical Facts must match
	// the ACK against the acknowledging domain's ledger fence.
	control := guestbridge.Ticket{Anchor: families[1].Control.Anchor, Nonce: families[1].Control.Nonce, Number: 1, Credentials: second.credentials}
	payload, _, _, _, err := second.mappedExecute("VIEW_RELEASE", map[string]any{"request": control, "lease": lease})
	require(t, err)
	if !bytes.Contains(payload, []byte(`"status":"PENDING"`)) {
		t.Fatalf("mount 1 VIEW_RELEASE: %s", payload)
	}
	idle := (&r5Chain{peer: r.peers[0]}).exec(t, "VIEW_WAIT", map[string]any{"domain": domain, "after_sequence": 0})
	if bytes.Contains(idle, []byte(`"EVENT"`)) {
		t.Fatalf("mount 0 received an event of mount 1's scoped recall: %s", idle)
	}
	fencedRecall(t, r, []int{1}, []guestbridge.Capability{root.Capabilities[1]}, []uint64{1}, []uint64{1})
	if after := acquireDomainLease(t, second, families[1], 2, root.Capabilities[1]); after.Fence != 2 {
		t.Fatalf("mount 1 lease after its first recall: fence %d, want 2", after.Fence)
	}
	if after := acquireDomainLease(t, r.peers[0], families[0], 3, domain); after.Fence != 2 {
		t.Fatalf("mount 0 lease after mount 1's scoped recall: fence %d, want 2", after.Fence)
	}
}

// fencedRecall acknowledges every phase of the current recall from each
// participating mount in turn (mounts[i] is the peer index of domains[i]).
// Its kernel observation carries its own fence and starts at revision
// revisions[i]; REOPENED advances that revision, as the guest increments it
// before reopening.
func fencedRecall(t *testing.T, r *sourceRig, mounts []int, domains []guestbridge.Capability, fences, revisions []uint64) {
	t.Helper()
	chains := make([]*r5Chain, len(domains))
	for i := range domains {
		chains[i] = &r5Chain{peer: r.peers[mounts[i]]}
	}
	for _, phase := range []string{"STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		for i, chain := range chains {
			event := chain.exec(t, "VIEW_WAIT", map[string]any{"domain": domains[i], "after_sequence": 0})
			var poll struct{ Event map[string]json.RawMessage }
			require(t, json.Unmarshal(event, &poll))
			if len(poll.Event) == 0 {
				t.Fatalf("mount %d missing %s event %s", mounts[i], phase, event)
			}
			ack := map[string]any{"domain": domains[i], "sequence": poll.Event["sequence"], "recall": poll.Event["recall"], "phase": phase, "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": chain.peer.scope.Session, "manifest": nil, "target": poll.Event["target"]}
			if phase == "STOPPED" {
				ack["target"] = nil
			}
			if phase == "STOPPED" || phase == "DRAINED" {
				ack["manifest"] = map[string]any{"entries": []any{}, "hash": hashBytes([]byte("[]"))}
			}
			revision := revisions[i]
			if phase == "REOPENED" {
				revision++
			}
			observeMountACK(t, r, mounts[i], poll.Event, ack, revision, fences[i])
			chain.ack(t, phase, ack)
		}
	}
}

// observeMountACK records the declared kernel observation of mount index for
// one exact ACK, with that mount's own revision and fence.
func observeMountACK(t *testing.T, r *sourceRig, index int, event map[string]json.RawMessage, ack map[string]any, revision, fence uint64) {
	t.Helper()
	raw, err := canonicalJSON(ack)
	require(t, err)
	issued, err := canonicalJSON(event)
	require(t, err)
	r.installed.Admission.Observations[index] = kernelDomainObservation{Scope: r.peers[index].scope, Revision: revision, Fence: fence, Phase: ack["phase"].(string), EventHash: hashBytes(issued), AcknowledgementHash: hashBytes(raw)}
	updated, err := canonicalJSON(r.installed)
	require(t, err)
	require(t, os.WriteFile(r.observationFile, updated, 0600))
}

// bootstrapMount runs the zq3-r6 mount-0 bootstrap on peer and returns its
// first lease and domain capability. It is TestRuntimeAssemblyMountSequence-
// BeforeReady's sequence without the readiness probes.
func bootstrapMount(t *testing.T, peer *sourceRig, families viewcontrol.TicketFamilies) (guestbridge.ViewLease, guestbridge.Capability) {
	t.Helper()
	_, _, err := peer.execute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(peer.call("ROOT", map[string]any{}), &root))
	domain := root.Capabilities[1]
	wait := map[string]any{"domain": domain, "after_sequence": 0}
	_, _, _, _, err = peer.mappedExecute("VIEW_WAIT", wait)
	require(t, err)
	guestWait, emptyLeg := peer.cells, peer.mappingLastNative
	lease := acquireDomainLease(t, peer, families, 1, domain)
	control := guestbridge.Ticket{Anchor: families.Control.Anchor, Nonce: families.Control.Nonce, Number: 1, Credentials: peer.credentials}
	payload, _, _, _, err := peer.mappedExecute("VIEW_RELEASE", map[string]any{"request": control, "lease": lease})
	require(t, err)
	if !bytes.Contains(payload, []byte(`"status":"PENDING"`)) {
		t.Fatalf("VIEW_RELEASE: %s", payload)
	}
	recall, _, _, _, err := peer.mappedWaitContinuation(guestWait, 1, wait, emptyLeg)
	require(t, err)
	if !bytes.Contains(recall, []byte(`"phase":"RECALL"`)) {
		t.Fatalf("continued VIEW_WAIT: %s", recall)
	}
	r5RecallAcks(t, peer, domain, control, recall, func(string) {})
	return lease, domain
}

// acquireDomainLease sends one DOMAIN READ VIEW_ACQUIRE with acquisition
// ticket number and requires GRANTED.
func acquireDomainLease(t *testing.T, peer *sourceRig, families viewcontrol.TicketFamilies, number uint64, domain guestbridge.Capability) guestbridge.ViewLease {
	t.Helper()
	ticket := guestbridge.Ticket{Anchor: families.Acquisition.Anchor, Nonce: families.Acquisition.Nonce, Number: number, Credentials: peer.credentials}
	payload, _, _, _, err := peer.mappedExecute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": domain, "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
	require(t, err)
	var acquisition struct {
		Status string
		Lease  guestbridge.ViewLease
	}
	require(t, json.Unmarshal(payload, &acquisition))
	if acquisition.Status != "GRANTED" {
		t.Fatalf("VIEW_ACQUIRE %s: %s", peer.scope.Session, payload)
	}
	return acquisition.Lease
}
