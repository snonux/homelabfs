// Command hfs-retained9p-operator signs inert staged disposable handoffs.
package main

import (
	"fmt"
	"os"

	"github.com/snonux/homelabfs/internal/retained9plab"
)

func main() {
	if err := retained9plab.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hfs-retained9p-operator:", err)
		os.Exit(1)
	}
}
