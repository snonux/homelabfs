package guestbridge

import (
	"bytes"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// MappedVersion selects authenticated original-to-native provenance.
const MappedVersion = "9P2000.L.hfs1.4"

// DelegationVersion identifies the closed native-only translation metadata.
const DelegationVersion = "homelabfs-native-delegation/1"

// Delegation is untrusted wire metadata until the actual authenticated transport
// admits its protected candidate. The backend must reject guest non-null values.
type Delegation struct {
	Version      string `json:"version"`
	GuestCell    uint64 `json:"guest_cell"`
	GuestOp      string `json:"guest_op"`
	GuestReclaim *Ack   `json:"guest_reclaim"`
	PollIndex    uint64 `json:"poll_index"`
}

// MappedProtocol is the closed original-binding successor of cleanup1.3.
func MappedProtocol() Protocol { return Protocol{5, 1, 4, 16383, 65536, 3, 0} }

// TerminalProtocol explicitly selects authenticated terminal session ownership.
// The existing mapped tuple remains unchanged and cannot issue this extension.
func TerminalProtocol() Protocol { return Protocol{5, 1, 4, 32767, 65536, 3, 0} }

// IsMappedProtocol recognizes only the two exact original-binding tuples.
// The complete selected tuple must additionally match signed enrollment.
func IsMappedProtocol(p Protocol) bool { return p == MappedProtocol() || p == TerminalProtocol() }

// HasCleanupProtocol reports tuples implementing the complete parent-funded cleanup contract.
func HasCleanupProtocol(p Protocol) bool { return p == CleanupProtocol() || IsMappedProtocol(p) }

// MarshalJSON preserves the closed legacy envelopes and requires explicit null
// delegation in the new guest-original envelope.
func (c Command) MarshalJSON() ([]byte, error) {
	type wire Command
	if commandMinor(c) != 4 {
		return commandJSON(wire(c))
	}
	return commandJSON(struct {
		wire
		Delegation *Delegation `json:"delegation"`
	}{wire(c), c.Delegation})
}
func validateDelegation(v any) error {
	o, err := object(v, "version", "guest_cell", "guest_op", "guest_reclaim", "poll_index")
	if err != nil || text(o["version"]) != DelegationVersion || !uintRange(o["guest_cell"], 1, MaxCell) || !uintRange(o["poll_index"], 0, 15) || text(o["guest_op"]) == "" {
		return fsapi.Invalid
	}
	if o["guest_reclaim"] != nil && validateAck(o["guest_reclaim"]) != nil {
		return fsapi.Invalid
	}
	return nil
}

func commandJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}
