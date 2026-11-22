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
// goroutines fork/exec /bin/true, repeatedly pin an executable and exec the
// pinned copy through an inherited descriptor, as Start does with /dev/fd/4.
// Before nj4 the writable copy fd could be inherited by a concurrent fork
// that had not exec'd yet, so the descriptor exec failed with ETXTBSY
// (golang/go#22315). The run is time-bounded to a few seconds.
func TestNJ4PinnedExecSurvivesConcurrentForks(t *testing.T) {
	source, err := filepath.EvalSymlinks("/bin/true")
	if err != nil {
		t.Skipf("no /bin/true: %v", err)
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
			forkTrueUntil(stop)
		}()
	}
	defer func() { close(stop); forkers.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	iterations := 0
	for ; time.Now().Before(deadline) && iterations < 2000; iterations++ {
		if err := pinAndExecOnce(t, spec); err != nil {
			t.Fatalf("iteration %d: exec of pinned copy: %v", iterations, err)
		}
	}
	t.Logf("%d pin+exec iterations under concurrent forks", iterations)
}

// forkTrueUntil keeps forking /bin/true so pinExecutable's write window
// overlaps forks in this process.
func forkTrueUntil(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = exec.Command("/bin/true").Run()
	}
}

// pinAndExecOnce pins spec into a fresh owned directory and executes the
// pinned descriptor as /dev/fd/3. Only ETXTBSY is the regression; other exec
// failures (for example a host without /dev/fd) skip rather than pass.
func pinAndExecOnce(t *testing.T, spec Executable) error {
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
		t.Skipf("descriptor exec unsupported here: %v", err)
	}
	return err
}
