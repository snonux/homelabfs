//go:build linux

package runtimeprep

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/retained9plab"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// requestFixture is one generated bundle plus a proposal shaped like the one
// the runtime writes (actual identities replaced by fresh test UUIDs: the
// runtime/native state is the declared boundary here).
type requestFixture struct {
	root, output string
	template     RequestTemplate
	proposal     viewcontrol.Enrollment
	inputs       EnrollmentInputs
}

func newRequestFixture(t *testing.T) requestFixture {
	t.Helper()
	root := shortRoot(t)
	s := exampleSpec(t)
	operatorFixture(t, root, &s)
	_, output := generateBundle(t, root, s)
	f := requestFixture{root: root, output: output}
	readBundleJSON(t, output, TemplateFile, &f.template)
	var config controlledruntime.RuntimeConfig
	readBundleJSON(t, output, ConfigFile, &config)
	f.proposal = runtimeShapedProposal(t, config)
	f.inputs = EnrollmentInputs{ProposalPath: filepath.Join(root, "proposal.json"), Proposal: f.proposal,
		BootID: testUUID(t), ProviderNonce: testUUID(t), DeviceNames: []string{"virtio2", "virtio3"},
		Observer: ObserverInput{Version: ObserverVersion, SSH: "/usr/bin/ssh", KnownHosts: "/var/tmp/run/rocky-known_hosts",
			Identity: "/var/tmp/run/observer/id_ed25519", Host: "10.20.0.123",
			SSHSHA256: strings.Repeat("a1", 32), KnownHostsSHA256: strings.Repeat("b2", 32), ManifestSHA256: strings.Repeat("c3", 32)}}
	writePrivateJSON(t, f.inputs.ProposalPath, f.proposal)
	writePrivateJSON(t, filepath.Join(root, "observer.json"), f.inputs.Observer)
	return f
}

func testUUID(t *testing.T) string {
	t.Helper()
	id, err := freshUUID(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testID(t *testing.T) fsapi.UUID {
	t.Helper()
	id, err := fsapi.ParseUUID(testUUID(t))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// runtimeShapedProposal mirrors runtimeProposal: config scopes with the
// runtime-owned placeholders filled, the configured host/guest keys and
// fresh ticket families; GuestBoot stays empty.
func runtimeShapedProposal(t *testing.T, c controlledruntime.RuntimeConfig) viewcontrol.Enrollment {
	t.Helper()
	volume, generation, configuration, journal := testID(t), testID(t), testID(t), testID(t)
	e := viewcontrol.Enrollment{Version: viewcontrol.EnrollmentVersion, ID: testID(t), PageSize: 4096, Bounds: c.Views,
		BaselineStateHash: strings.Repeat("d4", 32),
		Baseline: record.FrameRef{StreamKind: 1, Position: fsapi.Position{Volume: volume, Generation: generation,
			Configuration: configuration, Epoch: 1, Sequence: 1}, SHA256: record.Hash{1}}}
	fuse, exporter, serving := testUUID(t), testUUID(t), testID(t)
	for _, template := range c.Scopes {
		scope := template
		scope.Configuration, scope.Generation, scope.OriginJournal = configuration.String(), generation.String(), journal.String()
		scope.ExporterIncarnation, scope.FuseSession = exporter, fuse
		protocol := guestbridge.TerminalProtocol()
		e.Domains = append(e.Domains, controlledvalidation.Enrollment{Protocol: &protocol,
			Profile: fsservice.ValidationProfile{Disposable: true, Name: "runtime prep request fixture"}, Scope: scope,
			Endpoint:          guestbridge.Endpoint{FuseSession: fuse, CommandNode: 2, ResultNode: 3, RecallNode: 4},
			ServingGeneration: serving, Ledger: testID(t),
			Journal: fsservice.ControlledJournal{Stream: segment.Stream{Volume: volume, Generation: generation,
				Configuration: configuration, Epoch: 1, Kind: 1}, Owner: testID(t), Incarnation: journal},
			HostKey: c.HostKeys[scope.Session].Public().(ed25519.PublicKey), GuestKey: c.GuestKeys[scope.Session]})
		e.TicketFamilies = append(e.TicketFamilies, viewcontrol.TicketFamilies{
			Acquisition: viewcontrol.TicketFamily{Anchor: testUUID(t), Nonce: testUUID(t)},
			Control:     viewcontrol.TicketFamily{Anchor: testUUID(t), Nonce: testUUID(t)}})
	}
	return e
}

func (f requestFixture) path(name string) string { return filepath.Join(f.root, name) }

// TestRequestsDriveActualSigner runs both fill steps through the CLI and
// signs their outputs with the actual external signer (hfs-retained9p-operator
// code) using a throwaway operator key, then checks the signed bind against
// the provision hash that the runtime would announce.
func TestRequestsDriveActualSigner(t *testing.T) {
	f := newRequestFixture(t)
	var stdout bytes.Buffer
	run(t, []string{"enroll-request", "--template", filepath.Join(f.output, TemplateFile), "--proposal", f.inputs.ProposalPath,
		"--boot-id", f.inputs.BootID, "--provider-nonce", f.inputs.ProviderNonce, "--device-names", "virtio2,virtio3",
		"--observer", f.path("observer.json"), "--output", f.path("enroll-request.json")}, &stdout)
	if err := retained9plab.Run([]string{"enroll", "--request", f.path("enroll-request.json"), "--output", f.path("enrolled")}); err != nil {
		t.Fatal(err)
	}
	var handoff controlledruntime.RuntimeEnrollment
	readJSONFile(t, filepath.Join(f.path("enrolled"), "enrollment.json"), &handoff)
	for i, d := range handoff.Authorization.Enrollment.Domains {
		if d.Scope.GuestBoot != f.inputs.BootID || d.Scope.Session != f.template.Keys[i].Session {
			t.Fatalf("signed domain %d lost the measured boot or order", i)
		}
	}
	if handoff.Observer.Host != "10.20.0.123" || hex.EncodeToString(handoff.Observer.ManifestHash[:]) != f.inputs.Observer.ManifestSHA256 {
		t.Fatal("observer not carried into the signed handoff")
	}
	provision, err := os.ReadFile(filepath.Join(f.path("enrolled"), "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	hash, _, err := inspectProvision(provision)
	if err != nil {
		t.Fatal(err)
	}
	controller := testUUID(t)
	writePrivateJSON(t, f.path("awaiting.json"), map[string]any{"stage": "AWAITING_GUEST_BIND",
		"controller": controller, "provision_hash": hash, "publication": nil})
	run(t, []string{"bind-request", "--template", filepath.Join(f.output, TemplateFile), "--provision",
		filepath.Join(f.path("enrolled"), "provision.json"), "--awaiting", f.path("awaiting.json"),
		"--output", f.path("bind-request.json")}, &stdout)
	if err := retained9plab.Run([]string{"bind", "--request", f.path("bind-request.json"), "--output", f.path("bound")}); err != nil {
		t.Fatal(err)
	}
	var bind struct {
		Payload struct {
			ProvisionHash string `json:"provision_hash"`
			GroupID       string `json:"group_id"`
			Controller    string `json:"controller"`
		} `json:"payload"`
	}
	readJSONFile(t, filepath.Join(f.path("bound"), "bind.json"), &bind)
	if bind.Payload.ProvisionHash != hash || bind.Payload.Controller != controller || bind.Payload.GroupID != f.proposal.ID.String() {
		t.Fatalf("signer bound a different provision/controller/group: %+v", bind.Payload)
	}
}

func run(t *testing.T, args []string, stdout *bytes.Buffer) {
	t.Helper()
	if err := Run(t.Context(), args, stdout); err != nil {
		t.Fatal(err)
	}
}

func readJSONFile(t *testing.T, path string, into any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, into)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestFillEnrollmentRefusals(t *testing.T) {
	cases := map[string]func(*RequestTemplate, *EnrollmentInputs){
		"foreign session":     func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.Domains[0].Scope.Session = testUUID(t) },
		"foreign host key":    func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.Domains[1].HostKey = make([]byte, 32) },
		"foreign guest key":   func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.Domains[1].GuestKey = make([]byte, 32) },
		"foreign volume":      func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.Domains[0].Scope.Volume = testUUID(t) },
		"swapped domains":     func(_ *RequestTemplate, in *EnrollmentInputs) { d := in.Proposal.Domains; d[0], d[1] = d[1], d[0] },
		"one domain":          func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.Domains = in.Proposal.Domains[:1] },
		"other boot":          func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.Domains[0].Scope.GuestBoot = testUUID(t) },
		"missing families":    func(_ *RequestTemplate, in *EnrollmentInputs) { in.Proposal.TicketFamilies = nil },
		"family collision":    collideFamily,
		"bad boot":            func(_ *RequestTemplate, in *EnrollmentInputs) { in.BootID = "boot" },
		"relative proposal":   func(_ *RequestTemplate, in *EnrollmentInputs) { in.ProposalPath = "proposal.json" },
		"tag as device":       func(_ *RequestTemplate, in *EnrollmentInputs) { in.DeviceNames = []string{"hfs0", "hfs1"} },
		"duplicate device":    func(_ *RequestTemplate, in *EnrollmentInputs) { in.DeviceNames = []string{"virtio2", "virtio2"} },
		"one device":          func(_ *RequestTemplate, in *EnrollmentInputs) { in.DeviceNames = []string{"virtio2"} },
		"observer user":       func(_ *RequestTemplate, in *EnrollmentInputs) { in.Observer.Host = "root@10.20.0.123" },
		"observer relative":   func(_ *RequestTemplate, in *EnrollmentInputs) { in.Observer.Identity = "id_ed25519" },
		"observer version":    func(_ *RequestTemplate, in *EnrollmentInputs) { in.Observer.Version = "x" },
		"observer zero":       func(_ *RequestTemplate, in *EnrollmentInputs) { in.Observer.ManifestSHA256 = strings.Repeat("0", 64) },
		"observer short hash": func(_ *RequestTemplate, in *EnrollmentInputs) { in.Observer.SSHSHA256 = "a1" },
		"template prefilled":  func(tm *RequestTemplate, _ *EnrollmentInputs) { tm.Mounts[0].DeviceName = "virtio9" },
		"template version":    func(tm *RequestTemplate, _ *EnrollmentInputs) { tm.Version = "x" },
		"template order":      func(tm *RequestTemplate, _ *EnrollmentInputs) { tm.Mounts[0].DomainIndex = 1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRequestFixture(t)
			template, inputs := f.template, f.inputs
			template.Mounts = append([]controlledruntime.GuestArtifactMount(nil), template.Mounts...)
			mutate(&template, &inputs)
			_, err := FillEnrollment(template, inputs)
			if err == nil {
				t.Fatal("invalid enrollment inputs accepted")
			}
			t.Log(err)
		})
	}
}

func collideFamily(tm *RequestTemplate, in *EnrollmentInputs) {
	in.Proposal.TicketFamilies[1].Acquisition = viewcontrol.TicketFamily{Anchor: tm.Mounts[1].Anchor, Nonce: tm.Mounts[1].Nonce}
}

func TestFillBindRefusals(t *testing.T) {
	f := newRequestFixture(t)
	request, err := FillEnrollment(f.template, f.inputs)
	if err != nil {
		t.Fatal(err)
	}
	writePrivateJSON(t, f.path("enroll-request.json"), request)
	if err := retained9plab.Run([]string{"enroll", "--request", f.path("enroll-request.json"), "--output", f.path("enrolled")}); err != nil {
		t.Fatal(err)
	}
	provisionPath := filepath.Join(f.path("enrolled"), "provision.json")
	provision, err := os.ReadFile(provisionPath)
	if err != nil {
		t.Fatal(err)
	}
	hash, _, err := inspectProvision(provision)
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"stage":"AWAITING_GUEST_BIND","controller":"` + testUUID(t) + `","provision_hash":"` + hash + `","publication":null}`
	if _, err := FillBind(f.template, provisionPath, provision, []byte(valid)); err != nil {
		t.Fatal(err)
	}
	other := newRequestFixture(t)
	cases := map[string]struct {
		template  RequestTemplate
		awaiting  string
		provision []byte
	}{
		"stage":            {f.template, strings.Replace(valid, "GUEST_BIND", "OPERATOR_ENROLLMENT", 1), provision},
		"publication":      {f.template, strings.Replace(valid, `"publication":null`, `"publication":{}`, 1), provision},
		"hash":             {f.template, strings.Replace(valid, hash, strings.Repeat("e5", 32), 1), provision},
		"controller":       {f.template, strings.Replace(valid, `"controller":"`, `"controller":"x`, 1), provision},
		"duplicate key":    {f.template, strings.Replace(valid, `{"stage"`, `{"stage":"x","stage"`, 1), provision},
		"unknown field":    {f.template, strings.Replace(valid, `{"stage"`, `{"extra":1,"stage"`, 1), provision},
		"foreign template": {other.template, valid, provision},
		"altered payload":  {f.template, valid, bytes.Replace(provision, []byte(`"reservations":1024`), []byte(`"reservations":1025`), 1)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := FillBind(c.template, provisionPath, c.provision, []byte(c.awaiting))
			if err == nil {
				t.Fatal("invalid bind inputs accepted")
			}
			t.Log(err)
		})
	}
}
