package normalpublic

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// Negotiate accepts precisely this representation. It enables no serving profile.
func Negotiate(major, minor uint16) error {
	if major != Major || minor != Minor {
		return fmt.Errorf("unsupported public binding version %d.%d", major, minor)
	}
	return nil
}

// Encode returns owned canonical public source bytes, distinct from normal3.
func Encode(f Frame) ([]byte, error) {
	if err := validateFrame(f); err != nil {
		return nil, err
	}
	return envelope("HFSPUB01", f)
}

// Decode rejects unknown versions, fields, duplicate fields and noncanonical bytes.
func Decode(data []byte) (Frame, error) {
	var f Frame
	if err := decodeEnvelope(data, "HFSPUB01", &f); err != nil {
		return f, err
	}
	return f, validateFrame(f)
}

// Reference derives a complete source identity from validated actual bytes.
func Reference(data []byte) (Ref, error) {
	f, err := Decode(data)
	if err != nil {
		return Ref{}, err
	}
	return Ref{f.Stream, f.Sequence, f.Creator, f.Kind, sha256.Sum256(data)}, nil
}

func envelope(magic string, value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(body) > MaxFrame-48 {
		return nil, fmt.Errorf("public frame exceeds bound")
	}
	data := make([]byte, 16, len(body)+48)
	copy(data, magic)
	binary.BigEndian.PutUint16(data[8:], Major)
	binary.BigEndian.PutUint16(data[10:], Minor)
	binary.BigEndian.PutUint32(data[12:], uint32(len(body)))
	data = append(data, body...)
	hash := sha256.Sum256(data)
	return append(data, hash[:]...), nil
}

func decodeEnvelope(data []byte, magic string, value any) error {
	if len(data) < 48 || len(data) > MaxFrame || string(data[:8]) != magic {
		return fmt.Errorf("invalid public envelope")
	}
	if err := Negotiate(binary.BigEndian.Uint16(data[8:]), binary.BigEndian.Uint16(data[10:])); err != nil {
		return err
	}
	if uint64(binary.BigEndian.Uint32(data[12:]))+48 != uint64(len(data)) {
		return fmt.Errorf("public envelope length")
	}
	hash := sha256.Sum256(data[:len(data)-32])
	if !bytes.Equal(hash[:], data[len(data)-32:]) {
		return fmt.Errorf("public envelope digest")
	}
	d := json.NewDecoder(bytes.NewReader(data[16 : len(data)-32]))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return fmt.Errorf("public decode: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("public trailing value")
	}
	canonical, err := envelope(magic, value)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, data) {
		return fmt.Errorf("noncanonical public encoding")
	}
	return nil
}

func validateFrame(f Frame) error {
	if !validStream(f.Stream) || f.Sequence == 0 || !f.Operation.Valid() || !f.Creator.Incarnation.Valid() || f.Creator.Node != f.Stream.Origin {
		return fmt.Errorf("public source identity")
	}
	n := 0
	for _, present := range []bool{f.Binding != nil, f.Choice != nil, f.Selection != nil, f.Result != nil} {
		if present {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("public closed payload union")
	}
	switch f.Kind {
	case BindingKind:
		if f.Binding == nil {
			return fmt.Errorf("public binding payload")
		}
		if err := validateBinding(*f.Binding); err != nil {
			return err
		}
		if f.Stream != f.Binding.Origin || f.Creator != f.Binding.OriginLaunch || f.Operation != f.Binding.Operation {
			return fmt.Errorf("public original creator mismatch")
		}
	case ChoiceKind:
		if f.Choice == nil || !validRef(f.Choice.Binding, BindingKind) || f.Choice.Outcome < 1 || f.Choice.Outcome > 2 {
			return fmt.Errorf("public choice union")
		}
	case SelectionKind:
		if f.Selection == nil || !validRef(f.Selection.Choice, ChoiceKind) || f.Selection.Slot.Serial == 0 || f.Creator != f.Selection.Slot.Sequencer {
			return fmt.Errorf("public selection issuer")
		}
		p := f.Selection
		if (p.Slot.Serial == 1) != (p.Predecessor == nil) || (p.Predecessor != nil && !validRef(*p.Predecessor, SelectionKind)) || p.Predecessor == nil && p.Baseline == (record.Hash{}) || p.Predecessor != nil && p.Baseline != (record.Hash{}) {
			return fmt.Errorf("public selection predecessor")
		}
	case ResultKind:
		if f.Result == nil || !validRef(f.Result.Selection, SelectionKind) || len(f.Result.Bytes) == 0 || len(f.Result.Bytes) > 1<<20 {
			return fmt.Errorf("public result payload")
		}
	default:
		return fmt.Errorf("unsupported public source kind")
	}
	return nil
}

func validateBinding(b Binding) error {
	if err := validateAnchor(b.Claim.Anchor); err != nil {
		return err
	}
	if b.Claim.Number == 0 || len(b.Intent) == 0 || len(b.Intent) > 1<<20 || sha256.Sum256(b.Intent) != b.Claim.Original || !b.Operation.Valid() {
		return fmt.Errorf("public original intent/operation")
	}
	if !validStream(b.Origin) || b.OriginLaunch.Node != b.Origin.Origin || !b.OriginLaunch.Incarnation.Valid() {
		return fmt.Errorf("public original origin")
	}
	for _, id := range []record.UUID{b.Scope.Export, b.Scope.ExporterIncarnation, b.Scope.FuseSession, b.Scope.GuestBoot, b.Scope.GuestMount, b.Scope.Session, b.Scope.VM, b.Credentials.Process, b.Credentials.UserNamespace} {
		if !id.Valid() {
			return fmt.Errorf("public incomplete scope/provenance identity")
		}
	}
	if b.Credentials.Revision == 0 || len(b.Credentials.Provenance) == 0 || len(b.Credentials.Provenance) > 65536 || len(b.Credentials.Groups) > 1024 {
		return fmt.Errorf("public credential/provenance bound")
	}
	for i := 1; i < len(b.Credentials.Groups); i++ {
		if b.Credentials.Groups[i-1] >= b.Credentials.Groups[i] {
			return fmt.Errorf("public groups must be sorted unique")
		}
	}
	hash, err := record.NormalAuthorityHash(b.Authority)
	if err != nil {
		return fmt.Errorf("public authority: %w", err)
	}
	if hash != b.Scope.Authority || b.Origin.Cluster != b.Authority.Cluster {
		return fmt.Errorf("public authority scope")
	}
	admitted := false
	for _, node := range b.Authority.Admitted {
		admitted = admitted || node == b.Origin.Origin
	}
	member := false
	for _, volume := range b.Authority.Volumes {
		if volume.Volume == b.Origin.Volume {
			member = volume.Generation == b.Origin.Generation && volume.Configuration == b.Origin.Configuration && volume.Epoch == b.Origin.Epoch
		}
	}
	if !admitted || !member || len(b.Dependencies) > 4096 || len(b.Sources) > 4096 {
		return fmt.Errorf("public authority membership/dependency bound")
	}
	for i, ref := range b.Dependencies {
		if !validRef(ref, ResultKind) || ref.Stream.Volume != b.Origin.Volume || i > 0 && bytes.Compare(b.Dependencies[i-1].Hash[:], ref.Hash[:]) >= 0 {
			return fmt.Errorf("public dependency identity/order")
		}
	}
	for i, ref := range b.Sources {
		if !validStream(ref.Stream) || ref.Stream.Volume != b.Origin.Volume || ref.Sequence == 0 || ref.Creator.Node != ref.Stream.Origin || !ref.Creator.Incarnation.Valid() || ref.Kind < record.NormalOpen || ref.Kind > record.NormalPromiseKind || i > 0 && bytes.Compare(b.Sources[i-1].SHA256[:], ref.SHA256[:]) >= 0 {
			return fmt.Errorf("public normal source identity/order")
		}
	}
	return nil
}

func validateAnchor(a Anchor) error {
	if !a.Realm.Valid() || !a.Anchor.Valid() || !a.Nonce.Valid() || len(a.Subject) == 0 || len(a.Subject) > 1024 {
		return fmt.Errorf("public anchor identity")
	}
	return nil
}

func validStream(s record.OriginStream) bool {
	return s.Cluster.Valid() && s.Volume.Valid() && s.Generation.Valid() && s.Configuration.Valid() && s.Epoch > 0 && s.Origin.Valid() && s.Journal.Valid()
}

func validRef(r Ref, kind Kind) bool {
	return r.Kind == kind && validStream(r.Stream) && r.Sequence > 0 && r.Creator.Node == r.Stream.Origin && r.Creator.Incarnation.Valid()
}

func equal(a, b any) bool {
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}
