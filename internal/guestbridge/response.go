package guestbridge

import (
	"encoding/json"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func validateControlResponse(c Command, raw json.RawMessage) error {
	if isViewExtension(c.Op) {
		return validateViewResponse(c, raw)
	}
	v, e := Canonical(raw)
	if e != nil {
		return e
	}
	switch c.Op {
	case "RETIRE":
		o, e := object(v, "status", "floor", "proof_hash")
		if e != nil || !oneOf(text(o["status"]), "RETIRED", "BLOCKED") || !uintRange(o["floor"], 0, ^uint64(0)) {
			return fsapi.Invalid
		}
		if o["status"] == "RETIRED" {
			if !hash(o["proof_hash"]) {
				return fsapi.Invalid
			}
		} else if o["proof_hash"] != nil {
			return fsapi.Invalid
		}
	case "RESUME":
		o, e := object(v, "status", "ledger_hash")
		if e != nil || !oneOf(text(o["status"]), "RECONCILED", "BLOCKED") || !hash(o["ledger_hash"]) {
			return fsapi.Invalid
		}
	case "VIEW_ACK":
		o, e := object(v, "status")
		if e != nil || !oneOf(text(o["status"]), "ACCEPTED", "STALE", "BLOCKED") {
			return fsapi.Invalid
		}
	case "VIEW_POLL":
		o, e := object(v, "status", "event")
		if e != nil || !oneOf(text(o["status"]), "EMPTY", "EVENT") {
			return fsapi.Invalid
		}
		if o["status"] == "EMPTY" {
			if o["event"] != nil {
				return fsapi.Invalid
			}
			return nil
		}
		event, e := object(o["event"], "sequence", "recall", "phase", "domain", "authority_hash", "closure_hash", "inventory_hash", "old_visibility", "target", "manifest_hash")
		if e != nil || !uintRange(event["sequence"], 1, ^uint64(0)) || !uuid(event["recall"]) || !uuid(event["old_visibility"]) || !oneOf(text(event["phase"]), "RECALL", "INSTALL_DRAIN", "INSTALL_FINAL", "REOPEN") || validateCap(event["domain"]) != nil {
			return fsapi.Invalid
		}
		for _, k := range []string{"authority_hash", "closure_hash", "inventory_hash"} {
			if !hash(event[k]) {
				return fsapi.Invalid
			}
		}
		if text(event["authority_hash"]) != c.Context.AuthorityHash {
			return fsapi.Stale
		}
		phase := text(event["phase"])
		if phase == "RECALL" {
			if event["target"] != nil || event["manifest_hash"] != nil {
				return fsapi.Invalid
			}
		} else if validateCommandTarget(c, event["target"]) != nil || !hash(event["manifest_hash"]) {
			return fsapi.Invalid
		}
		var p struct {
			Domain Capability `json:"domain"`
		}
		if json.Unmarshal(c.Payload, &p) != nil {
			return fsapi.Invalid
		}
		raw, e := marshal(event["domain"])
		if e != nil {
			return e
		}
		var cap Capability
		if json.Unmarshal(raw, &cap) != nil || !equalCap(p.Domain, cap) {
			return fsapi.Stale
		}
	default:
		return fsapi.Unsupported
	}
	return nil
}
func validateResult(raw json.RawMessage) (map[string]any, error) {
	v, e := Canonical(raw)
	if e != nil {
		return nil, e
	}
	o, e := object(v, "id", "code", "count", "data", "attributes", "capabilities", "entries", "capacity", "publication")
	if e != nil || !hash(o["id"]) || !uintRange(o["count"], 0, ^uint64(0)) || !bytesField(o["data"], 65536, false) {
		return nil, fsapi.Invalid
	}
	valid := oneOf(text(o["code"]), "ID_CONFLICT", "RETIRED_ID", "PROTOCOL_ERROR", "FileTooLarge")
	for _, name := range errnoNames() {
		valid = valid || text(o["code"]) == name
	}
	if !valid {
		return nil, fsapi.Invalid
	}
	caps, ok := o["capabilities"].([]any)
	if !ok || len(caps) > 256 {
		return nil, fsapi.Invalid
	}
	for _, cap := range caps {
		if validateCap(cap) != nil {
			return nil, fsapi.Invalid
		}
	}
	entries, ok := o["entries"].([]any)
	if !ok || len(entries) > 256 {
		return nil, fsapi.Invalid
	}
	last := ""
	for _, v := range entries {
		entry, e := object(v, "name", "capability", "attributes")
		if e != nil || !name(entry["name"]) || text(entry["name"]) <= last || validateCap(entry["capability"]) != nil || validateAttr(entry["attributes"]) != nil {
			return nil, fsapi.Invalid
		}
		last = text(entry["name"])
	}
	if o["attributes"] != nil && validateAttr(o["attributes"]) != nil {
		return nil, fsapi.Invalid
	}
	if o["publication"] != nil && validatePublication(o["publication"]) != nil {
		return nil, fsapi.Invalid
	}
	if o["capacity"] != nil {
		c, e := object(o["capacity"], "objects", "objects_free", "bytes", "bytes_free")
		if e != nil {
			return nil, e
		}
		for _, k := range []string{"objects", "objects_free", "bytes", "bytes_free"} {
			if !uintRange(c[k], 0, ^uint64(0)) {
				return nil, fsapi.Invalid
			}
		}
		a, _ := strconv.ParseUint(string(c["objects"].(json.Number)), 10, 64)
		b, _ := strconv.ParseUint(string(c["objects_free"].(json.Number)), 10, 64)
		x, _ := strconv.ParseUint(string(c["bytes"].(json.Number)), 10, 64)
		y, _ := strconv.ParseUint(string(c["bytes_free"].(json.Number)), 10, 64)
		if b > a || y > x {
			return nil, fsapi.Invalid
		}
	}
	if o["code"] != "OK" && (!uintRange(o["count"], 0, 0) || o["data"] != "" || len(caps) != 0 || len(entries) != 0 || o["attributes"] != nil || o["capacity"] != nil) {
		return nil, fsapi.Invalid
	}
	id := o["id"]
	delete(o, "id")
	b, e := marshal(o)
	if e != nil || digest(b) != id {
		return nil, fsapi.Invalid
	}
	o["id"] = id
	return o, nil
}
func validateAttr(v any) error {
	o, e := object(v, "object_home", "object_allocation", "kind", "uid", "gid", "mode", "size", "nlink", "revision", "atime", "mtime", "ctime")
	if e != nil || !uuid(o["object_home"]) || !uuid(o["object_allocation"]) || !oneOf(text(o["kind"]), "REGULAR", "DIRECTORY", "SYMLINK") || !uintRange(o["uid"], 0, 1<<32-1) || !uintRange(o["gid"], 0, 1<<32-1) || !uintRange(o["mode"], 0, 4095) || !uintRange(o["size"], 0, 1<<63-1) || !uintRange(o["nlink"], 0, ^uint64(0)) || !uintRange(o["revision"], 1, ^uint64(0)) {
		return fsapi.Invalid
	}
	for _, k := range []string{"atime", "mtime", "ctime"} {
		if validateTime(o[k]) != nil {
			return fsapi.Invalid
		}
	}
	return nil
}
