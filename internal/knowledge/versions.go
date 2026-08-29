package knowledge

import (
	"strings"

	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// FormatVersionCheck validates the requested runtime + services against the
// schema-derived catalog. Returns a markdown section with a checkmark per type
// that exists and a warning (with the available versions) per type that does
// not. Returns "" when the schema is unavailable/empty.
//
// Requested values are canonicalized (OS prefix + mode suffix stripped) before
// lookup, so a bare authored type (`nodejs@22`) matches a catalog keyed on the
// canonical bare base regardless of the spelling the agent submitted.
func FormatVersionCheck(runtime string, services []string, schemas *schema.Schemas) string {
	cv := buildCatalogView(schemas)
	if cv == nil {
		return ""
	}

	// Set of every full "base@version" name across runtime + managed.
	active := make(map[string]bool)
	for _, names := range cv.versionsByBase {
		for _, name := range names {
			active[name] = true
		}
	}

	var sb strings.Builder
	sb.WriteString("## Version Check\n\n")
	check := func(requested string) {
		if requested == "" {
			return
		}
		// A `:mode` decoration on a mode-incapable base (runtime / object-storage)
		// is invalid — without this guard canonRequest would strip `:ha`/`:single`
		// and `nodejs:ha@22` would canonicalize to `nodejs@22` and get a ✓.
		// Mirrors the catalog matcher's guard so both existence paths agree.
		if strings.Contains(requested, ":") && !topology.ServiceSupportsTypeVariant(requested) {
			sb.WriteString("- ⚠ `")
			sb.WriteString(requested)
			sb.WriteString("` unknown type\n")
			return
		}
		// Storage services are versionless — they have no entry in versionsByBase,
		// so they bypass the version-based path. Acceptance is gated on actual
		// schema membership (HasServiceType, equivalence-aware) so a bogus storage
		// version/spelling (`seaweedfs@99`) still reports as not-found rather than
		// ✓ off the kind alone.
		if topology.IsStorageType(requested) {
			if schemas.HasServiceType(requested) {
				sb.WriteString("- ✓ `")
				sb.WriteString(requested)
				sb.WriteString("`\n")
			} else {
				sb.WriteString("- ⚠ `")
				sb.WriteString(requested)
				sb.WriteString("` not found on the platform\n")
			}
			return
		}
		writeVersionLine(&sb, normalizeVersionInput(canonRequest(requested), cv.versionsByBase), active, cv.versionsByBase)
	}
	check(runtime)
	for _, svc := range services {
		check(svc)
	}
	return sb.String()
}

// canonRequest reduces an agent-supplied type to the canonical bare form the
// catalog is keyed on (OS prefix + mode suffix stripped, version kept).
func canonRequest(s string) string {
	if s == "" {
		return ""
	}
	return topology.CanonicalBareForm(strings.ToLower(s))
}

// normalizeVersionInput resolves a bare name (without @version) to the latest
// available version. E.g. "valkey" → "valkey@7.2" when that is available.
func normalizeVersionInput(input string, baseToVersions map[string][]string) string {
	if input == "" || strings.Contains(input, "@") {
		return input
	}
	if versions, ok := baseToVersions[input]; ok && len(versions) > 0 {
		return versions[len(versions)-1]
	}
	return input
}

// writeVersionLine writes a single version check line with checkmark or warning.
func writeVersionLine(sb *strings.Builder, requested string, activeVersions map[string]bool, baseToVersions map[string][]string) {
	if activeVersions[requested] {
		sb.WriteString("- ✓ `")
		sb.WriteString(requested)
		sb.WriteString("`\n")
		return
	}

	base, _, _ := strings.Cut(requested, "@")
	available := baseToVersions[base]
	if len(available) > 0 {
		sb.WriteString("- ⚠ `")
		sb.WriteString(requested)
		sb.WriteString("` not found. Available: ")
		sb.WriteString(strings.Join(available, ", "))
		sb.WriteByte('\n')
	} else {
		sb.WriteString("- ⚠ `")
		sb.WriteString(requested)
		sb.WriteString("` unknown type\n")
	}
}
