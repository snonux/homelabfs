package guestbridge

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TerminalRequest binds the exact original terminal attempt and observer cut.
// Decoding it supplies no physical, native, worker or owner provenance.
type TerminalRequest struct {
	RetirementID     string      `json:"retirement_id"`
	Credentials      Credentials `json:"credentials"`
	GuestThrough     uint64      `json:"guest_through"`
	ObserverRevision uint64      `json:"observer_revision"`
	ClosureHash      string      `json:"closure_hash"`
	SettledCells     []Ack       `json:"settled_cells"`
}

// TerminalResponse identifies the protected retained native response owner.
type TerminalResponse struct {
	Status           string `json:"status"`
	RetirementID     string `json:"retirement_id"`
	GuestThrough     uint64 `json:"guest_through"`
	ObserverRevision uint64 `json:"observer_revision"`
	ClosureHash      string `json:"closure_hash"`
	SettledHash      string `json:"settled_hash"`
	OwnershipHash    string `json:"ownership_hash"`
}

// TerminalEvidence is the closed SESSION_RETIRED receipt2 evidence.
type TerminalEvidence struct {
	RetirementID     string      `json:"retirement_id"`
	Credentials      Credentials `json:"credentials"`
	GuestThrough     uint64      `json:"guest_through"`
	ObserverRevision uint64      `json:"observer_revision"`
	ClosureHash      string      `json:"closure_hash"`
	SettledHash      string      `json:"settled_hash"`
	OwnershipHash    string      `json:"ownership_hash"`
	RetainedOwner    string      `json:"retained_owner"`
}

func validateTerminalRequest(v any) error {
	o, err := object(v, "retirement_id", "credentials", "guest_through", "observer_revision", "closure_hash", "settled_cells")
	if err != nil || !uuid(o["retirement_id"]) || validateCred(o["credentials"]) != nil || !uintRange(o["guest_through"], 0, MaxCell-1) || !uintRange(o["observer_revision"], 1, ^uint64(0)) || !hash(o["closure_hash"]) {
		return fsapi.Invalid
	}
	return validateSettledWire(o["settled_cells"], o["guest_through"])
}

// The manifest is inert wire data. Only the actual transport can match it to
// owned, fully observed original/native cells before transferring their credits.
func validateSettledWire(v, through any) error {
	values, ok := v.([]any)
	if !ok || len(values) > 79 || !uintRange(through, 0, MaxCell-1) {
		return fsapi.Invalid
	}
	high, _ := strconv.ParseUint(string(through.(json.Number)), 10, 64)
	var previous uint64
	for _, value := range values {
		if validateAck(value) != nil {
			return fsapi.Invalid
		}
		n, _ := strconv.ParseUint(string(value.(map[string]any)["cell"].(json.Number)), 10, 64)
		if n <= previous || n > high {
			return fsapi.Invalid
		}
		previous = n
	}
	return nil
}

func validateSettledAcks(acks []Ack, through uint64, donor *Ack) error {
	if acks == nil || len(acks) > 79 || through >= MaxCell || donor == nil && (len(acks) != 0 || through != 0) || donor != nil && (donor.Cell == 0 || donor.Cell > through || !hash(donor.ResponseHash)) {
		return fsapi.Invalid
	}
	var previous uint64
	for _, ack := range acks {
		if ack.Cell <= previous || ack.Cell > through || !hash(ack.ResponseHash) || donor != nil && ack.Cell == donor.Cell {
			return fsapi.Invalid
		}
		previous = ack.Cell
	}
	return nil
}

// SettledHash commits the exact canonical array; it supplies no owner authority.
func (r TerminalRequest) SettledHash() (string, error) {
	if r.SettledCells == nil || len(r.SettledCells) > 79 {
		return "", fsapi.Invalid
	}
	raw, err := marshal(r.SettledCells)
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}

func validateTerminalEvidence(v any, mapped bool) error {
	o, err := object(v, "retirement_id", "credentials", "guest_through", "observer_revision", "closure_hash", "settled_hash", "ownership_hash", "retained_owner")
	if !mapped || err != nil || !hash(o["settled_hash"]) || !hash(o["ownership_hash"]) || text(o["retained_owner"]) != "C8_TERMINAL_CELL" {
		return fsapi.Invalid
	}
	request := map[string]any{}
	for _, key := range []string{"retirement_id", "credentials", "guest_through", "observer_revision", "closure_hash"} {
		request[key] = o[key]
	}
	request["settled_cells"] = []any{}
	return validateTerminalRequest(request)
}

func validateTerminalResponse(c Command, raw []byte) error {
	if c.Protocol == nil || *c.Protocol != TerminalProtocol() || c.Op != "SESSION_RETIRE" {
		return fsapi.Permission
	}
	v, err := Canonical(raw)
	if err != nil {
		return err
	}
	o, err := object(v, "status", "retirement_id", "guest_through", "observer_revision", "closure_hash", "settled_hash", "ownership_hash")
	if err != nil || text(o["status"]) != "RETIRED" || !hash(o["settled_hash"]) || !hash(o["ownership_hash"]) {
		return fsapi.Invalid
	}
	request, err := envelopeFields(c.Payload)
	if err != nil {
		return err
	}
	reply, err := envelopeFields(raw)
	if err != nil {
		return err
	}
	for _, key := range []string{"retirement_id", "guest_through", "observer_revision", "closure_hash"} {
		if !bytes.Equal(request[key], reply[key]) {
			return fsapi.Stale
		}
	}
	decoded, err := DecodeTerminalRequest(c.Payload)
	if err != nil {
		return err
	}
	expected, err := decoded.SettledHash()
	if err != nil || text(o["settled_hash"]) != expected {
		return fsapi.Stale
	}
	return nil
}

func validateTerminalKind(c Command, r VerificationReceiptV2, payload []byte) error {
	if r.PollCount != 1 {
		return fsapi.Permission
	}
	if err := validateTerminalResponse(c, payload); err != nil {
		return err
	}
	request, err := envelopeFields(c.Payload)
	if err != nil {
		return err
	}
	evidence, err := envelopeFields(r.Evidence)
	if err != nil {
		return err
	}
	response, err := envelopeFields(payload)
	if err != nil {
		return err
	}
	for _, key := range []string{"retirement_id", "credentials", "guest_through", "observer_revision", "closure_hash"} {
		if !bytes.Equal(request[key], evidence[key]) {
			return fsapi.Stale
		}
	}
	if !bytes.Equal(response["ownership_hash"], evidence["ownership_hash"]) || !bytes.Equal(response["settled_hash"], evidence["settled_hash"]) {
		return fsapi.Stale
	}
	return nil
}

// DecodeTerminalRequest validates the closed canonical request without minting
// any worker, source, observer or physical authority.
func DecodeTerminalRequest(raw []byte) (TerminalRequest, error) {
	var request TerminalRequest
	v, err := Canonical(raw)
	if err == nil {
		err = validateTerminalRequest(v)
	}
	if err == nil {
		err = json.Unmarshal(raw, &request)
	}
	return request, err
}

// TerminalSnapshot is inert decoded evidence from the existing read-only kernel
// observer. Only live Admission measurement plus actual owner checks may use it.
type TerminalSnapshot struct {
	Version          string      `json:"version"`
	Scope            Context     `json:"scope"`
	Controller       string      `json:"controller"`
	RetirementID     string      `json:"retirement_id"`
	Revision         uint64      `json:"revision"`
	GuestThrough     uint64      `json:"guest_through"`
	Donor            *Ack        `json:"donor"`
	SettledCells     []Ack       `json:"settled_cells"`
	Credentials      Credentials `json:"credentials"`
	Entrants         uint64      `json:"entrants"`
	Polls            uint64      `json:"polls"`
	Unknown          uint64      `json:"unknown"`
	Claims           uint64      `json:"claims"`
	HeldResults      uint64      `json:"held_results"`
	Descriptions     uint64      `json:"descriptions"`
	Leases           uint64      `json:"leases"`
	Reservations     uint64      `json:"reservations"`
	DirtyPins        uint64      `json:"dirty_pins"`
	SourcePins       uint64      `json:"source_pins"`
	MappedPages      uint64      `json:"mapped_pages"`
	ReceivedExternal uint64      `json:"received_external"`
	Phase            string      `json:"phase"`
}

// Validate checks the closed immutable terminal object as inert data only.
func (s TerminalSnapshot) Validate() error {
	if s.Version != "homelabfs-guest-terminal/1" || !validUUID(s.Controller) || !validUUID(s.RetirementID) || s.Revision == 0 || s.GuestThrough >= MaxCell || s.Phase != "TERMINAL_PENDING" || s.Entrants != 0 || s.Polls != 0 || s.Unknown != 0 || s.Claims != 0 || s.HeldResults != 0 || s.Descriptions != 0 || s.Leases != 0 || s.Reservations != 0 || s.DirtyPins != 0 || s.SourcePins != 0 || s.MappedPages != 0 || s.ReceivedExternal != 0 {
		return fsapi.Permission
	}
	raw, err := marshal(s.Scope)
	if err != nil {
		return err
	}
	v, err := Canonical(raw)
	if err != nil || validateContext(v) != nil {
		return fsapi.Invalid
	}
	raw, err = marshal(s.Credentials)
	if err != nil {
		return err
	}
	v, err = Canonical(raw)
	if err != nil || validateCred(v) != nil {
		return fsapi.Invalid
	}
	if s.Donor != nil && (s.Donor.Cell == 0 || s.Donor.Cell > s.GuestThrough || !hash(s.Donor.ResponseHash)) {
		return fsapi.Invalid
	}
	return validateSettledAcks(s.SettledCells, s.GuestThrough, s.Donor)
}

// UnmarshalJSON checks every required field before Go's zero values can erase
// absence. This decodes inert observer bytes and creates no private provenance.
func (s *TerminalSnapshot) UnmarshalJSON(raw []byte) error {
	v, err := Canonical(raw)
	if err != nil {
		return err
	}
	o, err := object(v, "version", "scope", "controller", "retirement_id", "revision", "guest_through", "donor", "settled_cells", "credentials", "entrants", "polls", "unknown", "claims", "held_results", "descriptions", "leases", "reservations", "dirty_pins", "source_pins", "mapped_pages", "received_external", "phase")
	if err != nil {
		return err
	}
	if validateContext(o["scope"]) != nil || validateCred(o["credentials"]) != nil {
		return fsapi.Invalid
	}
	for _, key := range []string{"entrants", "polls", "unknown", "claims", "held_results", "descriptions", "leases", "reservations", "dirty_pins", "source_pins", "mapped_pages", "received_external"} {
		if !uintRange(o[key], 0, 0) {
			return fsapi.Invalid
		}
	}
	if !uintRange(o["guest_through"], 0, MaxCell-1) || !uintRange(o["revision"], 1, ^uint64(0)) {
		return fsapi.Invalid
	}
	if o["donor"] != nil && validateAck(o["donor"]) != nil {
		return fsapi.Invalid
	}
	if err = validateSettledWire(o["settled_cells"], o["guest_through"]); err != nil {
		return err
	}
	type wire TerminalSnapshot
	var decoded wire
	if err = json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	candidate := TerminalSnapshot(decoded)
	if err = candidate.Validate(); err != nil {
		return err
	}
	*s = candidate
	return nil
}
