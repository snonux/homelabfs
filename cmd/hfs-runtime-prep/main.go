// Command hfs-runtime-prep generates the inert inputs of one disposable
// controlled-runtime run and fills the external signer's requests. It never
// reads the operator private key, contacts a host or starts a runtime.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/snonux/homelabfs/internal/runtimeprep"
)

func main() {
	if err := runtimeprep.Run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "hfs-runtime-prep:", err)
		os.Exit(1)
	}
}
