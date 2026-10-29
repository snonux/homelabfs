//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"errors"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestBufferedIssuedGrantAndGates(t *testing.T) {
	a, service := serviceFixture(t, "", credentialFunc(testCredentials))
	a.buffered = true // private fixture: public construction additionally binds participants
	h := testHeader(1, 100)
	var created fuse.CreateOut
	if code := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR, Mode: 0600}, "mapped", &created); code != fuse.OK {
		t.Fatal(code)
	}
	if created.OpenFlags&fuse.FOPEN_DIRECT_IO != 0 {
		t.Fatal("buffered description has direct I/O")
	}
	file := testHeader(created.NodeId, 100)
	var captured fsapi.Cred
	real := a.service
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		if c.Operation == fsapi.Write {
			captured = r.Credentials()
		}
		return real.Dispatch(ctx, r, c)
	})
	// The cache-write header is intentionally empty: it supplies no authority.
	cache := fuse.WriteIn{InHeader: fuse.InHeader{NodeId: created.NodeId}, Fh: created.Fh, Size: 3, WriteFlags: fuse.WRITE_CACHE}
	if n, code := a.Write(nil, &cache, []byte("map")); code != fuse.OK || n != 3 {
		t.Fatal(n, code)
	}
	if captured.UID != 1000 || captured.GID != 2000 || captured.PID != 100 {
		t.Fatal("lost issuing grant", captured)
	}
	cache.WriteFlags = 0
	if _, code := a.Write(nil, &cache, []byte("bad")); code != fuse.EACCES {
		t.Fatal("ordinary write used grant", code)
	}
	cache.WriteFlags = fuse.WRITE_CACHE
	wrong := cache
	wrong.Fh++
	if _, code := a.Write(nil, &wrong, []byte("bad")); code != status(fsapi.Stale) {
		t.Fatal(code)
	}
	var readOnly fuse.OpenOut
	if code := a.Open(nil, &fuse.OpenIn{InHeader: file, Flags: syscall.O_RDONLY}, &readOnly); code != fuse.OK {
		t.Fatal(code)
	}
	wrong = cache
	wrong.Fh = readOnly.Fh
	if _, code := a.Write(nil, &wrong, []byte("bad")); code != fuse.EACCES {
		t.Fatal("read-only grant wrote", code)
	}
	// Chmod cannot revoke an already issued writable description.
	if code := a.SetAttr(nil, &fuse.SetAttrIn{SetAttrInCommon: fuse.SetAttrInCommon{InHeader: file, Valid: fuse.FATTR_MODE, Mode: 0}}, &fuse.AttrOut{}); code != fuse.OK {
		t.Fatal(code)
	}
	if _, code := a.Write(nil, &cache, []byte("yes")); code != fuse.OK {
		t.Fatal(code)
	}
	service.BlockAdmission("fixture gate")
	if _, code := a.Write(nil, &cache, []byte("bad")); code != status(fsapi.Unavailable) {
		t.Fatal("cache bypassed current service gate", code)
	}
}

func TestBufferedRejectsMixedWriterAuthorization(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	a.buffered = true
	var file fuse.CreateOut
	if code := a.Create(nil, &fuse.CreateIn{InHeader: testHeader(1, 100), Flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR, Mode: 0666}, "same", &file); code != fuse.OK {
		t.Fatal(code)
	}
	// A mixed-writer O_TRUNC rejection must precede any data effect.
	h := testHeader(file.NodeId, 100)
	if _, code := a.Write(nil, &fuse.WriteIn{InHeader: h, Fh: file.Fh, Size: 3}, []byte("old")); code != fuse.OK {
		t.Fatal(code)
	}
	// A separate process with the same authorization set is admitted.
	h = testHeader(file.NodeId, 200)
	var second fuse.OpenOut
	if code := a.Open(nil, &fuse.OpenIn{InHeader: h, Flags: syscall.O_RDWR}, &second); code != fuse.OK {
		t.Fatal(code)
	}
	a.Release(nil, &fuse.ReleaseIn{InHeader: h, Fh: file.Fh})
	a.Release(nil, &fuse.ReleaseIn{InHeader: h, Fh: second.Fh})
	h.Uid++
	if code := a.Open(nil, &fuse.OpenIn{InHeader: h, Flags: syscall.O_RDWR | syscall.O_TRUNC}, &fuse.OpenOut{}); code != status(fsapi.Unsupported) {
		t.Fatal("mixed writer admitted", code)
	}
	if a.Err() != nil {
		t.Fatal(a.Err())
	}
	// The original writer set stays reserved after all descriptions close.
	h.Uid--
	if code := a.Open(nil, &fuse.OpenIn{InHeader: h, Flags: syscall.O_RDWR}, &second); code != fuse.OK {
		t.Fatal(code)
	}
	read, code := a.Read(nil, &fuse.ReadIn{InHeader: h, Fh: second.Fh, Size: 3}, nil)
	if code != fuse.OK {
		t.Fatal(code)
	}
	data, code := read.Bytes(nil)
	read.Done()
	if code != fuse.OK || string(data) != "old" {
		t.Fatal("rejected mixed writer truncated data", string(data), code)
	}
	a.OnUnmount()
}

func TestBufferedProfileNeedsControlledConnection(t *testing.T) {
	_, s := serviceFixture(t, "", credentialFunc(testCredentials))
	_, err := New(Config{Service: s, Validation: s.FrontendInfo().Validation, Credentials: credentialFunc(testCredentials), BufferedValidation: true})
	if !errors.Is(err, fsapi.Unsupported) {
		t.Fatal(err)
	}
}

func TestDefaultProfileDeniesCacheWriteGrant(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	var created fuse.CreateOut
	if code := a.Create(nil, &fuse.CreateIn{InHeader: testHeader(1, 100), Flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR, Mode: 0600}, "direct", &created); code != fuse.OK {
		t.Fatal(code)
	}
	if created.OpenFlags&fuse.FOPEN_DIRECT_IO == 0 {
		t.Fatal("default lost direct I/O")
	}
	cache := fuse.WriteIn{InHeader: fuse.InHeader{NodeId: created.NodeId}, Fh: created.Fh, Size: 3, WriteFlags: fuse.WRITE_CACHE}
	if _, code := a.Write(nil, &cache, []byte("bad")); code != fuse.EACCES {
		t.Fatal("default admitted cache write", code)
	}
	a.OnUnmount()
}

func TestBufferedAppendDeniedBeforeServiceEffects(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	a.buffered = true
	var original fuse.CreateOut
	root := testHeader(1, 100)
	if code := a.Create(nil, &fuse.CreateIn{InHeader: root, Flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR, Mode: 0600}, "original", &original); code != fuse.OK {
		t.Fatal(code)
	}
	file := testHeader(original.NodeId, 100)
	if _, code := a.Write(nil, &fuse.WriteIn{InHeader: file, Fh: original.Fh, Size: 3}, []byte("old")); code != fuse.OK {
		t.Fatal(code)
	}
	calls := 0
	real := a.service
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		calls++
		return real.Dispatch(ctx, r, c)
	})
	for _, test := range []struct {
		name   string
		create bool
		flags  uint32
	}{
		{name: "create-exclusive", create: true, flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR | syscall.O_APPEND},
		{name: "create-exclusive-truncate", create: true, flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR | syscall.O_APPEND | syscall.O_TRUNC},
		{name: "open", flags: syscall.O_RDWR | syscall.O_APPEND},
		{name: "open-truncate", flags: syscall.O_RDWR | syscall.O_APPEND | syscall.O_TRUNC},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := calls
			var code fuse.Status
			if test.create {
				code = a.Create(nil, &fuse.CreateIn{InHeader: root, Flags: test.flags, Mode: 0600}, test.name, &fuse.CreateOut{})
			} else {
				code = a.Open(nil, &fuse.OpenIn{InHeader: file, Flags: test.flags}, &fuse.OpenOut{})
			}
			if code != status(fsapi.Unsupported) || calls != before {
				t.Fatalf("append admission: %v, service calls %d", code, calls-before)
			}
			if test.create {
				if code := a.Lookup(nil, &root, test.name, &fuse.EntryOut{}); code != fuse.ENOENT {
					t.Fatalf("rejected append created file: %v", code)
				}
			}
		})
	}
	read, code := a.Read(nil, &fuse.ReadIn{InHeader: file, Fh: original.Fh, Size: 3}, nil)
	if code != fuse.OK {
		t.Fatal(code)
	}
	data, code := read.Bytes(nil)
	read.Done()
	if code != fuse.OK || string(data) != "old" {
		t.Fatal("rejected append changed original data", string(data), code)
	}
	a.OnUnmount()
}

func TestDefaultDirectProfileAppend(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	var created fuse.CreateOut
	if code := a.Create(nil, &fuse.CreateIn{InHeader: testHeader(1, 100), Flags: syscall.O_CREAT | syscall.O_EXCL | syscall.O_RDWR | syscall.O_APPEND, Mode: 0600}, "append", &created); code != fuse.OK {
		t.Fatal(code)
	}
	h := testHeader(created.NodeId, 100)
	for _, data := range []string{"one", "two"} {
		if n, code := a.Write(nil, &fuse.WriteIn{InHeader: h, Fh: created.Fh, Offset: 0, Size: uint32(len(data))}, []byte(data)); code != fuse.OK || n != uint32(len(data)) {
			t.Fatal(n, code)
		}
	}
	var opened fuse.OpenOut
	if code := a.Open(nil, &fuse.OpenIn{InHeader: h, Flags: syscall.O_RDWR | syscall.O_APPEND}, &opened); code != fuse.OK {
		t.Fatal(code)
	}
	if created.OpenFlags&fuse.FOPEN_DIRECT_IO == 0 || opened.OpenFlags&fuse.FOPEN_DIRECT_IO == 0 {
		t.Fatal("append lost direct I/O")
	}
	if n, code := a.Write(nil, &fuse.WriteIn{InHeader: h, Fh: opened.Fh, Offset: 0, Size: 5}, []byte("three")); code != fuse.OK || n != 5 {
		t.Fatal(n, code)
	}
	read, code := a.Read(nil, &fuse.ReadIn{InHeader: h, Fh: created.Fh, Size: 32}, nil)
	if code != fuse.OK {
		t.Fatal(code)
	}
	data, code := read.Bytes(nil)
	read.Done()
	if code != fuse.OK || string(data) != "onetwothree" {
		t.Fatal("direct append changed", string(data), code)
	}
	a.OnUnmount()
}
