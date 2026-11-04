package guestbridge

import (
	"encoding/json"
	"testing"
)

// These are inert receipt-policy checks. Actual protected minting and private
// guest consumption are exercised by the controlled_received component driver.
func TestMappedViewFloorBinding(t *testing.T) {
	f := newBridge(t)
	request := map[string]any{"anchor": uid(110).String(), "nonce": uid(111).String(), "credentials": f.cred(), "through": uint64(1), "result_hashes": []string{digest([]byte("result"))}}
	evidence := map[string]any{"anchor": request["anchor"], "nonce": request["nonce"], "credentials": request["credentials"], "through": request["through"], "proof_hash": digest([]byte("protected"))}
	protocol := MappedProtocol()
	command := Command{Cell: 1, Context: f.c, Protocol: &protocol, Op: "VIEW_RETIRE", Payload: mustJSON(t, request)}
	receipt := VerificationReceiptV2{VerificationReceipt: VerificationReceipt{Kind: "VIEW_FLOOR", Evidence: mustJSON(t, evidence)}}
	response := mustJSON(t, map[string]any{"status": "RETIRED", "floor": 1, "proof_hash": evidence["proof_hash"]})
	if err := validateMappedKind(command, receipt, response); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"op", "kind", "credentials", "through", "proof", "extra"} {
		t.Run(name, func(t *testing.T) {
			c, r := command, receipt
			var e map[string]json.RawMessage
			if err := json.Unmarshal(r.Evidence, &e); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "op":
				c.Op = "RETIRE"
			case "kind":
				r.Kind = "FLOOR"
			case "credentials":
				changed := f.cred()
				changed.Groups = []uint32{999}
				e["credentials"] = mustJSON(t, changed)
			case "through":
				e["through"] = json.RawMessage(`2`)
			case "proof":
				e["proof_hash"] = mustJSON(t, digest([]byte("wrong")))
			case "extra":
				e["extra"] = json.RawMessage(`null`)
			}
			r.Evidence = mustJSON(t, e)
			shape, err := Canonical(r.Evidence)
			if err != nil {
				t.Fatal(err)
			}
			if validateVerificationEvidence(f.c, r.Kind, shape, true) == nil && validateMappedKind(c, r, response) == nil {
				t.Fatal("mismatched view floor accepted")
			}
		})
	}
	shape, err := Canonical(receipt.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if validateVerificationEvidence(f.c, "VIEW_FLOOR", shape, false) == nil {
		t.Fatal("old receipt accepted VIEW_FLOOR")
	}
}
