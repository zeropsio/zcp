package knowledge

import (
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

// managedCategories are API categories that represent managed (non-runtime) services.
var managedCategories = map[string]bool{
	"STANDARD":       true,
	"SHARED_STORAGE": true,
	"OBJECT_STORAGE": true,
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

// ValidateServiceTypes, ValidateProjectFields, and makeStringSet were deleted
// as part of plans/api-validation-plumbing.md W6. The Zerops API validates
// every field they used to check, and structured `apiMeta` on the error
// surface (see platform/errors.go APIMetaItem) carries the failing field
// name + reason to the LLM — no client-side duplicate needed.
