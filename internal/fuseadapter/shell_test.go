//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func TestUnboundShellRefusesPrivateWorkloadAndReadiness(t *testing.T) {
	shell, err := NewShell(99, 100, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	header := fuse.InHeader{NodeId: 1}
	if status := shell.Lookup(nil, &header, guestbridge.ControlName, &fuse.EntryOut{}); status != fuse.EACCES {
		t.Fatalf("private lookup %v", status)
	}
	if status := shell.Lookup(nil, &header, "workload", &fuse.EntryOut{}); status != fuse.EACCES {
		t.Fatalf("workload lookup %v", status)
	}
	if status := shell.GetAttr(nil, &fuse.GetAttrIn{InHeader: header}, &fuse.AttrOut{}); status != fuse.EACCES {
		t.Fatalf("getattr %v", status)
	}
	if _, status := shell.Write(nil, &fuse.WriteIn{InHeader: fuse.InHeader{NodeId: 100}}, nil); status != fuse.EACCES {
		t.Fatalf("private write %v", status)
	}
	if _, status := shell.Read(nil, &fuse.ReadIn{InHeader: fuse.InHeader{NodeId: 101}}, nil); status != fuse.EACCES {
		t.Fatalf("private read %v", status)
	}
	if status := shell.StatFs(nil, &header, &fuse.StatfsOut{}); status != fuse.EACCES {
		t.Fatalf("statfs %v", status)
	}
	if _, err = shell.Endpoint(); err == nil {
		t.Fatal("unbound endpoint reported live")
	}
	if err = shell.Bind(PrivateControlConfig{}); err == nil {
		t.Fatal("unbound shell bound without actual server")
	}
	shell.Init(&fuse.Server{})
	if _, err = shell.Endpoint(); err == nil {
		t.Fatal("fabricated zero server supplied native identity")
	}
	shell.OnUnmount()
	if err = shell.Bind(PrivateControlConfig{}); err == nil {
		t.Fatal("dead shell rebound")
	}
	for _, ids := range [][4]uint64{{1, 100, 101, 102}, {99, 100, 100, 102}, {99, 100, 101, 99}} {
		if _, err = NewShell(ids[0], ids[1], ids[2], ids[3]); err == nil {
			t.Fatalf("invalid IDs accepted %v", ids)
		}
	}
}

func TestShellAtomicBindingDispatchAndNoRebind(t *testing.T) {
	// This package-private dispatch fixture tests the switch only. It does not
	// create a mounted observation or count as native FreeBSD authority.
	private, transport, _, native := privateFixture(t)
	shell, err := NewShell(99, 100, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	shell.server = &fuse.Server{}
	shell.endpoint = transport.Endpoint()
	if err = shell.Bind(PrivateControlConfig{Transport: transport, Credentials: privateNative{native}, Directory: 98}); err == nil {
		t.Fatal("wrong directory bound")
	}
	if err = shell.Bind(PrivateControlConfig{Transport: transport, Credentials: privateNative{native}, Directory: 99}); err != nil {
		t.Fatal(err)
	}
	h := privateHeader(1, native)
	var out fuse.EntryOut
	if status := shell.Lookup(nil, &h, guestbridge.ControlName, &out); status != fuse.OK || out.NodeId != 99 {
		t.Fatalf("bound actual private adapter %v %+v", status, out)
	}
	if err = shell.Bind(PrivateControlConfig{Transport: transport, Credentials: privateNative{native}, Directory: 99}); err == nil {
		t.Fatal("second bind accepted")
	}
	shell.OnUnmount()
	if status := shell.Lookup(nil, &h, guestbridge.ControlName, &out); status != fuse.EACCES {
		t.Fatal("unmounted shell still dispatches")
	}
	_ = private
}

func TestNativeRequestObservationIsExactAndPrivate(t *testing.T) {
	c := Caller{UID: 1, GID: 2, Thread: 3, Unique: 4}
	ctx := context.Background()
	if ObservedNativeRequest(ctx, c, 100, "OPEN") {
		t.Fatal("caller fabricated request observation")
	}
	ctx = observeNativeRequest(ctx, c, 100, "OPEN")
	if !ObservedNativeRequest(ctx, c, 100, "OPEN") {
		t.Fatal("real callback observation lost")
	}
	changed := c
	changed.Unique++
	if ObservedNativeRequest(ctx, changed, 100, "OPEN") || ObservedNativeRequest(ctx, c, 101, "OPEN") || ObservedNativeRequest(ctx, c, 100, "WRITE") {
		t.Fatal("request observation escaped binding")
	}
}
