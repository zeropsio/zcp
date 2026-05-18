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
// the same service version, treating the legacy bare form as equivalent to
// the composite form it would canonicalize to.
//
// Asymmetric semantic — load-bearing:
//
//   - If at least one side is bare (no OS prefix, no mode suffix), comparison
//     is done after canonicalizing both to bare form. Lets BC clients (plan
//     submits `nodejs@22`) match a catalog/live side that has moved to the
//     composite form (`alpine/nodejs@22`).
//   - If both sides carry decoration (OS prefix and/or mode suffix), comparison
//     is strict byte equality. `alpine/nodejs@22` and `ubuntu/nodejs@22` are
//     NOT equivalent — they're different runtime variants. `postgresql:ha@18`
//     and `postgresql:single@18` are NOT equivalent — different deployment
//     modes are different services post-Sunday-release.
//
// Examples (true):
//
//	("nodejs@22", "alpine/nodejs@22")              // bare ↔ composite (BC)
//	("postgresql@18", "postgresql:single@18")      // bare ↔ mode-encoded (BC)
//	("alpine/nodejs@22", "alpine/nodejs@22")       // identity
//
// Examples (false):
//
//	("alpine/nodejs@22", "ubuntu/nodejs@22")       // different OS variants
//	("postgresql:ha@18", "postgresql:single@18")   // different modes
//	("nodejs@22", "nodejs@24")                     // different version
//	("nodejs@22", "bun@22")                        // different base
func TypesAreEquivalent(a, b string) bool {
	if a == b {
		return true
	}
	aBare := isBareTypeForm(a)
	bBare := isBareTypeForm(b)
	if !aBare && !bBare {
		// Both decorated — strict equality (already handled by a == b above).
		return false
	}
	return CanonicalBareForm(a) == CanonicalBareForm(b)
}

// isBareTypeForm reports whether t carries no Zerops decoration — neither
// an OS prefix (`alpine/`, `ubuntu/`) nor a mode suffix (`:single`, `:ha`).
// Used by TypesAreEquivalent to enforce the asymmetric semantic.
func isBareTypeForm(t string) bool {
	if strings.Contains(t, "/") {
		return false
	}
	colonIdx := strings.Index(t, ":")
	if colonIdx <= 0 {
		return true
	}
	// A colon followed by an `@` is the mode-suffix shape (`postgresql:single@18`).
	// A bare colon without `@` (`foo:bar`) is not Zerops-decoration; treat as
	// bare so unknown shapes still get bare-form lookup.
	atIdx := strings.Index(t, "@")
	return atIdx <= colonIdx
}
