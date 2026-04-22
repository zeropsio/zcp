package ops

import (
	"context"
	"net"
	"time"
)

// defaultVPNProbeTimeout is the per-host TCP dial deadline. Kept tight
// (2 s) so a missing-VPN probe never blocks a tool response.
const defaultVPNProbeTimeout = 2 * time.Second

// vpnProbeDialer is a seam for tests to inject a fake connector. Default
// uses net.DialTimeout against the real network.
var vpnProbeDialer = defaultVPNProbeDialer

func defaultVPNProbeDialer(_ context.Context, address string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("tcp", address, timeout)
}

// ProbeManagedReachable returns true if a TCP connection to host:port
// opens within the timeout. Used by the env dotenv generator on local
// env to decide whether the user still needs `zcli vpn up` — a missing
// VPN surfaces as a soft hint in the response, not an error. host is
// the managed-service hostname (platform-assigned), port is its
// service port.
func ProbeManagedReachable(ctx context.Context, host string, port int) bool {
	if host == "" || port <= 0 {
		return false
	}
	address := net.JoinHostPort(host, itoa(port))
	conn, err := vpnProbeDialer(ctx, address, defaultVPNProbeTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// OverrideVPNProbeDialerForTest swaps the dialer for tests. Returns a
// restore function. Keeps the production path free of a testing flag.
func OverrideVPNProbeDialerForTest(d func(ctx context.Context, address string, timeout time.Duration) (net.Conn, error)) func() {
	old := vpnProbeDialer
	vpnProbeDialer = d
	return func() { vpnProbeDialer = old }
}

// itoa is a local, allocation-cheap int-to-string (strconv.Itoa pulls
// in the larger strconv package transitively and this file has no other
// use for it).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
