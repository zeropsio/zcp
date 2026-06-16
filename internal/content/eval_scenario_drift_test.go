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

// retiredMechanicsVocab are semantic tokens that name MECHANICS the shipped
// system no longer has (or never had). Unlike neverShippedLaunchVocab (which
// names a whole never-shipped design), these are individual stale-mechanic
// literals that a scenario can drift into one prose line at a time while the
// compile-guard (`make vet-tags`) stays green — stale string literals compile
// fine (2026-06-16 consolidation audit, structural fix #1):
//   - ZEROPS_TOKEN_STAGE — a phantom stage repo secret from the never-shipped
//     inline cicd-method-prompt; the only shipped CI repo secret is
//     ZEROPS_TOKEN_PROD (the staged service secret is ZCP_LAUNCH_TOKEN).
//   - re-(call|supply|enter|paste) … same launchKey — trains the agent to
//     re-send the one-shot launch token on resume; the SHIPPED resume reads
//     it STAGE-FIRST from the staged ZCP_LAUNCH_TOKEN secret (P-LP-14), never
//     re-supplied.
//   - closeMode: git-push / "git-push close-mode" — the legacy git-push
//     close-mode value folds to `auto`; delivery is DERIVED from GitPushState,
//     not chosen by a close-mode value (spec-workflows §4.3).
//   - .netrc — git-push auth is via the container's git credential helper
//     reading the GIT_TOKEN service secret, never a .netrc file.
var retiredMechanicsVocab = []struct {
	pattern *regexp.Regexp
	label   string
}{
	{regexp.MustCompile(`\bZEROPS_TOKEN_STAGE\b`), "ZEROPS_TOKEN_STAGE (retired — phantom stage repo secret from the never-shipped inline cicd-method-prompt; the shipped CI repo secret is ZEROPS_TOKEN_PROD, the staged service secret is ZCP_LAUNCH_TOKEN)"},
	{regexp.MustCompile(`(?i)re-?(call|run|suppl(?:y|ies|ied)|enter|paste|send|use)[^.\n]{0,120}\bsame\b[^.\n]{0,40}launch[ -]?[kK]ey`), "re-supply same launchKey (retired — the one-shot launch token is NOT re-sent on resume; the shipped resume reads it STAGE-FIRST from the staged ZCP_LAUNCH_TOKEN secret, P-LP-14)"},
	{regexp.MustCompile(`(?i)\bsame\b[^.\n]{0,40}launch[ -]?[kK]ey[^.\n]{0,120}\bre-?(call|run|use|send|paste|enter|suppl)`), "re-supply same launchKey (retired — the one-shot launch token is NOT re-sent on resume; the shipped resume reads it STAGE-FIRST from the staged ZCP_LAUNCH_TOKEN secret, P-LP-14)"},
	{regexp.MustCompile(`(?i)close[ -]?mode\s*[:=]\s*git-push`), "closeMode=git-push (retired — the legacy git-push close-mode value folds to `auto`; delivery is DERIVED from GitPushState, spec-workflows §4.3)"},
	{regexp.MustCompile(`(?i)git-push close-?mode`), "\"git-push close-mode\" (retired — the legacy git-push close-mode value folds to `auto`; delivery is DERIVED from GitPushState, spec-workflows §4.3)"},
	{regexp.MustCompile(`\.netrc\b`), ".netrc (retired — git-push auth is via the container git credential helper reading the GIT_TOKEN service secret, never a .netrc file)"},
}

// TestNoRetiredMechanicsVocabInEvalScenarios keeps the flow-eval behavioral
// scenarios honest about MECHANICS the same way
// TestNoNeverShippedLaunchVocabInEvalScenarios keeps them honest about the
// never-shipped DESIGN: a scenario that names a retired mechanic
// (ZEROPS_TOKEN_STAGE, re-supply-same-launchKey, closeMode=git-push, .netrc)
// trains the agent against behavior the shipped handler cannot produce.
//
// Scoped to eval/behavioral/scenarios{,-local}/ deliberately — plans/ and
// archived design docs legitimately name these retired mechanics as history,
// so the whole-repo drift gate must NOT claim them; only the eval scenarios,
// which the harness runs against the real handler, must stay clean. Mirrors
// the structure of TestNoNeverShippedLaunchVocabInEvalScenarios so both drift
// classes are caught by the same scan shape.
func TestNoRetiredMechanicsVocabInEvalScenarios(t *testing.T) {
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
				for _, v := range retiredMechanicsVocab {
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
	msg.WriteString("flow-eval scenario(s) reference a RETIRED mechanic — rewrite to the shipped behavior (single-token resume reads the staged ZCP_LAUNCH_TOKEN STAGE-FIRST; delivery is DERIVED from GitPushState; git-push auth is via the container credential helper, not .netrc; the CI repo secret is ZEROPS_TOKEN_PROD):\n")
	for _, v := range violations {
		msg.WriteString("  " + v.file + ":" + itoa(v.line) + " — " + v.label + "\n      " + v.snippet + "\n")
	}
	t.Error(msg.String())
}
