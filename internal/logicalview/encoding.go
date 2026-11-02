package logicalview

import (
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

type stageWire struct {
	Slot                    record.OrderedSlot
	Previous                *record.SelectedRef
	Origin                  record.OriginStream
	OriginLaunch, Recipient record.Launch
	Claim                   fsapi.UUID
	Intent                  []byte
	Operations              [][]byte
	Dependencies            []Dependency
}
type snapshotWire struct {
	Catalog []byte
	Data    [][]byte
}
type versionWire struct {
	Cut    *record.SelectedRef
	Images []snapshotWire
	Pins   int
}

// MarshalJSON preserves byte-exact primitive names through the private transfer
// encoding. It does not define a normal journal or networking format.
func (s Stage) MarshalJSON() ([]byte, error) {
	wire := stageWire{Slot: s.Slot, Previous: s.Previous, Origin: s.Origin, OriginLaunch: s.OriginLaunch, Recipient: s.Recipient, Claim: s.Claim, Intent: s.Intent, Dependencies: s.Dependencies}
	for _, op := range s.Operations {
		raw, err := storage.EncodeOperation(op)
		if err != nil {
			return nil, err
		}
		wire.Operations = append(wire.Operations, raw)
	}
	return json.Marshal(wire)
}

// UnmarshalJSON restores exact primitive names. Restore additionally requires
// bounded canonical outer bytes and validates every semantic snapshot/effect.
func (s *Stage) UnmarshalJSON(raw []byte) error {
	var wire stageWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	stage := Stage{Slot: wire.Slot, Previous: wire.Previous, Origin: wire.Origin, OriginLaunch: wire.OriginLaunch, Recipient: wire.Recipient, Claim: wire.Claim, Intent: wire.Intent, Dependencies: wire.Dependencies}
	for _, raw := range wire.Operations {
		op, err := storage.DecodeOperation(raw, int64(len(raw)))
		if err != nil {
			return err
		}
		stage.Operations = append(stage.Operations, op)
	}
	*s = stage
	return nil
}

// MarshalJSON embeds the canonical catalog codec so non-UTF8 names and symlink
// text survive transfer without JSON's Unicode replacement behavior.
func (v RecoveryVersion) MarshalJSON() ([]byte, error) {
	wire := versionWire{Cut: v.Cut, Pins: v.Pins}
	for _, image := range v.Images {
		raw, err := storage.EncodeImage(image.Image, transferLimits())
		if err != nil {
			return nil, err
		}
		wire.Images = append(wire.Images, snapshotWire{Catalog: raw, Data: image.Data})
	}
	return json.Marshal(wire)
}

// UnmarshalJSON decodes byte-exact catalogs; configured limits are rechecked by
// Restore before any resulting version becomes available.
func (v *RecoveryVersion) UnmarshalJSON(raw []byte) error {
	var wire versionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	version := RecoveryVersion{Cut: wire.Cut, Pins: wire.Pins}
	for _, image := range wire.Images {
		catalog, err := storage.DecodeImage(image.Catalog, transferLimits())
		if err != nil {
			return err
		}
		version.Images = append(version.Images, storage.Snapshot{Image: catalog, Data: image.Data})
	}
	*v = version
	return nil
}
func transferLimits() storage.Limits {
	return storage.Limits{Objects: 1 << 20, Entries: 1 << 20, Operations: 1 << 20, Pins: 1 << 20, FileBytes: 1 << 30}
}
