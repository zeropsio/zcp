package knowledge

import (
	"strings"

	"github.com/zeropsio/zcp/internal/schema"
)

// FormatStackList formats the schema-derived catalog as a compact markdown list
// for workflow embedding. Returns "" when the schema is unavailable/empty.
func FormatStackList(schemas *schema.Schemas) string {
	cv := buildCatalogView(schemas)
	if cv == nil {
		return ""
	}
	lines := catalogLines(cv, nil)
	if len(lines) == 0 {
		return ""
	}
	return "## Available Service Stacks (live, active concrete versions)\n" +
		"Pick a concrete version (newest marked `(latest)`). " +
		"Family aliases (`go@1`) and rolling tags (`latest`/`canary`) are omitted — they resolve at import and won't match. " +
		"Want another active version? Pass it; if it's not available ZCP lists the alternatives.\n" +
		strings.Join(lines, "\n") + "\n"
}

// FormatServiceStacks formats the schema-derived catalog as rich markdown for
// briefing injection: runtimes carry a [B] marker when the base is also a valid
// build.base, and a "Build-only:" line lists build bases with no matching
// runtime type (e.g. `php` — its runtime is `php-nginx`/`php-apache`). Returns
// "" when the schema is unavailable/empty.
func FormatServiceStacks(schemas *schema.Schemas) string {
	cv := buildCatalogView(schemas)
	if cv == nil {
		return ""
	}
	lines := catalogLines(cv, cv.buildBaseNames)
	if len(cv.buildOnly) > 0 {
		entries := make([]string, 0, len(cv.buildOnly))
		for _, bv := range cv.buildOnly {
			entries = append(entries, compactBase(bv))
		}
		lines = append(lines, "Build-only: "+strings.Join(entries, " | "))
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Service Stacks (live)\n[B]=also usable as build.base in zerops.yaml\n\n" +
		strings.Join(lines, "\n") + "\n"
}

// catalogLines renders the runtime/managed/storage buckets as one markdown line
// each. When buildBases is non-nil, runtime entries whose base is a build base
// get a [B] marker.
func catalogLines(cv *catalogView, buildBases map[string]bool) []string {
	var lines []string
	if line := catalogLine("Runtime", cv.runtime, buildBases); line != "" {
		lines = append(lines, line)
	}
	if line := catalogLine("Managed", cv.managed, nil); line != "" {
		lines = append(lines, line)
	}
	if cv.sharedStorage {
		lines = append(lines, "Shared storage: shared-storage")
	}
	if cv.objectStorage {
		lines = append(lines, "Object storage: object-storage")
	}
	return lines
}

func catalogLine(label string, group []baseVersions, buildBases map[string]bool) string {
	if len(group) == 0 {
		return ""
	}
	entries := make([]string, 0, len(group))
	for _, bv := range group {
		entry := compactBase(bv)
		if buildBases != nil && buildBases[bv.base] {
			entry += " [B]"
		}
		entries = append(entries, entry)
	}
	return label + ": " + strings.Join(entries, " | ")
}
