package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// MappingMaxBytes is the fixed per-cell scalar/hash allowance. It is charged
// within the existing 8MiB cell and replacement workspace, never another arena.
const MappingMaxBytes = 16 << 10
const mappingRecentLimit = 80

// MappingLeg is inert protected evidence for one actually admitted native leg.
// ResponseHash hashes the response base, avoiding signature recursion.
type MappingLeg struct {
	Index                     uint64
	Cell                      uint64
	CommandHash, ResponseHash string
}

// ProtectedMapping retains only bounded scalars/hashes; no payload is copied.
// Inspection does not reconstruct a live transport or authenticated request.
type MappingDonor struct {
	GuestCell, NativeCell                                        uint64
	GuestCommandHash, MappingHash, NativeFullHash, GuestFullHash string
}

type ProtectedMapping struct {
	Donor                                      *MappingDonor
	GuestCell                                  uint64
	GuestOp, GuestCommandHash                  string
	GuestReclaim                               *Ack
	PollIndex                                  uint64
	CommandHash                                string
	Admitted                                   bool
	Legs                                       []MappingLeg
	MappingHash, NativeBaseHash, GuestBaseHash string
	NativeFullHash, GuestFullHash              string
	Empty                                      bool
}

// GuestIssuance is a finite freshness guard pinned by older incomplete cells.
// It is not a retirement, restored authority, or proof of any filesystem effect.
type GuestIssuance struct {
	Session string
	Cell    uint64
}

// MappingObservation is detached evidence from the private authenticated worker
// context. Constructing this exported value cannot install that private context.
type MappingObservation struct {
	NativeCell                                       uint64
	NativeCommandHash, NativeResponseHash            string
	Transport                                        *Transport
	GuestCell                                        uint64
	GuestCommandHash, GuestResponseHash, MappingHash string
	PollCount                                        uint64
}
type mappingWorkerKey struct{}
type mappingWorker struct {
	transport *Transport
	cell      uint64
}
type mappingObservationKey struct{}

// VerifiedMapping returns only evidence minted after protected base completion.
func VerifiedMapping(ctx context.Context) (MappingObservation, bool) {
	o, ok := ctx.Value(mappingObservationKey{}).(MappingObservation)
	return o, ok
}

// VerificationBinding returns actual constructed objects only to this exact
// configured producer. It cannot register a candidate or authenticate a request.
func (t *Transport) VerificationBinding(producer VerificationProducer) (VerificationBinding, error) {
	if t == nil {
		return VerificationBinding{}, fsapi.Permission
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil || t.ledger == nil || t.service.config.Verification != producer {
		return VerificationBinding{}, fsapi.Permission
	}
	s := t.service
	return VerificationBinding{Endpoint: s.config.Endpoint, Controller: s.controller, Native: s.config.Native, Claims: s.config.Claims, Execution: s.config.Execution, Views: s.config.Views, ViewProtocol: s.config.ViewProtocol, Scopes: append([]Context(nil), s.config.Scopes...)}, nil
}

func (t *Transport) guestWatermark(session string) uint64 { return t.guestHigh[session] }
func (t *Transport) mappingIncomplete() int {
	n := 0
	for _, c := range t.cells {
		if c.frame.Minor == 4 && c.command == nil {
			n++
		}
	}
	if t.workspace != nil && t.workspace.frame.Minor == 4 {
		n++
	}
	return n
}
func (t *Transport) pruneGuestRecent() {
	kept := t.guestRecent[:0]
	for _, issued := range t.guestRecent {
		minimum := t.guestHigh[issued.Session]
		for _, c := range t.cells {
			if c.frame.Session.String() == issued.Session && c.command == nil && c.guestWatermark < minimum {
				minimum = c.guestWatermark
			}
		}
		if c := t.workspace; c != nil && c.frame.Session.String() == issued.Session && c.command == nil && c.guestWatermark < minimum {
			minimum = c.guestWatermark
		}
		if issued.Cell > minimum {
			kept = append(kept, issued)
		}
	}
	t.guestRecent = kept
}
func (t *Transport) prepareMapping(c *cell, command Command) (*ProtectedMapping, error) {
	if commandMinor(command) != 4 {
		return nil, nil
	}
	d := command.Delegation
	if t.ledger == nil || d == nil || command.Op == "VIEW_WAIT" {
		return nil, fsapi.Permission
	}
	original, err := inverseCommand(command)
	if err != nil {
		return nil, err
	}
	originalHash, err := hashCommand(original)
	if err != nil {
		return nil, err
	}
	m := &ProtectedMapping{GuestCell: d.GuestCell, GuestOp: d.GuestOp, GuestReclaim: copyAck(d.GuestReclaim), GuestCommandHash: originalHash, PollIndex: d.PollIndex, CommandHash: hex.EncodeToString(c.frame.Hash[:])}
	var existing *cell
	for _, other := range t.cells {
		if other != c && other.command != nil && other.command.Context == command.Context && other.mapping != nil && other.mapping.GuestCell == d.GuestCell {
			existing = other
			break
		}
	}
	if d.PollIndex > 0 {
		if existing == nil || existing.mapping == nil || command.Reclaim == nil || command.Reclaim.Cell != existing.frame.Cell {
			return nil, fsapi.Permission
		}
		old := existing.mapping
		if d.GuestOp != "VIEW_WAIT" || command.Op != "VIEW_POLL" || !old.Empty || old.GuestCommandHash != originalHash || old.PollIndex+1 != d.PollIndex || old.NativeFullHash == "" || command.Reclaim.ResponseHash != old.NativeFullHash || c.frame.Cell <= existing.frame.Cell {
			return nil, fsapi.Permission
		}
		m.Legs = append([]MappingLeg(nil), old.Legs...)
		if old.Donor != nil {
			copy := *old.Donor
			m.Donor = &copy
		}
		m.MappingHash = old.MappingHash
	} else {
		if existing != nil || d.GuestCell <= c.guestWatermark {
			return nil, fsapi.Stale
		}
		for _, issued := range t.guestRecent {
			if issued.Session == command.Context.Session && issued.Cell == d.GuestCell {
				return nil, fsapi.Stale
			}
		}
		if (d.GuestReclaim == nil) != (command.Reclaim == nil) {
			return nil, fsapi.Permission
		}
		if d.GuestReclaim != nil {
			donor := t.cells[command.Reclaim.Cell]
			if donor == nil || donor.command == nil || donor.command.Context != command.Context || donor.mapping == nil || donor.mapping.GuestCell != d.GuestReclaim.Cell || donor.mapping.GuestFullHash != d.GuestReclaim.ResponseHash || donor.mapping.NativeFullHash != command.Reclaim.ResponseHash || donor.mapping.NativeFullHash == "" {
				return nil, fsapi.Permission
			}
			m.Donor = &MappingDonor{GuestCell: donor.mapping.GuestCell, NativeCell: donor.frame.Cell, GuestCommandHash: donor.mapping.GuestCommandHash, MappingHash: donor.mapping.MappingHash, NativeFullHash: donor.mapping.NativeFullHash, GuestFullHash: donor.mapping.GuestFullHash}
		}
		m.MappingHash, err = mappingSeed(t.Endpoint(), t.service.controller, command.Context, d.GuestCell, originalHash)
		if err != nil {
			return nil, err
		}
		t.pruneGuestRecent()
		if len(t.guestRecent) >= mappingRecentLimit {
			return nil, fsapi.WouldBlock
		}
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MappingMaxBytes {
		return nil, fsapi.NoSpace
	}
	return m, nil
}
func (t *Transport) acceptMapping(c *cell, command Command, m *ProtectedMapping) {
	c.mapping = m
	if m == nil || m.PollIndex != 0 {
		return
	}
	t.guestHigh[command.Context.Session] = max(t.guestHigh[command.Context.Session], m.GuestCell)
	t.guestRecent = append(t.guestRecent, GuestIssuance{command.Context.Session, m.GuestCell})
}
func (t *Transport) admitMapping(ctx context.Context, command Command) error {
	worker, ok := ctx.Value(mappingWorkerKey{}).(mappingWorker)
	observed, authenticated := VerifiedRequest(ctx)
	if !ok || worker.transport != t || worker.cell != command.Cell || !authenticated || observed.Controller != t.service.controller || observed.Scope != command.Context {
		return fsapi.Permission
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.cells[command.Cell]
	if t.failed != nil || t.closed || c == nil || c.mapping == nil || c.mapping.CommandHash != observed.CommandHash || c.command == nil {
		return fsapi.Permission
	}
	c.mapping.Admitted = true
	if err := t.protectLocked(command.Cell); err != nil {
		return err
	}
	return t.protectedPoint("mapping-admission")
}
func (t *Transport) finishMappingBase(ctx context.Context, command Command, raw []byte) (context.Context, error) {
	worker, ok := ctx.Value(mappingWorkerKey{}).(mappingWorker)
	observed, authenticated := VerifiedRequest(ctx)
	if !ok || worker.transport != t || !authenticated || worker.cell != command.Cell {
		return ctx, fsapi.Permission
	}
	nativeHash := digest(raw)
	guestHash, err := translatedResponseHash(raw, command.Delegation.GuestCell, command.Delegation.GuestOp, false)
	if err != nil {
		return ctx, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	c := t.cells[command.Cell]
	if t.failed != nil || t.closed || c == nil || c.mapping == nil || !c.mapping.Admitted || c.mapping.CommandHash != observed.CommandHash {
		return ctx, fsapi.Permission
	}
	m := c.mapping
	if m.NativeBaseHash != "" {
		if m.NativeBaseHash != nativeHash || m.GuestBaseHash != guestHash {
			t.failed = fsapi.Stale
			return ctx, fsapi.Stale
		}
	} else {
		if len(m.Legs) != int(m.PollIndex) || len(m.Legs) >= 16 {
			return ctx, fsapi.Stale
		}
		leg := MappingLeg{Index: m.PollIndex, Cell: command.Cell, CommandHash: observed.CommandHash, ResponseHash: nativeHash}
		chain, e := mappingStep(m.MappingHash, leg)
		if e != nil {
			return ctx, e
		}
		m.Legs = append(m.Legs, leg)
		m.MappingHash = chain
		m.NativeBaseHash = nativeHash
		m.GuestBaseHash = guestHash
		fields, e := envelopeFields(raw)
		if e != nil {
			return ctx, e
		}
		m.Empty = command.Op == "VIEW_POLL" && bytes.Equal(fields["payload"], []byte(`{"event":null,"status":"EMPTY"}`))
	}
	if err = t.protectLocked(command.Cell); err != nil {
		return ctx, err
	}
	if err = t.protectedPoint("mapping-base"); err != nil {
		return ctx, err
	}
	o := MappingObservation{NativeCell: command.Cell, NativeCommandHash: observed.CommandHash, NativeResponseHash: m.NativeBaseHash, Transport: t, GuestCell: m.GuestCell, GuestCommandHash: m.GuestCommandHash, GuestResponseHash: m.GuestBaseHash, MappingHash: m.MappingHash, PollCount: m.PollIndex + 1}
	return context.WithValue(ctx, mappingObservationKey{}, o), nil
}
func (t *Transport) finishMappingFull(c *cell, raw []byte) error {
	if c.mapping == nil {
		return nil
	}
	if !c.mapping.Admitted || c.mapping.NativeBaseHash == "" {
		return fsapi.Permission
	}
	guest, err := translatedResponseHash(raw, c.mapping.GuestCell, c.mapping.GuestOp, false)
	if err != nil {
		return err
	}
	c.mapping.NativeFullHash = digest(raw)
	c.mapping.GuestFullHash = guest
	return nil
}
func inverseCommand(c Command) (Command, error) {
	d := c.Delegation
	if d == nil || d.Version != DelegationVersion || d.GuestCell < 1 || d.GuestCell > MaxCell || d.PollIndex > 15 {
		return Command{}, fsapi.Invalid
	}
	if d.GuestOp != c.Op && (d.GuestOp != "VIEW_WAIT" || c.Op != "VIEW_POLL") || d.PollIndex > 0 && d.GuestOp != "VIEW_WAIT" {
		return Command{}, fsapi.Permission
	}
	c.Cell = d.GuestCell
	c.Op = d.GuestOp
	c.Reclaim = copyAck(d.GuestReclaim)
	c.Delegation = nil
	if c.Op == "RELEASE_CELL" {
		if c.Reclaim == nil {
			return Command{}, fsapi.Invalid
		}
		raw, err := marshal(c.Reclaim)
		if err != nil {
			return Command{}, err
		}
		c.Payload = raw
	}
	return c, nil
}
func copyAck(a *Ack) *Ack {
	if a == nil {
		return nil
	}
	copy := *a
	return &copy
}
func mappingSeed(endpoint Endpoint, controller fsapi.UUID, scope Context, cell uint64, original string) (string, error) {
	raw, err := marshal(map[string]any{"domain": "homelabfs-mapping-chain/1", "endpoint": endpoint, "controller": controller.String(), "scope": scope, "guest_cell": cell, "guest_command_hash": original})
	return digest(raw), err
}
func mappingStep(previous string, leg MappingLeg) (string, error) {
	raw, err := marshal(map[string]any{"domain": "homelabfs-mapping-leg/1", "previous": previous, "index": leg.Index, "native_cell": leg.Cell, "native_command_hash": leg.CommandHash, "native_response_hash": leg.ResponseHash})
	return digest(raw), err
}
func hashCommand(c Command) (string, error) {
	fields := map[string][]byte{"cell": []byte(strconv.FormatUint(c.Cell, 10)), "payload": c.Payload}
	for key, v := range map[string]any{"context": c.Context, "delegation": c.Delegation, "op": c.Op, "protocol": c.Protocol, "reclaim": c.Reclaim} {
		raw, err := marshal(v)
		if err != nil {
			return "", err
		}
		fields[key] = raw
	}
	return hashFields(fields), nil
}
func hashFields(fields map[string][]byte) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	_, _ = h.Write([]byte("{"))
	for i, key := range keys {
		if i > 0 {
			_, _ = h.Write([]byte(","))
		}
		raw, _ := json.Marshal(key)
		_, _ = h.Write(raw)
		_, _ = h.Write([]byte(":"))
		_, _ = h.Write(fields[key])
	}
	_, _ = h.Write([]byte("}"))
	return hex.EncodeToString(h.Sum(nil))
}
func translatedResponseHash(raw []byte, cell uint64, op string, removeVerification bool) (string, error) {
	fields, err := envelopeFields(raw)
	if err != nil {
		return "", err
	}
	fields["cell"] = []byte(strconv.FormatUint(cell, 10))
	fields["op"], err = marshal(op)
	if err != nil {
		return "", err
	}
	if removeVerification {
		delete(fields, "verification")
	}
	return hashFields(fields), nil
}

// envelopeFields borrows canonical top-level values. It never duplicates the
// potentially 1MiB payload just to replace scalar routing fields for hashing.
func envelopeFields(raw []byte) (map[string][]byte, error) {
	if len(raw) < 2 || raw[0] != '{' || raw[len(raw)-1] != '}' {
		return nil, fsapi.Invalid
	}
	fields := map[string][]byte{}
	for start := 1; start < len(raw)-1; {
		if raw[start] != '"' {
			return nil, fsapi.Invalid
		}
		end := start + 1
		for end < len(raw) && raw[end] != '"' {
			if raw[end] == '\\' {
				return nil, fsapi.Invalid
			}
			end++
		}
		if end+1 >= len(raw) || raw[end+1] != ':' {
			return nil, fsapi.Invalid
		}
		key := string(raw[start+1 : end])
		value := end + 2
		cursor := value
		depth := 0
		quoted, escaped := false, false
		for cursor < len(raw)-1 {
			ch := raw[cursor]
			if quoted {
				if escaped {
					escaped = false
				} else if ch == '\\' {
					escaped = true
				} else if ch == '"' {
					quoted = false
				}
			} else {
				switch ch {
				case '"':
					quoted = true
				case '{', '[':
					depth++
				case '}', ']':
					depth--
				case ',':
					if depth == 0 {
						goto complete
					}
				}
			}
			cursor++
		}
	complete:
		if quoted || depth != 0 || cursor <= value {
			return nil, fsapi.Invalid
		}
		if _, exists := fields[key]; exists {
			return nil, fsapi.Invalid
		}
		fields[key] = raw[value:cursor]
		start = cursor + 1
	}
	return fields, nil
}

func validateProtectedMapping(c ProtectedTransportCell) error {
	if c.Header.Minor != 4 {
		if c.Mapping != nil || c.GuestWatermark != 0 {
			return fsapi.Stale
		}
		return nil
	}
	if c.Command == nil {
		if c.Mapping != nil {
			return fsapi.Stale
		}
		return nil
	}
	m := c.Mapping
	if m == nil || m.PollIndex > 15 || m.CommandHash != hex.EncodeToString(c.Header.Hash[:]) || len(m.Legs) > 16 {
		return fsapi.Stale
	}
	raw, err := json.Marshal(m)
	if err != nil || len(raw) > MappingMaxBytes {
		return fsapi.Stale
	}
	original, err := inverseCommand(*c.Command)
	if err != nil {
		return fsapi.Stale
	}
	guestHash, err := hashCommand(original)
	if err != nil || guestHash != m.GuestCommandHash || original.Cell != m.GuestCell || original.Op != m.GuestOp || !sameAck(original.Reclaim, m.GuestReclaim) || c.Command.Delegation.PollIndex != m.PollIndex {
		return fsapi.Stale
	}
	if m.Admitted && c.JobPhase == "accepted" {
		return fsapi.Stale
	}
	if m.Donor != nil && m.Donor.NativeCell >= c.Header.Cell {
		return fsapi.Stale
	}
	if original.Reclaim == nil && m.Donor != nil || original.Reclaim != nil && (m.Donor == nil || m.Donor.GuestCell != original.Reclaim.Cell || m.Donor.GuestFullHash != original.Reclaim.ResponseHash || m.Donor.NativeCell < 1 || !validDigest(m.Donor.NativeFullHash) || !validDigest(m.Donor.MappingHash) || !validDigest(m.Donor.GuestCommandHash)) {
		return fsapi.Stale
	}
	// The receipt source/controller is checked by the index binding below. A leg
	// chain is recomputed there because the controller belongs to that index.
	expected := int(m.PollIndex)
	if m.NativeBaseHash != "" {
		expected++
	}
	if len(m.Legs) != expected || m.NativeBaseHash == "" && (m.GuestBaseHash != "" || m.NativeFullHash != "" || m.GuestFullHash != "" || m.Empty) || m.NativeBaseHash != "" && (!m.Admitted || !validDigest(m.NativeBaseHash) || !validDigest(m.GuestBaseHash)) {
		return fsapi.Stale
	}
	var previous uint64
	for i, leg := range m.Legs {
		if leg.Index != uint64(i) || leg.Cell <= previous || !validDigest(leg.CommandHash) || !validDigest(leg.ResponseHash) {
			return fsapi.Stale
		}
		previous = leg.Cell
	}
	if m.NativeBaseHash != "" {
		last := m.Legs[len(m.Legs)-1]
		if last.Cell != c.Header.Cell || last.CommandHash != m.CommandHash || last.ResponseHash != m.NativeBaseHash {
			return fsapi.Stale
		}
	}
	if c.Response != nil {
		fields, e := envelopeFields(c.Response)
		if e != nil {
			return fsapi.Stale
		}
		delete(fields, "verification")
		if hashFields(fields) != m.NativeBaseHash || digest(c.Response) != m.NativeFullHash {
			return fsapi.Stale
		}
		guest, e := translatedResponseHash(c.Response, m.GuestCell, m.GuestOp, false)
		if e != nil || guest != m.GuestFullHash {
			return fsapi.Stale
		}
		guest, e = translatedResponseHash(c.Response, m.GuestCell, m.GuestOp, true)
		if e != nil || guest != m.GuestBaseHash {
			return fsapi.Stale
		}
		empty := c.Command.Op == "VIEW_POLL" && bytes.Equal(fields["payload"], []byte(`{"event":null,"status":"EMPTY"}`))
		if empty != m.Empty {
			return fsapi.Stale
		}
	} else if m.NativeFullHash != "" || m.GuestFullHash != "" {
		return fsapi.Stale
	}
	return nil
}
func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func sameAck(a, b *Ack) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func validateMappingChain(s ProtectedTransportState, c ProtectedTransportCell) error {
	m := c.Mapping
	if m == nil {
		return nil
	}
	chain, err := mappingSeed(s.Endpoint, s.Controller, c.Command.Context, m.GuestCell, m.GuestCommandHash)
	if err != nil {
		return err
	}
	for _, leg := range m.Legs {
		chain, err = mappingStep(chain, leg)
		if err != nil {
			return err
		}
	}
	if chain != m.MappingHash {
		return fsapi.Stale
	}
	return nil
}

// mappedCellFunding is a conservative bound on a maximally occupied row:
// command RawMessage <=1MiB; four base64 arenas (body, response, framed wire,
// byte-coverage), each <=1MiB+32 frame headers; 33 full 1024-group native
// observations, each <=16KiB; 32KiB scalar/frame/bitmap JSON; and MappingMaxBytes.
// SetEscapeHTML(false) is essential for the RawMessage term. Arrays/bytes use
// their bounded base64 representation. Encoding owns one buffer, not a second
// 8MiB clone; existing ledger transaction ownership funds its bounded copies.
func mappedCellFunding() error {
	const base64Arena = ((BodyBytes+32*HeaderBytes+2)/3)*4 + 32*4
	const worstRow = BodyBytes + 4*base64Arena + 33*(16<<10) + (32 << 10) + MappingMaxBytes
	if worstRow > transportCellLimit {
		return fsapi.NoSpace
	}
	return nil
}
