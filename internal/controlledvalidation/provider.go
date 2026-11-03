// Package controlledvalidation supplies a durable, explicitly enrolled disposable
// single-node provider. Its private ledger is not normal distributed authority.
package controlledvalidation

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

const ledgerVersion = "homelabfs-private-controlled-validation/1"

// Enrollment is the operator-approved complete synthetic scope. Both endpoint
// keys additionally sign these exact bytes, proving acceptance of this profile.
type Enrollment struct {
	Profile           fsservice.ValidationProfile
	Scope             guestbridge.Context
	Endpoint          guestbridge.Endpoint
	ServingGeneration fsapi.UUID
	Ledger            fsapi.UUID
	Journal           fsservice.ControlledJournal
	HostKey, GuestKey []byte
}

// Authorization contains three detached Ed25519 signatures over json.Marshal
// of Enrollment. Operator is pinned out of band, never read from the ledger.
type Authorization struct {
	Enrollment                                       Enrollment
	OperatorSignature, HostSignature, GuestSignature []byte
}

// Facts rechecks actual installed component enrollment and guest provenance.
// No implementation/default is supplied: kernel/cache/installation facts belong
// to their real platform components, never a signature or a boolean substitute.
type Facts interface {
	Check(context.Context, Enrollment) error
}

// Config requires an existing private ledger directory, actual local service,
// signed endpoint enrollment and an independent live component fact verifier.
type Config struct {
	Directory                                                             string
	Initialize                                                            bool
	Service                                                               *fsservice.Service
	Operator                                                              ed25519.PublicKey
	Authorization                                                         Authorization
	Facts                                                                 Facts
	MaxClaims                                                             int
	MaxRetainedBytes, MaxSourceBytes, ClaimCreditBytes, SourceCreditBytes uint64
	// Failpoint is component-test instrumentation. Any error preserves evidence.
	Failpoint func(string) error
}

// Provider owns a single locked durable ledger and its exact enrolled scope.
type Provider struct {
	config    Config
	db        *bolt.DB
	execution sync.Mutex
	closed    atomic.Bool
}

// entry retains original bytes and exact recovery plan before any service effect.
type entry struct {
	Version                      string
	Claim                        guestbridge.Claim
	Binding                      guestbridge.Binding
	Status                       string
	Plan                         *fsservice.ControlledPlan
	Outcome                      *guestbridge.Outcome
	Result                       json.RawMessage `json:",omitempty"`
	Gap                          bool
	ProposedResult               json.RawMessage `json:",omitempty"`
	BridgeState                  json.RawMessage `json:",omitempty"`
	PlannedOutcome               *guestbridge.Outcome
	SnapshotVersion              uint64
	RetainedCredit, SourceCredit uint64
}
type principal struct{ High, Floor uint64 }
type captured struct {
	cut      *fsservice.ControlledCut
	plan     *fsservice.ControlledPlan
	provider *Provider
	owned    context.Context
	binding  guestbridge.Binding
}

var _ guestbridge.Claims = (*Provider)(nil)
var _ guestbridge.Execution = (*Provider)(nil)
var _ guestbridge.Captured = (*captured)(nil)
var _ guestbridge.ValidationState = (*Provider)(nil)
var _ guestbridge.ValidationExecution = (*Provider)(nil)

// New authenticates both endpoints before opening the private versioned ledger.
// bbolt's exclusive OS lock prevents concurrent/restarted writers from sharing it.
func New(ctx context.Context, c Config) (*Provider, error) {
	if c.Service == nil || nilValue(c.Facts) || len(c.Operator) != ed25519.PublicKeySize {
		return nil, fsapi.Invalid
	}
	if c.Service.State() != fsservice.Serving {
		return nil, fsapi.Unavailable
	}
	a := c.Authorization
	e := a.Enrollment
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	if validateEnrollment(e) != nil || bytes.Equal(e.HostKey, e.GuestKey) || !e.Ledger.Valid() || !e.Profile.Disposable || e.Profile.Name == "" || len(e.HostKey) != ed25519.PublicKeySize || len(e.GuestKey) != ed25519.PublicKeySize || !ed25519.Verify(c.Operator, raw, a.OperatorSignature) || !ed25519.Verify(e.HostKey, raw, a.HostSignature) || !ed25519.Verify(e.GuestKey, raw, a.GuestSignature) {
		return nil, fsapi.Permission
	}
	journal, err := c.Service.ControlledJournalInfo(ctx, e.Profile)
	if err != nil {
		return nil, err
	}
	if e.Journal != journal || e.Scope.Generation != journal.Stream.Generation.String() || e.Scope.Configuration != journal.Stream.Configuration.String() {
		return nil, fsapi.Permission
	}
	info := c.Service.FrontendInfo()
	if info.Validation != e.Profile || !info.UnixPermissions || info.Generation != e.ServingGeneration || info.Volume.String() != e.Scope.Volume || e.Endpoint.FuseSession != e.Scope.FuseSession {
		return nil, fsapi.Permission
	}
	if err = c.Facts.Check(ctx, e); err != nil {
		return nil, err
	}
	if c.MaxRetainedBytes == 0 {
		c.MaxRetainedBytes = 256 << 20
	}
	if c.MaxSourceBytes == 0 {
		c.MaxSourceBytes = 128 << 20
	}
	if c.ClaimCreditBytes == 0 {
		c.ClaimCreditBytes = 4 << 20
	}
	if c.SourceCreditBytes == 0 {
		c.SourceCreditBytes = 2 << 20
	}
	if c.MaxRetainedBytes > 1<<30 || c.MaxSourceBytes > 1<<30 || c.ClaimCreditBytes > c.MaxRetainedBytes || c.SourceCreditBytes > c.MaxSourceBytes || c.ClaimCreditBytes < 4096 || c.SourceCreditBytes < 4096 {
		return nil, fsapi.Invalid
	}
	if c.MaxClaims == 0 {
		c.MaxClaims = 4096
	}
	if c.MaxClaims < 1 || c.MaxClaims > 65536 {
		return nil, fsapi.Invalid
	}
	if !filepath.IsAbs(c.Directory) {
		return nil, fsapi.Invalid
	}
	st, err := os.Lstat(c.Directory)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode().Perm() != 0700 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return nil, fsapi.Permission
	}
	// Roots and policy keys are retained by value, not caller-owned slices.
	c.Operator = bytes.Clone(c.Operator)
	c.Authorization.Enrollment.HostKey = bytes.Clone(e.HostKey)
	c.Authorization.Enrollment.GuestKey = bytes.Clone(e.GuestKey)
	filename := filepath.Join(c.Directory, "controlled-validation-v1.db")
	fileInfo, statErr := os.Lstat(filename)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}
	if statErr == nil {
		if c.Initialize || !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm() != 0600 || fileInfo.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || fileInfo.Sys().(*syscall.Stat_t).Nlink != 1 {
			return nil, fsapi.Permission
		}
	} else {
		if !c.Initialize {
			return nil, fsapi.Unavailable
		}
		entries, err := os.ReadDir(c.Directory)
		if err != nil {
			return nil, err
		}
		if len(entries) != 0 {
			return nil, fsapi.Permission
		}
		file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
	}
	db, err := bolt.Open(filename, 0600, &bolt.Options{Timeout: time.Millisecond * 100})
	if err != nil {
		return nil, err
	}
	p := &Provider{config: c, db: db}
	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(ledgerVersion))
		if err != nil {
			return err
		}
		scope, err := json.Marshal(struct {
			Scope    guestbridge.Context
			Profile  fsservice.ValidationProfile
			Ledger   fsapi.UUID
			Operator []byte
		}{e.Scope, e.Profile, e.Ledger, c.Operator})
		if err != nil {
			return err
		}
		if old := b.Get([]byte("scope")); (old == nil && !c.Initialize) || (old != nil && !bytes.Equal(old, scope)) {
			return fsapi.Permission
		}
		if err = b.Put([]byte("scope"), scope); err != nil {
			return err
		}
		for _, name := range []string{"claims", "principals", "bridge", "processes", "retirements", "operations", "allocations"} {
			if _, err = b.CreateBucketIfNotExists([]byte(name)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if c.Initialize {
		directory, err := os.Open(c.Directory)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		syncErr := directory.Sync()
		closeErr := directory.Close()
		if syncErr != nil {
			_ = db.Close()
			return nil, syncErr
		}
		if closeErr != nil {
			_ = db.Close()
			return nil, closeErr
		}
	}
	return p, nil
}

// Close releases the ledger lock. It neither retires claims nor deletes state.
func (p *Provider) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	p.execution.Lock()
	defer p.execution.Unlock()
	return p.db.Close()
}

// Check checks immutable exact scope and live component facts without taking
// the execution mutex, waiting for an operation, or manufacturing authority.
func (p *Provider) Check(ctx context.Context, scope guestbridge.Context) error {
	if p.closed.Load() {
		return fsapi.Unavailable
	}
	if scope != p.config.Authorization.Enrollment.Scope {
		return fsapi.Permission
	}
	if p.config.Service.State() != fsservice.Serving {
		return fsapi.Unavailable
	}
	return p.config.Facts.Check(ctx, p.config.Authorization.Enrollment)
}

// ServingGeneration returns the actual selected local service generation.
func (p *Provider) ServingGeneration(ctx context.Context, scope guestbridge.Context) (fsapi.UUID, error) {
	if err := p.Check(ctx, scope); err != nil {
		return fsapi.UUID{}, err
	}
	return p.config.Service.FrontendInfo().Generation, nil
}

// CheckValidation verifies the exact operator-enrolled profile and endpoint at
// each bridge construction; changing only a disposable label grants no access.
func (p *Provider) CheckValidation(ctx context.Context, scope guestbridge.Context, profile fsservice.ValidationProfile, endpoint guestbridge.Endpoint) error {
	e := p.config.Authorization.Enrollment
	if e.Profile != profile || e.Endpoint != endpoint {
		return fsapi.Permission
	}
	return p.Check(ctx, scope)
}
