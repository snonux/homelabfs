package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	dir, err := os.MkdirTemp("", "hfa-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{SocketPath: filepath.Join(dir, "admin.sock"), NodeID: "node", VolumeID: "volume", Generation: "generation", Status: func(context.Context) (Status, error) { return Status{NodeID: "node"}, nil }, Audit: func(context.Context, Event) error { return nil }}
}
func testServer(t *testing.T, c Config) *Server {
	t.Helper()
	s, err := Listen(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func call(t *testing.T, c Config, r Request) (Response, error) {
	t.Helper()
	r.Version = 1
	return Call(t.Context(), c.SocketPath, r)
}

func TestSocketKernelCredentialsAndReadControlSeparation(t *testing.T) {
	c := testConfig(t)
	observed := make(chan struct {
		peer  Peer
		level Level
	}, 2)
	c.Authorize = func(p Peer, l Level) bool {
		observed <- struct {
			peer  Peer
			level Level
		}{p, l}
		return l == Read
	}
	testServer(t, c)
	response, err := call(t, c, Request{Operation: "status"})
	if err != nil || response.Status == nil || response.Status.NodeID != "node" {
		t.Fatal(response, err)
	}
	identity := <-observed
	peer, level := identity.peer, identity.level
	if peer.UID != uint32(os.Geteuid()) || peer.GID != uint32(os.Getegid()) || level != Read {
		t.Fatal("not kernel process identity", peer, level)
	}
	if peer.PID != nil && *peer.PID != int32(os.Getpid()) {
		t.Fatal("unexpected kernel pid", *peer.PID)
	}
	if _, err := call(t, c, Request{Operation: "test-fault", Fault: "example"}); err == nil {
		t.Fatal("read role controlled faults")
	}
	if err := privateSocket(c.SocketPath); err != nil {
		t.Fatal(err)
	}
}

func TestControlHooksRequireEnableAuthorizationAndAudit(t *testing.T) {
	for _, scenario := range []string{"disabled", "enabled", "read-only", "audit-failure", "unsupported-recovery", "foreign-uid"} {
		t.Run(scenario, func(t *testing.T) {
			c := testConfig(t)
			c.EnableTestHooks = scenario != "disabled"
			calls := 0
			events := []Event{}
			c.ToggleFault = func(context.Context, string, bool) error { calls++; return nil }
			c.Authorize = func(_ Peer, l Level) bool { return scenario != "read-only" || l == Read }
			c.Audit = func(_ context.Context, e Event) error {
				events = append(events, e)
				if scenario == "audit-failure" {
					return errors.New("disk unavailable")
				}
				return nil
			}
			s := testServer(t, c)
			r := Request{Version: 1, Operation: "test-fault", Fault: "example", Enabled: true}
			p := Peer{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())}
			if scenario == "unsupported-recovery" {
				r.Operation = "emergency-activate"
			}
			if scenario == "foreign-uid" {
				p.UID++
			}
			response := s.dispatch(t.Context(), p, r)
			if scenario == "enabled" {
				if response.Error != "" || calls != 1 || len(events) != 2 || events[0].Outcome != "requested" || events[1].Outcome != "applied" {
					t.Fatal(response, calls, events)
				}
			} else if response.Error == "" || calls != 0 || len(events) != 1 {
				t.Fatal("control admission escaped gate", response, calls, events)
			}
			for _, e := range events {
				if e.Peer.UID != p.UID || e.VolumeID != c.VolumeID || e.Generation != c.Generation || e.Fault != r.Fault {
					t.Fatal("audit lost target/operator", e)
				}
			}
		})
	}
}

func TestWireRejectsInvalidFrames(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"operation":"status","operation":"test-fault"}`,
		`{"Version":1,"operation":"status"}`,
		`{"version":1,"operation":"status","enabled":false}`,
		`{"version":1,"operation":"test-fault","fault":"x"}`,
		`{"version":1,"operation":"status","fault":null}`,
		`{"version":1,"operation":"status"} {}`,
		`{"version":2,"operation":"status"}`,
		`{"version":1,"operation":"status","forged_uid":0}`,
	} {
		if _, err := decodeRequest([]byte(raw + "\n")); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	c := testConfig(t)
	testServer(t, c)
	for _, raw := range []string{`{"version":1,"operation":"status"}` + "\n", strings.Repeat("x", maxFrame+1) + "\n"} {
		conn, err := net.Dial("unix", c.SocketPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte(raw)); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		response, err := readFrame(conn)
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
		var reply Response
		if err := json.Unmarshal(response, &reply); err != nil {
			t.Fatal(err)
		}
		if (len(raw) > maxFrame) != (reply.Error != "") {
			t.Fatal("unexpected wire response", reply)
		}
	}
}

func TestSocketPreservesExistingPathsAndRejectsPermissions(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "directory-mode"} {
		t.Run(kind, func(t *testing.T) {
			c := testConfig(t)
			switch kind {
			case "file":
				if err := os.WriteFile(c.SocketPath, []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing", c.SocketPath); err != nil {
					t.Fatal(err)
				}
			case "directory-mode":
				if err := os.Chmod(filepath.Dir(c.SocketPath), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Listen(t.Context(), c); err == nil {
				t.Fatal("accepted unsafe existing path")
			}
			if kind != "directory-mode" {
				if _, err := os.Lstat(c.SocketPath); err != nil {
					t.Fatal("existing path removed", err)
				}
			}
		})
	}
	c := testConfig(t)
	testServer(t, c)
	if err := os.Chmod(c.SocketPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, c, Request{Operation: "status"}); err == nil {
		t.Fatal("client accepted public socket")
	}
}

func TestConnectionsBoundedAndIdleDeadline(t *testing.T) {
	c := testConfig(t)
	s := testServer(t, c)
	connections := []net.Conn{}
	defer func() {
		for _, c := range connections {
			_ = c.Close()
		}
	}()
	for i := 0; i < maxConnections; i++ {
		conn, err := net.Dial("unix", c.SocketPath)
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
	}
	deadline := time.Now().Add(time.Second)
	for len(s.slots) != maxConnections {
		if time.Now().After(deadline) {
			t.Fatal("connections not admitted")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := call(t, c, Request{Operation: "status"}); err == nil {
		t.Fatal("exceeded connection bound")
	}
	conn := connections[0]
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, _ = readFrame(conn) // A timed-out peer may close before a refusal can be written.
	started := time.Now()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("close exceeded request deadline")
	}
	if _, err := os.Lstat(c.SocketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("socket not removed", err)
	}
}

func TestConcurrentStatusAndControls(t *testing.T) {
	c := testConfig(t)
	var fault atomic.Bool
	c.EnableTestHooks = true
	c.ToggleFault = func(_ context.Context, _ string, v bool) error { fault.Store(v); return nil }
	c.Status = func(context.Context) (Status, error) { return Status{TestHooksEnabled: fault.Load()}, nil }
	testServer(t, c)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			for j := 0; j < 8; j++ {
				if _, err := call(t, c, Request{Operation: "status"}); err != nil {
					t.Error(err)
				}
				if _, err := call(t, c, Request{Operation: "test-fault", Fault: "example", Enabled: j%2 == 0}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	group.Wait()
}

func TestStatusAuthorizationDeniesBeforeObservation(t *testing.T) {
	c := testConfig(t)
	var reads atomic.Int32
	c.Status = func(context.Context) (Status, error) { reads.Add(1); return Status{}, nil }
	c.Authorize = func(Peer, Level) bool { return false }
	server := testServer(t, c)
	if _, err := call(t, c, Request{Operation: "status"}); err == nil {
		t.Fatal("read denial bypassed")
	}
	response := server.dispatch(t.Context(), Peer{UID: uint32(os.Geteuid()) + 1}, Request{Version: 1, Operation: "status"})
	if response.Error == "" || reads.Load() != 0 {
		t.Fatal("unauthorized status disclosed state", response, reads.Load())
	}
}
