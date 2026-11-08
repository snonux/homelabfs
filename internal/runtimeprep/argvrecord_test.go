//go:build linux

package runtimeprep

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledruntime"
)

// argvPy is the G4 record generator this package must agree with.
var argvPy = filepath.Join("..", "..", "tools", "lab", "directlaunch", "argv.py")

// g4Record builds a genuine record with argv.py's own pure build_record
// (launch facts are the declared boundary: fixed vars/facts pins stand in).
const g4Record = `import importlib.util, sys
spec = importlib.util.spec_from_file_location("argv", sys.argv[1])
argv = importlib.util.module_from_spec(spec)
spec.loader.exec_module(argv)
pins = {"vars_sha256": "ab" * 32, "vars_bytes": 131072, "timestamp": "2026-11-08T18:10:00Z"}
record = argv.build_record("zq3-r1", sys.argv[2:4], "tap100", pins, "cd" * 32)
sys.stdout.buffer.write(argv.canonical(record))
`

func python(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable; the G4 cross-check needs argv.py")
	}
	return path
}

func buildG4Record(t *testing.T, sessions ...string) []byte {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), python(t), append([]string{"-I", "-B", "-c", g4Record, argvPy}, sessions...)...).Output()
	if err != nil {
		t.Fatalf("argv.py build_record: %v", err)
	}
	return out
}

type argvFixture struct {
	output, record, pin string
	config              controlledruntime.RuntimeConfig
	generation          Record
	raw                 []byte
}

func newArgvFixture(t *testing.T) argvFixture {
	t.Helper()
	root := shortRoot(t)
	s := exampleSpec(t)
	operatorFixture(t, root, &s)
	bundle, output := generateBundle(t, root, s)
	f := argvFixture{output: output, generation: bundle.Record, record: filepath.Join(root, "bhyve-argv.json")}
	readBundleJSON(t, output, ConfigFile, &f.config)
	f.raw = buildG4Record(t, bundle.Record.Sessions[0].Session, bundle.Record.Sessions[1].Session)
	f.pin = sha256Hex(f.raw)
	if err := os.WriteFile(f.record, f.raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestCheckArgvRecordAgreesWithG4 follows the documented order: generate,
// argv.py record from the generated sessions, then check-argv and the actual
// argv.py verify on the generated runtime config.
func TestCheckArgvRecordAgreesWithG4(t *testing.T) {
	f := newArgvFixture(t)
	var stdout bytes.Buffer
	run(t, []string{"check-argv", "--bundle", f.output, "--record", f.record, "--record-sha256", f.pin}, &stdout)
	if !strings.Contains(stdout.String(), `"verified": true`) {
		t.Fatalf("check-argv output: %s", stdout.String())
	}
	verify := exec.CommandContext(t.Context(), python(t), "-I", "-B", argvPy, "verify", "--record", f.record,
		"--record-sha256", f.pin, "--runtime-config", filepath.Join(f.output, ConfigFile))
	if out, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("argv.py verify refused the generated config: %v %s", err, out)
	}
}

func TestCheckArgvRecordRefusals(t *testing.T) {
	f := newArgvFixture(t)
	swapped := buildG4Record(t, f.generation.Sessions[1].Session, f.generation.Sessions[0].Session)
	tampered := bytes.Replace(f.raw, []byte(`"args_sha256":"`), []byte(`"args_sha256":"0`), 1)
	cases := map[string]func(*controlledruntime.RuntimeConfig, *Record, *[]byte, *string){
		"pin": func(_ *controlledruntime.RuntimeConfig, _ *Record, _ *[]byte, p *string) {
			*p = strings.Repeat("0", 64)
		},
		"swapped session": func(_ *controlledruntime.RuntimeConfig, _ *Record, raw *[]byte, p *string) {
			*raw, *p = swapped, sha256Hex(swapped)
		},
		"args_sha256": func(_ *controlledruntime.RuntimeConfig, _ *Record, raw *[]byte, p *string) {
			*raw, *p = tampered, sha256Hex(tampered)
		},
		"unknown key": func(_ *controlledruntime.RuntimeConfig, _ *Record, raw *[]byte, p *string) {
			*raw = append([]byte(`{"extra":1,`), (*raw)[1:]...)
			*p = sha256Hex(*raw)
		},
		"changed arg": func(c *controlledruntime.RuntimeConfig, _ *Record, _ *[]byte, _ *string) {
			c.Exporter.Args[7] = "tap101"
		},
		"extra arg": func(c *controlledruntime.RuntimeConfig, _ *Record, _ *[]byte, _ *string) {
			c.Exporter.Args = append(c.Exporter.Args, "-S")
		},
		"reordered arg": func(c *controlledruntime.RuntimeConfig, _ *Record, _ *[]byte, _ *string) {
			a := c.Exporter.Args
			a[1], a[3] = a[3], a[1] // two different -s devices swap places
		},
		"exporter path": func(c *controlledruntime.RuntimeConfig, _ *Record, _ *[]byte, _ *string) {
			c.Exporter.Path = "/usr/sbin/bhyve"
		},
		"exporter hash": func(c *controlledruntime.RuntimeConfig, _ *Record, _ *[]byte, _ *string) { c.Exporter.SHA256[0] ^= 1 },
		"run id":        func(_ *controlledruntime.RuntimeConfig, r *Record, _ *[]byte, _ *string) { r.RunID = "zq3-r2" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			config, generation, raw, pin := f.config, f.generation, f.raw, f.pin
			config.Exporter.Args = append([]string(nil), f.config.Exporter.Args...)
			mutate(&config, &generation, &raw, &pin)
			_, err := CheckArgvRecord(config, generation, raw, pin)
			if err == nil {
				t.Fatal("mismatched argv record accepted")
			}
			t.Log(err)
		})
	}
}
