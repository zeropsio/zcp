package ingest

import (
	"net/netip"
	"strings"
)

// blocklist is the R5 ops-lever kill switch (spec §6): env-configurable
// install-id / IP denylists, checked before any other processing. In-memory,
// per-process — no persistence, no dynamic reload (a redeploy picks up a
// new list). Matched values are NEVER logged — a blocklist entry is itself
// an ops decision about a specific install/IP, so it must not leak into
// logs any more than the client IP does (spec B6-adjacent discipline).
type blocklist struct {
	ips      map[string]struct{}
	installs map[string]struct{}
}

// newBlocklist builds a blocklist from already-split value lists (spec
// Config.BlockedIPs/BlockedInstalls, themselves parsed from
// INGEST_BLOCK_IPS/INGEST_BLOCK_INSTALLS). Blank entries are ignored; a nil
// or empty list blocks nothing.
func newBlocklist(ips, installs []string) *blocklist {
	return &blocklist{ips: toIPSet(ips), installs: toSet(installs)}
}

func (b *blocklist) blockedIP(ip string) bool {
	_, ok := b.ips[ip]
	return ok
}

func (b *blocklist) blockedInstall(installID string) bool {
	_, ok := b.installs[installID]
	return ok
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	return set
}

// toIPSet is toSet for IPs: each entry is canonicalized through net/netip so
// a configured block entry matches the key clientIP() produces (same
// canonical form — equivalent IPv6 spellings collapse to one). A
// non-parseable entry is kept verbatim so an operator's literal typo still
// blocks its exact string.
func toIPSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if addr, err := netip.ParseAddr(v); err == nil {
			v = addr.String()
		}
		set[v] = struct{}{}
	}
	return set
}
