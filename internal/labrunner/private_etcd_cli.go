package labrunner

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// PrivateEtcdCLI provides offline review/PKI generation and explicitly approved
// guest-local helpers. It never invokes SSH, starts a VM or fetches a package.
func PrivateEtcdCLI(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("private etcd command required")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "strict plan or request JSON")
	output := flags.String("out", "", "new review file or private PKI directory")
	tlsInput := flags.String("tls", "", "private selected-member TLS input (native tls only)")
	planPath := flags.String("plan", "", "plan JSON (request only)")
	approvalPath := flags.String("approval", "", "operator approval JSON (request only)")
	step := flags.String("step", "", "exact step ID (request only)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected private etcd positional arguments")
	}
	if args[0] == "private-etcd-request" {
		if *planPath == "" || *approvalPath == "" || *step == "" || *output == "" || *input != "" {
			return errors.New("private-etcd-request requires --plan --approval --step --out")
		}
		var p Plan
		var a Approval
		if err := ReadJSON(*planPath, &p); err != nil {
			return err
		}
		if err := ReadJSON(*approvalPath, &a); err != nil {
			return err
		}
		var req EtcdStepRequest
		var err error
		if *step == "etcd-rbac" {
			req, err = PrepareEtcdBootstrapStep(p, a, *tlsInput, time.Now())
		} else if *tlsInput != "" {
			err = errors.New("controller PKI input belongs only to RBAC request preparation")
		} else {
			req, err = PrepareEtcdStep(p, a, *step, time.Now())
		}
		if err != nil {
			return err
		}
		return WriteJSON(*output, req)
	}
	if *input == "" || *planPath != "" || *approvalPath != "" || *step != "" {
		return errors.New("private etcd command requires only --input and operation-specific output")
	}
	switch args[0] {
	case "private-etcd-plan", "private-etcd-pki":
		if *output == "" || *tlsInput != "" {
			return errors.New("private etcd planning/PKI requires --input and --out")
		}
		var p Plan
		if err := ReadJSON(*input, &p); err != nil {
			return err
		}
		if err := VerifyEtcdBundle(p); err != nil {
			return err
		}
		if args[0] == "private-etcd-pki" {
			pki, err := GenerateEtcdPKI(p, time.Now())
			if err != nil {
				return err
			}
			return WriteEtcdPKI(*output, p, pki)
		}
		ops, err := PrivateEtcdOperations(p)
		if err != nil {
			return err
		}
		hash, err := p.Hash()
		if err != nil {
			return err
		}
		return WriteJSON(*output, struct {
			PlanHash   string           `json:"plan_hash"`
			Operations []EtcdOperation  `json:"operations"`
			Scenarios  []NativeScenario `json:"scenarios"`
		}{hash, ops, NativeScenarios()})
	case "private-etcd-native", "private-etcd-bootstrap":
		if *output != "" {
			return errors.New("native helper writes only its infrastructure receipt to stdout")
		}
		var req EtcdStepRequest
		if err := ReadJSON(*input, &req); err != nil {
			return err
		}
		if err := validateEtcdStepRequest(req, time.Now()); err != nil {
			return err
		}
		if args[0] == "private-etcd-bootstrap" {
			if req.Operation != "rbac" || *tlsInput == "" {
				return errors.New("bootstrap requires the exact RBAC request and private controller PKI directory")
			}
			r := EtcdStepReceipt{PlanHash: req.PlanHash, Step: req.Step, Target: req.Target, Started: time.Now().UTC()}
			ctx, cancel := context.WithDeadline(ctx, req.Deadline)
			defer cancel()
			enrollment, err := BootstrapPrivateEtcd(ctx, req, *tlsInput)
			r.Controller = &enrollment
			r.Finished = time.Now().UTC()
			r.Terminal = err == nil
			r.Outcome = "applied"
			if err != nil {
				r.Outcome = "uncertain"
				r.Message = boundedEtcdMessage(err)
			} else {
				r.Message = "three-member mTLS topology and exact RBAC/role denials checked; bootstrap key retained controller-only"
			}
			return errors.Join(err, json.NewEncoder(out).Encode(r))
		}
		return invokeEtcdNativeCLI(ctx, req, *tlsInput, out)
	default:
		return errors.New("unknown private etcd command")
	}
}

// NewPrivateEtcdIdentity reserves the hfst UUID prefix exclusively for lab
// namespaces and adds 96 fresh random bits. Production must never admit it.
func NewPrivateEtcdIdentity(run string) (cluster, token string, err error) {
	if !validID(run) {
		return "", "", errors.New("bounded private run ID required")
	}
	var id fsapi.UUID
	copy(id[:4], []byte("hfst"))
	if _, err := rand.Read(id[4:]); err != nil {
		return "", "", err
	}
	return id.String(), "hfs-private-" + run + "-" + id.String(), nil
}

// WriteEtcdPKI creates one private controller directory containing a selected
// member input per guest and separate controller-only client identities.
// Every file is mode0600; no CA private key is retained or exported.
func WriteEtcdPKI(directory string, p Plan, pki EtcdPKI) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.PrivateEtcd == nil {
		return errors.New("private etcd inputs required")
	}
	if !cleanAbsolute(directory) {
		return errors.New("absolute new private PKI directory required")
	}
	if err := validateEtcdPrincipalSet(p, pki); err != nil {
		return err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return err
	}
	if err := privateDirectory(directory); err != nil {
		return err
	}
	if err := writeEtcdFile(filepath.Join(directory, "client-ca.pem"), pki.ClientCA, 0600); err != nil {
		return err
	}
	for i := range 3 {
		guest := fmt.Sprintf("hfs-test%d", i)
		selected := EtcdPKI{ClientCA: pki.ClientCA, PeerCA: pki.PeerCA, Members: map[string]map[string]EtcdIdentity{guest: pki.Members[guest]}}
		if err := WriteJSON(filepath.Join(directory, guest+".json"), selected); err != nil {
			return err
		}
	}
	for name, identity := range pki.Clients {
		if err := writeEtcdFile(filepath.Join(directory, name+".pem"), identity.Certificate, 0600); err != nil {
			return err
		}
		if err := writeEtcdFile(filepath.Join(directory, name+"-key.pem"), identity.Key, 0600); err != nil {
			return err
		}
	}
	hash, err := p.Hash()
	if err != nil {
		return err
	}
	return WriteJSON(filepath.Join(directory, "plan.json"), struct {
		PlanHash string `json:"plan_hash"`
		Cluster  string `json:"cluster"`
	}{hash, p.PrivateEtcd.Cluster})
}

func validateEtcdPrincipalSet(p Plan, pki EtcdPKI) error {
	roles, err := EtcdRoles(p)
	if err != nil {
		return err
	}
	expected := map[string]bool{"root": true}
	for _, role := range roles {
		expected[role.User] = true
	}
	if len(pki.Clients) != len(expected) {
		return errors.New("private etcd PKI requires the exact role principal set plus root")
	}
	for name, identity := range pki.Clients {
		if !expected[name] || len(identity.Certificate) == 0 || len(identity.Key) == 0 {
			return errors.New("foreign or incomplete private etcd client principal")
		}
	}
	return nil
}

func invokeEtcdNativeCLI(ctx context.Context, req EtcdStepRequest, tlsInput string, out io.Writer) error {
	if err := admitEtcdNative(req.Plan, req.Approval, req.Target, req.Plan.faultStep(req.Step).Phase == "cleanup"); err != nil {
		return err
	}
	if req.Operation != "tls" && tlsInput != "" {
		return errors.New("TLS material belongs only to the selected member TLS step")
	}
	// CPU soft-limit notification must reach the owning helper's cleanup path;
	// a separate approved hard limit leaves CPU headroom to terminate/reap.
	ctx, stopCPU := signal.NotifyContext(ctx, syscall.SIGXCPU)
	defer stopCPU()
	r := EtcdStepReceipt{PlanHash: req.PlanHash, Step: req.Step, Target: req.Target, Started: time.Now().UTC()}
	var err error
	if req.Operation != "serve" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, req.Deadline)
		defer cancel()
	}
	switch req.Operation {
	case "stage":
		if err = privateEtcdLimits(req.Plan.PrivateEtcd); err == nil {
			err = StagePrivateEtcd(ctx, req.Plan, req.Approval, req.Target, LocalEtcdCommands{})
		}
	case "tls":
		var pki EtcdPKI
		if tlsInput != "" {
			if _, err = privateEtcdFile(tlsInput); err == nil {
				err = ReadJSON(tlsInput, &pki)
			}
		} else {
			err = errors.New("selected member private TLS input required")
		}
		if err == nil {
			err = InstallEtcdMemberTLS(req.Plan, req.Approval, req.Target, pki)
		}
	case "serve":
		if err = privateEtcdLimits(req.Plan.PrivateEtcd); err == nil {
			err = servePrivateEtcd(ctx, req.Plan, req.Approval, req.Target, func(process FaultProcess) error {
				if time.Now().After(req.Deadline) {
					return errors.New("etcd launch exceeded its approved startup deadline")
				}
				r.Terminal = true
				r.Process = &process
				r.Outcome = "applied"
				r.Finished = time.Now().UTC()
				return json.NewEncoder(out).Encode(r)
			})
			if err == nil {
				return nil
			}
		}
	case "collect":
		var collection EtcdCollection
		collection, err = CollectPrivateEtcd(ctx, req.Plan, req.Approval, req.Target)
		if err == nil {
			r.Collection = &collection
		}
	case "teardown":
		err = TeardownPrivateEtcd(ctx, req.Plan, req.Approval, req.Target)
	default:
		err = errors.New("unsupported private etcd native operation")
	}
	r.Finished = time.Now().UTC()
	r.Terminal = err == nil
	r.Outcome = "applied"
	if err != nil {
		r.Outcome = "uncertain"
		r.Message = boundedEtcdMessage(err)
	}
	return errors.Join(err, json.NewEncoder(out).Encode(r))
}

func boundedEtcdMessage(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 4096 {
		return message[:4096]
	}
	return message
}
