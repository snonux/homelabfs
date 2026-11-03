package fsservice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestControlledServiceExcludesReusedIdentities(t *testing.T) {
	f := setup(t, "os")
	permissions, err := NewUnixPermissions(uid(60))
	if err != nil {
		t.Fatal(err)
	}
	f.c.Permissions = permissions
	f.start()
	operation := uid(61)
	allocation := fsapi.ObjectID{Home: f.c.Storage.Volume, Allocation: uid(62)}
	c := fsapi.Command{Mount: f.c.Storage.Volume, Generation: f.c.Workspace.ID, Operation: fsapi.Create, Parent: f.c.Storage.Root, Name: "controlled", Mode: 0660, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}}
	err = f.service.Controlled(t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
		plan, err := cut.PlanControlled(ctx, fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}, c, operation, allocation, nil)
		if err != nil {
			return err
		}
		if _, err = cut.ApplyControlled(ctx, plan, false); err != nil {
			return err
		}
		if _, err = cut.PlanControlled(ctx, fsapi.Cred{}, c, operation, allocation, nil); !errors.Is(err, fsapi.Stale) {
			t.Fatalf("operation reused: %v", err)
		}
		if _, err = cut.PlanControlled(ctx, fsapi.Cred{}, c, uid(63), allocation, nil); !errors.Is(err, fsapi.Stale) {
			t.Fatalf("allocation reused: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestControlledLifetimeRecoveryRequiresEffectEvidence(t *testing.T) {
	for _, op := range []fsapi.Operation{fsapi.Open, fsapi.Lookup, fsapi.Release, fsapi.Forget, fsapi.DescriptorClose, fsapi.ProcessExit} {
		for _, applied := range []bool{false, true} {
			t.Run(fmt.Sprintf("op-%d/applied-%v", op, applied), func(t *testing.T) {
				f := setup(t, "os")
				policy, err := NewUnixPermissions(uid(60))
				if err != nil {
					t.Fatal(err)
				}
				f.c.Permissions = policy
				f.start()
				created := f.create(f.c.Storage.Root, "file")
				c := fsapi.Command{Mount: f.c.Storage.Volume, Generation: f.c.Workspace.ID, Operation: op, Object: created.Attr.Object, Handle: created.Handle, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}, Lock: fsapi.Lock{Owner: owner(f, 1)}}
				if op == fsapi.Open {
					c.Handle = 0
				}
				if op == fsapi.Lookup {
					c.Handle = 0
					c.Parent = f.c.Storage.Root
					c.Name = "file"
				}
				if op == fsapi.Release {
					c.DescriptionOnly = true
				}
				if op == fsapi.Forget {
					c.Handle = 0
					c.Count = 1
				}
				if op == fsapi.ProcessExit {
					c.Handle = 0
				}
				var plan ControlledPlan
				err = f.service.Controlled(t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
					p, e := cut.PlanControlled(ctx, fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}, c, uid(61), fsapi.ObjectID{}, nil)
					if e != nil {
						return e
					}
					plan = p
					if applied {
						_, e = cut.ApplyControlled(ctx, plan, false)
					}
					return e
				})
				if err != nil {
					t.Fatal(err)
				}
				var intervening fsapi.Result
				if !applied && op == fsapi.Open {
					intervening = f.ok(fsapi.Command{Operation: fsapi.Open, Object: created.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
					if intervening.Handle != plan.Result.Handle {
						t.Fatal("test did not consume predicted handle")
					}
				}
				err = f.service.Controlled(t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
					handles, next, lookups, locks, exited := len(f.service.handles), f.service.nextHandle, f.service.lookupCount, len(f.service.locks), f.service.owners[owner(f, 1)]
					_, e := cut.ApplyControlled(ctx, plan, true)
					if !applied && !errors.Is(e, fsapi.Unavailable) {
						t.Fatalf("unexecuted lifetime reported success: %v", e)
					}
					if applied && e != nil {
						t.Fatalf("completed lifetime evidence lost: %v", e)
					}
					if handles != len(f.service.handles) || next != f.service.nextHandle || lookups != f.service.lookupCount || locks != len(f.service.locks) || exited != f.service.owners[owner(f, 1)] {
						t.Fatal("recovery repeated or invented lifetime effects")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if intervening.Handle != 0 {
					f.ok(fsapi.Command{Operation: fsapi.Read, Handle: intervening.Handle, Size: 1})
				}
			})
		}
	}
}

func TestControlledLifetimeCompletionWakesNativeWaiter(t *testing.T) {
	for _, op := range []fsapi.Operation{fsapi.Release, fsapi.DescriptorClose, fsapi.ProcessExit} {
		t.Run(fmt.Sprintf("op-%d", op), func(t *testing.T) {
			f := setup(t, "os")
			policy, err := NewUnixPermissions(uid(60))
			if err != nil {
				t.Fatal(err)
			}
			f.c.Permissions = policy
			f.start()
			created := f.create(f.c.Storage.Root, "file")
			other := f.ok(fsapi.Command{Operation: fsapi.Open, Object: created.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
			domain := fsapi.POSIXLock
			if op == fsapi.Release {
				domain = fsapi.FlockLock
			}
			f.ok(lockCommand(fsapi.TryLock, created.Handle, owner(f, 1), domain, fsapi.ExclusiveLock, 0, 0))
			wait := lockCommand(fsapi.WaitLock, other.Handle, owner(f, 2), domain, fsapi.ExclusiveLock, 0, 0)
			wait.Lock.WaitID = uid(64)
			pending := pendingLock(f, wait)
			c := fsapi.Command{Mount: f.c.Storage.Volume, Generation: f.c.Workspace.ID, Operation: op, Object: created.Attr.Object, Handle: created.Handle, DescriptionOnly: op == fsapi.Release, Lock: fsapi.Lock{Owner: owner(f, 1)}}
			if op == fsapi.ProcessExit {
				c.Handle = 0
			}
			err = f.service.Controlled(t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
				p, e := cut.PlanControlled(ctx, fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}}, c, uid(65), fsapi.ObjectID{}, nil)
				if e != nil {
					return e
				}
				_, e = cut.ApplyControlled(ctx, p, false)
				return e
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := pending.Completion().Wait(ctx)
			if err != nil || result.Code != fsapi.OK {
				t.Fatalf("native waiter was not retried after controlled lifetime: %v %v", result.Code, err)
			}
		})
	}
}
