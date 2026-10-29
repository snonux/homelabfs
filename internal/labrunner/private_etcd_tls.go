package labrunner

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/snonux/homelabfs/internal/coord"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// EtcdIdentity holds private test material. Never serialize it into logs or
// evidence. Peer and client trust domains have independent fresh signing keys.
type EtcdIdentity struct{ Certificate, Key []byte }

// EtcdPKI holds per-run material. CA private keys are discarded after signing;
// no production certificate, SSH key, password or system trust root is loaded.
type EtcdPKI struct {
	ClientCA, PeerCA []byte
	Members          map[string]map[string]EtcdIdentity
	Clients          map[string]EtcdIdentity
}

// EtcdRole binds one certificate CN to coord's exact cluster-specific ranges.
type EtcdRole struct {
	User        string             `json:"user"`
	Permissions []coord.Permission `json:"permissions"`
}

type testEtcdCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

// GenerateEtcdPKI creates fresh test-only certificates, bounded to 24 hours.
// It performs no filesystem or network I/O and does not approve a lab run.
func GenerateEtcdPKI(p Plan, now time.Time) (EtcdPKI, error) {
	if err := p.Validate(); err != nil {
		return EtcdPKI{}, err
	}
	if p.PrivateEtcd == nil || now.Before(p.Created) || now.After(p.Expires) {
		return EtcdPKI{}, errors.New("current private etcd plan required")
	}
	clientCA, err := newTestEtcdCA("hfs-private-"+p.RunID+"-client", now)
	if err != nil {
		return EtcdPKI{}, err
	}
	peerCA, err := newTestEtcdCA("hfs-private-"+p.RunID+"-peer", now)
	if err != nil {
		return EtcdPKI{}, err
	}
	out := EtcdPKI{ClientCA: clientCA.pem, PeerCA: peerCA.pem, Members: map[string]map[string]EtcdIdentity{}, Clients: map[string]EtcdIdentity{}}
	for i := range 3 {
		guest := fmt.Sprintf("hfs-test%d", i)
		server, err := clientCA.issue("hfs-private-"+p.RunID+"-"+guest, guestAddress(guest), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, now)
		if err != nil {
			return EtcdPKI{}, err
		}
		peer, err := peerCA.issue("hfs-private-"+p.RunID+"-peer-"+guest, guestAddress(guest), []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, now)
		if err != nil {
			return EtcdPKI{}, err
		}
		out.Members[guest] = map[string]EtcdIdentity{"server": server, "peer": peer}
	}
	roles, err := EtcdRoles(p)
	if err != nil {
		return EtcdPKI{}, err
	}
	for _, role := range append(roles, EtcdRole{User: "root"}) {
		identity, err := clientCA.issue(role.User, "", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, now)
		if err != nil {
			return EtcdPKI{}, err
		}
		out.Clients[role.User] = identity
	}
	return out, nil
}

// EtcdRoles reuses the mandatory mTLS client's exact RBAC ranges. Bootstrap
// root is managed separately and must be removed from workload reachability.
func EtcdRoles(p Plan) ([]EtcdRole, error) {
	if p.PrivateEtcd == nil {
		return nil, errors.New("private etcd plan required")
	}
	id, err := fsapi.ParseUUID(p.PrivateEtcd.Cluster)
	if err != nil {
		return nil, err
	}
	var out []EtcdRole
	for _, role := range []struct {
		name string
		cap  coord.Capability
	}{{"daemon-hfs-test0", coord.Daemon}, {"daemon-hfs-test1", coord.Daemon}, {"daemon-hfs-test2", coord.Daemon}, {"controller", coord.Controller}, {"operator", coord.Operator}} {
		permissions, err := coord.Permissions(id, role.cap)
		if err != nil {
			return nil, err
		}
		out = append(out, EtcdRole{User: "hfs-private-" + p.RunID + "-" + role.name, Permissions: permissions})
	}
	return out, nil
}

func validateEtcdMemberTLS(p Plan, guest string, pki EtcdPKI) error {
	clientCA, err := parseTestEtcdCA(pki.ClientCA, "hfs-private-"+p.RunID+"-client")
	if err != nil {
		return err
	}
	peerCA, err := parseTestEtcdCA(pki.PeerCA, "hfs-private-"+p.RunID+"-peer")
	if err != nil {
		return err
	}
	if clientCA.Equal(peerCA) {
		return errors.New("peer/client test trust domains must be distinct")
	}
	for _, item := range []struct {
		name, cn string
		ca       *x509.Certificate
		uses     []x509.ExtKeyUsage
	}{{"server", "hfs-private-" + p.RunID + "-" + guest, clientCA, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, {"peer", "hfs-private-" + p.RunID + "-peer-" + guest, peerCA, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}} {
		pair, err := tls.X509KeyPair(pki.Members[guest][item.name].Certificate, pki.Members[guest][item.name].Key)
		if err != nil {
			return err
		}
		cert, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return err
		}
		if cert.Subject.CommonName != item.cn || cert.IsCA || len(cert.ExtKeyUsage) != len(item.uses) || len(cert.IPAddresses) != 1 || cert.NotAfter.Sub(cert.NotBefore) > 24*time.Hour+time.Minute {
			return errors.New("foreign or overprivileged test member certificate")
		}
		if err := cert.CheckSignatureFrom(item.ca); err != nil {
			return err
		}
		roots := x509.NewCertPool()
		roots.AddCert(item.ca)
		for _, use := range item.uses {
			if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, DNSName: guestAddress(guest), KeyUsages: []x509.ExtKeyUsage{use}}); err != nil {
				return err
			}
		}
	}
	return nil
}
func parseTestEtcdCA(raw []byte, name string) (*x509.Certificate, error) {
	block, rest := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" || len(rest) != 0 {
		return nil, errors.New("exact single test CA required")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	if !cert.IsCA || cert.Subject.CommonName != name || cert.NotAfter.Sub(cert.NotBefore) > 24*time.Hour+time.Minute {
		return nil, errors.New("foreign or unbounded test CA")
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		return nil, err
	}
	return cert, nil
}

func newTestEtcdCA(name string, now time.Time) (testEtcdCA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return testEtcdCA{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return testEtcdCA{}, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, MaxPathLenZero: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return testEtcdCA{}, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return testEtcdCA{}, err
	}
	return testEtcdCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}, nil
}

func (ca testEtcdCA) issue(name, address string, usages []x509.ExtKeyUsage, now time.Time) (EtcdIdentity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return EtcdIdentity{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return EtcdIdentity{}, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: usages, BasicConstraintsValid: true}
	if address != "" {
		template.IPAddresses = []net.IP{net.ParseIP(address)}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		return EtcdIdentity{}, err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return EtcdIdentity{}, err
	}
	return EtcdIdentity{Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), Key: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})}, nil
}

func guestAddress(guest string) string {
	return map[string]string{"hfs-test0": "172.31.253.11", "hfs-test1": "172.31.253.12", "hfs-test2": "172.31.253.13"}[guest]
}
