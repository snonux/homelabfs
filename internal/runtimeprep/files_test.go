//go:build linux

package runtimeprep

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeStrictRefusals(t *testing.T) {
	type schema struct {
		Name  string
		Inner struct{ Value int }
		Tag   string `json:"tagged"`
	}
	var ok schema
	if err := decodeStrict([]byte(`{"Name":"a","Inner":{"Value":1},"tagged":"x"}`+"\n"), &ok); err != nil || ok.Tag != "x" {
		t.Fatalf("valid document refused: %v", err)
	}
	for name, raw := range map[string]string{
		"duplicate":        `{"Name":"a","Name":"b"}`,
		"nested duplicate": `{"Inner":{"Value":1,"Value":2}}`,
		"alias":            `{"name":"a"}`,
		"tag alias":        `{"Tag":"x"}`,
		"unknown":          `{"Other":1}`,
		"nested unknown":   `{"Inner":{"Other":1}}`,
		"trailing":         `{"Name":"a"} {}`,
		"wrong type":       `{"Name":1}`,
		"not utf8":         "{\"Name\":\"\xff\"}",
		"deep":             strings.Repeat("[", 40) + strings.Repeat("]", 40),
	} {
		var s schema
		if err := decodeStrict([]byte(raw), &s); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestReadPrivateRefusals(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good.json")
	if err := os.WriteFile(good, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivate(good, 16); err != nil {
		t.Fatal(err)
	}
	wide := filepath.Join(root, "wide.json")
	link := filepath.Join(root, "link.json")
	hard := filepath.Join(root, "hard.json")
	if err := os.WriteFile(wide, []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(wide, hard); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{wide, link, hard, root, "relative.json", root + "/../x"} {
		if _, err := readPrivate(path, 16); err == nil {
			t.Fatalf("%s accepted", path)
		}
	}
	if _, err := readPrivate(good, 1); err == nil {
		t.Fatal("oversized file accepted")
	}
}

func TestWriteFreshDirectoryNeverReuses(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "out")
	if err := writeFreshDirectory(output, map[string][]byte{"a": []byte("1"), "keys/b": []byte("2")}); err != nil {
		t.Fatal(err)
	}
	if err := writeFreshDirectory(output, map[string][]byte{"a": []byte("1")}); err == nil {
		t.Fatal("existing output directory reused")
	}
	if err := writeFreshDirectory(filepath.Join(root, "escape"), map[string][]byte{"../x": nil}); err == nil {
		t.Fatal("escaping name accepted")
	}
	if err := writeExclusive(filepath.Join(output, "a"), []byte("2")); err == nil {
		t.Fatal("existing file replaced")
	}
}

func TestRunUsageRefusals(t *testing.T) {
	root := t.TempDir()
	spec := filepath.Join(root, "spec.json")
	raw, err := os.ReadFile(filepath.Join("testdata", "zq3-spec.json"))
	if err == nil {
		err = os.WriteFile(spec, raw, 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"none":        nil,
		"unknown":     {"sign"},
		"missing":     {"generate", "--spec", spec},
		"positional":  {"generate", "--spec", spec, "--output", filepath.Join(root, "o"), "extra"},
		"bad flag":    {"bind-request", "--operator-key", "/x"},
		"relative":    {"generate", "--spec", "spec.json", "--output", filepath.Join(root, "o")},
		"no template": {"enroll-request", "--template", filepath.Join(root, "missing.json"), "--proposal", spec, "--boot-id", "b", "--provider-nonce", "n", "--device-names", "virtio1,virtio2", "--observer", spec, "--output", filepath.Join(root, "r.json")},
	} {
		var stdout bytes.Buffer
		if err := Run(context.Background(), args, &stdout); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

// TestRunGenerateCLI writes a bundle through the command entry point and
// refuses to overwrite it.
func TestRunGenerateCLI(t *testing.T) {
	root := t.TempDir()
	s := exampleSpec(t)
	operatorFixture(t, root, &s)
	spec := filepath.Join(root, "spec.json")
	writePrivateJSON(t, spec, s)
	output := filepath.Join(root, "bundle")
	var stdout bytes.Buffer
	if err := Run(t.Context(), []string{"generate", "--spec", spec, "--output", output}, &stdout); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"OperatorPrivateKeyRead": false`) {
		t.Fatalf("summary missing: %s", stdout.String())
	}
	if err := Run(t.Context(), []string{"generate", "--spec", spec, "--output", output}, &stdout); err == nil {
		t.Fatal("existing bundle overwritten")
	}
}
