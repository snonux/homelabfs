//go:build linux || freebsd

package controlledruntime

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
	"golang.org/x/sys/unix"
)

// All semantic/protocol/file/process/C bootstrap owners are real. Only native
// mount/INIT/fd syscalls and observed guest kernel/physical state are OS seams.
func runtimeDriverFixture(t *testing.T, binary string, devices int) (*sourceRig, *restartChild, RuntimeConfig, runtimeOS) {
	t.Helper()
	root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "driver-")
	require(t, err)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("preserved runtime resources %s", root)
		} else {
			require(t, os.RemoveAll(root))
		}
	})
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(root, "io"), Net: "unix"})
	require(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	s, err := Create(t.Context(), filepath.Join(root, "owner"))
	require(t, err)
	data, err := os.ReadFile(binary)
	require(t, err)
	args := []string{filepath.Join(root, "io"), root, "runtime", testUUID(50).String()}
	if devices == 2 {
		args = append(args, testUUID(151).String())
	}
	spec := Executable{Path: binary, SHA256: sha256.Sum256(data), Args: args, ControlledBootstrap: true}
	require(t, s.Start(t.Context(), spec))
	d := &restartChild{t: t, originals: map[uint64][]byte{}, writes: map[uint64]int{}, writeBodies: map[uint64][]byte{}, keep: map[string]bool{}, cells: make([]uint64, 2), claims: make([]uint64, 2), lastCall: make([]uint64, 2)}
	for i := 0; i < 2; i++ {
		require(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
		conn, e := listener.AcceptUnix()
		require(t, e)
		var kind [1]byte
		_, e = io.ReadFull(conn, kind[:])
		require(t, e)
		if kind[0] == 'C' {
			d.command = conn
		} else {
			d.native = conn
		}
		d.acks = append(d.acks, map[uint64]guestbridge.Ack{})
		d.classes = append(d.classes, map[uint64]bool{})
		d.replies = append(d.replies, map[uint64][]byte{})
	}
	t.Cleanup(func() {
		_ = d.writePacket('Q', nil)
		_ = d.command.Close()
		_ = d.native.Close()
		_ = s.Close()
		select {
		case <-s.child.exited:
		case <-time.After(5 * time.Second):
			t.Error("test C stand-in did not exit on explicit fixture Q")
		}
		_ = s.child.executable.Close()
	})
	r := newSourceRigDomainsOwnedSupervisor(t, guestbridge.TerminalProtocol(), 2, hashBytes([]byte("map")), false, nil, s)
	r.mappingTransport.Close()
	require(t, r.controller.Close())
	for _, peer := range r.peers {
		require(t, peer.provider.Close())
	}
	require(t, os.Rename(filepath.Join(s.root, "guest-admission.json"), filepath.Join(s.root, "fixture-prior-admission.json")))
	s.record.Controller = fsapi.UUID{}
	s.record.Stage = "mounted"
	d.rig = r
	c := RuntimeConfig{Version: "homelabfs-controlled-runtime/1", VolumeConfig: filepath.Join(root, "volume-config"), OwnerDirectory: s.root, EnrollmentFile: filepath.Join(root, "signed-handoff.json"), Exporter: spec, Operator: r.artifactOperator.Public().(ed25519.PublicKey), HostKeys: map[string]ed25519.PrivateKey{}, GuestKeys: map[string]ed25519.PublicKey{}, Views: r.artifactAuthorization.Enrollment.Bounds, Providers: ProviderLimits{Claims: 64, RetainedBytes: 16 << 20, SourceBytes: 8 << 20, ClaimCreditBytes: 1 << 20, SourceCreditBytes: 1 << 20}, Capabilities: 64, CapabilityIdentities: 128, CredentialRevisions: 32, BootstrapSeconds: 3, Service: fsservice.Limits{Queue: 8, IOBytes: 65536, Handles: 64, Lookups: 128, Reservations: 128, Locks: 64, LogicalBytes: 4 << 20}, Budget: RuntimeBudget{HostBytes: 4 << 30, ServiceBytes: 64 << 20, ExporterBytes: 274 << 20, GuestCopyBytes: 16 << 20, GuestBytes: 2 << 30, GuestBaseBytes: 256 << 20, Guest: GuestArtifactBudget{OrdinaryBytes: 64 << 20, ControlBytes: 32 << 20, Reservations: 32, Cookies: 32, Received: 32}}}
	for _, peer := range r.peers {
		c.Scopes = append(c.Scopes, peer.scope)
		c.HostKeys[peer.scope.Session] = peer.key
		c.GuestKeys[peer.scope.Session] = r.artifactGuest.Public().(ed25519.PublicKey)
	}
	platform := runtimeOS{
		open: func(context.Context, RuntimeConfig) (*runtimeLocal, error) {
			return &runtimeLocal{service: r.service, mountpoint: root, close: r.service.Close}, nil
		},
		create: func(context.Context, string) (*Supervisor, error) { return s, nil },
		start: func(_ context.Context, owner *Supervisor, e Executable) error {
			if owner != s || e.SHA256 != spec.SHA256 || !e.ControlledBootstrap {
				return fsapi.Permission
			}
			return s.child.live()
		},
		mount: func(_ context.Context, owner *Supervisor, _ string, shell *fuseadapter.Shell) (guestbridge.Endpoint, error) {
			if owner != s {
				return guestbridge.Endpoint{}, fsapi.Permission
			}
			s.mount.shell = shell
			return r.native.Endpoint, s.checkLive()
		},
		bind: func(_ context.Context, owner *Supervisor, transport *guestbridge.Transport) error {
			if owner != s {
				return fsapi.Permission
			}
			d.mu.Lock()
			d.transport = transport
			d.mu.Unlock()
			var fds [3]int
			for i := 0; i < 3; i++ {
				path := filepath.Join(root, "native-primitive-"+string(rune('0'+i)))
				require(t, os.WriteFile(path, nil, 0600))
				flags := os.O_RDONLY
				if i == 0 {
					flags = os.O_WRONLY
				}
				file, e := os.OpenFile(path, flags, 0)
				if e != nil {
					return e
				}
				t.Cleanup(func() { _ = file.Close() })
				s.mount.files[i] = file
				fds[i] = int(file.Fd())
				var st unix.Stat_t
				if e = unix.Fstat(fds[i], &st); e != nil {
					return e
				}
				s.mount.expected[i] = descriptorIdentity{Device: uint64(st.Dev), Inode: uint64(i + 2), Size: guestbridge.VirtualSize, Access: func() uint32 {
					if i == 0 {
						return 1
					}
					return 2
				}()}
			}
			s.record.Stage = "exported"
			if e := s.protect(); e != nil {
				return e
			}
			go d.serveNative()
			return sendTuple(s.child.control, s.record.Endpoint, s.record.Controller, fds)
		},
	}
	return r, d, c, platform
}

func runtimeSignedHandoff(t *testing.T, r *sourceRig, c RuntimeConfig, e viewcontrol.Enrollment) RuntimeEnrollment {
	t.Helper()
	group := GuestProvisionGroup{Authorization: viewcontrol.Authorization{Enrollment: e}}
	for i, peer := range r.peers {
		group.Signers = append(group.Signers, GuestArtifactSigners{peer.key, r.artifactGuest})
		group.LocalMounts = append(group.LocalMounts, GuestArtifactMount{DomainIndex: i, Session: peer.scope.Session, DeviceTag: "hfs" + string(rune('0'+i)), DeviceName: "virtio" + string(rune('0'+i)), UserNamespace: peer.credentials.UserNamespace, IDMapHash: peer.credentials.IDMapHash, Realm: testUUID(100).String(), Subject: "616263", Anchor: testUUID(101).String(), Nonce: testUUID(103).String(), ViewControlNonce: e.TicketFamilies[i].Control.Nonce})
	}
	resignArtifact(t, &group, r.artifactOperator)
	provision, err := SignGuestProvision(r.scope.GuestBoot, r.installed.Admission.ProviderNonce, []GuestProvisionGroup{group}, c.Budget.Guest, r.artifactOperator)
	require(t, err)
	h := RuntimeEnrollment{Authorization: group.Authorization, Provision: provision, Observer: r.admission.observer}
	for i, domain := range e.Domains {
		raw, err := json.Marshal(domain)
		require(t, err)
		h.Providers = append(h.Providers, controlledvalidation.Authorization{Enrollment: domain, OperatorSignature: ed25519.Sign(r.artifactOperator, raw), HostSignature: ed25519.Sign(r.peers[i].key, raw), GuestSignature: ed25519.Sign(r.artifactGuest, raw)})
	}
	return h
}

func TestRuntimeDriverSharedSequenceRealC(t *testing.T) {
	binary := buildRestartChild(t)
	for _, fault := range []string{"none", "missing-device", "unbound-guest", "one-hello", "changed-policy", "missing-provider", "missing-provision", "missing-observer", "missing-host-key", "missing-service", "missing-native"} {
		t.Run(fault, func(t *testing.T) {
			devices := 2
			if fault == "missing-device" {
				devices = 1
			}
			r, d, c, platform := runtimeDriverFixture(t, binary, devices)
			if fault == "missing-host-key" {
				c.HostKeys = nil
			}
			if fault == "missing-service" {
				platform.open = func(context.Context, RuntimeConfig) (*runtimeLocal, error) { return nil, fsapi.Unsupported }
			}
			if fault == "missing-native" {
				platform.mount = func(context.Context, *Supervisor, string, *fuseadapter.Shell) (guestbridge.Endpoint, error) {
					return guestbridge.Endpoint{}, fsapi.Unsupported
				}
			}
			var submitted RuntimeEnrollment
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader, writer := io.Pipe()
			defer func() { _ = reader.Close() }()
			done := make(chan error, 1)
			go func() { err := runRuntimeWithOS(ctx, c, writer, platform); _ = writer.Close(); done <- err }()
			scanner := bufio.NewScanner(reader)
			ready := false
			stages := []string{}
			for scanner.Scan() {
				var event map[string]any
				require(t, json.Unmarshal(scanner.Bytes(), &event))
				stage, _ := event["stage"].(string)
				stages = append(stages, stage)
				switch stage {
				case "AWAITING_OPERATOR_ENROLLMENT":
					var proposal viewcontrol.Enrollment
					require(t, readRuntimeFile(event["proposal"].(string), &proposal))
					h := runtimeSignedHandoff(t, r, c, proposal)
					if fault == "missing-provider" {
						h.Providers = nil
					}
					if fault == "missing-provision" {
						h.Provision = nil
					}
					if fault == "missing-observer" {
						h.Observer.KnownHostsHash = [32]byte{}
					}
					submitted = h
					if fault == "changed-policy" {
						h.Authorization.Enrollment.Domains[1].Scope.PolicyHash = hashBytes([]byte("changed"))
					}
					raw, err := json.Marshal(h)
					require(t, err)
					temp := c.EnrollmentFile + ".next"
					require(t, os.WriteFile(temp, raw, 0600))
					require(t, os.Rename(temp, c.EnrollmentFile))
				case "AWAITING_GUEST_BIND":
					if fault == "unbound-guest" {
						continue
					}
					signers := []GuestArtifactSigners{}
					for _, peer := range r.peers {
						signers = append(signers, GuestArtifactSigners{peer.key, r.artifactGuest})
					}
					bind, err := SignGuestBind(submitted.Provision, submitted.Authorization.Enrollment.ID.String(), event["controller"].(string), r.artifactOperator, signers)
					require(t, err)
					var artifact guestBindArtifact
					require(t, json.Unmarshal(bind, &artifact))
					if artifact.Payload.Controller != event["controller"] || artifact.Payload.ProvisionHash != event["provision_hash"] {
						t.Fatal("signed binding changed actual controller/provision")
					}
					// External operator/guest physical binding is the declared kernel
					// primitive. Actual C Tversion/HELLO and all native HSM/H9B follow.
					count := 2
					if fault == "one-hello" {
						count = 1
					}
					for i := 0; i < count; i++ {
						d.exchange(i, "HELLO", guestbridge.TerminalProtocol(), nil)
					}
				case "READY":
					ready = true
					if event["publication"] != nil || event["domains"] != float64(2) {
						t.Fatal("wrong controlled readiness", event)
					}
					if fault != "none" {
						t.Fatal("failed adapter became ready", fault)
					}
					d.autoReclaim = true // reclaim original HELLO through the real C/host paired ledgers
					_, result := d.root(0)
					if !bytes.Contains(result, []byte(`"publication":null`)) {
						t.Fatal("actual C/service publication", string(result))
					}
					require(t, d.transport.CheckControlledSessions(t.Context(), r.supervisor.record.Controller, []guestbridge.Context{r.scope, r.peers[1].scope}, guestbridge.TerminalProtocol()))
					cancel()
				}
			}
			err := <-done
			if len(stages) > 0 && (r.supervisor.runtimeFunding == nil || r.supervisor.runtimeFunding.transport.Usage().Used.Bytes == 0) {
				t.Fatal("uncertain native owner lost its retained funding")
			}
			if fault == "none" {
				if err != nil {
					b, _ := os.ReadFile(filepath.Join(filepath.Dir(c.OwnerDirectory), "runtime-child.stderr"))
					t.Logf("stages=%v tuple=%v claims=%v C log=%s", stages, r.supervisor.child.tupleVerified, r.supervisor.bootstrapClaimed, b)
				}
				require(t, err)
				if !ready || len(stages) != 3 {
					t.Fatalf("shared driver sequence missing stages: %v", stages)
				}
			} else {
				if err == nil || ready {
					t.Fatalf("failure emitted readiness: %s %v", fault, err)
				}
				if e := r.supervisor.child.live(); e != nil {
					t.Fatalf("failed bootstrap killed VM stand-in: %v", e)
				}
			}
			if err := scanner.Err(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
				t.Fatal(err)
			}
		})
	}
}
