package eval

import "strings"

// findYAMLBlocksInSections extracts YAML code blocks from H2 sections matching sectionSubstr.
func findYAMLBlocksInSections(content, sectionSubstr string) []string {
	sections := parseRecipeSections(content)
	var blocks []string
	for title, body := range sections {
		if strings.Contains(title, sectionSubstr) {
			blocks = append(blocks, extractYAMLBlocks(body)...)
		}
	}
	return blocks
}

// parseRecipeSections extracts H2 sections from markdown as title→content map.
func parseRecipeSections(content string) map[string]string {
	sections := make(map[string]string)
	var currentTitle string
	var currentContent strings.Builder
	inCodeBlock := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			if currentTitle != "" {
				currentContent.WriteString(line + "\n")
			}
			continue
		}

		if !inCodeBlock && strings.HasPrefix(trimmed, "## ") {
			if currentTitle != "" {
				sections[currentTitle] = currentContent.String()
			}
			currentTitle = strings.TrimPrefix(trimmed, "## ")
			currentContent.Reset()
			continue
		}

		if currentTitle != "" {
			currentContent.WriteString(line + "\n")
		}
	}

	if currentTitle != "" {
		sections[currentTitle] = currentContent.String()
	}

	return sections
}

// extractYAMLBlocks extracts content from ```yaml ... ``` fenced code blocks.
func extractYAMLBlocks(section string) []string {
	var blocks []string
	var current strings.Builder
	inBlock := false

	for line := range strings.SplitSeq(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock && (trimmed == "```yaml" || trimmed == "```yml") {
			inBlock = true
			continue
		}
		if inBlock && trimmed == "```" {
			blocks = append(blocks, current.String())
			current.Reset()
			inBlock = false
			continue
		}
		if inBlock {
			current.WriteString(line + "\n")
		}
	}

	return blocks
}
