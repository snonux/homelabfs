package guestbridge

import (
	"encoding/json"
	"github.com/snonux/homelabfs/internal/fsapi"
	"strconv"
)

// ProtectedBridgeSession is complete inert C8 evidence. It includes HELLO,
// capability tombstones, credentials, close counters and terminal ownership;
// unlike a provider outcome snapshot it is not limited to ordinary commands.
// State cannot enroll a controller or establish physical continuity.
type ProtectedBridgeSession struct {
	Controller fsapi.UUID
	State      json.RawMessage
}

// bridgeSessionLimit bounds ALL complete scope rows together. Copies (retained
// encoding, transaction staging and decoded state) spend at most 24 MiB within
// the existing 1 GiB ledger reservation, not an additional parser workspace.
const bridgeSessionLimit = 8 << 20

func bridgeSessionKey(session string) []byte { return []byte("session:" + session) }

func (s *Service) protectedSessions() (map[string][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make(map[string][]byte, len(s.sessions))
	total := 0
	for id, state := range s.sessions {
		raw, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		row, err := json.Marshal(ProtectedBridgeSession{Controller: s.controller, State: raw})
		if err != nil {
			return nil, err
		}
		total += len(row)
		if total > bridgeSessionLimit {
			return nil, fsapi.NoSpace
		}
		rows[string(bridgeSessionKey(id))] = row
	}
	return rows, nil
}

func validateBridgeSession(index ProtectedTransportState, scope Context, row ProtectedBridgeSession) error {
	if row.Controller != index.Controller {
		return fsapi.Stale
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(row.State, &fields); err != nil {
		return err
	}
	for _, key := range []string{"Generation", "Reserved", "Context", "Hello", "Credentials", "Exited", "Grants", "CloseSequences"} {
		if _, ok := fields[key]; !ok {
			return fsapi.Stale
		}
	}
	var ss session
	if err := decodeTransportJSON(row.State, bridgeSessionLimit, &ss); err != nil {
		return err
	}
	if _, ok := index.RuntimeHellos[scope.Session]; ok && !ss.Hello {
		return fsapi.Stale
	}
	if ss.Context != scope || !ss.Generation.Valid() || ss.Reserved < 0 || len(ss.Grants) > 65536 || len(ss.Credentials) > 65536 || ss.Grants == nil || ss.Credentials == nil || ss.Exited == nil || ss.CloseSequences == nil {
		return fsapi.Stale
	}
	for key, g := range ss.Grants {
		if g == nil || key != g.Cap.Token || g.Cap.Scope != scope {
			return fsapi.Stale
		}
	}
	if ss.Terminal != nil && index.Terminal[scope.Session] == 0 {
		return fsapi.Stale
	}
	if ss.Terminal != nil && (ss.Terminal.Scope != scope || ss.Terminal.Controller != index.Controller.String() || ss.Terminal.Owner != "BRIDGE") {
		return fsapi.Stale
	}
	return nil
}

// checkpointFootprint includes the largest future authority-reducing closure of
// the admitted identities. Ordinary admission cannot spend this space. Close
// identities are bounded by both the credential quota and the Cartesian product
// of descriptions and admitted processes (plus the terminal observer process).
// Credentials are closed-schema: 1024 uint32 groups fit within 32 KiB including
// base64 encoding and their map key. A complete terminal seal fits within 2 KiB.
func (s *Service) checkpointFootprint(ss *session) (int, error) {
	state, err := json.Marshal(ss)
	if err != nil {
		return 0, err
	}
	raw, err := json.Marshal(ProtectedBridgeSession{Controller: s.controller, State: state})
	if err != nil {
		return 0, err
	}
	if ss.Terminal != nil {
		return len(raw), nil
	}
	descriptions := 0
	growth := 0
	for _, g := range ss.Grants {
		if g.Cap.Kind == "DESCRIPTION" && !g.Released {
			descriptions++
			growth += 20 - len(strconv.FormatUint(g.Sequence, 10))
		}
	}
	processes := map[string]bool{}
	for key := range ss.Credentials {
		if len(key) >= 36 {
			processes[key[:36]] = true
		}
	}
	closeRows := min(s.config.MaxCredentialRevisions, descriptions*(len(processes)+1))
	closeRows = max(closeRows, len(ss.CloseSequences))
	closeBytes, err := json.Marshal(ss.CloseSequences)
	if err != nil {
		return 0, err
	}
	exitBytes, err := json.Marshal(ss.Exited)
	if err != nil {
		return 0, err
	}
	reserve := max(0, 128*closeRows-(len(closeBytes)-2)) + max(0, 64*(len(processes)+1)-(len(exitBytes)-2)) + growth + 32768
	if ss.Terminal == nil {
		reserve += 2048
	}
	return len(raw) + reserve, nil
}

// checkpointAdmissionLocked is called before publishing newly admitted state.
// Staged pre-effect result construction also charges all other live scopes.
func (s *Service) checkpointAdmissionLocked(terminal bool) error {
	total := s.checkpointPeers
	for _, ss := range s.sessions {
		n, err := s.checkpointFootprint(ss)
		if err != nil {
			return err
		}
		total += n
	}
	limit := bridgeSessionLimit
	if terminal {
		limit += 32768
	} // the fenced terminal call consumes its own reserve
	if total > limit {
		return fsapi.NoSpace
	}
	return nil
}
