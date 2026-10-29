package record

import (
	"bytes"
	"reflect"
	"testing"
)

// This test-only type preserves the result layout while allowing malformed
// terminal results to be placed in CRC-valid frames for decoder checks.
type uncheckedTerminalResult TerminalResult

func TestAbortedCompleteCleanupRoundTrip(t *testing.T) {
	r := abortedComplete(t)
	want := r.Payload.(Complete)
	raw, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Payload.(Complete)
	if !reflect.DeepEqual(got.Installs, want.Installs) || !reflect.DeepEqual(got.Installed, want.Installed) {
		t.Fatal("abort lost exact participant cleanup references or positions")
	}
	if got.Result.Status != 2 || got.Result.Code != want.Result.Code || got.Result.Decision == nil || *got.Result.Decision != got.Decision {
		t.Fatal("abort lost its terminal result or decision")
	}
	if len(got.Result.Committed) != 0 || got.Result.Visibility != nil || got.Visibility != want.Visibility {
		t.Fatal("abort invented committed positions or changed visibility")
	}
	back, err := Encode(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, back) {
		t.Fatal("noncanonical abort cleanup roundtrip")
	}
}

func TestCompleteResultAgreement(t *testing.T) {
	tests := []struct {
		name    string
		aborted bool
		mutate  func(*TerminalResult)
	}{
		{"commit_positions", false, func(p *TerminalResult) { p.Committed[0].Sequence++ }},
		{"commit_visibility", false, func(p *TerminalResult) { p.Visibility.Generation++ }},
		{"abort_committed_positions", true, func(p *TerminalResult) { p.Committed = Vector{fixturePosition()} }},
		{"abort_result_visibility", true, func(p *TerminalResult) { p.Visibility = ptr(fixtureVisibility()) }},
		{"abort_decision", true, func(p *TerminalResult) { p.Decision.ID.Term++ }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := recordOf[Complete](t)
			if test.aborted {
				r = abortedComplete(t)
			}
			raw, err := Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			p := r.Payload.(Complete)
			before, err := marshal(p.Result)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&p.Result)
			r.Payload = p
			_, err = Encode(r)
			requireError(t, err, Corrupt)
			after, err := marshal(uncheckedTerminalResult(p.Result))
			if err != nil {
				t.Fatal(err)
			}
			_, err = Decode(replaceFrameBytes(t, raw, before, after, 1))
			requireError(t, err, Corrupt)
		})
	}
}

func abortedComplete(t testing.TB) Record {
	t.Helper()
	r := recordOf[Complete](t)
	p := r.Payload.(Complete)
	p.Installs = nil
	p.Installed = nil
	for i, participant := range fixtureBinding().Participants {
		ref := fixtureRef()
		ref.Position.Volume = participant
		ref.Position.Sequence = uint64(i + 4)
		p.Installs = append(p.Installs, ref)
		p.Installed = append(p.Installed, ref.Position)
	}
	p.Result.Status = 2
	p.Result.Code = 1
	p.Result.Committed = nil
	p.Result.Visibility = nil
	p.CacheBarriers = nil
	r.Payload = p
	return r
}
