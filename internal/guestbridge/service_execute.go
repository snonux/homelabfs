package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Execute runs outside native vnode callbacks, producing one immutable snapshot.
// Transport receipt is separate from protected operation completion.
//
// It runs in three steps: admitExecution authenticates and checks the
// command, dispatchOperation performs its operation and respond builds the
// response envelope and its verification receipt.
//
// A mapped (minor 4) controlled cell whose response base the transport has
// already protected skips the operation: a retry after a transient
// verification refusal reuses that exact base (nc4, see retainedBase).
func (s *Service) Execute(ctx context.Context, n Native, c Command) ([]byte, error) {
	ctx, c, err := s.admitExecution(ctx, n, c)
	if err != nil {
		return nil, err
	}
	if base, ok := s.retainedMappedBase(ctx, c); ok {
		return s.finishMapped(ctx, c, base)
	}
	payload, err := s.dispatchOperation(ctx, c)
	if err != nil {
		return nil, err
	}
	return s.respond(ctx, c, payload)
}

// admitExecution authenticates the native caller, revalidates the command
// bytes, protocol and scope, admits a mapped cell's mapping and runs the
// verification preparation and every request check that precedes the
// operation. It returns the observed context and the validated command.
func (s *Service) admitExecution(ctx context.Context, n Native, c Command) (context.Context, Command, error) {
	if err := s.Authenticate(ctx, n); err != nil {
		return ctx, c, err
	}
	raw, err := marshal(c)
	if err != nil {
		return ctx, c, err
	}
	c, err = DecodeCommandVersion(raw, commandMinor(c))
	if err != nil {
		return ctx, c, err
	}
	expectedProtocol := s.config.sessionProtocol(c.Context.Session)
	if (c.Protocol == nil) != (expectedProtocol == nil) || c.Protocol != nil && *c.Protocol != *expectedProtocol {
		return ctx, c, fsapi.Permission
	}
	if err = s.CheckScope(c.Context); err != nil {
		return ctx, c, err
	}
	ctx = observeCommand(ctx, n, s.controller, c, raw)
	if commandMinor(c) == 4 {
		transport := s.attachedTransport()
		if transport == nil || c.Delegation == nil {
			return ctx, c, fsapi.Permission
		}
		if err = transport.admitMapping(ctx, c); err != nil {
			return ctx, c, err
		}
	}
	return ctx, c, s.checkExecution(ctx, c)
}

// checkExecution runs the verification preparation, the execution check, the
// HELLO gate and the view protocol request check of one admitted command.
func (s *Service) checkExecution(ctx context.Context, c Command) error {
	if c.Protocol != nil && IsControlledProtocol(*c.Protocol) && !missing(s.config.Verification) {
		if err := s.config.Verification.Prepare(ctx, c); err != nil {
			return err
		}
	}
	if c.Op != "RELEASE_CELL" {
		if err := s.config.Execution.Check(ctx, c.Context); err != nil {
			return err
		}
	}
	s.mu.Lock()
	hello := s.sessions[c.Context.Session].Hello
	s.mu.Unlock()
	if c.Op != "HELLO" && !hello &&
		(c.Op != "SESSION_RETIRE" || c.Protocol == nil || *c.Protocol != TerminalProtocol()) {
		return fsapi.Permission
	}
	// A codec-valid controlled view exchange cannot stand in for a missing
	// runtime source/event producer and its verified receipt.
	if missing(s.config.Verification) && c.Protocol != nil && IsControlledProtocol(*c.Protocol) && (isViewExtension(c.Op) || c.Op == "VIEW_POLL" || c.Op == "VIEW_ACK") {
		return fsapi.Unsupported
	}
	// ALLOCATE/GAP_RESOLVE, READDIR_CLEANUP and ABANDON (hh4, abandon.go)
	// are claim-ledger control work, not view operations: the claims provider
	// authenticates them against the exact original bindings itself.
	if c.Protocol != nil && c.Op != "HELLO" && !isAllocation(c.Op) && c.Op != "READDIR_CLEANUP" && c.Op != "ABANDON" {
		if err := s.config.ViewProtocol.CheckRequest(ctx, c.Context, c.Op, bytes.Clone(c.Payload)); err != nil {
			return err
		}
	}
	return nil
}

// attachedTransport returns the transport attached to this service, if any.
func (s *Service) attachedTransport() *Transport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transport
}

// dispatchOperation performs the operation of one admitted command and
// returns its response payload.
func (s *Service) dispatchOperation(ctx context.Context, c Command) (any, error) {
	switch c.Op {
	case "HELLO":
		s.mu.Lock()
		s.sessions[c.Context.Session].Hello = true
		s.mu.Unlock()
		return map[string]any{"status": "ACCEPTED", "selection": json.RawMessage(c.Payload)}, nil
	case "ALLOCATE", "GAP_RESOLVE":
		return s.allocation(ctx, c)
	case "READDIR_CLEANUP":
		return s.cleanupReaddir(ctx, c)
	case "CALL":
		return s.call(ctx, c)
	case "QUERY", "CANCEL":
		return s.queryClaim(ctx, c)
	case "RETIRE":
		return s.retire(ctx, c)
	case "SESSION_RETIRE":
		return s.retireSession(ctx, c)
	case "ABANDON":
		return s.abandon(ctx, c)
	case "RELEASE_CELL":
		return map[string]any{"status": "RELEASED"}, nil
	case "RESUME":
		// No reconstruction/install API exists yet for the controller-owned cell,
		// response/floor and capability maps. Opaque callback success cannot cover
		// missing bytes or reset state, even with unchanged endpoint UUIDs.
		var p struct {
			LedgerHash string `json:"ledger_hash"`
		}
		if json.Unmarshal(c.Payload, &p) != nil {
			return nil, fsapi.Invalid
		}
		return map[string]any{"status": "BLOCKED", "ledger_hash": p.LedgerHash}, nil
	case "VIEW_POLL", "VIEW_ACK":
		return s.exchangeControl(ctx, c)
	}
	if !isViewExtension(c.Op) || c.Protocol == nil {
		return nil, fsapi.Unsupported
	}
	return s.exchangeView(ctx, c)
}

// queryClaim answers QUERY and CANCEL from the claim owner, recovering the
// validation state of a claim that is not a declined mapped QUERY.
func (s *Service) queryClaim(ctx context.Context, c Command) (any, error) {
	var p struct {
		Claim Claim `json:"claim"`
	}
	if err := json.Unmarshal(c.Payload, &p); err != nil {
		return nil, fsapi.Invalid
	}
	var state ClaimState
	var err error
	if c.Op == "CANCEL" {
		state, err = s.config.Claims.Cancel(ctx, c.Context, p.Claim)
	} else {
		state, err = s.config.Claims.Resolve(ctx, c.Context, p.Claim)
	}
	if err != nil {
		return nil, err
	}
	declinedQuery := c.Op == "QUERY" && c.Protocol != nil && IsMappedProtocol(*c.Protocol)
	if state.Status != "DECLINED" || !declinedQuery {
		state, err = s.recoverValidation(ctx, state)
		if err != nil {
			return nil, err
		}
	}
	if state.Status == "TERMINAL" {
		if _, err = validateResult(state.Result); err != nil {
			return nil, err
		}
	}
	return queryPayload(p.Claim, state, declinedQuery)
}

// exchangeControl runs one VIEW_POLL or VIEW_ACK exchange with the view
// control boundary and checks its reply.
func (s *Service) exchangeControl(ctx context.Context, c Command) (any, error) {
	if c.Protocol != nil {
		if err := s.checkViewControlTarget(ctx, c, c.Payload); err != nil {
			return nil, err
		}
	}
	if err := s.validateDomain(c); err != nil {
		return nil, err
	}
	b, err := s.config.Views.Exchange(ctx, c.Context, c.Op, bytes.Clone(c.Payload))
	if err == nil {
		err = validateControlResponse(c, b)
		if err == nil && c.Protocol != nil {
			err = s.checkViewControlTarget(ctx, c, b)
		}
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// respond builds the response envelope of one executed command. A controlled
// response carries a verification receipt over its base envelope; a mapped
// one is finished by finishMapped after the transport protected the base.
func (s *Service) respond(ctx context.Context, c Command, payload any) ([]byte, error) {
	response := map[string]any{"cell": c.Cell, "context": c.Context, "op": c.Op, "payload": payload}
	if c.Protocol == nil {
		return marshal(response)
	}
	response["protocol"] = c.Protocol
	if !IsControlledProtocol(*c.Protocol) {
		return marshal(response)
	}
	if commandMinor(c) == 4 {
		base, err := marshal(response)
		if err != nil {
			return nil, err
		}
		return s.finishMapped(ctx, c, base)
	}
	var receipt *VerificationReceipt
	if !missing(s.config.Verification) {
		base, err := marshal(response)
		if err != nil {
			return nil, err
		}
		receipt, err = s.config.Verification.Produce(ctx, c, base)
		if err != nil {
			return nil, err
		}
		if receipt != nil {
			encoded, err := marshal(receipt)
			if err != nil {
				return nil, err
			}
			if _, err = DecodeVerificationReceipt(encoded); err != nil {
				return nil, err
			}
		}
	}
	response["verification"] = receipt
	return marshal(response)
}

// finishMapped protects the base envelope of a mapped cell (the first time;
// a retry passes the retained base, which matches), signs the mapped receipt
// over it and returns base plus "verification". The base is canonical marshal
// output, so re-encoding its top-level fields reproduces it exactly.
func (s *Service) finishMapped(ctx context.Context, c Command, base []byte) ([]byte, error) {
	transport := s.attachedTransport()
	if transport == nil {
		return nil, fsapi.Permission
	}
	ctx, err := transport.finishMappingBase(ctx, c, base)
	if err != nil {
		return nil, err
	}
	producer, ok := s.config.Verification.(MappedVerificationProducer)
	if !ok {
		return nil, fsapi.Permission
	}
	receipt, err := producer.ProduceMapped(ctx, c, base)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		encoded, err := marshal(receipt)
		if err != nil {
			return nil, err
		}
		if _, err = DecodeVerificationReceiptV2(encoded); err != nil {
			return nil, err
		}
	}
	var response map[string]json.RawMessage
	if err = json.Unmarshal(base, &response); err != nil {
		return nil, err
	}
	encoded, err := marshal(receipt)
	if err != nil {
		return nil, err
	}
	response["verification"] = encoded
	return marshal(response)
}

// retainedMappedBase returns the protected response base of a mapped
// controlled cell that is retried by its worker (see Transport.retainedBase).
func (s *Service) retainedMappedBase(ctx context.Context, c Command) ([]byte, bool) {
	if commandMinor(c) != 4 || c.Protocol == nil || !IsControlledProtocol(*c.Protocol) {
		return nil, false
	}
	transport := s.attachedTransport()
	if transport == nil {
		return nil, false
	}
	return transport.retainedBase(ctx, c)
}
