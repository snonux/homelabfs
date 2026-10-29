package sim

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/transactions"
)

func generateTransactionScript(seed uint64, count int) (transactionScript, error) {
	script := transactionScript{Seed: seed}
	if count < 1 || count > 4 {
		return script, transactions.ErrLimit
	}
	random := NewRandom(seed, "transaction-scenario/v1")
	model := transactionBaseline()
	// Choose original caller paths against an independent workload model. Neither
	// engine output nor replica contents determines future expected input.
	for i := range count {
		var b [8]byte
		_, _ = random.Read(b[:])
		names := make([]string, 0)
		for name, id := range model.Names {
			if model.Objects[id].Type == 1 {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		selected := names[int(b[0])%len(names)]
		slash := 36 // UUID.String() has 36 bytes.
		volume, err := recordUUID(selected[:slash])
		if err != nil {
			return script, err
		}
		input := TransactionInput{ID: transactionScenarioID(uint64(i + 1)), SourceVolume: volume, Source: selected[slash+1:]}
		switch b[1] % 4 {
		case 0:
			input.Kind = "link"
		case 1:
			input.Kind = "rename"
		case 2:
			input.Kind = "write"
			input.Data = []byte(fmt.Sprintf("seed=%d action=%d bytes=%x", seed, i, b))
		case 3:
			if len(names) > 1 {
				input.Kind = "unlink"
			} else {
				input.Kind = "write"
				input.Data = []byte{b[4], b[5]}
			}
		}
		if input.Kind == "link" || input.Kind == "rename" {
			input.DestinationVolume = txUUID(3 + b[2]%2)
			input.Destination = fmt.Sprintf("seed-%d-%d", seed, i)
		}
		faults := []string{"none", "reply", "receipt", "barrier", "crash"}
		script.Actions = append(script.Actions, transactionAction{Input: input, Fault: faults[b[3]%5], Boundary: int(b[4] % 14)})
		if _, err := replayTransaction(&model, input); err != nil {
			return script, err
		}
	}
	return script, nil
}
func transactionScenarioID(number uint64) record.RequestID {
	return record.RequestID{Session: recordSession(), Number: number}
}
func recordSession() fsapi.SessionID {
	return fsapi.SessionID{Principal: record.Principal{Realm: txUUID(30), Subject: "synthetic-user"}, Anchor: txUUID(3), Nonce: txUUID(31)}
}

func recordUUID(s string) (record.UUID, error) { return fsapi.ParseUUID(s) }

func minimizeTransactionScript(ctx context.Context, original transactionScript, budget int, reproduce func(transactionScript) (bool, error)) (transactionScript, int, error) {
	best := original
	best.Actions = slices.Clone(original.Actions)
	if len(best.Actions) > 4 || budget < 1 || budget > 32 || reproduce == nil {
		return best, 0, transactions.ErrLimit
	}
	attempts := 0
	for width := len(best.Actions); width >= 1; width /= 2 {
		for start := 0; start+width <= len(best.Actions) && attempts < budget; {
			if err := ctx.Err(); err != nil {
				return best, attempts, err
			}
			candidate := best
			candidate.Actions = slices.Clone(best.Actions)
			candidate.Actions = slices.Delete(candidate.Actions, start, start+width)
			attempts++
			failed, err := reproduce(candidate)
			if err != nil {
				return best, attempts, err
			}
			if failed {
				best = candidate
			} else {
				start += width
			}
		}
		if attempts == budget {
			break
		}
	}
	return best, attempts, nil
}
func runTransactionScript(f *transactionFixture, script transactionScript) error {
	for _, action := range script.Actions {
		if err := f.run(action); err != nil {
			return err
		}
	}
	if err := f.restart(); err != nil {
		return err
	}
	return f.observe()
}

func TestTransactionSeeded(t *testing.T) {
	seed, cases := uint64(1), 8
	if value := os.Getenv("HOMELABFS_SIM_SEED"); value != "" {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		seed = n
	}
	if value := os.Getenv("HOMELABFS_SIM_CASES"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 32 {
			t.Fatal("transaction cases must be 1..32")
		}
		cases = n
	}
	for index := 0; index < cases; index++ {
		t.Run(fmt.Sprint(seed+uint64(index)), func(t *testing.T) {
			script, err := generateTransactionScript(seed+uint64(index), 4)
			if err != nil {
				t.Fatal(err)
			}
			f := newTransactionFixture(t, script.Seed)
			if err := runTransactionScript(f, script); err != nil {
				var invariant *CheckFailure
				if !errors.As(err, &invariant) {
					t.Fatalf("script setup/execution: %+v: %v", script, err)
				}
				target := transactionFailureClass(err)
				reduced, attempts, reduceErr := minimizeTransactionScript(f.rig.ctx, script, 16, func(candidate transactionScript) (bool, error) {
					fresh := newTransactionFixture(t, candidate.Seed)
					err := runTransactionScript(fresh, candidate)
					return transactionFailureClass(err) == target, nil
				})
				// JSON contains independent history, original+reduced schedules and exact
				// journal/namespace observations, never just a seed or replica-equality bit.
				evidence := struct {
					Original, Reduced transactionScript
					Attempts          int
					History           []TransactionInvocation
					Observations      []TransactionObservation
					Trace             []Event
				}{script, reduced, attempts, f.history, f.observations, f.rig.s.Trace()}
				raw, jsonErr := json.Marshal(evidence)
				if jsonErr != nil {
					t.Fatalf("%v; evidence encoding: %v; reduction: %v", err, jsonErr, reduceErr)
				}
				t.Fatalf("%v; reduction %v; evidence %s", err, reduceErr, raw)
			}
		})
	}
}

func TestTransactionReproducibleSeed(t *testing.T) {
	script, err := generateTransactionScript(42, 3)
	if err != nil {
		t.Fatal(err)
	}
	a, b := newTransactionFixture(t, 42), newTransactionFixture(t, 42)
	if err := runTransactionScript(a, script); err != nil {
		t.Fatal(err)
	}
	if err := runTransactionScript(b, script); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.rig.s.Trace(), b.rig.s.Trace()) || !reflect.DeepEqual(a.apply.published, b.apply.published) {
		t.Fatal("seed did not reproduce trace and namespace")
	}
	// Frame bytes are deterministic too; private gob map order is never used in
	// journal identities or chosen replay input.
	ar, br := a.engine.Retained(), b.engine.Retained()
	if !reflect.DeepEqual(ar, br) {
		t.Fatal("seed did not reproduce engine journal cuts")
	}
}

func TestTransactionOracleNegativeControls(t *testing.T) {
	input := TransactionInput{ID: transactionScenarioID(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
	t.Run("partial-visibility", func(t *testing.T) {
		f := newTransactionFixture(t, 11)
		f.apply.partial = true
		err := f.run(transactionAction{Input: input})
		var invariant *CheckFailure
		if !errors.As(err, &invariant) {
			t.Fatalf("partial namespace not caught by oracle: %v", err)
		}
	})
	t.Run("double-execution", func(t *testing.T) {
		f := newTransactionFixture(t, 12)
		if err := f.run(transactionAction{Input: input}); err != nil {
			t.Fatal(err)
		}
		f.apply.double = true
		if err := f.engine.Reconcile(f.rig.ctx); err != nil {
			t.Fatal(err)
		}
		f.apply.published = f.apply.view(f.visibility)
		var invariant *CheckFailure
		if err := f.observe(); !errors.As(err, &invariant) {
			t.Fatalf("duplicate execution not caught: %v", err)
		}
	})
	t.Run("premature-home-reclamation", func(t *testing.T) {
		f := newTransactionFixture(t, 13)
		if err := f.run(transactionAction{Input: input}); err != nil {
			t.Fatal(err)
		}
		delete(f.apply.state.Objects, txObject(3, 50))
		f.apply.published = f.apply.view(f.visibility)
		var invariant *CheckFailure
		if err := f.observe(); !errors.As(err, &invariant) {
			t.Fatalf("missing home not caught: %v", err)
		}
	})
	t.Run("acknowledged-history-loss", func(t *testing.T) {
		f := newTransactionFixture(t, 14)
		if err := f.run(transactionAction{Input: input}); err != nil {
			t.Fatal(err)
		}
		observation := TransactionObservation{Ordinal: 100, View: cloneTransactionView(f.baseline)}
		if err := CheckTransactions(f.baseline, f.history, []TransactionObservation{observation}); err == nil {
			t.Fatal("missing acknowledged transaction passed")
		}
	})
	t.Run("minimized-target-failure", func(t *testing.T) {
		script := transactionScript{Seed: 15, Actions: []transactionAction{{Input: input}, {Input: TransactionInput{ID: transactionScenarioID(2), Kind: "write", SourceVolume: txUUID(4), Source: "alias", Data: []byte("dependent")}}}}
		reduced, attempts, err := minimizeTransactionScript(t.Context(), script, 12, func(candidate transactionScript) (bool, error) {
			f := newTransactionFixture(t, 15)
			f.apply.partial = true
			err := runTransactionScript(f, candidate)
			var invariant *CheckFailure
			return errors.As(err, &invariant), nil
		})
		if err != nil || attempts > 12 || len(reduced.Actions) != 1 || reduced.Actions[0].Input.Kind != "link" {
			t.Fatalf("minimization changed target: %+v attempts=%d err=%v", reduced, attempts, err)
		}
	})
}

func TestTransactionLostReplyThenAcknowledgedRetryHistoryLoss(t *testing.T) {
	f := newTransactionFixture(t, 16)
	input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "moved"}
	if err := f.run(transactionAction{Input: input, Fault: "reply"}); err != nil {
		t.Fatal(err)
	}
	p := f.states[input.ID]
	if _, err := f.propose(input.ID.Session.Anchor, record.RequestClaim{Document: p.document, Digest: p.digest}); err != nil {
		t.Fatal(err)
	}
	status, err := f.engine.GetStatus(f.rig.ctx, input.ID)
	if err != nil || status.Result == nil {
		t.Fatalf("retry result: %v", err)
	}
	retry := f.history[0]
	retry.Completion = "committed"
	retry.ReplyLost = false
	retry.CompletedAt = uint64(len(f.observations) + 1)
	retry.Reply = status.Result
	f.history = append(f.history, retry)
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	missing := TransactionObservation{Ordinal: 100, View: cloneTransactionView(f.baseline)}
	if err := CheckTransactions(f.baseline, f.history, []TransactionObservation{missing}); err == nil {
		t.Fatal("successful retry lost with uncertain original")
	}
	// Failure evidence must serialize complete maps, inputs and exact frames.
	if _, err := json.Marshal(struct {
		History      []TransactionInvocation
		Observations []TransactionObservation
	}{f.history, f.observations}); err != nil {
		t.Fatalf("counterexample evidence unavailable: %v", err)
	}
}

func TestTransactionConflictingRetryRequiresWinningClaim(t *testing.T) {
	f := newTransactionFixture(t, 24)
	input := TransactionInput{ID: f.id(1), Kind: "rename", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "moved"}
	if err := f.run(transactionAction{Input: input, Fault: "reply"}); err != nil {
		t.Fatal(err)
	}
	competing := *f.states[input.ID].document.Original
	competing.Destination.Components = []string{"different"}
	canonical, err := record.EncodeOriginal(competing)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	if _, err := f.propose(input.ID.Session.Anchor, record.RequestClaim{Document: record.RequestDocument{Class: 1, Original: &competing}, Digest: digest}); !errors.Is(err, transactions.ErrConflict) {
		t.Fatalf("conflicting retry: %v", err)
	}
	retry := f.history[0]
	retry.Canonical = canonical
	retry.Digest = digest
	retry.Completion = "conflict"
	retry.CompletedAt = uint64(len(f.observations) + 1)
	f.history = append(f.history, retry)
	if err := f.observe(); err != nil {
		t.Fatal(err)
	}
	observation := TransactionObservation{Ordinal: 100, View: cloneTransactionView(f.baseline)}
	if err := CheckTransactions(f.baseline, f.history, []TransactionObservation{observation}); err == nil {
		t.Fatal("conflict reply passed without its winning claim")
	}
}

func transactionFailureClass(err error) string {
	var invariant *CheckFailure
	if !errors.As(err, &invariant) {
		return ""
	}
	message := invariant.Cause.Error()
	for _, prefix := range []string{"oracle namespace/visibility/home mismatch", "oracle execution count", "oracle acknowledged", "oracle missing retained dependency", "oracle owner prefix", "oracle required history pin lost"} {
		if strings.HasPrefix(message, prefix) {
			return prefix
		}
	}
	kind, _, _ := strings.Cut(message, ":")
	return kind
}

func TestTransactionOracleRetainedDependencies(t *testing.T) {
	f := newTransactionFixture(t, 27)
	input := TransactionInput{ID: f.id(1), Kind: "link", SourceVolume: txUUID(3), Source: "file", DestinationVolume: txUUID(4), Destination: "alias"}
	if err := f.run(transactionAction{Input: input}); err != nil {
		t.Fatal(err)
	}
	complete := f.observations[len(f.observations)-1]
	controls := []struct {
		name   string
		remove func(any) bool
	}{
		{"install", func(payload any) bool { _, ok := payload.(record.Install); return ok }},
		{"barrier", func(payload any) bool { _, ok := payload.(record.ViewBarrier); return ok }},
		{"acceptance", func(payload any) bool { _, ok := payload.(record.Accept); return ok }},
		{"session-owner-prefix", func(payload any) bool { _, ok := payload.(record.Session); return ok }},
		{"prepare", func(payload any) bool { _, ok := payload.(record.Prepared); return ok }},
	}
	for _, control := range controls {
		t.Run(control.name, func(t *testing.T) {
			broken := complete
			broken.Frames = nil
			removed := 0
			for _, entry := range complete.Frames {
				if control.remove(entry.Record.Payload) {
					removed++
				} else {
					broken.Frames = append(broken.Frames, entry)
				}
			}
			if removed == 0 {
				t.Fatal("control removed no source frames")
			}
			if err := CheckTransactions(f.baseline, f.history, []TransactionObservation{broken}); err == nil {
				t.Fatal("missing recovery dependency passed oracle")
			}
		})
	}
	t.Run("unresolved-prepare-pin", func(t *testing.T) {
		pending := newTransactionFixture(t, 28)
		input.ID = pending.id(1)
		p, err := pending.start(input)
		if err != nil {
			t.Fatal(err)
		}
		steps := pending.steps(p)
		for _, step := range steps[:8] {
			if err := step.run(); err != nil {
				t.Fatal(err)
			}
		}
		if err := pending.observe(); err != nil {
			t.Fatal(err)
		}
		previous := pending.observations[0]
		lost := previous
		lost.Ordinal++
		lost.Frames = nil
		for _, entry := range previous.Frames {
			if _, prepared := entry.Record.Payload.(record.Prepared); !prepared {
				lost.Frames = append(lost.Frames, entry)
			}
		}
		// A prepared tail need not yet be referenced by a DECISION. The earlier
		// actual observation pins it independently of terminal outcome evidence.
		if err := CheckTransactions(pending.baseline, pending.history, []TransactionObservation{previous, lost}); err == nil {
			t.Fatal("unresolved prepare evidence was forgotten")
		}
	})
}
