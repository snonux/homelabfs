// Command hfs-controlled-runtime composes the explicit disposable retained runtime.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/snonux/homelabfs/internal/controlledruntime"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := controlledruntime.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "hfs-controlled-runtime:", err)
		os.Exit(1)
	}
}
