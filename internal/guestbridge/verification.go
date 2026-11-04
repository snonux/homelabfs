package guestbridge

import (
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// VerificationReceipt is a structurally decoded controlled-v1 receipt. Neither
// this value nor a valid signature proves source bytes or physical facts. Only
// the later enrolled runtime verifier may produce/consume authority from it.
// The optional explicitly bound runtime producer supplies actual evidence; the
// codec and this exported value cannot create it.
type VerificationReceipt struct {
	Version           string          `json:"version"`
	Kind              string          `json:"kind"`
	Scope             Context         `json:"scope"`
	Endpoint          Endpoint        `json:"endpoint"`
	Controller        string          `json:"controller"`
	ServingGeneration string          `json:"serving_generation"`
	Cell              uint64          `json:"cell"`
	CommandHash       string          `json:"command_hash"`
	ResponseHash      string          `json:"response_hash"`
	Evidence          json.RawMessage `json:"evidence"`
	Signature         string          `json:"signature"`
}

// DecodeVerificationReceipt checks the closed wire shape only. It does not
// authenticate a signer, observe transport receipt or verify physical/source
// continuity. Runtime consumers must do all three independently.
func DecodeVerificationReceipt(raw []byte) (VerificationReceipt, error) {
	return decodeVerificationReceipt(raw, false)
}

func decodeVerificationReceipt(raw []byte, mapped bool) (VerificationReceipt, error) {
	v, err := Canonical(raw)
	if err != nil {
		return VerificationReceipt{}, err
	}
	o, err := object(v, "version", "kind", "scope", "endpoint", "controller", "serving_generation", "cell", "command_hash", "response_hash", "evidence", "signature")
	if err != nil || text(o["version"]) != "homelabfs-controlled-verification/1" || validateContext(o["scope"]) != nil || !uuid(o["controller"]) || !uuid(o["serving_generation"]) || !uintRange(o["cell"], 1, MaxCell) || !hash(o["command_hash"]) || !hash(o["response_hash"]) || !bytesField(o["signature"], 64, true) || len(text(o["signature"])) != 128 {
		return VerificationReceipt{}, fsapi.Invalid
	}
	endpoint, err := object(o["endpoint"], "fuse_session", "command_node", "result_node", "recall_node")
	if err != nil || !uuid(endpoint["fuse_session"]) {
		return VerificationReceipt{}, fsapi.Invalid
	}
	for _, key := range []string{"command_node", "result_node", "recall_node"} {
		if !uintRange(endpoint[key], 2, ^uint64(0)) {
			return VerificationReceipt{}, fsapi.Invalid
		}
	}
	var receipt VerificationReceipt
	if json.Unmarshal(raw, &receipt) != nil || !validEndpoint(receipt.Endpoint) || receipt.Scope.FuseSession != receipt.Endpoint.FuseSession {
		return VerificationReceipt{}, fsapi.Invalid
	}
	if err = validateVerificationEvidence(receipt.Scope, receipt.Kind, o["evidence"], mapped); err != nil {
		return VerificationReceipt{}, err
	}
	return receipt, nil
}
func validateVerificationEvidence(scope Context, kind string, v any, mapped bool) error {
	switch kind {
	case "SESSION_RETIRED":
		return validateTerminalEvidence(v, mapped)
	case "SOURCE":
		o, err := object(v, "target", "retained_owner")
		if err != nil || !uuid(o["retained_owner"]) || validateControlledReceiptTarget(scope, o["target"]) != nil {
			return fsapi.Invalid
		}
	case "EVENT":
		o, err := object(v, "domain_token", "sequence", "recall", "phase", "manifest_hash", "target", "retained_owner")
		if err != nil || !hash(o["domain_token"]) || !uintRange(o["sequence"], 1, ^uint64(0)) || !uuid(o["recall"]) || !uuid(o["retained_owner"]) || !oneOf(text(o["phase"]), "RECALL", "INSTALL_DRAIN", "INSTALL_FINAL", "REOPEN") {
			return fsapi.Invalid
		}
		if text(o["phase"]) == "RECALL" {
			if o["target"] != nil || o["manifest_hash"] != nil {
				return fsapi.Invalid
			}
		} else if !hash(o["manifest_hash"]) || validateControlledReceiptTarget(scope, o["target"]) != nil {
			return fsapi.Invalid
		}
	case "VIEW_FLOOR":
		o, err := object(v, "anchor", "nonce", "through", "proof_hash", "credentials")
		if !mapped || err != nil || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !uintRange(o["through"], 1, ^uint64(0)) || !hash(o["proof_hash"]) || validateCred(o["credentials"]) != nil {
			return fsapi.Invalid
		}
	case "FLOOR":
		o, err := object(v, "realm", "subject", "anchor", "nonce", "through", "proof_hash", "credentials")
		if err != nil || !uuid(o["realm"]) || !bytesField(o["subject"], 1024, true) || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !uintRange(o["through"], 1, ^uint64(0)) || !hash(o["proof_hash"]) || validateCred(o["credentials"]) != nil {
			return fsapi.Invalid
		}
	default:
		return fsapi.Unsupported
	}
	return nil
}

func validateControlledReceiptTarget(scope Context, v any) error {
	o, ok := v.(map[string]any)
	if !ok || text(o["kind"]) != "CONTROLLED_LOCAL" {
		return fsapi.Invalid
	}
	return validateViewTarget(scope, v)
}
