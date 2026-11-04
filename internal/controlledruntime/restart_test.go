//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
)

type restartChild struct {
	t             *testing.T
	mu            sync.Mutex
	command       net.Conn
	native        net.Conn
	rig           *sourceRig
	transport     *guestbridge.Transport
	cells         []uint64
	claims        []uint64
	acks          []map[uint64]guestbridge.Ack
	classes       []map[uint64]bool
	replies       []map[uint64][]byte
	keep          map[string]bool
	autoReclaim   bool
	lastCall      []uint64
	originals     map[uint64][]byte
	writes        map[uint64]int
	writeBodies   map[uint64][]byte
	loseCallReply bool
	lost          bool
}

func buildRestartChild(t *testing.T) string {
	t.Helper()
	compiler, err := exec.LookPath("cc")
	require(t, err)
	repository, err := filepath.Abs("../..")
	require(t, err)
	binary := filepath.Join(t.TempDir(), "restart-child")
	crypto := "-lcrypto"
	if runtime.GOOS == "freebsd" {
		crypto = "-lmd"
	}
	command := exec.CommandContext(t.Context(), compiler, "-std=c11", "-D_DEFAULT_SOURCE", "-D_POSIX_C_SOURCE=200809L", "-Wall", "-Wextra", "-Werror", "-O1", "-g", filepath.Join(repository, "tools/integration/lib9p/tests/restart_child.c"), filepath.Join(repository, "tools/integration/lib9p/src/hfs9p_json.c"), filepath.Join(repository, "tools/integration/lib9p/src/hfs9p_ledger.c"), "-pthread", crypto, "-o", binary)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual C recovery fixture compile: %v\n%s", err, output)
	}
	return binary
}
func startRestartChild(t *testing.T, binary string) *restartChild {
	t.Helper()
	root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "restart-")
	require(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("preserved restart resources %s", root)
		} else {
			require(t, os.RemoveAll(root))
		}
	})
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "io"), Net: "unix"})
	require(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	ledger := filepath.Join(root, "exporter")
	require(t, os.Mkdir(ledger, 0700))
	supervisor, err := Create(t.Context(), filepath.Join(root, "native-owner"))
	require(t, err)
	t.Cleanup(func() {
		require(t, supervisor.Close())
		if supervisor.child != nil {
			select {
			case <-supervisor.child.exited:
			case <-time.After(5 * time.Second):
				t.Error("managed C child failed to exit on original management EOF")
			}
			_ = supervisor.child.executable.Close()
		}
	})
	executable, err := os.ReadFile(binary)
	require(t, err)
	require(t, supervisor.Start(t.Context(), Executable{Path: binary, SHA256: sha256.Sum256(executable), Args: []string{filepath.Join(root, "io"), ledger}}))
	driver := &restartChild{t: t, originals: map[uint64][]byte{}, writes: map[uint64]int{}, writeBodies: map[uint64][]byte{}}
	for i := 0; i < 2; i++ {
		require(t, listener.SetDeadline(time.Now().Add(10*time.Second)))
		connection, e := listener.AcceptUnix()
		require(t, e)
		var kind [1]byte
		_, e = io.ReadFull(connection, kind[:])
		require(t, e)
		switch kind[0] {
		case 'C':
			driver.command = connection
		case 'N':
			driver.native = connection
		default:
			t.Fatal("unknown fixture channel")
		}
	}
	if driver.command == nil || driver.native == nil {
		t.Fatal("missing actual C fixture channel")
	}
	t.Cleanup(func() { _ = driver.command.Close(); _ = driver.native.Close() })
	driver.rig = newSourceRigDomainsOwnedSupervisor(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false, nil, supervisor)
	driver.transport = driver.rig.mappingTransport
	driver.cells = make([]uint64, 2)
	driver.claims = make([]uint64, 2)
	driver.lastCall = make([]uint64, 2)
	driver.keep = map[string]bool{}
	for i := 0; i < 2; i++ {
		driver.acks = append(driver.acks, map[uint64]guestbridge.Ack{})
		driver.classes = append(driver.classes, map[uint64]bool{})
		driver.replies = append(driver.replies, map[uint64][]byte{})
	}
	go driver.serveNative()
	require(t, driver.writePacket('I', mustRuntimeJSON(t, map[string]any{"contexts": driver.rig.installed.Admission.Domains, "controller": supervisor.record.Controller.String()})))
	status, _, err := driver.readResult()
	require(t, err)
	if status != 0 {
		t.Fatalf("actual C init %d", status)
	}
	return driver
}
func (d *restartChild) writePacket(op byte, body []byte) error {
	header := make([]byte, 5)
	header[0] = op
	binary.BigEndian.PutUint32(header[1:], uint32(len(body)))
	if _, err := d.command.Write(append(header, body...)); err != nil {
		return err
	}
	return nil
}
func readFixturePacket(connection net.Conn) (byte, []byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(connection, header[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(header[1:])
	if n > guestbridge.BodyBytes+32 {
		return 0, nil, fsapi.NoSpace
	}
	body := make([]byte, n)
	_, err := io.ReadFull(connection, body)
	return header[0], body, err
}
func (d *restartChild) readResult() (uint32, []byte, error) {
	op, body, err := readFixturePacket(d.command)
	if err != nil {
		return 0, nil, err
	}
	if op != 'D' || len(body) < 4 {
		return 0, nil, fsapi.Invalid
	}
	return binary.BigEndian.Uint32(body), body[4:], nil
}
func (d *restartChild) serveNative() {
	for {
		op, body, err := readFixturePacket(d.native)
		if err != nil {
			return
		}
		if len(body) < 12 {
			return
		}
		position, n := binary.BigEndian.Uint64(body), binary.BigEndian.Uint32(body[8:])
		var output []byte
		var status uint32
		d.mu.Lock()
		rig, transport := d.rig, d.transport
		native := rig.native
		d.mu.Unlock()
		switch op {
		case 'V':
			err = rig.supervisor.Verify(context.Background(), native, rig.supervisor.record.Controller)
		case 'W':
			if int(n) != len(body)-12 {
				return
			}
			frame, e := guestbridge.DecodeFrame(body[12:])
			if e != nil {
				return
			}
			d.mu.Lock()
			d.writes[frame.Cell]++
			if prior := d.writeBodies[frame.Cell]; prior != nil && !bytes.Equal(prior, body[12:]) {
				d.mu.Unlock()
				return
			}
			d.writeBodies[frame.Cell] = bytes.Clone(body[12:])
			d.mu.Unlock()
			var written uint32
			written, err = transport.Write(context.Background(), native, native.Endpoint.CommandNode, position, body[12:])
			output = make([]byte, 4)
			binary.BigEndian.PutUint32(output, written)
			if err == nil {
				command, e := guestbridge.DecodeCommandVersion(frame.Data, frame.Minor)
				d.mu.Lock()
				if e == nil && command.Op == "CALL" && d.loseCallReply && !d.lost {
					d.lost = true
					err = fsapi.IO
				}
				d.mu.Unlock()
			}
		case 'R':
			node := native.Endpoint.ResultNode
			if position>>63 != 0 {
				node = native.Endpoint.RecallNode
				position &= ^(uint64(1) << 63)
			}
			output, err = transport.Read(context.Background(), native, node, position, n)
		default:
			return
		}
		if err != nil {
			status = uint32(unix.EIO)
			if errors.Is(err, fsapi.WouldBlock) {
				status = uint32(unix.EAGAIN)
			}
			output = nil
		}
		header := make([]byte, 9)
		header[0] = 'D'
		binary.BigEndian.PutUint32(header[1:], uint32(4+len(output)))
		binary.BigEndian.PutUint32(header[5:], status)
		if _, err = d.native.Write(append(header, output...)); err != nil {
			return
		}
	}
}
func (d *restartChild) exchange(session int, op string, payload any, reclaim *guestbridge.Ack) (uint64, []byte) {
	d.t.Helper()
	d.cells[session]++
	cell := d.cells[session]
	scope := d.rig.peers[session].scope
	protocol := guestbridge.TerminalProtocol()
	control := !restartOrdinary(op)
	if reclaim == nil && d.autoReclaim {
		var chosen uint64
		for id := range d.acks[session] {
			if d.classes[session][id] == control && !d.keep[fmt.Sprintf("%d/%d", session, id)] && (chosen == 0 || id < chosen) {
				chosen = id
			}
		}
		if chosen != 0 {
			ack := d.acks[session][chosen]
			reclaim = &ack
		}
	}
	raw := mustRuntimeJSON(d.t, guestbridge.Command{Cell: cell, Context: scope, Protocol: &protocol, Op: op, Payload: mustRuntimeJSON(d.t, payload), Reclaim: reclaim})
	d.originals[cell+uint64(session)*guestbridge.MaxCell] = bytes.Clone(raw)
	deadline := time.Now().Add(10 * time.Second)
	for {
		require(d.t, d.writePacket('S', append([]byte{byte(session)}, raw...)))
		status, _, err := d.readResult()
		require(d.t, err)
		if status == 0 {
			break
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("actual C SEND %s status=%d", op, status)
		}
		time.Sleep(time.Millisecond)
	}
	response := d.fetch(session, cell)
	if reclaim != nil {
		if op == "SESSION_RETIRE" {
			control = d.classes[session][reclaim.Cell]
		}
		delete(d.acks[session], reclaim.Cell)
		delete(d.classes[session], reclaim.Cell)
	}
	if op == "SESSION_RETIRE" {
		d.acks[session] = map[uint64]guestbridge.Ack{}
		d.classes[session] = map[uint64]bool{}
	}
	d.acks[session][cell] = guestbridge.Ack{Cell: cell, ResponseHash: hashBytes(response)}
	d.classes[session][cell] = control
	d.replies[session][cell] = bytes.Clone(response)
	return cell, response
}
func (d *restartChild) fetch(session int, cell uint64) []byte {
	d.t.Helper()
	request := make([]byte, 9)
	request[0] = byte(session)
	binary.BigEndian.PutUint64(request[1:], cell)
	deadline := time.Now().Add(10 * time.Second)
	for {
		require(d.t, d.writePacket('F', request))
		status, response, err := d.readResult()
		require(d.t, err)
		if status == 0 {
			return response
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("actual C FETCH %d status=%d", cell, status)
		}
		time.Sleep(time.Millisecond)
	}
}
func (d *restartChild) hello() {
	for i := range d.cells {
		d.exchange(i, "HELLO", guestbridge.TerminalProtocol(), nil)
	}
}
func (d *restartChild) root(session int) (guestbridge.Call, json.RawMessage) {
	return d.ordinary(session, "ROOT", map[string]any{})
}
func (d *restartChild) ordinary(session int, opcode string, args any) (guestbridge.Call, json.RawMessage) {
	d.t.Helper()
	d.claims[session]++
	intent := guestbridge.Intent{Opcode: opcode, Args: mustRuntimeJSON(d.t, args)}
	claim := guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: d.claims[session], OriginalHash: hashBytes(mustRuntimeJSON(d.t, intent))}
	call := guestbridge.Call{Claim: claim, Credentials: d.rig.peers[session].credentials, Intent: intent}
	d.exchange(session, "ALLOCATE", call, nil)
	cell, _ := d.exchange(session, "CALL", call, nil)
	d.lastCall[session] = cell
	_, wire := d.exchange(session, "QUERY", map[string]any{"claim": claim}, nil)
	var envelope struct {
		Payload struct{ Result json.RawMessage }
	}
	require(d.t, json.Unmarshal(wire, &envelope))
	var result struct {
		Code        string
		Publication json.RawMessage
	}
	require(d.t, json.Unmarshal(envelope.Payload.Result, &result))
	if result.Code != "OK" || string(result.Publication) != "null" {
		d.t.Fatalf("actual %s result %s", opcode, envelope.Payload.Result)
	}
	return call, envelope.Payload.Result
}
func restartOrdinary(op string) bool {
	switch op {
	case "HELLO", "ALLOCATE", "CALL", "VIEW_ACQUIRE", "DIRTY_RESERVE":
		return true
	}
	return false
}
func restartPayload(t *testing.T, wire []byte) json.RawMessage {
	t.Helper()
	var response struct{ Payload json.RawMessage }
	require(t, json.Unmarshal(wire, &response))
	return response.Payload
}
func cleanupRestored(t *testing.T, a *RestoredEndpoint) {
	t.Helper()
	t.Cleanup(func() {
		if a == nil {
			return
		}
		if a.Transport != nil {
			a.Transport.Close()
		}
		if a.Controller != nil {
			_ = a.Controller.Close()
		}
		for _, p := range a.Providers {
			if p != nil {
				_ = p.Close()
			}
		}
		if a.Service != nil {
			_ = a.Service.Close()
		}
	})
}

func (d *restartChild) install(a *RestoredEndpoint) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.transport = a.Transport
	for _, peer := range d.rig.peers {
		peer.bridge = a.Bridge
		peer.mappingTransport = a.Transport
		peer.provider = a.Providers[peer.scope.Session]
		peer.controller = a.Controller
		peer.facts = a.Facts
		peer.producer = a.Producer
		peer.service = a.Service
	}
}

func TestBarrieredEndpointRestartRealC(t *testing.T) {
	binary := buildRestartChild(t)
	t.Run("current-source-original-capabilities-and-lost-reply", func(t *testing.T) {
		d := startRestartChild(t, binary)
		d.hello()
		d.mu.Lock()
		d.loseCallReply = true
		d.mu.Unlock()
		call, result := d.root(0)
		var root struct{ Capabilities []guestbridge.Capability }
		require(t, json.Unmarshal(result, &root))
		if len(root.Capabilities) != 2 {
			t.Fatal("original ROOT and DOMAIN identities absent")
		}
		family := d.rig.facts.enrollment.TicketFamilies[0].Acquisition
		request := map[string]any{"request": guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: 1, Credentials: d.rig.credentials}, "domain": root.Capabilities[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}
		leaseCell, sourceResponse := d.exchange(0, "VIEW_ACQUIRE", request, nil)
		if !bytes.Contains(sourceResponse, []byte(`"kind":"SOURCE"`)) {
			t.Fatal("actual nonempty retained source receipt absent")
		}
		oldController := d.rig.supervisor.record.Controller
		oldService := d.rig.service
		oldProvider := d.rig.provider
		oldViews := d.rig.controller
		restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
		require(t, err)
		originalIndex := restart.funding.State()
		assembly, err := restart.Reopen(t.Context())
		require(t, err)
		t.Cleanup(func() {
			assembly.Transport.Close()
			_ = assembly.Controller.Close()
			for _, p := range assembly.Providers {
				_ = p.Close()
			}
			_ = assembly.Service.Close()
		})
		if assembly.Service == oldService || assembly.Providers[d.rig.scope.Session] == oldProvider || assembly.Controller == oldViews || d.rig.supervisor.record.Controller != oldController {
			t.Fatal("semantic owners not reopened or actual controller changed")
		}
		d.install(assembly)
		// Native routing/INIT is the explicitly substituted OS boundary. The real C
		// same-owner carrier resumes through its actual inherited management channel.
		require(t, restart.resumeExporter(t.Context()))
		if replay := d.fetch(0, leaseCell); !bytes.Equal(replay, sourceResponse) {
			t.Fatal("source/full signature bytes changed across complete reopen")
		}
		_, wire := d.exchange(0, "QUERY", map[string]any{"claim": call.Claim}, nil)
		var query struct {
			Payload struct{ Result json.RawMessage }
		}
		require(t, json.Unmarshal(wire, &query))
		if !bytes.Equal(query.Payload.Result, result) {
			t.Fatal("protected original result/capabilities replanned")
		}
		d.mu.Lock()
		lost := d.lost
		d.mu.Unlock()
		if !lost {
			t.Fatal("actual native accepted-write reply was not lost")
		}
		_, otherResult := d.root(1)
		if len(otherResult) == 0 {
			t.Fatal("second original scope failed after restart")
		}
		assembly.Transport.Close()
		after, err := guestbridge.InspectTransport(restart.funding.Root(), nil)
		require(t, err)
		if after.Controller != originalIndex.Controller || after.High <= originalIndex.High || after.Ordinary < originalIndex.Ordinary {
			t.Fatal("allocator/class/controller reset on component restart")
		}
	})
}

// This drives the actual host controller and C transport through a clean recall;
// the pinned read-only observer is the declared running-kernel/MM primitive.
func (d *restartChild) releaseAndRetireRead(lease guestbridge.ViewLease, acquireID string, retire ...bool) {
	t := d.t
	family := d.rig.facts.enrollment.TicketFamilies[0]
	ticket := guestbridge.Ticket{Anchor: family.Control.Anchor, Nonce: family.Control.Nonce, Number: 1, Credentials: d.rig.credentials}
	d.exchange(0, "VIEW_RELEASE", map[string]any{"request": ticket, "lease": lease}, nil)
	for _, phase := range []string{"STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		_, wire := d.exchange(0, "VIEW_WAIT", map[string]any{"domain": lease.Domain, "after_sequence": 0}, nil)
		var poll struct{ Event map[string]json.RawMessage }
		require(t, json.Unmarshal(restartPayload(t, wire), &poll))
		if len(poll.Event) == 0 {
			t.Fatalf("missing %s event %s", phase, wire)
		}
		ack := map[string]any{"domain": lease.Domain, "sequence": poll.Event["sequence"], "recall": poll.Event["recall"], "phase": phase, "authority_hash": poll.Event["authority_hash"], "closure_hash": poll.Event["closure_hash"], "inventory_hash": poll.Event["inventory_hash"], "session": d.rig.scope.Session, "manifest": nil, "target": poll.Event["target"]}
		if phase == "STOPPED" || phase == "DRAINED" {
			ack["manifest"] = map[string]any{"entries": []any{}, "hash": hashBytes([]byte("[]"))}
		}
		revision := uint64(1)
		if phase == "REOPENED" {
			revision = 2
		}
		d.rig.observeACK(poll.Event, ack, revision)
		d.exchange(0, "VIEW_ACK", ack, nil)
	}
	_, wire := d.exchange(0, "VIEW_QUERY", map[string]any{"request": ticket}, nil)
	var release struct{ ID, Status string }
	require(t, json.Unmarshal(restartPayload(t, wire), &release))
	if release.Status != "RELEASED" {
		t.Fatalf("release remained %s", wire)
	}
	if len(retire) == 0 || retire[0] {
		for _, row := range []struct {
			family viewcontrol.TicketFamily
			id     string
		}{{family.Acquisition, acquireID}, {family.Control, release.ID}} {
			_, wire = d.exchange(0, "VIEW_RETIRE", map[string]any{"anchor": row.family.Anchor, "nonce": row.family.Nonce, "through": 1, "result_hashes": []string{row.id}, "credentials": d.rig.credentials}, nil)
			if !bytes.Contains(wire, []byte(`"status":"RETIRED"`)) {
				t.Fatalf("actual view floor failed %s", wire)
			}
		}
	}
	// Same admitted boot/provider after its completed reopen. No host event or
	// source authority is created by this observation alone.
	d.rig.installed.Admission.Observations[0] = kernelDomainObservation{Scope: d.rig.scope, Revision: 2, Fence: 2, Phase: "LIVE"}
	require(t, os.WriteFile(d.rig.observationFile, mustRuntimeJSON(t, d.rig.installed), 0600))
}

func TestBarrieredMutatingEndpointRestartRealC(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.autoReclaim = true
	d.hello()
	_, rootRaw := d.root(0)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(rootRaw, &root))
	_, created := d.ordinary(0, "CREATE", map[string]any{"parent": root.Capabilities[0], "name": "726573746172742d7061796c6f6164", "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	var file struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(created, &file))
	if len(file.Capabilities) != 2 {
		t.Fatal("actual CREATE did not issue path and description")
	}
	payload := bytes.Repeat([]byte("exact original mutation bytes\x00"), 16)
	d.mu.Lock()
	d.loseCallReply = true
	d.mu.Unlock()
	// Keep the actual WRITE transport cell for post-restart exact replay; normal
	// same-class original/native reclaim bounds all other live carrier entries.
	writeCell := d.cells[0] + 2
	d.keep[fmt.Sprintf("0/%d", writeCell)] = true
	writeCall, writeResult := d.ordinary(0, "WRITE", map[string]any{"description": file.Capabilities[1], "offset": 0, "data": hex.EncodeToString(payload), "max_end": len(payload)})
	writeResponse := bytes.Clone(d.replies[0][writeCell])
	d.ordinary(0, "DESCRIPTION_CLOSE", map[string]any{"description": file.Capabilities[1], "process": d.rig.credentials.Process, "close_sequence": 1})
	d.ordinary(0, "DESCRIPTION_RELEASE", map[string]any{"description": file.Capabilities[1], "release_sequence": 1})
	d.ordinary(0, "FORGET", map[string]any{"object": file.Capabilities[0], "count": 1})
	family := d.rig.facts.enrollment.TicketFamilies[0].Acquisition
	acquire := func(number uint64) (uint64, []byte) {
		return d.exchange(0, "VIEW_ACQUIRE", map[string]any{"request": guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: number, Credentials: d.rig.credentials}, "domain": root.Capabilities[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}, nil)
	}
	_, acquired := acquire(1)
	var first struct {
		ID    string
		Lease guestbridge.ViewLease
	}
	require(t, json.Unmarshal(restartPayload(t, acquired), &first))
	d.releaseAndRetireRead(first.Lease, first.ID)
	sourceCell := d.cells[0] + 1
	d.keep[fmt.Sprintf("0/%d", sourceCell)] = true
	_, sourceResponse := acquire(2)
	if !bytes.Contains(sourceResponse, []byte(`"kind":"SOURCE"`)) {
		t.Fatal("nonempty payload source receipt absent")
	}
	before := sourceJournalBytes(t, d.rig)
	restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
	require(t, err)
	assembly, err := restart.Reopen(t.Context())
	cleanupRestored(t, assembly)
	require(t, err)
	d.install(assembly)
	require(t, restart.resumeExporter(t.Context()))
	if got := d.fetch(0, writeCell); !bytes.Equal(got, writeResponse) {
		t.Fatal("actual WRITE full reply changed on restart")
	}
	if got := d.fetch(0, sourceCell); !bytes.Equal(got, sourceResponse) {
		t.Fatal("current nonempty source identity/signature changed")
	}
	_, queried := d.exchange(0, "QUERY", map[string]any{"claim": writeCall.Claim}, nil)
	var result struct{ Result json.RawMessage }
	require(t, json.Unmarshal(restartPayload(t, queried), &result))
	if !bytes.Equal(result.Result, writeResult) {
		t.Fatal("protected mutating result replanned")
	}
	if after := sourceJournalBytes(t, d.rig); !bytes.Equal(before, after) {
		t.Fatal("reopen/query appended a second mutation/effect")
	}
	object := fsapi.ObjectID{Home: parseRuntimeUUID(file.Capabilities[0].Home), Allocation: parseRuntimeUUID(file.Capabilities[0].Allocation)}
	require(t, assembly.Service.Controlled(t.Context(), d.rig.provider.Enrollment().Profile, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		source, err := cut.ViewSource(ctx, 64<<10)
		if err != nil {
			return err
		}
		if !bytes.Contains(source.State, payload) {
			return fsapi.Stale
		}
		if !object.Valid() {
			return fsapi.Stale
		}
		return nil
	}))
	d.mu.Lock()
	lost := d.lost
	d.mu.Unlock()
	if !lost {
		t.Fatal("mutating accepted-write reply not actually lost")
	}
}

func sourceJournalBytes(t *testing.T, r *sourceRig) []byte {
	t.Helper()
	enrollment := r.provider.Enrollment()
	reader, err := segment.OpenReader(t.Context(), r.source, enrollment.Journal.Stream, segment.RecoveryOptions{}, fsapi.Position{Volume: enrollment.Journal.Stream.Volume, Generation: enrollment.Journal.Stream.Generation, Configuration: enrollment.Journal.Stream.Configuration, Epoch: enrollment.Journal.Stream.Epoch, Sequence: 1})
	require(t, err)
	defer func() { _ = reader.Close() }()
	var out []byte
	for {
		entry, err := reader.Next(t.Context())
		if err == io.EOF {
			break
		}
		require(t, err)
		out = append(out, entry.Bytes...)
	}
	return out
}

func prepareTerminalRestart(t *testing.T, d *restartChild) []byte {
	t.Helper()
	d.hello()
	d.exchange(0, "HELLO", guestbridge.TerminalProtocol(), nil)
	d.exchange(0, "RESUME", map[string]any{"previous_session": d.rig.scope.Session, "ledger_hash": hashBytes([]byte("unsupported guest-wire resume")), "claims_hash": hashBytes([]byte("claims")), "domains_hash": hashBytes([]byte("domains"))}, nil)
	donor := d.acks[0][1]
	settled := []guestbridge.Ack{}
	for cell, ack := range d.acks[0] {
		if cell != donor.Cell {
			settled = append(settled, ack)
		}
	}
	slices.SortFunc(settled, func(a, b guestbridge.Ack) int {
		if a.Cell < b.Cell {
			return -1
		}
		if a.Cell > b.Cell {
			return 1
		}
		return 0
	})
	snapshot := guestbridge.TerminalSnapshot{Version: "homelabfs-guest-terminal/1", Scope: d.rig.scope, Controller: d.rig.supervisor.record.Controller.String(), RetirementID: testUUID(222).String(), Revision: 2, GuestThrough: d.cells[0], Donor: &donor, SettledCells: settled, Credentials: d.rig.credentials, Phase: "TERMINAL_PENDING"}
	observed := d.rig.installed
	observed.Admission.Observations = append([]kernelDomainObservation(nil), observed.Admission.Observations...)
	observed.Admission.Observations[0] = kernelDomainObservation{Scope: d.rig.scope, Revision: 2, Fence: 1, Phase: "TERMINAL_PENDING", Terminal: &snapshot}
	require(t, os.WriteFile(d.rig.observationFile, mustRuntimeJSON(t, observed), 0600))
	request := guestbridge.TerminalRequest{RetirementID: snapshot.RetirementID, Credentials: snapshot.Credentials, GuestThrough: snapshot.GuestThrough, ObserverRevision: snapshot.Revision, ClosureHash: hashBytes(mustRuntimeJSON(t, snapshot)), SettledCells: settled}
	_, response := d.exchange(0, "SESSION_RETIRE", request, &donor)
	if !bytes.Contains(response, []byte(`"kind":"SESSION_RETIRED"`)) {
		t.Fatalf("actual terminal handoff missing %s", response)
	}
	return response
}
func TestBarrieredTerminalEndpointRestartRealC(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	response := prepareTerminalRestart(t, d)
	terminalGuest := d.cells[0]
	restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
	require(t, err)
	before := restart.funding.State()
	terminalNative := before.Terminal[d.rig.scope.Session]
	if terminalNative == 0 || before.Ordinary != 2 || before.Control != 0 || len(before.Cells) != 2 {
		t.Fatalf("mixed old manifest did not transfer exact final class: %+v", before)
	}
	var original guestbridge.ProtectedTransportCell
	_, err = guestbridge.InspectTransport(restart.funding.Root(), func(c guestbridge.ProtectedTransportCell) error {
		if c.Header.Cell == terminalNative {
			original = c
		}
		return nil
	})
	require(t, err)
	if original.Terminal == nil || len(original.Terminal.Donors) != 2 || original.Terminal.OldOrdinary != 2 || original.Terminal.OldControl != 1 || original.Terminal.Phase != "TERMINAL_RESPONDED" {
		t.Fatal("complete mixed terminal cut not retained")
	}
	assembly, err := restart.Reopen(t.Context())
	cleanupRestored(t, assembly)
	require(t, err)
	d.install(assembly)
	require(t, restart.resumeExporter(t.Context()))
	if replay := d.fetch(0, terminalGuest); !bytes.Equal(replay, response) {
		t.Fatal("permanent original terminal guest reply changed")
	}
	raw, err := assembly.Transport.Read(t.Context(), d.rig.native, d.rig.native.Endpoint.ResultNode, terminalNative*guestbridge.CellStride, uint32(len(original.Wire[0])))
	require(t, err)
	if !bytes.Equal(raw, original.Wire[0]) {
		t.Fatal("permanent native final cell bytes changed")
	}
	// Retired scope cannot issue a new header through the original live C owner.
	next := mustRuntimeJSON(t, guestbridge.Command{Cell: terminalGuest + 1, Context: d.rig.scope, Protocol: ptrTerminalProtocol(), Op: "HELLO", Payload: mustRuntimeJSON(t, guestbridge.TerminalProtocol())})
	require(t, d.writePacket('S', append([]byte{0}, next...)))
	status, _, err := d.readResult()
	require(t, err)
	if status == 0 {
		t.Fatal("permanently retired scope reopened")
	}
	d.root(1) // Other enrolled scope and original global allocator remain usable.
	assembly.Transport.Close()
	after, err := guestbridge.InspectTransport(restart.funding.Root(), func(c guestbridge.ProtectedTransportCell) error {
		if c.Header.Cell == terminalNative && (!bytes.Equal(c.Response, original.Response) || !reflect.DeepEqual(c.Terminal, original.Terminal) || !reflect.DeepEqual(c.Mapping, original.Mapping)) {
			return fsapi.Stale
		}
		return nil
	})
	require(t, err)
	if after.Controller != before.Controller || after.Terminal[d.rig.scope.Session] != terminalNative || after.High <= before.High || !slices.Contains(after.Cells, terminalNative) {
		t.Fatal("terminal native ownership/fence/counter lost")
	}
}
func ptrTerminalProtocol() *guestbridge.Protocol {
	protocol := guestbridge.TerminalProtocol()
	return &protocol
}

func ownedSemanticFiles(t *testing.T, d *restartChild, name string) []string {
	t.Helper()
	root := filepath.Dir(filepath.Dir(d.rig.mappingDirectory))
	paths := []string{}
	require(t, filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == name {
			paths = append(paths, file)
		}
		return nil
	}))
	slices.Sort(paths)
	if len(paths) == 0 {
		t.Fatalf("missing actual test ledger %s", name)
	}
	return paths
}
func TestBarrieredEndpointRestartRefusesIncompleteOwners(t *testing.T) {
	binary := buildRestartChild(t)
	for _, fault := range []string{"provider", "controller", "transport", "stale-controller", "unexcluded-domain", "changed-catalog", "changed-C-file", "changed-physical-domain", "replacement-connection", "copied-provider", "copied-controller", "expired-grant"} {
		t.Run(fault, func(t *testing.T) {
			d := startRestartChild(t, binary)
			d.hello()
			d.root(0)
			restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
			require(t, err)
			controller := d.rig.supervisor.record.Controller
			switch fault {
			case "provider", "controller", "transport":
				file := restart.funding.Root() + "/transport.db"
				if fault == "provider" {
					file = ownedSemanticFiles(t, d, "controlled-validation-v1.db")[0]
				}
				if fault == "controller" {
					file = ownedSemanticFiles(t, d, "viewcontrol-v1.db")[0]
				}
				require(t, os.Rename(file, file+".missing"))

			case "copied-provider", "copied-controller":
				name := "controlled-validation-v1.db"
				if fault == "copied-controller" {
					name = "viewcontrol-v1.db"
				}
				file := ownedSemanticFiles(t, d, name)[0]
				raw, e := os.ReadFile(file)
				require(t, e)
				require(t, os.Rename(file, file+".original"))
				require(t, os.WriteFile(file, raw, 0600))
			case "expired-grant":
				restart.expires = time.Now().Add(-time.Second)
			case "stale-controller":
				mutateTransportDB(t, restart, func(index *guestbridge.ProtectedTransportState) { index.Controller = testUUID(230) }, nil)
			case "unexcluded-domain":
				observed := d.rig.installed
				observed.Admission.Domains = observed.Admission.Domains[:1]
				observed.Admission.Observations = observed.Admission.Observations[:1]
				delete(observed.Admission.Protocols, d.rig.peers[1].scope.Session)
				require(t, os.WriteFile(d.rig.observationFile, mustRuntimeJSON(t, observed), 0600))
			case "replacement-connection":
				endpoint := d.rig.native.Endpoint
				endpoint.FuseSession = testUUID(233).String()
				d.rig.supervisor.mount.endpointObservation = func() (guestbridge.Endpoint, error) { return endpoint, nil }
			case "changed-physical-domain":
				observed := d.rig.installed
				observed.Admission.Observations = append([]kernelDomainObservation(nil), observed.Admission.Observations...)
				observed.Admission.Observations[1] = kernelDomainObservation{Scope: d.rig.peers[1].scope, Revision: 1, Fence: 1, Phase: "STOPPED", EventHash: hashBytes([]byte("unissued event")), AcknowledgementHash: hashBytes([]byte("unexcluded old physical owner"))}
				require(t, os.WriteFile(d.rig.observationFile, mustRuntimeJSON(t, observed), 0600))
			case "changed-catalog":
				root := filepath.Dir(filepath.Dir(d.rig.mappingDirectory))
				changed := false
				require(t, filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !changed && !entry.IsDir() && strings.HasPrefix(entry.Name(), "image-") {
						f, err := os.OpenFile(file, os.O_WRONLY, 0)
						if err != nil {
							return err
						}
						_, err = f.WriteAt([]byte("!"), 0)
						closeErr := f.Close()
						if err != nil {
							return err
						}
						changed = true
						return closeErr
					}
					return nil
				}))
				if !changed {
					t.Fatal("no actual applied catalog mutated")
				}
			case "changed-C-file":
				file := d.rig.supervisor.child.command.Args[2] + "/forwarding"
				// The original same live exporter must recheck its actual protected file
				// before both cut binding and release, not accept a stale copied witness.
				require(t, os.Rename(file, file+".missing"))
			}
			a, err := restart.Reopen(t.Context())
			cleanupRestored(t, a)
			if fault == "changed-C-file" && err == nil {
				d.install(a)
				err = restart.resumeExporter(t.Context())
			}
			if err == nil {
				t.Fatal("partial/missing/stale owner restored")
			}
			if d.rig.supervisor.record.Controller != controller {
				t.Fatal("failure changed controller incarnation")
			}
		})
	}
}
func mutateTransportDB(t *testing.T, r *Restart, indexEdit func(*guestbridge.ProtectedTransportState), cellEdit func(*guestbridge.ProtectedTransportCell)) {
	t.Helper()
	db, err := bolt.Open(r.funding.Root()+"/transport.db", 0600, &bolt.Options{Timeout: time.Millisecond})
	require(t, err)
	require(t, db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte("hfs-transport-1"))
		var index guestbridge.ProtectedTransportState
		if err := json.Unmarshal(bucket.Get([]byte("index")), &index); err != nil {
			return err
		}
		if indexEdit != nil {
			indexEdit(&index)
			raw, err := json.Marshal(index)
			if err != nil {
				return err
			}
			if err = bucket.Put([]byte("index"), raw); err != nil {
				return err
			}
		}
		if cellEdit != nil {
			for _, id := range index.Cells {
				key := make([]byte, 9)
				key[0] = 'c'
				binary.BigEndian.PutUint64(key[1:], id)
				var cell guestbridge.ProtectedTransportCell
				if err := json.Unmarshal(bucket.Get(key), &cell); err != nil {
					return err
				}
				cellEdit(&cell)
				raw, err := json.Marshal(cell)
				if err != nil {
					return err
				}
				if err = bucket.Put(key, raw); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	require(t, db.Close())
}

type forgedRestoreNative struct{ calls int }

func (*forgedRestoreNative) Enroll(context.Context, guestbridge.Endpoint, fsapi.UUID) error {
	return nil
}
func (*forgedRestoreNative) Verify(context.Context, guestbridge.Native, fsapi.UUID) error { return nil }
func (f *forgedRestoreNative) RestoreEndpoint(context.Context, guestbridge.ProtectedTransportState, guestbridge.Config) error {
	f.calls++
	return nil
}

func TestBarrieredEndpointRejectsCopiedCutAndVerifier(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.hello()
	d.root(0)
	restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
	require(t, err)
	original := restart.funding.State()
	changed := restart.funding.State()
	changed.Scopes[0].Session = testUUID(231).String()
	changed.GuestHigh[d.rig.scope.Session] = 999
	if restart.funding.State().Scopes[0] != original.Scopes[0] || restart.funding.State().GuestHigh[d.rig.scope.Session] != original.GuestHigh[d.rig.scope.Session] {
		t.Fatal("public inert state aliased private funding cut")
	}
	copied := filepath.Join(d.rig.temporary(), "copied")
	require(t, os.Mkdir(copied, 0700))
	for _, name := range []string{"owner.lock", "transport.db"} {
		raw, err := os.ReadFile(restart.funding.Root() + "/" + name)
		require(t, err)
		require(t, os.WriteFile(copied+"/"+name, raw, 0600))
	}
	c := restart.bridge
	if _, _, err = guestbridge.RestoreDurableEndpoint(t.Context(), c, copied, restart.funding); err == nil {
		t.Fatal("copied same-controller ledger selected original funding")
	}
	forged := &forgedRestoreNative{}
	c.Native = forged
	if _, _, err = guestbridge.RestoreDurableEndpoint(t.Context(), c, restart.funding.Root(), restart.funding); err == nil || forged.calls != 0 {
		t.Fatal("replacement verifier could authorize its own cut")
	}
	// Both rejections leave the actual one-use authority to the original private
	// runtime coordinator; copied metadata did not consume or replace its owner.
	a, err := restart.Reopen(t.Context())
	cleanupRestored(t, a)
	require(t, err)
	d.install(a)
	require(t, restart.resumeExporter(t.Context()))
}
func TestBarrieredEndpointRefusesUnresolvedNativeWrite(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.hello()
	d.root(0)
	d.claims[0]++
	intent := guestbridge.Intent{Opcode: "ROOT", Args: json.RawMessage(`{}`)}
	call := guestbridge.Call{Claim: guestbridge.Claim{Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), Number: d.claims[0], OriginalHash: hashBytes(mustRuntimeJSON(t, intent))}, Credentials: d.rig.credentials, Intent: intent}
	d.exchange(0, "ALLOCATE", call, nil)
	d.cells[0]++
	raw := mustRuntimeJSON(t, guestbridge.Command{Cell: d.cells[0], Context: d.rig.scope, Protocol: ptrTerminalProtocol(), Op: "CALL", Payload: mustRuntimeJSON(t, call)})
	d.mu.Lock()
	d.loseCallReply = true
	d.mu.Unlock()
	require(t, d.writePacket('S', append([]byte{0}, raw...)))
	status, _, err := d.readResult()
	require(t, err)
	// COPIED is only guest input acceptance. Deliberately never FETCH/retry the
	// actual accepted native write whose acknowledgement was lost.
	if status != 0 {
		t.Fatalf("guest input not accepted: %d", status)
	}
	d.mu.Lock()
	lost := d.lost
	d.mu.Unlock()
	if !lost {
		t.Fatal("native write acknowledgement not actually lost")
	}
	if _, err = d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport); err == nil {
		t.Fatal("unissued/unresponded exporter owner reconstructed from host result alone")
	}
	if !d.rig.supervisor.closed {
		t.Fatal("uncertain barrier left native admission open")
	}
}
func TestBarrieredTerminalRejectsIncompleteManifest(t *testing.T) {
	binary := buildRestartChild(t)
	for _, fault := range []string{"missing-manifest-member", "missing-fence", "old-pending-phase", "class-charge"} {
		t.Run(fault, func(t *testing.T) {
			d := startRestartChild(t, binary)
			prepareTerminalRestart(t, d)
			restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
			require(t, err)
			mutateTransportDB(t, restart, func(index *guestbridge.ProtectedTransportState) {
				if fault == "missing-fence" {
					delete(index.Terminal, d.rig.scope.Session)
				}
			}, func(cell *guestbridge.ProtectedTransportCell) {
				if cell.Terminal == nil {
					return
				}
				switch fault {
				case "missing-manifest-member":
					cell.Terminal.Donors = cell.Terminal.Donors[:1]
					cell.Terminal.Controls = cell.Terminal.Controls[:1]
				case "old-pending-phase":
					cell.Terminal.Phase = "TERMINAL_PENDING"
				case "class-charge":
					cell.Terminal.Controls[0] = !cell.Terminal.Controls[0]
				}
			})
			a, err := restart.Reopen(t.Context())
			cleanupRestored(t, a)
			if err == nil {
				t.Fatal("partial terminal cut restored")
			}
		})
	}
}

func TestBarrieredEndpointRefusesHistoricalSource(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.autoReclaim = true
	d.hello()
	_, rootRaw := d.root(0)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(rootRaw, &root))
	family := d.rig.facts.enrollment.TicketFamilies[0].Acquisition
	_, wire := d.exchange(0, "VIEW_ACQUIRE", map[string]any{"request": guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: 1, Credentials: d.rig.credentials}, "domain": root.Capabilities[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}, nil)
	var acquired struct {
		ID    string
		Lease guestbridge.ViewLease
	}
	require(t, json.Unmarshal(restartPayload(t, wire), &acquired))
	d.releaseAndRetireRead(acquired.Lease, acquired.ID, false)

	before := sourceJournalBytes(t, d.rig)
	_, created := d.ordinary(0, "CREATE", map[string]any{"parent": root.Capabilities[0], "name": "686973746f726963616c", "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	var file struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(created, &file))
	if len(file.Capabilities) != 2 {
		t.Fatal("actual advancing mutation absent")
	}
	d.ordinary(0, "DESCRIPTION_CLOSE", map[string]any{"description": file.Capabilities[1], "process": d.rig.credentials.Process, "close_sequence": 1})
	d.ordinary(0, "DESCRIPTION_RELEASE", map[string]any{"description": file.Capabilities[1], "release_sequence": 1})
	d.ordinary(0, "FORGET", map[string]any{"object": file.Capabilities[0], "count": 1})
	if bytes.Equal(before, sourceJournalBytes(t, d.rig)) {
		t.Fatal("historical test did not advance actual journal")
	}
	restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
	require(t, err)
	assembly, err := restart.Reopen(t.Context())
	cleanupRestored(t, assembly)
	if err == nil {
		t.Fatal("historical source cut restored from newer current bytes")
	}
}

func TestBarrieredReaddir256ActualProjection(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.autoReclaim = true
	d.hello()
	_, wire := d.root(0)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(wire, &root))
	read := func(want int) {
		_, raw := d.ordinary(0, "READDIR", map[string]any{"object": root.Capabilities[0], "after": "", "revision": 0, "limit": 256})
		var result struct{ Entries []json.RawMessage }
		require(t, json.Unmarshal(raw, &result))
		if len(result.Entries) != want {
			t.Fatalf("READDIR256 actual entries %d want%d: %s", len(result.Entries), want, raw)
		}
	}
	read(0)
	d.ordinary(0, "MKDIR", map[string]any{"parent": root.Capabilities[0], "name": "656e747279", "mode": 0770, "umask": 0})
	read(1)
}
