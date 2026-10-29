package labrunner

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
)

// ServingBinding records the explicitly approved runtime-readiness-v1 binding.
// Supervisor birth identifies a process launch; Generation identifies the
// separate daemon workspace learned from readiness after that launch.
type ServingBinding struct {
	RunID      string       `json:"run_id"`
	PlanHash   string       `json:"plan_hash"`
	Target     string       `json:"target"`
	Node       string       `json:"node"`
	Volume     string       `json:"volume"`
	Generation string       `json:"generation"`
	Socket     string       `json:"socket"`
	Process    FaultProcess `json:"process"`
	ObservedAt time.Time    `json:"observed_at"`
	Readiness  admin.Status `json:"readiness"`
}

// BindWorkspace learns one fresh daemon workspace only after child startup.
// It checks the exact private daemon target while retaining the unreaped child,
// records immutable evidence, then opens signal admission. It never treats a
// supervisor launch nonce as a daemon workspace or authorizes a changed plan.
func (s *FaultSupervisor) BindWorkspace(ctx context.Context) (ServingBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.config.Role == "private-etcd" {
		return ServingBinding{}, errors.New("private etcd cannot bind a HomelabFS workspace")
	}
	if s.closed || s.admission || s.child == nil || s.exited == nil {
		return ServingBinding{}, errors.New("workspace binding requires a new supervised child")
	}
	exited, err := s.exited()
	if err != nil {
		return ServingBinding{}, err
	}
	if exited {
		return ServingBinding{}, errors.New("child exited before readiness binding")
	}
	socket := filepath.Join(s.config.Directory, "admin-"+s.config.Volume+".sock")
	response, err := admin.CallProcess(ctx, socket, admin.Request{Version: 1, Operation: "status"}, s.process.PID)
	if err != nil {
		return ServingBinding{}, err
	}
	if response.Status == nil || response.Status.NodeID != s.config.Node || len(response.Status.Volumes) != 1 || response.Status.Volumes[0].VolumeID != s.config.Volume || !validID(response.Status.Volumes[0].Generation) {
		return ServingBinding{}, errors.New("daemon readiness target mismatch")
	}
	exited, err = s.exited()
	if err != nil {
		return ServingBinding{}, err
	}
	if exited {
		return ServingBinding{}, errors.New("child exited during readiness binding")
	}
	binding := ServingBinding{RunID: s.config.RunID, PlanHash: s.config.PlanHash, Target: s.config.Target, Node: s.config.Node, Volume: s.config.Volume, Generation: response.Status.Volumes[0].Generation, Socket: socket, Process: s.process, ObservedAt: time.Now().UTC(), Readiness: *response.Status}
	path := filepath.Join(s.config.Directory, "supervisor-"+s.config.Handle+".serving.json")
	if err := WriteJSON(path, binding); err != nil {
		return ServingBinding{}, err
	}
	s.admission = true
	return binding, nil
}
func loadServingBinding(p Plan, f Fault) (*ServingBinding, error) {
	if f.GenerationBinding == "" {
		return nil, nil
	}
	var binding ServingBinding
	if err := readOwnedJSON(filepath.Join(p.StateDir, "fault-binding-"+f.ID+".json"), &binding); err != nil {
		return nil, err
	}
	if err := validateServingBinding(p, f, binding); err != nil {
		return nil, err
	}
	return &binding, nil
}
func validateServingBinding(p Plan, f Fault, b ServingBinding) error {
	h, err := p.Hash()
	if err != nil {
		return err
	}
	if f.GenerationBinding != "runtime-readiness-v1" || f.Generation != "" || b.RunID != p.RunID || b.PlanHash != h || b.Target != f.Target || b.Node != f.Node || b.Volume != f.Volume || b.Socket != f.Socket || !validID(b.Generation) || b.ObservedAt.Before(p.Created) || b.ObservedAt.After(p.Expires) || b.ObservedAt.After(time.Now()) || f.Process == nil || b.Process.PID <= 1 || len(b.Process.Birth) != 32 {
		return errors.New("runtime readiness binding identity/time mismatch")
	}
	expected := *f.Process
	expected.PID = b.Process.PID
	expected.Birth = b.Process.Birth
	if !reflect.DeepEqual(expected, b.Process) {
		return errors.New("runtime readiness binding launch mismatch")
	}
	target := f
	target.Generation = b.Generation
	return exactFaultStatus(target, b.Readiness)
}
func resolvedFault(req FaultRequest) Fault {
	f := req.Fault
	if f.GenerationBinding == "runtime-readiness-v1" && req.Binding != nil {
		f.Generation = req.Binding.Generation
	}
	return f
}
