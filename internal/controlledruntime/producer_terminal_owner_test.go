//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type terminalOldCell struct {
	guest, native guestbridge.Ack
	control       bool
}

// The OS/native headers and kernel observer remain the existing explicit
// sourceRig primitives. Every journal, provider, controller, C8 worker, mapping,
// protection transaction and receipt below is the actual production component.
func terminalIssueOld(t *testing.T, r *sourceRig, control bool) terminalOldCell {
	t.Helper()
	op, value := "HELLO", any(guestbridge.TerminalProtocol())
	if control {
		op = "RESUME"
		value = map[string]any{"previous_session": r.scope.Session,
			"ledger_hash": hashBytes([]byte("ledger")), "claims_hash": hashBytes([]byte("claims")), "domains_hash": hashBytes([]byte("domains"))}
	}
	_, _, _, response, err := r.mappedExecute(op, value)
	require(t, err)
	var fields map[string]json.RawMessage
	require(t, json.Unmarshal(response, &fields))
	fields["cell"], err = canonicalJSON(*r.mappingNativeCell)
	require(t, err)
	native, err := canonicalJSON(fields)
	require(t, err)
	return terminalOldCell{guest: guestbridge.Ack{Cell: r.cells, ResponseHash: hashBytes(response)},
		native: guestbridge.Ack{Cell: *r.mappingNativeCell, ResponseHash: hashBytes(native)}, control: control}
}

func terminalPublishCut(t *testing.T, r *sourceRig, old []terminalOldCell, funding int) (guestbridge.TerminalRequest, *guestbridge.Ack, *guestbridge.Ack, *bool) {
	t.Helper()
	var guestAck, nativeAck *guestbridge.Ack
	var donorControl *bool
	settled := []guestbridge.Ack{}
	for i := range old {
		if i == funding {
			guestAck, nativeAck, donorControl = &old[i].guest, &old[i].native, &old[i].control
		} else {
			settled = append(settled, old[i].guest)
		}
	}
	snapshot := guestbridge.TerminalSnapshot{Version: "homelabfs-guest-terminal/1", Scope: r.scope,
		Controller: r.supervisor.record.Controller.String(), RetirementID: testUUID(222).String(), Revision: 2,
		GuestThrough: r.cells, Donor: guestAck, SettledCells: settled, Credentials: r.credentials, Phase: "TERMINAL_PENDING"}
	raw, err := canonicalJSON(snapshot)
	require(t, err)
	observation := r.admission.installed
	current, err := os.ReadFile(r.observationFile)
	require(t, err)
	require(t, json.Unmarshal(current, &observation))
	observation.Admission.Observations = append([]kernelDomainObservation(nil), observation.Admission.Observations...)
	for i := range observation.Admission.Observations {
		if observation.Admission.Observations[i].Scope == r.scope {
			observation.Admission.Observations[i].Revision = snapshot.Revision
			observation.Admission.Observations[i].Phase = "TERMINAL_PENDING"
			observation.Admission.Observations[i].Terminal = &snapshot
		}
	}
	observed, err := canonicalJSON(observation)
	require(t, err)
	require(t, os.WriteFile(r.observationFile, observed, 0600))
	return guestbridge.TerminalRequest{RetirementID: snapshot.RetirementID, Credentials: r.credentials,
		GuestThrough: r.cells, ObserverRevision: snapshot.Revision, ClosureHash: hashBytes(raw), SettledCells: settled}, guestAck, nativeAck, donorControl
}

func terminalInspect(t *testing.T, r *sourceRig) (guestbridge.ProtectedTransportState, []guestbridge.ProtectedTransportCell) {
	t.Helper()
	r.mappingTransport.Close()
	var cells []guestbridge.ProtectedTransportCell
	state, err := guestbridge.InspectTransport(r.mappingDirectory, func(c guestbridge.ProtectedTransportCell) error {
		cells = append(cells, c)
		return nil
	})
	require(t, err)
	return state, cells
}

func TestTerminalProducerActualMultipleCellOwnerTransfer(t *testing.T) {
	for _, test := range []struct {
		name              string
		ordinary, control int
		funding           int
		emptyGroups       bool
	}{
		{"never-hello", 0, 0, -1, false},
		{"sole-ordinary", 1, 0, 0, false},
		{"empty-supplementary-groups", 1, 0, 0, true},
		{"mixed-ordinary-funding", 2, 1, 0, false},
		{"mixed-control-funding", 1, 1, 1, false},
		{"all-80-old-cells", 64, 16, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var captures []guestbridge.Credentials
			if test.emptyGroups {
				captures = []guestbridge.Credentials{{FSUID: 1000, FSGID: 1001, Groups: []uint32{},
					Process: testUUID(62).String(), Revision: 1, IDMapHash: hashBytes([]byte("map")), UserNamespace: testUUID(64).String()}}
			}
			r := newSourceRigDomainsBootstrap(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false, captures...)
			if test.emptyGroups {
				r.credentials = captures[0]
			}
			old := []terminalOldCell{}
			for i := 0; i < test.ordinary+test.control; i++ {
				old = append(old, terminalIssueOld(t, r, i >= test.ordinary))
			}
			request, guestAck, nativeAck, donorControl := terminalPublishCut(t, r, old, test.funding)
			_, receipt, original, response, err := r.mappedExecuteReclaim("SESSION_RETIRE", request, guestAck, nativeAck, donorControl)
			require(t, err)
			if receipt == nil || receipt.Kind != "SESSION_RETIRED" || receipt.PollCount != 1 {
				t.Fatal("actual terminal worker failed to produce exact receipt")
			}
			_, err = guestbridge.VerifyMappedReceipt(r.mappedBinding(), original, response)
			require(t, err)
			terminalNative := *r.mappingNativeCell
			// Another scope continues on the original shared owner/allocator.
			_, _, _, _, err = r.peers[1].mappedExecute("HELLO", guestbridge.TerminalProtocol())
			require(t, err)
			if *r.mappingNativeCell != uint64(len(old)+2) {
				t.Fatal("shared native highwater changed or stopped")
			}
			state, cells := terminalInspect(t, r)
			finalControl := test.funding < 0 || old[test.funding].control
			ordinary, control := 2, 0 // The other scope's HELLO is ordinary.
			if finalControl {
				ordinary, control = 1, 1
			}
			if state.Workspace || len(cells) != 2 || state.Ordinary != ordinary || state.Control != control || state.Terminal[r.scope.Session] != terminalNative || state.GuestHigh[r.scope.Session] != uint64(len(old)+1) {
				t.Fatalf("wrong retained cell/accounting cut: %+v", state)
			}
			found := false
			for _, cell := range cells {
				if cell.Header.Cell != terminalNative {
					continue
				}
				found = true
				if cell.Terminal == nil || cell.Terminal.Phase != "TERMINAL_RESPONDED" || len(cell.Terminal.Donors) != max(0, len(old)-1) || cell.Terminal.OldOrdinary != test.ordinary || cell.Terminal.OldControl != test.control || cell.Control != finalControl {
					t.Fatal("final owner lost original donor/class accounting")
				}
				var fields map[string]json.RawMessage
				require(t, json.Unmarshal(cell.Response, &fields))
				fields["cell"], err = canonicalJSON(uint64(len(old) + 1))
				require(t, err)
				translated, err := canonicalJSON(fields)
				require(t, err)
				if !bytes.Equal(translated, response) {
					t.Fatal("durable final response changed after handoff")
				}
			}
			if !found {
				t.Fatal("final native owner was released")
			}
		})
	}
}

// Send the actual mapped command without consuming a result or using absence
// as proof. Refusal tests synchronize with the actual private worker hook.
func terminalSend(t *testing.T, r *sourceRig, request guestbridge.TerminalRequest, guestAck, nativeAck *guestbridge.Ack, donorControl *bool) (uint64, error) {
	t.Helper()
	r.cells++
	*r.mappingNativeCell++
	protocol := guestbridge.TerminalProtocol()
	payload, err := canonicalJSON(request)
	require(t, err)
	command := guestbridge.Command{Cell: *r.mappingNativeCell, Context: r.scope, Protocol: &protocol,
		Op: "SESSION_RETIRE", Payload: payload, Reclaim: nativeAck,
		Delegation: &guestbridge.Delegation{Version: guestbridge.DelegationVersion, GuestCell: r.cells, GuestOp: "SESSION_RETIRE", GuestReclaim: guestAck}}
	raw, err := canonicalJSON(command)
	require(t, err)
	flags := uint16(1)
	if nativeAck != nil {
		flags |= 2
		if donorControl != nil && !*donorControl {
			flags &^= 1
		}
	}
	for offset := 0; offset < len(raw); offset += guestbridge.ChunkBytes {
		wire, err := guestbridge.EncodeFrame(guestbridge.Frame{Minor: 4, Kind: 1, Flags: flags,
			Session: parseRuntimeUUID(r.scope.Session), Cell: command.Cell, Total: uint32(len(raw)),
			Offset: uint32(offset), Hash: sha256.Sum256(raw), Data: raw[offset:min(offset+guestbridge.ChunkBytes, len(raw))]})
		require(t, err)
		_, err = r.mappingTransport.Write(t.Context(), r.native, r.native.Endpoint.CommandNode,
			command.Cell*guestbridge.CellStride+uint64(offset/guestbridge.ChunkBytes)*(guestbridge.HeaderBytes+guestbridge.ChunkBytes), wire)
		if err != nil {
			return command.Cell, err
		}
	}
	return command.Cell, nil
}

func TestTerminalProducerMalformedOldCutPreservesActualOwners(t *testing.T) {
	for _, name := range []string{"omission", "duplicate", "funding-repeated", "changed-full-hash", "future", "other-scope", "wrong-native-funding-hash"} {
		t.Run(name, func(t *testing.T) {
			r := newSourceRigDomainsUnstarted(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")))
			old := []terminalOldCell{terminalIssueOld(t, r, false), terminalIssueOld(t, r, false), terminalIssueOld(t, r, true)}
			peer := r.peers[1]
			terminalIssueOld(t, peer, false)
			foreign := terminalIssueOld(t, peer, false)
			request, guestAck, nativeAck, donorControl := terminalPublishCut(t, r, old, 0)
			switch name {
			case "omission":
				request.SettledCells = request.SettledCells[:1]
			case "duplicate":
				request.SettledCells[1] = request.SettledCells[0]
			case "funding-repeated":
				request.SettledCells = append([]guestbridge.Ack{*guestAck}, request.SettledCells...)
			case "changed-full-hash":
				request.SettledCells[0].ResponseHash = hashBytes([]byte("unobserved"))
			case "future":
				request.SettledCells[1].Cell = r.cells + 1
			case "other-scope":
				request.SettledCells[0] = foreign.guest // Same guest cell number; different original scope/full reply.
			case "wrong-native-funding-hash":
				copy := *nativeAck
				copy.ResponseHash = hashBytes([]byte("wrong native reply"))
				nativeAck = &copy
			}
			if _, err := terminalSend(t, r, request, guestAck, nativeAck, donorControl); err == nil {
				t.Fatal("malformed actual old cut accepted")
			}
			state, cells := terminalInspect(t, r)
			if state.High != 5 || len(state.Terminal) != 0 || !state.Workspace || state.Ordinary != 4 || state.Control != 1 || state.GuestHigh[r.scope.Session] != 3 || state.GuestHigh[peer.scope.Session] != 2 {
				t.Fatalf("refusal changed old ownership, credit or issuance cut: %+v", state)
			}
			// The extra row is the retained, unissued replacement workspace.
			if len(cells) != 6 {
				t.Fatalf("lost actual old cells on rejection: %d", len(cells))
			}
		})
	}
}

func TestTerminalProducerActualOpenClaimRefusesSeal(t *testing.T) {
	r := newSourceRigDomainsUnstarted(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")))
	old := []terminalOldCell{terminalIssueOld(t, r, false)}
	claim := guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(),
		Nonce: testUUID(103).String(), Number: 1, OriginalHash: hashBytes([]byte("unsent original"))}
	require(t, r.provider.Allocate(t.Context(), r.scope, claim))
	checked := make(chan error, 1)
	var once sync.Once
	r.producer.terminalFailpoint = func(ctx context.Context, a *guestbridge.TerminalAttempt, _ string) error {
		once.Do(func() {
			_, err := r.provider.SealTerminal(ctx, a)
			checked <- err
		})
		return fsapi.Permission
	}
	request, guestAck, nativeAck, donorControl := terminalPublishCut(t, r, old, 0)
	cell, err := terminalSend(t, r, request, guestAck, nativeAck, donorControl)
	require(t, err)
	select {
	case err := <-checked:
		if !errors.Is(err, fsapi.WouldBlock) {
			t.Fatalf("actual open claim did not block provider seal: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual terminal worker did not run")
	}
	if _, err := r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("open claim exposed a terminal result: %v", err)
	}
	_, _, _, _, err = r.peers[1].mappedExecute("HELLO", guestbridge.TerminalProtocol())
	require(t, err)
	state, cells := terminalInspect(t, r)
	if state.Terminal[r.scope.Session] != cell {
		t.Fatal("refused terminal attempt lost its permanent pending fence")
	}
	for _, retained := range cells {
		if retained.Header.Cell == cell && (retained.Response != nil || retained.Terminal == nil || retained.Terminal.Phase != "TERMINAL_PENDING" || retained.Terminal.Ownership != nil) {
			t.Fatal("open claim acquired sealed or responded authority")
		}
	}
}

func TestTerminalProducerOtherScopeContinuesDuringBridgeProtection(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	r := newSourceRigDomainsBootstrapFailpoint(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false,
		func(index int, stage string) error {
			if index == 0 && stage == "terminal-bridge-protect" {
				enterOnce.Do(func() { close(entered) })
				<-release
			}
			return nil
		})
	// Register after the fixture so failure releases the actual writer before
	// normal LIFO transport/ledger cleanup joins its workers.
	t.Cleanup(unblock)
	old := []terminalOldCell{terminalIssueOld(t, r, false)}
	request, guestAck, nativeAck, donorControl := terminalPublishCut(t, r, old, 0)
	cell, err := terminalSend(t, r, request, guestAck, nativeAck, donorControl)
	require(t, err)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("actual bridge lifetime writer was not reached")
	}
	// This is the real other-scope framing/worker/response path while the first
	// scope's actual bbolt writer is parked. No lock ownership is simulated.
	finished := make(chan error, 1)
	go func() {
		_, _, _, _, err := r.peers[1].mappedExecute("HELLO", guestbridge.TerminalProtocol())
		finished <- err
	}()
	select {
	case err := <-finished:
		require(t, err)
	case <-time.After(5 * time.Second):
		unblock()
		finishedErr := <-finished
		t.Fatalf("other scope blocked behind terminal bridge persistence: %v", finishedErr)
	}
	if raw, err := r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768); len(raw) != 0 || !errors.Is(err, fsapi.WouldBlock) {
		t.Fatal("bridge success escaped before actual protected lifetime commit", err)
	}
	unblock()
	// Observe actual committed bytes before Close, which deliberately prevents
	// further signing/worker use rather than authorizing completion itself.
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768)
		if errors.Is(err, fsapi.WouldBlock) && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
			continue
		}
		require(t, err)
		if len(raw) <= guestbridge.HeaderBytes {
			t.Fatal("terminal worker exposed no actual result frame")
		}
		break
	}
	state, cells := terminalInspect(t, r)
	if len(cells) != 2 || state.Terminal[r.scope.Session] != cell {
		t.Fatal("concurrent scope changed retained final ownership")
	}
	for _, retained := range cells {
		if retained.Header.Cell == cell && (retained.Terminal == nil || retained.Terminal.Phase != "TERMINAL_RESPONDED" || retained.Response == nil) {
			t.Fatal("terminal worker did not finish the protected handoff")
		}
	}
}

func TestTerminalProducerPartialSealsRetainSameLiveAttempt(t *testing.T) {
	for _, point := range []struct {
		name  string
		index int
		stage string
	}{
		{"provider-sealed", 0, "terminal-provider-sealed"},
		{"controller-sealed", -1, "terminal-controller-sealed"},
		{"bridge-before-protection", 0, "terminal-bridge-protect"},
	} {
		t.Run(point.name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var enterOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			fired := false
			r := newSourceRigDomainsBootstrapFailpoint(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false, func(index int, stage string) error {
				if index == point.index && stage == point.stage && !fired {
					fired = true
					enterOnce.Do(func() { close(entered) })
					<-release
					return fsapi.WouldBlock
				}
				return nil
			})
			t.Cleanup(unblock)
			old := []terminalOldCell{terminalIssueOld(t, r, false), terminalIssueOld(t, r, true)}
			request, guestAck, nativeAck, donorControl := terminalPublishCut(t, r, old, 0)
			cell, err := terminalSend(t, r, request, guestAck, nativeAck, donorControl)
			require(t, err)
			guestCell := r.cells
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("actual partial seal boundary not reached")
			}
			if raw, err := r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768); len(raw) != 0 || !errors.Is(err, fsapi.WouldBlock) {
				t.Fatal("terminal response exposed before seal completion", err)
			}
			_, _, _, _, err = r.peers[1].mappedExecute("HELLO", guestbridge.TerminalProtocol())
			require(t, err)
			unblock()
			deadline := time.Now().Add(5 * time.Second)
			var wire []byte
			for {
				wire, err = r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768)
				if errors.Is(err, fsapi.WouldBlock) && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
					continue
				}
				require(t, err)
				break
			}
			frame, err := guestbridge.DecodeFrame(wire)
			require(t, err)
			if frame.Cell != cell || frame.Offset != 0 || int(frame.Total) != len(frame.Data) {
				t.Fatal("incomplete actual terminal result frame")
			}
			var fields map[string]json.RawMessage
			require(t, json.Unmarshal(frame.Data, &fields))
			fields["cell"] = mustRuntimeJSON(t, guestCell)
			response := mustRuntimeJSON(t, fields)
			protocol := guestbridge.TerminalProtocol()
			original := mustRuntimeJSON(t, guestbridge.Command{Cell: guestCell, Context: r.scope, Protocol: &protocol, Op: "SESSION_RETIRE", Payload: mustRuntimeJSON(t, request), Reclaim: guestAck})
			receipt, err := guestbridge.VerifyMappedReceipt(r.mappedBinding(), original, response)
			require(t, err)
			if receipt.Kind != "SESSION_RETIRED" || receipt.PollCount != 1 || r.cells != guestCell || *r.mappingNativeCell != cell+1 {
				t.Fatal("retry advanced original guest/native candidate or lost proof")
			}
			state, cells := terminalInspect(t, r)
			if state.Workspace || state.Ordinary != 2 || state.Control != 0 || len(cells) != 2 || state.Terminal[r.scope.Session] != cell {
				t.Fatalf("partial seal retry lost retained final owner: %+v", state)
			}
			for _, owner := range cells {
				if owner.Header.Cell == cell && (owner.Terminal == nil || owner.Terminal.Phase != "TERMINAL_RESPONDED" || len(owner.Terminal.Seals) != 3) {
					t.Fatal("completed actual candidate lacks three protected seals")
				}
			}
		})
	}
}

// These are failures of the actual C8 owner, never a positive replacement-owner
// recovery. Protected records are inspected only after the original endpoint
// is fenced and closed; a persisted signature supplies no restored authority.
func TestTerminalProducerProtectionCrashCutsExposeNoReply(t *testing.T) {
	for _, point := range []string{"protected-terminal-sealed", "protected-mapping-base", "before-full-response-commit", "protected-response"} {
		t.Run(point, func(t *testing.T) {
			var active, full atomic.Bool
			fired := make(chan struct{})
			sentinel := errors.New("actual terminal protection boundary unavailable")
			r := newSourceRigDomainsBootstrapFailpoint(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false, func(index int, stage string) error {
				if index != -2 || !active.Load() {
					return nil
				}
				selected := stage == point || point == "before-full-response-commit" && stage == "before-commit" && full.Load()
				if selected && active.CompareAndSwap(true, false) {
					close(fired)
					return sentinel
				}
				return nil
			})
			r.producer.terminalFailpoint = func(ctx context.Context, a *guestbridge.TerminalAttempt, stage string) error {
				if _, ok := guestbridge.VerifiedMapping(ctx); ok {
					full.Store(true)
				}
				return nil
			}
			old := []terminalOldCell{terminalIssueOld(t, r, false), terminalIssueOld(t, r, true)}
			request, guestAck, nativeAck, donorControl := terminalPublishCut(t, r, old, 0)
			active.Store(true)
			cell, err := terminalSend(t, r, request, guestAck, nativeAck, donorControl)
			require(t, err)
			select {
			case <-fired:
			case <-time.After(5 * time.Second):
				t.Fatal("actual C8 protection boundary not reached")
			}
			raw, err := r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768)
			if len(raw) != 0 || err == nil {
				t.Fatal("terminal reply escaped failed protection", err)
			}
			state, cells := terminalInspect(t, r)
			if state.High != cell || state.Terminal[r.scope.Session] != cell || len(cells) != 1 || state.Workspace || state.Ordinary != 1 || state.Control != 0 {
				t.Fatalf("crash cut lost original terminal owner: %+v", state)
			}
			owner := cells[0]
			if owner.Terminal == nil || owner.Terminal.Ownership == nil || len(owner.Terminal.Seals) != 3 || len(owner.Terminal.Donors) != 1 || owner.Terminal.OldOrdinary != 1 || owner.Terminal.OldControl != 1 {
				t.Fatal("partial protection lost exact manifest/seal owners")
			}
			wantPhase := "TERMINAL_SEALED"
			if point == "protected-response" {
				wantPhase = "TERMINAL_RESPONDED"
			}
			if owner.Terminal.Phase != wantPhase {
				t.Fatalf("wrong protected crash phase %s want%s", owner.Terminal.Phase, wantPhase)
			}
			if point == "protected-terminal-sealed" {
				if owner.Mapping.NativeBaseHash != "" {
					t.Fatal("base appears before protected seal boundary")
				}
			} else if owner.Mapping.NativeBaseHash == "" {
				t.Fatal("protected response base lost")
			}
			if point != "protected-response" {
				if len(owner.Response) != 0 || owner.Mapping.NativeFullHash != "" {
					t.Fatal("uncommitted signed response appears durable")
				}
			} else {
				if len(owner.Response) == 0 || owner.Mapping.NativeFullHash != hashBytes(owner.Response) {
					t.Fatal("actual protected full signed response lost")
				}
				var fields map[string]json.RawMessage
				require(t, json.Unmarshal(owner.Response, &fields))
				fields["cell"] = mustRuntimeJSON(t, r.cells)
				response := mustRuntimeJSON(t, fields)
				protocol := guestbridge.TerminalProtocol()
				original := mustRuntimeJSON(t, guestbridge.Command{Cell: r.cells, Context: r.scope, Protocol: &protocol, Op: "SESSION_RETIRE", Payload: mustRuntimeJSON(t, request), Reclaim: guestAck})
				receipt, err := guestbridge.VerifyMappedReceipt(r.mappedBinding(), original, response)
				require(t, err)
				if receipt.Kind != "SESSION_RETIRED" {
					t.Fatal("protected terminal record is not the real signed response")
				}
			}
			if raw, err = r.mappingTransport.Read(t.Context(), r.native, r.native.Endpoint.ResultNode, cell*guestbridge.CellStride, 32768); len(raw) != 0 || err == nil {
				t.Fatal("closed/inspected ledger revived native reads", err)
			}
		})
	}
}

// Permanent finals exhaust the existing control capacity. An unrelated new
// scope cannot evict a retired identity or recycle its native number.
func TestTerminalProducerPermanentControlExhaustion(t *testing.T) {
	r := newSourceRigDomainsUnstarted(t, guestbridge.TerminalProtocol(), 17, hashBytes([]byte("map")))
	responses := make(map[uint64][]byte)
	for i, peer := range r.peers[:16] {
		request, ga, na, control := terminalPublishCut(t, peer, nil, -1)
		_, receipt, original, response, err := peer.mappedExecuteReclaim("SESSION_RETIRE", request, ga, na, control)
		require(t, err)
		if receipt == nil || receipt.Kind != "SESSION_RETIRED" {
			t.Fatal("missing terminal proof")
		}
		_, err = guestbridge.VerifyMappedReceipt(peer.mappedBinding(), original, response)
		require(t, err)
		responses[uint64(i+1)] = append([]byte(nil), response...)
	}
	peer := r.peers[16]
	request, ga, na, control := terminalPublishCut(t, peer, nil, -1)
	_, _, _, _, err := peer.mappedExecuteReclaim("SESSION_RETIRE", request, ga, na, control)
	if !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("permanent finals must exhaust control credits: %v", err)
	}
	state, cells := terminalInspect(t, r)
	if state.Control != 16 || state.Ordinary != 0 || len(cells) != 16 || len(state.Terminal) != 16 {
		t.Fatalf("permanent final owners evicted/refunded: %+v", state)
	}
	for _, cell := range cells {
		if cell.Terminal == nil || cell.Terminal.Phase != "TERMINAL_RESPONDED" || !cell.Control || state.Terminal[cell.Header.Session.String()] != cell.Header.Cell {
			t.Fatal("lost permanent terminal identity")
		}
		// Translate only the guest/native cell number, exactly as the mapper does.
		var fields map[string]json.RawMessage
		require(t, json.Unmarshal(cell.Response, &fields))
		fields["cell"], err = canonicalJSON(uint64(1))
		require(t, err)
		translated, err := canonicalJSON(fields)
		require(t, err)
		if !bytes.Equal(translated, responses[cell.Header.Cell]) {
			t.Fatal("retained signed terminal bytes changed")
		}
	}
}
