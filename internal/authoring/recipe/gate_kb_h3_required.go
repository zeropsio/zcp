package recipe

import (
	"fmt"
	"path/filepath"
	"strings"
)

func gateRequireH3InKnowledgeBaseFragment(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		fragID := fmt.Sprintf("codebase/%s/knowledge-base", cb.Hostname)
		body, ok := ctx.Plan.Fragments[fragID]
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		if !startsWithH3Heading(body) {
			out = append(out, Violation{
				Code:     "kb-fragment-not-h3-rooted",
				Path:     filepath.Join(cb.SourceRoot, "README.md"),
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"codebase/%s knowledge-base fragment body must start with an H3 heading (`### <Topic>`) per derived_rules.md §KB1. The Zerops dashboard's KB extractor expects H3-rooted content; H2 (`## ...`) and flat-bullet-only forms are silently dropped on the recipe page render. Re-author via `record-fragment fragmentId=%s mode=replace` with a body that opens `### Gotchas` (or `### <Topic>`) followed by paragraphs / bullets.",
					cb.Hostname, fragID,
				),
			})
		}
	}
	return out
}

func startsWithH3Heading(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return strings.HasPrefix(trimmed, "### ")
	}
	return false
}
