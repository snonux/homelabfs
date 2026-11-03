package guestbridge

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func validateTicket(v any) error {
	o, e := object(v, "anchor", "nonce", "number", "credentials")
	if e != nil || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !uintRange(o["number"], 1, ^uint64(0)) || validateCred(o["credentials"]) != nil {
		return fsapi.Invalid
	}
	return nil
}
func validateClaimNumber(v any) error {
	o, e := object(v, "realm", "subject", "anchor", "nonce", "number")
	if e != nil || !uuid(o["realm"]) || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !bytesField(o["subject"], 1024, true) || !uintRange(o["number"], 1, ^uint64(0)) {
		return fsapi.Invalid
	}
	return nil
}
func equalWire(a, b any) bool {
	x, e := marshal(a)
	if e != nil {
		return false
	}
	y, e := marshal(b)
	return e == nil && bytes.Equal(x, y)
}
func wireHash(v any) string {
	b, e := marshal(v)
	if e != nil {
		return ""
	}
	return digest(b)
}
func number(v any) uint64     { n, _ := strconv.ParseUint(textNumber(v), 10, 64); return n }
func textNumber(v any) string { n, _ := v.(json.Number); return string(n) }
func pageRange(offset, length any) bool {
	return uintRange(offset, 0, (1<<63-1)-4096) && number(offset)%4096 == 0 && uintRange(length, 4096, 4096)
}
func validateViewTarget(c Context, v any) error {
	o, ok := v.(map[string]any)
	if !ok {
		return fsapi.Invalid
	}
	switch text(o["kind"]) {
	case "NORMAL_PUBLIC":
		o, e := object(v, "kind", "publication")
		if e != nil || validatePublication(o["publication"]) != nil {
			return fsapi.Invalid
		}
		p := o["publication"].(map[string]any)
		if p["authority_hash"] != c.AuthorityHash {
			return fsapi.Stale
		}
		if p["result_ref"] != nil {
			r := p["result_ref"].(map[string]any)
			if r["cluster"] != c.Cluster || r["volume"] != c.Volume || r["generation"] != c.Generation || r["configuration"] != c.Configuration {
				return fsapi.Stale
			}
		}
	case "CONTROLLED_LOCAL":
		o, e := object(v, "kind", "cut")
		if e != nil {
			return e
		}
		cut, e := object(o["cut"], "enrollment_hash", "profile_hash", "context_hash", "journal_incarnation", "journal_epoch", "journal_position", "journal_hash", "state_hash", "visibility_generation")
		if e != nil || !uuid(cut["journal_incarnation"]) || !uuid(cut["visibility_generation"]) || !uintRange(cut["journal_epoch"], 1, ^uint64(0)) || !uintRange(cut["journal_position"], 0, ^uint64(0)) {
			return fsapi.Invalid
		}
		for _, k := range []string{"enrollment_hash", "profile_hash", "context_hash", "journal_hash", "state_hash"} {
			if !hash(cut[k]) {
				return fsapi.Invalid
			}
		}
		if cut["context_hash"] != wireHash(c) || cut["journal_incarnation"] != c.OriginJournal {
			return fsapi.Stale
		}
	default:
		return fsapi.Unsupported
	}
	return nil
}
func validateAccess(c Context, v any, mode string) error {
	o, ok := v.(map[string]any)
	if !ok {
		return fsapi.Invalid
	}
	switch text(o["kind"]) {
	case "DOMAIN":
		if _, e := object(v, "kind"); e != nil || mode != "READ" {
			return fsapi.Invalid
		}
	case "OBJECT":
		o, e := object(v, "kind", "object")
		if e != nil || mode != "READ" || validateScopedCap(c, o["object"], "ROOT", "PATH") != nil {
			return fsapi.Invalid
		}
	case "PAGE":
		o, e := object(v, "kind", "description", "offset", "length")
		if e != nil || !pageRange(o["offset"], o["length"]) || validateScopedCap(c, o["description"], "DESCRIPTION") != nil {
			return fsapi.Invalid
		}
		rights := uint64(1)
		if mode == "EXCLUSIVE_DIRTY" {
			rights = 2
		}
		cap := o["description"].(map[string]any)
		if number(cap["rights"])&rights != rights {
			return fsapi.Permission
		}
	default:
		return fsapi.Unsupported
	}
	return nil
}
func validateScopedCap(c Context, v any, kinds ...string) error {
	if validateCap(v) != nil {
		return fsapi.Invalid
	}
	o := v.(map[string]any)
	if !oneOf(text(o["kind"]), kinds...) || !equalWire(o["scope"], c) {
		return fsapi.Permission
	}
	return nil
}
func validateHashedObject(o map[string]any) error {
	id := o["id"]
	if !hash(id) {
		return fsapi.Invalid
	}
	copy := map[string]any{}
	for k, v := range o {
		if k != "id" {
			copy[k] = v
		}
	}
	if wireHash(copy) != id {
		return fsapi.Invalid
	}
	return nil
}
func validateLease(c Context, v any) error {
	o, e := object(v, "id", "scope", "domain", "access", "credential_hash", "mode", "target", "fence", "revision")
	if e != nil || !equalWire(o["scope"], c) || validateScopedCap(c, o["domain"], "DOMAIN") != nil || !hash(o["credential_hash"]) || !oneOf(text(o["mode"]), "READ", "EXCLUSIVE_DIRTY") || !uintRange(o["fence"], 1, ^uint64(0)) || !uintRange(o["revision"], 1, ^uint64(0)) {
		return fsapi.Invalid
	}
	if e = validateAccess(c, o["access"], text(o["mode"])); e != nil {
		return e
	}
	if e = validateViewTarget(c, o["target"]); e != nil {
		return e
	}
	return validateHashedObject(o)
}
func validateReservation(c Context, v any) error {
	o, e := object(v, "id", "scope", "lease_id", "description", "credentials", "offset", "length", "claim_number", "provenance", "drain_credit")
	if e != nil || !equalWire(o["scope"], c) || !hash(o["lease_id"]) || validateScopedCap(c, o["description"], "DESCRIPTION") != nil || validateCred(o["credentials"]) != nil || !pageRange(o["offset"], o["length"]) || validateClaimNumber(o["claim_number"]) != nil || !uuid(o["provenance"]) || !hash(o["drain_credit"]) {
		return fsapi.Invalid
	}
	if number(o["description"].(map[string]any)["rights"])&2 != 2 {
		return fsapi.Permission
	}
	return validateHashedObject(o)
}
func validateViewPayload(c Context, op string, v any) error {
	if op == "VIEW_RETIRE" {
		o, e := object(v, "anchor", "nonce", "credentials", "through", "result_hashes")
		if e != nil || !uuid(o["anchor"]) || !uuid(o["nonce"]) || validateCred(o["credentials"]) != nil || !uintRange(o["through"], 1, ^uint64(0)) {
			return fsapi.Invalid
		}
		hashes, ok := o["result_hashes"].([]any)
		if !ok || len(hashes) < 1 || len(hashes) > 1024 {
			return fsapi.Invalid
		}
		for _, h := range hashes {
			if !hash(h) {
				return fsapi.Invalid
			}
		}
		return nil
	}
	fields := map[string][]string{
		"VIEW_ACQUIRE": {"request", "domain", "access", "mode", "target"},
		"VIEW_RELEASE": {"request", "lease"}, "VIEW_RENEW": {"request", "lease"},
		"VIEW_CANCEL": {"request"}, "VIEW_QUERY": {"request"},
		"DIRTY_RESERVE": {"request", "lease", "description", "offset", "length", "claim_number"},
		"DIRTY_FREEZE":  {"request", "reservation", "recall", "data", "claim"},
	}
	keys, ok := fields[op]
	if !ok {
		return fsapi.Unsupported
	}
	o, e := object(v, keys...)
	if e != nil || validateTicket(o["request"]) != nil {
		return fsapi.Invalid
	}
	cred := o["request"].(map[string]any)["credentials"]
	switch op {
	case "VIEW_ACQUIRE":
		if validateScopedCap(c, o["domain"], "DOMAIN") != nil || !oneOf(text(o["mode"]), "READ", "EXCLUSIVE_DIRTY") {
			return fsapi.Invalid
		}
		if e = validateAccess(c, o["access"], text(o["mode"])); e != nil {
			return e
		}
		if o["target"] == nil {
			return nil
		}
		return validateViewTarget(c, o["target"])
	case "VIEW_RELEASE", "VIEW_RENEW", "DIRTY_RESERVE":
		if e = validateLease(c, o["lease"]); e != nil {
			return e
		}
		lease := o["lease"].(map[string]any)
		if lease["credential_hash"] != wireHash(cred) {
			return fsapi.Permission
		}
		if op == "DIRTY_RESERVE" {
			access := lease["access"].(map[string]any)
			if lease["mode"] != "EXCLUSIVE_DIRTY" || access["kind"] != "PAGE" || validateScopedCap(c, o["description"], "DESCRIPTION") != nil || !equalWire(o["description"], access["description"]) || !pageRange(o["offset"], o["length"]) || !equalWire(o["offset"], access["offset"]) || !equalWire(o["length"], access["length"]) || validateClaimNumber(o["claim_number"]) != nil {
				return fsapi.Invalid
			}
		}
	case "DIRTY_FREEZE":
		if e = validateReservation(c, o["reservation"]); e != nil {
			return e
		}
		r := o["reservation"].(map[string]any)
		if !equalWire(cred, r["credentials"]) || !uuid(o["recall"]) || !bytesField(o["data"], 4096, true) || validateClaim(o["claim"]) != nil {
			return fsapi.Invalid
		}
		claim := o["claim"].(map[string]any)
		n := map[string]any{}
		for k, v := range claim {
			if k != "original_hash" {
				n[k] = v
			}
		}
		if !equalWire(n, r["claim_number"]) {
			return fsapi.Stale
		}
		entry, intent := frozenEntry(r, o["recall"], o["data"], claim)
		if claim["original_hash"] != wireHash(intent) || validateManifest(map[string]any{"hash": wireHash([]any{entry}), "entries": []any{entry}}) != nil {
			return fsapi.Invalid
		}
	}
	return nil
}
func frozenEntry(r map[string]any, recall, data any, claim map[string]any) (map[string]any, map[string]any) {
	payload, _ := hexBytes(data)
	entry := map[string]any{"description": r["description"], "offset": r["offset"], "length": json.Number(strconv.Itoa(len(payload))), "payload_hash": digest(payload), "reservation": r["id"]}
	entry["id"] = wireHash(entry)
	entry["claim"] = claim
	intent := map[string]any{"opcode": "DRAIN_WRITE", "args": map[string]any{"description": r["description"], "offset": r["offset"], "data": data, "recall": recall, "dirty_entry": entry["id"], "reservation": r["id"]}}
	return entry, intent
}
func hexBytes(v any) ([]byte, error) { return hex.DecodeString(text(v)) }
