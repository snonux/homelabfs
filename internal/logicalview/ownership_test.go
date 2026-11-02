package logicalview_test

import (
	"context"
	"slices"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/logicalview"
)

func TestStoreOwnsInputsAndReturnedState(t *testing.T) {
	f := newViewFixture(t)
	g := f.stage(t, 50, []byte("next"))
	f.config.Baseline[0].Data[0][0] = 'X'
	f.config.Baseline[0].Image.Objects[1].Attr.Mode = 0
	if got := readFile(t, f.view, f.file); got != "base" {
		t.Fatal("borrowed baseline", got)
	}
	digest, err := f.view.Stage(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	g.Intent[0] = 'X'
	g.Operations[0].Data[0] = 'X'
	g.Operations[0].Objects[0].After.Attr.Mode = 0
	claims := f.view.Claims()
	if string(claims[0].Stage.Intent) != "write" || string(claims[0].Stage.Operations[0].Data) != "next" {
		t.Fatal("borrowed stage")
	}
	claims[0].Stage.Operations[0].Data[0] = 'Y'
	e := evidence(g, digest)
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	e.Result[0] = 'X'
	e.Sources[0].Through = 99
	saved := f.view.Claims()[0].Evidence
	if string(saved.Result) != "written" || saved.Sources[0].Through != 1 {
		t.Fatal("borrowed evidence")
	}
	saved.Result[0] = 'Y'
	e = evidence(g, digest)
	e.Acks = slices.Clone(f.config.Domains)
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	frontiers := f.view.Frontiers()
	frontiers.Visible.Slot.Serial = 99
	if f.view.Frontiers().Visible.Slot.Serial != 1 {
		t.Fatal("borrowed frontier")
	}
	domains := f.view.Domains()
	domains[0].Session = uid(99)
	if f.view.Domains()[0].Session == uid(99) {
		t.Fatal("borrowed domains")
	}
	o, err := f.view.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	image, err := o.Image(t.Context(), f.file.Home)
	if err != nil {
		t.Fatal(err)
	}
	image.Entries[0].Target.Allocation = uid(99)
	entry, err := o.Lookup(t.Context(), f.root, "base")
	if err != nil || *entry.Target != f.file {
		t.Fatal("borrowed image", entry, err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f.view, f.file); got != "next" {
		t.Fatal("borrowed payload", got)
	}
}

func TestVerifierCannotMutateLiveStateAndCanRefuseTransitions(t *testing.T) {
	c, root, file := viewConfig(t)
	var fail logicalview.Phase
	c.Verifier = verifierFunc(func(ctx context.Context, check logicalview.Check) error {
		if check.Phase == fail {
			return fsapi.Unavailable
		}
		if check.Phase == logicalview.Baseline {
			check.Baseline[0].Data[0][0] = 'X'
		}
		if len(check.Stage.Operations) > 0 {
			check.Stage.Operations[0].Data[0] = 'X'
		}
		if len(check.Evidence.Result) > 0 {
			check.Evidence.Result[0] = 'X'
		}
		check.Authority.Volumes[0].Replicas[0] = uid(99)
		check.Domains[0].Session = uid(99)
		return ctx.Err()
	})
	v, err := logicalview.New(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	f := &viewFixture{config: c, view: v, root: root, file: file}
	g := f.stage(t, 50, []byte("next"))
	fail = logicalview.Candidate
	_, err = v.Stage(t.Context(), g)
	requireError(t, err, fsapi.Unavailable)
	if len(v.Claims()) != 0 {
		t.Fatal("refused candidate retained")
	}
	fail = 0
	digest, err := v.Stage(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	e := evidence(g, digest)
	fail = logicalview.Selection
	requireError(t, v.Select(t.Context(), e), fsapi.Unavailable)
	if v.Claims()[0].Evidence != nil {
		t.Fatal("refused selection resolved")
	}
	fail = 0
	if err := v.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	e.Acks = slices.Clone(c.Domains)
	fail = logicalview.Publication
	requireError(t, v.Publish(t.Context(), e), fsapi.Unavailable)
	fail = 0
	if got := readFile(t, v, file); got != "base" {
		t.Fatal(got)
	}
	if err := v.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, v, file); got != "next" {
		t.Fatal(got)
	}
}

func TestNewRejectsBoundsAndReturnsReservedBudget(t *testing.T) {
	cases := []struct {
		name   string
		change func(*logicalview.Config)
		want   error
	}{
		{"missing-verifier", func(c *logicalview.Config) { c.Verifier = nil }, fsapi.Invalid},
		{"duplicate-domain", func(c *logicalview.Config) { c.Domains[1] = c.Domains[0] }, fsapi.Invalid},
		{"snapshot-bound", func(c *logicalview.Config) { c.Limits.MaxSnapshotBytes = 1 }, fsapi.NoSpace},
		{"identity-bound", func(c *logicalview.Config) { c.Limits.Identities = 1 }, fsapi.NoSpace},
		{"wrong-home-generation", func(c *logicalview.Config) { c.Baseline[0].Image.Position.Generation = uid(99) }, fsapi.Stale},
		{"baseline-authority-refusal", func(c *logicalview.Config) {
			c.Verifier = verifierFunc(func(context.Context, logicalview.Check) error { return fsapi.Unavailable })
		}, fsapi.Unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := viewConfig(t)
			tc.change(&c)
			_, err := logicalview.New(t.Context(), c)
			requireError(t, err, tc.want)
			if got := c.Budget.Usage().Used; got != (admission.Resources{}) {
				t.Fatalf("failed constructor leaked %+v", got)
			}
		})
	}
}

func TestSourceBackingAndRetirementKeepOwnershipSeparate(t *testing.T) {
	f := newViewFixture(t)
	old, err := f.view.PinVersion(nil)
	if err != nil {
		t.Fatal(err)
	}
	g := f.stage(t, 50, []byte("next"))
	e := f.selectStage(t, g)
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	source := logicalview.SourceFrontier{Stream: g.Origin, Received: 9, Durable: 7}
	if err := f.view.ObserveSource(source); err != nil {
		t.Fatal(err)
	}
	requireError(t, f.view.ObserveSource(logicalview.SourceFrontier{Stream: g.Origin, Received: 8, Durable: 7}), fsapi.Stale)
	requireError(t, f.view.ObserveSource(logicalview.SourceFrontier{Stream: g.Origin, Received: 9, Durable: 10}), fsapi.Invalid)
	requireError(t, f.view.MarkBacking(t.Context(), e.Selected, true, []byte("durable")), fsapi.Stale)
	requireError(t, f.view.Retire(t.Context(), e.Selected, []fsapi.UUID{g.Claim}, []byte("replacement")), fsapi.Stale)
	if err := f.view.MarkBacking(t.Context(), e.Selected, false, []byte("installed")); err != nil {
		t.Fatal(err)
	}
	if err := f.view.MarkBacking(t.Context(), e.Selected, true, []byte("durable")); err != nil {
		t.Fatal(err)
	}
	requireError(t, f.view.Retire(t.Context(), e.Selected, []fsapi.UUID{g.Claim, g.Claim}, nil), fsapi.Invalid)
	if err := f.view.Retire(t.Context(), e.Selected, []fsapi.UUID{g.Claim}, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	if len(f.view.Claims()) != 0 {
		t.Fatal("claim not retired")
	}
	retained, err := f.view.PinVersion(nil)
	if err != nil {
		t.Fatal("pinned baseline retired", err)
	}
	retained.Release()
	requireError(t, f.view.Close(), fsapi.Busy)
	old.Release()
	old.Release()
	if err := f.view.Retire(t.Context(), e.Selected, nil, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	_, err = f.view.PinVersion(nil)
	requireError(t, err, fsapi.Stale)
	requireError(t, f.view.ReserveIdentity(t.Context(), g.Claim, fsapi.ObjectID{}), fsapi.Stale)
	frontiers := f.view.Frontiers()
	if len(frontiers.Sources) != 1 || frontiers.Sources[0] != source || *frontiers.Visible != e.Selected || *frontiers.Durable != e.Selected {
		t.Fatalf("frontiers = %+v", frontiers)
	}
	if err := f.view.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.view.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.config.Budget.Usage().Used; got != (admission.Resources{}) {
		t.Fatalf("close leaked %+v", got)
	}
}

func TestHandleRightsGenerationAndRetirementRefusal(t *testing.T) {
	c, root, file := viewConfig(t)
	refuse := false
	c.Verifier = verifierFunc(func(_ context.Context, check logicalview.Check) error {
		if refuse && check.Phase == logicalview.HandleRetirement {
			return fsapi.Unavailable
		}
		return nil
	})
	v, err := logicalview.New(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	o, err := v.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	flags := fsapi.OpenFlags{Access: fsapi.ReadOnly}
	h, err := o.Open(t.Context(), file, flags)
	if err != nil {
		t.Fatal(err)
	}
	_, err = o.Open(t.Context(), root, fsapi.OpenFlags{Access: fsapi.ReadWrite})
	requireError(t, err, fsapi.IsDirectory)
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = v.ResolveHandle(uid(99), h)
	requireError(t, err, fsapi.Stale)
	object, rights, err := v.ResolveHandle(c.Generation, h)
	if err != nil || object != file || rights != flags {
		t.Fatal(object, rights, err)
	}
	refuse = true
	requireError(t, v.ReleaseHandle(t.Context(), c.Generation, h), fsapi.Unavailable)
	if _, _, err := v.ResolveHandle(c.Generation, h); err != nil {
		t.Fatal("refused retirement dropped handle", err)
	}
	requireError(t, v.Close(), fsapi.Busy)
	refuse = false
	if err := v.ReleaseHandle(t.Context(), c.Generation, h); err != nil {
		t.Fatal(err)
	}
	requireError(t, v.ReleaseHandle(t.Context(), c.Generation, h), fsapi.Stale)
	o, err = v.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	next, err := o.Open(t.Context(), file, flags)
	if err != nil || next <= h {
		t.Fatal("reused handle", next, err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v.ReleaseHandle(t.Context(), c.Generation, next); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSelectedNoEffectRetainsResultWithoutPublishingCandidateBytes(t *testing.T) {
	f := newViewFixture(t)
	g := f.stage(t, 50, []byte("next"))
	digest, err := f.view.Stage(t.Context(), g)
	if err != nil {
		t.Fatal(err)
	}
	e := evidence(g, digest)
	e.NoEffect = true
	e.Result = []byte("no effect")
	if err := f.view.Select(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	e.Acks = slices.Clone(f.config.Domains)
	if err := f.view.Publish(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, f.view, f.file); got != "base" {
		t.Fatal("no-effect published candidate bytes", got)
	}
	claim := f.view.Claims()[0]
	if !claim.Evidence.NoEffect || string(claim.Evidence.Result) != "no effect" {
		t.Fatal("no-effect result lost", claim)
	}
	if f.view.Frontiers().Visible.Slot.Serial != 1 {
		t.Fatal("no-effect did not advance selected cut")
	}
}
