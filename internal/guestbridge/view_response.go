package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func validateViewAckVersion(c Context, v any) error {
	return validateViewAckWith(v, func(v any) error { return validateViewTarget(c, v) })
}
func validateCommandTarget(c Command, v any) error {
	if c.Protocol != nil {
		return validateViewTarget(c.Context, v)
	}
	return validatePublication(v)
}

func (s *Service) exchangeView(ctx context.Context, c Command) (json.RawMessage, error) {
	v, e := Canonical(c.Payload)
	if e != nil {
		return nil, e
	}
	o := v.(map[string]any)
	cred := o["credentials"]
	if c.Op != "VIEW_RETIRE" {
		cred = o["request"].(map[string]any)["credentials"]
	}
	raw, e := marshal(cred)
	if e != nil {
		return nil, e
	}
	var credentials Credentials
	if json.Unmarshal(raw, &credentials) != nil {
		return nil, fsapi.Invalid
	}
	if e = s.credentials(ctx, c.Context, credentials, oneOf(c.Op, "DIRTY_FREEZE", "VIEW_QUERY", "VIEW_CANCEL", "VIEW_RELEASE", "VIEW_RETIRE")); e != nil {
		return nil, e
	}
	// Every provider must authorize the actual original scoped principal before
	// querying or changing even a pending ticket. Request hashes are not secrets.
	if c.Op == "VIEW_ACQUIRE" {
		if e = s.issuedViewAccess(c.Context, o); e != nil {
			return nil, e
		}
		if o["target"] != nil {
			if e = s.checkTarget(ctx, c.Context, o["target"]); e != nil {
				return nil, e
			}
		}
	}
	if c.Op == "DIRTY_RESERVE" {
		lease := o["lease"].(map[string]any)
		if e = s.issuedViewAccess(c.Context, map[string]any{"domain": lease["domain"], "access": lease["access"]}); e != nil {
			return nil, e
		}
	}
	response, e := s.config.ViewProtocol.ExchangeView(ctx, c.Context, c.Op, bytes.Clone(c.Payload))
	if e != nil {
		return nil, e
	}
	if e = validateViewResponse(c, response); e != nil {
		return nil, e
	}
	if e = s.config.ViewProtocol.CheckResponse(ctx, c.Context, c.Op, bytes.Clone(c.Payload), bytes.Clone(response)); e != nil {
		return nil, e
	}
	value, e := Canonical(response)
	if e != nil {
		return nil, e
	}
	r := value.(map[string]any)
	if r["lease"] != nil {
		if e = s.checkTarget(ctx, c.Context, r["lease"].(map[string]any)["target"]); e != nil {
			return nil, e
		}
	}
	return bytes.Clone(response), nil
}
func (s *Service) issuedViewAccess(c Context, o map[string]any) error {
	caps := []any{o["domain"]}
	a := o["access"].(map[string]any)
	if a["object"] != nil {
		caps = append(caps, a["object"])
	}
	if a["description"] != nil {
		caps = append(caps, a["description"])
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range caps {
		raw, e := marshal(v)
		if e != nil {
			return e
		}
		var cap Capability
		if json.Unmarshal(raw, &cap) != nil {
			return fsapi.Invalid
		}
		g := s.sessions[c.Session].Grants[cap.Token]
		if g == nil || g.Released || !equalCap(g.Cap, cap) || cap.Scope != c {
			return fsapi.Permission
		}
	}
	return nil
}
func (s *Service) checkTarget(ctx context.Context, c Context, v any) error {
	if e := validateViewTarget(c, v); e != nil {
		return e
	}
	raw, e := marshal(v)
	if e != nil {
		return e
	}
	var target ViewTarget
	if json.Unmarshal(raw, &target) != nil {
		return fsapi.Invalid
	}
	if (target.Kind == "CONTROLLED_LOCAL") != (s.config.Validation != nil) {
		return fsapi.Permission
	}
	return s.config.ViewProtocol.CheckTarget(ctx, c, target)
}
func validateViewResponse(c Command, raw json.RawMessage) error {
	v, e := Canonical(raw)
	if e != nil {
		return e
	}
	if c.Op == "VIEW_RETIRE" {
		o, e := object(v, "status", "floor", "proof_hash")
		if e != nil || !oneOf(text(o["status"]), "RETIRED", "BLOCKED") || !uintRange(o["floor"], 0, ^uint64(0)) {
			return fsapi.Invalid
		}
		if o["status"] == "RETIRED" {
			if !hash(o["proof_hash"]) {
				return fsapi.Invalid
			}
		} else if o["proof_hash"] != nil {
			return fsapi.Invalid
		}
		return nil
	}
	o, e := object(v, "id", "status", "request", "operation", "request_hash", "lease", "reservation", "entry")
	if e != nil || validateHashedObject(o) != nil || validateTicket(o["request"]) != nil {
		return fsapi.Invalid
	}
	request, e := Canonical(c.Payload)
	if e != nil {
		return e
	}
	p := request.(map[string]any)
	if !equalWire(p["request"], o["request"]) {
		return fsapi.Permission
	}
	original := text(o["operation"])
	if !oneOf(original, "VIEW_ACQUIRE", "VIEW_RELEASE", "VIEW_RENEW", "DIRTY_RESERVE", "DIRTY_FREEZE") || !hash(o["request_hash"]) {
		return fsapi.Invalid
	}
	if !oneOf(c.Op, "VIEW_QUERY", "VIEW_CANCEL") && (original != c.Op || o["request_hash"] != wireHash(map[string]any{"op": c.Op, "payload": p})) {
		return fsapi.Stale
	}
	status := text(o["status"])
	if c.Op == "VIEW_CANCEL" && !oneOf(status, "CANCELLED", "PENDING", "INDETERMINATE", "BLOCKED", "RETIRED_ID") {
		return fsapi.Invalid
	}
	switch status {
	case "PENDING", "INDETERMINATE", "BLOCKED", "RETIRED_ID":
		if o["lease"] != nil || o["reservation"] != nil || o["entry"] != nil {
			return fsapi.Invalid
		}
	case "RELEASED", "CANCELLED":
		if o["lease"] != nil || o["reservation"] != nil || o["entry"] != nil {
			return fsapi.Invalid
		}
		if status == "RELEASED" && (original != "VIEW_RELEASE" || !oneOf(c.Op, "VIEW_RELEASE", "VIEW_QUERY")) {
			return fsapi.Invalid
		}
		if status == "CANCELLED" && !oneOf(c.Op, "VIEW_CANCEL", "VIEW_QUERY") {
			return fsapi.Invalid
		}
	case "GRANTED":
		if o["entry"] != nil {
			return fsapi.Invalid
		}
		cred := o["request"].(map[string]any)["credentials"]
		if o["lease"] != nil {
			if o["reservation"] != nil || !oneOf(original, "VIEW_ACQUIRE", "VIEW_RENEW") || validateLease(c.Context, o["lease"]) != nil {
				return fsapi.Invalid
			}
			lease := o["lease"].(map[string]any)
			if lease["credential_hash"] != wireHash(cred) {
				return fsapi.Permission
			}
			if c.Op == "VIEW_ACQUIRE" && (!equalWire(lease["domain"], p["domain"]) || !equalWire(lease["access"], p["access"]) || lease["mode"] != p["mode"] || p["target"] != nil && !equalWire(lease["target"], p["target"])) {
				return fsapi.Stale
			}
			if c.Op == "VIEW_RENEW" {
				old := p["lease"].(map[string]any)
				if number(old["revision"]) == ^uint64(0) || number(lease["revision"]) != number(old["revision"])+1 {
					return fsapi.Stale
				}
				for _, k := range []string{"scope", "domain", "access", "credential_hash", "mode", "target", "fence"} {
					if !equalWire(lease[k], old[k]) {
						return fsapi.Stale
					}
				}
			}
		} else {
			if original != "DIRTY_RESERVE" || validateReservation(c.Context, o["reservation"]) != nil {
				return fsapi.Invalid
			}
			r := o["reservation"].(map[string]any)
			if !equalWire(r["credentials"], cred) {
				return fsapi.Permission
			}
			if c.Op == "DIRTY_RESERVE" {
				if r["lease_id"] != p["lease"].(map[string]any)["id"] {
					return fsapi.Stale
				}
				for _, k := range []string{"description", "offset", "length", "claim_number"} {
					if !equalWire(r[k], p[k]) {
						return fsapi.Stale
					}
				}
			}
		}
	case "FROZEN":
		if original != "DIRTY_FREEZE" || o["lease"] != nil || o["reservation"] != nil {
			return fsapi.Invalid
		}
		entry := o["entry"]
		if validateManifest(map[string]any{"hash": wireHash([]any{entry}), "entries": []any{entry}}) != nil {
			return fsapi.Invalid
		}
		if c.Op == "DIRTY_FREEZE" {
			expected, _ := frozenEntry(p["reservation"].(map[string]any), p["recall"], p["data"], p["claim"].(map[string]any))
			if !equalWire(expected, entry) {
				return fsapi.Stale
			}
		}
	default:
		return fsapi.Invalid
	}
	return nil
}

func (s *Service) checkViewControlTarget(ctx context.Context, c Command, raw json.RawMessage) error {
	v, e := Canonical(raw)
	if e != nil {
		return e
	}
	o := v.(map[string]any)
	if event, ok := o["event"].(map[string]any); ok {
		o = event
	}
	if target := o["target"]; target != nil {
		return s.checkTarget(ctx, c.Context, target)
	}
	return nil
}
