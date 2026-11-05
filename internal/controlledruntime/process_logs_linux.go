//go:build linux

package controlledruntime

import "os"

// Linux has no SO_NOSIGPIPE option for an inherited write(2) descriptor. This
// pair is solely the explicit Linux process/fd component-test boundary; actual
// native runtime startup refuses Linux before constructing any owner/channel.
// It cannot prove the FreeBSD VM's survival after daemon loss.
func diagnosticOutputPair() (*os.File, *os.File, error) { return os.Pipe() }
