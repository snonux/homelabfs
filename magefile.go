//go:build mage

// Package main provides local Mage targets for HomelabFS.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const binaryName = "homelabfs"

// Default builds both supported daemon targets.
func Default() { mg.Deps(Build) }

// Build cross-compiles static Linux and FreeBSD amd64 binaries into bin/.
func Build() error {
	for _, target := range []string{"linux", "freebsd"} {
		dir := filepath.Join("bin", target+"-amd64")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create build directory: %w", err)
		}
		env := map[string]string{"GOOS": target, "GOARCH": "amd64", "CGO_ENABLED": "0"}
		if err := sh.RunWithV(env, "go", "build", "-o", filepath.Join(dir, binaryName), "./cmd/homelabfs"); err != nil {
			return fmt.Errorf("build %s: %w", target, err)
		}
	}
	return nil
}

// Test runs local Go unit tests only. Each invocation requires operator approval.
func Test() error { return sh.RunV("go", "test", "./...") }

// Vet runs Go static analysis on the root module.
func Vet() error { return sh.RunV("go", "vet", "./...") }

// Lint runs golangci-lint when installed.
func Lint() error {
	if _, err := exec.LookPath("golangci-lint"); err != nil {
		if !errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("find golangci-lint: %w", err)
		}
		fmt.Println("golangci-lint is not installed; lint skipped")
		return nil
	}
	return sh.RunV("golangci-lint", "run", "./...")
}

// FuzzShort runs each root-module fuzz target for five seconds with one worker.
// Each invocation requires operator approval; lab tests are separate.
func FuzzShort() error {
	packages, err := sh.Output("go", "list", "./...")
	if err != nil {
		return fmt.Errorf("list packages: %w", err)
	}
	found := false
	for _, pkg := range strings.Fields(packages) {
		listing, err := sh.Output("go", "test", pkg, "-list", "^Fuzz")
		if err != nil {
			return fmt.Errorf("list fuzz targets in %s: %w", pkg, err)
		}
		for _, name := range strings.Fields(listing) {
			if !strings.HasPrefix(name, "Fuzz") {
				continue
			}
			found = true
			if err := sh.RunV("go", "test", pkg, "-run", "^$", "-fuzz", "^"+name+"$", "-fuzztime", "5s", "-parallel", "1"); err != nil {
				return fmt.Errorf("fuzz %s/%s: %w", pkg, name, err)
			}
		}
	}
	if !found {
		fmt.Println("No fuzz targets yet")
	}
	return nil
}
