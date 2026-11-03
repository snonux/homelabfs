//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func TestExporterHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "hfs-exporter" {
		return
	}
	socket := os.NewFile(3, "inherited-supervisor")
	if socket == nil {
		os.Exit(91)
	}
	if _, err := socket.Write([]byte("R")); err != nil {
		os.Exit(92)
	}
	var command [1]byte
	if _, err := socket.Read(command[:]); err == nil && command[0] == 'F' {
		endpoint := guestbridge.Endpoint{FuseSession: fsapi.UUID{1}.String(), CommandNode: 100, ResultNode: 101, RecallNode: 102}
		if err := writeControl(socket, controlFrame{opcode: ControlClaim, sequence: 1, payload: append(endpointBytes(endpoint), make([]byte, 32)...)}); err != nil {
			os.Exit(94)
		}
		_, _ = io.Copy(io.Discard, socket)
	} else if err == nil && command[0] == 'E' {
		_ = syscall.Exec("/bin/sleep", []string{"sleep", "10"}, []string{})
		os.Exit(93)
	}
	_ = socket.Close()
	os.Exit(0)
}

func helperSpec(t *testing.T) Executable {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	return Executable{Path: executable, SHA256: sha256.Sum256(bytes), Args: []string{"-test.run=^TestExporterHelper$", "--", "hfs-exporter"}}
}

func ownerFixture(t *testing.T) *Supervisor {
	t.Helper()
	s, err := Create(t.Context(), filepath.Join(t.TempDir(), "owner"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
		if s.child != nil {
			select {
			case <-s.child.exited:
			case <-time.After(5 * time.Second):
				t.Error("exporter did not exit after management EOF")
			}
			_ = s.child.executable.Close()
		}
	})
	return s
}

func runningFixture(t *testing.T) *Supervisor {
	t.Helper()
	s := ownerFixture(t)
	if err := s.Start(t.Context(), helperSpec(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.child.control.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var ready [1]byte
	if _, err := io.ReadFull(s.child.control, ready[:]); err != nil || ready[0] != 'R' {
		t.Fatalf("real child readiness: %q %v", ready, err)
	}
	return s
}

func TestExclusiveRegistryMissingLedgerAndReplacementRefusal(t *testing.T) {
	s := runningFixture(t)
	if _, err := Open(t.Context(), s.root); err == nil {
		t.Fatal("second owner acquired live lock")
	}
	if err := s.child.live(); err != nil {
		t.Fatalf("real exporter not live: %v", err)
	}
	id, err := s.ExporterIncarnation(t.Context())
	if err != nil || !id.Valid() || id != s.record.ProcessID {
		t.Fatalf("actual exporter label: %v %v", id, err)
	}
	if err := s.Start(t.Context(), helperSpec(t)); err == nil {
		t.Fatal("replacement launch accepted")
	}
	root := s.root
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err = reopened.Start(t.Context(), helperSpec(t)); err == nil {
		t.Fatal("reopened ownership became fresh")
	}
	if err = reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, "issuance.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(t.Context(), root); err == nil {
		t.Fatal("missing prior ledger accepted")
	}
}

func TestFailedStartupPreservesConsumedOwnership(t *testing.T) {
	for _, name := range []string{"wrong-digest", "missing-executable", "non-executable-bytes"} {
		t.Run(name, func(t *testing.T) {
			s := ownerFixture(t)
			spec := helperSpec(t)
			switch name {
			case "wrong-digest":
				spec.SHA256[0] ^= 1
			case "missing-executable":
				spec.Path = filepath.Join(t.TempDir(), "absent")
			case "non-executable-bytes":
				spec.Path = filepath.Join(t.TempDir(), "invalid")
				b := []byte("not an executable")
				if err := os.WriteFile(spec.Path, b, 0600); err != nil {
					t.Fatal(err)
				}
				spec.SHA256 = sha256.Sum256(b)
			}
			if err := s.Start(t.Context(), spec); err == nil {
				t.Fatal("invalid launch succeeded")
			}
			b, err := os.ReadFile(filepath.Join(s.root, "issuance.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(b, []byte("start-failed")) {
				t.Fatalf("failure not retained: %s", b)
			}
			if err = s.Start(t.Context(), helperSpec(t)); err == nil {
				t.Fatal("failed startup reused ownership")
			}
		})
	}
}

func TestLiveIdentityRejectsReplacementGroupsAndDeath(t *testing.T) {
	t.Run("groups", func(t *testing.T) {
		s := runningFixture(t)
		s.child.identity.Groups = append(s.child.identity.Groups, 0xffffffff)
		if err := s.child.live(); err == nil {
			t.Fatal("truncated/changed full native groups accepted")
		}
	})
	t.Run("exec-replacement", func(t *testing.T) {
		s := runningFixture(t)
		if _, err := s.child.control.Write([]byte("E")); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for s.child.live() == nil && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if err := s.child.live(); err == nil {
			t.Fatal("replacement executable retained authority")
		}
		// This is our disposable fixture process only; request a normal SIGTERM.
		if err := s.child.command.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("death", func(t *testing.T) {
		s := runningFixture(t)
		if err := s.child.control.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-s.child.exited:
		case <-time.After(5 * time.Second):
			t.Fatal("child did not exit")
		}
		if err := s.child.live(); err == nil {
			t.Fatal("dead child authenticated")
		}
	})
}

func TestPortableFixtureCannotMintNativeAuthority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux primitive-only negative constructor boundary; never mount in tests")
	}
	s := runningFixture(t)
	shell, err := fuseadapter.NewShell(99, 100, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MountShell(t.Context(), t.TempDir(), shell); err == nil {
		t.Fatal("portable primitive minted native endpoint")
	}
	endpoint := guestbridge.Endpoint{FuseSession: fsapi.UUID{1}.String(), CommandNode: 100, ResultNode: 101, RecallNode: 102}
	if err = s.Enroll(t.Context(), endpoint, fsapi.UUID{2}); err == nil {
		t.Fatal("synthetic tuple enrolled")
	}
	if _, err = s.Snapshot(t.Context(), fuseadapter.Caller{Thread: s.child.identity.PID, UID: s.child.identity.UID, GID: s.child.identity.GID, Unique: 1}, endpoint); err == nil {
		t.Fatal("unbound shell supplied native credentials")
	}
	if err = s.ServeControl(context.Background()); err == nil {
		t.Fatal("unbound management became ready")
	}
}

func TestProductionControlFramingAndBounds(t *testing.T) {
	endpoint := guestbridge.Endpoint{FuseSession: fsapi.UUID{1}.String(), CommandNode: 100, ResultNode: 101, RecallNode: 102}
	payload := append(endpointBytes(endpoint), make([]byte, 32)...)
	want := controlFrame{opcode: ControlClaim, sequence: 1, payload: payload}
	parent, child, err := controlPair()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close() }()
	defer func() { _ = child.Close() }()
	if err = parent.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- writeControl(child, want) }()
	got, err := readControl(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if got.opcode != want.opcode || got.sequence != want.sequence || !bytes.Equal(got.payload, want.payload) {
		t.Fatal("production frame changed exact bytes")
	}
	valid := encodeControl(want)
	for _, name := range []string{"magic", "version", "reserved", "opcode", "oversized", "length", "truncated"} {
		t.Run(name, func(t *testing.T) {
			bad := bytes.Clone(valid)
			switch name {
			case "magic":
				bad[0] ^= 1
			case "version":
				bad[4]++
			case "reserved":
				bad[6] = 1
			case "opcode":
				bad[5] = 255
			case "oversized":
				binary.BigEndian.PutUint32(bad[12:], ^uint32(0))
				bad = bad[:16]
			case "length":
				binary.BigEndian.PutUint32(bad[12:], 71)
			case "truncated":
				bad = bad[:len(bad)-1]
			}
			if _, err := readControl(bytes.NewReader(bad)); err == nil {
				t.Fatal("invalid control frame accepted")
			}
		})
	}
}

func TestProvisionObservationCannotBeReusedOrRebound(t *testing.T) {
	// This tests the private one-use context primitive, not native admission.
	s := &Supervisor{}
	n := guestbridge.Native{UID: 1, GID: 2, PID: 3, Unique: 4, Process: "incarnation", Groups: []uint32{2, 9}, Endpoint: guestbridge.Endpoint{CommandNode: 100}}
	if consumeProvisionObservation(context.Background(), s, n) {
		t.Fatal("ordinary context authenticated provisioner")
	}
	ctx := context.WithValue(context.Background(), provisionKey{}, &provisionObservation{owner: s, native: n})
	changed := n
	changed.Unique++
	if consumeProvisionObservation(ctx, s, changed) {
		t.Fatal("wrong request consumed observation")
	}
	changed = n
	changed.Groups = []uint32{2}
	if consumeProvisionObservation(ctx, s, changed) {
		t.Fatal("truncated groups consumed observation")
	}
	if consumeProvisionObservation(ctx, &Supervisor{}, n) {
		t.Fatal("wrong owner consumed observation")
	}
	if !consumeProvisionObservation(ctx, s, n) {
		t.Fatal("original observation refused")
	}
	if consumeProvisionObservation(ctx, s, n) {
		t.Fatal("observation reused for another Verify")
	}
}

func TestOwnershipRootAndExecutableReplacementAreRejected(t *testing.T) {
	t.Run("unsafe-root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "unsafe")
		if err := os.Mkdir(root, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(t.Context(), root); err == nil {
			t.Fatal("world-accessible root accepted")
		}
	})
	t.Run("symlink-root", func(t *testing.T) {
		s := ownerFixture(t)
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(s.root, link); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(t.Context(), link); err == nil {
			t.Fatal("symlink ownership root accepted")
		}
	})
	t.Run("pinned-path-replacement", func(t *testing.T) {
		s := runningFixture(t)
		next := filepath.Join(s.root, "replacement")
		if err := os.WriteFile(next, []byte("replacement"), 0500); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(next, filepath.Join(s.root, "exporter")); err != nil {
			t.Fatal(err)
		}
		if err := s.child.live(); err == nil {
			t.Fatal("replaced/unlinked executable retained authority")
		}
	})
}

func TestProvisionerHasOnlyExactMetadataAndNoSyntheticCallback(t *testing.T) {
	endpoint := guestbridge.Endpoint{FuseSession: fsapi.UUID{1}.String(), CommandNode: 100, ResultNode: 101, RecallNode: 102}
	for _, test := range []struct {
		node uint64
		op   string
		want bool
	}{
		{1, "LOOKUP:" + guestbridge.ControlName, true}, {99, "LOOKUP:command", true}, {99, "LOOKUP:result", true}, {99, "LOOKUP:recall", true},
		{1, "GETATTR", true}, {99, "GETATTR", true}, {100, "GETATTR", true}, {100, "OPEN", true}, {101, "OPEN", true}, {102, "OPEN", true}, {1, "STATFS", true},
		{1, "OPEN", false}, {99, "OPEN", false}, {100, "READ", false}, {101, "READ", false}, {100, "WRITE", false}, {102, "POLL", false}, {100, "SEND", false}, {99, "LOOKUP:workload", false}, {100, "LOOKUP:command", false}, {200, "GETATTR", false}, {100, "STATFS", false},
	} {
		if got := provisionMetadata(test.node, test.op, endpoint, 99); got != test.want {
			t.Errorf("metadata %d %s=%v want %v", test.node, test.op, got, test.want)
		}
	}
	identity, err := observeProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	shell, err := fuseadapter.NewShell(99, 100, 101, 102)
	if err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{mount: &nativeMount{shell: shell, provisioning: true, provisioner: identity}, record: issuance{Endpoint: endpoint}}
	caller := fuseadapter.Caller{UID: identity.UID, GID: identity.GID, Thread: identity.PID, Unique: 1}
	for _, op := range []string{"GETATTR", "OPEN", "READ", "WRITE", "SEND", "STATFS"} {
		if _, _, err := s.SnapshotRequest(context.Background(), caller, endpoint, 100, op); err == nil {
			t.Fatalf("synthetic %s callback minted provisioning authority", op)
		}
	}
	s.mount.provisioning = false
	if _, _, err := s.SnapshotRequest(context.Background(), caller, endpoint, 100, "GETATTR"); err == nil {
		t.Fatal("sealed provisioner remained admissible")
	}
}

func TestNativeFDObservationExactTupleControllerAndRequest(t *testing.T) {
	// Serialized observations are supplied by the pinned native C adapter's actual
	// syscalls, never by the guest. This tests the production comparison/codec.
	endpoint := guestbridge.Endpoint{FuseSession: fsapi.UUID{1}.String(), CommandNode: 100, ResultNode: 101, RecallNode: 102}
	controller := fsapi.UUID{2}
	expected := [3]descriptorIdentity{{Device: 77, Inode: 100, Size: guestbridge.VirtualSize, Access: 1}, {Device: 77, Inode: 101, Size: guestbridge.VirtualSize, Access: 2}, {Device: 77, Inode: 102, Size: guestbridge.VirtualSize, Access: 2}}
	payload := make([]byte, 184)
	copy(payload, endpointBytes(endpoint))
	copy(payload[40:], controller[:])
	binary.BigEndian.PutUint64(payload[56:], 9)
	for i, want := range expected {
		p := payload[64+i*40 : 64+(i+1)*40]
		binary.BigEndian.PutUint32(p, uint32(5+i))
		binary.BigEndian.PutUint32(p[4:], want.Access|4)
		binary.BigEndian.PutUint64(p[8:], want.Device)
		binary.BigEndian.PutUint64(p[16:], want.Inode)
		binary.BigEndian.PutUint64(p[24:], want.Size)
		binary.BigEndian.PutUint32(p[32:], 1)
	}
	got, err := validateFDObservation(payload, endpoint, controller, expected, 9)
	if err != nil || got != [3]int{5, 6, 7} {
		t.Fatalf("valid typed observation: %v %v", got, err)
	}
	for _, offset := range []int{0, 16, 40, 56, 68, 72, 80, 88, 96, 100, 108, 112, 120, 128, 136, 140, 148, 152, 160, 168, 176, 180} {
		bad := bytes.Clone(payload)
		bad[offset] ^= 0x80
		if _, err := validateFDObservation(bad, endpoint, controller, expected, 9); err == nil {
			t.Errorf("mutated observation byte %d accepted", offset)
		}
	}
	if _, err := validateFDObservation(payload, endpoint, controller, expected, 10); err == nil {
		t.Fatal("old request observation accepted")
	}
	if _, err := validateFDObservation(payload, endpoint, fsapi.UUID{3}, expected, 9); err == nil {
		t.Fatal("foreign controller accepted")
	}
	duplicate := bytes.Clone(payload)
	copy(duplicate[104:108], duplicate[64:68])
	if _, err := validateFDObservation(duplicate, endpoint, controller, expected, 9); err == nil {
		t.Fatal("duplicate child fd accepted")
	}
	frame := encodeControl(controlFrame{opcode: ControlVerify, sequence: 9, payload: payload})
	decoded, err := readControl(bytes.NewReader(frame))
	if err != nil || !bytes.Equal(decoded.payload, payload) {
		t.Fatalf("production VERIFY frame %v", err)
	}
}

func TestRealChildUsesProductionInheritedControlFrame(t *testing.T) {
	s := runningFixture(t)
	if _, err := s.child.control.Write([]byte("F")); err != nil {
		t.Fatal(err)
	}
	frame, err := readControl(s.child.control)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := guestbridge.Endpoint{FuseSession: fsapi.UUID{1}.String(), CommandNode: 100, ResultNode: 101, RecallNode: 102}
	if frame.opcode != ControlClaim || frame.sequence != 1 || !sameEndpoint(frame.payload[:40], endpoint) {
		t.Fatal("inherited child changed production frame")
	}
}

func TestFDObservationFundsOnlyOneActualKernelRequest(t *testing.T) {
	child := &exporter{}
	if child.consumeObservation(10) {
		t.Fatal("missing C observation admitted request")
	}
	child.tupleVerified = true
	if child.consumeObservation(0) {
		t.Fatal("synthetic zero unique admitted")
	}
	if !child.consumeObservation(10) {
		t.Fatal("actual first unique refused")
	}
	if child.consumeObservation(11) {
		t.Fatal("consumed C observation reused for new request")
	}
	child.observationConsumed = false
	if child.consumeObservation(10) {
		t.Fatal("old kernel request rebound to new observation")
	}
	if !child.consumeObservation(11) {
		t.Fatal("fresh observation failed next actual request")
	}
}

func TestRegistryContenderHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-1] != "hfs-registry-contender" {
		return
	}
	s, err := Open(context.Background(), os.Args[len(os.Args)-2])
	if err == nil {
		_ = s.Close()
		os.Exit(95)
	}
	os.Exit(0)
}

func TestOwnerLockRejectsIndependentProcess(t *testing.T) {
	s := ownerFixture(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestRegistryContenderHelper$", "--", s.root, "hfs-registry-contender")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("independent process acquired owner lock or helper failed: %v %s", err, output)
	}
}

func TestServeControlRejectsSecondReaderWithoutDisturbingFirst(t *testing.T) {
	// Package-private prerequisites exercise only reader ownership and framing,
	// never claim a native endpoint or invoke native authentication.
	parent, peer, err := controlPair()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = parent.Close(); _ = peer.Close() }()
	file, err := os.CreateTemp(t.TempDir(), "private-tuple-fixture")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	s := &Supervisor{child: &exporter{control: parent}, mount: &nativeMount{files: [3]*os.File{file}}, record: issuance{Controller: fsapi.UUID{1}, Stage: "exported"}}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() { first <- s.ServeControl(ctx) }()
	for {
		s.mu.Lock()
		started := s.controlStarted
		s.mu.Unlock()
		if started {
			break
		}
		select {
		case err := <-first:
			t.Fatalf("first reader exited before ownership: %v", err)
		case <-ctx.Done():
			t.Fatal("first reader failed to acquire ownership")
		case <-time.After(time.Millisecond):
		}
	}
	if err := s.ServeControl(ctx); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("second reader: %v", err)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		t.Fatal("second invocation fenced active reader")
	}
	select {
	case err := <-first:
		t.Fatalf("second invocation disturbed active read: %v", err)
	default:
	}
	malformed := make([]byte, 16)
	copy(malformed, "BAD!")
	if _, err = peer.Write(malformed); err != nil {
		t.Fatalf("second invocation closed active stream: %v", err)
	}
	select {
	case err = <-first:
		if !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("active reader did not parse its original stream: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("active reader did not finish bounded frame")
	}
}
