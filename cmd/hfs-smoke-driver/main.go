// Command hfs-smoke-driver implements the pinned Stage 2 adapter protocol.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snonux/homelabfs/internal"
	"github.com/snonux/homelabfs/internal/labrunner"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		fmt.Println(internal.Version)
		return
	}
	flagSet := flag.NewFlagSet("hfs-smoke-driver", flag.ContinueOnError)
	step := flagSet.String("step", "", "approved step ID")
	if err := flagSet.Parse(os.Args[1:]); err != nil || flagSet.NArg() != 0 {
		os.Exit(20)
	}
	var p labrunner.Plan
	if err := labrunner.ReadJSON(os.Getenv("HFS_LAB_PLAN"), &p); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(20)
	}
	h, err := p.Hash()
	if err != nil || h != os.Getenv("HFS_LAB_PLAN_HASH") || p.RunID != os.Getenv("HFS_LAB_RUN_ID") {
		fmt.Fprintln(os.Stderr, "plan identity mismatch")
		os.Exit(20)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	r, err := labrunner.RunSmokeStep(ctx, p, *step, labrunner.ProcessSmokeAdapter{})
	if encodeErr := json.NewEncoder(os.Stdout).Encode(r); encodeErr != nil {
		fmt.Fprintln(os.Stderr, encodeErr)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		var failure *labrunner.Error
		if errors.As(err, &failure) && !failure.Uncertain {
			os.Exit(10)
		}
		os.Exit(1)
	}
}
