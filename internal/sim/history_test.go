package sim

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

func oracleBaseline() map[string]FileState {
	return map[string]FileState{".": {Kind: fsapi.Directory, Mode: 0o775, UID: 1000, GID: 1000, Nlink: 2}}
}

func oracleHistory(t *testing.T, uncertain Outcome) (*History, []AppliedEffect) {
	t.Helper()
	h := NewHistory()
	intents := []Intent{{Kind: CreateFile, Name: "a", Mode: 0o640, UID: 1000, GID: 1000}, {Kind: AppendFile, Name: "a", Data: []byte("payload")}}
	var effects []AppliedEffect
	for i, intent := range intents {
		id := []string{"create", "append"}[i]
		if err := h.Invoke(id, UUID(1, "volume"), "client", time.Duration(i), intent); err != nil {
			t.Fatal(err)
		}
		outcome := Acknowledged
		if i == 1 {
			outcome = uncertain
		}
		if err := h.Complete(id, time.Duration(i), outcome, "client boundary"); err != nil {
			t.Fatal(err)
		}
		source := oracleObservation(nil, "").Through
		source.Position.Sequence = uint64(2 + i)
		source.SHA256 = record.Hash(sha256.Sum256([]byte(id)))
		effect := AppliedEffect{ID: id, Operation: UUID(1, id), Source: source, Intent: intent}
		if intent.Kind == AppendFile {
			offset := uint64(0)
			effect.WriteOffset = &offset
		}
		effects = append(effects, effect)
	}
	return h, effects
}

func oracleObservation(effects []AppliedEffect, payload string) Observation {
	return Observation{Replica: "a", Through: record.FrameRef{StreamKind: 1, Position: record.Position{Volume: UUID(1, "volume"), Generation: UUID(1, "generation"), Configuration: UUID(1, "configuration"), Epoch: 1, Sequence: 10}, SHA256: record.Hash(sha256.Sum256([]byte("exact-frame")))}, Effects: effects, Files: map[string]FileState{".": oracleBaseline()["."], "a": {Kind: fsapi.Regular, Mode: 0o640, UID: 1000, GID: 1000, Nlink: 1, Size: int64(len(payload)), Data: []byte(payload)}}}
}

func TestHistoryCheckerHandBuilt(t *testing.T) {
	tests := []struct {
		name    string
		outcome Outcome
		change  func(*Observation)
		want    string
	}{
		{"ack retained", Acknowledged, func(*Observation) {}, ""},
		{"missing append offset", Acknowledged, func(o *Observation) { o.Effects[1].WriteOffset = nil }, "missing selected append offset"},
		{"nonzero append offset on empty file", Acknowledged, func(o *Observation) { offset := uint64(1); o.Effects[1].WriteOffset = &offset }, "differs from independent length 0"},
		{"uncertain retained", Uncertain, func(*Observation) {}, ""},
		{"uncertain absent", Uncertain, func(o *Observation) {
			o.Effects = o.Effects[:1]
			f := o.Files["a"]
			f.Data, f.Size = nil, 0
			o.Files["a"] = f
		}, ""},
		{"ack missing even at earlier cut", Acknowledged, func(o *Observation) {
			o.Effects = o.Effects[:1]
			o.Through.Position.Sequence--
			f := o.Files["a"]
			f.Data, f.Size = nil, 0
			o.Files["a"] = f
		}, "acknowledged effect lost"},
		{"duplicate effect", Acknowledged, func(o *Observation) { o.Effects = append(o.Effects, o.Effects[1]) }, "duplicate"},
		{"reordered effect", Acknowledged, func(o *Observation) { o.Effects[0], o.Effects[1] = o.Effects[1], o.Effects[0] }, "unordered recovered source"},
		{"invented effect", Acknowledged, func(o *Observation) { o.Effects[1].ID = "not-invoked" }, "uninvoked"},
		{"changed payload", Acknowledged, func(o *Observation) { o.Effects[1].Intent.Data = []byte("changed") }, "changed recovered intent"},
		{"double append bytes", Acknowledged, func(o *Observation) {
			f := o.Files["a"]
			f.Data = []byte("payloadpayload")
			f.Size = int64(len(f.Data))
			o.Files["a"] = f
		}, "data or metadata"},
		{"metadata wrong", Acknowledged, func(o *Observation) { f := o.Files["a"]; f.Mode = 0o777; o.Files["a"] = f }, "data or metadata"},
		{"unknown namespace entry", Acknowledged, func(o *Observation) { o.Files["extra"] = o.Files["a"] }, "namespace differs"},
		{"root metadata wrong", Acknowledged, func(o *Observation) { f := o.Files["."]; f.GID++; o.Files["."] = f }, "data or metadata"},
		{"rejected applied", Rejected, func(*Observation) {}, "rejected"},
		{"missing cut hash", Acknowledged, func(o *Observation) { o.Through.SHA256 = record.Hash{} }, "missing exact"},
		{"last effect is exact cut", Acknowledged, func(o *Observation) { o.Through = o.Effects[1].Source }, ""},
		{"last effect conflicts with cut hash", Acknowledged, func(o *Observation) { o.Through.Position.Sequence = o.Effects[1].Source.Position.Sequence }, "disagrees with exact cut"},
		{"effect beyond cut", Acknowledged, func(o *Observation) { o.Effects[1].Source.Position.Sequence = 11 }, "invalid"},
		{"effect foreign generation", Acknowledged, func(o *Observation) { o.Effects[1].Source.Position.Generation = UUID(2, "generation") }, "invalid"},
		{"same journal operation twice", Acknowledged, func(o *Observation) { o.Effects[1].Operation = o.Effects[0].Operation }, "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, effects := oracleHistory(t, tt.outcome)
			o := oracleObservation(effects, "payload")
			tt.change(&o)
			err := CheckHistory(UUID(1, "volume"), oracleBaseline(), h.Entries(), []Observation{o})
			if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}

func TestHistoryCheckerCommonCutAndSingleHistory(t *testing.T) {
	h, effects := oracleHistory(t, Acknowledged)
	for _, kind := range []string{"same", "hash", "sequence", "generation", "order", "offset", "both wrong"} {
		t.Run(kind, func(t *testing.T) {
			a, b := oracleObservation(effects, "payload"), oracleObservation(append([]AppliedEffect(nil), effects...), "payload")
			b.Replica = "b"
			switch kind {
			case "hash":
				b.Through.SHA256[0]++
			case "sequence":
				b.Through.Position.Sequence++
			case "generation":
				b.Through.Position.Generation = UUID(2, "generation")
			case "order":
				b.Effects[0], b.Effects[1] = b.Effects[1], b.Effects[0]
			case "offset":
				offset := uint64(1)
				b.Effects[1].WriteOffset = &offset
			case "both wrong":
				a.Files["a"] = FileState{}
				b.Files["a"] = FileState{}
			}
			err := CheckHistory(UUID(1, "volume"), oracleBaseline(), h.Entries(), []Observation{a, b})
			if (err == nil) != (kind == "same") {
				t.Fatal(err)
			}
		})
	}
}

func TestHistoryEmptyRecoveredHistory(t *testing.T) {
	a, b := oracleObservation(nil, ""), oracleObservation([]AppliedEffect{}, "")
	a.Files, b.Files = oracleBaseline(), oracleBaseline()
	b.Replica = "b"
	if err := CheckHistory(UUID(1, "volume"), oracleBaseline(), nil, []Observation{a, b}); err != nil {
		t.Fatal(err)
	}
}

func TestHistoryRecorderOwnsInputAndRejectsInvalidOrder(t *testing.T) {
	h, effects := oracleHistory(t, Uncertain)
	e := h.Entries()
	e[1].Intent.Data[0] = 'X'
	if string(h.Entries()[1].Intent.Data) != "payload" {
		t.Fatal("borrowed evidence")
	}
	if err := h.Complete("append", 3, Acknowledged, "late reply"); err == nil {
		t.Fatal("reclassified timed-out reply")
	}
	if err := h.Invoke("append", UUID(1, "volume"), "client", 4, effects[1].Intent); err == nil {
		t.Fatal("duplicate operation admitted")
	}
	if err := h.Invoke("pending", UUID(1, "volume"), "client", 4, effects[1].Intent); err != nil {
		t.Fatal(err)
	}
	if err := h.Invoke("overlap", UUID(1, "volume"), "client", 4, effects[1].Intent); err == nil {
		t.Fatal("concurrent per-volume history admitted")
	}
	o := oracleObservation(effects, "payload")
	if err := CheckHistory(UUID(1, "volume"), oracleBaseline(), h.Entries(), []Observation{o}); err != nil {
		t.Fatal(err)
	}
	e = h.Entries()
	e[0].PayloadSHA256[0]++
	if err := CheckHistory(UUID(1, "volume"), oracleBaseline(), e, []Observation{o}); err == nil {
		t.Fatal("corrupt external checksum accepted")
	}
}

func TestHistoryFileLifecycleModel(t *testing.T) {
	h, effects := oracleHistory(t, Acknowledged)
	steps := []struct {
		id     string
		intent Intent
		files  map[string]FileState
	}{
		{"chmod", Intent{Kind: ChmodFile, Name: "a", Mode: 0o600}, map[string]FileState{".": oracleBaseline()["."], "a": {Kind: fsapi.Regular, Mode: 0o600, UID: 1000, GID: 1000, Nlink: 1, Size: 7, Data: []byte("payload")}}},
		{"rename", Intent{Kind: RenameFile, Name: "a", Destination: "b"}, map[string]FileState{".": oracleBaseline()["."], "b": {Kind: fsapi.Regular, Mode: 0o600, UID: 1000, GID: 1000, Nlink: 1, Size: 7, Data: []byte("payload")}}},
		{"unlink", Intent{Kind: RemoveFile, Name: "b"}, oracleBaseline()},
	}
	for i, step := range steps {
		if err := h.Invoke(step.id, UUID(1, "volume"), "other-client", time.Duration(i+2), step.intent); err != nil {
			t.Fatal(err)
		}
		if err := h.Complete(step.id, time.Duration(i+2), Acknowledged, "OK"); err != nil {
			t.Fatal(err)
		}
		o := oracleObservation(nil, "")
		source := o.Through
		source.Position.Sequence = uint64(4 + i)
		source.SHA256 = record.Hash(sha256.Sum256([]byte(step.id)))
		effects = append(effects, AppliedEffect{ID: step.id, Operation: UUID(1, step.id), Source: source, Intent: step.intent})
		o.Effects, o.Files = effects, step.files
		if err := CheckHistory(UUID(1, "volume"), oracleBaseline(), h.Entries(), []Observation{o}); err != nil {
			t.Fatal(step.id, err)
		}
	}
}

func TestHistoryCheckerRejectsMalformedEvidence(t *testing.T) {
	for _, kind := range []string{"checksum", "overlap", "time", "pending ack", "duplicate ID", "oversized payload"} {
		t.Run(kind, func(t *testing.T) {
			h, effects := oracleHistory(t, Acknowledged)
			entries := h.Entries()
			switch kind {
			case "checksum":
				entries[1].PayloadSHA256[0]++
			case "overlap":
				entries[1].Invoked = entries[0].Completed
			case "time":
				entries[0].CompletedAt = 3
			case "pending ack":
				entries[1].Completed = 0
			case "duplicate ID":
				entries[1].ID = entries[0].ID
			case "oversized payload":
				entries[1].Intent.Data = make([]byte, 4097)
				entries[1].PayloadSHA256 = sha256.Sum256(entries[1].Intent.Data)
			}
			if err := CheckHistory(UUID(1, "volume"), oracleBaseline(), entries, []Observation{oracleObservation(effects, "payload")}); err == nil {
				t.Fatal("malformed evidence passed")
			}
		})
	}
}

func TestScenarioSeedReplayAndBoundedMinimization(t *testing.T) {
	a, err := GenerateScenario(42, 12)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateScenario(42, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("seed did not replay")
	}
	changed, err := GenerateScenario(43, 12)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a, changed) {
		t.Fatal("different seeds generated same script")
	}
	target := a.Actions[7].ID
	mini, attempts, err := MinimizeScenario(t.Context(), a, 32, func(_ context.Context, s Scenario) (bool, error) {
		for _, action := range s.Actions {
			if action.ID == target {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil || attempts > 32 || len(mini.Actions) != 1 || mini.Actions[0].ID != target {
		t.Fatal(mini, attempts, err)
	}
	_, attempts, err = MinimizeScenario(t.Context(), a, 1, func(context.Context, Scenario) (bool, error) { return false, nil })
	if err != nil || attempts != 1 {
		t.Fatal(attempts, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = MinimizeScenario(ctx, a, 32, func(context.Context, Scenario) (bool, error) { t.Fatal("ran after cancellation"); return false, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
