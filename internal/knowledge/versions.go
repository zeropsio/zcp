package knowledge

import (
	"fmt"
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

	// Check runtime
	if runtime != "" {
		writeVersionLine(&sb, runtime, activeVersions, baseToVersions)
	}
	// Check services
	for _, svc := range services {
		writeVersionLine(&sb, svc, activeVersions, baseToVersions)
	}

	return sb.String()
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
