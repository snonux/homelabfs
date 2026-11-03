package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// Admission separates recall acquisition from the serialized execution worker.
// Admit must return promptly when physical/source closure is not established.
type Admission interface {
	Admit(context.Context, guestbridge.Binding) (Admitted, error)
}

// Admitted pins a serialized local cut until Close. Check runs on the real
// service worker immediately before planning/effects, including recovery.
type Admitted interface{ fsservice.ControlledPermit }

// Earmark is a protected unbound future number, never a placeholder Claim.
// Credit and original page provenance are retained before a writable PTE exists.
type Earmark struct {
	GapProof       string
	Scope          guestbridge.Context
	Number         guestbridge.ClaimNumber
	Credentials    guestbridge.Credentials
	Description    guestbridge.Capability
	Reservation    string
	Offset, Length uint64
	Closed         bool
	Bound          *guestbridge.Call
}

// EarmarkNumber permanently allocates the next ordinary number and preowns its
// final claim/result/source arenas. Exact retries are stable; changed provenance
// cannot adopt the number. The caller must independently authenticate the page.
func (p *Provider) EarmarkNumber(ctx context.Context, e Earmark) error {
	if err := p.Check(ctx, e.Scope); err != nil {
		return err
	}
	n := e.Number
	for _, v := range []string{n.Realm, n.Anchor, n.Nonce} {
		if _, err := fsapi.ParseUUID(v); err != nil {
			return fsapi.Invalid
		}
	}
	subject, err := hex.DecodeString(n.Subject)
	if err != nil || len(subject) == 0 || len(subject) > 1024 || n.Number == 0 || e.Reservation == "" || e.Description.Kind != "DESCRIPTION" || e.Description.Scope != e.Scope || e.Description.Rights&2 == 0 || e.Length != 4096 || e.Offset%4096 != 0 || e.Offset > 1<<63-1-4096 || e.Closed || e.Bound != nil || e.GapProof != "" {
		return fsapi.Invalid
	}
	c := numberClaim(n)
	return p.db.Update(func(tx *bolt.Tx) error {
		bucket := p.bucket(tx, "earmarks")
		if raw := bucket.Get(claimKey(c)); raw != nil {
			var old Earmark
			if err := json.Unmarshal(raw, &old); err != nil {
				return err
			}
			if old.Closed {
				return fsapi.Stale
			}
			if old.Bound != nil {
				return fsapi.WouldBlock
			}
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(e)
			if !bytes.Equal(a, b) {
				return fsapi.Stale
			}
			return nil
		}
		if p.bucket(tx, "claims").Get(claimKey(c)) != nil || p.identityCount(tx) >= p.config.MaxClaims {
			return fsapi.NoSpace
		}
		ps, err := p.principal(tx, c)
		if err != nil {
			return err
		}
		if ps.High == ^uint64(0) || n.Number != ps.High+1 {
			return fsapi.Stale
		}
		if err = p.reserveCredit(tx, "retained-credit", p.config.ClaimCreditBytes, p.config.MaxRetainedBytes); err != nil {
			return err
		}
		if err = p.reserveCredit(tx, "source-credit", p.config.SourceCreditBytes, p.config.MaxSourceBytes); err != nil {
			return err
		}
		ps.High = n.Number
		if err = p.putPrincipal(tx, c, ps); err != nil {
			return err
		}
		return put(bucket, claimKey(c), e)
	})
}

// BindFrozen binds the original earmark exactly once after immutable bytes are
// known. It neither advances the counter nor changes a previously bound Claim.
// Call must be the canonical DRAIN_WRITE derived from that reserved page.
func (p *Provider) BindFrozen(ctx context.Context, scope guestbridge.Context, reservation string, call guestbridge.Call) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	raw, err := canonicalJSON(call.Intent)
	if err != nil || hash(raw) != call.Claim.OriginalHash || !validClaim(call.Claim) || call.Intent.Opcode != "DRAIN_WRITE" {
		return fsapi.Invalid
	}
	if verifier, ok := p.currentAdmission().(interface {
		CheckFrozenBinding(context.Context, guestbridge.Context, string, guestbridge.Call) error
	}); ok {
		if err := verifier.CheckFrozenBinding(ctx, scope, reservation, call); err != nil {
			return err
		}
	} else if p.config.Service.ControlledViewsRequired() {
		return fsapi.Permission
	}
	return p.db.Update(func(tx *bolt.Tx) error {
		bucket := p.bucket(tx, "earmarks")
		var e Earmark
		raw := bucket.Get(claimKey(call.Claim))
		if raw == nil {
			return fsapi.Permission
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.Closed || e.Scope != scope || e.Reservation != reservation {
			return fsapi.Stale
		}
		if e.Bound != nil {
			if !equalJSON(e.Bound, call) {
				return fsapi.Stale
			}
			return nil
		}
		var a struct {
			Description guestbridge.Capability
			Reservation string
			Offset      uint64
			Data        string
		}
		if err := json.Unmarshal(call.Intent.Args, &a); err != nil {
			return err
		}
		data, decodeErr := hex.DecodeString(a.Data)
		if decodeErr != nil || len(data) == 0 || uint64(len(data)) > e.Length || a.Offset != e.Offset {
			return fsapi.Stale
		}
		if a.Reservation != reservation || !equalJSON(a.Description, e.Description) || !equalJSON(call.Credentials, e.Credentials) {
			return fsapi.Stale
		}
		if p.bucket(tx, "claims").Get(claimKey(call.Claim)) != nil {
			return fsapi.Stale
		}
		e.Bound = &call
		if err := put(bucket, claimKey(call.Claim), e); err != nil {
			return err
		}
		return put(p.bucket(tx, "claims"), claimKey(call.Claim), entry{Version: ledgerVersion, Claim: call.Claim, Status: "ALLOCATED", RetainedCredit: p.config.ClaimCreditBytes, SourceCredit: p.config.SourceCreditBytes})
	})
}

// CheckFrozenResult verifies exact protected terminal call bytes and local null
// Publication. Absence, execution intent or a hash alone never closes a drain.
func (p *Provider) CheckFrozenResult(ctx context.Context, scope guestbridge.Context, call guestbridge.Call) error {
	if err := p.Check(ctx, scope); err != nil {
		return err
	}
	return p.db.View(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, call.Claim)
		if err != nil {
			return err
		}
		if !ok || e.Status != "TERMINAL" || !equalJSON(e.Binding.Call, call) || e.Binding.Context != scope || !controlledResult(e.Result) {
			return fsapi.Unavailable
		}
		return nil
	})
}

// BindAdmission installs the mandatory controller once for this explicit profile.
func (p *Provider) BindAdmission(admission Admission) error {
	p.admissionMu.Lock()
	defer p.admissionMu.Unlock()
	if nilValue(admission) || p.admission != nil && !closedAdmission(p.admission) || !p.config.Service.ControlledViewsRequired() {
		return fsapi.Permission
	}
	p.admission = admission
	return nil
}

// Enrollment returns a detached copy of the authenticated immutable enrollment.
func (p *Provider) Enrollment() Enrollment {
	raw, _ := json.Marshal(p.config.Authorization.Enrollment)
	var e Enrollment
	_ = json.Unmarshal(raw, &e)
	return e
}

// CloseEarmark protects an exact unbound no-effect gap only after the installed
// controller has durably recorded verified physical/source exclusion. It returns
// a stable historical proof; it never promises reusable live resource credit.
func (p *Provider) CloseEarmark(ctx context.Context, original Earmark, proof string) (string, error) {
	if err := p.Check(ctx, original.Scope); err != nil {
		return "", err
	}
	if raw, err := hex.DecodeString(proof); err != nil || len(raw) != 32 {
		return "", fsapi.Invalid
	}
	verifier, ok := p.currentAdmission().(interface {
		CheckEarmarkClosure(context.Context, Earmark, string) error
	})
	if !ok {
		return "", fsapi.Permission
	}
	if err := verifier.CheckEarmarkClosure(ctx, original, proof); err != nil {
		return "", err
	}
	err := p.db.Update(func(tx *bolt.Tx) error {
		bucket := p.bucket(tx, "earmarks")
		claim := numberClaim(original.Number)
		key := claimKey(claim)
		raw := bucket.Get(key)
		e := original
		if raw == nil {
			if p.bucket(tx, "claims").Get(key) != nil || p.identityCount(tx) >= p.config.MaxClaims {
				return fsapi.NoSpace
			}
			principal, err := p.principal(tx, claim)
			if err != nil {
				return err
			}
			if principal.High == ^uint64(0) || claim.Number != principal.High+1 {
				return fsapi.Stale
			}
			principal.High = claim.Number
			if err = p.putPrincipal(tx, claim, principal); err != nil {
				return err
			}
		} else {
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			before := e
			before.Closed = false
			before.GapProof = ""
			if !equalJSON(before, original) || e.Bound != nil {
				return fsapi.Stale
			}
			if e.Closed && e.GapProof != proof {
				return fsapi.Stale
			}
		}
		e.Closed = true
		e.GapProof = proof
		return put(bucket, key, e)
	})
	return proof, err
}

// UsesService binds controller admission to this exact real service instance.
func (p *Provider) UsesService(service *fsservice.Service) bool { return p.config.Service == service }

// CheckViewReady excludes new observations/mutations while an original effected
// operation still lacks its protected result/session installation. Recovery of
// that exact original binding remains available through IsRecoveryBinding.
func (p *Provider) CheckViewReady(ctx context.Context) error {
	if err := p.Check(ctx, p.config.Authorization.Enrollment.Scope); err != nil {
		return err
	}
	return p.db.View(func(tx *bolt.Tx) error {
		return p.bucket(tx, "claims").ForEach(func(_, raw []byte) error {
			var e entry
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			if e.Status == "PENDING" && e.Plan != nil && e.Outcome == nil {
				return fsapi.WouldBlock
			}
			return nil
		})
	})
}

// IsRecoveryBinding verifies the exact original protected plan before its
// recovery may bypass exclusion intended for fresh operation planning.
func (p *Provider) IsRecoveryBinding(ctx context.Context, b guestbridge.Binding) (bool, error) {
	if err := p.Check(ctx, b.Context); err != nil {
		return false, err
	}
	var recovery bool
	err := p.db.View(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, b.Call.Claim)
		if err != nil {
			return err
		}
		recovery = ok && sameExact(e.Binding, b) && e.Status == "PENDING" && e.Plan != nil
		return nil
	})
	return recovery, err
}

// CheckViewProtocol binds real required-view execution to the same controller
// that issues view grants and owns recall/drain state. A substituted provider or
// default legacy protocol cannot manufacture untracked cache/PTE authority.
func (p *Provider) CheckViewProtocol(views guestbridge.Views, protocol guestbridge.ViewProtocolProvider) error {
	if !p.config.Service.ControlledViewsRequired() {
		return nil
	}
	admission := p.currentAdmission()
	expectedViews, ok := admission.(guestbridge.Views)
	if !ok {
		return fsapi.Permission
	}
	expectedProtocol, ok := admission.(guestbridge.ViewProtocolProvider)
	if !ok {
		return fsapi.Permission
	}
	if !sameInstance(expectedViews, views) || !sameInstance(expectedProtocol, protocol) {
		return fsapi.Permission
	}
	return nil
}

func numberClaim(n guestbridge.ClaimNumber) guestbridge.Claim {
	return guestbridge.Claim{Realm: n.Realm, Subject: n.Subject, Anchor: n.Anchor, Nonce: n.Nonce, Number: n.Number}
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func (p *Provider) currentAdmission() Admission {
	p.admissionMu.RLock()
	defer p.admissionMu.RUnlock()
	return p.admission
}

func closedAdmission(value any) bool {
	old, ok := value.(interface{ IsClosed() bool })
	return ok && old.IsClosed()
}

func (p *Provider) identityCount(tx *bolt.Tx) int {
	count := p.bucket(tx, "claims").Stats().KeyN
	_ = p.bucket(tx, "earmarks").ForEach(func(key, value []byte) error {
		if p.bucket(tx, "claims").Get(key) == nil {
			count++
		}
		return nil
	})
	return count
}

func sameInstance(a, b any) bool {
	if nilValue(a) || nilValue(b) {
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Kind() == reflect.Pointer && x.Pointer() == y.Pointer()
}
