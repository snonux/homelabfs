//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"

	"github.com/snonux/homelabfs/internal/fsapi"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// This verifies host ownership composition with the existing explicitly
// substituted guest observer and native OS primitives. Actual Linux private
// consumption and physical inventory belong to the production C harness.
func TestTerminalProducerActualOwnersSharedEndpoint(t *testing.T) {
	testTerminalProducerActualOwners(t, nil)
}

func TestTerminalProducerActualWorkerInvocationExpires(t *testing.T) {
	var prior *guestbridge.TerminalAttempt
	var priorContext context.Context
	var invocations atomic.Int32
	testTerminalProducerActualOwners(t, func(p *Producer) {
		p.terminalFailpoint = func(ctx context.Context, a *guestbridge.TerminalAttempt, stage string) error {
			call := invocations.Add(1)
			if call == 1 {
				prior = a
				priorContext = ctx
				return fsapi.WouldBlock
			}
			if prior.Check(priorContext) == nil {
				t.Error("prior invocation revived during actual worker retry")
			}
			return nil
		}
	})
	if invocations.Load() < 2 || prior == nil || prior.Check(priorContext) == nil {
		t.Fatal("actual worker attempt remained usable after return")
	}
}

func testTerminalProducerActualOwners(t *testing.T, setup func(*Producer)) {
	r := newSourceRigDomainsBootstrap(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false)
	_, _, _, hello, err := r.mappedExecute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	peer := r.peers[1]
	_, _, _, _, err = peer.mappedExecute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	if setup != nil {
		setup(r.producer)
	}
	guestAck := &guestbridge.Ack{Cell: 1, ResponseHash: hashBytes(hello)}
	var fields map[string]json.RawMessage
	require(t, json.Unmarshal(hello, &fields))
	fields["cell"], err = canonicalJSON(uint64(1))
	require(t, err)
	nativeHello, err := canonicalJSON(fields)
	require(t, err)
	nativeAck := &guestbridge.Ack{Cell: 1, ResponseHash: hashBytes(nativeHello)}
	snapshot := guestbridge.TerminalSnapshot{Version: "homelabfs-guest-terminal/1", Scope: r.scope, Controller: r.supervisor.record.Controller.String(), RetirementID: testUUID(222).String(), Revision: 2, GuestThrough: 1, Donor: guestAck, SettledCells: []guestbridge.Ack{}, Credentials: r.credentials, Phase: "TERMINAL_PENDING"}
	snapshotRaw, err := canonicalJSON(snapshot)
	require(t, err)
	observation := r.admission.installed
	observation.Admission.Observations = append([]kernelDomainObservation(nil), observation.Admission.Observations...)
	for i := range observation.Admission.Observations {
		row := &observation.Admission.Observations[i]
		if row.Scope == r.scope {
			row.Phase = "TERMINAL_PENDING"
			row.Revision = 2
			row.Terminal = &snapshot
		}
	}
	observedRaw, err := canonicalJSON(observation)
	require(t, err)
	require(t, os.WriteFile(r.observationFile, observedRaw, 0600))
	request := guestbridge.TerminalRequest{RetirementID: snapshot.RetirementID, Credentials: r.credentials, GuestThrough: 1, ObserverRevision: 2, ClosureHash: hashBytes(snapshotRaw), SettledCells: []guestbridge.Ack{}}
	ordinary := false
	_, receipt, original, retired, err := r.mappedExecuteReclaim("SESSION_RETIRE", request, guestAck, nativeAck, &ordinary)
	require(t, err)
	if receipt == nil || receipt.Kind != "SESSION_RETIRED" || receipt.PollCount != 1 {
		t.Fatal("missing exact terminal receipt")
	}
	if _, err = guestbridge.VerifyMappedReceipt(r.mappedBinding(), original, retired); err != nil {
		t.Fatal(err)
	}
	// The shared endpoint/global allocator and other scope remain live.
	_, _, _, _, err = peer.mappedExecute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	if *r.mappingNativeCell != 4 {
		t.Fatal("shared allocator reset or failed to advance")
	}
	// No fresh old-scope command can follow the permanent terminal fence.
	if _, _, _, _, err = r.mappedExecute("HELLO", guestbridge.TerminalProtocol()); err == nil {
		t.Fatal("terminal scope reopened")
	}
	// Trusted local allocation is refused by the provider's actual transaction.
	if err = r.provider.Allocate(t.Context(), r.scope, guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: 1, OriginalHash: hashBytes([]byte("never-issued"))}); !errors.Is(err, fsapi.Permission) {
		t.Fatal("sealed provider admitted allocation")
	}
}
