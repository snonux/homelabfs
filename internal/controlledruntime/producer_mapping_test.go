//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func (r *sourceRig) mappedExecute(op string, value any) (json.RawMessage, *guestbridge.VerificationReceiptV2, []byte, []byte, error) {
	return r.mappedExecuteReclaim(op, value, nil, nil, nil)
}

func (r *sourceRig) mappedExecuteReclaim(op string, value any, guestAck, nativeAck *guestbridge.Ack, donorControl *bool) (json.RawMessage, *guestbridge.VerificationReceiptV2, []byte, []byte, error) {
	r.cells++
	*r.mappingNativeCell++
	p := r.installed.Admission.Protocols[r.scope.Session]
	payload, err := canonicalJSON(value)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	original := guestbridge.Command{Cell: r.cells, Context: r.scope, Protocol: &p, Op: op, Payload: payload, Reclaim: guestAck}
	guestRaw, err := canonicalJSON(original)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	native := original
	if op == "VIEW_WAIT" {
		native.Op = "VIEW_POLL"
	}
	native.Cell = *r.mappingNativeCell
	native.Reclaim = nativeAck
	if op == "RELEASE_CELL" && nativeAck != nil {
		native.Payload, _ = canonicalJSON(nativeAck)
	}
	native.Delegation = &guestbridge.Delegation{Version: guestbridge.DelegationVersion, GuestCell: original.Cell, GuestOp: op, GuestReclaim: guestAck}
	nativeRaw, err := canonicalJSON(native)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	flags := uint16(1)
	if op == "HELLO" || op == "CALL" || op == "ALLOCATE" || op == "VIEW_ACQUIRE" || op == "DIRTY_RESERVE" {
		flags = 0
	}
	if nativeAck != nil {
		flags |= 2
		if donorControl != nil && !*donorControl {
			flags &^= 1
		}
	}
	hash := sha256.Sum256(nativeRaw)
	for offset := 0; offset < len(nativeRaw); offset += guestbridge.ChunkBytes {
		frame, err := guestbridge.EncodeFrame(guestbridge.Frame{Minor: 4, Kind: 1, Flags: flags, Total: uint32(len(nativeRaw)), Offset: uint32(offset), Session: parseRuntimeUUID(r.scope.Session), Cell: native.Cell, Hash: hash, Data: nativeRaw[offset:min(offset+guestbridge.ChunkBytes, len(nativeRaw))]})
		if err != nil {
			return nil, nil, nil, nil, err
		}
		_, err = r.mappingTransport.Write(r.t.Context(), r.native, r.native.Endpoint.CommandNode, native.Cell*guestbridge.CellStride+uint64(offset/guestbridge.ChunkBytes)*(guestbridge.HeaderBytes+guestbridge.ChunkBytes), frame)
		if err != nil {
			return nil, nil, nil, nil, err
		}
	}
	raw := []byte{}
	total := 1
	deadline := time.Now().Add(5 * time.Second)
	for chunk := 0; len(raw) < total; chunk++ {
		position := native.Cell*guestbridge.CellStride + uint64(chunk)*(guestbridge.HeaderBytes+guestbridge.ChunkBytes)
		var wire []byte
		node := r.native.Endpoint.ResultNode
		if native.Op == "VIEW_POLL" {
			node = r.native.Endpoint.RecallNode
		}
		for {
			wire, err = r.mappingTransport.Read(r.t.Context(), r.native, node, position, 32768)
			if errors.Is(err, fsapi.WouldBlock) && time.Now().Before(deadline) {
				runtime.Gosched()
				continue
			}
			break
		}
		if err != nil || len(wire) < guestbridge.HeaderBytes {
			return nil, nil, nil, nil, fmt.Errorf("mapped response unavailable: %w", err)
		}
		total = int(binary.LittleEndian.Uint32(wire[16:]))
		size := guestbridge.HeaderBytes + min(guestbridge.ChunkBytes, total-chunk*guestbridge.ChunkBytes)
		for len(wire) < size {
			more, e := r.mappingTransport.Read(r.t.Context(), r.native, node, position+uint64(len(wire)), uint32(size-len(wire)))
			if e != nil {
				return nil, nil, nil, nil, e
			}
			wire = append(wire, more...)
		}
		frame, e := guestbridge.DecodeFrame(wire)
		if e != nil || frame.Cell != native.Cell || frame.Minor != 4 {
			return nil, nil, nil, nil, fsapi.Stale
		}
		raw = append(raw, frame.Data...)
		if len(raw) == total && sha256.Sum256(raw) != frame.Hash {
			return nil, nil, nil, nil, fsapi.Stale
		}
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, nil, nil, err
	}
	fields["cell"], _ = canonicalJSON(original.Cell)
	fields["op"], _ = canonicalJSON(original.Op)
	translated, err := canonicalJSON(fields)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	var body struct {
		Payload      json.RawMessage
		Verification *guestbridge.VerificationReceiptV2
	}
	if err = json.Unmarshal(translated, &body); err != nil {
		return nil, nil, nil, nil, err
	}
	if body.Verification != nil {
		if _, err = guestbridge.VerifyMappedReceipt(r.mappedBinding(), guestRaw, translated); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	return body.Payload, body.Verification, guestRaw, translated, nil
}
func parseRuntimeUUID(s string) fsapi.UUID { id, _ := fsapi.ParseUUID(s); return id }
func (r *sourceRig) mappedBinding() guestbridge.MappedReceiptBinding {
	return guestbridge.MappedReceiptBinding{PublicKey: r.provider.Enrollment().HostKey, Scope: r.scope, Endpoint: r.native.Endpoint, Controller: r.supervisor.record.Controller.String(), ServingGeneration: r.provider.Enrollment().ServingGeneration.String()}
}
func TestMappedProducerActualTransportTwoScopes(t *testing.T) {
	r := newSourceRigDomains(t, guestbridge.MappedProtocol(), 2)
	for i, peer := range r.peers {
		var result struct {
			ID           string
			Capabilities []guestbridge.Capability
		}
		require(t, json.Unmarshal(peer.call("ROOT", map[string]any{}), &result))
		family := r.facts.enrollment.TicketFamilies[i].Acquisition
		ticket := guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: 1, Credentials: peer.credentials}
		_, receipt, original, response, err := peer.mappedExecute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": result.Capabilities[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
		require(t, err)
		if receipt == nil || receipt.Kind != "SOURCE" || receipt.Cell == receipt.GuestCell || receipt.GuestCommandHash != hashBytes(original) || receipt.PollCount != 1 {
			t.Fatal("missing distinct actual/native original binding")
		}
		if _, err = guestbridge.DecodeVerificationReceipt(response); err == nil {
			t.Fatal("legacy decoder accepted v2 response")
		}
		for name, mutate := range map[string]func(map[string]json.RawMessage){
			"guest-cell": func(f map[string]json.RawMessage) { f["cell"] = json.RawMessage(`999`) },
			"reclaim": func(f map[string]json.RawMessage) {
				f["reclaim"] = json.RawMessage(`{"cell":1,"response_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`)
			},
			"guest-op": func(f map[string]json.RawMessage) { f["op"] = json.RawMessage(`"VIEW_QUERY"`) },
			"payload":  func(f map[string]json.RawMessage) { f["payload"] = json.RawMessage(`{}`) },
		} {
			t.Run(name, func(t *testing.T) {
				var fields map[string]json.RawMessage
				require(t, json.Unmarshal(original, &fields))
				mutate(fields)
				changed, err := canonicalJSON(fields)
				require(t, err)
				if _, err = guestbridge.VerifyMappedReceipt(peer.mappedBinding(), changed, response); err == nil {
					t.Fatal("changed original verified")
				}
			})
		}
		var fields map[string]json.RawMessage
		require(t, json.Unmarshal(response, &fields))
		fields["payload"] = json.RawMessage(`{}`)
		changed, err := canonicalJSON(fields)
		require(t, err)
		if _, err = guestbridge.VerifyMappedReceipt(peer.mappedBinding(), original, changed); err == nil {
			t.Fatal("changed response verified")
		}
		binding := peer.mappedBinding()
		binding.PublicKey = bytes.Repeat([]byte{7}, 32)
		if _, err = guestbridge.VerifyMappedReceipt(binding, original, response); err == nil {
			t.Fatal("wrong enrolled key verified")
		}
		for _, mutate := range []func(*guestbridge.MappedReceiptBinding){func(b *guestbridge.MappedReceiptBinding) { b.Scope.GuestMount = testUUID(245).String() }, func(b *guestbridge.MappedReceiptBinding) { b.Endpoint.CommandNode = 99 }, func(b *guestbridge.MappedReceiptBinding) { b.ServingGeneration = testUUID(246).String() }} {
			wrong := peer.mappedBinding()
			mutate(&wrong)
			if _, err = guestbridge.VerifyMappedReceipt(wrong, original, response); err == nil {
				t.Fatal("changed full enrolled binding verified")
			}
		}
		binding = peer.mappedBinding()
		binding.Controller = testUUID(242).String()
		if _, err = guestbridge.VerifyMappedReceipt(binding, original, response); err == nil {
			t.Fatal("wrong controller verified")
		}
		// Mutating native bindings or the host chain while preserving guest bytes
		// must fail the pinned signature.
		for name, mutate := range map[string]func(*guestbridge.VerificationReceiptV2){
			"native-cell": func(r *guestbridge.VerificationReceiptV2) { r.Cell++ },
			"native-command": func(r *guestbridge.VerificationReceiptV2) {
				r.CommandHash = hashBytes([]byte("changed native command"))
			},
			"native-response": func(r *guestbridge.VerificationReceiptV2) {
				r.ResponseHash = hashBytes([]byte("changed native response"))
			},
			"chain": func(r *guestbridge.VerificationReceiptV2) { r.MappingHash = hashBytes([]byte("changed chain")) },
		} {
			t.Run(name, func(t *testing.T) {
				altered := *receipt
				mutate(&altered)
				var fields map[string]json.RawMessage
				require(t, json.Unmarshal(response, &fields))
				fields["verification"] = mustRuntimeJSON(t, altered)
				changed := mustRuntimeJSON(t, fields)
				if _, err = guestbridge.VerifyMappedReceipt(peer.mappedBinding(), original, changed); err == nil {
					t.Fatal("changed native binding/chain verified")
				}
			})
		}
		var commandFields map[string]json.RawMessage
		require(t, json.Unmarshal(original, &commandFields))
		var payloadFields, requestFields, credentialFields map[string]json.RawMessage
		require(t, json.Unmarshal(commandFields["payload"], &payloadFields))
		require(t, json.Unmarshal(payloadFields["request"], &requestFields))
		require(t, json.Unmarshal(requestFields["credentials"], &credentialFields))
		credentialFields["groups"] = json.RawMessage(`[200,201]`)
		requestFields["credentials"] = mustRuntimeJSON(t, credentialFields)
		payloadFields["request"] = mustRuntimeJSON(t, requestFields)
		commandFields["payload"] = mustRuntimeJSON(t, payloadFields)
		changedOriginal := mustRuntimeJSON(t, commandFields)
		if _, err = guestbridge.VerifyMappedReceipt(peer.mappedBinding(), changedOriginal, response); err == nil {
			t.Fatal("changed full credential groups verified")
		}
		// Even cryptographically valid host attestations obey kind/op and poll policy.
		for _, policy := range []string{"event-for-source", "multi-poll-for-acquire"} {
			altered := *receipt
			if policy == "event-for-source" {
				altered.Kind = "EVENT"
				altered.Evidence = mustRuntimeJSON(t, map[string]any{"domain_token": result.Capabilities[1].Token, "sequence": 1, "recall": testUUID(243).String(), "phase": "RECALL", "manifest_hash": nil, "target": nil, "retained_owner": testUUID(244).String()})
			} else {
				altered.PollCount = 2
			}
			raw := mustRuntimeJSON(t, altered)
			var fields map[string]json.RawMessage
			require(t, json.Unmarshal(raw, &fields))
			delete(fields, "signature")
			unsigned := mustRuntimeJSON(t, fields)
			altered.Signature = hex.EncodeToString(ed25519.Sign(peer.key, unsigned))
			signature, err := hex.DecodeString(altered.Signature)
			require(t, err)
			if !ed25519.Verify(peer.key.Public().(ed25519.PublicKey), unsigned, signature) {
				t.Fatal("policy fixture signature invalid")
			}
			var body map[string]json.RawMessage
			require(t, json.Unmarshal(response, &body))
			body["verification"] = mustRuntimeJSON(t, altered)
			if _, err = guestbridge.VerifyMappedReceipt(peer.mappedBinding(), original, mustRuntimeJSON(t, body)); err == nil {
				t.Fatal("valid signature bypassed kind/poll policy", policy)
			}
		}
		retirement := map[string]any{"realm": testUUID(100).String(), "subject": "616263", "anchor": testUUID(101).String(), "nonce": testUUID(103).String(), "through": 1, "result_hashes": []string{result.ID}, "credentials": peer.credentials}
		_, floor, _, _, err := peer.mappedExecute("RETIRE", retirement)
		require(t, err)
		if floor == nil || floor.Kind != "FLOOR" {
			t.Fatal("actual covered floor missing mapped receipt2")
		}
		// JSON and even an exposed Native cannot create the private accepted job.
		var delegated guestbridge.Command
		require(t, json.Unmarshal(original, &delegated))
		delegated.Delegation = &guestbridge.Delegation{Version: guestbridge.DelegationVersion, GuestCell: delegated.Cell, GuestOp: delegated.Op}
		if _, err = peer.bridge.Execute(t.Context(), peer.native, delegated); !errors.Is(err, fsapi.Permission) {
			t.Fatal("direct delegated Execute admitted", err)
		}
	}
}

func TestMappedProducerInheritsActualParentFundedCleanup(t *testing.T) {
	r := newSourceRigProtocol(t, guestbridge.MappedProtocol())
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(r.call("ROOT", map[string]any{}), &root))
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	r.call("CREATE", map[string]any{"parent": root.Capabilities[0], "name": "636c65616e7570", "mode": 0660, "umask": 0, "flags": flags})
	// A nonempty actual page owns its PATH-token closure and immutable parent.
	// It grants no new source/floor receipt.
	var page struct {
		ID, Code string
		Entries  []struct{ Capability guestbridge.Capability }
	}
	require(t, json.Unmarshal(r.call("READDIR", map[string]any{"object": root.Capabilities[0], "after": "", "revision": 0, "limit": 256}), &page))
	if page.Code != "OK" || len(page.Entries) != 1 {
		t.Fatal("nonempty READDIR failed", page.Code, len(page.Entries))
	}
	intent := guestbridge.Intent{Opcode: "READDIR", Args: mustRuntimeJSON(t, map[string]any{"object": root.Capabilities[0], "after": "", "revision": 0, "limit": 256})}
	original := mustRuntimeJSON(t, intent)
	request := guestbridge.ReaddirCleanup{Claim: guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: r.claims, OriginalHash: hashBytes(original)}, Credentials: r.credentials, ResultID: page.ID, Capabilities: []guestbridge.Capability{}}
	for _, entry := range page.Entries {
		request.Capabilities = append(request.Capabilities, entry.Capability)
	}
	var before struct{ Code string }
	require(t, json.Unmarshal(r.call("GETATTR", map[string]any{"object": page.Entries[0].Capability}), &before))
	if before.Code != "OK" {
		t.Fatal("page token was not live before cleanup")
	}
	snapshot, err := r.provider.Restore(t.Context(), r.scope)
	require(t, err)
	first, receipt, _, _, err := r.mappedExecute("READDIR_CLEANUP", request)
	require(t, err)
	if receipt != nil {
		t.Fatal("cleanup minted new authority")
	}
	second, _, _, _, err := r.mappedExecute("READDIR_CLEANUP", request)
	require(t, err)
	if !bytes.Equal(first, second) {
		t.Fatal("cleanup retry changed receipt")
	}
	var after struct{ Code string }
	require(t, json.Unmarshal(r.call("GETATTR", map[string]any{"object": page.Entries[0].Capability}), &after))
	if after.Code == "OK" {
		t.Fatal("cleanup left transient PATH token usable")
	}
	// The selected1.4 predictor must reject an oversized canonical result before
	// any plan could apply; it cannot use the older tuple's smaller envelope.
	enlarged := map[string]any{"code": "OK", "id": page.ID, "entries": []map[string]any{{"capability": page.Entries[0].Capability}}, "padding": strings.Repeat("a", guestbridge.BodyBytes)}
	binding := guestbridge.Binding{Context: r.scope, Call: guestbridge.Call{Claim: request.Claim, Credentials: r.credentials, Intent: intent}}
	if _, err = guestbridge.PrepareReaddirClosure(binding, mustRuntimeJSON(t, enlarged), snapshot, guestbridge.MappedProtocol()); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("oversized1.4closure envelope accepted", err)
	}
	request.Credentials.Revision++
	// Direct delegated execution still refuses this changed request because it
	// lacks a private transport candidate. This does not test issuer validation.
	r.cells++
	*r.mappingNativeCell++
	p := guestbridge.MappedProtocol()
	command := guestbridge.Command{Cell: *r.mappingNativeCell, Context: r.scope, Protocol: &p, Op: "READDIR_CLEANUP", Payload: mustRuntimeJSON(t, request), Delegation: &guestbridge.Delegation{Version: guestbridge.DelegationVersion, GuestCell: r.cells, GuestOp: "READDIR_CLEANUP"}}
	if _, err = r.bridge.Execute(t.Context(), r.native, command); !errors.Is(err, fsapi.Permission) {
		t.Fatal("unobserved cleanup bypassed mapping", err)
	}
}
func mustRuntimeJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := canonicalJSON(value)
	require(t, err)
	return raw
}

func TestMappedProducerActualWaitEventAndEvidencePolicy(t *testing.T) {
	r := newSourceRigProtocol(t, guestbridge.MappedProtocol())
	var result struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(r.call("ROOT", map[string]any{}), &result))
	domain := result.Capabilities[1]
	ticket := guestbridge.Ticket{Anchor: testUUID(110).String(), Nonce: testUUID(111).String(), Number: 1, Credentials: r.credentials}
	payload, _, _, _, err := r.mappedExecute("VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": domain, "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
	require(t, err)
	var acquisition struct{ Lease guestbridge.ViewLease }
	require(t, json.Unmarshal(payload, &acquisition))
	control := guestbridge.Ticket{Anchor: ticket.Anchor, Nonce: testUUID(112).String(), Number: 1, Credentials: r.credentials}
	_, _, _, _, err = r.mappedExecute("VIEW_RELEASE", map[string]any{"request": control, "lease": acquisition.Lease})
	require(t, err)
	_, receipt, original, response, err := r.mappedExecute("VIEW_WAIT", map[string]any{"domain": domain, "after_sequence": 0})
	require(t, err)
	if receipt == nil || receipt.Kind != "EVENT" || receipt.PollCount != 1 {
		t.Fatal("actual terminal WAIT event missing receipt2")
	}
	altered := *receipt
	var evidence map[string]json.RawMessage
	require(t, json.Unmarshal(receipt.Evidence, &evidence))
	evidence["sequence"] = json.RawMessage(`999`)
	altered.Evidence = mustRuntimeJSON(t, evidence)
	var fields map[string]json.RawMessage
	require(t, json.Unmarshal(mustRuntimeJSON(t, altered), &fields))
	delete(fields, "signature")
	altered.Signature = hex.EncodeToString(ed25519.Sign(r.key, mustRuntimeJSON(t, fields)))
	var body map[string]json.RawMessage
	require(t, json.Unmarshal(response, &body))
	body["verification"] = mustRuntimeJSON(t, altered)
	if _, err = guestbridge.VerifyMappedReceipt(r.mappedBinding(), original, mustRuntimeJSON(t, body)); err == nil {
		t.Fatal("valid signature hid wrong actual event evidence")
	}
}
