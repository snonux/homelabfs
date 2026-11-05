//go:build linux

package controlledruntime

import "golang.org/x/sys/unix"

func descriptorExecMount(report *descriptorExecReport) error {
	report.MountType = "linux-fd-alias"
	return nil
}

func descriptorExecAlias(path string, st *unix.Stat_t) error {
	// Linux /dev/fd normally links to procfs, whose numeric entries are kernel
	// magic links. This check supplies no evidence about FreeBSD fdescfs.
	return unix.Stat(path, st)
}

func descriptorExecFile(fd int) error { return nil }
