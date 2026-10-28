package fsapi_test

import (
	"math"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestLockRangeValidationAndConflictReplyOwnership(t *testing.T) {
	generation := uuid(2)
	l := fsapi.Lock{Domain: fsapi.POSIXLock, Kind: fsapi.ExclusiveLock, Owner: fsapi.LockOwner{Session: uuid(1), Generation: generation}, Start: math.MaxInt64}
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
	l.Length = 1
	if err := l.Validate(); err != fsapi.Invalid {
		t.Fatal(err)
	}
	l.Length = 0
	conflict := &fsapi.LockConflict{Lock: l}
	complete := fsapi.NewCompletion[fsapi.Result]()
	if err := complete.Complete(fsapi.Result{Generation: generation, Conflict: conflict}); err != nil {
		t.Fatal(err)
	}
	conflict.Lock.Start = 1
	first, err := complete.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.Conflict.Lock.Start != math.MaxInt64 {
		t.Fatal("borrowed conflict reply")
	}
	first.Conflict.Lock.Start = 2
	second, err := complete.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if second.Conflict.Lock.Start != math.MaxInt64 {
		t.Fatal("shared waiter reply")
	}
	for _, bad := range []fsapi.Result{{Conflict: conflict}, {Code: fsapi.WouldBlock, Conflict: conflict}, {Generation: generation, Conflict: &fsapi.LockConflict{Lock: l, Handle: 1}}} {
		if err := fsapi.NewCompletion[fsapi.Result]().Complete(bad); err == nil {
			t.Fatal("accepted invalid conflict reply")
		}
	}
}
