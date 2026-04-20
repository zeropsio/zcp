package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// extractContentAuthoringBriefBlock returns the body of the MANDATORY
// block inside the content-authoring-brief section that carries the
// "Canonical output tree" heading (v8.104 Fix A). Empty if not found.
func extractContentAuthoringOutputTreeBlock(t *testing.T) string {
	t.Helper()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	blocks := mandatoryBlockRe.FindAllStringSubmatch(md, -1)
	for _, m := range blocks {
		body := m[1]
		if strings.Contains(body, "Canonical output tree") {
			return body
		}
	}
	return ""
}

// TestWriterBrief_CanonicalOutputTreeBlockPresent — v8.104 Fix A.
// The content-authoring-brief section carries a MANDATORY block that
// pins the writer's output paths to the canonical set and names
// BuildFinalizeOutput as the authority for root/env files.
func TestWriterBrief_CanonicalOutputTreeBlockPresent(t *testing.T) {
	t.Parallel()

	block := extractContentAuthoringOutputTreeBlock(t)
	if block == "" {
		t.Fatal("writer-brief is missing the MANDATORY \"Canonical output tree\" block (v8.104 Fix A)")
	}
	if !strings.Contains(block, "BuildFinalizeOutput") {
		t.Error("canonical-output-tree block must name `BuildFinalizeOutput` as the Go-side authority")
	}
	if !strings.Contains(block, "EnvFolder") {
		t.Error("canonical-output-tree block must name `EnvFolder` as the env-folder-name authority")
	}
	if !strings.Contains(block, "ZCP_CONTENT_MANIFEST.json") {
		t.Error("canonical-output-tree block must name the content manifest as the third allowed write target")
	}
}

// TestWriterBrief_CanonicalOutputTreeForbidsParaphrasedFolders — v8.104 Fix A.
// The block must explicitly forbid paraphrased env folder names that v33
// shipped (`Development`, `Review`, `HA production`, `Small production`)
// and name paraphrased output roots (`recipe-{slug}/`, `{slug}-output/`)
// as forbidden.
func TestWriterBrief_CanonicalOutputTreeForbidsParaphrasedFolders(t *testing.T) {
	t.Parallel()

	block := extractContentAuthoringOutputTreeBlock(t)
	if block == "" {
		t.Fatal("canonical-output-tree block not found")
	}
	// Paraphrased root names v33 invented.
	for _, forbidden := range []string{"recipe-{slug}/", "{slug}-output/"} {
		if !strings.Contains(block, forbidden) {
			t.Errorf("block must name %q as a forbidden paraphrased output root", forbidden)
		}
	}
	// Paraphrased env folder names v33 invented.
	for _, paraphrase := range []string{
		"0 — Development with agent",
		"4 — Small production",
		"5 — HA production",
	} {
		if !strings.Contains(block, paraphrase) {
			t.Errorf("block must explicitly forbid paraphrased env folder %q", paraphrase)
		}
	}
	// The canonical names must be quoted as the authoritative set.
	for _, canonical := range []string{
		"0 — AI Agent",
		"4 — Small Production",
		"5 — Highly-available Production",
	} {
		if !strings.Contains(block, canonical) {
			t.Errorf("block must name canonical env folder %q as the correct shape", canonical)
		}
	}
}

// TestZeropsYamlRules_TwoExecOnceKeyShapes — v8.104 Fix B.
// The initCommands / zerops-yaml-rules region carries the "Two
// `execOnce` keys, two lifetimes" section pairing per-deploy and static
// key shapes with their correct uses.
func TestZeropsYamlRules_TwoExecOnceKeyShapes(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if !strings.Contains(md, "Two `execOnce` keys, two lifetimes") {
		t.Error("missing \"Two `execOnce` keys, two lifetimes\" heading (v8.104 Fix B)")
	}
	if !strings.Contains(md, "bootstrap-seed-v1") {
		t.Error("section must name `bootstrap-seed-v1` as the canonical example static key")
	}
	// Must label both per-deploy and static key shapes.
	for _, phrase := range []string{
		"Per-deploy key",
		"Static key",
	} {
		if !strings.Contains(md, phrase) {
			t.Errorf("two-execOnce-keys section missing %q", phrase)
		}
	}
}

// TestZeropsYamlRules_AntiPatternForSeedOnAppVersionId — v8.104 Fix B.
// The "Anti-pattern" callout explicitly names `${appVersionId}` on
// seed as the wrong shape and ties it to the v33 apidev gotcha #7
// symptom (search index not materialized after a no-op re-seed).
func TestZeropsYamlRules_AntiPatternForSeedOnAppVersionId(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if !strings.Contains(md, "Anti-pattern") {
		t.Error("Fix B section missing \"Anti-pattern\" callout")
	}
	// The fix is the key shape, not a guard.
	if !strings.Contains(md, "The fix is the key shape") {
		t.Error("Anti-pattern callout must state the fix is the key shape, not a row-count guard")
	}
}

// TestScaffoldBrief_ForbidsShortCircuitOnRowCount — v8.104 Fix B.
// The scaffold-subagent brief's seed-script rule explicitly forbids
// short-circuiting on row count and points at the two-execOnce-keys
// section as the correct idempotency mechanism.
func TestScaffoldBrief_ForbidsShortCircuitOnRowCount(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if !strings.Contains(md, "Do NOT short-circuit on row count") {
		t.Error("scaffold-subagent seed rule must forbid row-count short-circuit (v8.104 Fix B)")
	}
	if !strings.Contains(md, "Two `execOnce` keys, two lifetimes") {
		t.Error("scaffold-subagent seed rule must reference the two-key section by name")
	}
}

// TestCommentStyle_VisualStyleSubsection — v8.104 Fix C.
// The comment-voice section carries a visual-style subsection forbidding
// Unicode box-drawing and ASCII dividers, and naming plain-`# ` as the
// only allowed shape.
func TestCommentStyle_VisualStyleSubsection(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if !strings.Contains(md, "Visual style") {
		t.Error("missing \"Visual style\" subsection in comment-voice (v8.104 Fix C)")
	}
	// Forbid-list must explicitly name box-drawing and ASCII dividers.
	if !strings.Contains(md, "Unicode box-drawing") {
		t.Error("Visual-style forbid-list must name \"Unicode box-drawing\"")
	}
	// The literal box-drawing characters themselves must appear in the
	// forbid-list so an agent scanning for examples recognizes them.
	if !strings.Contains(md, "──") {
		t.Error("Visual-style forbid-list must show a literal box-drawing example (──)")
	}
	// ASCII divider examples must be present.
	for _, divider := range []string{"# ----", "# ===="} {
		if !strings.Contains(md, divider) {
			t.Errorf("Visual-style forbid-list must name ASCII divider example %q", divider)
		}
	}
}

// TestFeatureBrief_DiagnosticProbeCadence — v8.104 Fix D.
// The feature-subagent-brief carries a "Diagnostic-probe cadence" rule
// inside a MANDATORY block: three-probe ceiling, no parallel-identical
// probes, stop and report after three.
func TestFeatureBrief_DiagnosticProbeCadence(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if !strings.Contains(md, "Diagnostic-probe cadence") {
		t.Error("feature-subagent-brief must contain \"Diagnostic-probe cadence\" rule (v8.104 Fix D)")
	}
	// Locate the MANDATORY block carrying the rule.
	blocks := mandatoryBlockRe.FindAllStringSubmatch(md, -1)
	var block string
	for _, m := range blocks {
		if strings.Contains(m[1], "Diagnostic-probe cadence") {
			block = m[1]
			break
		}
	}
	if block == "" {
		t.Fatal("Diagnostic-probe cadence rule is not inside a MANDATORY sentinel pair")
	}
	if !strings.Contains(block, "at most THREE") && !strings.Contains(block, "at most three") {
		t.Error("cadence rule must bound probes at THREE (upper-case for emphasis)")
	}
	if !strings.Contains(block, "parallel-identical probes") {
		t.Error("cadence rule must name \"parallel-identical probes\" as the forbidden shape")
	}
	if !strings.Contains(block, "STOP") {
		t.Error("cadence rule must tell the subagent to STOP and report after three")
	}
}

// TestGitConfigMountBlock_PostScaffoldReInitRule — v8.104 Fix F.
// The git-config-mount block names the scaffold-subagent `.git/` cleanup
// as the reason every post-scaffold commit requires a fresh `git init`.
func TestGitConfigMountBlock_PostScaffoldReInitRule(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	// The rule must appear in the same block as the existing git-config
	// init shape — reuse its unique preamble as an anchor.
	anchor := "Every scaffold-return commit re-runs `git init`"
	if !strings.Contains(md, anchor) {
		t.Fatal("git-config-mount block missing the post-scaffold re-init rule (v8.104 Fix F)")
	}
	// The rule must name `.git/` deletion and the sequence of events.
	if !strings.Contains(md, "deletes `/var/www/.git/`") {
		t.Error("re-init rule must name that the scaffold subagent deletes /var/www/.git/")
	}
	if !strings.Contains(md, "fatal: not a git repository") {
		t.Error("re-init rule must name the symptom if the author skips the re-init")
	}
}
