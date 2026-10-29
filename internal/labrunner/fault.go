package labrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
)

// Fault is an exact, plan-bound native test action, never fencing authority.
// Optional fields are forbidden outside the action that consumes them.
type Fault struct {
	ID                string        `json:"id"`
	Step              string        `json:"step"`
	CleanupStep       string        `json:"cleanup_step"`
	Target            string        `json:"target"`
	Action            string        `json:"action"`
	Resource          string        `json:"resource"`
	Node              string        `json:"node"`
	Volume            string        `json:"volume"`
	Generation        string        `json:"generation"`
	GenerationBinding string        `json:"generation_binding,omitempty"`
	Socket            string        `json:"socket"`
	Log               string        `json:"log"`
	Process           *FaultProcess `json:"process,omitempty"`
	Network           *FaultNetwork `json:"network,omitempty"`
	QuotaBytes        int64         `json:"quota_bytes,omitempty"`
	Enabled           bool          `json:"enabled"`
}

// FaultProcess binds a descriptor-owning supervisor to one enrolled executable
// and birth identity. A reusable PID or ps-then-kill is never sufficient.
type FaultProcess struct {
	PID        int    `json:"pid"`
	Birth      string `json:"birth"`
	Executable string `json:"executable"`
	SHA256     string `json:"sha256"`
	Handle     string `json:"handle"`
}

// FaultNetwork reserves an already attached, run-owned PF anchor and dummynet
// pipe. The adapter must fail closed on collisions or unmanaged parent rules.
// TCP ports are dedicated test replication ports, never SSH/control ports.
type FaultNetwork struct {
	Peer        string `json:"peer"`
	Port        uint16 `json:"port"`
	Anchor      string `json:"anchor"`
	Pipe        uint16 `json:"pipe"`
	Direction   string `json:"direction"`
	LossPercent int    `json:"loss_percent"`
	DelayMillis int    `json:"delay_millis"`
}

// FaultCommand describes exact argv/stdin plus the prerequisite checks a native
// backend must establish while holding its enrollment/launch lock. It is not a
// shell program. No backend that contacts the lab is installed by this package.
type FaultCommand struct {
	Target string   `json:"target"`
	Path   string   `json:"path"`
	Args   []string `json:"args"`
	Stdin  string   `json:"stdin,omitempty"`
	Checks []string `json:"checks"`
}

// FaultRequest carries approval identity and a bounded command recipe. The
// executor must retain outstanding operations on transport loss, never retry.
type FaultRequest struct {
	Protocol         string          `json:"protocol"`
	Plan             Plan            `json:"plan"`
	Approval         Approval        `json:"approval"`
	PlanHash         string          `json:"plan_hash"`
	RunID            string          `json:"run_id"`
	Step             string          `json:"step"`
	Cleanup          bool            `json:"cleanup"`
	Fault            Fault           `json:"fault"`
	Binding          *ServingBinding `json:"binding,omitempty"`
	Seed             int64           `json:"seed"`
	Deadline         time.Time       `json:"deadline"`
	MaxEvidenceBytes int64           `json:"max_evidence_bytes"`
	Commands         []FaultCommand  `json:"commands"`
}

// FaultReceipt preserves raw bounded evidence and exact observed identities.
// Terminal requires that no child or remote operation remains outstanding.
type FaultReceipt struct {
	Protocol           string                     `json:"protocol"`
	PlanHash           string                     `json:"plan_hash"`
	RunID              string                     `json:"run_id"`
	Step               string                     `json:"step"`
	Target             string                     `json:"target"`
	Node               string                     `json:"node"`
	Volume             string                     `json:"volume"`
	Generation         string                     `json:"generation"`
	Seed               int64                      `json:"seed"`
	Started            time.Time                  `json:"started"`
	Finished           time.Time                  `json:"finished"`
	Terminal           bool                       `json:"terminal"`
	Outcome            string                     `json:"outcome"`
	Message            string                     `json:"message"`
	Process            *FaultProcess              `json:"process,omitempty"`
	Evidence           map[string]json.RawMessage `json:"evidence"`
	RemainingResources []Resource                 `json:"remaining_resources"`
}

// FaultExecutor implements native effects and collection under a pinned driver.
// It must atomically verify run enrollment, real paths/dataset GUIDs, private
// admin socket owner/target and descriptor-held process identities before use.
// Missing native capability returns a terminal unsupported receipt, never a pass.
type FaultExecutor interface {
	InvokeFault(context.Context, FaultRequest) (FaultReceipt, error)
}

// RunFaultStep verifies fresh exact approval before calling a supplied executor.
// Receipt errors after invocation are uncertain, preserving the runner's normal
// reconciliation boundary. This API does not manufacture approval or fencing.
func RunFaultStep(ctx context.Context, p Plan, a Approval, id string, executor FaultExecutor, now time.Time) (FaultReceipt, error) {
	if executor == nil {
		return FaultReceipt{}, errors.New("native fault executor is unavailable")
	}
	req, err := PrepareFaultRequest(p, a, id, now)
	if err != nil {
		return FaultReceipt{}, err
	}
	step := p.faultStep(req.Step)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
	defer cancel()
	receipt, invokeErr := executor.InvokeFault(ctx, req)
	if err := validateFaultReceipt(p, req, receipt); err != nil {
		return receipt, &Error{Category: "infrastructure", Uncertain: true, Err: errors.Join(invokeErr, err)}
	}
	if invokeErr != nil {
		return receipt, &Error{Category: "infrastructure", Uncertain: true, Err: invokeErr}
	}
	if receipt.Outcome != "applied" {
		return receipt, &Error{Category: "infrastructure", Err: fmt.Errorf("fault %s: %s: %s", id, receipt.Outcome, receipt.Message)}
	}
	return receipt, nil
}

// PrepareFaultRequest performs controller-only validation and builds one exact
// bounded native request. It has no lab contact or authority to approve a run.
func PrepareFaultRequest(p Plan, a Approval, id string, now time.Time) (FaultRequest, error) {
	cleanup := p.faultStep(id).Phase == "cleanup"
	if err := p.CheckApproval(a, now, cleanup); err != nil {
		return FaultRequest{}, err
	}
	if err := p.VerifyArtifacts(); err != nil {
		return FaultRequest{}, err
	}
	var f Fault
	for _, candidate := range p.Faults {
		if candidate.Step == id || candidate.CleanupStep == id {
			f = candidate
		}
	}
	if f.ID == "" {
		return FaultRequest{}, errors.New("fault is absent from approved plan")
	}
	step := p.faultStep(id)
	binding, err := loadServingBinding(p, f)
	if err != nil {
		return FaultRequest{}, err
	}
	commands, err := faultRequestCommands(p, f, cleanup, binding)
	if err != nil {
		return FaultRequest{}, err
	}
	h, err := p.Hash()
	if err != nil {
		return FaultRequest{}, err
	}
	req := FaultRequest{Protocol: "hfs-native-fault-v1", Plan: p, Approval: a, PlanHash: h, RunID: p.RunID, Step: step.ID, Cleanup: cleanup, Fault: f, Binding: binding, Deadline: now.Add(time.Duration(step.TimeoutSeconds) * time.Second), MaxEvidenceBytes: p.Budget.ArtifactBytes / int64(len(p.Steps)+2), Commands: commands}
	for _, selection := range p.Tests {
		if selection.Name == p.faultStep(f.Step).Test {
			req.Seed = selection.Seed
		}
	}

	return req, nil
}

// FaultCommands builds deterministic recipes only; it never invokes commands.
// A descriptor signal and connection reset are typed backend operations because
// bare PID signals and global PF state killing cannot establish the scope.
func FaultCommands(p Plan, f Fault) ([]FaultCommand, error) {
	if err := p.validateFault(f); err != nil {
		return nil, err
	}
	if f.GenerationBinding != "" {
		return nil, errors.New("runtime generation requires its explicit prepared readiness binding")
	}
	return buildFaultCommands(p, f)
}
func buildFaultCommands(p Plan, f Fault) ([]FaultCommand, error) {
	checks := []string{"run-enrollment", "exact-guest-identity", "real-owned-paths", "remaining-resource-inventory"}
	command := FaultCommand{Target: f.Target, Checks: checks}
	switch f.Action {
	case "daemon-kill", "daemon-stop", "daemon-continue":
		command.Path = "supervisor-signal"
		command.Args = []string{f.Process.Handle, map[string]string{"daemon-kill": "KILL", "daemon-stop": "STOP", "daemon-continue": "CONT"}[f.Action]}
		command.Checks = append(command.Checks, "owned-unreaped-child-pid-birth-executable")
	case "vm-stop-clean", "vm-stop-abrupt":
		command.Target = "f3"
		command.Path = "vms.sh"
		command.Args = []string{"--approved-fault", p.RunID, f.Target, strings.TrimPrefix(f.Action, "vm-stop-")}
		command.Checks = append(command.Checks, "locked-vm-launch-enrollment", "not-fence-proof")
	case "quota":
		resource := p.faultResource(f.Resource)
		command.Path = "/sbin/zfs"
		command.Args = []string{"set", "quota=" + strconv.FormatInt(f.QuotaBytes, 10), resource.Location}
		command.Checks = append(command.Checks, "dataset-guid-and-run-property", "quota-before-and-after", "preserve-payload")
	case "failpoint":
		command.Path = "admin-v1"
		request := admin.Request{Version: 1, Operation: "test-fault", Fault: "journal-before-sync", Enabled: f.Enabled, ExpectedNode: f.Node, ExpectedVolume: f.Volume, ExpectedGeneration: f.Generation}
		raw, err := json.Marshal(request)
		if err != nil {
			return nil, err
		}
		command.Args = []string{f.Socket}
		command.Stdin = string(raw) + "\n"
		command.Checks = append(command.Checks, "admin.Call-private-socket-peer", "exact-node-volume-generation", "hooks-enabled", "audit-request-and-completion")
	case "network-reset", "network-partition", "network-loss", "network-delay", "network-clear":
		return networkCommands(p, f, command), nil
	case "gnop-error":
		command.Path = "unsupported"
		command.Args = []string{"gnop on existing pools is unproven and cannot target run-owned child datasets"}
	case "collect":
		command.Path = "collect-native-evidence"
		command.Args = []string{f.Socket, p.faultResource(f.Resource).Location}
	}
	return []FaultCommand{command}, nil
}

func (p Plan) validateFaults() error {
	if len(p.Faults) > 128 || len(p.Faults) > 0 && !p.PreserveFailures {
		return errors.New("fault plans require bounded entries and preservation of failures")
	}
	seen := map[string]bool{}
	for _, f := range p.Faults {
		if seen[f.ID] {
			return errors.New("duplicate fault ID")
		}
		seen[f.ID] = true
		if err := p.validateFault(f); err != nil {
			return fmt.Errorf("fault %s: %w", f.ID, err)
		}
		step := p.faultStep(f.Step)
		if step.ID == "" || step.Action != "driver" || step.Contract != "native-fault-v1:"+f.ID || len(step.Args) != 0 {
			return errors.New("fault requires exact driver contract and no custom argv")
		}
		if f.Action == "collect" {
			if step.Phase != "collect" || step.Effects != "read-only" {
				return errors.New("evidence fault must be read-only collection")
			}
		} else if step.Phase != "test" || step.Effects != "run-owned" || !contains(p.Disruptions, "fault:"+f.ID) {
			return errors.New("fault mutation requires a selected test and explicit disruption")
		}
	}
	for _, f := range p.Faults {
		if f.Action != "collect" {
			cleanup := p.faultStep(f.CleanupStep)
			if cleanup.ID == "" || cleanup.Phase != "cleanup" || cleanup.Action != "driver" || cleanup.Contract != "native-fault-cleanup-v1:"+f.ID || cleanup.Effects != "run-owned" || !cleanup.Idempotent || !contains(cleanup.AppliesAfter, f.Step) || len(cleanup.Args) != 0 {
				return errors.New("fault requires exact paired native cleanup")
			}
		}
	}
	for _, step := range p.Steps {
		if strings.HasPrefix(step.Contract, "native-fault-cleanup-v1:") && !seen[strings.TrimPrefix(step.Contract, "native-fault-cleanup-v1:")] {
			return errors.New("fault cleanup names absent request")
		}
		if strings.HasPrefix(step.Contract, "native-fault-v1:") && !seen[strings.TrimPrefix(step.Contract, "native-fault-v1:")] {
			return errors.New("fault contract names absent request")
		}
	}
	return nil
}

func (p Plan) validateFault(f Fault) error {
	if p.Stage != "stage1" || !validID(p.RunID) || !validID(f.ID) || !nativeGuest(f.Target) || !validID(f.Node) || !validID(f.Volume) || (f.GenerationBinding == "" && !validID(f.Generation)) || (f.GenerationBinding != "" && (f.GenerationBinding != "runtime-readiness-v1" || f.Generation != "")) {
		return errors.New("native fault requires run and exact guest/node/volume/generation")
	}
	enrolled := false
	for _, target := range p.Targets {
		enrolled = enrolled || target.Name == f.Target && target.Host == "f3"
	}
	resource := p.faultResource(f.Resource)
	if !enrolled || resource.Name == "" || resource.Target != f.Target || !p.validResource(resource) {
		return errors.New("fault must target an enrolled guest and its explicit run resource")
	}
	if f.Log == "" || !safeRunPath(p, f.Target, f.Log) {
		return errors.New("bounded run-owned daemon log required")
	}
	if f.Socket != "" && (!safeRunPath(p, f.Target, f.Socket) || filepath.Base(f.Socket) != "admin-"+f.Volume+".sock") {
		return errors.New("admin socket must be an explicitly enrolled private run path")
	}
	signal := contains([]string{"daemon-kill", "daemon-stop", "daemon-continue"}, f.Action)
	network := strings.HasPrefix(f.Action, "network-")
	needsProcess := signal || f.GenerationBinding == "runtime-readiness-v1"
	if needsProcess != (f.Process != nil) || network != (f.Network != nil) || f.Action != "quota" && f.QuotaBytes != 0 || f.Action != "failpoint" && f.Enabled {
		return errors.New("irrelevant fault fields are forbidden")
	}
	if needsProcess && (f.Process.PID != 0 || f.Process.Birth != "" || f.Process.Handle == "" || !validID(f.Process.Handle) || !safeRunPath(p, f.Target, f.Process.Executable) || !sha256Text(f.Process.SHA256)) {
		return errors.New("signals require a logical supervisor handle and run executable digest; PID/birth are enrolled at launch")
	}
	switch f.Action {
	case "daemon-kill", "daemon-stop", "daemon-continue":
		if f.Socket == "" {
			return errors.New("supervisor control directory required via admin socket")
		}
	case "vm-stop-clean", "vm-stop-abrupt", "gnop-error":
	case "quota":
		root := "hfs-run-" + p.RunID
		if resource.Kind != "dataset" || !strings.Contains(resource.Location, "/"+root+"/") || f.QuotaBytes <= 0 || f.QuotaBytes > resource.MaxBytes {
			return errors.New("quota fault needs a bounded child run dataset; parent quota is forbidden")
		}
	case "failpoint", "collect":
		if f.Socket == "" {
			return errors.New("admin socket required")
		}
	case "network-loss", "network-delay", "network-partition", "network-reset", "network-clear":
		if err := p.validateFaultNetwork(f); err != nil {
			return err
		}
	default:
		return errors.New("unsupported native fault")
	}
	return nil
}
func (p Plan) validateFaultNetwork(f Fault) error {
	n := f.Network
	if !nativeGuest(n.Peer) || n.Peer == f.Target || n.Port < 1024 || n.Port == 2379 || n.Port == 2380 || n.Anchor != "hfs-run-"+p.RunID+"/"+f.ID || len(networkLabels(p.RunID, f.ID)[0]) > 63 || n.Pipe < 10000 || n.Direction != "in" && n.Direction != "out" || n.LossPercent < 0 || n.LossPercent > 100 || n.DelayMillis < 0 || n.DelayMillis > 60000 {
		return errors.New("network faults require exact dedicated test peer/port and owned anchor/pipe")
	}
	if f.Action == "network-loss" && (n.LossPercent == 0 || n.DelayMillis != 0) || f.Action == "network-delay" && (n.DelayMillis == 0 || n.LossPercent != 0) || f.Action != "network-loss" && n.LossPercent != 0 || f.Action != "network-delay" && n.DelayMillis != 0 {
		return errors.New("network action does not match bounded parameters")
	}
	for _, other := range p.Faults {
		if other.ID != f.ID && other.Target == f.Target && other.Network != nil && (other.Network.Pipe == n.Pipe || other.Network.Anchor == n.Anchor) {
			return errors.New("network resource collision")
		}
	}
	return nil
}
func (p Plan) faultStep(id string) Step {
	for _, s := range p.Steps {
		if s.ID == id {
			return s
		}
	}
	return Step{}
}
func (p Plan) faultResource(id string) Resource {
	for _, r := range p.Resources {
		if r.Name == id {
			return r
		}
	}
	return Resource{}
}
func nativeGuest(name string) bool {
	return name == "hfs-test0" || name == "hfs-test1" || name == "hfs-test2"
}
func safeRunPath(p Plan, target, path string) bool {
	if !cleanAbsolute(path) {
		return false
	}
	for _, r := range p.Resources {
		if r.Target == target && r.Kind == "directory" && p.validResource(r) && (path == r.Location || strings.HasPrefix(path, r.Location+"/")) {
			return true
		}
	}
	return false
}
func sha256Text(s string) bool { return len(s) == 64 && strings.Trim(s, "0123456789abcdef") == "" }
