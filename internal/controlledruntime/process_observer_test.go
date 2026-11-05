//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// This helper is a disposable process counter, never an exporter or native VM.
func TestRefusedObserverChildCounter(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "hfs-refused-observer-child" {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("child-created"), 0600); err != nil {
		os.Exit(91)
	}
	os.Exit(0)
}

func refusedObserverSpec(t *testing.T) (Executable, string) {
	t.Helper()
	spec := helperSpec(t)
	marker := filepath.Join(t.TempDir(), "child-counter")
	spec.Args = []string{"-test.run=^TestRefusedObserverChildCounter$", "--", "hfs-refused-observer-child", marker}
	return spec, marker
}

func assertObserverRefusalHasNoChild(t *testing.T, s *Supervisor, child *exporter, marker string) {
	t.Helper()
	if child != nil || s.child != nil {
		t.Fatal("observer refusal retained an actually started child")
	}
	for _, path := range []string{marker, filepath.Join(s.root, "exporter")} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("observer refusal reached launch effects: %s: %v", path, err)
		}
	}
}

func TestObserverPreflightFailureCreatesNoChild(t *testing.T) {
	// Explicit OS boundary failures exercise the actual launch implementation.
	// They are not native qualification of any selected build.
	for _, failure := range []error{fsapi.Unsupported, fsapi.Permission, fsapi.Unavailable} {
		t.Run(failure.Error(), func(t *testing.T) {
			s := ownerFixture(t)
			spec, marker := refusedObserverSpec(t)
			calls := 0
			child, err := s.launchObserved(t.Context(), spec, func(pid int) (processIdentity, error) {
				calls++
				if pid != os.Getpid() {
					t.Fatal("capability observation attempted child identity")
				}
				return processIdentity{}, failure
			})
			if !errors.Is(err, failure) || calls != 1 {
				t.Fatalf("capability refusal: calls=%d err=%v", calls, err)
			}
			assertObserverRefusalHasNoChild(t, s, child, marker)
		})
	}
}

func TestObserverPreflightRejectsIncompleteSelf(t *testing.T) {
	identity, err := observeProcess(os.Getpid())
	if errors.Is(err, fsapi.Unsupported) {
		t.Skip("unsupported observer has a dedicated actual-build refusal test")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pid", "uid", "gid", "groups", "birth"} {
		t.Run(name, func(t *testing.T) {
			bad := identity
			switch name {
			case "pid":
				bad.PID++
			case "uid":
				bad.UID++
			case "gid":
				bad.GID++
			case "groups":
				bad.Groups = append(append([]uint32{}, identity.Groups...), ^uint32(0))
			case "birth":
				bad.Incarnation = ""
			}
			if _, err := checkProcessObserver(func(int) (processIdentity, error) { return bad, nil }); !errors.Is(err, fsapi.Permission) {
				t.Fatalf("incomplete self accepted: %v", err)
			}
		})
	}
}

func TestObserverQualificationFlagsAreExclusive(t *testing.T) {
	for _, args := range [][]string{
		{"--check-process-observer", "--config=/absent"},
		{"--check-process-observer", "--disposable-validation"},
		{"--check-process-observer", "argument"},
	} {
		var output bytes.Buffer
		if err := Run(context.Background(), args, &output, &output); err == nil || output.Len() != 0 {
			t.Fatalf("combined qualification flags accepted: %v output=%q error=%v", args, output.String(), err)
		}
	}
}

// Guard the ordering that an unsupported native binary cannot exercise on Linux.
// Actual FreeBSD !cgo execution separately checks the same invariant above the OS.
func TestObserverPreflightDominatesLaunchEffects(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "process.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var launch, observed *ast.FuncDecl
	for _, declaration := range source.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		switch function.Name.Name {
		case "launch":
			launch = function
		case "launchObserved":
			observed = function
		}
	}
	if launch == nil || observed == nil || len(launch.Body.List) != 1 {
		t.Fatal("production launch no longer delegates through the observed boundary")
	}
	returned, ok := launch.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(returned.Results) != 1 {
		t.Fatal("missing observed production launch")
	}
	call, ok := returned.Results[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 3 {
		t.Fatal("missing production observer argument")
	}
	observer, ok := call.Args[2].(*ast.Ident)
	if !ok || observer.Name != "observeProcess" {
		t.Fatal("production launch does not select the actual platform observer")
	}
	var preflight token.Pos
	ast.Inspect(observed.Body, func(node ast.Node) bool {
		invocation, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if function, ok := invocation.Fun.(*ast.Ident); ok && function.Name == "checkProcessObserver" {
			preflight = invocation.Pos()
		}
		return true
	})
	if preflight == token.NoPos {
		t.Fatal("launch lost observer preflight")
	}
	ast.Inspect(observed.Body, func(node ast.Node) bool {
		invocation, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := invocation.Fun.(*ast.SelectorExpr)
		if ok && (selector.Sel.Name == "pinExecutable" || selector.Sel.Name == "Start") && invocation.Pos() < preflight {
			t.Fatal("native launch effect precedes observer preflight")
		}
		return true
	})
}
