package labrunner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"go.etcd.io/etcd/api/v3/authpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
)

// EtcdRBACOperation provisions only test credentials and exact coord ranges.
type EtcdRBACOperation struct {
	Kind, User, Role, Start, End string
	Write                        bool
}

// EtcdRBACExecutor is the single bounded authenticated provisioning seam.
type EtcdRBACExecutor interface {
	ApplyEtcdRBAC(context.Context, EtcdRBACOperation) error
}

type etcdRBACClient struct{ client *clientv3.Client }

// EtcdRBACOperations creates passwordless certificate-CN users and exact ranges,
// then enables auth last. Root is etcd's reserved bootstrap user; its key stays
// on the controller. HFS operator/controller/daemon roles never receive root.
func EtcdRBACOperations(p Plan) ([]EtcdRBACOperation, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	roles, err := EtcdRoles(p)
	if err != nil {
		return nil, err
	}
	out := []EtcdRBACOperation{{Kind: "user", User: "root"}, {Kind: "grant", User: "root", Role: "root"}}
	for _, role := range roles {
		out = append(out, EtcdRBACOperation{Kind: "role", Role: role.User})
		for _, permission := range role.Permissions {
			out = append(out, EtcdRBACOperation{Kind: "permission", Role: role.User, Start: permission.Start, End: permission.End, Write: permission.Write})
		}
		out = append(out, EtcdRBACOperation{Kind: "user", User: role.User}, EtcdRBACOperation{Kind: "grant", User: role.User, Role: role.User})
	}
	return append(out, EtcdRBACOperation{Kind: "enable"}), nil
}

// ProvisionEtcdRBAC applies a deterministic sequence with no retry. A partial
// setup is a failed preserved cluster, never an implicitly repaired installation.
func ProvisionEtcdRBAC(ctx context.Context, p Plan, executor EtcdRBACExecutor) error {
	if executor == nil {
		return errors.New("private etcd RBAC executor required")
	}
	ops, err := EtcdRBACOperations(p)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := executor.ApplyEtcdRBAC(ctx, op); err != nil {
			return fmt.Errorf("private etcd RBAC %s: %w", op.Kind, err)
		}
	}
	return nil
}

// BootstrapPrivateEtcd contacts only the three fixed private guest endpoints,
// after exact plan approval. It requires an initially unauthenticated fresh
// cluster, checks native membership, installs RBAC, and verifies role denials.
// This is infrastructure setup and never establishes HFS serving readiness.
func BootstrapPrivateEtcd(ctx context.Context, req EtcdStepRequest, directory string) (EtcdControllerEnrollment, error) {
	observed, err := inspectEtcdControllerRuntime()
	if err != nil {
		return EtcdControllerEnrollment{}, err
	}
	return bootstrapEtcdOnController(ctx, req, directory, observed, bootstrapPrivateEtcd)
}

func bootstrapEtcdOnController(ctx context.Context, req EtcdStepRequest, directory string, observed etcdControllerRuntime, run func(context.Context, Plan, string) error) (EtcdControllerEnrollment, error) {
	ctx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	enrollment, err := enrollEtcdController(req, directory, observed)
	if err != nil {
		return enrollment, err
	}
	if err := run(ctx, req.Plan, directory); err != nil {
		return enrollment, err
	}
	enrollment.Configured = true
	return enrollment, replaceJSON(etcdControllerRoot(req.Plan)+"/controller-enrollment.json", enrollment)
}

func bootstrapPrivateEtcd(ctx context.Context, p Plan, directory string) error {
	if p.PrivateEtcd == nil || p.faultStep("etcd-rbac").Contract != "private-etcd-v1:rbac:f3" {
		return errors.New("approved private etcd RBAC step required")
	}
	if err := privateDirectory(directory); err != nil {
		return err
	}
	var binding struct {
		PlanHash string `json:"plan_hash"`
		Cluster  string `json:"cluster"`
	}
	if err := readOwnedJSON(filepath.Join(directory, "plan.json"), &binding); err != nil {
		return err
	}
	hash, err := p.Hash()
	if err != nil {
		return err
	}
	if binding.PlanHash != hash || binding.Cluster != p.PrivateEtcd.Cluster {
		return errors.New("private PKI belongs to a different plan")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.PrivateEtcd.StepSeconds)*time.Second)
	defer cancel()
	client, err := openPrivateEtcdClient(ctx, p, directory, "root")
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	status, err := client.AuthStatus(ctx)
	if err != nil {
		return err
	}
	if status.Enabled {
		return errors.New("bootstrap requires fresh cluster with auth not yet enabled")
	}
	if err := checkPrivateEtcdMembership(ctx, p, client); err != nil {
		return err
	}
	if err := ProvisionEtcdRBAC(ctx, p, etcdRBACClient{client}); err != nil {
		return err
	}
	roles, err := EtcdRoles(p)
	if err != nil {
		return err
	}
	for _, role := range roles {
		observed, err := client.RoleGet(ctx, role.User)
		if err != nil {
			return err
		}
		if len(observed.Perm) != len(role.Permissions) {
			return errors.New("private etcd RBAC grants differ from exact ranges")
		}
		for _, permission := range role.Permissions {
			kind := clientv3.PermRead
			if permission.Write {
				kind = clientv3.PermReadWrite
			}
			if !slices.ContainsFunc(observed.Perm, func(v *authpb.Permission) bool {
				return string(v.Key) == permission.Start && string(v.RangeEnd) == permission.End && v.PermType == kind
			}) {
				return errors.New("private etcd RBAC range mismatch")
			}
		}
		roleClient, err := openPrivateEtcdClient(ctx, p, directory, role.User)
		if err != nil {
			return err
		}
		_, readErr := roleClient.Get(ctx, role.Permissions[0].Start)
		_, deniedErr := roleClient.Get(ctx, "/hfs-private-forbidden/"+p.RunID)
		closeErr := roleClient.Close()
		if readErr != nil || !errors.Is(deniedErr, rpctypes.ErrPermissionDenied) || closeErr != nil {
			return errors.Join(errors.New("private etcd role permission probe failed"), readErr, closeErr)
		}
	}
	return nil
}

func (e etcdRBACClient) ApplyEtcdRBAC(ctx context.Context, op EtcdRBACOperation) error {
	var err error
	switch op.Kind {
	case "user":
		_, err = e.client.UserAddWithOptions(ctx, op.User, "", &clientv3.UserAddOptions{NoPassword: true})
	case "role":
		_, err = e.client.RoleAdd(ctx, op.Role)
	case "grant":
		_, err = e.client.UserGrantRole(ctx, op.User, op.Role)
	case "permission":
		kind := clientv3.PermRead
		if op.Write {
			kind = clientv3.PermReadWrite
		}
		_, err = e.client.RoleGrantPermission(ctx, op.Role, op.Start, op.End, clientv3.PermissionType(kind))
	case "enable":
		_, err = e.client.AuthEnable(ctx)
	default:
		err = errors.New("unknown private etcd RBAC operation")
	}
	return err
}

func openPrivateEtcdClient(ctx context.Context, p Plan, directory, user string) (*clientv3.Client, error) {
	for _, name := range []string{"client-ca.pem", user + ".pem", user + "-key.pem"} {
		if _, err := privateEtcdFile(filepath.Join(directory, name)); err != nil {
			return nil, err
		}
	}
	ca, err := os.ReadFile(filepath.Join(directory, "client-ca.pem"))
	if err != nil {
		return nil, err
	}
	if _, err := parseTestEtcdCA(ca, "hfs-private-"+p.RunID+"-client"); err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid private client CA")
	}
	identity, err := tls.LoadX509KeyPair(filepath.Join(directory, user+".pem"), filepath.Join(directory, user+"-key.pem"))
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(identity.Certificate[0])
	if err != nil {
		return nil, err
	}
	if leaf.Subject.CommonName != user {
		return nil, errors.New("private client principal mismatch")
	}
	if leaf.IsCA || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		return nil, errors.New("client certificate has unexpected usage")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, err
	}
	var endpoints []string
	for i := range 3 {
		endpoints = append(endpoints, fmt.Sprintf("https://%s:%d", guestAddress(fmt.Sprintf("hfs-test%d", i)), PrivateEtcdClientPort))
	}
	return clientv3.New(clientv3.Config{Context: ctx, Endpoints: endpoints, TLS: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{identity}, MinVersion: tls.VersionTLS13}, DialTimeout: 2 * time.Second, MaxCallRecvMsgSize: 1 << 20, MaxCallSendMsgSize: 1 << 20, DialOptions: []grpc.DialOption{grpc.WithContextDialer(privateEtcdControllerDialer(p))}})
}
func checkPrivateEtcdMembership(ctx context.Context, p Plan, c *clientv3.Client) error {
	members, err := c.MemberList(ctx)
	if err != nil {
		return err
	}
	if len(members.Members) != 3 {
		return errors.New("private etcd must have exactly three voters")
	}
	seen := map[string]bool{}
	for _, member := range members.Members {
		guest := ""
		for i := range 3 {
			candidate := fmt.Sprintf("hfs-test%d", i)
			if member.Name == "hfs-private-"+p.RunID+"-"+candidate {
				guest = candidate
			}
		}
		if guest == "" || seen[guest] || member.IsLearner || !slices.Equal(member.PeerURLs, []string{fmt.Sprintf("https://%s:%d", guestAddress(guest), PrivateEtcdPeerPort)}) || !slices.Equal(member.ClientURLs, []string{fmt.Sprintf("https://%s:%d", guestAddress(guest), PrivateEtcdClientPort)}) {
			return errors.New("foreign or incomplete private etcd member")
		}
		seen[guest] = true
	}
	for _, endpoint := range c.Endpoints() {
		status, err := c.Status(ctx, endpoint)
		if err != nil {
			return err
		}
		if status.Header.ClusterId != members.Header.ClusterId {
			return errors.New("private etcd endpoints disagree on cluster identity")
		}
	}
	return nil
}
