//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Producer owns canonical controlled-local receipts from the actual Facts,
// controller, provider and pinned enrolled endpoint signing key. It never signs
// normal publication or a caller-supplied evidence object.
type Producer struct {
	mu         sync.Mutex
	facts      *Facts
	controller *viewcontrol.Controller
	providers  map[string]*controlledvalidation.Provider
	keys       map[string]ed25519.PrivateKey
	bindings   map[string]fsapi.UUID
	transports map[string]*guestbridge.Transport
}

var _ guestbridge.VerificationProducer = (*Producer)(nil)
var _ guestbridge.MappedVerificationProducer = (*Producer)(nil)

// NewProducer binds real local owners and verifies every endpoint signing key
// against the already enrolled host key. No verification callback is accepted.
func NewProducer(ctx context.Context, facts *Facts, controller *viewcontrol.Controller, providers map[string]*controlledvalidation.Provider, keys map[string]ed25519.PrivateKey) (*Producer, error) {
	if facts == nil || controller == nil || !controller.UsesFacts(facts.service, facts) || len(providers) != len(facts.enrollment.Domains) || len(keys) != len(providers) {
		return nil, fsapi.Permission
	}
	if err := facts.CheckEnrollment(ctx, facts.enrollment); err != nil {
		return nil, err
	}
	p := &Producer{facts: facts, controller: controller, providers: map[string]*controlledvalidation.Provider{}, keys: map[string]ed25519.PrivateKey{}, bindings: map[string]fsapi.UUID{}, transports: map[string]*guestbridge.Transport{}}
	for _, e := range facts.enrollment.Domains {
		provider, key := providers[e.Scope.Session], keys[e.Scope.Session]
		if provider == nil || !provider.UsesFacts(facts) || !provider.UsesService(facts.service) || !sameJSON(provider.Enrollment(), e) || len(key) != ed25519.PrivateKeySize || !sameJSON(key.Public(), ed25519.PublicKey(e.HostKey)) {
			return nil, fsapi.Permission
		}
		p.providers[e.Scope.Session] = provider
		p.keys[e.Scope.Session] = append(ed25519.PrivateKey(nil), key...)
	}
	return p, nil
}

// Bind checks actual constructed bridge/provider objects and its nonreused
// controller, after native enrollment. A copied identity is insufficient.
func (p *Producer) Bind(ctx context.Context, b guestbridge.VerificationBinding) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !b.Controller.Valid() || len(b.Scopes) == 0 || len(b.Scopes) > 64 || b.Views != p.controller || b.ViewProtocol != p.controller {
		return fsapi.Permission
	}
	for _, scope := range b.Scopes {
		a := p.facts.admission(scope)
		provider := p.providers[scope.Session]
		if a == nil || provider == nil || b.Native != a.supervisor || b.Endpoint != a.endpoint || !p.providerBinding(b, scope, provider) || p.bindings[scope.Session].Valid() {
			return fsapi.Permission
		}
		if err := a.check(scope); err != nil {
			return err
		}
	}
	for _, scope := range b.Scopes {
		p.bindings[scope.Session] = b.Controller
	}
	return ctx.Err()
}

// Prepare refreshes all complete installed inventories before dispatch acquires
// controller/service locks. Uncertainty fences the admission and retains owners.
func (p *Producer) Prepare(ctx context.Context, c guestbridge.Command) error {
	if err := p.observed(ctx, c); err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, a := range p.facts.admissions {
		a.mu.Lock()
		refresh := c.Op == "VIEW_ACK" || time.Since(a.refreshed) >= 5*time.Second
		a.mu.Unlock()
		if refresh {
			if err := a.Refresh(bounded); err != nil {
				return err
			}
		}
	}
	return p.facts.Authorize(ctx, c.Context, c.Op, c.Payload)
}

// Produce verifies actual retained bytes/events or an exact protected full-
// original floor, then binds and signs the original command and response bytes.
func (p *Producer) Produce(ctx context.Context, c guestbridge.Command, response json.RawMessage) (*guestbridge.VerificationReceipt, error) {
	if c.Protocol != nil && *c.Protocol == guestbridge.MappedProtocol() {
		return nil, fsapi.Permission
	}
	return p.produce(ctx, c, response)
}

func (p *Producer) produce(ctx context.Context, c guestbridge.Command, response json.RawMessage) (*guestbridge.VerificationReceipt, error) {
	if err := p.observed(ctx, c); err != nil {
		return nil, err
	}
	if err := p.facts.Authorize(ctx, c.Context, c.Op, c.Payload); err != nil {
		return nil, err
	}
	var body struct {
		Cell     uint64               `json:"cell"`
		Context  guestbridge.Context  `json:"context"`
		Op       string               `json:"op"`
		Protocol guestbridge.Protocol `json:"protocol"`
		Payload  json.RawMessage      `json:"payload"`
	}
	if err := strictJSON(response, &body); err != nil {
		return nil, err
	}
	if body.Cell != c.Cell || body.Context != c.Context || body.Op != c.Op || !p.facts.matchesProtocol(c.Context, body.Protocol) {
		return nil, fsapi.Stale
	}
	var kind string
	var evidence json.RawMessage
	var err error
	switch {
	case c.Op == "READDIR_CLEANUP":
		// Exact authenticated authority reduction grants no new source/floor.
		return nil, nil
	case c.Op == "RETIRE":
		kind, evidence, err = p.floor(ctx, c, body.Payload)
	case c.Op == "VIEW_RETIRE" && c.Protocol != nil && *c.Protocol == guestbridge.MappedProtocol():
		kind, evidence, err = p.viewFloor(ctx, c, body.Payload)
	case c.Op == "VIEW_POLL" || c.Op == "VIEW_ACK" || isViewOperation(c.Op):
		kind, evidence, err = p.controller.ReceiptEvidence(ctx, c, body.Payload)
	}
	if err != nil || kind == "" {
		return nil, err
	}
	observed, _ := guestbridge.VerifiedRequest(ctx)
	var generation fsapi.UUID
	for _, e := range p.facts.enrollment.Domains {
		if e.Scope == c.Context {
			generation = e.ServingGeneration
		}
	}
	r := guestbridge.VerificationReceipt{Version: "homelabfs-controlled-verification/1", Kind: kind, Scope: c.Context, Endpoint: observed.Native.Endpoint, Controller: observed.Controller.String(), ServingGeneration: generation.String(), Cell: c.Cell, CommandHash: observed.CommandHash, ResponseHash: hashBytes(response), Evidence: evidence}
	unsigned, err := unsignedReceipt(r)
	if err != nil {
		return nil, err
	}
	r.Signature = hex.EncodeToString(ed25519.Sign(p.keys[c.Context.Session], unsigned))
	raw, err := canonicalJSON(r)
	if err != nil {
		return nil, err
	}
	// VIEW_FLOOR is emitted only through ProduceMapped, which validates the
	// complete V2 receipt after adding its protected original/native mapping.
	if kind != "VIEW_FLOOR" {
		if _, err = guestbridge.DecodeVerificationReceipt(raw); err != nil {
			return nil, err
		}
	}
	return &r, nil
}

func (p *Producer) observed(ctx context.Context, c guestbridge.Command) error {
	o, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || o.Scope != c.Context || o.Cell != c.Cell || o.Operation != c.Op || o.PayloadHash != hashBytes(c.Payload) || c.Protocol == nil || !p.facts.matchesProtocol(c.Context, *c.Protocol) {
		return fsapi.Permission
	}
	raw, err := canonicalJSON(c)
	if err != nil {
		return err
	}
	if hashBytes(raw) != o.CommandHash {
		return fsapi.Permission
	}
	p.mu.Lock()
	controller := p.bindings[c.Context.Session]
	p.mu.Unlock()
	if !controller.Valid() || controller != o.Controller {
		return fsapi.Permission
	}
	return nil
}

func (p *Producer) floor(ctx context.Context, c guestbridge.Command, payload json.RawMessage) (string, json.RawMessage, error) {
	var reply struct {
		Status    string
		Floor     uint64
		ProofHash string `json:"proof_hash"`
	}
	if json.Unmarshal(payload, &reply) != nil {
		return "", nil, fsapi.Invalid
	}
	if reply.Status != "RETIRED" {
		return "", nil, nil
	}
	provider := p.providers[c.Context.Session]
	if err := provider.VerifyRetirementReceipt(ctx, c.Context, c.Payload, payload); err != nil {
		return "", nil, err
	}
	var request struct {
		Realm, Subject, Anchor, Nonce string
		Through                       uint64
		Credentials                   guestbridge.Credentials
	}
	if json.Unmarshal(c.Payload, &request) != nil || reply.Floor != request.Through {
		return "", nil, fsapi.Stale
	}
	evidence, err := canonicalJSON(map[string]any{"realm": request.Realm, "subject": request.Subject, "anchor": request.Anchor, "nonce": request.Nonce, "through": request.Through, "proof_hash": reply.ProofHash, "credentials": request.Credentials})
	return "FLOOR", evidence, err
}

// viewFloor consumes only the actual controller's protected original receipt.
func (p *Producer) viewFloor(ctx context.Context, c guestbridge.Command, payload json.RawMessage) (string, json.RawMessage, error) {
	if err := p.controller.CheckResponse(ctx, c.Context, c.Op, c.Payload, payload); err != nil {
		return "", nil, err
	}
	var reply struct {
		Status    string
		Floor     uint64
		ProofHash string `json:"proof_hash"`
	}
	if json.Unmarshal(payload, &reply) != nil {
		return "", nil, fsapi.Invalid
	}
	if reply.Status != "RETIRED" {
		return "", nil, nil
	}
	var request struct {
		Anchor, Nonce string
		Through       uint64
		Credentials   guestbridge.Credentials
	}
	if json.Unmarshal(c.Payload, &request) != nil || reply.Floor != request.Through {
		return "", nil, fsapi.Stale
	}
	evidence, err := canonicalJSON(map[string]any{"anchor": request.Anchor, "nonce": request.Nonce, "through": request.Through, "proof_hash": reply.ProofHash, "credentials": request.Credentials})
	return "VIEW_FLOOR", evidence, err
}

func unsignedReceipt(r guestbridge.VerificationReceipt) ([]byte, error) {
	raw, err := canonicalJSON(r)
	if err != nil {
		return nil, err
	}
	var value map[string]json.RawMessage
	if err = json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	delete(value, "signature")
	return canonicalJSON(value)
}
func isViewOperation(op string) bool {
	switch op {
	case "VIEW_ACQUIRE", "VIEW_RELEASE", "VIEW_RENEW", "VIEW_CANCEL", "VIEW_QUERY", "VIEW_RETIRE", "DIRTY_RESERVE", "DIRTY_FREEZE":
		return true
	}
	return false
}

// providerBinding accepts only actual providers or one actual immutable router.
func (p *Producer) providerBinding(b guestbridge.VerificationBinding, scope guestbridge.Context, provider *controlledvalidation.Provider) bool {
	if b.Claims == provider && b.Execution == provider {
		return true
	}
	claims, ok := b.Claims.(*ProviderRouter)
	execution, executionOK := b.Execution.(*ProviderRouter)
	if !ok || !executionOK || claims == nil || claims != execution {
		return false
	}
	actual, err := claims.route(scope)
	return err == nil && actual == provider
}
