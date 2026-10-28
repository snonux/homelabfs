package cluster

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONFile(t *testing.T) {
	name := filepath.Join(t.TempDir(), "cluster.json")
	writeTestFile(t, name, configJSON(t))
	r, err := Load(context.Background(), JSONFile{Path: name})
	if err != nil {
		t.Fatal(err)
	}
	if r.Cluster() != testID(1) || r.Revision() != 1 {
		t.Fatal("registry identity")
	}
	if err := r.CheckAssignment(testID(1), testID(2), testID(5), testConfig().Volumes[0].History, Primary); err != nil {
		t.Fatal(err)
	}
}

func TestJSONFileRejectsAmbiguousSchema(t *testing.T) {
	valid := configJSON(t)
	cases := []struct{ name, raw string }{
		{"duplicate root", strings.Replace(valid, `"format_version":1`, `"format_version":1,"format_version":1`, 1)},
		{"unknown root", strings.Replace(valid, `"format_version":1`, `"extra":1,"format_version":1`, 1)},
		{"case alias", strings.Replace(valid, `"cluster_id"`, `"CLUSTER_ID"`, 1)},
		{"duplicate node", strings.Replace(valid, `"admission":"admitted"`, `"admission":"admitted","admission":"removed"`, 1)},
		{"duplicate member", strings.Replace(valid, `"role":"primary"`, `"role":"primary","role":"async"`, 1)},
		{"unknown member", strings.Replace(valid, `"role":"primary"`, `"role":"primary","bad":false`, 1)},
		{"missing", strings.Replace(valid, `"registry_revision":1,`, "", 1)},
		{"null", strings.Replace(valid, `"nodes":[`, `"nodes":null,"discard":[`, 1)},
		{"null field", strings.Replace(valid, `"registry_revision":1`, `"registry_revision":null`, 1)},
		{"wrong version", strings.Replace(valid, `"format_version":1`, `"format_version":2`, 1)},
		{"float", strings.Replace(valid, `"registry_revision":1`, `"registry_revision":1.0`, 1)},
		{"negative", strings.Replace(valid, `"registry_revision":1`, `"registry_revision":-1`, 1)},
		{"trailing", valid + ` {}`},
		{"UTF8", valid + string([]byte{255})},
		{"noncanonical IPv6", strings.Replace(valid, "2001:db8::3", "2001:0db8::3", 1)},
		{"foreign volume ID", strings.Replace(valid, testID(5).String(), "garbage", 1)},
		{"invalid UUID", strings.Replace(valid, testID(1).String(), "00000000-0000-0000-0000-00000000000A", 1)},
		{"oversized", strings.Repeat(" ", maxConfigBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "config.json")
			writeTestFile(t, name, tc.raw)
			if _, err := (JSONFile{Path: name}).Load(context.Background()); err == nil {
				t.Fatal("accepted invalid JSON")
			}
		})
	}
}

func TestJSONFilePrivateAdmission(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "config.json")
	writeTestFile(t, name, configJSON(t))
	if err := os.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (JSONFile{Path: name}).Load(context.Background()); err == nil {
		t.Fatal("accepted public file")
	}
	if err := os.Chmod(name, 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Link(name, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := (JSONFile{Path: name}).Load(context.Background()); err == nil {
		t.Fatal("accepted hard link")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(name, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := (JSONFile{Path: alias}).Load(context.Background()); err == nil {
		t.Fatal("accepted symlink")
	}
	linkedDir := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, linkedDir); err != nil {
		t.Fatal(err)
	}
	if _, err := (JSONFile{Path: filepath.Join(linkedDir, "config.json")}).Load(context.Background()); err == nil {
		t.Fatal("accepted symlink ancestor")
	}
	if _, err := (JSONFile{Path: "relative.json"}).Load(context.Background()); err == nil {
		t.Fatal("accepted relative path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (JSONFile{Path: name}).Load(ctx); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func configJSON(t *testing.T) string {
	t.Helper()
	c := testConfig()
	raw := fileConfig{FormatVersion: 1, Cluster: c.Cluster.String(), Revision: c.Revision}
	for _, n := range c.Nodes {
		f := fileNode{ID: n.ID.String(), Admission: n.Admission}
		for _, a := range n.Addresses {
			f.Addresses = append(f.Addresses, a.String())
		}
		raw.Nodes = append(raw.Nodes, f)
	}
	for _, v := range c.Volumes {
		f := fileVolume{ID: v.ID.String(), Generation: v.History.Generation.String(), Configuration: v.History.Configuration.String(), Epoch: v.History.Epoch, CoordinatorTerm: v.History.CoordinatorTerm}
		for _, m := range v.Members {
			f.Members = append(f.Members, fileMember{Node: m.Node.String(), Role: m.Role})
		}
		raw.Volumes = append(raw.Volumes, f)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeTestFile(t *testing.T, name, raw string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}
