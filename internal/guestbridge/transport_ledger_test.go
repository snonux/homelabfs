package guestbridge

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/sys/unix"
)

func durableFixture(t *testing.T) (*bridgeFixture, *Transport, string) {
	t.Helper()
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	root := filepath.Join(t.TempDir(), "transport")
	tr, err := NewDurableTransport(f.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	return f, tr, root
}
func helloCommand(t *testing.T, f *bridgeFixture) Command {
	return f.command(t, "HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0})
}
func partialFrame(t *testing.T, f *bridgeFixture, id uint64, flags uint16) (Frame, []byte) {
	t.Helper()
	body := bytes.Repeat([]byte("p"), 2*ChunkBytes)
	frame := Frame{Kind: 1, Flags: flags, Total: uint32(len(body)), Session: parseID(f.c.Session), Cell: id, Hash: sha256.Sum256(body), Data: body[:ChunkBytes]}
	wire, err := EncodeFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	return frame, wire
}
func inspectCells(t *testing.T, root string) (ProtectedTransportState, []ProtectedTransportCell) {
	t.Helper()
	var cells []ProtectedTransportCell
	state, err := InspectTransport(root, func(c ProtectedTransportCell) error { cells = append(cells, c); return nil })
	if err != nil {
		t.Fatal(err)
	}
	return state, cells
}

func TestDurableTransportChunksWorkspaceAndIdentity(t *testing.T) {
	for _, workspace := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "workspace"}[workspace], func(t *testing.T) {
			f, tr, root := durableFixture(t)
			flags := uint16(0)
			if workspace {
				flags = 2
			}
			frame, wire := partialFrame(t, f, 1, flags)
			if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); err != nil {
				t.Fatal(err)
			}
			// A conflicting duplicate cannot erase an acknowledged candidate.
			bad := frame
			bad.Data = bytes.Clone(frame.Data)
			bad.Data[0] ^= 1
			changed, _ := EncodeFrame(bad)
			if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, changed); err == nil {
				t.Fatal("changed chunk accepted")
			}
			tr.Close()
			state, cells := inspectCells(t, root)
			if state.Endpoint != f.native.Endpoint || state.Controller != f.s.controller || state.Workspace != workspace || len(state.Digest) != 64 || len(cells) != 1 {
				t.Fatal(state)
			}
			c := cells[0]
			if !bytes.Equal(c.Body[:ChunkBytes], frame.Data) || c.Count != 1 || !c.Chunks[0] || c.Chunks[1] || !reflect.DeepEqual(c.Observations[0], f.native) || c.Command != nil {
				t.Fatal("accepted chunk/native changed")
			}
			if workspace {
				if state.High != 0 || len(state.Cells) != 0 {
					t.Fatal(state)
				}
			} else if state.High != 1 || !reflect.DeepEqual(state.Cells, []uint64{1}) {
				t.Fatal(state)
			}
		})
	}
}

func TestDurableTransportResponseCoverageAndReclaim(t *testing.T) {
	f, tr, root := durableFixture(t)
	command := helloCommand(t, f)
	sendNative(t, tr, f.native, command, 0)
	response := fetchNative(t, tr, f.native, 1)
	ack := &Ack{Cell: 1, ResponseHash: digest(response)}
	replacement := f.command(t, "RELEASE_CELL", ack)
	replacement.Reclaim = ack
	sendNative(t, tr, f.native, replacement, 2)
	_ = fetchNative(t, tr, f.native, 2)
	// The old ID remains closed; no released hole can be reused.
	old := mustJSON(t, command)
	wire, _ := EncodeFrame(Frame{Kind: 1, Total: uint32(len(old)), Session: parseID(f.c.Session), Cell: 1, Hash: sha256.Sum256(old), Data: old})
	if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); !errors.Is(err, fsapi.Stale) {
		t.Fatal(err)
	}
	tr.mu.Lock()
	expected := protectedCell(tr.cells[2])
	tr.mu.Unlock()
	tr.Close()
	state, cells := inspectCells(t, root)
	if state.High != 2 || !reflect.DeepEqual(state.Cells, []uint64{2}) || state.Ordinary != 1 || state.Control != 0 || state.Workspace || len(cells) != 1 {
		t.Fatal(state)
	}
	if !reflect.DeepEqual(cells[0], expected) || cells[0].Unread != 0 || cells[0].JobPhase != "responded" {
		t.Fatal("immutable response/read/reclaim state changed")
	}
	if _, err := NewDurableTransport(newBridge(t).s, durableBudget(t), root); err == nil {
		t.Fatal("old root accepted as fresh")
	}
}

func TestDurableTransportFailureFencesBeforeExternalSuccess(t *testing.T) {
	for _, phase := range []string{"before-commit", "after-commit"} {
		t.Run(phase, func(t *testing.T) {
			f, tr, root := durableFixture(t)
			tr.ledger.failpoint = func(name string) error {
				if name == phase {
					return errors.New("injected sync uncertainty")
				}
				return nil
			}
			_, wire := partialFrame(t, f, 1, 0)
			if n, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); err == nil || n != 0 {
				t.Fatal("failed protect acknowledged", n, err)
			}
			if _, err := tr.Read(t.Context(), f.native, f.native.Endpoint.ResultNode, CellStride, 1); !errors.Is(err, fsapi.Unavailable) {
				t.Fatal("failure did not fence", err)
			}
			if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); !errors.Is(err, fsapi.Unavailable) {
				t.Fatal(err)
			}
			tr.Close()
			state, cells := inspectCells(t, root)
			if phase == "before-commit" {
				if state.High != 0 || len(cells) != 0 {
					t.Fatal(state)
				}
			} else if state.High != 1 || len(cells) != 1 {
				t.Fatal(state)
			}
		})
	}
}

func TestDurableTransportReadFailureRetainsProtectedOwnership(t *testing.T) {
	f, tr, root := durableFixture(t)
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	deadline := time.Now().Add(2 * time.Second)
	for {
		tr.mu.Lock()
		ready := tr.cells[1].response != nil
		tr.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("response absent")
		}
		runtime.Gosched()
	}
	tr.mu.Lock()
	expected := protectedCell(tr.cells[1])
	expected.Seen = cloneWires(expected.Seen)
	tr.ledger.failpoint = func(name string) error {
		if name == "before-commit" {
			return fsapi.NoSpace
		}
		return nil
	}
	tr.mu.Unlock()
	if raw, err := tr.Read(t.Context(), f.native, f.native.Endpoint.ResultNode, CellStride, 32768); err == nil || raw != nil {
		t.Fatal("unprotected read exposed", err)
	}
	tr.Close()
	_, cells := inspectCells(t, root)
	if !reflect.DeepEqual(cells[0], expected) {
		t.Fatal("failed read released protected ownership")
	}
}
func cloneWires(b [][]byte) [][]byte {
	out := make([][]byte, len(b))
	for i := range b {
		out[i] = bytes.Clone(b[i])
	}
	return out
}

func TestDurableTransportPrivateFilesAndExclusiveWriter(t *testing.T) {
	f, tr, root := durableFixture(t)
	_ = f
	if _, err := InspectTransport(root, nil); err == nil {
		t.Fatal("second owner acquired live ledger")
	}
	if _, err := openTransportLedger(root, false); err == nil {
		t.Fatal("second writer acquired live ledger")
	}
	tr.Close()
	if err := os.WriteFile(filepath.Join(root, "unknown"), []byte("uncertain"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectTransport(root, nil); err == nil {
		t.Fatal("unknown file ignored")
	}
	if err := os.Remove(filepath.Join(root, "unknown")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "transport.db"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectTransport(root, nil); err == nil {
		t.Fatal("public ledger admitted")
	}
	if err := os.Chmod(filepath.Join(root, "transport.db"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "ledger-link")
	if err := os.Link(filepath.Join(root, "transport.db"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectTransport(root, nil); err == nil {
		t.Fatal("hardlinked ledger admitted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(filepath.Join(root, "transport.db"), moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, filepath.Join(root, "transport.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectTransport(root, nil); err == nil {
		t.Fatal("symlink ledger admitted")
	}
}

func TestDurableTransportBoundsAndCorruption(t *testing.T) {
	f, tr, root := durableFixture(t)
	_, wire := partialFrame(t, f, 1, 0)
	if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); err != nil {
		t.Fatal(err)
	}
	tr.mu.Lock()
	c := protectedCell(tr.cells[1])
	c.Body = make([]byte, BodyBytes+1)
	if err := validateProtectedCell(c, false); err == nil {
		t.Fatal("oversized body admitted")
	}
	index := tr.protectedIndex()
	index.Cells = append(index.Cells, 1)
	index.Ordinary++
	if err := validateTransportIndex(index); err == nil {
		t.Fatal("duplicate identity admitted")
	}
	// A large JSON buffer is never accepted from an existing damaged ledger.
	if err := decodeTransportJSON(make([]byte, transportCellLimit+1), transportCellLimit, &c); err == nil {
		t.Fatal("unbounded row admitted")
	}
	if err := tr.ledger.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(transportBucket).Put(transportCellKey(1), []byte(`{"unknown":1}`))
	}); err != nil {
		t.Fatal(err)
	}
	tr.mu.Unlock()
	tr.Close()
	if _, err := InspectTransport(root, nil); err == nil {
		t.Fatal("corrupt row admitted")
	}
}

// Child processes use actual durable files and disappear without Close. Portable
// fixture Native/HELLO are component primitives, not installed native authority.
func TestDurableTransportProcessLoss(t *testing.T) {
	if stage := os.Getenv("HFS_TRANSPORT_CRASH_STAGE"); stage != "" {
		durableCrashChild(t, stage)
		return
	}
	for _, stage := range []string{"chunk", "workspace", "response", "read", "reclaim", "pending"} {
		t.Run(stage, func(t *testing.T) {
			base := transportCrashDirectory(t)
			root := filepath.Join(base, "ledger")
			cmd := exec.Command(os.Args[0], "-test.run=^TestDurableTransportProcessLoss$", "-test.timeout=15s")
			cmd.Env = append(os.Environ(), "HFS_TRANSPORT_CRASH_STAGE="+stage, "HFS_TRANSPORT_CRASH_ROOT="+root)
			output, err := cmd.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 73 {
				t.Fatalf("child: %v %s", err, output)
			}
			// Wait reports process exit, not necessarily final kernel release
			// of every mmap-backed open-file description. Observe both actual
			// locks after the known exit; no timeout or ledger error is success.
			if err := waitCrashedTransportLocks(root, time.Second); err != nil {
				t.Fatal(err)
			}
			state, cells := inspectCells(t, root)
			if len(cells) != 1 || !state.Controller.Valid() {
				t.Fatal(state)
			}
			c := cells[0]
			switch stage {
			case "chunk", "workspace":
				expected, err := os.ReadFile(filepath.Join(base, "expected"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(c.Body[:ChunkBytes], expected) || c.Count != 1 || c.Command != nil {
					t.Fatal("acknowledged bytes lost")
				}
				if stage == "workspace" {
					if state.High != 0 || !state.Workspace {
						t.Fatal(state)
					}
				} else if state.High != 1 {
					t.Fatal(state)
				}
			case "response", "read":
				expected, err := os.ReadFile(filepath.Join(base, "expected"))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(c.Wire[0], expected) || c.JobPhase != "responded" || len(c.Response) == 0 {
					t.Fatal("immutable response lost")
				}
				if stage == "read" && c.Unread != 0 || stage == "response" && c.Unread == 0 {
					t.Fatal("read coverage lost")
				}
			case "reclaim":
				if state.High != 2 || !reflect.DeepEqual(state.Cells, []uint64{2}) || c.Command == nil || c.Command.Op != "RELEASE_CELL" || c.Control {
					t.Fatal("reclaim lost", state)
				}
			case "pending":
				if c.Command == nil || c.Command.Op != "CALL" || c.JobPhase != "executing" || !validUUID(c.Native.Process) || c.Response != nil {
					t.Fatal("pending ownership lost", c.JobPhase)
				}
			}
			// The crashed root never becomes a fresh controller/allocator automatically.
			if _, err := createTransportLedger(root); err == nil {
				t.Fatal("crash ledger reused")
			}
		})
	}
}
func durableCrashChild(t *testing.T, stage string) {
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	root := os.Getenv("HFS_TRANSPORT_CRASH_ROOT")
	tr, err := NewDurableTransport(f.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	expected := func(b []byte) {
		if err := os.WriteFile(filepath.Join(filepath.Dir(root), "expected"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	switch stage {
	case "chunk", "workspace":
		flags := uint16(0)
		if stage == "workspace" {
			flags = 2
		}
		frame, wire := partialFrame(t, f, 1, flags)
		expected(frame.Data)
		if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); err != nil {
			t.Fatal(err)
		}
	case "response":
		tr.ledger.failpoint = func(name string) error {
			if name == "protected-response" {
				expected(tr.cells[1].wire[0])
				os.Exit(73)
			}
			return nil
		}
		sendNative(t, tr, f.native, helloCommand(t, f), 0)
		time.Sleep(5 * time.Second)
		t.Fatal("response failpoint missed")
	case "read", "reclaim":
		sendNative(t, tr, f.native, helloCommand(t, f), 0)
		raw := fetchNative(t, tr, f.native, 1)
		if stage == "read" {
			expected(tr.cells[1].wire[0])
			break
		}
		ack := &Ack{Cell: 1, ResponseHash: digest(raw)}
		c := f.command(t, "RELEASE_CELL", ack)
		c.Reclaim = ack
		tr.ledger.failpoint = func(name string) error {
			if name == "protected-reclaim" {
				os.Exit(73)
			}
			return nil
		}
		sendNative(t, tr, f.native, c, 2)
		t.Fatal("reclaim failpoint missed")
	case "pending":
		f.driver.block = make(chan struct{})
		f.driver.entered = make(chan struct{}, 1)
		intent := Intent{Opcode: "ROOT", Args: mustJSON(t, map[string]any{})}
		claim := Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 1, OriginalHash: digest(mustJSON(t, intent))}
		sendNative(t, tr, f.native, f.command(t, "CALL", Call{Claim: claim, Credentials: f.cred(), Intent: intent}), 0)
		select {
		case <-f.driver.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("pending job not entered")
		}
	default:
		t.Fatal("unknown child stage")
	}
	os.Exit(73)
}

func TestDurableTransportOperatingCeilingFences(t *testing.T) {
	f, tr, root := durableFixture(t)
	// Sparse extension tests the actual file-size gate without filling memory or
	// disk. The failed root is intentionally retained by the disposable fixture.
	if err := tr.ledger.file.Truncate(transportFileCeiling); err != nil {
		t.Fatal(err)
	}
	_, wire := partialFrame(t, f, 1, 0)
	if n, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); n != 0 || !errors.Is(err, fsapi.NoSpace) {
		t.Fatal(n, err)
	}
	if tr.failed == nil {
		t.Fatal("size uncertainty not fenced")
	}
	tr.Close()
	if _, err := os.Stat(filepath.Join(root, "transport.db")); err != nil {
		t.Fatal(err)
	}
}

func TestDurableTransportInspectorRejectsVisitorFailure(t *testing.T) {
	f, tr, root := durableFixture(t)
	_, wire := partialFrame(t, f, 1, 0)
	if _, err := tr.Write(t.Context(), f.native, f.native.Endpoint.CommandNode, CellStride, wire); err != nil {
		t.Fatal(err)
	}
	tr.Close()
	sentinel := errors.New("stop inspection")
	if _, err := InspectTransport(root, func(ProtectedTransportCell) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	// Visitor refusal releases exclusive inspection ownership without changing bytes.
	state, _ := inspectCells(t, root)
	if len(state.Digest) != 64 || state.High != 1 {
		t.Fatal(state)
	}
}

func durableBudget(t *testing.T) *admission.Budget {
	t.Helper()
	var config admission.Config
	for class, size := range map[admission.Class]uint64{admission.Live: 512 << 20, admission.Control: 128 << 20, admission.Memory: TransportLedgerMemoryBytes + (12 << 20)} {
		r := admission.Resources{Bytes: size, Operations: 80, Workers: 4}
		config.Pools[class] = admission.Limits{Global: r, PerPeer: r, PerVolume: r}
	}
	budget, err := admission.New(config)
	if err != nil {
		t.Fatal(err)
	}
	return budget
}
func TestDurableTransportRequiresLedgerBudget(t *testing.T) {
	f := newBridge(t)
	root := filepath.Join(t.TempDir(), "unfunded")
	if _, err := NewDurableTransport(f.s, transportBudget(t), root); err == nil {
		t.Fatal("unfunded ledger admitted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("ledger created before funding", err)
	}
}
func TestDurableTransportExactSelectedProtocols(t *testing.T) {
	f, tr, _ := durableFixture(t)
	tr.mu.Lock()
	state := tr.protectedIndex()
	tr.mu.Unlock()
	for _, protocol := range []Protocol{ControlledProtocol(), CleanupProtocol()} {
		state.Protocols = []*Protocol{&protocol}
		raw, err := marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ProtectedTransportState
		if err := decodeTransportJSON(raw, transportIndexLimit, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := validateTransportIndex(decoded); err != nil {
			t.Fatal(err)
		}
		if *decoded.Protocols[0] != protocol || decoded.Scopes[0] != f.c {
			t.Fatal("selected tuple changed")
		}
		decoded.Protocols[0].Features ^= 1
		if err := validateTransportIndex(decoded); err == nil {
			t.Fatal("partial tuple admitted")
		}
	}
}
func TestDurableTransportLargeRowReuse(t *testing.T) {
	f, tr, _ := durableFixture(t)
	frame, _ := partialFrame(t, f, 1, 0)
	frame.Total = BodyBytes
	c := newCell(frame)
	c.reservation = CellReservation{Release: func() {}}
	c.count = len(c.chunks)
	for i := range c.chunks {
		c.chunks[i] = true
		c.observations[i] = cloneNative(f.native)
	}
	tr.mu.Lock()
	tr.cells[1] = c
	tr.high = 1
	tr.ordinary = 1
	for i := 0; i < 12; i++ {
		c.body[0] = byte(i)
		if err := tr.protectLocked(1); err != nil {
			t.Fatal(err)
		}
	}
	info, err := tr.ledger.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	tr.mu.Unlock()
	if info.Size() > 16<<20 {
		t.Fatal("bounded row retained append history", info.Size())
	}
}

func TestDurableTransportResponseAndJobBindingNegatives(t *testing.T) {
	f, tr, _ := durableFixture(t)
	sendNative(t, tr, f.native, helloCommand(t, f), 0)
	_ = fetchNative(t, tr, f.native, 1)
	tr.mu.Lock()
	index := tr.protectedIndex()
	raw, err := boundedTransportJSON(protectedCell(tr.cells[1]))
	if err != nil {
		t.Fatal(err)
	}
	tr.mu.Unlock()
	cases := map[string]func(*ProtectedTransportCell){
		"wire":        func(c *ProtectedTransportCell) { c.Wire[0][len(c.Wire[0])-1] ^= 1 },
		"coverage":    func(c *ProtectedTransportCell) { c.Unread++ },
		"seen":        func(c *ProtectedTransportCell) { c.Seen[0][0] = 2 },
		"native":      func(c *ProtectedTransportCell) { c.Native.Endpoint.CommandNode++ },
		"observation": func(c *ProtectedTransportCell) { c.Observations[0].Endpoint.CommandNode++ },
		"command":     func(c *ProtectedTransportCell) { c.Command.Cell++ },
		"phase":       func(c *ProtectedTransportCell) { c.JobPhase = "accepted" },
		"class":       func(c *ProtectedTransportCell) { c.Control = true },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var c ProtectedTransportCell
			if err := decodeTransportJSON(raw, transportCellLimit, &c); err != nil {
				t.Fatal(err)
			}
			change(&c)
			if err := validateTransportCellBinding(index, c, false); err == nil {
				t.Fatal("changed protected ownership admitted")
			}
		})
	}
}

func TestDurableTransportNamingParentIsPinnedAndSynced(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	ledger, err := createTransportLedger(filepath.Join(parent, "ledger"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ledger.close() }()
	original, err := ledger.parent.Stat()
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(base, "moved-parent")
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	pinned, err := ledger.parent.Stat()
	if err != nil {
		t.Fatal(err)
	}
	replaced, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(original, pinned) || os.SameFile(replaced, pinned) {
		t.Fatal("naming parent was reopened by path")
	}
	if err := ledger.finishNamespace(); err != nil {
		t.Fatal(err)
	}
	if ledger.parent != nil {
		t.Fatal("parent fd retained after successful sync")
	}
	if _, err := os.Stat(filepath.Join(moved, "ledger", "transport.db")); err != nil {
		t.Fatal(err)
	}
}
func TestDurableTransportNamingParentSyncFailureRetainsRoot(t *testing.T) {
	f := newBridge(t)
	root := filepath.Join(t.TempDir(), "ledger")
	ledger, err := createTransportLedger(root)
	if err != nil {
		t.Fatal(err)
	}
	tr := &Transport{service: f.s, cells: map[uint64]*cell{}, ledger: ledger}
	if err := tr.protectLocked(); err != nil {
		t.Fatal(err)
	}
	// An actual closed directory fd makes Sync fail. No callback can claim that
	// the namespace was protected, and the original private files remain.
	if err := ledger.parent.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ledger.finishNamespace(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("closed parent sync succeeded", err)
	}
	_ = ledger.close()
	state, cells := inspectCells(t, root)
	if state.Controller != f.s.controller || state.High != 0 || len(cells) != 0 {
		t.Fatal("failed namespace protection destroyed ledger")
	}
	if _, err := createTransportLedger(root); err == nil {
		t.Fatal("failed namespace root reused")
	}
}

// transportCrashDirectory preserves failed process-loss resources for inspection.
func transportCrashDirectory(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("", "hfs-transport-crash-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("preserved failed transport fixture: %s", base)
			return
		}
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove successful fixture: %v", err)
		}
	})
	return base
}

// waitCrashedTransportLocks is an oracle synchronization primitive used only
// after the exact child has exited with status73. Linux may transiently retain
// the database open-file lock after Wait while releasing its mapping. One second
// bounds scheduler/kernel handoff, not authority: only both successful actual
// nonblocking flocks prove release. Other errors and persistent contention fail.
// Product InspectTransport remains one-shot and refuses any uncertain owner.
func waitCrashedTransportLocks(root string, bound time.Duration) error {
	open := func(name string) (*os.File, error) {
		fd, err := unix.Open(filepath.Join(root, name), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), name)
		if err := transportOwned(f, false); err != nil {
			_ = f.Close()
			return nil, err
		}
		return f, nil
	}
	owner, err := open("owner.lock")
	if err != nil {
		return err
	}
	defer func() { _ = owner.Close() }()
	database, err := open("transport.db")
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()
	deadline := time.Now().Add(bound)
	for {
		err = unix.Flock(int(owner.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			err = unix.Flock(int(database.Fd()), unix.LOCK_SH|unix.LOCK_NB)
			if err == nil {
				unlockDB := unix.Flock(int(database.Fd()), unix.LOCK_UN)
				unlockOwner := unix.Flock(int(owner.Fd()), unix.LOCK_UN)
				if unlockDB != nil {
					return unlockDB
				}
				return unlockOwner
			}
			if e := unix.Flock(int(owner.Fd()), unix.LOCK_UN); e != nil {
				return e
			}
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("exited transport still owns kernel lock: %w", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDurableTransportCrashOracleRefusesActualHeldLocks(t *testing.T) {
	for _, kind := range []string{"owner", "database"} {
		t.Run(kind, func(t *testing.T) {
			_, tr, root := durableFixture(t)
			var file *os.File
			if kind == "database" {
				tr.Close()
				var err error
				file, err = os.Open(filepath.Join(root, "transport.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = file.Close() }()
				if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			err := waitCrashedTransportLocks(root, 10*time.Millisecond)
			if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
				t.Fatal("persistent actual ownership was not refused", err)
			}
			if kind == "database" {
				if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
					t.Fatal(err)
				}
			} else {
				tr.Close()
			}
			if err := waitCrashedTransportLocks(root, time.Second); err != nil {
				t.Fatal("released actual ownership not observed", err)
			}
			_, _ = inspectCells(t, root)
		})
	}
	root := filepath.Join(t.TempDir(), "missing")
	if err := waitCrashedTransportLocks(root, time.Second); !os.IsNotExist(err) {
		t.Fatal("non-contention error hidden", err)
	}
}
