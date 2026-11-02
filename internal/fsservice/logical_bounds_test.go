package fsservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

func TestLogicalBoundedVersionsPinsClaimsAndRecoveryReserve(t *testing.T) {
	f := newLogicalFixture(t)
	f.config.Limits.Versions = 2
	f.config.Limits.Claims = 1
	view, err := logicalview.New(t.Context(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.view = view
	permissions, err := NewUnixPermissions(uid(419))
	if err != nil {
		t.Fatal(err)
	}
	f.planner, err = NewLogicalPlanner(LogicalConfig{View: view, Permissions: permissions, Clock: fixedClock{}})
	if err != nil {
		t.Fatal(err)
	}
	pin, err := view.PinVersion(nil)
	if err != nil {
		t.Fatal(err)
	}
	cred := logicalCred()
	root := f.local.c.Storage.Root
	first := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "one", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	e := f.commit(first, uid(11))
	second := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "two", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	stage := logicalview.Stage{Slot: e.Selected.Slot, Previous: &e.Selected, Origin: e.Sources[0].Stream, OriginLaunch: e.Sources[0].Holder, Recipient: f.config.Authority.Sequencer, Claim: second.Claim, Intent: second.Intent, Operations: []storage.Operation{second.Operation}}
	stage.Slot.Serial++
	if _, err = view.Stage(t.Context(), stage); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("bounded candidate admitted", err)
	}
	proof := []byte("verified durable replacement")
	if err = view.MarkBacking(t.Context(), e.Selected, false, proof); err != nil {
		t.Fatal(err)
	}
	if err = view.MarkBacking(t.Context(), e.Selected, true, proof); err != nil {
		t.Fatal(err)
	}
	if err = view.Retire(t.Context(), e.Selected, []fsapi.UUID{first.Claim}, proof); err != nil {
		t.Fatal(err)
	}
	if _, err = view.Stage(t.Context(), stage); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("pinned version was released", err)
	}
	pin.Release()
	pin.Release()
	if err = view.Retire(t.Context(), e.Selected, nil, proof); err != nil {
		t.Fatal(err)
	}
	if _, err = view.Stage(t.Context(), stage); err != nil {
		t.Fatal("released capacity did not admit next slot", err)
	}
	if err = view.Close(); !errors.Is(err, fsapi.Busy) {
		t.Fatal("uncertain candidate was discarded", err)
	}
	// Exhaust the remaining ordinary arena pool. Recovery was already reserved.
	used := f.config.Budget.Usage().Pools[admission.Memory]
	remaining := uint64(64<<20) - used.Bytes
	lease, err := f.config.Budget.Reserve(admission.Request{Peer: f.config.Authority.Sequencer.Node.String(), Volume: root.Home.String(), Class: admission.Memory, Cost: admission.Resources{Bytes: remaining, OwnerRAM: remaining, Operations: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lease.Release() }()
	var checkpoint bytes.Buffer
	if err = view.WriteRecovery(t.Context(), &checkpoint); err != nil || checkpoint.Len() == 0 {
		t.Fatal("recovery starved behind ordinary capacity", err)
	}
}

func TestLogicalEvidenceIdentityAndSourceFrontiersRejectFalseProgress(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "stable", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	g, digest := f.stage(p, uid(11))
	e := f.protect(p, g, digest, false)
	if err := f.view.ObserveSource(logicalview.SourceFrontier{Stream: g.Origin, Received: 100, Durable: 90}); err != nil {
		t.Fatal(err)
	}
	other := g.Origin
	other.Origin = uid(9)
	other.Journal = uid(991)
	if err := f.view.ObserveSource(logicalview.SourceFrontier{Stream: other, Received: 1, Durable: 1}); err != nil {
		t.Fatal(err)
	}
	if frontier := f.view.Frontiers(); frontier.Selected != nil || frontier.Visible != nil || len(frontier.Sources) != 2 {
		t.Fatal("receipt/durability selected candidate", frontier)
	}
	if err := f.view.ObserveSource(logicalview.SourceFrontier{Stream: g.Origin, Received: 80, Durable: 70}); !errors.Is(err, fsapi.Stale) {
		t.Fatal("source frontier rolled back", err)
	}
	for _, mutate := range []func(*logicalview.Evidence){
		func(e *logicalview.Evidence) { e.Digest[0] ^= 1 },
		func(e *logicalview.Evidence) { e.Acceptances = nil },
		func(e *logicalview.Evidence) { e.Sources = nil },
		func(e *logicalview.Evidence) {
			e.Sources = append([]record.SourceCoverage(nil), e.Sources...)
			e.Sources[0].Holder = f.config.Authority.Sequencer
		},
		func(e *logicalview.Evidence) { e.Selected.Slot.Serial++ },
	} {
		bad := e
		mutate(&bad)
		if err := f.view.Select(t.Context(), bad); err == nil {
			t.Fatal("invalid selection evidence accepted")
		}
	}
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	bad := e
	bad.Result = bytes.Clone(e.Result)
	bad.Result[0] ^= 1
	if err := f.view.Select(t.Context(), bad); !errors.Is(err, fsapi.Stale) {
		t.Fatal("immutable result changed", err)
	}
	f.verifier.fail = logicalview.Publication
	if err := f.view.Publish(t.Context(), e); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal(err)
	}
	f.verifier.fail = 0
	o := f.observation()
	if _, err := o.Lookup(t.Context(), root, "stable"); !errors.Is(err, fsapi.NotFound) {
		t.Fatal(err)
	}
	_ = o.Close()
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	// A changed intent under the previously consumed operation ID cannot execute.
	g.Previous = &e.Selected
	g.Slot.Serial++
	g.Intent = []byte("changed")
	if _, err := f.view.Stage(t.Context(), g); !errors.Is(err, fsapi.Stale) {
		t.Fatal("claim ID reused", err)
	}
}

func TestLogicalRecoveryRawNamesAndTamperedPendingRedo(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	name := "raw-\xff"
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: name, Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	other := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "raw-\xfe", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	if bytes.Equal(p.Intent, other.Intent) {
		t.Fatal("distinct raw claims collapsed to one Unicode replacement")
	}
	g, digest := f.stage(p, uid(11))
	e := f.protect(p, g, digest, false)
	var encoded bytes.Buffer
	if err := f.view.WriteRecovery(t.Context(), &encoded); err != nil {
		t.Fatal(err)
	}
	makeConfig := func() logicalview.Config {
		config := f.config
		budget, err := admission.New(logicalBudget())
		if err != nil {
			t.Fatal(err)
		}
		config.Budget = budget
		return config
	}
	restored, err := logicalview.Restore(t.Context(), makeConfig(), bytes.NewReader(encoded.Bytes()), []byte("verified full source recovery"))
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if err = restored.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	o, err := restored.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := o.Lookup(t.Context(), root, name)
	if err != nil || entry.Name != name {
		t.Fatal("raw namespace bytes lost", entry, err)
	}
	_ = o.Close()
	for _, selected := range []bool{false, true} {
		if selected {
			if err = f.view.Select(t.Context(), e); err != nil {
				t.Fatal(err)
			}
		}
		encoded.Reset()
		if err = f.view.WriteRecovery(t.Context(), &encoded); err != nil {
			t.Fatal(err)
		}
		var state logicalview.RecoveryState
		if err = json.Unmarshal(encoded.Bytes(), &state); err != nil {
			t.Fatal(err)
		}
		for i := range state.Pending.Next.Images[0].Image.Objects {
			object := &state.Pending.Next.Images[0].Image.Objects[i]
			if object.Attr.Object == p.Operation.Object {
				object.Attr.Mode ^= 1
			}
		}
		var tampered bytes.Buffer
		if err = json.NewEncoder(&tampered).Encode(state); err != nil {
			t.Fatal(err)
		}
		if _, err = logicalview.Restore(t.Context(), makeConfig(), &tampered, []byte("verified full source recovery")); !errors.Is(err, fsapi.Stale) {
			t.Fatal("unrelated valid afterimage replaced immutable candidate", selected, err)
		}
	}
	duplicate := bytes.Replace(encoded.Bytes(), []byte("\"Version\":1,"), []byte("\"Version\":1,\"Version\":1,"), 1)
	if _, err = logicalview.Restore(t.Context(), makeConfig(), bytes.NewReader(duplicate), []byte("verified full source recovery")); err == nil {
		t.Fatal("duplicate recovery field accepted")
	}
}

func TestLogicalNeverReusedReservationsReaderBoundsAndAuthorityGate(t *testing.T) {
	f := newLogicalFixture(t)
	var observations []*logicalview.Observation
	for i := 0; i < f.config.Limits.Readers; i++ {
		observations = append(observations, f.observation())
	}
	if _, err := f.view.Begin(t.Context()); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("unbounded observations", err)
	}
	for _, o := range observations {
		if err := o.Close(); err != nil {
			t.Fatal(err)
		}
	}
	f.verifier.fail = logicalview.ReadObservation
	if _, err := f.view.Begin(t.Context()); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("authority loss admitted read", err)
	}
	f.verifier.fail = 0
	count := 0
	for i := uint64(1000); i < 2000; i++ {
		err := f.view.ReserveIdentity(t.Context(), uid(i), fsapi.ObjectID{})
		if errors.Is(err, fsapi.NoSpace) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count == 0 || count >= f.config.Limits.Identities {
		t.Fatal("identity ledger failed finite backpressure", count)
	}
	config := f.config
	config.Limits.Identities = 1
	if _, err := logicalview.New(t.Context(), config); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("baseline exceeded reserved identity ledger", err)
	}
}

func TestLogicalDescriptionPinVerificationAndReclaimAdmission(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "orphan", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	e := f.commit(p, uid(11))
	id := p.Operation.Object
	o := f.observation()
	f.verifier.fail = logicalview.HandleAdmission
	if _, err := f.planner.Observe(t.Context(), o, cred, fsapi.Command{Mount: root.Home, Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}}); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("unprotected home handle admitted", err)
	}
	f.verifier.fail = 0
	opened, err := f.planner.Observe(t.Context(), o, cred, fsapi.Command{Mount: root.Home, Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
	if err != nil {
		t.Fatal(err)
	}
	_ = o.Close()
	e = f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Unlink, Parent: root, Name: "orphan"}), uid(9))
	o = f.observation()
	image, err := o.Image(t.Context(), root.Home)
	if err != nil {
		t.Fatal(err)
	}
	object, err := o.Stat(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	_ = o.Close()
	claim := uid(3100)
	if err = f.view.ReserveIdentity(t.Context(), claim, fsapi.ObjectID{}); err != nil {
		t.Fatal(err)
	}
	position := image.Position
	position.Sequence++
	reclaim := storage.Operation{ID: claim, Position: position, Kind: storage.Reclaim, Object: id, ExpectedRevision: object.Attr.Revision, ExpectedLedgerRevision: object.LedgerRevision, Participants: []fsapi.UUID{root.Home}}
	stage := logicalview.Stage{Slot: e.Selected.Slot, Previous: &e.Selected, Origin: e.Sources[0].Stream, OriginLaunch: e.Sources[0].Holder, Recipient: f.config.Authority.Sequencer, Claim: claim, Intent: []byte("verified home retirement request"), Operations: []storage.Operation{reclaim}}
	stage.Slot.Serial++
	if _, err = f.view.Stage(t.Context(), stage); !errors.Is(err, fsapi.Busy) {
		t.Fatal("live description did not pin orphan", err)
	}
	f.verifier.fail = logicalview.HandleRetirement
	if err = f.view.ReleaseHandle(t.Context(), f.view.Generation(), opened.Handle); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("failed pin retirement dropped handle", err)
	}
	if _, _, err = f.view.ResolveHandle(f.view.Generation(), opened.Handle); err != nil {
		t.Fatal(err)
	}
	f.verifier.fail = 0
	if err = f.view.ReleaseHandle(t.Context(), f.view.Generation(), opened.Handle); err != nil {
		t.Fatal(err)
	}
	if _, err = f.view.Stage(t.Context(), stage); err != nil {
		t.Fatal(err)
	}
	o = f.observation()
	defer func() { _ = o.Close() }()
	if _, err = o.Open(t.Context(), id, fsapi.OpenFlags{Access: fsapi.ReadWrite}); !errors.Is(err, fsapi.Busy) {
		t.Fatal("late open bypassed frozen reclaim", err)
	}
}

func TestLogicalDefaultArenasFitDefaultAdmission(t *testing.T) {
	f := newLogicalFixture(t)
	config := f.config
	config.Limits = logicalview.Limits{}
	budget, err := admission.New(admission.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	config.Budget = budget
	view, err := logicalview.New(t.Context(), config)
	if err != nil {
		t.Fatal("default view profile cannot reserve default admission", err)
	}
	if err = view.Close(); err != nil {
		t.Fatal(err)
	}
	if usage := budget.Usage(); usage.Used.Bytes != 0 || usage.Used.OwnerRAM != 0 {
		t.Fatal("closed local arena leaked accounting", usage)
	}
}

func TestLogicalPrecloneBoundsRejectOversizedCommandsAndEvidence(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	o := f.observation()
	for i, test := range []struct {
		name    string
		cred    fsapi.Cred
		command fsapi.Command
		want    error
	}{
		{"payload", cred, fsapi.Command{Operation: fsapi.Write, Data: make([]byte, 4097)}, fsapi.Invalid},
		{"credentials", fsapi.Cred{Groups: make([]uint32, 1025)}, fsapi.Command{Operation: fsapi.Getattr, Object: root}, fsapi.Invalid},
		{"name", cred, fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: string(make([]byte, 256))}, fsapi.NameTooLong},
		{"cursor", cred, fsapi.Command{Operation: fsapi.Readdir, Object: root, After: string(make([]byte, 256))}, fsapi.NameTooLong},
		{"operation", cred, fsapi.Command{Operation: 255}, fsapi.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := test.command
			c.Mount = root.Home
			claim := uid(4000 + uint64(i))
			if _, err := f.planner.Plan(t.Context(), o, test.cred, c, claim, fsapi.ObjectID{}); !errors.Is(err, test.want) {
				t.Fatal(err)
			}
			if _, err := f.planner.Observe(t.Context(), o, test.cred, c); !errors.Is(err, test.want) {
				t.Fatal(err)
			}
			if err := f.view.ReserveIdentity(t.Context(), claim, fsapi.ObjectID{}); err != nil {
				t.Fatal("rejected command consumed planning identity", err)
			}
		})
	}
	_ = o.Close()
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "bounded", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	g, digest := f.stage(p, uid(11))
	e := f.protect(p, g, digest, false)
	for _, mutate := range []func(*logicalview.Evidence){
		func(e *logicalview.Evidence) { e.Acks = make([]logicalview.Domain, f.config.Limits.Dependencies+1) },
		func(e *logicalview.Evidence) { e.Claims = make([]fsapi.UUID, f.config.Limits.Claims+1) },
		func(e *logicalview.Evidence) {
			e.Sources = append([]record.SourceCoverage(nil), e.Sources...)
			e.Sources[0].Dependencies = make([]record.NormalRef, f.config.Limits.Dependencies+1)
		},
		func(e *logicalview.Evidence) {
			source := e.Sources[0]
			source.Dependencies = make([]record.NormalRef, 128)
			e.Sources = []record.SourceCoverage{source, source}
		},
	} {
		bad := e
		mutate(&bad)
		if err := f.view.Select(t.Context(), bad); !errors.Is(err, fsapi.NoSpace) {
			t.Fatal("oversized nested evidence accepted", err)
		}
	}
	// A real durable dependency under the aggregate cap remains admissible.
	for ref := range f.verifier.origins[g.Origin].frames {
		if ref.Kind == record.NormalFragmentKind {
			e.Sources = append([]record.SourceCoverage(nil), e.Sources...)
			e.Sources[0].Dependencies = []record.NormalRef{ref}
			break
		}
	}
	if len(e.Sources[0].Dependencies) != 1 {
		t.Fatal("missing real nested dependency")
	}
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	before := f.config.Budget.Usage().Used
	config := f.config
	config.Authority.Admitted = make([]fsapi.UUID, 4097)
	if _, err := logicalview.New(t.Context(), config); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("oversized authority admitted", err)
	}
	if after := f.config.Budget.Usage().Used; after != before {
		t.Fatal("rejected authority leaked reservation", before, after)
	}
}

func TestLogicalRecoveredPinsCannotRetireLiveTokens(t *testing.T) {
	f := newLogicalFixture(t)
	proof := []byte("verified durable replacement")
	original, err := f.view.PinVersion(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.view.ReleaseRecoveredPins(t.Context(), nil, 1, proof); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("recovery release consumed live original token", err)
	}
	var data bytes.Buffer
	if err = f.view.WriteRecovery(t.Context(), &data); err != nil {
		t.Fatal(err)
	}
	config := f.config
	budget, err := admission.New(logicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	config.Budget = budget
	recovered, err := logicalview.Restore(t.Context(), config, bytes.NewReader(data.Bytes()), []byte("verified full source recovery"))
	if err != nil {
		t.Fatal(err)
	}
	live, err := recovered.PinVersion(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = recovered.ReleaseRecoveredPins(t.Context(), nil, 2, proof); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("inherited release erased new live ownership", err)
	}
	if err = recovered.ReleaseRecoveredPins(t.Context(), nil, 1, proof); err != nil {
		t.Fatal(err)
	}
	if err = recovered.Close(); !errors.Is(err, fsapi.Busy) {
		t.Fatal("live token no longer pins version", err)
	}
	live.Release()
	live.Release()
	if err = recovered.Close(); err != nil {
		t.Fatal("independent token release failed", err)
	}
	original.Release()
	var pins []*logicalview.VersionPin
	for i := 0; i < f.config.Limits.Identities; i++ {
		pin, err := f.view.PinVersion(nil)
		if err != nil {
			t.Fatal(err)
		}
		pins = append(pins, pin)
	}
	if _, err = f.view.PinVersion(nil); !errors.Is(err, fsapi.NoSpace) {
		t.Fatal("unbounded version-pin tokens", err)
	}
	for _, pin := range pins {
		pin.Release()
	}
	if err = f.view.Close(); err != nil {
		t.Fatal("released pin count leaked", err)
	}
}

func TestLogicalPlannerRejectsUnencodableIOProfile(t *testing.T) {
	f := newLogicalFixture(t)
	policy, err := NewUnixPermissions(uid(419))
	if err != nil {
		t.Fatal(err)
	}
	before := f.config.Budget.Usage().Used
	if _, err = NewLogicalPlanner(LogicalConfig{View: f.view, Permissions: policy, Limits: Limits{IOBytes: (1 << 20) + 1}}); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("planner accepted IO beyond existing record.Write bound", err)
	}
	// Rejection precedes the smaller view-budget clamp; it is not hidden by
	// this fixture's conservative per-request capacity.
	if after := f.config.Budget.Usage().Used; after != before {
		t.Fatal("invalid IO profile changed reservations")
	}
	if _, err = NewLogicalPlanner(LogicalConfig{View: f.view, Permissions: policy, Limits: Limits{IOBytes: 1 << 20}}); err != nil {
		t.Fatal("existing codec IO bound rejected", err)
	}
}
