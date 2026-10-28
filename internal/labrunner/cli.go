package labrunner

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// CLI parses local inputs. Approval files must be recorded by the operator.
func CLI(ctx context.Context, args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: hfs-lab {plan|execute|status|cleanup} [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	planPath := flags.String("plan", "", "approved plan JSON")
	input := flags.String("input", "", "plan source JSON (plan only)")
	output := flags.String("out", "", "new canonical plan path (plan only)")
	approvalPath := flags.String("approval", "", "operator-written approval JSON")
	reconcilePath := flags.String("reconcile", "", "operator-written terminal outcome JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	var p Plan
	if args[0] == "plan" {
		if *input == "" || *output == "" {
			return errors.New("plan requires --input and --out")
		}
		if err := ReadJSON(*input, &p); err != nil {
			return err
		}
		h, err := p.Hash()
		if err != nil {
			return err
		}
		if err := p.VerifyArtifacts(); err != nil {
			return err
		}
		if err := WriteJSON(*output, p); err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", h)
		return err
	}
	if err := ReadJSON(*planPath, &p); err != nil {
		return err
	}
	if args[0] == "status" {
		rep, err := Status(p)
		if err != nil {
			return err
		}
		if err := json.NewEncoder(out).Encode(rep); err != nil {
			return err
		}
		if !rep.Success {
			return errors.New(rep.Summary)
		}
		return nil
	}
	if args[0] != "execute" && args[0] != "cleanup" {
		return errors.New("unknown command")
	}
	var a Approval
	if err := ReadJSON(*approvalPath, &a); err != nil {
		return err
	}
	var rec *Reconciliation
	if *reconcilePath != "" {
		rec = &Reconciliation{}
		if err := ReadJSON(*reconcilePath, rec); err != nil {
			return err
		}
	}
	rep, err := (Runner{Executor: ProcessExecutor{}, Now: time.Now}).Run(ctx, p, a, args[0] == "cleanup", rec)
	if rep.RunID != "" {
		err = errors.Join(err, json.NewEncoder(out).Encode(rep))
		_, printErr := fmt.Fprintln(stderr, rep.Summary)
		err = errors.Join(err, printErr)
	}
	return err
}
