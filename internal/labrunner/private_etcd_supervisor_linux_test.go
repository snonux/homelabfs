//go:build linux

package labrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
)

func TestPrivateEtcdOwnedProcessRole(t *testing.T) {
	root, err := os.MkdirTemp("/var/tmp", "hfs-run-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := os.Mkdir(root+"/control", 0700); err != nil {
		t.Fatal(err)
	}
	sleep, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Fatal(err)
	}
	binary := root + "/etcd-synthetic"
	if err := os.WriteFile(binary, sleep, 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(sleep)
	intent := false
	c := SupervisorConfig{Role: "private-etcd", RunID: strings.TrimPrefix(filepath.Base(root), "hfs-run-"), Target: "hfs-test0", Node: "hfs-test0", Volume: "private-etcd", PlanHash: strings.Repeat("a", 64), Directory: root + "/control", Executable: binary, SHA256: hex.EncodeToString(sum[:]), Handle: "etcd", Args: []string{"30"}, MaxLogBytes: 1024, TerminateIntent: func(p FaultProcess) error {
		intent = true
		if p.PID <= 1 || p.Birth == "" {
			return errors.New("unbound launch")
		}
		return nil
	}}
	s, err := StartFaultSupervisor(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s.child.ProcessState == nil {
			_ = finishEtcdChild(s)
		}
	}()
	if !s.admission {
		t.Fatal("process-only supervisor incorrectly requires HFS workspace")
	}
	if _, err := s.BindWorkspace(t.Context()); err == nil {
		t.Fatal("etcd masqueraded as HFS workspace")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	response, err := admin.Call(ctx, s.Socket(), admin.Request{Version: 1, Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status.Volumes[0].Profile != "private-etcd-process" || response.Status.Volumes[0].NormalPublication {
		t.Fatal(response.Status)
	}
	identity := s.Process()
	_, err = admin.Call(ctx, s.Socket(), admin.Request{Version: 1, Operation: "test-fault", Fault: signalToken("etcd-terminate", identity), Enabled: true, ExpectedNode: "hfs-test0", ExpectedVolume: "private-etcd", ExpectedGeneration: identity.Birth})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Close()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !expectedEtcdSignalExit(err) {
		t.Fatal("unexpected synthetic child termination", err)
	}
	if !intent || s.child.ProcessState == nil {
		t.Fatal("child not owned and reaped after intent")
	}
	if err := s.toggle(ctx, signalToken("daemon-kill", identity), true); err == nil {
		t.Fatal("closed process identity reused")
	}
	if expectedEtcdSignalExit(errors.Join(err, errors.New("disk sync failed"))) {
		t.Fatal("termination hid unrelated cleanup failure")
	}
	// A startup failure can leave public signal admission closed. The same
	// unreaped owner must still stop/reap its actual child without opening it.
	if err := os.Mkdir(root+"/partial", 0700); err != nil {
		t.Fatal(err)
	}
	c.Directory = root + "/partial"
	partial, err := StartFaultSupervisor(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	partial.mu.Lock()
	partial.admission = false
	partial.mu.Unlock()
	defer func() {
		if partial.child.ProcessState == nil {
			_ = finishEtcdChild(partial)
		}
	}()
	if err := finishEtcdChild(partial); !expectedEtcdSignalExit(err) {
		t.Fatal("partial launch cleanup failed", err)
	}
	if partial.child.ProcessState == nil || partial.admission {
		t.Fatal("partial child leaked or public admission opened")
	}
}
