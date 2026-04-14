package workflow

import (
	"strconv"
	"strings"
)

// latestManagedVersion returns the highest concrete version of a managed
// service base in the given service-type list (e.g. for base "postgresql"
// across {"postgresql@14","postgresql@16","postgresql@17","postgresql@18"}
// it returns "18"). Returns the empty string when the base has no concrete
// versions — bases with only an alias ("latest"/"canary"/...), bases not
// present in the list, and bases whose only versions are non-numeric all
// fall through to empty so the caller can skip the check instead of
// erroring out on data shapes the rule cannot reason about.
//
// The "+suffix" build tag on combined types (e.g. php-nginx "8.4+1.22"
// bundles nginx 1.22) is stripped before comparison: for ordering it is
// the framework version that matters, not the bundled secondary package.
func latestManagedVersion(serviceTypes []string, base string) string {
	var bestParts []int
	bestVer := ""
	for _, st := range serviceTypes {
		b, v, hasV := strings.Cut(st, "@")
		if !hasV || b != base || isVersionAlias(v) {
			continue
		}
		core, _, _ := strings.Cut(v, "+")
		parts := parseVersionComponents(core)
		if parts == nil {
			continue
		}
		if bestParts == nil || versionLess(bestParts, parts) {
			bestParts = parts
			bestVer = v
		}
	}
	return bestVer
}

// isVersionAlias reports whether a version tag is a moving alias rather
// than a concrete release. Aliases must never participate in latest-version
// comparison: "latest" is always the latest by definition, and unstable
// channels (canary/nightly/edge) must not outrank a stable concrete version.
func isVersionAlias(version string) bool {
	switch version {
	case "latest", "canary", "nightly", "stable", "edge":
		return true
	}
	return false
}

// parseVersionComponents splits a dotted numeric version into integer parts.
// Returns nil if any component is non-numeric — that filters out exotic
// version strings (date-based, hash-suffixed, alpha tags) that cannot be
// ordered against simple numeric versions without inventing a precedence
// rule. The caller treats nil as "skip, can't reason about ordering."
func parseVersionComponents(version string) []int {
	if version == "" {
		return nil
	}
	chunks := strings.Split(version, ".")
	out := make([]int, len(chunks))
	for i, c := range chunks {
		n, err := strconv.Atoi(c)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

// versionLess reports whether a < b under component-wise integer comparison,
// padding the shorter slice with zeros. So "1.2" < "1.2.1" and "1.2" == "1.2.0".
func versionLess(a, b []int) bool {
	n := max(len(a), len(b))
	for i := range n {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av != bv {
			return av < bv
		}
	}
	return false
}
