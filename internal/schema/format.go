package schema

import (
	"fmt"
	"sort"
	"strings"
)

// FormatZeropsYmlForLLM returns a compact, LLM-friendly representation of the zerops.yaml schema.
// Includes field descriptions, types, and valid enum values.
func FormatZeropsYmlForLLM(s *ZeropsYmlSchema) string {
	if s == nil || s.Raw == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## zerops.yaml Schema (live)\n\n")

	setup := navigatePath(s.Raw, "properties", "zerops", "items", "properties")
	if setup == nil {
		return ""
	}

	// Build section.
	if build := navigateMap(setup, "build"); build != nil {
		sb.WriteString("### build\n")
		writeFieldsFromProps(&sb, navigateMap(build, "properties"), []fieldOverride{
			{name: "base", extra: "Valid: " + compactEnumList(s.BuildBases)},
		})
		if req := extractRequired(build); len(req) > 0 {
			sb.WriteString(fmt.Sprintf("Required: %s\n", strings.Join(req, ", ")))
		}
		sb.WriteByte('\n')
	}

	// Deploy section.
	if deploy := navigateMap(setup, "deploy"); deploy != nil {
		sb.WriteString("### deploy\n")
		writeFieldsFromProps(&sb, navigateMap(deploy, "properties"), nil)
		sb.WriteByte('\n')
	}

	// Run section.
	if run := navigateMap(setup, "run"); run != nil {
		sb.WriteString("### run\n")
		writeFieldsFromProps(&sb, navigateMap(run, "properties"), []fieldOverride{
			{name: "base", extra: "Valid: " + compactEnumList(s.RunBases)},
		})
		sb.WriteByte('\n')
	}

	return sb.String()
}

// FormatImportYmlForLLM returns a compact, LLM-friendly representation of the import.yaml schema.
func FormatImportYmlForLLM(s *ImportYmlSchema) string {
	if s == nil || s.Raw == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## import.yaml Schema (live)\n\n")

	// Project section.
	if proj := navigatePath(s.Raw, "properties", "project", "properties"); proj != nil {
		sb.WriteString("### project\n")
		writeFieldsFromProps(&sb, proj, nil)
		sb.WriteByte('\n')
	}

	// Services section.
	svcProps := navigatePath(s.Raw, "properties", "services", "items", "properties")
	if svcProps != nil {
		sb.WriteString("### services[]\n")
		writeFieldsFromProps(&sb, svcProps, []fieldOverride{
			{name: "type", extra: "Valid: " + compactEnumList(s.ServiceTypes)},
		})
		if req := extractRequired(navigatePath(s.Raw, "properties", "services", "items")); len(req) > 0 {
			sb.WriteString(fmt.Sprintf("Required: %s\n", strings.Join(req, ", ")))
		}
		sb.WriteByte('\n')

		// verticalAutoscaling sub-fields.
		if va := navigateMap(svcProps, "verticalAutoscaling"); va != nil {
			sb.WriteString("### services[].verticalAutoscaling\n")
			writeFieldsFromProps(&sb, navigateMap(va, "properties"), nil)
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// FormatBothForLLM returns combined formatted schemas.
func FormatBothForLLM(schemas *Schemas) string {
	if schemas == nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if s := FormatZeropsYmlForLLM(schemas.ZeropsYml); s != "" {
		parts = append(parts, s)
	}
	if s := FormatImportYmlForLLM(schemas.ImportYml); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n---\n\n")
}

// fieldOverride provides extra info for specific fields in formatted output.
type fieldOverride struct {
	name  string
	extra string
}

// writeFieldsFromProps writes formatted field lines from a JSON schema properties map.
func writeFieldsFromProps(sb *strings.Builder, props map[string]any, overrides []fieldOverride) {
	if props == nil {
		return
	}
	overrideMap := make(map[string]string, len(overrides))
	for _, o := range overrides {
		overrideMap[o.name] = o.extra
	}

	// Sort keys for deterministic output (stable LLM prompts, testable output).
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, name := range keys {
		m, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		typStr := schemaTypeString(m)
		desc, _ := m["description"].(string)

		sb.WriteString("- **")
		sb.WriteString(name)
		sb.WriteString("**")
		if typStr != "" {
			sb.WriteString(": ")
			sb.WriteString(typStr)
		}

		if extra, ok := overrideMap[name]; ok {
			sb.WriteString(" — ")
			sb.WriteString(extra)
		} else if desc != "" {
			// Truncate long descriptions.
			if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			sb.WriteString(" — ")
			sb.WriteString(desc)
		}
		sb.WriteByte('\n')
	}
}

// schemaTypeString returns a human-readable type from a JSON schema node.
func schemaTypeString(node map[string]any) string {
	if enum := toStringSlice(node["enum"]); len(enum) > 0 {
		if len(enum) <= 5 {
			return strings.Join(enum, " | ")
		}
		return strings.Join(enum[:3], " | ") + " | ..."
	}
	if t, ok := node["type"].(string); ok {
		if t == "array" {
			if items, ok := node["items"].(map[string]any); ok {
				if it, ok := items["type"].(string); ok {
					return it + "[]"
				}
			}
			return "array"
		}
		return t
	}
	if _, ok := node["oneOf"]; ok {
		return "string | string[]"
	}
	return ""
}

// extractRequired extracts the "required" field array from a schema node.
func extractRequired(node map[string]any) []string {
	if node == nil {
		return nil
	}
	return toStringSlice(node["required"])
}

// navigateMap gets a key from a map and returns it as map[string]any.
func navigateMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

// compactEnumList returns a compact representation of enum values.
// Groups by base name: "php@{8.1,8.3,8.4,8.5}" instead of listing each.
func compactEnumList(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}

	// Group by base name.
	type group struct {
		base     string
		versions []string
	}
	var groups []group
	groupIdx := make(map[string]int)

	for _, v := range values {
		base, version, hasVersion := strings.Cut(v, "@")
		if !hasVersion {
			// Standalone value (e.g., "static", "runtime").
			groups = append(groups, group{base: v})
			continue
		}
		if idx, ok := groupIdx[base]; ok {
			groups[idx].versions = append(groups[idx].versions, version)
		} else {
			groupIdx[base] = len(groups)
			groups = append(groups, group{base: base, versions: []string{version}})
		}
	}

	// Format.
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		switch {
		case len(g.versions) == 0:
			parts = append(parts, g.base)
		case len(g.versions) == 1:
			parts = append(parts, g.base+"@"+g.versions[0])
		default:
			parts = append(parts, g.base+"@{"+strings.Join(g.versions, ",")+"}")
		}
	}
	return strings.Join(parts, ", ")
}
