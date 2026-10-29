// hfs-behavior exercises bounded single-node Unix filesystem behavior.
package main

import (
	"context"
	"os"

	"github.com/snonux/homelabfs/internal/behavior"
)

func main() { os.Exit(behavior.CLI(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }
