package fsservice

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestDelegatedWriteCapturedEnd(t *testing.T) {
	for _, tc := range []struct {
		name   string
		end    int64
		count  int64
		failed bool
	}{
		{"zero-bound", 0, 0, true}, {"at-limit", 2, 0, true}, {"past-limit", 1, 0, true},
		{"crossing-short-prefix", 4, 2, false}, {"exact-end", 6, 4, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLogicalFixture(t)
			cred := logicalCred()
			created := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: f.local.c.Storage.Root, Name: "bounded", Mode: 0600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}})
			f.commit(created, uid(11))
			id := created.Operation.Object
			h := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: id, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Append: true}}).Handle
			// Another origin appends after the guest chose offset zero.
			f.commit(f.plan(cred, fsapi.Command{Operation: fsapi.Write, Handle: h, Data: []byte("ab")}), uid(9))
			c := fsapi.Command{Operation: fsapi.Write, Mount: f.local.c.Storage.Volume, Generation: f.view.Generation(), Handle: h, Offset: 0, Data: []byte("cdef")}
			o := f.observation()
			f.next++
			p, err := f.planner.PlanDelegatedWrite(t.Context(), o, cred, c, uid(f.next), fsapi.ObjectID{}, tc.end)
			if e := o.Close(); e != nil {
				t.Fatal(e)
			}
			if tc.failed {
				if !errors.Is(err, ErrDelegatedFileTooLarge) {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if p.Operation.Offset != 2 || p.Result.Count != tc.count {
					t.Fatalf("offset=%d count=%d", p.Operation.Offset, p.Result.Count)
				}
				var binding struct {
					Command fsapi.Command
					MaxEnd  int64
				}
				if err = json.Unmarshal(p.Intent, &binding); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(binding.Command.Data, []byte("cdef")) || binding.Command.Offset != 0 || binding.MaxEnd != tc.end {
					t.Fatalf("lost original binding: %s", p.Intent)
				}
				// The same full request at this cut binds identically for a distinct claim;
				// lost replies must resolve their retained claim rather than reserve it twice.
				o = f.observation()
				f.next++
				retry, e := f.planner.PlanDelegatedWrite(t.Context(), o, cred, c, uid(f.next), fsapi.ObjectID{}, tc.end)
				_ = o.Close()
				if e != nil || !bytes.Equal(retry.Intent, p.Intent) {
					t.Fatalf("retry: %v", e)
				}
				f.commit(p, uid(11))
			}
			got := f.observe(cred, fsapi.Command{Operation: fsapi.Read, Handle: h, Size: 16})
			want := "ab"
			if !tc.failed {
				want += "cdef"[:tc.count]
			}
			if string(got.Data) != want {
				t.Fatalf("data=%q want=%q", got.Data, want)
			}
		})
	}
}

func TestDelegatedWriteInvalidBounds(t *testing.T) {
	f := newLogicalFixture(t)
	o := f.observation()
	defer func() { _ = o.Close() }()
	for _, c := range []fsapi.Command{{Operation: fsapi.Write}, {Operation: fsapi.Getattr}} {
		if _, err := f.planner.PlanDelegatedWrite(t.Context(), o, logicalCred(), c, uid(900), fsapi.ObjectID{}, -1); !errors.Is(err, fsapi.Invalid) {
			t.Fatal(err)
		}
	}
}

func TestDelegatedWriteNonAppendOffset(t *testing.T) {
	f := newLogicalFixture(t)
	cred := logicalCred()
	created := f.plan(cred, fsapi.Command{Operation: fsapi.Create, Parent: f.local.c.Storage.Root, Name: "positioned", Mode: 0600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}})
	f.commit(created, uid(11))
	h := f.observe(cred, fsapi.Command{Operation: fsapi.Open, Object: created.Operation.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}}).Handle
	o := f.observation()
	defer func() { _ = o.Close() }()
	command := fsapi.Command{Operation: fsapi.Write, Mount: f.local.c.Storage.Volume, Generation: f.view.Generation(), Handle: h, Offset: 3, Data: []byte("abcd")}
	p, err := f.planner.PlanDelegatedWrite(t.Context(), o, cred, command, uid(980), fsapi.ObjectID{}, 5)
	if err != nil || p.Result.Count != 2 || p.Operation.Offset != 3 {
		t.Fatalf("count=%d offset=%d error=%v", p.Result.Count, p.Operation.Offset, err)
	}
	command.Offset = -1
	if _, err = f.planner.PlanDelegatedWrite(t.Context(), o, cred, command, uid(981), fsapi.ObjectID{}, 5); !errors.Is(err, fsapi.Invalid) {
		t.Fatal(err)
	}
	command.Operation = fsapi.Getattr
	if _, err = f.planner.PlanDelegatedWrite(t.Context(), o, cred, command, uid(982), fsapi.ObjectID{}, 5); !errors.Is(err, fsapi.Invalid) {
		t.Fatal(err)
	}
}
