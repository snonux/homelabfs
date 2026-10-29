package sim

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// TransactionInput is independently retained workload input. Paths are relative
// to fixed volume roots; Data is used only by a synthetic home write. This is a
// simulator workload, not an implementation of the public transaction service.
type TransactionInput struct {
	ID                              fsapi.TransactionID
	Kind                            string
	SourceVolume, DestinationVolume fsapi.UUID
	Source, Destination             string
	Data                            []byte
	ExpectedSource                  *fsapi.ObjectID
	Denied                          bool
}

// TransactionObject is the external observable projection of one home object.
// Pins are nonexpiring synthetic handle/snapshot obligations, not kernel pins.
type TransactionObject struct {
	Type        uint8
	Hash        [32]byte
	Size, Nlink uint64
	Parent      fsapi.ObjectID
	Pins        uint64
}

// TransactionView is one atomic synthetic visibility generation. Every visible
// path names a stable object; Objects includes unlinked objects still pinned.
type TransactionView struct {
	Generation uint64
	Names      map[string]fsapi.ObjectID
	Objects    map[fsapi.ObjectID]TransactionObject
}

// TransactionInvocation is recorder-owned input, including the complete original
// bytes saved before execution. A lost reply is not an abort assertion.
type TransactionInvocation struct {
	Input       TransactionInput
	Canonical   []byte
	Digest      record.Hash
	ReplyLost   bool
	Completion  string
	CompletedAt uint64
	Reply       *record.TerminalResult
}

// TransactionObservation comes from actual retained engine frames and private
// apply observations. Published views never come from this checker's model.
type TransactionObservation struct {
	View    TransactionView
	Frames  []segment.Entry
	Applies map[fsapi.TransactionID]map[fsapi.UUID]uint64
	Gated   bool
	Ordinal uint64
}

type transactionFrameIdentity struct {
	Kind                  uint8
	Reporter, Incarnation fsapi.UUID
	Position              record.Position
	Hash                  record.Hash
}

// CheckTransactions replays independent high-level inputs, checks exact anchored
// identities/results and compares every publication to that replay. Agreement
// between replicas is not an expected outcome. All frames are conservative GC
// pins; terminality or retired IDs do not release them.
func CheckTransactions(baseline TransactionView, history []TransactionInvocation, observations []TransactionObservation) error {
	if len(history) > 32 || len(observations) == 0 || len(observations) > 128 || len(baseline.Names) > 256 || len(baseline.Objects) > 256 {
		return fmt.Errorf("transaction oracle budget")
	}
	for index, observation := range observations {
		if index > 0 {
			if err := checkTransactionPins(observations[index-1].Frames, observation.Frames); err != nil {
				return err
			}
		}
		if err := checkTransactionObservation(baseline, history, observation); err != nil {
			return err
		}
	}
	return nil
}

func checkTransactionObservation(baseline TransactionView, history []TransactionInvocation, o TransactionObservation) error {
	if len(o.View.Names) > 256 || len(o.View.Objects) > 256 || len(o.Frames) > 1024 {
		return fmt.Errorf("transaction observation budget")
	}
	total := 0
	for _, entry := range o.Frames {
		total += len(entry.Bytes)
		if total > 4<<20 {
			return fmt.Errorf("transaction frame byte budget")
		}
	}
	expected := cloneTransactionView(baseline)
	claims := make(map[fsapi.TransactionID]record.RequestClaim)
	results := make(map[fsapi.TransactionID]record.TerminalResult)
	bindings := make(map[fsapi.TransactionID]record.Binding)
	complete := make(map[fsapi.TransactionID]record.Complete)
	begun := make(map[fsapi.TransactionID]bool)
	prepares := make(map[fsapi.TransactionID]map[fsapi.UUID]bool)
	frames := make(map[record.Hash]bool)
	checked, err := checkTransactionGraph(o.Frames)
	if err != nil {
		return err
	}
	for _, entry := range checked {
		decoded, ref := entry.Record, entry.Reference
		frames[ref.SHA256] = true
		switch p := decoded.Payload.(type) {
		case record.RequestClaim:
			id := transactionDocumentID(p.Document)
			if _, exists := claims[id]; exists {
				return fmt.Errorf("oracle double claim")
			}
			claims[id] = p
		case record.Begin:
			begun[p.Binding.Request] = true
		case record.RequestBind:
			bindings[p.Binding.Request] = p.Binding
		case record.Prepared:
			if prepares[p.Request] == nil {
				prepares[p.Request] = make(map[fsapi.UUID]bool)
			}
			prepares[p.Request][ref.Position.Volume] = true
		case record.Complete:
			complete[p.Request] = p
		case record.RequestResult:
			results[p.Request] = p.Result
		}
	}
	for id := range begun {
		if _, done := complete[id]; !done && !o.Gated {
			return fmt.Errorf("oracle pending closure gate open")
		}
	}
	seen := make(map[fsapi.TransactionID]TransactionInvocation)
	for _, invocation := range history {
		id := invocation.Input.ID
		claim, claimed := claims[id]
		result, terminal := results[id]
		c, completed := complete[id]
		acknowledged := invocation.CompletedAt != 0 && invocation.CompletedAt <= o.Ordinal
		if !claimed {
			if acknowledged && (invocation.Completion == "committed" || invocation.Completion == "aborted" || invocation.Completion == "rejected" || invocation.Completion == "conflict") {
				return fmt.Errorf("oracle acknowledged claim missing")
			}
			continue
		}
		original, digest, err := transactionDocumentBytes(claim.Document)
		matches := err == nil && digest == invocation.Digest && slices.Equal(original, invocation.Canonical) && claim.Digest == invocation.Digest
		if !matches {
			if invocation.Completion == "conflict" {
				continue
			}
			return fmt.Errorf("oracle anchored original mismatch")
		}
		// Every completion is checked, including a successful retry after a lost
		// first reply. Only effect replay below is deduplicated by immutable ID.
		if acknowledged {
			switch invocation.Completion {
			case "committed", "aborted", "rejected":
				want := uint8(1)
				if invocation.Completion == "aborted" {
					want = 2
				}
				if invocation.Completion == "rejected" {
					want = 3
				}
				if !terminal || result.Status != want || invocation.Reply == nil || !reflect.DeepEqual(*invocation.Reply, result) {
					return fmt.Errorf("oracle acknowledged terminal result missing or changed")
				}
				if want != 3 && (!completed || !reflect.DeepEqual(c.Result, result)) {
					return fmt.Errorf("oracle acknowledged COMPLETE missing or changed")
				}
				if want == 1 && c.Visibility.Generation > o.View.Generation {
					return fmt.Errorf("oracle acknowledged publication missing")
				}
			case "conflict":
				return fmt.Errorf("oracle conflict reply for matching request")
			case "uncertain":
			default:
				return fmt.Errorf("oracle unknown completion")
			}
		}
		if terminal && result.Status != 3 && !completed {
			return fmt.Errorf("oracle terminal result before COMPLETE")
		}
		if prior, exists := seen[id]; exists {
			if prior.Digest != invocation.Digest || !slices.Equal(prior.Canonical, invocation.Canonical) {
				return fmt.Errorf("oracle double original identity")
			}
			continue
		}
		seen[id] = invocation
		// COMPLETE makes a private generation ready. Only the adapter's explicit
		// protected publication can make it externally visible.
		if !completed || c.Result.Status != 1 || c.Visibility.Generation > o.View.Generation {
			continue
		}
		b, ok := bindings[id]
		if !ok || b.RequestDigest != invocation.Digest {
			return fmt.Errorf("oracle missing immutable binding")
		}
		if len(b.Participants) < 2 || len(b.Authorities) != len(b.Participants) {
			return fmt.Errorf("oracle participant closure")
		}
		for _, a := range b.Authorities {
			if len(a.Members) != 3 {
				return fmt.Errorf("oracle three-node membership")
			}
		}
		if c.Result.Decision == nil || !frames[c.Result.Decision.Decision.SHA256] {
			return fmt.Errorf("oracle missing pinned decision")
		}
		if !reflect.DeepEqual(c.Result, result) && terminal {
			return fmt.Errorf("oracle anchored terminal mismatch")
		}
		for _, member := range b.Participants {
			if !prepares[id][member] || o.Applies[id][member] != 1 {
				return fmt.Errorf("oracle execution count %d for %v/%v", o.Applies[id][member], id, member)
			}
		}
		source, err := replayTransaction(&expected, invocation.Input)
		if err != nil {
			return fmt.Errorf("oracle invalid committed input: %w", err)
		}
		if c.Result.Source == nil || *c.Result.Source != source {
			return fmt.Errorf("oracle wrong stable result object")
		}
		expected.Generation++
	}
	for id := range claims {
		found := false
		for _, invocation := range history {
			if invocation.Input.ID == id {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("oracle uninvoked anchored claim")
		}
	}
	if !reflect.DeepEqual(expected, o.View) {
		return fmt.Errorf("oracle namespace/visibility/home mismatch: expected %+v observed %+v", expected, o.View)
	}
	// Even a closed gate must keep the last whole generation: the comparison above
	// checks the actual observed view after every partial participant operation.
	for name, id := range o.View.Names {
		object, ok := o.View.Objects[id]
		if !ok || id.Home == (fsapi.UUID{}) || object.Type == 0 {
			return fmt.Errorf("oracle dangling home %s", name)
		}
	}
	return nil
}

func replayTransaction(v *TransactionView, input TransactionInput) (fsapi.ObjectID, error) {
	source := input.SourceVolume.String() + "/" + input.Source
	destination := input.DestinationVolume.String() + "/" + input.Destination
	object, ok := v.Names[source]
	if !ok {
		return fsapi.ObjectID{}, fmt.Errorf("source absent")
	}
	state := v.Objects[object]
	if input.Denied || input.ExpectedSource != nil && *input.ExpectedSource != object {
		return object, fmt.Errorf("authorization or expected identity")
	}
	switch input.Kind {
	case "link":
		if state.Type != 1 {
			return object, fmt.Errorf("link type")
		}
		if _, exists := v.Names[destination]; exists {
			return object, fmt.Errorf("destination exists")
		}
		v.Names[destination] = object
		state.Nlink++
	case "rename":
		if state.Type == 2 && strings.HasPrefix(destination, source+"/") {
			return object, fmt.Errorf("directory cycle")
		}
		if old, exists := v.Names[destination]; exists {
			if old == object {
				return object, nil
			}
			previous := v.Objects[old]
			previous.Nlink--
			v.Objects[old] = previous
		}
		delete(v.Names, source)
		v.Names[destination] = object
		if state.Type == 2 {
			for name, child := range maps.Clone(v.Names) {
				if strings.HasPrefix(name, source+"/") {
					delete(v.Names, name)
					v.Names[destination+strings.TrimPrefix(name, source)] = child
				}
			}
			parentPath := destination[:strings.LastIndex(destination, "/")]
			nextParent := v.Names[parentPath]
			old := v.Objects[state.Parent]
			old.Nlink--
			v.Objects[state.Parent] = old
			next := v.Objects[nextParent]
			next.Nlink++
			v.Objects[nextParent] = next
			state.Parent = nextParent
		}
	case "write":
		state.Hash = sha256.Sum256(input.Data)
		state.Size = uint64(len(input.Data))
	case "unlink":
		delete(v.Names, source)
		state.Nlink--
	default:
		return object, fmt.Errorf("unsupported input")
	}
	v.Objects[object] = state
	return object, nil
}

func cloneTransactionView(v TransactionView) TransactionView {
	v.Names = maps.Clone(v.Names)
	v.Objects = maps.Clone(v.Objects)
	return v
}
func transactionDocumentID(d record.RequestDocument) fsapi.TransactionID {
	if d.Class == 1 {
		return d.Original.ID
	}
	return d.Internal.ID
}
func transactionDocumentBytes(d record.RequestDocument) ([]byte, record.Hash, error) {
	if d.Class == 1 {
		raw, err := record.EncodeOriginal(*d.Original)
		return raw, sha256.Sum256(raw), err
	}
	return record.EncodeInternal(*d.Internal)
}

func transactionIdentity(ref record.FrameRef) transactionFrameIdentity {
	key := transactionFrameIdentity{Kind: ref.StreamKind, Position: ref.Position, Hash: ref.SHA256}
	if ref.Reporter != nil {
		key.Reporter = *ref.Reporter
	}
	if ref.ReporterIncarnation != nil {
		key.Incarnation = *ref.ReporterIncarnation
	}
	return key
}

// checkTransactionGraph resolves retained pins independently of engine replay.
// Owner histories are untrimmed; cached typed ACCEPTs deliberately need not
// include reporter OPEN frames delegated to the trusted Protector capability.
func checkTransactionGraph(source []segment.Entry) ([]segment.Entry, error) {
	checked := make([]segment.Entry, 0, len(source))
	opens := make(map[record.UUID]record.SegmentOpen)
	for _, entry := range source {
		decoded, err := record.Decode(entry.Bytes)
		if err != nil {
			return nil, fmt.Errorf("oracle decode: %w", err)
		}
		entry.Record = decoded
		checked = append(checked, entry)
		if open, ok := decoded.Payload.(record.SegmentOpen); ok {
			opens[open.Segment] = open
		}
	}
	exact := make(map[transactionFrameIdentity]segment.Entry)
	owners := make(map[transactionFrameIdentity]map[uint64]segment.Entry)
	positions := make(map[record.Position]bool)
	for _, entry := range checked {
		ref, err := record.ReferenceTo(entry.Record, entry.Bytes)
		if seal, ok := entry.Record.Payload.(record.SegmentSeal); ok {
			open, exists := opens[seal.Segment]
			if !exists {
				return nil, fmt.Errorf("oracle SEAL missing OPEN")
			}
			ref, err = record.SealReference(entry.Record, entry.Bytes, open)
		}
		if err != nil || !reflect.DeepEqual(ref, entry.Reference) {
			return nil, fmt.Errorf("oracle exact frame identity")
		}
		key := transactionIdentity(ref)
		if _, exists := exact[key]; exists {
			return nil, fmt.Errorf("oracle duplicate retained frame")
		}
		exact[key] = entry
		if ref.StreamKind == 1 {
			positions[ref.Position] = true
			stream := key
			stream.Position.Sequence = 0
			stream.Hash = record.Hash{}
			if owners[stream] == nil {
				owners[stream] = make(map[uint64]segment.Entry)
			}
			if _, exists := owners[stream][ref.Position.Sequence]; exists {
				return nil, fmt.Errorf("oracle contradictory owner position")
			}
			owners[stream][ref.Position.Sequence] = entry
		}
	}
	for _, prefix := range owners {
		first, ok := prefix[1]
		if !ok {
			return nil, fmt.Errorf("oracle owner prefix missing first frame")
		}
		open, ok := first.Record.Payload.(record.SegmentOpen)
		if !ok || open.First != 1 || open.PreviousSeal != nil {
			return nil, fmt.Errorf("oracle owner prefix missing initial OPEN")
		}
		for sequence := uint64(1); sequence <= uint64(len(prefix)); sequence++ {
			if _, ok := prefix[sequence]; !ok {
				return nil, fmt.Errorf("oracle owner prefix gap")
			}
		}
		// len+1 catches a suffix with an interior hole even when its maximum is larger.
		for sequence := range prefix {
			if sequence > uint64(len(prefix)) {
				return nil, fmt.Errorf("oracle owner prefix gap")
			}
		}
	}
	lookup := func(ref record.FrameRef) (segment.Entry, error) {
		entry, ok := exact[transactionIdentity(ref)]
		if !ok {
			return entry, fmt.Errorf("oracle missing retained dependency: %+v", ref)
		}
		return entry, nil
	}
	var visit func(reflect.Value) error
	visits := 0
	visit = func(value reflect.Value) error {
		if !value.IsValid() {
			return nil
		}
		visits++
		if visits > 1<<20 {
			return fmt.Errorf("oracle dependency traversal budget")
		}
		if value.Type() == reflect.TypeOf(record.FrameRef{}) {
			_, err := lookup(value.Interface().(record.FrameRef))
			return err
		}
		if value.Type() == reflect.TypeOf(record.Position{}) {
			position := value.Interface().(record.Position)
			if position.Sequence > 0 && !positions[position] {
				return fmt.Errorf("oracle missing owner dependency position: %+v", position)
			}
			return nil
		}
		switch value.Kind() {
		case reflect.Interface, reflect.Pointer:
			if !value.IsNil() {
				return visit(value.Elem())
			}
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				if err := visit(value.Field(i)); err != nil {
					return err
				}
			}
		case reflect.Slice:
			if value.Type().Elem().Kind() == reflect.Uint8 {
				return nil
			}
			for i := 0; i < value.Len(); i++ {
				if err := visit(value.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, entry := range checked {
		if err := visit(reflect.ValueOf(entry.Record.Payload)); err != nil {
			return nil, err
		}
		if err := checkTransactionLinks(entry, lookup); err != nil {
			return nil, err
		}
	}
	return checked, nil
}

func checkTransactionLinks(entry segment.Entry, lookup func(record.FrameRef) (segment.Entry, error)) error {
	switch p := entry.Record.Payload.(type) {
	case record.Install:
		decision, err := lookup(p.Decision.Decision)
		if err != nil {
			return err
		}
		d, ok := decision.Record.Payload.(record.Decision)
		if !ok || d.ID != p.Decision.ID || d.ID.Request != p.Request || d.Digest != p.Digest || d.Outcome != p.Outcome {
			return fmt.Errorf("oracle INSTALL decision mismatch")
		}
		if p.Outcome == 1 {
			if p.Prepare == nil || p.Redo == nil {
				return fmt.Errorf("oracle INSTALL missing prepare")
			}
			prepared, err := lookup(p.Prepare.Prepare)
			if err != nil {
				return err
			}
			pr, ok := prepared.Record.Payload.(record.Prepared)
			if !ok || pr.Request != p.Request || pr.Digest != p.Digest || prepared.Record.Header.Volume != entry.Record.Header.Volume || !reflect.DeepEqual(pr.Operation.Redo, *p.Redo) || pr.RedoSHA256 != p.Prepare.RedoSHA256 {
				return fmt.Errorf("oracle INSTALL prepare mismatch")
			}
		}
		if len(p.Acceptance) == 0 {
			return fmt.Errorf("oracle INSTALL missing acceptance")
		}
		for _, ref := range p.Acceptance {
			accepted, err := lookup(ref)
			if err != nil {
				return err
			}
			a, ok := accepted.Record.Payload.(record.Accept)
			if !ok || a.Request == nil || *a.Request != p.Request || !reflect.DeepEqual(a.Candidate, p.Decision.Decision) || a.Term != d.ID.Term || !reflect.DeepEqual(a.Authority, d.Authority) {
				return fmt.Errorf("oracle INSTALL ACCEPT mismatch")
			}
		}
	case record.Complete:
		decision, err := lookup(p.Decision.Decision)
		if err != nil {
			return err
		}
		d, ok := decision.Record.Payload.(record.Decision)
		if !ok || d.ID.Request != p.Request || d.Digest != p.Digest || d.Outcome != p.Result.Status {
			return fmt.Errorf("oracle COMPLETE decision mismatch")
		}
		begin, err := lookup(d.Begin)
		if err != nil {
			return err
		}
		b, ok := begin.Record.Payload.(record.Begin)
		if !ok || len(p.Installs) != len(b.Binding.Participants) || len(p.Installed) != len(p.Installs) {
			return fmt.Errorf("oracle COMPLETE participant vector")
		}
		members := make(map[record.UUID]bool)
		for _, ref := range p.Installs {
			installed, err := lookup(ref)
			if err != nil {
				return err
			}
			i, ok := installed.Record.Payload.(record.Install)
			if !ok || i.Request != p.Request || i.Digest != p.Digest || !reflect.DeepEqual(i.Decision, p.Decision) || !slices.Contains(p.Installed, ref.Position) || members[ref.Position.Volume] {
				return fmt.Errorf("oracle COMPLETE install mismatch")
			}
			members[ref.Position.Volume] = true
		}
		for _, member := range b.Binding.Participants {
			if !members[member] {
				return fmt.Errorf("oracle COMPLETE missing member")
			}
		}
		if p.Result.Status == 1 {
			if len(p.CacheBarriers) == 0 {
				return fmt.Errorf("oracle COMPLETE missing barrier")
			}
			for _, ref := range p.CacheBarriers {
				barrier, err := lookup(ref)
				if err != nil {
					return err
				}
				v, ok := barrier.Record.Payload.(record.ViewBarrier)
				if !ok || v.NewVisibility != p.Visibility || !v.Drained {
					return fmt.Errorf("oracle COMPLETE barrier mismatch")
				}
			}
		}
	}
	return nil
}

func checkTransactionPins(previous, current []segment.Entry) error {
	present := make(map[transactionFrameIdentity]bool, len(current))
	for _, entry := range current {
		present[transactionIdentity(entry.Reference)] = true
	}
	for _, entry := range previous {
		if !present[transactionIdentity(entry.Reference)] {
			return fmt.Errorf("oracle required history pin lost: %+v", entry.Reference)
		}
	}
	return nil
}
