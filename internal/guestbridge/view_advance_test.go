package guestbridge

import (
	"encoding/json"
	"testing"
)

// Guest-requested view advance (task xf4, view_advance.go): the optional
// VIEW_ACQUIRE "advance" field and the GRANTED target it allows. The corpus
// command read_domain_advance and its GRANTED response (an advanced cut) are
// checked by TestViewProtocolLiteralCodec like every other literal.

// canonicalCommand returns the canonical map of the corpus command name.
func canonicalCommand(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, e := marshal(viewCommands(t)[name])
	if e != nil {
		t.Fatal(e)
	}
	v, e := Canonical(raw)
	if e != nil {
		t.Fatal(e)
	}
	return v.(map[string]any)
}

// TestViewAdvanceSchemaRefusals: advance is accepted only as exactly true on
// a READ DOMAIN acquisition with a CONTROLLED_LOCAL expected target, so every
// request keeps a single encoding and no other acquisition can ask for a
// different target.
func TestViewAdvanceSchemaRefusals(t *testing.T) {
	normal := canonicalCommand(t, "read_domain")["payload"].(map[string]any)["target"]
	object := canonicalCommand(t, "read_object")["payload"].(map[string]any)["access"]
	pageAccess := canonicalCommand(t, "dirty_page")["payload"].(map[string]any)["access"]
	cases := []struct {
		name   string
		change func(p map[string]any)
	}{
		{"advance false", func(p map[string]any) { p["advance"] = false }},
		{"advance string", func(p map[string]any) { p["advance"] = "true" }},
		{"advance number", func(p map[string]any) { p["advance"] = json.Number("1") }},
		{"advance null", func(p map[string]any) { p["advance"] = nil }},
		{"discovery target null", func(p map[string]any) { p["target"] = nil }},
		{"normal public target", func(p map[string]any) { p["target"] = normal }},
		{"OBJECT access", func(p map[string]any) { p["access"] = object }},
		{"PAGE READ access", func(p map[string]any) { p["access"] = pageAccess }},
		{"PAGE EXCLUSIVE_DIRTY", func(p map[string]any) { p["access"], p["mode"] = pageAccess, "EXCLUSIVE_DIRTY" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := canonicalCommand(t, "read_domain_advance")
			tc.change(o["payload"].(map[string]any))
			raw, e := marshal(o)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = DecodeCommandVersion(raw, 1); e == nil {
				t.Fatal("accepted advance outside a READ DOMAIN local observation")
			}
		})
	}
	o := canonicalCommand(t, "read_domain_advance")
	delete(o["payload"].(map[string]any), "advance")
	raw, e := marshal(o)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeCommandVersion(raw, 1); e != nil {
		t.Fatalf("the same acquire without advance: %v", e)
	}
}

// advanceResponse returns the corpus GRANTED reply of read_domain_advance
// rebuilt for command o with its lease target changed by change (nil keeps
// it), so only the target check can refuse it: request hash, lease id and
// reply id are recomputed.
func advanceResponse(t *testing.T, o map[string]any, change func(cut map[string]any)) json.RawMessage {
	t.Helper()
	var body json.RawMessage
	for _, r := range readViewCorpus(t).Responses {
		if r.Command == "read_domain_advance" {
			body = r.Body
		}
	}
	v, e := Canonical(mustCanonical(t, body))
	if e != nil {
		t.Fatal(e)
	}
	reply := v.(map[string]any)
	lease := reply["lease"].(map[string]any)
	if change != nil {
		change(lease["target"].(map[string]any)["cut"].(map[string]any))
	}
	delete(lease, "id")
	lease["id"] = wireHash(lease)
	reply["request_hash"] = wireHash(map[string]any{"op": "VIEW_ACQUIRE", "payload": o["payload"]})
	delete(reply, "id")
	reply["id"] = wireHash(reply)
	raw, e := marshal(reply)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

// decodeCanonical decodes the canonical command map o.
func decodeCanonical(t *testing.T, o map[string]any) Command {
	t.Helper()
	raw, e := marshal(o)
	if e != nil {
		t.Fatal(e)
	}
	c, e := DecodeCommandVersion(raw, 1)
	if e != nil {
		t.Fatal(e)
	}
	return c
}

// TestViewAdvanceResponseTarget: a GRANTED lease on another target than the
// expected one passes the bridge only for an advance request and only on an
// AdvanceCompatible cut; the same reply to the request without advance, or
// on a cut behind the expected one, of another epoch or a divergent journal
// at the same position, is Stale.
func TestViewAdvanceResponseTarget(t *testing.T) {
	advance := canonicalCommand(t, "read_domain_advance")
	if e := validateControlResponse(decodeCanonical(t, advance), advanceResponse(t, advance, nil)); e != nil {
		t.Fatalf("advanced grant refused: %v", e)
	}
	plain := canonicalCommand(t, "read_domain_advance")
	delete(plain["payload"].(map[string]any), "advance")
	if e := validateControlResponse(decodeCanonical(t, plain), advanceResponse(t, plain, nil)); e == nil {
		t.Fatal("different target granted without advance")
	}
	for name, change := range map[string]func(cut map[string]any){
		"behind the expected cut": func(cut map[string]any) { cut["journal_position"] = json.Number("6") },
		"another epoch":           func(cut map[string]any) { cut["journal_epoch"] = json.Number("4") },
		"divergent at the same position": func(cut map[string]any) {
			cut["journal_position"] = json.Number("7")
		},
	} {
		if e := validateControlResponse(decodeCanonical(t, advance), advanceResponse(t, advance, change)); e == nil {
			t.Errorf("%s: advanced grant accepted", name)
		}
	}
}

// TestAdvanceCompatible pins every field of the advance condition.
func TestAdvanceCompatible(t *testing.T) {
	base := ViewTarget{Kind: "CONTROLLED_LOCAL", Cut: &LocalViewCut{EnrollmentHash: "e", ProfileHash: "p", ContextHash: "c", JournalIncarnation: "i", JournalEpoch: 3, JournalPosition: 7, JournalHash: "j", StateHash: "s", VisibilityGeneration: "v"}}
	cases := []struct {
		name   string
		change func(*LocalViewCut)
		want   bool
	}{
		{"newer position", func(c *LocalViewCut) { c.JournalPosition, c.JournalHash, c.StateHash = 9, "j2", "s2" }, true},
		{"same position and journal, other state", func(c *LocalViewCut) { c.StateHash = "s2" }, true},
		{"same position, divergent journal", func(c *LocalViewCut) { c.JournalHash = "j2" }, false},
		{"older position", func(c *LocalViewCut) { c.JournalPosition = 6 }, false},
		{"other enrollment", func(c *LocalViewCut) { c.JournalPosition, c.EnrollmentHash = 9, "x" }, false},
		{"other profile", func(c *LocalViewCut) { c.JournalPosition, c.ProfileHash = 9, "x" }, false},
		{"other context", func(c *LocalViewCut) { c.JournalPosition, c.ContextHash = 9, "x" }, false},
		{"other incarnation", func(c *LocalViewCut) { c.JournalPosition, c.JournalIncarnation = 9, "x" }, false},
		{"other epoch", func(c *LocalViewCut) { c.JournalPosition, c.JournalEpoch = 9, 4 }, false},
		{"other visibility generation", func(c *LocalViewCut) { c.JournalPosition, c.VisibilityGeneration = 9, "x" }, false},
	}
	for _, tc := range cases {
		cut := *base.Cut
		tc.change(&cut)
		granted := ViewTarget{Kind: "CONTROLLED_LOCAL", Cut: &cut}
		if got := AdvanceCompatible(base, granted); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if AdvanceCompatible(ViewTarget{Kind: "NORMAL_PUBLIC"}, base) || AdvanceCompatible(base, ViewTarget{Kind: "CONTROLLED_LOCAL"}) {
		t.Error("advance accepted a non-local or cutless target")
	}
}
