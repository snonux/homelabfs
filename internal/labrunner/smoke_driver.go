package labrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

// SmokeRequest is the versioned, bounded deployment-adapter input. The adapter
// must validate live state and run the probe inside the declared real guest.
type SmokeRequest struct {
	Protocol           string `json:"protocol"`
	PlanHash           string `json:"plan_hash"`
	Plan               Plan   `json:"plan"`
	Step               Step   `json:"step"`
	Operation          string `json:"operation"`
	Volume             string `json:"volume"`
	Checkpoint         uint64 `json:"checkpoint"`
	OracleDigest       string `json:"oracle_digest"`
	PreserveFailedData bool   `json:"preserve_failed_data"`
}

// RetainedSmokeVolume records the bounded failed-data inventory after normal
// client shutdown and unpublication. Absence is explicit, never inferred from
// a missing mount; the deployment management API must establish Exists.
type RetainedSmokeVolume struct {
	ID          string    `json:"id"`
	Guest       string    `json:"guest"`
	Owner       string    `json:"owner"`
	Exists      bool      `json:"exists"`
	Published   bool      `json:"published"`
	MaxBytes    int64     `json:"max_bytes"`
	UsedBytes   int64     `json:"used_bytes"`
	RetainUntil time.Time `json:"retain_until"`
}

// ReplicaPoint proves a supported inspection at a common committed checkpoint.
type ReplicaPoint struct {
	Node     string `json:"node"`
	Received uint64 `json:"received"`
	Durable  uint64 `json:"durable"`
	Applied  uint64 `json:"applied"`
	Digest   string `json:"digest"`
}

// SmokeReceipt is terminal evidence from a pinned deployment adapter. A lost
// transport or missing terminal receipt is uncertain, never a clean failure.
type SmokeReceipt struct {
	Protocol            string                     `json:"protocol"`
	PlanHash            string                     `json:"plan_hash"`
	Step                string                     `json:"step"`
	Volume              string                     `json:"volume"`
	Terminal            bool                       `json:"terminal"`
	Success             bool                       `json:"success"`
	Checks              []string                   `json:"checks"`
	Evidence            map[string]json.RawMessage `json:"evidence"`
	Guest               string                     `json:"guest"`
	Owner               string                     `json:"owner"`
	Epoch               uint64                     `json:"epoch"`
	Filesystem          string                     `json:"filesystem"`
	HostFilesystem      string                     `json:"host_filesystem"`
	Mount               string                     `json:"mount"`
	HostMount           string                     `json:"host_mount"`
	Export              string                     `json:"export"`
	IOBytes             int64                      `json:"io_bytes"`
	Operations          int                        `json:"operations"`
	Checkpoint          uint64                     `json:"checkpoint"`
	OracleDigest        string                     `json:"oracle_digest"`
	Replicas            []ReplicaPoint             `json:"replicas"`
	Message             string                     `json:"message"`
	PreservedFailedData bool                       `json:"preserved_failed_data"`
	ClientsStopped      bool                       `json:"clients_stopped"`
	VolumeDeleted       bool                       `json:"volume_deleted"`
	NoResidue           bool                       `json:"no_residue"`
	RetainedVolumes     []RetainedSmokeVolume      `json:"retained_volumes"`
	AdapterDiagnostics  string                     `json:"adapter_diagnostics,omitempty"`
}

// SmokeAdapter owns and reaps every local/remote action before returning a
// terminal receipt. Implementations cannot treat a lost SSH client as terminal.
type SmokeAdapter interface {
	Invoke(context.Context, SmokeRequest) (SmokeReceipt, error)
}

// ProcessSmokeAdapter invokes only the pinned adapter, using JSON on stdin.
type ProcessSmokeAdapter struct{}

type adapterOutput struct {
	mu        sync.Mutex
	stdout    bytes.Buffer
	stderr    bytes.Buffer
	remaining int64
	exceeded  bool
}

type adapterStream struct {
	output *adapterOutput
	stderr bool
}

var _ SmokeAdapter = ProcessSmokeAdapter{}

// RunSmokeStep enforces the supported schedule and validates adapter evidence.
// Known run failures preserve payloads while still stopping run clients and
// unpublishing normally; cleanup never repairs live storage.
func RunSmokeStep(ctx context.Context, p Plan, id string, adapter SmokeAdapter) (SmokeReceipt, error) {
	if err := p.Validate(); err != nil {
		return SmokeReceipt{}, err
	}
	if p.Stage != "stage2" || adapter == nil {
		return SmokeReceipt{}, errors.New("stage2 adapter required")
	}
	if err := verifyStage1(p); err != nil {
		return SmokeReceipt{}, err
	}
	var step Step
	for _, s := range p.Steps {
		if s.ID == id {
			step = s
		}
	}
	if step.ID == "" {
		return SmokeReceipt{}, errors.New("step is absent from approved plan")
	}
	h, err := p.Hash()
	if err != nil {
		return SmokeReceipt{}, err
	}
	parts := strings.Split(step.Contract, ":")
	req := SmokeRequest{Protocol: "hfs-live-v1", PlanHash: h, Plan: p, Step: step, Operation: parts[1], Volume: parts[2]}
	if req.Operation == "L08" {
		var oracle SmokeReceipt
		if err := ReadJSON(smokeOraclePath(p, req.Volume), &oracle); err != nil {
			return SmokeReceipt{}, fmt.Errorf("read external checkpoint oracle: %w", err)
		}
		if oracle.PlanHash != h || oracle.Volume != req.Volume || oracle.Checkpoint == 0 || !oracle.Success {
			return SmokeReceipt{}, errors.New("checkpoint oracle does not match this run/volume")
		}
		req.Checkpoint = oracle.Checkpoint
		req.OracleDigest = oracle.OracleDigest
	}
	if step.Phase == "cleanup" {
		var state State
		if err := ReadJSON(filepath.Join(p.StateDir, "journal.json"), &state); err != nil {
			return SmokeReceipt{}, fmt.Errorf("read preservation state: %w", err)
		}
		if state.PlanHash != h {
			return SmokeReceipt{}, errors.New("preservation state hash mismatch")
		}
		req.PreserveFailedData = state.Aborted || forwardFailure(state) != nil
		for _, previous := range state.History {
			if previous.Status == "failed" {
				req.PreserveFailedData = true
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
	defer cancel()
	receipt, err := adapter.Invoke(ctx, req)
	if err != nil {
		return receipt, err
	}
	if receipt.Protocol != req.Protocol || receipt.PlanHash != h || receipt.Step != id || receipt.Volume != req.Volume || !receipt.Terminal {
		return receipt, &Error{Category: "infrastructure", Uncertain: true, Err: errors.New("missing matching terminal deployment receipt")}
	}
	if !receipt.Success {
		return receipt, &Error{Category: "assertion", Err: fmt.Errorf("deployment smoke failed: %s", receipt.Message)}
	}
	if err := validateSmokeReceipt(req, receipt); err != nil {
		return receipt, &Error{Category: "assertion", Err: err}
	}
	if req.Operation == "L07" {
		if err := WriteJSON(smokeOraclePath(p, req.Volume), receipt); err != nil {
			return receipt, fmt.Errorf("persist external checkpoint oracle: %w", err)
		}
	}
	return receipt, nil
}

// Invoke does no shell evaluation and caps stdout/stderr together. Unknown
// adapter exits and deadline kills are uncertain under the runner contract.
func (ProcessSmokeAdapter) Invoke(ctx context.Context, req SmokeRequest) (SmokeReceipt, error) {
	if err := req.Plan.Validate(); err != nil {
		return SmokeReceipt{}, err
	}
	if req.Plan.Stage != "stage2" {
		return SmokeReceipt{}, errors.New("deployment adapter requires stage2")
	}
	if err := req.Plan.VerifyArtifacts(); err != nil {
		return SmokeReceipt{}, err
	}
	var path string
	for _, a := range req.Plan.Artifacts {
		if a.ID == req.Plan.Live.Adapter {
			path = a.Path
		}
	}
	input, err := json.Marshal(req)
	if err != nil {
		return SmokeReceipt{}, err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = bytes.NewReader(input)
	out := &adapterOutput{remaining: req.Plan.Budget.ArtifactBytes / int64(len(req.Plan.Steps)+2)}
	cmd.Stdout = adapterStream{output: out}
	cmd.Stderr = adapterStream{output: out, stderr: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/usr/local/sbin", "HOME=" + os.Getenv("HOME")}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		cmd.Env = append(cmd.Env, "SSH_AUTH_SOCK="+sock)
	}
	err = cmd.Run()
	stdout, diagnostics, exceeded := out.snapshot()
	receipt := SmokeReceipt{AdapterDiagnostics: diagnostics}
	if exceeded {
		err = errors.Join(err, errors.New("deployment adapter output exceeds evidence budget"))
	}
	if err != nil {
		return receipt, &Error{Category: "infrastructure", Uncertain: cmd.Process != nil, Err: fmt.Errorf("deployment adapter: %w", err)}
	}
	d := json.NewDecoder(strings.NewReader(stdout))
	d.DisallowUnknownFields()
	decodeErr := d.Decode(&receipt)
	// Captured stderr is driver-owned evidence; an adapter cannot replace it
	// by supplying this optional field in its stdout JSON.
	receipt.AdapterDiagnostics = diagnostics
	if decodeErr != nil {
		return receipt, &Error{Category: "infrastructure", Uncertain: true, Err: decodeErr}
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return receipt, &Error{Category: "infrastructure", Uncertain: true, Err: errors.New("adapter must return exactly one terminal receipt")}
	}
	return receipt, nil
}

func (s adapterStream) Write(p []byte) (int, error) {
	o := s.output
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	if int64(n) > o.remaining {
		n = int(o.remaining)
		o.exceeded = true
	}
	destination := &o.stdout
	if s.stderr {
		destination = &o.stderr
	}
	written, err := destination.Write(p[:n])
	o.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	if n < len(p) {
		return written, errors.New("deployment adapter output exceeds evidence budget")
	}
	return written, nil
}

func (o *adapterOutput) snapshot() (string, string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stdout.String(), o.stderr.String(), o.exceeded
}

func validateSmokeReceipt(req SmokeRequest, r SmokeReceipt) error {
	checks := map[string][]string{
		"preflight": {"stage1-candidate-pass", "deployment-authorized", "versions", "pinned-ssh", "headroom", "health-before"},
		"create":    {"dedicated-volume", "quota", "local-primary", "run-ownership"},
		"L01":       {"live-mount-chain", "9p-capabilities", "owner-ready", "replicas-ready", "versions", "numeric-identities"},
		"L02":       {"write-fsync-reopen-checksum"}, "L03": {"mkdir-rmdir", "rename-replace", "truncate", "hardlink", "unlink-open"},
		"L04": {"uid-gid-mode", "allowed-access", "denied-access"}, "L05": {"flock-contention", "fcntl-contention", "mmap-flush-sync"},
		"L06": {"sqlite-delete-full", "sqlite-wal-full", "sqlite-reopen-integrity"},
		"L07": {"sync-contract", "async-catchup"}, "L08": {"checkpoint-inspection", "content-metadata-match"},
		"L09":     {"bounded-mix", "health-after", "bounded-lag", "bounded-resources"},
		"collect": {"versions", "checksums", "operation-history", "replication-positions", "health"},
		"L10":     {"clients-stopped", "normal-unpublish", "volume-deleted", "no-residue", "health-unchanged"},
	}
	if req.Operation == "L10" && req.PreserveFailedData {
		checks["L10"] = []string{"clients-stopped", "normal-unpublish", "failed-data-preserved", "retained-inventory", "health-unchanged"}
	}
	if !reflect.DeepEqual(r.Checks, checks[req.Operation]) {
		return fmt.Errorf("%s lacks exact required evidence checks", req.Operation)
	}
	for _, check := range r.Checks {
		raw := bytes.TrimSpace(r.Evidence[check])
		if len(raw) < 3 || raw[0] != '{' && raw[0] != '[' || bytes.Equal(raw, []byte("{}")) || bytes.Equal(raw, []byte("[]")) || !json.Valid(raw) {
			return fmt.Errorf("%s requires structured supporting evidence", check)
		}
	}
	if r.IOBytes < 0 || r.IOBytes > req.Plan.Budget.IOBytes/int64(len(req.Plan.Live.Volumes)*7) || r.Operations < 0 || r.Operations > req.Plan.Live.MaxOperations/(len(req.Plan.Live.Volumes)*7) {
		return errors.New("smoke receipt exceeds per-operation I/O budget")
	}
	mutating := contains([]string{"L02", "L03", "L04", "L05", "L06", "L09"}, req.Operation)
	if mutating && (r.IOBytes == 0 || r.Operations == 0) || !mutating && (r.IOBytes != 0 || r.Operations != 0) {
		return errors.New("workload counters must be positive only for workload operations")
	}
	if req.Operation == "preflight" || req.Operation == "collect" {
		return nil
	}
	var v SmokeVolume
	for _, volume := range req.Plan.Live.Volumes {
		if volume.ID == req.Volume {
			v = volume
		}
	}
	absentCleanup := req.Operation == "L10" && req.PreserveFailedData && len(r.RetainedVolumes) == 1 && !r.RetainedVolumes[0].Exists
	if r.Guest != v.Guest || r.Owner != v.Owner || r.Mount != v.GuestMount || r.HostMount != v.HostMount || r.Export != v.Export {
		return errors.New("live evidence does not match approved guest → 9p → HomelabFS FUSE → volume identity")
	}
	if absentCleanup {
		if r.Epoch != 0 || r.Filesystem != "" || r.HostFilesystem != "" {
			return errors.New("absent-volume cleanup cannot claim an active epoch or mounted filesystem")
		}
	} else if r.Epoch != v.Epoch || r.Filesystem != "9p" || r.HostFilesystem != "homelabfs-fuse" {
		return errors.New("live evidence lacks the approved owner epoch and 9p/HomelabFS FUSE chain")
	}
	if req.Operation == "L10" {
		if err := validateSmokeCleanup(req, r, v); err != nil {
			return err
		}
	}
	if req.Operation == "L07" || req.Operation == "L08" {
		if len(r.OracleDigest) != 64 || strings.Trim(r.OracleDigest, "0123456789abcdef") != "" {
			return errors.New("checkpoint requires an external expected content/metadata digest")
		}
		if req.Operation == "L08" && (r.Checkpoint != req.Checkpoint || r.OracleDigest != req.OracleDigest) {
			return errors.New("replica inspection changed the external run checkpoint/oracle")
		}
		if r.Checkpoint == 0 || len(r.Replicas) != len(v.Replicas) {
			return errors.New("replica evidence requires a nonzero common checkpoint and all configured replicas")
		}
		seen := map[string]bool{}
		digest := ""
		for _, point := range r.Replicas {
			if !contains(v.Replicas, point.Node) || seen[point.Node] || point.Received < r.Checkpoint || point.Durable < r.Checkpoint || point.Applied < r.Checkpoint {
				return errors.New("replica has not reached common received/durable/applied checkpoint")
			}
			seen[point.Node] = true
			if req.Operation == "L08" {
				if len(point.Digest) != 64 || strings.Trim(point.Digest, "0123456789abcdef") != "" || digest != "" && digest != point.Digest || point.Digest != r.OracleDigest {
					return errors.New("replica content/metadata checkpoint digests differ or are missing")
				}
				digest = point.Digest
			}
		}
	}
	return nil
}

func validateSmokeCleanup(req SmokeRequest, r SmokeReceipt, v SmokeVolume) error {
	if !r.ClientsStopped || r.PreservedFailedData != req.PreserveFailedData {
		return errors.New("cleanup must stop run clients and match the requested data-preservation mode")
	}
	if !req.PreserveFailedData {
		if !r.VolumeDeleted || !r.NoResidue || len(r.RetainedVolumes) != 0 {
			return errors.New("successful-run cleanup requires volume deletion and no residue")
		}
		return nil
	}
	if r.VolumeDeleted || r.NoResidue || len(r.RetainedVolumes) != 1 {
		return errors.New("failed-run cleanup must preserve data and report the exact retained-volume inventory")
	}
	retained := r.RetainedVolumes[0]
	deadline := req.Plan.Created.Add(time.Duration(req.Plan.Budget.RetentionHours) * time.Hour)
	if retained.ID != v.ID || retained.Guest != v.Guest || retained.Owner != v.Owner || retained.Published || retained.MaxBytes != v.MaxBytes || retained.UsedBytes < 0 || retained.UsedBytes > v.MaxBytes || !retained.Exists && retained.UsedBytes != 0 || !retained.RetainUntil.Equal(deadline) {
		return errors.New("retained-volume manifest is foreign, published, unbounded or outside the approved retention policy")
	}
	return nil
}

func smokeOraclePath(p Plan, volume string) string {
	return filepath.Join(p.StateDir, "smoke-oracle-"+volume+".json")
}

func verifyStage1(p Plan) error {
	paths := map[string]string{}
	for _, a := range p.Artifacts {
		paths[a.ID] = a.Path
	}
	var earlier Plan
	if err := ReadJSON(paths[p.Live.Stage1Plan], &earlier); err != nil {
		return fmt.Errorf("read Stage 1 plan: %w", err)
	}
	if earlier.Stage != "stage1" {
		return errors.New("stage 2 requires a separate Stage 1 candidate plan")
	}
	h, err := earlier.Hash()
	if err != nil {
		return err
	}
	var report Report
	if err := ReadJSON(paths[p.Live.Stage1Evidence], &report); err != nil {
		return err
	}
	if !report.Success || !report.Complete || report.PlanHash != h || report.RunID != earlier.RunID || len(report.Results) != len(earlier.Steps) {
		return errors.New("stage 1 candidate evidence is unsuccessful or does not match its plan")
	}
	for i, s := range earlier.Steps {
		r := report.Results[i]
		if r.ID != s.ID || r.Phase != s.Phase || r.Status != "passed" && r.Status != "skipped" || s.Phase == "test" && r.Status != "passed" {
			return errors.New("stage 1 report does not demonstrate all selected candidate steps")
		}
	}
	for _, r := range append(append([]Result(nil), report.Results...), report.History...) {
		if r.Status == "failed" || r.Status == "ambiguous" {
			return errors.New("stage 1 candidate evidence retains a failed or uncertain outcome")
		}
	}
	var candidate Artifact
	for _, a := range p.Artifacts {
		if a.ID == p.Live.Candidate {
			candidate = a
		}
	}
	for _, a := range earlier.Artifacts {
		if a.SHA256 == candidate.SHA256 && a.Version == candidate.Version {
			return nil
		}
	}
	return errors.New("candidate bytes/version were not included in the passed Stage 1 plan")
}
