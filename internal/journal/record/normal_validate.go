package record

import (
	"crypto/sha256"
	"math"
	"reflect"
)

func validateNormal(value any) error {
	valid := true
	switch v := value.(type) {
	case NormalRef:
		valid = v.Creator.Node == v.Stream.Origin
	case NormalAuthority:
		valid = v.Sequencer.Node != v.OwnerPartner.Node && contains(v.Admitted, v.Sequencer.Node) && contains(v.Admitted, v.OwnerPartner.Node)
		for _, volume := range v.Volumes {
			valid = valid && contains(volume.Replicas, v.Sequencer.Node) && contains(volume.Replicas, v.OwnerPartner.Node)
			for _, node := range volume.Replicas {
				valid = valid && contains(v.Admitted, node)
			}
		}
	case SelectedRef:
		valid = v.Certificate.Kind == NormalSelectionKind && v.Certificate.Creator == v.Slot.Sequencer
	case NormalGrant:
		return validateNormalGrant(v)
	case OriginChoice:
		valid = v.Grant.Kind == NormalGrantKind && (v.Choice == 1) == (v.Prepared != nil)
		if v.Prepared != nil {
			valid = valid && v.Prepared.Kind == NormalAcceptanceKind
		}
	case MemoryAcceptance:
		valid = v.Grant.Kind == NormalGrantKind && v.Recipient.Node != v.Origin.Origin
		if v.Phase == 1 {
			valid = valid && v.Target == v.Grant && v.DurableThrough == 0
		}
		if v.Phase == 2 {
			valid = valid && v.Target.Kind == NormalChoiceKind && v.Target.Stream == v.Origin && v.DurableThrough >= v.Target.Sequence
		}
		if v.Phase == 3 {
			valid = valid && v.Target.Kind == NormalResultKind && v.Target.Stream == v.Origin && v.DurableThrough >= v.Target.Sequence
		}
	case SlotSelection:
		valid = v.Grant.Kind == NormalGrantKind && v.Choice.Kind == NormalChoiceKind && v.Acceptance.Kind == NormalAcceptanceKind && validPredecessor(v.Slot, v.Predecessor)
	case NormalResult:
		valid = v.Grant.Kind == NormalGrantKind
	case SourceCoverage:
		valid = v.Stream == v.Last.Stream && v.Through == v.Last.Sequence
	case NormalFrontier:
		valid = v.Durable <= v.Received && cutBefore(v.Visible, v.Selected) && cutBefore(v.Installed, v.Selected) && cutBefore(v.Checkpoint, v.Installed)
		for _, source := range v.Sources {
			valid = valid && source.Stream == v.Stream
		}
		for _, ref := range v.Acceptances {
			valid = valid && ref.Kind == NormalAcceptanceKind
		}
	case ResultObservation:
		valid = v.Grant.Kind == NormalGrantKind && (v.State == 3) == (v.Result != nil)
		if v.Result != nil {
			valid = valid && v.Result.Kind == NormalResultKind
		}
	case OriginCloseReport:
		valid = (!v.Continuity || !v.LostRAM) && v.Reporter.Node == v.Origin.Node
		if v.State == 1 {
			valid = valid && !v.Continuity
		}
		if v.State == 2 {
			valid = valid && len(v.Unresolved)+len(v.Frontiers)+len(v.Retained)+len(v.Obligations) == 0
		}
		for _, f := range v.Frontiers {
			valid = valid && f.Stream.Origin == v.Origin.Node
		}
		for _, ref := range v.Unresolved {
			valid = valid && ref.Kind == NormalGrantKind
		}
	case ReportEquivalent:
		valid = v.Report == nil || v.Report.Kind == NormalCloseKind && v.Report.Creator.Node == v.Missing.Node
	case NormalRecovery:
		return validateNormalRecovery(v)
	case NormalBarrier:
		valid = v.Origin.Cluster == v.Authority.Cluster && contains(v.Authority.Admitted, v.Origin.Origin) && validRecipient(v.Authority, v.Origin.Origin, v.Recipient)
	case OriginOpen:
		valid = v.Previous == nil || v.Previous.Kind == NormalSeal
	case OriginSeal:
		valid = v.First <= v.Last
	}
	if !valid {
		return failure("normal 3.0 identity/role/order invariant")
	}
	return nil
}

func validateNormalGrant(v NormalGrant) error {
	a, o := v.Authority, v.Origin
	b, err := marshal(a)
	if err != nil {
		return err
	}
	valid := v.Slot.Authority == sha256.Sum256(b) && v.Slot.Sequencer == a.Sequencer && validPredecessor(v.Slot, v.Predecessor)
	valid = valid && o.Cluster == a.Cluster && contains(a.Admitted, o.Origin) && v.OriginLaunch.Node == o.Origin && validRecipient(a, o.Origin, v.Recipient)
	found := false
	for _, volume := range a.Volumes {
		if volume.Volume == o.Volume {
			found = volume.Generation == o.Generation && volume.Configuration == o.Configuration && volume.Epoch == o.Epoch
		}
	}
	valid = valid && found
	// This foundation represents ordinary single-home work. Public transaction
	// documents and foreign-home object graphs need the later typed 2PC contract.
	if v.Document.Class != 2 || !normalLocalObjects(reflect.ValueOf(v.Mutation), o.Volume) {
		return &Error{Unsupported, "normal3 transaction/foreign-home grant"}
	}
	valid = valid && v.Document.Internal.NamespaceVolume == o.Volume
	valid = valid && len(v.Mutation.Dependencies) == 0 && v.NoEffectResult.Code != 0 && v.NoEffectResult.AcceptedBytes == 0
	for _, authorization := range v.Mutation.Authorizations {
		valid = valid && equalEncoding(authorization.Credentials, v.Credentials)
	}
	if v.Document.Class == 2 {
		valid = valid && equalEncoding(v.Document.Internal.Intent, v.Mutation.Intent)
	}
	// Reuse v1's exact per-home metadata/redo agreement, without relaxing any v1
	// stream role or creating a legacy owner commit.
	if err := validateRecord(Record{Header: Header{Volume: o.Volume}, Payload: v.Mutation}); err != nil {
		return err
	}
	if !valid {
		return failure("normal grant authority/origin/intent binding")
	}
	return nil
}

func normalLocalObjects(value reflect.Value, volume UUID) bool {
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		return value.IsNil() || normalLocalObjects(value.Elem(), volume)
	}
	if value.Type() == reflect.TypeFor[ObjectID]() {
		return value.Interface().(ObjectID).Home == volume
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if !normalLocalObjects(value.Field(i), volume) {
				return false
			}
		}
	case reflect.Slice:
		if value.Type().Elem().Kind() != reflect.Uint8 {
			for i := 0; i < value.Len(); i++ {
				if !normalLocalObjects(value.Index(i), volume) {
					return false
				}
			}
		}
	}
	return true
}

func validRecipient(a NormalAuthority, origin UUID, recipient Launch) bool {
	if origin == a.Sequencer.Node {
		return recipient == a.OwnerPartner
	}
	return recipient == a.Sequencer && recipient.Node != origin
}

func validPredecessor(slot OrderedSlot, previous *SelectedRef) bool {
	if slot.Serial == 1 {
		return previous == nil
	}
	return previous != nil && previous.Slot.Authority == slot.Authority && previous.Slot.Sequencer == slot.Sequencer && previous.Slot.Serial < math.MaxUint64 && previous.Slot.Serial+1 == slot.Serial
}

func cutBefore(left, right *SelectedRef) bool {
	if left == nil {
		return true
	}
	if right == nil {
		return false
	}
	return left.Slot.Authority == right.Slot.Authority && left.Slot.Sequencer == right.Slot.Sequencer && left.Slot.Serial <= right.Slot.Serial && (left.Slot.Serial != right.Slot.Serial || *left == *right)
}

func validateNormalRecovery(v NormalRecovery) error {
	// Graph refs must be present, exact and acyclic. Recovery byte verification
	// and source coverage validation remain explicit additional gates.
	nodes := make(map[NormalRef]RecoveryNode, len(v.Graph))
	for _, n := range v.Graph {
		nodes[n.Frame] = n
	}
	state := make(map[NormalRef]uint8, len(nodes))
	var visit func(NormalRef) bool
	visit = func(ref NormalRef) bool {
		node, ok := nodes[ref]
		if !ok || state[ref] == 1 {
			return false
		}
		if state[ref] == 2 {
			return true
		}
		state[ref] = 1
		for _, dependency := range node.Dependencies {
			if !visit(dependency) {
				return false
			}
		}
		state[ref] = 2
		return true
	}
	for ref := range nodes {
		if !visit(ref) {
			return failure("normal recovery incomplete/cyclic graph")
		}
	}
	for _, ref := range normalDependencies(v) {
		if !visit(ref) {
			return failure("normal recovery missing typed source/report dependency")
		}
	}
	if v.Selected != nil && !visit(v.Selected.Certificate) {
		return failure("normal recovery missing selected certificate")
	}
	for _, source := range v.Sources {
		if !visit(source.Last) {
			return failure("normal recovery missing source endpoint")
		}
	}
	for _, ref := range v.Unresolved {
		if ref.Kind != NormalGrantKind || !visit(ref) {
			return failure("normal recovery missing unresolved grant")
		}
	}
	for _, result := range v.Results {
		if !visit(result.Grant) || result.Result != nil && !visit(*result.Result) {
			return failure("normal recovery missing result graph")
		}
	}
	return nil
}
