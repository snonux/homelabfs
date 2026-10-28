package fsapi_test

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func uuid(n byte) fsapi.UUID { return fsapi.UUID{0: n, 15: n} }

func object(n byte) fsapi.ObjectID {
	return fsapi.ObjectID{Home: uuid(n), Allocation: uuid(n + 1)}
}

func validAttr() fsapi.Attr {
	return fsapi.Attr{Object: object(1), Kind: fsapi.Regular, UID: 1000, GID: 1000, Mode: 0o6754, Size: 12, Nlink: 1, Revision: 1}
}

func TestUUIDAndObjectIdentity(t *testing.T) {
	id := uuid(1)
	parsed, err := fsapi.ParseUUID(id.String())
	if err != nil || parsed != id {
		t.Fatalf("UUID round trip: %v, %v", parsed, err)
	}
	for _, text := range []string{
		"00000000-0000-0000-0000-000000000000",
		"01000000000000000000000000000001",
		"ab000000-0000-0000-0000-00000000000G",
		"AB000000-0000-0000-0000-000000000001",
		"01000000_0000-0000-0000-000000000001",
		strings.Repeat("-", 36),
	} {
		if _, err := fsapi.ParseUUID(text); !errors.Is(err, fsapi.Invalid) {
			t.Errorf("accepted UUID %q: %v", text, err)
		}
	}
	if (fsapi.ObjectID{Home: id}).Valid() || (fsapi.ObjectID{Allocation: id}).Valid() {
		t.Fatal("accepted incomplete object identity")
	}
	if object(1) == (fsapi.ObjectID{Home: uuid(3), Allocation: object(1).Allocation}) {
		t.Fatal("identity ignored immutable object home")
	}
}

func TestPortableRangesAndMetadata(t *testing.T) {
	for _, tc := range []struct {
		offset, length int64
		valid          bool
	}{
		{0, 0, true}, {0, math.MaxInt64, true}, {math.MaxInt64, 0, true},
		{math.MaxInt64 - 1, 1, true}, {math.MaxInt64, 1, false},
		{-1, 0, false}, {0, -1, false}, {1, math.MaxInt64, false},
	} {
		if err := fsapi.ValidateRange(tc.offset, tc.length); (err == nil) != tc.valid {
			t.Errorf("range %d,%d: %v", tc.offset, tc.length, err)
		}
	}
	if err := validAttr().Validate(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*fsapi.Attr){
		"object":   func(a *fsapi.Attr) { a.Object.Home = fsapi.UUID{} },
		"kind":     func(a *fsapi.Attr) { a.Kind = 0 },
		"size":     func(a *fsapi.Attr) { a.Size = -1 },
		"mode":     func(a *fsapi.Attr) { a.Mode = 0o10000 },
		"revision": func(a *fsapi.Attr) { a.Revision = 0 },
		"nanos":    func(a *fsapi.Attr) { a.Mtime.Nanoseconds = 1_000_000_000 },
	} {
		t.Run(name, func(t *testing.T) {
			a := validAttr()
			change(&a)
			if err := a.Validate(); !errors.Is(err, fsapi.Invalid) {
				t.Fatalf("accepted invalid attributes: %+v: %v", a, err)
			}
		})
	}
	a := validAttr()
	a.Nlink = 0 // open-unlinked objects still have valid metadata.
	a.Atime = fsapi.Timestamp{Seconds: math.MinInt64, Nanoseconds: 999_999_999}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	if !(fsapi.WallClock{}).Now().Valid() {
		t.Fatal("wall clock produced invalid timestamp")
	}
}

func TestOpenOptionsAndLockScope(t *testing.T) {
	for _, flags := range []fsapi.OpenFlags{
		{Access: fsapi.ReadOnly},
		{Access: fsapi.ReadWrite, Create: true, Exclusive: true},
		{Access: fsapi.WriteOnly, Truncate: true, Append: true, Sync: fsapi.IntegrityWrite},
		{Access: fsapi.ReadWrite, Sync: fsapi.DataWrite},
	} {
		if err := flags.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, flags := range []fsapi.OpenFlags{
		{}, {Access: fsapi.AccessMode(255)}, {Access: fsapi.ReadOnly, Truncate: true},
		{Access: fsapi.ReadOnly, Append: true}, {Access: fsapi.ReadWrite, Exclusive: true},
		{Access: fsapi.ReadWrite, Sync: fsapi.SyncMode(255)},
	} {
		if err := flags.Validate(); !errors.Is(err, fsapi.Invalid) {
			t.Errorf("accepted invalid flags %+v: %v", flags, err)
		}
	}
	if !fsapi.DataDurability.Valid() || !fsapi.FileDurability.Valid() || !fsapi.DirectoryDurability.Valid() || fsapi.DurabilityRequest(0).Valid() || fsapi.DurabilityRequest(255).Valid() {
		t.Fatal("incorrect durability domain")
	}
	owner := fsapi.LockOwner{Session: uuid(1), Generation: uuid(2), Token: 42}
	if !owner.Valid() || (fsapi.LockOwner{Session: uuid(1)}).Valid() || (fsapi.LockOwner{Token: 42}).Valid() {
		t.Fatal("lock owner lacks scoped identity")
	}
}

func TestErrnoMappingCoverage(t *testing.T) {
	codes := fsapi.Errnos()
	if err := fsapi.ValidateErrnoCoverage(codes); err != nil {
		t.Fatal(err)
	}
	for i, code := range codes {
		without := append(append([]fsapi.Errno{}, codes[:i]...), codes[i+1:]...)
		if err := fsapi.ValidateErrnoCoverage(without); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("missing mapping for %v accepted: %v", code, err)
		}
		if !code.Valid() || code.Error() == "" || strings.Contains(code.Error(), "UNKNOWN") {
			t.Fatalf("enum value %d has no mapping name", code)
		}
	}
	for _, invalid := range [][]fsapi.Errno{nil, append(codes, fsapi.OK), append(fsapi.Errnos(), fsapi.Errno(255))} {
		if err := fsapi.ValidateErrnoCoverage(invalid); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("invalid mapping accepted: %v", err)
		}
	}
	if fsapi.Errno(255).Valid() || !strings.Contains(fsapi.Errno(255).Error(), "UNKNOWN") {
		t.Fatal("unknown code treated as supported")
	}
}

func TestReferencesSeparateLookupAndOpenLifetimes(t *testing.T) {
	var refs fsapi.References
	if err := refs.Lookup(2); err != nil {
		t.Fatal(err)
	}
	if err := refs.Open(); err != nil {
		t.Fatal(err)
	}
	if err := refs.Forget(2); err != nil {
		t.Fatal(err)
	}
	if got := refs.Counts(); got.Lookups != 0 || got.Open != 1 {
		t.Fatalf("forget released open-unlinked handle: %+v", got)
	}
	if err := refs.Lookup(math.MaxUint64); err != nil {
		t.Fatal(err)
	}
	if err := refs.Lookup(1); !errors.Is(err, fsapi.Invalid) {
		t.Fatalf("reference counter wrapped: %v", err)
	}
	if err := refs.Release(); err != nil {
		t.Fatal(err)
	}
	if got := refs.Counts(); got.Open != 0 || got.Lookups != math.MaxUint64 {
		t.Fatalf("release changed lookup count: %+v", got)
	}
	if err := refs.Forget(math.MaxUint64); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{refs.Forget(1), refs.Forget(0), refs.Lookup(0), refs.Release()} {
		if !errors.Is(err, fsapi.Invalid) {
			t.Errorf("invalid reference release accepted: %v", err)
		}
	}
}

func TestReferencesConcurrentLifetimes(t *testing.T) {
	var refs fsapi.References
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for _, err := range []error{refs.Lookup(1), refs.Open(), refs.Forget(1), refs.Release()} {
				if err != nil {
					t.Errorf("concurrent references: %v", err)
				}
			}
		})
	}
	wg.Wait()
	if got := refs.Counts(); got != (fsapi.ReferenceCounts{}) {
		t.Fatalf("references leaked: %+v", got)
	}
}
