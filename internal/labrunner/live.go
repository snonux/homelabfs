package labrunner

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// LivePlan binds separately deployed smoke resources and the adapter protocol.
// Adapters are pinned local executables, not arbitrary command strings.
type LivePlan struct {
	Driver             string        `json:"driver"`
	Adapter            string        `json:"adapter"`
	Probe              string        `json:"probe"`
	Candidate          string        `json:"candidate"`
	KnownHosts         string        `json:"known_hosts"`
	Stage1Plan         string        `json:"stage1_plan"`
	Stage1Evidence     string        `json:"stage1_evidence"`
	DeploymentEvidence string        `json:"deployment_evidence"`
	Volumes            []SmokeVolume `json:"volumes"`
	MaxOperations      int           `json:"max_operations"`
	UID                int           `json:"uid"`
	GID                int           `json:"gid"`
	DeniedUID          int           `json:"denied_uid"`
}

// SmokeVolume describes one local-primary dedicated volume and guest export.
type SmokeVolume struct {
	ID         string   `json:"id"`
	Guest      string   `json:"guest"`
	Owner      string   `json:"owner"`
	Epoch      uint64   `json:"epoch"`
	Replicas   []string `json:"replicas"`
	GuestMount string   `json:"guest_mount"`
	HostMount  string   `json:"host_mount"`
	Export     string   `json:"export"`
	MaxBytes   int64    `json:"max_bytes"`
}

// SmokeSteps generates the complete, reviewable baseline schedule. It performs
// no I/O. Deadlines remain explicit inputs and must fit the approved budget.
func SmokeSteps(p Plan, seconds int) []Step {
	if p.Live == nil {
		return nil
	}
	var steps []Step
	add := func(id, phase, test, op, volume, effects string, paired []string) {
		steps = append(steps, Step{ID: id, Phase: phase, Test: test, Action: "driver", Artifact: p.Live.Driver, Args: []string{"--step", id}, Contract: "live-smoke-v1:" + op + ":" + volume, Effects: effects, TimeoutSeconds: seconds, MaxAttempts: 1, Idempotent: phase == "cleanup", AppliesAfter: paired})
	}
	add("live-preflight", "setup", "", "preflight", "all", "read-only", nil)
	for i, v := range p.Live.Volumes {
		add(fmt.Sprintf("create-%d", i), "setup", "", "create", v.ID, "run-owned", nil)
	}
	for i, v := range p.Live.Volumes {
		for n := 1; n <= 9; n++ {
			test := fmt.Sprintf("L%02d", n)
			effects := "read-only"
			if n >= 2 && n <= 6 || n == 9 {
				effects = "run-owned"
			}
			add(fmt.Sprintf("%s-%d", test, i), "test", test, test, v.ID, effects, nil)
		}
	}
	add("live-collect", "collect", "", "collect", "all", "read-only", nil)
	for i, v := range p.Live.Volumes {
		paired := []string{fmt.Sprintf("create-%d", i)}
		for n := 2; n <= 9; n++ {
			if n <= 6 || n == 9 {
				paired = append(paired, fmt.Sprintf("L%02d-%d", n, i))
			}
		}
		add(fmt.Sprintf("L10-%d", i), "cleanup", "", "L10", v.ID, "run-owned", paired)
	}
	return steps
}

func (p Plan) validateLive() error {
	l := p.Live
	if p.Schema != 1 || !validID(p.RunID) || p.Profile != "live-smoke" || l == nil || p.PreparedGuests || len(p.Disruptions) != 0 || !p.PreserveFailures {
		return errors.New("stage2 requires live-smoke, dedicated live inputs, failure preservation and no lifecycle/disruptions")
	}
	if p.Created.IsZero() || !p.Expires.After(p.Created) || p.Expires.Sub(p.Created) > 24*time.Hour {
		return errors.New("live plan validity must be positive and at most 24 hours")
	}
	if !cleanAbsolute(p.StateDir) || !cleanAbsolute(p.EvidenceDir) || pathOverlap(p.StateDir, p.EvidenceDir) {
		return errors.New("live controller state/evidence must be separate absolute directories")
	}
	b := p.Budget
	if b.GuestMemoryBytes != 0 || b.CPUs != 0 || b.RestorationSeconds != 0 || b.HostHeadroomBytes <= 0 || b.DiskBytes <= 0 || b.IOBytes <= 0 || b.ArtifactBytes <= 0 || b.RetentionHours <= 0 || b.RuntimeSeconds <= 0 || b.RuntimeSeconds > 86400 || b.CleanupSeconds <= 0 || b.CleanupSeconds > 86400 || l.MaxOperations < 700*len(l.Volumes) || l.MaxOperations > 10000 || l.UID <= 0 || l.GID <= 0 || l.DeniedUID <= 0 || l.UID == l.DeniedUID {
		return errors.New("live budgets/identities must be explicit and bounded; VM allocations and restoration are forbidden")
	}
	if int64(b.RetentionHours) > int64((1<<63-1)/time.Hour) {
		return errors.New("live retention hours exceed the representable retention duration")
	}
	if len(l.Volumes) == 0 || len(l.Volumes) > 3 || len(p.Targets) != len(l.Volumes) || len(p.Resources) != len(l.Volumes) || len(p.Steps) > 64 || len(p.Artifacts) > 32 {
		return errors.New("live smoke requires one to three dedicated guest/volume/resource pairs")
	}
	artifacts := map[string]bool{}
	for _, a := range p.Artifacts {
		if !validID(a.ID) || artifacts[a.ID] || !cleanAbsolute(a.Path) || a.Version == "" || len(a.SHA256) != 64 || strings.Trim(a.SHA256, "0123456789abcdef") != "" {
			return errors.New("live artifacts require unique IDs, absolute paths, versions and SHA-256")
		}
		artifacts[a.ID] = true
	}
	for _, id := range []string{l.Driver, l.Adapter, l.Probe, l.Candidate, l.KnownHosts, l.Stage1Plan, l.Stage1Evidence, l.DeploymentEvidence} {
		if !artifacts[id] {
			return fmt.Errorf("missing pinned live input %q", id)
		}
	}
	if l.Driver == l.Adapter {
		return errors.New("live driver and deployment adapter must be distinct")
	}
	seen := map[string]bool{}
	var size int64
	for i, v := range l.Volumes {
		t := p.Targets[i]
		r := p.Resources[i]
		prefix := "hfs-smoke-" + p.RunID + "-"
		if !validID(v.ID) || !strings.HasPrefix(v.ID, prefix) || seen[v.ID] || seen[v.Guest] || !validID(v.Guest) || !validID(v.Owner) || v.Epoch == 0 || len(v.Replicas) < 2 || len(v.Replicas) > 4 || v.MaxBytes <= 0 || v.MaxBytes > b.DiskBytes-size {
			return errors.New("live volumes require unique run-owned IDs, guest, owner, epoch, replicas and quota")
		}
		seen[v.ID] = true
		seen[v.Guest] = true
		size += v.MaxBytes
		replicas := map[string]bool{}
		for _, node := range v.Replicas {
			if !validID(node) || replicas[node] {
				return errors.New("invalid replica set")
			}
			replicas[node] = true
		}
		if !replicas[v.Owner] {
			return errors.New("local primary must belong to replica set")
		}
		if v.GuestMount != "/mnt/hfs-smoke/"+v.ID || v.HostMount != "/var/run/homelabfs/smoke/"+v.ID || v.Export != v.ID {
			return errors.New("live mount and export paths must use the dedicated smoke namespace")
		}
		if t.Name != v.Guest || t.Host != v.Owner || t.InitialState != "Running" || t.Address == "" || t.Kernel == "" || t.Packages == "" {
			return errors.New("live inventory must identify each already running Rocky guest and colocated FreeBSD owner")
		}
		if r.Name != v.ID || r.Kind != "smoke-volume" || r.Target != v.Guest || r.Location != v.GuestMount || r.MaxBytes != v.MaxBytes {
			return errors.New("live resources must exactly match approved smoke volumes")
		}
		if pathOverlap(p.StateDir, v.GuestMount) || pathOverlap(p.EvidenceDir, v.GuestMount) || pathOverlap(p.StateDir, v.HostMount) || pathOverlap(p.EvidenceDir, v.HostMount) {
			return errors.New("controller oracle/evidence must be outside smoke mounts")
		}
	}
	if len(p.Tests) != 9 {
		return errors.New("baseline requires L01 through L09; L10 is mandatory cleanup")
	}
	for i, t := range p.Tests {
		if t.Name != fmt.Sprintf("L%02d", i+1) {
			return errors.New("baseline selections must be ordered L01 through L09 with explicit seeds")
		}
	}
	expected := SmokeSteps(p, 1)
	if len(expected) != len(p.Steps) {
		return errors.New("incomplete live schedule")
	}
	forward, cleanup := 0, 0
	for i, s := range p.Steps {
		x := expected[i]
		x.TimeoutSeconds = s.TimeoutSeconds
		x.MaxAttempts = s.MaxAttempts
		if s.TimeoutSeconds < 1 || s.TimeoutSeconds > 86400 || s.MaxAttempts < 1 || s.MaxAttempts > 3 || s.Phase != "cleanup" && s.MaxAttempts != 1 || !reflect.DeepEqual(s, x) {
			return fmt.Errorf("live step %d differs from the supported non-disruptive schedule", i)
		}
		if s.Phase == "cleanup" {
			cleanup += s.TimeoutSeconds * s.MaxAttempts
		} else {
			forward += s.TimeoutSeconds
		}
	}
	if forward > b.RuntimeSeconds || cleanup > b.CleanupSeconds {
		return errors.New("live step deadlines exceed approved phase budgets")
	}
	return nil
}

func pathOverlap(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
