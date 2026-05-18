package topology

import "strings"

// CanonicalBareForm strips OS-prefix and mode-suffix decorations from a Zerops
// service type identifier so two equivalent forms compare equal.
//
// Sunday-release 2026-05-18 moved Zerops upstream identifiers to composite
// form: runtimes carry `<os>/<base>@<version>` (`alpine/nodejs@22`) and
// managed services carry `<base>:<mode>@<version>` (`postgresql:single@18`).
// Legacy forms (`nodejs@22` + sibling `os:`, `postgresql@18` + sibling `mode:`)
// stay accepted by the Zerops API for BC; ZCP must accept both.
//
//	alpine/nodejs@22      -> nodejs@22
//	ubuntu/nodejs@22      -> nodejs@22
//	postgresql:single@18  -> postgresql@18
//	postgresql:ha@18      -> postgresql@18
//	object-storage        -> object-storage         (no version)
//
// Already-canonical inputs (`nodejs@22`, `postgresql@18`, `zcp@1`) pass
// through unchanged.
//
// Unknown OS prefixes are left intact — only the known-canonical Zerops OS
// names (`alpine/`, `ubuntu/`) are stripped, so this stays a precise
// transformation rather than a heuristic that could mis-canonicalize agent
// input.
func CanonicalBareForm(t string) string {
	t = stripKnownOSPrefix(t)
	return stripModeSuffix(t)
}

func stripKnownOSPrefix(t string) string {
	for _, prefix := range []string{"alpine/", "ubuntu/"} {
		if strings.HasPrefix(t, prefix) {
			return t[len(prefix):]
		}
	}
	return t
}

func stripModeSuffix(t string) string {
	colonIdx := strings.Index(t, ":")
	if colonIdx <= 0 {
		return t
	}
	atIdx := strings.Index(t, "@")
	if atIdx <= colonIdx {
		// `:` without an `@` after — not a mode-encoded type, leave alone.
		return t
	}
	return t[:colonIdx] + t[atIdx:]
}

// TypesAreEquivalent returns true when two Zerops type identifiers refer to
// the same service version after stripping OS prefix and mode suffix. Use this
// anywhere a plan-side type string is compared against a catalog-side or
// recipe-side type string — the two sides may use either the composite or the
// legacy bare form, both must compare equal.
//
// Examples (true):
//
//	("nodejs@22", "alpine/nodejs@22")
//	("alpine/nodejs@22", "ubuntu/nodejs@22")  // both bare to "nodejs@22"
//	("postgresql@18", "postgresql:single@18")
//	("postgresql:ha@18", "postgresql:single@18")  // both bare to "postgresql@18"
//
// Examples (false):
//
//	("nodejs@22", "nodejs@24")               // different version
//	("nodejs@22", "bun@22")                  // different base
//	("postgresql@18", "postgresql@17")       // different version
func TypesAreEquivalent(a, b string) bool {
	if a == b {
		return true
	}
	return CanonicalBareForm(a) == CanonicalBareForm(b)
}
