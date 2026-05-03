package eval

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// retrospectivePromptsFS holds the closed enum of post-hoc retrospective
// prompts used by RunBehavioralScenario. Each file is named <style>.md and
// is loaded by promptStyle from scenario frontmatter.
//
//go:embed retrospective_prompts/*.md
var retrospectivePromptsFS embed.FS

// LoadRetrospectivePrompt returns the body of the named prompt style. Style
// names map to files: "briefing-future-agent" → retrospective_prompts/briefing-future-agent.md.
func LoadRetrospectivePrompt(style string) (string, error) {
	if style == "" {
		return "", fmt.Errorf("retrospective prompt style is empty")
	}
	if strings.ContainsAny(style, "/\\.") {
		return "", fmt.Errorf("invalid prompt style %q (no slashes or dots)", style)
	}
	data, err := retrospectivePromptsFS.ReadFile(filepath.Join("retrospective_prompts", style+".md"))
	if err != nil {
		return "", fmt.Errorf("retrospective prompt %q not found (available: %s)", style, strings.Join(availableRetrospectiveStyles(), ", "))
	}
	return string(data), nil
}

// availableRetrospectiveStyles returns the sorted list of embedded prompt
// styles, used by error messages and the `behavioral list` subcommand for
// helpful hints.
func availableRetrospectiveStyles() []string {
	entries, err := fs.ReadDir(retrospectivePromptsFS, "retrospective_prompts")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if base, ok := strings.CutSuffix(e.Name(), ".md"); ok {
			out = append(out, base)
		}
	}
	return out
}
