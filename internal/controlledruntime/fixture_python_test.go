//go:build linux || freebsd

package controlledruntime

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fixedPythonCandidates are the conventional absolute interpreter paths:
// Linux distributions ship /usr/bin/python3, FreeBSD ports install under
// /usr/local/bin. FreeBSD often has only a versioned python3.N binary, which
// versionedPythonCandidates discovers.
var fixedPythonCandidates = []string{"/usr/bin/python3", "/usr/local/bin/python3"}

// versionedPython matches exactly "python3.<minor>", excluding helpers such
// as python3.12-config whose shebang would not run Python.
var versionedPython = regexp.MustCompile(`^python3\.([0-9]+)$`)

// fixturePython returns an absolute python3 interpreter for local process
// fixtures that stand in for the external SSH/guest primitive, or skips the
// test when the host has none.
//
// The observer executes its pinned primitive as /dev/fd/3 with
// PATH=/usr/bin:/bin, so a "#!/usr/bin/env python3" shebang would not find a
// FreeBSD ports interpreter and a hardcoded "#!/usr/bin/python3" does not
// exist on FreeBSD at all (execve then fails with ENOENT for the interpreter,
// which looks like an FD-exec failure). Resolving a real absolute path at
// fixture creation keeps the fixture portable while the production checks
// (pinned hash of the exact script bytes, FD exec, checkDescriptorExec) stay
// unchanged: callers hash the script after embedding this path.
func fixturePython(t *testing.T) string {
	t.Helper()
	candidates := pythonCandidates()
	if python, ok := firstExecutable(candidates); ok {
		return python
	}
	t.Skipf("local process fixture needs an absolute python3 interpreter; tried %s", strings.Join(candidates, ", "))
	return ""
}

// pythonCandidates lists interpreter paths in preference order: the fixed
// conventional paths, then versioned /usr/local/bin/python3.N (newest minor
// first), then whatever python3 PATH lookup yields as an absolute path.
func pythonCandidates() []string {
	candidates := append([]string(nil), fixedPythonCandidates...)
	candidates = append(candidates, versionedPythonCandidates("/usr/local/bin")...)
	if found, err := exec.LookPath("python3"); err == nil {
		if absolute, err := filepath.Abs(found); err == nil {
			candidates = append(candidates, absolute)
		}
	}
	return candidates
}

// versionedPythonCandidates returns directory/python3.N entries ordered by
// descending minor version. A missing or unreadable directory yields none.
func versionedPythonCandidates(directory string) []string {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	type versioned struct {
		path  string
		minor int
	}
	var found []versioned
	for _, entry := range entries {
		match := versionedPython.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}
		minor, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		found = append(found, versioned{filepath.Join(directory, entry.Name()), minor})
	}
	slices.SortFunc(found, func(a, b versioned) int { return b.minor - a.minor })
	paths := make([]string, 0, len(found))
	for _, item := range found {
		paths = append(paths, item.path)
	}
	return paths
}

// firstExecutable returns the first absolute candidate that resolves (through
// symlinks, as execve does for a shebang) to a regular file with an execute
// bit. Relative paths are refused because a shebang must not depend on the
// working directory of the exec'ing process.
func firstExecutable(candidates []string) (string, bool) {
	for _, candidate := range candidates {
		if !filepath.IsAbs(candidate) {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		return candidate, true
	}
	return "", false
}

// TestFixturePythonSelection checks interpreter selection against real local
// files, including the refusals that would otherwise yield an unusable shebang.
func TestFixturePythonSelection(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, mode os.FileMode) string {
		path := filepath.Join(directory, name)
		require(t, os.WriteFile(path, []byte("#!/bin/sh\n"), mode))
		return path
	}
	executable := write("python3.12", 0700)
	older := write("python3.9", 0700)
	write("python3.12-config", 0700)
	plain := write("python3", 0600)
	link := filepath.Join(directory, "python3-link")
	require(t, os.Symlink(executable, link))
	missing := filepath.Join(directory, "absent")
	cases := []struct {
		name       string
		candidates []string
		want       string
		ok         bool
	}{
		{"first executable wins", []string{executable, older}, executable, true},
		{"missing skipped", []string{missing, older}, older, true},
		{"non-executable skipped", []string{plain, executable}, executable, true},
		{"directory skipped", []string{directory, older}, older, true},
		{"relative refused", []string{"python3.12"}, "", false},
		{"symlink to executable accepted", []string{link}, link, true},
		{"nothing usable", []string{missing, plain, directory}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := firstExecutable(c.candidates)
			if got != c.want || ok != c.ok {
				t.Fatalf("firstExecutable(%v) = %q, %v; want %q, %v", c.candidates, got, ok, c.want, c.ok)
			}
		})
	}
	if got := versionedPythonCandidates(directory); !slices.Equal(got, []string{executable, older}) {
		t.Fatalf("versioned candidates %v exclude helpers and order newest first", got)
	}
	if got := versionedPythonCandidates(missing); got != nil {
		t.Fatalf("missing directory yielded %v", got)
	}
}
