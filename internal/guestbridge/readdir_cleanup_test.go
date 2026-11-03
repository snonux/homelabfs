package guestbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestReaddirCleanupClosedSuccessor(t *testing.T) {
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/cleanup.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus viewCorpus
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Protocol != CleanupProtocol() {
		t.Fatal("cleanup tuple changed")
	}
	for _, vector := range corpus.Commands {
		raw := mustCanonical(t, vector.Body)
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != vector.Hash {
			t.Fatal("literal hash changed")
		}
		command, err := DecodeCommandVersion(raw, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, minor := range []uint16{0, 1, 2} {
			if _, err = DecodeCommandVersion(raw, minor); err == nil {
				t.Fatal("old closed tuple accepted successor")
			}
		}
		if command.Op == "READDIR_CLEANUP" && !controlCommand(command) {
			t.Fatal("cleanup did not use funded control lane")
		}
		value, err := Canonical(raw)
		if err != nil {
			t.Fatal(err)
		}
		object := value.(map[string]any)
		object["protocol"].(map[string]any)["features"] = json.Number("4095")
		changed, err := marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeCommandVersion(changed, 3); err == nil {
			t.Fatal("partial tuple accepted")
		}
	}
}
