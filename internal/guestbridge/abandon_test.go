package guestbridge

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// abandonContext is a schema-valid Context for codec-only tests.
func abandonContext() Context {
	return Context{AuthorityHash: digest([]byte("authority")), Cluster: uid(1).String(), Configuration: uid(2).String(), Export: uid(3).String(), ExporterIncarnation: uid(4).String(), FuseSession: uid(5).String(), Generation: uid(6).String(), GuestBoot: uid(7).String(), GuestMount: uid(8).String(), OriginIncarnation: uid(9).String(), OriginJournal: uid(10).String(), OriginNode: uid(11).String(), PolicyHash: digest([]byte("policy")), RegistryHash: digest([]byte("registry")), Session: uid(12).String(), VM: uid(13).String(), Volume: uid(14).String()}
}

func abandonBody(t *testing.T, protocol Protocol, payload any) []byte {
	t.Helper()
	raw, err := marshal(Command{Protocol: &protocol, Cell: 7, Context: abandonContext(), Op: "ABANDON", Payload: mustJSON(t, payload)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func abandonClaims(numbers ...uint64) map[string]any {
	claims := []map[string]any{}
	for _, n := range numbers {
		claims = append(claims, map[string]any{"number": n, "original_hash": digest([]byte{byte(n)})})
	}
	return map[string]any{"realm": uid(20).String(), "subject": "616263", "anchor": uid(21).String(), "nonce": uid(22).String(), "claims": claims}
}

// TestAbandonCodec: ABANDON decodes only under a controlled tuple (frame
// minor >= 2) with the closed payload, and every malformed list refuses.
func TestAbandonCodec(t *testing.T) {
	for minor, protocol := range map[uint16]Protocol{2: ControlledProtocol(), 3: CleanupProtocol(), 4: MappedProtocol()} {
		c, err := DecodeCommandVersion(abandonBody(t, protocol, abandonClaims(2, 5, 9)), minor)
		if err != nil || c.Op != "ABANDON" {
			t.Fatalf("minor %d: %v", minor, err)
		}
	}
	if _, err := DecodeCommandVersion(abandonBody(t, ViewProtocol(), abandonClaims(2)), 1); err == nil {
		t.Fatal("bridge1.1 decoded ABANDON")
	}
	legacy, _ := marshal(Command{Cell: 7, Context: abandonContext(), Op: "ABANDON", Payload: mustJSON(t, abandonClaims(2))})
	if _, err := DecodeCommand(legacy); err == nil {
		t.Fatal("closed1.0 decoded ABANDON")
	}
	many := make([]uint64, MaxAbandonClaims+1)
	for i := range many {
		many[i] = uint64(i + 1)
	}
	extra := abandonClaims(2)
	extra["credentials"] = nil
	zero := abandonClaims(0)
	badHash := abandonClaims(2)
	badHash["claims"].([]map[string]any)[0]["original_hash"] = "00"
	for name, payload := range map[string]any{"empty": abandonClaims(), "too many": abandonClaims(many...), "descending": abandonClaims(5, 2), "duplicate": abandonClaims(3, 3), "extra field": extra, "number zero": zero, "short hash": badHash} {
		if _, err := DecodeCommandVersion(abandonBody(t, ControlledProtocol(), payload), 2); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	full := abandonClaims(many[:MaxAbandonClaims]...)
	if _, err := DecodeCommandVersion(abandonBody(t, ControlledProtocol(), full), 2); err != nil {
		t.Fatalf("a full list refused: %v", err)
	}
}

// TestAbandonResponseShape: the bridge accepts a provider reply only with
// one outcome per requested number, in order, and a proof exactly on
// ABANDONED.
func TestAbandonResponseShape(t *testing.T) {
	request := mustJSON(t, abandonClaims(2, 3))
	proof := digest([]byte("gap"))
	reply := func(outcomes ...map[string]any) json.RawMessage {
		return mustJSON(t, map[string]any{"claims": outcomes})
	}
	ok := reply(map[string]any{"number": 2, "status": "ABANDONED", "proof_hash": proof}, map[string]any{"number": 3, "status": "KEPT", "proof_hash": nil})
	if err := validateAbandonResponse(request, ok); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string]json.RawMessage{
		"missing outcome": reply(map[string]any{"number": 2, "status": "UNKNOWN", "proof_hash": nil}),
		"reordered":       reply(map[string]any{"number": 3, "status": "KEPT", "proof_hash": nil}, map[string]any{"number": 2, "status": "KEPT", "proof_hash": nil}),
		"proof on KEPT":   reply(map[string]any{"number": 2, "status": "KEPT", "proof_hash": proof}, map[string]any{"number": 3, "status": "KEPT", "proof_hash": nil}),
		"no proof":        reply(map[string]any{"number": 2, "status": "ABANDONED", "proof_hash": nil}, map[string]any{"number": 3, "status": "KEPT", "proof_hash": nil}),
		"unknown status":  reply(map[string]any{"number": 2, "status": "DECLINED", "proof_hash": nil}, map[string]any{"number": 3, "status": "KEPT", "proof_hash": nil}),
	} {
		if err := validateAbandonResponse(request, bad); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
