//go:build linux || freebsd

package controlledruntime

import (
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestNJ4PinnedExecSurvivesConcurrentForks is the nj4 regression: while other
// goroutines fork/exec true (looked up on PATH: /bin/true on Linux,
// /usr/bin/true on FreeBSD), repeatedly pin an executable and exec the
// pinned copy through an inherited descriptor, as Start does with /dev/fd/4.
// Before nj4 the writable copy fd could be inherited by a concurrent fork
// that had not exec'd yet, so the descriptor exec failed with ETXTBSY
// (golang/go#22315). The run is time-bounded to a few seconds.
func TestNJ4PinnedExecSurvivesConcurrentForks(t *testing.T) {
	trueBin, err := exec.LookPath("true")
	if err != nil {
		t.Skipf("no true on PATH: %v", err)
	}
	source, err := filepath.EvalSymlinks(trueBin)
	if err != nil {
		t.Skipf("no %s: %v", trueBin, err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	spec := Executable{Path: source, SHA256: sha256.Sum256(data)}
	stop := make(chan struct{})
	var forkers sync.WaitGroup
	for range 3 {
		forkers.Add(1)
		go func() {
			defer forkers.Done()
			forkTrueUntil(stop, trueBin)
		}()
	}
	defer func() { close(stop); forkers.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	iterations := 0
	for ; time.Now().Before(deadline) && iterations < 2000; iterations++ {
		if err := pinAndExecOnce(t, spec, iterations == 0); err != nil {
			t.Fatalf("iteration %d: exec of pinned copy: %v", iterations, err)
		}
	}
	t.Logf("%d pin+exec iterations under concurrent forks", iterations)
}

// forkTrueUntil keeps forking trueBin so pinExecutable's write window
// overlaps forks in this process.
func forkTrueUntil(stop <-chan struct{}, trueBin string) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = exec.Command(trueBin).Run()
	}
}

// pinAndExecOnce pins spec into a fresh owned directory and executes the
// pinned descriptor as /dev/fd/3. Only ETXTBSY is the regression. Another
// exec failure on the first round means descriptor exec is unsupported here
// (for example no /dev/fd) and skips; on any later round it fails the test,
// since the same exec already worked once.
func pinAndExecOnce(t *testing.T, spec Executable, first bool) error {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "owner")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = directory.Close() }()
	pinned, err := (&Supervisor{directory: directory}).pinExecutable(spec)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	defer func() { _ = pinned.Close() }()
	command := exec.Command("/dev/fd/3")
	command.ExtraFiles = []*os.File{pinned}
	err = command.Run()
	if err != nil && !errors.Is(err, syscall.ETXTBSY) {
		if first {
			t.Skipf("descriptor exec unsupported here: %v", err)
		}
		t.Fatalf("exec of pinned copy failed after earlier rounds worked: %v", err)
	}
	return err
}
