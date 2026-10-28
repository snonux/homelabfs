package fsapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func transactionSpec() fsapi.TransactionSpec {
	return fsapi.TransactionSpec{
		Version: fsapi.TransactionVersion,
		ID: fsapi.TransactionID{Session: fsapi.SessionID{
			Principal: fsapi.Principal{Realm: uuid(1), Subject: "native:user-1000"},
			Anchor:    uuid(2), Nonce: uuid(3),
		}, Number: 1},
		Operation:   fsapi.Rename,
		Source:      fsapi.Endpoint{Volume: uuid(4), View: uuid(10), Components: []string{"dir", "source"}},
		Destination: fsapi.Endpoint{Volume: uuid(5), View: uuid(11), Components: []string{"dest"}},
		Replacement: fsapi.Replace,
	}
}

func committedOutcome() fsapi.TransactionOutcome {
	spec := transactionSpec()
	position := fsapi.Position{Volume: uuid(2), Generation: uuid(6), Configuration: uuid(7), Epoch: 1, Sequence: 4}
	decision := fsapi.DecisionRef{
		ID:     fsapi.DecisionID{Coordinator: position.Volume, Generation: position.Generation, Configuration: position.Configuration, Epoch: 1, Term: 1, Request: spec.ID},
		Record: position, Digest: fsapi.Digest{1},
	}
	replaced := object(8)
	return fsapi.TransactionOutcome{ID: spec.ID, Claimed: true, RequestDigest: fsapi.Digest{2}, State: fsapi.Committed,
		Result: &fsapi.CommittedResult{Object: object(4), Replaced: &replaced, Decision: decision,
			Positions:  []fsapi.Position{position, {Volume: uuid(4), Generation: uuid(6), Configuration: uuid(7), Epoch: 1, Sequence: 10}, {Volume: uuid(5), Generation: uuid(6), Configuration: uuid(7), Epoch: 1, Sequence: 11}, {Volume: uuid(8), Generation: uuid(6), Configuration: uuid(7), Epoch: 1, Sequence: 12}},
			Visibility: fsapi.Visibility{Closure: uuid(9), Manifest: 1, Generation: 1}},
	}
}

func original(t *testing.T, spec fsapi.TransactionSpec) fsapi.OriginalRequest {
	t.Helper()
	r, err := fsapi.NewOriginalRequest(spec)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestOriginalRequestImmutableIdentity(t *testing.T) {
	spec := transactionSpec()
	original := original(t, spec)
	spec.Source.Components[0] = "changed"
	copySpec := original.Spec()
	copySpec.Destination.Components[0] = "changed"
	copyBytes := original.CanonicalBytes()
	copyBytes[0] = 'X'
	if got := original.Spec(); got.Source.Components[0] != "dir" || got.Destination.Components[0] != "dest" {
		t.Fatalf("original request borrowed path storage: %+v", got)
	}
	if original.Digest() != sha256.Sum256(original.CanonicalBytes()) {
		t.Fatal("canonical byte mutation changed original hash")
	}
	if !original.Equal(original) || original.Equal(fsapi.OriginalRequest{}) {
		t.Fatal("original identity equality accepted invalid identity")
	}
	cred := fsapi.Cred{UID: 1000, Groups: []uint32{10}}
	first, err := fsapi.NewExecuteRequest(original, cred)
	if err != nil {
		t.Fatal(err)
	}
	cred.Groups[0] = 20
	cred.UID = 2000
	second, err := fsapi.NewExecuteRequest(original, cred)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Original().Equal(second.Original()) || first.Credentials().Groups[0] != 10 {
		t.Fatal("mutable execution credentials altered original identity")
	}
	first.Credentials().Groups[0] = 99
	if first.Credentials().Groups[0] != 10 {
		t.Fatal("execute accessor leaked credentials")
	}
	if _, err := fsapi.NewExecuteRequest(fsapi.OriginalRequest{}, cred); !errors.Is(err, fsapi.Invalid) {
		t.Fatalf("zero original request accepted: %v", err)
	}
}

func TestEveryOriginalFieldParticipatesInIdentity(t *testing.T) {
	base := original(t, transactionSpec())
	for name, change := range map[string]func(*fsapi.TransactionSpec){
		"realm":              func(s *fsapi.TransactionSpec) { s.ID.Session.Principal.Realm = uuid(20) },
		"subject":            func(s *fsapi.TransactionSpec) { s.ID.Session.Principal.Subject += "x" },
		"anchor":             func(s *fsapi.TransactionSpec) { s.ID.Session.Anchor = uuid(20) },
		"nonce":              func(s *fsapi.TransactionSpec) { s.ID.Session.Nonce = uuid(20) },
		"number":             func(s *fsapi.TransactionSpec) { s.ID.Number++ },
		"operation":          func(s *fsapi.TransactionSpec) { s.Operation = fsapi.Link; s.Replacement = fsapi.NoReplace },
		"source-volume":      func(s *fsapi.TransactionSpec) { s.Source.Volume = uuid(20) },
		"source-view":        func(s *fsapi.TransactionSpec) { s.Source.View = uuid(20) },
		"source-path":        func(s *fsapi.TransactionSpec) { s.Source.Components[1] += "x" },
		"destination-volume": func(s *fsapi.TransactionSpec) { s.Destination.Volume = uuid(20) },
		"destination-view":   func(s *fsapi.TransactionSpec) { s.Destination.View = uuid(20) },
		"destination-path":   func(s *fsapi.TransactionSpec) { s.Destination.Components[0] += "x" },
		"replacement":        func(s *fsapi.TransactionSpec) { s.Replacement = fsapi.NoReplace },
		"expected-object":    func(s *fsapi.TransactionSpec) { s.ExpectedSource = fsapi.ExpectedObject{Set: true, Value: object(20)} },
		"source-parent":      func(s *fsapi.TransactionSpec) { s.SourceParent = fsapi.ExpectedRevision{Set: true, Value: 1} },
		"source-name":        func(s *fsapi.TransactionSpec) { s.SourceName = fsapi.ExpectedRevision{Set: true, Value: 1} },
		"destination-parent": func(s *fsapi.TransactionSpec) { s.DestinationParent = fsapi.ExpectedRevision{Set: true, Value: 1} },
		"destination-name":   func(s *fsapi.TransactionSpec) { s.DestinationName = fsapi.ExpectedRevision{Set: true, Value: 1} },
	} {
		t.Run(name, func(t *testing.T) {
			spec := transactionSpec()
			change(&spec)
			changed := original(t, spec)
			if base.Equal(changed) || base.Digest() == changed.Digest() {
				t.Fatal("changed original field retained identity")
			}
		})
	}
}

func TestCanonicalRequestPreservesRawBytesAndBoundaries(t *testing.T) {
	spec := transactionSpec()
	spec.Source.Components = []string{"a\xff", "bc"}
	a := original(t, spec)
	spec.Source.Components = []string{"a\xfe", "bc"}
	b := original(t, spec)
	if a.Equal(b) || !bytes.Contains(a.CanonicalBytes(), []byte{'a', 0xff}) {
		t.Fatal("canonicalization normalized invalid UTF-8 path bytes")
	}
	spec.Source.Components = []string{"ab", "c"}
	c := original(t, spec)
	spec.Source.Components = []string{"a", "bc"}
	if c.Equal(original(t, spec)) {
		t.Fatal("component concatenation collision")
	}
	spec.Source.Components = []string{"é"}
	composed := original(t, spec)
	spec.Source.Components = []string{"e\u0301"}
	if composed.Equal(original(t, spec)) {
		t.Fatal("canonicalization applied Unicode normalization")
	}
}

func TestPresentZeroNameRevisionPreservesOriginalIdentity(t *testing.T) {
	spec := transactionSpec()
	spec.Replacement = fsapi.NoReplace
	omitted := original(t, spec)
	omittedBytes := omitted.CanonicalBytes()
	// The five absent expectations are the original request's final five bytes.
	prefix := omittedBytes[:len(omittedBytes)-5]
	for _, tc := range []struct {
		name   string
		set    func(*fsapi.TransactionSpec)
		get    func(fsapi.TransactionSpec) fsapi.ExpectedRevision
		suffix []byte
	}{
		{
			name:   "source-name",
			set:    func(s *fsapi.TransactionSpec) { s.SourceName = fsapi.ExpectedRevision{Set: true} },
			get:    func(s fsapi.TransactionSpec) fsapi.ExpectedRevision { return s.SourceName },
			suffix: []byte{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			name:   "destination-name",
			set:    func(s *fsapi.TransactionSpec) { s.DestinationName = fsapi.ExpectedRevision{Set: true} },
			get:    func(s fsapi.TransactionSpec) fsapi.ExpectedRevision { return s.DestinationName },
			suffix: []byte{0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := omitted.Spec()
			tc.set(&spec)
			request := original(t, spec)
			if got := tc.get(request.Spec()); got != (fsapi.ExpectedRevision{Set: true, Value: 0}) {
				t.Fatalf("present-zero expectation changed: %+v", got)
			}
			expectedBytes := append(bytes.Clone(prefix), tc.suffix...)
			if !bytes.Equal(request.CanonicalBytes(), expectedBytes) {
				t.Fatalf("present-zero canonical bytes = %x, want %x", request.CanonicalBytes(), expectedBytes)
			}
			if request.Digest() != sha256.Sum256(expectedBytes) {
				t.Fatal("present-zero digest does not cover its exact canonical bytes")
			}
			if request.Equal(omitted) || bytes.Equal(request.CanonicalBytes(), omittedBytes) || request.Digest() == omitted.Digest() {
				t.Fatal("present-zero name revision collapsed into omission")
			}
		})
	}
}

func TestCanonicalRequestJournalGolden(t *testing.T) {
	// Independently encoded with Python struct/hashlib in the journal-format
	// specification. This binds API identity to retained original journal bytes.
	repeated := func(value byte) fsapi.UUID {
		var id fsapi.UUID
		for i := range id {
			id[i] = value
		}
		return id
	}
	spec := fsapi.TransactionSpec{
		Version: fsapi.TransactionVersion,
		ID: fsapi.TransactionID{Session: fsapi.SessionID{
			Principal: fsapi.Principal{Realm: repeated(0x11), Subject: "u"},
			Anchor:    repeated(0x22), Nonce: repeated(0x33),
		}, Number: 1},
		Operation: fsapi.Link, Replacement: fsapi.NoReplace,
		Source:      fsapi.Endpoint{Volume: repeated(0x44), View: repeated(0x55), Components: []string{"a\xff"}},
		Destination: fsapi.Endpoint{Volume: repeated(0x66), View: repeated(0x77), Components: []string{"b"}},
	}
	const canonicalHex = "000111111111111111111111111111111111000000017522222222222222222222222222222222333333333333333333333333333333330000000000000001024444444444444444444444444444444455555555555555555555555555555555000000010000000261ff6666666666666666666666666666666677777777777777777777777777777777000000010000000162020000000000"
	const digestHex = "93d01dd563af238c52b3a3aadfd52d3611ad939505f5fc6b275a7389f1832cc7"
	request := original(t, spec)
	if got := hex.EncodeToString(request.CanonicalBytes()); got != canonicalHex {
		t.Fatalf("canonical journal identity changed: %s", got)
	}
	digest := request.Digest()
	if got := hex.EncodeToString(digest[:]); got != digestHex {
		t.Fatalf("journal identity digest changed: %s", got)
	}
}

func TestEndpointBounds(t *testing.T) {
	spec := transactionSpec()
	spec.ID.Session.Principal.Subject = strings.Repeat("u", 1024)
	spec.Source.Components = make([]string, fsapi.MaxPathComponents)
	for i := range spec.Source.Components {
		spec.Source.Components[i] = "a"
	}
	_ = original(t, spec)
	spec.Source.Components = make([]string, 65)
	for i := range 64 {
		spec.Source.Components[i] = strings.Repeat("a", fsapi.MaxNameBytes)
	}
	spec.Source.Components[64] = strings.Repeat("b", 64)
	_ = original(t, spec) // exactly 16384 raw path bytes.
	spec.Source.Components[64] += "b"
	if _, err := fsapi.NewOriginalRequest(spec); !errors.Is(err, fsapi.Invalid) {
		t.Fatalf("path above byte limit accepted: %v", err)
	}
}

func TestRejectInvalidOriginalRequest(t *testing.T) {
	for name, change := range map[string]func(*fsapi.TransactionSpec){
		"version":             func(s *fsapi.TransactionSpec) { s.Version++ },
		"operation":           func(s *fsapi.TransactionSpec) { s.Operation = 0 },
		"link-replace":        func(s *fsapi.TransactionSpec) { s.Operation = fsapi.Link },
		"replacement":         func(s *fsapi.TransactionSpec) { s.Replacement = 0 },
		"zero-number":         func(s *fsapi.TransactionSpec) { s.ID.Number = 0 },
		"zero-anchor":         func(s *fsapi.TransactionSpec) { s.ID.Session.Anchor = fsapi.UUID{} },
		"zero-realm":          func(s *fsapi.TransactionSpec) { s.ID.Session.Principal.Realm = fsapi.UUID{} },
		"empty-subject":       func(s *fsapi.TransactionSpec) { s.ID.Session.Principal.Subject = "" },
		"long-subject":        func(s *fsapi.TransactionSpec) { s.ID.Session.Principal.Subject = strings.Repeat("s", 1025) },
		"zero-volume":         func(s *fsapi.TransactionSpec) { s.Source.Volume = fsapi.UUID{} },
		"empty-view":          func(s *fsapi.TransactionSpec) { s.Source.View = fsapi.UUID{} },
		"too-many-components": func(s *fsapi.TransactionSpec) { s.Source.Components = make([]string, fsapi.MaxPathComponents+1) },
		"too-long-name": func(s *fsapi.TransactionSpec) {
			s.Source.Components = []string{strings.Repeat("a", fsapi.MaxNameBytes+1)}
		},
		"too-long-path": func(s *fsapi.TransactionSpec) {
			s.Source.Components = make([]string, 65)
			for i := range s.Source.Components {
				s.Source.Components[i] = strings.Repeat("a", fsapi.MaxNameBytes)
			}
		},
		"root":                               func(s *fsapi.TransactionSpec) { s.Source.Components = nil },
		"absolute":                           func(s *fsapi.TransactionSpec) { s.Source.Components = []string{"/a"} },
		"empty-component":                    func(s *fsapi.TransactionSpec) { s.Source.Components = []string{"a", ""} },
		"dot":                                func(s *fsapi.TransactionSpec) { s.Source.Components = []string{"."} },
		"dotdot":                             func(s *fsapi.TransactionSpec) { s.Destination.Components = []string{".."} },
		"nul":                                func(s *fsapi.TransactionSpec) { s.Source.Components = []string{"a\x00b"} },
		"embedded-slash":                     func(s *fsapi.TransactionSpec) { s.Source.Components = []string{"a/b"} },
		"expected-object":                    func(s *fsapi.TransactionSpec) { s.ExpectedSource.Set = true },
		"hidden-object":                      func(s *fsapi.TransactionSpec) { s.ExpectedSource.Value.Home = uuid(1) },
		"zero-source-parent-revision":        func(s *fsapi.TransactionSpec) { s.SourceParent.Set = true },
		"zero-destination-parent-revision":   func(s *fsapi.TransactionSpec) { s.DestinationParent.Set = true },
		"hidden-source-parent-revision":      func(s *fsapi.TransactionSpec) { s.SourceParent.Value = 1 },
		"hidden-source-name-revision":        func(s *fsapi.TransactionSpec) { s.SourceName.Value = 1 },
		"hidden-destination-parent-revision": func(s *fsapi.TransactionSpec) { s.DestinationParent.Value = 1 },
		"hidden-destination-name-revision":   func(s *fsapi.TransactionSpec) { s.DestinationName.Value = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			spec := transactionSpec()
			change(&spec)
			if _, err := fsapi.NewOriginalRequest(spec); !errors.Is(err, fsapi.Invalid) {
				t.Fatalf("accepted invalid original request: %v", err)
			}
		})
	}
}

func TestTypedTransactionOutcomesAndReceiptOwnership(t *testing.T) {
	committed := committedOutcome()
	for _, outcome := range []fsapi.TransactionOutcome{
		{ID: committed.ID, State: fsapi.Rejected, Code: fsapi.UnknownID},
		{ID: committed.ID, Claimed: true, RequestDigest: committed.RequestDigest, State: fsapi.Rejected, Code: fsapi.IDConflict},
		{ID: committed.ID, Claimed: true, State: fsapi.Pending, Stage: fsapi.Claimed},
		committed,
		{ID: committed.ID, Claimed: true, State: fsapi.Aborted, Code: fsapi.AbortRequested, AbortDecision: &committed.Result.Decision},
		{ID: committed.ID, State: fsapi.Indeterminate, Uncertainty: fsapi.MissingHistory},
		{ID: committed.ID, State: fsapi.RetiredID},
	} {
		completion := fsapi.NewCompletion[fsapi.TransactionOutcome]()
		if err := completion.Complete(outcome); err != nil {
			t.Fatalf("valid outcome %+v: %v", outcome, err)
		}
	}
	completion := fsapi.NewCompletion[fsapi.TransactionOutcome]()
	if err := completion.Complete(committed); err != nil {
		t.Fatal(err)
	}
	committed.Result.Positions[0].Sequence = 999
	committed.Result.Replaced.Home = uuid(99)
	committed.Result.Object.Home = uuid(99)
	first, err := completion.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.Positions[0].Sequence != 4 || first.Result.Replaced.Home != uuid(8) || first.Result.Object.Home != uuid(4) {
		t.Fatal("receipt borrowed result storage")
	}
	first.Result.Positions[0].Sequence = 888
	first.Result.Replaced.Allocation = uuid(99)
	second, err := completion.Wait(t.Context())
	if err != nil || second.Result.Positions[0].Sequence != 4 || second.Result.Replaced.Allocation != uuid(9) {
		t.Fatalf("waiter altered retained receipt: %+v, %v", second, err)
	}
}

func TestRejectInvalidTransactionOutcomes(t *testing.T) {
	for name, change := range map[string]func(*fsapi.TransactionOutcome){
		"id":                     func(o *fsapi.TransactionOutcome) { o.ID.Number = 0 },
		"state":                  func(o *fsapi.TransactionOutcome) { o.State = 255 },
		"unclaimed":              func(o *fsapi.TransactionOutcome) { o.Claimed = false },
		"no-receipt":             func(o *fsapi.TransactionOutcome) { o.Result = nil },
		"bad-object":             func(o *fsapi.TransactionOutcome) { o.Result.Object = fsapi.ObjectID{} },
		"bad-replaced":           func(o *fsapi.TransactionOutcome) { *o.Result.Replaced = fsapi.ObjectID{} },
		"decision-request":       func(o *fsapi.TransactionOutcome) { o.Result.Decision.ID.Request.Number++ },
		"decision-history":       func(o *fsapi.TransactionOutcome) { o.Result.Decision.Record.Generation = uuid(99) },
		"missing-anchor":         func(o *fsapi.TransactionOutcome) { o.Result.Positions = o.Result.Positions[1:] },
		"missing-object-home":    func(o *fsapi.TransactionOutcome) { o.Result.Object.Home = uuid(99) },
		"missing-replaced-home":  func(o *fsapi.TransactionOutcome) { o.Result.Replaced.Home = uuid(99) },
		"decision-not-covered":   func(o *fsapi.TransactionOutcome) { o.Result.Positions[0].Sequence = 1 },
		"position-wrong-history": func(o *fsapi.TransactionOutcome) { o.Result.Positions[0].Generation = uuid(99) },
		"duplicate-position": func(o *fsapi.TransactionOutcome) {
			o.Result.Positions = append(o.Result.Positions, o.Result.Positions[0])
		},
		"invalid-position":      func(o *fsapi.TransactionOutcome) { o.Result.Positions[0].Epoch = 0 },
		"invalid-visibility":    func(o *fsapi.TransactionOutcome) { o.Result.Visibility.Generation = 0 },
		"code-on-commit":        func(o *fsapi.TransactionOutcome) { o.Code = fsapi.TransactionIO },
		"stage-on-commit":       func(o *fsapi.TransactionOutcome) { o.Stage = fsapi.Installing },
		"uncertainty-on-commit": func(o *fsapi.TransactionOutcome) { o.Uncertainty = fsapi.MissingHistory },
		"abort-on-commit":       func(o *fsapi.TransactionOutcome) { o.AbortDecision = &o.Result.Decision },
	} {
		t.Run(name, func(t *testing.T) {
			outcome := committedOutcome()
			change(&outcome)
			if err := outcome.Validate(); !errors.Is(err, fsapi.Invalid) {
				t.Fatalf("invalid transaction outcome accepted: %v", err)
			}
		})
	}
	for _, outcome := range []fsapi.TransactionOutcome{
		{ID: transactionSpec().ID, State: fsapi.Pending, Stage: fsapi.Preparing},
		{ID: transactionSpec().ID, State: fsapi.Indeterminate},
		{ID: transactionSpec().ID, State: fsapi.Rejected},
		{ID: transactionSpec().ID, State: fsapi.Rejected, Code: fsapi.IDConflict},
		{ID: transactionSpec().ID, Claimed: true, State: fsapi.Rejected, Code: fsapi.UnknownID},
		{ID: transactionSpec().ID, Claimed: true, State: fsapi.Aborted, Code: fsapi.AbortRequested},
	} {
		if err := outcome.Validate(); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("incomplete outcome accepted: %+v: %v", outcome, err)
		}
	}
}

func TestSessionIdentityAndRetirementRequests(t *testing.T) {
	session := transactionSpec().ID.Session
	cred := fsapi.Cred{UID: 1000, Groups: []uint32{10}}
	request, err := fsapi.NewSessionRequest(session, cred)
	if err != nil {
		t.Fatal(err)
	}
	cred.Groups[0] = 20
	identity, err := fsapi.NewIdentityRequest(request, 1)
	if err != nil {
		t.Fatal(err)
	}
	retirement, err := fsapi.NewRetirementRequest(request, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if request.Session() != session || identity.ID() != transactionSpec().ID || retirement.Session() != session || retirement.Through() != 100 || !retirement.Close() {
		t.Fatal("immutable session anchor/request changed")
	}
	for _, snapshot := range []fsapi.Cred{request.Credentials(), identity.Credentials(), retirement.Credentials()} {
		if snapshot.Groups[0] != 10 {
			t.Fatal("request borrowed credentials")
		}
		snapshot.Groups[0] = 99
	}
	if retirement.Credentials().Groups[0] != 10 {
		t.Fatal("retirement accessor leaked credentials")
	}
	for _, err := range []error{
		func() error { _, err := fsapi.NewSessionRequest(fsapi.SessionID{}, cred); return err }(),
		func() error { _, err := fsapi.NewIdentityRequest(request, 0); return err }(),
		func() error { _, err := fsapi.NewIdentityRequest(fsapi.SessionRequest{}, 1); return err }(),
		func() error { _, err := fsapi.NewRetirementRequest(request, 0, false); return err }(),
	} {
		if !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("invalid session request accepted: %v", err)
		}
	}
	if _, err := fsapi.NewRetirementRequest(request, 0, true); err != nil {
		t.Fatalf("empty session cannot close: %v", err)
	}
	for _, outcome := range []fsapi.SessionOutcome{
		{Session: session, State: fsapi.SessionReady, RetiredThrough: 100},
		{Session: session, State: fsapi.SessionClosed, RetiredThrough: 100},
		{Session: session, State: fsapi.SessionPending},
		{Session: session, State: fsapi.SessionIndeterminate, Uncertainty: fsapi.MissingHistory},
		{Session: session, State: fsapi.SessionRejected, Code: fsapi.ResourceLimit},
	} {
		completion := fsapi.NewCompletion[fsapi.SessionOutcome]()
		if err := completion.Complete(outcome); err != nil {
			t.Fatal(err)
		}
		if _, err := completion.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, outcome := range []fsapi.SessionOutcome{
		{}, {Session: session}, {Session: session, State: fsapi.SessionReady, Code: fsapi.AccessDenied},
		{Session: session, State: fsapi.SessionRejected}, {Session: session, State: fsapi.SessionIndeterminate},
		{Session: session, State: fsapi.SessionClosed, Uncertainty: fsapi.MissingHistory},
	} {
		if err := outcome.Validate(); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("invalid session outcome accepted: %+v: %v", outcome, err)
		}
	}
}
