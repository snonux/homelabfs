package guestbridge

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func validatePayload(op string, v, reclaim any) error {
	switch op {
	case "HELLO":
		o, e := object(v, "body_schema", "bridge_major", "bridge_minor", "features", "msize", "normal_major", "normal_minor")
		if e != nil {
			return e
		}
		for k, w := range map[string]uint64{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0} {
			if !uintRange(o[k], w, w) {
				return fsapi.Invalid
			}
		}
	case "CALL":
		o, e := object(v, "claim", "credentials", "intent")
		if e != nil || validateClaim(o["claim"]) != nil || validateCred(o["credentials"]) != nil {
			return fsapi.Invalid
		}
		in, e := object(o["intent"], "opcode", "args")
		if e != nil {
			return e
		}
		if e = validateArgs(text(in["opcode"]), in["args"]); e != nil {
			return e
		}
		raw, e := marshal(o["intent"])
		if e != nil || digest(raw) != text(o["claim"].(map[string]any)["original_hash"]) {
			return fsapi.Invalid
		}
	case "QUERY", "CANCEL":
		o, e := object(v, "claim")
		if e != nil {
			return e
		}
		return validateClaim(o["claim"])
	case "RELEASE_CELL":
		if reclaim == nil || validateAck(v) != nil {
			return fsapi.Invalid
		}
		a, e := marshal(v)
		if e != nil {
			return e
		}
		b, e := marshal(reclaim)
		if e != nil || !bytes.Equal(a, b) {
			return fsapi.Invalid
		}
	case "RETIRE":
		o, e := object(v, "realm", "subject", "anchor", "nonce", "through", "result_hashes")
		if e != nil || !uuid(o["realm"]) || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !bytesField(o["subject"], 1024, true) || !uintRange(o["through"], 1, ^uint64(0)) {
			return fsapi.Invalid
		}
		a, ok := o["result_hashes"].([]any)
		if !ok || len(a) == 0 || len(a) > 1024 {
			return fsapi.Invalid
		}
		for _, h := range a {
			if !hash(h) {
				return fsapi.Invalid
			}
		}
	case "RESUME":
		o, e := object(v, "previous_session", "ledger_hash", "claims_hash", "domains_hash")
		if e != nil || !uuid(o["previous_session"]) || !hash(o["ledger_hash"]) || !hash(o["claims_hash"]) || !hash(o["domains_hash"]) {
			return fsapi.Invalid
		}
	case "VIEW_POLL":
		o, e := object(v, "domain", "after_sequence")
		if e != nil || validateCap(o["domain"]) != nil || text(o["domain"].(map[string]any)["kind"]) != "DOMAIN" || !uintRange(o["after_sequence"], 0, ^uint64(0)) {
			return fsapi.Invalid
		}
	case "VIEW_ACK":
		return validateViewAck(v)
	default:
		return fsapi.Unsupported
	}
	return nil
}
func validateArgs(op string, v any) error {
	fields := map[string]string{
		"ROOT": "", "LOOKUP": "parent name", "GETATTR": "object", "READLINK": "object", "CREATE": "parent name mode umask flags", "MKDIR": "parent name mode umask", "SYMLINK": "parent name target", "OPEN": "object flags", "READ": "description offset count", "WRITE": "description offset data max_end", "TRUNCATE": "object description size times", "SETATTR": "object attributes", "RENAME": "source_parent source_name destination_parent destination_name no_replace", "LINK": "object parent name", "UNLINK": "parent name", "RMDIR": "parent name", "READDIR": "object after revision limit", "STATFS": "root", "SYNC": "object description scope", "DESCRIPTION_CLOSE": "description process close_sequence", "DESCRIPTION_RELEASE": "description release_sequence", "FORGET": "object count", "PROCESS_EXIT": "process exit_sequence", "DRAIN_WRITE": "description offset data recall dirty_entry reservation",
	}
	names, ok := fields[op]
	if !ok {
		return fsapi.Unsupported
	}
	var keys []string
	if names != "" {
		keys = strings.Split(names, " ")
	}
	o, e := object(v, keys...)
	if e != nil {
		return e
	}
	for k, x := range o {
		switch k {
		case "object", "parent", "source_parent", "destination_parent", "root", "description":
			if x == nil && (op == "TRUNCATE" || op == "SYNC") && k == "description" {
				continue
			}
			if validateCap(x) != nil {
				return fsapi.Invalid
			}
			kind := text(x.(map[string]any)["kind"])
			if k == "description" && kind != "DESCRIPTION" || k != "description" && kind != "ROOT" && kind != "PATH" {
				return fsapi.Invalid
			}
		case "name", "source_name", "destination_name":
			if !name(x) {
				return fsapi.Invalid
			}
		case "target":
			if !bytesField(x, 4096, true) {
				return fsapi.Invalid
			}
			b, _ := hex.DecodeString(text(x))
			if bytes.ContainsRune(b, 0) {
				return fsapi.Invalid
			}
		case "data":
			if !bytesField(x, 65536, false) {
				return fsapi.Invalid
			}
		case "after":
			if !bytesField(x, 255, false) {
				return fsapi.Invalid
			}
		case "offset", "size", "max_end":
			if !uintRange(x, 0, 1<<63-1) {
				return fsapi.Invalid
			}
		case "mode", "umask":
			hi := uint64(4095)
			if k == "umask" {
				hi = 511
			}
			if !uintRange(x, 0, hi) {
				return fsapi.Invalid
			}
		case "count":
			lo, hi := uint64(0), uint64(65536)
			if op == "FORGET" {
				lo, hi = 1, ^uint64(0)
			}
			if !uintRange(x, lo, hi) {
				return fsapi.Invalid
			}
		case "revision":
			if !uintRange(x, 0, ^uint64(0)) {
				return fsapi.Invalid
			}
		case "limit":
			if !uintRange(x, 1, 256) {
				return fsapi.Invalid
			}
		case "close_sequence", "release_sequence", "exit_sequence":
			if !uintRange(x, 1, ^uint64(0)) {
				return fsapi.Invalid
			}
		case "process", "recall":
			if !uuid(x) {
				return fsapi.Invalid
			}
		case "dirty_entry", "reservation":
			if !hash(x) {
				return fsapi.Invalid
			}
		case "no_replace":
			if _, ok := x.(bool); !ok {
				return fsapi.Invalid
			}
		case "flags":
			if validateFlags(x) != nil {
				return fsapi.Invalid
			}
		case "times":
			if validateTimes(x, true) != nil {
				return fsapi.Invalid
			}
		case "attributes":
			if validateTimes(x, false) != nil {
				return fsapi.Invalid
			}
		case "scope":
			if !oneOf(text(x), "DATA", "FILE", "DIRECTORY", "VOLUME") {
				return fsapi.Invalid
			}
		default:
			return fsapi.Invalid
		}
	}
	return nil
}
func name(v any) bool {
	if !bytesField(v, 255, true) {
		return false
	}
	b, _ := hex.DecodeString(text(v))
	return !bytes.ContainsAny(b, "\x00/") && !bytes.Equal(b, []byte(".")) && !bytes.Equal(b, []byte("..")) && string(b) != ControlName
}
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func validateFlags(v any) error {
	o, e := object(v, "access", "append", "create", "exclusive", "sync", "truncate")
	if e != nil || !oneOf(text(o["access"]), "READ", "WRITE", "READWRITE") || !oneOf(text(o["sync"]), "ORDINARY", "DATA", "FILE") {
		return fsapi.Invalid
	}
	for _, k := range []string{"append", "create", "exclusive", "truncate"} {
		if _, ok := o[k].(bool); !ok {
			return fsapi.Invalid
		}
	}
	if o["exclusive"] == true && o["create"] != true || o["access"] == "READ" && (o["truncate"] == true || o["append"] == true) {
		return fsapi.Invalid
	}
	return nil
}
func validateTime(v any) error {
	o, e := object(v, "seconds", "nanos")
	if e != nil || !uintRange(o["nanos"], 0, 999999999) {
		return fsapi.Invalid
	}
	n, ok := o["seconds"].(json.Number)
	if !ok {
		return fsapi.Invalid
	}
	if _, e = n.Int64(); e != nil {
		return fsapi.Invalid
	}
	return nil
}
func validateTimes(v any, truncate bool) error {
	keys := []string{"atime", "mtime", "atime_now", "mtime_now", "mode", "uid", "gid"}
	if truncate {
		keys = []string{"atime", "mtime", "ctime", "atime_now", "mtime_now"}
	}
	o, e := object(v, keys...)
	if e != nil {
		return e
	}
	selected := false
	for _, k := range []string{"atime", "mtime"} {
		b, ok := o[k+"_now"].(bool)
		if !ok || b && o[k] != nil {
			return fsapi.Invalid
		}
		if o[k] != nil {
			if validateTime(o[k]) != nil {
				return fsapi.Invalid
			}
			selected = true
		}
		selected = selected || b
	}
	if truncate {
		if o["ctime"] != nil && validateTime(o["ctime"]) != nil {
			return fsapi.Invalid
		}
		return nil
	}
	for _, k := range []string{"mode", "uid", "gid"} {
		if o[k] == nil {
			continue
		}
		hi := uint64(1<<32 - 1)
		if k == "mode" {
			hi = 4095
		}
		if !uintRange(o[k], 0, hi) {
			return fsapi.Invalid
		}
		selected = true
	}
	if !selected {
		return fsapi.Invalid
	}
	return nil
}
func validatePublication(v any) error {
	o, e := object(v, "authority_hash", "sequencer_node", "sequencer_incarnation", "serial", "selection_hash", "result_ref", "source_manifest_hash", "visibility_generation")
	if e != nil || !hash(o["authority_hash"]) || !uuid(o["sequencer_node"]) || !uuid(o["sequencer_incarnation"]) || !uintRange(o["serial"], 0, ^uint64(0)) || !hash(o["selection_hash"]) || !hash(o["source_manifest_hash"]) || !uuid(o["visibility_generation"]) {
		return fsapi.Invalid
	}
	if o["result_ref"] == nil {
		return nil
	}
	if uintRange(o["serial"], 0, 0) {
		return fsapi.Invalid
	}
	r, e := object(o["result_ref"], "cluster", "volume", "generation", "configuration", "epoch", "origin_node", "journal_incarnation", "sequence", "frame_hash", "creator_node", "creator_incarnation", "kind")
	if e != nil {
		return e
	}
	for k, x := range r {
		switch k {
		case "epoch", "sequence":
			if !uintRange(x, 1, ^uint64(0)) {
				return fsapi.Invalid
			}
		case "kind":
			if !uintRange(x, 1, 13) {
				return fsapi.Invalid
			}
		case "frame_hash":
			if !hash(x) {
				return fsapi.Invalid
			}
		default:
			if !uuid(x) {
				return fsapi.Invalid
			}
		}
	}
	return nil
}
func validateViewAck(v any) error {
	o, e := object(v, "domain", "sequence", "recall", "phase", "authority_hash", "closure_hash", "inventory_hash", "session", "manifest", "target")
	if e != nil || validateCap(o["domain"]) != nil || text(o["domain"].(map[string]any)["kind"]) != "DOMAIN" || !uintRange(o["sequence"], 1, ^uint64(0)) || !uuid(o["recall"]) || !uuid(o["session"]) {
		return fsapi.Invalid
	}
	for _, k := range []string{"authority_hash", "closure_hash", "inventory_hash"} {
		if !hash(o[k]) {
			return fsapi.Invalid
		}
	}
	phase := text(o["phase"])
	if !oneOf(phase, "STOPPED", "DRAINED", "INSTALLED_DRAIN", "INSTALLED_FINAL", "REOPENED") {
		return fsapi.Invalid
	}
	if phase == "STOPPED" || phase == "DRAINED" {
		if validateManifest(o["manifest"]) != nil {
			return fsapi.Invalid
		}
		if phase == "STOPPED" && o["target"] != nil {
			return fsapi.Invalid
		}
	} else if o["manifest"] != nil || o["target"] == nil {
		return fsapi.Invalid
	}
	if o["target"] != nil {
		return validatePublication(o["target"])
	}
	return nil
}
func validateManifest(v any) error {
	o, e := object(v, "hash", "entries")
	if e != nil || !hash(o["hash"]) {
		return fsapi.Invalid
	}
	entries, ok := o["entries"].([]any)
	if !ok || len(entries) > 1024 {
		return fsapi.Invalid
	}
	last := ""
	for _, v := range entries {
		d, e := object(v, "id", "description", "offset", "length", "payload_hash", "claim", "reservation")
		if e != nil || !hash(d["id"]) || text(d["id"]) <= last || validateCap(d["description"]) != nil || text(d["description"].(map[string]any)["kind"]) != "DESCRIPTION" || !uintRange(d["offset"], 0, 1<<63-1) || !uintRange(d["length"], 1, 65536) || !hash(d["payload_hash"]) || validateClaim(d["claim"]) != nil || !hash(d["reservation"]) {
			return fsapi.Invalid
		}
		last = text(d["id"])
		copy := map[string]any{}
		for k, x := range d {
			if k != "id" && k != "claim" {
				copy[k] = x
			}
		}
		raw, e := marshal(copy)
		if e != nil || digest(raw) != last {
			return fsapi.Invalid
		}
	}
	raw, e := marshal(entries)
	if e != nil || digest(raw) != text(o["hash"]) {
		return fsapi.Invalid
	}
	return nil
}
