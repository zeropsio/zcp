package recipe

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	rf1HeadingTitle        = "## Recipe features"
	pd1HeadingTitle        = "## Production vs. Development"
	understandHeadingTitle = "## Understand Zerops Core Concepts"
)

func gateForbidRecipeLevelSectionsOnAppsRepos(ctx GateContext) []Violation {
	return forbidRecipeLevelSectionsOnAppsRepos(ctx.Plan)
}

func forbidRecipeLevelSectionsOnAppsRepos(plan *Plan) []Violation {
	if plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		body, _, err := AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			continue
		}
		readmePath := filepath.Join(cb.SourceRoot, "README.md")
		for _, forbidden := range []struct {
			heading string
			code    string
		}{
			{heading: rf1HeadingTitle, code: "apps-repo-has-rf1"},
			{heading: pd1HeadingTitle, code: "apps-repo-has-pd1"},
			{heading: understandHeadingTitle, code: "apps-repo-has-understand"},
		} {
			if !containsHeading(body, forbidden.heading) {
				continue
			}
			out = append(out, Violation{
				Code:     forbidden.code,
				Path:     readmePath,
				Severity: SeverityBlocking,
				Message: fmt.Sprintf(
					"apps-repo (%s) README contains forbidden recipe-level H2 %q. Apps-repo READMEs are codebase-integration surfaces; recipe overview, HA-migration, and concept-bridge sections do not belong on any codebase README. Remove the section from `codebase/%s/integration-guide` (or the un-slotted fragment that authored it) and rerun `complete-phase phase=codebase-content`.",
					cb.Hostname, forbidden.heading, cb.Hostname,
				),
			})
		}
	}
	return out
}

// containsHeading reports whether body has a markdown H2 line whose
// title matches want (case-insensitive). Matches `^## <title>` only;
// does not match deeper headings or inline mentions.
//
// Skips lines inside fenced code blocks (```...```) so an IG step that
// quotes the literal heading text inside a code fence doesn't trip the
// heading scan. Pinned by
// TestContainsHeading_HeadingInsideCodeFence_NotMatched.
func containsHeading(body, want string) bool {
	wantLower := strings.ToLower(want)
	inFence := false
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(strings.TrimLeft(trimmed, " \t"), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.EqualFold(trimmed, want) {
			return true
		}
		if lower := strings.ToLower(trimmed); strings.HasPrefix(lower, wantLower) {
			return true
		}
	}
	return false
}
