package guestbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"sync/atomic"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// New requires explicitly verified providers before any delegated effects.
// This constructs a component, never authorizes normal native publication.
func New(ctx context.Context, c Config) (*Service, error) {
	if err := prepareProtocols(&c); err != nil {
		return nil, err
	}
	if c.Validation != nil && (!c.Validation.Disposable || c.Validation.Name == "") {
		return nil, fsapi.Invalid
	}
	if missing(c.Native) || missing(c.Attestor) || missing(c.Claims) || missing(c.Execution) || missing(c.Views) || len(c.Scopes) == 0 || len(c.Scopes) > 64 || !validEndpoint(c.Endpoint) {
		return nil, fsapi.Invalid
	}
	if c.MaxCapabilities == 0 {
		c.MaxCapabilities = 4096
	}
	if c.MaxCapabilityIdentities == 0 {
		c.MaxCapabilityIdentities = 65536
	}
	if c.MaxCapabilityIdentities < c.MaxCapabilities || c.MaxCapabilityIdentities > 65536 {
		return nil, fsapi.Invalid
	}
	if c.MaxCredentialRevisions == 0 {
		c.MaxCredentialRevisions = 4096
	}
	if c.MaxCapabilities < 1 || c.MaxCapabilities > 65536 || c.MaxCredentialRevisions < 1 || c.MaxCredentialRevisions > 65536 {
		return nil, fsapi.Invalid
	}
	for _, provider := range []any{c.Claims, c.Execution} {
		if bound, ok := provider.(interface {
			CheckViewProtocol(Views, ViewProtocolProvider) error
		}); ok {
			if err := bound.CheckViewProtocol(c.Views, c.ViewProtocol); err != nil {
				return nil, err
			}
		}
	}
	if c.Validation != nil {
		profile := *c.Validation
		c.Validation = &profile
	}
	controller, err := randomUUID()
	if err != nil {
		return nil, err
	}
	s := &Service{controller: parseID(controller), config: c, sessions: make(map[string]*session)}
	for _, scope := range c.Scopes {
		for _, provider := range []any{c.Claims, c.Execution} {
			if bound, ok := provider.(interface {
				CheckBridgeProtocol(context.Context, Context, *Protocol) error
			}); ok {
				if err := bound.CheckBridgeProtocol(ctx, scope, c.sessionProtocol(scope.Session)); err != nil {
					return nil, err
				}
			}
		}
		if protocol := c.sessionProtocol(scope.Session); protocol != nil {
			if err := c.ViewProtocol.CheckEnrollment(ctx, scope, *protocol); err != nil {
				return nil, err
			}
		}
		if c.Validation != nil {
			claims, claimBound := c.Claims.(ValidationEnrollment)
			execution, executionBound := c.Execution.(ValidationEnrollment)
			if claimBound || executionBound {
				if !claimBound || !executionBound {
					return nil, fsapi.Permission
				}
				if err := claims.CheckValidation(ctx, scope, *c.Validation, c.Endpoint); err != nil {
					return nil, err
				}
				if err := execution.CheckValidation(ctx, scope, *c.Validation, c.Endpoint); err != nil {
					return nil, err
				}
			}
		}
		b, e := marshal(scope)
		if e != nil {
			return nil, e
		}
		v, e := Canonical(b)
		if e != nil || validateContext(v) != nil || scope.FuseSession != c.Endpoint.FuseSession {
			return nil, fsapi.Invalid
		}
		if _, ok := s.sessions[scope.Session]; ok {
			return nil, fsapi.Invalid
		}
		generation, err := c.Execution.ServingGeneration(ctx, scope)
		if err != nil || !generation.Valid() {
			return nil, fsapi.Unavailable
		}
		if c.Validation == nil {
			if c.Planner == nil {
				return nil, fsapi.Invalid
			}
			normalGeneration, err := c.Planner.DelegatedGeneration(ctx, parseID(scope.Volume), parseID(scope.Generation))
			if err != nil {
				return nil, err
			}
			if generation != normalGeneration {
				return nil, fsapi.Stale
			}
		}
		if e = c.Execution.Check(ctx, scope); e != nil {
			return nil, e
		}
		s.sessions[scope.Session] = &session{Generation: generation, Context: scope, Credentials: map[string][]byte{}, Exited: map[string]uint64{}, Grants: map[string]*grant{}, CloseSequences: map[string]uint64{}}
	}
	for _, scope := range c.Scopes {
		if err := s.restoreValidation(ctx, scope); err != nil {
			return nil, err
		}
	}
	if err := c.Native.Enroll(ctx, c.Endpoint, s.controller); err != nil {
		return nil, err
	}
	if !missing(c.Verification) {
		if err := c.Verification.Bind(ctx, VerificationBinding{Endpoint: c.Endpoint, Controller: s.controller, Native: c.Native, Claims: c.Claims, Execution: c.Execution, Views: c.Views, ViewProtocol: c.ViewProtocol, Scopes: append([]Context(nil), c.Scopes...)}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Authenticate must run for every native callback, including reads of old cells.
func (s *Service) Authenticate(ctx context.Context, n Native) error {
	if n.Endpoint != s.config.Endpoint || len(n.Groups) > 1024 || !validUUID(n.Process) {
		return fsapi.Permission
	}
	return s.config.Native.Verify(ctx, n, s.controller)
}

// CheckScope refuses unregistered or changed scope without touching any ledger.
func (s *Service) CheckScope(c Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[c.Session]
	if ss == nil || ss.Context != c {
		return fsapi.Permission
	}
	return nil
}

// Execute runs outside native vnode callbacks, producing one immutable snapshot.
// Transport receipt is separate from protected operation completion.
func (s *Service) Execute(ctx context.Context, n Native, c Command) ([]byte, error) {
	if err := s.Authenticate(ctx, n); err != nil {
		return nil, err
	}
	raw, validationErr := marshal(c)
	if validationErr != nil {
		return nil, validationErr
	}
	validated, validationErr := DecodeCommandVersion(raw, commandMinor(c))
	if validationErr != nil {
		return nil, validationErr
	}
	c = validated
	expectedProtocol := s.config.sessionProtocol(c.Context.Session)
	if (c.Protocol == nil) != (expectedProtocol == nil) || c.Protocol != nil && *c.Protocol != *expectedProtocol {
		return nil, fsapi.Permission
	}
	if e := s.CheckScope(c.Context); e != nil {
		return nil, e
	}
	ctx = observeCommand(ctx, n, s.controller, c, raw)
	if commandMinor(c) == 4 {
		s.mu.Lock()
		transport := s.transport
		s.mu.Unlock()
		if transport == nil || c.Delegation == nil {
			return nil, fsapi.Permission
		}
		if err := transport.admitMapping(ctx, c); err != nil {
			return nil, err
		}
	}
	if c.Protocol != nil && IsControlledProtocol(*c.Protocol) && !missing(s.config.Verification) {
		if err := s.config.Verification.Prepare(ctx, c); err != nil {
			return nil, err
		}
	}
	if c.Op != "RELEASE_CELL" {
		if e := s.config.Execution.Check(ctx, c.Context); e != nil {
			return nil, e
		}
	}
	s.mu.Lock()
	ss := s.sessions[c.Context.Session]
	hello := ss.Hello
	s.mu.Unlock()
	if c.Op != "HELLO" && !hello &&
		(c.Op != "SESSION_RETIRE" || c.Protocol == nil || *c.Protocol != TerminalProtocol()) {
		return nil, fsapi.Permission
	}
	// A codec-valid controlled view exchange cannot stand in for a missing
	// runtime source/event producer and its verified receipt.
	if missing(s.config.Verification) && c.Protocol != nil && IsControlledProtocol(*c.Protocol) && (isViewExtension(c.Op) || c.Op == "VIEW_POLL" || c.Op == "VIEW_ACK") {
		return nil, fsapi.Unsupported
	}
	if c.Protocol != nil && c.Op != "HELLO" && !isAllocation(c.Op) && c.Op != "READDIR_CLEANUP" {
		if err := s.config.ViewProtocol.CheckRequest(ctx, c.Context, c.Op, bytes.Clone(c.Payload)); err != nil {
			return nil, err
		}
	}
	var payload any
	var err error
	switch c.Op {
	case "HELLO":
		s.mu.Lock()
		ss.Hello = true
		s.mu.Unlock()
		payload = map[string]any{"status": "ACCEPTED", "selection": json.RawMessage(c.Payload)}
	case "ALLOCATE", "GAP_RESOLVE":
		payload, err = s.allocation(ctx, c)
	case "READDIR_CLEANUP":
		payload, err = s.cleanupReaddir(ctx, c)
	case "CALL":
		payload, err = s.call(ctx, c)
	case "QUERY", "CANCEL":
		var p struct {
			Claim Claim `json:"claim"`
		}
		if err = json.Unmarshal(c.Payload, &p); err != nil {
			return nil, fsapi.Invalid
		}
		var state ClaimState
		if c.Op == "CANCEL" {
			state, err = s.config.Claims.Cancel(ctx, c.Context, p.Claim)
		} else {
			state, err = s.config.Claims.Resolve(ctx, c.Context, p.Claim)
		}
		if err == nil {
			declinedQuery := c.Op == "QUERY" && c.Protocol != nil && IsMappedProtocol(*c.Protocol)
			if state.Status != "DECLINED" || !declinedQuery {
				state, err = s.recoverValidation(ctx, state)
				if err != nil {
					return nil, err
				}
			}
			if state.Status == "TERMINAL" {
				if _, e := validateResult(state.Result); e != nil {
					return nil, e
				}
			}
			payload, err = queryPayload(p.Claim, state, declinedQuery)
		}
	case "RETIRE":
		var b json.RawMessage
		if c.Protocol != nil && IsControlledProtocol(*c.Protocol) {
			b, err = s.retireControlled(ctx, c)
		} else {
			b, err = s.config.Claims.Retire(ctx, c.Context, bytes.Clone(c.Payload))
		}
		if err == nil {
			err = validateControlResponse(c, b)
		}
		payload = b
	case "SESSION_RETIRE":
		payload, err = s.retireSession(ctx, c)
	case "RELEASE_CELL":
		payload = map[string]any{"status": "RELEASED"}
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
		payload = map[string]any{"status": "BLOCKED", "ledger_hash": p.LedgerHash}
	case "VIEW_POLL", "VIEW_ACK":
		if c.Protocol != nil {
			if err = s.checkViewControlTarget(ctx, c, c.Payload); err != nil {
				return nil, err
			}
		}
		if c.Op != "RESUME" {
			if err = s.validateDomain(c); err != nil {
				return nil, err
			}
		}
		var b json.RawMessage
		b, err = s.config.Views.Exchange(ctx, c.Context, c.Op, bytes.Clone(c.Payload))
		if err == nil {
			err = validateControlResponse(c, b)
			if err == nil && c.Protocol != nil {
				err = s.checkViewControlTarget(ctx, c, b)
			}
		}
		payload = b
	default:
		if !isViewExtension(c.Op) || c.Protocol == nil {
			return nil, fsapi.Unsupported
		}
		payload, err = s.exchangeView(ctx, c)
	}
	if err != nil {
		return nil, err
	}
	response := map[string]any{"cell": c.Cell, "context": c.Context, "op": c.Op, "payload": payload}
	if c.Protocol != nil {
		response["protocol"] = c.Protocol
		if IsControlledProtocol(*c.Protocol) {
			if commandMinor(c) == 4 {
				base, err := marshal(response)
				if err != nil {
					return nil, err
				}
				s.mu.Lock()
				transport := s.transport
				s.mu.Unlock()
				ctx, err = transport.finishMappingBase(ctx, c, base)
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
				response["verification"] = receipt
				return marshal(response)
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
		}
	}
	return marshal(response)
}
func (s *Service) call(ctx context.Context, c Command) (any, error) {
	var call Call
	if err := json.Unmarshal(c.Payload, &call); err != nil {
		return nil, fsapi.Invalid
	}
	raw, err := marshal(call)
	if err != nil {
		return nil, err
	}
	binding := Binding{Context: c.Context, Call: call, Canonical: raw}
	// Stable anchor lookup precedes capabilities, owner/origin selection and paths.
	old, err := s.config.Claims.Resolve(ctx, c.Context, call.Claim)
	if err != nil {
		return nil, err
	}
	if old.Status != "UNKNOWN" {
		if old.Status == "RETIRED_ID" {
			return callPayload(call.Claim, "REJECTED", "RETIRED_ID"), nil
		}
		if !bytes.Equal(binding.Canonical, old.Binding.Canonical) || (binding.Context != old.Binding.Context && (old.ReconciledScope == nil || *old.ReconciledScope != binding.Context)) {
			return callPayload(call.Claim, "REJECTED", "ID_CONFLICT"), nil
		}
		if _, err = s.recoverValidation(ctx, old); err != nil {
			return nil, err
		}
		return callPayload(call.Claim, "ADMITTED", "OK"), nil
	}
	private := s.privateValidation()
	if private {
		s.resultMu.Lock()
		defer s.resultMu.Unlock()
		if err = s.restoreValidation(ctx, c.Context); err != nil {
			return nil, err
		}
	}
	if err = s.credentials(ctx, c.Context, call.Credentials, call.Intent.Opcode == "DRAIN_WRITE" || s.originalCleanup(binding)); err != nil {
		return callPayload(call.Claim, "REJECTED", code(err)), nil
	}
	state, err := s.config.Claims.Reserve(ctx, binding)
	if err != nil {
		return callPayload(call.Claim, "REJECTED", code(err)), nil
	}
	if !sameBinding(binding, state.Binding) || !state.Binding.Operation.Valid() {
		return nil, fsapi.Unavailable
	}
	binding = state.Binding
	if state.Status != "RESERVED" {
		if state.Status == "PENDING" || state.Status == "TERMINAL" {
			return callPayload(call.Claim, "ADMITTED", "OK"), nil
		}
		return nil, fsapi.Unavailable
	}
	return s.executeCall(ctx, binding, false)
}

// executeCall retains the original operation, allocation and canonical credentials.
// Private callers hold resultMu across restore, preparation and snapshot protection.
func (s *Service) executeCall(ctx context.Context, binding Binding, resume bool) (any, error) {
	private := s.privateValidation()
	var err error
	need := capNeeds(binding.Call.Intent)
	s.mu.Lock()
	ss := s.sessions[binding.Context.Session]
	enough := liveGrants(ss.Grants)+ss.Reserved+need <= s.config.MaxCapabilities && len(ss.Grants)+ss.Reserved+need <= s.config.MaxCapabilityIdentities
	if enough {
		ss.Reserved += need
	}
	s.mu.Unlock()
	if enough {
		defer func() { s.mu.Lock(); ss.Reserved -= need; s.mu.Unlock() }()
	}
	var prepared atomic.Bool
	prepare := func(o Captured) (Prepared, error) {
		if !prepared.CompareAndSwap(false, true) {
			return Prepared{}, fsapi.Stale
		}
		if s.config.Validation == nil {
			cut, ok := o.(*LogicalCut)
			if !ok || cut.planner != s.config.Planner {
				return Prepared{}, fsapi.Permission
			}
		}
		if !enough {
			return Prepared{}, fsapi.NoSpace
		}
		if s.privateValidation() {
			if err := s.credentials(ctx, binding.Context, binding.Call.Credentials, binding.Call.Intent.Opcode == "DRAIN_WRITE" || s.originalCleanup(binding)); err != nil {
				return Prepared{}, err
			}
		}
		return s.prepare(ctx, binding, o)
	}
	var out Outcome
	if validation, ok := s.config.Execution.(ValidationExecution); s.config.Validation != nil && ok {
		protect := func(out Outcome) (json.RawMessage, json.RawMessage, error) {
			return s.prepareValidationResult(ctx, binding, out)
		}
		if resume {
			continuation, ok := s.config.Execution.(ValidationContinuation)
			if !ok {
				return nil, fsapi.Unsupported
			}
			out, err = continuation.ResumeValidation(ctx, binding, prepare, protect)
		} else {
			out, err = validation.ExecuteValidation(ctx, binding, prepare, protect)
		}
		if err == nil {
			err = s.restoreValidation(context.WithoutCancel(ctx), binding.Context)
		}
	} else {
		out, err = s.config.Execution.Execute(ctx, binding, prepare)
	}
	// Never synthesize no-effect after reservation or dispatch uncertainty.
	if err != nil || !prepared.Load() {
		return callPayload(binding.Call.Claim, "ADMITTED", "OK"), nil
	}
	if private {
		protected, e := s.config.Claims.Resolve(ctx, binding.Context, binding.Call.Claim)
		if e != nil {
			return nil, e
		}
		if protected.Status != "TERMINAL" {
			return nil, fsapi.Unavailable
		}
		return callPayload(binding.Call.Claim, "ADMITTED", "OK"), nil
	}
	_, err = s.finalizeResult(ctx, binding, out)
	if err != nil {
		return nil, err
	}
	return callPayload(binding.Call.Claim, "ADMITTED", "OK"), nil
}

// originalCleanup permits only authority reduction by the exact retained issuer
// after process exit. It cannot admit a fresh credential revision or revive any
// ordinary request, and preparation rechecks the same live grant before effects.
func (s *Service) originalCleanup(b Binding) bool {
	var a arguments
	if json.Unmarshal(b.Call.Intent.Args, &a) != nil {
		return false
	}
	var cap Capability
	switch b.Call.Intent.Opcode {
	case "FORGET":
		cap = a.cap("object")
		if cap.Kind != "PATH" || a.number("count") != 1 {
			return false
		}
	case "DESCRIPTION_RELEASE":
		cap = a.cap("description")
		if cap.Kind != "DESCRIPTION" {
			return false
		}
	default:
		return false
	}
	raw, err := marshal(b.Call.Credentials)
	if err != nil {
		return false
	}
	key := b.Call.Credentials.Process + ":" + strconv.FormatUint(b.Call.Credentials.Revision, 10)
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[b.Context.Session]
	g := ss.Grants[cap.Token]
	return g != nil && !g.Released && cap.Scope == b.Context && equalCap(g.Cap, cap) && cap.CredentialHash == digest(raw) && bytes.Equal(ss.Credentials[key], raw)
}

func (s *Service) credentials(ctx context.Context, c Context, cred Credentials, allowExited bool) error {
	if err := s.config.Attestor.Verify(ctx, c, cred); err != nil {
		return err
	}
	raw, e := marshal(cred)
	if e != nil {
		return e
	}
	key := cred.Process + ":" + strconv.FormatUint(cred.Revision, 10)
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.sessions[c.Session]
	if old, ok := ss.Credentials[key]; ok {
		if !bytes.Equal(old, raw) {
			return fsapi.Stale
		}
		if _, dead := ss.Exited[cred.Process]; dead && !allowExited {
			return fsapi.Stale
		}
		return nil
	}
	if _, dead := ss.Exited[cred.Process]; dead {
		return fsapi.Stale
	}
	if len(ss.Credentials) >= s.config.MaxCredentialRevisions {
		return fsapi.NoSpace
	}
	ss.Credentials[key] = raw
	return nil
}
func (s *Service) validateDomain(c Command) error {
	var p struct {
		Domain    Capability `json:"domain"`
		Session   string     `json:"session"`
		Authority string     `json:"authority_hash"`
	}
	if err := json.Unmarshal(c.Payload, &p); err != nil {
		return fsapi.Invalid
	}
	if c.Op == "VIEW_ACK" && (p.Session != c.Context.Session || p.Authority != c.Context.AuthorityHash) {
		return fsapi.Permission
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.sessions[c.Context.Session].Grants[p.Domain.Token]
	if g == nil || g.Released || g.Cap.Kind != "DOMAIN" || !equalCap(g.Cap, p.Domain) || p.Domain.Scope != c.Context {
		return fsapi.Permission
	}
	return nil
}
func validEndpoint(e Endpoint) bool {
	return validUUID(e.FuseSession) && e.CommandNode > 1 && e.ResultNode > 1 && e.RecallNode > 1 && e.CommandNode != e.ResultNode && e.ResultNode != e.RecallNode && e.CommandNode != e.RecallNode
}
func validUUID(s string) bool { _, e := fsapi.ParseUUID(s); return e == nil }
func sameBinding(a, b Binding) bool {
	return a.Context == b.Context && bytes.Equal(a.Canonical, b.Canonical)
}
func callPayload(c Claim, status, code string) any {
	return map[string]any{"status": status, "claim": c, "code": code}
}
func queryPayload(c Claim, s ClaimState, declinedQuery bool) (any, error) {
	status := s.Status
	if status == "UNKNOWN" || status == "RESERVED" {
		status = "INDETERMINATE"
	}
	if !oneOf(status, "PENDING", "INDETERMINATE", "TERMINAL", "RETIRED_ID") && (!declinedQuery || status != "DECLINED" || len(s.Result) != 0) {
		return nil, fsapi.Unavailable
	}
	var result any
	if status == "TERMINAL" {
		if len(s.Result) == 0 {
			return nil, fsapi.Unavailable
		}
		result = json.RawMessage(s.Result)
	}
	return map[string]any{"status": status, "claim": c, "result": result}, nil
}
func code(err error) string {
	for _, v := range []fsapi.Errno{fsapi.Invalid, fsapi.NotFound, fsapi.Exists, fsapi.Permission, fsapi.ReadOnlyFS, fsapi.NotDirectory, fsapi.IsDirectory, fsapi.NotEmpty, fsapi.CrossDevice, fsapi.Unsupported, fsapi.NoSpace, fsapi.Quota, fsapi.IO, fsapi.Unavailable, fsapi.Stale, fsapi.Interrupted, fsapi.Busy, fsapi.WouldBlock, fsapi.TooManyLinks, fsapi.NameTooLong} {
		if errors.Is(err, v) {
			return errnoNames()[v]
		}
	}
	return "IO"
}
func randomToken() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func randomUUID() (string, error) {
	var id fsapi.UUID
	if _, e := rand.Read(id[:]); e != nil {
		return "", e
	}
	if !id.Valid() {
		return "", fsapi.IO
	}
	return id.String(), nil
}
func equalCap(a, b Capability) bool {
	x, e := marshal(a)
	if e != nil {
		return false
	}
	y, e := marshal(b)
	return e == nil && bytes.Equal(x, y)
}
func guestCred(c Credentials) fsapi.Cred {
	return fsapi.Cred{UID: c.FSUID, GID: c.FSGID, Groups: slices.Clone(c.Groups)}
}

func missing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return r.IsNil()
	}
	return false
}
