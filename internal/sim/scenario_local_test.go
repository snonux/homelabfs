package sim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/failpoint"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// localHistoryDriver deliberately uses the existing actual local service probe.
// It supplies no Protocol, RECEIPT or normal-mode serving/readiness assertion.
type localHistoryDriver struct {
	f                  *localService
	r                  *rig
	correlation        map[fsapi.UUID]string
	doubleAppend       bool
	overwriteOperation string
}

var _ ScenarioDriver = (*localHistoryDriver)(nil)

func newLocalHistoryDriver(t *testing.T, seed uint64, broken bool) *localHistoryDriver {
	f := &localService{}
	return &localHistoryDriver{f: f, r: newRigWithTimeout(t, seed, 1, f.factory, 30*time.Second), correlation: make(map[fsapi.UUID]string), doubleAppend: broken}
}

func (d *localHistoryDriver) Volume() fsapi.UUID             { return d.f.config.Storage.Volume }
func (d *localHistoryDriver) Now() time.Duration             { return d.r.s.Clock().Time().Sub(epoch()) }
func (d *localHistoryDriver) Baseline() map[string]FileState { return oracleBaseline() }

func (d *localHistoryDriver) Execute(ctx context.Context, action ScenarioAction) (Outcome, string, error) {
	outcome, message := Acknowledged, "OK (disposable local validation only)"
	err := d.r.nodes[0].Call(ctx, action.ID, func(ctx context.Context, _ *Runtime) error {
		command, err := d.command(ctx, action.Intent, action.ID == d.overwriteOperation)
		if err != nil {
			return err
		}
		if action.Fault == FailBeforeAppend {
			if _, err = d.r.nodes[0].Enable(failpoint.JournalAppend, failpoint.Rule{Action: failpoint.ReturnError, Nth: 1, Err: errors.New("seeded before-append failure")}); err != nil {
				return err
			}
		}
		_, requestErr := d.f.command(ctx, command)
		if action.Fault == FailBeforeAppend {
			if requestErr == nil {
				return fmt.Errorf("before-append fault did not reject mutation")
			}
			outcome, message = Uncertain, "I/O error after admission; rollback not asserted"
		} else if requestErr != nil {
			return requestErr
		}
		if d.doubleAppend && action.Intent.Kind == AppendFile && requestErr == nil {
			// Deliberately broken client/protocol behavior executes the same
			// logical append twice against the actual service and journal.
			if _, err = d.f.command(ctx, command); err != nil {
				return err
			}
		}
		entries, err := d.scan(ctx)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if _, ok := e.Record.Payload.(record.Mutation); ok {
				id := e.Record.Header.Operation
				if _, known := d.correlation[id]; !known {
					d.correlation[id] = action.ID
				}
			}
		}
		if action.Fault == LoseReply || action.Fault == LoseReplyAndCrash {
			outcome, message = Uncertain, "client reply lost"
		}
		return nil
	})
	return outcome, message, err
}

func (d *localHistoryDriver) After(ctx context.Context, action ScenarioAction) error {
	if action.Fault == CrashAfterReply || action.Fault == LoseReplyAndCrash || action.Fault == FailBeforeAppend {
		if err := d.recover(ctx); err != nil {
			return err
		}
	}
	return d.r.s.Advance(action.Advance)
}

func (d *localHistoryDriver) Observe(ctx context.Context) ([]Observation, error) {
	if err := d.recover(ctx); err != nil {
		return nil, err
	}
	o := Observation{Replica: "local-validation", Files: make(map[string]FileState)}
	err := d.r.nodes[0].Call(ctx, "observe recovered actual data and metadata", func(ctx context.Context, _ *Runtime) error {
		root, err := d.f.command(ctx, fsapi.Command{Operation: fsapi.Getattr, Object: d.f.config.Storage.Root})
		if err != nil {
			return err
		}
		o.Files["."] = fileState(*root.Attr, nil)
		page, err := d.f.command(ctx, fsapi.Command{Operation: fsapi.Readdir, Object: d.f.config.Storage.Root, Limit: 64})
		if err != nil {
			return err
		}
		for _, entry := range page.Entries {
			opened, err := d.f.command(ctx, fsapi.Command{Operation: fsapi.Open, Object: entry.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
			if err != nil {
				return err
			}
			read, err := d.f.command(ctx, fsapi.Command{Operation: fsapi.Read, Handle: opened.Handle, Size: opened.Attr.Size})
			if err != nil {
				return err
			}
			o.Files[entry.Name] = fileState(*opened.Attr, read.Data)
			if _, err = d.f.command(ctx, fsapi.Command{Operation: fsapi.Release, Handle: opened.Handle}); err != nil {
				return err
			}
		}
		return d.observeJournal(ctx, &o)
	})
	return []Observation{o}, err
}

func (d *localHistoryDriver) command(ctx context.Context, i Intent, overwrite bool) (fsapi.Command, error) {
	root := d.f.config.Storage.Root
	if i.Kind == CreateFile {
		return fsapi.Command{Operation: fsapi.Create, Parent: root, Name: i.Name, Mode: i.Mode, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}}, nil
	}
	if i.Kind == RenameFile {
		return fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: i.Name, Destination: root, DestinationMount: d.Volume(), NewName: i.Destination, NoReplace: true}, nil
	}
	if i.Kind == RemoveFile {
		return fsapi.Command{Operation: fsapi.Unlink, Parent: root, Name: i.Name}, nil
	}
	lookup, err := d.f.command(ctx, fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: i.Name})
	if err != nil {
		return fsapi.Command{}, err
	}
	if i.Kind == ChmodFile {
		return fsapi.Command{Operation: fsapi.Setattr, Object: lookup.Attr.Object, Attributes: fsapi.Attributes{Mode: &i.Mode}}, nil
	}
	// The negative adapter can deliberately issue a positional overwrite for
	// an externally requested append. Its actual journal offset remains evidence.
	opened, err := d.f.command(ctx, fsapi.Command{Operation: fsapi.Open, Object: lookup.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Append: !overwrite}})
	return fsapi.Command{Operation: fsapi.Write, Handle: opened.Handle, Data: i.Data}, err
}

func (d *localHistoryDriver) recover(ctx context.Context) error {
	if err := d.r.nodes[0].Crash(); err != nil {
		return err
	}
	return d.r.nodes[0].Restart(ctx)
}

func (d *localHistoryDriver) scan(ctx context.Context) ([]segment.Entry, error) {
	c := d.f.config
	r, err := segment.OpenReader(ctx, c.Source, c.Stream, segment.RecoveryOptions{}, record.Position{Volume: c.Stream.Volume, Generation: c.Stream.Generation, Configuration: c.Stream.Configuration, Epoch: c.Stream.Epoch, Sequence: 1})
	if err != nil {
		return nil, err
	}
	var entries []segment.Entry
	for {
		e, nextErr := r.Next(ctx)
		if nextErr == io.EOF {
			return entries, r.Close()
		}
		if nextErr != nil {
			return nil, errors.Join(nextErr, r.Close())
		}
		entries = append(entries, e)
		if len(entries) > 4096 {
			return nil, errors.Join(fmt.Errorf("journal observation budget"), r.Close())
		}
	}
}

func (d *localHistoryDriver) observeJournal(ctx context.Context, o *Observation) error {
	entries, err := d.scan(ctx)
	if err != nil {
		return err
	}
	cut := d.f.service.Status().AppliedPosition
	mutations := make(map[record.Hash]segment.Entry)
	names := make(map[fsapi.ObjectID]string)
	for _, e := range entries {
		if e.Reference.Position.Sequence > cut.Sequence {
			break
		}
		if e.Reference.Position == cut {
			o.Through = e.Reference
		}
		switch p := e.Record.Payload.(type) {
		case record.Mutation:
			mutations[e.Reference.SHA256] = e
		case record.Commit:
			mutation, ok := mutations[p.Mutation.SHA256]
			if !ok || !reflect.DeepEqual(mutation.Reference, p.Mutation) {
				return fmt.Errorf("unobserved commit mutation")
			}
			intent, err := decodeLocalIntent(mutation.Record.Payload.(record.Mutation), names)
			if err != nil {
				return err
			}
			id, ok := d.correlation[mutation.Record.Header.Operation]
			if !ok {
				return fmt.Errorf("uncorrelated actual mutation")
			}
			effect := AppliedEffect{ID: id, Operation: mutation.Record.Header.Operation, Source: e.Reference, Intent: intent}
			if write, ok := mutation.Record.Payload.(record.Mutation).Intent.Arguments.(record.Write); ok {
				offset := write.Offset
				effect.WriteOffset = &offset
			}
			o.Effects = append(o.Effects, effect)
		}
	}
	return nil
}

func decodeLocalIntent(m record.Mutation, names map[fsapi.ObjectID]string) (Intent, error) {
	switch a := m.Intent.Arguments.(type) {
	case record.Create:
		names[a.Object] = a.Name
		return Intent{Kind: CreateFile, Name: a.Name, Mode: a.RequestedMode &^ a.Umask, UID: 1000, GID: 1000}, nil
	case record.Write:
		return Intent{Kind: AppendFile, Name: names[a.Object], Data: a.Data}, nil
	case record.Setattr:
		if a.Mode == nil || a.Mask != 1 {
			return Intent{}, fmt.Errorf("unsupported recovered setattr")
		}
		return Intent{Kind: ChmodFile, Name: names[a.Object], Mode: *a.Mode}, nil
	case record.Rename:
		names[a.Source] = a.DestinationName
		return Intent{Kind: RenameFile, Name: a.SourceName, Destination: a.DestinationName}, nil
	case record.Unlink:
		delete(names, a.Removed)
		return Intent{Kind: RemoveFile, Name: a.Name}, nil
	default:
		return Intent{}, fmt.Errorf("unsupported recovered mutation %T", a)
	}
}

func fileState(a fsapi.Attr, data []byte) FileState {
	return FileState{Kind: a.Kind, Mode: a.Mode, UID: a.UID, GID: a.GID, Nlink: a.Nlink, Size: a.Size, Data: data}
}

func TestSeededLocalHistoryScenarios(t *testing.T) {
	seed, count := uint64(1), uint64(8)
	for name, target := range map[string]*uint64{"HOMELABFS_SIM_SEED": &seed, "HOMELABFS_SIM_CASES": &count} {
		if value := os.Getenv(name); value != "" {
			n, err := strconv.ParseUint(value, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			*target = n
		}
	}
	if count < 1 || count > 64 || seed > ^uint64(0)-count {
		t.Fatal("seed/case budget")
	}
	for i := range count {
		t.Run(fmt.Sprintf("seed-%d", seed+i), func(t *testing.T) {
			scenario, err := GenerateScenario(seed+i, 12)
			if err != nil {
				t.Fatal(err)
			}
			d := newLocalHistoryDriver(t, seed+i, false)
			result, err := RunScenario(d.r.ctx, scenario, d)
			if err != nil {
				minimal, attempts := scenario, 0
				var failure *CheckFailure
				if errors.As(err, &failure) {
					var minimizeErr error
					minimal, attempts, minimizeErr = MinimizeScenario(t.Context(), scenario, 24, func(ctx context.Context, s Scenario) (bool, error) {
						return reproduceLocalFailure(t, ctx, s, false)
					})
					if minimizeErr != nil {
						t.Logf("minimization incomplete: %v", minimizeErr)
					}
				}
				encoded, encodeErr := json.Marshal(struct {
					Scenario Scenario
					Minimal  Scenario
					Attempts int
					Evidence ScenarioResult
					Trace    []Event
				}{scenario, minimal, attempts, result, d.r.s.Trace()})
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				t.Fatalf("%v; replay=%s", err, encoded)
			}
		})
	}
}

func TestLocalHistoryReplayAndBrokenAppendMinimization(t *testing.T) {
	scenario := Scenario{Seed: 99, Actions: []ScenarioAction{
		{ID: "create", Client: "one", Intent: Intent{Kind: CreateFile, Name: "a", Mode: 0o640, UID: 1000, GID: 1000}},
		{ID: "append", Client: "two", Intent: Intent{Kind: AppendFile, Name: "a", Data: []byte("once")}, Fault: LoseReplyAndCrash},
		{ID: "chmod", Client: "one", Intent: Intent{Kind: ChmodFile, Name: "a", Mode: 0o644}},
	}}
	var traces [][]Event
	for range 2 {
		d := newLocalHistoryDriver(t, 99, false)
		if _, err := RunScenario(d.r.ctx, scenario, d); err != nil {
			t.Fatal(err)
		}
		traces = append(traces, d.r.s.Trace())
	}
	if !reflect.DeepEqual(traces[0], traces[1]) {
		t.Fatal("same seeded script changed its trace")
	}
	reproduce := func(ctx context.Context, s Scenario) (bool, error) { return reproduceLocalFailure(t, ctx, s, true) }
	failed, err := reproduce(t.Context(), scenario)
	if err != nil || !failed {
		t.Fatal("deliberate real-service duplicate append escaped", err)
	}
	minimal, attempts, err := MinimizeScenario(t.Context(), scenario, 24, reproduce)
	if err != nil || attempts > 24 || len(minimal.Actions) != 2 {
		t.Fatal(minimal, attempts, err)
	}
}

func TestLocalHistoryRetainsEvidenceOnScriptFailure(t *testing.T) {
	d := newLocalHistoryDriver(t, 77, false)
	s := Scenario{Seed: 77, Actions: []ScenarioAction{
		{ID: "create", Client: "client", Intent: Intent{Kind: CreateFile, Name: "a", Mode: 0o640, UID: 1000, GID: 1000}},
		{ID: "invalid", Fault: ScenarioFault(255)},
	}}
	result, err := RunScenario(d.r.ctx, s, d)
	if err == nil || len(result.History) != 1 || result.History[0].Outcome != Acknowledged {
		t.Fatal("lost external evidence on invalid script", result, err)
	}
}

func TestLocalHistoryAppendOffsetMaskedByUnlink(t *testing.T) {
	scenario := Scenario{Seed: 123, Actions: []ScenarioAction{
		{ID: "create", Client: "one", Intent: Intent{Kind: CreateFile, Name: "a", Mode: 0o640, UID: 1000, GID: 1000}},
		{ID: "append-A", Client: "one", Intent: Intent{Kind: AppendFile, Name: "a", Data: []byte("A")}},
		{ID: "append-B", Client: "two", Intent: Intent{Kind: AppendFile, Name: "a", Data: []byte("B")}},
		{ID: "unlink", Client: "one", Intent: Intent{Kind: RemoveFile, Name: "a"}},
	}}
	for _, broken := range []bool{false, true} {
		t.Run(fmt.Sprintf("overwrite=%t", broken), func(t *testing.T) {
			d := newLocalHistoryDriver(t, scenario.Seed, false)
			if broken {
				d.overwriteOperation = "append-B"
			}
			result, err := RunScenario(d.r.ctx, scenario, d)
			if len(result.Observations) != 1 {
				t.Fatalf("missing recovered journal: %v", err)
			}
			o := result.Observations[0]
			if !reflect.DeepEqual(o.Files, oracleBaseline()) {
				t.Fatalf("unlink did not mask the bytes: %v", o.Files)
			}
			if len(o.Effects) != 4 {
				t.Fatalf("missing acknowledged effects: %v", o.Effects)
			}
			for index, want := range map[int]uint64{1: 0, 2: 1} {
				if broken && index == 2 {
					want = 0
				}
				got := o.Effects[index].WriteOffset
				if got == nil || *got != want {
					t.Fatalf("effect %d: offset %v, want %d", index, got, want)
				}
			}
			if !broken {
				if err != nil {
					t.Fatal(err)
				}
				// Even correct final bytes/history must fail closed when the
				// adapter omits an append's actual selected journal offset.
				o.Effects[2].WriteOffset = nil
				err = CheckHistory(d.Volume(), d.Baseline(), result.History, []Observation{o})
				if err == nil || !strings.Contains(err.Error(), "missing selected append offset") {
					t.Fatalf("missing evidence accepted: %v", err)
				}
				return
			}
			var failure *CheckFailure
			if !errors.As(err, &failure) || !strings.Contains(err.Error(), "selected append offset 0 differs from independent length 1") {
				t.Fatalf("masked overwrite escaped checker: %v", err)
			}
		})
	}
}

func reproduceLocalFailure(t *testing.T, ctx context.Context, s Scenario, broken bool) (bool, error) {
	t.Helper()
	d := newLocalHistoryDriver(t, s.Seed, broken)
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := RunScenario(bounded, s, d)
	if closeErr := d.r.nodes[0].Close(); closeErr != nil {
		return false, closeErr
	}
	var failure *CheckFailure
	return errors.As(err, &failure), nil
}
