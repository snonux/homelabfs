package record

import (
	"bytes"
	"sort"
	"strings"
	"testing"
)

type normalGraphHistory struct {
	recovery                          NormalRecovery
	frames                            map[NormalRef][]byte
	grants                            []NormalGrant
	grantRefs, resultRefs             []NormalRef
	cuts                              []SelectedRef
	sequencerSequence, originSequence uint64
}

func newNormalGraphHistory(t *testing.T, count int) *normalGraphHistory {
	t.Helper()
	h := &normalGraphHistory{recovery: NormalRecovery{Authority: normalFixture(t).Authority}, frames: make(map[NormalRef][]byte)}
	for range count {
		h.appendSlot(t, h.nextGrant(t))
	}
	return h
}

func (h *normalGraphHistory) nextGrant(t *testing.T) NormalGrant {
	t.Helper()
	g := normalFixture(t)
	i := len(h.grants)
	g.Operation = fixtureUUID(byte(0xc0 + i))
	g.Document.Internal.ID.Number = uint64(i + 1)
	g.Slot.Serial = uint64(i + 1)
	// The legacy fixture shares its metadata and redo slice; detach them so
	// each new slot advances the logical revision exactly once.
	g.Mutation.Redo.Objects = append([]ObjectEdit(nil), g.Mutation.Redo.Objects...)
	for j := range g.Mutation.Intent.SelectedMetadata {
		g.Mutation.Intent.SelectedMetadata[j].ExpectedRevision += uint64(i)
		g.Mutation.Intent.SelectedMetadata[j].After.Revision += uint64(i)
	}
	for j := range g.Mutation.Redo.Objects {
		g.Mutation.Redo.Objects[j].ExpectedRevision += uint64(i)
		g.Mutation.Redo.Objects[j].After.Revision += uint64(i)
	}
	for j := range g.Mutation.Authorizations {
		g.Mutation.Authorizations[j].ObjectRevision += uint64(i)
	}
	g.Document.Internal.Intent = g.Mutation.Intent
	if i > 0 {
		previous := h.cuts[i-1]
		g.Predecessor = &previous
		g.Cuts = []NormalCut{{Volume: g.Origin.Volume, Selected: previous}}
	}
	return g
}

func (h *normalGraphHistory) add(t *testing.T, r NormalRecord, deps ...NormalRef) NormalRef {
	t.Helper()
	b, err := EncodeNormal(r)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := NormalReference(b)
	if err != nil {
		t.Fatal(err)
	}
	h.frames[ref] = b
	unique := make(map[NormalRef]bool)
	dependencies := []NormalRef{}
	for _, dependency := range deps {
		if !unique[dependency] {
			unique[dependency] = true
			dependencies = append(dependencies, dependency)
		}
	}
	sort.Slice(dependencies, func(i, j int) bool {
		a, _ := sortKey(dependencies[i])
		b, _ := sortKey(dependencies[j])
		return bytes.Compare(a, b) < 0
	})
	h.recovery.Graph = append(h.recovery.Graph, RecoveryNode{Frame: ref, Dependencies: dependencies})
	sort.Slice(h.recovery.Graph, func(i, j int) bool {
		a, _ := sortKey(h.recovery.Graph[i])
		b, _ := sortKey(h.recovery.Graph[j])
		return bytes.Compare(a, b) < 0
	})
	return ref
}

func (h *normalGraphHistory) appendSlot(t *testing.T, g NormalGrant) {
	t.Helper()
	previous := []NormalRef{}
	if g.Predecessor != nil {
		previous = append(previous, g.Predecessor.Certificate)
	}
	for _, cut := range g.Cuts {
		previous = append(previous, cut.Selected.Certificate)
	}
	h.sequencerSequence++
	grant := h.add(t, normalRecord(g, false, h.sequencerSequence, g), previous...)
	h.originSequence++
	fragment := h.add(t, normalRecord(g, true, h.originSequence, OriginFragment{GrantBytes: h.frames[grant]}), grant)
	h.sequencerSequence++
	prepared := h.add(t, normalRecord(g, false, h.sequencerSequence, MemoryAcceptance{Grant: grant, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 1, Target: grant, Dependencies: []NormalRef{grant}}), grant)
	h.originSequence++
	choice := h.add(t, normalRecord(g, true, h.originSequence, OriginChoice{Grant: grant, Slot: g.Slot, Choice: 1, Prepared: &prepared, Fragment: []NormalRef{fragment}}), grant, fragment, prepared)
	h.sequencerSequence++
	accepted := h.add(t, normalRecord(g, false, h.sequencerSequence, MemoryAcceptance{Grant: grant, Slot: g.Slot, Origin: g.Origin, Recipient: g.Recipient, Phase: 2, Target: choice, DurableThrough: h.originSequence, Dependencies: []NormalRef{choice}}), grant, choice)
	h.sequencerSequence++
	selectionDeps := append([]NormalRef{grant, choice, accepted}, previous...)
	selection := h.add(t, normalRecord(g, false, h.sequencerSequence, SlotSelection{Grant: grant, Slot: g.Slot, Predecessor: g.Predecessor, Outcome: 1, Choice: choice, Acceptance: accepted, Volumes: []UUID{g.Origin.Volume}}), selectionDeps...)
	cut := SelectedRef{Slot: g.Slot, Certificate: selection}
	h.originSequence++
	result := h.add(t, normalRecord(g, true, h.originSequence, NormalResult{Grant: grant, Selected: cut, Value: g.Result}), grant, selection)
	h.grants = append(h.grants, g)
	h.grantRefs = append(h.grantRefs, grant)
	h.resultRefs = append(h.resultRefs, result)
	h.cuts = append(h.cuts, cut)
	h.recovery.Selected = &cut
	h.recovery.IssuedThrough = g.Slot.Serial
}

func (h *normalGraphHistory) addRecovery(t *testing.T, recovery NormalRecovery) {
	t.Helper()
	deps := []NormalRef{}
	for _, node := range recovery.Graph {
		deps = append(deps, node.Frame)
	}
	h.originSequence++
	h.add(t, normalRecord(h.grants[len(h.grants)-1], true, h.originSequence, recovery), deps...)
}

func TestNormalSelectedCertificateBindings(t *testing.T) {
	t.Run("valid three slot chain", func(t *testing.T) {
		h := newNormalGraphHistory(t, 3)
		if err := VerifyNormalGraph(h.recovery, h.frames); err != nil {
			t.Fatal(err)
		}
	})
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *normalGraphHistory)
	}{
		{"grant and selection predecessor serial hole", func(t *testing.T, h *normalGraphHistory) {
			g := h.nextGrant(t)
			g.Slot.Serial = 3
			g.Predecessor.Slot.Serial = 2
			h.appendSlot(t, g)
		}},
		{"grant and selection predecessor authority", func(t *testing.T, h *normalGraphHistory) {
			g := h.nextGrant(t)
			g.Authority.RegistryRevision++
			hash, err := NormalAuthorityHash(g.Authority)
			if err != nil {
				t.Fatal(err)
			}
			g.Slot.Authority = hash
			g.Predecessor.Slot.Authority = hash
			h.appendSlot(t, g)
		}},
		{"grant cut serial", func(t *testing.T, h *normalGraphHistory) {
			g := h.nextGrant(t)
			g.Cuts[0].Selected.Slot.Serial++
			h.appendSlot(t, g)
		}},
		{"recovery selected serial", func(_ *testing.T, h *normalGraphHistory) {
			cut := *h.recovery.Selected
			cut.Slot.Serial++
			h.recovery.Selected = &cut
		}},
		{"recovery selected authority", func(_ *testing.T, h *normalGraphHistory) {
			cut := *h.recovery.Selected
			cut.Slot.Authority[0] ^= 1
			h.recovery.Selected = &cut
		}},
		{"nested recovery selected", func(t *testing.T, h *normalGraphHistory) {
			nested := h.recovery
			cut := *nested.Selected
			cut.Slot.Serial++
			nested.Selected = &cut
			h.addRecovery(t, nested)
		}},
		{"frontier all cuts", func(t *testing.T, h *normalGraphHistory) {
			cut := h.cuts[0]
			cut.Slot.Serial++
			g := h.grants[0]
			frontier := NormalFrontier{Stream: g.Origin, Selected: &cut, Visible: &cut, Installed: &cut, Checkpoint: &cut}
			h.originSequence++
			h.add(t, normalRecord(g, true, h.originSequence, frontier), cut.Certificate)
		}},
		{"barrier cut", func(t *testing.T, h *normalGraphHistory) {
			g := h.grants[0]
			cut := h.cuts[0]
			cut.Slot.Serial++
			last := h.resultRefs[0]
			barrier := NormalBarrier{Authority: g.Authority, Origin: g.Origin, Recipient: g.Recipient, Cuts: []NormalCut{{Volume: g.Origin.Volume, Selected: cut}}, Sources: []SourceCoverage{{Stream: last.Stream, Holder: g.OriginLaunch, Through: last.Sequence, Last: last}}}
			h.originSequence++
			h.add(t, normalRecord(g, true, h.originSequence, barrier), cut.Certificate, last)
		}},
		{"close report nested frontier", func(_ *testing.T, h *normalGraphHistory) {
			g := h.grants[0]
			cut := h.cuts[0]
			cut.Slot.Serial++
			h.recovery.Reports = []OriginCloseReport{{Origin: g.OriginLaunch, Reporter: g.OriginLaunch, Promise: TransitionPromise{Transition: fixtureUUID(0xef), PredecessorAuthority: g.Slot.Authority, RegistryRevision: 2, Epoch: 8, Term: 2, Exclusions: []NormalRef{h.grantRefs[0]}}, State: 3, IssuedThrough: 1, Frontiers: []NormalFrontier{{Stream: g.Origin, Selected: &cut}}}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newNormalGraphHistory(t, 1)
			test.mutate(t, h)
			if err := VerifyNormalGraph(h.recovery, h.frames); err == nil || !strings.Contains(err.Error(), "normal selected reference differs from certificate slot") {
				t.Fatalf("forgery did not reach selected certificate binding check: %v", err)
			}
		})
	}
}

func TestNormalTerminalObservationGrantBinding(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, crossed := range []bool{false, true} {
			name := "root matching"
			if nested {
				name = "nested matching"
			}
			if crossed {
				name += " crossed"
			}
			t.Run(name, func(t *testing.T) {
				h := newNormalGraphHistory(t, 2)
				// Establish that both grants, results and the ordered chain are valid
				// independently of the observation being attached below.
				if err := VerifyNormalGraph(h.recovery, h.frames); err != nil {
					t.Fatal(err)
				}
				result := h.resultRefs[0]
				if crossed {
					result = h.resultRefs[1]
				}
				observation := ResultObservation{Grant: h.grantRefs[0], State: 3, Result: &result}
				if nested {
					recovery := h.recovery
					recovery.Results = []ResultObservation{observation}
					h.addRecovery(t, recovery)
				} else {
					h.recovery.Results = []ResultObservation{observation}
				}
				err := VerifyNormalGraph(h.recovery, h.frames)
				if (err == nil) == crossed {
					t.Fatalf("crossed=%v verification=%v", crossed, err)
				}
				if crossed && !strings.Contains(err.Error(), "normal terminal observation differs from result grant") {
					t.Fatalf("crossed observation failed for a different reason: %v", err)
				}
			})
		}
	}
}
