package guestbridge

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// VerificationBinding contains actual constructed providers; the mandatory
// producer must bind these exact objects, never independently asserted labels.
type VerificationBinding struct {
	Endpoint     Endpoint
	Controller   fsapi.UUID
	Native       NativeVerifier
	Claims       Claims
	Execution    Execution
	Views        Views
	ViewProtocol ViewProtocolProvider
	Scopes       []Context
}

// VerificationProducer prepares observations outside controller/service locks
// and signs only actual verified source/event/floor evidence after dispatch.
// A nil producer preserves the pre-mutation controlled-view refusal.
type VerificationProducer interface {
	Bind(context.Context, VerificationBinding) error
	Prepare(context.Context, Command) error
	Produce(context.Context, Command, json.RawMessage) (*VerificationReceipt, error)
}

// MappedVerificationProducer binds the actual durable transport after bridge
// construction and signs only its privately observed protected mappings.
type MappedVerificationProducer interface {
	VerificationProducer
	BindTransport(context.Context, *Transport) error
	ProduceMapped(context.Context, Command, json.RawMessage) (*VerificationReceiptV2, error)
}
