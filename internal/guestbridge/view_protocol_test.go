package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

type viewCorpus struct {
	Protocol Protocol `json:"protocol"`
	Commands []struct {
		Name  string          `json:"name"`
		Body  json.RawMessage `json:"body"`
		Frame string          `json:"frame_hex"`
		Hash  string          `json:"sha256"`
	} `json:"commands"`
	Responses []struct {
		Command string          `json:"command"`
		Body    json.RawMessage `json:"body"`
	} `json:"responses"`
}
type refusingViewProvider struct{ checks int }

func (p *refusingViewProvider) CheckEnrollment(context.Context, Context, Protocol) error {
	return fsapi.Permission
}
func (p *refusingViewProvider) CheckTarget(context.Context, Context, ViewTarget) error {
	p.checks++
	return fsapi.Permission
}
func (p *refusingViewProvider) CheckRequest(context.Context, Context, string, json.RawMessage) error {
	return fsapi.Permission
}
func (p *refusingViewProvider) CheckResponse(context.Context, Context, string, json.RawMessage, json.RawMessage) error {
	return fsapi.Permission
}
func (p *refusingViewProvider) ExchangeView(context.Context, Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, fsapi.Permission
}

func readViewCorpus(t *testing.T) viewCorpus {
	t.Helper()
	raw, e := os.ReadFile("../../docs/spec/retained9p-fixtures/views.json")
	if e != nil {
		t.Fatal(e)
	}
	var c viewCorpus
	if e = json.Unmarshal(raw, &c); e != nil {
		t.Fatal(e)
	}
	return c
}
func viewCommands(t *testing.T) map[string]Command {
	t.Helper()
	out := map[string]Command{}
	for _, c := range readViewCorpus(t).Commands {
		v, e := Canonical(mustCanonical(t, c.Body))
		if e != nil {
			t.Fatal(e)
		}
		raw, e := marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		cmd, e := DecodeCommandVersion(raw, 1)
		if e != nil {
			t.Fatalf("%s: %v", c.Name, e)
		}
		out[c.Name] = cmd
	}
	return out
}
func mustCanonical(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(&v); e != nil {
		t.Fatal(e)
	}
	b, e := marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestViewProtocolLiteralCodec(t *testing.T) {
	corpus := readViewCorpus(t)
	if corpus.Protocol != ViewProtocol() {
		t.Fatal("tuple")
	}
	commands := map[string]Command{}
	for _, c := range corpus.Commands {
		t.Run(c.Name, func(t *testing.T) {
			raw := mustCanonical(t, c.Body)
			if digest(raw) != c.Hash {
				t.Fatal("literal digest changed")
			}
			cmd, e := DecodeCommandVersion(raw, 1)
			if e != nil {
				t.Fatal(e)
			}
			commands[c.Name] = cmd
			if _, e = DecodeCommand(raw); e == nil {
				t.Fatal("old body reader accepted extension")
			}
			if _, e = DecodeCommandVersion(raw, 0); e == nil {
				t.Fatal("minor0 accepted1.1 body")
			}
			frame, e := hex.DecodeString(c.Frame)
			if e != nil {
				t.Fatal(e)
			}
			f, e := DecodeFrame(frame)
			if e != nil {
				t.Fatal(e)
			}
			if f.Minor != 1 || f.Hash != sha256.Sum256(raw) || !bytes.Equal(f.Data, raw) || f.Cell != cmd.Cell {
				t.Fatal("literal frame binding")
			}
			encoded, e := EncodeFrame(f)
			if e != nil || !bytes.Equal(encoded, frame) {
				t.Fatal("frame roundtrip")
			}
		})
	}
	for _, r := range corpus.Responses {
		t.Run("response_"+r.Command, func(t *testing.T) {
			c := commands[r.Command]
			raw := mustCanonical(t, r.Body)
			if e := validateControlResponse(c, raw); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestViewProtocolClosedNegativeControls(t *testing.T) {
	commands := viewCommands(t)
	type mutation struct {
		name, command string
		change        func(map[string]any)
	}
	cases := []mutation{
		{"unknown_feature", "hello", func(o map[string]any) { o["protocol"].(map[string]any)["features"] = json.Number("4095") }},
		{"wrong_body_schema", "hello", func(o map[string]any) { o["protocol"].(map[string]any)["body_schema"] = json.Number("1") }},
		{"hello_downgrade", "hello", func(o map[string]any) { o["payload"].(map[string]any)["bridge_minor"] = json.Number("0") }},
		{"missing_protocol", "query", func(o map[string]any) { delete(o, "protocol") }},
		{"wrong_context", "dirty_page", func(o map[string]any) { o["context"].(map[string]any)["session"] = uid(201).String() }},
		{"wrong_rights", "dirty_page", func(o map[string]any) {
			a := o["payload"].(map[string]any)["access"].(map[string]any)
			a["description"].(map[string]any)["rights"] = json.Number("1")
		}},
		{"page_overflow", "dirty_page", func(o map[string]any) {
			o["payload"].(map[string]any)["access"].(map[string]any)["offset"] = json.Number("9223372036854775807")
		}},
		{"unaligned_page", "dirty_page", func(o map[string]any) {
			o["payload"].(map[string]any)["access"].(map[string]any)["offset"] = json.Number("1")
		}},
		{"unsupported_page_size", "dirty_page", func(o map[string]any) {
			o["payload"].(map[string]any)["access"].(map[string]any)["length"] = json.Number("65536")
		}},
		{"dirty_domain", "dirty_page", func(o map[string]any) { o["payload"].(map[string]any)["access"] = map[string]any{"kind": "DOMAIN"} }},
		{"untagged_publication", "read_domain", func(o map[string]any) {
			p := o["payload"].(map[string]any)
			p["target"] = p["target"].(map[string]any)["publication"]
		}},
		{"normal_extra_local_cut", "read_domain", func(o map[string]any) { o["payload"].(map[string]any)["target"].(map[string]any)["cut"] = nil }},
		{"null_install_target", "installed", func(o map[string]any) { o["payload"].(map[string]any)["target"] = nil }},
		{"null_lease_target", "reserve", func(o map[string]any) { o["payload"].(map[string]any)["lease"].(map[string]any)["target"] = nil }},
		{"wrong_epoch", "dirty_page", func(o map[string]any) {
			o["payload"].(map[string]any)["target"].(map[string]any)["cut"].(map[string]any)["journal_epoch"] = json.Number("0")
		}},
		{"wrong_journal", "dirty_page", func(o map[string]any) {
			o["payload"].(map[string]any)["target"].(map[string]any)["cut"].(map[string]any)["journal_incarnation"] = uid(201).String()
		}},
		{"wrong_credentials", "reserve", func(o map[string]any) {
			o["payload"].(map[string]any)["request"].(map[string]any)["credentials"].(map[string]any)["credential_revision"] = json.Number("19")
		}},
		{"freeze_changed_data", "freeze", func(o map[string]any) { o["payload"].(map[string]any)["data"] = "41424345" }},
		{"freeze_counter_reuse", "freeze", func(o map[string]any) {
			o["payload"].(map[string]any)["claim"].(map[string]any)["number"] = json.Number("10")
		}},
		{"freeze_changed_recall", "freeze", func(o map[string]any) { o["payload"].(map[string]any)["recall"] = uid(201).String() }},
		{"freeze_changed_scope", "freeze", func(o map[string]any) {
			o["payload"].(map[string]any)["reservation"].(map[string]any)["scope"].(map[string]any)["generation"] = uid(201).String()
		}},
		{"retire_no_evidence", "retire", func(o map[string]any) { o["payload"].(map[string]any)["result_hashes"] = []any{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, e := marshal(commands[tc.command])
			if e != nil {
				t.Fatal(e)
			}
			v, e := Canonical(raw)
			if e != nil {
				t.Fatal(e)
			}
			o := v.(map[string]any)
			tc.change(o)
			raw, e = marshal(o)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = DecodeCommandVersion(raw, 1); e == nil {
				t.Fatal("accepted invalid union/binding")
			}
		})
	}
}
func TestViewResponseCannotInventAuthority(t *testing.T) {
	cmds := viewCommands(t)
	corpus := readViewCorpus(t)
	for _, r := range corpus.Responses {
		if r.Command != "dirty_page" || !bytes.Contains(r.Body, []byte("GRANTED")) {
			continue
		}
		v, e := Canonical(mustCanonical(t, r.Body))
		if e != nil {
			t.Fatal(e)
		}
		o := v.(map[string]any)
		o["status"] = "PENDING"
		delete(o, "id")
		o["id"] = wireHash(o)
		raw, e := marshal(o)
		if e != nil {
			t.Fatal(e)
		}
		if e = validateViewResponse(cmds[r.Command], raw); e == nil {
			t.Fatal("pending leased authority")
		}
	}
	c := cmds["dirty_page"]
	p, e := Canonical(c.Payload)
	if e != nil {
		t.Fatal(e)
	}
	target := p.(map[string]any)["target"]
	if e = validateViewTarget(c.Context, target); e != nil {
		t.Fatal(e)
	}
	provider := &refusingViewProvider{}
	s := &Service{config: Config{Validation: &fsservice.ValidationProfile{Name: "fixture", Disposable: true}, ViewProtocol: provider}}
	if e = s.checkTarget(t.Context(), c.Context, target); e == nil || provider.checks != 1 {
		t.Fatal("hashes supplied source proof")
	}
}

func TestViewReplyLineageAndTargetNegativeControls(t *testing.T) {
	commands := viewCommands(t)
	for _, r := range readViewCorpus(t).Responses {
		if r.Command != "dirty_page" && r.Command != "poll" {
			continue
		}
		v, e := Canonical(mustCanonical(t, r.Body))
		if e != nil {
			t.Fatal(e)
		}
		o := v.(map[string]any)
		if r.Command == "poll" {
			o["event"].(map[string]any)["target"] = nil
			raw, e := marshal(o)
			if e != nil {
				t.Fatal(e)
			}
			if e = validateControlResponse(commands["poll"], raw); e == nil {
				t.Fatal("null install target")
			}
			continue
		}
		for _, field := range []string{"operation", "request_hash"} {
			original := o[field]
			if field == "operation" {
				o[field] = "DIRTY_FREEZE"
			} else {
				o[field] = digest([]byte("changed admission"))
			}
			delete(o, "id")
			o["id"] = wireHash(o)
			raw, e := marshal(o)
			if e != nil {
				t.Fatal(e)
			}
			if e = validateViewResponse(commands["dirty_page"], raw); e == nil {
				t.Fatal("changed original admission lineage")
			}
			o[field] = original
		}
	}
}

func TestViewServiceEnforcesEnrolledVersionAndProvider(t *testing.T) {
	f := newBridge(t)
	legacyHello := f.command(t, "HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0})
	protocol := ViewProtocol()
	config := f.s.config
	config.Protocol = &protocol
	if _, e := New(t.Context(), config); e == nil {
		t.Fatal("missing mandatory extension provider")
	}
	provider := &refusingViewProvider{}
	config.ViewProtocol = provider
	if _, e := New(t.Context(), config); e == nil {
		t.Fatal("enrollment fabricated by tuple")
	}
	f.s.config.Protocol = &protocol
	f.s.config.ViewProtocol = provider
	if _, e := f.s.Execute(t.Context(), f.native, legacyHello); e == nil {
		t.Fatal("enrolled1.1 accepted1.0 HELLO")
	}
	payload, e := marshal(protocol)
	if e != nil {
		t.Fatal(e)
	}
	hello := Command{Protocol: &protocol, Cell: 1, Context: f.c, Op: "HELLO", Payload: payload}
	response, e := f.s.Execute(t.Context(), f.native, hello)
	if e != nil {
		t.Fatal(e)
	}
	value, e := Canonical(response)
	if e != nil {
		t.Fatal(e)
	}
	if validateProtocol(value.(map[string]any)["protocol"]) != nil {
		t.Fatal("response lost protocol")
	}
	queryPayload, e := marshal(map[string]any{"request": Ticket{Anchor: uid(81).String(), Nonce: uid(82).String(), Number: 1, Credentials: f.cred()}})
	if e != nil {
		t.Fatal(e)
	}
	query := Command{Protocol: &protocol, Cell: 2, Context: f.c, Op: "VIEW_QUERY", Payload: queryPayload}
	if _, e = f.s.Execute(t.Context(), f.native, query); e == nil {
		t.Fatal("ticket lookup bypassed original principal authorization")
	}
	transport, e := NewTransport(f.s, transportBudget(t))
	if e != nil {
		t.Fatal(e)
	}
	defer transport.Close()
	raw, e := marshal(hello)
	if e != nil {
		t.Fatal(e)
	}
	frame := Frame{Minor: 0, Kind: 1, Session: parseID(f.c.Session), Cell: 1, Total: uint32(len(raw)), Hash: sha256.Sum256(raw), Data: raw}
	wire, e := EncodeFrame(frame)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = transport.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); e == nil {
		t.Fatal("frame/body negotiated version disagreement")
	}
	if transport.high != 0 || len(transport.cells) != 0 {
		t.Fatal("version refusal mutated allocator")
	}
	if cap(transport.acquisitionQueue) != 64 || cap(transport.drainQueue) != 16 || cap(transport.controlQueue) != 16 || cap(transport.replacementQueue) != 1 {
		t.Fatal("bounded independent queues")
	}
}

func TestViewSessionProtocolEnrollmentIsImmutable(t *testing.T) {
	scope := viewCommands(t)["hello"].Context
	original := map[string]Protocol{scope.Session: ViewProtocol()}
	config := Config{SessionProtocols: original, Scopes: []Context{scope}, ViewProtocol: &refusingViewProvider{}}
	if e := prepareProtocols(&config); e != nil {
		t.Fatal(e)
	}
	p := original[scope.Session]
	p.Features = 255
	original[scope.Session] = p
	if config.sessionProtocol(scope.Session) == nil || *config.sessionProtocol(scope.Session) != ViewProtocol() {
		t.Fatal("caller mutated installed tuple")
	}
	if config.sessionProtocol(uid(201).String()) != nil {
		t.Fatal("unlisted session gained1.1")
	}
	for _, c := range []Config{
		{Protocol: ptrProtocol(ViewProtocol()), SessionProtocols: map[string]Protocol{}, ViewProtocol: &refusingViewProvider{}},
		{SessionProtocols: map[string]Protocol{uid(201).String(): ViewProtocol()}, Scopes: []Context{scope}, ViewProtocol: &refusingViewProvider{}},
		{SessionProtocols: map[string]Protocol{scope.Session: p}, Scopes: []Context{scope}, ViewProtocol: &refusingViewProvider{}},
	} {
		if e := prepareProtocols(&c); e == nil {
			t.Fatal("invalid/mixed enrollment accepted")
		}
	}
}
func ptrProtocol(p Protocol) *Protocol { return &p }
