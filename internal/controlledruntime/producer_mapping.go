//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// BindTransport completes staged ownership using the actual durable transport
// attached to the same producer, bridge controller, native and provider/router.
// A public detached binding or successful caller callback is insufficient.
func (p *Producer) BindTransport(ctx context.Context, t *guestbridge.Transport) error {
	b, err := t.VerificationBinding(p)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(b.Scopes) == 0 || b.Views != p.controller || b.ViewProtocol != p.controller {
		return fsapi.Permission
	}
	for _, scope := range b.Scopes {
		a := p.facts.admission(scope)
		provider := p.providers[scope.Session]
		if a == nil || provider == nil || b.Endpoint != a.endpoint || b.Native != a.supervisor || !p.providerBinding(b, scope, provider) || p.bindings[scope.Session] != b.Controller || p.transports[scope.Session] != nil {
			return fsapi.Permission
		}
		if err = a.check(scope); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	for _, scope := range b.Scopes {
		p.transports[scope.Session] = t
	}
	return nil
}

// ProduceMapped signs exact original/translated and final-native bindings only
// after the actual transport has protected every admitted native leg and base.
func (p *Producer) ProduceMapped(ctx context.Context, c guestbridge.Command, response json.RawMessage) (*guestbridge.VerificationReceiptV2, error) {
	mapping, ok := guestbridge.VerifiedMapping(ctx)
	p.mu.Lock()
	transport := p.transports[c.Context.Session]
	p.mu.Unlock()
	if !ok || mapping.NativeCell != c.Cell || mapping.NativeResponseHash != hashBytes(response) || transport == nil || mapping.Transport != transport || c.Protocol == nil || *c.Protocol != guestbridge.MappedProtocol() || c.Delegation == nil || mapping.GuestCell != c.Delegation.GuestCell || mapping.PollCount != c.Delegation.PollIndex+1 {
		return nil, fsapi.Permission
	}
	receipt, err := p.produce(ctx, c, response)
	if err != nil || receipt == nil {
		return nil, err
	}
	if receipt.Cell != mapping.NativeCell || receipt.CommandHash != mapping.NativeCommandHash || receipt.ResponseHash != mapping.NativeResponseHash {
		return nil, fsapi.Stale
	}
	receipt.Version = "homelabfs-controlled-verification/2"
	r := guestbridge.VerificationReceiptV2{VerificationReceipt: *receipt, GuestCell: mapping.GuestCell, GuestCommandHash: mapping.GuestCommandHash, GuestResponseHash: mapping.GuestResponseHash, PollCount: mapping.PollCount, MappingHash: mapping.MappingHash}
	raw, err := canonicalJSON(r)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "signature")
	unsigned, err := canonicalJSON(fields)
	if err != nil {
		return nil, err
	}
	r.Signature = hex.EncodeToString(ed25519.Sign(p.keys[c.Context.Session], unsigned))
	raw, err = canonicalJSON(r)
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.DecodeVerificationReceiptV2(raw); err != nil {
		return nil, err
	}
	return &r, nil
}
