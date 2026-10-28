package fsservice

import (
	"context"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

// Synthetic authority/retention fixtures exercise the integration contract only;
// these references do not constitute authenticated production retirement proof.
type syntheticRetirement struct {
	retention                           record.FrameRef
	allow, fenced                       bool
	calls, released, activePins, drains int
	corrupt                             func(*RetirementProof)
}
type syntheticLease struct{ released *int }

func (l syntheticLease) Release() { *l.released++ }
func (v *syntheticRetirement) AuthorizeRetirement(_ context.Context, r RetirementRequest) (RetirementProof, error) {
	v.calls++
	v.activePins = 0
	v.drains = len(r.Releases)
	for _, p := range r.Pins {
		if p.Active {
			v.activePins++
		}
	}
	if !v.allow || v.activePins > 0 && !v.fenced {
		return RetirementProof{}, fsapi.Busy
	}
	proof := RetirementProof{Generation: r.Generation, OwnerIncarnation: r.Authority.OwnerIncarnation, Through: r.Through, Object: r.Object.Attr.Object, Revision: r.Object.Attr.Revision, LedgerRevision: r.Object.LedgerRevision, Retention: v.retention, Lease: syntheticLease{&v.released}}
	if v.fenced {
		fence := v.retention
		proof.Fence = &fence
	}
	if v.corrupt != nil {
		v.corrupt(&proof)
	}
	return proof, nil
}
func crashRestart(f *fixture) {
	f.t.Helper()
	if err := f.service.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.service = nil
	for _, root := range f.roots {
		root.(*vfs.Simulator).Crash()
	}
	f.workspace()
	f.start()
}

func TestUnlinkedLifetimeIndependentLookupOpenAndHardLinks(t *testing.T) {
	for _, backend := range []string{"sim", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := setup(t, backend)
			proof := &syntheticRetirement{allow: true, retention: f.c.Selection.Through}
			f.c.Reclamation = proof
			f.start()
			a := f.create(f.c.Storage.Root, "file")
			id := a.Attr.Object
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("old")})
			f.ok(fsapi.Command{Operation: fsapi.LinkOp, Object: id, Destination: f.c.Storage.Root, NewName: "alias"})
			b := f.ok(fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "file"})
			f.ok(fsapi.Command{Operation: fsapi.Forget, Object: id, Count: 1})
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "alias"})
			f.data(a.Handle, []byte("old"))
			f.ok(fsapi.Command{Operation: fsapi.Release, Handle: a.Handle})
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: b.Handle, Offset: 3, Data: []byte("-live")})
			f.data(b.Handle, []byte("old-live"))
			if proof.calls != 0 {
				t.Fatal("reclaim attempted while open", proof.calls)
			}
			f.ok(fsapi.Command{Operation: fsapi.Release, Handle: b.Handle})
			assertCode(t, f.request(fsapi.Command{Operation: fsapi.Getattr, Object: id}), fsapi.NotFound)
			if proof.calls != 1 || proof.released != 1 || proof.activePins != 0 || proof.drains != 1 {
				t.Fatal("missing final drain/lease evidence", proof)
			}
			// A later object receives a different identity and cannot revive a retired ID.
			next := f.create(f.c.Storage.Root, "file")
			if next.Attr.Object == id {
				t.Fatal("retired allocation reused")
			}
		})
	}
}

func TestLookupStillPinsAfterFinalOpenClose(t *testing.T) {
	f := setup(t, "sim")
	proof := &syntheticRetirement{allow: true, retention: f.c.Selection.Through}
	f.c.Reclamation = proof
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	id := a.Attr.Object
	f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "file"})
	f.ok(fsapi.Command{Operation: fsapi.Release, Handle: a.Handle})
	if proof.calls != 0 {
		t.Fatal("lookup obligation ignored")
	}
	f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: id})
	f.ok(fsapi.Command{Operation: fsapi.Forget, Object: id, Count: 1})
	assertCode(t, f.request(fsapi.Command{Operation: fsapi.Getattr, Object: id}), fsapi.NotFound)
}

func TestCrashOrphanRequiresAuthenticExclusionAndRetention(t *testing.T) {
	f := setup(t, "sim")
	proof := &syntheticRetirement{retention: f.c.Selection.Through}
	f.c.Reclamation = proof
	f.start()
	a := f.create(f.c.Storage.Root, "file")
	id := a.Attr.Object
	old := f.c.Workspace.ID
	f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("retained")})
	f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "file"})
	crashRestart(f)
	assertCode(t, f.request(fsapi.Command{Operation: fsapi.Read, Handle: a.Handle, Generation: old, Size: 8}), fsapi.Stale)
	proof.allow = true // Current owner alone is insufficient; no old-cache fence.
	f.ok(fsapi.Command{Operation: fsapi.Reap})
	attr := f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: id}).Attr
	if attr.Size != 8 || attr.Nlink != 0 || proof.activePins != 1 {
		t.Fatal(attr, proof)
	}
	// Positive independent old-incarnation exclusion plus retained history permits GC.
	proof.fenced = true
	f.ok(fsapi.Command{Operation: fsapi.Reap})
	assertCode(t, f.request(fsapi.Command{Operation: fsapi.Getattr, Object: id}), fsapi.NotFound)
	if proof.released != 1 {
		t.Fatal("retirement lease leaked", proof)
	}
	crashRestart(f)
	assertCode(t, f.request(fsapi.Command{Operation: fsapi.Getattr, Object: id}), fsapi.NotFound)
}

func TestReclaimInterruptedReleaseAndReclaimReplay(t *testing.T) {
	for _, boundary := range []apply.Boundary{apply.AfterApply, apply.EffectsDurable, apply.AfterRename} {
		for _, failAt := range []int{1, 2} {
			t.Run(string(boundary)+string(rune('0'+failAt)), func(t *testing.T) {
				f := setup(t, "sim")
				proof := &syntheticRetirement{retention: f.c.Selection.Through}
				f.c.Reclamation = proof
				armed, calls := false, 0
				f.c.Apply.Hook = func(_ context.Context, b apply.Boundary) error {
					if armed && b == boundary {
						calls++
						if calls == failAt {
							armed = false
							return errors.New("interrupted lifetime apply")
						}
					}
					return nil
				}
				f.start()
				a := f.create(f.c.Storage.Root, "file")
				id := a.Attr.Object
				f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("preserve")})
				f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "file"})
				crashRestart(f)
				proof.allow = true
				proof.fenced = true
				armed = true
				assertCode(t, f.request(fsapi.Command{Operation: fsapi.Reap}), fsapi.IO)
				if f.service.State() != Blocked || proof.released != 1 {
					t.Fatal("uncertain cleanup not gated", proof)
				}
				crashRestart(f)
				if failAt == 1 {
					f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: id})
					f.ok(fsapi.Command{Operation: fsapi.Reap})
				}
				assertCode(t, f.request(fsapi.Command{Operation: fsapi.Getattr, Object: id}), fsapi.NotFound)
			})
		}
	}
}

func TestRetirementProofMismatchFailsClosed(t *testing.T) {
	for name, corrupt := range map[string]func(*RetirementProof){
		"generation":  func(p *RetirementProof) { p.Generation = uid(59000) },
		"incarnation": func(p *RetirementProof) { p.OwnerIncarnation = uid(59001) },
		"cut":         func(p *RetirementProof) { p.Through.Position.Sequence++ },
		"object":      func(p *RetirementProof) { p.Object.Allocation = uid(56000) },
		"revision":    func(p *RetirementProof) { p.Revision++ },
		"retention":   func(p *RetirementProof) { p.Retention.SHA256 = [32]byte{} },
		"fence":       func(p *RetirementProof) { p.Fence = nil },
		"lease":       func(p *RetirementProof) { p.Lease = nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t, "sim")
			proof := &syntheticRetirement{retention: f.c.Selection.Through}
			f.c.Reclamation = proof
			f.start()
			a := f.create(f.c.Storage.Root, "file")
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "file"})
			crashRestart(f)
			proof.allow = true
			proof.fenced = true
			proof.corrupt = corrupt
			assertCode(t, f.request(fsapi.Command{Operation: fsapi.Reap}), fsapi.Permission)
			if f.service.State() != Blocked {
				t.Fatal("invalid proof did not block")
			}
			proof.corrupt = nil
			proof.allow = false
			crashRestart(f)
			f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: a.Attr.Object})
		})
	}
}

type leaseFunc func()

func (f leaseFunc) Release() { f() }

type exclusiveRetirement struct {
	syntheticRetirement
	held, busyFirst        bool
	acquisitions, releases int
}

func (v *exclusiveRetirement) AuthorizeRetirement(ctx context.Context, r RetirementRequest) (RetirementProof, error) {
	if !v.allow {
		return RetirementProof{}, fsapi.Busy
	}
	if v.held {
		return RetirementProof{}, errors.New("non-reentrant retirement lease already held")
	}
	p, err := v.syntheticRetirement.AuthorizeRetirement(ctx, r)
	if err != nil {
		return p, err
	}
	v.held = true
	v.acquisitions++
	p.Lease = leaseFunc(func() { v.held = false; v.releases++ })
	if v.busyFirst && v.acquisitions == 1 {
		return p, fsapi.Busy
	}
	return p, nil
}
func TestReapReleasesExclusiveLeaseBeforeNextOrphan(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "busy-with-lease"}[busy], func(t *testing.T) {
			f := setup(t, "sim")
			v := &exclusiveRetirement{syntheticRetirement: syntheticRetirement{retention: f.c.Selection.Through, fenced: true}, busyFirst: busy}
			f.c.Reclamation = v
			f.start()
			a := f.create(f.c.Storage.Root, "first")
			b := f.create(f.c.Storage.Root, "second")
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "first"})
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: f.c.Storage.Root, Name: "second"})
			crashRestart(f)
			v.allow = true
			f.ok(fsapi.Command{Operation: fsapi.Reap})
			if v.held || v.acquisitions != 2 || v.releases != 2 {
				t.Fatal("retirement lease overlaps next object", v)
			}
			if busy {
				f.ok(fsapi.Command{Operation: fsapi.Reap})
			}
			for _, id := range []fsapi.ObjectID{a.Attr.Object, b.Attr.Object} {
				assertCode(t, f.request(fsapi.Command{Operation: fsapi.Getattr, Object: id}), fsapi.NotFound)
			}
		})
	}
}
