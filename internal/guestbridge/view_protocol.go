package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Protocol is an indivisible negotiated tuple, also present in every 1.1 body.
// The zero/default configuration remains the closed 1.0 protocol.
type Protocol struct {
	BodySchema  uint64 `json:"body_schema"`
	BridgeMajor uint64 `json:"bridge_major"`
	BridgeMinor uint64 `json:"bridge_minor"`
	Features    uint64 `json:"features"`
	Msize       uint64 `json:"msize"`
	NormalMajor uint64 `json:"normal_major"`
	NormalMinor uint64 `json:"normal_minor"`
}

// ViewProtocol returns the only supported view-lease extension tuple.
func ViewProtocol() Protocol { return Protocol{2, 1, 1, 2047, 65536, 3, 0} }

// Ticket is a resource/provenance identity, never an ordinary execution Claim.
// Every lookup/mutation authenticates these original full credentials and scope.
type Ticket struct {
	Anchor      string      `json:"anchor"`
	Nonce       string      `json:"nonce"`
	Number      uint64      `json:"number"`
	Credentials Credentials `json:"credentials"`
}

// ClaimNumber permanently earmarks a future ordinary claim number. There is no
// OriginalHash until freeze. Existing Claims.Reserve/Allocate cannot issue it.
type ClaimNumber struct {
	Realm   string `json:"realm"`
	Subject string `json:"subject"`
	Anchor  string `json:"anchor"`
	Nonce   string `json:"nonce"`
	Number  uint64 `json:"number"`
}

// ViewTarget is the exact closed tagged target union. Raw payloads are validated
// independently; syntactically valid source fields are never source proof.
type ViewTarget struct {
	Kind        string          `json:"kind"`
	Publication json.RawMessage `json:"publication,omitempty"`
	Cut         *LocalViewCut   `json:"cut,omitempty"`
}

// LocalViewCut identifies an actual sealed disposable service/journal state.
// It grants no normal authority; Result.Publication remains null.
type LocalViewCut struct {
	EnrollmentHash       string `json:"enrollment_hash"`
	ProfileHash          string `json:"profile_hash"`
	ContextHash          string `json:"context_hash"`
	JournalIncarnation   string `json:"journal_incarnation"`
	JournalEpoch         uint64 `json:"journal_epoch"`
	JournalPosition      uint64 `json:"journal_position"`
	JournalHash          string `json:"journal_hash"`
	StateHash            string `json:"state_hash"`
	VisibilityGeneration string `json:"visibility_generation"`
}

// ViewAccess is DOMAIN, OBJECT (with Object), or PAGE (with Description/range).
// The selected extension profile supports exactly 4096-byte aligned pages.
type ViewAccess struct {
	Kind        string      `json:"kind"`
	Object      *Capability `json:"object,omitempty"`
	Description *Capability `json:"description,omitempty"`
	Offset      *uint64     `json:"offset,omitempty"`
	Length      *uint64     `json:"length,omitempty"`
}

// ViewLease is issued only by the verified controller, with monotone fence and
// revision. A pending request or codec-valid caller copy grants no cache access.
type ViewLease struct {
	ID             string     `json:"id"`
	Scope          Context    `json:"scope"`
	Domain         Capability `json:"domain"`
	Access         ViewAccess `json:"access"`
	CredentialHash string     `json:"credential_hash"`
	Mode           string     `json:"mode"`
	Target         ViewTarget `json:"target"`
	Fence          uint64     `json:"fence"`
	Revision       uint64     `json:"revision"`
}

// DirtyReservation preowns the manifest, immutable freeze, request/result and
// drain capacities before PTE installation. Its ID never depends on future data.
type DirtyReservation struct {
	ID          string      `json:"id"`
	Scope       Context     `json:"scope"`
	LeaseID     string      `json:"lease_id"`
	Description Capability  `json:"description"`
	Credentials Credentials `json:"credentials"`
	Offset      uint64      `json:"offset"`
	Length      uint64      `json:"length"`
	ClaimNumber ClaimNumber `json:"claim_number"`
	Provenance  string      `json:"provenance"`
	DrainCredit string      `json:"drain_credit"`
}

// ViewProtocolProvider is the controller authorization/dispatch seam, not an
// implementation. CheckEnrollment verifies signed disposable enrollment or real
// normal authority, exact tuple and durable session lifetime. CheckTarget must
// verify actual sealed journal/state/visibility or genuine normal source proof;
// it cannot accept caller hashes as evidence. Exchange must not block on recall:
// acquisitions use separately bounded work and return PENDING when necessary.
// It owns immutable scoped ticket bindings, stable replies, gap/floor retirement,
// fence/revision and source closure, including disconnect/unknown outcomes.
// No default provider exists. k04 supplies the separate durable local engine.
type ViewProtocolProvider interface {
	CheckEnrollment(context.Context, Context, Protocol) error
	CheckTarget(context.Context, Context, ViewTarget) error
	// CheckRequest verifies live authorization (including description rights and
	// ancestor/namespace dependencies), original scoped ticket and reserved credit.
	CheckRequest(context.Context, Context, string, json.RawMessage) error
	ExchangeView(context.Context, Context, string, json.RawMessage) (json.RawMessage, error)
	// CheckResponse verifies original durable admission bytes/operation/hash and
	// the protected current outcome, especially for QUERY/CANCEL and retirement.
	CheckResponse(context.Context, Context, string, json.RawMessage, json.RawMessage) error
}

// DecodeCommandVersion validates a body under an exact frame version. Legacy
// DecodeCommand intentionally continues to reject every extension envelope.
func DecodeCommandVersion(b []byte, minor uint16) (Command, error) {
	if minor == 0 {
		return DecodeCommand(b)
	}
	if minor != 1 && minor != 2 {
		return Command{}, fsapi.Unsupported
	}
	v, err := Canonical(b)
	if err != nil {
		return Command{}, err
	}
	o, err := object(v, "cell", "context", "op", "payload", "protocol", "reclaim")
	if err != nil || !uintRange(o["cell"], 1, MaxCell) || validateContext(o["context"]) != nil || validateProtocolVersion(o["protocol"], minor) != nil {
		return Command{}, fsapi.Invalid
	}
	if o["reclaim"] != nil && validateAck(o["reclaim"]) != nil {
		return Command{}, fsapi.Invalid
	}
	var c Command
	if err = json.Unmarshal(b, &c); err != nil {
		return Command{}, fsapi.Invalid
	}
	if c.Op == "HELLO" {
		err = validateProtocolVersion(o["payload"], minor)
	} else if minor == 2 && isAllocation(c.Op) {
		err = validatePayload("CALL", o["payload"], nil)
		if err == nil && c.Payload != nil {
			var call Call
			_ = json.Unmarshal(c.Payload, &call)
			if call.Intent.Opcode == "DRAIN_WRITE" {
				err = fsapi.Permission
			}
		}
	} else if minor == 2 && c.Op == "RETIRE" {
		err = validateControlledRetirement(o["payload"])
	} else if isViewExtension(c.Op) {
		err = validateViewPayload(c.Context, c.Op, o["payload"])
	} else if c.Op == "VIEW_ACK" {
		err = validateViewAckVersion(c.Context, o["payload"])
	} else {
		err = validatePayload(c.Op, o["payload"], o["reclaim"])
	}
	if err != nil {
		return Command{}, err
	}
	return c, nil
}

func validateProtocol(v any) error { return validateProtocolVersion(v, 1) }

func validateProtocolVersion(v any, minor uint16) error {
	raw, err := marshal(v)
	if err != nil {
		return err
	}
	selected := ViewProtocol()
	if minor == 2 {
		selected = ControlledProtocol()
	}
	expected, err := marshal(selected)
	if err != nil || !bytes.Equal(raw, expected) {
		return fsapi.Unsupported
	}
	return nil
}
func isViewExtension(op string) bool {
	return oneOf(op, "VIEW_ACQUIRE", "VIEW_RELEASE", "VIEW_RENEW", "VIEW_CANCEL", "VIEW_QUERY", "VIEW_RETIRE", "DIRTY_RESERVE", "DIRTY_FREEZE")
}
func commandMinor(c Command) uint16 {
	if c.Protocol != nil {
		return uint16(c.Protocol.BridgeMinor)
	}
	return 0
}

func (c Config) sessionProtocol(session string) *Protocol {
	if c.SessionProtocols != nil {
		if p, ok := c.SessionProtocols[session]; ok {
			return &p
		}
		return nil
	}
	return c.Protocol
}
func prepareProtocols(c *Config) error {
	if c.Protocol != nil && c.SessionProtocols != nil {
		return fsapi.Invalid
	}
	if c.Protocol != nil {
		if !supportedProtocol(*c, *c.Protocol) || missing(c.ViewProtocol) {
			return fsapi.Invalid
		}
		p := *c.Protocol
		c.Protocol = &p
		return nil
	}
	if len(c.SessionProtocols) > 64 {
		return fsapi.Invalid
	}
	if len(c.SessionProtocols) == 0 {
		if !missing(c.ViewProtocol) {
			return fsapi.Invalid
		}
		return nil
	}
	if missing(c.ViewProtocol) {
		return fsapi.Invalid
	}
	protocols := map[string]Protocol{}
	for id, p := range c.SessionProtocols {
		known := false
		for _, scope := range c.Scopes {
			if scope.Session == id {
				known = true
				break
			}
		}
		if !known || !supportedProtocol(*c, p) {
			return fsapi.Invalid
		}
		protocols[id] = p
	}
	c.SessionProtocols = protocols
	return nil
}
