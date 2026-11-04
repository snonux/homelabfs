package guestbridge

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TerminalSeal is inert evidence of an authoritative owner's durable scope seal.
// Only the bound coordinator reads it from the corresponding actual owner.
type TerminalSeal struct {
	Version          string   `json:"version"`
	Owner            string   `json:"owner"`
	Scope            Context  `json:"scope"`
	Endpoint         Endpoint `json:"endpoint"`
	Controller       string   `json:"controller"`
	RetirementID     string   `json:"retirement_id"`
	GuestCommandHash string   `json:"guest_command_hash"`
	ClosureHash      string   `json:"closure_hash"`
	StateHash        string   `json:"state_hash"`
}

// TerminalOwnership is the protected, bounded retained final-cell descriptor.
// It excludes all response hashes to avoid a signature/hash cycle.
type TerminalOwnership struct {
	Version            string   `json:"version"`
	Scope              Context  `json:"scope"`
	Endpoint           Endpoint `json:"endpoint"`
	Controller         string   `json:"controller"`
	ServingGeneration  string   `json:"serving_generation"`
	RetirementID       string   `json:"retirement_id"`
	GuestCell          uint64   `json:"guest_cell"`
	NativeCell         uint64   `json:"native_cell"`
	GuestCommandHash   string   `json:"guest_command_hash"`
	NativeCommandHash  string   `json:"native_command_hash"`
	GuestThrough       uint64   `json:"guest_through"`
	ObserverRevision   uint64   `json:"observer_revision"`
	ClosureHash        string   `json:"closure_hash"`
	SettledHash        string   `json:"settled_hash"`
	ProviderSealHash   string   `json:"provider_seal_hash"`
	ControllerSealHash string   `json:"controller_seal_hash"`
	BridgeSealHash     string   `json:"bridge_seal_hash"`
	RetainedOwner      string   `json:"retained_owner"`
}

// ProtectedTerminal is inert durable evidence, never a restored live owner.
type ProtectedTerminal struct {
	Phase     string
	Request   TerminalRequest
	Ownership *TerminalOwnership
	Seals     []TerminalSeal
	// Records come only from the actual live old cells, before atomic deletion.
	// Controls parallels Donors; the normal mapping owns the funding donor.
	Donors      []MappingDonor
	Controls    []bool
	OldOrdinary int
	OldControl  int
}

const terminalNativeBytes = 32 << 10
const terminalStateBytes = 128 << 10

// terminalDeletionDelta widens the normal two-ID transaction only for the exact
// already-owned terminal cut. It permits deletions, one candidate put, and no
// additional workspace or arbitrary multi-row update.
func (t *Transport) terminalDeletionDelta(ids []uint64) bool {
	if len(ids) < 3 || len(ids) > 81 || t.workspace != nil {
		return false
	}
	c := t.cells[ids[0]]
	if c == nil || c.terminal == nil || c.terminal.Phase != "TERMINAL_PENDING" || c.mapping == nil || c.command == nil || c.command.Op != "SESSION_RETIRE" || t.terminal[c.command.Context.Session] != ids[0] {
		return false
	}
	expected := make(map[uint64]bool, len(c.terminal.Donors)+1)
	if c.mapping.Donor != nil {
		expected[c.mapping.Donor.NativeCell] = true
	}
	for _, donor := range c.terminal.Donors {
		expected[donor.NativeCell] = true
	}
	if len(expected) != len(ids)-1 {
		return false
	}
	for _, id := range ids[1:] {
		if !expected[id] || t.cells[id] != nil {
			return false
		}
		delete(expected, id)
	}
	return len(expected) == 0
}

func validateTerminalDonors(s ProtectedTransportState, c ProtectedTransportCell) error {
	t := c.Terminal
	if t == nil || c.Mapping == nil || len(t.Donors) != len(t.Request.SettledCells) || len(t.Controls) != len(t.Donors) || t.Donors == nil || t.Controls == nil || t.OldOrdinary < 0 || t.OldControl < 0 || t.OldOrdinary > 64 || t.OldControl > 16 {
		return fsapi.Stale
	}
	ordinary, control := 0, 0
	seen := make(map[uint64]bool, len(t.Donors)+1)
	if c.Mapping.Donor != nil {
		seen[c.Mapping.Donor.NativeCell] = true
		if c.Control {
			control++
		} else {
			ordinary++
		}
	} else if len(t.Donors) != 0 || !c.Control {
		return fsapi.Stale
	}
	for i, donor := range t.Donors {
		ack := t.Request.SettledCells[i]
		if donor.GuestCell != ack.Cell || donor.GuestFullHash != ack.ResponseHash || donor.NativeCell == 0 || donor.NativeCell >= c.Header.Cell || seen[donor.NativeCell] || !hash(donor.GuestCommandHash) || !hash(donor.MappingHash) || !hash(donor.NativeFullHash) {
			return fsapi.Stale
		}
		seen[donor.NativeCell] = true
		if t.Controls[i] {
			control++
		} else {
			ordinary++
		}
	}
	for _, id := range s.Cells {
		if seen[id] {
			return fsapi.Stale
		}
	}
	if ordinary != t.OldOrdinary || control != t.OldControl {
		return fsapi.Stale
	}
	raw, err := marshal(t.Donors)
	if err != nil || len(raw) > terminalNativeBytes {
		return fsapi.NoSpace
	}
	raw, err = marshal(t)
	if err != nil || len(raw) > terminalStateBytes {
		return fsapi.NoSpace
	}
	return nil
}

// TerminalAttempt is minted only by the actual private C8 worker. Its fields
// cannot be supplied through decoded wire values or a detached observation.
type TerminalAttempt struct {
	transport   *Transport
	cell        uint64
	commandHash string
	invocation  *workerInvocation
}

// TerminalCoordinator is implemented by the bound runtime Producer, which owns
// the actual observer/provider/controller/service assembly.
type TerminalCoordinator interface {
	RetireSession(context.Context, *TerminalAttempt) (TerminalResponse, error)
}

// Attempt returns an inert copy of the exact original request and scope after
// checking the still-live private worker. Copies supply no independent authority.
func (a *TerminalAttempt) Attempt(ctx context.Context) (TerminalRequest, Context, error) {
	if err := a.lock(ctx); err != nil {
		return TerminalRequest{}, Context{}, err
	}
	t := a.transport
	defer t.mu.Unlock()
	c := t.cells[a.cell]
	request := c.terminal.Request
	request.Credentials.Groups = append([]uint32{}, request.Credentials.Groups...)
	request.SettledCells = append([]Ack{}, request.SettledCells...)
	return request, c.command.Context, nil
}

// Check requires the original live transport worker and native observation.
func (a *TerminalAttempt) Check(ctx context.Context) error {
	if err := a.lock(ctx); err != nil {
		return err
	}
	a.transport.mu.Unlock()
	return nil
}

// lock returns with the transport mutex held only on success. Every access
// revalidates the exact live invocation under the owner lock before dereference.
func (a *TerminalAttempt) lock(ctx context.Context) error {
	if a == nil || a.transport == nil {
		return fsapi.Permission
	}
	worker, ok := ctx.Value(mappingWorkerKey{}).(mappingWorker)
	observed, verified := VerifiedRequest(ctx)
	t := a.transport
	if !ok || !verified || worker.transport != t || worker.cell != a.cell || worker.invocation != a.invocation || observed.Cell != a.cell || observed.CommandHash != a.commandHash || observed.Operation != "SESSION_RETIRE" || observed.Protocol == nil || *observed.Protocol != TerminalProtocol() {
		return fsapi.Permission
	}
	t.mu.Lock()
	c := t.cells[a.cell]
	if a.invocation == nil || !a.invocation.live || c == nil || c.invocation != a.invocation || c.jobPhase != "executing" || t.closed || t.failed != nil || c.terminal == nil || c.mapping == nil || !c.mapping.Admitted || c.command == nil || observed.Scope != c.command.Context || observed.Controller != t.service.controller || c.mapping.CommandHash != a.commandHash || t.terminal[observed.Scope.Session] != a.cell {
		t.mu.Unlock()
		return fsapi.Permission
	}
	return nil
}

// Seal creates only the common inert record for the actual checked owner.
// The owner must durably retain its checked snapshot before returning this record.
func (a *TerminalAttempt) Seal(ctx context.Context, owner, stateHash string) (TerminalSeal, error) {
	if !oneOf(owner, "PROVIDER", "CONTROLLER", "BRIDGE") || !hash(stateHash) {
		return TerminalSeal{}, fsapi.Invalid
	}
	if err := a.lock(ctx); err != nil {
		return TerminalSeal{}, err
	}
	t := a.transport
	defer t.mu.Unlock()
	c := t.cells[a.cell]
	request := c.terminal.Request
	return TerminalSeal{Version: "homelabfs-terminal-seal/1", Owner: owner, Scope: c.command.Context, Endpoint: t.Endpoint(), Controller: t.service.controller.String(), RetirementID: request.RetirementID, GuestCommandHash: c.mapping.GuestCommandHash, ClosureHash: request.ClosureHash, StateHash: stateHash}, nil
}

// ProtectOwnership is available only to the exact bound coordinator. It commits
// the immutable descriptor before response construction or receipt signing.
func (a *TerminalAttempt) ProtectOwnership(ctx context.Context, coordinator TerminalCoordinator, generation string, seals []TerminalSeal) (TerminalOwnership, error) {
	if err := a.lock(ctx); err != nil {
		return TerminalOwnership{}, err
	}
	t := a.transport
	defer t.mu.Unlock()
	if any(t.service.config.Verification) != any(coordinator) || !validUUID(generation) || len(seals) != 3 {
		return TerminalOwnership{}, fsapi.Permission
	}
	c := t.cells[a.cell]
	r := c.terminal.Request
	for i, owner := range []string{"PROVIDER", "CONTROLLER", "BRIDGE"} {
		s := seals[i]
		if s.Version != "homelabfs-terminal-seal/1" || s.Owner != owner || s.Scope != c.command.Context || s.Endpoint != t.Endpoint() || s.Controller != t.service.controller.String() || s.RetirementID != r.RetirementID || s.GuestCommandHash != c.mapping.GuestCommandHash || s.ClosureHash != r.ClosureHash || !hash(s.StateHash) {
			return TerminalOwnership{}, fsapi.Stale
		}
	}
	hashes := make([]string, 3)
	for i, s := range seals {
		raw, err := marshal(s)
		if err != nil {
			return TerminalOwnership{}, err
		}
		hashes[i] = digest(raw)
	}
	settledHash, err := r.SettledHash()
	if err != nil {
		return TerminalOwnership{}, err
	}
	owner := TerminalOwnership{Version: "homelabfs-terminal-owner/1", Scope: c.command.Context, Endpoint: t.Endpoint(), Controller: t.service.controller.String(), ServingGeneration: generation, RetirementID: r.RetirementID, GuestCell: c.mapping.GuestCell, NativeCell: a.cell, GuestCommandHash: c.mapping.GuestCommandHash, NativeCommandHash: a.commandHash, GuestThrough: r.GuestThrough, ObserverRevision: r.ObserverRevision, ClosureHash: r.ClosureHash, SettledHash: settledHash, ProviderSealHash: hashes[0], ControllerSealHash: hashes[1], BridgeSealHash: hashes[2], RetainedOwner: "C8_TERMINAL_CELL"}
	if c.terminal.Ownership != nil {
		raw, _ := marshal(owner)
		old, _ := marshal(c.terminal.Ownership)
		if string(raw) != string(old) {
			t.failed = fsapi.Stale
			return TerminalOwnership{}, fsapi.Stale
		}
		return owner, nil
	}
	c.terminal.Ownership = &owner
	c.terminal.Seals = append([]TerminalSeal(nil), seals...)
	c.terminal.Phase = "TERMINAL_SEALED"
	if err := t.protectLocked(a.cell); err != nil {
		return TerminalOwnership{}, err
	}
	if err := t.protectedPoint("terminal-sealed"); err != nil {
		return TerminalOwnership{}, err
	}
	return owner, nil
}

// VerifyOwnership verifies the protected live SEALED owner before signing.
func (a *TerminalAttempt) VerifyOwnership(ctx context.Context, coordinator TerminalCoordinator, owner TerminalOwnership) error {
	if err := a.lock(ctx); err != nil {
		return err
	}
	t := a.transport
	defer t.mu.Unlock()
	c := t.cells[a.cell]
	if any(t.service.config.Verification) != any(coordinator) || c.terminal.Ownership == nil || !oneOf(c.terminal.Phase, "TERMINAL_SEALED", "TERMINAL_RESPONDED") {
		return fsapi.Permission
	}
	raw, _ := marshal(owner)
	protected, _ := marshal(c.terminal.Ownership)
	if string(raw) != string(protected) {
		return fsapi.Stale
	}
	return nil
}

func (t *Transport) prepareTerminal(c *cell, command Command, mapping *ProtectedMapping) (*ProtectedTerminal, error) {
	if command.Op != "SESSION_RETIRE" {
		return nil, nil
	}
	if command.Protocol == nil || *command.Protocol != TerminalProtocol() || mapping == nil || mapping.PollIndex != 0 || t.ledger == nil || t.terminal[command.Context.Session] != 0 {
		return nil, fsapi.Permission
	}
	request, err := DecodeTerminalRequest(command.Payload)
	if err != nil {
		return nil, err
	}
	if request.GuestThrough != t.guestHigh[command.Context.Session] || request.GuestThrough != c.guestWatermark || mapping.GuestCell <= request.GuestThrough {
		return nil, fsapi.Stale
	}
	if err := validateSettledAcks(request.SettledCells, request.GuestThrough, mapping.GuestReclaim); err != nil {
		return nil, err
	}
	terminal := &ProtectedTerminal{Phase: "TERMINAL_PENDING", Request: request,
		Donors: make([]MappingDonor, len(request.SettledCells)), Controls: make([]bool, len(request.SettledCells))}
	indices := make(map[uint64]int, len(request.SettledCells))
	for i, ack := range request.SettledCells {
		indices[ack.Cell] = i
	}
	oldCount := 0
	for _, other := range t.cells {
		if other == c || other.frame.Session != c.frame.Session {
			continue
		}
		if other.command == nil || other.command.Context != command.Context || other.command.Protocol == nil || *other.command.Protocol != TerminalProtocol() || other.mapping == nil || other.response == nil || other.unread != 0 || other.jobPhase != "responded" || other.terminal != nil || other.mapping.NativeFullHash != digest(other.response) || !hash(other.mapping.GuestFullHash) || other.mapping.Empty && other.mapping.GuestOp == "VIEW_WAIT" {
			return nil, fsapi.WouldBlock
		}
		oldCount++
		if other.reservation.Control {
			terminal.OldControl++
		} else {
			terminal.OldOrdinary++
		}
		if command.Reclaim != nil && other.frame.Cell == command.Reclaim.Cell {
			if mapping.Donor == nil || mapping.Donor.GuestCell != other.mapping.GuestCell || command.Reclaim.ResponseHash != other.mapping.NativeFullHash {
				return nil, fsapi.Stale
			}
			continue
		}
		i, ok := indices[other.mapping.GuestCell]
		if !ok || request.SettledCells[i].ResponseHash != other.mapping.GuestFullHash {
			return nil, fsapi.Permission
		}
		terminal.Donors[i] = MappingDonor{GuestCell: other.mapping.GuestCell, NativeCell: other.frame.Cell,
			GuestCommandHash: other.mapping.GuestCommandHash, MappingHash: other.mapping.MappingHash,
			NativeFullHash: other.mapping.NativeFullHash, GuestFullHash: other.mapping.GuestFullHash}
		terminal.Controls[i] = other.reservation.Control
	}
	want := len(request.SettledCells)
	if command.Reclaim != nil {
		want++
	}
	if oldCount != want {
		return nil, fsapi.Stale
	}
	for _, donor := range terminal.Donors {
		if donor.NativeCell == 0 {
			return nil, fsapi.Stale
		}
	}
	if t.workspace != nil && t.workspace != c && t.workspace.frame.Session == c.frame.Session {
		return nil, fsapi.WouldBlock
	}
	if command.Reclaim == nil && !c.reservation.Control {
		return nil, fsapi.Permission
	}
	raw, err := marshal(terminal.Donors)
	if err != nil || len(raw) > terminalNativeBytes {
		return nil, fsapi.NoSpace
	}
	return terminal, nil
}

func (s *Service) retireSession(ctx context.Context, c Command) (TerminalResponse, error) {
	var empty TerminalResponse
	observed, ok := VerifiedRequest(ctx)
	if !ok {
		return empty, fsapi.Permission
	}
	s.mu.Lock()
	t := s.transport
	s.mu.Unlock()
	coordinator, ok := s.config.Verification.(TerminalCoordinator)
	if !ok || t == nil {
		return empty, fsapi.Permission
	}
	worker, _ := ctx.Value(mappingWorkerKey{}).(mappingWorker)
	attempt := &TerminalAttempt{transport: t, cell: c.Cell, commandHash: observed.CommandHash, invocation: worker.invocation}
	if err := attempt.Check(ctx); err != nil {
		return empty, err
	}
	var request TerminalRequest
	if err := json.Unmarshal(c.Payload, &request); err != nil {
		return empty, err
	}
	if err := s.admitCredentials(ctx, c.Context, request.Credentials, true, true); err != nil {
		return empty, err
	}
	return coordinator.RetireSession(ctx, attempt)
}

func validateTerminalCell(s ProtectedTransportState, c ProtectedTransportCell, workspace bool) error {
	session := c.Header.Session.String()
	final := s.Terminal[session]
	if c.Terminal == nil {
		if final == c.Header.Cell || c.Command != nil && c.Command.Op == "SESSION_RETIRE" {
			return fsapi.Stale
		}
		if final != 0 {
			return fsapi.Stale
		}
		return nil
	}
	if workspace || final != c.Header.Cell || c.Command == nil || c.Command.Op != "SESSION_RETIRE" || c.Command.Protocol == nil || *c.Command.Protocol != TerminalProtocol() || c.Mapping == nil || c.Mapping.PollIndex != 0 || !oneOf(c.Terminal.Phase, "TERMINAL_PENDING", "TERMINAL_SEALED", "TERMINAL_RESPONDED") {
		return fsapi.Stale
	}
	raw, err := marshal(c.Terminal.Request)
	if err != nil || string(raw) != string(c.Command.Payload) || validateSettledAcks(c.Terminal.Request.SettledCells, c.Terminal.Request.GuestThrough, c.Mapping.GuestReclaim) != nil || c.Terminal.Request.GuestThrough != c.GuestWatermark || c.Mapping.GuestCell <= c.Terminal.Request.GuestThrough || s.GuestHigh[session] != c.Mapping.GuestCell {
		return fsapi.Stale
	}
	if err := validateTerminalDonors(s, c); err != nil {
		return err
	}
	if c.Terminal.Phase == "TERMINAL_PENDING" {
		if c.Terminal.Ownership != nil || len(c.Terminal.Seals) != 0 || c.Response != nil {
			return fsapi.Stale
		}
		return nil
	}
	o := c.Terminal.Ownership
	settledHash, err := c.Terminal.Request.SettledHash()
	if err != nil || o == nil || len(c.Terminal.Seals) != 3 || o.Version != "homelabfs-terminal-owner/1" || o.Scope != c.Command.Context || o.Endpoint != s.Endpoint || o.Controller != s.Controller.String() || !validUUID(o.ServingGeneration) || o.RetirementID != c.Terminal.Request.RetirementID || o.GuestCell != c.Mapping.GuestCell || o.NativeCell != c.Header.Cell || o.GuestCommandHash != c.Mapping.GuestCommandHash || o.NativeCommandHash != c.Mapping.CommandHash || o.GuestThrough != c.Terminal.Request.GuestThrough || o.ObserverRevision != c.Terminal.Request.ObserverRevision || o.ClosureHash != c.Terminal.Request.ClosureHash || o.SettledHash != settledHash || o.RetainedOwner != "C8_TERMINAL_CELL" {
		return fsapi.Stale
	}
	for i, owner := range []string{"PROVIDER", "CONTROLLER", "BRIDGE"} {
		seal := c.Terminal.Seals[i]
		if seal.Owner != owner || seal.Version != "homelabfs-terminal-seal/1" || seal.Scope != o.Scope || seal.Endpoint != o.Endpoint || seal.Controller != o.Controller || seal.RetirementID != o.RetirementID || seal.GuestCommandHash != o.GuestCommandHash || seal.ClosureHash != o.ClosureHash || !hash(seal.StateHash) {
			return fsapi.Stale
		}
		raw, err := marshal(seal)
		if err != nil || digest(raw) != []string{o.ProviderSealHash, o.ControllerSealHash, o.BridgeSealHash}[i] {
			return fsapi.Stale
		}
	}
	if c.Terminal.Phase == "TERMINAL_RESPONDED" {
		if c.Response == nil || c.JobPhase != "responded" {
			return fsapi.Stale
		}
	} else if c.Response != nil {
		return fsapi.Stale
	}
	return nil
}

// TerminalAttempt returns a live attempt only to the actual current private
// worker. Exported disk/observation values cannot reconstruct this capability.
func (t *Transport) TerminalAttempt(ctx context.Context) (*TerminalAttempt, error) {
	observed, ok := VerifiedRequest(ctx)
	if !ok {
		return nil, fsapi.Permission
	}
	worker, _ := ctx.Value(mappingWorkerKey{}).(mappingWorker)
	a := &TerminalAttempt{transport: t, cell: observed.Cell, commandHash: observed.CommandHash, invocation: worker.invocation}
	if err := a.Check(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

// CheckSnapshot binds the observer's immutable cut to this exact original
// candidate, including its full donor and captured credential snapshot.
func (a *TerminalAttempt) CheckSnapshot(ctx context.Context, snapshot TerminalSnapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	if err := a.lock(ctx); err != nil {
		return err
	}
	t := a.transport
	defer t.mu.Unlock()
	c := t.cells[a.cell]
	r := c.terminal.Request
	raw, err := marshal(snapshot)
	if err != nil {
		return err
	}
	credentials, _ := marshal(snapshot.Credentials)
	expected, _ := marshal(r.Credentials)
	donor, _ := marshal(snapshot.Donor)
	expectedDonor, _ := marshal(c.mapping.GuestReclaim)
	settled, _ := marshal(snapshot.SettledCells)
	expectedSettled, _ := marshal(r.SettledCells)
	if digest(raw) != r.ClosureHash || snapshot.Scope != c.command.Context || snapshot.Controller != t.service.controller.String() || snapshot.RetirementID != r.RetirementID || snapshot.Revision != r.ObserverRevision || snapshot.GuestThrough != r.GuestThrough || string(credentials) != string(expected) || string(donor) != string(expectedDonor) || string(settled) != string(expectedSettled) {
		return fsapi.Stale
	}
	return nil
}

// Ownership returns only this bound coordinator's protected live descriptor.
func (a *TerminalAttempt) Ownership(ctx context.Context, coordinator TerminalCoordinator) (TerminalOwnership, error) {
	if err := a.lock(ctx); err != nil {
		return TerminalOwnership{}, err
	}
	t := a.transport
	defer t.mu.Unlock()
	c := t.cells[a.cell]
	if any(t.service.config.Verification) != any(coordinator) || c.terminal.Ownership == nil || !oneOf(c.terminal.Phase, "TERMINAL_SEALED", "TERMINAL_RESPONDED") {
		return TerminalOwnership{}, fsapi.Permission
	}
	return *c.terminal.Ownership, nil
}
