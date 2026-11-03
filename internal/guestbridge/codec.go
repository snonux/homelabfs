package guestbridge

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Frame is one complete positional H9B chunk, independently owned by its caller.
type Frame struct {
	Minor         uint16
	Flags         uint16
	Kind          uint16
	Total, Offset uint32
	Session       fsapi.UUID
	Cell          uint64
	Hash          [32]byte
	Data          []byte
}

// EncodeFrame emits exactly one bounded wire chunk.
func EncodeFrame(f Frame) ([]byte, error) {
	if err := validFrame(f); err != nil {
		return nil, err
	}
	b := make([]byte, HeaderBytes+len(f.Data))
	copy(b, "HFS9BR1\x00")
	binary.LittleEndian.PutUint16(b[8:], 1)
	binary.LittleEndian.PutUint16(b[10:], f.Minor)
	binary.LittleEndian.PutUint16(b[12:], f.Kind)
	binary.LittleEndian.PutUint16(b[14:], f.Flags)
	binary.LittleEndian.PutUint32(b[16:], f.Total)
	binary.LittleEndian.PutUint32(b[20:], f.Offset)
	binary.LittleEndian.PutUint32(b[24:], uint32(len(f.Data)))
	copy(b[32:], f.Session[:])
	binary.LittleEndian.PutUint64(b[48:], f.Cell)
	copy(b[56:], f.Hash[:])
	copy(b[88:], f.Data)
	return b, nil
}

// DecodeFrame rejects all trailing bytes, padding, partial chunks and reserved bits.
func DecodeFrame(b []byte) (Frame, error) {
	if len(b) < HeaderBytes || string(b[:8]) != "HFS9BR1\x00" || binary.LittleEndian.Uint16(b[8:]) != 1 || binary.LittleEndian.Uint16(b[10:]) > 3 || binary.LittleEndian.Uint32(b[28:]) != 0 {
		return Frame{}, fsapi.Invalid
	}
	f := Frame{Minor: binary.LittleEndian.Uint16(b[10:]), Flags: binary.LittleEndian.Uint16(b[14:]), Kind: binary.LittleEndian.Uint16(b[12:]), Total: binary.LittleEndian.Uint32(b[16:]), Offset: binary.LittleEndian.Uint32(b[20:]), Cell: binary.LittleEndian.Uint64(b[48:])}
	copy(f.Session[:], b[32:48])
	copy(f.Hash[:], b[56:88])
	if uint64(binary.LittleEndian.Uint32(b[24:])) != uint64(len(b)-HeaderBytes) {
		return Frame{}, fsapi.Invalid
	}
	f.Data = bytes.Clone(b[HeaderBytes:])
	return f, validFrame(f)
}

// Canonical returns bounded sorted ASCII JSON or refuses noncanonical input.
func Canonical(b []byte) (any, error) {
	if len(b) == 0 || len(b) > BodyBytes {
		return nil, fsapi.Invalid
	}
	depth, nodes := 0, 0
	quoted, escape := false, false
	for _, c := range b {
		if c < 32 || c > 126 {
			return nil, fsapi.Invalid
		}
		if quoted {
			if escape {
				if c != '"' && c != '\\' {
					return nil, fsapi.Invalid
				}
				escape = false
			} else if c == '\\' {
				escape = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
			nodes++
		case '{', '[':
			depth++
			nodes++
			if depth > 24 {
				return nil, fsapi.Invalid
			}
		case '}', ']':
			depth--
		case ',':
			nodes++
		}
		if nodes > 32768 {
			return nil, fsapi.NoSpace
		}
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, fsapi.Invalid
	}
	if d.Decode(new(any)) != io.EOF {
		return nil, fsapi.Invalid
	}
	if err := integers(value); err != nil {
		return nil, err
	}
	encoded, err := marshal(value)
	if err != nil || !bytes.Equal(encoded, b) {
		return nil, fsapi.Invalid
	}
	return value, nil
}

// DecodeCommand validates the complete closed union before queue admission.
func DecodeCommand(b []byte) (Command, error) {
	v, err := Canonical(b)
	if err != nil {
		return Command{}, err
	}
	o, err := object(v, "cell", "context", "op", "payload", "reclaim")
	if err != nil {
		return Command{}, err
	}
	if !uintRange(o["cell"], 1, MaxCell) || validateContext(o["context"]) != nil {
		return Command{}, fsapi.Invalid
	}
	if o["reclaim"] != nil && validateAck(o["reclaim"]) != nil {
		return Command{}, fsapi.Invalid
	}
	op, ok := o["op"].(string)
	if !ok {
		return Command{}, fsapi.Invalid
	}
	if err = validatePayload(op, o["payload"], o["reclaim"]); err != nil {
		return Command{}, err
	}
	var c Command
	if err = json.Unmarshal(b, &c); err != nil {
		return Command{}, fsapi.Invalid
	}
	return c, nil
}
func validFrame(f Frame) error {
	if f.Minor > 3 || f.Flags & ^uint16(3) != 0 || f.Kind < 1 || f.Kind > 3 || !f.Session.Valid() || f.Cell < 1 || f.Cell > MaxCell || f.Total < 1 || f.Total > BodyBytes || f.Offset >= f.Total || f.Offset%ChunkBytes != 0 || len(f.Data) != min(ChunkBytes, int(f.Total-f.Offset)) {
		return fsapi.Invalid
	}
	return nil
}
func marshal(v any) ([]byte, error) {
	var first bytes.Buffer
	e := json.NewEncoder(&first)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	d := json.NewDecoder(&first)
	d.UseNumber()
	var generic any
	if err := d.Decode(&generic); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	e = json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(generic); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func integers(v any) error {
	switch x := v.(type) {
	case json.Number:
		s := string(x)
		if strings.ContainsAny(s, ".eE+") || s == "-0" {
			return fsapi.Invalid
		}
		if strings.HasPrefix(s, "-") {
			if _, e := strconv.ParseInt(s, 10, 64); e != nil {
				return fsapi.Invalid
			}
		} else if _, e := strconv.ParseUint(s, 10, 64); e != nil {
			return fsapi.Invalid
		}
	case map[string]any:
		for _, v := range x {
			if err := integers(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range x {
			if err := integers(v); err != nil {
				return err
			}
		}
	}
	return nil
}
func object(v any, keys ...string) (map[string]any, error) {
	o, ok := v.(map[string]any)
	if !ok || len(o) != len(keys) {
		return nil, fsapi.Invalid
	}
	for _, k := range keys {
		if _, ok = o[k]; !ok {
			return nil, fsapi.Invalid
		}
	}
	return o, nil
}
func uintRange(v any, lo, hi uint64) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	u, e := strconv.ParseUint(string(n), 10, 64)
	return e == nil && u >= lo && u <= hi
}
func text(v any) string { s, _ := v.(string); return s }
func uuid(v any) bool   { _, e := fsapi.ParseUUID(text(v)); return e == nil }
func hash(v any) bool {
	s := text(v)
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func bytesField(v any, max int, nonempty bool) bool {
	s, ok := v.(string)
	if !ok || len(s) > 2*max || nonempty && s == "" {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && hex.EncodeToString(b) == s
}
func validateContext(v any) error {
	keys := []string{"authority_hash", "cluster", "configuration", "export", "exporter_incarnation", "fuse_session", "generation", "guest_boot", "guest_mount", "origin_incarnation", "origin_journal", "origin_node", "policy_hash", "registry_hash", "session", "vm", "volume"}
	o, e := object(v, keys...)
	if e != nil {
		return e
	}
	for _, k := range keys {
		if strings.HasSuffix(k, "_hash") {
			if !hash(o[k]) {
				return fsapi.Invalid
			}
		} else if !uuid(o[k]) {
			return fsapi.Invalid
		}
	}
	return nil
}
func validateAck(v any) error {
	o, e := object(v, "cell", "response_hash")
	if e != nil || !uintRange(o["cell"], 1, MaxCell) || !hash(o["response_hash"]) {
		return fsapi.Invalid
	}
	return nil
}
func validateCred(v any) error {
	o, e := object(v, "fsuid", "fsgid", "groups", "process", "credential_revision", "idmap_hash", "user_namespace")
	if e != nil || !uintRange(o["fsuid"], 0, 1<<32-1) || !uintRange(o["fsgid"], 0, 1<<32-1) || !uuid(o["process"]) || !uuid(o["user_namespace"]) || !hash(o["idmap_hash"]) || !uintRange(o["credential_revision"], 1, ^uint64(0)) {
		return fsapi.Invalid
	}
	groups, ok := o["groups"].([]any)
	if !ok || len(groups) > 1024 {
		return fsapi.Invalid
	}
	var last uint64
	for i, g := range groups {
		if !uintRange(g, 0, 1<<32-1) {
			return fsapi.Invalid
		}
		n, _ := strconv.ParseUint(string(g.(json.Number)), 10, 64)
		if i > 0 && n <= last {
			return fsapi.Invalid
		}
		last = n
	}
	return nil
}
func validateClaim(v any) error {
	o, e := object(v, "realm", "subject", "anchor", "nonce", "number", "original_hash")
	if e != nil || !uuid(o["realm"]) || !uuid(o["anchor"]) || !uuid(o["nonce"]) || !bytesField(o["subject"], 1024, true) || !uintRange(o["number"], 1, ^uint64(0)) || !hash(o["original_hash"]) {
		return fsapi.Invalid
	}
	return nil
}
func validateCap(v any) error {
	o, e := object(v, "token", "kind", "scope", "object_home", "object_allocation", "description", "rights", "revision", "credential_hash", "ancestors_hash")
	if e != nil || !hash(o["token"]) || validateContext(o["scope"]) != nil || !uuid(o["object_home"]) || !uuid(o["object_allocation"]) || !hash(o["credential_hash"]) || !hash(o["ancestors_hash"]) || !uintRange(o["rights"], 0, 63) || !uintRange(o["revision"], 1, ^uint64(0)) {
		return fsapi.Invalid
	}
	switch text(o["kind"]) {
	case "DESCRIPTION":
		if !uuid(o["description"]) {
			return fsapi.Invalid
		}
	case "ROOT", "PATH", "DOMAIN":
		if o["description"] != nil {
			return fsapi.Invalid
		}
	default:
		return fsapi.Invalid
	}
	return nil
}
