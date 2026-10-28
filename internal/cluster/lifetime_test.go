package cluster

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

type faultDisk struct {
	lifetimeDisk
	event     string
	triggered bool
}

type faultFile struct {
	vfs.File
	disk        *faultDisk
	incarnation bool
}

func TestLifetimeFreshDurableAndExclusive(t *testing.T) {
	dir := newControl(t)
	first, err := Start(context.Background(), dir, testID(1), testID(2))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Error(err)
		}
	})
	id := first.Identity()
	if id.Cluster != testID(1) || id.Node != testID(2) || !id.Incarnation.Valid() {
		t.Fatal(id)
	}
	if _, err := Start(context.Background(), dir, testID(1), testID(2)); err == nil {
		t.Fatal("concurrent start admitted")
	}
	assertIncarnation(t, dir, id)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Start(context.Background(), dir, testID(1), testID(2))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := second.Close(); err != nil {
			t.Error(err)
		}
	}()
	if second.Identity().Incarnation == id.Incarnation {
		t.Fatal("restart reused incarnation")
	}
	assertIncarnation(t, dir, id)
	assertIncarnation(t, dir, second.Identity())
}

func TestLifetimeRefusesWrongIdentityAndPrivatePaths(t *testing.T) {
	cases := []struct {
		name          string
		setup         func(*testing.T, string)
		cluster, node fsapi.UUID
	}{
		{"node mismatch", func(*testing.T, string) {}, testID(1), testID(3)},
		{"zero cluster", func(*testing.T, string) {}, fsapi.UUID{}, testID(2)},
		{"public control", func(t *testing.T, d string) {
			if err := os.Chmod(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}, testID(1), testID(2)},
		{"public identity", func(t *testing.T, d string) {
			if err := os.Chmod(filepath.Join(d, "node.json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, testID(1), testID(2)},
		{"duplicate identity field", func(t *testing.T, d string) {
			writeTestFile(t, filepath.Join(d, "node.json"), `{"format_version":1,"node_id":"`+testID(2).String()+`","node_id":"`+testID(2).String()+`"}`)
		}, testID(1), testID(2)},
		{"corrupt identity", func(t *testing.T, d string) { writeTestFile(t, filepath.Join(d, "node.json"), `{`) }, testID(1), testID(2)},
		{"corrupt cluster", func(t *testing.T, d string) { writeTestFile(t, filepath.Join(d, "cluster.json"), `{`) }, testID(1), testID(2)},
		{"public incarnations", func(t *testing.T, d string) {
			if err := os.Mkdir(filepath.Join(d, "incarnations"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, testID(1), testID(2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newControl(t)
			tc.setup(t, dir)
			if l, err := Start(context.Background(), dir, tc.cluster, tc.node); err == nil {
				_ = l.Close()
				t.Fatal("accepted invalid lifecycle")
			}
		})
	}
	dir := newControl(t)
	l, err := Start(context.Background(), dir, testID(1), testID(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), dir, testID(90), testID(2)); err == nil {
		t.Fatal("rebound control directory to foreign cluster")
	}
	l, err = Start(context.Background(), dir, testID(1), testID(2))
	if err != nil {
		t.Fatal("failed start leaked lock:", err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(ctx, dir, testID(1), testID(2)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLifetimeUncertainPersistenceNeverReusesID(t *testing.T) {
	for _, event := range []string{"write", "file-sync", "file-close", "dir-sync"} {
		t.Run(event, func(t *testing.T) {
			dir := newControl(t)
			disk, err := vfs.NewOS(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := disk.Close(); err != nil {
					t.Error(err)
				}
			}()
			fault := &faultDisk{lifetimeDisk: disk, event: event}
			allocate := func() (fsapi.UUID, error) { return testID(20), nil }
			if id, err := persistLifetime(context.Background(), fault, testID(1), testID(2), allocate); err == nil || id.Valid() || !fault.triggered {
				t.Fatalf("failure not propagated: %s %v triggered=%t", id, err, fault.triggered)
			}
			name := filepath.Join(dir, "incarnations", testID(20).String()+".json")
			original, err := os.ReadFile(name)
			if err != nil {
				t.Fatal("uncertain attempt removed:", err)
			}
			if _, err := persistLifetime(context.Background(), disk, testID(1), testID(2), allocate); !errors.Is(err, fs.ErrExist) {
				t.Fatal("reused uncertain incarnation:", err)
			}
			got, err := os.ReadFile(name)
			if err != nil || string(got) != string(original) {
				t.Fatal("uncertain bytes replaced", err)
			}
			id, err := persistLifetime(context.Background(), disk, testID(1), testID(2), func() (fsapi.UUID, error) { return testID(21), nil })
			if err != nil || id != testID(21) {
				t.Fatal(id, err)
			}
		})
	}
}

func TestLifetimeAcrossProcessCrash(t *testing.T) {
	dir := newControl(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := lifetimeProcess(ctx, dir, false)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(out)
	if !scanner.Scan() {
		t.Fatal("child did not persist identity:", scanner.Err())
	}
	prior, err := fsapi.ParseUUID(scanner.Text())
	if err != nil {
		t.Fatal(err)
	}
	// A separate process sees the same OS lock, rather than only a Go mutex.
	if output, err := lifetimeProcess(ctx, dir, true).CombinedOutput(); err != nil {
		t.Fatalf("second process did not refuse: %v %s", err, output)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("crash process exited successfully")
	}
	restarted, err := Start(context.Background(), dir, testID(1), testID(2))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := restarted.Close(); err != nil {
			t.Error(err)
		}
	}()
	if restarted.Identity().Incarnation == prior {
		t.Fatal("process crash reused incarnation")
	}
	assertIncarnation(t, dir, Identity{Cluster: testID(1), Node: testID(2), Incarnation: prior})
}

func TestLifetimeProcessHelper(t *testing.T) {
	dir := os.Getenv("HOMELABFS_LIFETIME_TEST_DIR")
	if dir == "" {
		return
	}
	l, err := Start(context.Background(), dir, testID(1), testID(2))
	if os.Getenv("HOMELABFS_LIFETIME_TEST_REFUSE") == "1" {
		if err == nil {
			_ = l.Close()
			t.Fatal("concurrent process admitted")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := fmt.Fprintln(os.Stdout, l.Identity().Incarnation.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func (d *faultDisk) Open(ctx context.Context, name string, flag int, mode fs.FileMode) (vfs.File, error) {
	f, err := d.lifetimeDisk.Open(ctx, name, flag, mode)
	if err != nil {
		return nil, err
	}
	return &faultFile{File: f, disk: d, incarnation: strings.HasPrefix(name, "incarnations/")}, nil
}

func (d *faultDisk) SyncDir(ctx context.Context, name string) error {
	if err := d.lifetimeDisk.SyncDir(ctx, name); err != nil {
		return err
	}
	if name == "incarnations" {
		return d.fail("dir-sync")
	}
	return nil
}

func (d *faultDisk) fail(event string) error {
	if d.event == event && !d.triggered {
		d.triggered = true
		return errors.New("injected uncertain " + event)
	}
	return nil
}

func (f *faultFile) Stat(ctx context.Context) (vfs.Info, error) {
	return f.File.(vfs.Statter).Stat(ctx)
}

func (f *faultFile) WriteAt(ctx context.Context, b []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(ctx, b, off)
	if err == nil && f.incarnation {
		err = f.disk.fail("write")
	}
	return n, err
}

func (f *faultFile) Sync(ctx context.Context) error {
	if err := f.File.Sync(ctx); err != nil {
		return err
	}
	if f.incarnation {
		return f.disk.fail("file-sync")
	}
	return nil
}

func (f *faultFile) Close() error {
	err := f.File.Close()
	if err == nil && f.incarnation {
		err = f.disk.fail("file-close")
	}
	return err
}

func lifetimeProcess(ctx context.Context, dir string, refuse bool) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLifetimeProcessHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "HOMELABFS_LIFETIME_TEST_DIR="+dir)
	if refuse {
		cmd.Env = append(cmd.Env, "HOMELABFS_LIFETIME_TEST_REFUSE=1")
	}
	return cmd
}

func newControl(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "control")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "node.json"), `{"format_version":1,"node_id":"`+testID(2).String()+`"}`)
	return dir
}

func assertIncarnation(t *testing.T, dir string, id Identity) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "incarnations", id.Incarnation.String()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var m incarnationManifest
	if err := decodeStrict(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.FormatVersion != 1 || m.Cluster != id.Cluster.String() || m.Node != id.Node.String() || m.Incarnation != id.Incarnation.String() {
		t.Fatal(m)
	}
}
