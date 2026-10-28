package coord

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdConfig binds a role to externally provisioned mutual-TLS credentials.
// The certificate CN must identify an etcd RBAC user with Permissions' ranges.
// CA/certificate/key files and their ancestors must be administratively private.
type EtcdConfig struct {
	Cluster                     fsapi.UUID
	Capability                  Capability
	Endpoints                   []string
	CAFile, CertFile, KeyFile   string
	DialTimeout, RequestTimeout time.Duration
}

// Permission describes one etcd RBAC range. Empty End means one exact key;
// otherwise the range is [Start, End). Write permits both read and write.
type Permission struct {
	Start, End string
	Write      bool
}
type etcdBackend struct {
	client  *clientv3.Client
	timeout time.Duration
}

var _ backend = (*etcdBackend)(nil)

// NewEtcd opens an etcd v3 client with mandatory verified mutual TLS. There is
// no plaintext endpoint, system-CA fallback, password or skip-verification mode.
func NewEtcd(ctx context.Context, config EtcdConfig) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !config.Cluster.Valid() || config.Capability < Daemon || config.Capability > Operator || len(config.Endpoints) == 0 || len(config.Endpoints) > 16 || config.DialTimeout <= 0 || config.RequestTimeout <= 0 {
		return nil, fmt.Errorf("invalid etcd configuration")
	}
	for _, endpoint := range config.Endpoints {
		u, err := url.Parse(endpoint)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("etcd endpoint must be an https authority")
		}
	}
	transport, err := loadTLS(config)
	if err != nil {
		return nil, err
	}
	client, err := clientv3.New(clientv3.Config{Context: ctx, Endpoints: config.Endpoints, TLS: transport, DialTimeout: config.DialTimeout, MaxCallRecvMsgSize: 2 * maxValueBytes, MaxCallSendMsgSize: maxValueBytes + 65536})
	if err != nil {
		return nil, fmt.Errorf("create etcd client: %w", err)
	}
	return &Client{store: &etcdBackend{client: client, timeout: config.RequestTimeout}, cluster: config.Cluster, capability: config.Capability, prefix: keyPrefix(config.Cluster), close: client.Close}, nil
}

// Permissions returns minimal cluster-specific RBAC ranges for one principal.
// It describes provisioning only; this package never enables auth or grants roles.
func Permissions(cluster fsapi.UUID, capability Capability) ([]Permission, error) {
	if !cluster.Valid() || capability < Daemon || capability > Operator {
		return nil, ErrDenied
	}
	p := keyPrefix(cluster)
	policy := Permission{Start: p + "policy", Write: capability == Operator}
	grants, configurations := p+"grants/", p+"configurations/"
	out := []Permission{policy, {Start: grants, End: clientv3.GetPrefixRangeEnd(grants)}}
	if capability == Controller {
		out[1].Write = true
		out = append(out, Permission{Start: configurations, End: clientv3.GetPrefixRangeEnd(configurations), Write: true})
	}
	return out, nil
}

func (b *etcdBackend) get(ctx context.Context, key string) (value, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	response, err := b.client.Get(ctx, key)
	if err != nil {
		return value{}, etcdError(err)
	}
	if len(response.Kvs) != 1 {
		return value{}, ErrNotFound
	}
	kv := response.Kvs[0]
	if len(kv.Value) > maxValueBytes {
		return value{}, fmt.Errorf("oversized stored coordination value")
	}
	return value{bytes: kv.Value, revision: kv.ModRevision, observed: response.Header.Revision}, nil
}

func (b *etcdBackend) commit(ctx context.Context, checks []condition, updates []update) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	comparisons := make([]clientv3.Cmp, 0, len(checks))
	for _, check := range checks {
		comparisons = append(comparisons, clientv3.Compare(clientv3.ModRevision(check.key), "=", check.revision))
	}
	operations := make([]clientv3.Op, 0, len(updates))
	for _, update := range updates {
		operations = append(operations, clientv3.OpPut(update.key, string(update.bytes)))
	}
	response, err := b.client.Txn(ctx).If(comparisons...).Then(operations...).Commit()
	if err != nil {
		return 0, etcdError(err)
	}
	if !response.Succeeded {
		return 0, ErrConflict
	}
	return response.Header.Revision, nil
}

func (b *etcdBackend) watch(ctx context.Context, key string, after int64) (<-chan change, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	out := make(chan change)
	go func() {
		defer close(out)
		defer cancel()
		ticker := time.NewTicker(b.timeout)
		defer ticker.Stop()
		created := make(chan clientv3.WatchChan, 1)
		go func() { created <- b.client.Watch(clientv3.WithRequireLeader(ctx), key, clientv3.WithRev(after+1)) }()
		var stream clientv3.WatchChan
		for {
			select {
			case <-ctx.Done():
				return
			case stream = <-created:
				created = nil
			case <-ticker.C:
				// The etcd watch library retries recoverable disconnects indefinitely.
				// An independent quorum-backed read makes connectivity loss observable.
				if _, err := b.get(ctx, key); err != nil {
					sendChange(ctx, out, change{err: err})
					return
				}
			case response, ok := <-stream:
				if !ok {
					if ctx.Err() == nil {
						sendChange(ctx, out, change{err: ErrUnavailable})
					}
					return
				}
				if response.CompactRevision != 0 {
					sendChange(ctx, out, change{err: ErrCompacted})
					return
				}
				if err := response.Err(); err != nil {
					sendChange(ctx, out, change{err: etcdError(err)})
					return
				}
				for _, event := range response.Events {
					if event.Type != clientv3.EventTypePut {
						sendChange(ctx, out, change{err: fmt.Errorf("policy deleted: %w", ErrDenied)})
						return
					}
					if !sendChange(ctx, out, change{value: value{bytes: event.Kv.Value, revision: event.Kv.ModRevision}}) {
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func loadTLS(config EtcdConfig) (*tls.Config, error) {
	data, err := os.ReadFile(config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read etcd trust roots: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("invalid etcd trust roots")
	}
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load etcd client identity: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil || leaf.Subject.CommonName == "" {
		return nil, fmt.Errorf("etcd client certificate requires RBAC user CN")
	}
	return &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}, nil
}

func keyPrefix(cluster fsapi.UUID) string { return "/homelabfs/v1/clusters/" + cluster.String() + "/" }

func etcdError(err error) error {
	if errors.Is(err, rpctypes.ErrPermissionDenied) || errors.Is(err, rpctypes.ErrAuthFailed) {
		return fmt.Errorf("%w: %w", ErrDenied, err)
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

func sendChange(ctx context.Context, out chan<- change, change change) bool {
	select {
	case out <- change:
		return true
	case <-ctx.Done():
		return false
	}
}
