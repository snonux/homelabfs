//go:build linux || freebsd

package runtimeprep

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/snonux/homelabfs/internal/controlledruntime"
)

// ArgvRecordVersion is the G4 record written by tools/lab/directlaunch/argv.py.
const ArgvRecordVersion = "homelabfs-zq3-bhyve-argv/1"

// ArgvRecord is the closed top-level schema of the G4 record. Fields this
// check does not compare stay raw; argv.py verify rebuilds the whole record
// from its derivation, so they are still covered there.
type ArgvRecord struct {
	Version      string            `json:"version"`
	RunID        string            `json:"run_id"`
	VM           string            `json:"vm"`
	FactsSHA256  string            `json:"facts_sha256"`
	LaunchLogged json.RawMessage   `json:"launch_logged"`
	Sessions     map[string]string `json:"sessions"`
	Host         json.RawMessage   `json:"host"`
	Exporter     struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"exporter"`
	Changes    json.RawMessage `json:"changes"`
	Args       []string        `json:"args"`
	ArgsSHA256 string          `json:"args_sha256"`
}

// CheckArgvRecord binds a generated bundle to the pinned G4 argv record. The
// order of operations is: generate (draws the sessions), argv.py generate
// with those sessions, then this check plus argv.py verify. It requires the
// approved record bytes, the bundle's run ID, the same session for every
// share tag, the same exporter path/digest and Exporter.Args equal to the
// record's args element by element. It returns the record's args_sha256.
func CheckArgvRecord(c controlledruntime.RuntimeConfig, r Record, raw []byte, pin string) (string, error) {
	if _, err := decodeHex(pin, 32, "record SHA256 pin"); err != nil {
		return "", err
	}
	if sha256Hex(raw) != pin {
		return "", fmt.Errorf("argv record bytes are not the approved pin")
	}
	var record ArgvRecord
	if err := decodeStrict(raw, &record); err != nil {
		return "", fmt.Errorf("argv record: %w", err)
	}
	if record.Version != ArgvRecordVersion || record.RunID != r.RunID {
		return "", fmt.Errorf("argv record version or run ID %q differs from the bundle's %q", record.RunID, r.RunID)
	}
	if err := matchRecordSessions(record, r); err != nil {
		return "", err
	}
	if record.Exporter.Path != c.Exporter.Path || record.Exporter.SHA256 != hex.EncodeToString(c.Exporter.SHA256[:]) {
		return "", fmt.Errorf("argv record exporter path/digest differs from the config")
	}
	if err := sameArgs(record.Args, c.Exporter.Args); err != nil {
		return "", err
	}
	digest, err := canonicalArgsSHA256(record.Args)
	if err != nil || digest != record.ArgsSHA256 {
		return "", fmt.Errorf("argv record args_sha256 does not cover its args")
	}
	return digest, nil
}

// matchRecordSessions requires exactly the bundle's sessions, keyed by tag.
func matchRecordSessions(record ArgvRecord, r Record) error {
	if len(record.Sessions) != len(r.Sessions) {
		return fmt.Errorf("argv record names %d sessions; the bundle has %d", len(record.Sessions), len(r.Sessions))
	}
	for _, s := range r.Sessions {
		if record.Sessions[s.DeviceTag] != s.Session {
			return fmt.Errorf("argv record session for %s differs from the bundle", s.DeviceTag)
		}
	}
	return nil
}

func sameArgs(record, config []string) error {
	if len(record) != len(config) {
		return fmt.Errorf("argv record has %d args; the config has %d", len(record), len(config))
	}
	for i := range record {
		if record[i] != config[i] {
			return fmt.Errorf("Exporter.Args[%d] %q differs from the record's %q", i, config[i], record[i])
		}
	}
	return nil
}

// canonicalArgsSHA256 reproduces argv.py's canonical(): compact JSON with a
// trailing newline. Python's ensure_ascii escapes non-ASCII and Go escapes
// HTML characters by default, so only printable ASCII args are accepted.
func canonicalArgsSHA256(args []string) (string, error) {
	for _, a := range args {
		for _, b := range []byte(a) {
			if b < 0x20 || b > 0x7e {
				return "", fmt.Errorf("non-printable or non-ASCII argument")
			}
		}
	}
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(args); err != nil {
		return "", err
	}
	return sha256Hex(out.Bytes()), nil
}
