// Command homelabfs is the entry point for the planned HomelabFS daemon.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/snonux/homelabfs/internal"
)

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(internal.Version)
		return
	}
	fmt.Fprintln(os.Stderr, "homelabfs: daemon is not implemented yet")
	os.Exit(1)
}
