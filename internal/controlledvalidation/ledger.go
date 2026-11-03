package controlledvalidation

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	bolt "go.etcd.io/bbolt"
)

func (p *Provider) bucket(tx *bolt.Tx, name string) *bolt.Bucket {
	return tx.Bucket([]byte(ledgerVersion)).Bucket([]byte(name))
}

func (p *Provider) principal(tx *bolt.Tx, c guestbridge.Claim) (principal, error) {
	var ps principal
	raw := p.bucket(tx, "principals").Get(principalKey(c))
	if raw == nil {
		return ps, nil
	}
	err := json.Unmarshal(raw, &ps)
	return ps, err
}

func (p *Provider) putPrincipal(tx *bolt.Tx, c guestbridge.Claim, ps principal) error {
	return put(p.bucket(tx, "principals"), principalKey(c), ps)
}

func (p *Provider) load(tx *bolt.Tx, c guestbridge.Claim) (entry, bool, error) {
	var e entry
	raw := p.bucket(tx, "claims").Get(claimKey(c))
	if raw == nil {
		return e, false, nil
	}
	err := json.Unmarshal(raw, &e)
	if err == nil && e.Plan != nil && len(e.Plan.MutationEncoding) > 0 {
		var decoded record.Record
		decoded, err = record.Decode(e.Plan.MutationEncoding)
		if err == nil {
			mutation, ok := decoded.Payload.(record.Mutation)
			if !ok {
				err = fsapi.Stale
			} else {
				e.Plan.Mutation = &mutation
			}
		}
	}
	if err == nil && e.Version != ledgerVersion {
		err = fsapi.Stale
	}
	if err == nil && e.PlannedOutcome != nil && e.Plan != nil && e.Plan.TooLarge {
		e.PlannedOutcome.DelegatedError = fsservice.ErrDelegatedFileTooLarge
	}
	if err == nil && e.Outcome != nil && e.Plan != nil && e.Plan.TooLarge {
		e.Outcome.DelegatedError = fsservice.ErrDelegatedFileTooLarge
	}
	return e, true, err
}

func (p *Provider) update(b guestbridge.Binding, change func(*entry) error) error {
	return p.db.Update(func(tx *bolt.Tx) error {
		e, ok, err := p.load(tx, b.Call.Claim)
		if err != nil {
			return err
		}
		if !ok || !sameExact(e.Binding, b) {
			return fsapi.Stale
		}
		if err = change(&e); err != nil {
			return err
		}
		return put(p.bucket(tx, "claims"), claimKey(e.Claim), e)
	})
}

func (p *Provider) fail(name string) error {
	if p.config.Failpoint != nil {
		return p.config.Failpoint(name)
	}
	return nil
}

func put(b *bolt.Bucket, k []byte, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return b.Put(k, raw)
}

func state(e entry) guestbridge.ClaimState {
	status := e.Status
	if status == "DECLINED" {
		status = "INDETERMINATE"
	}
	return guestbridge.ClaimState{Status: status, Binding: e.Binding, Result: bytes.Clone(e.Result)}
}

func principalKey(c guestbridge.Claim) []byte {
	raw, _ := json.Marshal([]string{c.Realm, c.Subject, c.Anchor, c.Nonce})
	return raw
}

func claimKey(c guestbridge.Claim) []byte {
	key := principalKey(c)
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], c.Number)
	return append(key, n[:]...)
}

func sameBinding(a, b guestbridge.Binding) bool {
	return a.Context == b.Context && bytes.Equal(a.Canonical, b.Canonical)
}

func sameExact(a, b guestbridge.Binding) bool {
	if !sameBinding(a, b) || a.Operation != b.Operation || a.Allocation != b.Allocation {
		return false
	}
	raw, err := canonicalCall(b.Call)
	return err == nil && bytes.Equal(raw, a.Canonical)
}

func hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func canonicalCall(call guestbridge.Call) ([]byte, error) { return canonicalJSON(call) }
func canonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var v any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&v); err != nil {
		return nil, err
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	err = encoder.Encode(v)
	raw = bytes.TrimSuffix(canonical.Bytes(), []byte{'\n'})
	if err != nil {
		return nil, err
	}
	if _, err = guestbridge.Canonical(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func validClaim(c guestbridge.Claim) bool {
	_, a := fsapi.ParseUUID(c.Realm)
	_, b := fsapi.ParseUUID(c.Anchor)
	_, d := fsapi.ParseUUID(c.Nonce)
	subject, e := hex.DecodeString(c.Subject)
	digest, f := hex.DecodeString(c.OriginalHash)
	return a == nil && b == nil && d == nil && e == nil && f == nil && len(subject) > 0 && len(subject) <= 1024 && len(digest) == 32 && c.Number > 0
}

func newUUID() (fsapi.UUID, error) {
	var id fsapi.UUID
	if _, err := rand.Read(id[:]); err != nil {
		return id, err
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	return id, nil
}

func nilValue(v any) bool {
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

func controlledResult(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return false
	}
	var result struct{ Publication json.RawMessage }
	return json.Unmarshal(raw, &result) == nil && string(result.Publication) == "null"
}

func (p *Provider) protectSnapshot(tx *bolt.Tx, e entry) error {
	bucket := p.bucket(tx, "bridge")
	key := []byte(e.Binding.Context.Session)
	versionKey := []byte(e.Binding.Context.Session + "/version")
	if raw := bucket.Get(versionKey); raw != nil {
		if len(raw) != 8 {
			return fsapi.Stale
		}
		if binary.BigEndian.Uint64(raw) > e.SnapshotVersion {
			return nil
		}
	}
	if err := bucket.Put(key, e.BridgeState); err != nil {
		return err
	}
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], e.SnapshotVersion)
	return bucket.Put(versionKey, version[:])
}

func (p *Provider) reserveCredit(tx *bolt.Tx, name string, amount, maximum uint64) error {
	root := tx.Bucket([]byte(ledgerVersion))
	var used uint64
	if raw := root.Get([]byte(name)); raw != nil {
		if len(raw) != 8 {
			return fsapi.Stale
		}
		used = binary.BigEndian.Uint64(raw)
	}
	if used > maximum || amount > maximum-used {
		return fsapi.NoSpace
	}
	var next [8]byte
	binary.BigEndian.PutUint64(next[:], used+amount)
	return root.Put([]byte(name), next[:])
}

func validateEnrollment(e Enrollment) error {
	scope := e.Scope
	for _, value := range []string{scope.Cluster, scope.Configuration, scope.Export, scope.ExporterIncarnation, scope.FuseSession, scope.Generation, scope.GuestBoot, scope.GuestMount, scope.OriginIncarnation, scope.OriginJournal, scope.OriginNode, scope.Session, scope.VM, scope.Volume} {
		if _, err := fsapi.ParseUUID(value); err != nil {
			return fsapi.Invalid
		}
	}
	for _, value := range []string{scope.AuthorityHash, scope.PolicyHash, scope.RegistryHash} {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != value {
			return fsapi.Invalid
		}
	}
	endpoint := e.Endpoint
	if !e.ServingGeneration.Valid() || endpoint.FuseSession != scope.FuseSession || endpoint.CommandNode <= 1 || endpoint.ResultNode <= 1 || endpoint.RecallNode <= 1 || endpoint.CommandNode == endpoint.ResultNode || endpoint.CommandNode == endpoint.RecallNode || endpoint.ResultNode == endpoint.RecallNode {
		return fsapi.Invalid
	}
	return nil
}

func (p *Provider) reserveUUID(tx *bolt.Tx, namespace string) (fsapi.UUID, error) {
	bucket := p.bucket(tx, namespace)
	for range 128 {
		id, err := newUUID()
		if err != nil {
			return fsapi.UUID{}, err
		}
		if bucket.Get(id[:]) != nil {
			continue
		}
		if err = bucket.Put(id[:], []byte{1}); err != nil {
			return fsapi.UUID{}, err
		}
		return id, nil
	}
	return fsapi.UUID{}, fsapi.NoSpace
}

// checkPendingStage is durable session admission exclusion, independent of
// Check so original claim queries and recovery remain available.
func (p *Provider) checkPendingStage(tx *bolt.Tx, scope guestbridge.Context, except fsapi.UUID) error {
	return p.bucket(tx, "claims").ForEach(func(_, raw []byte) error {
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.Binding.Context == scope && e.Binding.Operation != except && e.Status == "PENDING" && e.Plan != nil && len(e.BridgeState) > 0 {
			return fsapi.WouldBlock
		}
		return nil
	})
}
