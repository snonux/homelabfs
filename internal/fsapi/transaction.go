package fsapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

// TransactionVersion is the initial single-namespace-operation API version.
const TransactionVersion uint16 = 1

const (
	// MaxNameBytes is the initial portable path component byte limit.
	MaxNameBytes = 255
	// MaxPathComponents bounds one original endpoint path.
	MaxPathComponents = 256
	// MaxPathBytes bounds the sum of raw component bytes in one endpoint.
	MaxPathBytes = 16384
)

// Principal is a stable authentication identity, distinct from mutable Unix
// execution credentials. Subject preserves 1..1024 exact bytes without Unicode
// normalization; Realm scopes the authentication authority that assigned it.
type Principal struct {
	Realm   UUID
	Subject string
}

// SessionID is the immutable deduplication anchor, never inferred from paths,
// current ownership or the lowest UUID in a dependency closure.
type SessionID struct {
	Principal Principal
	Anchor    UUID
	Nonce     UUID
}

// TransactionID identifies one persisted request number in an anchored session.
// Clients allocate strictly increasing nonzero numbers and persist before send.
type TransactionID struct {
	Session SessionID
	Number  uint64
}

// Endpoint names a relative component path under an authorized volume view.
// A view ID is an immutable UUID scoped by Volume. Only the service
// can verify that the view is authorized and matches that volume. Intermediate
// symlinks are rejected initially; a final symlink is renamed without following.
type Endpoint struct {
	Volume     UUID
	View       UUID
	Components []string
}

// NamespaceOperation is exactly one atomic Rename or Link.
type NamespaceOperation uint8

const (
	// Rename moves a namespace reference, preserving its immutable object home.
	Rename NamespaceOperation = iota + 1
	// Link adds a regular-file reference to the same home-owned object.
	Link
)

// ReplacementPolicy specifies rename replacement; Link requires NoReplace.
type ReplacementPolicy uint8

const (
	// Replace permits a compatible destination replacement.
	Replace ReplacementPolicy = iota + 1
	// NoReplace requires an absent destination.
	NoReplace
)

// ExpectedRevision is an optional original compare-and-set expectation.
// A supplied name revision may be zero to require a virgin absent name; supplied
// parent revisions must be nonzero. An unsupplied Value must be zero.
type ExpectedRevision struct {
	Set   bool
	Value uint64
}

// ExpectedObject is an optional original source-object expectation.
type ExpectedObject struct {
	Set   bool
	Value ObjectID
}

// TransactionSpec contains only immutable original request fields. Wait contexts,
// deadlines, credentials, routing hints, resolved objects/revisions and selected
// coordinators are deliberately absent from its canonical identity.
type TransactionSpec struct {
	Version                            uint16
	ID                                 TransactionID
	Operation                          NamespaceOperation
	Source, Destination                Endpoint
	Replacement                        ReplacementPolicy
	ExpectedSource                     ExpectedObject
	SourceParent, SourceName           ExpectedRevision
	DestinationParent, DestinationName ExpectedRevision
}

// Digest is SHA-256 of exact canonical original request bytes. Equality of a
// digest alone is insufficient for deduplication: compare the original bytes too.
type Digest [32]byte

// OriginalRequest is an immutable owned snapshot constructed by
// NewOriginalRequest. Its private encoding defines only this API's versioned
// canonical original identity, shared with the journal OriginalRequest schema;
// it is not a complete network or journal frame.
type OriginalRequest struct {
	spec      TransactionSpec
	canonical []byte
	digest    Digest
}

// ExecuteRequest pairs immutable original identity with an owned credential
// snapshot. Credentials/policy versions captured at admission are separate
// execution evidence and never change the original request's digest.
type ExecuteRequest struct {
	original    OriginalRequest
	credentials Cred
}

// NewOriginalRequest validates and snapshots the original request before any
// asynchronous dispatch, pathname resolution or request claim.
func NewOriginalRequest(spec TransactionSpec) (OriginalRequest, error) {
	if err := spec.Validate(); err != nil {
		return OriginalRequest{}, err
	}
	spec = spec.clone()
	canonical := canonicalRequest(spec)
	return OriginalRequest{spec: spec, canonical: canonical, digest: sha256.Sum256(canonical)}, nil
}

// NewExecuteRequest snapshots execution credentials beside the original request.
func NewExecuteRequest(original OriginalRequest, credentials Cred) (ExecuteRequest, error) {
	if !original.Valid() {
		return ExecuteRequest{}, fmt.Errorf("%w: uninitialized original request", Invalid)
	}
	return ExecuteRequest{original: original, credentials: credentials.Clone()}, nil
}

// Valid reports whether the stable authentication identity is well formed.
func (p Principal) Valid() bool {
	return p.Realm.Valid() && len(p.Subject) >= 1 && len(p.Subject) <= 1024
}

// Valid reports whether the complete immutable anchor is well formed.
func (s SessionID) Valid() bool { return s.Principal.Valid() && s.Anchor.Valid() && s.Nonce.Valid() }

// Valid reports whether the anchored request number is well formed.
func (id TransactionID) Valid() bool { return id.Session.Valid() && id.Number != 0 }

// Validate rejects path traversal, empty/root paths and malformed view IDs.
// Path components preserve bytes, case and Unicode spelling; backslash is an
// ordinary Unix filename byte, not a path separator.
func (e Endpoint) Validate() error {
	if !e.Volume.Valid() || !e.View.Valid() || len(e.Components) == 0 || len(e.Components) > MaxPathComponents {
		return fmt.Errorf("%w: namespace endpoint", Invalid)
	}
	total := 0
	for _, component := range e.Components {
		if component == "" || component == "." || component == ".." || len(component) > MaxNameBytes || strings.ContainsAny(component, "/\x00") {
			return fmt.Errorf("%w: path component", Invalid)
		}
		total += len(component)
	}
	if total > MaxPathBytes {
		return fmt.Errorf("%w: path byte limit", Invalid)
	}
	return nil
}

// Validate checks original syntax only. Authorization, current-state checks,
// placement, cycles, capacity and claims belong to the anchored service.
func (s TransactionSpec) Validate() error {
	if s.Version != TransactionVersion || !s.ID.Valid() || s.Operation < Rename || s.Operation > Link || s.Replacement < Replace || s.Replacement > NoReplace || s.Operation == Link && s.Replacement != NoReplace {
		return fmt.Errorf("%w: transaction version, identity or operation", Invalid)
	}
	if err := s.Source.Validate(); err != nil {
		return err
	}
	if err := s.Destination.Validate(); err != nil {
		return err
	}
	if s.ExpectedSource.Set != s.ExpectedSource.Value.Valid() || !s.ExpectedSource.Set && s.ExpectedSource.Value != (ObjectID{}) {
		return fmt.Errorf("%w: expected source", Invalid)
	}
	for _, revision := range []ExpectedRevision{s.SourceParent, s.DestinationParent} {
		if revision.Set && revision.Value == 0 {
			return fmt.Errorf("%w: expected parent revision", Invalid)
		}
	}
	for _, revision := range []ExpectedRevision{s.SourceParent, s.SourceName, s.DestinationParent, s.DestinationName} {
		if !revision.Set && revision.Value != 0 {
			return fmt.Errorf("%w: expected revision", Invalid)
		}
	}
	return nil
}

// Valid reports whether r was built by NewOriginalRequest.
func (r OriginalRequest) Valid() bool { return len(r.canonical) != 0 }

// Spec returns an independent copy of original fields, never resolved paths.
func (r OriginalRequest) Spec() TransactionSpec { return r.spec.clone() }

// CanonicalBytes returns an independent copy of the complete original identity.
func (r OriginalRequest) CanonicalBytes() []byte { return slices.Clone(r.canonical) }

// Digest returns the hash of the complete original identity.
func (r OriginalRequest) Digest() Digest { return r.digest }

// Equal compares complete original bytes in addition to their digest.
func (r OriginalRequest) Equal(other OriginalRequest) bool {
	return r.Valid() && other.Valid() && r.digest == other.digest && bytes.Equal(r.canonical, other.canonical)
}

// Original returns immutable original identity, safe to retain asynchronously.
func (r ExecuteRequest) Original() OriginalRequest { return r.original }

// Credentials returns an independent execution-credential snapshot.
func (r ExecuteRequest) Credentials() Cred { return r.credentials.Clone() }

func (s TransactionSpec) clone() TransactionSpec {
	s.Source.Components = slices.Clone(s.Source.Components)
	s.Destination.Components = slices.Clone(s.Destination.Components)
	return s
}

// canonicalRequest has fixed field order, big-endian integers, uint32 byte
// lengths/counts, and 0/1 expectation presence. Strings are exact raw bytes.
func canonicalRequest(s TransactionSpec) []byte {
	b := binary.BigEndian.AppendUint16(nil, s.Version)
	b = append(b, s.ID.Session.Principal.Realm[:]...)
	b = appendString(b, s.ID.Session.Principal.Subject)
	b = append(b, s.ID.Session.Anchor[:]...)
	b = append(b, s.ID.Session.Nonce[:]...)
	b = binary.BigEndian.AppendUint64(b, s.ID.Number)
	b = append(b, byte(s.Operation))
	b = appendEndpoint(b, s.Source)
	b = appendEndpoint(b, s.Destination)
	b = append(b, byte(s.Replacement))
	if s.ExpectedSource.Set {
		b = append(b, 1)
		b = append(b, s.ExpectedSource.Value.Home[:]...)
		b = append(b, s.ExpectedSource.Value.Allocation[:]...)
	} else {
		b = append(b, 0)
	}
	for _, revision := range []ExpectedRevision{s.SourceParent, s.SourceName, s.DestinationParent, s.DestinationName} {
		if revision.Set {
			b = append(b, 1)
			b = binary.BigEndian.AppendUint64(b, revision.Value)
		} else {
			b = append(b, 0)
		}
	}
	return b
}

func appendString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

func appendEndpoint(b []byte, endpoint Endpoint) []byte {
	b = append(b, endpoint.Volume[:]...)
	b = append(b, endpoint.View[:]...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(endpoint.Components)))
	for _, component := range endpoint.Components {
		b = appendString(b, component)
	}
	return b
}
