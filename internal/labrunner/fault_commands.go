package labrunner

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

func networkCommands(p Plan, f Fault, c FaultCommand) []FaultCommand {
	n := f.Network
	c.Checks = append(c.Checks, "pf-already-enabled", "attached-exclusive-run-anchor", "reserved-unused-or-owned-pipe", "only-owned-labelled-states", "test-replication-port")
	local, peer := nativeAddress(f.Target), nativeAddress(n.Peer)
	source, destination := local, peer
	if n.Direction == "in" {
		source, destination = peer, local
	}
	port := strconv.Itoa(int(n.Port))
	// Either endpoint may initiate replication. In the selected packet direction,
	// the dedicated service port can therefore be the source or destination.
	matches := []string{
		"from " + source + " port " + port + " to " + destination,
		"from " + source + " to " + destination + " port " + port,
	}
	labels := networkLabels(p.RunID, f.ID)
	action, options := "pass", " no state"
	result := []FaultCommand{}
	switch f.Action {
	case "network-loss", "network-delay":
		pipe := c
		pipe.Path = "/sbin/dnctl"
		pipe.Args = []string{"pipe", strconv.Itoa(int(n.Pipe)), "config", "delay", strconv.Itoa(n.DelayMillis), "plr", fmt.Sprintf("%.2f", float64(n.LossPercent)/100)}
		result = append(result, pipe)
		options += " dnpipe " + strconv.Itoa(int(n.Pipe))
	case "network-partition":
		action, options = "block drop", ""
	case "network-reset":
		action, options = "block return-rst", ""
	case "network-clear":
		options = " keep state"
	}
	c.Path = "/sbin/pfctl"
	c.Args = []string{"-a", n.Anchor, "-f", "-"}
	for i, match := range matches {
		c.Stdin += fmt.Sprintf("%s %s quick inet proto tcp %s%s label %q\n", action, n.Direction, match, options, labels[i])
	}
	result = append(result, c)
	// Each preinstalled run label is exclusive. Never flush a state table or
	// invalidate by host; both connection roles retain their own scoped label.
	for _, label := range labels {
		kill := c
		kill.Args = []string{"-k", "label", "-k", label}
		kill.Stdin = ""
		result = append(result, kill)
	}
	if f.Action == "network-clear" {
		pipe := c
		pipe.Path = "/sbin/dnctl"
		pipe.Args = []string{"pipe", "delete", strconv.Itoa(int(n.Pipe))}
		pipe.Stdin = ""
		result = append(result, pipe)
	}
	return result
}

func networkLabels(runID, faultID string) []string {
	base := "hfs-" + runID + "-" + faultID
	return []string{base + "-src", base + "-dst"}
}

func nativeAddress(target string) string {
	return "172.31.253." + strconv.Itoa(11+int(target[len(target)-1]-'0'))
}

func validateFaultReceipt(p Plan, req FaultRequest, r FaultReceipt) error {
	if r.Protocol != req.Protocol || r.PlanHash != req.PlanHash || r.RunID != req.RunID || r.Step != req.Step || r.Target != req.Fault.Target || r.Node != req.Fault.Node || r.Volume != req.Fault.Volume || r.Generation != resolvedFault(req).Generation || r.Seed != req.Seed || !r.Terminal || r.Started.IsZero() || r.Started.Before(p.Created) || r.Finished.Before(r.Started) || r.Finished.After(req.Deadline) {
		return errors.New("missing matching bounded terminal fault receipt")
	}
	if !contains([]string{"applied", "failed", "unsupported"}, r.Outcome) || r.Outcome != "applied" && r.Message == "" {
		return errors.New("invalid fault outcome")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if int64(len(raw)) > req.MaxEvidenceBytes {
		return errors.New("fault evidence exceeds approved per-step cap")
	}
	if req.Fault.Process != nil {
		expected := req.Fault.Process
		if req.Binding != nil && (r.Process == nil || !reflect.DeepEqual(*r.Process, req.Binding.Process)) {
			return errors.New("receipt differs from exact runtime launch binding")
		}
		if r.Process == nil || r.Process.PID <= 1 || r.Process.Birth == "" || r.Process.Executable != expected.Executable || r.Process.SHA256 != expected.SHA256 || r.Process.Handle != expected.Handle {
			return errors.New("process identity mismatch")
		}
	} else if r.Process != nil {
		return errors.New("unexpected process identity")
	}
	for _, retained := range r.RemainingResources {
		found := false
		for _, resource := range p.Resources {
			found = found || retained == resource
		}
		if !found {
			return errors.New("foreign remaining resource")
		}
	}
	// Every category is recorded even when unavailable: an object must carry an
	// explicit error/unsupported value instead of invented progress or a fake pass.
	for _, key := range []string{"daemon-logs", "admin-audit", "status-before", "status-after", "versions", "timing", "resource-inventory", "action"} {
		raw := r.Evidence[key]
		if len(raw) < 3 || raw[0] != '{' && raw[0] != '[' || string(raw) == "{}" || string(raw) == "[]" || !json.Valid(raw) {
			return fmt.Errorf("missing structured %s evidence", key)
		}
	}
	return nil
}

func faultRequestCommands(p Plan, original Fault, cleanup bool, bindings ...*ServingBinding) ([]FaultCommand, error) {
	if err := p.validateFault(original); err != nil {
		return nil, err
	}
	f := effectiveFault(p, original, cleanup)
	if original.GenerationBinding == "" && len(bindings) > 0 && bindings[0] != nil {
		return nil, errors.New("unexpected runtime binding for fixed generation")
	}
	if original.GenerationBinding != "" {
		if len(bindings) != 1 || bindings[0] == nil {
			return nil, errors.New("runtime readiness binding is required")
		}
		if err := validateServingBinding(p, original, *bindings[0]); err != nil {
			return nil, err
		}
		f.Generation = bindings[0].Generation
	}

	if f.Action == "collect" {
		return []FaultCommand{{Target: f.Target, Path: "collect-native-evidence", Args: []string{f.Socket, p.faultResource(f.Resource).Location}, Checks: []string{"preserve-payload", "remaining-resource-inventory"}}}, nil
	}
	return buildFaultCommands(p, f)
}
func effectiveFault(p Plan, original Fault, cleanup bool) Fault {
	if !cleanup {
		return original
	}
	f := original
	switch {
	case original.Action == "daemon-stop":
		f.Action = "daemon-continue"
	case original.Action == "quota":
		f.QuotaBytes = p.faultResource(f.Resource).MaxBytes
	case original.Action == "failpoint":
		f.Enabled = false
	case original.Network != nil:
		network := *original.Network
		network.DelayMillis = 0
		network.LossPercent = 0
		f.Network = &network
		f.Action = "network-clear"
	default:
		f.Action = "collect"
	}
	return f
}
