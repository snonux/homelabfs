package labrunner

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/snonux/homelabfs/internal/admin"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeFaultExecutor struct {
	calls   []FaultRequest
	outcome string
	mutate  func(*FaultReceipt)
}

func (e *fakeFaultExecutor) InvokeFault(_ context.Context, req FaultRequest) (FaultReceipt, error) {
	e.calls = append(e.calls, req)
	r := FaultReceipt{Protocol: req.Protocol, PlanHash: req.PlanHash, RunID: req.RunID, Step: req.Step, Target: req.Fault.Target, Node: req.Fault.Node, Volume: req.Fault.Volume, Generation: resolvedFault(req).Generation, Seed: req.Seed, Started: req.Plan.Created.Add(time.Second), Finished: req.Plan.Created.Add(2 * time.Second), Terminal: true, Outcome: e.outcome, Evidence: map[string]json.RawMessage{}, RemainingResources: req.Plan.Resources}
	if req.Binding != nil {
		process := req.Binding.Process
		r.Process = &process
	}
	if r.Outcome == "" {
		r.Outcome = "applied"
	}
	if r.Outcome != "applied" {
		r.Message = "unsupported fixture capability"
	}
	for _, name := range []string{"daemon-logs", "admin-audit", "status-before", "status-after", "versions", "timing", "resource-inventory", "action"} {
		r.Evidence[name] = json.RawMessage(`{"observed":"fixture"}`)
	}
	if e.mutate != nil {
		e.mutate(&r)
	}
	return r, nil
}
func faultFixture(t *testing.T) (Plan, Approval, time.Time) {
	p, _, now := fixture(t)
	f := Fault{ID: "quota1", Step: "exercise", CleanupStep: "data-clean", Target: "hfs-test0", Action: "quota", Resource: "child", Node: "node0", Volume: "volume", Generation: "generation", Socket: "/var/tmp/hfs-run-run1/control/admin-volume.sock", Log: "/var/tmp/hfs-run-run1/control/daemon.log", QuotaBytes: 4096}
	p.Resources = append(p.Resources, Resource{Name: "child", Kind: "dataset", Target: "hfs-test0", Location: "hfsdata0/data/hfs-run-run1/child", MaxBytes: 1 << 20})
	p.Faults = []Fault{f}
	p.Disruptions = append(p.Disruptions, "fault:quota1")
	p.Steps[4].Contract = "native-fault-v1:quota1"
	p.Steps[4].Args = nil
	p.Steps[6].Contract = "native-fault-cleanup-v1:quota1"
	p.Steps[6].Args = nil
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return p, Approval{PlanHash: h, Operator: "fixture", ApprovedAt: now, Statement: "I approve this exact plan"}, now
}
func TestFaultApprovalAndEvidence(t *testing.T) {
	p, a, now := faultFixture(t)
	executor := &fakeFaultExecutor{}
	receipt, err := RunFaultStep(context.Background(), p, a, "exercise", executor, now)
	if err != nil || receipt.Seed != 42 || len(executor.calls) != 1 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	p.Faults[0].QuotaBytes++
	if _, err := RunFaultStep(context.Background(), p, a, "exercise", executor, now); err == nil || len(executor.calls) != 1 {
		t.Fatal("changed fault escaped approval")
	}
}
func TestFaultRejectsUnsafeShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Plan)
	}{
		{"production", func(p *Plan) { p.Faults[0].Target = "rocky" }},
		{"sink", func(p *Plan) { p.Resources[1].Location = "zdata/sink/f3/child" }},
		{"parent quota", func(p *Plan) { p.Resources[1].Location = "hfsdata0/data/hfs-run-run1" }},
		{"unapproved", func(p *Plan) { p.Disruptions = nil }},
		{"no cleanup", func(p *Plan) { p.Faults[0].CleanupStep = "missing" }},
		{"custom args", func(p *Plan) { p.Steps[4].Args = []string{"arbitrary"} }},
		{"foreign socket", func(p *Plan) { p.Faults[0].Socket = "/var/run/admin-volume.sock" }},
		{"foreign logs", func(p *Plan) { p.Faults[0].Log = "/var/log/messages" }},
		{"payload deletion", func(p *Plan) { p.PreserveFailures = false }},
		{"unknown action", func(p *Plan) { p.Faults[0].Action = "destroy-pool" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := faultFixture(t)
			tc.change(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("accepted unsafe fault")
			}
		})
	}
}
func TestFaultReceiptCannotInventSuccess(t *testing.T) {
	for _, tc := range []struct {
		name      string
		outcome   string
		mutate    func(*FaultReceipt)
		uncertain bool
	}{
		{name: "wrong generation", mutate: func(r *FaultReceipt) { r.Generation = "other" }, uncertain: true},
		{name: "missing status", mutate: func(r *FaultReceipt) { delete(r.Evidence, "status-after") }, uncertain: true},
		{name: "nonterminal", mutate: func(r *FaultReceipt) { r.Terminal = false }, uncertain: true},
		{name: "unbounded", mutate: func(r *FaultReceipt) { r.Message = strings.Repeat("X", 2<<20) }, uncertain: true},
		{name: "unsupported", outcome: "unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, a, now := faultFixture(t)
			e := &fakeFaultExecutor{outcome: tc.outcome, mutate: tc.mutate}
			_, err := RunFaultStep(context.Background(), p, a, "exercise", e, now)
			var failure *Error
			if !errors.As(err, &failure) || failure.Uncertain != tc.uncertain {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
func TestFaultCleanupRestoresQuotaAfterExpiry(t *testing.T) {
	p, a, now := faultFixture(t)
	e := &fakeFaultExecutor{}
	// Terminal fake timing remains inside the cleanup's later request deadline.
	if _, err := RunFaultStep(context.Background(), p, a, "data-clean", e, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	req := e.calls[0]
	if !req.Cleanup || req.Seed != 42 || !reflect.DeepEqual(req.Commands[0].Args, []string{"set", "quota=1048576", "hfsdata0/data/hfs-run-run1/child"}) {
		t.Fatalf("unsafe cleanup=%+v", req)
	}
}
func TestNetworkCommandsExactRulesForEitherConnectionRole(t *testing.T) {
	p, _, _ := faultFixture(t)
	for _, action := range []string{"network-loss", "network-delay", "network-partition", "network-reset", "network-clear"} {
		for _, direction := range []string{"in", "out"} {
			t.Run(action+"/"+direction+"/target-client-and-listener", func(t *testing.T) {
				f := p.Faults[0]
				f.Action = action
				f.QuotaBytes = 0
				f.Network = &FaultNetwork{Peer: "hfs-test1", Port: 31000, Anchor: "hfs-run-run1/quota1", Pipe: 10001, Direction: direction}
				if action == "network-loss" {
					f.Network.LossPercent = 10
				}
				if action == "network-delay" {
					f.Network.DelayMillis = 20
				}
				p.Faults[0] = f
				commands, err := FaultCommands(p, f)
				if err != nil {
					t.Fatal(err)
				}
				first := 0
				if action == "network-loss" || action == "network-delay" {
					first = 1
				}
				if commands[first].Path != "/sbin/pfctl" || !reflect.DeepEqual(commands[first].Args, []string{"-a", "hfs-run-run1/quota1", "-f", "-"}) || commands[first].Stdin != expectedNetworkRules(action, direction) {
					t.Fatalf("wrong peer/port coverage:\n%+v", commands[first])
				}
				for i, label := range []string{"hfs-run1-quota1-src", "hfs-run1-quota1-dst"} {
					command := commands[first+1+i]
					if command.Path != "/sbin/pfctl" || !reflect.DeepEqual(command.Args, []string{"-k", "label", "-k", label}) || command.Stdin != "" {
						t.Fatalf("unscoped state invalidation: %+v", command)
					}
				}
				count := first + 3
				if action == "network-clear" {
					count++
				}
				if len(commands) != count {
					t.Fatalf("unexpected extra effect: %+v", commands)
				}
				f.Network.Port = 22
				if _, err := FaultCommands(p, f); err == nil {
					t.Fatal("SSH fault accepted")
				}
			})
		}
	}
}

// These literal rules specify packet directions independently of which guest
// initiated the TCP connection. Source-port rules cover listener replies out
// and client replies in; destination-port rules cover client requests out and
// listener requests in. Both stay restricted to the exact two guest addresses.
func expectedNetworkRules(action, direction string) string {
	rules := map[string]string{
		"network-loss/out": `pass out quick inet proto tcp from 172.31.253.11 port 31000 to 172.31.253.12 no state dnpipe 10001 label "hfs-run1-quota1-src"
pass out quick inet proto tcp from 172.31.253.11 to 172.31.253.12 port 31000 no state dnpipe 10001 label "hfs-run1-quota1-dst"
`,
		"network-loss/in": `pass in quick inet proto tcp from 172.31.253.12 port 31000 to 172.31.253.11 no state dnpipe 10001 label "hfs-run1-quota1-src"
pass in quick inet proto tcp from 172.31.253.12 to 172.31.253.11 port 31000 no state dnpipe 10001 label "hfs-run1-quota1-dst"
`,
		"network-delay/out": `pass out quick inet proto tcp from 172.31.253.11 port 31000 to 172.31.253.12 no state dnpipe 10001 label "hfs-run1-quota1-src"
pass out quick inet proto tcp from 172.31.253.11 to 172.31.253.12 port 31000 no state dnpipe 10001 label "hfs-run1-quota1-dst"
`,
		"network-delay/in": `pass in quick inet proto tcp from 172.31.253.12 port 31000 to 172.31.253.11 no state dnpipe 10001 label "hfs-run1-quota1-src"
pass in quick inet proto tcp from 172.31.253.12 to 172.31.253.11 port 31000 no state dnpipe 10001 label "hfs-run1-quota1-dst"
`,
		"network-partition/out": `block drop out quick inet proto tcp from 172.31.253.11 port 31000 to 172.31.253.12 label "hfs-run1-quota1-src"
block drop out quick inet proto tcp from 172.31.253.11 to 172.31.253.12 port 31000 label "hfs-run1-quota1-dst"
`,
		"network-partition/in": `block drop in quick inet proto tcp from 172.31.253.12 port 31000 to 172.31.253.11 label "hfs-run1-quota1-src"
block drop in quick inet proto tcp from 172.31.253.12 to 172.31.253.11 port 31000 label "hfs-run1-quota1-dst"
`,
		"network-reset/out": `block return-rst out quick inet proto tcp from 172.31.253.11 port 31000 to 172.31.253.12 label "hfs-run1-quota1-src"
block return-rst out quick inet proto tcp from 172.31.253.11 to 172.31.253.12 port 31000 label "hfs-run1-quota1-dst"
`,
		"network-reset/in": `block return-rst in quick inet proto tcp from 172.31.253.12 port 31000 to 172.31.253.11 label "hfs-run1-quota1-src"
block return-rst in quick inet proto tcp from 172.31.253.12 to 172.31.253.11 port 31000 label "hfs-run1-quota1-dst"
`,
		"network-clear/out": `pass out quick inet proto tcp from 172.31.253.11 port 31000 to 172.31.253.12 keep state label "hfs-run1-quota1-src"
pass out quick inet proto tcp from 172.31.253.11 to 172.31.253.12 port 31000 keep state label "hfs-run1-quota1-dst"
`,
		"network-clear/in": `pass in quick inet proto tcp from 172.31.253.12 port 31000 to 172.31.253.11 keep state label "hfs-run1-quota1-src"
pass in quick inet proto tcp from 172.31.253.12 to 172.31.253.11 port 31000 keep state label "hfs-run1-quota1-dst"
`,
	}
	return rules[action+"/"+direction]
}

func TestNetworkLabelBoundsIncludeRoleSuffix(t *testing.T) {
	p, _, _ := faultFixture(t)
	f := p.Faults[0]
	f.Action = "network-partition"
	f.QuotaBytes = 0
	for _, size := range []int{50, 51} {
		f.ID = strings.Repeat("x", size)
		f.Network = &FaultNetwork{Peer: "hfs-test1", Port: 31000, Anchor: "hfs-run-run1/" + f.ID, Pipe: 10001, Direction: "in"}
		_, err := FaultCommands(p, f)
		if (size == 50) != (err == nil) {
			t.Fatalf("label length %d: %v", size, err)
		}
	}
}

func TestRuntimeReadinessBindingRequiresExplicitPolicyAndExactLaunch(t *testing.T) {
	p, _, now := faultFixture(t)
	f := &p.Faults[0]
	f.Action = "failpoint"
	f.QuotaBytes = 0
	f.Enabled = true
	f.Generation = ""
	f.GenerationBinding = "runtime-readiness-v1"
	f.Process = &FaultProcess{Executable: "/var/tmp/hfs-run-run1/homelabfs", SHA256: strings.Repeat("a", 64), Handle: "daemon"}
	h, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	approval := Approval{PlanHash: h, Operator: "fixture", ApprovedAt: now, Statement: "I approve this exact plan"}
	if _, err := PrepareFaultRequest(p, approval, "exercise", now); err == nil {
		t.Fatal("unknown workspace silently resolved without readiness binding")
	}
	if err := os.Mkdir(p.StateDir, 0700); err != nil {
		t.Fatal(err)
	}
	process := *f.Process
	process.PID = 123
	process.Birth = strings.Repeat("b", 32)
	binding := ServingBinding{RunID: p.RunID, PlanHash: h, Target: f.Target, Node: f.Node, Volume: f.Volume, Generation: "learned-after-child-start", Socket: f.Socket, Process: process, ObservedAt: p.Created.Add(time.Second), Readiness: admin.Status{NodeID: f.Node, Volumes: []admin.VolumeStatus{{VolumeID: f.Volume, Generation: "learned-after-child-start"}}}}
	if err := WriteJSON(filepath.Join(p.StateDir, "fault-binding-"+f.ID+".json"), binding); err != nil {
		t.Fatal(err)
	}
	request, err := PrepareFaultRequest(p, approval, "exercise", now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Faults[0].Generation != "" || request.Binding.Generation == process.Birth {
		t.Fatal("workspace and launch identity were conflated")
	}
	var control admin.Request
	if err := json.Unmarshal([]byte(request.Commands[0].Stdin), &control); err != nil {
		t.Fatal(err)
	}
	if control.ExpectedGeneration != binding.Generation {
		t.Fatal("control did not bind learned daemon workspace")
	}
	executor := &fakeFaultExecutor{}
	if _, err := RunFaultStep(context.Background(), p, approval, "exercise", executor, now); err != nil {
		t.Fatal(err)
	}
	binding.Process.Executable = "/var/tmp/hfs-run-run1/other"
	if err := validateServingBinding(p, *f, binding); err == nil {
		t.Fatal("different executable admitted as learned workspace")
	}
	copy := p
	copy.Faults = append([]Fault(nil), p.Faults...)
	copy.Faults[0].GenerationBinding = ""
	copy.Faults[0].Process = nil
	if err := copy.Validate(); err == nil {
		t.Fatal("unknown generation accepted without explicit runtime policy")
	}
}
