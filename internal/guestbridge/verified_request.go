package guestbridge

import (
	"context"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// CommandObservation binds actual authenticated callback provenance to one
// canonical command. It supplies no kernel, PTE, journal or serving proof.
// Constructing this exported value cannot create a verified context receipt.
type CommandObservation struct {
	Native                              Native
	Controller                          fsapi.UUID
	Scope                               Context
	Protocol                            *Protocol
	Cell                                uint64
	Operation, CommandHash, PayloadHash string
}

type commandObservationKey struct{}

// VerifiedRequest returns a detached observation minted only by Service.Execute
// after native authentication and exact scope/version checks. Runtime Facts must
// additionally establish installed source, live kernel barriers and completeness.
// Native group/receipt copies are bounded by the existing1024-group request cap;
// only hashes/headers are retained, never another1MiB command/payload copy.
func VerifiedRequest(ctx context.Context) (CommandObservation, bool) {
	observed, ok := ctx.Value(commandObservationKey{}).(CommandObservation)
	if !ok {
		return CommandObservation{}, false
	}
	observed.Native.Groups = slices.Clone(observed.Native.Groups)
	if observed.Protocol != nil {
		protocol := *observed.Protocol
		observed.Protocol = &protocol
	}
	return observed, true
}

func observeCommand(ctx context.Context, n Native, controller fsapi.UUID, c Command, canonical []byte) context.Context {
	n.Groups = slices.Clone(n.Groups)
	var protocol *Protocol
	if c.Protocol != nil {
		copyProtocol := *c.Protocol
		protocol = &copyProtocol
	}
	observed := CommandObservation{Native: n, Controller: controller, Scope: c.Context, Protocol: protocol, Cell: c.Cell, Operation: c.Op, CommandHash: digest(canonical), PayloadHash: digest(c.Payload)}
	return context.WithValue(ctx, commandObservationKey{}, observed)
}
