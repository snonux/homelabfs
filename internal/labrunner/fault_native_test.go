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

func TestNativeQuotaChecksIdentityBeforeSetting(t *testing.T) {
	p, _, _ := faultFixture(t)
	for _, scenario := range []string{"good", "wrong-guid", "wrong-run", "wrong-cap", "failed-set", "cleanup-repeat"} {
		t.Run(scenario, func(t *testing.T) {
			req := FaultRequest{Plan: p, RunID: p.RunID, Fault: p.Faults[0], Step: "exercise", Cleanup: scenario == "cleanup-repeat"}
			req.Commands, _ = faultRequestCommands(p, req.Fault, req.Cleanup)
			s := &nativeFaultSession{req: req, enrollment: FaultEnrollment{DatasetGUIDs: map[string]string{p.Resources[1].Location: "guid1"}}, path: filepath.Join(t.TempDir(), "enrollment.json"), receipt: FaultReceipt{Evidence: map[string]json.RawMessage{}}}
			var calls []FaultCommand
			set := false
			s.run = func(_ context.Context, c FaultCommand) (string, error) {
				calls = append(calls, c)
				switch c.Args[0] {
				case "get":
					switch c.Args[len(c.Args)-2] {
					case "guid":
						if scenario == "wrong-guid" {
							return "other", nil
						}
						return "guid1", nil
					case "org.homelabfs:run":
						if scenario == "wrong-run" {
							return "other", nil
						}
						return "run1", nil
					case "quota":
						if scenario == "wrong-cap" {
							return "none", nil
						}
						if set {
							return "4096", nil
						}
						return "1048576", nil
					}
				case "set":
					set = true
					if scenario == "failed-set" {
						return "", errors.New("uncertain interruption")
					}
					return "", nil
				}
				return "", errors.New("unexpected command")
			}
			err := s.quota(context.Background())
			if scenario == "good" || scenario == "cleanup-repeat" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe quota accepted")
			}
			if strings.HasPrefix(scenario, "wrong-") && set {
				t.Fatal("mutated before verifying dataset identity")
			}
			if scenario == "failed-set" && s.enrollment.Outstanding != "exercise" {
				t.Fatal("failed effect guard cleared")
			}
			if scenario == "cleanup-repeat" && set {
				t.Fatal("idempotent cleanup repeated set")
			}
		})
	}
}
func TestNativeNetworkRejectsUnmanagedRulesAndPipes(t *testing.T) {
	p, _, _ := faultFixture(t)
	f := p.Faults[0]
	f.Action = "network-loss"
	f.QuotaBytes = 0
	f.Network = &FaultNetwork{Peer: "hfs-test1", Port: 31000, Anchor: "hfs-run-run1/quota1", Pipe: 10001, Direction: "out", LossPercent: 10}
	p.Faults[0] = f
	parent := `anchor "hfs-run-run1/*" all` + "\n"
	anchor := expectedNetworkRules("network-clear", "out")
	for _, scenario := range []string{"parent-changed", "pipe-occupied", "label-collision", "destination-label-collision", "unattached"} {
		t.Run(scenario, func(t *testing.T) {
			lease := NetworkEnrollment{Network: *f.Network, ParentRulesSHA256: textDigest(parent), AnchorRulesSHA256: textDigest(anchor)}
			req := FaultRequest{Plan: p, RunID: p.RunID, Fault: f, Step: "exercise"}
			req.Commands, _ = FaultCommands(p, f)
			s := &nativeFaultSession{req: req, enrollment: FaultEnrollment{Networks: map[string]NetworkEnrollment{f.ID: lease}}, path: filepath.Join(t.TempDir(), "enrollment.json")}
			s.run = func(_ context.Context, c FaultCommand) (string, error) {
				switch {
				case reflect.DeepEqual(c.Args, []string{"-si"}):
					return "Status: Enabled", nil
				case reflect.DeepEqual(c.Args, []string{"-sr"}):
					if scenario == "parent-changed" {
						return "other", nil
					}
					if scenario == "unattached" {
						return "", nil
					}
					return parent, nil
				case reflect.DeepEqual(c.Args, []string{"-a", f.Network.Anchor, "-sr"}):
					return anchor, nil
				case reflect.DeepEqual(c.Args, []string{"-a", "*", "-sr"}):
					if scenario == "label-collision" {
						return anchor + strings.SplitAfter(anchor, "\n")[0], nil
					}
					if scenario == "destination-label-collision" {
						return anchor + strings.SplitAfter(anchor, "\n")[1], nil
					}
					return anchor, nil
				case reflect.DeepEqual(c.Args, []string{"pipe", "show"}):
					return "10001: occupied pipe", nil
				default:
					t.Fatalf("unsafe mutation issued: %+v", c)
					return "", nil
				}
			}
			if err := s.network(context.Background()); err == nil {
				t.Fatal("unmanaged network accepted")
			}
		})
	}
}

func TestNativeNetworkAppliesBothFlowRolesAndCleansWithEvidence(t *testing.T) {
	for _, direction := range []string{"in", "out"} {
		t.Run(direction, func(t *testing.T) {
			p, _, _ := faultFixture(t)
			root, err := os.MkdirTemp("/var/tmp", "hfs-run-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(root) }()
			runID := strings.TrimPrefix(filepath.Base(root), "hfs-run-")
			p.RunID = runID
			for i := range p.Resources {
				p.Resources[i].Location = strings.ReplaceAll(p.Resources[i].Location, "hfs-run-run1", "hfs-run-"+runID)
			}
			control := filepath.Join(root, "control")
			if err := os.Mkdir(control, 0700); err != nil {
				t.Fatal(err)
			}
			f := p.Faults[0]
			f.Action = "network-loss"
			f.QuotaBytes = 0
			f.Socket = filepath.Join(control, "admin-volume.sock")
			f.Log = filepath.Join(control, "daemon.log")
			f.Network = &FaultNetwork{Peer: "hfs-test1", Port: 31000, Anchor: "hfs-run-" + runID + "/quota1", Pipe: 10001, Direction: direction, LossPercent: 10}
			p.Faults[0] = f
			for _, path := range []string{f.Log, filepath.Join(control, "admin-volume.audit.jsonl")} {
				if err := os.WriteFile(path, []byte("fake bounded evidence\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			hash, err := p.Hash()
			if err != nil {
				t.Fatal(err)
			}
			rules := func(action string) string {
				return strings.ReplaceAll(expectedNetworkRules(action, direction), "hfs-run1-quota1", "hfs-"+runID+"-quota1")
			}
			parent := `anchor "hfs-run-` + runID + `/*" all` + "\n"
			currentAnchor := rules("network-clear")
			pipeConfigured := false
			enrollment := FaultEnrollment{RunID: runID, PlanHash: hash, Target: f.Target, Node: f.Node, Volume: f.Volume, Generation: f.Generation, Networks: map[string]NetworkEnrollment{f.ID: {Network: *f.Network, ParentRulesSHA256: textDigest(parent), AnchorRulesSHA256: textDigest(currentAnchor)}}}
			session := &nativeFaultSession{path: filepath.Join(root, "fault-enrollment.json"), enrollment: enrollment, logLimit: 4096}
			if err := WriteJSON(session.path, enrollment); err != nil {
				t.Fatal(err)
			}
			session.callAdmin = func(_ context.Context, socket string, request admin.Request) (admin.Response, error) {
				if socket != f.Socket || request.Operation != "status" {
					t.Fatalf("unexpected admin effect %s %+v", socket, request)
				}
				return admin.Response{Version: 1, Status: &admin.Status{NodeID: f.Node, Volumes: []admin.VolumeStatus{{VolumeID: f.Volume, Generation: f.Generation}}}}, nil
			}
			for _, cleanup := range []bool{false, true} {
				action, step := "network-loss", f.Step
				if cleanup {
					action, step = "network-clear", f.CleanupStep
				}
				commands, err := faultRequestCommands(p, f, cleanup)
				if err != nil {
					t.Fatal(err)
				}
				req := FaultRequest{Protocol: "hfs-native-fault-v1", Plan: p, PlanHash: hash, RunID: runID, Fault: f, Step: step, Cleanup: cleanup, Seed: 42, Deadline: time.Now().Add(5 * time.Second), MaxEvidenceBytes: p.Budget.ArtifactBytes / int64(len(p.Steps)+2), Commands: commands}
				session.req = req
				session.receipt = FaultReceipt{Protocol: req.Protocol, PlanHash: hash, RunID: runID, Step: step, Target: f.Target, Node: f.Node, Volume: f.Volume, Generation: f.Generation, Seed: 42, Started: time.Now().UTC(), Evidence: map[string]json.RawMessage{}, RemainingResources: p.Resources}
				// The expected mutation list is manually specified, including both scoped
				// invalidations. Query responses are fake; no native command is executed.
				expected := []FaultCommand{}
				if !cleanup {
					expected = append(expected, FaultCommand{Path: "/sbin/dnctl", Args: []string{"pipe", "10001", "config", "delay", "0", "plr", "0.10"}})
				}
				expected = append(expected, FaultCommand{Path: "/sbin/pfctl", Args: []string{"-a", f.Network.Anchor, "-f", "-"}, Stdin: rules(action)},
					FaultCommand{Path: "/sbin/pfctl", Args: []string{"-k", "label", "-k", "hfs-" + runID + "-quota1-src"}},
					FaultCommand{Path: "/sbin/pfctl", Args: []string{"-k", "label", "-k", "hfs-" + runID + "-quota1-dst"}})
				if cleanup {
					expected = append(expected, FaultCommand{Path: "/sbin/dnctl", Args: []string{"pipe", "delete", "10001"}})
				}
				var mutations []FaultCommand
				session.run = func(_ context.Context, c FaultCommand) (string, error) {
					switch {
					case c.Path == "/usr/bin/uname":
						return "FreeBSD fixture\n", nil
					case c.Path == "/usr/local/sbin/pkg":
						return "homelabfs-fixture\n", nil
					case reflect.DeepEqual(c.Args, []string{"-si"}):
						return "Status: Enabled", nil
					case reflect.DeepEqual(c.Args, []string{"-sr"}):
						return parent, nil
					case reflect.DeepEqual(c.Args, []string{"-a", f.Network.Anchor, "-sr"}):
						return currentAnchor, nil
					case reflect.DeepEqual(c.Args, []string{"-a", "*", "-sr"}):
						return currentAnchor, nil
					case reflect.DeepEqual(c.Args, []string{"pipe", "show"}):
						if pipeConfigured {
							return "10001: run-owned pipe\n", nil
						}
						return "", nil
					}
					actual := FaultCommand{Path: c.Path, Args: c.Args, Stdin: c.Stdin}
					index := len(mutations)
					if index >= len(expected) || !reflect.DeepEqual(actual, expected[index]) {
						t.Fatalf("unexpected mutation %d:\n got %+v\nwant %+v", index, actual, expected)
					}
					mutations = append(mutations, actual)
					if c.Path == "/sbin/pfctl" && len(c.Args) == 4 && c.Args[2] == "-f" {
						currentAnchor = c.Stdin
					}
					if c.Path == "/sbin/dnctl" {
						pipeConfigured = !cleanup
					}
					return "", nil
				}
				receipt, err := session.invoke(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(mutations, expected) {
					t.Fatalf("missing mutation: got %+v want %+v", mutations, expected)
				}
				if receipt.Outcome != "applied" || !receipt.Terminal || session.enrollment.Outstanding != "" {
					t.Fatalf("missing terminal applied evidence: %+v", receipt)
				}
				if err := validateFaultReceipt(p, req, receipt); err != nil {
					t.Fatal(err)
				}
				lease := session.enrollment.Networks[f.ID]
				if lease.AnchorRulesSHA256 != textDigest(rules(action)) || lease.ParentRulesSHA256 != textDigest(parent) || lease.PipeConfigured == cleanup {
					t.Fatalf("wrong retained network ownership: %+v", lease)
				}
				var evidence struct {
					Observation struct {
						Commands []FaultCommand `json:"commands"`
						Outcome  string         `json:"outcome"`
					} `json:"observation"`
				}
				if err := json.Unmarshal(receipt.Evidence["action"], &evidence); err != nil {
					t.Fatal(err)
				}
				if evidence.Observation.Outcome != "applied" || !reflect.DeepEqual(evidence.Observation.Commands, commands) {
					t.Fatal("action receipt omitted exact applied commands")
				}
			}
		})
	}
}
