package labrunner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"time"
)

// FaultCLI prepares controller requests or serves one enrolled guest-local
// request. It never installs a transport, creates approval or prepares the lab.
func FaultCLI(ctx context.Context, args []string, out, stderr io.Writer) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	planPath := flags.String("plan", "", "reviewed plan path (fault-request)")
	approvalPath := flags.String("approval", "", "operator approval path (fault-request)")
	step := flags.String("step", "", "exact approved step ID (fault-request)")
	input := flags.String("input", "", "prepared request path (native-fault)")
	output := flags.String("out", "", "new local request path (fault-request)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	switch args[0] {
	case "fault-request":
		if *planPath == "" || *approvalPath == "" || *step == "" || *output == "" || *input != "" {
			return errors.New("fault-request requires --plan --approval --step --out")
		}
		var p Plan
		var approval Approval
		if err := ReadJSON(*planPath, &p); err != nil {
			return err
		}
		if err := ReadJSON(*approvalPath, &approval); err != nil {
			return err
		}
		req, err := PrepareFaultRequest(p, approval, *step, time.Now())
		if err != nil {
			return err
		}
		if err := WriteJSON(*output, req); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(req)
	case "native-fault":
		if *input == "" || *planPath != "" || *approvalPath != "" || *step != "" || *output != "" {
			return errors.New("native-fault requires only --input")
		}
		var req FaultRequest
		if err := ReadJSON(*input, &req); err != nil {
			return err
		}
		receipt, err := (NativeFaultExecutor{}).InvokeFault(ctx, req)
		if receipt.RunID != "" {
			err = errors.Join(err, json.NewEncoder(out).Encode(receipt))
		}
		return err
	default:
		return errors.New("unknown fault command")
	}
}
