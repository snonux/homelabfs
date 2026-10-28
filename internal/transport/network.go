package transport

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

// Network substitutes transport mechanics in deterministic simulators. An
// implementation must report exact numeric local/remote addresses on sockets.
// It never substitutes for registry authentication or stream authorization.
type Network interface {
	Listen(context.Context, netip.AddrPort) (net.Listener, error)
	Dial(context.Context, netip.AddrPort, netip.AddrPort) (net.Conn, error)
}

// TCP binds both listeners and outgoing source addresses exactly as configured.
// Deployment must enforce WireGuard route/AllowedIPs/firewall policy; TCP cannot
// prove the physical interface or peer key that carried a packet.
type TCP struct{}

var _ Network = TCP{}

// Listen binds the supplied numeric host address without fallback or DNS.
func (TCP) Listen(ctx context.Context, local netip.AddrPort) (net.Listener, error) {
	if !validAddress(local, true) {
		return nil, fmt.Errorf("invalid pinned listener")
	}
	var lc net.ListenConfig
	return lc.Listen(ctx, networkFor(local.Addr()), local.String())
}

// Dial fixes the local host IP and exact remote host/port without fallback.
func (TCP) Dial(ctx context.Context, local, remote netip.AddrPort) (net.Conn, error) {
	if !validAddress(local, true) || !validAddress(remote, false) || local.Addr().Is4() != remote.Addr().Is4() {
		return nil, fmt.Errorf("invalid pinned route")
	}
	d := net.Dialer{LocalAddr: net.TCPAddrFromAddrPort(netip.AddrPortFrom(local.Addr(), 0))}
	return d.DialContext(ctx, networkFor(remote.Addr()), remote.String())
}

func validAddress(a netip.AddrPort, allowZero bool) bool {
	ip := a.Addr()
	return ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.Is4In6() && ip.Zone() == "" && (allowZero || a.Port() != 0)
}
func networkFor(ip netip.Addr) string {
	if ip.Is4() {
		return "tcp4"
	}
	return "tcp6"
}
func observed(a net.Addr) (netip.AddrPort, error) {
	if a == nil {
		return netip.AddrPort{}, fmt.Errorf("missing socket address")
	}
	p, err := netip.ParseAddrPort(a.String())
	if err != nil || !validAddress(p, false) {
		return netip.AddrPort{}, fmt.Errorf("invalid socket address %v", a)
	}
	return p, nil
}
