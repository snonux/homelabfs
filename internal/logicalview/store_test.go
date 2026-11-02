package logicalview_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

type verifierFunc func(context.Context, logicalview.Check) error

func (f verifierFunc) Check(ctx context.Context, c logicalview.Check) error { return f(ctx, c) }

type viewFixture struct {
	config     logicalview.Config
	view       *logicalview.Store
	root, file fsapi.ObjectID
}

func newViewFixture(t *testing.T) *viewFixture {
	t.Helper()
	c, root, file := viewConfig(t)
	v, err := logicalview.New(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	return &viewFixture{config: c, view: v, root: root, file: file}
}

func viewConfig(t *testing.T) (logicalview.Config, fsapi.ObjectID, fsapi.ObjectID) {
	t.Helper()
	volume := uid(1)
	root := fsapi.ObjectID{Home: volume, Allocation: uid(2)}
	file := fsapi.ObjectID{Home: volume, Allocation: uid(3)}
	position := fsapi.Position{Volume: volume, Generation: uid(4), Configuration: uid(5), Epoch: 1}
	data := []byte("base")
	baseline := storage.Snapshot{Image: storage.Image{Version: 1, Volume: volume, Root: root, Position: position,
		Objects: []storage.Object{
			{Descriptor: storage.Descriptor{Object: root, Kind: fsapi.Directory, Creation: uid(6)}, Attr: fsapi.Attr{Object: root, Kind: fsapi.Directory, Mode: 0o700, Nlink: 2, Revision: 1}, LedgerRevision: 1, Parent: &storage.Parent{Object: root, Revision: 1}},
			{Descriptor: storage.Descriptor{Object: file, Kind: fsapi.Regular, Creation: uid(7)}, Attr: fsapi.Attr{Object: file, Kind: fsapi.Regular, Mode: 0o600, Size: int64(len(data)), Nlink: 1, Revision: 1}, LedgerRevision: 1, References: []storage.Reference{{Parent: root, Name: "base", Revision: 1}}},
		}, Entries: []storage.Entry{{Parent: root, Name: "base", Revision: 1, Target: &file}}, Payloads: []storage.Payload{{Object: file, Size: int64(len(data)), SHA256: sha256.Sum256(data)}}}, Data: [][]byte{data}}
	budget, err := admission.New(admission.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	authority := record.NormalAuthority{Cluster: uid(10), Registry: uid(11), RegistryRevision: 1, RecoveryGeneration: uid(12), Closure: uid(13), ManifestVersion: 1, Admitted: []fsapi.UUID{uid(20), uid(21), uid(22)}, Volumes: []record.NormalVolume{{Volume: volume, Generation: position.Generation, Configuration: position.Configuration, Epoch: 1, Replicas: []fsapi.UUID{uid(20), uid(21), uid(22)}}}, Sequencer: record.Launch{Node: uid(20), Incarnation: uid(23)}, OwnerPartner: record.Launch{Node: uid(21), Incarnation: uid(24)}, FenceLineage: []record.Hash{sha256.Sum256([]byte("test exclusion"))}}
	// The verifier is an external authority seam, not a simulated replication
	// protocol. These tests exercise the component's own state and validation.
	config := logicalview.Config{Authority: authority, Generation: uid(30), Baseline: []storage.Snapshot{baseline}, Domains: []logicalview.Domain{{ID: uid(31), Session: uid(32)}, {ID: uid(33), Session: uid(34)}}, Budget: budget, Verifier: verifierFunc(func(ctx context.Context, _ logicalview.Check) error { return ctx.Err() }), Limits: logicalview.Limits{MaxSnapshotBytes: 16 << 10, MaxClaimBytes: 16 << 10, Versions: 8, Claims: 8, Handles: 4, Readers: 2, Dependencies: 8, Identities: 32}}
	return config, root, file
}

func uid(n byte) fsapi.UUID {
	var id fsapi.UUID
	id[6], id[8], id[15] = 0x40, 0x80, n
	return id
}

func (f *viewFixture) stage(t *testing.T, id byte, data []byte) logicalview.Stage {
	t.Helper()
	o, err := f.view.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := o.Close(); err != nil {
			t.Error(err)
		}
	}()
	object, err := o.Stat(t.Context(), f.file)
	if err != nil {
		t.Fatal(err)
	}
	image, err := o.Image(t.Context(), f.file.Home)
	if err != nil {
		t.Fatal(err)
	}
	cut, err := o.Cut()
	if err != nil {
		t.Fatal(err)
	}
	serial := uint64(1)
	if cut != nil {
		serial = cut.Slot.Serial + 1
	}
	image.Position.Sequence++
	before := object.Attr.Revision
	object.Attr.Revision++
	object.Attr.Size = max(object.Attr.Size, int64(len(data)))
	a := f.config.Authority
	hash, err := record.NormalAuthorityHash(a)
	if err != nil {
		t.Fatal(err)
	}
	return logicalview.Stage{Slot: record.OrderedSlot{Authority: hash, Sequencer: a.Sequencer, Serial: serial}, Previous: cut, Origin: record.OriginStream{Cluster: a.Cluster, Volume: image.Volume, Generation: image.Position.Generation, Configuration: image.Position.Configuration, Epoch: 1, Origin: a.Sequencer.Node, Journal: uid(40)}, OriginLaunch: a.Sequencer, Recipient: a.OwnerPartner, Claim: uid(id), Intent: []byte("write"), Operations: []storage.Operation{{ID: uid(id), Position: image.Position, Kind: storage.Write, Object: f.file, Objects: []storage.ObjectChange{{Before: before, After: object}}, Data: data, Participants: []fsapi.UUID{f.file.Home}}}}
}

func evidence(g logicalview.Stage, digest record.Hash) logicalview.Evidence {
	h := sha256.Sum256([]byte("external certificate"))
	selection := record.NormalRef{Stream: g.Origin, Creator: g.Slot.Sequencer, Kind: record.NormalSelectionKind, Sequence: g.Slot.Serial, SHA256: h}
	commit := record.NormalRef{Stream: g.Origin, Creator: g.OriginLaunch, Kind: record.NormalResultKind, Sequence: g.Slot.Serial, SHA256: h}
	recipientStream := g.Origin
	recipientStream.Origin = g.Recipient.Node
	acceptance := record.NormalRef{Stream: recipientStream, Creator: g.Recipient, Kind: record.NormalAcceptanceKind, Sequence: g.Slot.Serial, SHA256: h}
	return logicalview.Evidence{Digest: digest, Selected: record.SelectedRef{Slot: g.Slot, Certificate: selection}, Sources: []record.SourceCoverage{{Stream: g.Origin, Holder: g.OriginLaunch, Through: commit.Sequence, Last: commit}}, Acceptances: []record.NormalRef{acceptance}, Result: []byte("written"), Proof: []byte("external proof")}
}

func (f *viewFixture) selectStage(t *testing.T, g logicalview.Stage) logicalview.Evidence {
	t.Helper()
	digest, err := f.view.Stage(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	e := evidence(g, digest)
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	e.Acks = slices.Clone(f.config.Domains)
	return e
}

func readFile(t *testing.T, v *logicalview.Store, id fsapi.ObjectID) string {
	t.Helper()
	o, err := v.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := o.Close(); err != nil {
			t.Error(err)
		}
	}()
	p := make([]byte, 32)
	n, err := o.Read(t.Context(), id, p, 0)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("read = %d, %v", n, err)
	}
	return string(p[:n])
}

func requireError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}

func TestStageSelectPublishKeepsCutsSeparate(t *testing.T) {
	f := newViewFixture(t)
	g := f.stage(t, 50, []byte("next"))
	digest, err := f.view.Stage(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f.view, f.file); got != "base" {
		t.Fatal(got)
	}
	e := evidence(g, digest)
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	frontiers := f.view.Frontiers()
	if frontiers.Selected == nil || *frontiers.Selected != e.Selected || frontiers.Visible != nil || frontiers.Installed != nil || frontiers.Durable != nil {
		t.Fatalf("frontiers = %+v", frontiers)
	}
	if got := readFile(t, f.view, f.file); got != "base" {
		t.Fatal(got)
	}
	old, err := f.view.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	e.Acks = slices.Clone(f.config.Domains)
	requireError(t, f.view.Publish(t.Context(), e), fsapi.Busy)
	if cut, err := old.Cut(); err != nil || cut != nil {
		t.Fatal(cut, err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f.view, f.file); got != "next" {
		t.Fatal(got)
	}
	frontiers = f.view.Frontiers()
	if frontiers.Visible == nil || *frontiers.Visible != e.Selected || frontiers.Durable != nil {
		t.Fatalf("frontiers = %+v", frontiers)
	}
	requireError(t, f.view.Publish(t.Context(), e), fsapi.Stale)
}

func TestStageRejectsInvalidInputsWithoutAdmittingClaim(t *testing.T) {
	cases := []struct {
		name   string
		change func(*logicalview.Stage)
		want   error
	}{
		{"missing-intent", func(g *logicalview.Stage) { g.Intent = nil }, fsapi.Invalid},
		{"wrong-slot", func(g *logicalview.Stage) { g.Slot.Serial++ }, fsapi.Stale},
		{"same-recipient", func(g *logicalview.Stage) { g.Recipient = g.OriginLaunch }, fsapi.Permission},
		{"operation-identity", func(g *logicalview.Stage) { g.Operations[0].ID = uid(99) }, fsapi.Stale},
		{"object-revision", func(g *logicalview.Stage) { g.Operations[0].Objects[0].Before++ }, fsapi.Stale},
		{"claim-bounds", func(g *logicalview.Stage) { g.Operations[0].Data = make([]byte, 16<<10) }, fsapi.NoSpace},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newViewFixture(t)
			g := f.stage(t, 50, []byte("next"))
			tc.change(&g)
			_, err := f.view.Stage(t.Context(), g)
			requireError(t, err, tc.want)
			if len(f.view.Claims()) != 0 || readFile(t, f.view, f.file) != "base" {
				t.Fatal("rejected candidate altered state")
			}
			if err := f.view.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectionAndPublicationRequireExactEvidence(t *testing.T) {
	cases := []struct {
		name   string
		change func(*logicalview.Evidence)
		want   error
	}{
		{"digest", func(e *logicalview.Evidence) { e.Digest = record.Hash{} }, fsapi.Stale},
		{"certificate", func(e *logicalview.Evidence) { e.Selected.Certificate.Kind = record.NormalResultKind }, fsapi.Stale},
		{"source", func(e *logicalview.Evidence) { e.Sources = nil }, fsapi.Unavailable},
		{"source-position", func(e *logicalview.Evidence) { e.Sources[0].Through++ }, fsapi.Invalid},
		{"recipient", func(e *logicalview.Evidence) { e.Acceptances[0].Creator = e.Sources[0].Holder }, fsapi.Unavailable},
		{"proof", func(e *logicalview.Evidence) { e.Proof = nil }, fsapi.Unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newViewFixture(t)
			g := f.stage(t, 50, []byte("next"))
			digest, err := f.view.Stage(t.Context(), g)
			if err != nil {
				t.Fatal(err)
			}
			e := evidence(g, digest)
			tc.change(&e)
			requireError(t, f.view.Select(t.Context(), e), tc.want)
			claims := f.view.Claims()
			if len(claims) != 1 || claims[0].Evidence != nil || f.view.Frontiers().Selected != nil {
				t.Fatal("rejected evidence resolved claim")
			}
			e = evidence(g, digest)
			if err := f.view.Select(t.Context(), e); err != nil {
				t.Fatal(err)
			}
			e.Acks = []logicalview.Domain{f.config.Domains[0], f.config.Domains[0]}
			requireError(t, f.view.Publish(t.Context(), e), fsapi.Unavailable)
			e.Acks = slices.Clone(f.config.Domains)
			e.Acks[1].Session = uid(99)
			requireError(t, f.view.Publish(t.Context(), e), fsapi.Unavailable)
			if got := readFile(t, f.view, f.file); got != "base" {
				t.Fatal(got)
			}
			e.Acks = slices.Clone(f.config.Domains)
			if err := f.view.Publish(t.Context(), e); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentObservationCloseAndCanceledAdmission(t *testing.T) {
	f := newViewFixture(t)
	o, err := f.view.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := o.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	_, err = o.Image(t.Context(), f.file.Home)
	requireError(t, err, fsapi.Stale)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = f.view.Begin(ctx)
	requireError(t, err, context.Canceled)
	_, err = f.view.Stage(ctx, f.stage(t, 50, []byte("next")))
	requireError(t, err, context.Canceled)
	if len(f.view.Claims()) != 0 {
		t.Fatal("cancellation retained claim")
	}
	if err := f.view.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.config.Budget.Usage().Used; got != (admission.Resources{}) {
		t.Fatalf("arena leaked: %+v", got)
	}
}

func TestDependencyRequiresExactBytesAndSourceCoverage(t *testing.T) {
	f := newViewFixture(t)
	g := f.stage(t, 50, []byte("next"))
	raw, err := record.EncodeNormal(record.NormalRecord{Stream: g.Origin, Creator: g.OriginLaunch, Sequence: 1, Operation: uid(70), Payload: record.OriginOpen{Segment: uid(71), MaximumBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := record.NormalReference(raw)
	if err != nil {
		t.Fatal(err)
	}
	g.Dependencies = []logicalview.Dependency{{Ref: ref, Bytes: raw}}
	_, err = f.view.Stage(t.Context(), g)
	requireError(t, err, fsapi.Unavailable)
	if err := f.view.ObserveSource(logicalview.SourceFrontier{Stream: g.Origin, Received: 1, Durable: 1}); err != nil {
		t.Fatal(err)
	}
	_, err = f.view.Stage(t.Context(), g)
	requireError(t, err, fsapi.Unavailable)
	if len(f.view.Claims()) != 0 {
		t.Fatal("source receipt admitted replay without coverage")
	}
	if err := f.view.Close(); err != nil {
		t.Fatal(err)
	}
	f.config.Sources = []record.SourceCoverage{{Stream: g.Origin, Holder: g.OriginLaunch, Through: 1, Last: ref, Dependencies: []record.NormalRef{ref}}}
	f.view, err = logicalview.New(t.Context(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.config.Sources[0].Dependencies[0].Sequence = 99
	g.Dependencies[0].Bytes = bytes.Clone(raw)
	g.Dependencies[0].Bytes[len(raw)-1] ^= 1
	_, err = f.view.Stage(t.Context(), g)
	requireError(t, err, fsapi.Invalid)
	g.Dependencies[0].Bytes = bytes.Clone(raw)
	if _, err := f.view.Stage(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	g.Dependencies[0].Bytes[0] ^= 1
	claims := f.view.Claims()
	if !bytes.Equal(claims[0].Stage.Dependencies[0].Bytes, raw) {
		t.Fatal("borrowed dependency bytes")
	}
}
