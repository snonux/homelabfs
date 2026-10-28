// Command homelabfs initializes and serves disposable local validation volumes.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snonux/homelabfs/internal/daemoncli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := daemoncli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "homelabfs:", err)
		os.Exit(1)
	}
}
