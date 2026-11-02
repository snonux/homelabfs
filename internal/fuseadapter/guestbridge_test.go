//go:build linux || freebsd

package fuseadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type privateNative struct{ native guestbridge.Native }
type privateAttestor struct{}
type privateClaims struct{}
type privateDriver struct{}
type privateViews struct{}

func (p privateNative) Snapshot(_ context.Context, c Caller, _ guestbridge.Endpoint) (guestbridge.Native, error) {
	n := p.native
	n.Unique = c.Unique
	return n, nil
}
func (privateNative) Enroll(context.Context, guestbridge.Endpoint, fsapi.UUID) error { return nil }
func (p privateNative) Verify(_ context.Context, n guestbridge.Native, _ fsapi.UUID) error {
	if n.UID != p.native.UID || n.GID != p.native.GID || n.PID != p.native.PID || n.Process != p.native.Process || n.Endpoint != p.native.Endpoint || !reflect.DeepEqual(n.Groups, p.native.Groups) {
		return fsapi.Permission
	}
	return nil
}
func (privateAttestor) Verify(context.Context, guestbridge.Context, guestbridge.Credentials) error {
	return fsapi.Unavailable
}
func (privateClaims) Resolve(context.Context, guestbridge.Context, guestbridge.Claim) (guestbridge.ClaimState, error) {
	return guestbridge.ClaimState{}, fsapi.Unavailable
}
func (privateClaims) Reserve(context.Context, guestbridge.Binding) (guestbridge.ClaimState, error) {
	return guestbridge.ClaimState{}, fsapi.Unavailable
}
func (privateClaims) ProtectResult(context.Context, guestbridge.Binding, json.RawMessage) error {
	return fsapi.Unavailable
}
func (privateClaims) Cancel(context.Context, guestbridge.Context, guestbridge.Claim) (guestbridge.ClaimState, error) {
	return guestbridge.ClaimState{}, fsapi.Unavailable
}
func (privateClaims) Retire(context.Context, guestbridge.Context, json.RawMessage) (json.RawMessage, error) {
	return nil, fsapi.Unavailable
}
func (privateDriver) ServingGeneration(context.Context, guestbridge.Context) (fsapi.UUID, error) {
	return testID(30), nil
}
func (privateDriver) Check(ctx context.Context, _ guestbridge.Context) error { return ctx.Err() }
func (privateDriver) Execute(context.Context, guestbridge.Binding, func(guestbridge.Captured) (guestbridge.Prepared, error)) (guestbridge.Outcome, error) {
	return guestbridge.Outcome{}, fsapi.Unavailable
}
func (privateViews) ValidateDrain(context.Context, guestbridge.Binding) error {
	return fsapi.Unavailable
}
func (privateViews) Exchange(context.Context, guestbridge.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, fsapi.Unavailable
}
func privateFixture(t *testing.T) (*PrivateControl, *guestbridge.Transport, guestbridge.Context, guestbridge.Native) {
	t.Helper()
	scope := guestbridge.Context{}
	v := reflect.ValueOf(&scope).Elem()
	for i := 0; i < v.NumField(); i++ {
		if strings.HasSuffix(v.Type().Field(i).Name, "Hash") {
			v.Field(i).SetString(strings.Repeat("1", 64))
		} else {
			v.Field(i).SetString(testID(byte(i + 40)).String())
		}
	}
	scope.Volume = testID(1).String()
	scope.Generation = testID(4).String()
	native := guestbridge.Native{UID: 0, GID: 0, PID: 123, Process: testID(90).String(), Groups: []uint32{0, 5}, Endpoint: guestbridge.Endpoint{FuseSession: scope.FuseSession, CommandNode: 100, ResultNode: 101, RecallNode: 102}}
	service, e := guestbridge.New(t.Context(), guestbridge.Config{Validation: &fsservice.ValidationProfile{Disposable: true, Name: "raw callback HELLO fixture only"}, Endpoint: native.Endpoint, Native: privateNative{native}, Attestor: privateAttestor{}, Claims: privateClaims{}, Execution: privateDriver{}, Views: privateViews{}, Scopes: []guestbridge.Context{scope}})
	if e != nil {
		t.Fatal(e)
	}
	var bc admission.Config
	for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: 12 << 20} {
		r := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
		bc.Pools[class] = admission.Limits{Global: r, PerPeer: r, PerVolume: r}
	}
	budget, e := admission.New(bc)
	if e != nil {
		t.Fatal(e)
	}
	transport, e := guestbridge.NewTransport(service, budget)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(transport.Close)
	adapter, e := NewPrivateControl(PrivateControlConfig{Transport: transport, Credentials: privateNative{native}, Directory: 99})
	if e != nil {
		t.Fatal(e)
	}
	return adapter, transport, scope, native
}
func privateHeader(node uint64, n guestbridge.Native) fuse.InHeader {
	return fuse.InHeader{NodeId: node, Unique: 1, Caller: fuse.Caller{Owner: fuse.Owner{Uid: n.UID, Gid: n.GID}, Pid: n.PID}}
}
func TestPrivateControlCallbacksIdentityVisibilityAndPositionalReceipt(t *testing.T) {
	a, _, scope, native := privateFixture(t)
	h := privateHeader(1, native)
	var entry fuse.EntryOut
	stranger := h
	stranger.Uid = 1000
	if s := a.Lookup(nil, &stranger, guestbridge.ControlName, &entry); s != fuse.EACCES {
		t.Fatal(s)
	}
	if s := a.Lookup(nil, &h, guestbridge.ControlName, &entry); s != fuse.OK || entry.NodeId != 99 {
		t.Fatal(s, entry)
	}
	h.NodeId = 99
	if s := a.Lookup(nil, &h, "command", &entry); s != fuse.OK || entry.NodeId != 100 {
		t.Fatal(s, entry)
	}
	var attr fuse.AttrOut
	if s := a.GetAttr(nil, &fuse.GetAttrIn{InHeader: privateHeader(100, native)}, &attr); s != fuse.OK || attr.Size != guestbridge.VirtualSize {
		t.Fatal(s, attr)
	}
	var opened, again fuse.OpenOut
	in := fuse.OpenIn{InHeader: privateHeader(100, native), Flags: uint32(syscall.O_WRONLY)}
	if s := a.Open(nil, &in, &opened); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.Open(nil, &in, &again); s != fuse.OK || opened.Fh != again.Fh || opened.OpenFlags != fuse.FOPEN_DIRECT_IO {
		t.Fatal(s, opened, again)
	}
	in.Flags |= uint32(syscall.O_APPEND)
	if s := a.Open(nil, &in, &again); s != fuse.EACCES {
		t.Fatal(s)
	}
	// Construct a canonical HELLO. Native write receipt precedes asynchronous reply.
	body := map[string]any{"cell": uint64(1), "context": scope, "op": "HELLO", "payload": map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0}, "reclaim": nil}
	first, e := json.Marshal(body)
	if e != nil {
		t.Fatal(e)
	}
	var generic any
	if e = json.Unmarshal(first, &generic); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(generic)
	if e != nil {
		t.Fatal(e)
	}
	session, e := fsapi.ParseUUID(scope.Session)
	if e != nil {
		t.Fatal(e)
	}
	wire, e := guestbridge.EncodeFrame(guestbridge.Frame{Kind: 1, Total: uint32(len(raw)), Session: session, Cell: 1, Hash: sha256.Sum256(raw), Data: raw})
	if e != nil {
		t.Fatal(e)
	}
	write := fuse.WriteIn{InHeader: privateHeader(100, native), Fh: opened.Fh, Offset: guestbridge.CellStride, Size: uint32(len(wire))}
	if count, s := a.Write(nil, &write, wire); s != fuse.OK || count != uint32(len(wire)) {
		t.Fatal(count, s)
	}
	read := fuse.ReadIn{InHeader: privateHeader(101, native), Fh: 999, Offset: guestbridge.CellStride, Size: 11}
	deadline := time.Now().Add(2 * time.Second)
	var data []byte
	for {
		r, s := a.Read(nil, &read, nil)
		if s == fuse.EAGAIN && time.Now().Before(deadline) {
			runtime.Gosched()
			continue
		}
		if s != fuse.OK {
			t.Fatal(s)
		}
		data, s = r.Bytes(nil)
		r.Done()
		if s != fuse.OK {
			t.Fatal(s)
		}
		break
	}
	if !bytes.HasPrefix(data, []byte("HFS9BR1\x00")) {
		t.Fatal(data)
	}
	read.Offset += uint64(len(data))
	read.Size = 32768
	r, s := a.Read(nil, &read, nil)
	if s != fuse.OK {
		t.Fatal(s)
	}
	rest, s := r.Bytes(nil)
	r.Done()
	if s != fuse.OK {
		t.Fatal(s)
	}
	joined := append(bytes.Clone(data), rest...)
	frame, e := guestbridge.DecodeFrame(joined)
	if e != nil || frame.Cell != 1 || frame.Flags != 0 {
		t.Fatal(e, frame)
	}
	read.Offset = guestbridge.CellStride + uint64(len(joined))
	r, s = a.Read(nil, &read, nil)
	if s != fuse.OK || r.Size() != 0 {
		t.Fatal(s)
	}
	r.Done()
	read.Offset = guestbridge.VirtualSize
	r, s = a.Read(nil, &read, nil)
	if s != fuse.OK || r.Size() != 0 {
		t.Fatal(s)
	}
	r.Done()
	if s := a.Ioctl(nil, &fuse.IoctlIn{InHeader: privateHeader(100, native)}, nil, &fuse.IoctlOut{}, nil); s != fuse.EACCES {
		t.Fatal(s)
	}
	if s := a.Create(nil, &fuse.CreateIn{InHeader: privateHeader(1, native)}, guestbridge.ControlName, &fuse.CreateOut{}); s != fuse.EACCES {
		t.Fatal(s)
	}
	if _, s := a.Read(nil, &fuse.ReadIn{InHeader: privateHeader(100, native), Offset: guestbridge.CellStride, Size: 1}, nil); s != fuse.EACCES {
		t.Fatal(s)
	}
}
