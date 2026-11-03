//go:build freebsd && !cgo

package controlledruntime

import "github.com/snonux/homelabfs/internal/fsapi"

// Native full process/file ownership requires the selected FreeBSD libc ABI.
// A CGO-disabled build compiles but cannot enroll or start a native exporter.
func observeProcess(int) (processIdentity, error) { return processIdentity{}, fsapi.Unsupported }
