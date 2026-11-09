// Package viewcontrol implements the explicit disposable local view controller.
// It supplies no normal publication or installed kernel/MM proof.
package viewcontrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	bolt "go.etcd.io/bbolt"
)

// EnrollmentVersion is the exact private controlled ledger/enrollment version.
const EnrollmentVersion = "homelabfs-private-viewcontrol/1"

const version = EnrollmentVersion

// Bounds are signed, finite, separate capacities. Reservations preown 128KiB of
// manifest/freeze/control bytes plus actual provider claim/result/source credit.
// Sources preown three complete snapshot copies before any capture starts.
type Bounds struct {
	WorkBytes                                                  int64
	Tickets, ControlTickets, Reservations, Identities, Sources int
	SourceBytes                                                int64
	LedgerBytes                                                int
}

// TicketFamily is one signed monotone ticket identity lane.
type TicketFamily struct{ Anchor, Nonce string }

// TicketFamilies separates resource-refusable acquisitions from preowned controls.
// It is aligned one-for-one with Enrollment.Domains.
type TicketFamilies struct{ Acquisition, Control TicketFamily }

// Enrollment pins every domain's signed local provider enrollment, source and
// baseline. Every enrolled host/guest signs this complete aggregate inventory.
type Enrollment struct {
	TicketFamilies    []TicketFamilies
	Version           string
	ID                fsapi.UUID
	Domains           []controlledvalidation.Enrollment
	PageSize          uint64
	Bounds            Bounds
	Baseline          record.FrameRef
	BaselineStateHash string
}

// Signatures contains one domain's signatures over the complete Enrollment.
type Signatures struct{ Host, Guest []byte }

// Authorization pins operator approval plus every participating endpoint.
type Authorization struct {
	Enrollment Enrollment
	Operator   []byte
	Domains    []Signatures
}

// Unadopted binds the universal old-epoch exclusion statement. Its verifier must
// obtain actual authenticated ACK provenance and the admitted running kernel's
// adoption-barrier evidence; this structure/list/hash supplies no such proof.
type Unadopted struct {
	Enrollment                     Enrollment
	Event, Acknowledgement, Ledger json.RawMessage
	Scope                          guestbridge.Context
	OldFence                       uint64
	Omitted                        []guestbridge.DirtyReservation
}

// Facts is the mandatory external installed/native/guest trust boundary. Each
// method must verify live evidence, never caller booleans or hash equality.
// CheckPhysical covers cache/PTE/TLB/pins/busy aliases and source obligations for
// the exact request and full retained ledger. CheckRecovery establishes actual
// reconstructed continuity or positive exclusion for ALL old physical rights.
// Verifiers must use already available authenticated observations, never wait
// for recall while holding controller/service resources or reenter the worker.
// The package deliberately supplies no runtime implementation.
type Facts interface {
	CheckEnrollment(context.Context, Enrollment) error
	Authorize(context.Context, guestbridge.Context, string, json.RawMessage) error
	CheckPhysical(context.Context, guestbridge.Context, string, json.RawMessage, json.RawMessage) error
	CheckRecovery(context.Context, Enrollment, json.RawMessage) error
	CheckUnadopted(context.Context, Unadopted) error
}

// Config constructs one controlled component; it does not activate any frontend.
// Providers must be authenticated real controlledvalidation providers over Service.
type Config struct {
	restartFile os.FileInfo
	Directory   string
	Initialize  bool
	// RestoreCurrentSources reconstructs only exact current source bytes, never historical cuts.
	RestoreCurrentSources bool
	Service               *fsservice.Service
	Providers             map[string]*controlledvalidation.Provider
	Operator              ed25519.PublicKey
	Authorization         Authorization
	Facts                 Facts
	Failpoint             func(string) error
}

// Controller owns durable tickets, sources, reservations and physical recalls.
// mu is acquired with TryLock by ordinary admissions. An admitted execution owns
// it until completion, so no worker ever waits on recall holding execution locks.
type Controller struct {
	file                        os.FileInfo
	config                      Config
	db                          *bolt.DB
	mu                          sync.Mutex
	state                       ledger
	enrollmentHash, profileHash string
	closed                      atomic.Bool
	poisoned                    atomic.Bool
	sealed                      sync.Map
	// blockedACKs bounds repeated BLOCKED VIEW_ACK replies per session (see
	// BlockedACKBound); guarded by mu. now is a test clock, nil in production.
	blockedACKs map[string]blockedACK
	now         func() time.Time
}

type ledger struct {
	Terminal                map[string]terminalScope `json:",omitempty"`
	UUIDs                   map[string]bool
	Revisions               map[string]uint64
	AckArchive              map[string]string
	Retirements             map[string]json.RawMessage
	Version, EnrollmentHash string
	Sources                 map[string]fsservice.ControlledViewSource
	Tickets                 map[string]ticketState
	Families                map[string]family
	Leases                  map[string]leaseState
	Reservations            map[string]reservationState
	Domains                 map[string]domainState
	Recall                  *recallState
	Fence                   uint64
	Identities              int
	Recovery                bool
}
type family struct{ High, Floor uint64 }
type ticketState struct {
	DeclinedGap                 *controlledvalidation.Earmark
	GapClosureReply             json.RawMessage
	ExpectedMismatch            bool
	PayloadHash                 string
	Scope                       guestbridge.Context
	Request                     guestbridge.Ticket
	Operation                   string
	Payload                     json.RawMessage
	Initial, Current, Cancelled json.RawMessage
}
type leaseState struct {
	ReleaseTicket string
	ReleaseCredit bool
	Lease         guestbridge.ViewLease
	Active        bool
}
type reservationState struct {
	ClosureReply json.RawMessage
	ExclusionACK string
	TicketKey    string
	Reservation  guestbridge.DirtyReservation
	Status       string
	Recall       string
	Call         *guestbridge.Call
	Entry        json.RawMessage
}
type domainState struct {
	Domain   guestbridge.Capability
	Sequence uint64
}
type recallState struct {
	ID, Phase, Inventory, Closure string
	Events                        map[string]json.RawMessage
	Acks                          map[string]json.RawMessage
	Manifests                     map[string]json.RawMessage
}

var _ guestbridge.ViewProtocolProvider = (*Controller)(nil)
var _ guestbridge.Views = (*Controller)(nil)
var _ controlledvalidation.Admission = (*Controller)(nil)
var _ fsservice.ControlledAdmission = (*Controller)(nil)

// New verifies signed inventory and actual sealed source before opening an
// exclusive private durable ledger. Reopen requires full live continuity facts.
func New(ctx context.Context, c Config) (*Controller, error) {
	if c.Service == nil || nilValue(c.Facts) || !c.Service.ControlledViewsRequired() || len(c.Operator) != ed25519.PublicKeySize || !filepath.IsAbs(c.Directory) {
		return nil, fsapi.Invalid
	}
	raw, err := json.Marshal(c.Authorization.Enrollment)
	if err != nil {
		return nil, err
	}
	e := c.Authorization.Enrollment
	if e.Version != version || !e.ID.Valid() || e.PageSize != 4096 || len(e.Domains) < 1 || len(e.Domains) > 64 || len(e.Domains) != len(c.Authorization.Domains) || len(e.TicketFamilies) != len(e.Domains) || !ed25519.Verify(c.Operator, raw, c.Authorization.Operator) || !validBounds(e.Bounds) {
		return nil, fsapi.Permission
	}
	seen := map[string]bool{}
	for i, d := range e.Domains {
		families := e.TicketFamilies[i]
		for _, id := range []string{families.Acquisition.Anchor, families.Acquisition.Nonce, families.Control.Anchor, families.Control.Nonce} {
			if _, err := fsapi.ParseUUID(id); err != nil {
				return nil, fsapi.Invalid
			}
		}
		if families.Acquisition == families.Control {
			return nil, fsapi.Permission
		}
		signatures := c.Authorization.Domains[i]
		p := c.Providers[d.Scope.Session]
		if p == nil || !p.UsesService(c.Service) || !equal(p.Enrollment(), d) || seen[d.Scope.Session] || !ed25519.Verify(d.HostKey, raw, signatures.Host) || !ed25519.Verify(d.GuestKey, raw, signatures.Guest) {
			return nil, fsapi.Permission
		}
		seen[d.Scope.Session] = true
		info, err := c.Service.ControlledJournalInfo(ctx, d.Profile)
		if err != nil {
			return nil, err
		}
		if info != d.Journal || d.Scope.OriginJournal != info.Incarnation.String() || d.Scope.Volume != info.Stream.Volume.String() || d.Scope.Generation != info.Stream.Generation.String() || d.Scope.Configuration != info.Stream.Configuration.String() || c.Service.FrontendInfo().Generation != d.ServingGeneration {
			return nil, fsapi.Permission
		}
	}
	if len(c.Providers) != len(e.Domains) {
		return nil, fsapi.Permission
	}
	if e.Bounds.WorkBytes < 3*int64(e.Bounds.LedgerBytes)+6*e.Bounds.SourceBytes+8*c.Service.ControlledViewMetadataBytes() {
		return nil, fsapi.NoSpace
	}
	if err = c.Facts.CheckEnrollment(ctx, e); err != nil {
		return nil, err
	}
	// Detach all caller-owned maps, slices, signatures and policy bytes.
	var copied Authorization
	authorizationBytes, _ := json.Marshal(c.Authorization)
	if err = json.Unmarshal(authorizationBytes, &copied); err != nil {
		return nil, err
	}
	c.Authorization = copied
	c.Operator = bytes.Clone(c.Operator)
	providers := map[string]*controlledvalidation.Provider{}
	for k, v := range c.Providers {
		providers[k] = v
	}
	c.Providers = providers
	if err = privateDirectory(c.Directory); err != nil {
		return nil, err
	}
	filename := filepath.Join(c.Directory, "viewcontrol-v1.db")
	if err = prepareFile(filename, c.Initialize); err != nil {
		return nil, err
	}
	var actualFile os.FileInfo
	db, err := bolt.Open(filename, 0600, &bolt.Options{Timeout: 100 * time.Millisecond, OpenFile: restartLedgerOpener(c.restartFile, &actualFile)})
	if err != nil {
		return nil, err
	}
	controller := &Controller{config: c, db: db, file: actualFile, enrollmentHash: hash(raw), profileHash: digest(struct {
		Domains  []controlledvalidation.Enrollment
		Bounds   Bounds
		Families []TicketFamilies
		PageSize uint64
	}{e.Domains, e.Bounds, e.TicketFamilies, e.PageSize})}
	ok := false
	defer func() {
		if !ok {
			_ = db.Close()
			controller.closed.Store(true)
		}
	}()
	if err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(version))
		if err != nil {
			return err
		}
		old := b.Get([]byte("state"))
		if old == nil {
			if !c.Initialize {
				return fsapi.Unavailable
			}
			controller.state = ledger{UUIDs: map[string]bool{}, Revisions: map[string]uint64{}, AckArchive: map[string]string{}, Retirements: map[string]json.RawMessage{}, Version: version, EnrollmentHash: controller.enrollmentHash, Sources: map[string]fsservice.ControlledViewSource{}, Tickets: map[string]ticketState{}, Families: map[string]family{}, Leases: map[string]leaseState{}, Reservations: map[string]reservationState{}, Domains: map[string]domainState{}, Fence: 1}
		} else {
			if c.Initialize {
				return fsapi.Permission
			}
			if len(old) > e.Bounds.LedgerBytes {
				return fsapi.NoSpace
			}
			if err = json.Unmarshal(old, &controller.state); err != nil {
				return err
			}
			if controller.state.Version != version || controller.state.EnrollmentHash != controller.enrollmentHash {
				return fsapi.Permission
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	source, err := controller.capture(ctx)
	if err != nil {
		return nil, err
	}
	if c.Initialize {
		if source.Through != e.Baseline || hash(source.State) != e.BaselineStateHash {
			return nil, fsapi.Stale
		}
	} else {
		for _, old := range controller.state.Sources {
			if old.Journal != source.Journal || old.Visibility != source.Visibility || old.Through.Position.Sequence > source.Through.Position.Sequence {
				return nil, fsapi.Stale
			}
		}
	}
	// Reopened JSON carries no live source seal. Only the exact current source,
	// recaptured by the real service worker above, can reconstruct correspondence.
	// Historical cuts remain unsupported rather than importing their hashes.
	if !c.Initialize && c.RestoreCurrentSources {
		for key, old := range controller.state.Sources {
			if !sameSource(old, source) {
				return nil, fsapi.Unavailable
			}
			controller.state.Sources[key] = source
		}
	}
	state, err := controller.rawState()
	if err != nil {
		return nil, err
	}
	if err = c.Facts.CheckRecovery(controller.factsContext(ctx), e, state); err != nil {
		return nil, err
	}
	if !c.Initialize && controller.state.Recall != nil {
		controller.state.Recall.Acks = map[string]json.RawMessage{}
	}
	if err = controller.validateLedger(); err != nil {
		return nil, err
	}
	if err = controller.remember(source); err != nil {
		return nil, err
	}
	if err = controller.save(); err != nil {
		return nil, err
	}
	if c.Initialize {
		if err = syncDirectory(c.Directory); err != nil {
			return nil, err
		}
	}
	for _, p := range c.Providers {
		if err = p.BindAdmission(controller); err != nil {
			return nil, err
		}
	}
	if err = c.Service.BindControlledAdmission(e.Domains[0].Profile, controller); err != nil {
		return nil, err
	}
	if !c.Initialize && c.RestoreCurrentSources {
		for session, scope := range controller.state.Terminal {
			controller.sealed.Store(session, scope.Seal)
		}
	}
	ok = true
	return controller, nil
}

// Close releases only the ledger lock. All rights/obligations remain durable.
func (c *Controller) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed.Load() {
		return nil
	}
	c.closed.Store(true)
	return c.db.Close()
}

// CheckEnrollment verifies exact tuple, immutable scope and live installed facts.
func (c *Controller) CheckEnrollment(ctx context.Context, scope guestbridge.Context, p guestbridge.Protocol) error {
	expected, ok := c.scopeProtocol(scope)
	if !ok || p != expected {
		return fsapi.Permission
	}
	return c.check(ctx, scope)
}

// CheckTarget verifies a retained sealed source against actual current service
// bytes, signed enrollment and exact scope. Historical source verification is
// deliberately fail-closed until its original source can be reverified.
func (c *Controller) CheckTarget(ctx context.Context, scope guestbridge.Context, target guestbridge.ViewTarget) error {
	if !c.mu.TryLock() {
		return fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	if err := c.check(ctx, scope); err != nil {
		return err
	}
	if _, sealed := c.state.Terminal[scope.Session]; sealed {
		return fsapi.Permission
	}
	if target.Kind != "CONTROLLED_LOCAL" || target.Cut == nil {
		return fsapi.Permission
	}
	var source *fsservice.ControlledViewSource
	for _, stored := range c.state.Sources {
		if equal(target, c.target(scope, stored)) {
			copySource := stored
			source = &copySource
			break
		}
	}
	if source == nil {
		return fsapi.Stale
	}
	return c.config.Service.Controlled(ctx, c.config.Authorization.Enrollment.Domains[0].Profile, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		if err := cut.VerifyViewReference(ctx, source.Through); err != nil {
			return err
		}
		current, err := cut.ViewSource(ctx, c.config.Authorization.Enrollment.Bounds.SourceBytes)
		if err != nil {
			return err
		}
		if current.Journal != source.Journal || current.Visibility != source.Visibility {
			return fsapi.Stale
		}
		if current.Through == source.Through && !sameSource(current, *source) {
			return fsapi.Stale
		}
		return nil
	})
}

// CheckRequest independently authorizes the actual original principal before
// any response, replay or state exposure, including control/non-view commands.
func (c *Controller) CheckRequest(ctx context.Context, scope guestbridge.Context, op string, raw json.RawMessage) error {
	if err := c.terminalIngress(ctx, scope, op); err != nil {
		return err
	}
	if err := c.check(ctx, scope); err != nil {
		return err
	}
	if _, err := guestbridge.Canonical(raw); err != nil {
		return err
	}
	return c.config.Facts.Authorize(ctx, scope, op, bytes.Clone(raw))
}

// IsClosed reports a released ledger owner for exact same-ledger hook recovery.
// It supplies no physical fencing or endpoint continuity evidence.
func (c *Controller) IsClosed() bool { return c.closed.Load() }

func (c *Controller) check(ctx context.Context, scope guestbridge.Context) error {
	if c.closed.Load() || c.poisoned.Load() || c.provider(scope) == nil || c.config.Service.State() != fsservice.Serving {
		return fsapi.Permission
	}
	return c.config.Facts.CheckEnrollment(ctx, c.config.Authorization.Enrollment)
}

func (c *Controller) provider(scope guestbridge.Context) *controlledvalidation.Provider {
	for _, e := range c.config.Authorization.Enrollment.Domains {
		if e.Scope == scope {
			return c.config.Providers[scope.Session]
		}
	}
	return nil
}

// capture reads the current controlled source on the service worker. A full
// service queue refuses with fsapi.NoSpace (fsservice.Controlled), which is
// transient contention, not a fault.
func (c *Controller) capture(ctx context.Context) (fsservice.ControlledViewSource, error) {
	var source fsservice.ControlledViewSource
	if err := c.fail("view-capture"); err != nil {
		return source, err
	}
	err := c.config.Service.Controlled(ctx, c.config.Authorization.Enrollment.Domains[0].Profile, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		var err error
		source, err = cut.ViewSource(ctx, c.config.Authorization.Enrollment.Bounds.SourceBytes)
		return err
	})
	return source, err
}

func (c *Controller) target(scope guestbridge.Context, source fsservice.ControlledViewSource) guestbridge.ViewTarget {
	return guestbridge.ViewTarget{Kind: "CONTROLLED_LOCAL", Cut: &guestbridge.LocalViewCut{EnrollmentHash: c.enrollmentHash, ProfileHash: c.profileHash, ContextHash: digest(scope), JournalIncarnation: source.Journal.Incarnation.String(), JournalEpoch: source.Through.Position.Epoch, JournalPosition: source.Through.Position.Sequence, JournalHash: hex.EncodeToString(source.Through.SHA256[:]), StateHash: hash(source.State), VisibilityGeneration: source.Visibility.String()}}
}

func (c *Controller) remember(source fsservice.ControlledViewSource) error {
	key := digest(source.Through) + hash(source.State)
	if _, ok := c.state.Sources[key]; ok {
		return nil
	}
	if len(c.state.Sources) >= c.config.Authorization.Enrollment.Bounds.Sources {
		return fsapi.NoSpace
	}
	c.state.Sources[key] = source
	return nil
}

func (c *Controller) rawState() (json.RawMessage, error) { return json.Marshal(c.state) }

func (c *Controller) save() (result error) {
	defer func() {
		if result != nil {
			c.poisoned.Store(true)
		}
	}()
	raw, err := c.rawState()
	if err != nil {
		return err
	}
	if len(raw) > c.config.Authorization.Enrollment.Bounds.LedgerBytes {
		return fsapi.NoSpace
	}
	if err = c.db.Update(func(tx *bolt.Tx) error { return tx.Bucket([]byte(version)).Put([]byte("state"), raw) }); err != nil {
		return err
	}
	return c.fail("state-protected")
}

func (c *Controller) fail(name string) error {
	if c.config.Failpoint != nil {
		return c.config.Failpoint(name)
	}
	return nil
}

func validBounds(b Bounds) bool {
	return b.Tickets > 0 && b.Tickets <= 65536 && b.ControlTickets >= 4 && b.ControlTickets <= 16384 && b.Reservations > 0 && b.Reservations <= 64 && b.Identities >= b.Tickets+b.ControlTickets && b.Identities <= 65536 && b.Sources >= 2 && b.Sources <= 128 && b.SourceBytes >= 4096 && b.SourceBytes <= 16<<20 && b.LedgerBytes >= b.Reservations*(128<<10)+int(b.SourceBytes)*b.Sources*3+(b.Tickets+b.ControlTickets)*(128<<10)+b.Identities*(32<<10) && b.LedgerBytes <= 512<<20 && b.WorkBytes > 0 && b.WorkBytes <= 2<<30
}

func privateDirectory(path string) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode().Perm() != 0700 || stat.Uid != uint32(os.Getuid()) {
		return fsapi.Permission
	}
	return nil
}

func prepareFile(filename string, initialize bool) error {
	st, err := os.Lstat(filename)
	if err == nil {
		stat, ok := st.Sys().(*syscall.Stat_t)
		if initialize || !ok || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
			return fsapi.Permission
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !initialize {
		return fsapi.Unavailable
	}
	entries, err := os.ReadDir(filepath.Dir(filename))
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fsapi.Permission
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	return f.Close()
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	closed := f.Close()
	if err != nil {
		return err
	}
	return closed
}

func hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func digest(v any) string {
	raw, err := canonical(v)
	if err != nil {
		return ""
	}
	return hash(raw)
}

func canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object any
	if err = decoder.Decode(&object); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(object); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte{'\n'}), nil
}

func equal(a, b any) bool {
	x, e := canonical(a)
	y, f := canonical(b)
	return e == nil && f == nil && bytes.Equal(x, y)
}

func uuid() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fsapi.UUID(b).String(), nil
}

func nilValue(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return r.IsNil()
	}
	return false
}

func sameSource(a, b fsservice.ControlledViewSource) bool {
	return a.Journal == b.Journal && a.Through == b.Through && a.Visibility == b.Visibility && a.Applied == b.Applied && bytes.Equal(a.State, b.State)
}

func (c *Controller) validateLedger() error {
	s := c.state
	b := c.config.Authorization.Enrollment.Bounds
	if s.UUIDs == nil || len(s.UUIDs) > 2*b.Identities || s.Revisions == nil || len(s.Revisions) > b.Identities || s.Sources == nil || s.Tickets == nil || s.Families == nil || s.Leases == nil || s.Reservations == nil || s.Domains == nil || s.AckArchive == nil || s.Retirements == nil || len(s.Sources) > b.Sources || len(s.Tickets) > b.Identities || len(s.Reservations) > b.Reservations || len(s.Leases) > b.Identities || len(s.Domains) > len(c.config.Authorization.Enrollment.Domains) || len(s.AckArchive) > b.Identities*8 || s.Identities > b.Identities || s.Fence == 0 {
		return fsapi.Stale
	}
	for key, source := range s.Sources {
		if key != digest(source.Through)+hash(source.State) || int64(len(source.State)) > b.SourceBytes {
			return fsapi.Stale
		}
	}
	ordinary, control := 0, 0
	for _, t := range s.Tickets {
		if c.retired(t) {
			continue
		}
		if controlTicket(t.Operation) {
			control++
		} else {
			ordinary++
		}
	}
	if ordinary > b.Tickets || control > b.ControlTickets {
		return fsapi.Stale
	}
	for key, t := range s.Tickets {
		k, f := ticketKeys(t.Scope, t.Request)
		if key != k || c.provider(t.Scope) == nil || s.Families[f].High < t.Request.Number {
			return fsapi.Stale
		}
	}
	for _, f := range s.Families {
		if f.Floor > f.High {
			return fsapi.Stale
		}
	}
	for key, l := range s.Leases {
		if key != l.Lease.ID || key != objectID(l.Lease) || c.provider(l.Lease.Scope) == nil {
			return fsapi.Stale
		}
	}
	for key, r := range s.Reservations {
		if key != r.Reservation.ID || key != objectID(r.Reservation) || c.provider(r.Reservation.Scope) == nil {
			return fsapi.Stale
		}
	}
	return nil
}

func (c *Controller) newUUID() (string, error) {
	if len(c.state.UUIDs) >= 2*c.config.Authorization.Enrollment.Bounds.Identities {
		return "", fsapi.NoSpace
	}
	for range 128 {
		id, err := uuid()
		if err != nil {
			return "", err
		}
		if c.state.UUIDs[id] {
			continue
		}
		c.state.UUIDs[id] = true
		return id, nil
	}
	return "", fsapi.NoSpace
}

// scopeProtocol derives the exact tuple from the complete signed domain inventory.
func (c *Controller) scopeProtocol(scope guestbridge.Context) (guestbridge.Protocol, bool) {
	for _, domain := range c.config.Authorization.Enrollment.Domains {
		if domain.Scope == scope {
			if domain.Protocol != nil {
				return *domain.Protocol, true
			}
			return guestbridge.ViewProtocol(), true
		}
	}
	return guestbridge.Protocol{}, false
}
