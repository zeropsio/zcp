package sync

import (
	"regexp"
	"strings"
)

// ExtractKnowledgeBase extracts knowledge-base content from a recipe .md file.
// Skips frontmatter, skips H1, stops at integration-guide boundary, demotes H2→H3.
// Returns empty string if no knowledge-base content found.
func ExtractKnowledgeBase(recipeContent string) string {
	lines := strings.Split(recipeContent, "\n")

	var out []string
	inFrontmatter := false
	inCodeBlock := false

	for i, line := range lines {
		if i == 0 && line == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if line == "---" {
				inFrontmatter = false
			}
			continue
		}

		// Track code blocks — headings inside them are not boundaries
		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
		}

		// Stop at integration-guide boundary or service definitions (only outside code blocks)
		if !inCodeBlock {
			if isIntegrationGuideHeading(line) || strings.HasPrefix(line, "## Service Definitions") {
				break
			}
		}

		// Skip H1 lines
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			continue
		}

		// Demote H2 → H3 (only real headings, not inside code blocks)
		if !inCodeBlock && strings.HasPrefix(line, "## ") {
			line = "#" + line
		}

		out = append(out, line)
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// InjectFragment replaces content between ZEROPS_EXTRACT markers in a README.
// If markers don't exist, appends them at the end.
func InjectFragment(readme, fragmentName, fragment string) string {
	startMarker := "<!-- #ZEROPS_EXTRACT_START:" + fragmentName + "# -->"
	endMarker := "<!-- #ZEROPS_EXTRACT_END:" + fragmentName + "# -->"

	if strings.Contains(readme, startMarker) {
		// Replace between markers
		lines := strings.Split(readme, "\n")
		var out []string
		skip := false
		for _, line := range lines {
			if strings.Contains(line, "ZEROPS_EXTRACT_START:"+fragmentName) {
				out = append(out, line)
				out = append(out, fragment)
				skip = true
				continue
			}
			if strings.Contains(line, "ZEROPS_EXTRACT_END:"+fragmentName) {
				skip = false
				out = append(out, line)
				continue
			}
			if !skip {
				out = append(out, line)
			}
		}
		return strings.Join(out, "\n")
	}

	// Append markers at end
	return readme + "\n\n" + startMarker + "\n" + fragment + "\n" + endMarker
}

// ExtractZeropsYAML extracts the YAML code block from "## zerops.yml" or
// "## zerops.yaml" section.
func ExtractZeropsYAML(recipeContent string) string {
	lines := strings.Split(recipeContent, "\n")

	found := false
	inYAML := false
	var out []string

	for _, line := range lines {
		if strings.HasPrefix(line, "## zerops.yml") ||
			strings.HasPrefix(line, "## zerops.yaml") {
			found = true
			continue
		}
		if !found {
			continue
		}
		if found && strings.HasPrefix(line, "## ") {
			break // next section
		}
		if strings.HasPrefix(line, "```yaml") {
			inYAML = true
			continue
		}
		if inYAML && strings.HasPrefix(line, "```") {
			break
		}
		if inYAML {
			out = append(out, line)
		}
	}

	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n")
}

// ExtractIntegrationGuide extracts the integration-guide section from a recipe .md.
// This is everything from the first integration-guide heading (## zerops.yml,
// ## 1. Adding zerops.yaml, etc.) up to "## Service Definitions", demoted H2→H3.
// Returns empty string if no such section exists.
func ExtractIntegrationGuide(recipeContent string) string {
	lines := strings.Split(recipeContent, "\n")

	inFrontmatter := false
	inCodeBlock := false
	found := false
	var out []string

	for i, line := range lines {
		if i == 0 && line == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if line == "---" {
				inFrontmatter = false
			}
			continue
		}

		if strings.HasPrefix(line, "```") {
			inCodeBlock = !inCodeBlock
		}

		// Start capturing at the integration-guide boundary (outside code blocks)
		if !found {
			if !inCodeBlock && isIntegrationGuideHeading(line) {
				found = true
				out = append(out, "#"+line) // demote H2 → H3
			}
			continue
		}

		// Stop at ## Service Definitions (outside code blocks)
		if !inCodeBlock && strings.HasPrefix(line, "## Service Definitions") {
			break
		}

		// Demote H2 → H3 (only real headings)
		if !inCodeBlock && strings.HasPrefix(line, "## ") {
			line = "#" + line
		}

		out = append(out, line)
	}

	if !found {
		return ""
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

// isIntegrationGuideHeading returns true if the line marks the start of an
// integration-guide section. Matches:
//   - ## zerops.yml / ## zerops.yaml
//   - ## 1. Adding `zerops.yaml` (numbered heading from API integration-guide)
//   - Any ## N. heading (numbered integration steps)
func isIntegrationGuideHeading(line string) bool {
	if strings.HasPrefix(line, "## zerops.yml") || strings.HasPrefix(line, "## zerops.yaml") {
		return true
	}
	// Numbered heading: "## 1. ..."
	if strings.HasPrefix(line, "## ") && len(line) > 4 && line[3] >= '0' && line[3] <= '9' && strings.Contains(line[:min(8, len(line))], ".") {
		return true
	}
	return false
}

// ExtractIntro extracts the frontmatter description as intro text.
func ExtractIntro(recipeContent string) string {
	return extractFrontmatterField(recipeContent, "description")
}

// ExtractRepo extracts the frontmatter repo URL.
// Written during pull from the API's gitRepo field — the authoritative source
// for where this recipe's app repo lives. Push reads this instead of guessing.
func ExtractRepo(recipeContent string) string {
	return extractFrontmatterField(recipeContent, "repo")
}

func extractFrontmatterField(content, field string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return ""
	}
	prefix := field + ":"
	for _, line := range lines[1:] {
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, prefix) {
			val := strings.TrimPrefix(line, prefix)
			val = strings.TrimSpace(val)
			val = strings.Trim(val, `"'`)
			return val
		}
	}
	return ""
}

// zeropsURIPattern matches zerops:// URIs with curly-brace suffixes.
var zeropsURIPattern = regexp.MustCompile(`(zerops://[^ ]*\{[^}]+\})`)

// ConvertGuideToMDX converts a guide .md to .mdx format.
// If existingMDX is non-empty, preserves its frontmatter; otherwise generates new.
func ConvertGuideToMDX(guideContent string, existingMDX string) string {
	var sb strings.Builder

	if existingMDX != "" {
		// Preserve existing frontmatter
		fm := extractFrontmatterBlock(existingMDX)
		if fm != "" {
			sb.WriteString(fm)
			sb.WriteString("\n\n")
		}
	} else {
		// Generate new frontmatter from guide content
		title := extractH1(guideContent)
		if title == "" {
			title = "Untitled"
		}
		desc := extractTLDR(guideContent)
		if desc == "" {
			desc = "Guide: " + title
		}
		desc = strings.ReplaceAll(desc, `"`, `\"`)
		sb.WriteString("---\n")
		sb.WriteString("title: " + title + "\n")
		sb.WriteString(`description: "` + desc + `"` + "\n")
		sb.WriteString("---\n\n")
	}

	// Strip H1 and convert body
	body := stripH1(guideContent)

	// Wrap zerops:// URIs in backticks (for MDX imports)
	inCode := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
		}
		if !inCode {
			line = zeropsURIPattern.ReplaceAllString(line, "`$1`")
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// ConvertMDXToGuide converts a docs .mdx file to guide .md format.
// Strips frontmatter, removes import lines, unwraps zerops:// backticks.
func ConvertMDXToGuide(mdxContent string) string {
	lines := strings.Split(mdxContent, "\n")

	// Extract title from frontmatter
	title := ""
	inFM := false
	fmDone := false
	var bodyLines []string

	for i, line := range lines {
		if i == 0 && line == "---" {
			inFM = true
			continue
		}
		if inFM {
			if line == "---" {
				inFM = false
				fmDone = true
				continue
			}
			if strings.HasPrefix(line, "title: ") {
				title = strings.TrimPrefix(line, "title: ")
				title = strings.Trim(title, `"'`)
			}
			continue
		}
		_ = fmDone
		bodyLines = append(bodyLines, line)
	}

	var out strings.Builder
	if title != "" {
		out.WriteString("# " + title + "\n\n")
	}

	inCode := false
	skipBlanks := true
	for _, line := range bodyLines {
		if strings.HasPrefix(line, "```") {
			inCode = !inCode
		}

		// Skip import lines outside code blocks
		if !inCode && strings.HasPrefix(line, "import ") {
			continue
		}

		// Skip leading blank lines after frontmatter
		if skipBlanks && line == "" {
			continue
		}
		skipBlanks = false

		// Unwrap zerops:// backticks
		if !inCode {
			line = unwrapZeropsBackticks(line)
		}

		out.WriteString(line)
		out.WriteString("\n")
	}

	return strings.TrimRight(out.String(), "\n") + "\n"
}

// extractFrontmatterBlock returns the full frontmatter block including delimiters.
func extractFrontmatterBlock(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	return ""
}

func extractH1(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

func extractTLDR(content string) string {
	lines := strings.Split(content, "\n")
	inTLDR := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## TL;DR") {
			inTLDR = true
			continue
		}
		if inTLDR && strings.HasPrefix(line, "## ") {
			break
		}
		if inTLDR && line != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func stripH1(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## ") {
			continue
		}
		out = append(out, line)
	}
	// Trim leading blank lines
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	return strings.Join(out, "\n")
}

// unwrapZeropsBackticks removes backtick wrapping around zerops:// URIs.
func unwrapZeropsBackticks(line string) string {
	// Match `zerops://...` and unwrap
	re := regexp.MustCompile("`(zerops://[^`]+)`")
	return re.ReplaceAllString(line, "$1")
}
