package schema

import (
	"sort"
	"strconv"
	"strings"
)

// ConcreteVersions reduces a base's version list to its concrete leaf versions:
// drops rolling tags (latest/canary/nightly/stable) and any version that is a
// strict dot-component prefix of another (a family alias like "1" or "1.3" when
// "1.3.9" is present), then sorts newest-first.
func ConcreteVersions(versions []string) []string {
	concrete := make([]string, 0, len(versions))
	for _, v := range versions {
		if !IsRollingTag(v) {
			concrete = append(concrete, v)
		}
	}
	leaves := make([]string, 0, len(concrete))
	for i, v := range concrete {
		isPrefix := false
		for j, w := range concrete {
			if i != j && isDotPrefix(v, w) {
				isPrefix = true
				break
			}
		}
		if !isPrefix {
			leaves = append(leaves, v)
		}
	}
	sort.Slice(leaves, func(i, j int) bool { return versionLess(leaves[j], leaves[i]) })
	return leaves
}

// IsRollingTag reports whether a version is a rolling channel.
func IsRollingTag(v string) bool {
	switch strings.ToLower(v) {
	case "latest", "canary", "nightly", "stable", "edge", "dev":
		return true
	default:
		return false
	}
}

// isDotPrefix reports whether a is a STRICT dot-component prefix of b
// ("1" ⊂ "1.3.9", "1.3" ⊂ "1.3.9"; "22" is NOT a prefix of "24").
func isDotPrefix(a, b string) bool {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	if len(ap) >= len(bp) {
		return false
	}
	for i := range ap {
		if ap[i] != bp[i] {
			return false
		}
	}
	return true
}

// versionLess compares two dot-segmented versions numerically per segment,
// falling back to string compare for non-numeric segments.
func versionLess(a, b string) bool {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		ai, aerr := strconv.Atoi(ap[i])
		bi, berr := strconv.Atoi(bp[i])
		switch {
		case aerr == nil && berr == nil:
			if ai != bi {
				return ai < bi
			}
		case ap[i] != bp[i]:
			return ap[i] < bp[i]
		}
	}
	return len(ap) < len(bp)
}
