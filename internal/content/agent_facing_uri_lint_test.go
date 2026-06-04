package content

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// bareZeropsURIRe matches an inline-code span that OPENS with a bare
// `zerops://` URI — the resource-reader bait. The converged tool-call form
// `zerops_knowledge uri="zerops://..."` opens the span with `zerops_knowledge`
// (the 8th char is `_`, not `:`), so it never matches. The recipe-atom synonym
// form `zerops_workflow action=dispatch-brief-atom atomId=...` carries no
// `zerops://` at all. Thus the pattern flags exactly the bait and nothing else.
var bareZeropsURIRe = regexp.MustCompile("`zerops://")

// agentFacingMarkdownDirs are the committed markdown trees an agent reads
// verbatim: atoms synthesized into workflow responses, theme + recipe briefs
// fetched on demand, and example/workflow content. Gitignored/synced guides
// (internal/knowledge/guides) are deliberately NOT here — the scan walks
// git-tracked files only (gitTrackedFiles), so the upstream-owned guide
// cross-refs converge via the zeropsio/docs push, not this gate.
var agentFacingMarkdownDirs = []string{
	"internal/knowledge/themes/",
	"internal/content/atoms/",
	"internal/content/examples/",
	"internal/content/workflows/",
	"internal/recipe/content/",
}

// TestNoBareZeropsURIInAgentContent pins the single-format convergence
// (plans/converge-knowledge-retrieval-format-2026-06-04.md): agent-facing
// markdown must present a zerops:// document reference ONLY through the
// canonical tool-call form `zerops_knowledge uri="zerops://..."`, never as a
// bare backticked `zerops://...` an agent could feed to a generic MCP resource
// reader. ZCP is tools-only — the MCP resources protocol surface was removed,
// so a bare URI is dead bait (TestServer_DoesNotAdvertiseResourcesCapability
// pins the server side; this pins the content side). Single-owner tell==check.
func TestNoBareZeropsURIInAgentContent(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	files := gitTrackedFiles(t, repoRoot)

	type violation struct {
		File    string
		Line    int
		Snippet string
	}
	var violations []violation

	for _, rel := range files {
		if !strings.HasSuffix(rel, ".md") || !underAnyDir(rel, agentFacingMarkdownDirs) {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil || !looksLikeText(body) {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			if bareZeropsURIRe.MatchString(scanner.Text()) {
				violations = append(violations, violation{rel, lineNo, trim(strings.TrimSpace(scanner.Text()), 140)})
			}
		}
	}

	if len(violations) == 0 {
		return
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		return violations[i].Line < violations[j].Line
	})
	var msg strings.Builder
	msg.WriteString("bare backticked `zerops://` in agent-facing markdown — convert to the tool-call form `zerops_knowledge uri=\"zerops://...\"` (or, for a recipe-atom, `zerops_workflow action=dispatch-brief-atom atomId=...`) so no agent feeds a bare URI to an MCP resource reader; ZCP is tools-only:\n")
	for _, v := range violations {
		msg.WriteString("  " + v.File + ":" + itoa(v.Line) + "\n      " + v.Snippet + "\n")
	}
	t.Error(msg.String())
}

func underAnyDir(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}
