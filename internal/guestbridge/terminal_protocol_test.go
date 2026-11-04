package guestbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

// These exercise wire validation only. Valid decoded objects and receipt
// fields do not supply worker, observer, native or physical provenance.
func terminalWireFixture(t *testing.T) (Command, TerminalSnapshot) {
	t.Helper()
	scope := Context{
		AuthorityHash: digest([]byte("authority")), Cluster: uid(1).String(),
		Configuration: uid(2).String(), Export: uid(3).String(),
		ExporterIncarnation: uid(4).String(), FuseSession: uid(5).String(),
		Generation: uid(6).String(), GuestBoot: uid(7).String(),
		GuestMount: uid(8).String(), OriginIncarnation: uid(9).String(),
		OriginJournal: uid(10).String(), OriginNode: uid(11).String(),
		PolicyHash: digest([]byte("policy")), RegistryHash: digest([]byte("registry")),
		Session: uid(12).String(), VM: uid(13).String(), Volume: uid(14).String(),
	}
	credentials := Credentials{FSUID: 1000, FSGID: 1001, Groups: []uint32{4, 17, 1000},
		Process: uid(15).String(), Revision: 3, IDMapHash: digest([]byte("idmap")), UserNamespace: uid(16).String()}
	snapshot := TerminalSnapshot{Version: "homelabfs-guest-terminal/1", Scope: scope,
		Controller: uid(17).String(), RetirementID: uid(18).String(), Revision: 7,
		GuestThrough: 9, Donor: &Ack{Cell: 9, ResponseHash: digest([]byte("sole donor"))},
		Credentials: credentials, SettledCells: []Ack{}, Phase: "TERMINAL_PENDING"}
	request := TerminalRequest{RetirementID: snapshot.RetirementID, Credentials: credentials,
		GuestThrough: snapshot.GuestThrough, ObserverRevision: snapshot.Revision, ClosureHash: digest(mustJSON(t, snapshot)), SettledCells: []Ack{}}
	protocol := TerminalProtocol()
	return Command{Cell: 10, Context: scope, Protocol: &protocol, Op: "SESSION_RETIRE",
		Payload: mustJSON(t, request), Reclaim: snapshot.Donor}, snapshot
}

func terminalWireFields(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func TestTerminalObserverRequiresCompleteInventory(t *testing.T) {
	_, snapshot := terminalWireFixture(t)
	raw := mustJSON(t, snapshot)
	for _, donor := range []*Ack{snapshot.Donor, nil} {
		snapshot.Donor = donor
		if donor == nil {
			snapshot.GuestThrough = 0
		}
		var decoded TerminalSnapshot
		encoded := mustJSON(t, snapshot)
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("complete observer rejected: %v", err)
		}
		if !bytes.Equal(mustJSON(t, decoded), encoded) {
			t.Fatal("observer round trip changed full credentials or donor")
		}
	}
	for key := range terminalWireFields(t, raw) {
		t.Run("missing/"+key, func(t *testing.T) {
			fields := terminalWireFields(t, raw)
			delete(fields, key)
			var decoded TerminalSnapshot
			if err := json.Unmarshal(mustJSON(t, fields), &decoded); err == nil {
				t.Fatal("incomplete observer accepted")
			}
		})
	}
	for _, key := range []string{"entrants", "polls", "unknown", "claims", "held_results", "descriptions", "leases", "reservations", "dirty_pins", "source_pins", "mapped_pages", "received_external"} {
		for _, invalid := range []string{"null", "1", "-1", "\"0\"", "false", "0.0"} {
			t.Run(key+"/"+invalid, func(t *testing.T) {
				fields := terminalWireFields(t, raw)
				fields[key] = json.RawMessage(invalid)
				var decoded TerminalSnapshot
				if err := json.Unmarshal(mustJSON(t, fields), &decoded); err == nil {
					t.Fatal("nonzero or non-integer inventory accepted")
				}
			})
		}
	}
	for _, outer := range []string{"scope", "credentials", "donor"} {
		original := terminalWireFields(t, raw)
		for key := range terminalWireFields(t, original[outer]) {
			t.Run("missing/"+outer+"/"+key, func(t *testing.T) {
				fields := terminalWireFields(t, raw)
				nested := terminalWireFields(t, fields[outer])
				delete(nested, key)
				fields[outer] = mustJSON(t, nested)
				var decoded TerminalSnapshot
				if err := json.Unmarshal(mustJSON(t, fields), &decoded); err == nil {
					t.Fatal("incomplete nested identity accepted")
				}
			})
		}
	}
	for _, change := range []func(map[string]json.RawMessage){
		func(f map[string]json.RawMessage) { f["guest_through"] = json.RawMessage("null") },
		func(f map[string]json.RawMessage) { f["unknown_field"] = json.RawMessage("0") },
		func(f map[string]json.RawMessage) {
			credentials := terminalWireFields(t, f["credentials"])
			credentials["fsuid"] = json.RawMessage("null")
			f["credentials"] = mustJSON(t, credentials)
		},
	} {
		fields := terminalWireFields(t, raw)
		change(fields)
		var decoded TerminalSnapshot
		if err := json.Unmarshal(mustJSON(t, fields), &decoded); err == nil {
			t.Fatal("invalid observer field accepted")
		}
	}
	duplicate := append([]byte(`{"entrants":0,`), raw[1:]...)
	var decoded TerminalSnapshot
	if err := json.Unmarshal(duplicate, &decoded); err == nil {
		t.Fatal("duplicate observer field accepted")
	}
}

func TestTerminalOpcodeRequiresExactSelectedTuple(t *testing.T) {
	command, _ := terminalWireFixture(t)
	if _, err := DecodeCommandVersion(mustJSON(t, command), 4); err != nil {
		t.Fatalf("terminal command rejected: %v", err)
	}
	for _, protocol := range []Protocol{ViewProtocol(), ControlledProtocol(), CleanupProtocol(), MappedProtocol()} {
		t.Run(fmt.Sprint(protocol.BridgeMinor, "/", protocol.Features), func(t *testing.T) {
			changed := command
			changed.Protocol = &protocol
			if _, err := DecodeCommandVersion(mustJSON(t, changed), uint16(protocol.BridgeMinor)); err == nil {
				t.Fatal("old tuple accepted terminal opcode")
			}
		})
	}
	for _, change := range []func(*Protocol){
		func(p *Protocol) { p.Features = 65535 },
		func(p *Protocol) { p.Features = 32768 },
		func(p *Protocol) { p.BodySchema++ },
		func(p *Protocol) { p.Msize-- },
		func(p *Protocol) { p.NormalMinor++ },
	} {
		protocol := TerminalProtocol()
		change(&protocol)
		changed := command
		changed.Protocol = &protocol
		if _, err := DecodeCommandVersion(mustJSON(t, changed), 4); err == nil {
			t.Fatal("nearby or superset tuple accepted terminal opcode")
		}
	}
	for _, protocol := range []Protocol{MappedProtocol(), TerminalProtocol()} {
		hello := command
		hello.Op, hello.Protocol, hello.Payload, hello.Reclaim = "HELLO", &protocol, mustJSON(t, protocol), nil
		before := mustJSON(t, hello)
		decoded, err := DecodeCommandVersion(before, 4)
		if err != nil || !bytes.Equal(mustJSON(t, decoded), before) {
			t.Fatalf("exact HELLO changed: %v", err)
		}
		other := MappedProtocol()
		if protocol == other {
			other = TerminalProtocol()
		}
		hello.Payload = mustJSON(t, other)
		if _, err := DecodeCommandVersion(mustJSON(t, hello), 4); err == nil {
			t.Fatal("HELLO silently changed selected tuple")
		}
	}
}

func TestTerminalManifestRequiresCompleteSortedAcknowledgements(t *testing.T) {
	command, snapshot := terminalWireFixture(t)
	request, err := DecodeTerminalRequest(command.Payload)
	if err != nil {
		t.Fatal(err)
	}
	h := digest([]byte("observed full reply"))
	for _, settled := range [][]Ack{{}, {{Cell: 1, ResponseHash: h}, {Cell: 8, ResponseHash: h}}} {
		request.SettledCells = settled
		decoded, err := DecodeTerminalRequest(mustJSON(t, request))
		if err != nil || !bytes.Equal(mustJSON(t, settled), mustJSON(t, decoded.SettledCells)) {
			t.Fatalf("valid complete manifest changed: %v", err)
		}
	}
	for name, invalid := range map[string]any{
		"null":              nil,
		"object":            map[string]any{},
		"duplicate":         []Ack{{Cell: 1, ResponseHash: h}, {Cell: 1, ResponseHash: h}},
		"unsorted":          []Ack{{Cell: 2, ResponseHash: h}, {Cell: 1, ResponseHash: h}},
		"future":            []Ack{{Cell: snapshot.GuestThrough + 1, ResponseHash: h}},
		"zero-cell":         []Ack{{Cell: 0, ResponseHash: h}},
		"missing-full-hash": []map[string]any{{"cell": 1}},
		"unknown-ack-field": []map[string]any{{"cell": 1, "response_hash": h, "scope": snapshot.Scope}},
	} {
		t.Run(name, func(t *testing.T) {
			fields := terminalWireFields(t, command.Payload)
			fields["settled_cells"] = mustJSON(t, invalid)
			if _, err := DecodeTerminalRequest(mustJSON(t, fields)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	request.GuestThrough = 100
	request.SettledCells = make([]Ack, 80)
	for i := range request.SettledCells {
		request.SettledCells[i] = Ack{Cell: uint64(i + 1), ResponseHash: h}
	}
	if _, err := DecodeTerminalRequest(mustJSON(t, request)); err == nil {
		t.Fatal("80 additional donors accepted beside the funding donor")
	}
	snapshot.SettledCells = []Ack{*snapshot.Donor}
	var decoded TerminalSnapshot
	if err := json.Unmarshal(mustJSON(t, snapshot), &decoded); err == nil {
		t.Fatal("observer counted funding donor twice")
	}
}

func TestTerminalReceiptKindPreservesWholeOriginalPrincipal(t *testing.T) {
	command, _ := terminalWireFixture(t)
	request, err := DecodeTerminalRequest(command.Payload)
	if err != nil {
		t.Fatal(err)
	}
	response := TerminalResponse{Status: "RETIRED", RetirementID: request.RetirementID,
		GuestThrough: request.GuestThrough, ObserverRevision: request.ObserverRevision,
		ClosureHash: request.ClosureHash, SettledHash: digest([]byte("[]")), OwnershipHash: digest([]byte("inert ownership descriptor"))}
	evidence := TerminalEvidence{RetirementID: request.RetirementID, Credentials: request.Credentials,
		GuestThrough: request.GuestThrough, ObserverRevision: request.ObserverRevision, ClosureHash: request.ClosureHash,
		SettledHash: response.SettledHash, OwnershipHash: response.OwnershipHash, RetainedOwner: "C8_TERMINAL_CELL"}
	receipt := VerificationReceiptV2{VerificationReceipt: VerificationReceipt{
		Kind: "SESSION_RETIRED", Evidence: mustJSON(t, evidence)}, PollCount: 1}
	if err := validateMappedKind(command, receipt, mustJSON(t, response)); err != nil {
		t.Fatalf("exact receipt fields rejected: %v", err)
	}
	// Same UID/GID and a valid group list cannot replace the captured principal.
	evidence.Credentials.Groups = []uint32{4, 18, 1000}
	changed := receipt
	changed.Evidence = mustJSON(t, evidence)
	if err := validateMappedKind(command, changed, mustJSON(t, response)); err == nil {
		t.Fatal("same-UID substituted groups accepted")
	}
	for _, kind := range []string{"SOURCE", "EVENT", "FLOOR", "VIEW_FLOOR"} {
		changed := receipt
		changed.Kind = kind
		if err := validateMappedKind(command, changed, mustJSON(t, response)); err == nil {
			t.Fatalf("%s substituted for terminal receipt", kind)
		}
	}
	for _, count := range []uint64{0, 2} {
		changed := receipt
		changed.PollCount = count
		if err := validateMappedKind(command, changed, mustJSON(t, response)); err == nil {
			t.Fatal("continuation chain accepted as terminal reply")
		}
	}
	for _, op := range []string{"RETIRE", "VIEW_RETIRE", "RELEASE_CELL"} {
		changed := command
		changed.Op = op
		if err := validateMappedKind(changed, receipt, mustJSON(t, response)); err == nil {
			t.Fatalf("terminal receipt authorized %s", op)
		}
	}
}
