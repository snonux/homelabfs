package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// CleanupVersion selects the explicitly closed parent-funded cleanup successor.
const CleanupVersion = "9P2000.L.hfs1.3"

// CleanupProtocol retains all controlled 1.2 requirements and adds only exact
// parent-funded READDIR cleanup. Earlier tuples retain their closed schemas.
func CleanupProtocol() Protocol {
	return Protocol{BodySchema: 4, BridgeMajor: 1, BridgeMinor: 3, Features: 8191, Msize: 65536, NormalMajor: 3, NormalMinor: 0}
}

// IsControlledProtocol reports the explicitly supported controlled tuples.
// Callers must still pin the actual selected tuple to their signed enrollment.
func IsControlledProtocol(p Protocol) bool {
	return p == ControlledProtocol() || HasCleanupProtocol(p)
}

// ReaddirCleanup closes every transient grant from one exact retained page.
// Capabilities are full tokens in original result order, including un-emitted
// entries. The complete original issuer is immutable.
type ReaddirCleanup struct {
	Claim        Claim        `json:"claim"`
	Credentials  Credentials  `json:"credentials"`
	ResultID     string       `json:"result_id"`
	Capabilities []Capability `json:"capabilities"`
}

// ReaddirClosure is the complete closure package owned by the original claim
// before its filesystem plan may apply. No new ordinary claim funds cleanup.
type ReaddirClosure struct {
	Request json.RawMessage
	Receipt json.RawMessage
	Closed  bool
}

// ReaddirCleanupStore accepts only authenticated exact successor exchanges.
type ReaddirCleanupStore interface {
	CleanupReaddir(context.Context, Context, json.RawMessage) (json.RawMessage, error)
}

// PrepareReaddirClosure derives exact closure from the proposed original result
// and protected grant snapshot. The provider accounts every byte before effect.
func PrepareReaddirClosure(b Binding, result, snapshot json.RawMessage, protocol Protocol) (*ReaddirClosure, error) {
	var page struct {
		Code    string
		ID      string
		Entries []struct{ Capability Capability }
	}
	if json.Unmarshal(result, &page) != nil || page.Code != "OK" || !hash(page.ID) {
		return nil, fsapi.Invalid
	}
	var state validationSnapshot
	if json.Unmarshal(snapshot, &state) != nil || state.CapabilityLifetimes != 2 || state.Context != b.Context {
		return nil, fsapi.Stale
	}
	request := ReaddirCleanup{Claim: b.Call.Claim, Credentials: b.Call.Credentials, ResultID: page.ID, Capabilities: make([]Capability, 0, len(page.Entries))}
	for _, entry := range page.Entries {
		cap := entry.Capability
		g := state.Grants[cap.Token]
		if g == nil || g.Released || !g.NoLookup || cap.Kind != "PATH" || cap.Scope != b.Context || !equalCap(g.Cap, cap) {
			return nil, fsapi.Stale
		}
		g.Released = true
		request.Capabilities = append(request.Capabilities, cap)
	}
	raw, err := marshal(request)
	if err != nil {
		return nil, err
	}
	receipt, err := marshal(map[string]any{"claim": request.Claim, "result_id": page.ID, "request_hash": digest(raw), "status": "CLOSED"})
	if err != nil {
		return nil, err
	}
	// Bound the full response/command envelopes before any filesystem effect.
	// MaxCell and a full reclaim object cover the largest decimal cell spelling.
	if !HasCleanupProtocol(protocol) {
		return nil, fsapi.Permission
	}
	commandEnvelope := map[string]any{"cell": MaxCell, "context": b.Context, "op": "READDIR_CLEANUP", "payload": json.RawMessage(raw), "protocol": protocol, "reclaim": map[string]any{"cell": MaxCell, "response_hash": digest(raw)}}
	if IsMappedProtocol(protocol) {
		commandEnvelope["delegation"] = &Delegation{Version: DelegationVersion, GuestCell: MaxCell, GuestOp: "READDIR_CLEANUP", GuestReclaim: &Ack{Cell: MaxCell, ResponseHash: digest(raw)}}
	}
	envelopes := []any{
		map[string]any{"cell": MaxCell, "context": b.Context, "op": "QUERY", "payload": map[string]any{"claim": b.Call.Claim, "status": "TERMINAL", "result": json.RawMessage(result)}, "protocol": protocol, "verification": nil},
		commandEnvelope,
	}
	for _, envelope := range envelopes {
		encoded, err := marshal(envelope)
		if err != nil {
			return nil, err
		}
		if len(encoded) > BodyBytes {
			return nil, fsapi.NoSpace
		}
	}
	return &ReaddirClosure{Request: raw, Receipt: receipt}, nil
}

// ApplyReaddirCleanup changes only exact original transient grant release bits
// in the current protected snapshot. False becomes true, so the canonical
// replacement must fit the snapshot owner's already charged two stored copies.
func ApplyReaddirCleanup(scope Context, snapshot json.RawMessage, request ReaddirCleanup) (json.RawMessage, error) {
	var state validationSnapshot
	if json.Unmarshal(snapshot, &state) != nil || state.CapabilityLifetimes != 2 || state.Context != scope {
		return nil, fsapi.Stale
	}
	for _, cap := range request.Capabilities {
		g := state.Grants[cap.Token]
		if g == nil || !g.NoLookup || cap.Kind != "PATH" || cap.Scope != scope || !equalCap(g.Cap, cap) {
			return nil, fsapi.Stale
		}
		g.Released = true
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if len(raw) > len(snapshot) {
		return nil, fsapi.NoSpace
	}
	return raw, nil
}

func validateReaddirCleanup(v any) error {
	o, err := object(v, "claim", "credentials", "result_id", "capabilities")
	if err != nil || validateClaim(o["claim"]) != nil || validateCred(o["credentials"]) != nil || !hash(o["result_id"]) {
		return fsapi.Invalid
	}
	caps, ok := o["capabilities"].([]any)
	if !ok || len(caps) > 256 {
		return fsapi.Invalid
	}
	for _, cap := range caps {
		if validateCap(cap) != nil || text(cap.(map[string]any)["kind"]) != "PATH" {
			return fsapi.Invalid
		}
	}
	return nil
}

func (s *Service) cleanupReaddir(ctx context.Context, c Command) (json.RawMessage, error) {
	if c.Protocol == nil || !HasCleanupProtocol(*c.Protocol) || !s.privateValidation() {
		return nil, fsapi.Permission
	}
	store, ok := s.config.Claims.(ReaddirCleanupStore)
	if !ok {
		return nil, fsapi.Unsupported
	}
	var request ReaddirCleanup
	if json.Unmarshal(c.Payload, &request) != nil {
		return nil, fsapi.Invalid
	}
	if err := s.config.Attestor.Verify(ctx, c.Context, request.Credentials); err != nil {
		return nil, err
	}
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	receipt, err := store.CleanupReaddir(ctx, c.Context, bytes.Clone(c.Payload))
	if err != nil {
		return nil, err
	}
	// Preserve every current in-memory credential/process/control field,
	// including metadata registered since the latest protected snapshot.
	// Ordinary grant mutation is serialized by resultMu; credentials use mu.
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[c.Context.Session]
	// A grant already pruned by retirement (o64) is skipped: the provider
	// answered from the exact protected closure of this request, which a
	// READDIR's retirement requires to be closed, so the replay only repeats
	// an applied release. Any grant still present must match.
	for _, cap := range request.Capabilities {
		g := ss.Grants[cap.Token]
		if g != nil && (!g.NoLookup || !equalCap(g.Cap, cap)) {
			return nil, fsapi.Stale
		}
	}
	for _, cap := range request.Capabilities {
		if g := ss.Grants[cap.Token]; g != nil {
			g.Released = true
		}
	}
	return receipt, nil
}
