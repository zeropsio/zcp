package knowledge

import (
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

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
