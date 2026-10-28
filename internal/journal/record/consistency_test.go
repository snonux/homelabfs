package record

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestInstallRedoHashAgreement(t *testing.T) {
	r := recordOf[Install](t)
	valid, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Payload.(Install)
	before := p.Prepare.RedoSHA256
	p.Prepare.RedoSHA256[0] ^= 0xff
	r.Payload = p
	if _, err := Encode(r); err == nil {
		t.Fatal("encoder accepted unrelated prepared redo hash")
	}
	bad := replaceFrameBytes(t, valid, before[:], p.Prepare.RedoSHA256[:], 1)
	_, err = Decode(bad)
	requireError(t, err, Corrupt)
}
func TestPublicBeginIntentAgreement(t *testing.T) {
	original, err := DecodeOriginal(golden(t, "original_link"))
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []uint8{1, 2} {
		t.Run(map[uint8]string{1: "rename", 2: "link"}[operation], func(t *testing.T) {
			r := publicBegin(t, original, operation)
			valid, err := Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			p := r.Payload.(Begin)
			oldIntent, err := marshal(p.Binding.Intent)
			if err != nil {
				t.Fatal(err)
			}
			// Preserve the exact public document, ID and digest while substituting a
			// syntactically valid WRITE intent in the binding.
			p.Binding.Intent = fixtureWrite().Intent
			r.Payload = p
			if _, err := Encode(r); err == nil {
				t.Fatal("public request admitted different mutation kind")
			}
			newIntent, err := marshal(p.Binding.Intent)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Decode(replaceFrameBytes(t, valid, oldIntent, newIntent, 1))
			requireError(t, err, Corrupt)
		})
	}
	r := publicBegin(t, original, 1)
	valid, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Payload.(Begin)
	oldIntent, err := marshal(p.Binding.Intent)
	if err != nil {
		t.Fatal(err)
	}
	rename := p.Binding.Intent.Arguments.(Rename)
	rename.Policy = 1
	p.Binding.Intent.Arguments = rename
	r.Payload = p
	if _, err := Encode(r); err == nil {
		t.Fatal("public no-replace admitted replace intent")
	}
	newIntent, err := marshal(p.Binding.Intent)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Decode(replaceFrameBytes(t, valid, oldIntent, newIntent, 1))
	requireError(t, err, Corrupt)
}
func TestInstallCompleteDecisionRequestAgreement(t *testing.T) {
	t.Run("install", func(t *testing.T) {
		r := recordOf[Install](t)
		valid, err := Encode(r)
		if err != nil {
			t.Fatal(err)
		}
		p := r.Payload.(Install)
		before, err := marshal(p.Decision)
		if err != nil {
			t.Fatal(err)
		}
		p.Decision.ID.Request.Number++
		r.Payload = p
		if _, err := Encode(r); err == nil {
			t.Fatal("INSTALL accepted decision for another request")
		}
		after, err := marshal(p.Decision)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(replaceFrameBytes(t, valid, before, after, 1))
		requireError(t, err, Corrupt)
	})
	t.Run("complete", func(t *testing.T) {
		r := recordOf[Complete](t)
		valid, err := Encode(r)
		if err != nil {
			t.Fatal(err)
		}
		p := r.Payload.(Complete)
		before, err := marshal(p.Decision)
		if err != nil {
			t.Fatal(err)
		}
		p.Decision.ID.Request.Number++
		*p.Result.Decision = p.Decision
		r.Payload = p
		if _, err := Encode(r); err == nil {
			t.Fatal("COMPLETE accepted another request despite internally consistent result decision")
		}
		after, err := marshal(p.Decision)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Decode(replaceFrameBytes(t, valid, before, after, 2))
		requireError(t, err, Corrupt)
	})
}
func TestLocalDecisionAndPrepareIdentities(t *testing.T) {
	tests := []struct {
		name   string
		record Record
		mutate func(Record) Record
	}{
		{"install_prepare_participant", recordOf[Install](t), func(r Record) Record {
			p := r.Payload.(Install)
			p.Prepare.Participant = fixtureUUID(0x22)
			p.Prepare.Prepare.Position.Volume = p.Prepare.Participant
			r.Payload = p
			return r
		}},
		{"decision_header_history", recordOf[Decision](t), func(r Record) Record { r.Header.Configuration = fixtureUUID(0xee); return r }},
		{"decision_authority_history", recordOf[Decision](t), func(r Record) Record {
			p := r.Payload.(Decision)
			p.Authority.Generation = fixtureUUID(0xee)
			r.Payload = p
			return r
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Encode(test.mutate(test.record)); err == nil {
				t.Fatal("accepted contradictory local identities")
			}
		})
	}
	ref := fixtureDecision()
	ref.Decision.Position.Configuration = fixtureUUID(0xee)
	if _, err := marshal(ref); err == nil {
		t.Fatal("decision reference position differs from slot history")
	}
	prepare := PrepareRef{Participant: fixtureUUID(0x22), Prepare: fixtureRef()}
	if _, err := marshal(prepare); err == nil {
		t.Fatal("prepare reference participant differs from frame volume")
	}
}
func publicBegin(t testing.TB, original OriginalRequest, operation uint8) Record {
	t.Helper()
	r := recordOf[Begin](t)
	p := r.Payload.(Begin)
	original.Operation = operation
	p.Document = RequestDocument{Class: 1, Original: &original}
	digest, err := documentDigest(p.Document)
	if err != nil {
		t.Fatal(err)
	}
	p.Binding.RequestDigest = digest
	chosen := fixtureWrite().Intent.SelectedMetadata
	if operation == 1 {
		p.Binding.Intent = Intent{Kind: 7, Arguments: Rename{SourceParent: fixtureParent(), SourceName: "a\xff", Source: fixtureObject(), DestinationParent: fixtureParent(), DestinationName: "b", Policy: original.Replacement}, SelectedMetadata: chosen}
	} else {
		p.Binding.Intent = Intent{Kind: 4, Arguments: Link{Source: fixtureObject(), TargetParent: fixtureParent(), TargetName: "b"}, SelectedMetadata: chosen}
	}
	r.Payload = p
	return r
}
func recordOf[T any](t testing.TB) Record {
	t.Helper()
	for _, r := range recordFixtures(t) {
		if _, ok := r.Payload.(T); ok {
			return r
		}
	}
	t.Fatal("missing record fixture")
	return Record{}
}
func replaceFrameBytes(t testing.TB, frame, before, after []byte, count int) []byte {
	t.Helper()
	payload := frame[144 : len(frame)-4]
	if bytes.Count(payload, before) != count {
		t.Fatalf("expected %d occurrences of replaced field", count)
	}
	mutated := bytes.ReplaceAll(payload, before, after)
	out := append(bytes.Clone(frame[:144]), mutated...)
	out = append(out, make([]byte, 4)...)
	binary.BigEndian.PutUint32(out[20:], uint32(len(out)))
	binary.BigEndian.PutUint32(out[24:], uint32(len(mutated)))
	fixCRC(out)
	return out
}
