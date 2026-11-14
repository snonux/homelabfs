package viewcontrol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

// primitiveFacts is explicitly a primitive-boundary shim. It requires exact
// predeclared observations, not a permissive success default. It establishes no
// installed guest/MM/PTE/TLB/native fencing or power-loss proof.
type primitiveFacts struct {
	exclusions       map[string]json.RawMessage
	mu               sync.Mutex
	enrollment       Enrollment
	scopes           map[string]guestbridge.Context
	credentials      map[string]guestbridge.Credentials
	caps             map[string]guestbridge.Capability
	physical         map[string]bool
	processes        *processRegistry
	recovery         string
	deny             bool
	initialExclusion bool
}
type providerPrimitive struct {
	expected controlledvalidation.Enrollment
}
type nativePrimitive struct {
	native     guestbridge.Native
	controller fsapi.UUID
}
type attestorPrimitive struct {
	scope       guestbridge.Context
	credentials guestbridge.Credentials
	// processes optionally admits further guest processes of this scope (the
	// zq3 workload model); nil keeps the single enrolled credential.
	processes *processRegistry
}
type rig struct {
	transportResponseWait time.Duration
	transportReclaim      []*guestbridge.Ack
	transport             []*guestbridge.Transport
	controlTickets        []uint64
	t                     *testing.T
	service               *fsservice.Service
	config                fsservice.Config
	controller            *Controller
	cc                    Config
	providers             []*controlledvalidation.Provider
	bridges               []*guestbridge.Service
	scopes                []guestbridge.Context
	credentials           []guestbridge.Credentials
	natives               []guestbridge.Native
	domains               []guestbridge.Capability
	// roots are the mount ROOT capabilities (claim 1 of each domain).
	roots                []guestbridge.Capability
	next, tickets, cells []uint64
	facts                *primitiveFacts
	// processes holds the extra guest process credentials of a workload model.
	processes *processRegistry
}
type result struct {
	// ID is the result's self id, which a RETIRE names as its result hash.
	ID           string
	Code, Data   string
	Count        uint64
	Capabilities []guestbridge.Capability
	// Entries are a READDIR page; each entry carries a transient grant.
	Entries     []struct{ Capability guestbridge.Capability }
	Publication json.RawMessage
}
type viewReply struct {
	ID, Status, Operation string
	Lease                 *guestbridge.ViewLease
	Reservation           *guestbridge.DirtyReservation
	Entry                 json.RawMessage
}

func (p providerPrimitive) Check(_ context.Context, e controlledvalidation.Enrollment) error {
	if !equal(p.expected, e) {
		return fsapi.Permission
	}
	return nil
}
func (n *nativePrimitive) Enroll(_ context.Context, e guestbridge.Endpoint, id fsapi.UUID) error {
	if n.controller.Valid() || e != n.native.Endpoint {
		return fsapi.Permission
	}
	n.controller = id
	return nil
}
func (n *nativePrimitive) Verify(_ context.Context, actual guestbridge.Native, id fsapi.UUID) error {
	if id != n.controller || !equal(actual, n.native) {
		return fsapi.Permission
	}
	return nil
}
func (a attestorPrimitive) Verify(_ context.Context, scope guestbridge.Context, credentials guestbridge.Credentials) error {
	if scope != a.scope || !equal(credentials, a.credentials) && !a.processes.has(scope.Session, credentials) {
		return fsapi.Permission
	}
	return nil
}
func (p *primitiveFacts) CheckEnrollment(_ context.Context, e Enrollment) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deny || !equal(e, p.enrollment) {
		return fsapi.Permission
	}
	return nil
}
func (p *primitiveFacts) Authorize(_ context.Context, s guestbridge.Context, op string, raw json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deny || p.scopes[s.Session] != s {
		return fsapi.Permission
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	var check func(any) error
	check = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			if _, ok := v["token"]; ok {
				b, _ := json.Marshal(v)
				var cap guestbridge.Capability
				if json.Unmarshal(b, &cap) != nil || !equal(cap, p.caps[cap.Token]) {
					return fsapi.Permission
				}
			}
			if _, ok := v["credential_revision"]; ok {
				b, _ := json.Marshal(v)
				var credential guestbridge.Credentials
				if json.Unmarshal(b, &credential) != nil || !equal(credential, p.credentials[s.Session]) && !p.processes.has(s.Session, credential) {
					return fsapi.Permission
				}
			}
			for _, child := range v {
				if err := check(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := check(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return check(value)
}
func (p *primitiveFacts) CheckPhysical(_ context.Context, _ guestbridge.Context, op string, raw, ledger json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deny || len(ledger) == 0 {
		return fsapi.Unavailable
	}
	if !p.physical[op+hash(raw)] {
		// Like the runtime Facts, only an ACK's missing measurement is the
		// "observer has not measured it yet" class; other ops stay Unavailable.
		if op == "VIEW_ACK" {
			return ErrNotObserved
		}
		return fsapi.Unavailable
	}
	return nil
}
func (p *primitiveFacts) CheckRecovery(_ context.Context, e Enrollment, ledger json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var initial ledgerStateForPrimitive
	_ = json.Unmarshal(ledger, &initial)
	initialProof := p.initialExclusion && len(initial.Tickets) == 0 && len(initial.Sources) == 0
	if p.deny || !equal(e, p.enrollment) || (!initialProof && hash(ledger) != p.recovery) {
		return fsapi.Unavailable
	}
	return nil
}
func uid(n byte) fsapi.UUID {
	var id fsapi.UUID
	id[0] = n
	id[6] = 64
	id[8] = 128
	id[15] = n
	return id
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func private(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hvc-")
	must(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	must(t, os.Chmod(dir, 0700))
	return dir
}
func root(t *testing.T) storage.Filesystem {
	t.Helper()
	fs, err := vfs.NewOS(t.Context(), private(t))
	must(t, err)
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}
func keys(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	return key
}
func newRig(t *testing.T, overrides ...func(*Config)) *rig {
	t.Helper()
	return newRigWith(t, nil, overrides...)
}

// newRigWith is newRig with explicit provider and bridge limits (nil keeps the
// component defaults), as the controlled runtime passes them from its spec.
func newRigWith(t *testing.T, limits *rigLimits, overrides ...func(*Config)) *rig {
	t.Helper()
	r := &rig{controlTickets: make([]uint64, 2), t: t, next: make([]uint64, 2), tickets: make([]uint64, 2), cells: make([]uint64, 2), processes: &processRegistry{allowed: map[string]bool{}}}
	object := fsapi.ObjectID{Home: uid(1), Allocation: uid(4)}
	profile := &fsservice.ValidationProfile{Disposable: true, Name: "viewcontrol-real-local"}
	r.startService(object, profile)
	k := rigKeys{operator: keys(t), hosts: []ed25519.PrivateKey{keys(t), keys(t)}, guests: []ed25519.PrivateKey{keys(t), keys(t)}}
	r.enrollProviders(profile, k, limits)
	r.signEnrollment(profile, k, overrides)
	// Native and provider effects are closed before the mandatory controller binds.
	req := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
	if err := r.service.Dispatch(t.Context(), req, fsapi.Command{Operation: fsapi.Getattr, Mount: uid(1), Object: object}); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("unguarded native: %v", err)
	}
	var err error
	r.controller, err = New(t.Context(), r.cc)
	must(t, err)
	r.cc.Initialize = false
	r.startBridges(profile, limits)
	t.Cleanup(func() {
		if r.controller != nil {
			_ = r.controller.Close()
		}
		for _, p := range r.providers {
			_ = p.Close()
		}
		_ = r.service.Close()
	})
	return r
}

// rigKeys are the operator and per-domain host/guest signing keys.
type rigKeys struct {
	operator      ed25519.PrivateKey
	hosts, guests []ed25519.PrivateKey
}

// startService writes the volume's initial journal and starts the real
// controlled-view service on it.
func (r *rig) startService(object fsapi.ObjectID, profile *fsservice.ValidationProfile) {
	t := r.t
	permissions, err := fsservice.NewUnixPermissions(uid(20))
	must(t, err)
	r.config = fsservice.Config{Stream: segment.Stream{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Kind: 1}, Storage: storage.Config{Volume: uid(1), Root: object, OwnerUID: uint32(os.Getuid())}, Validation: profile, RequireControlledViews: true, Permissions: permissions, Source: root(t), Reservations: root(t), Workspace: fsservice.Workspace{ID: uid(30), Data: root(t), Journal: root(t), Progress: root(t)}}
	r.config.Authority = record.Authority{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Owner: uid(6), Partner: uid(8), Members: []fsapi.UUID{uid(6), uid(8), uid(9)}, CoordinatorTerm: 1, OwnerIncarnation: uid(7), PartnerIncarnation: uid(10)}
	directory := "."
	for _, part := range split(r.config.Stream.Directory()) {
		next := path.Join(directory, part)
		must(t, r.config.Source.Mkdir(t.Context(), next, 0700))
		must(t, r.config.Source.SyncDir(t.Context(), directory))
		directory = next
	}
	header := record.Header{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Operation: uid(90), Incarnation: uid(7)}
	writer, err := segment.NewWriter(t.Context(), r.config.Source, r.config.Stream, segment.Options{MaximumBytes: 16 << 20, Structural: func(uint64) record.Header { return header }, NewSegment: func() fsapi.UUID { return uid(80) }})
	must(t, err)
	init := record.VolumeInit{Root: object, Initial: record.Metadata{Revision: 1, Type: 2, UID: 100, GID: 200, Mode: 02775, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: object}, RootLedger: record.LedgerEdit{Object: object, ResultingRevision: 1}, Authority: r.config.Authority, Authorization: record.Artifact{Kind: 6, ID: uid(70), Length: 1}}
	receipt, err := writer.Append(t.Context(), record.Record{Header: header, Payload: init})
	must(t, err)
	must(t, receipt.Wait(t.Context()))
	must(t, writer.Close())
	r.service, err = fsservice.New(t.Context(), r.config)
	must(t, err)
}

// enrollProviders creates both domains' signed private providers (limits
// applied) and their scopes, credentials and native peers.
func (r *rig) enrollProviders(profile *fsservice.ValidationProfile, k rigKeys, limits *rigLimits) {
	t := r.t
	journal, err := r.service.ControlledJournalInfo(t.Context(), *profile)
	must(t, err)
	r.cc = Config{Directory: private(t), Initialize: true, Service: r.service, Operator: k.operator.Public().(ed25519.PublicKey), Providers: map[string]*controlledvalidation.Provider{}}
	for i := range 2 {
		n := byte(i)
		scope := guestbridge.Context{AuthorityHash: hash([]byte("authority")), Cluster: uid(40).String(), Configuration: uid(3).String(), Export: uid(41 + n).String(), ExporterIncarnation: uid(42 + n).String(), FuseSession: uid(43 + n).String(), Generation: uid(2).String(), GuestBoot: uid(44 + n).String(), GuestMount: uid(45 + n).String(), OriginIncarnation: uid(46 + n).String(), OriginJournal: journal.Incarnation.String(), OriginNode: uid(48 + n).String(), PolicyHash: hash([]byte("policy")), RegistryHash: hash([]byte("registry")), Session: uid(50 + n).String(), VM: uid(52 + n).String(), Volume: uid(1).String()}
		credential := guestbridge.Credentials{FSUID: 1000, FSGID: 1001, Groups: []uint32{200}, Process: uid(63 + n).String(), Revision: 1, IDMapHash: hash([]byte("map")), UserNamespace: uid(64).String()}
		endpoint := guestbridge.Endpoint{FuseSession: scope.FuseSession, CommandNode: 2, ResultNode: 3, RecallNode: 4}
		enrollment := controlledvalidation.Enrollment{Profile: *profile, Scope: scope, Endpoint: endpoint, ServingGeneration: uid(30), Ledger: uid(65 + n), Journal: journal, HostKey: k.hosts[i].Public().(ed25519.PublicKey), GuestKey: k.guests[i].Public().(ed25519.PublicKey)}
		raw, _ := json.Marshal(enrollment)
		pc := controlledvalidation.Config{Directory: private(t), Initialize: true, Service: r.service, Operator: r.cc.Operator, Authorization: controlledvalidation.Authorization{Enrollment: enrollment, OperatorSignature: ed25519.Sign(k.operator, raw), HostSignature: ed25519.Sign(k.hosts[i], raw), GuestSignature: ed25519.Sign(k.guests[i], raw)}, Facts: providerPrimitive{enrollment}}
		limits.provider(&pc)
		p, err := controlledvalidation.New(t.Context(), pc)
		must(t, err)
		r.providers = append(r.providers, p)
		r.scopes = append(r.scopes, scope)
		r.credentials = append(r.credentials, credential)
		r.natives = append(r.natives, guestbridge.Native{UID: 1000, GID: 1001, PID: 100 + uint32(i), Unique: 1, Groups: []uint32{200}, Process: uid(75 + n).String(), Endpoint: endpoint})
		r.cc.Providers[scope.Session] = p
		r.cc.Authorization.Enrollment.Domains = append(r.cc.Authorization.Enrollment.Domains, enrollment)
	}
}

// signEnrollment completes the controller enrollment (default bounds, then
// the caller's overrides), signs it and installs the primitive facts.
func (r *rig) signEnrollment(profile *fsservice.ValidationProfile, k rigKeys, overrides []func(*Config)) {
	t := r.t
	e := &r.cc.Authorization.Enrollment
	for range 2 {
		e.TicketFamilies = append(e.TicketFamilies, TicketFamilies{Acquisition: TicketFamily{uid(110).String(), uid(111).String()}, Control: TicketFamily{uid(110).String(), uid(112).String()}})
	}
	e.Version = version
	e.ID = uid(99)
	e.PageSize = 4096
	e.Bounds = Bounds{Tickets: 64, ControlTickets: 32, Reservations: 4, Identities: 256, Sources: 64, SourceBytes: 64 << 10, LedgerBytes: 48 << 20, WorkBytes: 320 << 20}
	for _, override := range overrides {
		override(&r.cc)
	}
	var source fsservice.ControlledViewSource
	must(t, r.service.Controlled(t.Context(), *profile, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		var err error
		source, err = cut.ViewSource(ctx, e.Bounds.SourceBytes)
		return err
	}))
	e.Baseline = source.Through
	e.BaselineStateHash = hash(source.State)
	raw, _ := json.Marshal(*e)
	r.cc.Authorization.Operator = ed25519.Sign(k.operator, raw)
	for i := range 2 {
		r.cc.Authorization.Domains = append(r.cc.Authorization.Domains, Signatures{Host: ed25519.Sign(k.hosts[i], raw), Guest: ed25519.Sign(k.guests[i], raw)})
	}
	r.facts = &primitiveFacts{enrollment: *e, scopes: map[string]guestbridge.Context{}, credentials: map[string]guestbridge.Credentials{}, caps: map[string]guestbridge.Capability{}, physical: map[string]bool{}, exclusions: map[string]json.RawMessage{}, initialExclusion: true, processes: r.processes}
	for i, s := range r.scopes {
		r.facts.scopes[s.Session] = s
		r.facts.credentials[s.Session] = r.credentials[i]
	}
	r.cc.Facts = r.facts
}

// startBridges binds one view-protocol bridge per domain (limits applied),
// checks that a substituted view binding is refused, HELLOs and ROOTs.
func (r *rig) startBridges(profile *fsservice.ValidationProfile, limits *rigLimits) {
	t := r.t
	for i, p := range r.providers {
		protocol := guestbridge.ViewProtocol()
		config := guestbridge.Config{Validation: profile, Protocol: &protocol, ViewProtocol: r.controller, Endpoint: r.natives[i].Endpoint, Native: &nativePrimitive{native: r.natives[i]}, Attestor: attestorPrimitive{r.scopes[i], r.credentials[i], r.processes}, Claims: p, Execution: p, Views: r.controller, Scopes: []guestbridge.Context{r.scopes[i]}}
		limits.bridge(&config)
		for _, changed := range []func(*guestbridge.Config){func(c *guestbridge.Config) { c.Views = &Controller{} }, func(c *guestbridge.Config) { c.ViewProtocol = &Controller{} }, func(c *guestbridge.Config) { c.Protocol = nil; c.ViewProtocol = nil }} {
			wrong := config
			changed(&wrong)
			if _, err := guestbridge.New(t.Context(), wrong); !errors.Is(err, fsapi.Permission) {
				t.Fatalf("substituted required-view binding accepted %v", err)
			}
		}
		bridge, err := guestbridge.New(t.Context(), config)
		must(t, err)
		r.bridges = append(r.bridges, bridge)
		_, err = r.execute(i, "HELLO", protocol)
		must(t, err)
		result := r.call(i, "ROOT", map[string]any{})
		if result.Code != "OK" || len(result.Capabilities) != 2 {
			t.Fatalf("ROOT: %+v", result)
		}
		r.domains = append(r.domains, result.Capabilities[1])
		r.roots = append(r.roots, result.Capabilities[0])
	}
}
func split(s string) []string {
	out := []string{}
	start := 0
	for i, c := range s {
		if c == '/' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
func (r *rig) execute(i int, op string, payload any) (json.RawMessage, error) {
	r.t.Helper()
	r.cells[i]++
	raw, err := canonical(payload)
	if err != nil {
		return nil, err
	}
	protocol := guestbridge.ViewProtocol()
	command := guestbridge.Command{Cell: r.cells[i], Context: r.scopes[i], Protocol: &protocol, Op: op, Payload: raw}
	var response []byte
	if r.transport != nil {
		if transportControl(command) {
			command.Reclaim = r.transportReclaim[i]
		}
		r.transportWrite(i, command)
		response = r.transportRead(i, command)
		if transportControl(command) {
			r.transportReclaim[i] = &guestbridge.Ack{Cell: command.Cell, ResponseHash: hash(response)}
		}
	} else {
		response, err = r.bridges[i].Execute(r.t.Context(), r.natives[i], command)
	}
	if err != nil {
		return nil, err
	}
	var envelope struct{ Payload json.RawMessage }
	err = json.Unmarshal(response, &envelope)
	return envelope.Payload, err
}
func (r *rig) call(i int, op string, args any) result {
	r.t.Helper()
	r.next[i]++
	raw, err := canonical(args)
	must(r.t, err)
	intent := guestbridge.Intent{Opcode: op, Args: raw}
	claim := guestbridge.Claim{Realm: uid(100).String(), Subject: "616263", Anchor: uid(byte(101 + i)).String(), Nonce: uid(byte(103 + i)).String(), Number: r.next[i], OriginalHash: digest(intent)}
	must(r.t, r.providers[i].Allocate(r.t.Context(), r.scopes[i], claim))
	return r.sendCall(i, guestbridge.Call{Claim: claim, Credentials: r.credentials[i], Intent: intent})
}
func (r *rig) sendCall(i int, call guestbridge.Call) result {
	r.t.Helper()
	_, err := r.execute(i, "CALL", call)
	must(r.t, err)
	raw, err := r.execute(i, "QUERY", map[string]any{"claim": call.Claim})
	must(r.t, err)
	var query struct {
		Status string
		Result result
	}
	must(r.t, json.Unmarshal(raw, &query))
	if query.Status != "TERMINAL" {
		r.t.Fatalf("nonterminal call %s", raw)
	}
	if string(query.Result.Publication) != "null" {
		r.t.Fatalf("fabricated publication %s", raw)
	}
	r.facts.mu.Lock()
	for _, cap := range query.Result.Capabilities {
		r.facts.caps[cap.Token] = cap
	}
	r.facts.mu.Unlock()
	return query.Result
}
func (r *rig) ticket(i int) guestbridge.Ticket {
	r.t.Helper()
	r.tickets[i]++
	return guestbridge.Ticket{Anchor: uid(110).String(), Nonce: uid(111).String(), Number: r.tickets[i], Credentials: r.credentials[i]}
}
func (r *rig) view(i int, op string, payload any) viewReply {
	r.t.Helper()
	if controlTicket(op) {
		object := payload.(map[string]any)
		ticket := object["request"].(guestbridge.Ticket)
		// Tests construct acquisition request placeholders; actual controls use the
		// independently enrolled control allocator and never consume that counter.
		if ticket.Nonce == uid(111).String() {
			r.tickets[i]--
			r.controlTickets[i]++
			ticket.Nonce = uid(112).String()
			ticket.Number = r.controlTickets[i]
			object["request"] = ticket
		}
	}
	raw, err := r.execute(i, op, payload)
	must(r.t, err)
	var reply viewReply
	must(r.t, json.Unmarshal(raw, &reply))
	return reply
}
func (r *rig) create(i int, name string, appendMode bool) (guestbridge.Capability, guestbridge.Capability) {
	r.t.Helper()
	root := r.call(i, "ROOT", map[string]any{}).Capabilities[0]
	created := r.call(i, "CREATE", map[string]any{"parent": root, "name": hex.EncodeToString([]byte(name)), "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": appendMode, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	if created.Code != "OK" {
		r.t.Fatalf("create %+v", created)
	}
	return created.Capabilities[0], created.Capabilities[1]
}

func page(description guestbridge.Capability, offset uint64) guestbridge.ViewAccess {
	length := uint64(4096)
	return guestbridge.ViewAccess{Kind: "PAGE", Description: &description, Offset: &offset, Length: &length}
}
func (r *rig) acquire(i int, access guestbridge.ViewAccess, mode string) (guestbridge.Ticket, viewReply) {
	r.t.Helper()
	ticket := r.ticket(i)
	return ticket, r.view(i, "VIEW_ACQUIRE", map[string]any{"request": ticket, "domain": r.domains[i], "access": access, "mode": mode, "target": nil})
}
func (r *rig) open(i int, name string) guestbridge.Capability {
	r.t.Helper()
	root := r.call(i, "ROOT", map[string]any{}).Capabilities[0]
	object := r.call(i, "LOOKUP", map[string]any{"parent": root, "name": hex.EncodeToString([]byte(name))}).Capabilities[0]
	opened := r.call(i, "OPEN", map[string]any{"object": object, "flags": map[string]any{"access": "READWRITE", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}})
	if opened.Code != "OK" {
		r.t.Fatalf("OPEN %+v", opened)
	}
	return opened.Capabilities[0]
}

// ack sends one VIEW_ACK built from the current recall event, requires
// ACCEPTED and returns the exact canonical payload (so a test can prove that a
// guest resend is byte-identical).
func (r *rig) ack(i int, phase string, manifest json.RawMessage) json.RawMessage {
	r.t.Helper()
	raw := r.ackPayload(i, phase, manifest)
	reply, err := r.execute(i, "VIEW_ACK", json.RawMessage(raw))
	must(r.t, err)
	if status(reply) != "ACCEPTED" {
		r.t.Fatalf("%s ACK %s", phase, reply)
	}
	return raw
}

// ackPayload builds domain i's exact VIEW_ACK for phase from its current
// recall event and records the physical fact the primitive Facts require.
func (r *rig) ackPayload(i int, phase string, manifest json.RawMessage) json.RawMessage {
	r.t.Helper()
	r.controller.mu.Lock()
	event := bytes.Clone(r.controller.state.Recall.Events[r.scopes[i].Session])
	r.controller.mu.Unlock()
	var e map[string]json.RawMessage
	must(r.t, json.Unmarshal(event, &e))
	payload := map[string]any{"domain": r.domains[i], "sequence": e["sequence"], "recall": e["recall"], "phase": phase, "authority_hash": e["authority_hash"], "closure_hash": e["closure_hash"], "inventory_hash": e["inventory_hash"], "session": r.scopes[i].Session, "manifest": nil, "target": nil}
	if phase == "STOPPED" || phase == "DRAINED" {
		payload["manifest"] = manifest
	}
	if phase != "STOPPED" {
		payload["target"] = e["target"]
	}
	raw, err := canonical(payload)
	must(r.t, err)
	r.facts.mu.Lock()
	r.facts.physical["VIEW_ACK"+hash(raw)] = true
	r.facts.mu.Unlock()
	return raw
}
func (r *rig) emptyManifest() json.RawMessage {
	raw, _ := canonical(map[string]any{"entries": []any{}, "hash": digest([]any{})})
	return raw
}
func (r *rig) poll(i int) json.RawMessage {
	r.t.Helper()
	raw, err := r.execute(i, "VIEW_POLL", map[string]any{"domain": r.domains[i], "after_sequence": 0})
	must(r.t, err)
	return raw
}
func (r *rig) completeRecall(manifests []json.RawMessage) {
	r.t.Helper()
	r.poll(0)
	for i := range 2 {
		r.ack(i, "INSTALLED_DRAIN", nil)
	}
	for i := range 2 {
		r.ack(i, "DRAINED", manifests[i])
	}
	for i := range 2 {
		r.ack(i, "INSTALLED_FINAL", nil)
	}
	for i := range 2 {
		r.ack(i, "REOPENED", nil)
	}
	if r.controller.state.Recall != nil {
		r.t.Fatal("recall retained after exact reopen")
	}
}
func (r *rig) reopen() {
	r.t.Helper()
	r.controller.mu.Lock()
	raw, err := r.controller.rawState()
	r.controller.mu.Unlock()
	must(r.t, err)
	r.facts.mu.Lock()
	r.facts.recovery = hash(raw)
	r.facts.mu.Unlock()
	must(r.t, r.controller.Close())
	r.controller, err = New(r.t.Context(), r.cc)
	must(r.t, err)
}

func TestRealLocalTwoDomainFreezeDrainAppendAndRecall(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "append-mmap", true)
	written := r.call(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": "61626364", "max_end": 64})
	if written.Code != "OK" || written.Count != 4 {
		t.Fatalf("ordinary write %+v", written)
	}
	otherDescription := r.open(1, "append-mmap")
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	if dirty.Status != "GRANTED" {
		t.Fatalf("dirty %+v", dirty)
	}
	// A disjoint page in the SAME domain remains admissible and preserves the
	// first dirty grant; it cannot be mistaken for an origin-based exclusion.
	_, disjoint := r.acquire(0, page(description, 4096), "EXCLUSIVE_DIRTY")
	if disjoint.Status != "GRANTED" || !r.controller.state.Leases[dirty.Lease.ID].Active {
		t.Fatal("same-domain disjoint page lost prior grant")
	}
	r.next[0]++
	number := guestbridge.ClaimNumber{Realm: uid(100).String(), Subject: "616263", Anchor: uid(101).String(), Nonce: uid(103).String(), Number: r.next[0]}
	reserved := r.view(0, "DIRTY_RESERVE", map[string]any{"request": r.ticket(0), "lease": dirty.Lease, "description": description, "offset": 0, "length": 4096, "claim_number": number})
	if reserved.Status != "GRANTED" || reserved.Reservation == nil {
		t.Fatalf("reserve %+v", reserved)
	}
	pendingTicket, pending := r.acquire(1, page(otherDescription, 0), "READ")
	if pending.Status != "PENDING" || pending.Lease != nil {
		t.Fatalf("conflict granted %+v", pending)
	}
	recall := r.controller.state.Recall.ID
	reservation := *reserved.Reservation
	claim := guestbridge.Claim{Realm: number.Realm, Subject: number.Subject, Anchor: number.Anchor, Nonce: number.Nonce, Number: number.Number}
	entry := map[string]any{"description": description, "offset": 0, "length": 3, "payload_hash": hash([]byte("XYZ")), "reservation": reservation.ID}
	entry["id"] = digest(entry)
	args, _ := canonical(map[string]any{"description": description, "offset": 0, "data": "58595a", "recall": recall, "dirty_entry": entry["id"], "reservation": reservation.ID})
	intent := guestbridge.Intent{Opcode: "DRAIN_WRITE", Args: args}
	claim.OriginalHash = digest(intent)
	frozenReply := r.view(0, "DIRTY_FREEZE", map[string]any{"request": r.ticket(0), "reservation": reservation, "recall": recall, "data": "58595a", "claim": claim})
	if frozenReply.Status != "FROZEN" {
		t.Fatalf("freeze %+v", frozenReply)
	}
	manifest, _ := canonical(map[string]any{"entries": []json.RawMessage{frozenReply.Entry}, "hash": digest([]json.RawMessage{frozenReply.Entry})})
	r.ack(0, "STOPPED", manifest)
	r.ack(1, "STOPPED", r.emptyManifest())
	if r.controller.state.Recall.Phase != "DRAIN" {
		t.Fatal("not ready for reserved drains")
	}
	drained := r.sendCall(0, guestbridge.Call{Claim: claim, Credentials: r.credentials[0], Intent: intent})
	if drained.Code != "OK" || drained.Count != 3 {
		t.Fatalf("drain %+v", drained)
	}
	// The real O_APPEND handle must overwrite byte0 for mmap DRAIN_WRITE.
	r.completeRecall([]json.RawMessage{manifest, r.emptyManifest()})
	query := r.view(1, "VIEW_QUERY", map[string]any{"request": pendingTicket})
	if query.Status != "GRANTED" || query.Lease.Fence != 2 {
		t.Fatalf("pending did not receive new fence %+v", query)
	}
	read := r.call(1, "READ", map[string]any{"description": otherDescription, "offset": 0, "count": 64})
	if read.Code != "OK" || read.Data != "58595a64" {
		t.Fatalf("positional mmap drain appended %+v", read)
	}
	// Exact original admission still replays PENDING even after QUERY grants.
	replay := r.view(1, "VIEW_ACQUIRE", map[string]any{"request": pendingTicket, "domain": r.domains[1], "access": page(otherDescription, 0), "mode": "READ", "target": nil})
	if replay.Status != "PENDING" {
		t.Fatalf("mutated original reply %+v", replay)
	}
	// Actual source archive/state survive close/reopen; bridge objects continue
	// to name the same enrolled endpoint and closed old controller, so use direct
	// controller APIs only for this component recovery check.
	r.reopen()
	raw, _ := canonical(map[string]any{"request": pendingTicket})
	result, err := r.controller.ExchangeView(t.Context(), r.scopes[1], "VIEW_QUERY", raw)
	must(t, err)
	if !bytes.Contains(result, []byte(`"status":"GRANTED"`)) {
		t.Fatalf("lost durable grant %s", result)
	}
}

func TestControllerMandatoryFactsAndSourceByteNegatives(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "sources", false)
	r.call(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": "616263", "max_end": 4096})
	_, grant := r.acquire(0, page(description, 0), "READ")
	if grant.Status != "GRANTED" {
		t.Fatalf("grant %+v", grant)
	}
	must(t, r.controller.CheckTarget(t.Context(), r.scopes[0], grant.Lease.Target))
	forged := grant.Lease.Target
	copyCut := *forged.Cut
	forged.Cut = &copyCut
	forged.Cut.StateHash = hash([]byte("caller invented state"))
	if err := r.controller.CheckTarget(t.Context(), r.scopes[0], forged); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("invented source accepted %v", err)
	}
	r.facts.mu.Lock()
	r.facts.deny = true
	r.facts.mu.Unlock()
	if err := r.controller.CheckTarget(t.Context(), r.scopes[0], grant.Lease.Target); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("missing live facts accepted %v", err)
	}
	r.facts.mu.Lock()
	r.facts.deny = false
	r.facts.mu.Unlock()
	// Frozen source is a complete catalog plus exact payload bytes, not only an
	// arbitrary caller hash. Corrupting its retained bytes breaks the old target.
	r.controller.mu.Lock()
	for key, source := range r.controller.state.Sources {
		if bytes.Contains(source.State, []byte("abc")) {
			source.State = bytes.Replace(source.State, []byte("abc"), []byte("bad"), 1)
			r.controller.state.Sources[key] = source
		}
	}
	r.controller.mu.Unlock()
	if err := r.controller.CheckTarget(t.Context(), r.scopes[0], grant.Lease.Target); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("changed source bytes accepted %v", err)
	}
}

type ledgerStateForPrimitive struct {
	Tickets map[string]json.RawMessage
	Sources map[string]json.RawMessage
}

func (r *rig) physical(op string, payload any) {
	r.t.Helper()
	raw, err := canonical(payload)
	must(r.t, err)
	r.facts.mu.Lock()
	r.facts.physical[op+hash(raw)] = true
	r.facts.mu.Unlock()
}
func (r *rig) release(i int, lease *guestbridge.ViewLease) viewReply {
	r.t.Helper()
	ticket := r.controlTicket(i)
	payload := map[string]any{"request": ticket, "lease": lease}
	r.physical("VIEW_RELEASE", payload)
	reply := r.view(i, "VIEW_RELEASE", payload)
	if reply.Status == "PENDING" {
		r.controller.mu.Lock()
		clean := true
		for _, reservation := range r.controller.state.Reservations {
			if reservation.Status != "CLOSED" {
				clean = false
			}
		}
		r.controller.mu.Unlock()
		if clean {
			r.completeCleanRecall()
			reply = r.view(i, "VIEW_QUERY", map[string]any{"request": ticket})
		}
	}
	return reply
}
func (r *rig) retire(i int, through uint64, hashes []string) json.RawMessage {
	r.t.Helper()
	payload := map[string]any{"anchor": uid(110).String(), "nonce": uid(111).String(), "credentials": r.credentials[i], "through": through, "result_hashes": hashes}
	r.physical("VIEW_RETIRE", payload)
	raw, err := r.execute(i, "VIEW_RETIRE", payload)
	must(r.t, err)
	return raw
}
func (r *rig) reserve(i int, lease *guestbridge.ViewLease, description guestbridge.Capability) (guestbridge.Ticket, map[string]any) {
	r.t.Helper()
	r.next[i]++
	n := guestbridge.ClaimNumber{Realm: uid(100).String(), Subject: "616263", Anchor: uid(byte(101 + i)).String(), Nonce: uid(byte(103 + i)).String(), Number: r.next[i]}
	ticket := r.ticket(i)
	return ticket, map[string]any{"request": ticket, "lease": lease, "description": description, "offset": *lease.Access.Offset, "length": 4096, "claim_number": n}
}
func freezeRequest(ticket guestbridge.Ticket, reservation guestbridge.DirtyReservation, recall, data string) map[string]any {
	payload, _ := hex.DecodeString(data)
	n := reservation.ClaimNumber
	claim := guestbridge.Claim{Realm: n.Realm, Subject: n.Subject, Anchor: n.Anchor, Nonce: n.Nonce, Number: n.Number}
	entry := map[string]any{"description": reservation.Description, "offset": reservation.Offset, "length": len(payload), "payload_hash": hash(payload), "reservation": reservation.ID}
	entry["id"] = digest(entry)
	args, _ := canonical(map[string]any{"description": reservation.Description, "offset": reservation.Offset, "data": data, "recall": recall, "dirty_entry": entry["id"], "reservation": reservation.ID})
	claim.OriginalHash = digest(guestbridge.Intent{Opcode: "DRAIN_WRITE", Args: args})
	return map[string]any{"request": ticket, "reservation": reservation, "recall": recall, "data": data, "claim": claim}
}

func TestDurableRenewReleaseRetirementCompactsOnlyClosedSources(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "floor", false)
	ticket, first := r.acquire(0, page(description, 0), "READ")
	renewed := r.view(0, "VIEW_RENEW", map[string]any{"request": r.ticket(0), "lease": first.Lease})
	if renewed.Status != "GRANTED" || renewed.Lease.Revision != 2 {
		t.Fatalf("renew %+v", renewed)
	}
	blocked := r.retire(0, 2, []string{first.ID, renewed.ID})
	if !bytes.Contains(blocked, []byte(`"status":"BLOCKED"`)) {
		t.Fatalf("retired live lease %s", blocked)
	}
	released := r.release(0, renewed.Lease)
	if released.Status != "RELEASED" {
		t.Fatalf("release %+v", released)
	}
	r.retireControl(0, 1, []string{released.ID})
	receipt := r.retire(0, 2, []string{first.ID, renewed.ID})
	if !bytes.Contains(receipt, []byte(`"status":"RETIRED"`)) {
		t.Fatalf("floor %s", receipt)
	}
	if len(r.controller.state.Sources) != 0 || len(r.controller.state.Leases) != 0 {
		t.Fatal("closed source pins were not retired")
	}
	// Task q64: the retired rows are recycled with their identities; the
	// family floor alone keeps the old numbers retired.
	key, _ := ticketKeys(r.scopes[0], ticket)
	if _, kept := r.controller.state.Tickets[key]; kept || r.controller.state.Identities != 0 || len(r.controller.state.Tickets) != 0 {
		t.Fatalf("retired rows kept: %d identities, %d rows", r.controller.state.Identities, len(r.controller.state.Tickets))
	}
	r.requireRetiredRefused(ticket)
	if replay := r.retire(0, 2, []string{first.ID, renewed.ID}); !bytes.Equal(receipt, replay) {
		t.Fatal("changed latest protected floor replay")
	}
	r.requireNewerFloor(ticket, description, first, renewed)
}

// requireNewerFloor runs the next lease of the same page past its own
// retirement: it must get a new lease identity and fence, only the latest
// receipt is kept, and the reopened ledger keeps all of that.
func (r *rig) requireNewerFloor(ticket guestbridge.Ticket, description guestbridge.Capability, first, renewed viewReply) {
	r.t.Helper()
	_, next := r.acquire(0, page(description, 0), "READ")
	if next.Lease.ID == first.Lease.ID || next.Lease.Fence <= renewed.Lease.Fence {
		r.t.Fatal("retirement reused old lease lifetime identity")
	}
	closed := r.release(0, next.Lease)
	r.retireControl(0, 2, []string{closed.ID})
	latest := r.retire(0, 3, []string{next.ID})
	// The guest never resends a VIEW_RETIRE once a newer one of the family
	// was answered, so only the latest receipt is kept; the older range is
	// refused as Stale, never retired again or answered with another floor.
	older := map[string]any{"anchor": uid(110).String(), "nonce": uid(111).String(), "credentials": r.credentials[0], "through": 2, "result_hashes": []string{first.ID, renewed.ID}}
	r.physical("VIEW_RETIRE", older)
	if reply, err := r.controller.ExchangeView(r.t.Context(), r.scopes[0], "VIEW_RETIRE", mustRaw(r.t, older)); !errors.Is(err, fsapi.Stale) {
		r.t.Fatalf("recycled retirement receipt: %s %v, want Stale", reply, err)
	}
	// The reopened controller is reached directly: bridges keep pointing at
	// their closed controller (there is no bridge restore here).
	r.reopen()
	r.requireRetiredRefused(ticket)
	raw := mustRaw(r.t, map[string]any{"anchor": uid(110).String(), "nonce": uid(111).String(), "credentials": r.credentials[0], "through": 3, "result_hashes": []string{next.ID}})
	replay, err := r.controller.ExchangeView(r.t.Context(), r.scopes[0], "VIEW_RETIRE", raw)
	must(r.t, err)
	if !bytes.Equal(latest, replay) {
		r.t.Fatal("reopen changed the latest protected floor replay")
	}
}

// requireRetiredRefused requires QUERY and CANCEL of a retired, recycled
// ticket to be refused as Stale: the row with the original operation and
// hash is gone, and the number is never admitted again.
func (r *rig) requireRetiredRefused(ticket guestbridge.Ticket) {
	r.t.Helper()
	raw, _ := canonical(map[string]any{"request": ticket})
	for _, op := range []string{"VIEW_QUERY", "VIEW_CANCEL"} {
		if reply, err := r.controller.ExchangeView(r.t.Context(), r.scopes[0], op, raw); !errors.Is(err, fsapi.Stale) {
			r.t.Fatalf("%s of a recycled ticket: %s %v", op, reply, err)
		}
	}
}

func TestUnknownEarmarkGapCancellationAndRenewalLineage(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "unbound", false)
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	reserveTicket, payload := r.reserve(0, dirty.Lease, description)
	reserved := r.view(0, "DIRTY_RESERVE", payload)
	if reserved.Status != "GRANTED" {
		t.Fatalf("reserve %+v", reserved)
	}
	renewed := r.view(0, "VIEW_RENEW", map[string]any{"request": r.ticket(0), "lease": dirty.Lease})
	release := r.release(0, renewed.Lease)
	if release.Status != "PENDING" {
		t.Fatal("renewal lost original reservation lineage")
	}
	raw, _ := canonical(payload)
	r.physical("VIEW_CANCEL", json.RawMessage(raw))
	cancelled := r.view(0, "VIEW_CANCEL", map[string]any{"request": reserveTicket})
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("cancel %+v", cancelled)
	}
	e := reserved.Reservation
	if err := r.providers[0].EarmarkNumber(t.Context(), controlledvalidation.Earmark{Scope: e.Scope, Number: e.ClaimNumber, Credentials: e.Credentials, Description: e.Description, Reservation: e.ID, Offset: e.Offset, Length: e.Length}); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("closed earmark promised live credit %v", err)
	}
	initial := r.view(0, "DIRTY_RESERVE", payload)
	if initial.Status != "GRANTED" || initial.Reservation.ID != e.ID {
		t.Fatal("cancellation mutated original admission reply")
	}
	queried := r.view(0, "VIEW_QUERY", map[string]any{"request": reserveTicket})
	if queried.Status != "CANCELLED" {
		t.Fatal("query lost cancellation")
	}
	wrong := r.scopes[0]
	wrong.GuestBoot = uid(255).String()
	request, _ := canonical(map[string]any{"request": reserveTicket})
	if _, err := r.controller.ExchangeView(t.Context(), wrong, "VIEW_QUERY", request); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("wrong scope exposed ticket %v", err)
	}
	changed := reserveTicket
	changed.Credentials.Revision++
	request, _ = canonical(map[string]any{"request": changed})
	if _, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_QUERY", request); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("changed credentials adopted ticket %v", err)
	}
}

func TestCrossLedgerReservationAndFrozenCrashReconciliation(t *testing.T) {
	r := newRig(t)
	_, description := r.create(0, "crash", false)
	_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
	_, reservePayload := r.reserve(0, dirty.Lease, description)
	crash := errors.New("simulated component crash after independent durable barrier")
	r.controller.config.Failpoint = func(point string) error {
		if point == "earmark-protected" {
			return crash
		}
		return nil
	}
	raw, _ := canonical(reservePayload)
	if _, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_RESERVE", raw); !errors.Is(err, crash) {
		t.Fatalf("missing reservation crash %v", err)
	}
	if len(r.controller.state.Reservations) != 1 {
		t.Fatal("lost retained reserving stage")
	}
	r.reopen()
	protected, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_RESERVE", raw)
	must(t, err)
	var reservation viewReply
	must(t, json.Unmarshal(protected, &reservation))
	if reservation.Status != "GRANTED" {
		t.Fatalf("reserve recovery %s", protected)
	}
	// Exact intent changes are conflicts even if caller rebuilds valid hashes.
	altered := map[string]any{}
	for k, v := range reservePayload {
		altered[k] = v
	}
	number := altered["claim_number"].(guestbridge.ClaimNumber)
	number.Number++
	altered["claim_number"] = number
	changed, _ := canonical(altered)
	if _, err = r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_RESERVE", changed); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("changed reservation retry accepted %v", err)
	}
	acquire, _ := canonical(map[string]any{"request": r.ticket(0), "domain": r.domains[0], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil})
	pending, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "VIEW_ACQUIRE", acquire)
	must(t, err)
	if status(pending) != "PENDING" {
		t.Fatalf("own dirty overlay did not recall %s", pending)
	}
	freezePayload := freezeRequest(r.controlTicket(0), *reservation.Reservation, r.controller.state.Recall.ID, "616263")
	frozenRaw, _ := canonical(freezePayload)
	r.controller.config.Failpoint = func(point string) error {
		if point == "frozen-claim-protected" {
			return crash
		}
		return nil
	}
	if _, err = r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_FREEZE", frozenRaw); !errors.Is(err, crash) {
		t.Fatalf("missing freeze crash %v", err)
	}
	for _, state := range r.controller.state.Reservations {
		if state.Status != "FROZEN_PENDING" {
			t.Fatal("freeze reply preceded both durable ledgers")
		}
	}
	r.reopen()
	frozenReply, err := r.controller.ExchangeView(t.Context(), r.scopes[0], "DIRTY_FREEZE", frozenRaw)
	must(t, err)
	if status(frozenReply) != "FROZEN" {
		t.Fatalf("freeze recovery %s", frozenReply)
	}
	var request request
	must(t, json.Unmarshal(frozenRaw, &request))
	callEntry, call, err := frozen(request)
	must(t, err)
	if len(callEntry) == 0 {
		t.Fatal("missing frozen exact bytes")
	}
	binding := guestbridge.Binding{Context: r.scopes[0], Call: call}
	binding.Canonical, _ = canonical(call)
	must(t, r.controller.ValidateDrain(t.Context(), binding))
	changedCall := call
	changedCall.Intent.Args = bytes.Replace(call.Intent.Args, []byte("616263"), []byte("616264"), 1)
	binding.Call = changedCall
	binding.Canonical, _ = canonical(changedCall)
	if err = r.controller.ValidateDrain(t.Context(), binding); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("changed actual frozen byte accepted %v", err)
	}
	if _, err = r.providers[0].Cancel(t.Context(), r.scopes[0], call.Claim); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("ordinary CANCEL erased dirty obligation %v", err)
	}
}

func (r *rig) controlTicket(i int) guestbridge.Ticket {
	r.controlTickets[i]++
	return guestbridge.Ticket{Anchor: uid(110).String(), Nonce: uid(112).String(), Number: r.controlTickets[i], Credentials: r.credentials[i]}
}
func (r *rig) retireControl(i int, through uint64, hashes []string) json.RawMessage {
	r.t.Helper()
	payload := map[string]any{"anchor": uid(110).String(), "nonce": uid(112).String(), "credentials": r.credentials[i], "through": through, "result_hashes": hashes}
	r.physical("VIEW_RETIRE", payload)
	raw, err := r.execute(i, "VIEW_RETIRE", payload)
	must(r.t, err)
	return raw
}

func (p *primitiveFacts) CheckUnadopted(_ context.Context, statement Unadopted) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	actual, err := canonical(statement)
	if err != nil {
		return err
	}
	expected := p.exclusions[digest(statement)]
	if p.deny {
		return fsapi.Unavailable
	}
	if len(expected) == 0 || !bytes.Equal(expected, actual) {
		return ErrNotObserved // exclusion proof not observed yet
	}
	return nil
}
