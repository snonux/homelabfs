package normalpublic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
)

func rewrite(t testing.TB, g Graph, index int, mutate func(*Frame)) Graph {
	t.Helper()
	g = clone(g)
	replacements := make(map[Ref]Ref)
	for i, data := range g.Frames {
		old, err := Reference(data)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if i == index {
			mutate(&f)
		}
		replace := func(ref *Ref) {
			if next, ok := replacements[*ref]; ok {
				*ref = next
			}
		}
		if f.Choice != nil {
			replace(&f.Choice.Binding)
		}
		if f.Selection != nil {
			replace(&f.Selection.Choice)
			if f.Selection.Predecessor != nil {
				replace(f.Selection.Predecessor)
			}
		}
		if f.Result != nil {
			replace(&f.Result.Selection)
		}
		encoded, err := envelope("HFSPUB01", f)
		if err != nil {
			t.Fatal(err)
		}
		g.Frames[i] = encoded
		ref := Ref{Stream: f.Stream, Sequence: f.Sequence, Creator: f.Creator, Kind: f.Kind, Hash: sha256.Sum256(encoded)}
		replacements[old] = ref
		if old == g.Root {
			g.Root = ref
		}
	}
	return g
}

func TestPublicCodecAndTypedGraphRefusals(t *testing.T) {
	b, _, _ := fixture(t)
	proof := protection(t, b, 1)
	got, result, err := VerifyGraph(proof.Graph, 32)
	if err != nil || !equal(got, b) || len(result) == 0 {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		index  int
		mutate func(*Frame)
	}{
		{"wrong original creator", 0, func(f *Frame) { f.Creator.Incarnation = id(99) }},
		{"wrong O", 0, func(f *Frame) { f.Binding.Origin.Origin = id(0x88) }},
		{"modified intent", 0, func(f *Frame) { f.Binding.Intent[0] ^= 1 }},
		{"missing provenance", 0, func(f *Frame) { f.Binding.Credentials.Provenance = nil }},
		{"groups duplicate", 0, func(f *Frame) { f.Binding.Credentials.Groups = []uint32{20, 20} }},
		{"wrong authority", 0, func(f *Frame) { f.Binding.Authority.Closure = id(90) }},
		{"unadmitted O", 0, func(f *Frame) {
			f.Binding.Origin.Origin = id(0xfe)
			f.Binding.OriginLaunch.Node = id(0xfe)
			f.Stream.Origin = id(0xfe)
			f.Creator.Node = id(0xfe)
		}},
		{"wrong choice origin", 1, func(f *Frame) { f.Stream.Origin = id(0x88); f.Creator.Node = id(0x88) }},
		{"choice precedes original", 1, func(f *Frame) { f.Sequence = 1 }},
		{"wrong operation", 1, func(f *Frame) { f.Operation = id(90) }},
		{"unknown choice", 1, func(f *Frame) { f.Choice.Outcome = 3 }},
		{"wrong slot authority", 2, func(f *Frame) { f.Selection.Slot.Authority[0] ^= 1 }},
		{"wrong sequencer", 2, func(f *Frame) { f.Creator.Incarnation = id(92); f.Selection.Slot.Sequencer = f.Creator }},
		{"skipped slot", 2, func(f *Frame) { f.Selection.Slot.Serial = 2 }},
		{"missing baseline", 2, func(f *Frame) { f.Selection.Baseline = record.Hash{} }},
		{"result authored by A", 3, func(f *Frame) {
			f.Creator = b.Authority.Sequencer
			f.Stream.Origin = f.Creator.Node
			f.Stream.Journal = id(0xb0)
		}},
		{"wrong result history", 3, func(f *Frame) { f.Stream.Epoch++ }},
		{"unknown kind", 3, func(f *Frame) { f.Kind = 99 }},
		{"ambiguous union", 3, func(f *Frame) { f.Binding = &b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := rewrite(t, proof.Graph, tc.index, tc.mutate)
			if _, _, err := VerifyGraph(g, 32); err == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Graph)
	}{
		{"missing bytes", func(g *Graph) { g.Frames = g.Frames[1:] }},
		{"duplicate", func(g *Graph) { g.Frames = append(g.Frames, g.Frames[0]) }},
		{"modified source bytes", func(g *Graph) { g.Frames[0][20] ^= 1 }},
		{"wrong ref hash", func(g *Graph) { g.Root.Hash[0] ^= 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := clone(proof.Graph)
			tc.mutate(&g)
			if _, _, err := VerifyGraph(g, 32); err == nil {
				t.Fatal("invalid bytes accepted")
			}
		})
	}
	if _, _, err := VerifyGraph(proof.Graph, 3); err == nil {
		t.Fatal("graph count exceeded")
	}
}

func TestVersionCanonicalAndBoundedDecoding(t *testing.T) {
	b, _, _ := fixture(t)
	data := protection(t, b, 1).Graph.Frames[0]
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{"major", func(b []byte) []byte { binary.BigEndian.PutUint16(b[8:], 2); return b }},
		{"minor", func(b []byte) []byte { binary.BigEndian.PutUint16(b[10:], 1); return b }},
		{"magic", func(b []byte) []byte { b[0] ^= 1; return b }},
		{"length", func(b []byte) []byte { binary.BigEndian.PutUint32(b[12:], 0xffffffff); return b }},
		{"trailing", func(b []byte) []byte { return append(b, 0) }},
		{"torn", func(b []byte) []byte { return b[:len(b)-1] }},
		{"whitespace", func(b []byte) []byte { body := append([]byte(" "), b[16:len(b)-32]...); return rawEnvelope(body) }},
		{"duplicate field", func(b []byte) []byte {
			body := bytes.Replace(b[16:len(b)-32], []byte(`"Sequence":1`), []byte(`"Sequence":1,"Sequence":1`), 1)
			return rawEnvelope(body)
		}},
		{"unknown field", func(b []byte) []byte {
			body := bytes.Replace(b[16:len(b)-32], []byte(`"Sequence":1`), []byte(`"Future":0,"Sequence":1`), 1)
			return rawEnvelope(body)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := tc.mutate(append([]byte(nil), data...))
			if _, err := Decode(changed); err == nil {
				t.Fatal("invalid encoding accepted")
			}
		})
	}
	if _, err := Decode(make([]byte, MaxFrame+1)); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if err := Negotiate(1, 0); err != nil {
		t.Fatal(err)
	}
	if err := Negotiate(3, 0); err == nil {
		t.Fatal("normal version accepted as public")
	}
	if _, err := record.DecodeNormal(data); err == nil {
		t.Fatal("public cast into internal normal3")
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded.Binding.Intent[0] ^= 1
	if _, err := Decode(data); err != nil {
		t.Fatal("decoder aliases source", err)
	}
}

func rawEnvelope(body []byte) []byte {
	b := make([]byte, 16)
	copy(b, "HFSPUB01")
	binary.BigEndian.PutUint16(b[8:], Major)
	binary.BigEndian.PutUint32(b[12:], uint32(len(body)))
	b = append(b, body...)
	hash := sha256.Sum256(b)
	return append(b, hash[:]...)
}

func TestExactNormalSourceBytesAndGraph(t *testing.T) {
	b, _, _ := fixture(t)
	r := record.NormalRecord{Stream: b.Origin, Sequence: 1, Creator: b.OriginLaunch, Operation: b.Operation, Payload: record.OriginOpen{Segment: id(80), MaximumBytes: 1 << 20}}
	data, err := record.EncodeNormal(r)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := record.NormalReference(data)
	if err != nil {
		t.Fatal(err)
	}
	b.Sources = []record.NormalRef{ref}
	proof := protection(t, b, 1)
	proof.Graph.NormalRecovery = &record.NormalRecovery{Authority: b.Authority, Graph: []record.RecoveryNode{{Frame: ref}}}
	proof.Graph.NormalFrames = [][]byte{data}
	if _, _, err := VerifyGraph(proof.Graph, 32); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Graph)
	}{
		{"missing bytes", func(g *Graph) { g.NormalFrames = nil }},
		{"modified bytes", func(g *Graph) { g.NormalFrames[0][32] ^= 1 }},
		{"missing graph", func(g *Graph) { g.NormalRecovery = nil }},
		{"wrong source ref", func(g *Graph) { g.NormalRecovery.Graph[0].Frame.Creator.Incarnation = id(99) }},
		{"duplicate bytes", func(g *Graph) { g.NormalFrames = append(g.NormalFrames, g.NormalFrames[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := clone(proof.Graph)
			tc.mutate(&g)
			if _, _, err := VerifyGraph(g, 32); err == nil {
				t.Fatal("normal source/ref check omitted")
			}
		})
	}
}

func TestChangedResultRequiresExactRetentionProof(t *testing.T) {
	ctx := context.Background()
	b, _, c := fixture(t)
	l := openFixture(t, filepath.Join(runDir(t), "claims"), c)
	allocate(t, l, b, 1)
	reserve(t, l, b)
	proof := protection(t, b, 1)
	proof.Graph = rewrite(t, proof.Graph, 3, func(f *Frame) { f.Result.Bytes = []byte(`{"code":"OK","count":99}`) })
	if _, _, err := VerifyGraph(proof.Graph, 32); err != nil {
		t.Fatal("byte codec should not assert operation semantics", err)
	}
	if _, err := l.Protect(ctx, b.Scope, b.Claim, proof); !errors.Is(err, ErrBlocked) {
		t.Fatal("changed result bypassed exact proof", err)
	}
	got, err := l.Resolve(ctx, b.Scope, b.Claim)
	if err != nil || got.Status != Pending {
		t.Fatal("rejected result changed state", got, err)
	}
}

func TestJournalTamperAndLimits(t *testing.T) {
	ctx := context.Background()
	b, _, c := fixture(t)
	path := filepath.Join(runDir(t), "claims")
	l := openFixture(t, path, c)
	allocate(t, l, b, 1)
	reserve(t, l, b)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := int(binary.BigEndian.Uint32(data[12:])) + 48
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"corrupt", append([]byte(nil), data...)},
		{"missing allocated gap", append([]byte(nil), data[first:]...)},
		{"duplicated entry", append(append([]byte(nil), data...), data[first:]...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "corrupt" {
				tc.data[len(tc.data)-40] ^= 1
			}
			p := filepath.Join(runDir(t), "claims")
			if err := os.WriteFile(p, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			if opened, err := Open(ctx, p, c); err == nil {
				_ = opened.Close()
				t.Fatal("tampered journal accepted")
			}
		})
	}
	c.Limits.Entries = 1
	l = openFixture(t, filepath.Join(runDir(t), "limited"), c)
	if err := l.Allocate(ctx, b.Scope, b.Claim.Anchor, allocated(b, 2), []byte("authenticated serialized allocation")); !errors.Is(err, ErrBlocked) {
		t.Fatal("entry limit ignored", err)
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.Retention = nil },
		func(c *Config) { var p *testProofs; c.Retention = p },
		func(c *Config) { c.LedgerID = record.UUID{} }, func(c *Config) { c.Admission = nil }, func(c *Config) { c.Authentication = nil }, func(c *Config) { c.Limits.GraphFrames = 0 }, func(c *Config) { c.Limits.JournalBytes = 0 }} {
		bad := c
		mutate(&bad)
		if opened, err := Open(ctx, filepath.Join(runDir(t), "invalid"), bad); err == nil {
			_ = opened.Close()
			t.Fatal("missing proof/bound defaulted")
		}
	}
}

func runDir(t testing.TB) string {
	t.Helper()
	path, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "public-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("preserved failed evidence: %s", path)
		}
	})
	return path
}
