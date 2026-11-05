//go:build freebsd

package controlledruntime

import (
	"os"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// diagnosticOutputPair prevents the new diagnostic channel from signaling the
// native VM when this daemon exits or crashes. SO_NOSIGPIPE is scoped to the
// inherited child socket; it survives exec without changing process signals.
// The child still observes EPIPE, never fabricated successful output.
func diagnosticOutputPair() (*os.File, *os.File, error) {
	// FreeBSD caps mbuf accounting at high-water times this host factor.
	// Bound all eight configured socket buffers within the diagnostic slice;
	// unsupported widths/sysctls or larger host policies refuse before exec.
	factor, err := unix.SysctlUint64("kern.ipc.sockbuf_waste_factor")
	if err != nil {
		// Accept only an exactly sized 32-bit or 64-bit scalar. Both typed
		// helpers require the exact kernel-returned width; no cast of
		// an unvalidated buffer or guessed struct layout supplies the bound.
		var narrow uint32
		narrow, err = unix.SysctlUint32("kern.ipc.sockbuf_waste_factor")
		factor = uint64(narrow)
		if err != nil {
			return nil, nil, err
		}
	}
	if factor < 1 || factor > exporterDiagnosticSocketWasteFactor {
		return nil, nil, fsapi.NoSpace
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	reader := os.NewFile(uintptr(pair[0]), "diagnostic-reader")
	writer := os.NewFile(uintptr(pair[1]), "diagnostic-child")
	for _, fd := range pair {
		for _, option := range []int{unix.SO_SNDBUF, unix.SO_RCVBUF} {
			if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, option, exporterDiagnosticSocketBytes); err != nil {
				break
			}
			var size int
			size, err = unix.GetsockoptInt(fd, unix.SOL_SOCKET, option)
			if err == nil && (size < 1 || size > exporterDiagnosticSocketBytes) {
				err = fsapi.NoSpace
			}
			if err != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = unix.Shutdown(pair[0], unix.SHUT_WR)
	}
	if err == nil {
		err = unix.Shutdown(pair[1], unix.SHUT_RD)
	}
	if err == nil {
		err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_NOSIGPIPE, 1)
	}
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, nil, err
	}
	return reader, writer, nil
}
