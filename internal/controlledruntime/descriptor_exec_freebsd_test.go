//go:build freebsd

package controlledruntime

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
)

func descriptorOwner(t *testing.T) *Supervisor {
	t.Helper()
	root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "hfs-descriptor-")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Create(t.Context(), filepath.Join(root, "owner"))
	if err != nil {
		t.Fatalf("preserved %s: %v", root, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if s.child != nil {
			select {
			case <-s.child.exited:
				_ = s.child.executable.Close()
			case <-time.After(2 * time.Second):
				t.Error("owned child did not exit after normal management EOF")
			}
		}
		if t.Failed() {
			t.Logf("preserved failed native descriptor resources: %s", root)
		} else if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	return s
}

// Run explicitly on an unsupported native profile before setup. A supported
// machine skips this case; that is not negative native qualification evidence.
func TestDescriptorExecNativeUnsupported(t *testing.T) {
	if _, err := checkDescriptorExec(); err == nil {
		t.Skip("requires actual unsupported /dev/fd profile")
	}
	s := descriptorOwner(t)
	spec := helperSpec(t)
	marker := filepath.Join(s.root, "unexpected-child")
	spec.Args = []string{"-test.run=^TestRefusedObserverChildCounter$", "--", "hfs-refused-observer-child", marker}
	before, err := os.ReadFile(filepath.Join(s.root, "issuance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Start(t.Context(), spec); err == nil {
		t.Fatal("unsupported start accepted")
	}
	after, err := os.ReadFile(filepath.Join(s.root, "issuance.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || s.record.Stage != "created" || s.child != nil {
		t.Fatal("unsupported start changed owner or created child")
	}
	if _, err = os.Lstat(filepath.Join(s.root, "exporter")); !os.IsNotExist(err) {
		t.Fatalf("exporter pin side effect: %v", err)
	}
	if _, err = os.Lstat(marker); !os.IsNotExist(err) {
		t.Fatalf("refused platform reached the child: %v", err)
	}
	m, err := mountNative(t.Context(), "/absent", &fuseadapter.Shell{})
	if !errors.Is(err, fsapi.Unsupported) || m.witness.(*nativeConnection).device != nil {
		t.Fatal("unsupported mount reached device open")
	}
	o := GuestObserver{Host: "127.0.0.1", Identity: "/absent", ManifestHash: [32]byte{1}}
	if _, err = o.runProbe(t.Context(), ""); err == nil || os.IsNotExist(err) {
		t.Fatalf("unsupported observer reached file pin: %v", err)
	}
}

// This test requires actual native CGO observation and a supported FD namespace.
// It launches only the existing short owned test child, never a VM or mount.
func TestDescriptorExecNativeQualification(t *testing.T) {
	report, err := checkDescriptorExec()
	if err != nil {
		t.Fatalf("native FD prerequisite: %#v: %v", report, err)
	}
	s := descriptorOwner(t)
	if err = s.Start(t.Context(), helperSpec(t)); err != nil {
		t.Fatal(err)
	}
	if err = s.child.control.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var ready [1]byte
	if _, err = io.ReadFull(s.child.control, ready[:]); err != nil || ready[0] != 'R' {
		t.Fatalf("owned child readiness: %q %v", ready, err)
	}
	if err = s.child.live(); err != nil {
		t.Fatalf("actual FD-backed child's protected identity: %v", err)
	}
	t.Logf("native FD exec and protected identity qualified: %#v", report)
}
