package labrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// EtcdStepRequest binds one native helper invocation to the exact approved step.
// TLS is secret input transferred separately, never stored in plan/evidence.
type EtcdStepRequest struct {
	Plan          Plan                 `json:"plan"`
	Approval      Approval             `json:"approval"`
	PlanHash      string               `json:"plan_hash"`
	Step          string               `json:"step"`
	Target        string               `json:"target"`
	Operation     string               `json:"operation"`
	Deadline      time.Time            `json:"deadline"`
	ControllerPKI []EtcdControllerFile `json:"controller_pki,omitempty"`
}

// EtcdStepReceipt describes infrastructure effects only. It provides no HFS
// protocol, recovery, fencing or scenario acceptance result.
type EtcdStepReceipt struct {
	PlanHash   string                    `json:"plan_hash"`
	Step       string                    `json:"step"`
	Target     string                    `json:"target"`
	Outcome    string                    `json:"outcome"`
	Terminal   bool                      `json:"terminal"`
	Started    time.Time                 `json:"started"`
	Finished   time.Time                 `json:"finished"`
	Message    string                    `json:"message"`
	Collection *EtcdCollection           `json:"collection,omitempty"`
	Process    *FaultProcess             `json:"process,omitempty"`
	Controller *EtcdControllerEnrollment `json:"controller,omitempty"`
}

// EtcdStepExecutor supplies the approved transport/helper boundary. A lost
// connection or missing terminal receipt remains uncertain and preserves data.
type EtcdStepExecutor interface {
	InvokeEtcdStep(context.Context, EtcdStepRequest) (EtcdStepReceipt, error)
}

// PrivateEtcdSteps returns the concrete helper contracts to insert after lab
// startup and before lab shutdown. The pinned driver owns transport and keeps
// serve helpers alive while subsequent steps run. No step starts a VM itself.
func PrivateEtcdSteps(p Plan) []Step {
	if p.PrivateEtcd == nil {
		return nil
	}
	var out []Step
	for _, phase := range []string{"setup", "collect", "cleanup"} {
		operations := map[string][]string{"setup": {"stage", "tls", "serve"}, "collect": {"collect"}, "cleanup": {"teardown"}}[phase]
		for _, op := range operations {
			for i := range 3 {
				guest := fmt.Sprintf("hfs-test%d", i)
				id := "etcd-" + op + "-" + guest
				s := Step{ID: id, Phase: phase, Action: "driver", Artifact: p.PrivateEtcd.Driver, Args: []string{"--step", id}, Contract: "private-etcd-v1:" + op + ":" + guest, Effects: "run-owned", TimeoutSeconds: p.PrivateEtcd.StepSeconds, MaxAttempts: 1}
				if phase == "collect" {
					s.Effects = "read-only"
				}
				if phase == "cleanup" {
					s.Idempotent = true
					for _, forward := range []string{"stage", "tls", "serve"} {
						s.AppliesAfter = append(s.AppliesAfter, "etcd-"+forward+"-"+guest)
					}
					s.AppliesAfter = append(s.AppliesAfter, "etcd-rbac")
				}
				out = append(out, s)
			}
		}
		if phase == "setup" {
			out = append(out, Step{ID: "etcd-rbac", Phase: "setup", Action: "driver", Artifact: p.PrivateEtcd.Driver, Args: []string{"--step", "etcd-rbac"}, Contract: "private-etcd-v1:rbac:f3", Effects: "run-owned", TimeoutSeconds: p.PrivateEtcd.StepSeconds, MaxAttempts: 1})
		}
	}
	return out
}

// PrepareEtcdStep performs controller-only plan, artifact and approval checks.
// A serve step starts a persistent foreground helper; its step timeout covers
// startup, while Budget.RuntimeSeconds bounds the owned child lifetime.
func PrepareEtcdStep(p Plan, a Approval, id string, now time.Time) (EtcdStepRequest, error) {
	req, err := prepareEtcdStep(p, a, id, now)
	if err == nil && req.Operation == "rbac" {
		return EtcdStepRequest{}, errors.New("RBAC preparation requires the exact controller PKI directory")
	}
	return req, err
}
func prepareEtcdStep(p Plan, a Approval, id string, now time.Time) (EtcdStepRequest, error) {
	if p.PrivateEtcd == nil {
		return EtcdStepRequest{}, errors.New("private etcd plan inputs required")
	}
	s := p.faultStep(id)
	if err := p.CheckApproval(a, now, s.Phase == "cleanup"); err != nil {
		return EtcdStepRequest{}, err
	}
	if err := p.VerifyArtifacts(); err != nil {
		return EtcdStepRequest{}, err
	}
	parts := strings.Split(s.Contract, ":")
	if len(parts) != 3 || parts[0] != "private-etcd-v1" || !etcdStepTarget(parts[1], parts[2]) {
		return EtcdStepRequest{}, errors.New("exact private etcd helper step required")
	}
	hash, err := p.Hash()
	if err != nil {
		return EtcdStepRequest{}, err
	}
	return EtcdStepRequest{Plan: p, Approval: a, PlanHash: hash, Step: id, Target: parts[2], Operation: parts[1], Deadline: now.Add(time.Duration(s.TimeoutSeconds) * time.Second)}, nil
}

// RunEtcdStep requires a matching terminal infrastructure receipt. Exit code
// zero or an arbitrary success document cannot supply protocol test evidence.
func RunEtcdStep(ctx context.Context, p Plan, a Approval, id string, executor EtcdStepExecutor, now time.Time) (EtcdStepReceipt, error) {
	if executor == nil {
		return EtcdStepReceipt{}, errors.New("private etcd executor required")
	}
	req, err := PrepareEtcdStep(p, a, id, now)
	if err != nil {
		return EtcdStepReceipt{}, err
	}
	return runEtcdStepRequest(ctx, req, executor, now)
}

// RunEtcdBootstrapStep prepares source pins and validates the exact f3 receipt.
func RunEtcdBootstrapStep(ctx context.Context, p Plan, a Approval, directory string, executor EtcdStepExecutor, now time.Time) (EtcdStepReceipt, error) {
	if executor == nil {
		return EtcdStepReceipt{}, errors.New("private etcd executor required")
	}
	req, err := PrepareEtcdBootstrapStep(p, a, directory, now)
	if err != nil {
		return EtcdStepReceipt{}, err
	}
	return runEtcdStepRequest(ctx, req, executor, now)
}

func runEtcdStepRequest(ctx context.Context, req EtcdStepRequest, executor EtcdStepExecutor, now time.Time) (EtcdStepReceipt, error) {
	p, id := req.Plan, req.Step
	ctx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	r, invokeErr := executor.InvokeEtcdStep(ctx, req)
	raw, marshalErr := json.Marshal(r)
	shapeErr := validateEtcdStepEvidence(req, r)
	if r.PlanHash != req.PlanHash || r.Step != id || r.Target != req.Target || !r.Terminal || !slices.Contains([]string{"applied", "unsupported", "failed"}, r.Outcome) || r.Started.Before(now) || r.Finished.Before(r.Started) || r.Finished.After(req.Deadline) || len(r.Message) > 4096 || marshalErr != nil || int64(len(raw)) > p.Budget.ArtifactBytes/int64(len(p.Steps)+2) || shapeErr != nil {
		return r, &Error{Category: "infrastructure", Uncertain: true, Err: errors.Join(invokeErr, errors.New("missing, foreign, oversized, late or nonterminal private etcd receipt"))}
	}
	if invokeErr != nil {
		return r, &Error{Category: "infrastructure", Uncertain: true, Err: invokeErr}
	}
	if r.Outcome != "applied" {
		return r, &Error{Category: "infrastructure", Err: fmt.Errorf("private etcd %s: %s", r.Outcome, r.Message)}
	}
	return r, nil
}

func validateEtcdStepEvidence(req EtcdStepRequest, r EtcdStepReceipt) error {
	if req.Operation == "rbac" && r.Outcome == "applied" {
		if err := validateEtcdControllerReceipt(req, r.Controller); err != nil {
			return err
		}
	} else if r.Controller != nil {
		return errors.New("irrelevant controller receipt")
	}
	if req.Operation == "serve" && r.Outcome == "applied" {
		x := r.Process
		if x == nil || x.PID <= 1 || len(x.Birth) != 32 || x.Executable != etcdArgs(req.Plan, req.Target)[0] || x.SHA256 != req.Plan.PrivateEtcd.EtcdSHA256 || x.Handle != "etcd" {
			return errors.New("missing exact owned etcd launch evidence")
		}
	} else if r.Process != nil {
		return errors.New("irrelevant process receipt field")
	}
	if req.Operation == "collect" && r.Outcome == "applied" {
		c := r.Collection
		if c == nil || c.Enrollment.PlanHash != req.PlanHash || c.Enrollment.Target != req.Target || c.Enrollment.RunID != req.Plan.RunID || c.Enrollment.Cluster != req.Plan.PrivateEtcd.Cluster || c.Enrollment.Inode == 0 || c.DirectoryBytes < 0 || c.DirectoryBytes > req.Plan.PrivateEtcd.DiskBytes || int64(len(c.Log)) > req.Plan.PrivateEtcd.MaxLogBytes || c.Observed.Before(r.Started) || c.Observed.After(r.Finished) {
			return errors.New("missing exact bounded etcd collection evidence")
		}
		var resources []Resource
		for _, resource := range req.Plan.Resources {
			if resource.Target == req.Target {
				resources = append(resources, resource)
			}
		}
		if !reflect.DeepEqual(c.RemainingResources, resources) {
			return errors.New("collection resource inventory changed")
		}
	} else if r.Collection != nil {
		return errors.New("irrelevant collection receipt field")
	}
	return nil
}

func validateEtcdSteps(p Plan) error {
	if p.PrivateEtcd == nil {
		return nil
	}
	want := PrivateEtcdSteps(p)
	last := -1
	start := -1
	stop := -1
	for i, s := range p.Steps {
		if s.Action == "start-lab" {
			start = i
		}
		if s.Action == "stop-lab" {
			stop = i
		}
	}
	for _, expected := range want {
		actual := p.faultStep(expected.ID)
		if actual.ID == "" || actual.Contract != expected.Contract || actual.Phase != expected.Phase || actual.Action != "driver" || actual.Artifact != expected.Artifact || actual.Effects != expected.Effects || actual.TimeoutSeconds != expected.TimeoutSeconds || actual.MaxAttempts != 1 || actual.Idempotent != expected.Idempotent || !slices.Equal(actual.AppliesAfter, expected.AppliesAfter) || !slices.Equal(actual.Args, expected.Args) {
			return errors.New("private etcd requires complete exact stage/TLS/serve/collect/teardown contracts")
		}
		index := slices.IndexFunc(p.Steps, func(s Step) bool { return s.ID == expected.ID })
		if index <= last || index <= start || expected.Phase == "cleanup" && index >= stop {
			return errors.New("private etcd setup/collection/teardown must preserve generated order inside the lab lifecycle")
		}
		last = index
	}
	for _, s := range p.Steps {
		if strings.HasPrefix(s.Contract, "private-etcd-v1:") && !slices.ContainsFunc(want, func(v Step) bool { return v.ID == s.ID }) {
			return errors.New("unrecognized private etcd step")
		}
	}
	return nil
}

func validateEtcdStepRequest(req EtcdStepRequest, now time.Time) error {
	if req.Plan.PrivateEtcd == nil {
		return errors.New("private etcd plan inputs required")
	}
	s := req.Plan.faultStep(req.Step)
	if err := req.Plan.CheckApproval(req.Approval, now, s.Phase == "cleanup"); err != nil {
		return err
	}
	hash, err := req.Plan.Hash()
	if err != nil {
		return err
	}
	if hash != req.PlanHash || s.Contract != "private-etcd-v1:"+req.Operation+":"+req.Target || !etcdStepTarget(req.Operation, req.Target) || req.Deadline.Before(now) || req.Deadline.After(now.Add(time.Duration(s.TimeoutSeconds)*time.Second)) {
		return errors.New("private etcd request differs from approved step or deadline")
	}
	if req.Operation == "rbac" {
		return validateEtcdControllerPins(req.Plan, req.ControllerPKI)
	}
	if len(req.ControllerPKI) != 0 {
		return errors.New("controller PKI pins belong only to f3 RBAC request")
	}
	return nil
}

func etcdStepTarget(operation, target string) bool {
	return operation == "rbac" && target == "f3" || operation != "rbac" && nativeGuest(target)
}
