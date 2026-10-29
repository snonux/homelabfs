package sim

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// ScenarioFault selects a named controlled local-service boundary. Network
// schedules remain exact-key scripts; no unconstrained Go calls are generated.
type ScenarioFault uint8

const (
	// NoFault observes the actual client reply.
	NoFault ScenarioFault = iota
	// LoseReply hides a completed reply from the external client history.
	LoseReply
	// CrashAfterReply crashes and reconstructs after an acknowledged reply.
	CrashAfterReply
	// LoseReplyAndCrash hides the reply, then crashes and reconstructs.
	LoseReplyAndCrash
	// FailBeforeAppend injects the real journal before-append error, then recovers.
	FailBeforeAppend
)

// ScenarioAction owns one serial request and a fault at its named boundary.
type ScenarioAction struct {
	ID, Client string
	Intent     Intent
	Fault      ScenarioFault
	Advance    time.Duration
}

// Scenario is an executable input retained alongside its seed and trace.
type Scenario struct {
	Seed    uint64
	Actions []ScenarioAction
}

// ScenarioDriver is the real-core integration seam. Execute must use Node.Call,
// exact failpoint/packet boundaries and explicit virtual time only. Observation
// must decode recovered journal effects and read real filesystem data/metadata.
// Normal distributed replication is not supplied by this interface.
type ScenarioDriver interface {
	Volume() fsapi.UUID
	Now() time.Duration
	Baseline() map[string]FileState
	Execute(context.Context, ScenarioAction) (Outcome, string, error)
	After(context.Context, ScenarioAction) error
	Observe(context.Context) ([]Observation, error)
}

// ScenarioResult retains external history and recovered observations on failure.
type ScenarioResult struct {
	History      []HistoryEntry
	Observations []Observation
}

// CheckFailure separates invariant failures from malformed scripts and fixture
// failures, so minimization cannot replace a defect with an invalid setup.
type CheckFailure struct{ Cause error }

// GenerateScenario creates at most twelve bounded local actions, permitting at
// most twelve crashes plus initial startup within the fixture lifetime limit.
// Its random stream is independent from transport and core allocation streams.
func GenerateScenario(seed uint64, count int) (Scenario, error) {
	if count < 1 || count > 12 {
		return Scenario{}, fmt.Errorf("scenario action budget (1..12)")
	}
	s := Scenario{Seed: seed}
	r := NewRandom(seed, "history-scenario/v1")
	name := "file"
	for i := range count {
		var random [16]byte
		_, _ = r.Read(random[:])
		a := ScenarioAction{ID: fmt.Sprintf("op/%d", i), Client: fmt.Sprintf("client/%d", random[0]%3), Fault: ScenarioFault(random[1] % 5), Advance: (time.Duration(random[2]) + 1) * time.Millisecond}
		if i == 0 {
			a.Intent = Intent{Kind: CreateFile, Name: name, Mode: 0o640, UID: 1000, GID: 1000}
			a.Fault = NoFault
		} else {
			switch random[3] % 3 {
			case 0:
				a.Intent = Intent{Kind: AppendFile, Name: name, Data: []byte(fmt.Sprintf("%016x/%d/%016x\n", seed, i, binary.BigEndian.Uint64(random[8:])))}
			case 1:
				a.Intent = Intent{Kind: ChmodFile, Name: name, Mode: 0o600 | uint16(random[4]&7)}
			case 2:
				next := fmt.Sprintf("file-%d", i)
				a.Intent = Intent{Kind: RenameFile, Name: name, Destination: next}
				name = next
				// Later root names depend on this rename having executed.
				if a.Fault == FailBeforeAppend {
					a.Fault = LoseReplyAndCrash
				}
			}
		}
		s.Actions = append(s.Actions, a)
	}
	return s, nil
}

// RunScenario records external evidence before each call, then checks the real
// recovered state. Driver errors remain distinct from checker counterexamples.
func RunScenario(ctx context.Context, scenario Scenario, driver ScenarioDriver) (result ScenarioResult, retErr error) {
	if driver == nil || len(scenario.Actions) > 12 {
		return result, fmt.Errorf("invalid scenario driver or action budget")
	}
	history := NewHistory()
	defer func() { result.History = history.Entries() }()
	for _, action := range scenario.Actions {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if action.Fault > FailBeforeAppend || action.Advance < 0 || action.Advance > time.Second {
			return result, fmt.Errorf("invalid scenario fault or time")
		}
		if err := history.Invoke(action.ID, driver.Volume(), action.Client, driver.Now(), action.Intent); err != nil {
			return result, err
		}
		outcome, message, err := driver.Execute(ctx, action)
		if err != nil {
			return result, fmt.Errorf("execute %s: %w", action.ID, err)
		}
		if err := history.Complete(action.ID, driver.Now(), outcome, message); err != nil {
			return result, err
		}
		// Record the client boundary before a chosen post-reply crash. A lost
		// reply stays uncertain even when recovery reveals the completed effect.
		if err := driver.After(ctx, action); err != nil {
			return result, fmt.Errorf("after %s: %w", action.ID, err)
		}
	}
	result.History = history.Entries()
	var err error
	result.Observations, err = driver.Observe(ctx)
	if err != nil {
		return result, fmt.Errorf("observe recovered state: %w", err)
	}
	if err := CheckHistory(driver.Volume(), driver.Baseline(), result.History, result.Observations); err != nil {
		return result, &CheckFailure{Cause: err}
	}
	return result, nil
}

// Error reports the invariant counterexample.
func (e *CheckFailure) Error() string { return "history invariant: " + e.Cause.Error() }

// Unwrap exposes the checker's detailed cause.
func (e *CheckFailure) Unwrap() error { return e.Cause }

// MinimizeScenario deletes contiguous action chunks within an explicit attempt
// budget. reproduce must use a fresh fixture and report true only for the target
// invariant failure, never for a setup failure. The result is a bounded reduction,
// not a claim of global minimality. The caller retains the original seed/script.
func MinimizeScenario(ctx context.Context, original Scenario, budget int, reproduce func(context.Context, Scenario) (bool, error)) (Scenario, int, error) {
	best := cloneScenario(original)
	if len(best.Actions) > 12 || budget < 1 || budget > 128 || reproduce == nil {
		return best, 0, fmt.Errorf("invalid minimization budget")
	}
	attempts := 0
	for width := len(best.Actions); width >= 1; width /= 2 {
		for start := 0; start+width <= len(best.Actions) && attempts < budget; {
			if err := ctx.Err(); err != nil {
				return best, attempts, err
			}
			candidate := cloneScenario(best)
			candidate.Actions = slices.Delete(candidate.Actions, start, start+width)
			attempts++
			failed, err := reproduce(ctx, candidate)
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

func cloneScenario(s Scenario) Scenario {
	s.Actions = slices.Clone(s.Actions)
	for i := range s.Actions {
		s.Actions[i].Intent.Data = slices.Clone(s.Actions[i].Intent.Data)
	}
	return s
}
