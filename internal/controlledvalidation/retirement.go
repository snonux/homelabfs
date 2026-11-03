package controlledvalidation

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

var _ guestbridge.ControlledRetirement = (*Provider)(nil)

// RetireOriginal binds full original credentials to every covered protected
// ordinary or dirty number. Existing Retire remains the authoritative complete
// coverage/floor engine and persists the exact extended request as its retry key.
func (p *Provider) RetireOriginal(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	observed, ok := guestbridge.VerifiedRequest(ctx)
	protocol := guestbridge.ControlledProtocol()
	enrolled := p.config.Authorization.Enrollment
	if !ok || observed.Scope != scope || observed.Operation != "RETIRE" || observed.Protocol == nil || *observed.Protocol != protocol || enrolled.Protocol == nil || *enrolled.Protocol != protocol || observed.PayloadHash != hash(raw) || observed.Native.Endpoint != enrolled.Endpoint {
		return nil, fsapi.Permission
	}
	envelope, err := canonicalJSON(guestbridge.Command{Protocol: &protocol, Cell: observed.Cell, Context: scope, Op: "RETIRE", Payload: raw})
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, 2); err != nil {
		return nil, err
	}
	var request struct {
		Realm, Subject, Anchor, Nonce string
		Through                       uint64
		Hashes                        []string `json:"result_hashes"`
		Credentials                   guestbridge.Credentials
	}
	if json.Unmarshal(raw, &request) != nil || uint64(len(request.Hashes)) > request.Through {
		return nil, fsapi.Invalid
	}
	err = p.db.View(func(tx *bolt.Tx) error {
		claim := guestbridge.Claim{Realm: request.Realm, Subject: request.Subject, Anchor: request.Anchor, Nonce: request.Nonce}
		start := request.Through - uint64(len(request.Hashes)) + 1
		for i := range request.Hashes {
			claim.Number = start + uint64(i)
			e, exists, err := p.load(tx, claim)
			if err != nil {
				return err
			}
			if exists {
				binding := e.Binding
				if e.Original != nil {
					binding = *e.Original
				}
				if binding.Context != scope || !equalJSON(binding.Call.Credentials, request.Credentials) {
					return fsapi.Permission
				}
			} else if raw := p.bucket(tx, "earmarks").Get(claimKey(claim)); raw != nil {
				var earmark Earmark
				if err := json.Unmarshal(raw, &earmark); err != nil {
					return err
				}
				if earmark.Scope != scope || !equalJSON(earmark.Credentials, request.Credentials) {
					return fsapi.Permission
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p.Retire(ctx, scope, raw)
}
