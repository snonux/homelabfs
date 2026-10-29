package sim

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// IntentKind selects the oracle's deliberately small regular-file workload.
type IntentKind uint8

const (
	// CreateFile exclusively creates an empty file in the volume root.
	CreateFile IntentKind = iota + 1
	// AppendFile appends the entire payload once.
	AppendFile
	// ChmodFile replaces permission bits.
	ChmodFile
	// RenameFile moves a file to an absent root name.
	RenameFile
	// RemoveFile removes a regular-file root name.
	RemoveFile
)

// Intent owns independent workload input, never implementation afterimages.
type Intent struct {
	Kind              IntentKind
	Name, Destination string
	Data              []byte
	Mode              uint16
	UID, GID          uint32
}

// Outcome distinguishes an observed acknowledgement from an ambiguous reply.
type Outcome uint8

const (
	// Uncertain includes timeouts, lost replies and still-pending invocations.
	Uncertain Outcome = iota
	// Acknowledged promises that the logical effect survives recovery.
	Acknowledged
	// Rejected is reserved for proven pre-admission rejection, not an I/O error.
	Rejected
)

// HistoryEntry is harness-owned evidence outside every crashable actor root.
// Ordinals order equal virtual timestamps without depending on goroutine order.
type HistoryEntry struct {
	ID                     string
	Volume                 fsapi.UUID
	Client                 string
	Intent                 Intent
	PayloadSHA256          [32]byte
	Invoked, Completed     uint64
	InvokedAt, CompletedAt time.Duration
	Outcome                Outcome
	Result                 string
}

// History records serial per-volume client calls. It is scenario-owned and must
// not be called concurrently; multiple clients are identities, not Go workers.
type History struct {
	entries []HistoryEntry
	ordinal uint64
	bytes   int
}

// NewHistory constructs an empty bounded external recorder.
func NewHistory() *History { return &History{} }

// Invoke retains owned input before sending it to the cluster.
func (h *History) Invoke(id string, volume fsapi.UUID, client string, at time.Duration, intent Intent) error {
	if len(h.entries) >= 256 || len(intent.Data) > 4096 || h.bytes+len(intent.Data) > 1<<20 {
		return fmt.Errorf("history input budget")
	}
	if !volume.Valid() || len(id) == 0 || len(id) > 128 || len(client) == 0 || len(client) > 128 || at < 0 {
		return fmt.Errorf("invalid invocation identity or time")
	}
	if err := validateIntent(intent); err != nil {
		return err
	}
	for _, e := range h.entries {
		if e.ID == id || e.Volume == volume && e.Completed == 0 {
			return fmt.Errorf("duplicate ID or overlapping volume call")
		}
		if at < e.InvokedAt || e.Completed != 0 && at < e.CompletedAt {
			return fmt.Errorf("history time moved backwards")
		}
	}
	h.ordinal++
	intent.Data = slices.Clone(intent.Data)
	h.entries = append(h.entries, HistoryEntry{ID: id, Volume: volume, Client: client, Intent: intent, PayloadSHA256: sha256.Sum256(intent.Data), Invoked: h.ordinal, InvokedAt: at})
	h.bytes += len(intent.Data)
	return nil
}

// Complete records the client-visible result. Lost replies use Uncertain even
// when the harness knows that the service finished; errors never prove rollback.
func (h *History) Complete(id string, at time.Duration, outcome Outcome, result string) error {
	if outcome > Rejected || len(result) > 1024 {
		return fmt.Errorf("invalid completion")
	}
	for i := range h.entries {
		e := &h.entries[i]
		if e.ID != id {
			continue
		}
		if e.Completed != 0 || at < e.InvokedAt {
			return fmt.Errorf("duplicate completion or backwards time")
		}
		for _, other := range h.entries {
			if at < other.InvokedAt || at < other.CompletedAt {
				return fmt.Errorf("history time moved backwards")
			}
		}
		h.ordinal++
		e.Completed, e.CompletedAt, e.Outcome, e.Result = h.ordinal, at, outcome, strings.Clone(result)
		return nil
	}
	return fmt.Errorf("completion without invocation")
}

// Entries returns evidence that remains owned when the caller changes its copy.
func (h *History) Entries() []HistoryEntry {
	out := slices.Clone(h.entries)
	for i := range out {
		out[i].Intent.Data = slices.Clone(out[i].Intent.Data)
	}
	return out
}

func validateIntent(i Intent) error {
	validName := func(s string) bool {
		return len(s) > 0 && len(s) <= 255 && s != "." && s != ".." && !strings.ContainsAny(s, "/\x00")
	}
	if i.Kind < CreateFile || i.Kind > RemoveFile || !validName(i.Name) || i.Mode & ^uint16(0o7777) != 0 {
		return fmt.Errorf("unsupported oracle intent")
	}
	if i.Kind == RenameFile && (!validName(i.Destination) || i.Destination == i.Name) {
		return fmt.Errorf("invalid rename intent")
	}
	if i.Kind == AppendFile && len(i.Data) == 0 || i.Kind != AppendFile && len(i.Data) != 0 {
		return fmt.Errorf("invalid intent payload")
	}
	return nil
}
