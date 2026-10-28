// Command hfs-lab orchestrates an explicitly approved disposable lab run.
package main

import (
	"context"
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := labrunner.CLI(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "hfs-lab:", err)
		os.Exit(1)
	}
}
