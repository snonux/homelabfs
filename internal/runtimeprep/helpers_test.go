//go:build linux

package runtimeprep

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// shortRoot is a private test directory with a short path. Generation puts
// its validation sandbox beside the output, and the runtime checks that
// sandbox's lifecycle socket path against sockaddr_un (103 bytes), which
// t.TempDir's subtest-named paths can exceed.
func shortRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "rp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// exampleSpec decodes the reviewed zq3-shaped example through the strict decoder.
func exampleSpec(t *testing.T) Spec {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "zq3-spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s Spec
	if err := decodeStrict(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// operatorFixture writes a fresh throwaway operator key (never the real one)
// and points the spec at it, as the external signer would be configured.
func operatorFixture(t *testing.T, root string, s *Spec) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	s.OperatorKey = filepath.Join(root, "operator.pem")
	if err := os.WriteFile(s.OperatorKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	s.OperatorPublicKey = hex.EncodeToString(key.Public().(ed25519.PublicKey))
	return key
}

// localLayout moves every host path into root so the volume can really be
// initialised and opened in the test (the host is an explicit OS boundary).
func localLayout(t *testing.T, root string, s *Spec) {
	t.Helper()
	host := filepath.Join(root, "host")
	if err := os.Mkdir(host, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(host, "handoff"), 0o700); err != nil {
		t.Fatal(err)
	}
	s.Volume = VolumeLayout{Config: filepath.Join(host, "volume-config.json"),
		ControlDirectory: filepath.Join(host, "control"), VolumeDirectory: filepath.Join(host, "volume"),
		Mountpoint: filepath.Join(host, "mnt")}
	s.OwnerDirectory = filepath.Join(host, "owner")
	s.EnrollmentFile = filepath.Join(host, "handoff", "enrollment.json")
}

// generateBundle generates and writes a bundle to root/bundle.
func generateBundle(t *testing.T, root string, s Spec) (Bundle, string) {
	t.Helper()
	output := filepath.Join(root, "bundle")
	bundle, err := Generate(t.Context(), s, output, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFreshDirectory(output, bundle.Files); err != nil {
		t.Fatal(err)
	}
	return bundle, output
}

func writePrivateJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := marshalFile(value)
	if err == nil {
		err = os.WriteFile(path, raw, 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
}
