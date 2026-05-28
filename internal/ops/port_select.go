package ops

import "github.com/zeropsio/zcp/internal/platform"

// HTTP-port selection.
//
// A service can expose several ports; only some serve HTTP (e.g. mailpit:
// SMTP 1025 + HTTP UI 8025). Picking Ports[0] mis-resolves the subdomain URL
// and the verify probe for such services. Port.Scheme (http/https/tcp/…) is
// set at deploy time from the port declaration, independent of subdomain-enable
// timing, so it is the reliable signal for which port serves HTTP. HTTPSupport
// is a post-enable hint only (empty until a subdomain enable propagates).

// isHTTPScheme reports whether a port's scheme indicates HTTP(S) traffic.
func isHTTPScheme(p platform.Port) bool {
	return p.Scheme == "http" || p.Scheme == "https"
}

// PreferredHTTPPort returns the port most likely to serve HTTP, for building a
// subdomain URL without a network probe. Priority: scheme http/https → the
// post-enable HTTPSupport hint → port 80 → first declared port. ok is false
// only when there are no ports.
func PreferredHTTPPort(ports []platform.Port) (port platform.Port, ok bool) {
	if len(ports) == 0 {
		return platform.Port{}, false
	}
	for _, p := range ports {
		if isHTTPScheme(p) {
			return p, true
		}
	}
	for _, p := range ports {
		if p.HTTPSupport {
			return p, true
		}
	}
	for _, p := range ports {
		if p.Port == 80 {
			return p, true
		}
	}
	return ports[0], true
}

// OrderedHTTPCandidatePorts returns the ports ordered most-likely-HTTP first,
// for a cross-port probe fallback when the preferred port doesn't answer (the
// brief window after deploy before scheme/routing settles, or a mis-declared
// port). Never drops a port; preserves declared order within each tier.
func OrderedHTTPCandidatePorts(ports []platform.Port) []platform.Port {
	if len(ports) <= 1 {
		return ports
	}
	ordered := make([]platform.Port, 0, len(ports))
	seen := make(map[int]bool, len(ports))
	add := func(pred func(platform.Port) bool) {
		for _, p := range ports {
			if !seen[p.Port] && pred(p) {
				ordered = append(ordered, p)
				seen[p.Port] = true
			}
		}
	}
	add(isHTTPScheme)
	add(func(p platform.Port) bool { return p.HTTPSupport })
	add(func(p platform.Port) bool { return p.Port == 80 })
	add(func(platform.Port) bool { return true }) // everything remaining
	return ordered
}
