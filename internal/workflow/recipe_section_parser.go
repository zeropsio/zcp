package workflow

import "strings"

// extractSection finds a <section name="{name}">...</section> block inside a
// recipe.md document and returns its content, trimmed. Recipe-local utility:
// bootstrap and develop no longer compose from sections, so this is only
// invoked by recipe_guidance.go and recipe_topic_registry.go.
func extractSection(md, name string) string {
	openTag := "<section name=\"" + name + "\">"
	closeTag := "</section>"
	start := strings.Index(md, openTag)
	if start < 0 {
		return ""
	}
	start += len(openTag)
	end := strings.Index(md[start:], closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(md[start : start+end])
}
