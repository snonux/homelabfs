//go:build linux

package labrunner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"golang.org/x/sys/unix"
)

func TestFaultSupervisorChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "fault-child" {
		return
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		os.Exit(2)
	}
	generation := "workspace-" + hex.EncodeToString(nonce)
	directory := os.Args[len(os.Args)-2]
	server, err := admin.Listen(context.Background(), admin.Config{SocketPath: filepath.Join(directory, "admin-volume.sock"), NodeID: "node", VolumeID: "volume", Generation: generation, Audit: func(context.Context, admin.Event) error { return nil }, Status: func(context.Context) (admin.Status, error) {
		return admin.Status{NodeID: "node", Volumes: []admin.VolumeStatus{{VolumeID: "volume", Generation: generation}}}, nil
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(generation)
	time.Sleep(30 * time.Second)
	_ = server.Close()
	os.Exit(0)
}
func TestFaultSupervisorOwnedSignals(t *testing.T) {
	root, err := os.MkdirTemp("/var/tmp", "hfs-run-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	directory := filepath.Join(root, "control")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = source.Close() }()
	path := filepath.Join(root, "homelabfs")
	target, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(target, h), source); err != nil {
		t.Fatal(err)
	}
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := StartFaultSupervisor(context.Background(), SupervisorConfig{RunID: strings.TrimPrefix(filepath.Base(root), "hfs-run-"), Target: "hfs-test0", Node: "node", Volume: "volume", PlanHash: strings.Repeat("a", 64), Directory: directory, Executable: path, SHA256: hex.EncodeToString(h.Sum(nil)), Handle: "daemon", Args: []string{"-test.run=^TestFaultSupervisorChild$", "--", directory, "fault-child"}, MaxLogBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s.admission {
			_, _ = admin.Call(context.Background(), s.Socket(), admin.Request{Version: 1, Operation: "test-fault", Fault: signalToken("daemon-kill", s.Process()), Enabled: true})
		}
		_ = s.Close()
	}()
	identity := s.Process()
	if s.admission {
		t.Fatal("signal admission opened before workspace readiness")
	}
	// This generation is emitted by the already running child. It was neither
	// known to the launch configuration nor interchangeable with its Birth.
	var servingGeneration string
	readinessDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(readinessDeadline) {
		raw, _ := os.ReadFile(filepath.Join(directory, "daemon.daemon.log"))
		if strings.HasPrefix(string(raw), "workspace-") {
			servingGeneration = strings.TrimSpace(string(raw))
			break
		}
		time.Sleep(time.Millisecond)
	}
	if servingGeneration == "" || servingGeneration == identity.Birth {
		t.Fatal("missing independent dynamic workspace generation")
	}
	daemonSocket := filepath.Join(directory, "admin-volume.sock")
	childSocket := daemonSocket + ".child"
	if err := os.Rename(daemonSocket, childSocket); err != nil {
		t.Fatal(err)
	}
	daemon, err := admin.Listen(context.Background(), admin.Config{SocketPath: daemonSocket, NodeID: "node", VolumeID: "volume", Generation: servingGeneration, Audit: func(context.Context, admin.Event) error { return nil }, Status: func(context.Context) (admin.Status, error) {
		return admin.Status{NodeID: "node", Volumes: []admin.VolumeStatus{{VolumeID: "volume", Generation: servingGeneration}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.BindWorkspace(context.Background()); err == nil {
		t.Fatal("same-UID parent status was accepted as owned child readiness")
	}
	if s.admission {
		t.Fatal("foreign readiness opened signal admission")
	}
	if _, err := os.Stat(filepath.Join(directory, "supervisor-daemon.serving.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("foreign readiness produced a serving binding")
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(childSocket, daemonSocket); err != nil {
		t.Fatal(err)
	}
	binding, err := s.BindWorkspace(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if binding.Generation != servingGeneration || binding.Process.Birth != identity.Birth || !s.admission {
		t.Fatal("workspace binding mixed launch and serving identity")
	}
	supervisorStatus, err := admin.Call(context.Background(), s.Socket(), admin.Request{Version: 1, Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if supervisorStatus.Status.Volumes[0].Generation != identity.Birth {
		t.Fatal("supervisor did not retain launch identity")
	}
	// Exercise the concrete signal client: its expected target must use Birth,
	// while the request's daemon workspace remains the later learned generation.
	session := &nativeFaultSession{req: FaultRequest{Fault: Fault{Node: "node", Volume: "volume", Generation: servingGeneration, Socket: daemonSocket, Process: &FaultProcess{Executable: path, SHA256: identity.SHA256, Handle: identity.Handle}}, Step: "signal"}, path: filepath.Join(directory, "fault-enrollment.json")}

	call := func(action string) error {
		session.req.Fault.Action = action
		return session.signal(context.Background())
	}
	wrong := identity
	wrong.Birth = "different"
	if _, err := admin.Call(context.Background(), s.Socket(), admin.Request{Version: 1, Operation: "test-fault", Fault: signalToken("daemon-kill", wrong), Enabled: true}); err == nil {
		t.Fatal("wrong launch signalled")
	}
	if err := call("daemon-stop"); err != nil {
		t.Fatal(err)
	}
	waitProcessState(t, identity.PID, "T")
	if err := call("daemon-continue"); err != nil {
		t.Fatal(err)
	}
	waitProcessState(t, identity.PID, "S")
	if err := call("daemon-kill"); err != nil {
		t.Fatal(err)
	}
	waitProcessState(t, identity.PID, "Z")
	if err := call("daemon-continue"); err == nil {
		t.Fatal("exited child signalled")
	}
	var info unix.Siginfo
	if err := unix.Waitid(unix.P_PID, identity.PID, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil); err != nil || info.Signo == 0 {
		t.Fatalf("zombie was reaped while admission open: %v %+v", err, info)
	}
	_ = s.Close()
	if err := unix.Waitid(unix.P_PID, identity.PID, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil); !errors.Is(err, unix.ECHILD) {
		t.Fatalf("closed supervisor did not reap: %v", err)
	}
	if _, err := s.BindWorkspace(context.Background()); err == nil {
		t.Fatal("workspace binding reopened a closed supervisor")
	}
	if err := s.toggle(context.Background(), signalToken("daemon-kill", identity), true); err == nil {
		t.Fatal("signal admission reopened after reap")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "daemon.audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"outcome":"rejected"`) || !strings.Contains(string(raw), `"outcome":"applied"`) {
		t.Fatal("signal intent/result evidence missing")
	}
}
func waitProcessState(t *testing.T, pid int, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join("/proc", fmt.Sprint(pid), "status"))
		if err == nil && strings.Contains(string(raw), "State:\t"+want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("owned child %d did not reach state %s", pid, want)
}
