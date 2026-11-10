package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

type originalRetirement struct {
	Realm, Subject, Anchor, Nonce string
	Through                       uint64
	Hashes                        []string `json:"result_hashes"`
	Credentials                   guestbridge.Credentials
}

// VerifyRetirementReceipt reads the exact already protected full-original floor
// receipt. It cannot advance a floor or turn a component reply into evidence.
// RetireOriginal previously checked every intermediate number and its complete
// original credentials in the same exclusive provider lifetime.
func (p *Provider) VerifyRetirementReceipt(ctx context.Context, scope guestbridge.Context, request, response json.RawMessage) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	var original originalRetirement
	if json.Unmarshal(request, &original) != nil || original.Credentials.Process == "" || uint64(len(original.Hashes)) > original.Through {
		return fsapi.Invalid
	}
	observed, ok := guestbridge.VerifiedRequest(ctx)
	if !ok || observed.Scope != scope || observed.Operation != "RETIRE" || observed.PayloadHash != hash(request) {
		return fsapi.Permission
	}
	return p.db.View(func(tx *bolt.Tx) error {
		// The receipt is read first: only the exact protected receipt lets
		// checkRetirementOwners accept a covered number that recycling
		// already removed (o64).
		protected := p.bucket(tx, "retirements").Get([]byte(hash(request)))
		if protected == nil {
			return fsapi.Stale
		}
		if err := p.checkRetirementOwners(tx, scope, original, true); err != nil {
			return err
		}
		a, err := canonicalJSON(json.RawMessage(protected))
		if err != nil {
			return err
		}
		b, err := canonicalJSON(response)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			return fsapi.Stale
		}
		return nil
	})
}

var _ guestbridge.ControlledRetirement = (*Provider)(nil)

// RetireOriginal binds full original credentials to every covered protected
// ordinary or dirty number. Existing Retire remains the authoritative complete
// coverage/floor engine and persists the exact extended request as its retry key.
func (p *Provider) RetireOriginal(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	if err := p.Check(ctx, scope); err != nil {
		return nil, err
	}
	observed, ok := guestbridge.VerifiedRequest(ctx)
	enrolled := p.config.Authorization.Enrollment
	if enrolled.Protocol == nil || !guestbridge.IsControlledProtocol(*enrolled.Protocol) {
		return nil, fsapi.Permission
	}
	protocol := *enrolled.Protocol
	if !ok || observed.Scope != scope || observed.Operation != "RETIRE" || observed.Protocol == nil || *observed.Protocol != protocol || enrolled.Protocol == nil || *enrolled.Protocol != protocol || observed.PayloadHash != hash(raw) || observed.Native.Endpoint != enrolled.Endpoint {
		return nil, fsapi.Permission
	}
	envelope, err := canonicalJSON(guestbridge.Command{Protocol: &protocol, Cell: observed.Cell, Context: scope, Op: "RETIRE", Payload: raw})
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.DecodeCommandVersion(envelope, uint16(protocol.BridgeMinor)); err != nil {
		return nil, err
	}
	var request originalRetirement
	if json.Unmarshal(raw, &request) != nil || uint64(len(request.Hashes)) > request.Through {
		return nil, fsapi.Invalid
	}
	err = p.db.View(func(tx *bolt.Tx) error { return p.checkRetirementOwners(tx, scope, request, false) })
	if err != nil {
		return nil, err
	}
	return p.Retire(ctx, scope, raw)
}

// checkRetirementOwners binds each covered number to the request's original
// credentials and, with coverage, to its result or gap hash. With coverage
// (VerifyRetirementReceipt, which already holds the exact protected receipt)
// an absent number at or below the floor is accepted: Retire checked its
// hash before recycling removed it (recycle.go). Without coverage
// (RetireOriginal) absent numbers pass, as before.
func (p *Provider) checkRetirementOwners(tx *bolt.Tx, scope guestbridge.Context, request originalRetirement, coverage bool) error {
	claim := guestbridge.Claim{Realm: request.Realm, Subject: request.Subject, Anchor: request.Anchor, Nonce: request.Nonce}
	family, err := p.principal(tx, claim)
	if err != nil {
		return err
	}
	start := request.Through - uint64(len(request.Hashes)) + 1
	for i, want := range request.Hashes {
		claim.Number = start + uint64(i)
		e, exists, err := p.load(tx, claim)
		if err != nil {
			return err
		}
		if exists {
			err = checkEntryOwner(scope, request, e, coverage, want)
		} else if raw := p.bucket(tx, "earmarks").Get(claimKey(claim)); raw != nil {
			err = checkEarmarkOwner(scope, request, raw, coverage, want)
		} else if coverage && claim.Number > family.Floor {
			err = fsapi.Stale
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkEntryOwner checks one retained entry: its original binding's scope and
// credentials and, with coverage, its terminal result id or gap hash.
func checkEntryOwner(scope guestbridge.Context, request originalRetirement, e entry, coverage bool, want string) error {
	binding := e.Binding
	if e.Original != nil {
		binding = *e.Original
	}
	if binding.Context != scope || !equalJSON(binding.Call.Credentials, request.Credentials) {
		return fsapi.Permission
	}
	if !coverage {
		return nil
	}
	var expected string
	switch {
	case e.Status == "TERMINAL":
		var result struct{ ID string }
		if err := json.Unmarshal(e.Result, &result); err != nil {
			return err
		}
		expected = result.ID
	case e.Gap && e.Status == "DECLINED":
		encoded, err := json.Marshal(e)
		if err != nil {
			return err
		}
		expected = hash(encoded)
	default:
		return fsapi.Stale
	}
	if expected != want {
		return fsapi.Stale
	}
	return nil
}

// checkEarmarkOwner checks one earmark-only number: with coverage it must be
// a closed unbound gap with the named proof; its scope and credentials must
// be the request's.
func checkEarmarkOwner(scope guestbridge.Context, request originalRetirement, raw []byte, coverage bool, want string) error {
	var earmark Earmark
	if err := json.Unmarshal(raw, &earmark); err != nil {
		return err
	}
	if coverage && (!earmark.Closed || earmark.Bound != nil || earmark.GapProof != want) {
		return fsapi.Stale
	}
	if earmark.Scope != scope || !equalJSON(earmark.Credentials, request.Credentials) {
		return fsapi.Permission
	}
	return nil
}
