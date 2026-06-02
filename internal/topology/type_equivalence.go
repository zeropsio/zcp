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
//	shared-storage:ha     -> shared-storage          (mode, no version)
//	object-storage        -> object-storage          (no version)
//
// Already-canonical inputs (`nodejs@22`, `postgresql@18`, `zcp@1`) pass
// through unchanged. This strips OS prefix + mode suffix (FORM); it does NOT
// normalize NAME aliases (`objectstorage` stays `objectstorage`) — that
// identity mapping lives in canonicalStorageKind / CanonicalBaseName.
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

// canonicalStorageKind maps any accepted spelling of a Zerops storage service
// to its canonical hyphenated base ("object-storage" / "shared-storage"), or ""
// when serviceType is not a storage service. Storage ships in several
// interchangeable spellings the platform import schema all accept: hyphenated
// ("shared-storage"), no-hyphen ("sharedstorage"), the implementation name
// ("seaweedfs" == shared-storage), each optionally mode-suffixed (":ha"/
// ":single") and versioned ("@3"). Centralizing the alias set here is the
// single source for storage classification — IsManagedService /
// IsObjectStorageType / IsSharedStorageType all route through it instead of
// each maintaining a divergent hyphen-only prefix list (the drift that left
// `objectstorage`/`seaweedfs` misclassified as runtime).
//
// It maps the NAME aliases that CanonicalBareForm (a pure FORM transform)
// cannot know — `objectstorage`==`object-storage`, `seaweedfs`==`shared-storage`
// — after reducing the input to its bare base via CanonicalBareForm.
func canonicalStorageKind(serviceType string) string {
	base, _, _ := strings.Cut(CanonicalBareForm(strings.ToLower(serviceType)), "@")
	switch base {
	case kindObjectStorage, "objectstorage":
		return kindObjectStorage
	case kindSharedStorage, "sharedstorage", "seaweedfs":
		return kindSharedStorage
	}
	return ""
}

// CanonicalBaseName reduces a service type to its bare base name — OS prefix,
// deployment-mode suffix, and version all removed, with storage spellings
// normalized to the canonical hyphenated kind. It is the SYMMETRIC matching
// key: an authored/plan-side type and a catalog/live-side type that name the
// same service compare equal under CanonicalBaseName regardless of which
// spelling each carries.
//
//	alpine/nodejs@22     -> nodejs
//	postgresql:single@18 -> postgresql
//	sharedstorage:ha     -> shared-storage
//	seaweedfs@3          -> shared-storage
//	nodejs@22            -> nodejs
func CanonicalBaseName(serviceType string) string {
	if kind := canonicalStorageKind(serviceType); kind != "" {
		return kind
	}
	base, _, _ := strings.Cut(CanonicalBareForm(strings.ToLower(serviceType)), "@")
	return base
}

// knownModes is the closed set of deployment-mode tokens Zerops encodes in a
// composite type identifier (`postgresql:single@18`, `shared-storage:ha`).
//
//nolint:gochecknoglobals // value-only closed set, not mutated.
var knownModes = map[string]bool{"single": true, "ha": true}

// stripModeSuffix removes a KNOWN deployment-mode suffix (`:single` / `:ha`,
// with or without a trailing `@version`) from a Zerops identifier, preserving
// the `@version`. Only known modes are stripped — mirroring stripKnownOSPrefix,
// which strips only known OS prefixes — so an arbitrary `:suffix` (`foo:bar`,
// `object-storage:bogus`) is left intact and therefore cannot canonicalize into
// (and falsely match) a real base.
func stripModeSuffix(t string) string {
	colonIdx := strings.Index(t, ":")
	if colonIdx <= 0 {
		return t
	}
	atIdx := strings.Index(t, "@")
	mode := t[colonIdx+1:]
	if atIdx > colonIdx {
		mode = t[colonIdx+1 : atIdx]
	}
	if !knownModes[mode] {
		return t
	}
	if atIdx > colonIdx {
		return t[:colonIdx] + t[atIdx:]
	}
	return t[:colonIdx]
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
// Used by TypesAreEquivalent to enforce the asymmetric semantic. A `:` is
// always a mode separator, with or without a trailing `@version`, so its
// presence means the form is decorated — `shared-storage:ha` and
// `shared-storage:single` are distinct (like `postgresql:ha@18` vs
// `postgresql:single@18`), while bare `shared-storage` matches either.
func isBareTypeForm(t string) bool {
	return !strings.ContainsAny(t, "/:")
}
