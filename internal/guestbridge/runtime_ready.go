package guestbridge

import (
	"context"
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"reflect"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// runtimeHelloWitness is minted only on the live funded endpoint worker after
// actual authenticated HELLO execution. The index and immutable response commit
// in the same C8 transaction. It survives normal cell reclaim, with one bounded
// witness per configured scope. Decoded index evidence grants no live authority.
type runtimeHelloWitness struct {
	Cell                      uint64
	CommandHash, ResponseHash string
}

func (t *Transport) protectRuntimeHello(c *cell) error {
	if c.command == nil || c.command.Op != "HELLO" || c.command.Protocol == nil || !IsMappedProtocol(*c.command.Protocol) {
		return nil
	}
	if t.ledger == nil || c.command.Delegation == nil || c.mapping == nil || !c.mapping.Admitted || c.mapping.NativeFullHash != digest(c.response) {
		return fsapi.Permission
	}
	var response struct {
		Op       string    `json:"op"`
		Protocol *Protocol `json:"protocol"`
		Payload  struct {
			Status    string   `json:"status"`
			Selection Protocol `json:"selection"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(c.response, &response); err != nil {
		return err
	}
	if response.Op != "HELLO" || response.Protocol == nil || *response.Protocol != *c.command.Protocol || response.Payload.Status != "ACCEPTED" || response.Payload.Selection != *c.command.Protocol {
		return fsapi.Permission
	}
	if t.runtimeHellos == nil {
		t.runtimeHellos = map[string]runtimeHelloWitness{}
	}
	// Replays cannot replace the original protected witness with another cell.
	if _, exists := t.runtimeHellos[c.command.Context.Session]; !exists {
		t.runtimeHellos[c.command.Context.Session] = runtimeHelloWitness{Cell: c.frame.Cell, CommandHash: digest(c.body), ResponseHash: digest(c.response)}
	}
	return nil
}

func validateRuntimeHellos(index ProtectedTransportState) error {
	if len(index.RuntimeHellos) > len(index.Scopes) {
		return fsapi.Stale
	}
	for session, w := range index.RuntimeHellos {
		known := false
		for i, scope := range index.Scopes {
			if scope.Session == session && i < len(index.Protocols) && index.Protocols[i] != nil && IsMappedProtocol(*index.Protocols[i]) {
				known = true
			}
		}
		if !known || w.Cell == 0 || w.Cell > index.High || !hash(w.CommandHash) || !hash(w.ResponseHash) {
			return fsapi.Stale
		}
	}
	return nil
}

// CheckControlledSessions checks the actual still-owned durable endpoint. Exact
// complete topology/controller/protocol and all committed worker HELLO witnesses
// are required. It does not establish guest physical/source/native facts; the
// runtime must independently refresh those immediately before reporting Ready.
func (t *Transport) CheckControlledSessions(ctx context.Context, controller fsapi.UUID, scopes []Context, protocol Protocol) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.failed != nil || t.ledger == nil || t.service.controller != controller || !IsMappedProtocol(protocol) || !reflect.DeepEqual(scopes, t.service.config.Scopes) {
		return fsapi.Permission
	}
	t.service.mu.Lock()
	defer t.service.mu.Unlock()
	if len(t.runtimeHellos) != len(scopes) {
		return fsapi.WouldBlock
	}
	for _, scope := range scopes {
		ss := t.service.sessions[scope.Session]
		if ss == nil || ss.Context != scope || !ss.Hello || t.service.config.sessionProtocol(scope.Session) == nil || *t.service.config.sessionProtocol(scope.Session) != protocol {
			return fsapi.Permission
		}
		if _, ok := t.runtimeHellos[scope.Session]; !ok {
			return fsapi.WouldBlock
		}
	}
	// Re-read the actual owned index, not an externally decoded Inspect result.
	// A failed commit fences t before this method can observe a tentative map.
	return t.ledger.db.View(func(tx *bolt.Tx) error {
		if tx.Bucket(transportBucket) == nil {
			return fsapi.Stale
		}
		var protected ProtectedTransportState
		if err := decodeTransportJSON(tx.Bucket(transportBucket).Get(transportIndexKey), transportIndexLimit, &protected); err != nil {
			return err
		}
		if protected.Controller != controller || protected.Endpoint != t.Endpoint() || !reflect.DeepEqual(protected.RuntimeHellos, t.runtimeHellos) {
			return fsapi.Stale
		}
		return validateRuntimeHellos(protected)
	})
}
