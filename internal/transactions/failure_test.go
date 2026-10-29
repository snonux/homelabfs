package transactions

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestProtectionNeverDowngrades(t *testing.T) {
	for _, name := range []string{"session", "connection", "partner", "incarnation", "candidate", "prefix", "authority"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.protector.mutate = func(r *ProtectionReceipt) {
				switch name {
				case "session":
					r.Session = uuid(100)
				case "connection":
					r.Connection = uuid(100)
				case "partner":
					r.Partner = uuid(100)
				case "incarnation":
					r.Incarnation = uuid(100)
				case "candidate":
					r.Candidate.SHA256[0]++
				case "prefix":
					r.Retained = nil
				case "authority":
					f.protector.authority = io.ErrClosedPipe
				}
			}
			_, err := f.engine.Propose(context.Background(), uuid(1), record.Session{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, State: 1})
			if err == nil {
				t.Fatal("accepted invalid retention")
			}
			if len(f.engine.Retained()) != 3 {
				t.Fatal("uncertain candidate not retained")
			}
			if err := f.engine.Reconcile(context.Background()); err == nil {
				t.Fatal("reconciliation fabricated evidence")
			}
		})
	}
}

func TestDecisionRequiresTypedCurrentAcceptance(t *testing.T) {
	for _, name := range []string{"missing", "wrong_type", "term", "candidate", "partner", "incarnation", "authority", "prefix", "request"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:7] {
				step()
			}
			f.protector.mutate = func(receipt *ProtectionReceipt) {
				if name == "missing" {
					receipt.Acceptance = nil
					return
				}
				r, err := record.Decode(receipt.Acceptance)
				if err != nil {
					t.Fatal(err)
				}
				a := r.Payload.(record.Accept)
				switch name {
				case "wrong_type":
					r.Payload = record.TailReport{RecoveryID: uuid(70), Reporter: a.Reporter, ReporterIncarnation: a.ReporterIncarnation}
				case "term":
					a.Term++
				case "candidate":
					a.Candidate.SHA256[0]++
				case "partner":
					a.Reporter = uuid(12)
				case "incarnation":
					a.ReporterIncarnation = uuid(22)
					r.Header.Incarnation = uuid(22)
				case "authority":
					a.Authority.OwnerIncarnation = uuid(22)
				case "prefix":
					a.Retained = nil
				case "request":
					a.Request.Number++
				}
				if name != "wrong_type" {
					r.Payload = a
				}
				receipt.Acceptance, err = record.Encode(r)
				if err != nil {
					t.Fatal(err)
				}
			}
			d := decisionPayload(f)
			entry, err := f.engine.Propose(context.Background(), uuid(1), d)
			if err == nil {
				t.Fatal("accepted invalid slot evidence")
			}
			if _, err := f.engine.Cancel(context.Background(), f.id, 15); !errors.Is(err, ErrEvidence) {
				t.Fatalf("uncertainty permitted cancellation: %v", err)
			}
			if len(entry.Bytes) == 0 {
				t.Fatal("lost local candidate")
			}
			if _, err := f.engine.Acceptance(entry.Reference); !errors.Is(err, ErrEvidence) {
				t.Fatal("candidate called chosen")
			}
			f.protector.mutate = nil
			if err := f.engine.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			chosen, err := f.engine.Cancel(context.Background(), f.id, 15)
			if err != nil || chosen.Record.Payload.(record.Decision).Outcome != 1 {
				t.Fatalf("commit changed on cancellation: %v", err)
			}
		})
	}
}

func decisionPayload(f *fixture) record.Decision {
	var refs []record.PrepareRef
	for _, entry := range f.prepares {
		p := entry.Record.Payload.(record.Prepared)
		refs = append(refs, record.PrepareRef{Participant: entry.Record.Header.Volume, Prepare: entry.Reference, RedoSHA256: p.RedoSHA256})
	}
	a := f.binding.Authorities[0]
	object := f.binding.Intent.Arguments.(record.Write).Object
	return record.Decision{ID: record.DecisionID{Coordinator: a.Volume, Generation: a.Generation, Configuration: a.Configuration, Epoch: a.Epoch, Request: f.id, Term: 1}, Digest: f.digest, Begin: f.begin.Reference, Outcome: 1, Prepares: refs, Result: record.TerminalResult{Status: 1, Source: &object}, Authority: a}
}

func TestMissingContradictoryTransitionsDoNotAppend(t *testing.T) {
	for _, name := range []string{"prepare_set", "decision_term", "decision_digest", "bind_digest", "participant", "retained_frame", "unprotected_result", "cancel_code"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:7] {
				step()
			}
			var payload any
			volume := uuid(1)
			switch name {
			case "prepare_set":
				d := decisionPayload(f)
				d.Prepares = d.Prepares[:1]
				payload = d
			case "decision_term":
				d := decisionPayload(f)
				d.ID.Term++
				payload = d
			case "decision_digest":
				d := decisionPayload(f)
				d.Digest[0]++
				payload = d
			case "bind_digest":
				b := f.binding
				b.RequestDigest[0]++
				payload = record.RequestBind{Claim: f.claim.Reference, Binding: b}
			case "participant":
				p := f.prepares[0].Record.Payload.(record.Prepared)
				volume = uuid(2)
				payload = p
			case "retained_frame":
				p := f.prepares[0].Record.Payload.(record.Prepared)
				p.Retained = []record.FrameRef{{StreamKind: 1, Position: record.Position{Volume: uuid(1), Generation: uuid(3), Configuration: uuid(4), Epoch: 1, Sequence: 900}}}
				payload = p
			case "unprotected_result":
				ref := f.bind.Reference
				payload = record.RequestResult{Request: f.id, Digest: f.digest, Claim: f.claim.Reference, Binding: &ref, Result: record.TerminalResult{Status: 3, Code: 1}}
			case "cancel_code":
				d := decisionPayload(f)
				d.Outcome = 2
				d.Result = record.TerminalResult{Status: 2}
				payload = d
			}
			before := len(f.engine.Retained())
			if _, err := f.engine.Propose(context.Background(), volume, payload); err == nil {
				t.Fatal("invalid transition accepted")
			}
			if len(f.engine.Retained()) != before {
				t.Fatal("invalid transition persisted")
			}
		})
	}
}

func TestAnchorFloorAndPermanentCloseSurviveCrash(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	steps[0]()
	steps[1]()
	result := f.propose(uuid(1), record.RequestResult{Request: f.id, Digest: f.digest, Claim: f.claim.Reference, Result: record.TerminalResult{Status: 3, Code: 1}})
	retirement := f.propose(uuid(1), record.SessionRetire{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, NewFloor: 3, TerminalResults: []record.FrameRef{result.Reference}})
	f.restart()
	for _, number := range []uint64{1, 2, 3} {
		id := f.id
		id.Number = number
		if _, err := f.engine.GetStatus(context.Background(), id); !errors.Is(err, ErrRetired) {
			t.Fatalf("floor %d: %v", number, err)
		}
	}
	if _, err := f.engine.Propose(context.Background(), uuid(1), f.claim.Record.Payload); !errors.Is(err, ErrRetired) {
		t.Fatalf("retired claim retry: %v", err)
	}
	f.propose(uuid(1), record.SessionClose{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, Floor: 3, Retirement: &retirement.Reference})
	f.restart()
	if _, err := f.engine.Propose(context.Background(), uuid(1), record.Session{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, State: 1}); !errors.Is(err, ErrRetired) {
		t.Fatalf("registration recreated closed session: %v", err)
	}
	if _, err := f.engine.GetStatus(context.Background(), record.RequestID{Session: f.id.Session, Number: 100}); !errors.Is(err, ErrRetired) {
		t.Fatal("tombstone did not cover future requests")
	}
	if len(f.engine.Retained()) < 6 {
		t.Fatal("floor released dependency pins")
	}
}

func TestUnresolvedClaimBlocksRetirementAndClose(t *testing.T) {
	f := newFixture(t)
	steps := f.steps()
	steps[0]()
	steps[1]()
	for _, payload := range []any{record.SessionRetire{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, NewFloor: 2}, record.SessionClose{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce}} {
		if _, err := f.engine.Propose(context.Background(), uuid(1), payload); err == nil {
			t.Fatal("unresolved session retired")
		}
	}
}

func TestConcurrentClaimUsesImmutableAnchor(t *testing.T) {
	f := newFixture(t)
	f.steps()[0]()
	request := record.InternalRequest{Version: 1, ID: f.id, NamespaceVolume: uuid(1), View: uuid(51), Intent: record.Intent{Kind: 7, Arguments: record.Rename{Source: record.ObjectID{Home: uuid(1), Allocation: uuid(50)}, SourceParent: record.ObjectID{Home: uuid(1), Allocation: uuid(51)}, DestinationParent: record.ObjectID{Home: uuid(2), Allocation: uuid(52)}, SourceName: "a", DestinationName: "b", Policy: 1, Noop: true}}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		request.Intent.Arguments = func() record.Rename {
			r := request.Intent.Arguments.(record.Rename)
			r.DestinationName = fmt.Sprint(i)
			return r
		}()
		copy := request
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, digest, err := record.EncodeInternal(copy)
			if err == nil {
				_, err = f.engine.Propose(context.Background(), uuid(1), record.RequestClaim{Document: record.RequestDocument{Class: 2, Internal: &copy}, Digest: digest})
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("claims %d conflicts %d", successes, conflicts)
	}
	f.restart()
	status, err := f.engine.GetStatus(context.Background(), f.id)
	if err != nil || status.Binding != nil || status.Result != nil {
		t.Fatalf("anchored claim %+v %v", status, err)
	}
}

func TestPersistedContradictionAndMissingParticipantFailClosed(t *testing.T) {
	for _, mode := range []string{"contradiction", "missing"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			for _, step := range steps[:7] {
				step()
			}
			if err := f.engine.Close(); err != nil {
				t.Fatal(err)
			}
			if mode == "contradiction" {
				g := f.options.Groups[0]
				w, err := segment.NewWriter(context.Background(), g.Backend, g.Stream, g.Writer)
				if err != nil {
					t.Fatal(err)
				}
				h := f.header()
				h.Volume = g.Stream.Volume
				h.Generation = g.Stream.Generation
				h.Configuration = g.Stream.Configuration
				h.Epoch = g.Stream.Epoch
				_, err = w.Append(context.Background(), record.Record{Header: h, Payload: f.claim.Record.Payload})
				if err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				g := f.options.Groups[1]
				reader, err := segment.Recover(context.Background(), g.Backend, g.Stream, segment.RecoveryOptions{})
				if err != nil {
					t.Fatal(err)
				}
				for _, summary := range reader.Segments {
					if err := g.Backend.Unlink(context.Background(), summary.Name); err != nil {
						t.Fatal(err)
					}
				}
				if err := g.Backend.SyncDir(context.Background(), g.Stream.Directory()); err != nil {
					t.Fatal(err)
				}
			}
			f.fs.Crash()
			engine, err := Open(context.Background(), f.options)
			if engine != nil {
				_ = engine.Close()
			}
			if mode == "contradiction" && !errors.Is(err, ErrConflict) || mode == "missing" && !errors.Is(err, ErrEvidence) {
				t.Fatalf("recovery %s: %v", mode, err)
			}
		})
	}
}

func TestJournalFaultNeedsReopenAndNeverAborts(t *testing.T) {
	for _, hook := range []string{"before-append", "after-append", "before-sync", "after-sync"} {
		for _, loss := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
			t.Run(fmt.Sprintf("%s/%d", hook, loss), func(t *testing.T) {
				f := newFixtureWithLoss(t, loss)
				steps := f.steps()
				steps[0]()
				if err := f.engine.Close(); err != nil {
					t.Fatal(err)
				}
				armed := false
				for i := range f.options.Groups {
					f.options.Groups[i].Writer.Failpoint = func(name string) error {
						if armed && name == hook {
							return io.ErrUnexpectedEOF
						}
						return nil
					}
				}
				var err error
				f.engine, err = Open(context.Background(), f.options)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.engine.Reconcile(context.Background()); err != nil {
					t.Fatal(err)
				}
				armed = true
				request := record.InternalRequest{Version: 1, ID: f.id, NamespaceVolume: uuid(1), View: uuid(51), Intent: record.Intent{Kind: 7, Arguments: record.Rename{Source: record.ObjectID{Home: uuid(1), Allocation: uuid(50)}, SourceParent: record.ObjectID{Home: uuid(1), Allocation: uuid(51)}, DestinationParent: record.ObjectID{Home: uuid(2), Allocation: uuid(52)}, SourceName: "a", DestinationName: "b", Policy: 1, Noop: true}}}
				_, digest, err := record.EncodeInternal(request)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.engine.Propose(context.Background(), uuid(1), record.RequestClaim{Document: record.RequestDocument{Class: 2, Internal: &request}, Digest: digest})
				if !errors.Is(err, ErrEvidence) {
					t.Fatalf("append fault: %v", err)
				}
				if err := f.engine.Reconcile(context.Background()); !errors.Is(err, ErrEvidence) {
					t.Fatal("poisoned writer reconciled without reopening")
				}
				armed = false
				f.restart()
				status, statusErr := f.engine.GetStatus(context.Background(), f.id)
				if status.Result != nil {
					t.Fatal("journal error invented terminal outcome")
				}
				if statusErr != nil && !errors.Is(statusErr, ErrEvidence) {
					t.Fatal(statusErr)
				}
			})
		}
	}
}

func TestCapacityAndMandatoryCapabilities(t *testing.T) {
	f := newFixture(t)
	for _, change := range []func(*Options){func(o *Options) { o.Protector = nil }, func(o *Options) { o.Applier = nil }, func(o *Options) { o.Header = nil }} {
		o := f.options
		change(&o)
		engine, err := Open(context.Background(), o)
		if engine != nil {
			_ = engine.Close()
		}
		if !errors.Is(err, ErrEvidence) {
			t.Fatal("missing capability accepted")
		}
	}
	f.engine.opts.MaximumFrames = 3
	if _, err := f.engine.Propose(context.Background(), uuid(1), record.Session{Principal: f.id.Session.Principal, Anchor: f.id.Session.Anchor, Nonce: f.id.Session.Nonce, State: 1}); !errors.Is(err, ErrLimit) {
		t.Fatalf("capacity: %v", err)
	}
	if _, err := f.fs.Open(context.Background(), "applied-"+uuid(1).String(), os.O_RDONLY, 0); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("capacity performed effects")
	}
}

func TestBindingCannotChangeClaimedIntent(t *testing.T) {
	for _, mode := range []string{"internal_intent", "public_link_to_write"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			steps := f.steps()
			steps[0]()
			var intent record.Intent
			if mode == "internal_intent" {
				steps[1]()
				intent = f.document.Internal.Intent
				write := intent.Arguments.(record.Write)
				write.Data = []byte("other bytes")
				intent.Arguments = write
			} else {
				original := record.OriginalRequest{Version: 1, ID: f.id, Operation: 2, Source: record.Endpoint{Volume: uuid(1), View: uuid(51), Components: []string{"a"}}, Destination: record.Endpoint{Volume: uuid(2), View: uuid(52), Components: []string{"b"}}, Replacement: 2}
				bytes, err := record.EncodeOriginal(original)
				if err != nil {
					t.Fatal(err)
				}
				f.document = record.RequestDocument{Class: 1, Original: &original}
				f.digest = sha256.Sum256(bytes)
				f.claim = f.propose(uuid(1), record.RequestClaim{Document: f.document, Digest: f.digest})
				object := record.ObjectID{Home: uuid(1), Allocation: uuid(50)}
				intent = record.Intent{Kind: 8, Arguments: record.Write{Object: object, Data: []byte("exact bytes")}, SelectedMetadata: []record.ObjectEdit{{Object: object, ExpectedRevision: 1, After: record.Metadata{Revision: 2, Type: 1, Mode: 0600, Size: 11, Nlink: 1}}}}
			}
			var authorities []record.Authority
			for _, g := range f.options.Groups {
				authorities = append(authorities, g.Binding.Authority)
			}
			manifest := record.Affinity{Closure: uuid(60), ManifestVersion: 1, Members: []record.UUID{uuid(1), uuid(2)}, Authorities: authorities}
			entry := f.propose(uuid(1), manifest)
			f.propose(uuid(2), manifest)
			binding := record.Binding{Request: f.id, RequestDigest: f.digest, Affinity: affinityRef(manifest, entry.Bytes), Coordinator: uuid(1), Intent: intent, Participants: manifest.Members, Authorities: authorities}
			before, prefix := len(f.engine.Retained()), len(f.protector.retained)
			_, err := f.engine.Propose(context.Background(), uuid(1), record.RequestBind{Claim: f.claim.Reference, Binding: binding})
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("contradictory bind %v", err)
			}
			if len(f.engine.Retained()) != before || len(f.protector.retained) != prefix || f.engine.state.requests[f.id].bind != nil {
				t.Fatal("contradictory binding appended/protected/fixed state")
			}
		})
	}
}
