package fuseadapter

import (
	"encoding/binary"
	"slices"
	"strings"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// decodeFreeBSDParticipant accepts only the 15.1 amd64 kinfo_proc ABI. Offsets
// and the effective-GID/supplementary-group split are pinned in the spec. In
// particular ki_ngroups includes cr_gid; stored expected groups do not.
// Go uses the COMPAT14 group ABI, normalized at the enrollment/exec boundary.
func decodeFreeBSDParticipant(raw []byte, pid, uid, gid uint32, groups []uint32) ([2]uint64, error) {
	var start [2]uint64
	if len(raw) != 1088 {
		return start, fsapi.Unsupported
	}
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(raw[at:]) }
	if u32(0) != 1088 || u32(4) != 0 {
		return start, fsapi.Unsupported
	}
	n := int(binary.LittleEndian.Uint16(raw[188:]))
	// Refuse overflow, capability mode and any future credential flags, rather
	// than treating a truncated group array as a complete request identity.
	if u32(72) != pid || u32(168) != uid || u32(172) != uid || u32(176) != uid || u32(180) != gid || u32(184) != gid || u32(588) != 0 || n < 1 || n > 16 || u32(192) != gid {
		return start, fsapi.Permission
	}
	actual := make([]uint32, n-1)
	for i := range actual {
		actual[i] = u32(196 + i*4)
	}
	slices.Sort(actual)
	if !slices.Equal(actual, groups) {
		return start, fsapi.Permission
	}
	start = [2]uint64{binary.LittleEndian.Uint64(raw[336:]), binary.LittleEndian.Uint64(raw[344:])}
	if start[0] == 0 || start[1] >= 1000000 {
		return [2]uint64{}, fsapi.Permission
	}
	return start, nil
}

func supportedFreeBSDParticipantABI(release, arch string) bool {
	if arch != "amd64" {
		return false
	}
	if release == "15.1-RELEASE" {
		return true
	}
	patch, ok := strings.CutPrefix(release, "15.1-RELEASE-p")
	if !ok || patch == "" {
		return false
	}
	for _, c := range patch {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// Go1.26/x/sys0.49 call FreeBSD COMPAT14 getgroups(79), which returns
// [effective GID, supplementary groups...]. Validate that prefix before removing
// it; a later kinfo_proc snapshot must still match the complete normalized set.
func freeBSDParticipantGroups(groups []int, gid uint32) ([]uint32, error) {
	if len(groups) == 0 || groups[0] < 0 || uint64(groups[0]) != uint64(gid) {
		return nil, fsapi.Permission
	}
	result := make([]uint32, len(groups)-1)
	for i, g := range groups[1:] {
		if g < 0 || uint64(g) > uint64(^uint32(0)) {
			return nil, fsapi.Permission
		}
		result[i] = uint32(g)
	}
	slices.Sort(result)
	return result, nil
}

// exec's COMPAT14 setgroups(80) consumes the first element as effective GID.
// Prepend it only to the exec copy, preserving supplementary-only expected
// groups and the caller's credential/group storage. A later setgid is not a
// replacement for supplying the full supplementary set to that syscall.
func freeBSDParticipantCredential(c syscall.Credential) syscall.Credential {
	c.Groups = append([]uint32{c.Gid}, c.Groups...)
	return c
}
