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

// neverShippedLaunchVocab are identifiers from a launch-production design that
// was DESIGNED (Phase-6b, plans/workflow-family-architecture-2026-05-14.md) but
// NEVER SHIPPED: a project-mode + cicd-method-prompt handshake. Three flow-eval
// scenarios pinned the agent against it — status awaiting-project-mode-choice,
// inputs LaunchProjectMode/CICDMethod, atoms launch-generate-prod-token /
// launch-cicd-actions-handoff — none of which exist in code or the atom corpus,
// so running them graded the agent against an interaction the shipped handler
// cannot produce (2026-06-16 finalization-audit finding B2). The shipped flow
// starts at scope-prompt, selects new vs existing by INPUT PRESENCE (launchKey
// vs ExistingProjectID+ExistingProdToken), and DERIVES the delivery family from
// the source BuildIntegration — there is no mode/method prompt.
var neverShippedLaunchVocab = []struct {
	pattern *regexp.Regexp
	label   string
}{
	{regexp.MustCompile(`\bawaiting-project-mode-choice\b`), "status awaiting-project-mode-choice (never shipped — the launch state machine starts at scope-prompt; new-vs-existing is selected by input presence)"},
	{regexp.MustCompile(`\bLaunchProjectMode\b`), "input LaunchProjectMode (never shipped — no such WorkflowInput; the path is keyed by launchKey vs ExistingProjectID+ExistingProdToken presence)"},
	{regexp.MustCompile(`\bCICDMethod\b`), "input CICDMethod (never shipped — the delivery family is DERIVED from the source BuildIntegration, not chosen via a prompt)"},
	{regexp.MustCompile(`\blaunch-generate-prod-token\b`), "atom launch-generate-prod-token (never shipped — absent from the atom corpus)"},
	{regexp.MustCompile(`\blaunch-cicd-actions-handoff\b`), "atom launch-cicd-actions-handoff (never shipped — absent from the atom corpus)"},
}

// TestNoNeverShippedLaunchVocabInEvalScenarios keeps the flow-eval behavioral
// scenarios honest: they are graded against the SHIPPED launch-production
// handler, so a scenario asserting the never-shipped Phase-6b handshake is a
// false-friction trap that burns a 14-17 min eval cycle with no code fix.
//
// Scoped to eval/behavioral/scenarios{,-local}/ deliberately — the plans/
// design doc that records the Phase-6b proposal legitimately names these
// identifiers as never-shipped roadmap, so the whole-repo drift gate
// (TestNoRetiredVocabAcrossRepo) must NOT claim them; only the eval scenarios,
// which the harness runs against the real handler, must stay clean.
func TestNoNeverShippedLaunchVocabInEvalScenarios(t *testing.T) {
	repoRoot := findRepoRoot(t)
	dirs := []string{
		filepath.Join(repoRoot, "eval", "behavioral", "scenarios"),
		filepath.Join(repoRoot, "eval", "behavioral", "scenarios-local"),
	}

	type violation struct {
		file    string
		line    int
		label   string
		snippet string
	}
	var violations []violation

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// scenarios-local may not exist on every checkout — not a failure.
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			full := filepath.Join(dir, e.Name())
			body, readErr := os.ReadFile(full)
			if readErr != nil {
				continue
			}
			scanner := bufio.NewScanner(bytes.NewReader(body))
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			lineNo := 0
			for scanner.Scan() {
				lineNo++
				line := scanner.Text()
				for _, v := range neverShippedLaunchVocab {
					if v.pattern.MatchString(line) {
						rel, relErr := filepath.Rel(repoRoot, full)
						if relErr != nil {
							rel = full
						}
						snippet := strings.TrimSpace(line)
						if len(snippet) > 140 {
							snippet = snippet[:140] + "…"
						}
						violations = append(violations, violation{file: rel, line: lineNo, label: v.label, snippet: snippet})
					}
				}
			}
		}
	}

	if len(violations) == 0 {
		return
	}
	var msg strings.Builder
	msg.WriteString("flow-eval scenario(s) reference a NEVER-SHIPPED launch-production design — rewrite to the shipped flow (scope-prompt → source-control → classify → ready-to-launch → launchKey mutation; existing path keyed by ExistingProjectID+ExistingProdToken; delivery family derived from source BuildIntegration):\n")
	for _, v := range violations {
		msg.WriteString("  " + v.file + ":" + itoa(v.line) + " — " + v.label + "\n      " + v.snippet + "\n")
	}
	t.Error(msg.String())
}
