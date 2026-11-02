package fsservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type logicalFixture struct {
	t        *testing.T
	local    *fixture
	config   logicalview.Config
	view     *logicalview.Store
	planner  *LogicalPlanner
	verifier *logicalVerifier
	next     uint64
}
type durableOrigin struct {
	frames   map[record.NormalRef][]byte
	source   *segment.OriginSource
	sequence uint64
	stream   record.OriginStream
	launch   record.Launch
}
type logicalVerifier struct {
	t        *testing.T
	origins  map[record.OriginStream]*durableOrigin
	retained map[record.NormalRef][]byte
	expected map[record.Hash]logicalview.Evidence
	fail     logicalview.Phase
}

func newLogicalFixture(t *testing.T) *logicalFixture {
	t.Helper()
	local := setup(t, "sim")
	local.start()
	local.create(local.c.Storage.Root, "base")
	baseline, err := local.service.applied.Store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	a := record.NormalAuthority{Cluster: uid(410), Registry: uid(411), RegistryRevision: 1, RecoveryGeneration: uid(412), Closure: uid(413), ManifestVersion: 1, Admitted: []fsapi.UUID{uid(6), uid(8), uid(9), uid(11)}, Volumes: []record.NormalVolume{{Volume: local.c.Storage.Volume, Generation: local.c.Stream.Generation, Configuration: local.c.Stream.Configuration, Epoch: 1, Replicas: []fsapi.UUID{uid(6), uid(8), uid(9)}}}, Sequencer: record.Launch{Node: uid(6), Incarnation: uid(7)}, OwnerPartner: record.Launch{Node: uid(8), Incarnation: uid(10)}, FenceLineage: []record.Hash{sha256.Sum256([]byte("fixture positive exclusion"))}}
	budget, err := admission.New(logicalBudget())
	if err != nil {
		t.Fatal(err)
	}
	verifier := &logicalVerifier{t: t, origins: map[record.OriginStream]*durableOrigin{}, retained: map[record.NormalRef][]byte{}, expected: map[record.Hash]logicalview.Evidence{}}
	config := logicalview.Config{Authority: a, Generation: uid(414), Baseline: []storage.Snapshot{baseline}, Domains: []logicalview.Domain{{ID: uid(415), Session: uid(416)}, {ID: uid(417), Session: uid(418)}}, Budget: budget, Verifier: verifier, Limits: logicalview.Limits{MaxSnapshotBytes: 64 << 10, MaxClaimBytes: 64 << 10, Versions: 32, Claims: 32, Readers: 4, Identities: 256, Dependencies: 256}}
	view, err := logicalview.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := NewUnixPermissions(uid(419))
	if err != nil {
		t.Fatal(err)
	}
	planner, err := NewLogicalPlanner(LogicalConfig{View: view, Permissions: permissions, Clock: fixedClock{}, Limits: Limits{IOBytes: 4096, LogicalBytes: 32 << 10}})
	if err != nil {
		t.Fatal(err)
	}
	return &logicalFixture{t: t, local: local, config: config, view: view, planner: planner, verifier: verifier, next: 500}
}
func (v *logicalVerifier) Check(ctx context.Context, c logicalview.Check) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.fail == c.Phase {
		return fsapi.Unavailable
	}
	switch c.Phase {
	case logicalview.Baseline:
		if len(c.Baseline) == 0 {
			return fsapi.Unavailable
		}
		for _, s := range c.Baseline {
			if err := storage.ValidateSnapshot(s, storage.Limits{}, 1<<20); err != nil {
				return err
			}
		}
	case logicalview.Candidate:
		for _, dependency := range c.Stage.Dependencies {
			origin := v.origins[dependency.Ref.Stream]
			if origin == nil {
				return fsapi.Unavailable
			}
			raw, err := origin.source.Read(ctx, dependency.Ref)
			if err != nil || !bytes.Equal(raw, dependency.Bytes) {
				return fsapi.Stale
			}
		}
	case logicalview.Selection, logicalview.Publication:
		expected, ok := v.expected[c.Evidence.Digest]
		if !ok || expected.Selected != c.Evidence.Selected || expected.NoEffect != c.Evidence.NoEffect || !bytes.Equal(expected.Result, c.Evidence.Result) {
			return fsapi.Unavailable
		}
		for _, source := range c.Evidence.Sources {
			origin := v.origins[source.Stream]
			if origin == nil {
				return fsapi.Unavailable
			}
			if _, err := origin.source.Read(ctx, source.Last); err != nil {
				return err
			}
			for _, dependency := range source.Dependencies {
				holder := v.origins[dependency.Stream]
				if holder == nil {
					return fsapi.Unavailable
				}
				if _, err := holder.source.Read(ctx, dependency); err != nil {
					return err
				}
			}
		}
		for _, acceptance := range c.Evidence.Acceptances {
			if len(v.retained[acceptance]) == 0 {
				return fsapi.Unavailable
			}
		}
	case logicalview.Recovery:
		if c.Recovery == nil || !bytes.Equal(c.Evidence.Proof, []byte("verified full source recovery")) {
			return fsapi.Unavailable
		}
	case logicalview.Retirement, logicalview.BackingInstalled, logicalview.BackingDurable:
		if !bytes.Equal(c.Evidence.Proof, []byte("verified durable replacement")) {
			return fsapi.Unavailable
		}
	}
	return nil
}
func (f *logicalFixture) observation() *logicalview.Observation {
	f.t.Helper()
	o, err := f.view.Begin(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return o
}
func (f *logicalFixture) observe(cred fsapi.Cred, c fsapi.Command) fsapi.Result {
	f.t.Helper()
	c.Mount = f.local.c.Storage.Volume
	if c.Handle != 0 && !c.Generation.Valid() {
		c.Generation = f.view.Generation()
	}
	o := f.observation()
	defer func() { _ = o.Close() }()
	r, err := f.planner.Observe(f.t.Context(), o, cred, c)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}
func (f *logicalFixture) plan(cred fsapi.Cred, c fsapi.Command) Planned {
	f.t.Helper()
	f.next++
	c.Mount = f.local.c.Storage.Volume
	if c.Handle != 0 && !c.Generation.Valid() {
		c.Generation = f.view.Generation()
	}
	if c.Operation == fsapi.LinkOp || c.Operation == fsapi.RenameOp {
		c.DestinationMount = c.Mount
	}
	allocation := fsapi.ObjectID{}
	if c.Operation == fsapi.Create || c.Operation == fsapi.Mkdir || c.Operation == fsapi.SymlinkOp {
		allocation = fsapi.ObjectID{Home: c.Mount, Allocation: uid(f.next + 10000)}
	}
	o := f.observation()
	defer func() { _ = o.Close() }()
	p, err := f.planner.Plan(f.t.Context(), o, cred, c, uid(f.next), allocation)
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}
func (f *logicalFixture) stage(p Planned, node fsapi.UUID) (logicalview.Stage, record.Hash) {
	f.t.Helper()
	a := f.config.Authority
	hash, err := record.NormalAuthorityHash(a)
	if err != nil {
		f.t.Fatal(err)
	}
	serial := uint64(1)
	if p.Cut != nil {
		serial = p.Cut.Slot.Serial + 1
	}
	origin := record.OriginStream{Cluster: a.Cluster, Volume: f.local.c.Storage.Volume, Generation: f.local.c.Stream.Generation, Configuration: f.local.c.Stream.Configuration, Epoch: 1, Origin: node, Journal: uid(600 + uint64(node[15]))}
	launch := record.Launch{Node: node, Incarnation: uid(700 + uint64(node[15]))}
	recipient := a.Sequencer
	if node == a.Sequencer.Node {
		launch = a.Sequencer
		recipient = a.OwnerPartner
	}
	g := logicalview.Stage{Slot: record.OrderedSlot{Authority: hash, Sequencer: a.Sequencer, Serial: serial}, Previous: p.Cut, Origin: origin, OriginLaunch: launch, Recipient: recipient, Claim: p.Claim, Intent: p.Intent, Operations: []storage.Operation{p.Operation}}
	for _, claim := range f.view.Claims() {
		if claim.Evidence != nil {
			for _, source := range claim.Evidence.Sources {
				raw, err := f.verifier.origins[source.Stream].source.Read(f.t.Context(), source.Last)
				if err != nil {
					f.t.Fatal(err)
				}
				g.Dependencies = append(g.Dependencies, logicalview.Dependency{Ref: source.Last, Bytes: raw})
			}
		}
	}
	digest, err := f.view.Stage(f.t.Context(), g)
	if err != nil {
		f.t.Fatal(err)
	}
	return g, digest
}
func (f *logicalFixture) origin(g logicalview.Stage) *durableOrigin {
	f.t.Helper()
	if origin := f.verifier.origins[g.Origin]; origin != nil {
		return origin
	}
	backend, err := vfs.NewSimulator(vfs.Config{})
	if err != nil {
		f.t.Fatal(err)
	}
	dir := "."
	for _, part := range strings.Split(segment.OriginDirectory(g.Origin), "/") {
		next := path.Join(dir, part)
		if err = backend.Mkdir(f.t.Context(), next, 0o700); err != nil {
			f.t.Fatal(err)
		}
		if err = backend.SyncDir(f.t.Context(), dir); err != nil {
			f.t.Fatal(err)
		}
		dir = next
	}
	source, err := segment.OpenOriginSource(f.t.Context(), backend, g.Origin, segment.Limits{})
	if err != nil {
		f.t.Fatal(err)
	}
	origin := &durableOrigin{source: source, stream: g.Origin, launch: g.OriginLaunch, frames: map[record.NormalRef][]byte{}}
	f.verifier.origins[g.Origin] = origin
	return origin
}
func (f *logicalFixture) protect(p Planned, g logicalview.Stage, digest record.Hash, noEffect bool) logicalview.Evidence {
	f.t.Helper()
	origin := f.origin(g)
	a := f.config.Authority
	astream := g.Origin
	astream.Origin = a.Sequencer.Node
	astream.Journal = uid(800)
	mstream := g.Origin
	mstream.Origin = g.Recipient.Node
	mstream.Journal = uid(801 + uint64(g.Recipient.Node[15]))
	counter := g.Slot.Serial * 10
	encode := func(stream record.OriginStream, creator record.Launch, seq uint64, payload any) ([]byte, record.NormalRef) {
		raw, err := record.EncodeNormal(record.NormalRecord{Stream: stream, Creator: creator, Sequence: seq, Operation: g.Claim, Payload: payload})
		if err != nil {
			f.t.Fatal(err)
		}
		ref, err := record.NormalReference(raw)
		if err != nil {
			f.t.Fatal(err)
		}
		return raw, ref
	}
	retain := func(raw []byte, ref record.NormalRef) { f.verifier.retained[ref] = bytes.Clone(raw) }
	appendOrigin := func(payload any) ([]byte, record.NormalRef) {
		origin.sequence++
		raw, ref := encode(origin.stream, origin.launch, origin.sequence, payload)
		got, err := origin.source.Append(f.t.Context(), raw)
		if err != nil || got != ref {
			f.t.Fatalf("durable origin append: %v", err)
		}
		origin.frames[ref] = bytes.Clone(raw)
		return raw, ref
	}
	cred := p.Mutation.Authorizations[0].Credentials
	document := record.RequestDocument{Class: 2, Internal: &record.InternalRequest{Version: 1, ID: fsapi.TransactionID{Session: fsapi.SessionID{Principal: fsapi.Principal{Realm: uid(900), Subject: "fixture"}, Anchor: g.Origin.Volume, Nonce: uid(901)}, Number: g.Slot.Serial}, NamespaceVolume: g.Origin.Volume, View: uid(419), Intent: p.Mutation.Intent}}
	grant := record.NormalGrant{Authority: a, Slot: g.Slot, Predecessor: g.Previous, Baseline: sha256.Sum256([]byte("verified local baseline")), Origin: g.Origin, OriginLaunch: g.OriginLaunch, Recipient: g.Recipient, Operation: g.Claim, Document: document, Credentials: cred, Mutation: p.Mutation, Result: record.GrantResult{AcceptedBytes: uint64(p.Result.Count)}, NoEffectResult: record.GrantResult{Code: 1}}
	grantRaw, grantRef := encode(astream, a.Sequencer, counter+1, grant)
	retain(grantRaw, grantRef)
	_, fragment := appendOrigin(record.OriginFragment{GrantBytes: grantRaw})
	preparedRaw, prepared := encode(mstream, g.Recipient, counter+2, record.MemoryAcceptance{Grant: grantRef, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 1, Target: grantRef, Dependencies: []record.NormalRef{grantRef}})
	retain(preparedRaw, prepared)
	choiceValue := uint8(1)
	preparedPtr := &prepared
	if noEffect {
		choiceValue = 2
		preparedPtr = nil
	}
	_, choice := appendOrigin(record.OriginChoice{Grant: grantRef, Slot: g.Slot, Choice: choiceValue, Prepared: preparedPtr, Fragment: []record.NormalRef{fragment}})
	acceptRaw, accepted := encode(mstream, g.Recipient, counter+3, record.MemoryAcceptance{Grant: grantRef, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 2, Target: choice, DurableThrough: choice.Sequence, Dependencies: []record.NormalRef{choice}})
	retain(acceptRaw, accepted)
	selectedRaw, selected := encode(astream, a.Sequencer, counter+4, record.SlotSelection{Grant: grantRef, Slot: g.Slot, Predecessor: g.Previous, Outcome: choiceValue, Choice: choice, Acceptance: accepted, Volumes: []fsapi.UUID{g.Origin.Volume}})
	retain(selectedRaw, selected)
	cut := record.SelectedRef{Slot: g.Slot, Certificate: selected}
	value := grant.Result
	if noEffect {
		value = grant.NoEffectResult
	}
	resultRaw, result := appendOrigin(record.NormalResult{Grant: grantRef, Selected: cut, Value: value})
	resultAcceptRaw, resultAccepted := encode(mstream, g.Recipient, counter+5, record.MemoryAcceptance{Grant: grantRef, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 3, Target: result, DurableThrough: result.Sequence, Dependencies: []record.NormalRef{result}})
	retain(resultAcceptRaw, resultAccepted)
	evidence := logicalview.Evidence{Digest: digest, Selected: cut, Sources: []record.SourceCoverage{{Stream: g.Origin, Holder: g.OriginLaunch, Through: result.Sequence, Last: result}}, Acceptances: []record.NormalRef{accepted, resultAccepted}, Result: resultRaw, Proof: []byte("local source sync and owned acceptance fixture"), Acks: f.view.Domains(), NoEffect: noEffect}
	f.verifier.expected[digest] = evidence
	return evidence
}
func (f *logicalFixture) commit(p Planned, node fsapi.UUID) logicalview.Evidence {
	f.t.Helper()
	g, digest := f.stage(p, node)
	e := f.protect(p, g, digest, false)
	if err := f.view.Select(f.t.Context(), e); err != nil {
		f.t.Fatal(err)
	}
	if err := f.view.Publish(f.t.Context(), e); err != nil {
		f.t.Fatal(err)
	}
	return e
}
func logicalCred() fsapi.Cred { return fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}} }

func TestLogicalOwnerRAMBeforeDiskAndConflictingOrigins(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	created := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "ram", Mode: 0o640, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	f.commit(created, uid(11)) // admitted origin outside R
	id := created.Operation.Object
	if _, err := f.local.service.applied.Store.Stat(t.Context(), id); !errors.Is(err, fsapi.NotFound) {
		t.Fatal("owner backing unexpectedly changed", err)
	}
	opened := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Append: true}})
	first := f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: opened.Handle, Data: []byte("ab")})
	f.commit(first, uid(11))
	second := f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: opened.Handle, Data: []byte("cd")})
	if second.Operation.Offset != 2 {
		t.Fatal("append resolved against stale backing", second.Operation.Offset)
	}
	f.commit(second, uid(9))
	got := f.observe(cred, fsapi.Command{Operation: fsapi.Read, Handle: opened.Handle, Size: 16})
	if string(got.Data) != "abcd" {
		t.Fatalf("RAM read=%q", got.Data)
	}
	frontier := f.view.Frontiers()
	if frontier.Visible == nil || frontier.Visible.Slot.Serial != 3 || frontier.Installed != nil || frontier.Durable != nil {
		t.Fatal(frontier)
	}
	// The owner has not authored a durable origin stream for any remote operation.
	for stream := range f.verifier.origins {
		if stream.Origin == f.config.Authority.Sequencer.Node {
			t.Fatal("remote write moved to owner durable log")
		}
	}
	denied := uint16(0)
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: fsapi.Attributes{Mode: &denied}}), uid(9))
	// Existing descriptions keep their access grant, while a new open sees chmod.
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: opened.Handle, Data: []byte("e")}), uid(11))
	o := f.observation()
	_, err := f.planner.Observe(t.Context(), o, cred, fsapi.Command{Mount: root.Home, Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	_ = o.Close()
	if !errors.Is(err, fsapi.Permission) {
		t.Fatal("stale permission metadata", err)
	}
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Unlink, Parent: root, Name: "ram"}), uid(11))
	got = f.observe(cred, fsapi.Command{Operation: fsapi.Read, Handle: opened.Handle, Size: 16})
	if string(got.Data) != "abcde" {
		t.Fatal("open-unlinked handle lost", got.Data)
	}
}

func TestLogicalCandidateFailureNoEffectAndBarriers(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "hidden", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	g, digest := f.stage(p, uid(11))
	e := f.protect(p, g, digest, false)
	o := f.observation()
	if _, err := o.Lookup(t.Context(), root, "hidden"); !errors.Is(err, fsapi.NotFound) {
		t.Fatal("staged candidate exposed", err)
	}
	f.verifier.fail = logicalview.Selection
	if err := f.view.Select(t.Context(), e); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal(err)
	}
	if _, err := o.Lookup(t.Context(), root, "hidden"); !errors.Is(err, fsapi.NotFound) {
		t.Fatal(err)
	}
	f.verifier.fail = 0
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if err := f.view.Publish(t.Context(), e); !errors.Is(err, fsapi.Busy) {
		t.Fatal("active observation ignored", err)
	}
	_ = o.Close()
	wrong := e
	wrong.Acks = wrong.Acks[:1]
	if err := f.view.Publish(t.Context(), wrong); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("missing domain acknowledgement", err)
	}
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	f.observe(cred, fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "hidden"})
	// An irrevocable durable DECLINE resolves a different candidate with no effect.
	p = f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "declined", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	g, digest = f.stage(p, uid(9))
	e = f.protect(p, g, digest, true)
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	o = f.observation()
	defer func() { _ = o.Close() }()
	if _, err := o.Lookup(t.Context(), root, "declined"); !errors.Is(err, fsapi.NotFound) {
		t.Fatal("NO_EFFECT installed candidate", err)
	}
}

func TestLogicalSparseTruncateLinksNamespaceAndOwnerOrigin(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "file", Mode: 0o640, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	f.commit(p, uid(6))
	id := p.Operation.Object
	h := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}}).Handle
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: h, Offset: 5, Data: []byte("abc")}), uid(11))
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Truncate, Handle: h, Size: 6}), uid(9))
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Truncate, Handle: h, Size: 9}), uid(11))
	got := f.observe(cred, fsapi.Command{Operation: fsapi.Read, Handle: h, Size: 16})
	if !bytes.Equal(got.Data, []byte{0, 0, 0, 0, 0, 'a', 0, 0, 0}) {
		t.Fatalf("truncate resurrected bytes: %v", got.Data)
	}
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.LinkOp, Object: id, Destination: root, NewName: "alias"}), uid(9))
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "alias", Destination: root, NewName: "renamed"}), uid(11))
	got = f.observe(cred, fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "renamed"})
	if got.Attr.Object != id || got.Attr.Nlink != 2 {
		t.Fatal(got.Attr)
	}
	dir := f.plan(cred, fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "dir", Mode: 0o750})
	f.commit(dir, uid(9))
	f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.SymlinkOp, Parent: dir.Operation.Object, Name: "sym", Data: []byte("../renamed")}), uid(11))
	listing := f.observe(cred, fsapi.Command{Operation: fsapi.Readdir, Object: root, Limit: 32})
	if len(listing.Entries) != 4 {
		t.Fatal(listing.Entries)
	}
	// Owner-origin protection actually used its separate configured B recipient.
	found := false
	for _, claim := range f.view.Claims() {
		if claim.Stage.Origin.Origin == uid(6) {
			found = true
			if claim.Stage.Recipient != f.config.Authority.OwnerPartner {
				t.Fatal("owner counted itself")
			}
		}
	}
	if !found {
		t.Fatal("owner-origin case absent")
	}
}

func TestLogicalRecoveryPreservesSelectedAndUnresolvedState(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncertain", true: "selected unpublished"}[selected], func(t *testing.T) {
			f := newLogicalFixture(t)
			cred := logicalCred()
			root := f.local.c.Storage.Root
			p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "pending", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
			g, digest := f.stage(p, uid(11))
			e := f.protect(p, g, digest, false)
			if selected {
				if err := f.view.Select(t.Context(), e); err != nil {
					t.Fatal(err)
				}
			}
			var data bytes.Buffer
			if err := f.view.WriteRecovery(t.Context(), &data); err != nil {
				t.Fatal(err)
			}
			budget, err := admission.New(logicalBudget())
			if err != nil {
				t.Fatal(err)
			}
			config := f.config
			config.Budget = budget
			restored, err := logicalview.Restore(t.Context(), config, bytes.NewReader(data.Bytes()), []byte("verified full source recovery"))
			if err != nil {
				t.Fatal(err)
			}
			o, err := restored.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = o.Lookup(t.Context(), root, "pending"); !errors.Is(err, fsapi.NotFound) {
				t.Fatal("recovery exposed unpublished candidate", err)
			}
			_ = o.Close()
			if !selected {
				if err = restored.Select(t.Context(), e); err != nil {
					t.Fatal(err)
				}
			}
			if err = restored.Publish(t.Context(), e); err != nil {
				t.Fatal(err)
			}
			o, err = restored.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = o.Lookup(t.Context(), root, "pending"); err != nil {
				t.Fatal(err)
			}
			_ = o.Close()
			if _, err = logicalview.Restore(t.Context(), config, bytes.NewReader(data.Bytes()[:data.Len()/2]), []byte("verified full source recovery")); err == nil {
				t.Fatal("truncated recovery accepted")
			}
		})
	}
}

func TestLogicalIdentityBoundsStaleHandlesAndDiskIndependence(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	root := f.local.c.Storage.Root
	// Pure planning still works with the original backing service fully closed.
	if err := f.local.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.local.service = nil
	p := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "nodisk", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	e := f.commit(p, uid(11))
	h := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: p.Operation.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}}).Handle
	o := f.observation()
	_, err := f.planner.Observe(t.Context(), o, cred, fsapi.Command{Mount: root.Home, Generation: uid(999), Operation: fsapi.Read, Handle: h, Size: 1})
	_ = o.Close()
	if !errors.Is(err, fsapi.Stale) {
		t.Fatal(err)
	}
	if err = f.view.ReserveIdentity(t.Context(), p.Claim, fsapi.ObjectID{}); !errors.Is(err, fsapi.Stale) {
		t.Fatal("reused operation", err)
	}
	proof := []byte("verified durable replacement")
	if err = f.view.MarkBacking(t.Context(), e.Selected, true, proof); !errors.Is(err, fsapi.Stale) {
		t.Fatal("durable exceeded installed", err)
	}
	if err = f.view.MarkBacking(t.Context(), e.Selected, false, proof); err != nil {
		t.Fatal(err)
	}
	if err = f.view.MarkBacking(t.Context(), e.Selected, true, proof); err != nil {
		t.Fatal(err)
	}
	if err = f.view.Retire(t.Context(), e.Selected, []fsapi.UUID{p.Claim}, nil); err == nil {
		t.Fatal("unproven source release")
	}
	if err = f.view.Retire(t.Context(), e.Selected, []fsapi.UUID{p.Claim}, proof); err != nil {
		t.Fatal(err)
	}
	if err = f.view.ReserveIdentity(t.Context(), p.Claim, fsapi.ObjectID{}); !errors.Is(err, fsapi.Stale) {
		t.Fatal("retired claim reused", err)
	}
	huge := f.plan(cred, fsapi.Command{Operation: fsapi.Truncate, Handle: h, Size: 32 << 10})
	stage := logicalview.Stage{Slot: record.OrderedSlot{Authority: e.Selected.Slot.Authority, Sequencer: f.config.Authority.Sequencer, Serial: 2}, Previous: &e.Selected, Origin: f.verifier.origins[e.Sources[0].Stream].stream, OriginLaunch: e.Sources[0].Holder, Recipient: f.config.Authority.Sequencer, Claim: huge.Claim, Intent: huge.Intent, Operations: []storage.Operation{huge.Operation}}
	// An overlarge expansion is rejected by the view, before allocating the hole.
	stage.Operations[0].Objects[0].After.Attr.Size = 1 << 40
	stage.Operations[0].Size = 1 << 40
	if _, err = f.view.Stage(t.Context(), stage); err == nil {
		t.Fatal("unbounded hole accepted")
	}
	if err = f.view.ReleaseHandle(t.Context(), f.view.Generation(), h); err != nil {
		t.Fatal(err)
	}
	if err = f.view.Close(); err != nil {
		t.Fatal(err)
	}
}

var _ logicalview.Verifier = (*logicalVerifier)(nil)

func logicalBudget() admission.Config {
	config := admission.DefaultConfig()
	pool := &config.Pools[admission.Recovery]
	pool.Global.Bytes = 128 << 20
	pool.Global.OwnerRAM = 128 << 20
	pool.PerPeer.Bytes = 128 << 20
	pool.PerPeer.OwnerRAM = 128 << 20
	pool.PerVolume.Bytes = 64 << 20
	pool.PerVolume.OwnerRAM = 64 << 20
	return config
}
