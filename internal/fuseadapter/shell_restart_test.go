//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"encoding/json"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"path/filepath"
	"testing"
)

type switchNative struct{ privateNative }

func (*switchNative) RestoreEndpoint(context.Context, guestbridge.ProtectedTransportState, guestbridge.Config) error {
	return nil
}

type switchClaims struct{ privateClaims }

func (switchClaims) CheckRestoredBridge(context.Context, guestbridge.Context, fsapi.UUID, json.RawMessage) error {
	return nil
}

func TestShellSwitchRequiresFundedSameNativePredecessor(t *testing.T) {
	// The two authority methods above are declared OS/owner primitives here. This
	// tests the real shell/C8 handoff, not mount INIT or runtime reconciliation.
	_, unrelated, scope, native := privateFixture(t)
	verifier := &switchNative{privateNative{native}}
	cfg := guestbridge.Config{Validation: &fsservice.ValidationProfile{Disposable: true, Name: "shell switch primitive"}, Endpoint: native.Endpoint, Native: verifier, Attestor: privateAttestor{}, Claims: switchClaims{}, Execution: privateDriver{}, Views: privateViews{}, Scopes: []guestbridge.Context{scope}}
	service, err := guestbridge.New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	var bc admission.Config
	for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: guestbridge.TransportLedgerMemoryBytes + (12 << 20)} {
		r := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
		bc.Pools[class] = admission.Limits{Global: r, PerPeer: r, PerVolume: r}
	}
	budget, err := admission.New(bc)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "transport")
	old, err := guestbridge.NewDurableTransport(service, budget, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(old.Close)
	shell, err := NewShell(99, 100, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	shell.server = &fuse.Server{}
	shell.endpoint = old.Endpoint()
	if err = shell.Bind(PrivateControlConfig{Transport: old, Credentials: verifier, Directory: 99}); err != nil {
		t.Fatal(err)
	}
	if err = shell.ReplaceEndpoint(old, unrelated); err == nil {
		t.Fatal("unfunded replacement accepted")
	}
	funding, err := old.Suspend()
	if err != nil {
		t.Fatal(err)
	}
	_, next, err := guestbridge.RestoreDurableEndpoint(t.Context(), cfg, root, funding)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next.Close)
	if err = shell.ReplaceEndpoint(unrelated, next); err == nil {
		t.Fatal("wrong predecessor accepted")
	}
	replacement, _ := NewShell(99, 100, 101, 102)
	replacement.server = &fuse.Server{}
	replacement.endpoint = old.Endpoint()
	if err = replacement.ReplaceEndpoint(old, next); err == nil {
		t.Fatal("replacement shell accepted")
	}
	if err = shell.ReplaceEndpoint(old, next); err != nil {
		t.Fatal(err)
	}
	if shell.dispatch.Load().transport != next {
		t.Fatal("original live shell did not switch actual transport")
	}
	if err = shell.ReplaceEndpoint(old, next); err == nil {
		t.Fatal("grant reused on old predecessor")
	}
	shell.OnUnmount()
	if err = shell.ReplaceEndpoint(next, next); err == nil {
		t.Fatal("unmounted shell accepted replacement")
	}
}
