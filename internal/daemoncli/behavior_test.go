package daemoncli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/snonux/homelabfs/internal/behavior"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func behaviorFixture(t *testing.T) (behaviorLayout, string) {
	t.Helper()
	base := t.TempDir()
	l := behaviorLayout{DataRoot: filepath.Join(base, "data"), JournalRoot: filepath.Join(base, "journal"), ControlRoot: filepath.Join(base, "control")}
	mount := filepath.Join(base, "mount")
	for _, p := range append(l.roots(), mount) {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return l, mount
}
func nonemptyFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	if err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > 0 {
				count++
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return count
}
func TestBehaviorThreeRootStorageAndReplay(t *testing.T) {
	l, mount := behaviorFixture(t)
	c, err := prepareBehavior(t.Context(), l, mount, true)
	if err != nil {
		t.Fatal(err)
	}
	local, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if local != nil {
			if err := local.close(); err != nil {
				t.Error(err)
			}
		}
	})
	firstGeneration := local.service.FrontendInfo().Generation
	root := local.service.FrontendInfo().Root
	file := send(t, local, fsapi.Command{Operation: fsapi.Create, Parent: root, Name: "synthetic", Mode: 0600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	send(t, local, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("acknowledged three-root replay")})
	for i, want := range []string{l.JournalRoot, l.DataRoot, l.DataRoot, l.JournalRoot, l.ControlRoot} {
		if !containsPath(want, local.paths[i]) || nonemptyFiles(t, local.paths[i]) == 0 {
			t.Fatalf("root %d not actually used at %s", i, local.paths[i])
		}
	}
	for _, forbidden := range []string{filepath.Join(c.VolumeDir, "source"), filepath.Join(filepath.Dir(local.paths[2]), "journal"), filepath.Join(filepath.Dir(local.paths[2]), "progress")} {
		if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
			t.Fatalf("unexpected default-layout path %s: %v", forbidden, err)
		}
	}
	if err := local.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := prepareBehavior(t.Context(), l, mount, false)
	if err != nil {
		t.Fatal(err)
	}
	local, err = openLocal(t.Context(), reopened)
	if err != nil {
		t.Fatal(err)
	}
	if local.service.FrontendInfo().Generation == firstGeneration {
		t.Fatal("reused serving generation")
	}
	found := send(t, local, fsapi.Command{Operation: fsapi.Lookup, Parent: local.service.FrontendInfo().Root, Name: "synthetic"})
	if found.Attr.Object != file.Attr.Object {
		t.Fatal("logical identity changed on replay")
	}
	opened := send(t, local, fsapi.Command{Operation: fsapi.Open, Object: found.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	got := send(t, local, fsapi.Command{Operation: fsapi.Read, Handle: opened.Handle, Size: 100})
	if string(got.Data) != "acknowledged three-root replay" {
		t.Fatal("lost bytes", string(got.Data))
	}
}
func TestBehaviorRootMappingRefusals(t *testing.T) {
	for _, scenario := range []string{"overlap", "symlink", "nonempty", "foreign-marker", "changed-path", "initialize-again"} {
		t.Run(scenario, func(t *testing.T) {
			l, mount := behaviorFixture(t)
			switch scenario {
			case "overlap":
				l.JournalRoot = l.DataRoot
			case "symlink":
				if err := os.Rename(l.DataRoot, l.DataRoot+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(l.DataRoot+"-saved", l.DataRoot); err != nil {
					t.Fatal(err)
				}
			case "nonempty":
				if err := os.WriteFile(filepath.Join(l.JournalRoot, "keep"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				if _, err := prepareBehavior(t.Context(), l, mount, true); err != nil {
					t.Fatal(err)
				}
				if scenario == "initialize-again" {
					if _, err := prepareBehavior(t.Context(), l, mount, true); err == nil {
						t.Fatal("reinitialized fixture")
					}
					return
				}
				if scenario == "changed-path" {
					l.DataRoot = l.DataRoot + "-foreign"
					if err := os.Mkdir(l.DataRoot, 0700); err != nil {
						t.Fatal(err)
					}
				} else {
					var marker behaviorLayout
					p := filepath.Join(l.JournalRoot, "fixture.json")
					if err := readJSON(p, &marker); err != nil {
						t.Fatal(err)
					}
					marker.VolumeID = "11111111-1111-4111-8111-111111111111"
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
					if err := writeNewJSON(p, marker); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := prepareBehavior(t.Context(), l, mount, false); err == nil {
					t.Fatal("foreign root accepted")
				}
				return
			}
			if _, err := prepareBehavior(t.Context(), l, mount, true); err == nil {
				t.Fatal("invalid layout accepted")
			}
		})
	}
}
func TestBehaviorCommandPreflight(t *testing.T) {
	l, mount := behaviorFixture(t)
	binary := filepath.Join(filepath.Dir(l.DataRoot), "helper")
	data := []byte("synthetic artifact; never executed")
	if err := os.WriteFile(binary, data, 0500); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	role := "unprivileged"
	if os.Geteuid() == 0 {
		role = "root"
	}
	o := behaviorOptions{layout: l, mountpoint: mount, binary: binary, digest: hex.EncodeToString(digest[:]), profile: "hfs-direct", role: role, jsonPath: filepath.Join(filepath.Dir(binary), "report.json"), xmlPath: filepath.Join(filepath.Dir(binary), "report.xml"), initialize: true}
	if err := o.validateInputs(); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"digest", "profile", "inside-report", "symlink-binary", "writable-binary"} {
		t.Run(scenario, func(t *testing.T) {
			next := o
			switch scenario {
			case "digest":
				next.digest = strings.Repeat("0", 64)
			case "role":
				if role == "root" {
					next.role = "unprivileged"
				} else {
					next.role = "root"
				}
			case "profile":
				next.profile = "filesystem"
			case "inside-report":
				next.jsonPath = filepath.Join(l.JournalRoot, "report")
			case "symlink-binary":
				next.binary = binary + "-link"
				if err := os.Symlink(binary, next.binary); err != nil {
					t.Fatal(err)
				}
			case "writable-binary":
				if err := os.Chmod(binary, 0777); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = os.Chmod(binary, 0500) }()
			}
			if err := next.validateInputs(); err == nil {
				t.Fatal("bad fixture inputs accepted")
			}
		})
	}
	var out bytes.Buffer
	if err := Run(t.Context(), []string{"behavior", "--initialize"}, &out, &out); err == nil {
		t.Fatal("missing disposable admission accepted")
	}
	for _, root := range l.roots() {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatal("preflight mutated root", root, err)
		}
	}
}

func TestBehaviorRoleRequiresOwnedRootDaemon(t *testing.T) {
	for _, role := range []string{"root", "unprivileged"} {
		if err := validateBehaviorRole(role, 0); err != nil {
			t.Fatal(role, err)
		}
		if err := validateBehaviorRole(role, 1001); err == nil {
			t.Fatal("unprivileged daemon admitted", role)
		}
	}
	if err := validateBehaviorRole("", 0); err == nil {
		t.Fatal("implicit helper role")
	}
}

func TestBehaviorFixtureHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "fixture-helper" {
		t.Skip("private owned-helper entry point")
	}
	if err := behavior.Helper(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}
func TestBehaviorPrepublicationReadyBarrier(t *testing.T) {
	p, err := fuseadapter.NewParticipants()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	var ids [3]fsapi.UUID
	for i := range ids {
		id, err := freshID()
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	if err := p.BindMount(fuseadapter.MountIdentity{Volume: ids[0], Generation: ids[1], Session: ids[2]}); err != nil {
		t.Fatal(err)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spawn := func(ctx context.Context, in io.Reader, out io.Writer, cred *syscall.Credential) (behavior.Process, error) {
		return p.Start(ctx, fuseadapter.ParticipantSpec{Executable: bin, Args: []string{"-test.run=^TestBehaviorFixtureHelper$", "fixture-helper"}, Env: os.Environ(), Credentials: cred, Stdin: in, Stdout: out, Stderr: os.Stderr})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	child, err := behavior.Start(ctx, spawn, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := child.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	if err := readyBehaviorHelper(ctx, child); err != nil {
		t.Fatal(err)
	}
	// Ready touches no target; a bound operation still refuses absent enrollment.
	reply, err := child.Call(ctx, behavior.Command{Op: "namespace"})
	if err != nil || reply.Error == "" {
		t.Fatal("idle helper touched an unenrolled target", reply, err)
	}
}
func TestBehaviorNormalUnmountBoundedAndPreserved(t *testing.T) {
	l, mount := behaviorFixture(t)
	c, err := prepareBehavior(t.Context(), l, mount, true)
	if err != nil {
		t.Fatal(err)
	}
	local, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := local.close(); err != nil {
			t.Error(err)
		}
	}()
	lifetime := &mountLifetime{local: local, retained: true}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	release, finished := make(chan struct{}), make(chan struct{})
	err = lifetime.unmount(func() error {
		return boundedBehaviorUnmount(ctx, func() error { defer close(finished); <-release; return nil })
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		close(release)
		t.Fatal("uncertain normal unmount reported success", err)
	}
	if !lifetime.retained {
		close(release)
		t.Fatal("timeout dropped resource retention")
	}
	if err := lifetime.close(); err != nil {
		close(release)
		t.Fatal(err)
	}
	if other, err := openLocal(t.Context(), c); err == nil {
		_ = other.close()
		close(release)
		t.Fatal("uncertain unmount released source writer")
	}
	close(release)
	<-finished
	if !lifetime.retained {
		t.Fatal("late normal completion released uncertain resources")
	}
}
