package record

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"sort"
	"testing"
)

func normalFixture(t testing.TB) NormalGrant {
	t.Helper()
	a := NormalAuthority{Cluster: fixtureUUID(1), Registry: fixtureUUID(2), RegistryRevision: 1, RecoveryGeneration: fixtureUUID(3), Closure: fixtureUUID(4), ManifestVersion: 1, Admitted: []UUID{fixtureUUID(0x44), fixtureUUID(0x66), fixtureUUID(0x88), fixtureUUID(0xaa)}, Volumes: []NormalVolume{{Volume: fixtureUUID(0x11), Generation: fixtureUUID(0x22), Configuration: fixtureUUID(0x33), Epoch: 7, Replicas: []UUID{fixtureUUID(0x44), fixtureUUID(0x66), fixtureUUID(0x88)}}}, Sequencer: Launch{fixtureUUID(0x44), fixtureUUID(0x55)}, OwnerPartner: Launch{fixtureUUID(0x66), fixtureUUID(0x77)}, FenceLineage: []Hash{sha256.Sum256([]byte("positive fence lineage"))}}
	h, err := NormalAuthorityHash(a)
	if err != nil {
		t.Fatal(err)
	}
	m := fixtureWrite()
	m.Dependencies = nil
	return NormalGrant{Authority: a, Slot: OrderedSlot{h, a.Sequencer, 1}, Baseline: sha256.Sum256([]byte("verified baseline")), Origin: OriginStream{a.Cluster, fixtureUUID(0x11), fixtureUUID(0x22), fixtureUUID(0x33), 7, fixtureUUID(0xaa), fixtureUUID(0xab)}, OriginLaunch: Launch{fixtureUUID(0xaa), fixtureUUID(0xac)}, Recipient: a.Sequencer, Operation: fixtureUUID(0xad), Document: fixtureDocument(), Credentials: m.Authorizations[0].Credentials, Mutation: m, Result: GrantResult{AcceptedBytes: 3}, NoEffectResult: GrantResult{Code: 1}}
}

func normalRecord(g NormalGrant, origin bool, seq uint64, payload any) NormalRecord {
	s, creator := g.Origin, g.OriginLaunch
	if !origin {
		s.Origin, s.Journal, creator = g.Authority.Sequencer.Node, fixtureUUID(0xb0), g.Authority.Sequencer
	}
	return NormalRecord{Stream: s, Sequence: seq, Operation: g.Operation, Creator: creator, Payload: payload}
}

func TestNormalAuthorityRolesAndLegacyPreservation(t *testing.T) {
	g := normalFixture(t)
	for _, test := range []struct {
		name   string
		mutate func(*NormalGrant)
		valid  bool
	}{
		{"outside placement origin", func(*NormalGrant) {}, true},
		{"remote origin does not require B", func(g *NormalGrant) {
			g.Authority.OwnerPartner.Incarnation = fixtureUUID(0xfe)
			g.Slot.Authority, _ = NormalAuthorityHash(g.Authority)
		}, true},
		{"owner requires separate B", func(g *NormalGrant) {
			g.Origin.Origin = g.Authority.Sequencer.Node
			g.OriginLaunch = g.Authority.Sequencer
			g.Recipient = g.Authority.OwnerPartner
		}, true},
		{"remote cannot count B", func(g *NormalGrant) { g.Recipient = g.Authority.OwnerPartner }, false},
		{"self acceptance", func(g *NormalGrant) { g.Recipient = g.OriginLaunch }, false},
		{"unadmitted", func(g *NormalGrant) { g.Origin.Origin = fixtureUUID(0xff); g.OriginLaunch.Node = g.Origin.Origin }, false},
		{"grant hash conflict", func(g *NormalGrant) { g.Slot.Authority[0] ^= 1 }, false},
		{"predecessor missing", func(g *NormalGrant) { g.Slot.Serial = 2 }, false},
		{"changed credentials", func(g *NormalGrant) { g.Credentials.UID++ }, false},
		{"changed selected redo", func(g *NormalGrant) { g.Mutation.Redo.Objects[0].After.UID++ }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := normalFixture(t)
			test.mutate(&g)
			_, err := EncodeNormal(normalRecord(g, false, 1, g))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
	data, err := EncodeNormal(normalRecord(g, false, 1, g))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err == nil {
		t.Fatal("legacy decoder accepted normal3")
	}
	if _, err := DecodeNormal(golden(t, "open")); err == nil {
		t.Fatal("normal3 reinterpreted legacy frame")
	}
	// Existing literal v1 vectors remain parsed and byte-identical.
	for _, name := range []string{"open", "seal", "write_candidate"} {
		old := golden(t, name)
		r, err := Decode(old)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := Encode(r)
		if err != nil || !bytes.Equal(old, encoded) {
			t.Fatalf("legacy %s changed: %v", name, err)
		}
	}
}

func TestNormalCompleteCreatorGraphAndUnknownResults(t *testing.T) {
	g := normalFixture(t)
	frames := map[NormalRef][]byte{}
	nodes := []RecoveryNode{}
	add := func(r NormalRecord, dependencies ...NormalRef) NormalRef {
		b, err := EncodeNormal(r)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := NormalReference(b)
		if err != nil {
			t.Fatal(err)
		}
		frames[ref] = b
		sort.Slice(dependencies, func(i, j int) bool {
			x, _ := sortKey(dependencies[i])
			y, _ := sortKey(dependencies[j])
			return bytes.Compare(x, y) < 0
		})
		nodes = append(nodes, RecoveryNode{ref, dependencies})
		return ref
	}
	grant := add(normalRecord(g, false, 1, g))
	fragment := add(normalRecord(g, true, 1, OriginFragment{frames[grant]}), grant)
	prepared := add(normalRecord(g, false, 2, MemoryAcceptance{Grant: grant, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 1, Target: grant, Dependencies: []NormalRef{grant}}), grant)
	choice := add(normalRecord(g, true, 2, OriginChoice{Grant: grant, Slot: g.Slot, Choice: 1, Prepared: &prepared, Fragment: []NormalRef{fragment}}), grant, fragment, prepared)
	accepted := add(normalRecord(g, false, 3, MemoryAcceptance{Grant: grant, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 2, Target: choice, DurableThrough: 2, Dependencies: []NormalRef{choice}}), grant, choice)
	selected := add(normalRecord(g, false, 4, SlotSelection{Grant: grant, Slot: g.Slot, Outcome: 1, Choice: choice, Acceptance: accepted, Volumes: []UUID{g.Origin.Volume}}), grant, choice, accepted)
	cut := SelectedRef{g.Slot, selected}
	result := add(normalRecord(g, true, 3, NormalResult{Grant: grant, Selected: cut, Value: g.Result}), grant, selected)
	sort.Slice(nodes, func(i, j int) bool {
		x, _ := sortKey(nodes[i])
		y, _ := sortKey(nodes[j])
		return bytes.Compare(x, y) < 0
	})
	recovery := NormalRecovery{Authority: g.Authority, IssuedThrough: 1, Selected: &cut, Graph: nodes, Results: []ResultObservation{{Grant: grant, State: 2}}}
	if err := VerifyNormalGraph(recovery, frames); err != nil {
		t.Fatal(err)
	}
	// Missing durable result is still an explicit unknown result beside COMMIT.
	delete(frames, result)
	if err := VerifyNormalGraph(recovery, frames); err == nil {
		t.Fatal("missing creator bytes accepted")
	}
	recovery.Graph = append([]RecoveryNode(nil), recovery.Graph...)
	for i, n := range recovery.Graph {
		if n.Frame == result {
			recovery.Graph = append(recovery.Graph[:i], recovery.Graph[i+1:]...)
			break
		}
	}
	if err := VerifyNormalGraph(recovery, frames); err != nil {
		t.Fatalf("COMMIT became absent when result unknown: %v", err)
	}
	for i, n := range recovery.Graph {
		if n.Frame == choice {
			recovery.Graph[i].Dependencies = []NormalRef{grant}
			break
		}
	}
	if err := VerifyNormalGraph(recovery, frames); err == nil {
		t.Fatal("omitted prepared/fragment dependencies accepted")
	}
}

func TestNormalBoundedMalformedFrames(t *testing.T) {
	g := normalFixture(t)
	b, err := EncodeNormal(normalRecord(g, true, 1, OriginOpen{Segment: fixtureUUID(1), MaximumBytes: 1024}))
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < len(b); n++ {
		if _, err := DecodeNormal(b[:n]); err == nil {
			t.Fatalf("truncation %d accepted", n)
		}
	}
	for _, test := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"emergency major", func(b []byte) { binary.BigEndian.PutUint16(b[8:], 2) }},
		{"unknown minor", func(b []byte) { binary.BigEndian.PutUint16(b[10:], 1) }},
		{"unknown kind", func(b []byte) { binary.BigEndian.PutUint16(b[12:], 99) }},
		{"reserved", func(b []byte) { b[15] = 1 }},
		{"oversize", func(b []byte) { binary.BigEndian.PutUint32(b[16:], MaxFrame+1) }},
		{"zero creator", func(b []byte) { clear(b[24+128 : 24+144]) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := bytes.Clone(b)
			test.mutate(bad)
			binary.BigEndian.PutUint32(bad[len(bad)-4:], crc32.Checksum(bad[:len(bad)-4], crc32.MakeTable(crc32.Castagnoli)))
			if _, err := DecodeNormal(bad); err == nil {
				t.Fatal("valid-CRC malformed bytes accepted")
			}
		})
	}
	bad := bytes.Clone(b)
	bad[200] ^= 1
	if _, err := DecodeNormal(bad); err == nil {
		t.Fatal("bad CRC accepted")
	}
	if _, err := DecodeNormal(append(b, 0)); err == nil {
		t.Fatal("trailing byte accepted")
	}
}

func TestNormalIndependentLiteralOpen(t *testing.T) {
	b := golden(t, "normal3_open")
	h := sha256.Sum256(b)
	if len(b) != 225 || hex.EncodeToString(h[:]) != "fda4d0f976044fa244467057cd6f40b1cb2bdf4607ebb8a7c77374a46b01777d" {
		t.Fatal("normal3 literal changed")
	}
	s := OriginStream{fixtureUUID(1), fixtureUUID(2), fixtureUUID(3), fixtureUUID(4), 7, fixtureUUID(5), fixtureUUID(6)}
	r := NormalRecord{Stream: s, Sequence: 1, Operation: fixtureUUID(7), Creator: Launch{fixtureUUID(5), fixtureUUID(8)}, Observed: Time{Seconds: -1, Nanoseconds: 123456789}, Payload: OriginOpen{Segment: fixtureUUID(9), MaximumBytes: 1024}}
	got, err := EncodeNormal(r)
	if err != nil || !bytes.Equal(b, got) {
		t.Fatalf("independent literal disagrees: %v", err)
	}
	back, err := DecodeNormal(b)
	if err != nil {
		t.Fatal(err)
	}
	got, err = EncodeNormal(back)
	if err != nil || !bytes.Equal(b, got) {
		t.Fatalf("literal roundtrip changed: %v", err)
	}
}

func TestNormalRejectsUnsupportedWorkAndAmbiguousStates(t *testing.T) {
	g := normalFixture(t)
	b, err := EncodeNormal(normalRecord(g, false, 1, g))
	if err != nil {
		t.Fatal(err)
	}
	grant, err := NormalReference(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		payload any
	}{
		{"commit without prepared", OriginChoice{Grant: grant, Slot: g.Slot, Choice: 1, Fragment: []NormalRef{grant}}},
		{"decline with prepared", OriginChoice{Grant: grant, Slot: g.Slot, Choice: 2, Prepared: &grant, Fragment: []NormalRef{grant}}},
		{"terminal without result", ResultObservation{Grant: grant, State: 3}},
		{"durable past received", NormalFrontier{Stream: g.Origin, Received: 1, Durable: 2}},
		{"unbounded dependency count", OriginChoice{Grant: grant, Slot: g.Slot, Choice: 2, Fragment: make([]NormalRef, 4097)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := marshal(test.payload); err == nil {
				t.Fatal("ambiguous/malformed state accepted")
			}
		})
	}
	g = normalFixture(t)
	g.Document = RequestDocument{Class: 1, Original: &OriginalRequest{Version: 1, ID: fixtureRequestID(), Operation: 1, Source: Endpoint{Volume: g.Origin.Volume, View: fixtureUUID(1), Components: []string{"a"}}, Destination: Endpoint{Volume: g.Origin.Volume, View: fixtureUUID(1), Components: []string{"b"}}, Replacement: 1}}
	if _, err := EncodeNormal(normalRecord(g, false, 1, g)); err == nil {
		t.Fatal("public transaction doc became ordinary grant")
	}
	g = normalFixture(t)
	g.Mutation.Intent.Arguments = Write{Object: ObjectID{Home: fixtureUUID(0xff), Allocation: fixtureObject().Allocation}, Data: []byte{1}}
	if _, err := EncodeNormal(normalRecord(g, false, 1, g)); err == nil {
		t.Fatal("foreign-home operation admitted")
	}
	recovery := NormalRecovery{Authority: normalFixture(t).Authority, Graph: []RecoveryNode{{Frame: grant, Dependencies: []NormalRef{grant}}}}
	if _, err := marshal(recovery); err == nil {
		t.Fatal("source graph cycle accepted")
	}
}

func TestNormalDurableDeclineHasOneNoEffectResult(t *testing.T) {
	for _, outcome := range []uint8{1, 2} {
		t.Run(string(rune('0'+outcome)), func(t *testing.T) {
			g := normalFixture(t)
			frames := map[NormalRef][]byte{}
			nodes := []RecoveryNode{}
			add := func(r NormalRecord, deps ...NormalRef) NormalRef {
				b, err := EncodeNormal(r)
				if err != nil {
					t.Fatal(err)
				}
				ref, err := NormalReference(b)
				if err != nil {
					t.Fatal(err)
				}
				frames[ref] = b
				sort.Slice(deps, func(i, j int) bool {
					x, _ := sortKey(deps[i])
					y, _ := sortKey(deps[j])
					return bytes.Compare(x, y) < 0
				})
				nodes = append(nodes, RecoveryNode{ref, deps})
				return ref
			}
			grant := add(normalRecord(g, false, 1, g))
			fragment := add(normalRecord(g, true, 1, OriginFragment{frames[grant]}), grant)
			decline := add(normalRecord(g, true, 2, OriginChoice{Grant: grant, Slot: g.Slot, Choice: 2, Fragment: []NormalRef{fragment}}), grant, fragment)
			acceptance := add(normalRecord(g, false, 2, MemoryAcceptance{Grant: grant, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 2, Target: decline, DurableThrough: 2, Dependencies: []NormalRef{decline}}), grant, decline)
			selected := add(normalRecord(g, false, 3, SlotSelection{Grant: grant, Slot: g.Slot, Outcome: outcome, Choice: decline, Acceptance: acceptance, Volumes: []UUID{g.Origin.Volume}}), grant, decline, acceptance)
			cut := SelectedRef{g.Slot, selected}
			add(normalRecord(g, true, 3, NormalResult{Grant: grant, Selected: cut, Value: g.NoEffectResult}), grant, selected)
			sort.Slice(nodes, func(i, j int) bool {
				x, _ := sortKey(nodes[i])
				y, _ := sortKey(nodes[j])
				return bytes.Compare(x, y) < 0
			})
			err := VerifyNormalGraph(NormalRecovery{Authority: g.Authority, IssuedThrough: 1, Selected: &cut, Graph: nodes}, frames)
			if (err == nil) != (outcome == 2) {
				t.Fatalf("DECLINE outcome=%d error=%v", outcome, err)
			}
		})
	}
}

func TestNormalRecoveryReportAndBarrierCodec(t *testing.T) {
	g := normalFixture(t)
	b, err := EncodeNormal(normalRecord(g, false, 1, g))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := NormalReference(b)
	if err != nil {
		t.Fatal(err)
	}
	promise := TransitionPromise{Transition: fixtureUUID(0xe0), PredecessorAuthority: g.Slot.Authority, RegistryRevision: 2, Epoch: 8, Term: 2, Exclusions: []NormalRef{ref}}
	report := OriginCloseReport{Origin: g.OriginLaunch, Reporter: g.OriginLaunch, Promise: promise, State: 2, IssuedThrough: 1}
	coverage := SourceCoverage{Stream: ref.Stream, Holder: g.OriginLaunch, Through: ref.Sequence, Last: ref}
	selection := ref
	selection.Kind = NormalSelectionKind
	cut := SelectedRef{g.Slot, selection}
	for _, payload := range []any{
		promise, report,
		NormalBarrier{Authority: g.Authority, Origin: g.Origin, Recipient: g.Recipient, Cuts: []NormalCut{{g.Origin.Volume, cut}}, Sources: []SourceCoverage{coverage}},
		NormalFrontier{Stream: g.Origin, Received: 1, Durable: 1},
		NormalRecovery{Authority: g.Authority, Predecessors: []NormalAuthority{g.Authority}},
	} {
		r := normalRecord(g, true, 1, payload)
		b, err := EncodeNormal(r)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeNormal(b)
		if err != nil {
			t.Fatal(err)
		}
		back, err := EncodeNormal(decoded)
		if err != nil || !bytes.Equal(b, back) {
			t.Fatal("normal positive schema roundtrip", err)
		}
	}
	report.State = 1
	report.Continuity = true
	if _, err := marshal(report); err == nil {
		t.Fatal("unknown report pretended continuity")
	}
	report.State = 2
	report.Continuity = false
	report.Unresolved = []NormalRef{ref}
	if _, err := marshal(report); err == nil {
		t.Fatal("sealed empty report hid issued grant")
	}
	object, err := EncodeNormalAuthority(g.Authority)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := DecodeNormalAuthority(object)
	if err != nil || !equalEncoding(authority, g.Authority) {
		t.Fatal("authority object changed", err)
	}
}
