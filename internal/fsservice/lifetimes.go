package fsservice

import (
	"context"
	"errors"
	"io"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

// RetirementRequest identifies the exact orphan and current selected history.
// Pins includes every active and drained obligation; Releases binds prior drains. A new Workspace UUID
// is never evidence that old kernel references, descriptors or mappings ended.
type RetirementRequest struct {
	Authority  record.Authority
	Generation fsapi.UUID
	Through    record.FrameRef
	Object     storage.Object
	Pins       []storage.Pin
	Releases   []record.FrameRef
}

// RetirementProof binds authenticated retention and old-generation cache fencing
// to the request. Fence must cover every supplied active pin, including caches,
// mappings and transferred descriptors, not merely a stopped daemon process.
type RetirementProof struct {
	Through                      record.FrameRef
	Generation, OwnerIncarnation fsapi.UUID
	Object                       fsapi.ObjectID
	Revision, LedgerRevision     uint64
	Retention                    record.FrameRef
	Fence                        *record.FrameRef
	Lease                        RetirementLease
}

// RetirementLease keeps retirement/fence/retention authority stable through apply.
// Release ends the reservation after success or failure and must not reenter Service.
type RetirementLease interface{ Release() }

// ReclamationPolicy verifies current authoritative retirement/fence/retention
// evidence and retains it through the ensuing protected release/reclaim. It must
// independently authenticate reference contents, history, coverage, ownership and
// current exclusion; a syntactically valid FrameRef or changed UUID is no proof.
// Busy/WouldBlock conservatively retain resources. Other errors block serving.
// No production verifier is supplied. Protocol.Authorize must independently
// verify the selected lifetime effects during online apply and every recovery.
type ReclamationPolicy interface {
	AuthorizeRetirement(context.Context, RetirementRequest) (RetirementProof, error)
}

func (s *Service) reap(ctx context.Context) error {
	if s.config.Reclamation == nil {
		return nil
	}
	image, err := s.applied.Store.Export(ctx)
	if err != nil {
		return s.block(err)
	}
	for _, object := range image.Objects {
		if object.Attr.Object == s.config.Storage.Root || object.Attr.Nlink != 0 || len(object.References) != 0 || s.lifetimes[object.Attr.Object] != nil {
			continue
		}
		request, active, releases, err := s.retirementRequest(ctx, object, image.Pins)
		if err != nil {
			return s.block(err)
		}
		if err = s.reapObject(ctx, request, active, releases); err != nil {
			return err
		}

	}
	return nil
}
func (s *Service) retirementRequest(ctx context.Context, object storage.Object, pins []storage.Pin) (RetirementRequest, []storage.Pin, []record.FrameRef, error) {
	request := RetirementRequest{Authority: s.config.Authority, Generation: s.config.Workspace.ID, Through: s.selection.Through, Object: object}
	request.Authority.Members = append([]fsapi.UUID(nil), request.Authority.Members...)
	for _, pin := range pins {
		if pin.Object == object.Attr.Object {
			request.Pins = append(request.Pins, pin)
		}
	}
	// Pass an independently owned object image to the external verifier.
	request.Object.References = append([]storage.Reference(nil), object.References...)
	request.Object.Text = append([]byte(nil), object.Text...)
	if object.Parent != nil {
		parent := *object.Parent
		request.Object.Parent = &parent
	}
	releases, e := s.releasedPins(ctx, object.Attr.Object)
	if e != nil {
		return RetirementRequest{}, nil, nil, e
	}
	request.Releases = append([]record.FrameRef(nil), releases...)
	var active []storage.Pin
	for _, pin := range request.Pins {
		if pin.Active {
			active = append(active, pin)
		}
	}
	return request, active, releases, nil
}
func (s *Service) reapObject(ctx context.Context, request RetirementRequest, active []storage.Pin, releases []record.FrameRef) error {
	proof, e := s.config.Reclamation.AuthorizeRetirement(ctx, request)
	if proof.Lease != nil {
		defer proof.Lease.Release()
	}
	if proof.Fence != nil {
		fence := *proof.Fence
		proof.Fence = &fence
	}
	if errors.Is(e, fsapi.Busy) || errors.Is(e, fsapi.WouldBlock) {
		return nil
	}
	if e != nil {
		return s.block(e)
	}
	if proof.Lease == nil || proof.Generation != s.config.Workspace.ID || proof.OwnerIncarnation != s.config.Authority.OwnerIncarnation || proof.Through != s.selection.Through || proof.Object != request.Object.Attr.Object || proof.Revision != request.Object.Attr.Revision || proof.LedgerRevision != request.Object.LedgerRevision || !validEvidence(proof.Retention) || len(active) > 0 && (proof.Fence == nil || !validEvidence(*proof.Fence)) {
		return s.block(fsapi.Permission)
	}
	for _, pin := range active {
		op, e := s.newUUID()
		if e != nil {
			return e
		}
		if e = s.protect(ctx, op, record.HandleRelease{Pin: pin.ID, Object: pin.Object, ServingGeneration: pin.Generation, Incarnation: pin.Incarnation, Reason: 2, Fence: proof.Fence}); e != nil {
			return e
		}
		releases = append(releases, s.selection.Through)
	}
	op, e := s.newUUID()
	if e != nil {
		return e
	}
	if e = s.protect(ctx, op, record.Reclaim{Object: request.Object.Attr.Object, ExpectedObjectRevision: request.Object.Attr.Revision, ExpectedLedgerRevision: request.Object.LedgerRevision, Retention: proof.Retention, PinsReleased: releases}); e != nil {
		return e
	}
	return nil
}
func validEvidence(r record.FrameRef) bool {
	return r.Position.Valid() && r.StreamKind == 1 && r.Reporter == nil && r.ReporterIncarnation == nil && r.SHA256 != ([32]byte{})
}

func (s *Service) releasedPins(ctx context.Context, object fsapi.ObjectID) ([]record.FrameRef, error) {
	reader, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	selected := make(map[record.FrameRef]bool, len(s.selection.Effects))
	for _, ref := range s.selection.Effects {
		selected[ref] = true
	}
	var refs []record.FrameRef
	for {
		entry, err := reader.Next(ctx)
		if err == io.EOF {
			return refs, nil
		}
		if err != nil {
			return nil, err
		}
		if release, ok := entry.Record.Payload.(record.HandleRelease); ok && release.Object == object && selected[entry.Reference] {
			refs = append(refs, entry.Reference)
		}
	}
}
