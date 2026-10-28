//go:build linux

package coord

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.etcd.io/etcd/client/pkg/v3/transport"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

type testPKI struct {
	root    *x509.Certificate
	key     ed25519.PrivateKey
	dir, ca string
	serial  int64
}
type testEtcd struct {
	pki      *testPKI
	root     *clientv3.Client
	endpoint string
	next     byte
	stop     func()
}

func TestEtcdContract(t *testing.T) {
	server := startEtcd(t)
	runContract(t, server.fixture)
}

func TestEtcdTLSAndRBAC(t *testing.T) {
	server := startEtcd(t)
	f := server.fixture(t)
	ctx := t.Context()
	snapshot, err := f.operator.PutPolicy(ctx, 0, policy(f.operator.cluster))
	if err != nil {
		t.Fatal(err)
	}
	daemon := f.daemon.store.(*etcdBackend).client
	// Server authorization is tested directly, independently of Client's local guard.
	for _, key := range []string{f.daemon.prefix + "policy", f.daemon.grantKey(id(5)), f.daemon.prefix + "configurations/" + id(7).String(), keyPrefix(id(99)) + "policy", f.daemon.prefix + "policy-sibling"} {
		if _, err := daemon.Put(ctx, key, "forbidden"); err == nil {
			t.Fatalf("daemon wrote %s", key)
		}
	}
	controller := f.controller.store.(*etcdBackend).client
	if _, err := controller.Put(ctx, f.controller.prefix+"policy", "forbidden"); err == nil {
		t.Fatal("controller wrote admission")
	}
	if _, err := daemon.Get(ctx, keyPrefix(id(99))+"policy"); err == nil {
		t.Fatal("cross-cluster read allowed")
	}
	// Exact-key watch permissions must not include adjacent key names.
	if _, err := daemon.Get(ctx, f.daemon.prefix+"policy-sibling"); err == nil {
		t.Fatal("sibling policy read allowed")
	}
	if _, err := f.daemon.GetPolicy(ctx); err != nil {
		t.Fatal(err)
	}
	forged, err := NewEtcd(ctx, server.clientConfig(t, "daemon", Operator, f.daemon.cluster))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = forged.Close() })
	changed := snapshot.Policy
	changed.Registry.Revision++
	if _, err := forged.PutPolicy(ctx, snapshot.Revision, changed); !errors.Is(err, ErrDenied) {
		t.Fatalf("role selection bypassed server RBAC: %v", err)
	}

	config := server.clientConfig(t, "daemon", Daemon, f.daemon.cluster)
	config.Endpoints = []string{"http://127.0.0.1:2379"}
	if _, err := NewEtcd(ctx, config); err == nil {
		t.Fatal("plaintext endpoint accepted")
	}
	config = server.clientConfig(t, "daemon", Daemon, f.daemon.cluster)
	alien := newPKI(t)
	config.CAFile = alien.ca
	bad, err := NewEtcd(ctx, config)
	if err == nil {
		defer func(client *Client) { _ = client.Close() }(bad)
		if _, err = bad.GetPolicy(ctx); err == nil {
			t.Fatal("untrusted server accepted")
		}
	}
	// Keep the already authorized identity: expiry must be the only reason
	// this client is refused, rather than an unknown user or missing RBAC role.
	config = server.clientConfig(t, "daemon", Daemon, f.daemon.cluster)
	config.CertFile, config.KeyFile = server.pki.certificate(t, "daemon", true)
	bad, err = NewEtcd(ctx, config)
	if err == nil {
		defer func(client *Client) { _ = client.Close() }(bad)
		if _, err = bad.GetPolicy(ctx); err == nil {
			t.Fatal("expired client accepted")
		}
	}
	if _, err := f.daemon.GetPolicy(ctx); err != nil {
		t.Fatalf("authorized healthy client unavailable after expiry refusal: %v", err)
	}
	if snapshot.Revision <= 0 {
		t.Fatal("invalid revision")
	}
}

func (s *testEtcd) fixture(t *testing.T) fixture {
	t.Helper()
	s.next++
	clusterID := id(100 + s.next)
	clients := make([]*Client, 0, 3)
	for _, entry := range []struct {
		name string
		cap  Capability
	}{{"operator", Operator}, {"controller", Controller}, {"daemon", Daemon}} {
		permissions, err := Permissions(clusterID, entry.cap)
		if err != nil {
			t.Fatal(err)
		}
		role := fmt.Sprintf("%s-%d", entry.name, s.next)
		if _, err := s.root.RoleAdd(t.Context(), role); err != nil {
			t.Fatal(err)
		}
		for _, permission := range permissions {
			typ := clientv3.PermissionType(clientv3.PermRead)
			if permission.Write {
				typ = clientv3.PermissionType(clientv3.PermReadWrite)
			}
			if _, err := s.root.RoleGrantPermission(t.Context(), role, permission.Start, permission.End, typ); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.root.UserGrantRole(t.Context(), entry.name, role); err != nil {
			t.Fatal(err)
		}
		c, err := NewEtcd(t.Context(), s.clientConfig(t, entry.name, entry.cap, clusterID))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		clients = append(clients, c)
	}
	return fixture{clients[0], clients[1], clients[2], func(revision int64) error { _, err := s.root.Compact(t.Context(), revision); return err }}
}

func (s *testEtcd) clientConfig(t *testing.T, name string, cap Capability, clusterID [16]byte) EtcdConfig {
	t.Helper()
	cert, key := s.pki.certificate(t, name, false)
	return EtcdConfig{Cluster: clusterID, Capability: cap, Endpoints: []string{s.endpoint}, CAFile: s.pki.ca, CertFile: cert, KeyFile: key, DialTimeout: time.Second, RequestTimeout: 2 * time.Second}
}

func startEtcd(t *testing.T) *testEtcd {
	t.Helper()
	pki := newPKI(t)
	cert, key := pki.certificate(t, "server", false)
	cfg := embed.NewConfig()
	cfg.Dir = t.TempDir()
	cfg.LogLevel = "error"
	cfg.Logger = "zap"
	cfg.LogOutputs = []string{"stderr"}
	clientURL := unusedURL(t, "https")
	peerURL := unusedURL(t, "http")
	cfg.ListenClientUrls = []url.URL{clientURL}
	cfg.AdvertiseClientUrls = cfg.ListenClientUrls
	cfg.ListenPeerUrls = []url.URL{peerURL}
	cfg.AdvertisePeerUrls = cfg.ListenPeerUrls
	cfg.InitialCluster = cfg.InitialClusterFromName(cfg.Name)
	cfg.ClientTLSInfo = transport.TLSInfo{CertFile: cert, KeyFile: key, TrustedCAFile: pki.ca, ClientCertAuth: true, MinVersion: tls.VersionTLS13}
	server, err := embed.StartEtcd(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(server.Close) }
	t.Cleanup(stop)
	select {
	case <-server.Server.ReadyNotify():
	case <-time.After(15 * time.Second):
		t.Fatal("embedded etcd start timeout")
	}
	s := &testEtcd{pki: pki, endpoint: clientURL.String(), stop: stop}
	config := s.clientConfig(t, "root", Operator, id(1))
	transport, err := loadTLS(config)
	if err != nil {
		t.Fatal(err)
	}
	s.root, err = clientv3.New(clientv3.Config{Endpoints: config.Endpoints, TLS: transport, DialTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.root.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, name := range []string{"root", "operator", "controller", "daemon"} {
		if _, err := s.root.UserAddWithOptions(ctx, name, "", &clientv3.UserAddOptions{NoPassword: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.root.RoleAdd(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.root.UserGrantRole(ctx, "root", "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.root.AuthEnable(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func newPKI(t *testing.T) *testPKI {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, root, root, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	p := &testPKI{root: root, key: key, dir: t.TempDir(), serial: 1}
	p.ca = filepath.Join(p.dir, "ca.pem")
	if err := os.WriteFile(p.ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func (p *testPKI) certificate(t *testing.T, name string, expired bool) (string, string) {
	t.Helper()
	p.serial++
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(p.serial), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if name == "server" {
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	if expired {
		cert.NotAfter = time.Now().Add(-time.Minute)
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, p.root, pub, p.key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(p.dir, fmt.Sprintf("%s-%d.crt", name, p.serial)), filepath.Join(p.dir, fmt.Sprintf("%s-%d.key", name, p.serial))
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func unusedURL(t *testing.T, scheme string) url.URL {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(scheme + "://" + address)
	if err != nil {
		t.Fatal(err)
	}
	return *u
}

func TestEtcdWatchDisconnect(t *testing.T) {
	server := startEtcd(t)
	f := server.fixture(t)
	snapshot, err := f.operator.PutPolicy(t.Context(), 0, policy(f.operator.cluster))
	if err != nil {
		t.Fatal(err)
	}
	f.daemon.store.(*etcdBackend).timeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	events, err := f.daemon.WatchPolicy(ctx, snapshot.Revision)
	if err != nil {
		t.Fatal(err)
	}
	server.stop()
	select {
	case event := <-events:
		if !errors.Is(event.Err, ErrUnavailable) {
			t.Fatalf("disconnected watch: %+v", event)
		}
	case <-ctx.Done():
		t.Fatal("watch hid server loss behind silent reconnect retries")
	}
}
