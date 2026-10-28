package coord

import (
	"fmt"
	"slices"

	"github.com/snonux/homelabfs/internal/cluster"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// Validate verifies default-deny policy shape; it does not establish authority.
func (p Policy) Validate() error {
	if p.Format != 1 {
		return fmt.Errorf("unsupported policy format")
	}
	registry, err := cluster.New(p.Registry)
	if err != nil {
		return fmt.Errorf("policy registry: %w", err)
	}
	switch p.Mode {
	case Normal:
		if p.EmergencyGeneration.Valid() || p.ServingNode.Valid() || len(p.Recovery) != 0 {
			return fmt.Errorf("normal policy contains emergency permissions")
		}
	case Emergency:
		n, ok := registry.Node(p.ServingNode)
		if !p.EmergencyGeneration.Valid() || !ok || n.Admission != cluster.Admitted || len(p.Recovery) > 4096 {
			return fmt.Errorf("invalid emergency generation or serving node")
		}
		return p.validateRecovery(registry)
	default:
		return fmt.Errorf("unknown admission mode")
	}
	return nil
}

// CheckRecovery checks only manually installed recovery-transfer admission.
// It never permits normal replication acceptance, promotion or workload serving.
func (p Policy) CheckRecovery(node, volume fsapi.UUID) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%w: invalid policy: %v", ErrDenied, err)
	}
	if p.Mode != Emergency {
		return ErrDenied
	}
	for _, peer := range p.Recovery {
		if peer.Node == node && slices.Contains(peer.Volumes, volume) {
			return nil
		}
	}
	return ErrDenied
}

func (p Policy) validateRecovery(registry *cluster.Registry) error {
	seen := make(map[fsapi.UUID]bool)
	for _, peer := range p.Recovery {
		node, ok := registry.Node(peer.Node)
		if !ok || node.Admission != cluster.Admitted || peer.Node == p.ServingNode || seen[peer.Node] || len(peer.Volumes) == 0 || len(peer.Volumes) > 4096 {
			return fmt.Errorf("invalid recovery peer")
		}
		seen[peer.Node] = true
		volumes := make(map[fsapi.UUID]bool)
		for _, volume := range peer.Volumes {
			if _, ok := registry.Volume(volume); !ok || volumes[volume] {
				return fmt.Errorf("unknown or duplicate recovery volume")
			}
			volumes[volume] = true
		}
	}
	return nil
}

func (p Policy) candidate(req Request) (cluster.Volume, error) {
	if p.Mode != Normal {
		return cluster.Volume{}, ErrDenied
	}
	registry, err := cluster.New(p.Registry)
	if err != nil {
		return cluster.Volume{}, err
	}
	n, ok := registry.Node(req.Owner)
	if !ok || n.Admission != cluster.Admitted {
		return cluster.Volume{}, ErrDenied
	}
	v, ok := registry.Volume(req.Volume)
	if !ok {
		return v, ErrDenied
	}
	for _, member := range v.Members {
		if member.Node == req.Owner {
			return v, nil
		}
	}
	return v, ErrDenied
}
