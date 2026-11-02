// Package guestbridge implements the retained-9p host component. It supplies no
// production identity, public-claim, normal publication or kernel-barrier provider.
package guestbridge

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/logicalview"
	"github.com/snonux/homelabfs/internal/storage"
)

const (
	HeaderBytes = 88
	ChunkBytes  = 32768
	BodyBytes   = 1048576
	CellStride  = 2097152
	VirtualSize = uint64(1) << 52
	MaxCell     = uint64(2147483647)
	ControlName = ".homelabfs-control-v1"
)

// Context is the complete authenticated, installed enrollment scope.
type Context struct {
	AuthorityHash       string `json:"authority_hash"`
	Cluster             string `json:"cluster"`
	Configuration       string `json:"configuration"`
	Export              string `json:"export"`
	ExporterIncarnation string `json:"exporter_incarnation"`
	FuseSession         string `json:"fuse_session"`
	Generation          string `json:"generation"`
	GuestBoot           string `json:"guest_boot"`
	GuestMount          string `json:"guest_mount"`
	OriginIncarnation   string `json:"origin_incarnation"`
	OriginJournal       string `json:"origin_journal"`
	OriginNode          string `json:"origin_node"`
	PolicyHash          string `json:"policy_hash"`
	RegistryHash        string `json:"registry_hash"`
	Session             string `json:"session"`
	VM                  string `json:"vm"`
	Volume              string `json:"volume"`
}

// Endpoint belongs to one actual FUSE connection, across all guest sessions.
type Endpoint struct {
	FuseSession string `json:"fuse_session"`
	CommandNode uint64 `json:"command_node"`
	ResultNode  uint64 `json:"result_node"`
	RecallNode  uint64 `json:"recall_node"`
}

// Native captures the real request, independently of delegated guest credentials.
type Native struct {
	UID, GID, PID uint32
	Unique        uint64
	Groups        []uint32
	Process       string
	Endpoint      Endpoint
}

// NativeVerifier authenticates the supervised process incarnation, exact native
// headers/groups and actual connection on EVERY callback. No default exists.
type NativeVerifier interface {
	// Enroll binds the actual supervised FUSE endpoint to one nonreused controller
	// incarnation. It MUST refuse an already-issued endpoint or missing prior
	// ledger/exclusion; creating a Go object is never proof of a fresh connection.
	Enroll(context.Context, Endpoint, fsapi.UUID) error
	// Verify includes that exact enrolled controller lifetime on every callback.
	Verify(context.Context, Native, fsapi.UUID) error
}

// Attestor verifies guest-kernel provenance and the pinned complete numeric map.
// It must cover every ID and user namespace, not merely match an enrollment label.
type Attestor interface {
	Verify(context.Context, Context, Credentials) error
}

// Credentials is the immutable guest VFS authorization snapshot.
type Credentials struct {
	FSUID         uint32   `json:"fsuid"`
	FSGID         uint32   `json:"fsgid"`
	Groups        []uint32 `json:"groups"`
	Process       string   `json:"process"`
	Revision      uint64   `json:"credential_revision"`
	IDMapHash     string   `json:"idmap_hash"`
	UserNamespace string   `json:"user_namespace"`
}

// Claim names a permanently nonreused allocated public request number.
type Claim struct {
	Realm        string `json:"realm"`
	Subject      string `json:"subject"`
	Anchor       string `json:"anchor"`
	Nonce        string `json:"nonce"`
	Number       uint64 `json:"number"`
	OriginalHash string `json:"original_hash"`
}

// Capability is a complete service-issued scoped grant; native fh is never one.
type Capability struct {
	Token          string  `json:"token"`
	Kind           string  `json:"kind"`
	Scope          Context `json:"scope"`
	Home           string  `json:"object_home"`
	Allocation     string  `json:"object_allocation"`
	Description    *string `json:"description"`
	Rights         uint32  `json:"rights"`
	Revision       uint64  `json:"revision"`
	CredentialHash string  `json:"credential_hash"`
	AncestorsHash  string  `json:"ancestors_hash"`
}

// Ack names one exact immutable, completely received native response snapshot.
type Ack struct {
	Cell         uint64 `json:"cell"`
	ResponseHash string `json:"response_hash"`
}

// Command is the closed common command envelope.
type Command struct {
	Cell    uint64          `json:"cell"`
	Context Context         `json:"context"`
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload"`
	Reclaim *Ack            `json:"reclaim"`
}

// Intent contains a closed, semantically validated operation union.
type Intent struct {
	Opcode string          `json:"opcode"`
	Args   json.RawMessage `json:"args"`
}

// Call binds the original intent and guest credential bytes to its public claim.
type Call struct {
	Claim       Claim       `json:"claim"`
	Credentials Credentials `json:"credentials"`
	Intent      Intent      `json:"intent"`
}

// Binding retains original admission bytes, even on forwarded/reconciled retries.
type Binding struct {
	Context    Context
	Call       Call
	Canonical  []byte
	Operation  fsapi.UUID
	Allocation fsapi.ObjectID
}

// ClaimState is protected provider state, not inferred from transport outcomes.
type ClaimState struct {
	// ReconciledScope is installed only after verified original-binding recovery; it authorizes this exact lookup scope without replacing original Context.
	ReconciledScope *Context
	Status          string
	Binding         Binding
	Result          json.RawMessage
}

// Claims owns protected public bindings and permanent retirement floors. Resolve
// authenticates lookup scope before returning any old binding. Reserve atomically
// reserves original operation/allocation and all result/source credits before effects.
// ProtectResult persists exact bytes and source evidence before TERMINAL. Cancel
// may return NO_EFFECT only after protected DECLINE plus verified absence. Retire
// MUST retain unsent/rejected allocated-number gaps until their protected no-effect
// resolver proves them; an absent map entry never supplies a floor or tombstone.
// Providers must also bound retained principal/process/claim identities.
type Claims interface {
	Resolve(context.Context, Context, Claim) (ClaimState, error)
	Reserve(context.Context, Binding) (ClaimState, error)
	ProtectResult(context.Context, Binding, json.RawMessage) error
	Cancel(context.Context, Context, Claim) (ClaimState, error)
	Retire(context.Context, Context, json.RawMessage) (json.RawMessage, error)
}

// Prepared is actual shared-service planning/observation at one committed RAM cut.
type Prepared struct {
	Command fsapi.Command
	Plan    *fsservice.Planned
	Result  fsapi.Result
}

// Outcome returns authoritative logical output plus exact verified publication.
type Outcome struct {
	// DelegatedError preserves ErrDelegatedFileTooLarge from prepare as a
	// confirmed pre-effect result; no v1 semantic errno is invented for it.
	// It requires an otherwise zero Result and no protected mutation.
	DelegatedError  error
	Result          fsapi.Result
	Publication     json.RawMessage
	EntryAttributes []fsapi.Attr
	EntryRevision   uint64
}

// Captured supplies one immutable committed metadata cut and the shared Unix
// planning/observation path. Normal execution uses LogicalCut. Controlled local
// execution must use the real service's sealed atomic capture/dispatch seam;
// raw backing stores and synthesized normal3 authority are never substitutes.
type Captured interface {
	Image(context.Context, fsapi.UUID) (storage.Image, error)
	Stat(context.Context, fsapi.ObjectID) (storage.Object, error)
	Lookup(context.Context, fsapi.ObjectID, string) (storage.Entry, error)
	Prepare(context.Context, Binding, fsapi.Command) (Prepared, error)
}

// Execution serializes all origins at one captured committed cut and invokes
// prepare exactly once. It protects/publishes complete results and creation/open
// pins; prepare returning a plan alone does not authorize visibility. Errors
// after reservation remain uncertain and queryable. Normal Check verifies real
// public bindings/source/all-domain authority. Controlled Check verifies the
// named disposable profile, operator policy, actual local service and journal.
// Neither profile has a default provider. Check must not wait on an operation,
// grant, recall, vnode or a mutex held across such work.
type Execution interface {
	Check(context.Context, Context) error
	ServingGeneration(context.Context, Context) (fsapi.UUID, error)
	Execute(context.Context, Binding, func(Captured) (Prepared, error)) (Outcome, error)
}

// LogicalCut adapts the shared service planner and committed owner RAM view.
// It carries no publication authority; the normal Execution verifies that.
type LogicalCut struct {
	planner     *fsservice.LogicalPlanner
	observation *logicalview.Observation
}

// Views owns verified recall manifests, dirty reservations, events and recovery.
// It cannot turn a hash-only assertion into reconstructed continuity.
type Views interface {
	// ValidateDrain binds original credential/page/description provenance, frozen dirty entry, reservation and anchored claim before normal planning.
	ValidateDrain(context.Context, Binding) error
	Exchange(context.Context, Context, string, json.RawMessage) (json.RawMessage, error)
}

// Config is component enrollment only. It adds no CLI/mount/publication readiness.
// Every provider is mandatory, including for disposable controlled validation.
type Config struct {
	// Validation explicitly labels disposable component fixtures; it supplies no normal publication authority.
	Validation                              *fsservice.ValidationProfile
	Endpoint                                Endpoint
	Native                                  NativeVerifier
	Attestor                                Attestor
	Claims                                  Claims
	Execution                               Execution
	Views                                   Views
	Planner                                 *fsservice.LogicalPlanner
	Scopes                                  []Context
	MaxCapabilities, MaxCredentialRevisions int
}
type ancestor struct {
	Parent                        fsapi.ObjectID
	Object                        fsapi.ObjectID
	ObjectRevision, EntryRevision uint64
	Name                          string
}
type grant struct {
	Cap       Capability
	Ancestors []ancestor
	Handle    fsapi.OpenHandle
	Released  bool
	Sequence  uint64
}
type session struct {
	Generation     fsapi.UUID
	Reserved       int
	Context        Context
	Hello          bool
	Credentials    map[string][]byte
	Exited         map[string]uint64
	Grants         map[string]*grant
	CloseSequences map[string]uint64
}

// Service is a bounded delegated component; callbacks enqueue, workers dispatch.
type Service struct {
	controller        fsapi.UUID
	transportAttached bool
	mu                sync.Mutex
	config            Config
	sessions          map[string]*session
}
