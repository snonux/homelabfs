package behavior

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBehaviorHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "behavior-helper" {
		t.Skip("private subprocess entry point")
	}
	if err := Helper(context.Background(), os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestPlainDirectoryBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path, err := os.MkdirTemp("", "hfs-behavior-baseline-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("run-owned baseline target: %s", path)
	c := Config{Path: path, Profile: "filesystem", RootSkip: "unit baseline uses unchanged current credentials"}
	for range 3 {
		child, err := Start(ctx, ExecutableSpawner(exe, "-test.run=^TestBehaviorHelper$", "behavior-helper"), nil)
		if err != nil {
			t.Fatal(err)
		}
		c.Clients = append(c.Clients, child)
		t.Cleanup(func() { _ = child.Stop(context.Background()) })
	}
	report := Run(ctx, c)
	for _, result := range report.Results {
		t.Logf("%s %s: %s", result.Name, result.Status, result.Detail)
	}
	if report.Failed() {
		t.Fatalf("failed resources retained at %s", path)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := report.JSON(&encoded); err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Results) != len(report.Results) {
		t.Fatal("JSON lost results")
	}
	encoded.Reset()
	if err := report.JUnit(&encoded); err != nil {
		t.Fatal(err)
	}
	var suite junitSuite
	if err := xml.Unmarshal(encoded.Bytes(), &suite); err != nil {
		t.Fatal(err)
	}
	if suite.Failures != 0 || suite.Skipped != 3 || suite.Tests != len(report.Results) {
		t.Fatalf("JUnit outcomes mismatch: %+v", suite)
	}
}

func TestIdentityRejectsPlainDirectory(t *testing.T) {
	path := t.TempDir()
	root, err := openRoot(path, "homelabfs-validation-v1\nwrong\n")
	if err == nil {
		_ = root.Close()
		t.Fatal("plain directory accepted as mounted HomelabFS")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("identity rejection wrote fallback data")
	}
}

func TestProfileRequirements(t *testing.T) {
	for _, c := range []Config{{Path: filepath.Clean("/tmp"), Profile: "hfs-direct"}, {Path: "relative", Profile: "filesystem"}, {Path: "/tmp", Profile: "filesystem", Identity: "claimed"}, {Path: "/tmp", Profile: "unknown"}} {
		c.Clients = make([]*Client, 3)
		r := run{c: c}
		if err := r.validate(); err == nil {
			t.Fatalf("accepted invalid profile %+v", c)
		}
	}
}

func TestReportPreflightDoesNotTouchTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(base, "existing.json")
	if err := os.WriteFile(output, []byte("existing evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := CLI(t.Context(), []string{"-path", target, "-json", output, "-junit", filepath.Join(base, "new.xml")}, &stdout, &stderr); code != 2 {
		t.Fatalf("existing report exit %d", code)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("preflight failure still ran against target")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing evidence" {
		t.Fatal("overwrote existing evidence")
	}
}

func TestReportsStayOutsideTarget(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "alias")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{filepath.Join(target, "report.json"), filepath.Join(link, "report.json")} {
		if err := outsideTarget(target, output); err == nil {
			t.Fatalf("accepted report inside target via %s", output)
		}
	}
}
