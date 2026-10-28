package apply

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestPreparedStagingRequiresSelectedInstall(t *testing.T) {
	for _, installed := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepared-only", true: "committed-install"}[installed], func(t *testing.T) {
			f := newFixture(t, "sim")
			f.create("a", oid(20))
			install := preparedInstall(f, installed)
			if installed {
				f.append(uid(800), install, true)
				obj := f.objects[oid(20)]
				obj.meta = install.Redo.Objects[0].After
				obj.data = []byte("tx!")
				f.objects[oid(20)] = obj
			}
			result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(result.Store)
		})
	}
}

func TestInstallRejectsEncodedDependencyMismatches(t *testing.T) {
	for _, test := range []string{"prepare-reference", "prepare-kind", "binding", "redo"} {
		t.Run(test, func(t *testing.T) {
			f := newFixture(t, "sim")
			f.create("a", oid(20))
			install := preparedInstall(f, true)
			wantErr := error(fsapi.Invalid)
			switch test {
			case "prepare-reference":
				install.Prepare.Prepare.SHA256[0] ^= 1
				wantErr = fsapi.Stale
			case "prepare-kind":
				install.Prepare.Prepare = f.selection.Effects[0]
			case "binding":
				install.Binding.Affinity.ManifestVersion++
			case "redo":
				// Keep Binding/selected metadata unchanged, and make the new Redo/hash
				// internally valid. Only comparison with the retained PREPARED rejects it.
				install.Redo.Ledgers = []record.LedgerEdit{{Object: oid(20), ExpectedRevision: 1, ResultingRevision: 2, ExpectedNlink: 1, ResultingNlink: 1}}
				install.Prepare.RedoSHA256 = mutationRedoHash(f, record.Mutation{Intent: install.Binding.Intent, Redo: *install.Redo})
			}
			// Append and read via the real segment/record codecs, so each negative is
			// a valid encoded INSTALL that must reach Replay's semantic comparisons.
			f.append(uid(800), install, true)
			d, j, p := fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{})
			result, err := f.replay(d, j, p, f.config)
			if !errors.Is(err, wantErr) || result.Store != nil {
				t.Fatalf("invalid install returned store or wrong error: %+v %v", result, err)
			}
			raw, err := readFile(t.Context(), p, "APPLIED", 0, progressSize)
			if err != nil {
				t.Fatal(err)
			}
			applied, err := decodeProgress(raw)
			if err != nil {
				t.Fatal(err)
			}
			if applied.source != f.selection.Effects[len(f.selection.Effects)-2] {
				t.Fatal("failed INSTALL advanced applied progress")
			}
			imageBytes, err := readFile(t.Context(), p, imageName(applied.workspace, applied.index), 0, maxImageBytes(f.config))
			if err != nil {
				t.Fatal(err)
			}
			image, err := storage.DecodeImage(imageBytes, f.config.Storage.Limits)
			if err != nil {
				t.Fatal(err)
			}
			retained, err := storage.Open(t.Context(), d, j, f.config.Storage, image)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(retained)
		})
	}
}

func preparedInstall(f *fixture, withDecision bool) record.Install {
	f.t.Helper()
	old := f.objects[oid(20)].meta
	after := old
	after.Revision++
	after.Size = 3
	edit := record.ObjectEdit{Object: oid(20), ExpectedRevision: old.Revision, After: after}
	m := record.Mutation{Intent: record.Intent{Kind: 8, Arguments: record.Write{Object: oid(20), Data: []byte("tx!")}, SelectedMetadata: []record.ObjectEdit{edit}}, Redo: record.Redo{Objects: []record.ObjectEdit{edit}}}
	redoHash := mutationRedoHash(f, m)
	request := record.RequestID{Session: fsapi.SessionID{Principal: fsapi.Principal{Realm: uid(60), Subject: "fixture"}, Anchor: uid(1), Nonce: uid(61)}, Number: 1}
	document := record.InternalRequest{Version: 1, ID: request, NamespaceVolume: uid(1), View: uid(62), Intent: m.Intent}
	_, digest, err := record.EncodeInternal(document)
	if err != nil {
		f.t.Fatal(err)
	}
	binding := record.Binding{Request: request, RequestDigest: digest, Affinity: record.AffinityRef{Closure: uid(63), ManifestVersion: 1}, Coordinator: uid(1), Intent: m.Intent, Participants: []fsapi.UUID{uid(1)}, Authorities: []record.Authority{f.authority()}}
	// Author retained dependencies in protocol order. Selection/protection are
	// still supplied explicitly by the test authorizer.
	f.append(uid(801), record.Session{Principal: request.Session.Principal, Anchor: request.Session.Anchor, Nonce: request.Session.Nonce, State: 1}, false)
	claim := f.append(uid(802), record.RequestClaim{Document: record.RequestDocument{Class: 2, Internal: &document}, Digest: digest}, false)
	bound := f.append(uid(803), record.RequestBind{Claim: claim, Binding: binding}, false)
	begin := f.append(uid(804), record.Begin{Binding: binding, Bind: bound, Document: record.RequestDocument{Class: 2, Internal: &document}, Term: 1}, false)
	prep := f.append(uid(800), record.Prepared{Request: request, Digest: digest, Begin: begin, Binding: binding, Term: 1, Operation: m, RedoSHA256: redoHash}, false)
	decisionID := record.DecisionID{Coordinator: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Request: request, Term: 1}
	source := oid(20)
	decisionFrame := begin
	if withDecision {
		decisionFrame = f.append(uid(805), record.Decision{ID: decisionID, Digest: digest, Begin: begin, Outcome: 1, Prepares: []record.PrepareRef{{Participant: uid(1), Prepare: prep, RedoSHA256: redoHash}}, Result: record.TerminalResult{Status: 1, Source: &source}, Authority: f.authority()}, false)
	}
	decision := record.DecisionRef{ID: decisionID, Decision: decisionFrame}
	return record.Install{Request: request, Digest: digest, Binding: binding, Decision: decision, Outcome: 1, Prepare: &record.PrepareRef{Participant: uid(1), Prepare: prep, RedoSHA256: redoHash}, Redo: &m.Redo, Applied: prep.Position}
}

func mutationRedoHash(f *fixture, m record.Mutation) record.Hash {
	f.t.Helper()
	// Intent precedes Redo; the two empty authorization/dependency counts and
	// checksum follow it. Hash the original codec bytes rather than Go structs.
	h := f.header(uid(800))
	h.Sequence = 1
	raw, err := record.Encode(record.Record{Header: h, Payload: m})
	if err != nil {
		f.t.Fatal(err)
	}
	intent, err := record.EncodeIntent(m.Intent)
	if err != nil {
		f.t.Fatal(err)
	}
	return sha256.Sum256(raw[144+len(intent) : len(raw)-12])
}
