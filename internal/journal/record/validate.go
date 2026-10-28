package record

import (
	"bytes"
	"crypto/sha256"
	"math"
	"reflect"
)

func validate(value any) error {
	valid := true
	switch v := value.(type) {
	case Time:
		valid = v.Valid()
	case Principal:
		valid = v.Valid()
	case RequestID:
		valid = v.Valid()
	case Endpoint:
		total := 0
		for _, s := range v.Components {
			valid = valid && validName(s)
			total += len(s)
		}
		valid = valid && total <= 16384
	case OriginalRequest:
		valid = v.Operation != 2 || v.Replacement == 2
		valid = valid && (v.SourceParent == nil || *v.SourceParent != 0) && (v.DestinationParent == nil || *v.DestinationParent != 0)
	case DecisionRef:
		position := v.Decision.Position
		valid = v.ID.Coordinator == position.Volume && v.ID.Generation == position.Generation && v.ID.Configuration == position.Configuration && v.ID.Epoch == position.Epoch
	case PrepareRef:
		valid = v.Participant == v.Prepare.Position.Volume
	case FrameRef:
		valid = v.Position.Sequence > 0 && streamIdentity(v.StreamKind, v.Reporter, v.ReporterIncarnation)
	case Authority:
		owner, partner := false, false
		for _, m := range v.Members {
			owner = owner || m == v.Owner
			partner = partner || m == v.Partner
		}
		valid = owner && partner && v.Owner != v.Partner
	case Metadata:
		valid = (v.Type != 2 || v.Size == 0) && (v.Type != 1 || v.Nlink <= 4096)
		if v.Type == 3 {
			valid = valid && len(v.SymlinkText) > 0 && v.Size == uint64(len(v.SymlinkText)) && !bytes.ContainsRune(v.SymlinkText, 0)
		} else {
			valid = valid && len(v.SymlinkText) == 0
		}
	case EntryState:
		valid = v.Target == nil || v.Revision != 0
	case EntryEdit:
		valid = next(v.Before.Revision, v.After.Revision) && (v.Before.Target != nil || v.After.Target != nil) && !reflect.DeepEqual(v.Before.Target, v.After.Target)
	case ObjectEdit:
		valid = next(v.ExpectedRevision, v.After.Revision)
	case LedgerEdit:
		valid = next(v.ExpectedRevision, v.ResultingRevision) && uint64(len(v.Remove)) <= v.ExpectedNlink && v.ResultingNlink == v.ExpectedNlink-uint64(len(v.Remove))+uint64(len(v.Add))
		for _, a := range v.Remove {
			for _, b := range v.Add {
				if a.Parent == b.Parent && a.Name == b.Name {
					valid = false
				}
			}
		}
	case ParentEdit:
		previous := uint64(0)
		if v.Before != nil {
			previous = v.Before.Revision
		}
		valid = next(previous, v.After.Revision)
	case Write:
		valid = uint64(len(v.Data)) <= math.MaxInt64-v.Offset
	case Intent:
		return validateIntent(v)
	case Mutation:
		return validateMutation(v)
	case Binding:
		valid = len(v.Participants) > 0 && v.Coordinator == v.Participants[0] && contains(v.Participants, v.Request.Session.Anchor)
		for _, e := range v.Intent.SelectedMetadata {
			valid = valid && contains(v.Participants, e.Object.Home)
		}
		valid = valid && len(v.Authorities) == len(v.Participants)
		for i, a := range v.Authorities {
			valid = valid && a.Volume == v.Participants[i]
			if i > 0 {
				valid = valid && a.Owner == v.Authorities[0].Owner && a.Partner == v.Authorities[0].Partner && reflect.DeepEqual(a.Members, v.Authorities[0].Members)
			}
		}
	case TerminalResult:
		return validateResult(v, false)
	case Alias:
		entry := v.Invalidate&3 != 0
		valid = (!entry || (v.Parent != nil && validName(v.Name))) && (entry || len(v.Name) == 0) && (v.Invalidate&12 == 0 || v.Object != nil)
	case Tail:
		valid = sameStream(v.Received, v.Durable) && sameStream(v.Received, v.Accepted) && sameStream(v.Received, v.Applied) && v.Durable.Sequence <= v.Received.Sequence && v.Accepted.Sequence <= v.Received.Sequence && v.Applied.Sequence <= v.Received.Sequence
	case VolumeInit:
		valid = v.Initial.Type == 2 && v.RootParent.Parent == v.Root && v.RootParent.ParentEntryRevision == 0 && v.RootLedger.Object == v.Root && v.Initial.Revision == 1 && v.RootLedger.ExpectedRevision == 0 && v.RootParent.Revision == 1 && v.Authorization.Kind == 6
	case Commit:
		valid = ownerRef(v.Mutation)
	case Barrier:
		valid = (v.Scope <= 2) == (v.Object != nil)
	case HandleRelease:
		valid = (v.Reason == 2) == (v.Fence != nil)
	case Pin:
		valid = (v.Action == 2) == (v.Predecessor != nil)
	case OrdinaryResult:
		valid = ownerRef(v.Mutation) && (v.Outcome != 1 || (v.Commit != nil && v.Code == 0)) && (v.Outcome != 2 || (v.Commit == nil && v.Resolution != nil && v.Code != 0 && v.AcceptedBytes == 0))
	case RequestClaim:
		digest, err := documentDigest(v.Document)
		if err != nil {
			return err
		}
		valid = v.Digest == digest
	case RequestBind:
		valid = ownerRef(v.Claim)
	case RequestResult:
		valid = ownerRef(v.Claim) && (v.Result.Status == 3 || v.Binding != nil)
	case SessionRetire:
		valid = v.NewFloor > v.PreviousFloor
	case SessionClose:
		valid = (v.Floor == 0) == (v.Retirement == nil)
	case Begin:
		digest, err := documentDigest(v.Document)
		if err != nil {
			return err
		}
		valid = digest == v.Binding.RequestDigest && documentID(v.Document) == v.Binding.Request
		if v.Document.Class == 2 {
			valid = valid && equalEncoding(v.Document.Internal.Intent, v.Binding.Intent)
		} else {
			valid = valid && publicIntentMatches(*v.Document.Original, v.Binding.Intent)
		}
	case TxAnchor:
		valid = v.Request == v.Binding.Request && v.Digest == v.Binding.RequestDigest
	case Prepared:
		b, err := marshal(v.Operation.Redo)
		if err != nil {
			return err
		}
		valid = v.Request == v.Binding.Request && v.Digest == v.Binding.RequestDigest && Hash(sha256.Sum256(b)) == v.RedoSHA256 && equalEncoding(v.Binding.Intent, v.Operation.Intent)
	case Decision:
		valid = v.Result.Decision == nil && v.Result.Visibility == nil && len(v.Result.Committed) == 0 && ((v.Outcome == 1 && v.Result.Status == 1) || (v.Outcome == 2 && v.Result.Status == 2))
	case Install:
		valid = v.Request == v.Binding.Request && v.Request == v.Decision.ID.Request && v.Digest == v.Binding.RequestDigest && ((v.Outcome == 1 && v.Prepare != nil && v.Redo != nil && v.Applied.Sequence > 0) || (v.Outcome == 2 && v.Redo == nil))
		if v.Outcome == 1 && v.Prepare != nil && v.Redo != nil {
			redo, err := marshal(*v.Redo)
			if err != nil {
				return err
			}
			valid = valid && Hash(sha256.Sum256(redo)) == v.Prepare.RedoSHA256
		}
	case Complete:
		valid = v.Request == v.Decision.ID.Request && v.Result.Decision != nil && reflect.DeepEqual(*v.Result.Decision, v.Decision) && equalEncoding(v.Result.Committed, v.Installed)
		if v.Result.Status == 1 {
			valid = valid && v.Result.Visibility != nil && *v.Result.Visibility == v.Visibility
		}
	case Accept:
		valid = (v.SlotKind == 2) == (v.Request != nil) && ownerRef(v.Candidate)
	case RecoverySelect:
		valid = (v.SlotKind == 2) == (v.Binding != nil) && (v.Selected != nil) != v.AbsenceAbort
	case ViewBarrier:
		valid = v.Drained && v.Affinity == v.OldVisibility.Affinity && v.Affinity == v.NewVisibility.Affinity && next(v.OldVisibility.Generation, v.NewVisibility.Generation) && v.Attestation.Kind == 7
	case SegmentOpen:
		valid = streamIdentity(v.StreamKind, v.Reporter, v.ReporterIncarnation) && v.MaximumBytes >= 412 && (v.PreviousSeal != nil || v.First == 1)
		if v.PreviousSeal != nil {
			valid = valid && next(v.PreviousSeal.Position.Sequence, v.First) && v.PreviousSeal.StreamKind == v.StreamKind && reflect.DeepEqual(v.PreviousSeal.Reporter, v.Reporter) && reflect.DeepEqual(v.PreviousSeal.ReporterIncarnation, v.ReporterIncarnation)
		}
	case SegmentSeal:
		valid = v.First > 0 && v.LastBeforeSeal >= v.First && v.LastBeforeSeal < math.MaxUint64 && v.FrameCount == v.LastBeforeSeal-v.First+1 && v.ByteCount >= 184 && v.ByteCount <= MaxSegment-228
	}
	if !valid {
		return failure("semantic invariant: " + reflect.TypeOf(value).String())
	}
	return nil
}
func validateResult(v TerminalResult, provisional bool) error {
	valid := v.Code == 11 || len(v.Conflicts) == 0
	switch v.Status {
	case 1:
		valid = valid && v.Code == 0 && v.Source != nil
		if !provisional {
			valid = valid && v.Decision != nil && len(v.Committed) > 0 && v.Visibility != nil
		}
	case 2:
		valid = valid && v.Code != 0
		if !provisional {
			valid = valid && v.Decision != nil
		}
		valid = valid && len(v.Committed) == 0 && v.Visibility == nil
	case 3:
		valid = valid && v.Code != 0 && v.Decision == nil && v.Visibility == nil && len(v.Committed) == 0
	default:
		valid = false
	}
	if !valid {
		return failure("terminal/provisional result invariants")
	}
	return nil
}
func validateIntent(v Intent) error {
	if len(v.SelectedMetadata) > 256 {
		return failure("selected metadata count")
	}
	if arg, ok := v.Arguments.(Rename); ok && arg.Noop {
		if len(v.SelectedMetadata) != 0 {
			return failure("no-op selected metadata")
		}
		return nil
	}
	var object ObjectID
	var requiredType uint8
	var creation bool
	switch a := v.Arguments.(type) {
	case Create:
		object = a.Object
		requiredType = 1
		creation = true
		if a.Parent.Home != a.Object.Home {
			return failure("creation home")
		}
	case Mkdir:
		object = a.Object
		requiredType = 2
		creation = true
		if a.Parent.Home != a.Object.Home {
			return failure("creation home")
		}
	case Symlink:
		object = a.Object
		requiredType = 3
		creation = true
		if a.Parent.Home != a.Object.Home {
			return failure("creation home")
		}
	case Write:
		object = a.Object
		requiredType = 1
	case Truncate:
		object = a.Object
		requiredType = 1
	case Setattr:
		object = a.Object
	case Link:
		object = a.Source
		requiredType = 1
	case Unlink:
		object = a.Removed
	case Rmdir:
		object = a.Removed
		requiredType = 2
	case Rename:
		object = a.Source
	}
	var chosen *ObjectEdit
	for i := range v.SelectedMetadata {
		if v.SelectedMetadata[i].Object == object {
			chosen = &v.SelectedMetadata[i]
		}
	}
	if chosen == nil {
		return failure("missing target selected afterimage")
	}
	m := chosen.After
	if requiredType != 0 && m.Type != requiredType {
		return failure("operation target type")
	}
	if creation && (chosen.ExpectedRevision != 0 || m.Revision != 1 || m.Type != 3 && m.Size != 0) {
		return failure("creation afterimage")
	}
	switch a := v.Arguments.(type) {
	case Create:
		if m.Nlink != 1 {
			return failure("create link state")
		}
	case Mkdir:
		if m.Nlink != 2 {
			return failure("mkdir link state")
		}
	case Symlink:
		if !bytes.Equal(m.SymlinkText, a.Text) || m.Mode != 0777 || m.Nlink != 1 {
			return failure("symlink afterimage")
		}
	case Write:
		if m.Size < a.Offset+uint64(len(a.Data)) {
			return failure("write afterimage size")
		}
	case Truncate:
		if m.Size != a.Size {
			return failure("truncate afterimage size")
		}
	case Setattr:
		if a.Mode != nil && m.Mode != *a.Mode || a.UID != nil && m.UID != *a.UID || a.GID != nil && m.GID != *a.GID || a.Atime != nil && m.Atime != a.Atime.Value || a.Mtime != nil && m.Mtime != a.Mtime.Value {
			return failure("SETATTR selected values")
		}
	case Unlink:
		if m.Type == 2 {
			return failure("unlink directory")
		}
	}
	return nil
}
func validateMutation(m Mutation) error {
	if a, ok := m.Intent.Arguments.(Rename); ok && a.Noop {
		if len(m.Redo.Objects)+len(m.Redo.Entries)+len(m.Redo.Ledgers)+len(m.Redo.Parents) != 0 {
			return failure("no-op redo")
		}
	}
	for _, e := range m.Redo.Ledgers {
		for _, o := range m.Redo.Objects {
			if e.Object == o.Object && o.After.Type == 1 && e.ResultingNlink != o.After.Nlink {
				return failure("ledger/metadata count")
			}
		}
	}
	return nil
}
func next(old, new uint64) bool { return old < math.MaxUint64 && new == old+1 }
func contains(ids []UUID, id UUID) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func sameStream(a, b Position) bool {
	return a.Volume == b.Volume && a.Generation == b.Generation && a.Configuration == b.Configuration && a.Epoch == b.Epoch
}
func streamIdentity(kind uint8, reporter, incarnation *UUID) bool {
	return kind == 1 && reporter == nil && incarnation == nil || kind == 2 && reporter != nil && incarnation != nil
}
func ownerRef(ref FrameRef) bool { return ref.StreamKind == 1 }
func equalEncoding(a, b any) bool {
	left, e1 := marshal(a)
	right, e2 := marshal(b)
	return e1 == nil && e2 == nil && bytes.Equal(left, right)
}
func documentID(v RequestDocument) RequestID {
	if v.Class == 1 && v.Original != nil {
		return v.Original.ID
	}
	if v.Class == 2 && v.Internal != nil {
		return v.Internal.ID
	}
	return RequestID{}
}
func documentDigest(v RequestDocument) (Hash, error) {
	if v.Class == 1 && v.Original != nil && v.Internal == nil {
		b, err := marshal(*v.Original)
		return sha256.Sum256(b), err
	}
	if v.Class == 2 && v.Internal != nil && v.Original == nil {
		_, h, err := EncodeInternal(*v.Internal)
		return h, err
	}
	return Hash{}, failure("document union")
}

func publicIntentMatches(original OriginalRequest, intent Intent) bool {
	switch original.Operation {
	case 1:
		rename, ok := intent.Arguments.(Rename)
		return intent.Kind == 7 && ok && rename.Policy == original.Replacement && (original.ExpectedSource == nil || *original.ExpectedSource == rename.Source)
	case 2:
		link, ok := intent.Arguments.(Link)
		return intent.Kind == 4 && ok && (original.ExpectedSource == nil || *original.ExpectedSource == link.Source)
	default:
		return false
	}
}
