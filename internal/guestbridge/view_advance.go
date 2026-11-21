package guestbridge

import (
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Guest-requested view advance (task xf4 step A, docs/design/xf4-view-advance.md).
//
// A VIEW_ACQUIRE may carry the optional field "advance": true. It says "my
// view at the expected target holds no target-bound cache; if the target is
// stale, grant on the current cut and I will adopt it". When the field is
// absent the request bytes are exactly the pre-xf4 bytes, and "advance": false
// is refused so every request has a single encoding. The field is accepted
// only together with READ mode, DOMAIN access and a nonnull CONTROLLED_LOCAL
// expected target: only a cacheless observation of an installed local view
// can adopt a newer cut (a discovery acquire has no view to advance, and a
// PAGE or OBJECT lease is target-bound cache by definition).
//
// The bridge's reply check (validateControlResponse) then accepts a GRANTED
// lease whose target differs from the expected one, but only for an advance
// request and only when AdvanceCompatible holds: the granted cut is the same
// enrollment, profile, context, journal incarnation, epoch and visibility
// generation, and not behind the expected position. The issuer
// (viewcontrol advance.go) grants only under that same condition; the guest
// codec applies its own equivalent check before adopting.

// validateAdvance checks the optional advance field of a VIEW_ACQUIRE payload
// o whose other fields already passed: it must be exactly true, and the
// request must be a READ DOMAIN acquisition with a CONTROLLED_LOCAL expected
// target.
func validateAdvance(o map[string]any) error {
	if advance, ok := o["advance"].(bool); !ok || !advance {
		return fsapi.Invalid
	}
	access, _ := o["access"].(map[string]any)
	target, _ := o["target"].(map[string]any)
	if text(o["mode"]) != "READ" || access == nil || access["kind"] != "DOMAIN" || target == nil || target["kind"] != "CONTROLLED_LOCAL" {
		return fsapi.Invalid
	}
	return nil
}

// acquireKeys returns the exact field set of a VIEW_ACQUIRE payload v: the
// base keys, plus "advance" when v carries it (validateAdvance then decides
// whether its value and the rest of the request allow it).
func acquireKeys(v any, keys []string) []string {
	if o, ok := v.(map[string]any); ok {
		if _, present := o["advance"]; present {
			return append(append([]string{}, keys...), "advance")
		}
	}
	return keys
}

// advancedLease reports whether the GRANTED lease target of the VIEW_ACQUIRE
// payload p may differ from p's expected target: p requested advance and the
// two targets are AdvanceCompatible.
func advancedLease(p map[string]any, granted any) bool {
	if advance, _ := p["advance"].(bool); !advance {
		return false
	}
	expected, ok := decodeTarget(p["target"])
	if !ok {
		return false
	}
	current, ok := decodeTarget(granted)
	return ok && AdvanceCompatible(expected, current)
}

// decodeTarget converts a canonical target value into a ViewTarget.
func decodeTarget(v any) (ViewTarget, bool) {
	var target ViewTarget
	raw, err := marshal(v)
	if err != nil || json.Unmarshal(raw, &target) != nil {
		return target, false
	}
	return target, true
}

// AdvanceCompatible reports whether a guest view installed at expected may
// adopt granted by advance (task xf4): both are CONTROLLED_LOCAL cuts of the
// same enrollment, profile, context, journal incarnation, epoch and
// visibility generation, and granted is not behind expected. At an equal
// position the journal hash must match too, so a divergent journal history
// at the same position never counts as an advance. Anything else (another
// incarnation after a reopen, another epoch or serving generation) needs the
// recall that installs the current cut.
func AdvanceCompatible(expected, granted ViewTarget) bool {
	e, g := expected.Cut, granted.Cut
	if expected.Kind != "CONTROLLED_LOCAL" || granted.Kind != "CONTROLLED_LOCAL" || e == nil || g == nil {
		return false
	}
	if e.EnrollmentHash != g.EnrollmentHash || e.ProfileHash != g.ProfileHash || e.ContextHash != g.ContextHash || e.JournalIncarnation != g.JournalIncarnation || e.JournalEpoch != g.JournalEpoch || e.VisibilityGeneration != g.VisibilityGeneration {
		return false
	}
	return e.JournalPosition < g.JournalPosition || e.JournalPosition == g.JournalPosition && e.JournalHash == g.JournalHash
}
