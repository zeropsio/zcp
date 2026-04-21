package checks

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// codeBlockFenceRe matches the opening fence of a fenced code block:
// three backticks followed by an optional language tag on the same line.
// Kept in-package so the shim can invoke the predicate without importing
// the tools package.
var codeBlockFenceRe = regexp.MustCompile("(?m)^\\s*```([a-zA-Z0-9+_-]*)")

// nonYamlCodeLanguages is the canonical set of language tags that count
// as "application code adjustment" inside an integration guide. YAML and
// variants are explicitly excluded — every IG already carries a
// zerops.yaml block, and that alone doesn't show what the user needs to
// change in their app code. Shell is accepted because some recipes
// document one-off setup commands (e.g. "run npm install before first
// deploy").
var nonYamlCodeLanguages = map[string]bool{
	"typescript": true,
	"ts":         true,
	"javascript": true,
	"js":         true,
	"jsx":        true,
	"tsx":        true,
	"svelte":     true,
	"vue":        true,
	"python":     true,
	"py":         true,
	"php":        true,
	"go":         true,
	"golang":     true,
	"ruby":       true,
	"rb":         true,
	"rust":       true,
	"rs":         true,
	"java":       true,
	"kotlin":     true,
	"swift":      true,
	"bash":       true,
	"sh":         true,
	"shell":      true,
	"zsh":        true,
	"dockerfile": true,
	"nginx":      true,
}

// CheckIGCodeAdjustment verifies the integration-guide fragment inside
// a per-codebase README contains at least one non-YAML code block —
// real application code a user adjusts to run on Zerops (trust-proxy +
// 0.0.0.0 bind, Vite allowedHosts, Laravel CORS, Django ALLOWED_HOSTS,
// …). v12 audit floor: every published integration guide must show what
// the USER changes in their OWN code, not just the zerops.yaml they
// copy-paste.
//
// Scoped to showcase tier (signaled by isShowcase=true). Minimal recipes
// often have no application-side changes to document; the check returns
// nil for them rather than failing for no-good-reason. When the content
// has no integration-guide fragment at all, the `fragment_*` checks
// already report that surface — this predicate returns nil too to avoid
// pile-on.
//
// Returns nil when out-of-scope or when the checkReadmeFragments gate
// upstream already reports the missing-fragment case. Otherwise returns
// exactly one StepCheck: pass with a language-summary Detail, or fail
// with the full remediation message.
func CheckIGCodeAdjustment(_ context.Context, content string, isShowcase bool) []workflow.StepCheck {
	if !isShowcase {
		return nil
	}
	igContent := extractFragmentContent(content, "integration-guide")
	if igContent == "" {
		return nil
	}
	matches := codeBlockFenceRe.FindAllStringSubmatch(igContent, -1)
	if len(matches) == 0 {
		// checkReadmeFragments already reports "no yaml block"; don't
		// pile on.
		return nil
	}
	// Each match opens a fence. Pairs alternate (open/close); the
	// language tag only appears on the opener. Count unique non-empty
	// language tags that fall into the accepted non-YAML set.
	nonYaml := 0
	var seenLangs []string
	for _, m := range matches {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		if lang == "" {
			continue // closing fence or untagged fence
		}
		if nonYamlCodeLanguages[lang] {
			nonYaml++
			seenLangs = append(seenLangs, lang)
		}
	}
	if nonYaml == 0 {
		return []workflow.StepCheck{{
			Name:   "integration_guide_code_adjustment",
			Status: StatusFail,
			Detail: "integration guide has zerops.yaml only — add at least one non-YAML code block showing an actual application-code change a user must make to run on Zerops. Examples: Express trust-proxy + 0.0.0.0 bind (typescript), Vite allowedHosts (js), Laravel CORS config (php), Django ALLOWED_HOSTS (py). The integration guide must document what the USER changes in their OWN code, not just the zerops.yaml they copy-paste.",
		}}
	}
	return []workflow.StepCheck{{
		Name:   "integration_guide_code_adjustment",
		Status: StatusPass,
		Detail: fmt.Sprintf("%d non-YAML code block(s): %s", nonYaml, strings.Join(uniqueStrings(seenLangs), ", ")),
	}}
}

// extractFragmentContent extracts content between the ZEROPS_EXTRACT_START
// and _END markers for the named fragment, or returns empty when either
// marker is absent. Migrated intact from internal/tools so the predicate
// is self-contained.
func extractFragmentContent(content, name string) string {
	startMarker := fmt.Sprintf("#ZEROPS_EXTRACT_START:%s#", name)
	endMarker := fmt.Sprintf("#ZEROPS_EXTRACT_END:%s#", name)

	startIdx := strings.Index(content, startMarker)
	if startIdx < 0 {
		return ""
	}
	afterStart := startIdx + len(startMarker)
	lineEnd := strings.Index(content[afterStart:], "\n")
	if lineEnd < 0 {
		return ""
	}
	contentStart := afterStart + lineEnd + 1

	endIdx := strings.Index(content[contentStart:], endMarker)
	if endIdx < 0 {
		return ""
	}
	extractEnd := contentStart + endIdx
	for extractEnd > contentStart && content[extractEnd-1] != '\n' {
		extractEnd--
	}
	return strings.TrimSpace(content[contentStart:extractEnd])
}

// uniqueStrings returns the slice with duplicates removed, preserving
// first-occurrence order.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
