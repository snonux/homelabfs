//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

func artifactKey(n byte) ed25519.PrivateKey {
	return ed25519.NewKeyFromSeed(bytes.Repeat([]byte{n}, 32))
}
func artifactUUID(n int) string { return fmt.Sprintf("%08x-0000-0000-0000-%012x", n, n) }
func artifactID(n int) fsapi.UUID {
	id, err := fsapi.ParseUUID(artifactUUID(n))
	if err != nil {
		panic(err)
	}
	return id
}
func artifactFixture(t *testing.T) ([]GuestProvisionGroup, GuestArtifactBudget, ed25519.PrivateKey) {
	t.Helper()
	operator := artifactKey(1)
	groups := make([]GuestProvisionGroup, 2)
	for gi := range groups {
		e := viewcontrol.Enrollment{Version: viewcontrol.EnrollmentVersion, ID: artifactID(100 + gi), PageSize: 4096, Bounds: viewcontrol.Bounds{WorkBytes: 64 << 20, Tickets: 8, ControlTickets: 8, Reservations: 2, Identities: 32, Sources: viewcontrol.MinSources(3), SourceBytes: 4096, LedgerBytes: 8 << 20}, BaselineStateHash: hashBytes([]byte("baseline"))}
		e.Baseline = record.FrameRef{StreamKind: 1, Position: fsapi.Position{Volume: artifactID(10), Generation: artifactID(11), Configuration: artifactID(12), Epoch: 1, Sequence: 1}, SHA256: record.Hash{1}}
		for di := range 3 {
			n := gi*3 + di
			scope := guestbridge.Context{AuthorityHash: hashBytes([]byte("authority")), Cluster: artifactUUID(1), Configuration: artifactUUID(12), Export: artifactUUID(2), ExporterIncarnation: artifactUUID(3), FuseSession: artifactUUID(4), Generation: artifactUUID(11), GuestBoot: artifactUUID(5), GuestMount: artifactUUID(200 + n), OriginIncarnation: artifactUUID(6), OriginJournal: artifactUUID(7), OriginNode: artifactUUID(8), PolicyHash: hashBytes([]byte("policy")), RegistryHash: hashBytes([]byte("registry")), Session: artifactUUID(300 + n), VM: artifactUUID(9), Volume: artifactUUID(10)}
			if di == 1 {
				scope.GuestBoot = artifactUUID(55)
				scope.VM = artifactUUID(99)
			}
			host, guest := artifactKey(byte(10+n*2)), artifactKey(byte(11+n*2))
			protocol := guestbridge.MappedProtocol()
			d := controlledvalidation.Enrollment{Protocol: &protocol, Profile: fsservice.ValidationProfile{Disposable: true, Name: `inert "quoted" \ fixture`}, Scope: scope, Endpoint: guestbridge.Endpoint{FuseSession: scope.FuseSession, CommandNode: 2, ResultNode: 3, RecallNode: 4}, ServingGeneration: artifactID(13), Ledger: artifactID(400 + n), Journal: fsservice.ControlledJournal{Stream: segment.Stream{Volume: artifactID(10), Generation: artifactID(11), Configuration: artifactID(12), Epoch: 1, Kind: 1}, Owner: artifactID(8), Incarnation: artifactID(7)}, HostKey: host.Public().(ed25519.PublicKey), GuestKey: guest.Public().(ed25519.PublicKey)}
			e.Domains = append(e.Domains, d)
			families := viewcontrol.TicketFamilies{Acquisition: viewcontrol.TicketFamily{Anchor: artifactUUID(500 + n), Nonce: artifactUUID(600 + n)}, Control: viewcontrol.TicketFamily{Anchor: artifactUUID(700 + n), Nonce: artifactUUID(800 + n)}}
			e.TicketFamilies = append(e.TicketFamilies, families)
			groups[gi].Signers = append(groups[gi].Signers, GuestArtifactSigners{host, guest})
			if di != 1 {
				groups[gi].LocalMounts = append(groups[gi].LocalMounts, GuestArtifactMount{DomainIndex: di, Session: scope.Session, DeviceTag: fmt.Sprintf("hfs%d", n), DeviceName: fmt.Sprintf("virtio%d", n), UserNamespace: artifactUUID(14), IDMapHash: "063ee717c983abc52c8cdeaeaee32c62ecb93c8fc88e6f887313db9a6c471f98", Realm: artifactUUID(15), Subject: "66697874757265", Anchor: artifactUUID(900 + n), Nonce: artifactUUID(1000 + n), ViewControlNonce: families.Control.Nonce})
			}
		}
		groups[gi].Authorization.Enrollment = e
		resignArtifact(t, &groups[gi], operator)
	}
	return groups, GuestArtifactBudget{OrdinaryBytes: 1 << 20, ControlBytes: 1 << 20, Reservations: 32, Cookies: 32, Received: 32}, operator
}
func resignArtifact(t *testing.T, g *GuestProvisionGroup, operator ed25519.PrivateKey) {
	t.Helper()
	raw, err := json.Marshal(g.Authorization.Enrollment)
	require(t, err)
	g.Authorization.Operator = ed25519.Sign(operator, raw)
	g.Authorization.Domains = nil
	for _, k := range g.Signers {
		g.Authorization.Domains = append(g.Authorization.Domains, viewcontrol.Signatures{Host: ed25519.Sign(k.Host, raw), Guest: ed25519.Sign(k.Guest, raw)})
	}
}
func emitArtifact(t *testing.T, g []GuestProvisionGroup, b GuestArtifactBudget, k ed25519.PrivateKey) []byte {
	t.Helper()
	raw, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k)
	require(t, err)
	return raw
}

func TestGuestArtifactsOriginalAndOuterSignatures(t *testing.T) {
	groups, budget, operator := artifactFixture(t)
	raw := emitArtifact(t, groups, budget, operator)
	var provision guestProvisionArtifact
	require(t, json.Unmarshal(raw, &provision))
	payload, err := canonicalJSON(provision.Payload)
	require(t, err)
	outerSignature, err := hex.DecodeString(provision.OperatorSignature)
	require(t, err)
	if !ed25519.Verify(operator.Public().(ed25519.PublicKey), payload, outerSignature) {
		t.Fatal("outer signature")
	}
	binds := make([]json.RawMessage, len(groups))
	enrollmentHashes := make([]string, len(groups))
	profileHashes := make([]string, len(groups))
	for i, g := range groups {
		original, err := json.Marshal(g.Authorization.Enrollment)
		require(t, err)
		decoded, err := hex.DecodeString(provision.Payload.Groups[i].EnrollmentBytes)
		require(t, err)
		if !bytes.Equal(original, decoded) || len(g.Authorization.Enrollment.Domains) != 3 {
			t.Fatal("original raw inventory changed")
		}
		if provision.Payload.Groups[i].EnrollmentOperatorSignature != hex.EncodeToString(g.Authorization.Operator) || !sameJSON(provision.Payload.Groups[i].EnrollmentDomainSignatures, guestOriginalSignatures(g.Authorization.Domains)) {
			t.Fatal("original signatures changed")
		}

		originalOperator, err := hex.DecodeString(provision.Payload.Groups[i].EnrollmentOperatorSignature)
		require(t, err)
		if !ed25519.Verify(operator.Public().(ed25519.PublicKey), original, originalOperator) {
			t.Fatal("original operator signature")
		}
		for di, d := range g.Authorization.Enrollment.Domains {
			for _, pair := range []struct {
				key   []byte
				sig   []byte
				outer string
			}{{d.HostKey, g.Authorization.Domains[di].Host, provision.GroupSignatures[i][di].Host}, {d.GuestKey, g.Authorization.Domains[di].Guest, provision.GroupSignatures[i][di].Guest}} {
				sig, err := hex.DecodeString(pair.outer)
				require(t, err)
				if !ed25519.Verify(pair.key, original, pair.sig) || !ed25519.Verify(pair.key, payload, sig) {
					t.Fatal("independent signature verification")
				}
			}
		}
		bind, err := SignGuestBind(raw, g.Authorization.Enrollment.ID.String(), artifactUUID(1100+i), operator, g.Signers)
		require(t, err)
		binds[i] = bind
		var binding guestBindArtifact
		require(t, json.Unmarshal(bind, &binding))
		bp, err := canonicalJSON(binding.Payload)
		require(t, err)
		bindOperator, err := hex.DecodeString(binding.OperatorSignature)
		require(t, err)
		if binding.Payload.ProvisionHash != hashBytes(payload) || !ed25519.Verify(operator.Public().(ed25519.PublicKey), bp, bindOperator) {
			t.Fatal("bind operator signature/hash")
		}
		if len(binding.DomainSignatures) != len(g.Authorization.Enrollment.Domains) {
			t.Fatal("bind signature cardinality")
		}
		for di, d := range g.Authorization.Enrollment.Domains {
			host, err := hex.DecodeString(binding.DomainSignatures[di].Host)
			require(t, err)
			guest, err := hex.DecodeString(binding.DomainSignatures[di].Guest)
			require(t, err)
			if !ed25519.Verify(d.HostKey, bp, host) || !ed25519.Verify(d.GuestKey, bp, guest) {
				t.Fatal("independent bind domain signatures")
			}
		}
		enrollmentHashes[i] = hashBytes(original)
		profile, err := canonicalJSON(struct {
			Domains  []controlledvalidation.Enrollment
			Bounds   viewcontrol.Bounds
			Families []viewcontrol.TicketFamilies
			PageSize uint64
		}{g.Authorization.Enrollment.Domains, g.Authorization.Enrollment.Bounds, g.Authorization.Enrollment.TicketFamilies, g.Authorization.Enrollment.PageSize})
		require(t, err)
		profileHashes[i] = hashBytes(profile)
	}
	// Explicit fixture export is only inert signed bytes, with no runtime access.
	bundle := struct {
		Provision        json.RawMessage   `json:"provision"`
		Binds            []json.RawMessage `json:"binds"`
		Operator         string            `json:"operator_public_key"`
		EnrollmentHashes []string          `json:"enrollment_hashes"`
		ProfileHashes    []string          `json:"profile_hashes"`
	}{raw, binds, hex.EncodeToString(operator.Public().(ed25519.PublicKey)), enrollmentHashes, profileHashes}
	bundleBytes, err := canonicalJSON(bundle)
	require(t, err)
	if path := os.Getenv("HFS_GUEST_ARTIFACT_FIXTURE"); path != "" {
		require(t, os.WriteFile(path, append(bundleBytes, '\n'), 0600))
	} else {
		committed, err := os.ReadFile("testdata/guest-artifacts.json")
		require(t, err)
		if !bytes.Equal(committed, append(bundleBytes, '\n')) {
			t.Fatal("committed fixture differs from freshly signed bytes")
		}
	}
}

func TestGuestArtifactsRefuseInvalidInputs(t *testing.T) {
	tests := map[string]func(*[]GuestProvisionGroup, *GuestArtifactBudget, *ed25519.PrivateKey){
		"empty groups":           func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) { *g = nil },
		"short operator":         func(_ *[]GuestProvisionGroup, _ *GuestArtifactBudget, k *ed25519.PrivateKey) { *k = (*k)[:31] },
		"corrupt private suffix": func(_ *[]GuestProvisionGroup, _ *GuestArtifactBudget, k *ed25519.PrivateKey) { (*k)[63] ^= 1 },
		"wrong operator":         func(_ *[]GuestProvisionGroup, _ *GuestArtifactBudget, k *ed25519.PrivateKey) { *k = artifactKey(99) },
		"short host": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].Signers[0].Host = nil
		},
		"wrong guest": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].Signers[0].Guest = artifactKey(99)
		},
		"reordered signers": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			s := (*g)[0].Signers
			s[0], s[1] = s[1], s[0]
		},
		"missing signer": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].Signers = (*g)[0].Signers[:2]
		},
		"extra signer": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].Signers = append((*g)[0].Signers, (*g)[0].Signers[0])
		},
		"remote signature": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].Authorization.Domains[1].Host[0] ^= 1
		},
		"missing original signature": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].Authorization.Domains = (*g)[0].Authorization.Domains[:2]
		},
		"extra original signature": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			a := &(*g)[0].Authorization
			a.Domains = append(a.Domains, a.Domains[0])
		},
		"duplicate enrollment": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) { (*g)[1] = (*g)[0] },
		"remote row": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].DomainIndex = 1
		},
		"missing row": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts = (*g)[0].LocalMounts[:1]
		},
		"extra row": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts = append((*g)[0].LocalMounts, (*g)[0].LocalMounts[0])
		},
		"reordered rows": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			s := (*g)[0].LocalMounts
			s[0], s[1] = s[1], s[0]
		},
		"duplicate tag": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[1].LocalMounts[0].DeviceTag = (*g)[0].LocalMounts[0].DeviceTag
		},
		"duplicate name": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[1].LocalMounts[0].DeviceName = (*g)[0].LocalMounts[0].DeviceName
		},
		"wrong session": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].Session = artifactUUID(44)
		},
		"slash": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].DeviceTag = "a/b"
		},
		"unicode": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].DeviceName = "é"
		},
		"overlong device": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].DeviceTag = strings.Repeat("x", 256)
		},
		"empty device": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].DeviceName = ""
		},
		"nul": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].DeviceName = "a\x00"
		},
		"bad subject": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].Subject = "AB"
		},
		"empty subject": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].Subject = ""
		},
		"long subject": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].Subject = strings.Repeat("ab", 1025)
		},
		"bad idmap": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].IDMapHash = strings.Repeat("A", 64)
		},
		"zero namespace": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].UserNamespace = "00000000-0000-0000-0000-000000000000"
		},
		"wrong control nonce": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			(*g)[0].LocalMounts[0].ViewControlNonce = artifactUUID(99)
		},
		"ordinary acquisition collision": func(g *[]GuestProvisionGroup, _ *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			m := &(*g)[0].LocalMounts[0]
			f := (*g)[0].Authorization.Enrollment.TicketFamilies[0].Acquisition
			m.Anchor, m.Nonce = f.Anchor, f.Nonce
		},
		"zero ordinary": func(_ *[]GuestProvisionGroup, b *GuestArtifactBudget, _ *ed25519.PrivateKey) { b.OrdinaryBytes = 0 },
		"sum overflow": func(_ *[]GuestProvisionGroup, b *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			b.OrdinaryBytes = ^uint64(0)
		},
		"sum too large": func(_ *[]GuestProvisionGroup, b *GuestArtifactBudget, _ *ed25519.PrivateKey) {
			b.OrdinaryBytes = 1 << 30
		},
		"reservation cap": func(_ *[]GuestProvisionGroup, b *GuestArtifactBudget, _ *ed25519.PrivateKey) { b.Reservations = 8193 },
		"cookie cap":      func(_ *[]GuestProvisionGroup, b *GuestArtifactBudget, _ *ed25519.PrivateKey) { b.Cookies = 4097 },
		"received cap":    func(_ *[]GuestProvisionGroup, b *GuestArtifactBudget, _ *ed25519.PrivateKey) { b.Received = 4097 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			g, b, k := artifactFixture(t)
			mutate(&g, &b, &k)
			if raw, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k); err == nil || raw != nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
}

func TestGuestArtifactsResignedSemanticRefusals(t *testing.T) {
	tests := map[string]func(*GuestProvisionGroup){
		"baseline kind":     func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Baseline.StreamKind = 0 },
		"baseline UUID":     func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Baseline.Position.Volume = fsapi.UUID{} },
		"baseline epoch":    func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Baseline.Position.Epoch = 0 },
		"baseline reporter": func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Baseline.Reporter = new(fsapi.UUID) },
		"baseline hash":     func(g *GuestProvisionGroup) { g.Authorization.Enrollment.BaselineStateHash = strings.Repeat("A", 64) },
		"journal kind":      func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Domains[0].Journal.Stream.Kind = 0 },
		"bad bounds":        func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Bounds.WorkBytes = 0 },
		"overflow bounds":   func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Bounds.Sources = int(^uint(0) >> 1) },
		"bad profile":       func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Domains[0].Profile.Disposable = false },
		"bad UUID": func(g *GuestProvisionGroup) {
			g.Authorization.Enrollment.Domains[1].Scope.VM = strings.ToUpper("abcdef01-0000-0000-0000-000000000001")
		},
		"equal endpoint keys": func(g *GuestProvisionGroup) {
			g.Authorization.Enrollment.Domains[1].GuestKey = g.Authorization.Enrollment.Domains[1].HostKey
			g.Signers[1].Guest = g.Signers[1].Host
		},
		"bad public key": func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Domains[1].HostKey = []byte{1} },
		"wrong local VM": func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Domains[2].Scope.VM = artifactUUID(99) },
		"duplicate session": func(g *GuestProvisionGroup) {
			g.Authorization.Enrollment.Domains[2].Scope.Session = g.Authorization.Enrollment.Domains[0].Scope.Session
		},
		"identical lanes": func(g *GuestProvisionGroup) {
			g.Authorization.Enrollment.TicketFamilies[1].Control = g.Authorization.Enrollment.TicketFamilies[1].Acquisition
		},
		"invalid lane UUID": func(g *GuestProvisionGroup) { g.Authorization.Enrollment.TicketFamilies[1].Acquisition.Anchor = "bad" },
		"old protocol": func(g *GuestProvisionGroup) {
			p := guestbridge.CleanupProtocol()
			g.Authorization.Enrollment.Domains[1].Protocol = &p
		},
		"zero ledger": func(g *GuestProvisionGroup) { g.Authorization.Enrollment.Domains[1].Ledger = fsapi.UUID{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			g, b, k := artifactFixture(t)
			mutate(&g[0])
			resignArtifact(t, &g[0], k)
			if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k); err == nil {
				t.Fatal("signed malformed original")
			}
		})
	}
}

// TestGuestArtifactsRefuseSourcesBelowFloor checks the signer names a
// Sources bound below viewcontrol.MinSources for the group's domain count
// (i64; zq3-r7 signed 2 and every non-read op then failed with NoSpace).
func TestGuestArtifactsRefuseSourcesBelowFloor(t *testing.T) {
	g, b, k := artifactFixture(t)
	e := &g[0].Authorization.Enrollment
	e.Bounds.Sources = viewcontrol.MinSources(len(e.Domains)) - 1
	resignArtifact(t, &g[0], k)
	if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k); !errors.Is(err, fsapi.Invalid) || !strings.Contains(err.Error(), "Bounds.Sources") {
		t.Fatalf("below-floor Sources: %v", err)
	}
}

func TestGuestArtifactsBindRejectsTampering(t *testing.T) {
	tests := map[string]func(*guestProvisionArtifact){
		"outer signature":   func(a *guestProvisionArtifact) { a.OperatorSignature = strings.Repeat("0", 128) },
		"remote signature":  func(a *guestProvisionArtifact) { a.GroupSignatures[0][1].Host = strings.Repeat("0", 128) },
		"missing group":     func(a *guestProvisionArtifact) { a.GroupSignatures = a.GroupSignatures[:1] },
		"extra group":       func(a *guestProvisionArtifact) { a.GroupSignatures = append(a.GroupSignatures, a.GroupSignatures[0]) },
		"reordered domains": func(a *guestProvisionArtifact) { s := a.GroupSignatures[0]; s[0], s[1] = s[1], s[0] },
		"missing domain":    func(a *guestProvisionArtifact) { a.GroupSignatures[0] = a.GroupSignatures[0][:2] },
		"upper signature": func(a *guestProvisionArtifact) {
			a.GroupSignatures[0][0].Guest = strings.ToUpper(a.GroupSignatures[0][0].Guest)
		},
		"upper enrollment": func(a *guestProvisionArtifact) {
			a.Payload.Groups[0].EnrollmentBytes = strings.ToUpper(a.Payload.Groups[0].EnrollmentBytes)
		},
		"original signature": func(a *guestProvisionArtifact) {
			a.Payload.Groups[0].EnrollmentDomainSignatures[1].Guest = strings.Repeat("0", 128)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			g, b, k := artifactFixture(t)
			raw := emitArtifact(t, g, b, k)
			var a guestProvisionArtifact
			require(t, json.Unmarshal(raw, &a))
			before, err := canonicalJSON(a.Payload)
			require(t, err)
			mutate(&a)
			after, err := canonicalJSON(a.Payload)
			require(t, err)
			if !bytes.Equal(before, after) {
				resignProvisionArtifact(t, &a, g, k)
			}
			changed, err := canonicalJSON(a)
			require(t, err)
			if _, err = SignGuestBind(changed, g[0].Authorization.Enrollment.ID.String(), artifactUUID(1100), k, g[0].Signers); err == nil {
				t.Fatal("accepted tampered provision")
			}
		})
	}
	g, b, k := artifactFixture(t)
	raw := emitArtifact(t, g, b, k)
	for _, bad := range [][]byte{append([]byte(" "), raw...), append(raw, '\n'), []byte(`{}`), bytes.Repeat([]byte("x"), guestArtifactLimit+1), bytes.Replace(raw, []byte(`"payload":`), []byte(`"unknown":`), 1)} {
		if _, err := SignGuestBind(bad, g[0].Authorization.Enrollment.ID.String(), artifactUUID(1100), k, g[0].Signers); err == nil {
			t.Fatal("accepted noncanonical provision")
		}
	}
	for _, id := range []string{"bad", artifactUUID(9999)} {
		if _, err := SignGuestBind(raw, id, artifactUUID(1100), k, g[0].Signers); err == nil {
			t.Fatal("accepted unknown group")
		}
	}
	if _, err := SignGuestBind(raw, g[0].Authorization.Enrollment.ID.String(), "bad", k, g[0].Signers); err == nil {
		t.Fatal("invalid controller")
	}
	if _, err := SignGuestBind(raw, g[0].Authorization.Enrollment.ID.String(), artifactUUID(1100), k, g[1].Signers); err == nil {
		t.Fatal("wrong bind signers")
	}
}

func TestGuestArtifactsBoundaries(t *testing.T) {
	g, _, k := artifactFixture(t)
	b := GuestArtifactBudget{OrdinaryBytes: (1 << 30) - 1, ControlBytes: 1, Reservations: 8192, Cookies: 4096, Received: 4096}
	g[0].LocalMounts[0].Subject = strings.Repeat("ab", 1024)
	g[0].LocalMounts[0].DeviceTag = strings.Repeat("x", 255)
	// Matching anchors are legal when the complete lanes differ.
	g[0].LocalMounts[0].Anchor = g[0].Authorization.Enrollment.TicketFamilies[0].Acquisition.Anchor
	emitArtifact(t, g, b, k)
	for _, id := range []string{"", "00000000-0000-0000-0000-000000000000", strings.ToUpper("abcdef01-0000-0000-0000-000000000001")} {
		if _, err := SignGuestProvision(id, artifactUUID(16), g, b, k); err == nil {
			t.Fatal("invalid boot")
		}
		if _, err := SignGuestProvision(artifactUUID(5), id, g, b, k); err == nil {
			t.Fatal("invalid nonce")
		}
	}
	// Exact byte cap applies after full canonical wrapper serialization.
	for _, size := range []int{guestArtifactLimit - 2, guestArtifactLimit - 1} {
		raw, err := guestArtifactBytes(strings.Repeat("x", size))
		if size == guestArtifactLimit-2 {
			require(t, err)
			if len(raw) != guestArtifactLimit {
				t.Fatal("exact cap")
			}
		} else if err == nil {
			t.Fatal("cap overflow")
		}
	}
	// Real signed artifacts can exceed the byte bound below the row/count caps.
	_, b, k = artifactFixture(t)
	oversized := []GuestProvisionGroup{}
	for i := range 12 {
		extra := uniqueArtifactGroup(t, i, k)
		oversized = append(oversized, extra)
	}
	if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), oversized, b, k); err != fsapi.NoSpace {
		t.Fatalf("oversized signed artifact: %v", err)
	}
	for _, mutate := range []func(*GuestArtifactBudget){func(b *GuestArtifactBudget) { b.ControlBytes = 0 }, func(b *GuestArtifactBudget) { b.Reservations = 0 }, func(b *GuestArtifactBudget) { b.Cookies = 0 }, func(b *GuestArtifactBudget) { b.Received = 0 }} {
		g, b, k = artifactFixture(t)
		mutate(&b)
		if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k); err == nil {
			t.Fatal("zero budget accepted")
		}
	}
}

func uniqueArtifactGroup(t *testing.T, n int, operator ed25519.PrivateKey) GuestProvisionGroup {
	t.Helper()
	groups, _, _ := artifactFixture(t)
	g := groups[0]
	g.Authorization.Enrollment.ID = artifactID(2000 + n)
	for i := range g.Authorization.Enrollment.Domains {
		g.Authorization.Enrollment.Domains[i].Scope.Session = artifactUUID(3000 + n*3 + i)
	}
	for i := range g.LocalMounts {
		m := &g.LocalMounts[i]
		m.Session = g.Authorization.Enrollment.Domains[m.DomainIndex].Scope.Session
		m.DeviceName = fmt.Sprintf("virtio-%d-%d", n, i)
		m.DeviceTag = fmt.Sprintf("hfs-%d-%d", n, i)
	}
	resignArtifact(t, &g, operator)
	return g
}
func TestGuestArtifactInventoryCountAndProfileBoundaries(t *testing.T) {
	var g []GuestProvisionGroup
	var b GuestArtifactBudget
	var k ed25519.PrivateKey
	for _, name := range []string{"", strings.Repeat("x", 256), "unicode-é", "control\tname", "html<name", "html>name", "html&name"} {
		g, b, k = artifactFixture(t)
		g[0].Authorization.Enrollment.Domains[0].Profile.Name = name
		resignArtifact(t, &g[0], k)
		if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k); err == nil {
			t.Fatalf("unsupported profile %q", name)
		}
	}
	for _, name := range []string{`quote"backslash\`, strings.Repeat("x", 255), "one/slash"} {
		g, b, k = artifactFixture(t)
		for i := range g[0].Authorization.Enrollment.Domains {
			g[0].Authorization.Enrollment.Domains[i].Profile.Name = name
		}
		resignArtifact(t, &g[0], k)
		emitArtifact(t, g, b, k)
	}
	inventory := guestArtifactInventory{ids: map[string]bool{}, sessions: map[string]bool{}, tags: map[string]bool{}, names: map[string]bool{}}
	for i := range 32 {
		group := uniqueArtifactGroup(t, i, k)
		require(t, inventory.check(group.Authorization.Enrollment, group.LocalMounts, artifactUUID(5)))
	}
	extra := uniqueArtifactGroup(t, 32, k)
	if err := inventory.check(extra.Authorization.Enrollment, extra.LocalMounts, artifactUUID(5)); err == nil {
		t.Fatal("more than64 local rows")
	}
	if inventory.count != 66 {
		t.Fatalf("expected aggregate row count66, got%d", inventory.count)
	}
	g, b, k = artifactFixture(t)
	groups := make([]GuestProvisionGroup, 65)
	for i := range groups {
		groups[i] = g[0]
	}
	if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), groups, b, k); err == nil {
		t.Fatal("more than64 groups")
	}
	// Keep every other signed invariant valid: extend the complete inventory
	// with remote domains and aligned lane identities, keys and signatures.
	complete := &g[0]
	for i := 3; i < 65; i++ {
		remote := complete.Authorization.Enrollment.Domains[1]
		remote.Scope.Session = artifactUUID(4000 + i)
		remote.Scope.GuestMount = artifactUUID(5000 + i)
		remote.Ledger = artifactID(6000 + i)
		complete.Authorization.Enrollment.Domains = append(complete.Authorization.Enrollment.Domains, remote)
		complete.Authorization.Enrollment.TicketFamilies = append(complete.Authorization.Enrollment.TicketFamilies, viewcontrol.TicketFamilies{Acquisition: viewcontrol.TicketFamily{Anchor: artifactUUID(7000 + i), Nonce: artifactUUID(8000 + i)}, Control: viewcontrol.TicketFamily{Anchor: artifactUUID(9000 + i), Nonce: artifactUUID(10000 + i)}})
		complete.Signers = append(complete.Signers, complete.Signers[1])
	}
	resignArtifact(t, complete, k)
	// Invalid identifies the domain count refusal before the independent byte cap.
	if _, err := SignGuestProvision(artifactUUID(5), artifactUUID(16), g, b, k); !errors.Is(err, fsapi.Invalid) {
		t.Fatalf("65 complete original domains: %v", err)
	}
}

func resignProvisionArtifact(t *testing.T, a *guestProvisionArtifact, groups []GuestProvisionGroup, operator ed25519.PrivateKey) {
	t.Helper()
	raw, err := canonicalJSON(a.Payload)
	require(t, err)
	a.OperatorSignature = hex.EncodeToString(ed25519.Sign(operator, raw))
	for i, g := range groups {
		a.GroupSignatures[i], err = guestSignDomains(raw, g.Authorization.Enrollment, g.Signers)
		require(t, err)
	}
}
func TestGuestArtifactsBindRejectsSignedInvalidPolicy(t *testing.T) {
	mutations := map[string]func(*guestProvisionArtifact){
		"budget": func(a *guestProvisionArtifact) { a.Payload.Budget.ControlBytes = 0 },
		"index":  func(a *guestProvisionArtifact) { a.Payload.Groups[0].LocalMounts[0].DomainIndex = 1 },
		"duplicate device": func(a *guestProvisionArtifact) {
			a.Payload.Groups[1].LocalMounts[0].DeviceName = a.Payload.Groups[0].LocalMounts[0].DeviceName
		},
		"nonce": func(a *guestProvisionArtifact) {
			a.Payload.Groups[0].LocalMounts[0].ViewControlNonce = artifactUUID(99)
		},
		"unknown original field": func(a *guestProvisionArtifact) {
			g := &a.Payload.Groups[0]
			raw, err := hex.DecodeString(g.EnrollmentBytes)
			require(t, err)
			raw = append([]byte(`{"Unknown":0,`), raw[1:]...)
			g.EnrollmentBytes = hex.EncodeToString(raw)
		},
		"noncanonical original order": func(a *guestProvisionArtifact) {
			g := &a.Payload.Groups[0]
			raw, err := hex.DecodeString(g.EnrollmentBytes)
			require(t, err)
			var e viewcontrol.Enrollment
			require(t, json.Unmarshal(raw, &e))
			canonical, err := canonicalJSON(e)
			require(t, err)
			g.EnrollmentBytes = hex.EncodeToString(canonical)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			g, b, k := artifactFixture(t)
			var a guestProvisionArtifact
			require(t, json.Unmarshal(emitArtifact(t, g, b, k), &a))
			mutate(&a)
			resignProvisionArtifact(t, &a, g, k)
			raw, err := canonicalJSON(a)
			require(t, err)
			if _, err = SignGuestBind(raw, g[0].Authorization.Enrollment.ID.String(), artifactUUID(1100), k, g[0].Signers); err == nil {
				t.Fatal("accepted fully signed invalid policy")
			}
		})
	}
}
