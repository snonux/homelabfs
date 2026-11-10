package guestbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type mappingProducerPrimitive struct {
	prepared, produced atomic.Int64
	attachmentErr      error
	pauseCell          uint64
	entered, resume    chan struct{}
}

func (*mappingProducerPrimitive) Bind(context.Context, VerificationBinding) error { return nil }
func (p *mappingProducerPrimitive) Prepare(ctx context.Context, command Command) error {
	p.prepared.Add(1)
	if command.Cell == p.pauseCell && p.entered != nil {
		close(p.entered)
		select {
		case <-p.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (*mappingProducerPrimitive) Produce(context.Context, Command, json.RawMessage) (*VerificationReceipt, error) {
	return nil, nil
}
func (p *mappingProducerPrimitive) BindTransport(_ context.Context, tr *Transport) error {
	_, err := tr.VerificationBinding(p)
	if err != nil {
		return err
	}
	return p.attachmentErr
}
func (p *mappingProducerPrimitive) ProduceMapped(ctx context.Context, _ Command, _ json.RawMessage) (*VerificationReceiptV2, error) {
	if _, ok := VerifiedMapping(ctx); !ok {
		return nil, fsapi.Permission
	}
	p.produced.Add(1)
	return nil, nil
}
func mappedFixture(t *testing.T) (*bridgeFixture, *Transport, string) {
	t.Helper()
	f := newBridge(t)
	f.hello(t)
	f.next = 0
	protocol := MappedProtocol()
	f.s.config.Protocol = &protocol
	f.s.config.Verification = &mappingProducerPrimitive{}
	f.s.config.ViewProtocol = &recordingViewProvider{scope: f.c}
	root := filepath.Join(t.TempDir(), "mapping")
	tr, err := NewDurableTransport(f.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	return f, tr, root
}
func mappedHello(t *testing.T, f *bridgeFixture, native, guest uint64) Command {
	p := MappedProtocol()
	return Command{Cell: native, Context: f.c, Protocol: &p, Op: "HELLO", Payload: mustJSON(t, p), Delegation: &Delegation{Version: DelegationVersion, GuestCell: guest, GuestOp: "HELLO"}}
}
func mappingWrite(t *testing.T, tr *Transport, n Native, c Command, flags uint16, begin, end int) error {
	t.Helper()
	raw := mustJSON(t, c)
	if end < 0 {
		end = len(raw)
	}
	for offset := begin; offset < end; offset += ChunkBytes {
		wire, err := EncodeFrame(Frame{Minor: 4, Kind: 1, Flags: flags, Total: uint32(len(raw)), Offset: uint32(offset), Session: parseID(c.Context.Session), Cell: c.Cell, Hash: sha256.Sum256(raw), Data: raw[offset:min(offset+ChunkBytes, len(raw))]})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tr.Write(t.Context(), n, n.Endpoint.CommandNode, c.Cell*CellStride+uint64(offset/ChunkBytes)*(HeaderBytes+ChunkBytes), wire); err != nil {
			return err
		}
	}
	return nil
}
func mappingLongCall(t *testing.T, f *bridgeFixture, native, guest uint64) Command {
	cap := Capability{Token: digest([]byte("unknown")), Kind: "DESCRIPTION", Scope: f.c, Home: f.c.Volume, Allocation: uid(3).String(), Description: ptr(uid(100).String()), Rights: 3, Revision: 1, CredentialHash: digest([]byte("cred")), AncestorsHash: digest([]byte("ancestors"))}
	intent := Intent{Opcode: "WRITE", Args: mustJSON(t, map[string]any{"description": cap, "offset": 0, "max_end": int64(1<<63 - 1), "data": strings.Repeat("ab", 65536)})}
	call := Call{Claim: Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: guest, OriginalHash: digest(mustJSON(t, intent))}, Credentials: f.cred(), Intent: intent}
	p := MappedProtocol()
	return Command{Cell: native, Context: f.c, Protocol: &p, Op: "CALL", Payload: mustJSON(t, call), Delegation: &Delegation{Version: DelegationVersion, GuestCell: guest, GuestOp: "CALL"}}
}
func TestMappedCodecClosedAndStreamingHash(t *testing.T) {
	f := newBridge(t)
	command := mappedHello(t, f, 7, 3)
	original, err := inverseCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, original)
	if !bytes.Contains(raw, []byte(`"delegation":null`)) {
		t.Fatal("missing null original")
	}
	hash, err := hashCommand(original)
	if err != nil || hash != digest(raw) {
		t.Fatal("streamed original hash differs", err)
	}
	for _, minor := range []uint16{0, 1, 2, 3} {
		if _, err = DecodeCommandVersion(raw, minor); err == nil {
			t.Fatal("legacy accepted new envelope")
		}
	}
	if _, err = DecodeCommandVersion(raw, 4); err != nil {
		t.Fatal(err)
	}
	fields, _ := envelopeFields(raw)
	delete(fields, "delegation")
	values := map[string]json.RawMessage{}
	for key, value := range fields {
		values[key] = value
	}
	if _, err = DecodeCommandVersion(mustJSON(t, values), 4); err == nil {
		t.Fatal("omitted null accepted")
	}
	for _, change := range []func(*Delegation){func(d *Delegation) { d.Version = "other" }, func(d *Delegation) { d.PollIndex = 16 }, func(d *Delegation) { d.GuestCell = 0 }, func(d *Delegation) { d.GuestOp = "RETIRE" }} {
		changed := command
		d := *command.Delegation
		changed.Delegation = &d
		change(&d)
		if _, err = inverseCommand(changed); err == nil {
			t.Fatal("invalid transformation accepted")
		}
	}
}
func TestMappedTransportDurablePrivateCandidateAndDonor(t *testing.T) {
	f, tr, root := mappedFixture(t)
	command := mappedHello(t, f, 1, 5)
	if _, err := f.s.Execute(t.Context(), f.native, command); !errors.Is(err, fsapi.Permission) {
		t.Fatal("direct delegated Execute admitted", err)
	}
	if err := mappingWrite(t, tr, f.native, command, 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	response := fetchNative(t, tr, f.native, 1)
	original, _ := inverseCommand(command)
	guestHash, err := translatedResponseHash(response, 5, "HELLO", false)
	if err != nil {
		t.Fatal(err)
	}
	ack := &Ack{Cell: 1, ResponseHash: digest(response)}
	guestAck := &Ack{Cell: 5, ResponseHash: guestHash}
	p := MappedProtocol()
	release := Command{Cell: 2, Context: f.c, Protocol: &p, Op: "RELEASE_CELL", Payload: mustJSON(t, ack), Reclaim: ack, Delegation: &Delegation{Version: DelegationVersion, GuestCell: 6, GuestOp: "RELEASE_CELL", GuestReclaim: guestAck}}
	bad := release
	d := *release.Delegation
	wrong := *guestAck
	wrong.ResponseHash = digest([]byte("wrong"))
	d.GuestReclaim = &wrong
	bad.Delegation = &d
	if err = mappingWrite(t, tr, f.native, bad, 2, 0, -1); !errors.Is(err, fsapi.Permission) {
		t.Fatal("wrong guest donor accepted", err)
	}
	// The refused donor send left the reclaim workspace (n64): the donor is
	// kept, and the valid exact donor transfer takes the same native number.
	tr.mu.Lock()
	kept := tr.workspace == nil && tr.high == 1 && tr.cells[1] != nil && tr.cells[1].mapping.GuestCommandHash == digest(mustJSON(t, original))
	tr.mu.Unlock()
	if !kept {
		t.Fatal("donor lost or workspace kept after a refused donor send")
	}
	if err = mappingWrite(t, tr, f.native, release, 2, 0, -1); err != nil {
		t.Fatal(err)
	}
	_ = fetchNative(t, tr, f.native, 2)
	tr.Close()
	state, cells := inspectCells(t, root)
	if len(state.Cells) != 1 || cells[0].Mapping.GuestCell != 6 || cells[0].Mapping.NativeFullHash == "" || len(cells[0].Mapping.Legs) != 1 {
		t.Fatal("mapping not protected with donor deletion")
	}
	memory := newBridge(t)
	memory.s.config.Protocol = &p
	memory.s.config.Verification = &mappingProducerPrimitive{}
	if _, err = NewTransport(memory.s, transportBudget(t)); err == nil {
		t.Fatal("memory 1.4 accepted")
	}
}
func TestMappedIncompleteWatermarksAndIssuedHazards(t *testing.T) {
	f, tr, root := mappedFixture(t)
	a := mappingLongCall(t, f, 1, 5)
	replay := mappingLongCall(t, f, 2, 5)
	if err := mappingWrite(t, tr, f.native, a, 0, 0, ChunkBytes); err != nil {
		t.Fatal(err)
	}
	if err := mappingWrite(t, tr, f.native, replay, 0, 0, ChunkBytes); err != nil {
		t.Fatal(err)
	}
	b := mappedHello(t, f, 3, 6)
	if err := mappingWrite(t, tr, f.native, b, 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	_ = fetchNative(t, tr, f.native, 3)
	// The exporter's sole writer never interleaves cells: a frame at the next
	// number proves that the incomplete cell before it was abandoned (x64,
	// replaceAbandoned). So native 1 was removed when native 2 began, and
	// native 2 when native 3 began; neither can complete under an old
	// watermark capture after the higher guest cell 6 was admitted.
	if err := mappingWrite(t, tr, f.native, a, 0, ChunkBytes, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("abandoned partial completed after higher guest", err)
	}
	if err := mappingWrite(t, tr, f.native, replay, 0, ChunkBytes, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("old-capture duplicate admitted", err)
	}
	tr.mu.Lock()
	badBinding := len(tr.cells) != 1 || tr.ordinary != 1 || tr.guestHigh[f.c.Session] != 6
	tr.mu.Unlock()
	if badBinding {
		t.Fatal("abandoned partials kept or admitted binding lost")
	}
	tr.Close()
	// Both abandoned numbers stay spent without a refusal record.
	state, cells := inspectCells(t, root)
	if state.GuestHigh[f.c.Session] != 6 || len(cells) != 1 || state.High != 3 || state.Refused != nil {
		t.Fatal(state)
	}
}
func TestMappedRowFundingDoesNotHTMLEscape(t *testing.T) {
	if err := mappedCellFunding(); err != nil {
		t.Fatal(err)
	}
	groups := make([]uint32, 1024)
	for i := range groups {
		groups[i] = ^uint32(0) - uint32(i)
	}
	native := Native{UID: ^uint32(0), GID: ^uint32(0), PID: ^uint32(0), Unique: ^uint64(0), Groups: groups, Process: uid(1).String(), Endpoint: Endpoint{FuseSession: uid(2).String(), CommandNode: ^uint64(0), ResultNode: ^uint64(0) - 1, RecallNode: ^uint64(0) - 2}}
	body := bytes.Repeat([]byte("x"), BodyBytes)
	payload := json.RawMessage(fmt.Sprintf("%q", strings.Repeat("<>&", (BodyBytes-4096)/3)))
	c := ProtectedTransportCell{Header: Frame{Cell: MaxCell}, Body: body, Command: &Command{Cell: MaxCell, Payload: payload}, Native: native, Response: body, Mapping: &ProtectedMapping{}}
	for i := 0; i < 32; i++ {
		c.Observations = append(c.Observations, native)
		c.Chunks = append(c.Chunks, true)
		c.Wire = append(c.Wire, make([]byte, ChunkBytes+HeaderBytes))
		c.Seen = append(c.Seen, make([]byte, ChunkBytes+HeaderBytes))
	}
	for i := 0; i < 16; i++ {
		c.Mapping.Legs = append(c.Mapping.Legs, MappingLeg{Index: uint64(i), Cell: MaxCell, CommandHash: strings.Repeat("a", 64), ResponseHash: strings.Repeat("b", 64)})
	}
	raw, err := boundedTransportJSON(c)
	if err != nil || len(raw) > transportCellLimit {
		t.Fatal("worst row exceeded funded8MiB", len(raw), err)
	}
	if bytes.Contains(raw, []byte(`\u003c`)) {
		t.Fatal("ledger expanded raw payload")
	}
	var decoded ProtectedTransportCell
	if err = decodeTransportJSON(raw, transportCellLimit, &decoded); err != nil || !bytes.Equal(decoded.Body, c.Body) || !bytes.Equal(decoded.Command.Payload, payload) || !bytes.Equal(decoded.Response, body) {
		t.Fatal("private serialization changed exact bytes", err)
	}
}

func mappedWaitFixture(t *testing.T) (*viewDispatchFixture, *Transport, string) {
	t.Helper()
	x := newViewDispatchFixture(t)
	protocol := MappedProtocol()
	x.bridge.s.config.Protocol = &protocol
	x.bridge.s.config.Verification = &mappingProducerPrimitive{}
	x.views.reply = []byte(`{"event":null,"status":"EMPTY"}`)
	root := filepath.Join(t.TempDir(), "wait-mapping")
	tr, err := NewDurableTransport(x.bridge.s, durableBudget(t), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	return x, tr, root
}
func waitLeg(t *testing.T, x *viewDispatchFixture, native, index uint64, reclaim *Ack) Command {
	p := MappedProtocol()
	return Command{Cell: native, Context: x.bridge.c, Protocol: &p, Op: "VIEW_POLL", Payload: mustJSON(t, map[string]any{"domain": x.domain, "after_sequence": 0}), Reclaim: reclaim, Delegation: &Delegation{Version: DelegationVersion, GuestCell: 5, GuestOp: "VIEW_WAIT", PollIndex: index}}
}
func TestMappedWaitSixteenActualEmptyLegsAndNonadjacentCells(t *testing.T) {
	x, tr, root := mappedWaitFixture(t)
	var ack *Ack
	var originalHash string
	for index := uint64(0); index < 16; index++ {
		native := index*2 + 1
		command := waitLeg(t, x, native, index, ack)
		original, _ := inverseCommand(command)
		currentHash, _ := hashCommand(original)
		if index == 0 {
			originalHash = currentHash
		} else if currentHash != originalHash {
			t.Fatal("WAIT original changed")
		}
		if err := mappingWrite(t, tr, x.bridge.native, command, 1+2*mappedContinuationFlag(index), 0, -1); err != nil {
			t.Fatal(err)
		}
		raw := fetchNativeAt(t, tr, x.bridge.native, native, x.bridge.native.Endpoint.RecallNode)
		ack = &Ack{Cell: native, ResponseHash: digest(raw)}
		if index < 15 {
			other := mappedHello(t, x.bridge, native+1, 100+index)
			if err := mappingWrite(t, tr, x.bridge.native, other, 0, 0, -1); err != nil {
				t.Fatal(err)
			}
			_ = fetchNative(t, tr, x.bridge.native, native+1)
		}
	}
	tr.Close()
	_, cells := inspectCells(t, root)
	var current *ProtectedMapping
	for _, c := range cells {
		if c.Header.Cell == 31 {
			current = c.Mapping
		}
	}
	if current == nil || len(current.Legs) != 16 || current.PollIndex != 15 || !current.Empty || current.GuestCommandHash != originalHash {
		t.Fatal("missing actual16-leg EMPTY chain")
	}
	for i, leg := range current.Legs {
		if leg.Cell != uint64(i*2+1) || leg.Index != uint64(i) {
			t.Fatal("chain assumed adjacent cells")
		}
	}
}
func mappedContinuationFlag(index uint64) uint16 {
	if index > 0 {
		return 1
	}
	return 0
}
func TestMappedWaitGuestClosureAndContinuationNegatives(t *testing.T) {
	for _, negative := range []string{"wrong-index", "changed-original", "wrong-full-native", "wrong-base-native", "wrong-full-guest", "wrong-base-guest", "closed-chain"} {
		t.Run(negative, func(t *testing.T) {
			x, tr, _ := mappedWaitFixture(t)
			first := waitLeg(t, x, 1, 0, nil)
			if err := mappingWrite(t, tr, x.bridge.native, first, 1, 0, -1); err != nil {
				t.Fatal(err)
			}
			raw := fetchNativeAt(t, tr, x.bridge.native, 1, x.bridge.native.Endpoint.RecallNode)
			ack := &Ack{Cell: 1, ResponseHash: digest(raw)}
			next := waitLeg(t, x, 2, 1, ack)
			switch negative {
			case "wrong-index":
				next.Delegation.PollIndex = 2
			case "changed-original":
				next.Delegation.GuestReclaim = &Ack{Cell: 99, ResponseHash: digest(raw)}
			case "wrong-full-native":
				next.Reclaim = &Ack{Cell: 1, ResponseHash: digest([]byte("wrong"))}
			case "wrong-base-native":
				tr.mu.Lock()
				base := tr.cells[1].mapping.NativeBaseHash
				tr.mu.Unlock()
				if base == ack.ResponseHash {
					t.Fatal("oracle base equals full")
				}
				next.Reclaim = &Ack{Cell: 1, ResponseHash: base}
			case "wrong-full-guest", "wrong-base-guest", "closed-chain":
				guestHash, _ := translatedResponseHash(raw, 5, "VIEW_WAIT", false)
				if negative == "wrong-full-guest" {
					guestHash = digest([]byte("wrong"))
				}
				if negative == "wrong-base-guest" {
					tr.mu.Lock()
					base := tr.cells[1].mapping.GuestBaseHash
					tr.mu.Unlock()
					if base == guestHash {
						t.Fatal("oracle base equals full")
					}
					guestHash = base
				}
				p := MappedProtocol()
				release := Command{Cell: 2, Context: x.bridge.c, Protocol: &p, Op: "RELEASE_CELL", Payload: mustJSON(t, ack), Reclaim: ack, Delegation: &Delegation{Version: DelegationVersion, GuestCell: 6, GuestOp: "RELEASE_CELL", GuestReclaim: &Ack{Cell: 5, ResponseHash: guestHash}}}
				err := mappingWrite(t, tr, x.bridge.native, release, 3, 0, -1)
				if negative == "wrong-full-guest" || negative == "wrong-base-guest" {
					if err == nil {
						t.Fatal("wrong translated full donor accepted")
					}
					return
				}
				if err != nil {
					t.Fatal("authenticated new guest could not close EMPTY chain", err)
				}
				_ = fetchNative(t, tr, x.bridge.native, 2)
				next = waitLeg(t, x, 3, 1, ack)
			}
			if err := mappingWrite(t, tr, x.bridge.native, next, 3, 0, -1); err == nil {
				t.Fatal("invalid or closed WAIT continuation admitted")
			}
		})
	}
}
func TestMappedRecentCapacityReservesIncompleteCompletion(t *testing.T) {
	// The exporter's sole writer finishes or abandons one cell before the
	// next (x64), so the only incomplete cell is the one at the high-water,
	// started here after 79 admitted cells. Without an older incomplete cell
	// their markers are pruned, so the marker limit is not reached; the test
	// checks that the partial still completes and that a later header is
	// admitted. Before x64 an interleaved partial reserved the last marker
	// credit; a new header now abandons such a partial (replaceAbandoned),
	// which frees that credit.
	f, tr, _ := mappedFixture(t)
	partial := mappingLongCall(t, f, 80, 80)
	var nativeAck, guestAck *Ack
	for guest := uint64(1); guest <= 79; guest++ {
		command := mappedHello(t, f, guest, guest)
		command.Reclaim = nativeAck
		command.Delegation.GuestReclaim = guestAck
		flags := uint16(0)
		if nativeAck != nil {
			flags = 2
		}
		if err := mappingWrite(t, tr, f.native, command, flags, 0, -1); err != nil {
			t.Fatalf("guest%d: %v", guest, err)
		}
		response := fetchNative(t, tr, f.native, guest)
		translated, _ := translatedResponseHash(response, guest, "HELLO", false)
		nativeAck = &Ack{Cell: guest, ResponseHash: digest(response)}
		guestAck = &Ack{Cell: guest, ResponseHash: translated}
	}
	if err := mappingWrite(t, tr, f.native, partial, 0, 0, ChunkBytes); err != nil {
		t.Fatal("partial header within the marker limit", err)
	}
	if err := mappingWrite(t, tr, f.native, partial, 0, ChunkBytes, -1); err != nil {
		t.Fatal("funded incomplete completion deadlocked", err)
	}
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 81, 81), 0, 0, -1); err != nil {
		t.Fatal("safe marker prune did not unblock new header", err)
	}
}

func TestMappedProtectionFailuresFenceBeforeAuthorityAndExposure(t *testing.T) {
	for _, point := range []string{"before-commit", "after-commit", "protected-mapping-admission", "protected-mapping-base", "protected-response"} {
		t.Run(point, func(t *testing.T) {
			f, tr, root := mappedFixture(t)
			producer := f.s.config.Verification.(*mappingProducerPrimitive)
			tr.ledger.failpoint = func(name string) error {
				if name == point {
					return errors.New("mapping protection uncertainty")
				}
				return nil
			}
			err := mappingWrite(t, tr, f.native, mappedHello(t, f, 1, 1), 0, 0, -1)
			if point == "before-commit" || point == "after-commit" {
				if err == nil {
					t.Fatal("input ACK escaped failed protection")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				tr.mu.Lock()
				failed := tr.failed != nil
				tr.mu.Unlock()
				if failed {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("mapping failure did not fence")
				}
				time.Sleep(time.Millisecond)
			}
			if _, err = tr.Read(t.Context(), f.native, f.native.Endpoint.ResultNode, CellStride, 32768); !errors.Is(err, fsapi.Unavailable) {
				t.Fatal("response exposed after uncertainty", err)
			}
			if point == "before-commit" || point == "after-commit" || point == "protected-mapping-admission" {
				if producer.prepared.Load() != 0 {
					t.Fatal("effects prepared before protected admission")
				}
			}
			if point != "protected-response" && producer.produced.Load() != 0 {
				t.Fatal("receipt production preceded protected chain base")
			}
			tr.Close()
			_, cells := inspectCells(t, root)
			if point == "before-commit" {
				if len(cells) != 0 {
					t.Fatal("uncommitted candidate appeared durable")
				}
				return
			}
			if len(cells) != 1 || cells[0].Mapping == nil {
				t.Fatal("uncertain mapping lost")
			}
			if point == "protected-response" && (cells[0].Mapping.NativeFullHash == "" || cells[0].Mapping.GuestFullHash == "") {
				t.Fatal("response committed without both full hashes")
			}
		})
	}
}
func TestMappedIndexFundingWorstScopeAndFreshnessFields(t *testing.T) {
	f := newBridge(t)
	state := ProtectedTransportState{Version: 1, Endpoint: f.native.Endpoint, Controller: uid(90), GuestHigh: map[string]uint64{}}
	p := MappedProtocol()
	for i := 0; i < 64; i++ {
		scope := f.c
		scope.Session = uid(byte(i + 100)).String()
		state.Scopes = append(state.Scopes, scope)
		state.Protocols = append(state.Protocols, &p)
		state.GuestHigh[scope.Session] = MaxCell
	}
	for i := 0; i < 80; i++ {
		state.Cells = append(state.Cells, MaxCell-uint64(79-i))
		state.GuestRecent = append(state.GuestRecent, GuestIssuance{Session: state.Scopes[i%64].Session, Cell: MaxCell - uint64(i)})
	}
	// Worst-case reorder holes: every unissued cell of each session's window.
	state.GuestHoles = map[string][]uint64{}
	for _, scope := range state.Scopes {
		for h := MaxCell - guestReorderWindow + 1; h < MaxCell; h++ {
			if !slices.Contains(state.GuestRecent, GuestIssuance{Session: scope.Session, Cell: h}) {
				state.GuestHoles[scope.Session] = append(state.GuestHoles[scope.Session], h)
			}
		}
	}
	state.High = MaxCell
	state.Ordinary = 64
	state.Control = 16
	state.Workspace = true
	raw, err := json.Marshal(state)
	if err != nil || len(raw) > transportIndexLimit {
		t.Fatal("index exceeded funded128KiB", len(raw), err)
	}
	if err = validateTransportIndex(state); err != nil {
		t.Fatal(err)
	}
}

func TestMappedCommandMarshalPreservesAllClosedLegacyShapes(t *testing.T) {
	f := newBridge(t)
	for minor := uint16(0); minor <= 3; minor++ {
		command := Command{Cell: MaxCell, Context: f.c, Op: "HELLO", Payload: json.RawMessage(`{"raw":"<>&\\\""}`)}
		if minor > 0 {
			p := ViewProtocol()
			if minor == 2 {
				p = ControlledProtocol()
			}
			if minor == 3 {
				p = CleanupProtocol()
			}
			command.Protocol = &p
		}
		expected := map[string]any{"cell": command.Cell, "context": command.Context, "op": command.Op, "payload": command.Payload, "reclaim": nil}
		if command.Protocol != nil {
			expected["protocol"] = command.Protocol
		}
		actual, err := marshal(command)
		if err != nil {
			t.Fatal(err)
		}
		before, err := marshal(expected)
		if err != nil || !bytes.Equal(actual, before) {
			t.Fatalf("closed%d encoding changed: %v", minor, err)
		}
		type oldCommand struct {
			Protocol *Protocol       `json:"protocol,omitempty"`
			Cell     uint64          `json:"cell"`
			Context  Context         `json:"context"`
			Op       string          `json:"op"`
			Payload  json.RawMessage `json:"payload"`
			Reclaim  *Ack            `json:"reclaim"`
		}
		oldJSON, err := json.Marshal(oldCommand{command.Protocol, command.Cell, command.Context, command.Op, command.Payload, command.Reclaim})
		if err != nil {
			t.Fatal(err)
		}
		newJSON, err := json.Marshal(command)
		if err != nil || !bytes.Equal(oldJSON, newJSON) {
			t.Fatal("json.Marshal changed closed legacy bytes", minor, err)
		}
		fields, err := envelopeFields(actual)
		if err != nil {
			t.Fatal(err)
		}
		if _, present := fields["delegation"]; present {
			t.Fatal("newfield in closed old union")
		}
	}
}

func TestMappedLiteralCodecAndSeparateReceiptUnion(t *testing.T) {
	raw, err := os.ReadFile("../../docs/spec/retained9p-fixtures/mapping.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus viewCorpus
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.Protocol != MappedProtocol() {
		t.Fatal("frozen tuple changed")
	}
	for _, vector := range corpus.Commands {
		t.Run(vector.Name, func(t *testing.T) {
			body := mustCanonical(t, vector.Body)
			if digest(body) != vector.Hash {
				t.Fatal("literal hash changed")
			}
			command, err := DecodeCommandVersion(body, 4)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := hex.DecodeString(vector.Frame)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := DecodeFrame(wire)
			if err != nil || frame.Minor != 4 || !bytes.Equal(frame.Data, body) {
				t.Fatal("literal frame changed", err)
			}
			encoded, err := EncodeFrame(frame)
			if err != nil || !bytes.Equal(encoded, wire) {
				t.Fatal("literal frame roundtrip", err)
			}
			if command.Delegation != nil {
				original, err := inverseCommand(command)
				if err != nil {
					t.Fatal(err)
				}
				streamed, err := hashCommand(original)
				if err != nil || streamed != digest(mustJSON(t, original)) {
					t.Fatal("literal original hash mismatch")
				}
			}
			for minor := uint16(0); minor < 4; minor++ {
				if _, err = DecodeCommandVersion(body, minor); err == nil {
					t.Fatal("old union accepted1.4")
				}
			}
		})
	}
	var receipts struct{ Receipts []json.RawMessage }
	if err = json.Unmarshal(raw, &receipts); err != nil {
		t.Fatal(err)
	}
	for _, raw := range receipts.Receipts {
		canonical := mustCanonical(t, raw)
		if _, err = DecodeVerificationReceiptV2(canonical); err != nil {
			t.Fatal(err)
		}
		if _, err = DecodeVerificationReceipt(canonical); err == nil {
			t.Fatal("v1 accepted receipt2")
		}
		for _, change := range []func(map[string]json.RawMessage){func(o map[string]json.RawMessage) { o["poll_count"] = json.RawMessage(`17`) }, func(o map[string]json.RawMessage) { delete(o, "guest_cell") }, func(o map[string]json.RawMessage) { o["extra"] = json.RawMessage(`null`) }, func(o map[string]json.RawMessage) {
			o["version"] = json.RawMessage(`"homelabfs-controlled-verification/1"`)
		}} {
			var o map[string]json.RawMessage
			if err = json.Unmarshal(canonical, &o); err != nil {
				t.Fatal(err)
			}
			change(o)
			if _, err = DecodeVerificationReceiptV2(mustJSON(t, o)); err == nil {
				t.Fatal("open/invalid receipt2 accepted")
			}
		}
	}
}

func TestMappedRecentRejectsReplayAfterActualDonorDeletion(t *testing.T) {
	f, tr, _ := mappedFixture(t)
	older := mappingLongCall(t, f, 1, 2)
	if err := mappingWrite(t, tr, f.native, older, 0, 0, ChunkBytes); err != nil {
		t.Fatal(err)
	}
	issued := mappedHello(t, f, 2, 2)
	if err := mappingWrite(t, tr, f.native, issued, 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	response := fetchNative(t, tr, f.native, 2)
	guestHash, _ := translatedResponseHash(response, 2, "HELLO", false)
	replacement := mappedHello(t, f, 3, 3)
	replacement.Reclaim = &Ack{Cell: 2, ResponseHash: digest(response)}
	replacement.Delegation.GuestReclaim = &Ack{Cell: 2, ResponseHash: guestHash}
	if err := mappingWrite(t, tr, f.native, replacement, 2, 0, -1); err != nil {
		t.Fatal(err)
	}
	_ = fetchNative(t, tr, f.native, 3)
	// The older header was abandoned when native 2 began (x64: the exporter's
	// sole writer never interleaves cells), so no old-capture header is left
	// that could replay the deleted guest key, and the marker need not be
	// retained for it. Its completion reads its spent number as Stale.
	tr.mu.Lock()
	active := tr.cells[2] != nil
	abandoned := tr.cells[1] == nil
	tr.mu.Unlock()
	if active || !abandoned {
		t.Fatal("oracle did not delete active donor or kept the abandoned header")
	}
	if err := mappingWrite(t, tr, f.native, older, 0, ChunkBytes, -1); !errors.Is(err, fsapi.Stale) {
		t.Fatal("deleted guest key replayed through older accepted header", err)
	}
}
func TestMappedEventClosesWaitPolling(t *testing.T) {
	x, tr, _ := mappedWaitFixture(t)
	_ = x.control(t, "VIEW_POLL") // actual valid EVENT payload from the fixture Views boundary
	first := waitLeg(t, x, 1, 0, nil)
	if err := mappingWrite(t, tr, x.bridge.native, first, 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	response := fetchNativeAt(t, tr, x.bridge.native, 1, x.bridge.native.Endpoint.RecallNode)
	next := waitLeg(t, x, 2, 1, &Ack{Cell: 1, ResponseHash: digest(response)})
	if err := mappingWrite(t, tr, x.bridge.native, next, 3, 0, -1); err == nil {
		t.Fatal("terminal EVENT admitted another poll")
	}
}

func TestMappedAcceptedOrdinaryExecutesAfterLaterControl(t *testing.T) {
	f, tr, _ := mappedFixture(t)
	producer := f.s.config.Verification.(*mappingProducerPrimitive)
	producer.pauseCell = 1
	producer.entered = make(chan struct{})
	producer.resume = make(chan struct{})
	defer func() {
		select {
		case <-producer.resume:
		default:
			close(producer.resume)
		}
	}()
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 1, 1), 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-producer.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ordinary worker did not enter barrier")
	}
	if err := mappingWrite(t, tr, f.native, mappedHello(t, f, 2, 2), 0, 0, -1); err != nil {
		t.Fatal(err)
	}
	p := MappedProtocol()
	query := Command{Cell: 3, Context: f.c, Protocol: &p, Op: "QUERY", Payload: mustJSON(t, map[string]any{"claim": Claim{Realm: uid(60).String(), Subject: "61", Anchor: uid(61).String(), Nonce: uid(62).String(), Number: 99, OriginalHash: digest([]byte("original"))}}), Delegation: &Delegation{Version: DelegationVersion, GuestCell: 3, GuestOp: "QUERY"}}
	if err := mappingWrite(t, tr, f.native, query, 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	_ = fetchNative(t, tr, f.native, 3)
	tr.mu.Lock()
	badOrder := tr.guestHigh[f.c.Session] != 3 || tr.cells[2].mapping.Admitted
	tr.mu.Unlock()
	if badOrder {
		t.Fatal("oracle did not leave earlier accepted job pending")
	}
	close(producer.resume)
	_ = fetchNative(t, tr, f.native, 1)
	_ = fetchNative(t, tr, f.native, 2)
}

func TestMappedConstructorRefusesTypedNilProducerBeforeOwnership(t *testing.T) {
	f := newBridge(t)
	protocol := MappedProtocol()
	f.s.config.Protocol = &protocol
	var producer *mappingProducerPrimitive
	f.s.config.Verification = producer
	budget := durableBudget(t)
	root := filepath.Join(t.TempDir(), "typed-nil")
	transport, err := NewDurableTransport(f.s, budget, root)
	if !errors.Is(err, fsapi.Permission) || transport != nil {
		t.Fatal("typed-nil mapped adapter was not refused", err)
	}
	if _, err = os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing producer created ledger namespace", err)
	}
	f.s.mu.Lock()
	attached := f.s.transportAttached || f.s.transport != nil
	f.s.mu.Unlock()
	if attached {
		t.Fatal("missing producer claimed transport ownership")
	}
	// The same budget, service and unopened root remain usable; the refusal
	// cannot consume an arena or permanently claim a fresh allocator.
	f.s.config.Verification = &mappingProducerPrimitive{}
	transport, err = NewDurableTransport(f.s, budget, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.Close)
}

func TestMappedAttachmentFailureReturnsUnopenedArenaBudget(t *testing.T) {
	failed := newBridge(t)
	protocol := MappedProtocol()
	failed.s.config.Protocol = &protocol
	rejected := errors.New("actual attachment refused")
	failed.s.config.Verification = &mappingProducerPrimitive{attachmentErr: rejected}
	budget := durableBudget(t)
	root := filepath.Join(t.TempDir(), "failed-attachment")
	transport, err := NewDurableTransport(failed.s, budget, root)
	if transport != nil || !errors.Is(err, rejected) {
		t.Fatal("attachment refusal was not preserved", err)
	}
	failed.s.mu.Lock()
	retained := failed.s.transportAttached && failed.s.transport == nil
	failed.s.mu.Unlock()
	if !retained {
		t.Fatal("attachment failure abandoned the service claim")
	}
	retryRoot := filepath.Join(t.TempDir(), "same-service-retry")
	if retry, err := NewDurableTransport(failed.s, budget, retryRoot); retry != nil || !errors.Is(err, fsapi.Busy) {
		t.Fatal("uncertain service admitted a second allocator", err)
	}
	if _, err = os.Stat(retryRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("blocked service created a replacement namespace", err)
	}
	state, cells := inspectCells(t, root)
	if state.High != 0 || len(cells) != 0 {
		t.Fatal("failed unopened owner changed its protected state")
	}
	// The same bounded budget must fund a separate fresh service/root. Keeping
	// the failed service claim must not strand reservations with no Close handle.
	fresh := newBridge(t)
	fresh.s.config.Protocol = &protocol
	fresh.s.config.Verification = &mappingProducerPrimitive{}
	transport, err = NewDurableTransport(fresh.s, budget, filepath.Join(t.TempDir(), "fresh-attachment"))
	if err != nil {
		t.Fatal("attachment failure leaked shared arena reservations", err)
	}
	t.Cleanup(transport.Close)
}
