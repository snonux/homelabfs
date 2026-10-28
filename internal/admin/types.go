// Package admin implements the private, local administrative control plane.
// Unix socket reachability is never sufficient authorization by itself.
package admin

import (
	"context"
	"time"
)

const (
	protocolVersion = 1
	maxFrame        = 16 << 10
	maxConnections  = 16
	ioTimeout       = 2 * time.Second
)

// Level separates observation, test control and unimplemented recovery authority.
type Level uint8

const (
	Read Level = iota + 1
	Control
	Recovery
)

// Peer is a kernel-observed local process identity. PID is absent on FreeBSD.
type Peer struct {
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`
	PID *int32 `json:"pid,omitempty"`
}

// Position preserves the entire journal identity; sequence is not a backing slot.
type Position struct {
	Volume        string `json:"volume"`
	Generation    string `json:"generation"`
	Configuration string `json:"configuration"`
	Epoch         uint64 `json:"epoch"`
	Sequence      uint64 `json:"sequence"`
}

// VolumeStatus describes observed local state. Nil replica progress means unknown.
type VolumeStatus struct {
	VolumeID          string    `json:"volume_id"`
	Generation        string    `json:"generation"`
	State             string    `json:"state"`
	WaitReason        string    `json:"wait_reason"`
	Profile           string    `json:"profile"`
	NormalPublication bool      `json:"normal_publication"`
	SourcePosition    *Position `json:"source_position"`
	AppliedPosition   *Position `json:"applied_position"`
	BackingSlots      uint64    `json:"backing_slots"`
	SyncPosition      *Position `json:"sync_position"`
	AsyncPosition     *Position `json:"async_position"`
}

// Status contains local daemon observations, not distributed readiness evidence.
type Status struct {
	NodeID           string         `json:"node_id"`
	Volumes          []VolumeStatus `json:"volumes"`
	EnabledFaults    []string       `json:"enabled_faults"`
	TestHooksEnabled bool           `json:"test_hooks_enabled"`
}

// Request is one bounded versioned JSON line. No caller-supplied identity is used.
type Request struct {
	Version   int    `json:"version"`
	Operation string `json:"operation"`
	Fault     string `json:"fault,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
}

// Response is a single bounded reply; status is present only on successful reads.
type Response struct {
	Version int     `json:"version"`
	Error   string  `json:"error,omitempty"`
	Status  *Status `json:"status,omitempty"`
}

// Event records the peer and exact target before and after control requests.
type Event struct {
	Time       time.Time `json:"time"`
	Peer       Peer      `json:"peer"`
	NodeID     string    `json:"node_id"`
	VolumeID   string    `json:"volume_id"`
	Generation string    `json:"generation"`
	Operation  string    `json:"operation"`
	Fault      string    `json:"fault,omitempty"`
	Enabled    bool      `json:"enabled"`
	Outcome    string    `json:"outcome"`
}

// Config supplies observations and narrowly scoped hooks. Recovery is always denied.
// Authorize is an additional restriction on the daemon UID, never an expansion.
// Audit must durably record control requests or return an error before side effects.
// Callbacks must honor context; kernel file synchronization may remain uninterruptible.
type Config struct {
	SocketPath                   string
	NodeID, VolumeID, Generation string
	Status                       func(context.Context) (Status, error)
	Authorize                    func(Peer, Level) bool
	Audit                        func(context.Context, Event) error
	EnableTestHooks              bool
	ToggleFault                  func(context.Context, string, bool) error
}
