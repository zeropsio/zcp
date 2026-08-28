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

// OSPrefix reports the known Zerops OS prefix a service type identifier
// carries. RED stub: not yet implemented.
func OSPrefix(t string) string {
	return ""
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

// Deployment-variant tokens encoded in a composite managed-service type
// (`postgresql:single@18`). The variant is the modern, AUTHORITATIVE HA
// encoding; the legacy sibling `mode:` field (HA/NON_HA) is deprecated.
const (
	VariantSingle = "single"
	VariantHA     = "ha"
)

// DeploymentVariant returns the known deployment-variant token a service type
// encodes (VariantSingle / VariantHA), or "" for a bare type (`postgresql@18`,
// `object-storage`), an OS-prefixed runtime (`alpine/nodejs@22`), or an
// unrecognized `:suffix`. Single owner of the `:`/`@` variant extraction so the
// recipe validators, port capture, and bundle composer all read the variant the
// same way instead of re-deriving the Cut(@)+Cut(:) parse.
func DeploymentVariant(serviceType string) string {
	base, _, _ := strings.Cut(serviceType, "@")
	if _, variant, found := strings.Cut(base, ":"); found && knownModes[variant] {
		return variant
	}
	return ""
}

// HasDeploymentVariant reports whether a service type already encodes a known
// deployment variant (`:single` / `:ha`). Used to decide whether a sibling
// `mode:` field is redundant: a variant type is self-describing, so emitting
// `mode:` alongside it is at best noise and at worst (`postgresql:ha@18` +
// `mode: NON_HA`) self-contradictory.
func HasDeploymentVariant(serviceType string) bool {
	return DeploymentVariant(serviceType) != ""
}

// VariantForHA maps an HA-ness boolean to the type-variant token the modern
// composite type encodes: true → VariantHA (`:ha`), false → VariantSingle
// (`:single`). Single owner of the bool→token pick so the bundle composer, the
// recipe engine, and the tier-fact briefing cannot diverge on which token an
// HA service gets.
func VariantForHA(ha bool) string {
	if ha {
		return VariantHA
	}
	return VariantSingle
}

// WithDeploymentVariant returns serviceType with its deployment variant set to
// the given token (VariantSingle/VariantHA), preserving the @version. Any
// existing variant is replaced and a (managed services never carry one) OS
// prefix is dropped. Bare and already-variant inputs both normalize:
//
//	postgresql@18         + "ha"     -> postgresql:ha@18
//	postgresql:single@18  + "ha"     -> postgresql:ha@18
//	shared-storage:single + "ha"     -> shared-storage:ha
func WithDeploymentVariant(serviceType, variant string) string {
	bare := CanonicalBareForm(serviceType) // strips any existing :variant + OS prefix, keeps @version
	base, version, hasVersion := strings.Cut(bare, "@")
	out := base + ":" + variant
	if hasVersion {
		out += "@" + version
	}
	return out
}

// IsProfileBearing reports whether a managed-service type takes a scaling
// `profile` (autoscaling tier): PostgreSQL and Valkey only. Every other managed
// type scales via verticalAutoscaling and the import endpoint rejects a profile
// field on it.
func IsProfileBearing(serviceType string) bool {
	switch CanonicalBaseName(serviceType) {
	case "postgresql", "valkey":
		return true
	default:
		return false
	}
}

// ScalingProfileName returns the full autoscaling-profile name for a
// profile-bearing managed service at the given tier base ("hobby" / "staging" /
// "production"). PostgreSQL profiles carry the `oltp-` workload prefix
// (oltp-hobby, oltp-staging); Valkey profiles are bare (hobby, staging).
// Returns "" for non-profile-bearing types. Single owner of the per-family
// profile naming so the export/launch composer and the recipe engine cannot
// drift apart.
func ScalingProfileName(serviceType, tierBase string) string {
	switch CanonicalBaseName(serviceType) {
	case "postgresql":
		return "oltp-" + tierBase
	case "valkey":
		return tierBase
	default:
		return ""
	}
}
