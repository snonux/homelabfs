package normalpublic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// Ledger owns one exclusively locked local journal. Every accepted transition is
// fully synced before it becomes queryable; any ambiguous I/O poisons this handle.
// Proof providers must not reenter the ledger while a transition is verified.
type Ledger struct {
	mu         sync.Mutex
	config     Config
	journal    *journal
	anchors    map[string]*anchorState
	operations map[record.UUID]struct{}
	entries    int
	sequence   uint64
	last       record.Hash
}

type anchorState struct {
	floor, allocated uint64
	entries          map[uint64]Entry
}

type event struct {
	Allocations []Allocation
	Ledger      record.UUID
	Sequence    uint64
	Previous    record.Hash
	Action      string
	Scope       Scope
	Anchor      Anchor
	Number      uint64
	Original    *record.Hash
	Binding     *Binding
	Protection  *Protection
	Evidence    []byte
}

// Open opens or creates a private journal and requires verified complete recovery
// before exposing any old claims. A torn/corrupt tail blocks, without truncation.
func Open(ctx context.Context, path string, config Config) (*Ledger, error) {
	if !config.LedgerID.Valid() || nilProvider(config.Authentication) || nilProvider(config.Admission) || nilProvider(config.Retention) || config.Limits.Anchors <= 0 || config.Limits.Anchors > 4096 || config.Limits.Entries <= 0 || config.Limits.Entries > 4096 || config.Limits.GraphFrames <= 0 || config.Limits.GraphFrames > 4096 || config.Limits.JournalBytes < 48 || config.Limits.JournalBytes > 256<<20 {
		return nil, fmt.Errorf("public ledger requires proofs and finite limits")
	}
	j, data, err := openJournal(ctx, path, config.Limits.JournalBytes)
	if err != nil {
		return nil, err
	}
	l := &Ledger{config: config, journal: j, anchors: make(map[string]*anchorState), operations: make(map[record.UUID]struct{})}
	if err := l.restore(ctx, data); err != nil {
		_ = j.close()
		return nil, err
	}
	if err := config.Retention.VerifyRecovery(ctx, config.LedgerID, append([]byte(nil), data...)); err != nil {
		_ = j.close()
		return nil, fmt.Errorf("public recovery proof: %w", err)
	}
	return l, nil
}

// Close releases the file and exclusive lock; it grants no claim retirement.
func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.journal.close()
}

// Resolve authenticates the anchor and resolves a known claim before any fresh
// capability lookup, origin selection or admission. Unknown is never NO_EFFECT.
func (l *Ledger) Resolve(ctx context.Context, scope Scope, claim Claim) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.journal.check(ctx); err != nil {
		return Entry{}, err
	}
	if err := validateAnchor(claim.Anchor); err != nil {
		return Entry{}, err
	}
	if claim.Number == 0 {
		return Entry{}, fmt.Errorf("public number zero")
	}
	if err := l.config.Authentication.Lookup(ctx, scope, clone(claim.Anchor)); err != nil {
		return Entry{}, err
	}
	a := l.anchors[anchorKey(claim.Anchor)]
	if a == nil {
		return Entry{Number: claim.Number, Status: Unknown}, nil
	}
	if claim.Number <= a.floor {
		return Entry{Number: claim.Number, Status: Retired}, ErrRetired
	}
	entry, ok := a.entries[claim.Number]
	if !ok {
		return Entry{Number: claim.Number, Status: Unknown}, nil
	}
	if entry.Original != claim.Original {
		return Entry{}, ErrConflict
	}
	return clone(entry), nil
}

// Allocate durably records every allocated number, including unsent and later
// pre-admission-rejected requests. The anchor verifier proves exact serialized
// allocation; an exporter counter alone cannot skip a gap or reset the namespace.
func (l *Ledger) Allocate(ctx context.Context, scope Scope, anchor Anchor, allocations []Allocation, evidence []byte) error {
	if len(allocations) == 0 || len(allocations) > l.config.Limits.Entries {
		return ErrBlocked
	}
	_, err := l.transact(ctx, event{Action: "allocate", Scope: scope, Anchor: anchor, Number: allocations[len(allocations)-1].Number, Allocations: allocations, Evidence: evidence})
	return err
}

// Reserve returns the immutable existing binding before fresh Admission checks.
// A retry from another exporter/O must use Resolve's binding; attempting to
// overwrite the original scope, credentials, intent, operation or O conflicts.
func (l *Ledger) Reserve(ctx context.Context, binding Binding) (Entry, error) {
	return l.transact(ctx, event{Action: "reserve", Scope: binding.Scope, Anchor: binding.Claim.Anchor, Number: binding.Claim.Number, Binding: &binding})
}

// Protect persists the exact verified terminal result graph. DECLINE is terminal
// only when selected and protected by the same proof boundary as COMMIT.
func (l *Ledger) Protect(ctx context.Context, scope Scope, claim Claim, protection Protection) (Entry, error) {
	return l.transact(ctx, event{Action: "protect", Scope: scope, Anchor: claim.Anchor, Number: claim.Number, Original: &claim.Original, Protection: &protection})
}

// Cancel records a waiting/cancellation request only; no effect is inferred.
// A selected protected DECLINE must arrive through Protect to become terminal.
func (l *Ledger) Cancel(ctx context.Context, scope Scope, claim Claim) (Entry, error) {
	return l.transact(ctx, event{Action: "cancel", Scope: scope, Anchor: claim.Anchor, Number: claim.Number, Original: &claim.Original})
}

// MarkIndeterminate preserves the original binding across transport uncertainty.
func (l *Ledger) MarkIndeterminate(ctx context.Context, scope Scope, claim Claim) (Entry, error) {
	return l.transact(ctx, event{Action: "unknown", Scope: scope, Anchor: claim.Anchor, Number: claim.Number, Original: &claim.Original})
}

// ProtectGap permanently resolves an allocated UNSENT number with authenticated
// positive no-effect evidence. It cannot erase an admitted or uncertain binding.
func (l *Ledger) ProtectGap(ctx context.Context, scope Scope, anchor Anchor, number uint64, evidence []byte) (Entry, error) {
	return l.transact(ctx, event{Action: "gap", Scope: scope, Anchor: anchor, Number: number, Evidence: evidence})
}

// Retire advances a permanent floor only over exact terminal/gap coverage. All
// original bytes and operation identities remain retained in this bounded version;
// source release/checkpoint equivalence belongs to the distributed coordinator.
func (l *Ledger) Retire(ctx context.Context, scope Scope, anchor Anchor, through uint64, evidence []byte) error {
	_, err := l.transact(ctx, event{Action: "retire", Scope: scope, Anchor: anchor, Number: through, Evidence: evidence})
	return err
}

func (l *Ledger) transact(ctx context.Context, input event) (Entry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.journal.check(ctx); err != nil {
		return Entry{}, err
	}
	e := clone(input)
	e.Ledger = l.config.LedgerID
	e.Sequence, e.Previous = l.sequence+1, l.last
	if e.Sequence == 0 {
		return Entry{}, ErrBlocked
	}
	entry, commit, err := l.transition(ctx, e, true)
	if err != nil {
		return Entry{}, err
	}
	if commit == nil {
		return clone(entry), nil
	}
	data, err := envelope("HFSPLOG1", e)
	if err != nil {
		return Entry{}, err
	}
	if err := l.journal.append(ctx, data); err != nil {
		return Entry{}, err
	}
	commit()
	l.sequence, l.last = e.Sequence, sha256.Sum256(data)
	return clone(entry), nil
}

func (l *Ledger) transition(ctx context.Context, e event, verify bool) (Entry, func(), error) {
	if err := validateAnchor(e.Anchor); err != nil {
		return Entry{}, nil, err
	}
	if e.Number == 0 || len(e.Evidence) > 1<<20 {
		return Entry{}, nil, fmt.Errorf("public transition number/evidence bound")
	}
	if verify {
		if err := l.config.Authentication.Lookup(ctx, e.Scope, clone(e.Anchor)); err != nil {
			return Entry{}, nil, err
		}
	}
	key := anchorKey(e.Anchor)
	a := l.anchors[key]
	if e.Action == "allocate" {
		return l.allocation(ctx, e, key, a, verify)
	}
	if len(e.Allocations) != 0 {
		return Entry{}, nil, fmt.Errorf("public nonallocation shape")
	}
	if a == nil {
		return Entry{Number: e.Number, Status: Unknown}, nil, ErrBlocked
	}
	if e.Number <= a.floor {
		return Entry{Number: e.Number, Status: Retired}, nil, ErrRetired
	}
	if e.Action == "retire" {
		return l.retirement(ctx, e, a, verify)
	}
	entry, ok := a.entries[e.Number]
	if !ok {
		return Entry{Number: e.Number, Status: Unknown}, nil, ErrBlocked
	}
	if e.Original != nil && *e.Original != entry.Original {
		return Entry{}, nil, ErrConflict
	}
	before := clone(entry)
	if err := l.update(ctx, e, &entry, verify); err != nil {
		return Entry{}, nil, err
	}
	if equal(before, entry) {
		return entry, nil, nil
	}
	return entry, func() {
		a.entries[e.Number] = entry
		if e.Action == "reserve" {
			l.operations[e.Binding.Operation] = struct{}{}
		}
	}, nil
}

func (l *Ledger) allocation(ctx context.Context, e event, key string, a *anchorState, verify bool) (Entry, func(), error) {
	if e.Original != nil || e.Binding != nil || e.Protection != nil || len(e.Evidence) == 0 {
		return Entry{}, nil, fmt.Errorf("public allocation shape")
	}
	if a == nil {
		if len(l.anchors) >= l.config.Limits.Anchors {
			return Entry{}, nil, ErrBlocked
		}
		a = &anchorState{entries: make(map[uint64]Entry)}
	}
	if e.Number <= a.allocated || e.Number-a.allocated != uint64(len(e.Allocations)) || len(e.Allocations) > l.config.Limits.Entries-l.entries {
		return Entry{}, nil, ErrBlocked
	}
	for i, allocation := range e.Allocations {
		if allocation.Number != a.allocated+uint64(i)+1 {
			return Entry{}, nil, ErrConflict
		}
	}
	if verify {
		if err := l.config.Authentication.Allocation(ctx, e.Scope, clone(e.Anchor), a.allocated, clone(e.Allocations), append([]byte(nil), e.Evidence...)); err != nil {
			return Entry{}, nil, err
		}
	}
	return Entry{}, func() {
		for _, allocation := range e.Allocations {
			a.entries[allocation.Number] = Entry{Number: allocation.Number, Original: allocation.Original, Status: Unsent}
			l.entries++
		}
		a.allocated = e.Number
		l.anchors[key] = a
	}, nil
}

func (l *Ledger) update(ctx context.Context, e event, entry *Entry, verify bool) error {
	switch e.Action {
	case "reserve":
		if e.Original != nil || e.Binding == nil || e.Protection != nil || len(e.Evidence) != 0 || !equal(e.Binding.Claim.Anchor, e.Anchor) || e.Binding.Claim.Number != e.Number || !equal(e.Binding.Scope, e.Scope) {
			return fmt.Errorf("public reservation shape")
		}
		if err := validateBinding(*e.Binding); err != nil {
			return err
		}
		if entry.Binding != nil {
			if !equal(entry.Binding, e.Binding) {
				return ErrConflict
			}
			return nil
		}
		if entry.Original != e.Binding.Claim.Original {
			return ErrConflict
		}
		if entry.Status != Unsent {
			return ErrConflict
		}
		if _, exists := l.operations[e.Binding.Operation]; exists {
			return ErrConflict
		}
		if verify {
			if err := l.config.Admission.VerifyAdmission(ctx, clone(*e.Binding)); err != nil {
				return err
			}
		}
		entry.Binding = e.Binding
		entry.Status = Pending
	case "protect":
		if e.Original == nil || entry.Binding == nil || e.Binding != nil || e.Protection == nil || len(e.Evidence) != 0 || len(e.Protection.Evidence) == 0 || len(e.Protection.Evidence) > 1<<20 {
			return ErrBlocked
		}
		b, result, err := VerifyGraph(e.Protection.Graph, l.config.Limits.GraphFrames)
		if err != nil {
			return err
		}
		if !equal(b, *entry.Binding) {
			return ErrConflict
		}
		if entry.Status == Terminal {
			if !equal(entry.Protection, e.Protection) {
				return ErrConflict
			}
			return nil
		}
		if verify {
			if err := l.config.Retention.VerifyProtection(ctx, clone(b), clone(*e.Protection)); err != nil {
				return err
			}
		}
		entry.Status = Terminal
		entry.Result = result
		entry.Protection = e.Protection
	case "cancel", "unknown":
		if e.Original == nil || e.Binding != nil || e.Protection != nil || len(e.Evidence) != 0 {
			return fmt.Errorf("public uncertainty shape")
		}
		if entry.Binding == nil {
			return ErrBlocked
		}
		if entry.Status == Terminal {
			return nil
		}
		if e.Action == "cancel" {
			entry.Status = CancelRequested
		} else if entry.Status != CancelRequested {
			entry.Status = Indeterminate
		}
	case "gap":
		if e.Original != nil || e.Binding != nil || e.Protection != nil || len(e.Evidence) == 0 {
			return fmt.Errorf("public gap shape")
		}
		if entry.Status != Unsent {
			return ErrConflict
		}
		if verify {
			if err := l.config.Retention.VerifyGap(ctx, e.Scope, clone(e.Anchor), e.Number, append([]byte(nil), e.Evidence...)); err != nil {
				return err
			}
		}
		entry.Status = Gap
		entry.GapEvidence = e.Evidence
	default:
		return fmt.Errorf("unsupported public ledger transition")
	}
	return nil
}

func (l *Ledger) retirement(ctx context.Context, e event, a *anchorState, verify bool) (Entry, func(), error) {
	if e.Original != nil || e.Binding != nil || e.Protection != nil || len(e.Evidence) == 0 || e.Number > a.allocated || e.Number-a.floor > 1024 {
		return Entry{}, nil, ErrBlocked
	}
	r := Retirement{Anchor: e.Anchor, Previous: a.floor, Through: e.Number, Evidence: e.Evidence}
	for n := a.floor + 1; ; n++ {
		entry, ok := a.entries[n]
		if !ok || entry.Status != Terminal && entry.Status != Gap {
			return Entry{}, nil, ErrBlocked
		}
		r.Entries = append(r.Entries, entry)
		if n == e.Number {
			break
		}
	}
	if verify {
		if err := l.config.Retention.VerifyRetirement(ctx, e.Scope, clone(r)); err != nil {
			return Entry{}, nil, err
		}
	}
	return Entry{Number: e.Number, Status: Retired}, func() { a.floor = e.Number }, nil
}

func anchorKey(a Anchor) string {
	data, _ := json.Marshal(a)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// clone only receives concrete package types whose JSON representation is total.
func clone[T any](value T) T {
	data, _ := json.Marshal(value)
	var result T
	_ = json.Unmarshal(data, &result)
	return result
}

func nilProvider(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return v.IsNil()
	}
	return false
}
