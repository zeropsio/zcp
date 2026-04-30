package content

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestNoRetiredVocabAcrossRepo is the cross-cutting drift gate that
// extends the atom-only lint (TestAtomAuthoringLint) to EVERY tracked
// text file in the repo. Without it, vocab sweeps after a refactor stay
// scoped to atoms and quietly leave stale instructions in specs, README,
// eval scenarios, preseed scripts, error messages, and tool docstrings —
// the C1-class bug pattern documented in
// docs/audit-prerelease-internal-testing-2026-04-30-roundtwo.md (Codex
// finding F1+F2: spec-work-session.md, spec-scenarios.md, and 4 preseed
// scripts kept retired `action="strategy"` / `deployStrategy` after the
// 40-file C1 sweep landed).
//
// What it catches
//
//   - `zerops_workflow action="X"` for X not in AcceptedWorkflowActions
//   - `zerops_deploy strategy="X"` for X not in AcceptedDeployStrategies
//     (and not the empty default)
//   - Hard-coded retired identifiers in any text surface:
//   - `DeployStrategy`, `StrategyConfirmed`, `PushGitTrigger`,
//     `MigrateOldMeta`/`migrateOldMeta` — Go-side names retired in
//     deploy-strategy decomposition
//   - JSON keys `"deployStrategy"`, `"strategyConfirmed"` — the
//     ServiceMeta fields renamed to `closeDeployMode` +
//     `closeDeployModeConfirmed` (silent-drift in eval preseed
//     scripts; unmarshal ignores unknown fields)
//   - Retired atom IDs `develop-push-git-deploy`, `develop-manual-deploy`
//     — referenced in specs after the C1 atom-rename sweep
//
// # Allowlist — legitimate historical references
//
// retiredVocabAllowlist holds <relative-file>::<exact-line-trimmed> →
// rationale entries. Add only when the line legitimately documents the
// retired vocabulary in past tense ("Replaces retired X" / "the legacy
// `X` value" in changelog or audit context). Every entry should justify
// itself in one short rationale string.
//
// # Scan exclusions
//
// File globs that are SKIPPED entirely (the file's whole purpose is to
// document or detect the drift, so individual line allowlist entries
// would be noise):
//
//   - plans/archive/**     historical plans
//   - docs/archive/**      historical docs
//   - docs/audit-*.md      audit reports legitimately quote the old vocab
//     as evidence of what was wrong
//   - internal/content/atoms_lint*.go         lint rules + allowlist define the patterns
//   - internal/content/repo_drift_test.go     this file
//   - internal/workflow/lifecycle_matrix_test.go   matrix-detector strings
//   - internal/eval/eval_test.go              instruction-variant + scenario assertions
//   - internal/eval/instruction_variants.go   when explicitly testing legacy-API rejection
//   - go.sum                                  binary noise
//   - **/testdata/**                          frozen test fixtures
//
// Adding a new retired token: extend `retiredIdentifiers` below + run
// `go test ./internal/content -run TestNoRetiredVocab` to surface every
// site that needs sweeping.
func TestNoRetiredVocabAcrossRepo(t *testing.T) {
	t.Parallel()

	repoRoot := findRepoRoot(t)
	files := gitTrackedFiles(t, repoRoot)

	accepted := make(map[string]bool, len(AcceptedWorkflowActions)+len(AcceptedDeployStrategies))
	for _, a := range AcceptedWorkflowActions {
		accepted["workflow:"+a] = true
	}
	for _, s := range AcceptedDeployStrategies {
		accepted["deploy:"+s] = true
	}
	// `zerops_deploy strategy=""` (omitted strategy = default zcli) is
	// always accepted — the regex requires a quoted value, so the
	// missing-arg case never reaches the check.

	type violation struct {
		File    string
		Line    int
		Pattern string
		Snippet string
	}
	var violations []violation

	for _, rel := range files {
		if skipForDriftScan(rel) {
			continue
		}
		full := filepath.Join(repoRoot, rel)
		body, err := os.ReadFile(full)
		if err != nil {
			// Symlinks, deleted-but-tracked entries, etc. — skip.
			continue
		}
		if !looksLikeText(body) {
			continue
		}
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			key := rel + "::" + trimmed
			if _, allowed := retiredVocabAllowlist[key]; allowed {
				continue
			}

			// 1. zerops_workflow action="X" — X must be accepted.
			for _, m := range workflowActionRe.FindAllStringSubmatch(line, -1) {
				if !accepted["workflow:"+m[1]] {
					violations = append(violations, violation{
						File: rel, Line: lineNo,
						Pattern: `zerops_workflow action="` + m[1] + `" (retired action)`,
						Snippet: trim(trimmed, 140),
					})
				}
			}
			// 2. zerops_deploy strategy="X" — X must be accepted.
			for _, m := range deployStrategyRe.FindAllStringSubmatch(line, -1) {
				if !accepted["deploy:"+m[1]] {
					violations = append(violations, violation{
						File: rel, Line: lineNo,
						Pattern: `zerops_deploy strategy="` + m[1] + `" (retired strategy)`,
						Snippet: trim(trimmed, 140),
					})
				}
			}
			// 3. Hard-coded retired identifiers anywhere in the line.
			for _, ri := range retiredIdentifiers {
				if ri.pattern.MatchString(line) {
					violations = append(violations, violation{
						File: rel, Line: lineNo,
						Pattern: ri.label,
						Snippet: trim(trimmed, 140),
					})
				}
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
	msg.WriteString("retired-vocab drift across repo (sweep into the canonical replacement; add to retiredVocabAllowlist only for past-tense documentation):\n")
	for _, v := range violations {
		msg.WriteString("  " + v.File + ":" + itoa(v.Line) + " — " + v.Pattern + "\n      " + v.Snippet + "\n")
	}
	t.Error(msg.String())
}

// retiredIdentifier is one hard-coded forbidden token + a label
// describing what the canonical replacement is, so the failure message
// points at the fix rather than just the symptom.
type retiredIdentifier struct {
	pattern *regexp.Regexp
	label   string
}

var retiredIdentifiers = []retiredIdentifier{
	{
		pattern: regexp.MustCompile(`\bDeployStrategy\b`),
		label:   "DeployStrategy (retired Go type — replaced by CloseDeployMode + GitPushState + BuildIntegration)",
	},
	{
		pattern: regexp.MustCompile(`\bStrategyConfirmed\b`),
		label:   "StrategyConfirmed (retired field — replaced by CloseDeployModeConfirmed)",
	},
	{
		pattern: regexp.MustCompile(`\bPushGitTrigger\b`),
		label:   "PushGitTrigger (retired Go type — replaced by BuildIntegration)",
	},
	{
		pattern: regexp.MustCompile(`\b[Mm]igrateOldMeta\b`),
		label:   "migrateOldMeta (retired migration helper — deleted in deploy-strategy decomposition)",
	},
	{
		pattern: regexp.MustCompile(`"deployStrategy"\s*:`),
		label:   `JSON key "deployStrategy" (retired ServiceMeta field — replaced by "closeDeployMode")`,
	},
	{
		pattern: regexp.MustCompile(`"strategyConfirmed"\s*:`),
		label:   `JSON key "strategyConfirmed" (retired ServiceMeta field — replaced by "closeDeployModeConfirmed")`,
	},
	{
		pattern: regexp.MustCompile(`\bdevelop-push-git-deploy\b`),
		label:   "develop-push-git-deploy atom (retired — replaced by develop-close-mode-git-push)",
	},
	{
		pattern: regexp.MustCompile(`\bdevelop-manual-deploy\b`),
		label:   "develop-manual-deploy atom (retired — replaced by develop-close-mode-manual)",
	},
	{
		pattern: regexp.MustCompile(`\bdevelop-push-dev-(deploy|workflow)`),
		label:   "develop-push-dev-* atom (retired — replaced by develop-close-mode-auto-*)",
	},
	{
		pattern: regexp.MustCompile(`\bdevelop-close-push-dev-`),
		label:   "develop-close-push-dev-* atom (retired — renamed to develop-close-mode-auto-*)",
	},
	{
		pattern: regexp.MustCompile(`strategies\s*=\s*\{`),
		label:   `strategies={…} action argument (retired — split into close-mode + git-push-setup + build-integration)`,
	},
}

// retiredVocabAllowlist exempts <relative-file>::<exact-line-trimmed>
// pairs that legitimately document the retired vocabulary in past tense.
// Each entry needs a one-line rationale documenting WHY the line is
// allowed — the value is shown when the entry is reviewed.
var retiredVocabAllowlist = map[string]string{
	"docs/spec-knowledge-distribution.md::| `strategy-setup` | Stateless synthesis phase emitted from `action=\"git-push-setup\"` (provisions GIT_TOKEN / .netrc / RemoteURL) and `action=\"build-integration\"` (wires webhook / actions). Replaces retired `cicd-active` and the conflated `action=\"strategy\"` entry point. |":                                                                                                                                                                                                                                                                                                   "past-tense documentation of what was retired in deploy-strategy decomposition",
	"docs/spec-knowledge-distribution.md::Callers are responsible for joining. The status renderer (`RenderStatus`) emits each body as a separate paragraph in the \"Guidance\" section, separated by blank lines. Stateless synthesis (`strategy-setup`, `export-active`) uses `SynthesizeImmediateWorkflow(phase, env)` which joins bodies with `\\n\\n---\\n\\n` and returns a single string. `strategy-setup` is invoked from `handleGitPushSetup` and `handleBuildIntegration` (the two split actions that replaced the retired `action=\"strategy\"`); `export-active` is invoked from the `workflow=export` immediate entry.": "past-tense reference to the retired action in the rendering doc",
	"CLAUDE.md::`GitPushState=configured`). The legacy `DeployStrategy` + `PushGitTrigger`": "past-tense documentation of the retired Go types in CLAUDE.md three-axis explanation",
}

// skipForDriftScan returns true for files whose ENTIRE content is exempt
// from the drift scan.
//
// Design philosophy: the drift gate enforces vocabulary cleanliness on
// the SURFACES THAT TEACH THE AGENT — code, specs, atoms, README, eval
// scripts, CLAUDE.md (per-line allowlist for past-tense documentation
// passages there). Plans (`plans/*.md`) are design scratchpads that
// legitimately reference retired vocab as design history; they archive
// when the work ships. Recipe-flow docs (`docs/zcprecipator*/`) are
// recipe-AUTHORING workflow content, out of scope for the lifecycle
// vocabulary sweep. Historical archives never participate.
func skipForDriftScan(rel string) bool {
	switch {
	case strings.HasPrefix(rel, "plans/"),
		strings.HasPrefix(rel, "docs/archive/"),
		strings.HasPrefix(rel, "docs/audit-"),
		strings.HasPrefix(rel, "docs/zcprecipator"),
		strings.HasPrefix(rel, "docs/recipes/"),
		strings.HasPrefix(rel, "internal/recipe/"),
		strings.HasPrefix(rel, "internal/content/workflows/recipe"),
		strings.HasPrefix(rel, "internal/content/recipes/"),
		strings.HasPrefix(rel, "internal/knowledge/recipes/"),
		strings.HasSuffix(rel, "go.sum"),
		strings.HasSuffix(rel, ".png"),
		strings.HasSuffix(rel, ".jpg"),
		strings.HasSuffix(rel, ".gif"),
		strings.HasSuffix(rel, ".pdf"):
		return true
	}
	switch rel {
	case "internal/content/atoms_lint.go",
		"internal/content/atoms_lint_axes.go",
		"internal/content/atoms_lint_seed_allowlist.go",
		"internal/content/atoms_lint_test.go",
		"internal/content/atoms_lint_axes_test.go",
		"internal/content/repo_drift_test.go",
		"internal/workflow/lifecycle_matrix_test.go",
		"internal/eval/eval_test.go",
		"internal/eval/instruction_variants.go":
		return true
	}
	return strings.Contains(rel, "/testdata/")
}

// looksLikeText returns true when the buffer is plausibly a text file
// (no NUL bytes in the first 8 KB). Drops binary-tracked files
// (compiled assets, archives, etc.) without needing an extension list.
func looksLikeText(b []byte) bool {
	limit := min(len(b), 8192)
	return !bytes.ContainsRune(b[:limit], 0)
}

// findRepoRoot walks up from the test binary's working directory until
// it finds a directory containing `go.mod` — that's the canonical repo
// root. Tests run from the package directory by default, so a few `..`
// hops always land on it.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 8 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root (go.mod) not found from " + dir)
	return ""
}

// gitTrackedFiles returns every file currently tracked in git, relative
// to repoRoot. The drift scan deliberately walks via git ls-files (not
// filesystem walk) so untracked files (build artifacts, local state) are
// invisible to the gate. Skips when git is unavailable (CI without git
// installed).
func gitTrackedFiles(t *testing.T, repoRoot string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-files")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files unavailable (%v) — drift scan skipped", err)
	}
	var files []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, line)
	}
	return files
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
