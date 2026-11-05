package retained9plab

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

func testID(n int) fsapi.UUID {
	value, err := fsapi.ParseUUID(fmt.Sprintf("%08x-0000-0000-0000-%012x", n, n))
	if err != nil {
		panic(err)
	}
	return value
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err == nil {
		err = os.WriteFile(path, raw, 0600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func newTestKey(t *testing.T, directory, name string) (ed25519.PrivateKey, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, name+".pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return key, path
}

func fixture(t *testing.T) (string, EnrollmentRequest, ed25519.PrivateKey) {
	t.Helper()
	root := t.TempDir()
	operator, path := newTestKey(t, root, "operator")
	request := EnrollmentRequest{Proposal: filepath.Join(root, "proposal.json"), OperatorKey: path,
		BootID: testID(5).String(), ProviderNonce: testID(16).String(),
		Budget: controlledruntime.GuestArtifactBudget{OrdinaryBytes: 128 << 20, ControlBytes: 128 << 20, Reservations: 32, Cookies: 64, Received: 64}}
	hash := sha256.Sum256([]byte("private source-backed test fixture"))
	h := hex.EncodeToString(hash[:])
	e := viewcontrol.Enrollment{Version: viewcontrol.EnrollmentVersion, ID: testID(100), PageSize: 4096,
		Bounds: viewcontrol.Bounds{WorkBytes: 64 << 20, Tickets: 8, ControlTickets: 8,
			Reservations: 2, Identities: 32, Sources: 4, SourceBytes: 4096, LedgerBytes: 8 << 20},
		BaselineStateHash: h,
		Baseline: record.FrameRef{StreamKind: 1, Position: fsapi.Position{Volume: testID(10),
			Generation: testID(11), Configuration: testID(12), Epoch: 1, Sequence: 1}, SHA256: record.Hash{1}}}
	for i := range 2 {
		host, hostPath := newTestKey(t, root, fmt.Sprintf("host%d", i))
		guest, guestPath := newTestKey(t, root, fmt.Sprintf("guest%d", i))
		scope := guestbridge.Context{AuthorityHash: h, Cluster: testID(1).String(), Configuration: testID(12).String(),
			Export: testID(2).String(), ExporterIncarnation: testID(3).String(), FuseSession: testID(4).String(),
			Generation: testID(11).String(), GuestBoot: "", GuestMount: testID(200 + i).String(),
			OriginIncarnation: testID(6).String(), OriginJournal: testID(7).String(), OriginNode: testID(8).String(),
			PolicyHash: h, RegistryHash: h, Session: testID(300 + i).String(), VM: testID(9).String(), Volume: testID(10).String()}
		protocol := guestbridge.TerminalProtocol()
		e.Domains = append(e.Domains, controlledvalidation.Enrollment{Protocol: &protocol,
			Profile: fsservice.ValidationProfile{Disposable: true, Name: "fresh external operator test"}, Scope: scope,
			Endpoint:          guestbridge.Endpoint{FuseSession: scope.FuseSession, CommandNode: 2, ResultNode: 3, RecallNode: 4},
			ServingGeneration: testID(13), Ledger: testID(400 + i),
			Journal: fsservice.ControlledJournal{Stream: segment.Stream{Volume: testID(10), Generation: testID(11), Configuration: testID(12), Epoch: 1, Kind: 1},
				Owner: testID(8), Incarnation: testID(7)}, HostKey: host.Public().(ed25519.PublicKey), GuestKey: guest.Public().(ed25519.PublicKey)})
		families := viewcontrol.TicketFamilies{Acquisition: viewcontrol.TicketFamily{Anchor: testID(500 + i).String(), Nonce: testID(600 + i).String()},
			Control: viewcontrol.TicketFamily{Anchor: testID(700 + i).String(), Nonce: testID(800 + i).String()}}
		e.TicketFamilies = append(e.TicketFamilies, families)
		request.Keys = append(request.Keys, DomainKeys{Session: scope.Session, Host: hostPath, Guest: guestPath})
		request.Mounts = append(request.Mounts, controlledruntime.GuestArtifactMount{DomainIndex: i, Session: scope.Session,
			DeviceTag: fmt.Sprintf("hfs%d", i), DeviceName: fmt.Sprintf("virtio%d", i), UserNamespace: testID(14).String(),
			IDMapHash: "063ee717c983abc52c8cdeaeaee32c62ecb93c8fc88e6f887313db9a6c471f98", Realm: testID(15).String(),
			Subject: "74657374", Anchor: testID(900 + i).String(), Nonce: testID(1000 + i).String(), ViewControlNonce: families.Control.Nonce})
	}
	writeJSON(t, request.Proposal, e)
	return root, request, operator
}

func TestOperatorExactExternalRoundTrip(t *testing.T) {
	root, request, operator := fixture(t)
	path := filepath.Join(root, "request.json")
	writeJSON(t, path, request)
	output := filepath.Join(root, "enrolled")
	if err := Run([]string{"enroll", "--request", path, "--output", output}); err != nil {
		t.Fatal(err)
	}
	var handoff controlledruntime.RuntimeEnrollment
	if err := readJSON(filepath.Join(output, "enrollment.json"), &handoff); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(handoff.Authorization.Enrollment)
	if err != nil || !ed25519.Verify(operator.Public().(ed25519.PublicKey), raw, handoff.Authorization.Operator) {
		t.Fatal("aggregate operator signature")
	}
	for i, domain := range handoff.Authorization.Enrollment.Domains {
		if domain.Scope.GuestBoot != request.BootID ||
			!ed25519.Verify(domain.HostKey, raw, handoff.Authorization.Domains[i].Host) ||
			!ed25519.Verify(domain.GuestKey, raw, handoff.Authorization.Domains[i].Guest) {
			t.Fatal("original aggregate domain signature/boot")
		}
		original, err := json.Marshal(domain)
		provider := handoff.Providers[i]
		if err != nil || !ed25519.Verify(operator.Public().(ed25519.PublicKey), original, provider.OperatorSignature) ||
			!ed25519.Verify(domain.HostKey, original, provider.HostSignature) ||
			!ed25519.Verify(domain.GuestKey, original, provider.GuestSignature) {
			t.Fatal("actual provider enrollment signatures")
		}
	}
	bind := BindRequest{Provision: filepath.Join(output, "provision.json"), OperatorKey: request.OperatorKey,
		GroupID: handoff.Authorization.Enrollment.ID.String(), Controller: testID(1100).String(), Keys: request.Keys}
	bindPath := filepath.Join(root, "bind-request.json")
	writeJSON(t, bindPath, bind)
	if err := Run([]string{"bind", "--request", bindPath, "--output", filepath.Join(root, "bound")}); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"enroll", "--request", path, "--output", output}); err == nil {
		t.Fatal("existing output reused")
	}
}

func TestOperatorRefusesChangedOrderKeysBootAndInput(t *testing.T) {
	for _, mutation := range []string{"order", "key", "boot", "duplicate", "alias", "symlink", "mode"} {
		t.Run(mutation, func(t *testing.T) {
			root, request, _ := fixture(t)
			path := filepath.Join(root, "request.json")
			switch mutation {
			case "order":
				request.Keys[0], request.Keys[1] = request.Keys[1], request.Keys[0]
			case "key":
				_, request.Keys[0].Guest = newTestKey(t, root, "foreign")
			case "boot":
				var proposal viewcontrol.Enrollment
				if err := readJSON(request.Proposal, &proposal); err != nil {
					t.Fatal(err)
				}
				proposal.Domains[1].Scope.GuestBoot = testID(55).String()
				writeJSON(t, request.Proposal, proposal)
			}
			writeJSON(t, path, request)
			if mutation == "duplicate" || mutation == "alias" {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if mutation == "duplicate" {
					raw = append([]byte(`{"Proposal":"duplicate",`), raw[1:]...)
				} else {
					raw = bytes.Replace(raw, []byte(`"Proposal"`), []byte(`"proposal"`), 1)
				}
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "mode" {
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "symlink" {
				link := filepath.Join(root, "link.json")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			}
			if err := Run([]string{"enroll", "--request", path, "--output", filepath.Join(root, "output")}); err == nil {
				t.Fatal("invalid administrative handoff accepted")
			}
		})
	}
}
