package knowledge

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

const versionStatusActive = "ACTIVE"

// hiddenVersionCategories are internal categories not shown to users.
var hiddenVersionCategories = map[string]bool{
	"CORE":             true,
	"INTERNAL":         true,
	"BUILD":            true,
	"PREPARE_RUNTIME":  true,
	"HTTP_L7_BALANCER": true,
}

// versionCategoryOrder defines display order for user-facing categories.
var versionCategoryOrder = []string{"USER", "STANDARD", "SHARED_STORAGE", "OBJECT_STORAGE"}

// versionCategoryDisplayName maps category to human-readable name.
func versionCategoryDisplayName(cat string) string {
	switch cat {
	case "USER":
		return "Runtime"
	case "STANDARD":
		return "Managed"
	case "SHARED_STORAGE":
		return "Shared storage"
	case "OBJECT_STORAGE":
		return "Object storage"
	default:
		return cat
	}
}

// FormatStackList formats live types as compact markdown for workflow embedding.
// Returns "" if types is nil/empty.
func FormatStackList(types []platform.ServiceStackType) string {
	if len(types) == 0 {
		return ""
	}

	grouped := make(map[string][]platform.ServiceStackType)
	for _, st := range types {
		if hiddenVersionCategories[st.Category] {
			continue
		}
		grouped[st.Category] = append(grouped[st.Category], st)
	}
	if len(grouped) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Service Stacks (live)\n")

	for _, cat := range versionCategoryOrder {
		stacks, ok := grouped[cat]
		if !ok {
			continue
		}
		var entries []string
		for _, st := range stacks {
			if entry := compactEntry(st); entry != "" {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			continue
		}
		sb.WriteString(versionCategoryDisplayName(cat))
		sb.WriteString(": ")
		sb.WriteString(strings.Join(entries, " | "))
		sb.WriteByte('\n')
	}

	return sb.String()
}

// compactEntry returns a compact representation of a service stack type.
func compactEntry(st platform.ServiceStackType) string {
	var versions []string
	for _, v := range st.Versions {
		if v.Status == versionStatusActive {
			versions = append(versions, v.Name)
		}
	}
	if len(versions) == 0 {
		return ""
	}
	return compactVersionGroup(versions)
}

// compactVersionGroup groups versions with a common prefix using brace notation.
// ["nodejs@18", "nodejs@20", "nodejs@22"] -> "nodejs@{18,20,22}"
func compactVersionGroup(versions []string) string {
	if len(versions) == 1 {
		return versions[0]
	}
	var prefix string
	var suffixes []string
	for i, v := range versions {
		p, suffix, ok := strings.Cut(v, "@")
		if !ok {
			return strings.Join(versions, ", ")
		}
		if i == 0 {
			prefix = p
		} else if p != prefix {
			return strings.Join(versions, ", ")
		}
		suffixes = append(suffixes, suffix)
	}
	return prefix + "@{" + strings.Join(suffixes, ",") + "}"
}

// managedCategories are API categories that represent managed (non-runtime) services.
var managedCategories = map[string]bool{
	"STANDARD":       true,
	"SHARED_STORAGE": true,
	"OBJECT_STORAGE": true,
}

// IsManagedCategory returns true if the given API category represents a managed service.
func IsManagedCategory(category string) bool {
	return managedCategories[category]
}

// ManagedBaseNames derives managed service base names from live API service stack types.
// Filters for STANDARD, SHARED_STORAGE, and OBJECT_STORAGE categories.
// Returns empty map (not nil) for nil/empty input.
func ManagedBaseNames(types []platform.ServiceStackType) map[string]bool {
	result := make(map[string]bool)
	for _, st := range types {
		if !managedCategories[st.Category] {
			continue
		}
		for _, v := range st.Versions {
			if v.Status != versionStatusActive {
				continue
			}
			base, _, _ := strings.Cut(v.Name, "@")
			result[base] = true
		}
	}
	return result
}

// FormatVersionCheck validates requested runtime + services against live types.
// Returns markdown section with checkmark/warning per type + suggestions.
// Returns "" if types is nil/empty (graceful degradation).
func FormatVersionCheck(runtime string, services []string, types []platform.ServiceStackType) string {
	if len(types) == 0 {
		return ""
	}

	// Build lookup: version name -> true for all active versions.
	activeVersions := make(map[string]bool)
	// Build lookup: base name -> []active version names.
	baseToVersions := make(map[string][]string)
	for _, st := range types {
		if hiddenVersionCategories[st.Category] {
			continue
		}
		for _, v := range st.Versions {
			if v.Status != versionStatusActive {
				continue
			}
			activeVersions[v.Name] = true
			base, _, _ := strings.Cut(v.Name, "@")
			baseToVersions[base] = append(baseToVersions[base], v.Name)
		}
	}

	var sb strings.Builder
	sb.WriteString("## Version Check\n\n")

	// Check runtime (normalize bare names like "valkey" → "valkey@7.2")
	if runtime != "" {
		writeVersionLine(&sb, normalizeVersionInput(runtime, baseToVersions), activeVersions, baseToVersions)
	}
	// Check services
	for _, svc := range services {
		writeVersionLine(&sb, normalizeVersionInput(svc, baseToVersions), activeVersions, baseToVersions)
	}

	return sb.String()
}

// normalizeVersionInput resolves bare names (without @version) to the latest available version.
// E.g., "valkey" → "valkey@7.2" if that's available.
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
		sb.WriteString("- \u2713 `")
		sb.WriteString(requested)
		sb.WriteString("`\n")
		return
	}

	base, _, _ := strings.Cut(requested, "@")
	available := baseToVersions[base]
	if len(available) > 0 {
		sb.WriteString("- \u26a0 `")
		sb.WriteString(requested)
		sb.WriteString("` not found. Available: ")
		sb.WriteString(strings.Join(available, ", "))
		sb.WriteByte('\n')
	} else {
		sb.WriteString("- \u26a0 `")
		sb.WriteString(requested)
		sb.WriteString("` unknown type\n")
	}
}

// ValidateServiceTypes checks import.yml service entries against live types.
// Returns warning strings. Also warns on missing mode for managed services.
// Returns nil if types is nil/empty.
func ValidateServiceTypes(services []map[string]any, types []platform.ServiceStackType) []string {
	if len(types) == 0 {
		return nil
	}

	// Build active version set.
	activeVersions := make(map[string]bool)
	// Build base name -> available versions.
	baseToVersions := make(map[string][]string)
	// Track which base names are STANDARD (managed) category.
	managedBases := make(map[string]bool)
	for _, st := range types {
		if hiddenVersionCategories[st.Category] {
			continue
		}
		for _, v := range st.Versions {
			if v.Status != versionStatusActive {
				continue
			}
			activeVersions[v.Name] = true
			base, _, _ := strings.Cut(v.Name, "@")
			baseToVersions[base] = append(baseToVersions[base], v.Name)
			if st.Category == "STANDARD" {
				managedBases[base] = true
			}
		}
	}

	var warnings []string
	for _, svc := range services {
		hostname, _ := svc["hostname"].(string)
		typeName, _ := svc["type"].(string)
		if typeName == "" {
			continue
		}

		// Check type validity.
		if !activeVersions[typeName] {
			base, _, _ := strings.Cut(typeName, "@")
			if available := baseToVersions[base]; len(available) > 0 {
				warnings = append(warnings, fmt.Sprintf(
					"service %q: type %q not found, available: %s",
					hostname, typeName, strings.Join(available, ", "),
				))
			} else {
				warnings = append(warnings, fmt.Sprintf(
					"service %q: unknown type %q",
					hostname, typeName,
				))
			}
		}

		// Check mode for managed services.
		base, _, _ := strings.Cut(typeName, "@")
		if managedBases[base] {
			if _, hasMode := svc["mode"]; !hasMode {
				warnings = append(warnings, fmt.Sprintf(
					"service %q: managed type %q requires 'mode: NON_HA' or 'mode: HA'",
					hostname, typeName,
				))
			}
		}
	}

	return warnings
}

// FormatServiceStacks formats live service stack types as rich markdown for briefing injection.
// Includes [B] markers for build-capable runtimes and a build-only section for unmatched build types.
// Returns "" if types is nil/empty.
func FormatServiceStacks(types []platform.ServiceStackType) string {
	if len(types) == 0 {
		return ""
	}

	// Collect build version names from BUILD category for cross-reference.
	buildVersions := make(map[string]bool)
	for _, st := range types {
		if st.Category != "BUILD" {
			continue
		}
		for _, v := range st.Versions {
			if v.Status == versionStatusActive {
				buildVersions[v.Name] = true
			}
		}
	}
	matchedBuild := make(map[string]bool)

	// Group visible types by category.
	grouped := make(map[string][]platform.ServiceStackType)
	for _, st := range types {
		if hiddenVersionCategories[st.Category] {
			continue
		}
		grouped[st.Category] = append(grouped[st.Category], st)
	}
	if len(grouped) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Service Stacks (live)\n[B]=also usable as build.base in zerops.yml\n")

	writeCategory := func(cat string, stacks []platform.ServiceStackType) {
		var entries []string
		for _, st := range stacks {
			if entry := formatStackEntry(st, buildVersions, matchedBuild); entry != "" {
				entries = append(entries, entry)
			}
		}
		if len(entries) == 0 {
			return
		}
		sb.WriteByte('\n')
		sb.WriteString(versionCategoryDisplayName(cat))
		sb.WriteString(": ")
		sb.WriteString(strings.Join(entries, " | "))
	}

	for _, cat := range versionCategoryOrder {
		if stacks, ok := grouped[cat]; ok {
			writeCategory(cat, stacks)
		}
	}

	// Remaining categories not in standard order, sorted for determinism.
	var remaining []string
	for cat := range grouped {
		if slices.Contains(versionCategoryOrder, cat) {
			continue
		}
		remaining = append(remaining, cat)
	}
	slices.Sort(remaining)
	for _, cat := range remaining {
		writeCategory(cat, grouped[cat])
	}

	// Show unmatched BUILD versions (e.g., php@8.4 for PHP build base).
	if buildSection := formatUnmatchedBuild(types, matchedBuild); buildSection != "" {
		sb.WriteString(buildSection)
	}

	sb.WriteByte('\n')
	return sb.String()
}

// formatStackEntry returns a compact representation of a service stack type with build markers.
func formatStackEntry(st platform.ServiceStackType, buildVersions, matchedBuild map[string]bool) string {
	var versions []string
	for _, v := range st.Versions {
		if v.Status == versionStatusActive {
			versions = append(versions, v.Name)
		}
	}
	if len(versions) == 0 {
		return ""
	}

	hasBuild := false
	for _, vn := range versions {
		if buildVersions[vn] {
			hasBuild = true
			matchedBuild[vn] = true
		}
	}

	result := compactVersionGroup(versions)
	if hasBuild {
		result += " [B]"
	}
	return result
}

// formatUnmatchedBuild returns BUILD versions that didn't match any visible run type.
func formatUnmatchedBuild(types []platform.ServiceStackType, matchedBuild map[string]bool) string {
	var entries []string
	for _, st := range types {
		if st.Category != "BUILD" || !strings.HasPrefix(st.Name, "zbuild ") {
			continue
		}
		var unmatched []string
		for _, v := range st.Versions {
			if v.Status == versionStatusActive && !matchedBuild[v.Name] {
				unmatched = append(unmatched, v.Name)
			}
		}
		if len(unmatched) > 0 {
			entries = append(entries, compactVersionGroup(unmatched))
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return "\nBuild-only: " + strings.Join(entries, " | ")
}
