package labrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/snonux/homelabfs/internal"
	"github.com/snonux/homelabfs/internal/admin"
)

// FaultEnrollment is written by the reviewed approved setup driver, inside the
// guest's private run root, before opening any replication connection. Dataset
// GUIDs and PF anchor/pipe reservations are live enrollment observations, never
// copied from historical manifests. No function creates this enrollment for you.
type FaultEnrollment struct {
	Outstanding  string                       `json:"outstanding,omitempty"`
	RunID        string                       `json:"run_id"`
	PlanHash     string                       `json:"plan_hash"`
	Target       string                       `json:"target"`
	Node         string                       `json:"node"`
	Volume       string                       `json:"volume"`
	Generation   string                       `json:"generation"`
	DatasetGUIDs map[string]string            `json:"dataset_guids"`
	Networks     map[string]NetworkEnrollment `json:"networks"`
}

// NetworkEnrollment proves a preinstalled exclusive PF anchor and pipe lease.
// Rules/state labels and pipes are reserved before test replication starts.
// The root rules remain untouched. Plain guest root ownership is trusted here;
// it is not a defense against an administrator intentionally editing the lease.
type NetworkEnrollment struct {
	Network           FaultNetwork `json:"network"`
	ParentRulesSHA256 string       `json:"parent_rules_sha256"`
	AnchorRulesSHA256 string       `json:"anchor_rules_sha256"`
	PipeConfigured    bool         `json:"pipe_configured"`
}

// NativeFaultExecutor executes inside the enrolled native guest only. It never
// connects over SSH, targets the physical host, enables PF, creates anchors,
// loads modules, modifies parents, destroys datasets or deletes failed payloads.
// VM stop recipes must instead use the reviewed host lifecycle driver.
type NativeFaultExecutor struct{}

type nativeFaultSession struct {
	req        FaultRequest
	receipt    FaultReceipt
	enrollment FaultEnrollment
	path       string
	logLimit   int64
	lock       *os.File
	run        func(context.Context, FaultCommand) (string, error)
	callAdmin  func(context.Context, string, admin.Request) (admin.Response, error)
}

var _ FaultExecutor = NativeFaultExecutor{}

// InvokeFault enforces run identity and private enrollment before any mutation.
// Local command deadline/unknown exits remain uncertain; a known administrative
// denial is evidence, never an injected-fault pass. Evidence collection is bounded.
func (NativeFaultExecutor) InvokeFault(ctx context.Context, req FaultRequest) (FaultReceipt, error) {
	ctx, cancel := context.WithDeadline(ctx, req.Deadline)
	defer cancel()
	session, err := openNativeFault(req)
	if err != nil {
		return FaultReceipt{}, err
	}
	defer func() { _ = session.lock.Close() }()
	return session.invoke(ctx)
}

func (session *nativeFaultSession) invoke(ctx context.Context) (FaultReceipt, error) {
	req := session.req
	r := &session.receipt
	before, beforeErr := session.status(ctx, req.Fault.Socket)
	r.Evidence["status-before"] = observation(before, beforeErr)
	var identityErr error
	if beforeErr == nil {
		identityErr = exactFaultStatus(resolvedFault(req), before)
	}
	r.Evidence["status-before"] = observation(before, errors.Join(beforeErr, identityErr))
	var actionErr error
	controlUncertain := false
	effective := effectiveFault(req.Plan, resolvedFault(req), req.Cleanup)
	if identityErr != nil {
		actionErr = identityErr
	} else {
		switch effective.Action {
		case "daemon-kill", "daemon-stop", "daemon-continue":
			actionErr = session.signal(ctx)
			controlUncertain = actionErr != nil
		case "failpoint":
			if beforeErr != nil {
				actionErr = beforeErr
				break
			}
			if !before.TestHooksEnabled {
				actionErr = errors.New("daemon test hooks are disabled")
				break
			}
			if actionErr = session.beginEffect(); actionErr != nil {
				break
			}
			_, actionErr = session.adminCall(ctx, req.Fault.Socket, admin.Request{Version: 1, Operation: "test-fault", Fault: "journal-before-sync", Enabled: effective.Enabled, ExpectedNode: effective.Node, ExpectedVolume: effective.Volume, ExpectedGeneration: effective.Generation})
			controlUncertain = actionErr != nil
			if actionErr == nil {
				actionErr = session.endEffect()
			}
		case "quota":
			actionErr = session.quota(ctx)
		case "network-loss", "network-delay", "network-partition", "network-reset", "network-clear":
			actionErr = session.network(ctx)
		case "collect":
		case "gnop-error":
			r.Outcome = "unsupported"
			actionErr = errors.New("gnop cannot safely isolate an existing pool's child run dataset")
		default:
			actionErr = errors.New("VM faults require the enrolled host lifecycle backend")
		}
	}
	if r.Outcome == "" {
		if actionErr == nil {
			r.Outcome = "applied"
		} else {
			r.Outcome = "failed"
		}
	}
	if actionErr != nil {
		r.Message = actionErr.Error()
	}
	after, afterErr := session.status(ctx, req.Fault.Socket)
	r.Evidence["status-after"] = observation(after, afterErr)
	session.collect(ctx)
	r.Evidence["action"] = observation(map[string]any{"commands": req.Commands, "outcome": r.Outcome}, actionErr)
	r.Finished = time.Now().UTC()
	r.Terminal = ctx.Err() == nil && !controlUncertain && session.enrollment.Outstanding == ""
	r.Evidence["timing"] = observation(map[string]any{"started": r.Started, "finished": r.Finished, "deadline": req.Deadline}, nil)
	if ctx.Err() != nil {
		return *r, ctx.Err()
	}
	if !r.Terminal {
		return *r, errors.New("fault effect outstanding; inspect preserved enrollment and audit")
	}
	return *r, nil
}

func openNativeFault(req FaultRequest) (*nativeFaultSession, error) {
	p := req.Plan
	if err := p.CheckApproval(req.Approval, time.Now(), req.Cleanup); err != nil {
		return nil, err
	}
	h, err := p.Hash()
	if err != nil {
		return nil, err
	}
	step := p.faultStep(req.Step)
	if req.Deadline.After(time.Now().Add(time.Duration(step.TimeoutSeconds) * time.Second)) {
		return nil, errors.New("fault deadline exceeds approved step")
	}
	if req.Protocol != "hfs-native-fault-v1" || req.PlanHash != h || req.RunID != p.RunID || req.MaxEvidenceBytes <= 0 || req.MaxEvidenceBytes > p.Budget.ArtifactBytes/int64(len(p.Steps)+2) || req.Deadline.Before(time.Now()) {
		return nil, errors.New("invalid fault request binding/budget/deadline")
	}
	expectedSeed := int64(0)
	for _, selection := range p.Tests {
		if selection.Name == p.faultStep(req.Fault.Step).Test {
			expectedSeed = selection.Seed
		}
	}
	if req.Seed != expectedSeed {
		return nil, errors.New("request seed differs from approved selection")
	}
	found := false
	for _, f := range p.Faults {
		found = found || reflect.DeepEqual(f, req.Fault) && (f.Step == req.Step && !req.Cleanup || f.CleanupStep == req.Step && req.Cleanup)
	}
	commands, err := faultRequestCommands(p, req.Fault, req.Cleanup, req.Binding)
	if err != nil {
		return nil, err
	}
	if !found || !reflect.DeepEqual(commands, req.Commands) {
		return nil, errors.New("fault request differs from approved recipe")
	}
	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	if hostname != req.Fault.Target {
		return nil, errors.New("fault backend must run inside exact enrolled guest")
	}
	root := "/var/tmp/hfs-run-" + p.RunID
	if err := privateDirectory(root); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(root, "fault.lock"), os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("another fault action is outstanding")
	}
	s := &nativeFaultSession{req: req, path: filepath.Join(root, "fault-enrollment.json"), logLimit: req.MaxEvidenceBytes / 16, lock: lock}
	if err := readOwnedJSON(s.path, &s.enrollment); err != nil {
		_ = lock.Close()
		return nil, err
	}
	e := s.enrollment
	f := resolvedFault(req)
	if req.Fault.GenerationBinding != "" {
		if req.Binding == nil {
			_ = lock.Close()
			return nil, errors.New("runtime binding missing")
		}
		var local ServingBinding
		if err := readOwnedJSON(filepath.Join(filepath.Dir(f.Socket), "supervisor-"+f.Process.Handle+".serving.json"), &local); err != nil {
			_ = lock.Close()
			return nil, err
		}
		if !reflect.DeepEqual(local, *req.Binding) {
			_ = lock.Close()
			return nil, errors.New("controller/native readiness binding differs")
		}
	}
	if e.Outstanding != "" || e.RunID != req.RunID || e.PlanHash != req.PlanHash || e.Target != f.Target || e.Node != f.Node || e.Volume != f.Volume || e.Generation != f.Generation {
		_ = lock.Close()
		return nil, errors.New("fault enrollment identity mismatch")
	}
	s.receipt = FaultReceipt{Protocol: req.Protocol, PlanHash: h, RunID: req.RunID, Step: req.Step, Target: f.Target, Node: f.Node, Volume: f.Volume, Generation: f.Generation, Seed: req.Seed, Started: time.Now().UTC(), RemainingResources: append([]Resource(nil), p.Resources...), Evidence: map[string]json.RawMessage{}}
	if f.Process != nil {
		var process FaultProcess
		if err := readOwnedJSON(filepath.Join(filepath.Dir(f.Socket), "supervisor-"+f.Process.Handle+".json"), &process); err != nil {
			_ = lock.Close()
			return nil, err
		}
		if process.Handle != f.Process.Handle || process.Executable != f.Process.Executable || process.SHA256 != f.Process.SHA256 || process.PID <= 1 || len(process.Birth) != 32 {
			_ = lock.Close()
			return nil, errors.New("supervisor enrollment mismatch")
		}
		s.receipt.Process = &process
	}
	return s, nil
}

func (s *nativeFaultSession) signal(ctx context.Context) error {
	f := effectiveFault(s.req.Plan, resolvedFault(s.req), s.req.Cleanup)
	directory := filepath.Dir(f.Socket)
	path := filepath.Join(directory, "supervisor-"+f.Process.Handle+".json")
	var process FaultProcess
	if err := readOwnedJSON(path, &process); err != nil {
		return err
	}
	if process.Handle != f.Process.Handle || process.Executable != f.Process.Executable || process.SHA256 != f.Process.SHA256 || process.PID <= 1 || len(process.Birth) != 32 {
		return errors.New("supervisor manifest identity mismatch")
	}
	s.receipt.Process = &process
	socket := filepath.Join(directory, "supervisor-"+process.Handle+".sock")
	status, err := s.status(ctx, socket)
	if err != nil {
		return err
	}
	supervisorTarget := f
	supervisorTarget.Generation = process.Birth
	if err := exactFaultStatus(supervisorTarget, status); err != nil {
		return err
	}
	if !status.TestHooksEnabled {
		return errors.New("supervisor admission is closed")
	}
	if err := s.beginEffect(); err != nil {
		return err
	}
	_, err = admin.Call(ctx, socket, admin.Request{Version: 1, Operation: "test-fault", Fault: signalToken(f.Action, process), Enabled: true, ExpectedNode: f.Node, ExpectedVolume: f.Volume, ExpectedGeneration: process.Birth})
	if err != nil {
		return err
	}
	return s.endEffect()
}
func (s *nativeFaultSession) quota(ctx context.Context) error {
	f := effectiveFault(s.req.Plan, resolvedFault(s.req), s.req.Cleanup)
	r := s.req.Plan.faultResource(f.Resource)
	guid := s.enrollment.DatasetGUIDs[r.Location]
	if guid == "" {
		return errors.New("dataset GUID is not enrolled")
	}
	for property, expected := range map[string]string{"guid": guid, "org.homelabfs:run": s.req.RunID} {
		output, err := s.command(ctx, FaultCommand{Path: "/sbin/zfs", Args: []string{"get", "-H", "-o", "value", property, r.Location}})
		if err != nil {
			return err
		}
		if strings.TrimSpace(output) != expected {
			return errors.New("live dataset identity mismatch")
		}
	}
	before, err := s.command(ctx, FaultCommand{Path: "/sbin/zfs", Args: []string{"get", "-Hp", "-o", "value", "quota", r.Location}})
	if err != nil {
		return err
	}
	expectedBefore := r.MaxBytes
	if s.req.Cleanup {
		expectedBefore = s.req.Fault.QuotaBytes
	}
	if s.req.Cleanup && strings.TrimSpace(before) == fmt.Sprint(r.MaxBytes) {
		return nil
	}
	if strings.TrimSpace(before) != fmt.Sprint(expectedBefore) {
		return errors.New("child dataset quota differs from enrolled approved cap")
	}
	if err := s.beginEffect(); err != nil {
		return err
	}
	if _, err := s.command(ctx, s.req.Commands[0]); err != nil {
		return err
	}
	after, err := s.command(ctx, FaultCommand{Path: "/sbin/zfs", Args: []string{"get", "-Hp", "-o", "value", "quota", r.Location}})
	s.receipt.Evidence["quota"] = observation(map[string]string{"guid": guid, "before": before, "after": after}, err)
	if err != nil {
		return err
	}
	if strings.TrimSpace(after) != fmt.Sprint(f.QuotaBytes) {
		return errors.New("quota effect not observed")
	}
	return s.endEffect()
}
func (s *nativeFaultSession) network(ctx context.Context) error {
	f := effectiveFault(s.req.Plan, resolvedFault(s.req), s.req.Cleanup)
	n := f.Network
	lease, ok := s.enrollment.Networks[f.ID]
	if !ok || !reflect.DeepEqual(lease.Network, *s.req.Fault.Network) {
		return errors.New("network lease is missing or differs from approved link")
	}
	info, err := s.command(ctx, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-si"}})
	if err != nil || !strings.Contains(info, "Status: Enabled") {
		return errors.New("PF must already be enabled by approved setup")
	}
	parent, err := s.command(ctx, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-sr"}})
	if err != nil {
		return err
	}
	anchor, err := s.command(ctx, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-a", n.Anchor, "-sr"}})
	if err != nil {
		return err
	}
	if !sha256Text(lease.ParentRulesSHA256) || !sha256Text(lease.AnchorRulesSHA256) || textDigest(parent) != lease.ParentRulesSHA256 || textDigest(anchor) != lease.AnchorRulesSHA256 || !strings.Contains(parent, `anchor "hfs-run-`+s.req.RunID+`/*"`) {
		return errors.New("PF enrollment/attachment changed; preserve rules")
	}
	allRules, err := s.command(ctx, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-a", "*", "-sr"}})
	if err != nil {
		return err
	}
	for _, ownedLabel := range networkLabels(s.req.RunID, f.ID) {
		label := `label "` + ownedLabel + `"`
		if strings.Count(anchor, label) != 1 || strings.Count(allRules, label) != 1 {
			return errors.New("run state label is not exclusive across PF anchors")
		}
	}

	// The setup driver must reserve each numeric pipe exclusively. Refuse a pipe
	// existing before our first configuration; never adopt an unmanaged pipe.
	pipes, err := s.command(ctx, FaultCommand{Path: "/sbin/dnctl", Args: []string{"pipe", "show"}})
	if err != nil {
		return err
	}
	exists := false
	for _, line := range strings.Split(pipes, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.TrimSuffix(fields[0], ":") == fmt.Sprint(n.Pipe) {
			exists = true
		}
	}
	if exists != lease.PipeConfigured {
		return errors.New("dummynet pipe ownership changed")
	}
	if err := s.beginEffect(); err != nil {
		return err
	}
	for _, command := range s.req.Commands {
		if command.Path == "/sbin/dnctl" && len(command.Args) > 1 && command.Args[1] == "delete" && !lease.PipeConfigured {
			continue
		}
		if _, err := s.command(ctx, command); err != nil {
			return err
		}
	}
	after, err := s.command(ctx, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-sr"}})
	if err != nil {
		return err
	}
	if after != parent {
		return errors.New("unrelated parent PF rules changed; preserve failed network resources")
	}
	afterAnchor, err := s.command(ctx, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-a", n.Anchor, "-sr"}})
	if err != nil {
		return err
	}
	lease.AnchorRulesSHA256 = textDigest(afterAnchor)
	lease.PipeConfigured = f.Action == "network-loss" || f.Action == "network-delay"
	s.enrollment.Networks[f.ID] = lease
	s.enrollment.Outstanding = ""
	return replaceJSON(s.path, s.enrollment)
}
func (s *nativeFaultSession) status(ctx context.Context, socket string) (admin.Status, error) {
	if socket == "" {
		return admin.Status{}, errors.New("admin endpoint not configured")
	}
	response, err := s.adminCall(ctx, socket, admin.Request{Version: 1, Operation: "status"})
	if err != nil {
		return admin.Status{}, err
	}
	if response.Status == nil {
		return admin.Status{}, errors.New("admin status missing")
	}
	return *response.Status, nil
}
func (s *nativeFaultSession) collect(ctx context.Context) {
	log, err := readOwnedBounded(s.req.Fault.Log, s.logLimit)
	s.receipt.Evidence["daemon-logs"] = observation(map[string]string{"path": s.req.Fault.Log, "content": string(log)}, err)
	auditPath := filepath.Join(filepath.Dir(s.req.Fault.Socket), "admin-"+s.req.Fault.Volume+".audit.jsonl")
	audit, auditErr := readOwnedBounded(auditPath, s.logLimit)
	audits := map[string]any{"daemon_path": auditPath, "daemon_audit": string(audit)}
	if process := s.req.Fault.Process; process != nil {
		supervisorAudit, supervisorErr := readOwnedBounded(filepath.Join(filepath.Dir(s.req.Fault.Socket), process.Handle+".audit.jsonl"), s.logLimit)
		audits["supervisor_audit"] = string(supervisorAudit)
		auditErr = errors.Join(auditErr, supervisorErr)
	}
	s.receipt.Evidence["admin-audit"] = observation(audits, auditErr)

	kernel, kernelErr := s.command(ctx, FaultCommand{Path: "/usr/bin/uname", Args: []string{"-a"}})
	packages, packageErr := s.command(ctx, FaultCommand{Path: "/usr/local/sbin/pkg", Args: []string{"query", "%n-%v"}})
	s.receipt.Evidence["versions"] = observation(map[string]any{"kernel": kernel, "packages": packages, "runner": internal.Version, "artifacts": s.req.Plan.Artifacts}, errors.Join(kernelErr, packageErr))
	s.receipt.Evidence["resource-inventory"] = observation(map[string]any{"may_remain": s.receipt.RemainingResources, "enrollment": s.enrollment, "payloads_preserved": true}, nil)
}
func (s *nativeFaultSession) command(ctx context.Context, c FaultCommand) (string, error) {
	if s.run != nil {
		return s.run(ctx, c)
	}
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	cmd.Stdin = strings.NewReader(c.Stdin)
	out := &adapterOutput{remaining: s.logLimit}
	cmd.Stdout = adapterStream{output: out}
	cmd.Stderr = adapterStream{output: out, stderr: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/usr/local/sbin"}
	err := cmd.Run()
	stdout, stderr, exceeded := out.snapshot()
	if exceeded {
		err = errors.Join(err, errors.New("native command evidence cap exceeded"))
	}
	if err != nil {
		return stdout, fmt.Errorf("%s: %w: %s", c.Path, err, stderr)
	}
	return stdout, nil
}
func exactFaultStatus(f Fault, status admin.Status) error {
	if status.NodeID != f.Node || len(status.Volumes) != 1 || status.Volumes[0].VolumeID != f.Volume || status.Volumes[0].Generation != f.Generation {
		return errors.New("admin node/volume/generation mismatch")
	}
	return nil
}
func textDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
func observation(value any, err error) json.RawMessage {
	entry := map[string]any{"observation": value}
	if err != nil {
		entry["error"] = err.Error()
	}
	raw, _ := json.Marshal(entry)
	return raw
}
func readOwnedJSON(path string, value any) error {
	raw, err := readOwnedBounded(path, 4<<20)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("trailing enrollment JSON")
	}
	return nil
}
func readOwnedBounded(path string, max int64) ([]byte, error) {
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0600 || stat.Nlink != 1 {
		return nil, errors.New("evidence/enrollment must be owned mode 0600 single-link regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return raw[:max], errors.New("evidence file truncated at approved bound")
	}
	return raw, nil
}

func (s *nativeFaultSession) beginEffect() error {
	if s.enrollment.Outstanding != "" {
		return errors.New("a prior native effect remains outstanding")
	}
	s.enrollment.Outstanding = s.req.Step
	return replaceJSON(s.path, s.enrollment)
}
func (s *nativeFaultSession) endEffect() error {
	s.enrollment.Outstanding = ""
	return replaceJSON(s.path, s.enrollment)
}

func (s *nativeFaultSession) adminCall(ctx context.Context, socket string, request admin.Request) (admin.Response, error) {
	if s.callAdmin != nil {
		return s.callAdmin(ctx, socket, request)
	}
	if s.req.Binding != nil && socket == s.req.Fault.Socket {
		return admin.CallProcess(ctx, socket, request, s.req.Binding.Process.PID)
	}
	return admin.Call(ctx, socket, request)
}
