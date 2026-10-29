package sim

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type transactionObjectEvidence struct {
	ID    fsapi.ObjectID
	State TransactionObject
}
type transactionApplyEvidence struct {
	ID           fsapi.TransactionID
	Participants map[fsapi.UUID]uint64
}

// MarshalJSON preserves object identities as explicit values; Go struct map
// keys are not supported by encoding/json. Evidence remains machine-readable.
func (v TransactionView) MarshalJSON() ([]byte, error) {
	objects := make([]transactionObjectEvidence, 0, len(v.Objects))
	for id, state := range v.Objects {
		objects = append(objects, transactionObjectEvidence{ID: id, State: state})
	}
	sort.Slice(objects, func(i, j int) bool {
		a, b := objects[i].ID, objects[j].ID
		if a.Home != b.Home {
			return bytes.Compare(a.Home[:], b.Home[:]) < 0
		}
		return bytes.Compare(a.Allocation[:], b.Allocation[:]) < 0
	})
	return json.Marshal(struct {
		Generation uint64
		Names      map[string]fsapi.ObjectID
		Objects    []transactionObjectEvidence
	}{v.Generation, v.Names, objects})
}

// MarshalJSON preserves per-ID apply counts without unsupported struct map keys.
func (o TransactionObservation) MarshalJSON() ([]byte, error) {
	applies := make([]transactionApplyEvidence, 0, len(o.Applies))
	for id, counts := range o.Applies {
		applies = append(applies, transactionApplyEvidence{ID: id, Participants: counts})
	}
	sort.Slice(applies, func(i, j int) bool { return fmt.Sprint(applies[i].ID) < fmt.Sprint(applies[j].ID) })
	// UUID implements String but not TextMarshaler; convert the inner map too.
	type count struct {
		Participant fsapi.UUID
		Count       uint64
	}
	type applied struct {
		ID           fsapi.TransactionID
		Participants []count
	}
	exported := make([]applied, 0, len(applies))
	for _, a := range applies {
		entry := applied{ID: a.ID}
		for member, n := range a.Participants {
			entry.Participants = append(entry.Participants, count{Participant: member, Count: n})
		}
		sort.Slice(entry.Participants, func(i, j int) bool {
			a, b := entry.Participants[i].Participant, entry.Participants[j].Participant
			return bytes.Compare(a[:], b[:]) < 0
		})
		exported = append(exported, entry)
	}
	return json.Marshal(struct {
		View    TransactionView
		Frames  any
		Applies []applied
		Gated   bool
		Ordinal uint64
	}{o.View, o.Frames, exported, o.Gated, o.Ordinal})
}
