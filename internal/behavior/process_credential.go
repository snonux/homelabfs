package behavior

import (
	"fmt"
	"slices"
	"syscall"
)

func copyExecutableCredential(c *syscall.Credential) *syscall.Credential {
	if c == nil {
		return nil
	}
	copy := *c
	copy.Groups = slices.Clone(c.Groups)
	return &copy
}

// Go 1.26 exec_bsd.go calls SYS_SETGROUPS directly. FreeBSD's COMPAT14
// setgroups(80) consumes [effective GID, supplementary groups...], unlike the
// supplementary-only setgroups(596). See the pinned kernel sources in
// docs/spec/Disposable-behavior.md. Refuse a changed syscall contract rather
// than guessing how a future Go exec implementation handles its group vector.
func freeBSDExecutableCredential(c *syscall.Credential, setgroups uintptr) (*syscall.Credential, error) {
	if c == nil || c.NoSetGroups {
		// NoSetGroups instructs exec to inherit groups; do not turn inheritance
		// into a setgroups call or reinterpret the unused caller vector.
		return copyExecutableCredential(c), nil
	}
	if setgroups != 80 {
		return nil, fmt.Errorf("helper exec FreeBSD setgroups ABI %d: %w", setgroups, syscall.ENOTSUP)
	}
	copy := *c
	copy.Groups = append([]uint32{c.Gid}, c.Groups...)
	return &copy, nil
}
