package content

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The atom lint's dev-server-call-missing-hostname rule reads atoms only; a
// call zcp itself writes into a reply, an error or a knowledge theme reaches
// the agent the same way. Every such call names hostname, under the same
// rule's pattern. A text that tells the agent to run an action names hostname
// too, never the bare action an agent would call without one (a Go comment
// reaches no agent).
func TestEveryAgentFacingDevServerCallInGoAndKnowledgeNamesHostname(t *testing.T) {
	t.Parallel()
	var rule *atomLintRule
	for i := range atomLintRules {
		if atomLintRules[i].name == "dev-server-call-missing-hostname" {
			rule = &atomLintRules[i]
		}
	}
	if rule == nil {
		t.Fatal("atom lint rule dev-server-call-missing-hostname is gone")
	}
	repoRoot := findRepoRoot(t)
	var violations []string
	for _, rel := range gitTrackedFiles(t, repoRoot) {
		goSource := strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") &&
			(strings.HasPrefix(rel, "internal/") || strings.HasPrefix(rel, "cmd/"))
		knowledge := strings.HasSuffix(rel, ".md") && underAnyDir(rel, agentFacingMarkdownDirs)
		if !goSource && !knowledge {
			continue
		}
		body, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for lineNo := 1; scanner.Scan(); lineNo++ {
			line := scanner.Text()
			comment := goSource && strings.HasPrefix(strings.TrimSpace(line), "//")
			if rule.pattern.MatchString(line) || (!comment && bareDevServerAction.MatchString(line)) {
				violations = append(violations, rel+":"+itoa(lineNo)+": "+trim(strings.TrimSpace(line), 160))
			}
		}
	}
	if len(violations) > 0 {
		t.Errorf("zerops_dev_server calls without hostname right after the action (refused as pasted):\n  %s", strings.Join(violations, "\n  "))
	}
}

// bareDevServerAction is a zerops_dev_server action, quoted or not, that no
// hostname follows.
var bareDevServerAction = regexp.MustCompile(`zerops_dev_server action=\\?"?[a-z]+\\?"?($|[^a-z" \\]| [^h])`)
