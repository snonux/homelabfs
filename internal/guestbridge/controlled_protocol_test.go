package guestbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestControlledLiteralCodec(t *testing.T) {
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/controlled.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus viewCorpus
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Protocol != ControlledProtocol() {
		t.Fatal("tuple changed")
	}
	for _, vector := range corpus.Commands {
		t.Run(vector.Name, func(t *testing.T) {
			raw := mustCanonical(t, vector.Body)
			c, err := DecodeCommandVersion(raw, 2)
			if err != nil {
				t.Fatal(err)
			}
			for _, old := range []uint16{0, 1} {
				if _, err = DecodeCommandVersion(raw, old); err == nil {
					t.Fatal("old version accepted successor")
				}
			}
			sha := sha256.Sum256(raw)
			if hex.EncodeToString(sha[:]) != vector.Hash {
				t.Fatal("literal hash changed")
			}
			wire, err := hex.DecodeString(vector.Frame)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := DecodeFrame(wire)
			if err != nil {
				t.Fatal(err)
			}
			if frame.Minor != 2 || frame.Hash != sha || !bytes.Equal(frame.Data, raw) {
				t.Fatal("literal frame differs")
			}
			encoded, err := EncodeFrame(frame)
			if err != nil || !bytes.Equal(encoded, wire) {
				t.Fatalf("frame encode %v", err)
			}
			if controlCommand(c) != (c.Op == "GAP_RESOLVE" || c.Op == "RETIRE") {
				t.Fatal("allocation spent control capacity")
			}
			// Every tuple member is exact, rather than negotiated by intersection.
			value, err := Canonical(raw)
			if err != nil {
				t.Fatal(err)
			}
			o := value.(map[string]any)
			o["protocol"].(map[string]any)["features"] = json.Number("2047")
			changed, err := marshal(o)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = DecodeCommandVersion(changed, 2); err == nil {
				t.Fatal("partial feature selection")
			}
		})
	}
}

func TestControlledOldEnvelopesRejectNewOperations(t *testing.T) {
	commands := viewCommands(t)
	var payload json.RawMessage
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/controlled.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus viewCorpus
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	var original Command
	if err = json.Unmarshal(corpus.Commands[1].Body, &original); err != nil {
		t.Fatal(err)
	}
	payload = original.Payload
	for _, minor := range []uint16{0, 1} {
		c := commands["hello"]
		c.Op = "ALLOCATE"
		c.Payload = payload
		if minor == 0 {
			c.Protocol = nil
		}
		raw, err := marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeCommandVersion(raw, minor); err == nil {
			t.Fatal("closed old envelope accepted ALLOCATE")
		}
	}
}

func TestControlledVerificationClosedLiteralUnions(t *testing.T) {
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/controlled.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct{ Receipts []json.RawMessage }
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Receipts) != 3 {
		t.Fatal("missing closed variants")
	}
	for _, literal := range corpus.Receipts {
		raw := mustCanonical(t, literal)
		decoded, err := DecodeVerificationReceipt(raw)
		if err != nil {
			t.Fatal(err)
		}
		// A zero signature is intentionally a structurally valid codec fixture.
		// This decoder supplies no signing or source/native verification authority.
		t.Run(decoded.Kind, func(t *testing.T) {
			for _, mutation := range []func(map[string]any){
				func(o map[string]any) { o["verified"] = true },
				func(o map[string]any) { o["version"] = "homelabfs-controlled-verification/2" },
				func(o map[string]any) { o["kind"] = "NORMAL_PUBLIC" },
				func(o map[string]any) { o["signature"] = "00" },
				func(o map[string]any) { o["evidence"].(map[string]any)["verified"] = true },
				func(o map[string]any) { o["endpoint"].(map[string]any)["result_node"] = json.Number("2") },
			} {
				value, err := Canonical(raw)
				if err != nil {
					t.Fatal(err)
				}
				o := value.(map[string]any)
				mutation(o)
				changed, err := marshal(o)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = DecodeVerificationReceipt(changed); err == nil {
					t.Fatal("open/invalid verification union accepted")
				}
			}
		})
	}
}

func TestControlledRetirementCredentialsAreVersioned(t *testing.T) {
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/controlled.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus viewCorpus
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, vector := range corpus.Commands {
		if vector.Name == "retire" {
			var command Command
			if err = json.Unmarshal(vector.Body, &command); err != nil {
				t.Fatal(err)
			}
			value, err := Canonical(mustCanonical(t, command.Payload))
			if err != nil {
				t.Fatal(err)
			}
			payload := value.(map[string]any)
			if err = validatePayload("RETIRE", payload, nil); err == nil {
				t.Fatal("old RETIRE accepted new credentials field")
			}
			delete(payload, "credentials")
			if err = validatePayload("RETIRE", payload, nil); err != nil {
				t.Fatalf("old RETIRE changed: %v", err)
			}
			command.Payload, err = marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			raw, err = marshal(command)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = DecodeCommandVersion(raw, 2); err == nil {
				t.Fatal("successor RETIRE omitted credentials")
			}
			return
		}
	}
	t.Fatal("missing retirement literal")
}
