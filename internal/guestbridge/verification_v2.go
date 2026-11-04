package guestbridge

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// VerificationReceiptV2 is the closed host-attested original/native binding.
// A signature attests the host's protected leg chain; it is not kernel receipt,
// independent observation of those legs, or independently verified source data.
type VerificationReceiptV2 struct {
	VerificationReceipt
	GuestCell         uint64 `json:"guest_cell"`
	GuestCommandHash  string `json:"guest_command_hash"`
	GuestResponseHash string `json:"guest_response_hash"`
	PollCount         uint64 `json:"poll_count"`
	MappingHash       string `json:"mapping_hash"`
}

// MappedReceiptBinding pins the enrolled signer and complete runtime context.
// Supplying this value does not enroll any key, endpoint, or kernel provider.
type MappedReceiptBinding struct {
	PublicKey                     ed25519.PublicKey
	Scope                         Context
	Endpoint                      Endpoint
	Controller, ServingGeneration string
}

// DecodeVerificationReceiptV2 checks the distinct closed V2 shape only.
func DecodeVerificationReceiptV2(raw []byte) (VerificationReceiptV2, error) {
	v, err := Canonical(raw)
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	o, err := object(v, "version", "kind", "scope", "endpoint", "controller", "serving_generation", "cell", "command_hash", "response_hash", "evidence", "signature", "guest_cell", "guest_command_hash", "guest_response_hash", "poll_count", "mapping_hash")
	if err != nil || text(o["version"]) != "homelabfs-controlled-verification/2" || !uintRange(o["guest_cell"], 1, MaxCell) || !uintRange(o["poll_count"], 1, 16) || !hash(o["guest_command_hash"]) || !hash(o["guest_response_hash"]) || !hash(o["mapping_hash"]) {
		return VerificationReceiptV2{}, fsapi.Invalid
	}
	common := map[string]any{}
	for key, value := range o {
		common[key] = value
	}
	for _, key := range []string{"guest_cell", "guest_command_hash", "guest_response_hash", "poll_count", "mapping_hash"} {
		delete(common, key)
	}
	common["version"] = "homelabfs-controlled-verification/1"
	base, err := marshal(common)
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	if _, err = decodeVerificationReceipt(base, true); err != nil {
		return VerificationReceiptV2{}, err
	}
	var receipt VerificationReceiptV2
	if json.Unmarshal(raw, &receipt) != nil {
		return VerificationReceiptV2{}, fsapi.Invalid
	}
	return receipt, nil
}

// VerifyMappedReceipt verifies a pinned host attestation against exact canonical
// original and translated response bytes. It returns inert data, never a
// KernelReceived, verified source, or permission to install a cache object.
func VerifyMappedReceipt(binding MappedReceiptBinding, original, response []byte) (VerificationReceiptV2, error) {
	command, err := DecodeCommandVersion(original, 4)
	if err != nil || command.Delegation != nil || command.Context != binding.Scope || len(binding.PublicKey) != ed25519.PublicKeySize || !validEndpoint(binding.Endpoint) || !validUUID(binding.Controller) || !validUUID(binding.ServingGeneration) {
		return VerificationReceiptV2{}, fsapi.Permission
	}
	v, err := Canonical(response)
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	o, err := object(v, "cell", "context", "op", "payload", "protocol", "verification")
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	fields, err := envelopeFields(response)
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	var scope Context
	var protocol Protocol
	var cell uint64
	var op string
	if json.Unmarshal(fields["context"], &scope) != nil || json.Unmarshal(fields["protocol"], &protocol) != nil || json.Unmarshal(fields["cell"], &cell) != nil || json.Unmarshal(fields["op"], &op) != nil || scope != command.Context || protocol != MappedProtocol() || cell != command.Cell || op != command.Op || o["verification"] == nil {
		return VerificationReceiptV2{}, fsapi.Stale
	}
	receipt, err := DecodeVerificationReceiptV2(fields["verification"])
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	if receipt.Scope != binding.Scope || receipt.Endpoint != binding.Endpoint || receipt.Controller != binding.Controller || receipt.ServingGeneration != binding.ServingGeneration || receipt.GuestCell != command.Cell || receipt.GuestCommandHash != digest(original) || receipt.PollCount > 1 && command.Op != "VIEW_WAIT" {
		return VerificationReceiptV2{}, fsapi.Stale
	}
	delete(fields, "verification")
	if hashFields(fields) != receipt.GuestResponseHash {
		return VerificationReceiptV2{}, fsapi.Stale
	}
	if err = validateMappedKind(command, receipt, fields["payload"]); err != nil {
		return VerificationReceiptV2{}, err
	}
	unsigned, err := unsignedVerificationV2(receipt)
	if err != nil {
		return VerificationReceiptV2{}, err
	}
	signature, err := hex.DecodeString(receipt.Signature)
	if err != nil || !ed25519.Verify(binding.PublicKey, unsigned, signature) {
		return VerificationReceiptV2{}, fsapi.Permission
	}
	return receipt, nil
}
func unsignedVerificationV2(receipt VerificationReceiptV2) ([]byte, error) {
	raw, err := marshal(receipt)
	if err != nil {
		return nil, err
	}
	fields, err := envelopeFields(raw)
	if err != nil {
		return nil, err
	}
	delete(fields, "signature")
	values := map[string]json.RawMessage{}
	for key, value := range fields {
		values[key] = value
	}
	return marshal(values)
}
func validateMappedKind(c Command, r VerificationReceiptV2, payload []byte) error {
	switch r.Kind {
	case "SOURCE":
		if !isViewExtension(c.Op) {
			return fsapi.Permission
		}
		if err := validateViewResponse(c, payload); err != nil {
			return err
		}
		fields, err := envelopeFields(payload)
		if err != nil {
			return err
		}
		present := false
		for _, key := range []string{"lease", "reservation", "entry"} {
			if value, ok := fields[key]; ok && !bytes.Equal(value, []byte("null")) {
				present = true
			}
		}
		if !present {
			return fsapi.Stale
		}
	case "EVENT":
		if c.Op != "VIEW_WAIT" && c.Op != "VIEW_POLL" {
			return fsapi.Permission
		}
		native := c
		native.Op = "VIEW_POLL"
		if err := validateControlResponse(native, payload); err != nil {
			return err
		}
		fields, err := envelopeFields(payload)
		if err != nil || !bytes.Equal(fields["status"], []byte(`"EVENT"`)) {
			return fsapi.Stale
		}
		event, err := envelopeFields(fields["event"])
		if err != nil {
			return err
		}
		evidence, err := envelopeFields(r.Evidence)
		if err != nil {
			return err
		}
		domain, err := envelopeFields(event["domain"])
		if err != nil {
			return err
		}
		if !bytes.Equal(domain["token"], evidence["domain_token"]) {
			return fsapi.Stale
		}
		for _, key := range []string{"sequence", "recall", "phase", "manifest_hash", "target"} {
			if !bytes.Equal(event[key], evidence[key]) {
				return fsapi.Stale
			}
		}
	case "FLOOR", "VIEW_FLOOR":
		if r.Kind == "FLOOR" && c.Op != "RETIRE" || r.Kind == "VIEW_FLOOR" && c.Op != "VIEW_RETIRE" {
			return fsapi.Permission
		}
		if r.Kind == "VIEW_FLOOR" {
			if err := validateViewResponse(c, payload); err != nil {
				return err
			}
		} else if err := validateControlResponse(c, payload); err != nil {
			return err
		}
		request, err := envelopeFields(c.Payload)
		if err != nil {
			return err
		}
		evidence, err := envelopeFields(r.Evidence)
		if err != nil {
			return err
		}
		reply, err := envelopeFields(payload)
		if err != nil {
			return err
		}
		keys := []string{"anchor", "nonce", "through", "credentials"}
		if r.Kind == "FLOOR" {
			keys = append(keys, "realm", "subject")
		}
		for _, key := range keys {
			if !bytes.Equal(request[key], evidence[key]) {
				return fsapi.Stale
			}
		}
		if !bytes.Equal(reply["status"], []byte(`"RETIRED"`)) || !bytes.Equal(reply["floor"], request["through"]) || !bytes.Equal(reply["proof_hash"], evidence["proof_hash"]) {
			return fsapi.Stale
		}
	default:
		return fsapi.Unsupported
	}
	return nil
}
