//go:build mage

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magefile/mage/sh"
)

// BehaviorBuild cross-compiles static portable behavior binaries without
// running them, changing mounts or contacting the lab.
func BehaviorBuild() error {
	for _, target := range []string{"linux", "freebsd"} {
		dir := filepath.Join("bin", target+"-amd64")
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("behavior build directory: %w", err)
		}
		if err := sh.RunWithV(map[string]string{"GOOS": target, "GOARCH": "amd64", "CGO_ENABLED": "0"}, "go", "build", "-o", filepath.Join(dir, "hfs-behavior"), "./cmd/hfs-behavior"); err != nil {
			return fmt.Errorf("build behavior for %s: %w", target, err)
		}
	}
	return nil
}
