//go:build !linux && !freebsd

package vfs

import "os"

// Other platforms retain Go's rename contract, which can reject replacing an
// existing directory. HomelabFS targets Linux and FreeBSD.
func renameRoot(root *os.Root, oldname, newname string) error {
	return root.Rename(oldname, newname)
}
