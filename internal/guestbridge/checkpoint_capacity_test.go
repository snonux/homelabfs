package guestbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/snonux/homelabfs/internal/fsapi"
	bolt "go.etcd.io/bbolt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This isolates the actual admission and snapshot owner. Full terminal authority
// and paired C/native ownership are exercised separately by controlledruntime.
func TestCheckpointOrdinaryPressurePreservesClosure(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	original := f.cred()
	b := Binding{Context: f.c, Call: Call{Credentials: original}}
	_, err := f.s.issue(b, fsapi.Attr{Object: f.root}, "DESCRIPTION", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	cred := original
	cred.Groups = make([]uint32, 1024)
	for i := range cred.Groups {
		cred.Groups[i] = math.MaxUint32
	}
	tr, err := NewDurableTransport(f.s, durableBudget(t), filepath.Join(t.TempDir(), "pressure"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tr.Close)
	admitted := 0
	for i := 1; i < 4096; i++ {
		cred.Process = fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		err = f.s.credentials(context.Background(), f.c, cred, false)
		if err == fsapi.NoSpace {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		admitted++
	}
	if admitted < 100 || admitted >= 4095 {
		t.Fatalf("pressure did not reach checkpoint limit: %d", admitted)
	}
	ss := f.s.sessions[f.c.Session]
	count := len(ss.Credentials)
	if err = f.s.credentials(context.Background(), f.c, cred, false); err != fsapi.NoSpace || len(ss.Credentials) != count {
		t.Fatalf("rejected state retained: %v", err)
	}
	// Every admitted process may close the description and exit at the largest
	// exact counter. This spends reserved bytes without admitting a new identity.
	for key := range ss.Credentials {
		process := key[:36]
		f.s.finishLifetime(Binding{Context: f.c, Call: Call{Intent: Intent{Opcode: "PROCESS_EXIT"}}}, arguments{"process": mustJSON(t, process), "exit_sequence": mustJSON(t, uint64(math.MaxUint64))})
		for token, g := range ss.Grants {
			if g.Cap.Kind == "DESCRIPTION" {
				ss.CloseSequences[token+":"+process] = math.MaxUint64
				g.Sequence = math.MaxUint64
				g.Released = true
			}
		}
	}
	if err = f.s.credentials(context.Background(), f.c, original, true); err != nil {
		t.Fatalf("original cleanup credentials lost: %v", err)
	}
	// The already-fenced terminal may spend its separately reserved maximum-size
	// credential. Ordinary pressure could not consume those bytes.
	cred.Process = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if err = f.s.admitCredentials(context.Background(), f.c, cred, true, true); err != nil {
		t.Fatalf("terminal credential reserve lost: %v", err)
	}
	tr.mu.Lock()
	err = tr.protectLocked()
	failed := tr.failed
	tr.mu.Unlock()
	if err != nil || failed != nil {
		t.Fatalf("actual C8 cleanup/terminal credential protection fenced: %v / %v", err, failed)
	}
	ss.Terminal = &TerminalSeal{Version: "terminal/v1", Owner: "BRIDGE", Scope: f.c, Endpoint: f.native.Endpoint, Controller: f.s.controller.String(), RetirementID: cred.Process, GuestCommandHash: digest(nil), ClosureHash: digest(nil), StateHash: digest(nil)}
	rows, err := f.s.protectedSessions()
	if err != nil {
		t.Fatalf("funded closure snapshot failed: %v", err)
	}
	total := 0
	for _, row := range rows {
		total += len(row)
	}
	if total > bridgeSessionLimit {
		t.Fatal(strconv.Itoa(total))
	}
}

func TestTerminalCheckpointReservesCredentialIdentity(t *testing.T) {
	f := newBridge(t)
	p := TerminalProtocol()
	f.s.config.Protocol = &p
	f.s.config.MaxCredentialRevisions = 2
	original := f.cred()
	if err := f.s.credentials(t.Context(), f.c, original, false); err != nil {
		t.Fatal(err)
	}
	other := original
	other.Process = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if err := f.s.credentials(t.Context(), f.c, other, false); err != fsapi.NoSpace {
		t.Fatalf("ordinary stole terminal identity: %v", err)
	}
	if err := f.s.admitCredentials(t.Context(), f.c, other, true, true); err != nil {
		t.Fatalf("reserved terminal identity refused: %v", err)
	}
	if err := f.s.credentials(t.Context(), f.c, original, true); err != nil {
		t.Fatalf("original cleanup refused: %v", err)
	}
}

func TestBarrieredCutCapacityBeforeDecodedRetention(t *testing.T) {
	_, tr, root := durableFixture(t)
	// Actual protected bbolt values exceed only the NEW barrier workspace. The
	// early byte-count check must refuse before trying to decode arbitrary rows.
	raw := bytes.Repeat([]byte("x"), transportCellLimit)
	err := tr.ledger.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(transportBucket)
		for i := 0; i < 3; i++ {
			if err := b.Put([]byte(fmt.Sprintf("oversized-%d", i)), raw); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = checkRestorationCapacity(tr.ledger); err != fsapi.NoSpace {
		t.Fatalf("retained cut byte ceiling ignored: %v", err)
	}
	tr.Close()
	visited := false
	_, err = InspectRestorationEndpoint(root, func(ProtectedTransportCell) error { visited = true; return nil }, nil)
	if err != fsapi.NoSpace || visited {
		t.Fatalf("decoded/retained before capacity refusal: %v/%v", err, visited)
	}
}

func TestCheckpointTerminalSealReserveBounds(t *testing.T) {
	f := newBridge(t)
	id := fsapi.UUID{}
	for i := range id {
		id[i] = 255
	}
	token := strings.Repeat("f", 64)
	seal := TerminalSeal{Version: "homelabfs-terminal-owner/1", Owner: "BRIDGE", Scope: f.c, Endpoint: Endpoint{FuseSession: id.String(), CommandNode: math.MaxUint64, ResultNode: math.MaxUint64, RecallNode: math.MaxUint64}, Controller: id.String(), RetirementID: id.String(), GuestCommandHash: token, ClosureHash: token, StateHash: token}
	raw, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)+20 > 2048 {
		t.Fatalf("terminal seal reserve undercounts: %d", len(raw))
	}
}
