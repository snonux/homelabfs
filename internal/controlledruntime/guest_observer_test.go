//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// This is a real local process substituted ONLY at the external SSH/guest OS
// primitive boundary. It proves no remote kernel, native mount or deployment.
func TestInstalledObserverPinnedProcessAndClosedObservation(t *testing.T) {
	python := fixturePython(t)
	fixture := installedObservation{Version: "homelabfs-installed-observation/1", ManifestHash: hashBytes([]byte("manifest")), BootID: testUUID(1).String(), KernelBuildID: strings.Repeat("ab", 20), Modules: map[string]string{}, ABI: physicalContract, Admission: kernelAdmission{Version: "homelabfs-kernel-admission/1", BootID: testUUID(1).String(), ProviderNonce: testUUID(2).String(), PhysicalContract: physicalContract, Protocols: map[string]guestbridge.Protocol{}}}
	raw, err := canonicalJSON(fixture)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "ssh-primitive")
	quoted, _ := json.Marshal(string(raw))
	// The shebang names the resolved interpreter; SSHHash pins these exact bytes.
	script := "#!" + python + "\nimport sys, pathlib\nargs=sys.argv[1:]\nassert '-oStrictHostKeyChecking=yes' in args\nassert '-F' in args and '/dev/null' in args\nassert 'root@guest.test' in args\nassert 'installed-admission-v1.json' in args[-1]\nfor prefix in ['-oUserKnownHostsFile=', '-oIdentityFile=']:\n p=next(a[len(prefix):] for a in args if a.startswith(prefix))\n assert pathlib.Path(p).read_bytes()\nsys.stdout.write(" + string(quoted) + ")\n"
	if err = os.WriteFile(path, []byte(script), 0500); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(directory, "known_hosts")
	identity := filepath.Join(directory, "identity")
	if err = os.WriteFile(hosts, []byte("pinned fixture host\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(identity, []byte("fixture key"), 0600); err != nil {
		t.Fatal(err)
	}
	observer := GuestObserver{SSH: path, SSHHash: sha256.Sum256([]byte(script)), KnownHosts: hosts, KnownHostsHash: sha256.Sum256([]byte("pinned fixture host\n")), Identity: identity, Host: "guest.test", ManifestHash: sha256.Sum256([]byte("manifest"))}
	actual, err := observer.observe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(actual, fixture) {
		t.Fatal("changed observation")
	}
	observer.KnownHostsHash[0] ^= 1
	if _, err = observer.observe(t.Context()); err == nil {
		t.Fatal("changed pinned host file accepted")
	}
	observer.KnownHostsHash[0] ^= 1
	observer.ManifestHash[0] ^= 1
	if _, err = observer.observe(t.Context()); err == nil {
		t.Fatal("unadmitted manifest accepted")
	}
	observer.ManifestHash[0] ^= 1
	observer.Host = "guest.test;touch /tmp/forged"
	if _, err = observer.observe(t.Context()); err == nil {
		t.Fatal("host shell injection accepted")
	}
	var bounded boundedProbeOutput
	if _, err = bounded.Write(make([]byte, (256<<10)+1)); err == nil {
		t.Fatal("unbounded observation accepted")
	}
}

func TestInstalledAdmissionRejectsIncompleteOrChangedKernelOwnership(t *testing.T) {
	scope := guestbridge.Context{Session: testUUID(3).String(), GuestBoot: testUUID(1).String(), FuseSession: testUUID(4).String(), ExporterIncarnation: testUUID(5).String()}
	a := &Admission{endpoint: guestbridge.Endpoint{FuseSession: scope.FuseSession}, process: testUUID(5)}
	o := installedObservation{Version: "homelabfs-installed-observation/1", BootID: scope.GuestBoot, KernelBuildID: strings.Repeat("ab", 20), ABI: physicalContract, Admission: kernelAdmission{Version: "homelabfs-kernel-admission/1", BootID: scope.GuestBoot, ProviderNonce: testUUID(2).String(), PhysicalContract: physicalContract, Protocols: map[string]guestbridge.Protocol{scope.Session: guestbridge.ControlledProtocol()}, Domains: []guestbridge.Context{scope}, Observations: []kernelDomainObservation{{Scope: scope, Revision: 1, Fence: 1, Phase: "LIVE"}}}}
	if err := a.accept(o, []guestbridge.Context{scope}); err != nil {
		t.Fatal(err)
	}
	missing := o
	missing.Admission.Domains = nil
	if err := a.accept(missing, []guestbridge.Context{scope}); err == nil {
		t.Fatal("missing old domain accepted")
	}
	missing = o
	missing.Admission.Observations = nil
	if err := a.accept(missing, []guestbridge.Context{scope}); err == nil {
		t.Fatal("missing physical domain inventory accepted")
	}
	wrongBoot := o
	wrongBoot.BootID = testUUID(6).String()
	if err := a.accept(wrongBoot, []guestbridge.Context{scope}); err == nil {
		t.Fatal("new boot accepted as old owner")
	}
	if err := (&Admission{refreshed: time.Now().Add(-time.Minute)}).check(scope); err == nil {
		t.Fatal("expired continuity accepted")
	}
	if err := (&Facts{}).Authorize(context.Background(), scope, "VIEW_ACK", json.RawMessage("{}")); err == nil {
		t.Fatal("caller JSON forged VerifiedRequest")
	}
}

func testUUID(n byte) fsapi.UUID {
	var id fsapi.UUID
	id[0], id[6], id[8], id[15] = n, 64, 128, n
	return id
}

func TestActualInstalledProbeReadsCompleteLocalArtifactPrimitives(t *testing.T) {
	python := fixturePython(t)
	directory := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		name = filepath.Join(directory, name)
		require(t, os.MkdirAll(filepath.Dir(name), 0700))
		require(t, os.WriteFile(name, data, 0600))
	}
	buildID := strings.Repeat("ab", 20)
	note := make([]byte, 36)
	binary.LittleEndian.PutUint32(note, 4)
	binary.LittleEndian.PutUint32(note[4:], 20)
	binary.LittleEndian.PutUint32(note[8:], 3)
	copy(note[12:], []byte("GNU\x00"))
	for i := 16; i < len(note); i++ {
		note[i] = 0xab
	}
	kernel, module := []byte("actual disposable kernel image"), []byte("actual disposable module bytes")
	write("boot/kernel", kernel)
	write("lib/modules/hfs_provider.ko", module)
	write("sys/kernel/notes", note)
	write("sys/module/hfs_provider/notes/.note.gnu.build-id", note)
	write("proc/modules", []byte("hfs_provider 100 0 - Live 0x0\n"))
	write("proc/sys/kernel/random/boot_id", []byte(testUUID(1).String()+"\n"))
	manifest, err := canonicalJSON(map[string]any{"version": "homelabfs-installed-artifacts/1", "kernel": map[string]any{"path": "/boot/kernel", "sha256": hashBytes(kernel), "build_id": buildID}, "modules": map[string]any{"hfs_provider": map[string]any{"path": "/lib/modules/hfs_provider.ko", "sha256": hashBytes(module), "build_id": buildID}}, "abi": physicalContract})
	require(t, err)
	write("etc/homelabfs/installed-admission-v1.json", manifest)
	admission, err := canonicalJSON(kernelAdmission{Version: "homelabfs-kernel-admission/1", BootID: testUUID(1).String(), ProviderNonce: testUUID(2).String(), PhysicalContract: physicalContract, Protocols: map[string]guestbridge.Protocol{}})
	require(t, err)
	write("sys/kernel/homelabfs/admission_v1", admission)
	// Execute the actual maintained probe. Only the guest OS root/filesystem
	// syscall boundary is redirected to real disposable local files.
	root, _ := json.Marshal(directory)
	source, _ := json.Marshal(installedProbe)
	wrapper := "import builtins,os,types\nroot=" + string(root) + "\nreal_open=builtins.open\nreal_stat=os.stat\ndef mapped(p):\n return root+str(p) if str(p).startswith('/') else p\ndef opened(p,*a,**k):\n return real_open(mapped(p),*a,**k)\ndef observed_stat(p,*a,**k):\n s=real_stat(mapped(p),*a,**k)\n return types.SimpleNamespace(st_mode=s.st_mode,st_uid=0,st_size=s.st_size)\nbuiltins.open=opened\nos.stat=observed_stat\nos.geteuid=lambda:0\nexec(" + string(source) + ")\n"
	run := func(phase bool) ([]byte, error) {
		args := []string{"-I", "-c", wrapper}
		if phase {
			args = append(args, "phase")
		}
		return exec.CommandContext(t.Context(), python, args...).CombinedOutput()
	}
	raw, err := run(false)
	if err != nil {
		t.Fatalf("actual probe: %s: %v", raw, err)
	}
	var actual installedObservation
	require(t, strictJSON(raw, &actual))
	if actual.ManifestHash != hashBytes(manifest) || actual.KernelBuildID != buildID || actual.Modules["hfs_provider"] != buildID {
		t.Fatal("probe lost actual installed/running measurements")
	}
	write("boot/kernel", []byte("altered artifact"))
	if output, err := run(false); err == nil {
		t.Fatalf("changed installed kernel accepted: %s", output)
	}
	// Phase refresh uses the original full installed admission, measures current
	// running notes/nonce/inventory, and does not rehash a large on-disk image.
	if output, err := run(true); err != nil {
		t.Fatalf("phase running-note refresh: %s: %v", output, err)
	}
	note[len(note)-1] ^= 1
	write("sys/module/hfs_provider/notes/.note.gnu.build-id", note)
	if output, err := run(true); err == nil {
		t.Fatalf("changed running module accepted: %s", output)
	}
}

func TestAdmissionHighWaterRejectsOldRevisionReopen(t *testing.T) {
	scope := guestbridge.Context{Session: testUUID(3).String()}
	a := &Admission{installed: installedObservation{Admission: kernelAdmission{Observations: []kernelDomainObservation{{Scope: scope, Revision: 7, Fence: 3, Phase: "STOPPED"}}}}, stopped: map[string]uint64{scope.Session: 7}}
	next := installedObservation{Admission: kernelAdmission{Observations: []kernelDomainObservation{{Scope: scope, Revision: 7, Fence: 3, Phase: "REOPENED"}}}}
	if err := a.advance(next); err == nil {
		t.Fatal("old-revision replies could adopt after reopen")
	}
	next.Admission.Observations[0].Revision = 8
	if err := a.advance(next); err != nil {
		t.Fatal(err)
	}
}
