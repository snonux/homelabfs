package daemoncli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func fixtureConfig(t *testing.T) (Config, string) {
	t.Helper()
	base := t.TempDir()
	n, err := initNode(t.Context(), filepath.Join(base, "control"))
	if err != nil {
		t.Fatal(err)
	}
	volume, err := freshID()
	if err != nil {
		t.Fatal(err)
	}
	c := Config{FormatVersion: 1, NodeID: n.NodeID, VolumeID: volume.String(), ControlDir: filepath.Join(base, "control"), VolumeDir: filepath.Join(base, "volume"), Mountpoint: filepath.Join(base, "mount")}
	if err := os.Mkdir(c.Mountpoint, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(base, "config.json")
	if err := writeNewJSON(name, c); err != nil {
		t.Fatal(err)
	}
	return c, name
}

func TestCLIInitializeInspectAndRefusals(t *testing.T) {
	c, name := fixtureConfig(t)
	var out, logs bytes.Buffer
	if err := Run(t.Context(), []string{"volume-init", "--config", name, "--disposable-validation"}, &out, &logs); err != nil {
		t.Fatal(err)
	}
	var v volumeIdentity
	if err := json.Unmarshal(out.Bytes(), &v); err != nil || v.VolumeID != c.VolumeID {
		t.Fatal(err, out.String())
	}
	before, err := os.ReadFile(filepath.Join(c.VolumeDir, "volume.json"))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(t.Context(), []string{"inspect", "--config", name}, &out, &logs); err != nil || !strings.Contains(out.String(), c.VolumeID) {
		t.Fatal(err, out.String())
	}
	for _, args := range [][]string{
		{"node-init", "--control", c.ControlDir},
		{"volume-init", "--config", name, "--disposable-validation"},
		{"volume-init", "--config", name},
		{"mount", "--config", name},
		{"serve", "--config", name, "--disposable-validation"},
		{"inspect", "--config", name, "extra"},
		{"inspect", "--config", name, "--self-client"},
		{"mount", "--config", name, "--disposable-validation", "--self-client", "--allow-other"},
	} {
		if err := Run(t.Context(), args, &out, &logs); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	after, err := os.ReadFile(filepath.Join(c.VolumeDir, "volume.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refusal changed existing manifest", err)
	}
	entries, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
	if err != nil || len(entries) != 0 {
		t.Fatal("normal-mode refusal created workspace", err, entries)
	}
}

func TestConfigurationAndIdentityNegatives(t *testing.T) {
	for _, scenario := range []string{"wrong-node", "foreign-volume", "node-version", "volume-version", "duplicate-key", "unknown-key", "trailing-json", "invalid-utf8", "config-permissions", "symlink-volume", "symlink-source", "symlink-manifest", "overlap", "relative-path", "unclean-path"} {
		t.Run(scenario, func(t *testing.T) {
			c, name := fixtureConfig(t)
			if _, err := initVolume(t.Context(), c); err != nil {
				t.Fatal(err)
			}
			write := func(name, data string) {
				t.Helper()
				if err := os.WriteFile(name, []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "wrong-node":
				c.NodeID = "11111111-1111-4111-8111-111111111111"
			case "foreign-volume":
				c.VolumeID = "22222222-2222-4222-8222-222222222222"
			case "node-version":
				write(filepath.Join(c.ControlDir, "node.json"), `{"format_version":2,"node_id":"`+c.NodeID+`"}`)
			case "volume-version":
				p := filepath.Join(c.VolumeDir, "volume.json")
				raw, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				write(p, strings.Replace(string(raw), `"format_version": 1`, `"format_version": 2`, 1))
			case "duplicate-key", "unknown-key", "trailing-json", "invalid-utf8":
				raw, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				extra := `"format_version":1,`
				if scenario == "unknown-key" {
					extra = `"unsafe":true,`
				}
				switch scenario {
				case "invalid-utf8":
					write(name, strings.Replace(string(raw), `"mountpoint"`, string([]byte{'"', 0xff, '"'}), 1))
				case "trailing-json":
					write(name, string(raw)+`{}`)
				default:
					write(name, "{"+extra+string(raw[1:]))
				}
			case "config-permissions":
				if err := os.Chmod(name, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink-volume", "symlink-source", "symlink-manifest":
				p := c.VolumeDir
				if scenario == "symlink-source" {
					p = filepath.Join(p, "source")
				}
				if scenario == "symlink-manifest" {
					p = filepath.Join(p, "volume.json")
				}
				if err := os.Rename(p, p+"-saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+"-saved", p); err != nil {
					t.Fatal(err)
				}
			case "overlap":
				c.Mountpoint = filepath.Join(c.VolumeDir, "workspaces")
			case "relative-path":
				c.VolumeDir = "volume"
			case "unclean-path":
				c.VolumeDir += "/../volume"
			}
			if scenario != "duplicate-key" && scenario != "unknown-key" && scenario != "trailing-json" && scenario != "invalid-utf8" && scenario != "config-permissions" {
				raw, err := json.Marshal(c)
				if err != nil {
					t.Fatal(err)
				}
				write(name, string(raw))
			}
			var out bytes.Buffer
			if err := Run(t.Context(), []string{"inspect", "--config", name}, &out, &out); err == nil {
				t.Fatal("accepted invalid configuration or identity")
			}
		})
	}
}

func TestCLIRejectsCaseVariantSchemaKeys(t *testing.T) {
	for _, schema := range []string{"configuration", "node", "volume"} {
		for _, scenario := range []string{"uppercase-only", "mixed-case-version-override"} {
			t.Run(schema+"/"+scenario, func(t *testing.T) {
				c, configPath := fixtureConfig(t)
				if _, err := initVolume(t.Context(), c); err != nil {
					t.Fatal(err)
				}
				name := configPath
				switch schema {
				case "node":
					name = filepath.Join(c.ControlDir, "node.json")
				case "volume":
					name = filepath.Join(c.VolumeDir, "volume.json")
				}
				raw, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				replacement := `"FORMAT_VERSION": 1`
				if scenario == "mixed-case-version-override" {
					replacement = `"format_version": 2, "FORMAT_VERSION": 1`
				}
				changed := strings.Replace(string(raw), `"format_version": 1`, replacement, 1)
				if err := os.WriteFile(name, []byte(changed), 0o600); err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				err = Run(t.Context(), []string{"inspect", "--config", configPath}, &output, &output)
				if err == nil || !strings.Contains(err.Error(), `unknown JSON key "FORMAT_VERSION"`) {
					t.Fatalf("case variant must fail at schema admission: %v", err)
				}
			})
		}
	}
}

func TestCLIRejectsCaseVariantPathOverride(t *testing.T) {
	c, configPath := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	validPath, err := json.Marshal(c.VolumeDir)
	if err != nil {
		t.Fatal(err)
	}
	// A case-insensitive decoder would silently replace the invalid lowercase
	// path with the valid uppercase alias and inspect the initialized volume.
	changed := strings.Replace(string(raw), `"volume_dir": `+string(validPath), `"volume_dir": "/unselected-disposable-volume", "VOLUME_DIR": `+string(validPath), 1)
	if err := os.WriteFile(configPath, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Run(t.Context(), []string{"inspect", "--config", configPath}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), `unknown JSON key "VOLUME_DIR"`) {
		t.Fatalf("case variant path override must fail at schema admission: %v", err)
	}
}

func TestInterruptedInitPreservesContents(t *testing.T) {
	c, _ := fixtureConfig(t)
	if err := os.Mkdir(c.VolumeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(c.VolumeDir, "unrecognized-state")
	if err := os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := initVolume(t.Context(), c); !errors.Is(err, os.ErrExist) {
		t.Fatal("expected exclusive creation refusal", err)
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "preserve" {
		t.Fatal("changed interrupted state", err)
	}
}

func TestCanceledInitAndVersion(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := filepath.Join(t.TempDir(), "node")
	if _, err := initNode(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled initialization created root", err)
	}
	var output bytes.Buffer
	if err := Run(t.Context(), []string{"-version"}, &output, &output); err != nil || output.Len() == 0 {
		t.Fatal(err)
	}
}

func send(t *testing.T, local *localService, cmd fsapi.Command) fsapi.Result {
	t.Helper()
	cmd.Mount = local.service.FrontendInfo().Volume
	if cmd.Handle != 0 {
		cmd.Generation = local.service.FrontendInfo().Generation
	}
	r := fsapi.NewRequest(fsapi.Cred{UID: uint32(os.Geteuid()), GID: uint32(os.Getegid())})
	if err := local.service.Dispatch(t.Context(), r, cmd); err != nil {
		t.Fatal(err)
	}
	result, err := r.Completion().Wait(t.Context())
	if err != nil || result.Code != fsapi.OK {
		t.Fatal(err, result.Code)
	}
	return result
}

func TestReplayExclusiveWriterAndPermanentReservations(t *testing.T) {
	c, _ := fixtureConfig(t)
	if _, err := initVolume(t.Context(), c); err != nil {
		t.Fatal(err)
	}
	l, err := openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.close() })
	oldGeneration := l.service.FrontendInfo().Generation
	file := send(t, l, fsapi.Command{Operation: fsapi.Create, Parent: l.service.FrontendInfo().Root, Name: "retained", Mode: 0o600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
	send(t, l, fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("durable synthetic bytes")})
	if _, err := openLocal(t.Context(), c); err == nil {
		t.Fatal("concurrent source writer admitted")
	}
	before, err := os.ReadDir(filepath.Join(c.VolumeDir, "reservations"))
	if err != nil || len(before) != 1 {
		t.Fatal(err, before)
	}
	if err := l.close(); err != nil {
		t.Fatal(err)
	}
	l, err = openLocal(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	if l.service.FrontendInfo().Generation == oldGeneration {
		t.Fatal("reused replay generation")
	}
	look := send(t, l, fsapi.Command{Operation: fsapi.Lookup, Parent: l.service.FrontendInfo().Root, Name: "retained"})
	if look.Attr.Object != file.Attr.Object {
		t.Fatal("replay changed allocation identity")
	}
	handle := send(t, l, fsapi.Command{Operation: fsapi.Open, Object: look.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	got := send(t, l, fsapi.Command{Operation: fsapi.Read, Handle: handle.Handle, Size: 100})
	if string(got.Data) != "durable synthetic bytes" {
		t.Fatal("replayed bytes", string(got.Data))
	}
	after, err := os.ReadDir(filepath.Join(c.VolumeDir, "reservations"))
	if err != nil || len(after) != len(before) || after[0].Name() != before[0].Name() {
		t.Fatal("lost permanent reservation", err)
	}
	workspaces, err := os.ReadDir(filepath.Join(c.VolumeDir, "workspaces"))
	if err != nil || len(workspaces) != 2 {
		t.Fatal("old replay workspace not preserved", err, workspaces)
	}
}
