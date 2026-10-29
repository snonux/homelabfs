package sim

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
)

// FileState is the independent namespace/data and stable Unix metadata view.
// Native inode numbers, service allocation IDs, revisions and times are outside
// this projection. The root is named "."; workload entries are regular files.
type FileState struct {
	Kind     fsapi.Kind
	Mode     uint16
	UID, GID uint32
	Nlink    uint64
	Size     int64
	Data     []byte
}

// AppliedEffect correlates a recovered committed journal mutation with its
// external invocation. Intent must be decoded from that mutation's arguments,
// not copied from the expected history. ID correlation is an adapter seam.
type AppliedEffect struct {
	ID        string
	Operation fsapi.UUID
	Source    record.FrameRef
	Intent    Intent
	// WriteOffset is the actual selected journal byte offset. Append effects
	// require this evidence even when the offset is zero; other effects omit it.
	WriteOffset *uint64
}

// Observation is one quiescent replica at an exact selected committed cut.
// Through must identify the actual recovered applied frame, including its hash.
// Effects excludes structural records, pins and barriers, retaining mutations.
type Observation struct {
	Replica string
	Through record.FrameRef
	Effects []AppliedEffect
	Files   map[string]FileState
}

// CheckHistory validates a complete recovered volume against external intent.
// The checkpoint must cover all supplied invocations. It accepts one allowed
// uncertain subset only when all observations have that same ordered history,
// exact cut and independently computed bytes and metadata. It never infers
// correctness from replicas agreeing with one another.
func CheckHistory(volume fsapi.UUID, baseline map[string]FileState, entries []HistoryEntry, observations []Observation) error {
	if !volume.Valid() || len(entries) > 256 || len(observations) < 1 || len(observations) > 16 {
		return fmt.Errorf("invalid history check bounds")
	}
	known, err := indexHistory(volume, entries)
	if err != nil {
		return err
	}
	first := observations[0]
	if len(first.Effects) > 256 {
		return fmt.Errorf("recovered effect budget")
	}
	p := first.Through.Position
	if p.Volume != volume || !p.Generation.Valid() || !p.Configuration.Valid() || p.Epoch == 0 || p.Sequence == 0 || first.Through.StreamKind != 1 || first.Through.Reporter != nil || first.Through.ReporterIncarnation != nil || first.Through.SHA256 == (record.Hash{}) {
		return fmt.Errorf("missing exact committed cut")
	}
	if err := validateEffectSources(first); err != nil {
		return err
	}
	expected, err := replayHistory(baseline, known, first.Effects)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, o := range observations {
		if o.Replica == "" || seen[o.Replica] {
			return fmt.Errorf("duplicate or empty replica")
		}
		seen[o.Replica] = true
		if !reflect.DeepEqual(o.Through, first.Through) || !sameEffects(o.Effects, first.Effects) {
			return fmt.Errorf("replica %s: divergent history or unequal committed cut", o.Replica)
		}
		if err := compareFiles(expected, o.Files); err != nil {
			return fmt.Errorf("replica %s: %w", o.Replica, err)
		}
	}
	return nil
}

func sameEffects(a, b []AppliedEffect) bool {
	return slices.EqualFunc(a, b, func(x, y AppliedEffect) bool {
		return x.ID == y.ID && x.Operation == y.Operation && reflect.DeepEqual(x.Source, y.Source) && sameIntent(x.Intent, y.Intent) && reflect.DeepEqual(x.WriteOffset, y.WriteOffset)
	})
}

func indexHistory(volume fsapi.UUID, entries []HistoryEntry) (map[string]HistoryEntry, error) {
	known := make(map[string]HistoryEntry)
	var prior uint64
	var priorAt time.Duration
	bytes := 0
	for _, e := range entries {
		if e.Volume != volume {
			continue
		}
		bytes += len(e.Intent.Data)
		if e.ID == "" || len(e.ID) > 128 || e.Client == "" || len(e.Client) > 128 || len(e.Result) > 1024 || len(e.Intent.Data) > 4096 || bytes > 1<<20 || e.Invoked == 0 || e.Invoked <= prior || e.Completed != 0 && e.Completed <= e.Invoked || e.Outcome > Rejected || e.Completed == 0 && e.Outcome != Uncertain {
			return nil, fmt.Errorf("invalid or overlapping history entry %s", e.ID)
		}
		if _, exists := known[e.ID]; exists {
			return nil, fmt.Errorf("duplicate history ID %s", e.ID)
		}
		if e.InvokedAt < priorAt || e.Completed != 0 && e.CompletedAt < e.InvokedAt || sha256.Sum256(e.Intent.Data) != e.PayloadSHA256 {
			return nil, fmt.Errorf("invalid time or payload checksum %s", e.ID)
		}
		if err := validateIntent(e.Intent); err != nil {
			return nil, err
		}
		known[e.ID] = e
		prior = e.Completed
		priorAt = e.CompletedAt
		if prior == 0 {
			prior = ^uint64(0)
		}
	}
	return known, nil
}

func validateEffectSources(o Observation) error {
	prior := uint64(0)
	operations := map[fsapi.UUID]bool{}
	for _, effect := range o.Effects {
		p, cut := effect.Source.Position, o.Through.Position
		sequence := p.Sequence
		p.Sequence = cut.Sequence
		if !effect.Operation.Valid() || operations[effect.Operation] || effect.Source.StreamKind != 1 || effect.Source.Reporter != nil || effect.Source.ReporterIncarnation != nil || p != cut || sequence <= prior || sequence > cut.Sequence || effect.Source.SHA256 == (record.Hash{}) {
			return fmt.Errorf("invalid, duplicate or unordered recovered source for %s", effect.ID)
		}
		if sequence == cut.Sequence && !reflect.DeepEqual(effect.Source, o.Through) {
			return fmt.Errorf("recovered source disagrees with exact cut for %s", effect.ID)
		}
		operations[effect.Operation], prior = true, sequence
	}
	return nil
}

func replayHistory(baseline map[string]FileState, known map[string]HistoryEntry, effects []AppliedEffect) (map[string]FileState, error) {
	if len(effects) > len(known) {
		return nil, fmt.Errorf("duplicate or uninvoked effects")
	}
	if err := validateView(baseline); err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	state := maps.Clone(baseline)
	if root, ok := state["."]; !ok || root.Kind != fsapi.Directory {
		return nil, fmt.Errorf("missing declared root baseline")
	}
	seen := map[string]bool{}
	var prior uint64
	for _, effect := range effects {
		e, ok := known[effect.ID]
		if !ok || seen[effect.ID] || e.Outcome == Rejected || e.Invoked <= prior {
			return nil, fmt.Errorf("duplicate, reordered, rejected or uninvoked effect %s", effect.ID)
		}
		if !sameIntent(effect.Intent, e.Intent) {
			return nil, fmt.Errorf("changed recovered intent %s", effect.ID)
		}
		if err := validateWriteOffset(state, e.Intent, effect.WriteOffset); err != nil {
			return nil, fmt.Errorf("effect %s: %w", effect.ID, err)
		}
		seen[effect.ID], prior = true, e.Invoked
		if err := applyIntent(state, e.Intent); err != nil {
			return nil, fmt.Errorf("effect %s: %w", effect.ID, err)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(known)) {
		e := known[id]
		if e.Outcome == Acknowledged && !seen[id] {
			return nil, fmt.Errorf("acknowledged effect lost: %s", id)
		}
	}
	return state, nil
}

func validateWriteOffset(state map[string]FileState, intent Intent, offset *uint64) error {
	if intent.Kind != AppendFile {
		if offset != nil {
			return fmt.Errorf("write offset on non-append effect")
		}
		return nil
	}
	if offset == nil {
		return fmt.Errorf("missing selected append offset")
	}
	f, exists := state[intent.Name]
	if !exists || f.Kind != fsapi.Regular {
		return fmt.Errorf("missing regular file %s", intent.Name)
	}
	want := uint64(len(f.Data))
	if *offset != want {
		return fmt.Errorf("selected append offset %d differs from independent length %d", *offset, want)
	}
	return nil
}

func sameIntent(a, b Intent) bool {
	return a.Kind == b.Kind && a.Name == b.Name && a.Destination == b.Destination && a.Mode == b.Mode && a.UID == b.UID && a.GID == b.GID && bytes.Equal(a.Data, b.Data)
}

func applyIntent(state map[string]FileState, i Intent) error {
	f, exists := state[i.Name]
	if i.Kind == CreateFile {
		if exists {
			return fmt.Errorf("exclusive create already exists")
		}
		state[i.Name] = FileState{Kind: fsapi.Regular, Mode: i.Mode, UID: i.UID, GID: i.GID, Nlink: 1}
		return nil
	}
	if !exists || f.Kind != fsapi.Regular {
		return fmt.Errorf("missing regular file %s", i.Name)
	}
	switch i.Kind {
	case AppendFile:
		if len(f.Data)+len(i.Data) > 1<<20 {
			return fmt.Errorf("oracle data budget")
		}
		f.Data = append(slices.Clone(f.Data), i.Data...)
		f.Size, f.Mode = int64(len(f.Data)), f.Mode&^0o6000
	case ChmodFile:
		f.Mode = i.Mode
	case RenameFile:
		if _, occupied := state[i.Destination]; occupied {
			return fmt.Errorf("rename destination exists")
		}
		delete(state, i.Name)
		state[i.Destination] = f
		return nil
	case RemoveFile:
		delete(state, i.Name)
		return nil
	default:
		return fmt.Errorf("unsupported effect")
	}
	state[i.Name] = f
	return nil
}

func compareFiles(expected, actual map[string]FileState) error {
	if err := validateView(actual); err != nil {
		return err
	}
	if len(expected) != len(actual) {
		return fmt.Errorf("namespace differs: expected %d entries, observed %d", len(expected), len(actual))
	}
	names := slices.Sorted(maps.Keys(expected))
	for _, name := range names {
		want := expected[name]
		got, ok := actual[name]
		if !ok || want.Kind != got.Kind || want.Mode != got.Mode || want.UID != got.UID || want.GID != got.GID || want.Nlink != got.Nlink || want.Size != got.Size || !bytes.Equal(want.Data, got.Data) {
			return fmt.Errorf("data or metadata differs at %q", name)
		}
	}
	return nil
}

func validateView(files map[string]FileState) error {
	if len(files) > 257 {
		return fmt.Errorf("namespace observation budget")
	}
	bytes := 0
	for name, f := range files {
		bytes += len(f.Data)
		if bytes > 1<<20 || f.Mode & ^uint16(0o7777) != 0 || f.Size < 0 {
			return fmt.Errorf("data or metadata observation budget")
		}
		if name == "." {
			if f.Kind != fsapi.Directory || len(f.Data) != 0 {
				return fmt.Errorf("invalid root metadata")
			}
		} else if f.Kind != fsapi.Regular || f.Size != int64(len(f.Data)) {
			return fmt.Errorf("invalid file data or metadata")
		}
	}
	return nil
}
