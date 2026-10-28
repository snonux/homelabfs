package record

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"
)

func fixtureAuthority(volume UUID) Authority {
	return Authority{Volume: volume, Generation: fixtureUUID(0x22), Configuration: fixtureUUID(0x33), Epoch: 7, Owner: fixtureUUID(0x44), Partner: fixtureUUID(0x66), Members: []UUID{fixtureUUID(0x44), fixtureUUID(0x66), fixtureUUID(0x88)}, CoordinatorTerm: 1, OwnerIncarnation: fixtureUUID(0x55), PartnerIncarnation: fixtureUUID(0x77)}
}
func fixtureRef() FrameRef {
	return FrameRef{StreamKind: 1, Position: fixturePosition(), SHA256: sha256.Sum256([]byte("retained frame"))}
}
func fixtureBinding() Binding {
	return Binding{Request: fixtureRequestID(), RequestDigest: fixtureDocumentHash(), Affinity: fixtureVisibility().Affinity, Coordinator: fixtureUUID(0x11), Intent: fixtureWrite().Intent, Participants: []UUID{fixtureUUID(0x11), fixtureUUID(0x22)}, Authorities: []Authority{fixtureAuthority(fixtureUUID(0x11)), fixtureAuthority(fixtureUUID(0x22))}, Dependencies: Vector{fixturePosition()}}
}
func fixtureVisibility() Visibility {
	return Visibility{Affinity: AffinityRef{Closure: fixtureUUID(0x88), ManifestVersion: 1}, Generation: 1}
}
func fixtureDocument() RequestDocument {
	v := InternalRequest{Version: 1, ID: fixtureRequestID(), NamespaceVolume: fixtureUUID(0x11), View: fixtureUUID(0x55), Intent: fixtureWrite().Intent}
	return RequestDocument{Class: 2, Internal: &v}
}
func fixtureDocumentHash() Hash {
	h, err := documentDigest(fixtureDocument())
	if err != nil {
		panic(err)
	}
	return h
}
func fixtureDecision() DecisionRef {
	return DecisionRef{ID: DecisionID{Coordinator: fixtureUUID(0x11), Generation: fixtureUUID(0x22), Configuration: fixtureUUID(0x33), Epoch: 7, Request: fixtureRequestID(), Term: 1}, Decision: fixtureRef()}
}
func fixtureResult() TerminalResult {
	obj := fixtureObject()
	decision := fixtureDecision()
	visibility := fixtureVisibility()
	return TerminalResult{Status: 1, Source: &obj, Decision: &decision, Committed: Vector{fixturePosition()}, Visibility: &visibility}
}
func fixtureArtifact(kind uint16) Artifact {
	return Artifact{Kind: kind, ID: fixtureUUID(0xaa), Length: 123}
}
func fixtureTail() Tail {
	pos := fixturePosition()
	return Tail{Received: pos, Durable: pos, Accepted: pos, Applied: pos, Incarnation: fixtureUUID(0x55), Continuous: true}
}
func recordFixtures(t testing.TB) []Record {
	t.Helper()
	id := fixtureRequestID()
	ref := fixtureRef()
	binding := fixtureBinding()
	visibility := fixtureVisibility()
	obj := fixtureObject()
	authority := fixtureAuthority(fixtureUUID(0x11))
	affinity := visibility.Affinity
	reporter := fixtureUUID(0x66)
	incarnation := fixtureUUID(0x55)
	artifact := fixtureArtifact(6)
	result := fixtureResult()
	decision := fixtureDecision()
	position := fixturePosition()
	vector := Vector{position}
	generation := fixtureUUID(0xbb)
	mutation := fixtureWrite()
	redoBytes, err := marshal(mutation.Redo)
	if err != nil {
		t.Fatal(err)
	}
	prepare := PrepareRef{Participant: obj.Home, Prepare: ref, RedoSHA256: sha256.Sum256(redoBytes)}
	reporterRef := ref
	reporterRef.StreamKind = 2
	reporterRef.Reporter = &reporter
	reporterRef.ReporterIncarnation = &incarnation
	frames := Evidence{Frames: [][]byte{golden(t, "write_candidate")}}
	document := fixtureDocument()
	provisional := result
	provisional.Decision = nil
	provisional.Visibility = nil
	provisional.Committed = nil
	payloads := []any{
		VolumeInit{Root: obj, Initial: fixtureMetadata(1, 2, 1000, 0755, 0, 2), RootParent: ParentState{Revision: 1, Parent: obj}, RootLedger: LedgerEdit{Object: obj, ResultingRevision: 1}, Authority: authority, Authorization: artifact},
		mutation,
		Commit{Mutation: ref, Authority: authority, Dependencies: vector},
		Barrier{Scope: 1, Object: &obj, Covered: vector, Decisions: []DecisionRef{decision}, Visibility: visibility},
		HandlePin{Pin: generation, Object: obj, ServingGeneration: generation, Incarnation: incarnation, Reasons: 7},
		HandleRelease{Pin: generation, Object: obj, ServingGeneration: generation, Incarnation: incarnation, Reason: 2, Fence: &ref},
		Reclaim{Object: obj, ExpectedObjectRevision: 4, ExpectedLedgerRevision: 1, Retention: ref, PinsReleased: []FrameRef{ref}},
		Pin{Pin: generation, Action: 1, Purpose: 1, Objects: []ObjectID{obj}, History: vector, Artifacts: []Artifact{artifact}},
		OrdinaryResult{Mutation: ref, Commit: &ref, Outcome: 1, Object: &obj, AcceptedBytes: 3, Dependencies: []FrameRef{ref}, Evidence: frames},
		Session{Principal: id.Session.Principal, Anchor: id.Session.Anchor, Nonce: id.Session.Nonce, State: 1},
		RequestClaim{Document: document, Digest: fixtureDocumentHash()},
		RequestBind{Claim: ref, Binding: binding, Authorizations: mutation.Authorizations},
		RequestResult{Request: id, Digest: fixtureDocumentHash(), Claim: ref, Binding: &ref, Result: result, Dependencies: []FrameRef{ref}, Evidence: frames},
		SessionRetire{Principal: id.Session.Principal, Anchor: id.Session.Anchor, Nonce: id.Session.Nonce, NewFloor: 1, TerminalResults: []FrameRef{ref}},
		SessionClose{Principal: id.Session.Principal, Anchor: id.Session.Anchor, Nonce: id.Session.Nonce, Floor: 1, Retirement: &ref},
		AnchorRetire{Anchor: id.Session.Anchor, ClosedSessions: []FrameRef{ref}, PermanentRejection: artifact, Authority: authority},
		Affinity{Closure: affinity.Closure, ManifestVersion: 1, Members: []UUID{fixtureUUID(0x11), fixtureUUID(0x22)}, Authorities: binding.Authorities},
		Begin{Binding: binding, Bind: ref, Document: document, Authorizations: mutation.Authorizations, Term: 1},
		TxAnchor{Request: id, Digest: binding.RequestDigest, Binding: binding, Begin: ref, CoordinatorTerm: 1},
		Prepared{Request: id, Digest: binding.RequestDigest, Begin: ref, Binding: binding, Term: 1, Operation: mutation, RedoSHA256: prepare.RedoSHA256, Retained: []FrameRef{ref}, Artifacts: []Artifact{artifact}},
		Decision{ID: decision.ID, Digest: binding.RequestDigest, Begin: ref, Outcome: 1, Prepares: []PrepareRef{prepare}, Result: provisional, Authority: authority},
		Install{Request: id, Digest: binding.RequestDigest, Binding: binding, Decision: decision, Outcome: 1, Prepare: &prepare, Redo: &mutation.Redo, Applied: position, Acceptance: []FrameRef{reporterRef}, Dependencies: []FrameRef{ref}, Evidence: frames},
		Complete{Request: id, Digest: binding.RequestDigest, Decision: decision, Installs: []FrameRef{ref}, Installed: vector, Result: result, Visibility: visibility, CacheBarriers: []FrameRef{ref}},
		Promise{RecoveryID: generation, Authority: authority, PromisedEpoch: 8, PromisedTerm: 2, Reporter: reporter, ReporterIncarnation: incarnation, Tails: []Tail{fixtureTail()}, PriorAcceptances: []FrameRef{reporterRef}, Authorization: artifact},
		Accept{SlotKind: 1, Candidate: ref, Term: 1, Authority: authority, Reporter: reporter, ReporterIncarnation: incarnation, Retained: vector, Dependencies: []FrameRef{ref}, Durable: vector},
		TailReport{RecoveryID: generation, Reporter: reporter, ReporterIncarnation: incarnation, Promises: []FrameRef{reporterRef}, Tails: []Tail{fixtureTail()}, Candidates: []FrameRef{ref}, Dependencies: []FrameRef{ref}},
		RecoverySelect{RecoveryID: generation, SlotKind: 1, Operation: fixtureUUID(0x44), NewAuthorities: []Authority{authority}, Promises: []FrameRef{reporterRef}, Reports: []FrameRef{reporterRef}, Selected: &ref, Dependencies: []FrameRef{ref}, Fence: []FrameRef{ref}, Evidence: frames},
		Fence{RecoveryID: generation, Authorities: []Authority{authority}, OldOwner: authority.Owner, OldIncarnation: incarnation, ServingGenerations: []UUID{generation}, Views: []UUID{generation}, Mechanism: generation, Attestation: fixtureArtifact(5), CompletedAt: fixtureHeader(1).Observed, Dependencies: []FrameRef{ref}},
		Configuration{RecoveryID: generation, Predecessors: []Authority{authority}, Successors: []Authority{authority}, Closure: affinity, Recovered: vector, Promises: []FrameRef{reporterRef}, Fences: []FrameRef{ref}, PartnerReady: []FrameRef{ref}, Authorization: artifact},
		LossRecovery{RecoveryID: generation, Previous: []Authority{authority}, Selected: vector, NewGeneration: generation, UnavailableRequests: []RequestID{id}, UnavailableRanges: []Position{position}, Baselines: []Artifact{artifact}, Fences: []FrameRef{ref}, Authorization: artifact},
		ViewBarrier{Affinity: affinity, OldVisibility: visibility, NewVisibility: Visibility{Affinity: affinity, Generation: 2}, ServingGeneration: generation, Incarnation: incarnation, Aliases: []Alias{{View: generation, Object: &obj, Invalidate: 12}}, Drained: true, Mechanism: generation, Attestation: fixtureArtifact(7), Dependencies: []FrameRef{ref}},
		CheckpointAccept{Checkpoint: generation, Root: obj, DataManifest: fixtureArtifact(2), Catalog: fixtureArtifact(1), Committed: vector, Applied: vector, Visibility: visibility, ClosureCheckpoint: generation, ClosureMembers: binding.Participants, Dependencies: []FrameRef{ref}, Replacements: []FrameRef{ref}, Pins: []FrameRef{ref}, RequiredSuffix: vector},
		Retention{RetentionID: generation, CheckpointAccepts: []FrameRef{ref}, PublishedManifests: []Artifact{artifact}, ClosureCheckpoint: generation, ReclaimThrough: vector, Coverage: []Coverage{{Replica: reporter, Incarnation: incarnation, Durable: vector, SuffixStart: vector, Dependencies: []FrameRef{ref}}}, Pins: []FrameRef{ref}, Replacements: []FrameRef{ref}, Authorities: []Authority{authority}},
		ReplicaProgress{Replica: reporter, Incarnation: incarnation, Received: vector, Durable: vector, Decided: vector, Applied: vector, RequiredSuffix: vector, TransferPins: []FrameRef{ref}},
		SegmentOpen{Segment: generation, StreamKind: 1, First: 1, MaximumBytes: MaxSegment},
		SegmentSeal{Segment: generation, First: 1, LastBeforeSeal: 1, FrameCount: 1, ByteCount: 184},
	}
	out := make([]Record, 0, len(payloads))
	for _, p := range payloads {
		h := fixtureHeader(2)
		if _, ok := p.(SegmentOpen); ok {
			h.Sequence = 1
		}
		out = append(out, Record{Header: h, Payload: p})
	}
	return out
}
func TestEveryRecordRoundTrip(t *testing.T) {
	fixtures := recordFixtures(t)
	if len(fixtures) != 36 {
		t.Fatal(len(fixtures))
	}
	for _, r := range fixtures {
		t.Run(fmt.Sprintf("%02d_%T", kindOf(r.Payload), r.Payload), func(t *testing.T) {
			raw, err := Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			back, err := Encode(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, back) {
				t.Fatal("noncanonical roundtrip")
			}
		})
	}
}
func TestEveryMutationRoundTrip(t *testing.T) {
	obj, parent := fixtureObject(), fixtureParent()
	mode := uint16(0644)
	uid := uint32(1000)
	gid := uid
	tm := TimeSelection{Source: 2, Value: fixtureHeader(1).Observed}
	args := []any{Create{Parent: parent, Name: "new", Object: obj, RequestedMode: 0644, Umask: 0022}, Mkdir{Parent: parent, Name: "new", Object: obj, RequestedMode: 0755, Umask: 0022}, Symlink{Parent: parent, Name: "new", Object: obj, Text: []byte("../target")}, Link{Source: obj, TargetParent: parent, TargetName: "new"}, Unlink{Parent: parent, Name: "old", Removed: obj}, Rmdir{Parent: parent, Name: "old", Removed: obj}, Rename{SourceParent: parent, SourceName: "old", Source: obj, DestinationParent: parent, DestinationName: "new", Policy: 2}, Write{Object: obj, Offset: 2, Data: []byte{0, 255}}, Truncate{Object: obj, Size: 17}, Setattr{Object: obj, Mask: 31, Mode: &mode, UID: &uid, GID: &gid, Atime: &tm, Mtime: &tm}}
	for i, arg := range args {
		t.Run(fmt.Sprintf("%02d_%T", i+1, arg), func(t *testing.T) {
			m := fixtureMetadata(4, 1, 1000, 0644, 5, 1)
			expected := uint64(3)
			switch i + 1 {
			case 1:
				m = fixtureMetadata(1, 1, 1000, 0644, 0, 1)
				expected = 0
			case 2:
				m = fixtureMetadata(1, 2, 1000, 0755, 0, 2)
				expected = 0
			case 3:
				m = fixtureMetadata(1, 3, 1000, 0777, 9, 1)
				m.SymlinkText = []byte("../target")
				expected = 0
			case 6:
				m = fixtureMetadata(4, 2, 1000, 0755, 0, 0)
			case 9:
				m.Size = 17
			}
			chosen := []ObjectEdit{{Object: obj, ExpectedRevision: expected, After: m}}
			intent := Intent{Kind: uint16(i + 1), Arguments: arg, SelectedMetadata: chosen}
			r := Record{Header: fixtureHeader(2), Payload: Mutation{Intent: intent, Redo: Redo{Objects: chosen}}}
			raw, err := Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			back, err := Encode(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, back) {
				t.Fatal("roundtrip")
			}
		})
	}
}
