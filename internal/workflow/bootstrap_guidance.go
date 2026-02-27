package workflow

import (
	"strings"

	"github.com/zeropsio/zcp/internal/content"
)

// stepSectionMap maps bootstrap step names to section heading markers in bootstrap.md.
// Each step maps to one or more section headings to extract.
var stepSectionMap = map[string][]string{
	"detect":          {"Step 0"},
	"plan":            {"Step 1"},
	"load-knowledge":  {"Step 2", "Step 3"},
	"generate-import": {"Step 4"},
	"import-services": {"Step 5", "Step 6", "Step 7"},
	"mount-dev":       {"Standard mode"},
	"discover-envs":   {"Env var discovery protocol"},
	"deploy":          {"Phase 2", "Service Bootstrap Agent Prompt"},
	"verify":          {"Verification Protocol", "Verification iteration loop"},
	"report":          {"After completion"},
}

// ResolveGuidance extracts relevant sections from the embedded bootstrap.md for a step.
// Returns empty string for unknown steps or if bootstrap.md cannot be loaded.
func ResolveGuidance(step string) string {
	markers, ok := stepSectionMap[step]
	if !ok {
		return ""
	}

	md, err := content.GetWorkflow("bootstrap")
	if err != nil {
		return ""
	}

	var sections []string
	for _, marker := range markers {
		section := extractSection(md, marker)
		if section != "" {
			sections = append(sections, section)
		}
	}
	return strings.Join(sections, "\n\n---\n\n")
}

// extractSection finds a markdown section by heading marker and returns its content.
// It finds a heading containing the marker and returns everything until the next heading
// at the same or higher level.
func extractSection(md, marker string) string {
	lines := strings.Split(md, "\n")
	start := -1
	level := 0

	for i, line := range lines {
		if isHeading(line) && strings.Contains(line, marker) {
			start = i
			level = headingLevel(line)
			break
		}
	}

	if start < 0 {
		return ""
	}

	// Collect lines until we hit a heading at the same or higher level.
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isHeading(lines[i]) && headingLevel(lines[i]) <= level {
			end = i
			break
		}
	}

	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

// isHeading checks if a line is a markdown heading (starts with #).
func isHeading(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// headingLevel returns the heading level (number of # characters).
func headingLevel(line string) int {
	trimmed := strings.TrimSpace(line)
	level := 0
	for _, c := range trimmed {
		if c == '#' {
			level++
		} else {
			break
		}
	}
	return level
}
