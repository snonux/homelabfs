package controlledvalidation

import (
	"encoding/json"
	"testing"
)

// fdAttributes queries a terminal claim and returns its result attributes.
func (h *bridgeHarness) fdAttributes(op string, args any) (string, map[string]any) {
	h.f.t.Helper()
	b := h.f.newBinding(op, args, false)
	h.send("CALL", b.Call)
	var result struct {
		Payload struct {
			Result struct {
				Code       string
				Attributes map[string]any
			}
		}
	}
	if err := json.Unmarshal(h.send("QUERY", map[string]any{"claim": b.Call.Claim}), &result); err != nil {
		h.f.t.Fatal(err)
	}
	return result.Payload.Result.Code, result.Payload.Result.Attributes
}

// TestBridgeDescribedAttrCallsAfterRenameUnlink pins s64 against the durable
// controlled provider (claims, ControlledCut planning and journaled SETATTR):
// after rename or unlink the path GETATTR/SETATTR stay Stale, while fstat and
// fchmod carrying the open description reach the original object, whose
// nlink is 0 once unlinked.
func TestBridgeDescribedAttrCallsAfterRenameUnlink(t *testing.T) {
	for _, change := range []string{"RENAME", "UNLINK"} {
		t.Run(change, func(t *testing.T) {
			h := newBridgeHarness(t)
			_, created := h.create()
			requireCode(t, created, "OK")
			object, description := created.Capabilities[0], created.Capabilities[1]
			root := h.root.Capabilities[0]
			var changed wireResult
			if change == "RENAME" {
				_, changed = h.call("RENAME", map[string]any{"source_parent": root, "source_name": "66696c65", "destination_parent": root, "destination_name": "6e6577", "no_replace": false})
			} else {
				_, changed = h.call("UNLINK", map[string]any{"parent": root, "name": "66696c65"})
			}
			requireCode(t, changed, "OK")
			chmod := map[string]any{"mode": 0o604, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
			_, r := h.call("GETATTR", map[string]any{"object": object})
			requireCode(t, r, "Stale")
			_, r = h.call("SETATTR", map[string]any{"object": object, "attributes": chmod})
			requireCode(t, r, "Stale")
			_, r = h.call("SETATTR", map[string]any{"object": object, "attributes": chmod, "description": description})
			requireCode(t, r, "OK")
			code, a := h.fdAttributes("GETATTR", map[string]any{"object": object, "description": description})
			nlink := float64(1)
			if change == "UNLINK" {
				nlink = 0
			}
			if code != "OK" || a["mode"] != float64(0o604) || a["nlink"] != nlink || a["object_allocation"] != object.Allocation {
				t.Fatalf("fstat: %s %v", code, a)
			}
			_, released := h.call("DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
			requireCode(t, released, "OK")
			_, r = h.call("GETATTR", map[string]any{"object": object, "description": description})
			requireCode(t, r, "Permission")
		})
	}
}
