package logicalview_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/logicalview"
)

func recoveryBytes(t *testing.T, v *logicalview.Store) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := v.WriteRecovery(t.Context(), &b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestRestoreKeepsPendingCandidateInvisible(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(map[bool]string{false: "unresolved", true: "selected"}[selected], func(t *testing.T) {
			f := newViewFixture(t)
			g := f.stage(t, 50, []byte("next"))
			digest, err := f.view.Stage(t.Context(), g)
			if err != nil {
				t.Fatal(err)
			}
			e := evidence(g, digest)
			if selected {
				if err := f.view.Select(t.Context(), e); err != nil {
					t.Fatal(err)
				}
			}
			raw := recoveryBytes(t, f.view)
			restored, err := logicalview.Restore(t.Context(), f.config, bytes.NewReader(raw), []byte("verified recovery"))
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, restored, f.file); got != "base" {
				t.Fatal("pending candidate became visible", got)
			}
			claims := restored.Claims()
			if len(claims) != 1 || (claims[0].Evidence != nil) != selected {
				t.Fatal("lost unresolved/selected distinction", claims)
			}
			if err := restored.Select(t.Context(), e); err != nil {
				t.Fatal(err)
			}
			e.Acks = slices.Clone(f.config.Domains)
			if err := restored.Publish(t.Context(), e); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, restored, f.file); got != "next" {
				t.Fatal(got)
			}
		})
	}
}

func TestRestorePreservesHandlePinsAndReservedAllocations(t *testing.T) {
	f := newViewFixture(t)
	pin, err := f.view.PinVersion(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Release()
	allocation := fsapi.ObjectID{Home: f.file.Home, Allocation: uid(60)}
	if err := f.view.ReserveIdentity(t.Context(), uid(61), allocation); err != nil {
		t.Fatal(err)
	}
	o, err := f.view.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	flags := fsapi.OpenFlags{Access: fsapi.ReadOnly}
	handle, err := o.Open(t.Context(), f.file, flags)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	raw := recoveryBytes(t, f.view)
	c := f.config
	c.Generation = uid(99)
	used := c.Budget.Usage().Used
	_, err = logicalview.Restore(t.Context(), c, bytes.NewReader(raw), nil)
	requireError(t, err, fsapi.Stale)
	if c.Budget.Usage().Used != used {
		t.Fatal("failed generation restore leaked arena")
	}
	restored, err := logicalview.Restore(t.Context(), f.config, bytes.NewReader(raw), []byte("recovery"))
	if err != nil {
		t.Fatal(err)
	}
	object, rights, err := restored.ResolveHandle(f.config.Generation, handle)
	if err != nil || object != f.file || rights != flags {
		t.Fatal("lost descriptor ownership", object, rights, err)
	}
	requireError(t, restored.ReserveIdentity(t.Context(), uid(62), allocation), fsapi.Stale)
	requireError(t, restored.ReserveIdentity(t.Context(), uid(61), fsapi.ObjectID{}), fsapi.Stale)
	if err := restored.ReleaseHandle(t.Context(), f.config.Generation, handle); err != nil {
		t.Fatal(err)
	}
	requireError(t, restored.Close(), fsapi.Busy)
	requireError(t, restored.ReleaseRecoveredPins(t.Context(), nil, 2, nil), fsapi.Invalid)
	if err := restored.ReleaseRecoveredPins(t.Context(), nil, 1, []byte("reconciled")); err != nil {
		t.Fatal(err)
	}
	requireError(t, restored.ReleaseRecoveredPins(t.Context(), nil, 1, nil), fsapi.Invalid)
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRejectsCorruptOrUnverifiedStateWithoutLeakingBudget(t *testing.T) {
	cases := []struct {
		name   string
		change func([]byte) []byte
		refuse bool
	}{
		{name: "trailing-state", change: func(raw []byte) []byte { return append(raw, []byte("{}")...) }},
		{name: "noncanonical-space", change: func(raw []byte) []byte { return append([]byte(" "), raw...) }},
		{name: "invalid-selection", change: func(raw []byte) []byte {
			var state logicalview.RecoveryState
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			state.Selected = 99
			var b bytes.Buffer
			if err := json.NewEncoder(&b).Encode(state); err != nil {
				t.Fatal(err)
			}
			return b.Bytes()
		}},
		{name: "verifier-refusal", refuse: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newViewFixture(t)
			raw := recoveryBytes(t, f.view)
			if tc.change != nil {
				raw = tc.change(raw)
			}
			config := f.config
			if tc.refuse {
				config.Verifier = verifierFunc(func(_ context.Context, c logicalview.Check) error {
					if c.Phase == logicalview.Recovery {
						return fsapi.Unavailable
					}
					return nil
				})
			}
			used := config.Budget.Usage().Used
			if _, err := logicalview.Restore(t.Context(), config, bytes.NewReader(raw), []byte("proof")); err == nil {
				t.Fatal("accepted invalid recovery")
			}
			if got := config.Budget.Usage().Used; got != used {
				t.Fatalf("restore leaked budget: before %+v after %+v", used, got)
			}
			if got := readFile(t, f.view, f.file); got != "base" {
				t.Fatal(got)
			}
			if err := f.view.Close(); err != nil {
				t.Fatal(err)
			}
			if got := config.Budget.Usage().Used; got != (admission.Resources{}) {
				t.Fatal(got)
			}
		})
	}
}

type failingRecoveryWriter struct{ err error }

func (w failingRecoveryWriter) Write([]byte) (int, error) { return 0, w.err }

type shortRecoveryWriter struct{}

func (shortRecoveryWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestWriteRecoveryFailurePreservesUnresolvedClaim(t *testing.T) {
	f := newViewFixture(t)
	g := f.stage(t, 50, []byte("next"))
	if _, err := f.view.Stage(t.Context(), g); err != nil {
		t.Fatal(err)
	}
	before := recoveryBytes(t, f.view)
	sentinel := errors.New("recovery destination failed")
	requireError(t, f.view.WriteRecovery(t.Context(), failingRecoveryWriter{err: sentinel}), sentinel)
	requireError(t, f.view.WriteRecovery(t.Context(), shortRecoveryWriter{}), io.ErrShortWrite)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	requireError(t, f.view.WriteRecovery(ctx, io.Discard), context.Canceled)
	requireError(t, f.view.Close(), fsapi.Busy)
	// Compare decoded state: map traversal order is not a recovery identity.
	var a, b logicalview.RecoveryState
	if err := json.Unmarshal(before, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(recoveryBytes(t, f.view), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Claims) != 1 || b.Claims[0].Digest != a.Claims[0].Digest || b.Pending == nil || b.Pending.Claim != g.Claim {
		t.Fatal("failed write lost uncertain state")
	}
}

func TestRecoveryPreservesRawNamespaceNames(t *testing.T) {
	c, root, file := viewConfig(t)
	name := string([]byte{0xff, 'x'})
	c.Baseline[0].Image.Entries[0].Name = name
	c.Baseline[0].Image.Objects[1].References[0].Name = name
	v, err := logicalview.New(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := logicalview.Restore(t.Context(), c, bytes.NewReader(recoveryBytes(t, v)), []byte("recovery"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := restored.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := o.Lookup(t.Context(), root, name)
	if err != nil || entry.Target == nil || *entry.Target != file || entry.Name != name {
		t.Fatal("raw name changed", entry, err)
	}
	if err := o.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
}
