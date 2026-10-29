package admin

import (
	"encoding/binary"
	"fmt"
)

// decodeFreeBSDPeer covers the native amd64 xucred v0 layout verified against
// FreeBSD releng/14.4, 15.0 and 15.1 sys/sys/ucred.h. The final pointer-sized
// union contains a kernel-supplied pid_t, not a pointer or user assertion.
// Groups start at 12, end at 76; pointer alignment puts cr_pid at 80 of 88 bytes.
func decodeFreeBSDPeer(raw []byte, release uint32, arch string) (Peer, error) {
	supportedRelease := release >= 1404000 && release < 1405000 || release >= 1500000 && release < 1502000
	if arch != "amd64" || !supportedRelease || len(raw) != 88 {
		return Peer{}, fmt.Errorf("unsupported FreeBSD peer credential ABI")
	}
	order := binary.LittleEndian
	version := order.Uint32(raw[:4])
	groups := int16(order.Uint16(raw[8:10]))
	pid := int32(order.Uint32(raw[80:84]))
	if version != 0 || groups < 1 || groups > 16 || pid <= 1 {
		return Peer{}, fmt.Errorf("invalid FreeBSD peer credential version/groups/PID")
	}
	return Peer{UID: order.Uint32(raw[4:8]), GID: order.Uint32(raw[12:16]), PID: &pid}, nil
}
