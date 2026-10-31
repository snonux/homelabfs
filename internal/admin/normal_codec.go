package admin

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// NormalObservation is a representation-only v3 status object. Frontier is a
// complete canonical normal3 frontier record, base64 encoded by JSON. No current
// admin server emits it; it cannot claim implemented normal publication.
type NormalObservation struct {
	Version           uint16 `json:"version"`
	NormalPublication bool   `json:"normal_publication"`
	Frontier          []byte `json:"frontier"`
}

// EncodeNormalObservation returns one bounded canonical JSON object. These
// exact three keys are mandatory; the existing admin 1.0 protocol is unchanged.
func EncodeNormalObservation(v NormalObservation) ([]byte, error) {
	if v.Version != 3 || v.NormalPublication {
		return nil, fmt.Errorf("unsupported normal observation version/publication")
	}
	r, err := record.DecodeNormal(v.Frontier)
	if err != nil {
		return nil, err
	}
	if _, ok := r.Payload.(record.NormalFrontier); !ok {
		return nil, fmt.Errorf("normal observation requires exact frontier record")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) > maxFrame {
		return nil, fmt.Errorf("normal observation exceeds admin bound")
	}
	return b, nil
}

// DecodeNormalObservation accepts only exact canonical bytes, rejecting aliases,
// duplicates, nulls, unknown fields and trailing values before use.
func DecodeNormalObservation(data []byte) (NormalObservation, error) {
	var v NormalObservation
	if len(data) > maxFrame {
		return v, fmt.Errorf("normal observation exceeds admin bound")
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, err
	}
	b, err := EncodeNormalObservation(v)
	if err != nil {
		return NormalObservation{}, err
	}
	if !bytes.Equal(b, data) {
		return NormalObservation{}, fmt.Errorf("noncanonical normal observation")
	}
	return v, nil
}
